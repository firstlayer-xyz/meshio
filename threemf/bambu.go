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
	colorGroupID   = 100
)

// EncodeBambu writes the object as a Bambu Studio 3MF: a container object whose
// components are the parts, part geometry in 3D/Objects, and filament slot
// assignments in Metadata/model_settings.config.
//
// Unlike Encode, which carries display color only, this produces a file that
// slices multi-material: each part is bound to a filament slot.
func EncodeBambu(w io.Writer, o *Object) error {
	if err := o.validate(); err != nil {
		return err
	}

	containerID := len(o.Parts) + 1
	objectsPath := fmt.Sprintf("3D/Objects/object_%d.model", containerID)
	palette, paletteBySlot := o.palette()

	// --- 3D/3dmodel.model: container object + build item ---
	var root strings.Builder
	root.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	fmt.Fprintf(&root, `<model unit="millimeter" xml:lang="en-US" xmlns="%s" xmlns:p="%s" xmlns:BambuStudio="%s" requiredextensions="p">`+"\n",
		nsCore, nsProduction, nsBambu)
	root.WriteString(` <metadata name="Application">meshio</metadata>` + "\n")
	root.WriteString(` <metadata name="BambuStudio:3mfVersion">1</metadata>` + "\n")
	root.WriteString(" <resources>\n")
	fmt.Fprintf(&root, "  <object id=\"%d\" p:UUID=\"%s\" type=\"model\">\n", containerID, derivedUUID("object", containerID))
	root.WriteString("   <components>\n")
	for i := range o.Parts {
		partID := i + 1
		fmt.Fprintf(&root, "    <component p:path=\"/%s\" objectid=\"%d\" p:UUID=\"%s\" transform=\"%s\"/>\n",
			objectsPath, partID, derivedUUID("component", partID), identityXform)
	}
	root.WriteString("   </components>\n")
	root.WriteString("  </object>\n")
	root.WriteString(" </resources>\n")
	fmt.Fprintf(&root, " <build p:UUID=\"%s\">\n", derivedUUID("build", 0))
	fmt.Fprintf(&root, "  <item objectid=\"%d\" p:UUID=\"%s\" transform=\"%s\" printable=\"1\"/>\n",
		containerID, derivedUUID("item", containerID), identityXform)
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
		fmt.Fprintf(&objects, "  <m:colorgroup id=\"%d\">\n", colorGroupID)
		for _, hexColor := range palette {
			fmt.Fprintf(&objects, "   <m:color color=\"%s\" />\n", hexColor)
		}
		objects.WriteString("  </m:colorgroup>\n")
	}
	for i, p := range o.Parts {
		partID := i + 1
		colorIdx := -1
		if idx, ok := paletteBySlot[o.slot(p)]; ok {
			colorIdx = idx
		}
		fmt.Fprintf(&objects, "  <object id=\"%d\" p:UUID=\"%s\" type=\"model\">\n", partID, derivedUUID("partobject", partID))
		// A part is one slot, so every triangle takes the same palette index.
		writeMeshXML(&objects, p.Geometry, "   ", colorGroupID, func(int) int { return colorIdx })
		objects.WriteString("  </object>\n")
	}
	objects.WriteString(" </resources>\n")
	objects.WriteString(" <build/>\n")
	objects.WriteString("</model>\n")

	// --- Metadata/model_settings.config ---
	settings := o.modelSettings(containerID)

	// --- OPC scaffolding ---
	var ct strings.Builder
	ct.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	ct.WriteString(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` + "\n")
	ct.WriteString(` <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml" />` + "\n")
	ct.WriteString(` <Default Extension="model" ContentType="application/vnd.ms-package.3dmanufacturing-3dmodel+xml" />` + "\n")
	for _, att := range o.Attachments {
		fmt.Fprintf(&ct, ` <Override PartName="/%s" ContentType="%s" />`+"\n", att.Path, att.ContentType)
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
func (o *Object) modelSettings(containerID int) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	sb.WriteString("<config>\n")
	fmt.Fprintf(&sb, "  <object id=\"%d\">\n", containerID)
	if o.Name != "" {
		fmt.Fprintf(&sb, "    <metadata key=\"name\" value=\"%s\"/>\n", o.Name)
	}
	objDefault := o.Filament
	if objDefault <= 0 {
		objDefault = 1
	}
	fmt.Fprintf(&sb, "    <metadata key=\"extruder\" value=\"%d\"/>\n", objDefault)
	for i, p := range o.Parts {
		partID := i + 1
		fmt.Fprintf(&sb, "    <part id=\"%d\" subtype=\"normal_part\">\n", partID)
		if p.Name != "" {
			fmt.Fprintf(&sb, "      <metadata key=\"name\" value=\"%s\"/>\n", p.Name)
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
// twice yields byte-identical output.
func derivedUUID(label string, index int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s/%d", label, index)))
	b := sum[:16]
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	s := hex.EncodeToString(b)
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:32]
}
