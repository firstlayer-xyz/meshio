# Per-Triangle Filament Paint Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a caller bind individual triangles of a single solid to filament slots, and write it in a form both Bambu Studio and PrusaSlicer consume — so a model with surface color needs no geometry change at all.

**Architecture:** A new `threemf.PaintedMesh` holds geometry plus a per-triangle slot array. The writer emits one object with one mesh, and puts both slicers' paint attributes on each painted triangle. Two small refactors make this possible without duplication: the slot-color logic becomes free functions both types call, and a per-triangle attribute struct replaces `writeMeshXML`'s palette-index callback.

**Tech Stack:** Go 1.25.0, standard library only. Module `github.com/firstlayer-xyz/meshio`. No third-party dependencies.

**Spec:** `docs/superpowers/specs/2026-07-21-meshio-painted-mesh-design.md`

## Global Constraints

- **Zero dependencies.** Standard library only. Do not add anything to `go.mod`.
- **Deterministic output.** Encoding the same input twice must produce byte-identical bytes. No `time.Now()`, no `math/rand`, no map iteration without sorting keys first.
- **Go 1.25.0**, module path `github.com/firstlayer-xyz/meshio`.
- **Test command:** `go test ./...` from the repo root. Single package: `go test ./threemf/ -run TestName -v`.
- **Import direction is one-way:** `meshio → {stl, obj, threemf} → geom`. `threemf` may import `geom` and stdlib only.
- **Filament slots are 1-based.** Slot `0` means unpainted — the attribute is omitted and the slicer uses its default.
- **Slots are capped at 16.** Above that the two slicers' encoders diverge and no single file serves both. This is a hard error, not a documented convention.
- **Never claim to be another application.** `Application` metadata is `meshio`. Writing "PrusaSlicer" would route Bambu to its compat importer under false pretences.
- Commit after every task using the message given in that task's final step.

---

## Task 0: The gate — PASSED 2026-07-22

**This is not a coding task and must not be skipped.** The design's one unverified assumption is that each slicer ignores the other's paint attribute on the same triangle. Everything else was read out of the slicers' source; this was not.

A prototype already exists at `~/Desktop/meshio-paint-test.3mf` — one closed solid, 96 triangles, top surface a 4×4 grid painted in four stripes (slots 1–4), bottom and walls unpainted, every painted triangle carrying both `slic3rpe:mmu_segmentation` and `paint_color`.

- [x] **Step 1: Open the prototype in Bambu Studio.** Check the paint tool. Expect four stripes on the top surface.
- [x] **Step 2: Open the prototype in PrusaSlicer.** Multi-material painting tool, with a multi-extruder printer profile selected. Expect the same four stripes.
- [x] **Step 3: Act on the result.** Both slicers render the four stripes. Proceed to Task 1 as written.

| Result | Action |
|---|---|
| Both show four stripes | Proceed to Task 1 as written. |
| One works, one does not | Stop. The plan still holds but splits: `EncodePaintedBambu` / `EncodePaintedPrusa` emit one attribute each, and Task 4's "both attributes" tests become per-writer. Escalate before continuing. |
| Neither shows paint | Stop. The bitstream reading is wrong. Return to `TriangleSelector::serialize` in both codebases before any implementation. |

If a slicer shows paint on the *wrong* stripes, record which — a permutation identifies a bit-ordering error precisely, and is far more informative than "it didn't work".

---

## Task 1: Share the slot-color logic without embedding

`SetSlotColor` and `palette()` are `Object` methods that `PaintedMesh` needs
identically. Extract those two bodies into free functions both call, and leave
each type its own fields.

**Embedding a shared struct was considered and rejected.** Go does not permit
promoted fields in composite literals, so `Object{Filament: 4}` would stop
compiling for every caller — a public API regression traded for three small
methods. Each type keeps thin wrappers instead; the bodies live once.

**Files:**
- Create: `threemf/slotcolor.go`, `threemf/slotcolor_test.go`
- Modify: `threemf/object.go` (two method bodies become one-line delegations)

**Interfaces:**
- Consumes: `normalizeHex` (existing, `threemf/write.go`).
- Produces:
  - `func setSlotColor(m map[int]string, slot int, hex string) map[int]string` — returns the map, allocating when nil, so callers assign the result.
  - `func slotColorPalette(slotColors map[int]string) ([]string, map[int]int)` — ordered distinct colors and slot→index.

  Only these two: `PaintedMesh` needs both. `Object.defaultSlot` stays exactly
  where it is, because `PaintedMesh` has no object-level default and extracting
  a function with one caller buys nothing.

  `Object`'s `SetSlotColor` and `palette` keep their signatures and become
  one-line delegations. No field moves, no literal changes, no caller changes.

- [ ] **Step 1: Write the failing test**

Create `threemf/slotcolor_test.go`:

```go
package threemf

import "testing"

func TestSetSlotColorAllocates(t *testing.T) {
	var m map[int]string
	m = setSlotColor(m, 3, "#C81E1E") // must not panic on the nil map
	if m[3] != "#C81E1E" {
		t.Errorf("m[3] = %q, want #C81E1E", m[3])
	}
	m = setSlotColor(m, 3, "#000000")
	if m[3] != "#000000" {
		t.Errorf("second write did not overwrite: %q", m[3])
	}
}

func TestSlotColorPaletteDedupsByColor(t *testing.T) {
	m := map[int]string{1: "#FF0000", 2: "#FF0000", 3: "#0000FF"}
	palette, bySlot := slotColorPalette(m)

	if len(palette) != 2 {
		t.Fatalf("palette = %v, want 2 entries (dedup by color)", palette)
	}
	if bySlot[1] != bySlot[2] {
		t.Errorf("slots 1 and 2 share a color but map to %d and %d", bySlot[1], bySlot[2])
	}
	if palette[bySlot[1]] != "#FF0000FF" {
		t.Errorf("palette[%d] = %q, want normalized #FF0000FF", bySlot[1], palette[bySlot[1]])
	}
	// Ascending slot order is what makes the output deterministic.
	if bySlot[1] != 0 {
		t.Errorf("slot 1 should claim palette index 0, got %d", bySlot[1])
	}
}

func TestSlotColorPaletteSkipsEmpty(t *testing.T) {
	palette, bySlot := slotColorPalette(map[int]string{1: "", 2: "#00FF00"})
	if len(palette) != 1 {
		t.Fatalf("palette = %v, want only the non-empty color", palette)
	}
	if _, ok := bySlot[1]; ok {
		t.Error("a slot with an empty color must not appear in the index")
	}
}

// Object still exposes the same API, now delegating.
func TestObjectStillDelegates(t *testing.T) {
	o := &Object{Filament: 4} // composite literal must still compile
	o.SetSlotColor(2, "#0000FF")
	if o.SlotColors[2] != "#0000FF" {
		t.Errorf("SlotColors[2] = %q", o.SlotColors[2])
	}
	if got := o.defaultSlot(); got != 4 {
		t.Errorf("defaultSlot = %d, want 4", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./threemf/ -run 'TestSetSlotColor|TestSlotColorPalette|TestObjectStillDelegates' -v`
Expected: FAIL — compile error, `undefined: setSlotColor`.

- [ ] **Step 3: Create `threemf/slotcolor.go`**

Move the bodies off `Object` verbatim, changing only how they get their inputs:

```go
package threemf

import "sort"

// Filament slots and their display colors are shared by every print-layer type:
// Object binds a slot per part, PaintedMesh binds one per triangle, and both
// record the color once per slot. These hold that logic so neither has to
// duplicate it.
//
// They are free functions rather than methods on a shared embedded struct
// because Go does not allow promoted fields in composite literals -- embedding
// would break Object{Filament: 4} for every caller.

// setSlotColor assigns a display color to a filament slot, allocating the map
// when it is nil. It returns the map, so callers assign the result.
func setSlotColor(m map[int]string, slot int, hex string) map[int]string {
	if m == nil {
		m = map[int]string{}
	}
	m[slot] = hex
	return m
}

// slotColorPalette returns the ordered distinct display colors and a map from
// filament slot to palette index.
//
// 3MF carries color per triangle while a slot map stores it once per slot, so
// this is the denormalization step. Slots are visited in ascending order and
// colors deduped by value, making the result deterministic: encoding the same
// input twice produces identical bytes.
func slotColorPalette(slotColors map[int]string) ([]string, map[int]int) {
	slots := make([]int, 0, len(slotColors))
	for slot := range slotColors {
		slots = append(slots, slot)
	}
	sort.Ints(slots)

	var palette []string
	idxByColor := map[string]int{}
	idxBySlot := map[int]int{}

	for _, slot := range slots {
		raw := slotColors[slot]
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
		idxBySlot[slot] = idx
	}
	return palette, idxBySlot
}
```

- [ ] **Step 4: Delegate from `Object`**

In `threemf/object.go`, replace two method bodies. Signatures and doc comments
stay; only the bodies change. `defaultSlot` is untouched.

```go
func (o *Object) SetSlotColor(slot int, hex string) {
	o.SlotColors = setSlotColor(o.SlotColors, slot, hex)
}

func (o *Object) palette() ([]string, map[int]int) {
	return slotColorPalette(o.SlotColors)
}
```

Remove the now-unused `sort` import from `object.go`.

- [ ] **Step 5: Run the full suite**

Run: `go test ./...`
Expected: PASS with no test edited. Every existing `Object` test exercises the
same behavior through the same API; if any needed changing, behavior drifted --
stop and report.

- [ ] **Step 6: Commit**

```bash
git add threemf/slotcolor.go threemf/slotcolor_test.go threemf/object.go
git commit -m "refactor: share slot-color logic as free functions

PaintedMesh needs the same slot-to-color mapping Object has. Free functions
rather than a shared embedded struct: Go forbids promoted fields in
composite literals, so embedding would break Object{Filament: 4}."
```

---

## Task 2: The paint encoder

A pure function from filament slot to the attribute string both slicers read. This is the whole of the format work, and it is a lookup table — so the tests must be strong enough to catch a wrong table.

**Files:**
- Create: `threemf/paint.go`, `threemf/paint_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `const maxPaintSlot = 16`
  - `func paintString(slot int) string` — `""` for slot 0; panics on out-of-range input, which validation must prevent from ever reaching it.

- [ ] **Step 1: Write the failing test**

Create `threemf/paint_test.go`:

```go
package threemf

import "testing"

// The encoding, read out of TriangleSelector::serialize and
// FacetsAnnotation::get_triangle_as_string in both PrusaSlicer and
// BambuStudio. Bits are pushed least-significant-first; the string is built by
// reading four bits at a time and PREPENDING each hex digit, so it comes out
// in reverse nibble order.
func TestPaintString(t *testing.T) {
	cases := []struct {
		slot int
		want string
	}{
		{0, ""},    // unpainted: attribute omitted entirely
		{1, "4"},   // 00 split + 01 state -> nibble 0b0100
		{2, "8"},   // 00 split + 10 state -> nibble 0b1000
		{3, "0C"},  // 00 + 11 indicator -> C, then (3-3)=0, prepended
		{4, "1C"},  // (4-3)=1
		{5, "2C"},
		{10, "7C"},
		{15, "CC"},
		{16, "DC"}, // (16-3)=13=D; the highest slot both slicers agree on
	}
	for _, tc := range cases {
		if got := paintString(tc.slot); got != tc.want {
			t.Errorf("paintString(%d) = %q, want %q", tc.slot, got, tc.want)
		}
	}
}

// Every slot in the supported range must produce a distinct string. A table
// that collided would silently paint two slots the same.
func TestPaintStringIsInjective(t *testing.T) {
	seen := map[string]int{}
	for slot := 1; slot <= maxPaintSlot; slot++ {
		s := paintString(slot)
		if s == "" {
			t.Errorf("paintString(%d) returned the empty (unpainted) string", slot)
		}
		if prev, ok := seen[s]; ok {
			t.Errorf("paintString(%d) = paintString(%d) = %q", slot, prev, s)
		}
		seen[s] = slot
	}
}

// The single-nibble forms are exactly slots 1 and 2; everything else carries
// the "C" indicator. This pins the shape, so a table rewritten to a different
// scheme fails even if it stayed injective.
func TestPaintStringShape(t *testing.T) {
	for slot := 1; slot <= maxPaintSlot; slot++ {
		s := paintString(slot)
		switch {
		case slot <= 2:
			if len(s) != 1 {
				t.Errorf("paintString(%d) = %q, want a single nibble", slot, s)
			}
		default:
			if len(s) != 2 || s[1] != 'C' {
				t.Errorf("paintString(%d) = %q, want two nibbles ending in C", slot, s)
			}
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./threemf/ -run TestPaintString -v`
Expected: FAIL — compile error, `undefined: paintString`.

- [ ] **Step 3: Create `threemf/paint.go`**

```go
package threemf

import "fmt"

// maxPaintSlot is the highest filament slot expressible in a file both slicers
// read.
//
// The two encoders agree only up to here. Above state 16 they diverge: Bambu
// emits repeating 0b1111 nibbles as a continuation, while PrusaSlicer treats
// 0b1110 as a prefix introducing eight more bits. At slot 17 they would read
// the same bytes as different values, so no single file serves both. Sixteen is
// also the AMS maximum, so the shared range covers every realistic case.
const maxPaintSlot = 16

// paintString encodes one whole, unsplit triangle painted with a single
// filament slot, in the form both slicers read.
//
// Derived from TriangleSelector::serialize and
// FacetsAnnotation::get_triangle_as_string. The bitstream is, least significant
// bit first: two bits for the number of split sides (always 0 here, since
// meshio never subdivides), then the state. States below 3 are two bits; states
// 3 and up are the marker 0b11 followed by four bits of (state - 3). States are
// filament slots directly: Extruder1 = 1, ExtruderN = N.
//
// The string is then read four bits at a time and each hex digit PREPENDED, so
// the emitted text is the nibbles in reverse order -- which is why the marker
// nibble "C" ends up last.
//
// Returns "" for slot 0, meaning unpainted: the attribute is omitted and the
// slicer uses its own default. Panics above maxPaintSlot; callers validate
// first, so reaching it is a bug rather than bad input.
func paintString(slot int) string {
	switch {
	case slot <= 0:
		return ""
	case slot < 3:
		// Two bits of state, in the high half of the only nibble.
		return fmt.Sprintf("%X", slot<<2)
	case slot <= maxPaintSlot:
		// Marker nibble 0b1100 = C, then (slot-3), prepended ahead of it.
		return fmt.Sprintf("%X", slot-3) + "C"
	default:
		panic(fmt.Sprintf("meshio: filament slot %d exceeds %d; validation should have rejected it", slot, maxPaintSlot))
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./threemf/ -run TestPaintString -v`
Expected: PASS, all three tests.

- [ ] **Step 5: Verify the tests catch a wrong table**

The encoder is a lookup table, so confirm the tests would fail against a plausible mistake. Temporarily change `+ "C"` to `"C" + ` (nibbles in the wrong order), run `go test ./threemf/ -run TestPaintString`, and confirm FAILURE. Restore, confirm PASS. Report both outcomes.

- [ ] **Step 6: Commit**

```bash
git add threemf/paint.go threemf/paint_test.go
git commit -m "feat: encode a filament slot as slicer paint

The bitstream form read out of both slicers' TriangleSelector. For a whole
unsplit triangle it collapses to one or two hex nibbles."
```

---

## Task 3: Carry paint through `writeMeshXML`

The shared mesh serializer's per-triangle callback returns a palette index. It must also carry the paint string. Return a small struct rather than growing the parameter list.

**Files:**
- Modify: `threemf/write.go` (the `writeMeshXML` signature and body)
- Modify: `threemf/write.go`, `threemf/bambu.go`, `threemf/prusa.go` (the three call sites)

**Interfaces:**
- Consumes: `paintString` from Task 2 (not called here, but the type exists for Task 4).
- Produces:
  - `type triangleAttrs struct { colorIdx int; paint string }`
  - `func writeMeshXML(sb *strings.Builder, g geom.Geometry, indent string, groupID int, attrsAt func(tri int) triangleAttrs)`

  `colorIdx < 0` omits `pid`/`p1`/`p2`/`p3`; `paint == ""` omits both paint attributes. The three existing callers return `triangleAttrs{colorIdx: ..., paint: ""}`.

- [ ] **Step 1: Write the failing test**

Add to `threemf/paint_test.go`:

```go
import (
	"strings"
	"testing"

	"github.com/firstlayer-xyz/meshio/geom"
)

// writeMeshXML must emit both slicers' paint attributes with the same value,
// and omit both when a triangle is unpainted.
func TestWriteMeshXMLEmitsPaint(t *testing.T) {
	g := geom.Geometry{
		Vertices: []float32{0, 0, 0, 1, 0, 0, 0, 1, 0, 1, 1, 0},
		Indices:  []uint32{0, 1, 2, 1, 3, 2},
	}
	var sb strings.Builder
	writeMeshXML(&sb, g, "", 0, func(tri int) triangleAttrs {
		if tri == 0 {
			return triangleAttrs{colorIdx: -1, paint: "1C"}
		}
		return triangleAttrs{colorIdx: -1} // unpainted
	})
	out := sb.String()

	if !strings.Contains(out, `slic3rpe:mmu_segmentation="1C"`) {
		t.Errorf("missing PrusaSlicer paint attribute:\n%s", out)
	}
	if !strings.Contains(out, `paint_color="1C"`) {
		t.Errorf("missing Bambu paint attribute:\n%s", out)
	}
	if got := strings.Count(out, "paint_color="); got != 1 {
		t.Errorf("paint_color on %d triangles, want 1 (the other is unpainted)", got)
	}
	if got := strings.Count(out, "mmu_segmentation="); got != 1 {
		t.Errorf("mmu_segmentation on %d triangles, want 1", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./threemf/ -run TestWriteMeshXMLEmitsPaint -v`
Expected: FAIL — compile error, `undefined: triangleAttrs`.

- [ ] **Step 3: Change the signature and body**

In `threemf/write.go`, replace the `colorAt` callback:

```go
// triangleAttrs is what varies per triangle in a 3MF <mesh>: its palette
// reference and its paint. Grouped into a struct so the serializer takes one
// callback rather than one parameter per attribute.
type triangleAttrs struct {
	colorIdx int    // index into the colorgroup; < 0 omits pid/p1/p2/p3
	paint    string // slicer paint encoding; "" omits the paint attributes
}

// writeMeshXML serializes geometry as a 3MF <mesh> element, shared by all 3MF
// writers. attrsAt returns the per-triangle attributes.
//
// Both slicers' paint attributes are written with the same value: they use the
// identical encoding and differ only in name, so one file serves both.
func writeMeshXML(sb *strings.Builder, g geom.Geometry, indent string, groupID int, attrsAt func(tri int) triangleAttrs) {
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
		a := attrsAt(i)
		fmt.Fprintf(sb, indent+"  <triangle v1=\"%d\" v2=\"%d\" v3=\"%d\"", v1, v2, v3)
		if a.colorIdx >= 0 {
			fmt.Fprintf(sb, " pid=\"%d\" p1=\"%d\" p2=\"%d\" p3=\"%d\"", groupID, a.colorIdx, a.colorIdx, a.colorIdx)
		}
		if a.paint != "" {
			fmt.Fprintf(sb, " slic3rpe:mmu_segmentation=\"%s\" paint_color=\"%s\"", xmlAttr(a.paint), xmlAttr(a.paint))
		}
		sb.WriteString(" />\n")
	}
	sb.WriteString(indent + " </triangles>\n")
	sb.WriteString(indent + "</mesh>\n")
}
```

- [ ] **Step 4: Update the three call sites**

`threemf/write.go` in `Encode`:

```go
	writeMeshXML(&sb, m.Geometry, "   ", colorGroupID, func(tri int) triangleAttrs {
		if !hasColors {
			return triangleAttrs{colorIdx: -1}
		}
		return triangleAttrs{colorIdx: faceColorIdx[tri]}
	})
```

`threemf/bambu.go`:

```go
		writeMeshXML(&objects, p.Geometry, "   ", l.colorGroupID, func(int) triangleAttrs {
			return triangleAttrs{colorIdx: colorIdx}
		})
```

`threemf/prusa.go`:

```go
	writeMeshXML(&model, merged, "   ", colorGroupID, func(tri int) triangleAttrs {
		return triangleAttrs{colorIdx: triColor[tri]}
	})
```

- [ ] **Step 5: Verify existing output is unchanged**

Run: `go test ./threemf/ -run 'TestBambu|TestPrusa|TestThreeMF|TestEncode3MF' -v`
Expected: PASS. These predate this task and assert the existing writers' bytes; a serializer change must not alter them.

- [ ] **Step 6: Run the full suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add threemf/write.go threemf/bambu.go threemf/prusa.go threemf/paint_test.go
git commit -m "refactor: carry per-triangle paint through writeMeshXML

The serializer's callback returned only a palette index. Group what varies
per triangle into a struct so paint rides alongside it without a fifth
parameter."
```

---

## Task 4: `PaintedMesh` and its writer

**Files:**
- Create: `threemf/painted.go`, `threemf/painted_test.go`

**Interfaces:**
- Consumes: `setSlotColor`/`slotColorPalette` (Task 1), `paintString`/`maxPaintSlot` (Task 2), `triangleAttrs`/`writeMeshXML` (Task 3), plus existing `validateAttachments`, `addZipEntry`, `addZipBytes`, `xmlAttr`, `resourceIDs`, `nsCore`, `relType3DModel`, `identityXform`.
- Produces:
  - `type PaintedMesh struct { Name string; Geometry geom.Geometry; FaceSlots []int; SlotColors map[int]string; Attachments []geom.Attachment }`

    No `Filament` field: no config part is written, so there is no object-level default to express. Slot `0` means whatever the slicer defaults to.
  - `func (p *PaintedMesh) SetSlotColor(slot int, hex string)` and `func (p *PaintedMesh) palette() ([]string, map[int]int)` — one-line delegations to Task 1's functions, mirroring `Object`'s
  - `func EncodePainted(w io.Writer, p *PaintedMesh) error`
  - `func WritePainted(path string, p *PaintedMesh) error`
  - `func (p *PaintedMesh) validate() error`

- [ ] **Step 1: Write the failing test**

Create `threemf/painted_test.go`:

```go
package threemf

import (
	"bytes"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/firstlayer-xyz/meshio/geom"
)

// quad is two triangles sharing an edge, so a fixture can paint one and leave
// the other bare.
func quad() geom.Geometry {
	return geom.Geometry{
		Vertices: []float32{0, 0, 0, 1, 0, 0, 0, 1, 0, 1, 1, 0},
		Indices:  []uint32{0, 1, 2, 1, 3, 2},
	}
}

func paintedQuad() *PaintedMesh {
	p := &PaintedMesh{Name: "plate", Geometry: quad(), FaceSlots: []int{1, 2}}
	p.SetSlotColor(1, "#FF0000")
	p.SetSlotColor(2, "#0000FF")
	return p
}

func TestPainted_WritesOneObjectOneMesh(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodePainted(&buf, paintedQuad()); err != nil {
		t.Fatalf("EncodePainted: %v", err)
	}
	model := readZipPart(t, buf.Bytes(), "3D/3dmodel.model")

	if got := strings.Count(model, "<object "); got != 1 {
		t.Errorf("objects = %d, want 1: paint keeps the caller's geometry whole", got)
	}
	if got := strings.Count(model, "<mesh>"); got != 1 {
		t.Errorf("meshes = %d, want 1", got)
	}
	if strings.Contains(model, "<component") {
		t.Error("painted output must not split into components")
	}
	if !strings.Contains(model, `<metadata name="Application">meshio</metadata>`) {
		t.Error("Application must identify meshio honestly")
	}
	if !strings.Contains(model, "xmlns:slic3rpe=") {
		t.Error("slic3rpe namespace must be declared when paint is emitted")
	}
}

func TestPainted_BothAttributesPerTriangle(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodePainted(&buf, paintedQuad()); err != nil {
		t.Fatalf("EncodePainted: %v", err)
	}
	model := readZipPart(t, buf.Bytes(), "3D/3dmodel.model")

	mmu := allMatches(`slic3rpe:mmu_segmentation="([^"]*)"`, model)
	bbs := allMatches(`paint_color="([^"]*)"`, model)
	if len(mmu) != 2 || len(bbs) != 2 {
		t.Fatalf("attribute counts: mmu=%d paint_color=%d, want 2 and 2", len(mmu), len(bbs))
	}
	for i := range mmu {
		if mmu[i] != bbs[i] {
			t.Errorf("triangle %d: mmu=%q but paint_color=%q; both must carry the same value", i, mmu[i], bbs[i])
		}
	}
	if mmu[0] != "4" || mmu[1] != "8" {
		t.Errorf("paint values = %v, want [4 8] for slots 1 and 2", mmu)
	}
}

func TestPainted_UnpaintedTriangleOmitsAttributes(t *testing.T) {
	p := &PaintedMesh{Geometry: quad(), FaceSlots: []int{2, 0}}
	p.SetSlotColor(2, "#0000FF")

	var buf bytes.Buffer
	if err := EncodePainted(&buf, p); err != nil {
		t.Fatalf("EncodePainted: %v", err)
	}
	model := readZipPart(t, buf.Bytes(), "3D/3dmodel.model")

	if got := strings.Count(model, "paint_color="); got != 1 {
		t.Errorf("painted triangles = %d, want 1", got)
	}
	if strings.Contains(model, `paint_color=""`) {
		t.Error("an unpainted triangle must omit the attribute, not emit it empty")
	}
}

func TestPainted_ColorGroupFromSlotColors(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodePainted(&buf, paintedQuad()); err != nil {
		t.Fatalf("EncodePainted: %v", err)
	}
	model := readZipPart(t, buf.Bytes(), "3D/3dmodel.model")

	if !strings.Contains(model, `<m:color color="#FF0000FF" />`) {
		t.Errorf("slot 1 color missing from the colorgroup:\n%s", model)
	}
	objectID := regexp.MustCompile(`<object id="(\d+)"`).FindStringSubmatch(model)[1]
	groupID := regexp.MustCompile(`<m:colorgroup id="(\d+)"`).FindStringSubmatch(model)[1]
	if objectID == groupID {
		t.Errorf("colorgroup id %s collides with the object id", groupID)
	}
}

func TestPainted_NoColorsOmitsColorgroup(t *testing.T) {
	p := &PaintedMesh{Geometry: quad(), FaceSlots: []int{1, 1}}
	var buf bytes.Buffer
	if err := EncodePainted(&buf, p); err != nil {
		t.Fatalf("EncodePainted: %v", err)
	}
	model := readZipPart(t, buf.Bytes(), "3D/3dmodel.model")
	if strings.Contains(model, "colorgroup") || strings.Contains(model, "pid=") {
		t.Errorf("no SlotColors set, but color output was written:\n%s", model)
	}
	// Paint is independent of display color and must still be present.
	if !strings.Contains(model, `paint_color="4"`) {
		t.Error("paint must be emitted even with no SlotColors")
	}
}

func TestPainted_Validation(t *testing.T) {
	tests := []struct {
		name string
		mesh *PaintedMesh
		want string
	}{
		{
			name: "valid",
			mesh: &PaintedMesh{Geometry: quad(), FaceSlots: []int{1, 2}},
			want: "",
		},
		{
			name: "empty geometry",
			mesh: &PaintedMesh{FaceSlots: []int{}},
			want: "empty geometry",
		},
		{
			name: "too few face slots",
			mesh: &PaintedMesh{Geometry: quad(), FaceSlots: []int{1}},
			want: "face slots",
		},
		{
			name: "too many face slots",
			mesh: &PaintedMesh{Geometry: quad(), FaceSlots: []int{1, 2, 3}},
			want: "face slots",
		},
		{
			name: "negative slot",
			mesh: &PaintedMesh{Geometry: quad(), FaceSlots: []int{1, -1}},
			want: "negative filament slot",
		},
		{
			name: "slot above the shared ceiling",
			mesh: &PaintedMesh{Geometry: quad(), FaceSlots: []int{1, 17}},
			want: "exceeds 16",
		},
		{
			name: "slot at the ceiling is fine",
			mesh: &PaintedMesh{Geometry: quad(), FaceSlots: []int{1, 16}},
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.mesh.validate()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("expected valid, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected an error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestPainted_Deterministic(t *testing.T) {
	var a, b bytes.Buffer
	if err := EncodePainted(&a, paintedQuad()); err != nil {
		t.Fatalf("first encode: %v", err)
	}
	if err := EncodePainted(&b, paintedQuad()); err != nil {
		t.Fatalf("second encode: %v", err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("encoding twice produced different bytes")
	}
}

func TestPainted_GeometrySurvivesRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodePainted(&buf, paintedQuad()); err != nil {
		t.Fatalf("EncodePainted: %v", err)
	}
	// Paint is write-only: Decode drops it, but the geometry must be intact.
	m, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(m.Indices) != 6 {
		t.Errorf("round-trip indices = %d, want 6", len(m.Indices))
	}
}

func TestWritePainted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plate.3mf")
	if err := WritePainted(path, paintedQuad()); err != nil {
		t.Fatalf("WritePainted: %v", err)
	}
	if _, err := Read(path); err != nil {
		t.Fatalf("Read: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./threemf/ -run TestPainted -v`
Expected: FAIL — compile error, `undefined: PaintedMesh`.

- [ ] **Step 3: Create `threemf/painted.go`**

```go
package threemf

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/firstlayer-xyz/meshio/geom"
)

const nsSlic3rPE = "http://schemas.slic3r.org/3mf/2017/06"

// PaintedMesh is a single solid whose triangles are individually bound to
// filament slots.
//
// Where an Object splits a model into parts that are each one slot, this keeps
// one mesh and paints it, so the caller's geometry ships unchanged. That is the
// difference that matters for surface color: a slab with a colored top face
// cannot be split into printable parts -- the non-base colors come out as flat,
// zero-thickness patches -- but it can be painted.
//
// FaceSlots is parallel to the triangle stream, the same shape as
// geom.Mesh.FaceColors but carrying a filament slot rather than a color. The
// two are deliberately different types: one answers "what color do I draw
// this", the other "which filament prints this".
type PaintedMesh struct {
	Name        string
	Geometry    geom.Geometry
	FaceSlots   []int          // len == numTris; 1-based slot, 0 = unpainted
	SlotColors  map[int]string // slot number -> display color; absent = uncolored
	Attachments []geom.Attachment
}

// SetSlotColor assigns a display color to a filament slot, allocating
// SlotColors if needed.
func (p *PaintedMesh) SetSlotColor(slot int, hex string) {
	p.SlotColors = setSlotColor(p.SlotColors, slot, hex)
}

// palette returns the ordered distinct display colors and a map from filament
// slot to palette index.
func (p *PaintedMesh) palette() ([]string, map[int]int) {
	return slotColorPalette(p.SlotColors)
}

// validate reports the first structural problem with the mesh.
func (p *PaintedMesh) validate() error {
	numTris := len(p.Geometry.Indices) / 3
	if len(p.Geometry.Vertices) == 0 || numTris == 0 {
		return fmt.Errorf("meshio: painted mesh %q has empty geometry", p.Name)
	}
	if len(p.FaceSlots) != numTris {
		return fmt.Errorf("meshio: %d face slots for %d triangles", len(p.FaceSlots), numTris)
	}
	for i, slot := range p.FaceSlots {
		if slot < 0 {
			return fmt.Errorf("meshio: negative filament slot %d on face %d", slot, i)
		}
		if slot > maxPaintSlot {
			return fmt.Errorf("meshio: filament slot %d on face %d exceeds %d; above that Bambu Studio and PrusaSlicer encode paint differently and no single file serves both", slot, i, maxPaintSlot)
		}
	}
	return validateAttachments(p.Attachments,
		"[Content_Types].xml", "_rels/.rels", "3D/3dmodel.model")
}

// EncodePainted writes the mesh as a 3MF whose triangles carry per-slot paint,
// readable by both Bambu Studio and PrusaSlicer.
//
// Both slicers use the identical paint encoding and differ only in the
// attribute name, so every painted triangle carries both and each reads its
// own. That is possible here and was not for multi-part output, where the two
// disagree about the geometry itself.
//
// It does not mutate its input.
func EncodePainted(w io.Writer, p *PaintedMesh) error {
	if err := p.validate(); err != nil {
		return err
	}

	palette, paletteBySlot := p.palette()

	var ids resourceIDs
	objectID := ids.next()
	colorGroupID := 0
	if len(palette) > 0 {
		colorGroupID = ids.next()
	}

	var model strings.Builder
	model.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	materialNS := ""
	if len(palette) > 0 {
		materialNS = ` xmlns:m="http://schemas.microsoft.com/3dmanufacturing/material/2015/02"`
	}
	fmt.Fprintf(&model, `<model unit="millimeter" xml:lang="en-US" xmlns="%s" xmlns:slic3rpe="%s"%s>`+"\n",
		nsCore, nsSlic3rPE, materialNS)
	model.WriteString(` <metadata name="Application">meshio</metadata>` + "\n")
	model.WriteString(" <resources>\n")
	if len(palette) > 0 {
		fmt.Fprintf(&model, "  <m:colorgroup id=\"%d\">\n", colorGroupID)
		for _, hexColor := range palette {
			fmt.Fprintf(&model, "   <m:color color=\"%s\" />\n", xmlAttr(hexColor))
		}
		model.WriteString("  </m:colorgroup>\n")
	}
	fmt.Fprintf(&model, "  <object id=\"%d\" type=\"model\">\n", objectID)
	writeMeshXML(&model, p.Geometry, "   ", colorGroupID, func(tri int) triangleAttrs {
		slot := p.FaceSlots[tri]
		a := triangleAttrs{colorIdx: -1, paint: paintString(slot)}
		if idx, ok := paletteBySlot[slot]; ok {
			a.colorIdx = idx
		}
		return a
	})
	model.WriteString("  </object>\n </resources>\n")
	model.WriteString(" <build>\n")
	fmt.Fprintf(&model, "  <item objectid=\"%d\" transform=\"%s\" printable=\"1\" />\n", objectID, identityXform)
	model.WriteString(" </build>\n</model>\n")

	var ct strings.Builder
	ct.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	ct.WriteString(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` + "\n")
	ct.WriteString(` <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml" />` + "\n")
	ct.WriteString(` <Default Extension="model" ContentType="application/vnd.ms-package.3dmanufacturing-3dmodel+xml" />` + "\n")
	writeContentTypeOverrides(&ct, p.Attachments)
	ct.WriteString("</Types>\n")

	rels := `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + "\n" +
		` <Relationship Target="/3D/3dmodel.model" Id="rel0" Type="` + relType3DModel + `" />` + "\n" +
		"</Relationships>\n"

	zw := zip.NewWriter(w)
	for _, e := range []struct{ name, content string }{
		{"[Content_Types].xml", ct.String()},
		{"_rels/.rels", rels},
		{"3D/3dmodel.model", model.String()},
	} {
		if err := addZipEntry(zw, e.name, e.content); err != nil {
			return err
		}
	}
	for _, att := range p.Attachments {
		if err := addZipBytes(zw, att.Path, att.Data); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("meshio: closing zip: %w", err)
	}
	return nil
}

// WritePainted exports the painted mesh to a 3MF file at path.
func WritePainted(path string, p *PaintedMesh) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("meshio: %w", err)
	}
	defer f.Close()
	return EncodePainted(f, p)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./threemf/ -run TestPainted -v`
Expected: PASS, all subtests.

- [ ] **Step 5: Run the full suite**

Run: `go test ./...`, then `go vet ./...`, then `gofmt -l .`
Expected: PASS, clean, no output.

- [ ] **Step 6: Commit**

```bash
git add threemf/painted.go threemf/painted_test.go
git commit -m "feat: write per-triangle filament paint

PaintedMesh binds individual triangles of one solid to filament slots, so a
model with surface color needs no geometry change. Every painted triangle
carries both slicers' attributes; they share an encoding and differ only in
name."
```

---

## Task 5: Documentation

**Files:**
- Modify: `README.md`

**Interfaces:**
- Consumes: the API from Task 4.
- Produces: no code.

- [ ] **Step 1: Add paint to the status table**

```markdown
| Per-triangle paint (`paint_color` / `mmu_segmentation`) | works — `threemf.WritePainted`, slots 1–16 |
```

- [ ] **Step 2: Add a section after the multi-part guidance**

```markdown
### Painting one solid instead of splitting it

Splitting a model into parts requires each part to be a printable solid, which
is why a slab with a colored top face cannot be done that way — the non-base
colors come out as flat, zero-thickness patches. Painting binds individual
triangles of a single mesh instead, so the geometry ships unchanged:

```go
p := &threemf.PaintedMesh{Name: "plate", Geometry: slab}
p.FaceSlots = slots // one entry per triangle; 0 leaves a face unpainted
p.SetSlotColor(1, "#000000")
p.SetSlotColor(2, "#C81E1E")
err := threemf.WritePainted("plate.3mf", p)
```

One file works in both slicers: they use the identical paint encoding and differ
only in the attribute name, so each triangle carries both. This is possible for
paint and not for parts, where the two disagree about the geometry itself.

**Slots are limited to 1–16.** Above 16 the encoders diverge — Bambu emits
repeating continuation nibbles where PrusaSlicer expects a prefix code — so no
single file can serve both. Sixteen is also the AMS maximum.

Paint is write-only: `Decode` drops it. A brush-painted file encodes a recursive
triangle-subdivision tree whose children are not in the mesh, so reading it is a
much larger question than writing.
```

- [ ] **Step 3: Verify every identifier against the source**

Run: `go vet ./...`, then re-read each README code block against `threemf/painted.go` and confirm `PaintedMesh`, `FaceSlots`, `SetSlotColor`, and `WritePainted` all match exactly.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: document per-triangle paint and the 16-slot ceiling"
```

---

## Acceptance gate

**Required, and separate from Task 0.** Task 0 validates the assumption before any code exists; this validates what the code actually produced.

- [ ] Generate a painted file with `WritePainted` — a slab with a painted top surface in at least three slots.
- [ ] Open it in Bambu Studio. Confirm the painted regions appear on the correct triangles.
- [ ] Open it in PrusaSlicer with a multi-extruder profile. Confirm the same.
- [ ] Slice in one of them and confirm the preview shows real filament changes.

Green tests are not sufficient evidence here. They prove the bytes match what was read out of the slicers' source; only the slicers prove that reading was right.
