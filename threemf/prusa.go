package threemf

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/firstlayer-xyz/meshio/geom"
)

// PrusaSlicer expresses multi-material as the inverse of Bambu Studio. Bambu
// assembles an object from component objects and binds a filament slot to each
// component; PrusaSlicer takes one object with a single merged mesh and carves
// it into volumes by triangle index range, binding a slot to each range.
//
// The two are mutually exclusive, so this writer cannot share EncodeBambu's
// geometry. PrusaSlicer's importer deletes any object that has components but
// no mesh of its own before configuration is applied, so a Bambu-layout package
// has nothing for Slic3r_PE_model.config to attach to and imports as a single
// material.
//
// Verified against PrusaSlicer's src/libslic3r/Format/3mf.cpp: _handle_end_object
// registers only objects carrying geometry; the config's <object id> is matched
// against those; _generate_volumes slices geometry.triangles[first..last] into a
// ModelVolume; and any metadata key it does not recognise -- "extruder" among
// them -- is applied via volume->config.set_deserialize.
const (
	prusaModelConfigPath = "Metadata/Slic3r_PE_model.config"

	// The merged object is the only resource that needs an id, so 1 is free
	// and matches what PrusaSlicer's own exporter emits.
	prusaObjectID = 1
)

// volumeRange is one part's contiguous span in the merged triangle stream.
type volumeRange struct {
	name        string
	first, last int // inclusive triangle indices
	slot        int
}

// mergeParts concatenates every part's geometry into one mesh, offsetting each
// part's indices by the running vertex count, and reports the triangle range
// each part occupies.
//
// Ranges are contiguous and in Parts order, which is what makes the PrusaSlicer
// dialect expressible at all: its volumes address triangle spans, and an
// Object's parts are already an ordered partition of the stream.
func (o *Object) mergeParts() (geom.Geometry, []volumeRange) {
	var merged geom.Geometry
	ranges := make([]volumeRange, 0, len(o.Parts))

	triOffset := 0
	vertOffset := uint32(0)
	for _, p := range o.Parts {
		numTris := len(p.Geometry.Indices) / 3

		merged.Vertices = append(merged.Vertices, p.Geometry.Vertices...)
		for _, idx := range p.Geometry.Indices {
			merged.Indices = append(merged.Indices, idx+vertOffset)
		}

		ranges = append(ranges, volumeRange{
			name:  p.Name,
			first: triOffset,
			last:  triOffset + numTris - 1,
			slot:  o.slot(p),
		})

		vertOffset += uint32(len(p.Geometry.Vertices) / 3)
		triOffset += numTris
	}
	return merged, ranges
}

// EncodePrusa writes the object as a PrusaSlicer 3MF: one object holding every
// part's geometry merged into a single mesh, with Metadata/Slic3r_PE_model.config
// carving it back into volumes by triangle range and binding a filament slot to
// each.
//
// Unlike EncodeBambu, which keeps parts as separate component objects, this
// merges them -- the two dialects disagree about how a multi-material object is
// structured and no single package satisfies both.
//
// Like EncodeBambu and unlike Encode, it does not mutate its input.
func EncodePrusa(w io.Writer, o *Object) error {
	if err := o.validate(prusaReservedPaths()...); err != nil {
		return err
	}

	merged, ranges := o.mergeParts()
	palette, paletteBySlot := o.palette()

	// --- 3D/3dmodel.model: one object, one merged mesh ---
	var model strings.Builder
	model.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	materialNS := ""
	if len(palette) > 0 {
		materialNS = ` xmlns:m="` + nsMaterial + `"`
	}
	fmt.Fprintf(&model, `<model unit="millimeter" xml:lang="en-US" xmlns="%s"%s>`+"\n", nsCore, materialNS)
	model.WriteString(` <metadata name="Application">meshio</metadata>` + "\n")
	model.WriteString(" <resources>\n")

	// Ids are unique per model part; the object takes 1, so the colorgroup
	// takes the next free id rather than a constant that could collide.
	var ids resourceIDs
	objectID := ids.next() // == prusaObjectID
	colorGroupID := 0
	if len(palette) > 0 {
		colorGroupID = ids.next()
		fmt.Fprintf(&model, "  <m:colorgroup id=\"%d\">\n", colorGroupID)
		for _, hexColor := range palette {
			fmt.Fprintf(&model, "   <m:color color=\"%s\" />\n", xmlAttr(hexColor))
		}
		model.WriteString("  </m:colorgroup>\n")
	}

	// Triangle -> palette index, derived from the range each triangle falls in.
	// A part is one slot, so this is constant within a range.
	triColor := make([]int, len(merged.Indices)/3)
	for i := range triColor {
		triColor[i] = -1
	}
	for _, r := range ranges {
		idx, ok := paletteBySlot[r.slot]
		if !ok {
			continue
		}
		for i := r.first; i <= r.last; i++ {
			triColor[i] = idx
		}
	}

	fmt.Fprintf(&model, "  <object id=\"%d\" type=\"model\">\n", objectID)
	writeMeshXML(&model, merged, "   ", colorGroupID, func(tri int) triangleAttrs {
		return triangleAttrs{colorIdx: triColor[tri]}
	})
	model.WriteString("  </object>\n")
	model.WriteString(" </resources>\n")
	model.WriteString(" <build>\n")
	fmt.Fprintf(&model, "  <item objectid=\"%d\" transform=\"%s\" printable=\"1\" />\n", objectID, identityXform)
	model.WriteString(" </build>\n")
	model.WriteString("</model>\n")

	// --- Metadata/Slic3r_PE_model.config ---
	config := o.prusaModelConfig(objectID, ranges)

	// --- OPC scaffolding ---
	var ct strings.Builder
	ct.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	ct.WriteString(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` + "\n")
	ct.WriteString(` <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml" />` + "\n")
	ct.WriteString(` <Default Extension="model" ContentType="application/vnd.ms-package.3dmanufacturing-3dmodel+xml" />` + "\n")
	writeContentTypeOverrides(&ct, o.Attachments)
	ct.WriteString("</Types>\n")

	rels := `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + "\n" +
		` <Relationship Target="/3D/3dmodel.model" Id="rel0" Type="` + relType3DModel + `" />` + "\n" +
		"</Relationships>\n"

	zw := zip.NewWriter(w)
	for _, e := range []struct{ name, content string }{
		{"[Content_Types].xml", ct.String()},
		{"_rels/.rels", rels},
		{"3D/3dmodel.model", model.String()},
		{prusaModelConfigPath, config},
	} {
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

// WritePrusa exports the object to a PrusaSlicer 3MF file at path.
func WritePrusa(path string, o *Object) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("meshio: %w", err)
	}
	defer f.Close()
	return EncodePrusa(f, o)
}

// prusaReservedPaths lists the package parts EncodePrusa emits itself, which an
// attachment must not collide with.
func prusaReservedPaths() []string {
	return []string{
		"[Content_Types].xml",
		"_rels/.rels",
		"3D/3dmodel.model",
		prusaModelConfigPath,
	}
}

// prusaModelConfig builds Metadata/Slic3r_PE_model.config: one <volume> per
// part, addressing its triangle range and carrying its filament slot.
//
// PrusaSlicer distinguishes object-level from volume-level entries by the type
// attribute, and reads the semantic name from key -- unlike the Bambu dialect,
// where key alone carries it.
func (o *Object) prusaModelConfig(objectID int, ranges []volumeRange) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	sb.WriteString("<config>\n")
	fmt.Fprintf(&sb, " <object id=\"%d\">\n", objectID)
	if o.Name != "" {
		fmt.Fprintf(&sb, "  <metadata type=\"object\" key=\"name\" value=\"%s\"/>\n", xmlAttr(o.Name))
	}
	fmt.Fprintf(&sb, "  <metadata type=\"object\" key=\"extruder\" value=\"%d\"/>\n", o.defaultSlot())
	for _, r := range ranges {
		fmt.Fprintf(&sb, "  <volume firstid=\"%d\" lastid=\"%d\">\n", r.first, r.last)
		if r.name != "" {
			fmt.Fprintf(&sb, "   <metadata type=\"volume\" key=\"name\" value=\"%s\"/>\n", xmlAttr(r.name))
		}
		sb.WriteString("   <metadata type=\"volume\" key=\"volume_type\" value=\"ModelPart\"/>\n")
		fmt.Fprintf(&sb, "   <metadata type=\"volume\" key=\"extruder\" value=\"%d\"/>\n", r.slot)
		sb.WriteString("   <mesh_stat edges_fixed=\"0\" degenerate_facets=\"0\" facets_removed=\"0\" facets_reversed=\"0\" backwards_edges=\"0\"/>\n")
		sb.WriteString("  </volume>\n")
	}
	sb.WriteString(" </object>\n")
	sb.WriteString("</config>\n")
	return sb.String()
}
