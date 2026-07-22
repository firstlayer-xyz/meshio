// Package meshio reads and writes triangle mesh files (3MF, STL, OBJ).
//
// Core encode/decode functions work with io.Writer/io.Reader. Read and Write
// handle file I/O via path strings.
//
// Read identifies a file by its content, falling back to the extension only
// when the content matches nothing known, so a mislabeled file still loads.
// Probe and ProbeFile expose that identification directly. Write dispatches on
// the extension, there being no content to identify when creating a file.
package meshio

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/firstlayer-xyz/meshio/geom"
	"github.com/firstlayer-xyz/meshio/obj"
	"github.com/firstlayer-xyz/meshio/stl"
	"github.com/firstlayer-xyz/meshio/threemf"
)

// Re-exported from geom so callers can use meshio.Mesh without importing geom
// directly. These are aliases, not new types: meshio.Mesh and geom.Mesh are
// the same type and are freely interchangeable.
type (
	Geometry   = geom.Geometry
	Mesh       = geom.Mesh
	FaceColor  = geom.FaceColor
	Attachment = geom.Attachment
)

// Encode writes the mesh to w in the specified format.
// Supported formats: "stl", "obj", "3mf".
//
// For "obj", this writes geometry only: no mtllib directive or companion
// material file, since Encode has no path to derive one from. Use obj.Write
// (or obj.Encode directly) for material output.
func Encode(w io.Writer, m *Mesh, format Format) error {
	switch Format(strings.ToLower(string(format))) {
	case FormatSTL:
		return stl.Encode(w, m)
	case FormatOBJ:
		return obj.Encode(w, m, nil, "")
	case Format3MF:
		return threemf.Encode(w, m)
	default:
		return fmt.Errorf("meshio: unsupported format %q", format)
	}
}

// Decode reads a mesh from r in the specified format.
// Supported formats: "stl", "obj", "3mf".
func Decode(r io.Reader, format Format) (*Mesh, error) {
	switch Format(strings.ToLower(string(format))) {
	case FormatSTL:
		return stl.Decode(r)
	case FormatOBJ:
		return obj.Decode(r)
	case Format3MF:
		return threemf.Decode(r)
	default:
		return nil, fmt.Errorf("meshio: unsupported format %q", format)
	}
}

// readers maps a lowercase file extension to its decoder. It is the single
// source of truth for which mesh formats Read treats as importable.
var readers = map[string]func(io.Reader) (*Mesh, error){
	".stl": stl.Decode,
	".obj": obj.Decode,
	".3mf": threemf.Decode,
}

// writers maps a lowercase file extension to its file writer. It is the single
// source of truth for which mesh formats Write can produce.
//
// Unlike Read, Write dispatches on the extension alone: there is no content to
// identify when creating a file, so the path is the only signal.
//
// These are the per-format Write functions rather than the Encode functions, so
// obj.Write still emits its companion .mtl -- which it can, having a path to
// derive the name from.
var writers = map[string]func(string, *Mesh) error{
	".stl": stl.Write,
	".obj": obj.Write,
	".3mf": threemf.Write,
}

// formatDecoders maps a probed Format to its decoder, so Read can act on what
// ProbeFile found.
var formatDecoders = map[Format]func(io.Reader) (*Mesh, error){
	FormatSTL: stl.Decode,
	FormatOBJ: obj.Decode,
	Format3MF: threemf.Decode,
}

// Read reads a mesh file, identifying the format from its content and falling
// back to the file extension when the content matches nothing known.
//
// Content wins because it is the more trustworthy signal: a 3MF saved as
// "model.stl" still loads. The extension is consulted only for files that
// ProbeFile cannot identify -- an OBJ holding nothing but comments, say, which
// has no distinguishing bytes but is still perfectly decodable.
func Read(path string) (*Mesh, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("meshio: %w", err)
	}
	defer f.Close()

	dec, err := readerFor(f, path)
	if err != nil {
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("meshio: %w", err)
	}
	return dec(f)
}

// readerFor picks a decoder for an open file: by probed content first, then by
// extension. It leaves f's offset unspecified; the caller rewinds.
func readerFor(f *os.File, path string) (func(io.Reader) (*Mesh, error), error) {
	format, err := probeOpenFile(f, path)
	if err != nil {
		return nil, err
	}
	if dec, ok := formatDecoders[format]; ok {
		return dec, nil
	}

	ext := strings.ToLower(pathExt(path))
	dec, ok := readers[ext]
	if !ok {
		return nil, fmt.Errorf("meshio: cannot identify %s: content matches no known format and extension %q is unsupported", path, ext)
	}
	return dec, nil
}

// Write writes a mesh file, choosing the format from the file extension.
//
// For ".obj" this also writes a companion .mtl beside it when the mesh carries
// face colors, matching obj.Write.
func Write(path string, m *Mesh) error {
	ext := strings.ToLower(pathExt(path))
	w, ok := writers[ext]
	if !ok {
		return fmt.Errorf("meshio: unsupported file extension %q", ext)
	}
	return w(path, m)
}

// CanWrite reports whether Write can produce the file at path, by extension.
func CanWrite(path string) bool {
	_, ok := writers[strings.ToLower(pathExt(path))]
	return ok
}

// WriteExtensions returns the exportable mesh extensions (each with a leading
// dot), in no particular order.
func WriteExtensions() []string {
	exts := make([]string, 0, len(writers))
	for e := range writers {
		exts = append(exts, e)
	}
	return exts
}

// CanRead reports whether Read can decode the file at path, by extension.
func CanRead(path string) bool {
	_, ok := readers[strings.ToLower(pathExt(path))]
	return ok
}

// ReadExtensions returns the importable mesh extensions (each with a leading
// dot), in no particular order.
func ReadExtensions() []string {
	exts := make([]string, 0, len(readers))
	for e := range readers {
		exts = append(exts, e)
	}
	return exts
}

// pathExt returns the file extension including the dot.
func pathExt(path string) string {
	for i := len(path) - 1; i >= 0 && path[i] != '/' && path[i] != '\\'; i-- {
		if path[i] == '.' {
			return path[i:]
		}
	}
	return ""
}
