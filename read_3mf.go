package meshio

import (
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// parseContentTypeOverrides extracts PartName -> ContentType from the OPC
// [Content_Types].xml <Override> elements.
func parseContentTypeOverrides(xmlText string) map[string]string {
	out := map[string]string{}
	dec := xml.NewDecoder(strings.NewReader(xmlText))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "Override" {
			continue
		}
		var part, ct string
		for _, a := range se.Attr {
			switch a.Name.Local {
			case "PartName":
				part = a.Value
			case "ContentType":
				ct = a.Value
			}
		}
		if part != "" {
			out[part] = ct
		}
	}
	return out
}

// meshData holds the raw vertex/index/color data parsed from a <mesh> element.
type meshData struct {
	vertices   []float32
	indices    []uint32
	faceColors []FaceColor
}

// componentRef is a reference from a component object to another object, with
// an optional part path (production extension) and a local transform.
type componentRef struct {
	objectID  string
	path      string // p:path (leading slash); "" = same part
	transform affine
}

// modelObject is one <object> in a 3MF model part. Either mesh is set (mesh
// object) or components is non-empty (component object).
type modelObject struct {
	mesh       *meshData
	components []componentRef
}

// buildItem is one <item> in the <build> section.
type buildItem struct {
	objectID  string
	transform affine
}

// parsedPart is the result of parsing one 3MF model XML part.
type parsedPart struct {
	objects    map[string]*modelObject
	buildItems []buildItem
}

// parseModelPart parses one 3MF model XML part into its object graph (objects +
// build items). Mesh objects carry vertices/triangles/per-face colors; component
// objects carry references (with transforms) to other objects/parts. A part with
// no inline mesh is valid (pure components) — it is NOT an error.
func parseModelPart(r io.Reader) (*parsedPart, error) {
	decoder := xml.NewDecoder(r)
	part := &parsedPart{objects: map[string]*modelObject{}}

	colorGroups := map[string][]string{} // part-scoped palettes
	var currentGroupID string
	var curObjID string
	var cur *modelObject
	var inVertices, inTriangles bool

	attr := func(attrs []xml.Attr, name string) string {
		for _, a := range attrs {
			if a.Name.Local == name {
				return a.Value
			}
		}
		return ""
	}
	xformOf := func(attrs []xml.Attr) affine {
		if s := attr(attrs, "transform"); s != "" {
			if m, err := parseAffine(s); err == nil {
				return m
			}
		}
		return identityAffine()
	}

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
			switch t.Name.Local {
			case "object":
				curObjID = attr(t.Attr, "id")
				cur = &modelObject{}
			case "mesh":
				if cur != nil {
					cur.mesh = &meshData{}
				}
			case "vertices":
				inVertices = true
			case "triangles":
				inTriangles = true
			case "colorgroup":
				currentGroupID = attr(t.Attr, "id")
				if currentGroupID != "" {
					colorGroups[currentGroupID] = nil
				}
			case "color":
				if currentGroupID != "" {
					if c := attr(t.Attr, "color"); c != "" {
						colorGroups[currentGroupID] = append(colorGroups[currentGroupID], c)
					}
				}
			case "vertex":
				if inVertices && cur != nil && cur.mesh != nil {
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
					cur.mesh.vertices = append(cur.mesh.vertices, float32(x), float32(y), float32(z))
				}
			case "triangle":
				if inTriangles && cur != nil && cur.mesh != nil {
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
					cur.mesh.indices = append(cur.mesh.indices, v1, v2, v3)
					hex := ""
					if pid != "" && p1 != "" {
						if pal, ok := colorGroups[pid]; ok {
							if idx, e := strconv.Atoi(p1); e == nil && idx >= 0 && idx < len(pal) {
								hex = pal[idx]
							}
						}
					}
					cur.mesh.faceColors = append(cur.mesh.faceColors, FaceColor{Hex: hex})
				}
			case "component":
				if cur != nil {
					cur.components = append(cur.components, componentRef{
						objectID:  attr(t.Attr, "objectid"),
						path:      attr(t.Attr, "path"), // p:path → Local "path"
						transform: xformOf(t.Attr),
					})
				}
			case "item":
				part.buildItems = append(part.buildItems, buildItem{
					objectID:  attr(t.Attr, "objectid"),
					transform: xformOf(t.Attr),
				})
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "object":
				if curObjID != "" && cur != nil {
					part.objects[curObjID] = cur
				}
				curObjID, cur = "", nil
			case "vertices":
				inVertices = false
			case "triangles":
				inTriangles = false
			case "colorgroup":
				currentGroupID = ""
			}
		}
	}
	return part, nil
}

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
