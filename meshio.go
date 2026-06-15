// Package meshio reads and writes triangle mesh files (3MF, STL, OBJ).
//
// Core encode/decode functions work with io.Writer/io.Reader.
// Convenience methods on Mesh handle file I/O via path strings.
package meshio

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// FaceColor holds per-triangle color information.
type FaceColor struct {
	Hex string // "#RRGGBB" or "#RRGGBBAA"
}

// Attachment is an extra OPC part carried inside a 3MF package alongside the
// mesh. It round-trips opaque bytes — meshio assigns no meaning to the content.
// Path is package-relative (e.g. "Metadata/extra.json"); ContentType is
// the OPC content type registered for the part.
type Attachment struct {
	Path        string
	ContentType string
	Data        []byte
}

// Mesh holds triangle geometry and optional per-face colors.
type Mesh struct {
	Vertices    []float32    // flat xyz positions (len = numVerts * 3)
	Indices     []uint32     // triangle vertex indices (len = numTris * 3)
	FaceColors  []FaceColor  // per-triangle color (len = numTris, or nil/empty for no color)
	Attachments []Attachment // extra OPC parts (3MF only); nil for none
}

// MergeVertices deduplicates coincident vertices by snapping coordinates
// to a grid and remapping indices. This produces a watertight mesh where
// adjacent triangles share vertex indices.
func (m *Mesh) MergeVertices() {
	numVerts := len(m.Vertices) / 3
	if numVerts == 0 {
		return
	}

	type vertKey struct{ x, y, z float32 }
	seen := make(map[vertKey]uint32, numVerts)
	remap := make([]uint32, numVerts)
	var merged []float32

	for i := 0; i < numVerts; i++ {
		k := vertKey{m.Vertices[i*3], m.Vertices[i*3+1], m.Vertices[i*3+2]}
		if idx, ok := seen[k]; ok {
			remap[i] = idx
		} else {
			idx := uint32(len(merged) / 3)
			seen[k] = idx
			remap[i] = idx
			merged = append(merged, k.x, k.y, k.z)
		}
	}

	for i := range m.Indices {
		m.Indices[i] = remap[m.Indices[i]]
	}
	m.Vertices = merged
}

// Encode writes the mesh to w in the specified format.
// Supported formats: "stl", "obj", "3mf".
func (m *Mesh) Encode(w io.Writer, format string) error {
	switch strings.ToLower(format) {
	case "stl":
		return m.EncodeSTL(w)
	case "obj":
		return m.EncodeOBJ(w, nil)
	case "3mf":
		return m.Encode3MF(w)
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

// Read reads a mesh file, auto-detecting format from the extension.
func Read(path string) (*Mesh, error) {
	ext := strings.ToLower(pathExt(path))
	switch ext {
	case ".stl":
		return ReadSTL(path)
	case ".obj":
		return ReadOBJ(path)
	case ".3mf":
		return Read3MF(path)
	default:
		return nil, fmt.Errorf("meshio: unsupported file extension %q", ext)
	}
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
