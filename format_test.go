package meshio

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// binarySTL builds a binary STL with the given header text and triangle count.
// Every triangle is the same degenerate face; only the framing matters here.
func binarySTL(header string, numTris uint32) []byte {
	var buf bytes.Buffer
	h := make([]byte, 80)
	copy(h, header)
	buf.Write(h)
	binary.Write(&buf, binary.LittleEndian, numTris)
	for i := uint32(0); i < numTris; i++ {
		binary.Write(&buf, binary.LittleEndian, make([]float32, 12))
		buf.Write([]byte{0, 0})
	}
	return buf.Bytes()
}

const asciiSTLSample = `solid cube
 facet normal 0 0 1
  outer loop
   vertex 0 0 0
   vertex 1 0 0
   vertex 0 1 0
  endloop
 endfacet
endsolid cube
`

const objSample = `# exported by something
# a second comment line

mtllib model.mtl
o cube
v 0 0 0
v 1 0 0
v 0 1 0
vn 0 0 1
usemtl red
f 1 2 3
`

func TestProbe(t *testing.T) {
	tests := []struct {
		name   string
		sample []byte
		want   Format
	}{
		{"ascii stl", []byte(asciiSTLSample), FormatSTL},
		{"ascii stl with leading whitespace", []byte("\n  " + asciiSTLSample), FormatSTL},
		{"binary stl", binarySTL("meshio test", 2), FormatSTL},
		// Some exporters write "solid..." into a binary STL's 80-byte header.
		// Both variants are FormatSTL, so the ambiguity must not surface.
		{"binary stl with solid header", binarySTL("solid cube exported by evil", 2), FormatSTL},
		{"obj", []byte(objSample), FormatOBJ},
		{"obj with only vertices", []byte("v 0 0 0\nv 1 0 0\n"), FormatOBJ},
		{"empty", nil, FormatUnknown},
		{"short garbage", []byte("xy"), FormatUnknown},
		{"prose", []byte("the quick brown fox jumps over the lazy dog\n"), FormatUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Probe(tc.sample); got != tc.want {
				t.Errorf("Probe() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestProbe_ZipMagicIs3MF(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("[Content_Types].xml")
	io.WriteString(w, "<Types/>")
	zw.Close()

	if got := Probe(buf.Bytes()); got != Format3MF {
		t.Errorf("Probe(zip) = %q, want %q", got, Format3MF)
	}
}

// Probe sees only bytes, so it cannot tell a 3MF from any other zip. ProbeFile
// can, because it can open the archive.
func TestProbeFile_RejectsNon3MFZip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notmesh.zip")

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("hello.txt")
	io.WriteString(w, "not a mesh")
	zw.Close()
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ProbeFile(path)
	if err != nil {
		t.Fatalf("ProbeFile: %v", err)
	}
	if got != FormatUnknown {
		t.Errorf("ProbeFile(plain zip) = %q, want %q", got, FormatUnknown)
	}
}

func TestProbeFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	var threemfBuf bytes.Buffer
	if err := Encode(&threemfBuf, triangle(), Format3MF); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		path string
		want Format
	}{
		{"binary stl", write("a.stl", binarySTL("hdr", 3)), FormatSTL},
		{"ascii stl", write("b.stl", []byte(asciiSTLSample)), FormatSTL},
		{"obj", write("c.obj", []byte(objSample)), FormatOBJ},
		{"3mf", write("d.3mf", threemfBuf.Bytes()), Format3MF},
		// The extension is not consulted: content decides.
		{"3mf named .stl", write("lying.stl", threemfBuf.Bytes()), Format3MF},
		{"obj named .dat", write("e.dat", []byte(objSample)), FormatOBJ},
		{"empty file", write("f.bin", nil), FormatUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ProbeFile(tc.path)
			if err != nil {
				t.Fatalf("ProbeFile: %v", err)
			}
			if got != tc.want {
				t.Errorf("ProbeFile(%s) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// A binary STL is exactly 84 + 50*n bytes. ProbeFile knows the file size, so it
// can require an exact match instead of guessing from the triangle count.
func TestProbeFile_BinarySTLSizeIsExact(t *testing.T) {
	dir := t.TempDir()
	good := binarySTL("hdr", 4)
	if len(good) != 84+50*4 {
		t.Fatalf("fixture wrong: %d bytes", len(good))
	}

	exact := filepath.Join(dir, "exact.bin")
	os.WriteFile(exact, good, 0o644)
	if got, _ := ProbeFile(exact); got != FormatSTL {
		t.Errorf("exact size: got %q, want %q", got, FormatSTL)
	}

	// One byte of trailing junk means the triangle count does not describe the
	// file, so it is not a binary STL.
	extra := filepath.Join(dir, "extra.bin")
	os.WriteFile(extra, append(append([]byte{}, good...), 0), 0o644)
	if got, _ := ProbeFile(extra); got == FormatSTL {
		t.Errorf("size off by one still probed as STL")
	}
}

func TestProbeFile_MissingFile(t *testing.T) {
	if _, err := ProbeFile(filepath.Join(t.TempDir(), "nope.stl")); err == nil {
		t.Error("ProbeFile on a missing file returned no error")
	}
}

// A binary STL's float payload can contain a newline followed by "v ", which
// looks exactly like an OBJ vertex line. Checking OBJ before binary STL
// therefore steals real binary STLs -- the reverse cannot happen, because
// looksLikeBinarySTL requires a NUL in the first 84 bytes and text has none.
// So the binary check is the more discriminating one and must run first.
func TestProbe_BinarySTLWithOBJishPayload(t *testing.T) {
	b := binarySTL("exported by something", 4)
	copy(b[100:], []byte("\nv 1 2 3\n"))

	if got := Probe(b); got != FormatSTL {
		t.Errorf("binary STL containing %q identified as %q, want %q", "\nv ", got, FormatSTL)
	}
}

// Probe and ProbeFile are the same identification at two strengths: ProbeFile
// may be more decisive, but it must never name a different format.
func TestProbe_AgreesWithProbeFile(t *testing.T) {
	objish := binarySTL("exported by something", 4)
	copy(objish[100:], []byte("\nv 1 2 3\n"))

	cases := []struct {
		name string
		body []byte
	}{
		{"ascii stl", []byte(asciiSTLSample)},
		{"obj", []byte(objSample)},
		{"binary stl", binarySTL("header", 4)},
		{"binary stl with objish payload", objish},
		{"binary stl with solid header", binarySTL("solid but binary", 4)},
	}

	dir := t.TempDir()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(dir, tc.name)
			if err := os.WriteFile(p, tc.body, 0o644); err != nil {
				t.Fatal(err)
			}
			fromFile, err := ProbeFile(p)
			if err != nil {
				t.Fatalf("ProbeFile: %v", err)
			}
			if fromSample := Probe(tc.body); fromSample != fromFile {
				t.Errorf("Probe = %q but ProbeFile = %q; the two must not name different formats",
					fromSample, fromFile)
			}
		})
	}
}
