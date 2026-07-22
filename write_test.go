package meshio

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestWriteDispatch(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"m.stl", "m.obj", "m.3mf"} {
		path := filepath.Join(dir, name)
		if err := Write(path, triangle()); err != nil {
			t.Fatalf("Write(%s): %v", name, err)
		}
		got, err := Read(path)
		if err != nil {
			t.Fatalf("Read(%s): %v", name, err)
		}
		if n := len(got.Indices) / 3; n != 1 {
			t.Errorf("%s: round-tripped %d triangles, want 1", name, n)
		}
	}
}

// Write dispatches on the extension because there is no content to probe when
// creating a file.
func TestWriteUnsupportedExtension(t *testing.T) {
	err := Write(filepath.Join(t.TempDir(), "m.ply"), triangle())
	if err == nil {
		t.Fatal("Write accepted an unsupported extension")
	}
}

// obj.Write emits a companion .mtl when the mesh carries face colors; the root
// Write must not lose that by routing around it.
func TestWriteOBJProducesMaterialFile(t *testing.T) {
	dir := t.TempDir()
	m := triangle()
	m.FaceColors = []FaceColor{{Hex: "#FF0000"}}
	path := filepath.Join(dir, "colored.obj")
	if err := Write(path, m); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "colored.mtl")); err != nil {
		t.Errorf("companion .mtl not written: %v", err)
	}
}

func TestCanWrite(t *testing.T) {
	for _, path := range []string{"a.stl", "b.OBJ", "c.3mf"} {
		if !CanWrite(path) {
			t.Errorf("CanWrite(%q) = false, want true", path)
		}
	}
	for _, path := range []string{"a.ply", "b", "c.txt"} {
		if CanWrite(path) {
			t.Errorf("CanWrite(%q) = true, want false", path)
		}
	}
}

func TestWriteExtensions(t *testing.T) {
	got := WriteExtensions()
	sort.Strings(got)
	want := []string{".3mf", ".obj", ".stl"}
	if len(got) != len(want) {
		t.Fatalf("WriteExtensions() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("WriteExtensions() = %v, want %v", got, want)
		}
	}
}

// Read identifies by content first, so a file whose extension lies about its
// format still loads.
func TestReadPrefersContentOverExtension(t *testing.T) {
	dir := t.TempDir()

	threemfPath := filepath.Join(dir, "real.3mf")
	if err := Write(threemfPath, triangle()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(threemfPath)
	if err != nil {
		t.Fatal(err)
	}

	lying := filepath.Join(dir, "lying.stl")
	if err := os.WriteFile(lying, data, 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := Read(lying)
	if err != nil {
		t.Fatalf("Read of a 3MF named .stl: %v", err)
	}
	if n := len(m.Indices) / 3; n != 1 {
		t.Errorf("got %d triangles, want 1", n)
	}
}

// An unknown extension is fine as long as the content identifies itself.
func TestReadUnknownExtensionWithKnownContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mesh.dat")
	if err := os.WriteFile(path, []byte(objSample), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Read(path)
	if err != nil {
		t.Fatalf("Read of an OBJ named .dat: %v", err)
	}
	if n := len(m.Indices) / 3; n != 1 {
		t.Errorf("got %d triangles, want 1", n)
	}
}

// When the content is unidentifiable, the extension is the fallback.
//
// The fixture is a binary STL with a trailing byte: its triangle count no
// longer predicts the file length, so ProbeFile will not claim it, but the
// decoder reads it fine because it ignores anything past the last triangle.
func TestReadFallsBackToExtension(t *testing.T) {
	dir := t.TempDir()
	data := append(binarySTL("hdr", 1), 0xFF)
	path := filepath.Join(dir, "padded.stl")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	format, err := ProbeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if format != FormatUnknown {
		t.Fatalf("fixture probed as %q; it must be unidentifiable to test the fallback", format)
	}

	m, err := Read(path)
	if err != nil {
		t.Fatalf("Read did not fall back to the extension: %v", err)
	}
	if n := len(m.Indices) / 3; n != 1 {
		t.Errorf("got %d triangles, want 1", n)
	}
}

func TestReadUnidentifiable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mystery.dat")
	if err := os.WriteFile(path, []byte("nothing meshy here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path); err == nil {
		t.Error("Read accepted a file identifiable by neither content nor extension")
	}
}

// CanRead answers from the path alone while Read identifies by content, so Read
// is strictly the more capable of the two. This pins that relationship: CanRead
// is a lower bound and may say no where Read succeeds, but it must never say
// yes where Read cannot even choose a decoder.
func TestCanReadIsALowerBoundOnRead(t *testing.T) {
	dir := t.TempDir()

	// Extensionless ASCII STL: CanRead says no, Read loads it anyway.
	noExt := filepath.Join(dir, "model")
	if err := os.WriteFile(noExt, []byte(asciiSTLSample), 0o644); err != nil {
		t.Fatal(err)
	}
	if CanRead(noExt) {
		t.Error("CanRead should answer from the extension alone, and there is none")
	}
	if _, err := Read(noExt); err != nil {
		t.Errorf("Read should identify this by content: %v", err)
	}

	// Known extension: CanRead says yes, and Read must be able to proceed.
	withExt := filepath.Join(dir, "model.stl")
	if err := os.WriteFile(withExt, []byte(asciiSTLSample), 0o644); err != nil {
		t.Fatal(err)
	}
	if !CanRead(withExt) {
		t.Error("CanRead should accept a known extension")
	}
	if _, err := Read(withExt); err != nil {
		t.Errorf("Read failed on a file CanRead accepted: %v", err)
	}
}
