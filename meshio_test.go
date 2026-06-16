package meshio

import (
	"bytes"
	"testing"
)

// triangle is a simple single-triangle mesh for testing.
func triangle() *Mesh {
	return &Mesh{
		Vertices: []float32{0, 0, 0, 1, 0, 0, 0, 1, 0},
		Indices:  []uint32{0, 1, 2},
	}
}

// coloredCube returns a cube-like mesh (8 verts, 12 tris) with 2 face colors.
func coloredCube() *Mesh {
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
	fc := make([]FaceColor, 12)
	for i := 0; i < 4; i++ {
		fc[i] = FaceColor{Hex: "#FF0000"}
	}
	for i := 4; i < 12; i++ {
		fc[i] = FaceColor{Hex: "#0000FF"}
	}
	return &Mesh{Vertices: v, Indices: idx, FaceColors: fc}
}

func TestMergeVertices(t *testing.T) {
	// Duplicate vertices
	m := &Mesh{
		Vertices: []float32{0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 1, 0},
		Indices:  []uint32{0, 1, 2, 2, 1, 3},
	}
	m.MergeVertices()
	numVerts := len(m.Vertices) / 3
	if numVerts != 3 {
		t.Errorf("MergeVertices: expected 3 unique vertices, got %d", numVerts)
	}
	// Index 2 should have been remapped to 0
	if m.Indices[2] != 0 {
		t.Errorf("MergeVertices: expected index 2 remapped to 0, got %d", m.Indices[2])
	}
}

// --- STL ---

func TestSTLRoundTrip(t *testing.T) {
	orig := triangle()
	var buf bytes.Buffer
	if err := orig.EncodeSTL(&buf); err != nil {
		t.Fatalf("EncodeSTL: %v", err)
	}
	decoded, err := DecodeSTL(&buf)
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

func TestSTLCubeRoundTrip(t *testing.T) {
	orig := coloredCube()
	var buf bytes.Buffer
	if err := orig.EncodeSTL(&buf); err != nil {
		t.Fatalf("EncodeSTL: %v", err)
	}
	decoded, err := DecodeSTL(&buf)
	if err != nil {
		t.Fatalf("DecodeSTL: %v", err)
	}
	if len(decoded.Indices)/3 != 12 {
		t.Errorf("STL cube: expected 12 triangles, got %d", len(decoded.Indices)/3)
	}
}

func TestSTLEmpty(t *testing.T) {
	m := &Mesh{}
	var buf bytes.Buffer
	if err := m.EncodeSTL(&buf); err == nil {
		t.Error("EncodeSTL: expected error for empty mesh")
	}
}

// --- OBJ ---

func TestOBJRoundTrip(t *testing.T) {
	orig := triangle()
	var buf bytes.Buffer
	if err := orig.EncodeOBJ(&buf, nil); err != nil {
		t.Fatalf("EncodeOBJ: %v", err)
	}
	decoded, err := DecodeOBJ(&buf)
	if err != nil {
		t.Fatalf("DecodeOBJ: %v", err)
	}
	if len(decoded.Indices)/3 != 1 {
		t.Errorf("OBJ round-trip: expected 1 triangle, got %d", len(decoded.Indices)/3)
	}
	if len(decoded.Vertices)/3 != 3 {
		t.Errorf("OBJ round-trip: expected 3 vertices, got %d", len(decoded.Vertices)/3)
	}
}

func TestOBJWithMaterials(t *testing.T) {
	orig := coloredCube()
	var objBuf, mtlBuf bytes.Buffer
	if err := orig.EncodeOBJ(&objBuf, &mtlBuf); err != nil {
		t.Fatalf("EncodeOBJ: %v", err)
	}
	// MTL should contain both colors
	mtl := mtlBuf.String()
	if !bytes.Contains([]byte(mtl), []byte("ff0000")) {
		t.Error("OBJ MTL: missing red material")
	}
	if !bytes.Contains([]byte(mtl), []byte("0000ff")) {
		t.Error("OBJ MTL: missing blue material")
	}
	// OBJ should reference mtl
	obj := objBuf.String()
	if !bytes.Contains([]byte(obj), []byte("mtllib")) {
		t.Error("OBJ: missing mtllib directive")
	}
	if !bytes.Contains([]byte(obj), []byte("usemtl")) {
		t.Error("OBJ: missing usemtl directive")
	}
}

func TestOBJCubeRoundTrip(t *testing.T) {
	orig := coloredCube()
	var buf bytes.Buffer
	if err := orig.EncodeOBJ(&buf, nil); err != nil {
		t.Fatalf("EncodeOBJ: %v", err)
	}
	decoded, err := DecodeOBJ(&buf)
	if err != nil {
		t.Fatalf("DecodeOBJ: %v", err)
	}
	if len(decoded.Indices)/3 != 12 {
		t.Errorf("OBJ cube: expected 12 triangles, got %d", len(decoded.Indices)/3)
	}
}

func TestOBJQuadFan(t *testing.T) {
	obj := "v 0 0 0\nv 1 0 0\nv 1 1 0\nv 0 1 0\nf 1 2 3 4\n"
	decoded, err := DecodeOBJ(bytes.NewReader([]byte(obj)))
	if err != nil {
		t.Fatalf("DecodeOBJ quad: %v", err)
	}
	// Quad should be split into 2 triangles
	if len(decoded.Indices)/3 != 2 {
		t.Errorf("OBJ quad: expected 2 triangles, got %d", len(decoded.Indices)/3)
	}
}

func TestOBJEmpty(t *testing.T) {
	m := &Mesh{}
	var buf bytes.Buffer
	if err := m.EncodeOBJ(&buf, nil); err == nil {
		t.Error("EncodeOBJ: expected error for empty mesh")
	}
}

// --- 3MF ---

func TestThreeMFRoundTrip(t *testing.T) {
	orig := triangle()
	var buf bytes.Buffer
	if err := orig.Encode3MF(&buf); err != nil {
		t.Fatalf("Encode3MF: %v", err)
	}
	decoded, err := Decode3MF(&buf)
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
	if err := orig.Encode3MF(&buf); err != nil {
		t.Fatalf("Encode3MF: %v", err)
	}
	decoded, err := Decode3MF(&buf)
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
	m := &Mesh{}
	var buf bytes.Buffer
	if err := m.Encode3MF(&buf); err == nil {
		t.Error("Encode3MF: expected error for empty mesh")
	}
}

// --- Encode/Decode dispatch ---

func TestEncodeDecodeDispatch(t *testing.T) {
	orig := triangle()

	for _, format := range []string{"stl", "obj"} {
		var buf bytes.Buffer
		if err := orig.Encode(&buf, format); err != nil {
			t.Fatalf("Encode(%s): %v", format, err)
		}
		decoded, err := Decode(&buf, format)
		if err != nil {
			t.Fatalf("Decode(%s): %v", format, err)
		}
		if len(decoded.Indices)/3 != 1 {
			t.Errorf("Dispatch %s: expected 1 triangle, got %d", format, len(decoded.Indices)/3)
		}
	}

	// 3MF
	var buf bytes.Buffer
	if err := orig.Encode(&buf, "3mf"); err != nil {
		t.Fatalf("Encode(3mf): %v", err)
	}
	decoded, err := Decode(&buf, "3mf")
	if err != nil {
		t.Fatalf("Decode(3mf): %v", err)
	}
	if len(decoded.Indices)/3 != 1 {
		t.Errorf("Dispatch 3mf: expected 1 triangle, got %d", len(decoded.Indices)/3)
	}
}

func TestEncodeUnsupported(t *testing.T) {
	m := triangle()
	var buf bytes.Buffer
	if err := m.Encode(&buf, "xyz"); err == nil {
		t.Error("Encode: expected error for unsupported format")
	}
}

func TestDecodeUnsupported(t *testing.T) {
	var buf bytes.Buffer
	if _, err := Decode(&buf, "xyz"); err == nil {
		t.Error("Decode: expected error for unsupported format")
	}
}

// --- Helpers ---

func TestParseHexColor(t *testing.T) {
	r, g, b := parseHexColor("#FF8000")
	if r != 1.0 || g < 0.50 || g > 0.51 || b != 0 {
		t.Errorf("parseHexColor(#FF8000): got %g, %g, %g", r, g, b)
	}
}

func TestSanitizeHex(t *testing.T) {
	got := sanitizeHex("#FF00AA")
	if got != "ff00aa" {
		t.Errorf("sanitizeHex: expected ff00aa, got %s", got)
	}
}

func TestPathExt(t *testing.T) {
	tests := []struct{ path, ext string }{
		{"/foo/bar.stl", ".stl"},
		{"model.3mf", ".3mf"},
		{"noext", ""},
		{"/a/b.c/d.obj", ".obj"},
	}
	for _, tt := range tests {
		got := pathExt(tt.path)
		if got != tt.ext {
			t.Errorf("pathExt(%q): expected %q, got %q", tt.path, tt.ext, got)
		}
	}
}

func TestPathStem(t *testing.T) {
	tests := []struct{ path, stem string }{
		{"/foo/bar.stl", "bar"},
		{"model.3mf", "model"},
		{"noext", "noext"},
	}
	for _, tt := range tests {
		got := pathStem(tt.path)
		if got != tt.stem {
			t.Errorf("pathStem(%q): expected %q, got %q", tt.path, tt.stem, got)
		}
	}
}

func TestPathDir(t *testing.T) {
	tests := []struct{ path, dir string }{
		{"/foo/bar.stl", "/foo"},
		{"bar.stl", "."},
	}
	for _, tt := range tests {
		got := pathDir(tt.path)
		if got != tt.dir {
			t.Errorf("pathDir(%q): expected %q, got %q", tt.path, tt.dir, got)
		}
	}
}

// --- STL ASCII ---

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
	decoded, err := DecodeSTL(bytes.NewReader([]byte(ascii)))
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

func TestCanRead(t *testing.T) {
	cases := map[string]bool{
		"a.stl": true, "A.STL": true, "b.obj": true, "c.3mf": true,
		"d.fct": false, "e.png": false, "noext": false,
	}
	for path, want := range cases {
		if got := CanRead(path); got != want {
			t.Errorf("CanRead(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestReadExtensions(t *testing.T) {
	got := ReadExtensions()
	want := map[string]bool{".stl": true, ".obj": true, ".3mf": true}
	if len(got) != len(want) {
		t.Fatalf("ReadExtensions() = %v, want 3 entries", got)
	}
	for _, e := range got {
		if !want[e] {
			t.Errorf("unexpected extension %q", e)
		}
	}
}
