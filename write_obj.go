package meshio

import (
	"bufio"
	"fmt"
	"io"
	"os"
)

// EncodeOBJ writes the mesh as Wavefront OBJ to w.
// If mtlW is non-nil and the mesh has face colors, material definitions are written to mtlW
// and a mtllib directive referencing mtlName is included.
func (m *Mesh) EncodeOBJ(w io.Writer, mtlW io.Writer) error {
	m.MergeVertices()
	numVerts := len(m.Vertices) / 3
	numTris := len(m.Indices) / 3

	if numVerts == 0 || numTris == 0 {
		return fmt.Errorf("meshio: empty mesh")
	}

	bw := bufio.NewWriter(w)

	hasMtl := mtlW != nil && len(m.FaceColors) == numTris
	if hasMtl {
		if err := encodeMtl(m, mtlW); err != nil {
			return err
		}
		fmt.Fprintf(bw, "mtllib material.mtl\n")
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
func encodeMtl(m *Mesh, w io.Writer) error {
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

// WriteOBJ exports a Mesh to a Wavefront OBJ file at the given path.
// A companion .mtl file is written alongside if face colors are present.
func (m *Mesh) WriteOBJ(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("meshio: %w", err)
	}
	defer f.Close()

	hasMtl := len(m.FaceColors) == len(m.Indices)/3
	var mtlFile *os.File
	if hasMtl {
		mtlPath := pathDir(path) + "/" + pathStem(path) + ".mtl"
		mtlFile, err = os.Create(mtlPath)
		if err != nil {
			return fmt.Errorf("meshio: %w", err)
		}
		defer mtlFile.Close()
	}

	return m.EncodeOBJ(f, mtlFile)
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
