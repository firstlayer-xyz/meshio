// Package stl reads and writes STL triangle meshes, binary and ASCII.
// STL carries no color; FaceColors are ignored on encode.
package stl

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/firstlayer-xyz/meshio/geom"
)

// Decode reads a binary or ASCII STL from r.
func Decode(r io.Reader) (*geom.Mesh, error) {
	// Read enough to detect format. Binary STL has 80-byte header + 4-byte count.
	buf := bufio.NewReader(r)
	header, err := buf.Peek(84)
	if err != nil {
		return nil, fmt.Errorf("meshio: reading STL header: %w", err)
	}

	// ASCII STL starts with "solid" (but some binary files do too).
	// Check if the claimed triangle count is plausible for binary.
	if isASCIISTL(header) {
		return decodeSTLASCII(buf)
	}
	return decodeSTLBinary(buf)
}

func isASCIISTL(header []byte) bool {
	trimmed := strings.TrimSpace(string(header[:80]))
	if !strings.HasPrefix(trimmed, "solid") {
		return false
	}
	// Heuristic: if the 4 bytes after the header give a triangle count
	// that would make the file unreasonably large, it's probably ASCII.
	numTris := binary.LittleEndian.Uint32(header[80:84])
	if numTris == 0 || numTris > 100_000_000 {
		return true
	}
	return false
}

func decodeSTLBinary(r io.Reader) (*geom.Mesh, error) {
	// Skip 80-byte header
	var header [80]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, fmt.Errorf("meshio: reading STL header: %w", err)
	}

	var numTris uint32
	if err := binary.Read(r, binary.LittleEndian, &numTris); err != nil {
		return nil, fmt.Errorf("meshio: reading triangle count: %w", err)
	}

	vertices := make([]float32, 0, numTris*9)
	indices := make([]uint32, 0, numTris*3)

	buf := make([]byte, 50)
	for i := uint32(0); i < numTris; i++ {
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, fmt.Errorf("meshio: reading triangle %d: %w", i, err)
		}

		// Skip normal (bytes 0-11), read 3 vertices (bytes 12-47)
		base := uint32(len(vertices) / 3)
		for v := 0; v < 3; v++ {
			off := 12 + v*12
			x := math.Float32frombits(binary.LittleEndian.Uint32(buf[off:]))
			y := math.Float32frombits(binary.LittleEndian.Uint32(buf[off+4:]))
			z := math.Float32frombits(binary.LittleEndian.Uint32(buf[off+8:]))
			vertices = append(vertices, x, y, z)
		}
		indices = append(indices, base, base+1, base+2)
	}

	m := &geom.Mesh{Geometry: geom.Geometry{Vertices: vertices, Indices: indices}}
	m.MergeVertices()
	return m, nil
}

func decodeSTLASCII(r io.Reader) (*geom.Mesh, error) {
	scanner := bufio.NewScanner(r)
	var vertices []float32
	var indices []uint32

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "vertex ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 4 {
			continue
		}
		x, err1 := strconv.ParseFloat(fields[1], 32)
		y, err2 := strconv.ParseFloat(fields[2], 32)
		z, err3 := strconv.ParseFloat(fields[3], 32)
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		idx := uint32(len(vertices) / 3)
		vertices = append(vertices, float32(x), float32(y), float32(z))
		indices = append(indices, idx)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("meshio: reading ASCII STL: %w", err)
	}

	if len(vertices) == 0 {
		return nil, fmt.Errorf("meshio: no vertices found in ASCII STL")
	}

	m := &geom.Mesh{Geometry: geom.Geometry{Vertices: vertices, Indices: indices}}
	m.MergeVertices()
	return m, nil
}

// Encode writes the mesh as binary STL to w.
// STL does not support per-face color; FaceColors are ignored.
// It mutates m: MergeVertices is called on the caller's mesh as a side effect.
func Encode(w io.Writer, m *geom.Mesh) error {
	m.MergeVertices()
	numTris := len(m.Indices) / 3

	if len(m.Vertices) == 0 || numTris == 0 {
		return fmt.Errorf("meshio: empty mesh")
	}

	// 80-byte header
	var header [80]byte
	copy(header[:], "Facet STL export")
	if _, err := w.Write(header[:]); err != nil {
		return fmt.Errorf("meshio: writing header: %w", err)
	}

	// Triangle count (uint32 little-endian)
	if err := binary.Write(w, binary.LittleEndian, uint32(numTris)); err != nil {
		return fmt.Errorf("meshio: writing triangle count: %w", err)
	}

	// Each triangle: normal (3×float32) + 3 vertices (9×float32) + attribute (uint16) = 50 bytes
	buf := make([]byte, 50)
	for i := 0; i < numTris; i++ {
		i0 := m.Indices[i*3]
		i1 := m.Indices[i*3+1]
		i2 := m.Indices[i*3+2]

		v0x := m.Vertices[i0*3]
		v0y := m.Vertices[i0*3+1]
		v0z := m.Vertices[i0*3+2]
		v1x := m.Vertices[i1*3]
		v1y := m.Vertices[i1*3+1]
		v1z := m.Vertices[i1*3+2]
		v2x := m.Vertices[i2*3]
		v2y := m.Vertices[i2*3+1]
		v2z := m.Vertices[i2*3+2]

		// Face normal via cross product
		e1x := v1x - v0x
		e1y := v1y - v0y
		e1z := v1z - v0z
		e2x := v2x - v0x
		e2y := v2y - v0y
		e2z := v2z - v0z
		nx := e1y*e2z - e1z*e2y
		ny := e1z*e2x - e1x*e2z
		nz := e1x*e2y - e1y*e2x
		ln := float32(math.Sqrt(float64(nx*nx + ny*ny + nz*nz)))
		if ln > 0 {
			nx /= ln
			ny /= ln
			nz /= ln
		}

		binary.LittleEndian.PutUint32(buf[0:], math.Float32bits(nx))
		binary.LittleEndian.PutUint32(buf[4:], math.Float32bits(ny))
		binary.LittleEndian.PutUint32(buf[8:], math.Float32bits(nz))

		binary.LittleEndian.PutUint32(buf[12:], math.Float32bits(v0x))
		binary.LittleEndian.PutUint32(buf[16:], math.Float32bits(v0y))
		binary.LittleEndian.PutUint32(buf[20:], math.Float32bits(v0z))

		binary.LittleEndian.PutUint32(buf[24:], math.Float32bits(v1x))
		binary.LittleEndian.PutUint32(buf[28:], math.Float32bits(v1y))
		binary.LittleEndian.PutUint32(buf[32:], math.Float32bits(v1z))

		binary.LittleEndian.PutUint32(buf[36:], math.Float32bits(v2x))
		binary.LittleEndian.PutUint32(buf[40:], math.Float32bits(v2y))
		binary.LittleEndian.PutUint32(buf[44:], math.Float32bits(v2z))

		// Attribute byte count (unused)
		buf[48] = 0
		buf[49] = 0

		if _, err := w.Write(buf); err != nil {
			return fmt.Errorf("meshio: writing triangle %d: %w", i, err)
		}
	}

	return nil
}

// Read reads a binary or ASCII STL file.
func Read(path string) (*geom.Mesh, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("meshio: %w", err)
	}
	defer f.Close()
	return Decode(f)
}

// Write exports a Mesh to a binary STL file at the given path.
func Write(path string, m *geom.Mesh) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("meshio: %w", err)
	}
	defer f.Close()
	return Encode(f, m)
}
