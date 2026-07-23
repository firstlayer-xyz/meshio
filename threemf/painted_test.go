package threemf

import (
	"bytes"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/firstlayer-xyz/meshio/geom"
)

// quad is two triangles sharing an edge, so a fixture can paint one and leave
// the other bare.
func quad() geom.Geometry {
	return geom.Geometry{
		Vertices: []float32{0, 0, 0, 1, 0, 0, 0, 1, 0, 1, 1, 0},
		Indices:  []uint32{0, 1, 2, 1, 3, 2},
	}
}

func paintedQuad() *PaintedMesh {
	p := &PaintedMesh{Name: "plate", Geometry: quad(), FaceSlots: []int{1, 2}}
	p.SetSlotColor(1, "#FF0000")
	p.SetSlotColor(2, "#0000FF")
	return p
}

func TestPainted_WritesOneObjectOneMesh(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodePainted(&buf, paintedQuad()); err != nil {
		t.Fatalf("EncodePainted: %v", err)
	}
	model := readZipPart(t, buf.Bytes(), "3D/3dmodel.model")

	if got := strings.Count(model, "<object "); got != 1 {
		t.Errorf("objects = %d, want 1: paint keeps the caller's geometry whole", got)
	}
	if got := strings.Count(model, "<mesh>"); got != 1 {
		t.Errorf("meshes = %d, want 1", got)
	}
	if strings.Contains(model, "<component") {
		t.Error("painted output must not split into components")
	}
	if !strings.Contains(model, `<metadata name="Application">meshio</metadata>`) {
		t.Error("Application must identify meshio honestly")
	}
	if !strings.Contains(model, "xmlns:slic3rpe=") {
		t.Error("slic3rpe namespace must be declared when paint is emitted")
	}
}

func TestPainted_BothAttributesPerTriangle(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodePainted(&buf, paintedQuad()); err != nil {
		t.Fatalf("EncodePainted: %v", err)
	}
	model := readZipPart(t, buf.Bytes(), "3D/3dmodel.model")

	mmu := allMatches(`slic3rpe:mmu_segmentation="([^"]*)"`, model)
	bbs := allMatches(`paint_color="([^"]*)"`, model)
	if len(mmu) != 2 || len(bbs) != 2 {
		t.Fatalf("attribute counts: mmu=%d paint_color=%d, want 2 and 2", len(mmu), len(bbs))
	}
	for i := range mmu {
		if mmu[i] != bbs[i] {
			t.Errorf("triangle %d: mmu=%q but paint_color=%q; both must carry the same value", i, mmu[i], bbs[i])
		}
	}
	if mmu[0] != "4" || mmu[1] != "8" {
		t.Errorf("paint values = %v, want [4 8] for slots 1 and 2", mmu)
	}
}

func TestPainted_UnpaintedTriangleOmitsAttributes(t *testing.T) {
	p := &PaintedMesh{Geometry: quad(), FaceSlots: []int{2, 0}}
	p.SetSlotColor(2, "#0000FF")

	var buf bytes.Buffer
	if err := EncodePainted(&buf, p); err != nil {
		t.Fatalf("EncodePainted: %v", err)
	}
	model := readZipPart(t, buf.Bytes(), "3D/3dmodel.model")

	if got := strings.Count(model, "paint_color="); got != 1 {
		t.Errorf("painted triangles = %d, want 1", got)
	}
	if strings.Contains(model, `paint_color=""`) {
		t.Error("an unpainted triangle must omit the attribute, not emit it empty")
	}
}

func TestPainted_ColorGroupFromSlotColors(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodePainted(&buf, paintedQuad()); err != nil {
		t.Fatalf("EncodePainted: %v", err)
	}
	model := readZipPart(t, buf.Bytes(), "3D/3dmodel.model")

	if !strings.Contains(model, `<m:color color="#FF0000FF" />`) {
		t.Errorf("slot 1 color missing from the colorgroup:\n%s", model)
	}
	objectID := regexp.MustCompile(`<object id="(\d+)"`).FindStringSubmatch(model)[1]
	groupID := regexp.MustCompile(`<m:colorgroup id="(\d+)"`).FindStringSubmatch(model)[1]
	if objectID == groupID {
		t.Errorf("colorgroup id %s collides with the object id", groupID)
	}
}

func TestPainted_NoColorsOmitsColorgroup(t *testing.T) {
	p := &PaintedMesh{Geometry: quad(), FaceSlots: []int{1, 1}}
	var buf bytes.Buffer
	if err := EncodePainted(&buf, p); err != nil {
		t.Fatalf("EncodePainted: %v", err)
	}
	model := readZipPart(t, buf.Bytes(), "3D/3dmodel.model")
	if strings.Contains(model, "colorgroup") || strings.Contains(model, "pid=") {
		t.Errorf("no SlotColors set, but color output was written:\n%s", model)
	}
	// Paint is independent of display color and must still be present.
	if !strings.Contains(model, `paint_color="4"`) {
		t.Error("paint must be emitted even with no SlotColors")
	}
}

func TestPainted_Validation(t *testing.T) {
	tests := []struct {
		name string
		mesh *PaintedMesh
		want string
	}{
		{
			name: "valid",
			mesh: &PaintedMesh{Geometry: quad(), FaceSlots: []int{1, 2}},
			want: "",
		},
		{
			name: "empty geometry",
			mesh: &PaintedMesh{FaceSlots: []int{}},
			want: "empty geometry",
		},
		{
			name: "too few face slots",
			mesh: &PaintedMesh{Geometry: quad(), FaceSlots: []int{1}},
			want: "face slots",
		},
		{
			name: "too many face slots",
			mesh: &PaintedMesh{Geometry: quad(), FaceSlots: []int{1, 2, 3}},
			want: "face slots",
		},
		{
			name: "negative slot",
			mesh: &PaintedMesh{Geometry: quad(), FaceSlots: []int{1, -1}},
			want: "negative filament slot",
		},
		{
			name: "slot above the shared ceiling",
			mesh: &PaintedMesh{Geometry: quad(), FaceSlots: []int{1, 17}},
			want: "exceeds 16",
		},
		{
			name: "slot at the ceiling is fine",
			mesh: &PaintedMesh{Geometry: quad(), FaceSlots: []int{1, 16}},
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.mesh.validate()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("expected valid, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected an error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestPainted_Deterministic(t *testing.T) {
	var a, b bytes.Buffer
	if err := EncodePainted(&a, paintedQuad()); err != nil {
		t.Fatalf("first encode: %v", err)
	}
	if err := EncodePainted(&b, paintedQuad()); err != nil {
		t.Fatalf("second encode: %v", err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("encoding twice produced different bytes")
	}
}

func TestPainted_GeometrySurvivesRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodePainted(&buf, paintedQuad()); err != nil {
		t.Fatalf("EncodePainted: %v", err)
	}
	// Paint is write-only: Decode drops it, but the geometry must be intact.
	m, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(m.Indices) != 6 {
		t.Errorf("round-trip indices = %d, want 6", len(m.Indices))
	}
}

func TestWritePainted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plate.3mf")
	if err := WritePainted(path, paintedQuad()); err != nil {
		t.Fatalf("WritePainted: %v", err)
	}
	if _, err := Read(path); err != nil {
		t.Fatalf("Read: %v", err)
	}
}
