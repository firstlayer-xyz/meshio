package meshio

import (
	"encoding/xml"
	"fmt"
	"io"
	"sort"
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
	xformOf := func(attrs []xml.Attr) (affine, error) {
		if s := attr(attrs, "transform"); s != "" {
			return parseAffine(s)
		}
		return identityAffine(), nil
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
					tf, err := xformOf(t.Attr)
					if err != nil {
						return nil, fmt.Errorf("meshio: 3mf component transform: %w", err)
					}
					cur.components = append(cur.components, componentRef{
						objectID:  attr(t.Attr, "objectid"),
						path:      attr(t.Attr, "path"), // p:path → Local "path"
						transform: tf,
					})
				}
			case "item":
				tf, err := xformOf(t.Attr)
				if err != nil {
					return nil, fmt.Errorf("meshio: 3mf build item transform: %w", err)
				}
				part.buildItems = append(part.buildItems, buildItem{
					objectID:  attr(t.Attr, "objectid"),
					transform: tf,
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

const maxComponentDepth = 256

// resolveBuild walks the root part's build items, recursing through components
// across parts (parsing referenced parts on demand via getPart), applies the
// composed transforms, and appends all geometry into one merged Mesh.
func resolveBuild(root *parsedPart, getPart func(path string) (*parsedPart, error)) (*Mesh, error) {
	out := &Mesh{}
	var resolve func(part *parsedPart, objID string, acc affine, depth int, seen map[string]bool) error
	resolve = func(part *parsedPart, objID string, acc affine, depth int, seen map[string]bool) error {
		if depth > maxComponentDepth {
			return fmt.Errorf("meshio: 3mf component nesting too deep (cycle?)")
		}
		obj := part.objects[objID]
		if obj == nil {
			return fmt.Errorf("meshio: 3mf object %q not found", objID)
		}
		if obj.mesh != nil {
			base := uint32(len(out.Vertices) / 3)
			for i := 0; i+2 < len(obj.mesh.vertices); i += 3 {
				x, y, z := acc.apply(obj.mesh.vertices[i], obj.mesh.vertices[i+1], obj.mesh.vertices[i+2])
				out.Vertices = append(out.Vertices, x, y, z)
			}
			for _, idx := range obj.mesh.indices {
				out.Indices = append(out.Indices, base+idx)
			}
			out.FaceColors = append(out.FaceColors, obj.mesh.faceColors...)
			return nil
		}
		for _, c := range obj.components {
			key := c.path + "#" + c.objectID
			if seen[key] {
				return fmt.Errorf("meshio: 3mf component cycle at %q", key)
			}
			seen[key] = true
			cp := part
			if c.path != "" {
				var err error
				if cp, err = getPart(c.path); err != nil {
					return err
				}
			}
			if err := resolve(cp, c.objectID, acc.mul(c.transform), depth+1, seen); err != nil {
				return err
			}
			delete(seen, key)
		}
		return nil
	}

	items := root.buildItems
	for _, it := range items {
		if err := resolve(root, it.objectID, it.transform, 0, map[string]bool{"#" + it.objectID: true}); err != nil {
			return nil, err
		}
	}
	if len(items) == 0 {
		// No build section: fall back to every top-level mesh object at identity.
		ids := make([]string, 0, len(root.objects))
		for objID := range root.objects {
			ids = append(ids, objID)
		}
		sort.Strings(ids)
		for _, objID := range ids {
			if root.objects[objID].mesh != nil {
				if err := resolve(root, objID, identityAffine(), 0, map[string]bool{}); err != nil {
					return nil, err
				}
			}
		}
	}
	if len(out.Vertices) == 0 {
		return nil, fmt.Errorf("meshio: no renderable geometry in 3mf")
	}
	normalizeFaceColors(out)
	return out, nil
}

// normalizeFaceColors strips a fully-FF alpha suffix and drops the slice when no
// triangle carries color (matching the pre-rework behavior).
func normalizeFaceColors(m *Mesh) {
	any := false
	for _, fc := range m.FaceColors {
		if fc.Hex != "" {
			any = true
			break
		}
	}
	if !any {
		m.FaceColors = nil
		return
	}
	for i, fc := range m.FaceColors {
		if len(fc.Hex) == 9 && strings.HasSuffix(strings.ToUpper(fc.Hex), "FF") {
			m.FaceColors[i].Hex = fc.Hex[:7]
		}
	}
}
