package meshio

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/firstlayer-xyz/meshio/obj"
	"github.com/firstlayer-xyz/meshio/stl"
	"github.com/firstlayer-xyz/meshio/threemf"
)

// triangle is a simple single-triangle mesh for testing.
func triangle() *Mesh {
	return &Mesh{
		Geometry: Geometry{
			Vertices: []float32{0, 0, 0, 1, 0, 0, 0, 1, 0},
			Indices:  []uint32{0, 1, 2},
		},
	}
}

// --- Encode/Decode dispatch ---

func TestEncodeDecodeDispatch(t *testing.T) {
	orig := triangle()

	for _, format := range []Format{FormatSTL, FormatOBJ} {
		var buf bytes.Buffer
		if err := Encode(&buf, orig, format); err != nil {
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
	if err := Encode(&buf, orig, "3mf"); err != nil {
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

// TestReadDispatch writes a file of each supported format via that format's
// own path-based Write function, then confirms meshio.Read auto-detects the
// format from the extension and decodes it end to end.
func TestReadDispatch(t *testing.T) {
	dir := t.TempDir()

	stlPath := filepath.Join(dir, "part.stl")
	if err := stl.Write(stlPath, triangle()); err != nil {
		t.Fatalf("stl.Write: %v", err)
	}

	objPath := filepath.Join(dir, "part.obj")
	if err := obj.Write(objPath, triangle()); err != nil {
		t.Fatalf("obj.Write: %v", err)
	}

	threemfPath := filepath.Join(dir, "part.3mf")
	if err := threemf.Write(threemfPath, triangle()); err != nil {
		t.Fatalf("threemf.Write: %v", err)
	}

	for _, path := range []string{stlPath, objPath, threemfPath} {
		decoded, err := Read(path)
		if err != nil {
			t.Fatalf("Read(%s): %v", path, err)
		}
		if len(decoded.Indices)/3 != 1 {
			t.Errorf("Read(%s): expected 1 triangle, got %d", path, len(decoded.Indices)/3)
		}
		if len(decoded.Vertices)/3 != 3 {
			t.Errorf("Read(%s): expected 3 vertices, got %d", path, len(decoded.Vertices)/3)
		}
	}
}

func TestEncodeUnsupported(t *testing.T) {
	m := triangle()
	var buf bytes.Buffer
	if err := Encode(&buf, m, "xyz"); err == nil {
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
