package meshio

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
)

// EncodeSTL writes the mesh as binary STL to w.
// STL does not support per-face color; FaceColors are ignored.
func (m *Mesh) EncodeSTL(w io.Writer) error {
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

// WriteSTL exports a Mesh to a binary STL file at the given path.
func (m *Mesh) WriteSTL(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("meshio: %w", err)
	}
	defer f.Close()
	return m.EncodeSTL(f)
}
