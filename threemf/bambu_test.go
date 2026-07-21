package threemf

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
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

func TestBambu_Deterministic(t *testing.T) {
	var a, b bytes.Buffer
	if err := EncodeBambu(&a, twoPartObject()); err != nil {
		t.Fatalf("first encode: %v", err)
	}
	if err := EncodeBambu(&b, twoPartObject()); err != nil {
		t.Fatalf("second encode: %v", err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("encoding twice produced different bytes; output must be deterministic")
	}
}

func TestBambu_GeometryRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodeBambu(&buf, twoPartObject()); err != nil {
		t.Fatalf("EncodeBambu: %v", err)
	}
	m, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode of our own output: %v", err)
	}
	// Two unit triangles: 6 indices.
	if len(m.Indices) != 6 {
		t.Errorf("round-trip indices: got %d, want 6", len(m.Indices))
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

func allMatches(pattern, s string) []string {
	re := regexp.MustCompile(pattern)
	var out []string
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}
