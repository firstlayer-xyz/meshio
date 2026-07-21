package threemf

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/firstlayer-xyz/meshio/geom"
)

func triCube() *geom.Mesh {
	return &geom.Mesh{
		Geometry: geom.Geometry{
			Vertices: []float32{0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0},
			Indices:  []uint32{0, 1, 2, 0, 2, 3},
		},
	}
}

func readZipPart(t *testing.T, data []byte, name string) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("open part %s: %v", name, err)
			}
			defer rc.Close()
			b, _ := io.ReadAll(rc)
			return string(b)
		}
	}
	t.Fatalf("part %q not found in zip", name)
	return ""
}

func TestEncode3MF_WritesAttachment(t *testing.T) {
	m := triCube()
	m.Attachments = []geom.Attachment{{
		Path:        "Metadata/Facet/project.json",
		ContentType: "application/vnd.facet.project+json",
		Data:        []byte(`{"version":1}`),
	}}
	var buf bytes.Buffer
	if err := Encode(&buf, m); err != nil {
		t.Fatalf("Encode3MF: %v", err)
	}
	data := buf.Bytes()
	if got := readZipPart(t, data, "Metadata/Facet/project.json"); got != `{"version":1}` {
		t.Fatalf("attachment bytes = %q", got)
	}
	ct := readZipPart(t, data, "[Content_Types].xml")
	if !strings.Contains(ct, `PartName="/Metadata/Facet/project.json"`) ||
		!strings.Contains(ct, `ContentType="application/vnd.facet.project+json"`) {
		t.Fatalf("content types missing override:\n%s", ct)
	}
	rels := readZipPart(t, data, "_rels/.rels")
	if !strings.Contains(rels, `Target="/Metadata/Facet/project.json"`) {
		t.Fatalf("rels missing attachment target:\n%s", rels)
	}
}

func TestDecode3MF_RoundTripsAttachment(t *testing.T) {
	m := triCube()
	want := []byte(`{"version":1,"entry":"Main"}`)
	m.Attachments = []geom.Attachment{{
		Path:        "Metadata/Facet/project.json",
		ContentType: "application/vnd.facet.project+json",
		Data:        want,
	}}
	var buf bytes.Buffer
	if err := Encode(&buf, m); err != nil {
		t.Fatalf("Encode3MF: %v", err)
	}
	got, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode3MF: %v", err)
	}
	if len(got.Indices) != 6 {
		t.Fatalf("geometry lost: %d indices", len(got.Indices))
	}
	var found *geom.Attachment
	for i := range got.Attachments {
		if got.Attachments[i].Path == "Metadata/Facet/project.json" {
			found = &got.Attachments[i]
		}
	}
	if found == nil {
		t.Fatalf("attachment not decoded; got %d attachments", len(got.Attachments))
	}
	if !bytes.Equal(found.Data, want) {
		t.Fatalf("attachment data = %q, want %q", found.Data, want)
	}
	if found.ContentType != "application/vnd.facet.project+json" {
		t.Fatalf("content type = %q", found.ContentType)
	}
}

func TestDecode3MF_NoAttachmentsWhenPlain(t *testing.T) {
	var buf bytes.Buffer
	if err := Encode(&buf, triCube()); err != nil {
		t.Fatalf("Encode3MF: %v", err)
	}
	got, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode3MF: %v", err)
	}
	if len(got.Attachments) != 0 {
		t.Fatalf("expected no attachments, got %d", len(got.Attachments))
	}
}

func TestEncode3MF_DuplicateAttachmentPathErrors(t *testing.T) {
	m := triCube()
	m.Attachments = []geom.Attachment{
		{Path: "Metadata/Facet/project.json", ContentType: "x", Data: []byte("a")},
		{Path: "Metadata/Facet/project.json", ContentType: "x", Data: []byte("b")},
	}
	var buf bytes.Buffer
	if err := Encode(&buf, m); err == nil {
		t.Fatal("expected error on duplicate attachment path, got nil")
	}
}

// TestEncode3MF_ReservedAttachmentPathErrors covers Fix 7: an attachment
// whose Path collides with a package part the writer itself emits must be
// rejected, not silently written -- archive/zip accepts duplicate part
// names, and which one a reader resolves is then reader-dependent.
func TestEncode3MF_ReservedAttachmentPathErrors(t *testing.T) {
	for _, reserved := range []string{"3D/3dmodel.model", "[Content_Types].xml", "_rels/.rels"} {
		t.Run(reserved, func(t *testing.T) {
			m := triCube()
			m.Attachments = []geom.Attachment{{Path: reserved, ContentType: "x", Data: []byte("a")}}
			var buf bytes.Buffer
			if err := Encode(&buf, m); err == nil {
				t.Fatalf("expected error on attachment path %q colliding with a reserved part, got nil", reserved)
			}
		})
	}
}
