// Package geom holds the shared mesh types: geometry, per-face display color,
// and package attachments. It is a leaf package -- it imports no other package
// in this module, so every format package can depend on it without a cycle.
package geom

// Geometry is triangle geometry — positions and indices, with no presentation
// or packaging concerns. It is the substrate shared by Mesh (display) and
// Part (printing).
type Geometry struct {
	Vertices []float32 // flat xyz positions (len = numVerts * 3)
	Indices  []uint32  // triangle vertex indices (len = numTris * 3)
}

// MergeVertices deduplicates coincident vertices by snapping coordinates
// to a grid and remapping indices. This produces a watertight mesh where
// adjacent triangles share vertex indices.
func (g *Geometry) MergeVertices() {
	numVerts := len(g.Vertices) / 3
	if numVerts == 0 {
		return
	}

	type vertKey struct{ x, y, z float32 }
	seen := make(map[vertKey]uint32, numVerts)
	remap := make([]uint32, numVerts)
	var merged []float32

	for i := 0; i < numVerts; i++ {
		k := vertKey{g.Vertices[i*3], g.Vertices[i*3+1], g.Vertices[i*3+2]}
		if idx, ok := seen[k]; ok {
			remap[i] = idx
		} else {
			idx := uint32(len(merged) / 3)
			seen[k] = idx
			remap[i] = idx
			merged = append(merged, k.x, k.y, k.z)
		}
	}

	for i := range g.Indices {
		g.Indices[i] = remap[g.Indices[i]]
	}
	g.Vertices = merged
}
