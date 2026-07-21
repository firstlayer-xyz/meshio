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
		return EncodeSTL(w, m)
	case "obj":
		return EncodeOBJ(w, m, nil)
	case "3mf":
		return Encode3MF(w, m)
	default:
		return fmt.Errorf("meshio: unsupported format %q", format)
	}
}

// Decode reads a mesh from r in the specified format.
// Supported formats: "stl", "obj", "3mf".
func Decode(r io.Reader, format string) (*Mesh, error) {
	switch strings.ToLower(format) {
	case "stl":
		return DecodeSTL(r)
	case "obj":
		return DecodeOBJ(r)
	case "3mf":
		return Decode3MF(r)
	default:
		return nil, fmt.Errorf("meshio: unsupported format %q", format)
	}
}

// readers maps a lowercase file extension to its reader. It is the single
// source of truth for which mesh formats Read (and the desktop app, facetc,
// and facetrender) treat as importable meshes.
var readers = map[string]func(string) (*Mesh, error){
	".stl": ReadSTL,
	".obj": ReadOBJ,
	".3mf": Read3MF,
}

// Read reads a mesh file, auto-detecting format from the extension.
func Read(path string) (*Mesh, error) {
	ext := strings.ToLower(pathExt(path))
	r, ok := readers[ext]
	if !ok {
		return nil, fmt.Errorf("meshio: unsupported file extension %q", ext)
	}
	return r(path)
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

// ReadSTL reads a binary or ASCII STL file.
func ReadSTL(path string) (*Mesh, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("meshio: %w", err)
	}
	defer f.Close()
	return DecodeSTL(f)
}

// ReadOBJ reads a Wavefront OBJ file.
func ReadOBJ(path string) (*Mesh, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("meshio: %w", err)
	}
	defer f.Close()
	return DecodeOBJ(f)
}

// Read3MF reads a 3MF file.
func Read3MF(path string) (*Mesh, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("meshio: %w", err)
	}
	defer f.Close()
	return Decode3MF(f)
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
