package meshio

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"strings"
)

// Encode3MF writes the mesh as a 3MF archive to w.
func (m *Mesh) Encode3MF(w io.Writer) error {
	m.MergeVertices()
	numVerts := len(m.Vertices) / 3
	numTris := len(m.Indices) / 3

	if numVerts == 0 || numTris == 0 {
		return fmt.Errorf("meshio: empty mesh")
	}

	seenAtt := map[string]bool{}
	for _, att := range m.Attachments {
		if seenAtt[att.Path] {
			return fmt.Errorf("meshio: duplicate attachment path %q", att.Path)
		}
		seenAtt[att.Path] = true
	}

	// Build color palette
	var palette []string
	paletteIdx := map[string]int{}
	var faceColorIdx []int
	hasColors := len(m.FaceColors) == numTris

	if hasColors {
		faceColorIdx = make([]int, numTris)
		for i, fc := range m.FaceColors {
			if fc.Hex == "" {
				faceColorIdx[i] = -1
				continue
			}
			hex := normalizeHex(fc.Hex)
			if idx, ok := paletteIdx[hex]; ok {
				faceColorIdx[i] = idx
			} else {
				idx = len(palette)
				palette = append(palette, hex)
				paletteIdx[hex] = idx
				faceColorIdx[i] = idx
			}
		}
		if len(palette) == 0 {
			hasColors = false
		}
	}

	// Build XML
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	sb.WriteString(`<model unit="millimeter" xml:lang="en-US"`)
	sb.WriteString(` xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02"`)
	if hasColors {
		sb.WriteString(` xmlns:m="http://schemas.microsoft.com/3dmanufacturing/material/2015/02"`)
	}
	sb.WriteString(">\n")

	sb.WriteString(" <resources>\n")

	colorGroupID := 100
	if hasColors {
		fmt.Fprintf(&sb, "  <m:colorgroup id=\"%d\">\n", colorGroupID)
		for _, hex := range palette {
			fmt.Fprintf(&sb, "   <m:color color=\"%s\" />\n", hex)
		}
		sb.WriteString("  </m:colorgroup>\n")
	}

	sb.WriteString("  <object id=\"1\" type=\"model\">\n")
	sb.WriteString("   <mesh>\n")

	sb.WriteString("    <vertices>\n")
	for i := 0; i < numVerts; i++ {
		x := m.Vertices[i*3]
		y := m.Vertices[i*3+1]
		z := m.Vertices[i*3+2]
		fmt.Fprintf(&sb, "     <vertex x=\"%g\" y=\"%g\" z=\"%g\" />\n", x, y, z)
	}
	sb.WriteString("    </vertices>\n")

	sb.WriteString("    <triangles>\n")
	for i := 0; i < numTris; i++ {
		v1 := m.Indices[i*3]
		v2 := m.Indices[i*3+1]
		v3 := m.Indices[i*3+2]
		if hasColors && faceColorIdx[i] >= 0 {
			ci := faceColorIdx[i]
			fmt.Fprintf(&sb, "     <triangle v1=\"%d\" v2=\"%d\" v3=\"%d\" pid=\"%d\" p1=\"%d\" p2=\"%d\" p3=\"%d\" />\n",
				v1, v2, v3, colorGroupID, ci, ci, ci)
		} else {
			fmt.Fprintf(&sb, "     <triangle v1=\"%d\" v2=\"%d\" v3=\"%d\" />\n", v1, v2, v3)
		}
	}
	sb.WriteString("    </triangles>\n")

	sb.WriteString("   </mesh>\n")
	sb.WriteString("  </object>\n")
	sb.WriteString(" </resources>\n")

	sb.WriteString(" <build>\n")
	sb.WriteString("  <item objectid=\"1\" />\n")
	sb.WriteString(" </build>\n")
	sb.WriteString("</model>\n")

	modelXML := sb.String()

	var ctBuilder strings.Builder
	ctBuilder.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	ctBuilder.WriteString(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` + "\n")
	ctBuilder.WriteString(` <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml" />` + "\n")
	ctBuilder.WriteString(` <Default Extension="model" ContentType="application/vnd.ms-package.3dmanufacturing-3dmodel+xml" />` + "\n")
	for _, att := range m.Attachments {
		fmt.Fprintf(&ctBuilder, ` <Override PartName="/%s" ContentType="%s" />`+"\n", att.Path, att.ContentType)
	}
	ctBuilder.WriteString("</Types>\n")
	contentTypes := ctBuilder.String()

	var relsBuilder strings.Builder
	relsBuilder.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	relsBuilder.WriteString(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + "\n")
	relsBuilder.WriteString(` <Relationship Target="/3D/3dmodel.model" Id="rel0" Type="http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel" />` + "\n")
	for i, att := range m.Attachments {
		fmt.Fprintf(&relsBuilder, ` <Relationship Target="/%s" Id="attachrel%d" Type="http://schemas.firstlayer.xyz/meshio/2026/01/attachment" />`+"\n", att.Path, i)
	}
	relsBuilder.WriteString("</Relationships>\n")
	rels := relsBuilder.String()

	zw := zip.NewWriter(w)

	if err := addZipEntry(zw, "[Content_Types].xml", contentTypes); err != nil {
		return err
	}
	if err := addZipEntry(zw, "_rels/.rels", rels); err != nil {
		return err
	}
	if err := addZipEntry(zw, "3D/3dmodel.model", modelXML); err != nil {
		return err
	}

	for _, att := range m.Attachments {
		if err := addZipBytes(zw, att.Path, att.Data); err != nil {
			return err
		}
	}

	if hasColors {
		modelConfig := buildModelConfig(numTris, faceColorIdx, palette)
		if modelConfig != "" {
			if err := addZipEntry(zw, "Metadata/Slic3r_PE_model.config", modelConfig); err != nil {
				return err
			}
		}
	}

	if err := zw.Close(); err != nil {
		return fmt.Errorf("meshio: closing zip: %w", err)
	}
	return nil
}

// Write3MF exports a Mesh to a 3MF file at the given path.
func (m *Mesh) Write3MF(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("meshio: %w", err)
	}
	defer f.Close()
	return m.Encode3MF(f)
}

func normalizeHex(hex string) string {
	if len(hex) == 7 {
		return hex + "FF"
	}
	return hex
}

func buildModelConfig(numTris int, faceColorIdx []int, palette []string) string {
	if len(faceColorIdx) == 0 {
		return ""
	}

	type volumeRange struct {
		firstTriID int
		lastTriID  int
		colorIdx   int
	}

	var ranges []volumeRange
	currentColor := faceColorIdx[0]
	rangeStart := 0

	for i := 1; i < numTris; i++ {
		if faceColorIdx[i] != currentColor {
			ranges = append(ranges, volumeRange{rangeStart, i - 1, currentColor})
			currentColor = faceColorIdx[i]
			rangeStart = i
		}
	}
	ranges = append(ranges, volumeRange{rangeStart, numTris - 1, currentColor})

	var sb strings.Builder
	sb.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	sb.WriteString("<config>\n")
	sb.WriteString(" <object id=\"1\">\n")
	for _, r := range ranges {
		hex := ""
		if r.colorIdx >= 0 && r.colorIdx < len(palette) {
			hex = palette[r.colorIdx]
		}
		fmt.Fprintf(&sb, "  <volume firstid=\"%d\" lastid=\"%d\">\n", r.firstTriID, r.lastTriID)
		if hex != "" {
			fmt.Fprintf(&sb, "   <metadata type=\"slic3r.extruder\" value=\"%s\" />\n", hex)
		}
		sb.WriteString("  </volume>\n")
	}
	sb.WriteString(" </object>\n")
	sb.WriteString("</config>\n")
	return sb.String()
}
