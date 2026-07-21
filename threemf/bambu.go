package threemf

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	nsCore         = "http://schemas.microsoft.com/3dmanufacturing/core/2015/02"
	nsProduction   = "http://schemas.microsoft.com/3dmanufacturing/production/2015/06"
	nsBambu        = "http://schemas.bambulab.com/package/2021"
	relType3DModel = "http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel"
	identityXform  = "1 0 0 0 1 0 0 0 1 0 0 0"
)

// bambuLayout is the id and path assignment for one Bambu package. Every id
// comes from a single resourceIDs counter, so a part object can never collide
// with the container or the colorgroup no matter how many parts there are.
//
// The container lives in 3D/3dmodel.model while the parts and colorgroup live
// in 3D/Objects/object_N.model. Ids only have to be unique per model part, but
// the production extension has components reference parts across files, so the
// package uses one id space throughout rather than restarting per file.
type bambuLayout struct {
	partIDs      []int  // partIDs[i] is the resource id of o.Parts[i]
	containerID  int    // the components object, also names objectsPath
	colorGroupID int    // 0 when the object has no slot colors
	objectsPath  string // package path of the part-geometry model part
}

// layout assigns the package's resource ids. hasPalette determines only whether
// a colorgroup id is reserved; part and container ids do not depend on it, so
// callers that need just a path or the container id may pass false.
func (o *Object) layout(hasPalette bool) bambuLayout {
	var ids resourceIDs
	l := bambuLayout{partIDs: make([]int, len(o.Parts))}
	for i := range o.Parts {
		l.partIDs[i] = ids.next()
	}
	l.containerID = ids.next()
	if hasPalette {
		l.colorGroupID = ids.next()
	}
	l.objectsPath = fmt.Sprintf("3D/Objects/object_%d.model", l.containerID)
	return l
}

// EncodeBambu writes the object as a Bambu Studio 3MF: a container object whose
// components are the parts, part geometry in 3D/Objects, and filament slot
// assignments in Metadata/model_settings.config.
//
// Unlike Encode, which carries display color only, this produces a file that
// slices multi-material: each part is bound to a filament slot.
//
// Unlike Encode, stl.Encode, and obj.Encode, EncodeBambu does not mutate its
// input: it never calls MergeVertices on o's part geometry.
func EncodeBambu(w io.Writer, o *Object) error {
	if err := o.validate(); err != nil {
		return err
	}

	palette, paletteBySlot := o.palette()
	l := o.layout(len(palette) > 0)
	containerID, objectsPath := l.containerID, l.objectsPath

	// --- 3D/3dmodel.model: container object + build item ---
	var root strings.Builder
	root.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	fmt.Fprintf(&root, `<model unit="millimeter" xml:lang="en-US" xmlns="%s" xmlns:p="%s" xmlns:BambuStudio="%s" requiredextensions="p">`+"\n",
		nsCore, nsProduction, nsBambu)
	root.WriteString(` <metadata name="Application">meshio</metadata>` + "\n")
	root.WriteString(` <metadata name="BambuStudio:3mfVersion">1</metadata>` + "\n")
	root.WriteString(" <resources>\n")
	fmt.Fprintf(&root, "  <object id=\"%d\" p:UUID=\"%s\" type=\"model\">\n", containerID, derivedUUID("object", o.Name, containerID))
	root.WriteString("   <components>\n")
	for i, p := range o.Parts {
		partID := l.partIDs[i]
		fmt.Fprintf(&root, "    <component p:path=\"/%s\" objectid=\"%d\" p:UUID=\"%s\" transform=\"%s\"/>\n",
			objectsPath, partID, derivedUUID("component", p.Name, partID), identityXform)
	}
	root.WriteString("   </components>\n")
	root.WriteString("  </object>\n")
	root.WriteString(" </resources>\n")
	fmt.Fprintf(&root, " <build p:UUID=\"%s\">\n", derivedUUID("build", o.Name, 0))
	fmt.Fprintf(&root, "  <item objectid=\"%d\" p:UUID=\"%s\" transform=\"%s\" printable=\"1\"/>\n",
		containerID, derivedUUID("item", o.Name, containerID), identityXform)
	root.WriteString(" </build>\n")
	root.WriteString("</model>\n")

	// --- 3D/Objects/object_N.model: one <object> per part ---
	var objects strings.Builder
	objects.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	materialNS := ""
	if len(palette) > 0 {
		materialNS = ` xmlns:m="http://schemas.microsoft.com/3dmanufacturing/material/2015/02"`
	}
	fmt.Fprintf(&objects, `<model unit="millimeter" xml:lang="en-US" xmlns="%s" xmlns:p="%s"%s requiredextensions="p">`+"\n",
		nsCore, nsProduction, materialNS)
	objects.WriteString(" <resources>\n")
	if len(palette) > 0 {
		fmt.Fprintf(&objects, "  <m:colorgroup id=\"%d\">\n", l.colorGroupID)
		for _, hexColor := range palette {
			fmt.Fprintf(&objects, "   <m:color color=\"%s\" />\n", xmlAttr(hexColor))
		}
		objects.WriteString("  </m:colorgroup>\n")
	}
	for i, p := range o.Parts {
		partID := l.partIDs[i]
		colorIdx := -1
		if idx, ok := paletteBySlot[o.slot(p)]; ok {
			colorIdx = idx
		}
		fmt.Fprintf(&objects, "  <object id=\"%d\" p:UUID=\"%s\" type=\"model\">\n", partID, derivedUUID("partobject", p.Name, partID))
		// A part is one slot, so every triangle takes the same palette index.
		writeMeshXML(&objects, p.Geometry, "   ", l.colorGroupID, func(int) int { return colorIdx })
		objects.WriteString("  </object>\n")
	}
	objects.WriteString(" </resources>\n")
	objects.WriteString(" <build/>\n")
	objects.WriteString("</model>\n")

	// --- Metadata/model_settings.config ---
	settings := o.modelSettings(l)

	// --- OPC scaffolding ---
	var ct strings.Builder
	ct.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	ct.WriteString(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` + "\n")
	ct.WriteString(` <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml" />` + "\n")
	ct.WriteString(` <Default Extension="model" ContentType="application/vnd.ms-package.3dmanufacturing-3dmodel+xml" />` + "\n")
	for _, att := range o.Attachments {
		fmt.Fprintf(&ct, ` <Override PartName="/%s" ContentType="%s" />`+"\n", xmlAttr(att.Path), xmlAttr(att.ContentType))
	}
	ct.WriteString("</Types>\n")

	rootRels := `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + "\n" +
		` <Relationship Target="/3D/3dmodel.model" Id="rel0" Type="` + relType3DModel + `" />` + "\n" +
		"</Relationships>\n"

	objectRels := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>`+"\n"+
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+"\n"+
		` <Relationship Target="/%s" Id="rel-1" Type="%s" />`+"\n"+
		"</Relationships>\n", objectsPath, relType3DModel)

	zw := zip.NewWriter(w)
	entries := []struct{ name, content string }{
		{"[Content_Types].xml", ct.String()},
		{"_rels/.rels", rootRels},
		{"3D/3dmodel.model", root.String()},
		{"3D/_rels/3dmodel.model.rels", objectRels},
		{objectsPath, objects.String()},
		{"Metadata/model_settings.config", settings},
	}
	for _, e := range entries {
		if err := addZipEntry(zw, e.name, e.content); err != nil {
			return err
		}
	}
	for _, att := range o.Attachments {
		if err := addZipBytes(zw, att.Path, att.Data); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("meshio: closing zip: %w", err)
	}
	return nil
}

// WriteBambu exports the object to a Bambu Studio 3MF file at path.
func WriteBambu(path string, o *Object) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("meshio: %w", err)
	}
	defer f.Close()
	return EncodeBambu(f, o)
}

// modelSettings builds Metadata/model_settings.config: the object default
// extruder plus one <part> per part, each with its resolved slot.
func (o *Object) modelSettings(l bambuLayout) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	sb.WriteString("<config>\n")
	fmt.Fprintf(&sb, "  <object id=\"%d\">\n", l.containerID)
	if o.Name != "" {
		fmt.Fprintf(&sb, "    <metadata key=\"name\" value=\"%s\"/>\n", xmlAttr(o.Name))
	}
	fmt.Fprintf(&sb, "    <metadata key=\"extruder\" value=\"%d\"/>\n", o.defaultSlot())
	for i, p := range o.Parts {
		fmt.Fprintf(&sb, "    <part id=\"%d\" subtype=\"normal_part\">\n", l.partIDs[i])
		if p.Name != "" {
			fmt.Fprintf(&sb, "      <metadata key=\"name\" value=\"%s\"/>\n", xmlAttr(p.Name))
		}
		sb.WriteString("      <metadata key=\"matrix\" value=\"1 0 0 0 0 1 0 0 0 0 1 0 0 0 0 1\"/>\n")
		fmt.Fprintf(&sb, "      <metadata key=\"extruder\" value=\"%d\"/>\n", o.slot(p))
		sb.WriteString("      <mesh_stat edges_fixed=\"0\" degenerate_facets=\"0\" facets_removed=\"0\" facets_reversed=\"0\" backwards_edges=\"0\"/>\n")
		sb.WriteString("    </part>\n")
	}
	sb.WriteString("  </object>\n")
	sb.WriteString("</config>\n")
	return sb.String()
}

// derivedUUID returns a deterministic RFC-4122-shaped UUID for a labelled
// element. Deterministic rather than random so that encoding the same object
// twice yields byte-identical output. The 3MF production extension spec
// scopes UUID identity to both name and index -- hashing index alone means
// every two-part object meshio writes shares an identical set of UUIDs
// regardless of what the object/parts are named, which the spec forbids.
func derivedUUID(label, name string, index int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%d", label, name, index)))
	b := sum[:16]
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	s := hex.EncodeToString(b)
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:32]
}
