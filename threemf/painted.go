package threemf

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/firstlayer-xyz/meshio/geom"
)

const nsSlic3rPE = "http://schemas.slic3r.org/3mf/2017/06"

// PaintedMesh is a single solid whose triangles are individually bound to
// filament slots.
//
// Where an Object splits a model into parts that are each one slot, this keeps
// one mesh and paints it, so the caller's geometry ships unchanged. That is the
// difference that matters for surface color: a slab with a colored top face
// cannot be split into printable parts -- the non-base colors come out as flat,
// zero-thickness patches -- but it can be painted.
//
// FaceSlots is parallel to the triangle stream, the same shape as
// geom.Mesh.FaceColors but carrying a filament slot rather than a color. The
// two are deliberately different types: one answers "what color do I draw
// this", the other "which filament prints this".
type PaintedMesh struct {
	Name        string
	Geometry    geom.Geometry
	FaceSlots   []int          // len == numTris; 1-based slot, 0 = unpainted
	SlotColors  map[int]string // slot number -> display color; absent = uncolored
	Attachments []geom.Attachment
}

// SetSlotColor assigns a display color to a filament slot, allocating
// SlotColors if needed.
func (p *PaintedMesh) SetSlotColor(slot int, hex string) {
	p.SlotColors = setSlotColor(p.SlotColors, slot, hex)
}

// palette returns the ordered distinct display colors and a map from filament
// slot to palette index.
func (p *PaintedMesh) palette() ([]string, map[int]int) {
	return slotColorPalette(p.SlotColors)
}

// validate reports the first structural problem with the mesh.
func (p *PaintedMesh) validate() error {
	numTris := len(p.Geometry.Indices) / 3
	if len(p.Geometry.Vertices) == 0 || numTris == 0 {
		return fmt.Errorf("meshio: painted mesh %q has empty geometry", p.Name)
	}
	if len(p.FaceSlots) != numTris {
		return fmt.Errorf("meshio: %d face slots for %d triangles", len(p.FaceSlots), numTris)
	}
	for i, slot := range p.FaceSlots {
		if slot < 0 {
			return fmt.Errorf("meshio: negative filament slot %d on face %d", slot, i)
		}
		if slot > maxPaintSlot {
			return fmt.Errorf("meshio: filament slot %d on face %d exceeds %d; above that Bambu Studio and PrusaSlicer encode paint differently and no single file serves both", slot, i, maxPaintSlot)
		}
	}
	return validateAttachments(p.Attachments,
		"[Content_Types].xml", "_rels/.rels", "3D/3dmodel.model")
}

// EncodePainted writes the mesh as a 3MF whose triangles carry per-slot paint,
// readable by both Bambu Studio and PrusaSlicer.
//
// Both slicers use the identical paint encoding and differ only in the
// attribute name, so every painted triangle carries both and each reads its
// own. That is possible here and was not for multi-part output, where the two
// disagree about the geometry itself.
//
// It does not mutate its input.
func EncodePainted(w io.Writer, p *PaintedMesh) error {
	if err := p.validate(); err != nil {
		return err
	}

	palette, paletteBySlot := p.palette()

	var ids resourceIDs
	objectID := ids.next()
	colorGroupID := 0
	if len(palette) > 0 {
		colorGroupID = ids.next()
	}

	var model strings.Builder
	model.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	materialNS := ""
	if len(palette) > 0 {
		materialNS = ` xmlns:m="http://schemas.microsoft.com/3dmanufacturing/material/2015/02"`
	}
	fmt.Fprintf(&model, `<model unit="millimeter" xml:lang="en-US" xmlns="%s" xmlns:slic3rpe="%s"%s>`+"\n",
		nsCore, nsSlic3rPE, materialNS)
	model.WriteString(` <metadata name="Application">meshio</metadata>` + "\n")
	model.WriteString(" <resources>\n")
	if len(palette) > 0 {
		fmt.Fprintf(&model, "  <m:colorgroup id=\"%d\">\n", colorGroupID)
		for _, hexColor := range palette {
			fmt.Fprintf(&model, "   <m:color color=\"%s\" />\n", xmlAttr(hexColor))
		}
		model.WriteString("  </m:colorgroup>\n")
	}
	fmt.Fprintf(&model, "  <object id=\"%d\" type=\"model\">\n", objectID)
	writeMeshXML(&model, p.Geometry, "   ", colorGroupID, func(tri int) triangleAttrs {
		slot := p.FaceSlots[tri]
		a := triangleAttrs{colorIdx: -1, paint: paintString(slot)}
		if idx, ok := paletteBySlot[slot]; ok {
			a.colorIdx = idx
		}
		return a
	})
	model.WriteString("  </object>\n </resources>\n")
	model.WriteString(" <build>\n")
	fmt.Fprintf(&model, "  <item objectid=\"%d\" transform=\"%s\" printable=\"1\" />\n", objectID, identityXform)
	model.WriteString(" </build>\n</model>\n")

	var ct strings.Builder
	ct.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	ct.WriteString(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` + "\n")
	ct.WriteString(` <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml" />` + "\n")
	ct.WriteString(` <Default Extension="model" ContentType="application/vnd.ms-package.3dmanufacturing-3dmodel+xml" />` + "\n")
	writeContentTypeOverrides(&ct, p.Attachments)
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
	} {
		if err := addZipEntry(zw, e.name, e.content); err != nil {
			return err
		}
	}
	for _, att := range p.Attachments {
		if err := addZipBytes(zw, att.Path, att.Data); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("meshio: closing zip: %w", err)
	}
	return nil
}

// WritePainted exports the painted mesh to a 3MF file at path.
func WritePainted(path string, p *PaintedMesh) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("meshio: %w", err)
	}
	defer f.Close()
	return EncodePainted(f, p)
}
