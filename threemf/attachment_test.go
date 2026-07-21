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

// buildPackageWithDefaultContentType writes a minimal 3MF whose attachment type
// is declared by <Default Extension>, not <Override>. Real Bambu packages
// declare png thumbnails and gcode this way.
func buildPackageWithDefaultContentType(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, body string) {
		t.Helper()
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if _, err := io.WriteString(w, body); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	add("[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
 <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml" />
 <Default Extension="model" ContentType="application/vnd.ms-package.3dmanufacturing-3dmodel+xml" />
 <Default Extension="png" ContentType="image/png" />
 <Override PartName="/Metadata/custom.json" ContentType="application/json" />
</Types>`)
	add("_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
 <Relationship Target="/3D/3dmodel.model" Id="rel0" Type="http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel" />
</Relationships>`)
	add("3D/3dmodel.model", `<?xml version="1.0" encoding="UTF-8"?>
<model unit="millimeter" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02">
 <resources><object id="1" type="model"><mesh>
  <vertices><vertex x="0" y="0" z="0"/><vertex x="1" y="0" z="0"/><vertex x="0" y="1" z="0"/></vertices>
  <triangles><triangle v1="0" v2="1" v3="2"/></triangles>
 </mesh></object></resources>
 <build><item objectid="1"/></build>
</model>`)
	add("Metadata/plate_1.png", "\x89PNG\r\n\x1a\n")
	add("Metadata/custom.json", `{"a":1}`)
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

// An attachment whose content type is declared by <Default Extension> must
// decode with that type. Losing it means re-encoding writes ContentType="",
// which is invalid OPC -- so this breaks reading a real Bambu file and writing
// it back out.
func TestDecode3MF_ResolvesDefaultContentType(t *testing.T) {
	m, err := Decode(bytes.NewReader(buildPackageWithDefaultContentType(t)))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	got := map[string]string{}
	for _, a := range m.Attachments {
		got[a.Path] = a.ContentType
	}
	if ct := got["Metadata/plate_1.png"]; ct != "image/png" {
		t.Errorf("png declared by <Default Extension>: ContentType = %q, want %q", ct, "image/png")
	}
	if ct := got["Metadata/custom.json"]; ct != "application/json" {
		t.Errorf("json declared by <Override>: ContentType = %q, want %q", ct, "application/json")
	}
}

// An <Override> naming a part must win over a <Default> for its extension.
func TestDecode3MF_OverrideBeatsDefault(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, body string) {
		w, _ := zw.Create(name)
		io.WriteString(w, body)
	}
	add("[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
 <Default Extension="model" ContentType="application/vnd.ms-package.3dmanufacturing-3dmodel+xml" />
 <Default Extension="dat" ContentType="application/octet-stream" />
 <Override PartName="/Metadata/special.dat" ContentType="application/x-special" />
</Types>`)
	add("_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
 <Relationship Target="/3D/3dmodel.model" Id="rel0" Type="http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel" />
</Relationships>`)
	add("3D/3dmodel.model", `<?xml version="1.0" encoding="UTF-8"?>
<model unit="millimeter" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02">
 <resources><object id="1" type="model"><mesh>
  <vertices><vertex x="0" y="0" z="0"/><vertex x="1" y="0" z="0"/><vertex x="0" y="1" z="0"/></vertices>
  <triangles><triangle v1="0" v2="1" v3="2"/></triangles>
 </mesh></object></resources>
 <build><item objectid="1"/></build>
</model>`)
	add("Metadata/plain.dat", "x")
	add("Metadata/special.dat", "y")
	zw.Close()

	m, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	got := map[string]string{}
	for _, a := range m.Attachments {
		got[a.Path] = a.ContentType
	}
	if ct := got["Metadata/special.dat"]; ct != "application/x-special" {
		t.Errorf("Override must win over Default: ContentType = %q, want %q", ct, "application/x-special")
	}
	if ct := got["Metadata/plain.dat"]; ct != "application/octet-stream" {
		t.Errorf("unoverridden .dat: ContentType = %q, want %q", ct, "application/octet-stream")
	}
}

// Re-encoding a decoded package must not emit ContentType="", which is invalid
// OPC and is what an unresolved <Default> used to produce.
func TestEncode3MF_RoundTripPreservesDefaultContentType(t *testing.T) {
	m, err := Decode(bytes.NewReader(buildPackageWithDefaultContentType(t)))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	var out bytes.Buffer
	if err := Encode(&out, m); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	ct := readZipPart(t, out.Bytes(), "[Content_Types].xml")
	if strings.Contains(ct, `ContentType=""`) {
		t.Errorf("re-encoded package declares an empty ContentType (invalid OPC):\n%s", ct)
	}
	if !strings.Contains(ct, "image/png") {
		t.Errorf("png content type lost on round trip:\n%s", ct)
	}
}

// An attachment with no ContentType cannot be declared in OPC -- writing one
// produces ContentType="", which a validating reader rejects. Catch it at the
// call rather than emitting a malformed package.
func TestEncode3MF_EmptyContentTypeErrors(t *testing.T) {
	m := triCube()
	m.Attachments = []geom.Attachment{{
		Path: "Metadata/extra.json",
		Data: []byte("{}"),
	}}
	var buf bytes.Buffer
	err := Encode(&buf, m)
	if err == nil {
		t.Fatalf("Encode accepted an attachment with no ContentType; wrote:\n%s",
			readZipPart(t, buf.Bytes(), "[Content_Types].xml"))
	}
	if !strings.Contains(err.Error(), "Metadata/extra.json") {
		t.Errorf("error should name the offending attachment, got: %v", err)
	}
}

func TestBambu_EmptyContentTypeErrors(t *testing.T) {
	o := twoPartObject()
	o.Attachments = []geom.Attachment{{
		Path: "Metadata/extra.json",
		Data: []byte("{}"),
	}}
	var buf bytes.Buffer
	if err := EncodeBambu(&buf, o); err == nil {
		t.Fatal("EncodeBambu accepted an attachment with no ContentType")
	}
}
