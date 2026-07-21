# meshio: multi-part objects with per-part filament slots

**Date:** 2026-07-21
**Status:** Approved, ready for implementation planning

## Problem

meshio writes per-face color as the 3MF core material extension — an
`<m:colorgroup>` plus `pid`/`p1`/`p2`/`p3` references on each triangle. That is
the *display* channel. Bambu Studio imports and renders it correctly but never
converts it into filament assignments, so the model slices as a single material.
Observed directly with tapmag output: colors show, toolpath is monochrome.

Slicers do not represent color as RGB. They represent it as a **filament slot
index**; the color comes from the filament profile loaded in that slot. meshio
currently has no way to express a slot, so it cannot produce a printable
multi-color file.

A second defect compounds this: `Encode3MF` also writes
`Metadata/Slic3r_PE_model.config` containing
`<metadata type="slic3r.extruder" value="#RRGGBBFF">`. PrusaSlicer expects
`<metadata type="volume" key="extruder" value="2">` — a slot index, not a hex
color. Nothing reads what we emit. It is inert code that advertises support we
do not have.

## Root cause

Both defects are symptoms of one design fault: `Mesh` is used both as the
display/interchange type and as the basis for printing. Those jobs need color to
mean different things — *"draw this triangle red"* versus *"print this with
whatever filament is in slot 3"* — and a single per-face color field can only
express the first. So the writer emits the display meaning, printing silently
gets nothing, and the file looks correct while slicing wrong.

Every fix that keeps one type serving both jobs is a patch on that coupling:
validating two copies against each other, moving the copy onto the part,
enforcing consistency across parts. The fix is to separate the types by job and
share only what they genuinely have in common, which is geometry.

## Goals

1. Let a caller express "this object is N sub-meshes, each printed with filament
   slot S."
2. Write that in the dialect Bambu Studio actually consumes.
3. Remove the inert PrusaSlicer path rather than leaving it as a false signal.

## Non-goals

- **PrusaSlicer writer.** The `Slic3r_PE_model.config` volume structure is known
  in outline but has not been verified against a real PrusaSlicer file. Shipping
  a second unverified path would repeat the mistake this spec fixes. Deferred
  until a sample is available.
- **Per-triangle painting** (`paint_color` / `slic3rpe:mmu_segmentation`). This
  is the better long-term answer for surface color because it needs no geometry
  change, but the attribute encodes a recursive subdivision tree and the encoding
  has not been verified. Prerequisite for a future spec: a painted sample saved
  out of Bambu Studio, decoded.
- **Reading slot assignments back** into an `Object`. The existing reader
  flattens production-extension geometry into a single `Mesh`; that behavior is
  unchanged and remains lossy for slots.
- **Palette-to-slot mapping** and **geometry splitting**. Both belong upstream
  (in tapmag), for the reason given under Constraints.

## Constraints discovered

**Splitting a colored solid by face color does not produce printable parts.**
tapmag's plate is a closed slab whose top grid is colored per cell and whose
bottom and walls take a base color. Split that by color and the non-base colors
become flat, zero-thickness patches while the remainder is an open shell.
Neither slices. Per-part color requires each part to be a genuine solid — a base
slab plus extruded per-color tiles — which is a modeling decision. meshio's job
is to *express* the assignment, not to invent geometry.

**Palette size is bounded by physical slots** (4 per AMS, 16 with four units).
Reducing the palette is the caller's responsibility.

## Verified evidence

Structures below were read out of real Bambu Studio project files (a multi-part
model and an AMS multi-color model), not from documentation.

Container object referencing parts, in `3D/3dmodel.model`:

```xml
<object id="155" p:UUID="..." type="model">
 <components>
  <component p:path="/3D/Objects/object_155.model" objectid="118" p:UUID="..." transform="..."/>
  <component p:path="/3D/Objects/object_155.model" objectid="119" p:UUID="..." transform="..."/>
 </components>
</object>
```

Slot assignment, in `Metadata/model_settings.config`:

```xml
<object id="155">
  <metadata key="name" value="Derpy Cat Head Final 2.obj"/>
  <metadata key="extruder" value="2"/>       <!-- object-level default -->
  <part id="118" subtype="normal_part">      <!-- part id == component objectid -->
    <mesh_stat .../>
  </part>                                    <!-- no extruder → inherits 2 -->
  <part id="119" subtype="normal_part">
    <metadata key="extruder" value="7"/>     <!-- overrides the default -->
  </part>
</object>
```

Confirmed: 37 components with `objectid` 118…154 correspond one-to-one with 37
`<part id>` entries. Part-level `extruder` overrides object-level; an absent
part-level value inherits.

Relationship entry in `3D/_rels/3dmodel.model.rels`:

```xml
<Relationship Target="/3D/Objects/object_155.model" Id="rel-1"
  Type="http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel"/>
```

`[Content_Types].xml` declares **no** content type for `.config` — neither a
`Default` for the extension nor an `Override` for the part. `model_settings.config`
ships as an undeclared part. This is strictly OPC-invalid but is what Bambu
writes and reads, so we match it rather than "correct" it.

## Data model

`Mesh` has quietly been three things at once: geometry, display presentation,
and OPC packaging. Splitting out the first names all three and gives `Part`
something to hold that cannot express color.

```go
// Geometry is triangle geometry — no presentation, no packaging.
type Geometry struct {
    Vertices []float32 // xyz triples, len = numVerts*3
    Indices  []uint32  // triangle indices, len = numTris*3
}

type Mesh struct {
    Geometry                 // embedded: m.Vertices and m.Indices still resolve
    FaceColors  []FaceColor  // display channel
    Attachments []Attachment // OPC package contents
}
```

`MergeVertices` moves to `Geometry`, where it belongs — it is a purely geometric
operation and never touched the other fields.

New file `object.go`:

```go
// Part is one sub-mesh of an Object, bound to a filament slot.
type Part struct {
    Name     string
    Geometry Geometry
    Filament int // 1-based slot; 0 = inherit Object.Filament
}

// Object is a printable object assembled from one or more parts.
type Object struct {
    Name        string
    Parts       []Part
    Filament    int            // default for parts with Filament == 0; 0 means slot 1
    SlotColors  map[int]string // slot number → display color; absent = uncolored
    Attachments []Attachment
}

// SetSlotColor assigns a display color to a filament slot, allocating
// SlotColors if needed.
func (o *Object) SetSlotColor(slot int, hex string)
```

```go
obj.SlotColors = map[int]string{1: "#000000", 3: "#C81E1E"}
obj.SetSlotColor(3, "#C81E1E")   // equivalent, nil-safe
```

Keyed by slot number, because that is what the caller has in hand. A slice
indexed by slot would force padding to reach a high slot
(`[]string{"", "", "", "#C81E1E"}`) and invites an off-by-one on every read,
since slots are 1-based everywhere else — the Bambu XML, the AMS UI, and
`Object.Filament`.

Map iteration order is unspecified, so the writer sorts slot numbers ascending
before building the palette. This is not extra work: the palette dedups by color
and therefore needs a defined order regardless.

A multi-part object is not a mesh, so it is a distinct type rather than a
`Parts` field on `Mesh`. The default/override inheritance mirrors the verified
Bambu structure exactly, so it serializes without translation.

A `Part` holds `Geometry`, not `*Mesh`. A mesh would drag in two fields that are
meaningless for a part — `FaceColors` (color comes from the slot) and
`Attachments` (a package-level concern). Holding geometry makes both
unrepresentable instead of documented-and-ignored.

### Breaking change

This breaks composite literals across repos: `meshio.Mesh{Vertices: v,
Indices: i}` becomes `meshio.Mesh{Geometry: meshio.Geometry{Vertices: v,
Indices: i}}`. Field *reads* are unaffected — embedding promotes `m.Vertices`
and `m.Indices` — as are `Read`, `Decode`, and every function signature
returning `*Mesh`.

Known call sites: `tapmag/plate.go`, and in Facet `pkg/manifold/encode.go`,
`pkg/manifold/export.go`, `pkg/meshpreview`, plus tests in each. The migration
is mechanical and compiler-guided: every break is a composite literal, and the
compiler finds all of them.

## API

```go
func (o *Object) EncodeBambu3MF(w io.Writer) error
func (o *Object) WriteBambu3MF(path string) error
```

Explicit names rather than a `Flavor` enum with a single value: adding
`EncodePrusa3MF` later is then purely additive and breaks no callers.

## Validation

All are hard errors, not silently tolerated:

| Condition | Error |
|---|---|
| `len(Parts) == 0` | `meshio: object has no parts` |
| `Part.Geometry` has zero vertices or triangles | `meshio: part %q has empty geometry` |
| `Filament < 0` on part or object | `meshio: negative filament slot` |
| Duplicate `Attachment.Path` | existing behavior, retained |

### Color is keyed by slot, and denormalized on write

One slot is one filament, therefore one color. Color functionally depends on the
slot, so the API stores it exactly once per slot in `Object.SlotColors`. Two
parts sharing a slot cannot declare different colors, because neither part
declares a color at all — there is no second copy to disagree.

**The API is not 1:1 with the output.** 3MF has no per-slot color concept; it
carries color per triangle. So the writer *derives* the denormalized form:

```
SlotColors  →  <m:colorgroup> (one entry per distinct non-empty color)
part slot   →  pid/p1/p2/p3 on every triangle of that part
```

Every triangle of a part gets the same reference, because a part is one slot. The
redundant representation exists only in the file, where the format requires it,
and is generated — never authored, never stored, never able to drift.

Two earlier drafts got this wrong and are recorded here so the reasoning is not
relitigated:

1. *Per-face color as the source, with a "one slot, one color" validation rule.*
   Rejected: the rule polices a denormalization the API should not have had, and
   would fire on incidental color, since Facet stamps a default color on nearly
   every mesh it produces.
2. *`Part.Color` as the source.* Rejected: it relocates the same defect. Two
   parts on one slot can still declare conflicting colors.

`SlotColors` is a statement of design intent — "slot 3 is meant to read as red" —
not a claim about what filament is physically loaded. meshio models no AMS state
and verifies nothing about the printer. The field drives display output only; the
print assignment is the slot index alone.

**Scope note.** Slot→color is really a property of the *document*, not of an
object: two objects on one plate share the printer's slots and could not
disagree about slot 3 in reality. It sits on `Object` because
`EncodeBambu3MF` writes exactly one object, making `Object` the document today.
If multi-object output is ever added, `SlotColors` must move up to the type that
represents the file — otherwise two objects can declare conflicting colors for
one slot, which is the defect this design exists to prevent, one level up.

A part has no per-face color to ignore: it holds `Geometry`, which has no color
field at all. `FaceColors` remains fully supported on `Mesh`, its own path —
Facet's preview and web viewer, the OBJ `.mtl` writer, and core 3MF colorgroup
round-trip all depend on it — and that path is unchanged by this spec.

## Emitted package

Part object ids are assigned `1..N`; the container object gets `N+1`, so ids
cannot collide. The part-mesh file is named after the container id. All part
meshes go in one `3D/Objects/object_<N+1>.model`, matching the AMS file where 37
parts of one object share a single file.

`p:UUID` values are derived deterministically — SHA-256 over the part name and
index, formatted as a UUID — rather than randomly. Output is therefore
byte-reproducible and golden-testable.

```
[Content_Types].xml              rels + model Defaults; Overrides for attachments only
_rels/.rels                      root → /3D/3dmodel.model
3D/3dmodel.model                 container object, components, build item
3D/_rels/3dmodel.model.rels      → /3D/Objects/object_<N+1>.model
3D/Objects/object_<N+1>.model    one <object> per part, each with its <mesh>
Metadata/model_settings.config   object + part slot assignments
<attachments>                    unchanged
```

`3D/3dmodel.model`:

```xml
<model unit="millimeter" xml:lang="en-US"
  xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02"
  xmlns:p="http://schemas.microsoft.com/3dmanufacturing/production/2015/06"
  xmlns:BambuStudio="http://schemas.bambulab.com/package/2021"
  requiredextensions="p">
 <metadata name="Application">meshio</metadata>
 <metadata name="BambuStudio:3mfVersion">1</metadata>
 <resources>
  <object id="4" p:UUID="..." type="model">
   <components>                              <!-- 3 parts → ids 1..3, container 4 -->
    <component p:path="/3D/Objects/object_4.model" objectid="1" p:UUID="..."
      transform="1 0 0 0 1 0 0 0 1 0 0 0"/>
    <component p:path="/3D/Objects/object_4.model" objectid="2" p:UUID="..."
      transform="1 0 0 0 1 0 0 0 1 0 0 0"/>
    <component p:path="/3D/Objects/object_4.model" objectid="3" p:UUID="..."
      transform="1 0 0 0 1 0 0 0 1 0 0 0"/>
   </components>
  </object>
 </resources>
 <build p:UUID="...">
  <item objectid="4" p:UUID="..." transform="1 0 0 0 1 0 0 0 1 0 0 0" printable="1"/>
 </build>
</model>
```

Part meshes carry an empty `<build/>`, as the core spec requires a build element
in every model part.

`Metadata/model_settings.config` emits identity `matrix` and an all-zero
`mesh_stat` per part. Both match the known-good file; the zeros are accurate
because meshio performs no mesh repair.

Empty names are omitted rather than emitted blank: if `Object.Name` or
`Part.Name` is `""`, the corresponding `<metadata key="name">` element is not
written. Names are cosmetic labels in the slicer, and an empty `value=""` is
noise. UUID derivation uses name *and* index, so it stays unique when names are
absent or duplicated.

An `<m:colorgroup>` is written when `SlotColors` has any non-empty entry,
derived as described above: one palette entry per distinct color, and every
triangle of a part referencing its slot's entry. When `SlotColors` is empty the
colorgroup is omitted entirely and the output matches the verified file exactly.

No `project_settings.config` is written. It is large, mostly required keys we
have no basis to invent, and unnecessary — the slot index alone drives the
print.

Because the verified Bambu file carries no colorgroup, its interaction with
Bambu's own filament-based part coloring is unverified. It affects display only,
never the toolpath, so the risk is cosmetic — but confirming it is part of the
manual acceptance gate.

## Refactor

Three axes are currently tangled inside single files: **format** (3MF/STL/OBJ),
**direction** (read/write), and **purpose** (display/print). Most visibly,
`Decode3MF` lives in `write_3mf.go` (`write_3mf.go:184`) together with
`toReaderAt` and the zip helpers — a file named "write" owns the decoder. And
`Encode3MF` is a 160-line function that builds a palette, serializes XML, and
writes the zip.

Give each axis its own dimension in the layout, one concern per file:

```
geometry.go          Geometry, MergeVertices
mesh.go              Mesh, FaceColor, Attachment, format dispatch,
                     Read/CanRead/ReadExtensions
object.go            Object, Part, SetSlotColor

opc_3mf.go           OPC plumbing: zip entries, [Content_Types].xml, .rels,
                     toReaderAt
read_3mf.go          Decode3MF, Read3MF, model-part parsing, component
                     resolution                        ← Decode3MF moves here
write_3mf.go         Mesh.Encode3MF/Write3MF — core flavor, colorgroup
write_bambu_3mf.go   Object.EncodeBambu3MF/WriteBambu3MF — print flavor
transform_3mf.go     affine math (unchanged)

read_stl.go / write_stl.go / read_obj.go / write_obj.go   (unchanged)
```

`meshio.go` is retired: its contents move to `mesh.go` and `geometry.go`.
`rels_3mf.go` folds into `opc_3mf.go`, which is the same concern. Mesh→XML
serialization is extracted once and shared by both 3MF writers.

**Not** split into sub-packages. A `meshio/print` package importing a shared
`geometry` package would make the display/print boundary compile-enforced, but
the types are already distinct so the original bug cannot recur, and at this size
it buys import friction plus a package that exists only to break a cycle.
Revisit if the print side grows a Prusa writer, paint encoding, and slot
validation — at that point it earns its own namespace.

Deletions:

- `buildModelConfig` and the `Slic3r_PE_model.config` write
  (`write_3mf.go:156-163`, `write_3mf.go:319-361`).
- The read-side special-case skip for that filename (`write_3mf.go:253`). Once
  meshio no longer emits it, a copy found in a file is PrusaSlicer's own, and
  preserving it as an ordinary `Attachment` is more correct than dropping it.
  One fewer special case.

## Testing

Automated:

1. Encode a two-part object; unzip in memory; assert each `component objectid`
   has a matching `<part id>`.
2. Assert emitted `extruder` values, including that a part with `Filament == 0`
   inherits the object default and one with a value overrides it.
3. Assert `3D/_rels/3dmodel.model.rels` targets the object file, and that
   `[Content_Types].xml` declares no `.config` type.
4. Geometry round-trip: `Decode3MF` our own Bambu output through the existing
   production-extension reader; vertices and triangles match the union of parts.
5. Each validation error above.
6. Color derivation: two parts sharing a slot produce one palette entry and
   identical `pid`/`p1` references; parts on different slots with the same color
   share a single palette entry (dedup by color, not by slot); a nil or empty
   `SlotColors` omits the colorgroup entirely; a part whose slot has no entry in
   `SlotColors` is uncolored rather than an error; and `SetSlotColor` on a nil
   map allocates rather than panicking.
7. Determinism: encoding twice yields byte-identical output.

Manual acceptance gate — **cannot be automated and is required**:

> Open the output in Bambu Studio. Confirm parts appear as one object with
> distinct filament assignments, and that slicing produces actual filament
> changes.

Today's bug is exactly "passes inspection, slices wrong." Green unit tests are
not sufficient evidence of success for this change; the slice preview is.

## Follow-on work

1. Update `README.md` — move multi-part from "not implemented" to "works", drop
   the `Slic3r_PE_model.config` row, and document `Geometry`.
2. Migrate downstream composite literals to the `Geometry` split: tapmag, and
   Facet's `pkg/manifold` and `pkg/meshpreview`. Compiler-guided and mechanical.
3. tapmag: generate per-color solids (base slab plus extruded tiles) and emit via
   `Object`.
4. **PrusaSlicer writer — fast follow.** `Object` is already the right
   abstraction for it: PrusaSlicer expresses the same per-part idea as
   triangle-range `<volume>` entries in `Metadata/Slic3r_PE_model.config` with
   `<metadata type="volume" key="extruder" value="2"/>`. Since parts are
   contiguous in the emitted triangle stream, the ranges fall out directly, and
   `EncodePrusa3MF` is additive — no change to `Object` or `Part`.

   **Prerequisite: a genuine PrusaSlicer-authored multi-material 3MF.** A scan of
   local files found three carrying `Metadata/Slic3r_PE_model.config`, but all
   three are meshio's own inert output (`type="slic3r.extruder"` with a hex
   value), not PrusaSlicer's. Building the writer from recollection would repeat
   the exact failure this spec exists to correct.
5. Approach B (per-triangle paint), once a painted Bambu sample has been decoded.
