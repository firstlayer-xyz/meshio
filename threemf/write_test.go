package threemf

import (
	"archive/zip"
	"bytes"
	"testing"

	"github.com/firstlayer-xyz/meshio/geom"
)

// triangle is a simple single-triangle mesh for testing.
func triangle() *geom.Mesh {
	return &geom.Mesh{
		Geometry: geom.Geometry{
			Vertices: []float32{0, 0, 0, 1, 0, 0, 0, 1, 0},
			Indices:  []uint32{0, 1, 2},
		},
	}
}

// coloredCube returns a cube-like mesh (8 verts, 12 tris) with 2 face colors.
func coloredCube() *geom.Mesh {
	v := []float32{
		0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0, // bottom
		0, 0, 1, 1, 0, 1, 1, 1, 1, 0, 1, 1, // top
	}
	idx := []uint32{
		0, 1, 2, 0, 2, 3, // bottom
		4, 6, 5, 4, 7, 6, // top
		0, 4, 5, 0, 5, 1, // front
		2, 6, 7, 2, 7, 3, // back
		0, 3, 7, 0, 7, 4, // left
		1, 5, 6, 1, 6, 2, // right
	}
	fc := make([]geom.FaceColor, 12)
	for i := 0; i < 4; i++ {
		fc[i] = geom.FaceColor{Hex: "#FF0000"}
	}
	for i := 4; i < 12; i++ {
		fc[i] = geom.FaceColor{Hex: "#0000FF"}
	}
	return &geom.Mesh{Geometry: geom.Geometry{Vertices: v, Indices: idx}, FaceColors: fc}
}

func TestThreeMFRoundTrip(t *testing.T) {
	orig := triangle()
	var buf bytes.Buffer
	if err := Encode(&buf, orig); err != nil {
		t.Fatalf("Encode3MF: %v", err)
	}
	decoded, err := Decode(&buf)
	if err != nil {
		t.Fatalf("Decode3MF: %v", err)
	}
	if len(decoded.Indices)/3 != 1 {
		t.Errorf("3MF round-trip: expected 1 triangle, got %d", len(decoded.Indices)/3)
	}
	if len(decoded.Vertices)/3 != 3 {
		t.Errorf("3MF round-trip: expected 3 vertices, got %d", len(decoded.Vertices)/3)
	}
}

func TestThreeMFColorRoundTrip(t *testing.T) {
	orig := coloredCube()
	var buf bytes.Buffer
	if err := Encode(&buf, orig); err != nil {
		t.Fatalf("Encode3MF: %v", err)
	}
	decoded, err := Decode(&buf)
	if err != nil {
		t.Fatalf("Decode3MF: %v", err)
	}
	if len(decoded.Indices)/3 != 12 {
		t.Errorf("3MF color: expected 12 triangles, got %d", len(decoded.Indices)/3)
	}
	if len(decoded.FaceColors) != 12 {
		t.Fatalf("3MF color: expected 12 face colors, got %d", len(decoded.FaceColors))
	}
	// First 4 should be red, next 8 blue
	for i := 0; i < 4; i++ {
		if decoded.FaceColors[i].Hex != "#FF0000" {
			t.Errorf("3MF color: face %d expected #FF0000, got %s", i, decoded.FaceColors[i].Hex)
		}
	}
	for i := 4; i < 12; i++ {
		if decoded.FaceColors[i].Hex != "#0000FF" {
			t.Errorf("3MF color: face %d expected #0000FF, got %s", i, decoded.FaceColors[i].Hex)
		}
	}
}

func TestThreeMFEmpty(t *testing.T) {
	m := &geom.Mesh{}
	var buf bytes.Buffer
	if err := Encode(&buf, m); err == nil {
		t.Error("Encode3MF: expected error for empty mesh")
	}
}

func TestEncode3MF_NoSlic3rConfig(t *testing.T) {
	var buf bytes.Buffer
	if err := Encode(&buf, coloredCube()); err != nil {
		t.Fatalf("Encode3MF: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	for _, f := range zr.File {
		if f.Name == "Metadata/Slic3r_PE_model.config" {
			t.Fatal("Encode3MF still writes the inert Slic3r_PE_model.config part")
		}
	}
}

func TestDecode3MF_Slic3rConfigBecomesAttachment(t *testing.T) {
	data := make3MF(map[string]string{
		"[Content_Types].xml":             ctXML,
		"_rels/.rels":                     relsRoot,
		"3D/3dmodel.model":                partWithMesh,
		"Metadata/Slic3r_PE_model.config": "<config/>",
	})
	m, err := Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Decode3MF: %v", err)
	}
	for _, a := range m.Attachments {
		if a.Path == "Metadata/Slic3r_PE_model.config" {
			return
		}
	}
	t.Fatal("Slic3r_PE_model.config was dropped; expected it preserved as an Attachment")
}
