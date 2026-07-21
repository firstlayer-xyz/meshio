package stl

import (
	"bytes"
	"path/filepath"
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

func TestSTLRoundTrip(t *testing.T) {
	orig := triangle()
	var buf bytes.Buffer
	if err := Encode(&buf, orig); err != nil {
		t.Fatalf("EncodeSTL: %v", err)
	}
	decoded, err := Decode(&buf)
	if err != nil {
		t.Fatalf("DecodeSTL: %v", err)
	}
	if len(decoded.Indices)/3 != 1 {
		t.Errorf("STL round-trip: expected 1 triangle, got %d", len(decoded.Indices)/3)
	}
	if len(decoded.Vertices)/3 != 3 {
		t.Errorf("STL round-trip: expected 3 vertices, got %d", len(decoded.Vertices)/3)
	}
}

// TestWriteReadRoundTrip exercises the path-based API: Write then Read,
// confirming geometry survives a real file round trip.
func TestWriteReadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cube.stl")

	orig := coloredCube()
	if err := Write(path, orig); err != nil {
		t.Fatalf("Write: %v", err)
	}

	decoded, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(decoded.Indices)/3 != 12 {
		t.Errorf("round trip: expected 12 triangles, got %d", len(decoded.Indices)/3)
	}
	if len(decoded.Vertices)/3 != 8 {
		t.Errorf("round trip: expected 8 vertices, got %d", len(decoded.Vertices)/3)
	}
}

func TestSTLCubeRoundTrip(t *testing.T) {
	orig := coloredCube()
	var buf bytes.Buffer
	if err := Encode(&buf, orig); err != nil {
		t.Fatalf("EncodeSTL: %v", err)
	}
	decoded, err := Decode(&buf)
	if err != nil {
		t.Fatalf("DecodeSTL: %v", err)
	}
	if len(decoded.Indices)/3 != 12 {
		t.Errorf("STL cube: expected 12 triangles, got %d", len(decoded.Indices)/3)
	}
}

func TestSTLEmpty(t *testing.T) {
	m := &geom.Mesh{}
	var buf bytes.Buffer
	if err := Encode(&buf, m); err == nil {
		t.Error("EncodeSTL: expected error for empty mesh")
	}
}

func TestSTLASCIIRoundTrip(t *testing.T) {
	ascii := `solid test
  facet normal 0 0 1
    outer loop
      vertex 0 0 0
      vertex 1 0 0
      vertex 0 1 0
    endloop
  endfacet
endsolid test
`
	decoded, err := Decode(bytes.NewReader([]byte(ascii)))
	if err != nil {
		t.Fatalf("DecodeSTL ASCII: %v", err)
	}
	if len(decoded.Indices)/3 != 1 {
		t.Errorf("STL ASCII: expected 1 triangle, got %d", len(decoded.Indices)/3)
	}
	if len(decoded.Vertices)/3 != 3 {
		t.Errorf("STL ASCII: expected 3 vertices, got %d", len(decoded.Vertices)/3)
	}
}
