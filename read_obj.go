package meshio

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// DecodeOBJ reads a Wavefront OBJ from r.
// Only triangular and quad faces are supported; quads are split into two triangles.
func DecodeOBJ(r io.Reader) (*Mesh, error) {
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

	return &Mesh{Vertices: vertices, Indices: indices}, nil
}
