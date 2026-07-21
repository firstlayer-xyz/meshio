# meshio Multi-Part Color Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let meshio express "this object is N sub-meshes, each printed with filament slot S" and write it in the dialect Bambu Studio actually consumes, so a generated 3MF slices multi-color instead of only *looking* multi-color.

**Architecture:** `Mesh` currently serves two jobs — display/interchange and the basis for printing — and a single per-face color field can only express the first. Split geometry into its own type, keep `Mesh` as the display type, and add `Object`/`Part` as the print type. Color is stored once per filament slot on `Object` and denormalized into per-triangle references at write time.

**Tech Stack:** Go 1.25.0, standard library only. Module `github.com/firstlayer-xyz/meshio`. No third-party dependencies — do not add any.

**Spec:** `docs/superpowers/specs/2026-07-21-meshio-multipart-color-design.md`

## Global Constraints

- **Zero dependencies.** Standard library only. Do not add anything to `go.mod`.
- **Deterministic output.** Encoding the same `Object` twice must produce byte-identical bytes. No `time.Now()`, no `math/rand`, no map iteration without sorting keys first.
- **Go 1.25.0**, module path `github.com/firstlayer-xyz/meshio`.
- **Test command:** `go test ./...` from the repo root. Single test: `go test -run TestName -v`.
- **Filament slots are 1-based** everywhere — matching the Bambu XML, the AMS UI, and `Object.Filament`. Slot `0` means "unset, inherit".
- **Hex colors** are `"#RRGGBB"` or `"#RRGGBBAA"`. `normalizeHex` (existing, `write_3mf.go`) pads 7-char values with `FF`.
- **Do not** write `Metadata/project_settings.config`, and **do not** declare a content type for `.config` parts — the verified Bambu file declares none.
- Commit after every task using the message given in that task's final step.

**Downstream note:** Task 1 is a breaking API change. Facet (`pkg/manifold`, `pkg/meshpreview`) and tapmag consume this module and will not compile against the new version until migrated. They break only when they bump their `go.mod`; migrating them is tracked as follow-on work in the spec, not in this plan.

---

### Task 1: Extract `Geometry` from `Mesh`

Splits the geometry concern out of `Mesh` so `Part` has something to hold that cannot carry color. Retires `meshio.go` in favour of `mesh.go` + `geometry.go`.

**Files:**
- Create: `geometry.go`
- Create: `mesh.go` (receives most of `meshio.go`)
- Delete: `meshio.go`
- Create: `geometry_test.go`
- Modify: `read_obj.go:71`, `read_stl.go:77`, `read_stl.go:114`
- Modify: `meshio_test.go:10`, `meshio_test.go:37`, `meshio_test.go:42`, `attachment_test.go:12`

**Interfaces:**
- Consumes: nothing (first task).
- Produces: `type Geometry struct { Vertices []float32; Indices []uint32 }` with method `func (g *Geometry) MergeVertices()`. `type Mesh struct { Geometry; FaceColors []FaceColor; Attachments []Attachment }` — embedding, so `m.Vertices`, `m.Indices`, and `m.MergeVertices()` all still resolve on a `*Mesh`.

- [ ] **Step 1: Write the failing test**

Create `geometry_test.go`:

```go
package meshio

import "testing"

func TestGeometryMergeVertices(t *testing.T) {
	g := &Geometry{
		Vertices: []float32{0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 0},
		Indices:  []uint32{0, 1, 2, 3, 1, 2},
	}
	g.MergeVertices()

	if len(g.Vertices) != 9 {
		t.Fatalf("expected 9 floats (3 verts) after merge, got %d", len(g.Vertices))
	}
	if g.Indices[3] != 0 {
		t.Errorf("duplicate vertex 3 should remap to 0, got %d", g.Indices[3])
	}
}

func TestMeshEmbedsGeometry(t *testing.T) {
	m := &Mesh{
		Geometry:   Geometry{Vertices: []float32{0, 0, 0, 1, 0, 0, 0, 1, 0}, Indices: []uint32{0, 1, 2}},
		FaceColors: []FaceColor{{Hex: "#FF0000"}},
	}
	// Promoted through embedding.
	if len(m.Vertices) != 9 {
		t.Fatalf("promoted Vertices: got %d floats", len(m.Vertices))
	}
	m.MergeVertices()
	if len(m.Indices) != 3 {
		t.Errorf("promoted MergeVertices: got %d indices", len(m.Indices))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run 'TestGeometryMergeVertices|TestMeshEmbedsGeometry' -v`
Expected: FAIL — compile error, `undefined: Geometry`.

- [ ] **Step 3: Create `geometry.go`**

```go
package meshio

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
```

- [ ] **Step 4: Create `mesh.go` with the rest of `meshio.go`**

Copy the entire contents of `meshio.go` into `mesh.go`, then make exactly two changes: remove the `MergeVertices` method (it now lives in `geometry.go`), and change the `Mesh` struct to embed `Geometry`:

```go
// Mesh holds triangle geometry, optional per-face display color, and any
// extra package parts. It is the interchange type: everything Read and
// Decode return.
type Mesh struct {
	Geometry                 // embedded: m.Vertices, m.Indices, m.MergeVertices()
	FaceColors  []FaceColor  // per-triangle display color (len = numTris, or nil)
	Attachments []Attachment // extra OPC parts (3MF only); nil for none
}
```

Everything else in the file — `FaceColor`, `Attachment`, `Encode`, `Decode`, `readers`, `Read`, `CanRead`, `ReadExtensions`, `ReadSTL`, `ReadOBJ`, `Read3MF`, `pathExt` — moves across unchanged.

- [ ] **Step 5: Delete `meshio.go`**

```bash
rm meshio.go
```

- [ ] **Step 6: Fix the four non-test composite literals**

`read_obj.go:71`:

```go
	return &Mesh{Geometry: Geometry{Vertices: vertices, Indices: indices}}, nil
```

`read_stl.go:77` and `read_stl.go:114` — both read `m := &Mesh{Vertices: vertices, Indices: indices}`, become:

```go
	m := &Mesh{Geometry: Geometry{Vertices: vertices, Indices: indices}}
```

`read_3mf.go:232` is `out := &Mesh{}` and needs no change; the appends at `read_3mf.go:246` and `read_3mf.go:249` resolve through embedding.

- [ ] **Step 7: Fix the four test composite literals**

`meshio_test.go:10` (`triangle`):

```go
func triangle() *Mesh {
	return &Mesh{
		Geometry: Geometry{
			Vertices: []float32{0, 0, 0, 1, 0, 0, 0, 1, 0},
			Indices:  []uint32{0, 1, 2},
		},
	}
}
```

`meshio_test.go:37` (end of `coloredCube`):

```go
	return &Mesh{Geometry: Geometry{Vertices: v, Indices: idx}, FaceColors: fc}
```

`meshio_test.go:42` (inside `TestMergeVertices`) — wrap its `Vertices`/`Indices` fields in `Geometry{...}` the same way.

`attachment_test.go:12` — wrap its `Vertices`/`Indices` fields in `Geometry{...}` the same way, leaving any `Attachments` field at the `Mesh` level.

- [ ] **Step 8: Run the full suite**

Run: `go test ./...`
Expected: PASS. If a composite literal was missed, the compiler names the file and line — fix and re-run.

- [ ] **Step 9: Commit**

```bash
git add geometry.go mesh.go geometry_test.go read_obj.go read_stl.go meshio_test.go attachment_test.go
git rm meshio.go
git commit -m "refactor: extract Geometry from Mesh

Mesh served two jobs, display and the basis for printing, so its single
per-face color field could not express filament assignment. Split geometry
into its own type so Part can hold it without carrying color."
```

---

### Task 2: Reorganize the 3MF files by concern

`Decode3MF` currently lives in `write_3mf.go` along with the zip helpers. Separate format, direction, and purpose into distinct files. No behavior changes — pure moves.

**Files:**
- Create: `opc_3mf.go`
- Modify: `write_3mf.go` (remove decode + OPC plumbing)
- Modify: `read_3mf.go` (receive `Decode3MF`)
- Delete: `rels_3mf.go` (contents fold into `opc_3mf.go`)

**Interfaces:**
- Consumes: `Geometry`, `Mesh` from Task 1.
- Produces: unexported helpers now living in `opc_3mf.go` — `addZipEntry(zw *zip.Writer, name, content string) error`, `addZipBytes(zw *zip.Writer, name string, content []byte) error`, `toReaderAt(r io.Reader) (io.ReaderAt, int64, error)`, `parseContentTypeOverrides(xmlText string) map[string]string`, `rootModelFromRels(relsXML string) string`, `findRootModelPart(zr *zip.Reader) string`. All keep their current signatures and bodies.

- [ ] **Step 1: Create `opc_3mf.go`**

Move these functions verbatim, with their doc comments:

- from `write_3mf.go`: `toReaderAt` (line 271), `addZipEntry` (line 289), `addZipBytes` (line 301)
- from `read_3mf.go`: `parseContentTypeOverrides` (line 14)
- from `rels_3mf.go`: `rootModelFromRels` (line 13), `findRootModelPart` (line 42)

File header:

```go
package meshio

import (
	"archive/zip"
	"encoding/xml"
	"io"
	"os"
	"strings"
)

// OPC (Open Packaging Conventions) plumbing shared by the 3MF reader and all
// 3MF writers: zip part I/O, [Content_Types].xml, and .rels relationships.
```

- [ ] **Step 2: Move `Decode3MF` into `read_3mf.go`**

Cut `Decode3MF` (currently `write_3mf.go:184-269`) and paste it into `read_3mf.go`, directly above the existing `parseModelPart`. Its body is unchanged.

- [ ] **Step 3: Delete `rels_3mf.go` and prune imports**

```bash
rm rels_3mf.go
```

Then fix the now-unused imports in `write_3mf.go` (it no longer needs `bytes`, `io`, or `os` if nothing else uses them) and `read_3mf.go` (it now needs `archive/zip`, `bytes`, `io`, `os`). Let the compiler drive this.

- [ ] **Step 4: Verify no behavior changed**

Run: `go test ./...`
Expected: PASS, with no test edits — this task moved code without changing it.

- [ ] **Step 5: Commit**

```bash
git add opc_3mf.go read_3mf.go write_3mf.go
git rm rels_3mf.go
git commit -m "refactor: split 3MF files by concern

Decode3MF lived in write_3mf.go alongside the zip helpers. Separate the
three tangled axes -- format, direction, and purpose -- into their own
files. No behavior change."
```

---

### Task 3: Delete the inert `Slic3r_PE_model.config` path

meshio emits `<metadata type="slic3r.extruder" value="#RRGGBBFF">`, but PrusaSlicer expects `<metadata type="volume" key="extruder" value="2">` — a slot index, not a hex color. Nothing reads what we write. Remove it rather than keep advertising support we do not have.

**Files:**
- Modify: `write_3mf.go` (remove `buildModelConfig` and its call site)
- Modify: `read_3mf.go` (remove the read-side filename skip)
- Modify: `meshio_test.go` (any assertion on that part)

**Interfaces:**
- Consumes: Task 2's file layout.
- Produces: no new API. `Encode3MF` output no longer contains `Metadata/Slic3r_PE_model.config`; `Decode3MF` no longer special-cases that filename and returns it as an ordinary `Attachment` when present.

- [ ] **Step 1: Write the failing test**

Add to `meshio_test.go`:

```go
func TestEncode3MF_NoSlic3rConfig(t *testing.T) {
	var buf bytes.Buffer
	if err := coloredCube().Encode3MF(&buf); err != nil {
		t.Fatalf("Encode3MF: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	for _, f := range zr.File {
		if f.Name == "Metadata/Slic3r_PE_model.config" {
			t.Fatal("Encode3MF still writes the inert Slic3r_PE_model.config part")
		}
	}
}

func TestDecode3MF_Slic3rConfigBecomesAttachment(t *testing.T) {
	data := make3MF(map[string]string{
		"[Content_Types].xml":              ctXML,
		"_rels/.rels":                      relsRoot,
		"3D/3dmodel.model":                 partWithMesh,
		"Metadata/Slic3r_PE_model.config":  "<config/>",
	})
	m, err := Decode3MF(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Decode3MF: %v", err)
	}
	for _, a := range m.Attachments {
		if a.Path == "Metadata/Slic3r_PE_model.config" {
			return
		}
	}
	t.Fatal("Slic3r_PE_model.config was dropped; expected it preserved as an Attachment")
}
```

`zip` must be in `meshio_test.go`'s imports; `make3MF`, `ctXML`, `relsRoot`, and `partWithMesh` already exist in `read_3mf_test.go` and are in the same package.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run 'TestEncode3MF_NoSlic3rConfig|TestDecode3MF_Slic3rConfigBecomesAttachment' -v`
Expected: both FAIL — the first finds the part, the second finds it dropped.

- [ ] **Step 3: Remove the writer**

In `write_3mf.go`, delete the entire `buildModelConfig` function (originally `write_3mf.go:319-361`) and its call site, which is this block near the end of `Encode3MF`:

```go
	if hasColors {
		modelConfig := buildModelConfig(numTris, faceColorIdx, palette)
		if modelConfig != "" {
			if err := addZipEntry(zw, "Metadata/Slic3r_PE_model.config", modelConfig); err != nil {
				return err
			}
		}
	}
```

`faceColorIdx` is still used to emit `pid`/`p1` on triangles, so leave it in place.

- [ ] **Step 4: Remove the reader's special case**

In `Decode3MF` (now in `read_3mf.go`), the attachment loop skips that filename. Change the condition from:

```go
		if strings.HasSuffix(name, ".model") ||
			name == "[Content_Types].xml" || strings.HasPrefix(name, "_rels/") ||
			strings.HasSuffix(name, "/.rels") || name == "Metadata/Slic3r_PE_model.config" {
			continue
		}
```

to:

```go
		if strings.HasSuffix(name, ".model") ||
			name == "[Content_Types].xml" || strings.HasPrefix(name, "_rels/") ||
			strings.HasSuffix(name, "/.rels") {
			continue
		}
```

- [ ] **Step 5: Run the full suite**

Run: `go test ./...`
Expected: PASS. If an older test asserted the config part exists, delete that assertion — the part is intentionally gone.

- [ ] **Step 6: Commit**

```bash
git add write_3mf.go read_3mf.go meshio_test.go
git commit -m "fix: remove inert Slic3r_PE_model.config output

It emitted type=slic3r.extruder with a hex value; PrusaSlicer expects
type=volume key=extruder with a slot index. Nothing read it. A copy found
in a file is now PrusaSlicer's own and is preserved as an Attachment."
```

---

### Task 4: `Object`, `Part`, and validation

The print-layer types. No serialization yet — this task is the data model and its rules.

**Files:**
- Create: `object.go`
- Create: `object_test.go`

**Interfaces:**
- Consumes: `Geometry` from Task 1.
- Produces:
  - `type Part struct { Name string; Geometry Geometry; Filament int }`
  - `type Object struct { Name string; Parts []Part; Filament int; SlotColors map[int]string; Attachments []Attachment }`
  - `func (o *Object) SetSlotColor(slot int, hex string)`
  - `func (o *Object) slot(p Part) int` — resolves `Part.Filament` → `Object.Filament` → `1`
  - `func (o *Object) validate() error`

- [ ] **Step 1: Write the failing test**

Create `object_test.go`:

```go
package meshio

import (
	"strings"
	"testing"
)

func unitTri() Geometry {
	return Geometry{
		Vertices: []float32{0, 0, 0, 1, 0, 0, 0, 1, 0},
		Indices:  []uint32{0, 1, 2},
	}
}

func TestSlotResolution(t *testing.T) {
	o := &Object{Filament: 3, Parts: []Part{
		{Name: "explicit", Geometry: unitTri(), Filament: 7},
		{Name: "inherits", Geometry: unitTri()},
	}}
	if got := o.slot(o.Parts[0]); got != 7 {
		t.Errorf("explicit slot: got %d, want 7", got)
	}
	if got := o.slot(o.Parts[1]); got != 3 {
		t.Errorf("inherited slot: got %d, want 3", got)
	}

	bare := &Object{Parts: []Part{{Name: "a", Geometry: unitTri()}}}
	if got := bare.slot(bare.Parts[0]); got != 1 {
		t.Errorf("default slot: got %d, want 1", got)
	}
}

func TestSetSlotColorAllocatesNilMap(t *testing.T) {
	o := &Object{}
	o.SetSlotColor(3, "#C81E1E") // must not panic on the nil map
	if o.SlotColors[3] != "#C81E1E" {
		t.Errorf("SlotColors[3] = %q, want #C81E1E", o.SlotColors[3])
	}
}

func TestObjectValidate(t *testing.T) {
	tests := []struct {
		name string
		obj  *Object
		want string // substring of the expected error; "" means valid
	}{
		{
			name: "valid",
			obj:  &Object{Parts: []Part{{Name: "a", Geometry: unitTri()}}},
			want: "",
		},
		{
			name: "no parts",
			obj:  &Object{},
			want: "no parts",
		},
		{
			name: "empty geometry",
			obj:  &Object{Parts: []Part{{Name: "hollow"}}},
			want: "empty geometry",
		},
		{
			name: "negative part slot",
			obj:  &Object{Parts: []Part{{Name: "a", Geometry: unitTri(), Filament: -1}}},
			want: "negative filament slot",
		},
		{
			name: "negative object slot",
			obj:  &Object{Filament: -2, Parts: []Part{{Name: "a", Geometry: unitTri()}}},
			want: "negative filament slot",
		},
		{
			name: "duplicate attachment",
			obj: &Object{
				Parts: []Part{{Name: "a", Geometry: unitTri()}},
				Attachments: []Attachment{
					{Path: "Metadata/x.json"},
					{Path: "Metadata/x.json"},
				},
			},
			want: "duplicate attachment",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.obj.validate()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("expected valid, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run 'TestSlotResolution|TestSetSlotColor|TestObjectValidate' -v`
Expected: FAIL — compile error, `undefined: Object`.

- [ ] **Step 3: Create `object.go`**

```go
package meshio

import "fmt"

// Part is one sub-mesh of an Object, bound to a filament slot.
//
// A Part holds Geometry rather than a Mesh: per-face color and package
// attachments are meaningless here. Print color is the filament slot, and the
// color for that slot lives once on the Object.
type Part struct {
	Name     string
	Geometry Geometry
	Filament int // 1-based slot; 0 = inherit Object.Filament
}

// Object is a printable object assembled from one or more parts. It is the
// print-layer counterpart to Mesh: where Mesh answers "what color do I draw
// this", Object answers "which filament prints this".
//
// SlotColors records display color once per filament slot. Two parts sharing a
// slot therefore cannot disagree about its color. It states design intent --
// "slot 3 is meant to read as red" -- and makes no claim about what filament is
// physically loaded.
type Object struct {
	Name        string
	Parts       []Part
	Filament    int            // default for parts with Filament == 0; 0 means slot 1
	SlotColors  map[int]string // slot number -> display color; absent = uncolored
	Attachments []Attachment
}

// SetSlotColor assigns a display color to a filament slot, allocating
// SlotColors if needed.
func (o *Object) SetSlotColor(slot int, hex string) {
	if o.SlotColors == nil {
		o.SlotColors = map[int]string{}
	}
	o.SlotColors[slot] = hex
}

// slot resolves the 1-based filament slot for a part:
// Part.Filament, else Object.Filament, else 1.
func (o *Object) slot(p Part) int {
	if p.Filament > 0 {
		return p.Filament
	}
	if o.Filament > 0 {
		return o.Filament
	}
	return 1
}

// validate reports the first structural problem with the object.
func (o *Object) validate() error {
	if len(o.Parts) == 0 {
		return fmt.Errorf("meshio: object has no parts")
	}
	if o.Filament < 0 {
		return fmt.Errorf("meshio: negative filament slot %d on object %q", o.Filament, o.Name)
	}
	for _, p := range o.Parts {
		if len(p.Geometry.Vertices) == 0 || len(p.Geometry.Indices) == 0 {
			return fmt.Errorf("meshio: part %q has empty geometry", p.Name)
		}
		if p.Filament < 0 {
			return fmt.Errorf("meshio: part %q has negative filament slot %d", p.Name, p.Filament)
		}
	}
	seen := map[string]bool{}
	for _, a := range o.Attachments {
		if seen[a.Path] {
			return fmt.Errorf("meshio: duplicate attachment path %q", a.Path)
		}
		seen[a.Path] = true
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -run 'TestSlotResolution|TestSetSlotColor|TestObjectValidate' -v`
Expected: PASS, all subtests.

- [ ] **Step 5: Commit**

```bash
git add object.go object_test.go
git commit -m "feat: add Object and Part print-layer types

Color is stored once per filament slot, so two parts sharing a slot cannot
declare conflicting colors."
```

---

### Task 5: Bambu writer — package structure and slot assignment

Emits the verified Bambu layout: a container object whose components are the parts, part geometry in one `3D/Objects/*.model`, and `Metadata/model_settings.config` carrying the extruder assignments. Color comes in Task 6.

**Files:**
- Create: `write_bambu_3mf.go`
- Create: `write_bambu_3mf_test.go`

**Interfaces:**
- Consumes: `Object`, `Part`, `o.slot(p)`, `o.validate()` from Task 4; `addZipEntry`, `addZipBytes` from Task 2.
- Produces:
  - `func (o *Object) EncodeBambu3MF(w io.Writer) error`
  - `func (o *Object) WriteBambu3MF(path string) error`
  - `func derivedUUID(label string, index int) string`
  - `func writeMeshXML(sb *strings.Builder, g Geometry, indent string, groupID int, colorAt func(tri int) int)` — lives in `write_3mf.go` and is shared by both 3MF writers. `colorAt` returns the palette index for a triangle, or `-1` to omit `pid`/`p1`/`p2`/`p3`. `Mesh.Encode3MF` passes a per-face lookup; the Bambu writer passes a constant, because a part is one slot.

Part object ids are `1..N`; the container is `N+1`; the part file is `3D/Objects/object_<N+1>.model`.

- [ ] **Step 1: Write the failing test**

Create `write_bambu_3mf_test.go`:

```go
package meshio

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
)

// twoPartObject: parts on slots 1 and 2, ids 1 and 2, container id 3.
func twoPartObject() *Object {
	return &Object{
		Name: "plate",
		Parts: []Part{
			{Name: "base", Geometry: unitTri(), Filament: 1},
			{Name: "tiles", Geometry: unitTri(), Filament: 2},
		},
	}
}

func TestBambu_ComponentIDsMatchPartIDs(t *testing.T) {
	var buf bytes.Buffer
	if err := twoPartObject().EncodeBambu3MF(&buf); err != nil {
		t.Fatalf("EncodeBambu3MF: %v", err)
	}
	root := readZipPart(t, buf.Bytes(), "3D/3dmodel.model")
	settings := readZipPart(t, buf.Bytes(), "Metadata/model_settings.config")

	compIDs := allMatches(`objectid="(\d+)"`, root)
	partIDs := allMatches(`<part id="(\d+)"`, settings)

	// Drop the build item's objectid (the container) from the component list.
	var comps []string
	for _, id := range compIDs {
		if id != "3" {
			comps = append(comps, id)
		}
	}
	if len(comps) != 2 || len(partIDs) != 2 {
		t.Fatalf("expected 2 components and 2 parts, got %d and %d", len(comps), len(partIDs))
	}
	for i := range comps {
		if comps[i] != partIDs[i] {
			t.Errorf("component objectid %s has no matching part id (%s)", comps[i], partIDs[i])
		}
	}
}

func TestBambu_ExtruderAssignment(t *testing.T) {
	o := &Object{
		Filament: 4,
		Parts: []Part{
			{Name: "inherits", Geometry: unitTri()},
			{Name: "explicit", Geometry: unitTri(), Filament: 7},
		},
	}
	var buf bytes.Buffer
	if err := o.EncodeBambu3MF(&buf); err != nil {
		t.Fatalf("EncodeBambu3MF: %v", err)
	}
	settings := readZipPart(t, buf.Bytes(), "Metadata/model_settings.config")

	got := allMatches(`<metadata key="extruder" value="(\d+)"`, settings)
	// object default, then part 1 (inherited 4), then part 2 (explicit 7)
	want := []string{"4", "4", "7"}
	if len(got) != len(want) {
		t.Fatalf("extruder values: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("extruder[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestBambu_PackageStructure(t *testing.T) {
	var buf bytes.Buffer
	if err := twoPartObject().EncodeBambu3MF(&buf); err != nil {
		t.Fatalf("EncodeBambu3MF: %v", err)
	}

	rels := readZipPart(t, buf.Bytes(), "3D/_rels/3dmodel.model.rels")
	if !strings.Contains(rels, `Target="/3D/Objects/object_3.model"`) {
		t.Errorf("object rels missing part-file target:\n%s", rels)
	}
	if !strings.Contains(rels, "http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel") {
		t.Errorf("object rels missing 3dmodel relationship type:\n%s", rels)
	}

	// The verified Bambu file declares no content type for .config parts.
	ct := readZipPart(t, buf.Bytes(), "[Content_Types].xml")
	if strings.Contains(ct, "config") {
		t.Errorf("[Content_Types].xml must not declare a .config type:\n%s", ct)
	}

	root := readZipPart(t, buf.Bytes(), "3D/3dmodel.model")
	if !strings.Contains(root, `requiredextensions="p"`) {
		t.Error("root model missing requiredextensions=\"p\"")
	}

	// Part geometry lives in the object file, not the root.
	objects := readZipPart(t, buf.Bytes(), "3D/Objects/object_3.model")
	if strings.Count(objects, "<mesh>") != 2 {
		t.Errorf("expected 2 meshes in the object file, got %d", strings.Count(objects, "<mesh>"))
	}
}

func TestBambu_Deterministic(t *testing.T) {
	var a, b bytes.Buffer
	if err := twoPartObject().EncodeBambu3MF(&a); err != nil {
		t.Fatalf("first encode: %v", err)
	}
	if err := twoPartObject().EncodeBambu3MF(&b); err != nil {
		t.Fatalf("second encode: %v", err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("encoding twice produced different bytes; output must be deterministic")
	}
}

func TestBambu_GeometryRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := twoPartObject().EncodeBambu3MF(&buf); err != nil {
		t.Fatalf("EncodeBambu3MF: %v", err)
	}
	m, err := Decode3MF(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode3MF of our own output: %v", err)
	}
	// Two unit triangles: 6 vertices before merge, 6 indices.
	if len(m.Indices) != 6 {
		t.Errorf("round-trip indices: got %d, want 6", len(m.Indices))
	}
}

func TestBambu_ValidationPropagates(t *testing.T) {
	var buf bytes.Buffer
	err := (&Object{}).EncodeBambu3MF(&buf)
	if err == nil {
		t.Fatal("expected an error encoding an object with no parts")
	}
}

func allMatches(pattern, s string) []string {
	re := regexp.MustCompile(pattern)
	var out []string
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run TestBambu -v`
Expected: FAIL — compile error, `o.EncodeBambu3MF undefined`.

- [ ] **Step 3: Create `write_bambu_3mf.go`**

```go
package meshio

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	nsCore         = "http://schemas.microsoft.com/3dmanufacturing/core/2015/02"
	nsProduction   = "http://schemas.microsoft.com/3dmanufacturing/production/2015/06"
	nsBambu        = "http://schemas.bambulab.com/package/2021"
	relType3DModel = "http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel"
	identityXform  = "1 0 0 0 1 0 0 0 1 0 0 0"
	colorGroupID   = 100
)

// EncodeBambu3MF writes the object as a Bambu Studio 3MF: a container object
// whose components are the parts, part geometry in 3D/Objects, and filament
// slot assignments in Metadata/model_settings.config.
//
// Unlike Mesh.Encode3MF, which carries display color only, this produces a file
// that slices multi-material: each part is bound to a filament slot.
func (o *Object) EncodeBambu3MF(w io.Writer) error {
	if err := o.validate(); err != nil {
		return err
	}

	containerID := len(o.Parts) + 1
	objectsPath := fmt.Sprintf("3D/Objects/object_%d.model", containerID)

	// --- 3D/3dmodel.model: container object + build item ---
	var root strings.Builder
	root.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	fmt.Fprintf(&root, `<model unit="millimeter" xml:lang="en-US" xmlns="%s" xmlns:p="%s" xmlns:BambuStudio="%s" requiredextensions="p">`+"\n",
		nsCore, nsProduction, nsBambu)
	root.WriteString(` <metadata name="Application">meshio</metadata>` + "\n")
	root.WriteString(` <metadata name="BambuStudio:3mfVersion">1</metadata>` + "\n")
	root.WriteString(" <resources>\n")
	fmt.Fprintf(&root, "  <object id=\"%d\" p:UUID=\"%s\" type=\"model\">\n", containerID, derivedUUID("object", containerID))
	root.WriteString("   <components>\n")
	for i := range o.Parts {
		partID := i + 1
		fmt.Fprintf(&root, "    <component p:path=\"/%s\" objectid=\"%d\" p:UUID=\"%s\" transform=\"%s\"/>\n",
			objectsPath, partID, derivedUUID("component", partID), identityXform)
	}
	root.WriteString("   </components>\n")
	root.WriteString("  </object>\n")
	root.WriteString(" </resources>\n")
	fmt.Fprintf(&root, " <build p:UUID=\"%s\">\n", derivedUUID("build", 0))
	fmt.Fprintf(&root, "  <item objectid=\"%d\" p:UUID=\"%s\" transform=\"%s\" printable=\"1\"/>\n",
		containerID, derivedUUID("item", containerID), identityXform)
	root.WriteString(" </build>\n")
	root.WriteString("</model>\n")

	// --- 3D/Objects/object_N.model: one <object> per part ---
	var objects strings.Builder
	objects.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	fmt.Fprintf(&objects, `<model unit="millimeter" xml:lang="en-US" xmlns="%s" xmlns:p="%s" requiredextensions="p">`+"\n",
		nsCore, nsProduction)
	objects.WriteString(" <resources>\n")
	for i, p := range o.Parts {
		partID := i + 1
		fmt.Fprintf(&objects, "  <object id=\"%d\" p:UUID=\"%s\" type=\"model\">\n", partID, derivedUUID("partobject", partID))
		// Uncolored for now; Task 6 supplies the palette index for this part's slot.
		writeMeshXML(&objects, p.Geometry, "   ", colorGroupID, func(int) int { return -1 })
		objects.WriteString("  </object>\n")
	}
	objects.WriteString(" </resources>\n")
	objects.WriteString(" <build/>\n")
	objects.WriteString("</model>\n")

	// --- Metadata/model_settings.config ---
	settings := o.modelSettings(containerID)

	// --- OPC scaffolding ---
	var ct strings.Builder
	ct.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	ct.WriteString(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` + "\n")
	ct.WriteString(` <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml" />` + "\n")
	ct.WriteString(` <Default Extension="model" ContentType="application/vnd.ms-package.3dmanufacturing-3dmodel+xml" />` + "\n")
	for _, att := range o.Attachments {
		fmt.Fprintf(&ct, ` <Override PartName="/%s" ContentType="%s" />`+"\n", att.Path, att.ContentType)
	}
	ct.WriteString("</Types>\n")

	rootRels := `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + "\n" +
		` <Relationship Target="/3D/3dmodel.model" Id="rel0" Type="` + relType3DModel + `" />` + "\n" +
		"</Relationships>\n"

	objectRels := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>`+"\n"+
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+"\n"+
		` <Relationship Target="/%s" Id="rel-1" Type="%s" />`+"\n"+
		"</Relationships>\n", objectsPath, relType3DModel)

	zw := zip.NewWriter(w)
	entries := []struct{ name, content string }{
		{"[Content_Types].xml", ct.String()},
		{"_rels/.rels", rootRels},
		{"3D/3dmodel.model", root.String()},
		{"3D/_rels/3dmodel.model.rels", objectRels},
		{objectsPath, objects.String()},
		{"Metadata/model_settings.config", settings},
	}
	for _, e := range entries {
		if err := addZipEntry(zw, e.name, e.content); err != nil {
			return err
		}
	}
	for _, att := range o.Attachments {
		if err := addZipBytes(zw, att.Path, att.Data); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("meshio: closing zip: %w", err)
	}
	return nil
}

// WriteBambu3MF exports the object to a Bambu Studio 3MF file at path.
func (o *Object) WriteBambu3MF(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("meshio: %w", err)
	}
	defer f.Close()
	return o.EncodeBambu3MF(f)
}

// modelSettings builds Metadata/model_settings.config: the object default
// extruder plus one <part> per part, each with its resolved slot.
func (o *Object) modelSettings(containerID int) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	sb.WriteString("<config>\n")
	fmt.Fprintf(&sb, "  <object id=\"%d\">\n", containerID)
	if o.Name != "" {
		fmt.Fprintf(&sb, "    <metadata key=\"name\" value=\"%s\"/>\n", o.Name)
	}
	objDefault := o.Filament
	if objDefault <= 0 {
		objDefault = 1
	}
	fmt.Fprintf(&sb, "    <metadata key=\"extruder\" value=\"%d\"/>\n", objDefault)
	for i, p := range o.Parts {
		partID := i + 1
		fmt.Fprintf(&sb, "    <part id=\"%d\" subtype=\"normal_part\">\n", partID)
		if p.Name != "" {
			fmt.Fprintf(&sb, "      <metadata key=\"name\" value=\"%s\"/>\n", p.Name)
		}
		fmt.Fprintf(&sb, "      <metadata key=\"matrix\" value=\"1 0 0 0 0 1 0 0 0 0 1 0 0 0 0 1\"/>\n")
		fmt.Fprintf(&sb, "      <metadata key=\"extruder\" value=\"%d\"/>\n", o.slot(p))
		sb.WriteString("      <mesh_stat edges_fixed=\"0\" degenerate_facets=\"0\" facets_removed=\"0\" facets_reversed=\"0\" backwards_edges=\"0\"/>\n")
		sb.WriteString("    </part>\n")
	}
	sb.WriteString("  </object>\n")
	sb.WriteString("</config>\n")
	return sb.String()
}

// derivedUUID returns a deterministic RFC-4122-shaped UUID for a labelled
// element. Deterministic rather than random so that encoding the same object
// twice yields byte-identical output.
func derivedUUID(label string, index int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s/%d", label, index)))
	b := sum[:16]
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	s := hex.EncodeToString(b)
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:32]
}
```

- [ ] **Step 4: Extract the shared mesh serializer into `write_3mf.go`**

Both 3MF writers serialize a `<mesh>`; they differ only in how a triangle picks its palette index — `Mesh.Encode3MF` looks it up per face, the Bambu writer uses one index for the whole part. A `colorAt` callback covers both. Add to `write_3mf.go`:

```go
// writeMeshXML serializes geometry as a 3MF <mesh> element, shared by all 3MF
// writers. colorAt returns the palette index for a triangle, or -1 to omit the
// pid/p1/p2/p3 color references.
func writeMeshXML(sb *strings.Builder, g Geometry, indent string, groupID int, colorAt func(tri int) int) {
	numVerts := len(g.Vertices) / 3
	numTris := len(g.Indices) / 3

	sb.WriteString(indent + "<mesh>\n")
	sb.WriteString(indent + " <vertices>\n")
	for i := 0; i < numVerts; i++ {
		fmt.Fprintf(sb, indent+"  <vertex x=\"%g\" y=\"%g\" z=\"%g\" />\n",
			g.Vertices[i*3], g.Vertices[i*3+1], g.Vertices[i*3+2])
	}
	sb.WriteString(indent + " </vertices>\n")
	sb.WriteString(indent + " <triangles>\n")
	for i := 0; i < numTris; i++ {
		v1, v2, v3 := g.Indices[i*3], g.Indices[i*3+1], g.Indices[i*3+2]
		if ci := colorAt(i); ci >= 0 {
			fmt.Fprintf(sb, indent+"  <triangle v1=\"%d\" v2=\"%d\" v3=\"%d\" pid=\"%d\" p1=\"%d\" p2=\"%d\" p3=\"%d\" />\n",
				v1, v2, v3, groupID, ci, ci, ci)
		} else {
			fmt.Fprintf(sb, indent+"  <triangle v1=\"%d\" v2=\"%d\" v3=\"%d\" />\n", v1, v2, v3)
		}
	}
	sb.WriteString(indent + " </triangles>\n")
	sb.WriteString(indent + "</mesh>\n")
}
```

- [ ] **Step 5: Rewrite `Encode3MF`'s mesh block to use it**

In `write_3mf.go`, `Encode3MF` currently inlines its own `<vertices>`/`<triangles>` loops (originally `write_3mf.go:82-104`). Replace that whole block with a single call. `hasColors` and `faceColorIdx` are already in scope from the palette-building code above it:

```go
	writeMeshXML(&sb, m.Geometry, "   ", colorGroupID, func(tri int) int {
		if !hasColors {
			return -1
		}
		return faceColorIdx[tri]
	})
```

`faceColorIdx[tri]` is already `-1` for uncolored faces, so the callback needs no extra branch. Delete the now-unused inline loops. The local `colorGroupID := 100` declaration in `Encode3MF` must also be deleted, since Task 5 introduced it as a package constant.

- [ ] **Step 6: Verify the core writer is unchanged in behavior**

Run: `go test -run 'TestThreeMF|TestEncode3MF' -v`
Expected: PASS. These tests predate this plan and assert the core 3MF output; extracting the serializer must not alter a byte.

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test -run TestBambu -v`
Expected: PASS, all seven tests.

- [ ] **Step 8: Run the full suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add write_bambu_3mf.go write_bambu_3mf_test.go write_3mf.go
git commit -m "feat: add Bambu Studio 3MF writer

Emits the container/component structure and model_settings.config filament
slot assignments verified against real Bambu project files. Mesh XML
serialization is extracted and shared with the core 3MF writer."
```

---

### Task 6: Derive the colorgroup from `SlotColors`

3MF carries color per triangle; `Object` stores it per slot. The writer denormalizes: one palette entry per distinct color, every triangle of a part referencing its slot's entry.

**Files:**
- Modify: `object.go` (add `palette`)
- Modify: `write_bambu_3mf.go` (declare the `m:` namespace when a palette exists)
- Modify: `write_bambu_3mf_test.go` (add derivation tests)

**Interfaces:**
- Consumes: `Object.SlotColors`, `o.slot(p)`, `writeMeshXML`, `normalizeHex` (existing, `write_3mf.go`).
- Produces: `func (o *Object) palette() ([]string, map[int]int)` — ordered distinct normalized colors, plus slot→palette-index. Slots are visited in ascending numeric order so output is deterministic.

- [ ] **Step 1: Write the failing test**

Append to `write_bambu_3mf_test.go`:

```go
func TestBambu_PaletteDedupsByColor(t *testing.T) {
	o := &Object{
		Parts: []Part{
			{Name: "a", Geometry: unitTri(), Filament: 1},
			{Name: "b", Geometry: unitTri(), Filament: 2},
			{Name: "c", Geometry: unitTri(), Filament: 3},
		},
	}
	o.SetSlotColor(1, "#FF0000")
	o.SetSlotColor(2, "#FF0000") // same color, different slot -> one palette entry
	o.SetSlotColor(3, "#0000FF")

	palette, bySlot := o.palette()
	if len(palette) != 2 {
		t.Fatalf("palette: got %d entries %v, want 2 (dedup by color)", len(palette), palette)
	}
	if bySlot[1] != bySlot[2] {
		t.Errorf("slots 1 and 2 share a color but map to %d and %d", bySlot[1], bySlot[2])
	}
	if bySlot[3] == bySlot[1] {
		t.Error("slot 3 has a distinct color but shares a palette index with slot 1")
	}
}

func TestBambu_TrianglesReferenceSlotColor(t *testing.T) {
	o := twoPartObject()
	o.SetSlotColor(1, "#FF0000")
	o.SetSlotColor(2, "#0000FF")

	var buf bytes.Buffer
	if err := o.EncodeBambu3MF(&buf); err != nil {
		t.Fatalf("EncodeBambu3MF: %v", err)
	}
	objects := readZipPart(t, buf.Bytes(), "3D/Objects/object_3.model")

	if !strings.Contains(objects, `<m:color color="#FF0000FF" />`) {
		t.Errorf("missing normalized slot-1 color in colorgroup:\n%s", objects)
	}
	if !strings.Contains(objects, `xmlns:m=`) {
		t.Error("material extension namespace not declared despite a palette")
	}
	refs := allMatches(`p1="(\d+)"`, objects)
	if len(refs) != 2 {
		t.Fatalf("expected 2 colored triangles, got %d", len(refs))
	}
	if refs[0] == refs[1] {
		t.Error("parts on different slots must reference different palette entries")
	}
}

func TestBambu_NoColorsOmitsColorgroup(t *testing.T) {
	var buf bytes.Buffer
	if err := twoPartObject().EncodeBambu3MF(&buf); err != nil {
		t.Fatalf("EncodeBambu3MF: %v", err)
	}
	objects := readZipPart(t, buf.Bytes(), "3D/Objects/object_3.model")
	if strings.Contains(objects, "colorgroup") {
		t.Errorf("no SlotColors set, but a colorgroup was written:\n%s", objects)
	}
	if strings.Contains(objects, "pid=") {
		t.Error("no SlotColors set, but triangles carry pid references")
	}
}

func TestBambu_UnmappedSlotIsUncolored(t *testing.T) {
	o := twoPartObject()
	o.SetSlotColor(1, "#FF0000") // slot 2 deliberately left unset

	var buf bytes.Buffer
	if err := o.EncodeBambu3MF(&buf); err != nil {
		t.Fatalf("EncodeBambu3MF: %v", err)
	}
	objects := readZipPart(t, buf.Bytes(), "3D/Objects/object_3.model")

	if len(allMatches(`p1="(\d+)"`, objects)) != 1 {
		t.Error("expected exactly one colored triangle; the unmapped slot must be uncolored, not an error")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run 'TestBambu_Palette|TestBambu_Triangles|TestBambu_NoColors|TestBambu_Unmapped' -v`
Expected: FAIL — compile error, `o.palette undefined`.

- [ ] **Step 3: Implement `palette` in `object.go`**

Add to `object.go`, with `"sort"` added to the file's imports:

```go
// palette returns the ordered distinct display colors and a map from filament
// slot to palette index.
//
// 3MF carries color per triangle while Object stores it per slot, so this is
// the denormalization step. Slots are visited in ascending order and colors
// deduped by value, making the result deterministic: encoding the same object
// twice produces identical bytes.
func (o *Object) palette() ([]string, map[int]int) {
	slots := make([]int, 0, len(o.SlotColors))
	for s := range o.SlotColors {
		slots = append(slots, s)
	}
	sort.Ints(slots)

	var palette []string
	idxByColor := map[string]int{}
	idxBySlot := map[int]int{}

	for _, s := range slots {
		raw := o.SlotColors[s]
		if raw == "" {
			continue
		}
		normalized := normalizeHex(raw)
		idx, ok := idxByColor[normalized]
		if !ok {
			idx = len(palette)
			palette = append(palette, normalized)
			idxByColor[normalized] = idx
		}
		idxBySlot[s] = idx
	}
	return palette, idxBySlot
}
```

- [ ] **Step 4: Wire the palette into the Bambu writer**

In `write_bambu_3mf.go`, three changes inside `EncodeBambu3MF`.

First, derive the palette immediately after `objectsPath` is computed:

```go
	palette, paletteBySlot := o.palette()
```

Second, declare the material namespace only when a palette exists. Replace the fixed objects-model header with:

```go
	objects.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	materialNS := ""
	if len(palette) > 0 {
		materialNS = ` xmlns:m="http://schemas.microsoft.com/3dmanufacturing/material/2015/02"`
	}
	fmt.Fprintf(&objects, `<model unit="millimeter" xml:lang="en-US" xmlns="%s" xmlns:p="%s"%s requiredextensions="p">`+"\n",
		nsCore, nsProduction, materialNS)
```

Third, emit the colorgroup and give each part its slot's palette index. Replace the `<resources>` block written in Task 5:

```go
	objects.WriteString(" <resources>\n")
	if len(palette) > 0 {
		fmt.Fprintf(&objects, "  <m:colorgroup id=\"%d\">\n", colorGroupID)
		for _, hexColor := range palette {
			fmt.Fprintf(&objects, "   <m:color color=\"%s\" />\n", hexColor)
		}
		objects.WriteString("  </m:colorgroup>\n")
	}
	for i, p := range o.Parts {
		partID := i + 1
		colorIdx := -1
		if idx, ok := paletteBySlot[o.slot(p)]; ok {
			colorIdx = idx
		}
		fmt.Fprintf(&objects, "  <object id=\"%d\" p:UUID=\"%s\" type=\"model\">\n", partID, derivedUUID("partobject", partID))
		// A part is one slot, so every triangle takes the same palette index.
		writeMeshXML(&objects, p.Geometry, "   ", colorGroupID, func(int) int { return colorIdx })
		objects.WriteString("  </object>\n")
	}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test -run TestBambu -v`
Expected: PASS, all eleven tests.

- [ ] **Step 6: Run the full suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add object.go write_bambu_3mf.go write_bambu_3mf_test.go
git commit -m "feat: derive 3MF colorgroup from Object.SlotColors

Color is stored once per filament slot and denormalized into per-triangle
references at write time, so the redundant form exists only in the file."
```

---

### Task 7: Update the README

The README currently documents multi-part as not implemented and the Slic3r config as inert-but-present. Both are now false.

**Files:**
- Modify: `README.md`

**Interfaces:**
- Consumes: the complete public API from Tasks 1–6.
- Produces: no code.

- [ ] **Step 1: Update the status table**

Replace the existing status table rows with:

```markdown
| Capability | State |
|---|---|
| Per-face RGB → core colorgroup | works |
| Read production-extension / multi-part 3MF | works (geometry flattened to one mesh) |
| Multi-part object with per-part filament slot | works — `Object.EncodeBambu3MF` |
| Per-triangle paint (`paint_color` / `mmu_segmentation`) | not implemented, encoding unverified |
| PrusaSlicer multi-material output | not implemented, needs a verified sample |
```

- [ ] **Step 2: Replace the "Practical guidance today" section**

```markdown
## Practical guidance today

- **Want color in a viewer, or interchange between tools?** `Mesh.FaceColors` +
  `Encode3MF` is correct and sufficient.
- **Want a multi-color print?** Build an `Object` whose parts are separate
  solids, assign each a filament slot, and use `WriteBambu3MF`:

```go
obj := &meshio.Object{Name: "plate", Filament: 1}
obj.SetSlotColor(1, "#000000")
obj.SetSlotColor(2, "#C81E1E")
obj.Parts = []meshio.Part{
    {Name: "base", Geometry: baseSlab},               // inherits slot 1
    {Name: "red",  Geometry: redTiles, Filament: 2},
}
err := obj.WriteBambu3MF("plate.3mf")
```

  Each part must be a genuine solid — see the geometry gotcha above. Splitting a
  colored slab by face color yields zero-thickness patches that will not slice.
- **PrusaSlicer** has no multi-material output yet.
- **Do not** assume a correct-looking preview means a correct toolpath. Verify by
  slicing and checking the filament-change count.
```

- [ ] **Step 3: Document the two color channels in the type list**

In the intro where `Mesh` is described, replace the struct block with:

```markdown
`Geometry` is triangles; `Mesh` adds display color and package parts; `Object`
is the print type:

```go
type Geometry struct {
    Vertices []float32 // xyz triples, len = numVerts*3
    Indices  []uint32  // triangle indices, len = numTris*3
}

type Mesh struct {
    Geometry                 // display/interchange: "draw this red"
    FaceColors  []FaceColor  // len = numTris, or nil
    Attachments []Attachment
}

type Object struct {          // printing: "print this with slot 3"
    Parts      []Part
    SlotColors map[int]string // slot number → display color
    // ...
}
```
```

- [ ] **Step 4: Remove the stale Slic3r paragraph**

Delete the paragraph beginning "There is also a `Metadata/Slic3r_PE_model.config` part written alongside." — it is no longer written. Keep the description of PrusaSlicer's expected format under "What the slicers actually consume", since it stays accurate and unverified.

- [ ] **Step 5: Verify the examples compile**

Run: `go vet ./...`
Expected: no output. Then re-read the README code blocks against `object.go` and confirm every type and method name matches — `SetSlotColor`, `WriteBambu3MF`, `Part.Geometry`, `Part.Filament`.

- [ ] **Step 6: Commit**

```bash
git add README.md
git commit -m "docs: document Object, Geometry, and multi-part printing"
```

---

## Acceptance gate

**Required, and cannot be automated.** Green tests are not sufficient evidence for this change — the bug being fixed is precisely "passes inspection, slices wrong."

- [ ] Generate a two-color object with `WriteBambu3MF`.
- [ ] Open it in Bambu Studio.
- [ ] Confirm the parts appear as **one object** with **distinct filament assignments**.
- [ ] Slice it and confirm the preview shows **actual filament changes**.
- [ ] Confirm the emitted colorgroup does not visually conflict with Bambu's own filament-based part coloring. The verified reference file carries no colorgroup, so this interaction is unverified; it affects display only, never the toolpath.

Do not report this work as complete until the slice preview has been seen.
