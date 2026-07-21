package geom

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

// Mesh holds triangle geometry, optional per-face display color, and any
// extra package parts. It is the interchange type: everything Read and
// Decode return.
type Mesh struct {
	Geometry                 // embedded: m.Vertices, m.Indices, m.MergeVertices()
	FaceColors  []FaceColor  // per-triangle display color (len = numTris, or nil)
	Attachments []Attachment // extra OPC parts (3MF only); nil for none
}
