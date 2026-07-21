# meshio Multi-Part Color Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let meshio express "this object is N sub-meshes, each printed with filament slot S" and write it in the dialect Bambu Studio actually consumes, so a generated 3MF slices multi-color instead of only *looking* multi-color. Along the way, split the library into per-format packages.

**Architecture:** `Mesh` served two jobs — display/interchange and the basis for printing — and a single per-face color field can only express the first. Geometry becomes its own type; `Mesh` stays the display type; `Object`/`Part` become the print type with color stored once per filament slot and denormalized into per-triangle references at write time. The library splits into `meshio` (dispatch), `meshio/geom` (shared types), and `meshio/stl`, `meshio/obj`, `meshio/threemf` (format I/O).

**Tech Stack:** Go 1.25.0, standard library only. Module `github.com/firstlayer-xyz/meshio`. No third-party dependencies — do not add any.

**Spec:** `docs/superpowers/specs/2026-07-21-meshio-multipart-color-design.md`

## Global Constraints

- **Zero dependencies.** Standard library only. Do not add anything to `go.mod`.
- **Deterministic output.** Encoding the same input twice must produce byte-identical bytes. No `time.Now()`, no `math/rand`, no map iteration without sorting keys first.
- **Go 1.25.0**, module path `github.com/firstlayer-xyz/meshio`.
- **Test command:** `go test ./...` from the repo root. Single test: `go test ./PKG/ -run TestName -v`.
- **Filament slots are 1-based** — matching the Bambu XML, the AMS UI, and `Object.Filament`. Slot `0` means "unset, inherit".
- **Hex colors** are `"#RRGGBB"` or `"#RRGGBBAA"`. `normalizeHex` pads 7-char values with `FF`.
- **Do not** write `Metadata/project_settings.config`, and **do not** declare a content type for `.config` parts — the verified Bambu file declares none.
- **Import direction is one-way:** `meshio → {stl, obj, threemf} → geom`. Nothing under `geom` imports a sibling; `geom` imports no local package. A cycle means the design was violated.
- Commit after every task using the message given in that task's final step.

**API break:** Go forbids methods on types from another package, so once `Mesh` lives in `geom`, method-style encoding cannot exist anywhere. Encoding becomes package functions — `stl.Encode(w, m)` rather than `m.EncodeSTL(w)` — mirroring `png.Encode`/`png.Decode`. This is intended and approved.

**Downstream note:** Facet (`pkg/manifold`, `pkg/meshpreview`) and tapmag consume this module and will not compile against the new version until migrated. They break only when they bump their `go.mod`; migrating them is follow-on work, not part of this plan.

## Status

- **Task 1 — complete** (commit `e3ba347`). `Geometry` extracted from `Mesh`; `Mesh` embeds it.
- **Task 2 — complete** (commit `d613e3e`). 3MF files split by concern; `Decode3MF` moved out of `write_3mf.go`; `opc_3mf.go` created; `rels_3mf.go` removed.

Tasks 3 onward are outstanding.

---

### Task 3: Delete the inert `Slic3r_PE_model.config` path

meshio emits `<metadata type="slic3r.extruder" value="#RRGGBBFF">`, but PrusaSlicer expects `<metadata type="volume" key="extruder" value="2">` — a slot index, not a hex color. Nothing reads what we write. Remove it before the package move, so there is less code to relocate.

**Files:**
- Modify: `write_3mf.go` (remove `buildModelConfig` and its call site)
- Modify: `read_3mf.go` (remove the read-side filename skip in `Decode3MF`)
- Modify: `meshio_test.go`

**Interfaces:**
- Consumes: current root-package layout.
- Produces: no new API. `Encode3MF` output no longer contains `Metadata/Slic3r_PE_model.config`; `Decode3MF` no longer special-cases that filename and returns it as an ordinary `Attachment`.

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
		"[Content_Types].xml":             ctXML,
		"_rels/.rels":                     relsRoot,
		"3D/3dmodel.model":                partWithMesh,
		"Metadata/Slic3r_PE_model.config": "<config/>",
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

`archive/zip` must be in `meshio_test.go`'s imports. `make3MF`, `ctXML`, `relsRoot`, and `partWithMesh` already exist in `read_3mf_test.go` in the same package.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run 'TestEncode3MF_NoSlic3rConfig|TestDecode3MF_Slic3rConfigBecomesAttachment' -v`
Expected: both FAIL — the first finds the part present, the second finds it dropped.

- [ ] **Step 3: Remove the writer**

In `write_3mf.go`, delete the entire `buildModelConfig` function and this call site near the end of `Encode3MF`:

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

`faceColorIdx` is still used to emit `pid`/`p1` on triangles — leave it in place.

- [ ] **Step 4: Remove the reader's special case**

In `Decode3MF` (in `read_3mf.go`), change the attachment-skip condition from:

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

### Task 4: Create the `geom` package

Move the shared types to a leaf package so the format packages can depend on them without a cycle. The root package keeps working via type aliases.

**Files:**
- Create: `geom/geometry.go`, `geom/mesh.go`, `geom/geometry_test.go`
- Delete: `geometry.go`, `geometry_test.go`
- Modify: `mesh.go` (becomes aliases + dispatch), `read_stl.go`, `read_obj.go`, `read_3mf.go`, `write_stl.go`, `write_obj.go`, `write_3mf.go`, `opc_3mf.go`, and all `*_test.go` in root

**Interfaces:**
- Consumes: Task 3's cleaned-up root package.
- Produces: package `geom` at `github.com/firstlayer-xyz/meshio/geom` exporting `Geometry` (with `MergeVertices`), `Mesh`, `FaceColor`, `Attachment`. Root re-exports all four as type aliases, so `meshio.Mesh` continues to resolve to the same type. Root's seven encoding methods become package functions:

```go
func Encode(w io.Writer, m *Mesh, format string) error
func EncodeSTL(w io.Writer, m *Mesh) error
func WriteSTL(path string, m *Mesh) error
func EncodeOBJ(w io.Writer, m *Mesh, mtlW io.Writer) error
func WriteOBJ(path string, m *Mesh) error
func Encode3MF(w io.Writer, m *Mesh) error
func Write3MF(path string, m *Mesh) error
```

  Parameter order matches the eventual per-package form (`stl.Encode(w, m)`), so Tasks 5-7 are pure relocation with no signature churn.

- [ ] **Step 1: Create `geom/geometry.go`**

Move `geometry.go` verbatim, changing only the package clause:

```go
// Package geom holds the shared mesh types: geometry, per-face display color,
// and package attachments. It is a leaf package -- it imports no other package
// in this module, so every format package can depend on it without a cycle.
package geom
```

The `Geometry` struct and its `MergeVertices` method move unchanged.

- [ ] **Step 2: Create `geom/mesh.go`**

Move `FaceColor`, `Attachment`, and `Mesh` out of root's `mesh.go` verbatim, under `package geom`. Their doc comments come with them. Do **not** move `Encode`, `Decode`, `Read`, `CanRead`, `ReadExtensions`, `ReadSTL`, `ReadOBJ`, `Read3MF`, or `pathExt` — those stay in root.

- [ ] **Step 3: Move the geometry test**

Move `geometry_test.go` to `geom/geometry_test.go`, changing the package clause to `package geom`. `TestMeshEmbedsGeometry` moves with it and needs no edit — `Mesh` is local to `geom` now.

- [ ] **Step 4: Replace root's type declarations with aliases**

In root's `mesh.go`, delete the `Mesh`, `FaceColor`, and `Attachment` declarations and add:

```go
// Re-exported from geom so callers can use meshio.Mesh without importing geom
// directly. These are aliases, not new types: meshio.Mesh and geom.Mesh are
// the same type and are freely interchangeable.
type (
	Geometry   = geom.Geometry
	Mesh       = geom.Mesh
	FaceColor  = geom.FaceColor
	Attachment = geom.Attachment
)
```

Add `"github.com/firstlayer-xyz/meshio/geom"` to the imports.

- [ ] **Step 5: Convert the seven encoding methods to functions**

Go forbids declaring a method whose receiver type is defined in another package, and a type alias does not change the defining package. So the moment `Mesh` lives in `geom`, every method on `*Mesh` stops compiling:

```
./mesh.go:28:10: cannot define new methods on non-local type Mesh
```

Convert all seven to package-level functions with the signatures given under **Interfaces** above. The conversion is mechanical: the receiver `m` becomes a parameter and each body stays byte-identical otherwise. Do not reorder or restructure anything inside them.

`MergeVertices` is unaffected — it is a method on `Geometry`, defined in `geom`, so it stays a method and moves with the type.

- [ ] **Step 6: Update call sites**

Roughly 20 call sites in `meshio_test.go` and `attachment_test.go`, plus internal calls in `mesh.go` and the `write_*.go` files. Call syntax only: `orig.EncodeSTL(&buf)` becomes `EncodeSTL(&buf, orig)`. **Do not touch a single test assertion** — if an assertion needs changing to pass, behavior drifted and something is wrong.

- [ ] **Step 7: Build and let the compiler find the rest**

Run: `go build ./...`

Remaining root files refer to `Mesh`, `FaceColor`, and `Geometry` unqualified; because root aliases them, those references need no change. Fix whatever the compiler still reports, and nothing else.

- [ ] **Step 8: Run the full suite**

Run: `go test ./...`
Expected: PASS for both `github.com/firstlayer-xyz/meshio` and `github.com/firstlayer-xyz/meshio/geom`, with no test assertions changed.

- [ ] **Step 9: Commit**

```bash
git add geom/ mesh.go
git rm geometry.go geometry_test.go
git add -A
git commit -m "refactor: move shared types into geom package

geom is a leaf package so the format packages can depend on it without a
cycle. Root re-exports the types as aliases, so meshio.Mesh is unchanged."
```

---

### Task 5: Extract the `stl` package

First format to move. Establishes the pattern the next two follow: `Decode(r)`, `Encode(w, m)`, `Read(path)`, `Write(path, m)` as package functions.

**Files:**
- Create: `stl/stl.go`, `stl/stl_test.go`
- Delete: `read_stl.go`, `write_stl.go`
- Modify: `mesh.go` (dispatch), `meshio_test.go` (move STL tests out)

**Interfaces:**
- Consumes: `geom.Mesh`, `geom.Geometry` from Task 4.
- Produces: package `stl` exporting
  - `func Decode(r io.Reader) (*geom.Mesh, error)` — handles both binary and ASCII STL
  - `func Encode(w io.Writer, m *geom.Mesh) error`
  - `func Read(path string) (*geom.Mesh, error)`
  - `func Write(path string, m *geom.Mesh) error`

  Root's `ReadSTL` is removed; callers use `stl.Read` or `meshio.Read`.

- [ ] **Step 1: Create `stl/stl.go`**

Combine `read_stl.go` and `write_stl.go` into one file under `package stl`. The existing function bodies move unchanged apart from these renames:

- `DecodeSTL` → `Decode`
- `func (m *Mesh) EncodeSTL(w io.Writer) error` → `func Encode(w io.Writer, m *geom.Mesh) error`
- `func (m *Mesh) WriteSTL(path string) error` → `func Write(path string, m *geom.Mesh) error`
- unexported helpers (`decodeSTLBinary`, `decodeSTLASCII`, `isASCIISTL`) keep their names
- every `*Mesh` becomes `*geom.Mesh`, every `Mesh{...}` becomes `geom.Mesh{...}`, every `Geometry{...}` becomes `geom.Geometry{...}`

Add a package doc comment:

```go
// Package stl reads and writes STL triangle meshes, binary and ASCII.
// STL carries no color; FaceColors are ignored on encode.
package stl
```

- [ ] **Step 2: Move the STL tests**

Move every `TestSTL*` function from `meshio_test.go` into `stl/stl_test.go` under `package stl`. Update each call: `DecodeSTL(r)` → `Decode(r)`, `m.EncodeSTL(w)` → `Encode(w, m)`. The `triangle()` and `triCube()` helpers are needed there too — copy the ones the STL tests use into `stl/stl_test.go`, and leave root's copies for root's remaining tests.

- [ ] **Step 3: Wire up root dispatch**

In root's `mesh.go`, `readers` currently maps to path-based readers. Change it to decoders and open the file once in `Read`:

```go
// readers maps a lowercase file extension to its decoder. It is the single
// source of truth for which mesh formats Read treats as importable.
var readers = map[string]func(io.Reader) (*Mesh, error){
	".stl": stl.Decode,
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
```

`.obj` and `.3mf` are added back to the map in Tasks 6 and 7. Until then the root `Decode`/`Encode` dispatch keeps calling the still-in-root OBJ and 3MF functions; only the STL arms change to `stl.Decode` / `stl.Encode`. Delete `ReadSTL` from root.

- [ ] **Step 4: Run the full suite**

Run: `go test ./...`
Expected: PASS for root, `geom`, and `stl`.

- [ ] **Step 5: Commit**

```bash
git add stl/ mesh.go meshio_test.go
git rm read_stl.go write_stl.go
git commit -m "refactor: extract stl package

Encoding becomes a package function, mirroring png.Encode/png.Decode,
because methods cannot be defined on geom.Mesh from another package."
```

---

### Task 6: Extract the `obj` package

Same pattern as Task 5. OBJ additionally writes a companion `.mtl` material library from `FaceColors`.

**Files:**
- Create: `obj/obj.go`, `obj/obj_test.go`
- Delete: `read_obj.go`, `write_obj.go`
- Modify: `mesh.go` (dispatch), `meshio_test.go` (move OBJ tests out)

**Interfaces:**
- Consumes: `geom.Mesh` from Task 4; the pattern established in Task 5.
- Produces: package `obj` exporting
  - `func Decode(r io.Reader) (*geom.Mesh, error)`
  - `func Encode(w io.Writer, m *geom.Mesh, mtlW io.Writer) error` — `mtlW` may be nil
  - `func Read(path string) (*geom.Mesh, error)`
  - `func Write(path string, m *geom.Mesh) error` — writes a sibling `.mtl` when the mesh has face colors

  Root's `ReadOBJ` is removed.

- [ ] **Step 1: Create `obj/obj.go`**

Combine `read_obj.go` and `write_obj.go` under `package obj`, with these renames:

- `DecodeOBJ` → `Decode`
- `func (m *Mesh) EncodeOBJ(w, mtlW io.Writer) error` → `func Encode(w io.Writer, m *geom.Mesh, mtlW io.Writer) error`
- `func (m *Mesh) WriteOBJ(path string) error` → `func Write(path string, m *geom.Mesh) error`
- unexported helpers (`encodeMtl`, `sanitizeHex`, `parseHexColor`, `pathDir`, `pathStem`) keep their names
- every `*Mesh` becomes `*geom.Mesh`, `Mesh{...}` becomes `geom.Mesh{...}`, `Geometry{...}` becomes `geom.Geometry{...}`

Package doc:

```go
// Package obj reads and writes Wavefront OBJ meshes. Per-face colors are
// written as a companion .mtl material library referenced by usemtl.
package obj
```

Note `encodeMtl` takes `m *Mesh` today; it becomes `m *geom.Mesh`.

- [ ] **Step 2: Move the OBJ tests**

Move every `TestOBJ*` function plus `TestParseHexColor`, `TestSanitizeHex`, `TestPathStem`, and `TestPathDir` from `meshio_test.go` into `obj/obj_test.go` under `package obj`. Update calls: `DecodeOBJ(r)` → `Decode(r)`, `m.EncodeOBJ(w, mtl)` → `Encode(w, m, mtl)`. Copy whichever mesh helpers those tests use.

`TestPathExt` stays in root — `pathExt` remains a root function.

- [ ] **Step 3: Wire up root dispatch**

The `readers` map already carries all three extensions (Task 5 kept `.obj`/`.3mf` pointing at root's in-root decoders so `TestCanRead` and `TestReadExtensions` keep passing). Repoint the `.obj` entry at `obj.Decode`, and point root's `Decode`/`Encode` OBJ arms at `obj.Decode` / `obj.Encode`. Root's `Encode` passes `nil` for `mtlW`, preserving today's behavior. Delete `ReadOBJ` from root.

- [ ] **Step 4: Run the full suite**

Run: `go test ./...`
Expected: PASS for root, `geom`, `stl`, and `obj`.

- [ ] **Step 5: Commit**

```bash
git add obj/ mesh.go meshio_test.go
git rm read_obj.go write_obj.go
git commit -m "refactor: extract obj package"
```

---

### Task 7: Extract the `threemf` package

The largest move: 3MF reading, writing, and the OPC plumbing. This package will also host the print types in Tasks 8-10.

**Files:**
- Create: `threemf/read.go`, `threemf/write.go`, `threemf/opc.go`, `threemf/transform.go`, and the corresponding `_test.go` files
- Delete: `read_3mf.go`, `write_3mf.go`, `opc_3mf.go`, `transform_3mf.go`, `read_3mf_test.go`, `transform_3mf_test.go`, `attachment_test.go`
- Modify: `mesh.go` (dispatch), `meshio_test.go`

**Interfaces:**
- Consumes: `geom.Mesh`, `geom.Geometry`, `geom.FaceColor`, `geom.Attachment` from Task 4.
- Produces: package `threemf` exporting
  - `func Decode(r io.Reader) (*geom.Mesh, error)`
  - `func Encode(w io.Writer, m *geom.Mesh) error`
  - `func Read(path string) (*geom.Mesh, error)`
  - `func Write(path string, m *geom.Mesh) error`

  All existing unexported helpers (`parseModelPart`, `resolveBuild`, `normalizeFaceColors`, `normalizeHex`, `addZipEntry`, `addZipBytes`, `toReaderAt`, `parseContentTypeOverrides`, `rootModelFromRels`, `findRootModelPart`, `affine` and its methods) move unchanged and stay unexported. Root's `Read3MF` is removed.

- [ ] **Step 1: Move the files**

Move each root file to its new home under `package threemf`, renaming as follows:

| From | To |
|---|---|
| `read_3mf.go` | `threemf/read.go` |
| `write_3mf.go` | `threemf/write.go` |
| `opc_3mf.go` | `threemf/opc.go` |
| `transform_3mf.go` | `threemf/transform.go` |
| `read_3mf_test.go` | `threemf/read_test.go` |
| `transform_3mf_test.go` | `threemf/transform_test.go` |
| `attachment_test.go` | `threemf/attachment_test.go` |

Apply the same renames as the other format packages: `Decode3MF` → `Decode`, `m.Encode3MF(w)` → `Encode(w, m)`, `m.Write3MF(path)` → `Write(path, m)`, and every `*Mesh`/`Mesh{...}`/`Geometry{...}`/`FaceColor{...}`/`Attachment{...}` gains the `geom.` qualifier.

Package doc, in `threemf/read.go`:

```go
// Package threemf reads and writes 3MF packages. Decode resolves the
// production extension, flattening components and build-item transforms into a
// single mesh. Encode writes per-face display color as a core material
// extension colorgroup; see Object for filament-slot output that slicers
// consume as multi-material.
package threemf
```

- [ ] **Step 2: Move the remaining 3MF tests out of root**

`meshio_test.go` still holds `TestThreeMFRoundTrip`, `TestThreeMFColorRoundTrip`, `TestThreeMFEmpty`, `TestEncode3MF_NoSlic3rConfig`, `TestDecode3MF_Slic3rConfigBecomesAttachment`, and `TestMergeVertices`. Move the 3MF ones into `threemf/write_test.go` under `package threemf`, updating calls to the new function forms. `TestMergeVertices` was already covered by `geom/geometry_test.go` in Task 4 — delete root's copy rather than moving it.

What remains in root's `meshio_test.go` is `TestEncodeDecodeDispatch`, `TestEncodeUnsupported`, `TestDecodeUnsupported`, `TestCanRead`, `TestReadExtensions`, and `TestPathExt`. Those test root's dispatch and stay.

- [ ] **Step 3: Wire up root dispatch**

Repoint the `readers` map's `.3mf` entry at `threemf.Decode` and point root's `Decode`/`Encode` 3MF arms at `threemf.Decode` / `threemf.Encode`. After this task every entry in the map points at a format package and no format code remains in root. Delete `Read3MF` from root.

Root's `mesh.go` now contains only: the four type aliases, `readers`, `Read`, `CanRead`, `ReadExtensions`, `Decode`, `Encode`, and `pathExt`.

- [ ] **Step 4: Verify the import graph**

Run: `go list -deps ./... | grep firstlayer`
Expected: `geom` appears with no local dependencies; `stl`, `obj`, and `threemf` each depend on `geom` only; the root package depends on all four. If any format package depends on another format package, the split is wrong — report it.

- [ ] **Step 5: Run the full suite**

Run: `go test ./...`
Expected: PASS for all five packages.

- [ ] **Step 6: Commit**

```bash
git add threemf/ mesh.go meshio_test.go
git rm read_3mf.go write_3mf.go opc_3mf.go transform_3mf.go read_3mf_test.go transform_3mf_test.go attachment_test.go
git commit -m "refactor: extract threemf package

Completes the format split. Import direction is now one-way:
meshio -> {stl, obj, threemf} -> geom."
```

---

### Task 8: `Object`, `Part`, and validation

The print-layer types, in `threemf` because every print writer is a 3MF writer. No serialization yet — this task is the data model and its rules.

**Files:**
- Create: `threemf/object.go`, `threemf/object_test.go`

**Interfaces:**
- Consumes: `geom.Geometry`, `geom.Attachment` from Task 4; the `threemf` package from Task 7.
- Produces:
  - `type Part struct { Name string; Geometry geom.Geometry; Filament int }`
  - `type Object struct { Name string; Parts []Part; Filament int; SlotColors map[int]string; Attachments []geom.Attachment }`
  - `func (o *Object) SetSlotColor(slot int, hex string)`
  - `func (o *Object) slot(p Part) int` — resolves `Part.Filament` → `Object.Filament` → `1`
  - `func (o *Object) validate() error`

- [ ] **Step 1: Write the failing test**

Create `threemf/object_test.go`:

```go
package threemf

import (
	"strings"
	"testing"

	"github.com/firstlayer-xyz/meshio/geom"
)

func unitTri() geom.Geometry {
	return geom.Geometry{
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
				Attachments: []geom.Attachment{
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

Run: `go test ./threemf/ -run 'TestSlotResolution|TestSetSlotColor|TestObjectValidate' -v`
Expected: FAIL — compile error, `undefined: Object`.

- [ ] **Step 3: Create `threemf/object.go`**

```go
package threemf

import (
	"fmt"

	"github.com/firstlayer-xyz/meshio/geom"
)

// Part is one sub-mesh of an Object, bound to a filament slot.
//
// A Part holds Geometry rather than a Mesh: per-face color and package
// attachments are meaningless here. Print color is the filament slot, and the
// color for that slot lives once on the Object.
type Part struct {
	Name     string
	Geometry geom.Geometry
	Filament int // 1-based slot; 0 = inherit Object.Filament
}

// Object is a printable object assembled from one or more parts. It is the
// print-layer counterpart to geom.Mesh: where a Mesh answers "what color do I
// draw this", an Object answers "which filament prints this".
//
// SlotColors records display color once per filament slot, so two parts sharing
// a slot cannot disagree about its color. It states design intent -- "slot 3 is
// meant to read as red" -- and makes no claim about what filament is physically
// loaded.
type Object struct {
	Name        string
	Parts       []Part
	Filament    int            // default for parts with Filament == 0; 0 means slot 1
	SlotColors  map[int]string // slot number -> display color; absent = uncolored
	Attachments []geom.Attachment
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

Run: `go test ./threemf/ -run 'TestSlotResolution|TestSetSlotColor|TestObjectValidate' -v`
Expected: PASS, all subtests.

- [ ] **Step 5: Commit**

```bash
git add threemf/object.go threemf/object_test.go
git commit -m "feat: add Object and Part print-layer types

Color is stored once per filament slot, so two parts sharing a slot cannot
declare conflicting colors."
```

---

### Task 9: Bambu writer — package structure and slot assignment

Emits the verified Bambu layout: a container object whose components are the parts, part geometry in one `3D/Objects/*.model`, and `Metadata/model_settings.config` carrying the extruder assignments. Color comes in Task 10.

**Files:**
- Create: `threemf/bambu.go`, `threemf/bambu_test.go`
- Modify: `threemf/write.go` (add the shared mesh serializer)

**Interfaces:**
- Consumes: `Object`, `Part`, `o.slot(p)`, `o.validate()` from Task 8; `addZipEntry`, `addZipBytes` from Task 7.
- Produces:
  - `func EncodeBambu(w io.Writer, o *Object) error`
  - `func WriteBambu(path string, o *Object) error`
  - `func derivedUUID(label string, index int) string`
  - `func writeMeshXML(sb *strings.Builder, g geom.Geometry, indent string, groupID int, colorAt func(tri int) int)` — in `write.go`, shared by both 3MF writers. `colorAt` returns the palette index for a triangle, or `-1` to omit `pid`/`p1`/`p2`/`p3`. `Encode` passes a per-face lookup; the Bambu writer passes a constant, because a part is one slot.

Part object ids are `1..N`; the container is `N+1`; the part file is `3D/Objects/object_<N+1>.model`.

- [ ] **Step 1: Write the failing test**

Create `threemf/bambu_test.go`:

```go
package threemf

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
	if err := EncodeBambu(&buf, twoPartObject()); err != nil {
		t.Fatalf("EncodeBambu: %v", err)
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
	if err := EncodeBambu(&buf, o); err != nil {
		t.Fatalf("EncodeBambu: %v", err)
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
	if err := EncodeBambu(&buf, twoPartObject()); err != nil {
		t.Fatalf("EncodeBambu: %v", err)
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
	if err := EncodeBambu(&a, twoPartObject()); err != nil {
		t.Fatalf("first encode: %v", err)
	}
	if err := EncodeBambu(&b, twoPartObject()); err != nil {
		t.Fatalf("second encode: %v", err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("encoding twice produced different bytes; output must be deterministic")
	}
}

func TestBambu_GeometryRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodeBambu(&buf, twoPartObject()); err != nil {
		t.Fatalf("EncodeBambu: %v", err)
	}
	m, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode of our own output: %v", err)
	}
	// Two unit triangles: 6 indices.
	if len(m.Indices) != 6 {
		t.Errorf("round-trip indices: got %d, want 6", len(m.Indices))
	}
}

func TestBambu_ValidationPropagates(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodeBambu(&buf, &Object{}); err == nil {
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

`readZipPart` already exists in `threemf/attachment_test.go` (moved there in Task 7) and is reusable in the same package.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./threemf/ -run TestBambu -v`
Expected: FAIL — compile error, `undefined: EncodeBambu`.

- [ ] **Step 3: Create `threemf/bambu.go`**

```go
package threemf

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/firstlayer-xyz/meshio/geom"
)

const (
	nsCore         = "http://schemas.microsoft.com/3dmanufacturing/core/2015/02"
	nsProduction   = "http://schemas.microsoft.com/3dmanufacturing/production/2015/06"
	nsBambu        = "http://schemas.bambulab.com/package/2021"
	relType3DModel = "http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel"
	identityXform  = "1 0 0 0 1 0 0 0 1 0 0 0"
	colorGroupID   = 100
)

// EncodeBambu writes the object as a Bambu Studio 3MF: a container object whose
// components are the parts, part geometry in 3D/Objects, and filament slot
// assignments in Metadata/model_settings.config.
//
// Unlike Encode, which carries display color only, this produces a file that
// slices multi-material: each part is bound to a filament slot.
func EncodeBambu(w io.Writer, o *Object) error {
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
		// Uncolored for now; Task 10 supplies the palette index for this part's slot.
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

// WriteBambu exports the object to a Bambu Studio 3MF file at path.
func WriteBambu(path string, o *Object) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("meshio: %w", err)
	}
	defer f.Close()
	return EncodeBambu(f, o)
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
		sb.WriteString("      <metadata key=\"matrix\" value=\"1 0 0 0 0 1 0 0 0 0 1 0 0 0 0 1\"/>\n")
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

Note: `geom` is imported here for `writeMeshXML`'s parameter type in `write.go`; if `bambu.go` itself ends up not referencing `geom` directly, drop the import and let the compiler guide you.

- [ ] **Step 4: Add the shared mesh serializer to `threemf/write.go`**

Both 3MF writers serialize a `<mesh>`; they differ only in how a triangle picks its palette index — `Encode` looks it up per face, the Bambu writer uses one index for the whole part. A `colorAt` callback covers both:

```go
// writeMeshXML serializes geometry as a 3MF <mesh> element, shared by all 3MF
// writers. colorAt returns the palette index for a triangle, or -1 to omit the
// pid/p1/p2/p3 color references.
func writeMeshXML(sb *strings.Builder, g geom.Geometry, indent string, groupID int, colorAt func(tri int) int) {
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

- [ ] **Step 5: Rewrite `Encode`'s mesh block to use it**

In `threemf/write.go`, `Encode` inlines its own `<vertices>`/`<triangles>` loops. Replace that whole block with one call. `hasColors` and `faceColorIdx` are already in scope from the palette-building code above it:

```go
	writeMeshXML(&sb, m.Geometry, "   ", colorGroupID, func(tri int) int {
		if !hasColors {
			return -1
		}
		return faceColorIdx[tri]
	})
```

`faceColorIdx[tri]` is already `-1` for uncolored faces, so no extra branch is needed. Delete the now-unused inline loops and the local `colorGroupID := 100` declaration, since it is now a package constant.

- [ ] **Step 6: Verify the core writer is unchanged in behavior**

Run: `go test ./threemf/ -run 'TestThreeMF|TestEncode' -v`
Expected: PASS. These tests predate this task; extracting the serializer must not alter a byte.

- [ ] **Step 7: Run tests to verify the Bambu tests pass**

Run: `go test ./threemf/ -run TestBambu -v`
Expected: PASS, all seven tests.

- [ ] **Step 8: Run the full suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add threemf/bambu.go threemf/bambu_test.go threemf/write.go
git commit -m "feat: add Bambu Studio 3MF writer

Emits the container/component structure and model_settings.config filament
slot assignments verified against real Bambu project files. Mesh XML
serialization is extracted and shared with the core 3MF writer."
```

---

### Task 10: Derive the colorgroup from `SlotColors`

3MF carries color per triangle; `Object` stores it per slot. The writer denormalizes: one palette entry per distinct color, every triangle of a part referencing its slot's entry.

**Files:**
- Modify: `threemf/object.go` (add `palette`)
- Modify: `threemf/bambu.go` (emit the colorgroup)
- Modify: `threemf/bambu_test.go` (add derivation tests)

**Interfaces:**
- Consumes: `Object.SlotColors`, `o.slot(p)`, `writeMeshXML`, `normalizeHex` (existing, `threemf/write.go`).
- Produces: `func (o *Object) palette() ([]string, map[int]int)` — ordered distinct normalized colors, plus slot→palette-index. Slots are visited in ascending numeric order so output is deterministic.

- [ ] **Step 1: Write the failing test**

Append to `threemf/bambu_test.go`:

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
	if err := EncodeBambu(&buf, o); err != nil {
		t.Fatalf("EncodeBambu: %v", err)
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
	if err := EncodeBambu(&buf, twoPartObject()); err != nil {
		t.Fatalf("EncodeBambu: %v", err)
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
	if err := EncodeBambu(&buf, o); err != nil {
		t.Fatalf("EncodeBambu: %v", err)
	}
	objects := readZipPart(t, buf.Bytes(), "3D/Objects/object_3.model")

	if len(allMatches(`p1="(\d+)"`, objects)) != 1 {
		t.Error("expected exactly one colored triangle; the unmapped slot must be uncolored, not an error")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./threemf/ -run 'TestBambu_Palette|TestBambu_Triangles|TestBambu_NoColors|TestBambu_Unmapped' -v`
Expected: FAIL — compile error, `o.palette undefined`.

- [ ] **Step 3: Add `palette` to `threemf/object.go`**

Add `"sort"` to the file's imports:

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

In `threemf/bambu.go`, three changes inside `EncodeBambu`.

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

Third, emit the colorgroup and give each part its slot's palette index. Replace the `<resources>` block written in Task 9:

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

Run: `go test ./threemf/ -run TestBambu -v`
Expected: PASS, all eleven tests.

- [ ] **Step 6: Run the full suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add threemf/object.go threemf/bambu.go threemf/bambu_test.go
git commit -m "feat: derive 3MF colorgroup from Object.SlotColors

Color is stored once per filament slot and denormalized into per-triangle
references at write time, so the redundant form exists only in the file."
```

---

### Task 11: Update the README

The README documents the old single-package, method-based API and lists multi-part as unimplemented. Both are now wrong.

**Files:**
- Modify: `README.md`

**Interfaces:**
- Consumes: the complete public API from Tasks 3–10.
- Produces: no code.

- [ ] **Step 1: Rewrite the intro and quick-start**

Replace the opening code block and type listing with:

```markdown
```go
mesh, err := meshio.Read("model.3mf")   // format from extension
err = threemf.Write("out.3mf", mesh)

// or per format
mesh, err := stl.Decode(r)
err = obj.Encode(w, mesh, mtlWriter)
```

## Packages

| Package | Holds |
|---|---|
| `meshio` | `Read`, `Decode`, `Encode` dispatch; type aliases for the `geom` types |
| `meshio/geom` | `Geometry`, `Mesh`, `FaceColor`, `Attachment` |
| `meshio/stl` | STL read/write |
| `meshio/obj` | OBJ read/write, `.mtl` materials |
| `meshio/threemf` | 3MF read/write, plus `Object`/`Part` for multi-material output |

Import direction is one-way: `meshio → {stl, obj, threemf} → geom`.

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

- [ ] **Step 2: Update the status table**

```markdown
| Capability | State |
|---|---|
| Per-face RGB → core colorgroup | works |
| Read production-extension / multi-part 3MF | works (geometry flattened to one mesh) |
| Multi-part object with per-part filament slot | works — `threemf.WriteBambu` |
| Per-triangle paint (`paint_color` / `mmu_segmentation`) | not implemented, encoding unverified |
| PrusaSlicer multi-material output | not implemented, needs a verified sample |
```

- [ ] **Step 3: Replace the "Practical guidance today" section**

```markdown
## Practical guidance today

- **Want color in a viewer, or interchange between tools?** `Mesh.FaceColors` +
  `threemf.Encode` is correct and sufficient.
- **Want a multi-color print?** Build an `Object` whose parts are separate
  solids, assign each a filament slot, and use `threemf.WriteBambu`:

```go
obj := &threemf.Object{Name: "plate", Filament: 1}
obj.SetSlotColor(1, "#000000")
obj.SetSlotColor(2, "#C81E1E")
obj.Parts = []threemf.Part{
    {Name: "base", Geometry: baseSlab},               // inherits slot 1
    {Name: "red",  Geometry: redTiles, Filament: 2},
}
err := threemf.WriteBambu("plate.3mf", obj)
```

  Each part must be a genuine solid — see the geometry gotcha above. Splitting a
  colored slab by face color yields zero-thickness patches that will not slice.
- **PrusaSlicer** has no multi-material output yet.
- **Do not** assume a correct-looking preview means a correct toolpath. Verify by
  slicing and checking the filament-change count.
```

- [ ] **Step 4: Remove the stale Slic3r paragraph**

Delete the paragraph beginning "There is also a `Metadata/Slic3r_PE_model.config` part written alongside." — it is no longer written. Keep the description of PrusaSlicer's expected format under "What the slicers actually consume"; it stays accurate and unverified.

- [ ] **Step 5: Verify the examples match reality**

Run: `go vet ./...`
Expected: no output. Then re-read every README code block against the real API and confirm each type and function name matches: `threemf.Object`, `threemf.Part`, `SetSlotColor`, `threemf.WriteBambu`, `stl.Decode`, `obj.Encode`, `meshio.Read`.

- [ ] **Step 6: Commit**

```bash
git add README.md
git commit -m "docs: document the package split and multi-part printing"
```

---

## Acceptance gate

**Required, and cannot be automated.** Green tests are not sufficient evidence for this change — the bug being fixed is precisely "passes inspection, slices wrong."

- [ ] Generate a two-color object with `threemf.WriteBambu`.
- [ ] Open it in Bambu Studio.
- [ ] Confirm the parts appear as **one object** with **distinct filament assignments**.
- [ ] Slice it and confirm the preview shows **actual filament changes**.
- [ ] Confirm the emitted colorgroup does not visually conflict with Bambu's own filament-based part coloring. The verified reference file carries no colorgroup, so this interaction is unverified; it affects display only, never the toolpath.

Do not report this work as complete until the slice preview has been seen.
