package threemf

import (
	"bytes"
	"encoding/xml"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/firstlayer-xyz/meshio/geom"
)

// twoPartObject: parts on slots 1 and 2, ids 1 and 2, container id 3.
func twoPartObject() *Object {
	return &Object{
		Name: "plate",
		Parts: []Part{
			{Name: "base", Geometry: unitTri(), Filament: 1},
			{Name: "tiles", Geometry: unitTri(), Filament: 2},
		},
	}
}

func TestBambu_ComponentIDsMatchPartIDs(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodeBambu(&buf, twoPartObject()); err != nil {
		t.Fatalf("EncodeBambu: %v", err)
	}
	root := readZipPart(t, buf.Bytes(), "3D/3dmodel.model")
	settings := readZipPart(t, buf.Bytes(), "Metadata/model_settings.config")

	compIDs := allMatches(`objectid="(\d+)"`, root)
	partIDs := allMatches(`<part id="(\d+)"`, settings)

	// Drop the build item's objectid (the container) from the component list.
	var comps []string
	for _, id := range compIDs {
		if id != "3" {
			comps = append(comps, id)
		}
	}
	if len(comps) != 2 || len(partIDs) != 2 {
		t.Fatalf("expected 2 components and 2 parts, got %d and %d", len(comps), len(partIDs))
	}
	for i := range comps {
		if comps[i] != partIDs[i] {
			t.Errorf("component objectid %s has no matching part id (%s)", comps[i], partIDs[i])
		}
	}
}

func TestBambu_ExtruderAssignment(t *testing.T) {
	o := &Object{
		Filament: 4,
		Parts: []Part{
			{Name: "inherits", Geometry: unitTri()},
			{Name: "explicit", Geometry: unitTri(), Filament: 7},
		},
	}
	var buf bytes.Buffer
	if err := EncodeBambu(&buf, o); err != nil {
		t.Fatalf("EncodeBambu: %v", err)
	}
	settings := readZipPart(t, buf.Bytes(), "Metadata/model_settings.config")

	got := allMatches(`<metadata key="extruder" value="(\d+)"`, settings)
	// object default, then part 1 (inherited 4), then part 2 (explicit 7)
	want := []string{"4", "4", "7"}
	if len(got) != len(want) {
		t.Fatalf("extruder values: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("extruder[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestBambu_PackageStructure(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodeBambu(&buf, twoPartObject()); err != nil {
		t.Fatalf("EncodeBambu: %v", err)
	}

	rels := readZipPart(t, buf.Bytes(), "3D/_rels/3dmodel.model.rels")
	if !strings.Contains(rels, `Target="/3D/Objects/object_3.model"`) {
		t.Errorf("object rels missing part-file target:\n%s", rels)
	}
	if !strings.Contains(rels, "http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel") {
		t.Errorf("object rels missing 3dmodel relationship type:\n%s", rels)
	}

	// The verified Bambu file declares no content type for .config parts.
	ct := readZipPart(t, buf.Bytes(), "[Content_Types].xml")
	if strings.Contains(ct, "config") {
		t.Errorf("[Content_Types].xml must not declare a .config type:\n%s", ct)
	}

	root := readZipPart(t, buf.Bytes(), "3D/3dmodel.model")
	if !strings.Contains(root, `requiredextensions="p"`) {
		t.Error("root model missing requiredextensions=\"p\"")
	}

	// Part geometry lives in the object file, not the root.
	objects := readZipPart(t, buf.Bytes(), "3D/Objects/object_3.model")
	if strings.Count(objects, "<mesh>") != 2 {
		t.Errorf("expected 2 meshes in the object file, got %d", strings.Count(objects, "<mesh>"))
	}
}

// threePartColoredObject has three parts on three distinct slots, each with a
// distinct SlotColors entry. Determinism depends on (*Object).palette
// visiting the SlotColors map in a fixed order (sort.Ints on the slot
// numbers); an object with no SlotColors set, like twoPartObject, never
// exercises that map at all.
func threePartColoredObject() *Object {
	o := &Object{
		Name: "plate",
		Parts: []Part{
			{Name: "a", Geometry: unitTri(), Filament: 1},
			{Name: "b", Geometry: unitTri(), Filament: 2},
			{Name: "c", Geometry: unitTri(), Filament: 3},
		},
	}
	o.SetSlotColor(1, "#FF0000")
	o.SetSlotColor(2, "#00FF00")
	o.SetSlotColor(3, "#0000FF")
	return o
}

func TestBambu_Deterministic(t *testing.T) {
	var a, b bytes.Buffer
	if err := EncodeBambu(&a, threePartColoredObject()); err != nil {
		t.Fatalf("first encode: %v", err)
	}
	if err := EncodeBambu(&b, threePartColoredObject()); err != nil {
		t.Fatalf("second encode: %v", err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("encoding twice produced different bytes; output must be deterministic")
	}
}

// offsetTri returns unitTri translated by (dx, dy, dz), so two parts built
// from it have distinguishable, non-overlapping geometry.
func offsetTri(dx, dy, dz float32) geom.Geometry {
	base := unitTri()
	v := make([]float32, len(base.Vertices))
	for i := 0; i+2 < len(v); i += 3 {
		v[i] = base.Vertices[i] + dx
		v[i+1] = base.Vertices[i+1] + dy
		v[i+2] = base.Vertices[i+2] + dz
	}
	return geom.Geometry{Vertices: v, Indices: base.Indices}
}

// hasVertex reports whether (x, y, z) appears anywhere among verts.
func hasVertex(verts []float32, x, y, z float32) bool {
	for i := 0; i+2 < len(verts); i += 3 {
		if verts[i] == x && verts[i+1] == y && verts[i+2] == z {
			return true
		}
	}
	return false
}

func TestBambu_GeometryRoundTrip(t *testing.T) {
	// Two parts with distinct, non-overlapping geometry: a writer that
	// mistakenly emitted the same objectid for both components would still
	// pass an indices-count-only check, since decoding would just resolve
	// one part's mesh twice.
	o := &Object{
		Name: "plate",
		Parts: []Part{
			{Name: "base", Geometry: unitTri(), Filament: 1},
			{Name: "tiles", Geometry: offsetTri(10, 0, 0), Filament: 2},
		},
	}
	var buf bytes.Buffer
	if err := EncodeBambu(&buf, o); err != nil {
		t.Fatalf("EncodeBambu: %v", err)
	}
	m, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode of our own output: %v", err)
	}
	// Two triangles: 6 indices.
	if len(m.Indices) != 6 {
		t.Fatalf("round-trip indices: got %d, want 6", len(m.Indices))
	}
	for _, want := range [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}} {
		if !hasVertex(m.Vertices, want[0], want[1], want[2]) {
			t.Errorf("decoded vertices missing base part vertex %v; got %v", want, m.Vertices)
		}
	}
	for _, want := range [][3]float32{{10, 0, 0}, {11, 0, 0}, {10, 1, 0}} {
		if !hasVertex(m.Vertices, want[0], want[1], want[2]) {
			t.Errorf("decoded vertices missing tiles part vertex %v; got %v", want, m.Vertices)
		}
	}
}

func TestBambu_ValidationPropagates(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodeBambu(&buf, &Object{}); err == nil {
		t.Fatal("expected an error encoding an object with no parts")
	}
}

func TestBambu_PaletteDedupsByColor(t *testing.T) {
	o := &Object{
		Parts: []Part{
			{Name: "a", Geometry: unitTri(), Filament: 1},
			{Name: "b", Geometry: unitTri(), Filament: 2},
			{Name: "c", Geometry: unitTri(), Filament: 3},
		},
	}
	o.SetSlotColor(1, "#FF0000")
	o.SetSlotColor(2, "#FF0000") // same color, different slot -> one palette entry
	o.SetSlotColor(3, "#0000FF")

	palette, bySlot := o.palette()
	if len(palette) != 2 {
		t.Fatalf("palette: got %d entries %v, want 2 (dedup by color)", len(palette), palette)
	}
	// Slots are visited in ascending order, so the lowest slot number to
	// introduce a new color claims the next palette index. Slot 1 is seen
	// before slot 3, so its color must land at index 0 regardless of map
	// iteration order -- this is what sort.Ints(slots) in (*Object).palette
	// guarantees, and what makes encoding the same object twice
	// byte-identical.
	if bySlot[1] != 0 {
		t.Fatalf("slot 1 must claim palette index 0 (ascending-slot order), got %d", bySlot[1])
	}
	if palette[bySlot[1]] != "#FF0000FF" {
		t.Errorf("palette[%d] = %q, want #FF0000FF (slot 1's color)", bySlot[1], palette[bySlot[1]])
	}
	if palette[bySlot[3]] != "#0000FFFF" {
		t.Errorf("palette[%d] = %q, want #0000FFFF (slot 3's color)", bySlot[3], palette[bySlot[3]])
	}
	if bySlot[1] != bySlot[2] {
		t.Errorf("slots 1 and 2 share a color but map to %d and %d", bySlot[1], bySlot[2])
	}
	if bySlot[3] == bySlot[1] {
		t.Error("slot 3 has a distinct color but shares a palette index with slot 1")
	}
}

func TestBambu_TrianglesReferenceSlotColor(t *testing.T) {
	o := twoPartObject()
	o.SetSlotColor(1, "#FF0000")
	o.SetSlotColor(2, "#0000FF")

	var buf bytes.Buffer
	if err := EncodeBambu(&buf, o); err != nil {
		t.Fatalf("EncodeBambu: %v", err)
	}
	objects := readZipPart(t, buf.Bytes(), "3D/Objects/object_3.model")

	if !strings.Contains(objects, `<m:color color="#FF0000FF" />`) {
		t.Errorf("missing normalized slot-1 color in colorgroup:\n%s", objects)
	}
	if !strings.Contains(objects, `xmlns:m=`) {
		t.Error("material extension namespace not declared despite a palette")
	}
	refs := allMatches(`p1="(\d+)"`, objects)
	if len(refs) != 2 {
		t.Fatalf("expected 2 colored triangles, got %d", len(refs))
	}
	if refs[0] == refs[1] {
		t.Error("parts on different slots must reference different palette entries")
	}
}

func TestBambu_NoColorsOmitsColorgroup(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodeBambu(&buf, twoPartObject()); err != nil {
		t.Fatalf("EncodeBambu: %v", err)
	}
	objects := readZipPart(t, buf.Bytes(), "3D/Objects/object_3.model")
	if strings.Contains(objects, "colorgroup") {
		t.Errorf("no SlotColors set, but a colorgroup was written:\n%s", objects)
	}
	if strings.Contains(objects, "pid=") {
		t.Error("no SlotColors set, but triangles carry pid references")
	}
}

func TestBambu_UnmappedSlotIsUncolored(t *testing.T) {
	o := twoPartObject()
	o.SetSlotColor(1, "#FF0000") // slot 2 deliberately left unset

	var buf bytes.Buffer
	if err := EncodeBambu(&buf, o); err != nil {
		t.Fatalf("EncodeBambu: %v", err)
	}
	objects := readZipPart(t, buf.Bytes(), "3D/Objects/object_3.model")

	if len(allMatches(`p1="(\d+)"`, objects)) != 1 {
		t.Error("expected exactly one colored triangle; the unmapped slot must be uncolored, not an error")
	}
}

// TestBambu_EscapesNamesInModelSettings covers Fix 1: Object.Name and
// Part.Name are free-form user data written straight into an XML attribute
// value in Metadata/model_settings.config. An unescaped "&", "<", ">", or
// '"' produces XML that fails to parse -- and that file is the ONLY place
// filament slot assignments live, so a rejected config silently loses every
// slot assignment even though the file "looks fine" in a zip browser.
func TestBambu_EscapesNamesInModelSettings(t *testing.T) {
	const trouble = `R&D <plate> "v2"`
	o := &Object{
		Name: trouble,
		Parts: []Part{
			{Name: trouble, Geometry: unitTri(), Filament: 1},
		},
	}
	var buf bytes.Buffer
	if err := EncodeBambu(&buf, o); err != nil {
		t.Fatalf("EncodeBambu: %v", err)
	}
	settings := readZipPart(t, buf.Bytes(), "Metadata/model_settings.config")

	var cfg struct {
		XMLName xml.Name `xml:"config"`
		Object  struct {
			Metadata []struct {
				Key   string `xml:"key,attr"`
				Value string `xml:"value,attr"`
			} `xml:"metadata"`
			Part struct {
				Metadata []struct {
					Key   string `xml:"key,attr"`
					Value string `xml:"value,attr"`
				} `xml:"metadata"`
			} `xml:"part"`
		} `xml:"object"`
	}
	if err := xml.Unmarshal([]byte(settings), &cfg); err != nil {
		t.Fatalf("model_settings.config did not parse as XML: %v\n%s", err, settings)
	}

	findValue := func(entries []struct {
		Key   string `xml:"key,attr"`
		Value string `xml:"value,attr"`
	}, key string) (string, bool) {
		for _, e := range entries {
			if e.Key == key {
				return e.Value, true
			}
		}
		return "", false
	}

	objName, ok := findValue(cfg.Object.Metadata, "name")
	if !ok {
		t.Fatal("object metadata missing name entry")
	}
	if objName != trouble {
		t.Errorf("round-tripped object name = %q, want %q", objName, trouble)
	}

	partName, ok := findValue(cfg.Object.Part.Metadata, "name")
	if !ok {
		t.Fatal("part metadata missing name entry")
	}
	if partName != trouble {
		t.Errorf("round-tripped part name = %q, want %q", partName, trouble)
	}
}

// TestEncode3MF_EscapesAttachmentMetadata covers Fix 1 for the plain Encode
// path: Attachment.Path/ContentType interpolated into [Content_Types].xml
// and _rels/.rels must be escaped, or those OPC parts fail to parse.
func TestEncode3MF_EscapesAttachmentMetadata(t *testing.T) {
	m := triCube()
	m.Attachments = []geom.Attachment{{
		Path:        `Metadata/R&D<x>.json`,
		ContentType: `application/x-"test"`,
		Data:        []byte(`{}`),
	}}
	var buf bytes.Buffer
	if err := Encode(&buf, m); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	ct := readZipPart(t, buf.Bytes(), "[Content_Types].xml")
	mustParseXML(t, "[Content_Types].xml", ct)
	rels := readZipPart(t, buf.Bytes(), "_rels/.rels")
	mustParseXML(t, "_rels/.rels", rels)
}

// TestBambu_ReservedAttachmentPathErrors covers Fix 7 through the public
// EncodeBambu entry point: an attachment path colliding with a part
// EncodeBambu itself writes must fail the encode, not silently double-write
// the part.
func TestBambu_ReservedAttachmentPathErrors(t *testing.T) {
	o := twoPartObject()
	o.Attachments = []geom.Attachment{{Path: "Metadata/model_settings.config", Data: []byte("{}")}}
	var buf bytes.Buffer
	if err := EncodeBambu(&buf, o); err == nil {
		t.Fatal("expected error encoding an attachment that collides with Metadata/model_settings.config, got nil")
	}
}

// TestDerivedUUID_DependsOnNameAndIndex covers Fix 4: the 3MF production
// extension spec scopes UUID identity to both name and index. Hashing index
// alone means every two-part object meshio writes shares an identical set of
// UUIDs no matter what the parts are named.
func TestDerivedUUID_DependsOnNameAndIndex(t *testing.T) {
	if derivedUUID("component", "base", 1) == derivedUUID("component", "tiles", 1) {
		t.Error("derivedUUID must depend on name: same label and index, different names, got equal UUIDs")
	}
	if derivedUUID("component", "base", 1) == derivedUUID("component", "base", 2) {
		t.Error("derivedUUID must depend on index: same label and name, different index, got equal UUIDs")
	}
	if derivedUUID("component", "base", 1) != derivedUUID("component", "base", 1) {
		t.Error("derivedUUID must be a pure function of its inputs: same inputs produced different UUIDs")
	}
}

// TestBambu_NestedRelsNotAttachments covers Fix 2: 3D/_rels/3dmodel.model.rels
// is OPC plumbing that EncodeBambu writes itself, not user data. It must not
// round-trip through Decode as an Attachment -- re-encoding a decoded mesh
// would otherwise write the stale rels part back out via the generic
// attachment path, alongside a ContentType="" Override for it, producing an
// invalid package.
func TestBambu_NestedRelsNotAttachments(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodeBambu(&buf, twoPartObject()); err != nil {
		t.Fatalf("EncodeBambu: %v", err)
	}
	m, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	for _, a := range m.Attachments {
		if strings.Contains(a.Path, "_rels") {
			t.Errorf("attachment %q leaked OPC rels plumbing into Attachments", a.Path)
		}
	}
}

// mustParseXML fails the test unless s parses as well-formed XML from start
// to end (unlike xml.Unmarshal into a struct, this doesn't stop early or
// silently ignore unknown elements/attributes).
func mustParseXML(t *testing.T, label, s string) {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(s))
	for {
		_, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				return
			}
			t.Fatalf("%s did not parse: %v\n%s", label, err, s)
		}
	}
}

func allMatches(pattern, s string) []string {
	re := regexp.MustCompile(pattern)
	var out []string
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}
