package meshio

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// DecodeSTL reads a binary or ASCII STL from r.
func DecodeSTL(r io.Reader) (*Mesh, error) {
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

func decodeSTLBinary(r io.Reader) (*Mesh, error) {
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

	m := &Mesh{Vertices: vertices, Indices: indices}
	m.MergeVertices()
	return m, nil
}

func decodeSTLASCII(r io.Reader) (*Mesh, error) {
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

	m := &Mesh{Vertices: vertices, Indices: indices}
	m.MergeVertices()
	return m, nil
}
