package meshio

import (
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// decode3MFModel parses the 3D model XML from a 3MF archive.
func decode3MFModel(r io.Reader) (*Mesh, error) {
	decoder := xml.NewDecoder(r)

	var vertices []float32
	var indices []uint32
	var faceColors []FaceColor

	// Color group palette: id -> list of hex colors
	colorGroups := map[string][]string{}
	var currentGroupID string
	var inVertices, inTriangles bool

	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("meshio: parsing 3mf xml: %w", err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			local := t.Name.Local
			switch local {
			case "vertices":
				inVertices = true
			case "triangles":
				inTriangles = true
			case "colorgroup":
				for _, a := range t.Attr {
					if a.Name.Local == "id" {
						currentGroupID = a.Value
						colorGroups[a.Value] = nil
					}
				}
			case "color":
				if currentGroupID != "" {
					for _, a := range t.Attr {
						if a.Name.Local == "color" {
							colorGroups[currentGroupID] = append(colorGroups[currentGroupID], a.Value)
						}
					}
				}
			case "vertex":
				if inVertices {
					var x, y, z float64
					for _, a := range t.Attr {
						switch a.Name.Local {
						case "x":
							x, _ = strconv.ParseFloat(a.Value, 32)
						case "y":
							y, _ = strconv.ParseFloat(a.Value, 32)
						case "z":
							z, _ = strconv.ParseFloat(a.Value, 32)
						}
					}
					vertices = append(vertices, float32(x), float32(y), float32(z))
				}
			case "triangle":
				if inTriangles {
					var v1, v2, v3 uint32
					var pid, p1 string
					for _, a := range t.Attr {
						switch a.Name.Local {
						case "v1":
							n, _ := strconv.ParseUint(a.Value, 10, 32)
							v1 = uint32(n)
						case "v2":
							n, _ := strconv.ParseUint(a.Value, 10, 32)
							v2 = uint32(n)
						case "v3":
							n, _ := strconv.ParseUint(a.Value, 10, 32)
							v3 = uint32(n)
						case "pid":
							pid = a.Value
						case "p1":
							p1 = a.Value
						}
					}
					indices = append(indices, v1, v2, v3)

					// Resolve color
					hex := ""
					if pid != "" && p1 != "" {
						if palette, ok := colorGroups[pid]; ok {
							idx, err := strconv.Atoi(p1)
							if err == nil && idx >= 0 && idx < len(palette) {
								hex = palette[idx]
							}
						}
					}
					faceColors = append(faceColors, FaceColor{Hex: hex})
				}
			}

		case xml.EndElement:
			local := t.Name.Local
			switch local {
			case "vertices":
				inVertices = false
			case "triangles":
				inTriangles = false
			case "colorgroup":
				currentGroupID = ""
			}
		}
	}

	if len(vertices) == 0 {
		return nil, fmt.Errorf("meshio: no vertices found in 3mf model")
	}

	// Strip face colors if none have actual color data
	hasAnyColor := false
	for _, fc := range faceColors {
		if fc.Hex != "" {
			hasAnyColor = true
			break
		}
	}
	if !hasAnyColor {
		faceColors = nil
	}

	// Normalize hex colors: strip alpha if "#RRGGBBFF"
	for i, fc := range faceColors {
		if len(fc.Hex) == 9 && strings.HasSuffix(strings.ToUpper(fc.Hex), "FF") {
			faceColors[i].Hex = fc.Hex[:7]
		}
	}

	return &Mesh{
		Vertices:   vertices,
		Indices:    indices,
		FaceColors: faceColors,
	}, nil
}
