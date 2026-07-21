package threemf

import (
	"archive/zip"
	"bytes"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/firstlayer-xyz/meshio/geom"
)

// shiftedTri returns unitTri translated along x, so merged parts stay
// distinguishable by vertex position.
func shiftedTri(dx float32) geom.Geometry {
	base := unitTri()
	v := make([]float32, len(base.Vertices))
	copy(v, base.Vertices)
	for i := 0; i < len(v); i += 3 {
		v[i] += dx
	}
	return geom.Geometry{Vertices: v, Indices: base.Indices}
}

func threePartPrusaObject() *Object {
	o := &Object{
		Name:     "plate",
		Filament: 1,
		Parts: []Part{
			{Name: "base", Geometry: unitTri()},
			{Name: "mid", Geometry: shiftedTri(10), Filament: 2},
			{Name: "top", Geometry: shiftedTri(20), Filament: 3},
		},
	}
	o.SetSlotColor(1, "#FF0000")
	o.SetSlotColor(2, "#00FF00")
	o.SetSlotColor(3, "#0000FF")
	return o
}

func TestPrusa_MergesPartsIntoOneObject(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodePrusa(&buf, threePartPrusaObject()); err != nil {
		t.Fatalf("EncodePrusa: %v", err)
	}
	model := readZipPart(t, buf.Bytes(), "3D/3dmodel.model")

	// PrusaSlicer deletes objects that have components but no mesh, so a
	// component-based package would import as a single material. There must be
	// exactly one object, carrying one mesh, with no components at all.
	if got := strings.Count(model, "<object "); got != 1 {
		t.Errorf("expected exactly 1 object, got %d", got)
	}
	if got := strings.Count(model, "<mesh>"); got != 1 {
		t.Errorf("expected exactly 1 mesh, got %d", got)
	}
	if strings.Contains(model, "<component") || strings.Contains(model, "p:path") {
		t.Error("model must not use components; PrusaSlicer discards component-only objects")
	}
	// Three unit triangles merged.
	if got := strings.Count(model, "<triangle "); got != 3 {
		t.Errorf("expected 3 triangles in the merged mesh, got %d", got)
	}
}

func TestPrusa_MergeOffsetsIndices(t *testing.T) {
	o := threePartPrusaObject()
	merged, ranges := o.mergeParts()

	if len(merged.Vertices)/3 != 9 {
		t.Fatalf("merged verts: got %d, want 9", len(merged.Vertices)/3)
	}
	// Part 2's indices must be shifted past part 1's three vertices, not left
	// pointing at part 1's geometry.
	if merged.Indices[3] != 3 || merged.Indices[4] != 4 || merged.Indices[5] != 5 {
		t.Errorf("second part's indices not offset: got %v", merged.Indices[3:6])
	}
	if merged.Indices[6] != 6 {
		t.Errorf("third part's indices not offset: got %d, want 6", merged.Indices[6])
	}
	// The shifted vertex must survive the merge, proving geometry is
	// concatenated rather than one part being written three times.
	if merged.Vertices[9] != 10 {
		t.Errorf("second part's x offset lost: got %v", merged.Vertices[9])
	}

	want := []volumeRange{
		{name: "base", first: 0, last: 0, slot: 1},
		{name: "mid", first: 1, last: 1, slot: 2},
		{name: "top", first: 2, last: 2, slot: 3},
	}
	if len(ranges) != len(want) {
		t.Fatalf("ranges: got %d, want %d", len(ranges), len(want))
	}
	for i := range want {
		if ranges[i] != want[i] {
			t.Errorf("range %d = %+v, want %+v", i, ranges[i], want[i])
		}
	}
}

func TestPrusa_VolumeRangesPartitionTheStream(t *testing.T) {
	// Parts of differing triangle counts, so an off-by-one in the running
	// offset cannot hide behind uniform sizes.
	o := &Object{Name: "plate", Parts: []Part{
		{Name: "one-tri", Geometry: unitTri(), Filament: 1},
		{Name: "two-tri", Geometry: geom.Geometry{
			Vertices: []float32{0, 0, 0, 1, 0, 0, 0, 1, 0, 1, 1, 0},
			Indices:  []uint32{0, 1, 2, 1, 3, 2},
		}, Filament: 2},
		{Name: "one-more", Geometry: shiftedTri(5), Filament: 3},
	}}

	var buf bytes.Buffer
	if err := EncodePrusa(&buf, o); err != nil {
		t.Fatalf("EncodePrusa: %v", err)
	}
	config := readZipPart(t, buf.Bytes(), prusaModelConfigPath)
	model := readZipPart(t, buf.Bytes(), "3D/3dmodel.model")

	pairs := regexp.MustCompile(`<volume firstid="(\d+)" lastid="(\d+)"`).FindAllStringSubmatch(config, -1)
	if len(pairs) != 3 {
		t.Fatalf("expected 3 volumes, got %d", len(pairs))
	}
	want := [][2]string{{"0", "0"}, {"1", "2"}, {"3", "3"}}
	for i, p := range pairs {
		if p[1] != want[i][0] || p[2] != want[i][1] {
			t.Errorf("volume %d = %s..%s, want %s..%s", i, p[1], p[2], want[i][0], want[i][1])
		}
	}

	// The last range must end exactly at the final triangle. PrusaSlicer's
	// _generate_volumes rejects a lastid past the mesh, so an over-long range
	// fails the whole import rather than degrading.
	numTris := strings.Count(model, "<triangle ")
	if want[len(want)-1][1] != "3" || numTris != 4 {
		t.Errorf("ranges must cover exactly the emitted %d triangles", numTris)
	}
}

func TestPrusa_ExtruderPerVolume(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodePrusa(&buf, threePartPrusaObject()); err != nil {
		t.Fatalf("EncodePrusa: %v", err)
	}
	config := readZipPart(t, buf.Bytes(), prusaModelConfigPath)

	// PrusaSlicer distinguishes object- from volume-scope by the type
	// attribute and reads the semantic name from key -- unlike the Bambu
	// dialect, where key alone carries it.
	got := allMatches(`<metadata type="volume" key="extruder" value="(\d+)"`, config)
	want := []string{"1", "2", "3"}
	if len(got) != len(want) {
		t.Fatalf("volume extruders: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("volume %d extruder = %s, want %s", i, got[i], want[i])
		}
	}
	if !strings.Contains(config, `<metadata type="object" key="extruder" value="1"/>`) {
		t.Errorf("missing object-level default extruder:\n%s", config)
	}
}

func TestPrusa_InheritsObjectSlot(t *testing.T) {
	o := &Object{Name: "plate", Filament: 4, Parts: []Part{
		{Name: "inherits", Geometry: unitTri()},
		{Name: "explicit", Geometry: shiftedTri(10), Filament: 7},
	}}
	var buf bytes.Buffer
	if err := EncodePrusa(&buf, o); err != nil {
		t.Fatalf("EncodePrusa: %v", err)
	}
	config := readZipPart(t, buf.Bytes(), prusaModelConfigPath)
	got := allMatches(`<metadata type="volume" key="extruder" value="(\d+)"`, config)
	if len(got) != 2 || got[0] != "4" || got[1] != "7" {
		t.Errorf("slot inheritance: got %v, want [4 7]", got)
	}
}

func TestPrusa_PackageShape(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodePrusa(&buf, threePartPrusaObject()); err != nil {
		t.Fatalf("EncodePrusa: %v", err)
	}

	// A nested 3D/_rels/ is what tells Bambu Studio a file is NOT from Prusa,
	// and this layout has no second model part to relate anyway.
	names := zipEntryNames(t, buf.Bytes())
	for _, n := range names {
		if strings.Contains(n, "3D/_rels/") || strings.Contains(n, "3D/Objects/") {
			t.Errorf("unexpected part %q: the Prusa layout is a single model part", n)
		}
	}
	if !contains(names, prusaModelConfigPath) {
		t.Errorf("missing %s; got %v", prusaModelConfigPath, names)
	}

	model := readZipPart(t, buf.Bytes(), "3D/3dmodel.model")
	if strings.Contains(model, "requiredextensions") || strings.Contains(model, "xmlns:p=") {
		t.Error("the production extension is unnecessary for a single model part")
	}
	// Never claim to be another application: Bambu routes a file to its
	// PrusaSlicer-compat importer when Application contains "PrusaSlicer".
	if !strings.Contains(model, `<metadata name="Application">meshio</metadata>`) {
		t.Error("Application metadata must identify meshio honestly")
	}
}

func TestPrusa_ConfigObjectIDMatchesModel(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodePrusa(&buf, threePartPrusaObject()); err != nil {
		t.Fatalf("EncodePrusa: %v", err)
	}
	model := readZipPart(t, buf.Bytes(), "3D/3dmodel.model")
	config := readZipPart(t, buf.Bytes(), prusaModelConfigPath)

	// PrusaSlicer looks the config's object id up against the ids of objects
	// that carry geometry. A mismatch silently discards every slot assignment.
	modelID := regexp.MustCompile(`<object id="(\d+)"`).FindStringSubmatch(model)
	configID := regexp.MustCompile(`<object id="(\d+)"`).FindStringSubmatch(config)
	if modelID[1] != configID[1] {
		t.Errorf("config object id %s does not match model object id %s", configID[1], modelID[1])
	}
	if !strings.Contains(model, `<item objectid="`+modelID[1]+`"`) {
		t.Errorf("build item does not reference object %s", modelID[1])
	}
}

func TestPrusa_ColorGroupDoesNotCollideWithObjectID(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodePrusa(&buf, threePartPrusaObject()); err != nil {
		t.Fatalf("EncodePrusa: %v", err)
	}
	model := readZipPart(t, buf.Bytes(), "3D/3dmodel.model")

	objectID := regexp.MustCompile(`<object id="(\d+)"`).FindStringSubmatch(model)[1]
	groupID := regexp.MustCompile(`<m:colorgroup id="(\d+)"`).FindStringSubmatch(model)[1]
	if objectID == groupID {
		t.Errorf("colorgroup id %s collides with the object id", groupID)
	}
	for _, p := range allMatches(`pid="(\d+)"`, model) {
		if p != groupID {
			t.Errorf("pid %s does not resolve to the colorgroup (%s)", p, groupID)
		}
	}
}

func TestPrusa_NoColorsOmitsColorgroup(t *testing.T) {
	o := &Object{Name: "plate", Parts: []Part{{Name: "a", Geometry: unitTri(), Filament: 1}}}
	var buf bytes.Buffer
	if err := EncodePrusa(&buf, o); err != nil {
		t.Fatalf("EncodePrusa: %v", err)
	}
	model := readZipPart(t, buf.Bytes(), "3D/3dmodel.model")
	if strings.Contains(model, "colorgroup") || strings.Contains(model, "pid=") {
		t.Errorf("no SlotColors set, but color output was written:\n%s", model)
	}
}

func TestPrusa_Deterministic(t *testing.T) {
	var a, b bytes.Buffer
	if err := EncodePrusa(&a, threePartPrusaObject()); err != nil {
		t.Fatalf("first encode: %v", err)
	}
	if err := EncodePrusa(&b, threePartPrusaObject()); err != nil {
		t.Fatalf("second encode: %v", err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("encoding twice produced different bytes; output must be deterministic")
	}
}

func TestPrusa_EscapesNames(t *testing.T) {
	o := &Object{Name: `R&D <plate> "v2"`, Parts: []Part{
		{Name: `a&b<c>"d`, Geometry: unitTri(), Filament: 1},
	}}
	var buf bytes.Buffer
	if err := EncodePrusa(&buf, o); err != nil {
		t.Fatalf("EncodePrusa: %v", err)
	}
	// The config is the only place slots live; if it fails to parse, every
	// assignment is silently lost.
	mustParseXML(t, prusaModelConfigPath, readZipPart(t, buf.Bytes(), prusaModelConfigPath))
	mustParseXML(t, "3D/3dmodel.model", readZipPart(t, buf.Bytes(), "3D/3dmodel.model"))
}

func TestPrusa_ValidationPropagates(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodePrusa(&buf, &Object{}); err == nil {
		t.Fatal("expected an error encoding an object with no parts")
	}

	o := &Object{
		Parts:       []Part{{Name: "a", Geometry: unitTri()}},
		Attachments: []geom.Attachment{{Path: prusaModelConfigPath}},
	}
	err := EncodePrusa(&buf, o)
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Errorf("attachment colliding with the config part should be rejected, got %v", err)
	}
}

func TestPrusa_GeometryRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodePrusa(&buf, threePartPrusaObject()); err != nil {
		t.Fatalf("EncodePrusa: %v", err)
	}
	m, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode of our own output: %v", err)
	}
	if len(m.Indices) != 9 {
		t.Fatalf("round-trip indices: got %d, want 9", len(m.Indices))
	}
	// Follow the triangles rather than scanning the vertex pool: a merge that
	// forgot to offset indices still appends every part's vertices, so the
	// shifted ones would be present but unreferenced.
	seen := map[float32]bool{}
	for _, idx := range m.Indices {
		seen[m.Vertices[int(idx)*3]] = true
	}
	for _, want := range []float32{0, 10, 20} {
		if !seen[want] {
			t.Errorf("no triangle references a vertex at x=%v; part geometry was lost or aliased", want)
		}
	}
}

func TestWritePrusaReadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plate.3mf")
	if err := WritePrusa(path, threePartPrusaObject()); err != nil {
		t.Fatalf("WritePrusa: %v", err)
	}
	m, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(m.Indices) != 9 {
		t.Errorf("round-trip indices: got %d, want 9", len(m.Indices))
	}
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// zipEntryNames returns every part name in the package, in archive order.
func zipEntryNames(t *testing.T, data []byte) []string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	return names
}
