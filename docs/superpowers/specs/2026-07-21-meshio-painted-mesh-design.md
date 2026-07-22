# meshio: per-triangle filament paint

**Date:** 2026-07-21
**Status:** Approved. Gate passed 2026-07-22 — the prototype renders correctly in both Bambu Studio and PrusaSlicer.

## Problem

meshio can express multi-material printing only by splitting a model into
separate parts, each bound to one filament slot (`threemf.Object`/`Part`). That
forces a geometry change on the caller, and for surface color the required
geometry is awkward or impossible.

The motivating case is a pixel-art plate: a flat slab whose top face is a grid
of colored cells. As a single closed solid it cannot be split by color — the
non-base colors come out as flat, zero-thickness patches and the remainder is an
open shell, and neither slices. Expressing it with parts requires rebuilding it
as a base slab plus one extruded tile per cell, which is real modeling work
undertaken purely to satisfy the file format.

Both slicers support the thing that actually fits: painting individual triangles
of one solid. meshio cannot write it.

## Goals

1. Let a caller bind individual triangles of a single solid to filament slots.
2. Write it in a form both Bambu Studio and PrusaSlicer consume.
3. Require no geometry change: the mesh the caller has is the mesh that ships.

## Non-goals

- **Reading paint back.** Decode ignores paint, as today. A brush-painted file
  encodes a recursive subdivision tree whose child triangles do not exist in the
  mesh, so reading it means deciding what a split triangle becomes in a flat
  mesh — a much larger question than writing.
- **Composing paint with parts.** Both slicers allow a part to be painted *and*
  have a default slot. meshio will not: `PaintedMesh` is a single solid, and
  `Object` stays parts-only. Supporting both at once would weaken the invariant
  that makes `Part` clean (a part is one slot) and double the writer paths.
- **More than 16 slots.** See Constraints.

## Verified encoding

Read out of `TriangleSelector::serialize` and
`FacetsAnnotation::get_triangle_as_string` in both PrusaSlicer and BambuStudio,
not inferred.

Each original triangle is encoded as a bitstream:

```
leaf (states 0-2):    xxyy              yy = number of split sides, xx = state
leaf (states 3-16):   zzzzxxyy          xx = 0b11 (indicator), zzzz = state - 3
non-leaf:             xxyy              xx = special side, yy = number of splits
```

Bits are pushed least-significant-first. `get_triangle_as_string` then reads the
stream four bits at a time, converts each nibble to a hex digit, and **prepends**
it — so the emitted string is the nibbles in reverse order.

State values come from `TriangleStateType` / `EnforcerBlockerType`, identical in
both: `NONE = 0`, `Extruder1 = 1`, `Extruder2 = 2`, `ExtruderN = N`.

meshio writes only unsplit whole triangles, so the general case collapses to:

| Slot | Bitstream | Emitted string |
|---|---|---|
| 0 (unpainted) | — | `""` — attribute omitted |
| 1 | `00` `01` | `"4"` |
| 2 | `00` `10` | `"8"` |
| 3 | `00` `11` `0000` | `"0C"` |
| 4 | `00` `11` `1000` | `"1C"` |
| 16 | `00` `11` `1011` | `"DC"` |

`get_triangle_as_string` is byte-identical between the two codebases. Only the
attribute name differs: `slic3rpe:mmu_segmentation` (PrusaSlicer) and
`paint_color` (Bambu Studio).

## Constraints discovered

**Slots above 16 cannot be expressed in a file that serves both slicers.** The
two encoders diverge for states above 16:

```
Bambu:       n -= 3; while (n >= 15) { emit 0b1111; n -= 15; } emit 4 bits of n
PrusaSlicer: if (n <= 16) emit 4 bits of (n-3)
             else         emit 0b1110, then 8 bits of (n-17)
```

For states 3–16 both emit the single nibble `n-3` and agree exactly. At state 17
they collide: Bambu emits nibble `14` as a value, PrusaSlicer reads `14` as an
extension prefix and consumes eight more bits. Sixteen is also the AMS maximum,
so the shared range covers every realistic case — but it is a hard limit, not a
convention, and must be enforced rather than documented.

**Bambu accepts a component-less object.** Verified in
`_BBS_3MF_Importer::_generate_current_object_list`: it seeds the list with the
object itself, recurses into components when present, and otherwise falls
through to "has geometry → push itself". A single object with a direct mesh
yields exactly one volume. This is what makes the painted layout viable there,
and it is the opposite of the parts case, where a component-less container was
required.

**No config part is written.** A `Metadata/model_settings.config` declaring an
object with no `<part>` entries would leave `volumes_ptr` pointing at an empty
list, and `_generate_volumes_new` would find no volume matching the sole
sub-object — risking an object that imports with zero volumes. Omitting the
config takes Bambu's "config not found" path, which builds the volume from the
object itself. The consequence is that there is no object-level default
extruder: slot `0` means whatever the slicer defaults to, which is 1.

## Gate — PASSED 2026-07-22

The design writes **both** attributes on every painted triangle:

```xml
<triangle v1=".." v2=".." v3=".."
          slic3rpe:mmu_segmentation="1C" paint_color="1C" />
```

Each slicer should read its own and ignore the other's, which would make one
file serve both — the thing that was structurally impossible for parts, because
there the *geometry* conflicted and here it does not.

This was reasoned rather than observed when the design was written: both parsers
look attributes up by name and ignore unknown ones, but that was an inference
about their XML handling, not something read off a real dual-attribute file.

**It has since been verified.** A prototype carrying both attributes renders its
four painted stripes correctly in Bambu Studio and in PrusaSlicer. One file
drives both — which was impossible for multi-part output, where the two slicers
disagree about the geometry itself.

**A prototype exists at `~/Desktop/meshio-paint-test.3mf`** — one closed solid,
96 triangles, top surface a 4×4 grid painted in four stripes, bottom and walls
unpainted. Opening it in both slicers resolves the question:

| Result | Consequence for this spec |
|---|---|
| Both show four stripes | Design proceeds as written. |
| One works, one does not | Split into `WritePaintedBambu` / `WritePaintedPrusa`, mirroring the parts writers. Everything else stands. |
| Neither shows paint | The bitstream reading is wrong; return to the source before implementing. |

Implementation was gated on this answer -- the same discipline the multi-part
work used, and the reason a dual-dialect parts file was abandoned after one
throwaway rather than after a feature.

## Data model

New file `threemf/painted.go`:

```go
// PaintedMesh is a single solid whose triangles are individually bound to
// filament slots.
//
// Where an Object splits a model into parts that are each one slot, this keeps
// one mesh and paints it, so the caller's geometry ships unchanged. That is the
// difference that matters for surface color: a slab with a colored top cannot
// be split into printable parts, but it can be painted.
type PaintedMesh struct {
	Name        string
	Geometry    geom.Geometry
	FaceSlots   []int          // len == numTris; 1-based slot, 0 = slicer default
	SlotColors  map[int]string // slot -> display color
	Attachments []geom.Attachment
}
```

`FaceSlots` is parallel to the triangle stream, the same shape as
`geom.Mesh.FaceColors` — but carrying a slot index rather than RGB. The two stay
separate types for the reason the rest of this library separates them: one
answers "what colour do I draw this", the other "which filament prints this".

## API

```go
func EncodePainted(w io.Writer, p *PaintedMesh) error
func WritePainted(path string, p *PaintedMesh) error
```

Named for what they write rather than for a slicer, because — subject to the
Gate — one file serves both. If the Gate splits them, they become
`EncodePaintedBambu` / `EncodePaintedPrusa` and the un-suffixed names are
retired.

## Emitted package

```
[Content_Types].xml    rels + model Defaults; Overrides for typed attachments
_rels/.rels            root -> /3D/3dmodel.model
3D/3dmodel.model       one object, one mesh, paint attributes per triangle
<attachments>          unchanged
```

The model part declares the core namespace, `xmlns:slic3rpe`, and the material
extension namespace when a palette exists. `Application` is `meshio` — never
another application's name, which would route Bambu to its PrusaSlicer-compat
importer under false pretences.

Each triangle carries both paint attributes when its slot is non-zero, and
neither when it is zero. A colorgroup is emitted from `SlotColors` exactly as
the other writers do, so viewers still show colour.

## Validation

Hard errors, all before any bytes are written:

| Condition | Error |
|---|---|
| zero vertices or triangles | `meshio: empty geometry` |
| `len(FaceSlots) != numTris` | `meshio: %d face slots for %d triangles` |
| any slot `< 0` | `meshio: negative filament slot %d on face %d` |
| any slot `> 16` | `meshio: filament slot %d on face %d exceeds 16; above that Bambu Studio and PrusaSlicer encode paint differently and no single file serves both` |
| duplicate or reserved attachment path | existing `validateAttachments` |

The slot ceiling names *why* in the message. A caller who hits it needs to know
it is a format limit, not an arbitrary cap.

## Shared refactors

Two, both because `PaintedMesh` needs what `Object` already has:

1. **`slotPalette`** — `SetSlotColor`, `defaultSlot`, and `palette()` are
   `Object` methods that `PaintedMesh` needs identically. Extract a small type
   both embed rather than duplicating three methods.

2. **`writeMeshXML`'s per-triangle callback** currently returns a palette index.
   It must also carry the paint string, so it returns a small struct instead of
   growing the parameter list. Three existing call sites change mechanically.

## Testing

Automated:

1. `paintString` against the table above, including slot 0 and the 16 boundary.
2. Both attributes present on painted triangles, absent on unpainted ones, and
   carrying identical values.
3. Slot inheritance is not a thing here — assert an unpainted triangle emits no
   attribute rather than `""`.
4. Every validation error, especially the slot ceiling at exactly 17.
5. Determinism: encoding twice yields byte-identical output.
6. Geometry round-trip through the existing reader — paint is dropped, geometry
   survives intact.
7. Mutation check: the encoder is a lookup table, so verify the tests fail if
   the nibble order is reversed or the `C` suffix dropped.

Manual acceptance gate — required, and distinct from the Gate above, which
precedes implementation:

> Generate a painted file and open it in both slicers. Confirm the painted
> regions appear in each, on the correct triangles.

## Follow-on work

1. tapmag: emit its plate as a `PaintedMesh` and delete the per-color-solid
   modeling it would otherwise need.
2. Reading paint back, if a use case appears.
3. Slots above 16, which would require choosing a slicer and writing separate
   files.
