// Package obj reads and writes Wavefront OBJ meshes. Per-face colors are
// written as a companion .mtl material library referenced by usemtl.
package obj

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/firstlayer-xyz/meshio/geom"
)

// Decode reads a Wavefront OBJ from r.
// Only triangular and quad faces are supported; quads are split into two triangles.
func Decode(r io.Reader) (*geom.Mesh, error) {
	scanner := bufio.NewScanner(r)
	var vertices []float32
	var indices []uint32

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) == 0 || line[0] == '#' {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		switch fields[0] {
		case "v":
			if len(fields) < 4 {
				continue
			}
			x, err1 := strconv.ParseFloat(fields[1], 32)
			y, err2 := strconv.ParseFloat(fields[2], 32)
			z, err3 := strconv.ParseFloat(fields[3], 32)
			if err1 != nil || err2 != nil || err3 != nil {
				continue
			}
			vertices = append(vertices, float32(x), float32(y), float32(z))

		case "f":
			faceVerts := make([]uint32, 0, len(fields)-1)
			for _, f := range fields[1:] {
				// OBJ face can be v, v/vt, v/vt/vn, or v//vn
				parts := strings.SplitN(f, "/", 2)
				idx, err := strconv.ParseUint(parts[0], 10, 32)
				if err != nil {
					continue
				}
				// OBJ indices are 1-based; negative means relative
				if idx == 0 {
					continue
				}
				faceVerts = append(faceVerts, uint32(idx-1))
			}
			// Triangulate: fan from first vertex
			for i := 2; i < len(faceVerts); i++ {
				indices = append(indices, faceVerts[0], faceVerts[i-1], faceVerts[i])
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("meshio: reading OBJ: %w", err)
	}

	if len(vertices) == 0 {
		return nil, fmt.Errorf("meshio: no vertices found in OBJ")
	}

	return &geom.Mesh{Geometry: geom.Geometry{Vertices: vertices, Indices: indices}}, nil
}

// Encode writes the mesh as Wavefront OBJ to w.
// If mtlW is non-nil and the mesh has face colors, material definitions are written to mtlW
// and a "mtllib <mtlName>" directive is emitted referencing it. mtlName should be the
// basename the caller will give the .mtl file (mtllib is resolved relative to the .obj
// file, not as an absolute or caller-relative path), and is unused when mtlW is nil.
// If mtlW is non-nil and mtlName is empty, Encode returns an error rather than emitting
// a directive with no target: an mtllib line consumers can't resolve is worse than no
// mtllib line at all, since it silently discards every face color on read-back.
// It mutates m: MergeVertices is called on the caller's mesh as a side effect.
func Encode(w io.Writer, m *geom.Mesh, mtlW io.Writer, mtlName string) error {
	m.MergeVertices()
	numVerts := len(m.Vertices) / 3
	numTris := len(m.Indices) / 3

	if numVerts == 0 || numTris == 0 {
		return fmt.Errorf("meshio: empty mesh")
	}

	hasMtl := mtlW != nil && len(m.FaceColors) == numTris
	if hasMtl && mtlName == "" {
		return fmt.Errorf("meshio: mtlName is required when mtlW is non-nil")
	}

	bw := bufio.NewWriter(w)

	if hasMtl {
		if err := encodeMtl(m, mtlW); err != nil {
			return err
		}
		fmt.Fprintf(bw, "mtllib %s\n", mtlName)
	}

	// Vertices
	for i := 0; i < numVerts; i++ {
		fmt.Fprintf(bw, "v %g %g %g\n",
			m.Vertices[i*3], m.Vertices[i*3+1], m.Vertices[i*3+2])
	}

	// Faces (OBJ indices are 1-based)
	if hasMtl {
		currentMtl := ""
		for i := 0; i < numTris; i++ {
			mtl := "mat_" + sanitizeHex(m.FaceColors[i].Hex)
			if mtl != currentMtl {
				fmt.Fprintf(bw, "usemtl %s\n", mtl)
				currentMtl = mtl
			}
			fmt.Fprintf(bw, "f %d %d %d\n",
				m.Indices[i*3]+1, m.Indices[i*3+1]+1, m.Indices[i*3+2]+1)
		}
	} else {
		for i := 0; i < numTris; i++ {
			fmt.Fprintf(bw, "f %d %d %d\n",
				m.Indices[i*3]+1, m.Indices[i*3+1]+1, m.Indices[i*3+2]+1)
		}
	}

	return bw.Flush()
}

// encodeMtl writes MTL material definitions to w.
func encodeMtl(m *geom.Mesh, w io.Writer) error {
	bw := bufio.NewWriter(w)
	seen := make(map[string]bool)
	for _, fc := range m.FaceColors {
		key := sanitizeHex(fc.Hex)
		if seen[key] {
			continue
		}
		seen[key] = true
		r, g, b := parseHexColor(fc.Hex)
		fmt.Fprintf(bw, "newmtl mat_%s\n", key)
		fmt.Fprintf(bw, "Kd %g %g %g\n\n", r, g, b)
	}
	return bw.Flush()
}

// Read reads a Wavefront OBJ file.
func Read(path string) (*geom.Mesh, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("meshio: %w", err)
	}
	defer f.Close()
	return Decode(f)
}

// Write exports a Mesh to a Wavefront OBJ file at the given path.
// A companion .mtl file is written alongside if face colors are present.
func Write(path string, m *geom.Mesh) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("meshio: %w", err)
	}
	defer f.Close()

	hasMtl := len(m.FaceColors) == len(m.Indices)/3
	var mtlFile *os.File
	var mtlName string
	if hasMtl {
		mtlName = pathStem(path) + ".mtl"
		mtlFile, err = os.Create(pathDir(path) + "/" + mtlName)
		if err != nil {
			return fmt.Errorf("meshio: %w", err)
		}
		defer mtlFile.Close()
	}

	return Encode(f, m, mtlFile, mtlName)
}

// parseHexColor converts "#RRGGBB" or "#RRGGBBAA" to [0,1] floats.
func parseHexColor(hex string) (r, g, b float64) {
	if len(hex) > 0 && hex[0] == '#' {
		hex = hex[1:]
	}
	if len(hex) < 6 {
		return 0.8, 0.8, 0.8
	}
	ri := hexByte(hex[0])<<4 | hexByte(hex[1])
	gi := hexByte(hex[2])<<4 | hexByte(hex[3])
	bi := hexByte(hex[4])<<4 | hexByte(hex[5])
	return float64(ri) / 255, float64(gi) / 255, float64(bi) / 255
}

func hexByte(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	}
	return 0
}

// sanitizeHex strips '#' and lowercases for use as a material name.
func sanitizeHex(hex string) string {
	out := make([]byte, 0, len(hex))
	for i := 0; i < len(hex); i++ {
		c := hex[i]
		if c == '#' {
			continue
		}
		if c >= 'A' && c <= 'F' {
			c = c - 'A' + 'a'
		}
		out = append(out, c)
	}
	return string(out)
}

// pathStem returns the filename without extension.
func pathStem(path string) string {
	base := path
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			base = path[i+1:]
			break
		}
	}
	for i := len(base) - 1; i > 0; i-- {
		if base[i] == '.' {
			return base[:i]
		}
	}
	return base
}

// pathDir returns the directory portion of a path.
func pathDir(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return "."
}
