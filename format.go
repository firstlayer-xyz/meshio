package meshio

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
)

// Format identifies a mesh file format. The zero value, FormatUnknown, means
// the format could not be determined.
//
// Format is a string type whose values match the format names Encode and Decode
// accept, so a probed format feeds straight back into them.
type Format string

const (
	FormatUnknown Format = ""
	FormatSTL     Format = "stl"
	FormatOBJ     Format = "obj"
	Format3MF     Format = "3mf"
)

// probeSampleSize is how many leading bytes ProbeFile reads to identify a file.
// Large enough to clear an OBJ's comment preamble and a binary STL's 84-byte
// header, small enough to stay a single read.
const probeSampleSize = 4096

// binarySTLHeaderSize is a binary STL's 80-byte header plus the uint32 triangle
// count that follows it. Each triangle is then 50 bytes: 12 float32s and a
// 2-byte attribute count.
const (
	binarySTLHeaderSize   = 84
	binarySTLTriangleSize = 50
)

// Probe identifies the format of a file from its leading bytes. It returns
// FormatUnknown if the sample matches nothing.
//
// Probe is best-effort: seeing only a prefix, it cannot tell a 3MF from any
// other zip, nor verify that a binary STL's triangle count describes the whole
// file. ProbeFile resolves both, because it can open the archive and stat the
// file. Prefer ProbeFile when you have a path.
func Probe(sample []byte) Format {
	// Order matters. The text checks run before the binary-STL check because a
	// binary STL has no magic number: it is identified by plausible framing
	// alone, and arbitrary text is all too capable of looking plausible.
	if hasZipMagic(sample) {
		return Format3MF
	}
	if isASCIISTLSample(sample) {
		return FormatSTL
	}
	if isOBJSample(sample) {
		return FormatOBJ
	}
	if looksLikeBinarySTL(sample) {
		return FormatSTL
	}
	return FormatUnknown
}

// ProbeFile identifies the format of the file at path by its content. The
// extension is not consulted, so a mislabeled file is still identified
// correctly. It returns FormatUnknown if the content matches no known format,
// and an error only if the file cannot be read.
func ProbeFile(path string) (Format, error) {
	f, err := os.Open(path)
	if err != nil {
		return FormatUnknown, fmt.Errorf("meshio: %w", err)
	}
	defer f.Close()
	return probeOpenFile(f, path)
}

// probeOpenFile identifies the format of an already-open file. It is shared by
// ProbeFile and by Read, which probes the handle it is about to decode from
// rather than opening the file twice. The file offset is left unspecified; a
// caller that goes on to read must rewind.
func probeOpenFile(f *os.File, path string) (Format, error) {
	info, err := f.Stat()
	if err != nil {
		return FormatUnknown, fmt.Errorf("meshio: %w", err)
	}
	sample := make([]byte, probeSampleSize)
	n, err := io.ReadFull(f, sample)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return FormatUnknown, fmt.Errorf("meshio: reading %s: %w", path, err)
	}
	return probeWithSize(sample[:n], info.Size(), f)
}

// probeWithSize identifies a format using the whole file: the archive contents
// for a zip, and the exact length for a binary STL. Both are checks Probe
// cannot make from a prefix alone.
func probeWithSize(sample []byte, size int64, r io.ReaderAt) (Format, error) {
	if hasZipMagic(sample) {
		if isThreeMFArchive(r, size) {
			return Format3MF, nil
		}
		// A zip that is not an OPC package. Nothing else starts with PK, so
		// there is no point trying the remaining formats.
		return FormatUnknown, nil
	}
	if isASCIISTLSample(sample) {
		return FormatSTL, nil
	}
	if isBinarySTLOfSize(sample, size) {
		return FormatSTL, nil
	}
	if isOBJSample(sample) {
		return FormatOBJ, nil
	}
	return FormatUnknown, nil
}

// hasZipMagic reports whether b starts with a zip local-file, end-of-central-
// directory, or spanned-archive signature. A 3MF is an OPC package, which is a
// zip.
func hasZipMagic(b []byte) bool {
	if len(b) < 4 || b[0] != 'P' || b[1] != 'K' {
		return false
	}
	switch {
	case b[2] == 0x03 && b[3] == 0x04, // local file header
		b[2] == 0x05 && b[3] == 0x06, // end of central directory (empty archive)
		b[2] == 0x07 && b[3] == 0x08: // spanned archive
		return true
	}
	return false
}

// isThreeMFArchive reports whether the zip is an OPC package. Every OPC package
// must contain [Content_Types].xml, which is what separates a 3MF from an
// arbitrary zip.
func isThreeMFArchive(r io.ReaderAt, size int64) bool {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return false
	}
	for _, f := range zr.File {
		if f.Name == "[Content_Types].xml" {
			return true
		}
	}
	return false
}

// isASCIISTLSample reports whether the sample begins with an ASCII STL's
// "solid" keyword.
//
// A binary STL may also carry "solid" in its 80-byte header -- some exporters
// write one -- but both variants are FormatSTL, so the ambiguity never reaches
// the caller. stl.Decode makes the binary/ASCII distinction itself.
func isASCIISTLSample(b []byte) bool {
	trimmed := strings.TrimLeft(string(b), " \t\r\n")
	return strings.HasPrefix(trimmed, "solid")
}

// isBinarySTLOfSize reports whether size is exactly the length the sample's
// triangle count describes: an 84-byte header plus 50 bytes per triangle. This
// is decisive -- no other format's bytes at offset 80 will happen to predict
// its own file length.
func isBinarySTLOfSize(sample []byte, size int64) bool {
	if len(sample) < binarySTLHeaderSize || size < binarySTLHeaderSize {
		return false
	}
	numTris := binary.LittleEndian.Uint32(sample[80:84])
	return size == int64(binarySTLHeaderSize)+int64(numTris)*binarySTLTriangleSize
}

// looksLikeBinarySTL reports whether the sample is plausibly the start of a
// binary STL, for the case where the file length is unknown.
//
// Without the length this cannot be decisive, so it requires a NUL byte in the
// header region: a binary STL's header is padded with NULs, and its triangle
// count is a little-endian uint32 whose high bytes are NUL for any realistic
// mesh. Text formats have no NULs at all, which is what keeps prose from
// matching.
func looksLikeBinarySTL(sample []byte) bool {
	if len(sample) < binarySTLHeaderSize {
		return false
	}
	if !bytes.ContainsRune(sample[:binarySTLHeaderSize], 0) {
		return false
	}
	numTris := binary.LittleEndian.Uint32(sample[80:84])
	if numTris == 0 {
		return false
	}
	// The sample cannot be longer than the file the count describes.
	predicted := int64(binarySTLHeaderSize) + int64(numTris)*binarySTLTriangleSize
	return predicted >= int64(len(sample))
}

// isOBJSample reports whether the sample looks like a Wavefront OBJ.
//
// OBJ has no magic number, so this requires a real geometry line -- a vertex or
// a face -- rather than accepting the comments, blank lines, and material
// references that may precede it. Keyword lines are matched with a trailing
// space so "vertex" from an ASCII STL cannot pass as OBJ's "v".
func isOBJSample(b []byte) bool {
	for _, raw := range strings.Split(string(b), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "v ") || strings.HasPrefix(line, "f ") {
			return true
		}
	}
	return false
}
