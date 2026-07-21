package meshio

import "testing"

func TestGeometryMergeVertices(t *testing.T) {
	g := &Geometry{
		Vertices: []float32{0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 0},
		Indices:  []uint32{0, 1, 2, 3, 1, 2},
	}
	g.MergeVertices()

	if len(g.Vertices) != 9 {
		t.Fatalf("expected 9 floats (3 verts) after merge, got %d", len(g.Vertices))
	}
	if g.Indices[3] != 0 {
		t.Errorf("duplicate vertex 3 should remap to 0, got %d", g.Indices[3])
	}
}

func TestMeshEmbedsGeometry(t *testing.T) {
	m := &Mesh{
		Geometry:   Geometry{Vertices: []float32{0, 0, 0, 1, 0, 0, 0, 1, 0}, Indices: []uint32{0, 1, 2}},
		FaceColors: []FaceColor{{Hex: "#FF0000"}},
	}
	// Promoted through embedding.
	if len(m.Vertices) != 9 {
		t.Fatalf("promoted Vertices: got %d floats", len(m.Vertices))
	}
	m.MergeVertices()
	if len(m.Indices) != 3 {
		t.Errorf("promoted MergeVertices: got %d indices", len(m.Indices))
	}
}
