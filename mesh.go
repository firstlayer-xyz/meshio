// Package meshio reads and writes triangle mesh files (3MF, STL, OBJ).
//
// Core encode/decode functions work with io.Writer/io.Reader.
// Convenience functions handle file I/O via path strings.
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
func Encode(w io.Writer, m *Mesh, format string) error {
	switch strings.ToLower(format) {
	case "stl":
		return stl.Encode(w, m)
	case "obj":
		return obj.Encode(w, m, nil)
	case "3mf":
		return threemf.Encode(w, m)
	default:
		return fmt.Errorf("meshio: unsupported format %q", format)
	}
}

// Decode reads a mesh from r in the specified format.
// Supported formats: "stl", "obj", "3mf".
func Decode(r io.Reader, format string) (*Mesh, error) {
	switch strings.ToLower(format) {
	case "stl":
		return stl.Decode(r)
	case "obj":
		return obj.Decode(r)
	case "3mf":
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

// Read reads a mesh file, auto-detecting format from the extension.
func Read(path string) (*Mesh, error) {
	ext := strings.ToLower(pathExt(path))
	dec, ok := readers[ext]
	if !ok {
		return nil, fmt.Errorf("meshio: unsupported file extension %q", ext)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("meshio: %w", err)
	}
	defer f.Close()
	return dec(f)
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
