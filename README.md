# meshio

A dependency-free Go library for reading and writing triangle meshes: **3MF**, **STL**, and **OBJ**, with per-face color.

```go
mesh, err := meshio.Read("model.3mf")   // format from extension
err = threemf.Write("out.3mf", mesh)

// or per format
mesh, err = stl.Decode(r)
err = obj.Encode(w, mesh, mtlWriter, "model.mtl") // mtlName is required when mtlWriter != nil
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

The 3MF reader resolves the production extension — objects split across
`3D/Objects/*.model`, components, and build-item transforms are flattened into a
single mesh.

---

# Color

**Read this before trying to produce a multi-color print.** Color in 3MF has two
completely separate channels, and using the wrong one is the most common way to
get a file that *looks* right and *slices* wrong.

## The two channels

| | Display color | Print assignment |
|---|---|---|
| Answers | "what color is this triangle?" | "which filament prints this?" |
| Value | RGB hex | a slot index (1, 2, 3…) |
| Lives in | `<m:colorgroup>` + `pid`/`p1` on triangles | slicer-specific metadata |
| Read by | viewers, 3D Builder, three.js | the slicer's toolpath generator |

**Slicers do not think in RGB.** They think in filament slots; the actual color
comes from the filament profile loaded in that slot. A 3MF that only carries RGB
tells the slicer what to *draw*, never what to *print*.

## What meshio writes today

`threemf.Encode` writes the **display** channel only — the spec-correct core
material extension:

```xml
<m:colorgroup id="2">
  <m:color color="#C81E1EFF" />
</m:colorgroup>
...
<triangle v1="0" v2="21" v3="1" pid="2" p1="0" p2="0" p3="0" />
```

Consequences:

- **Viewers**: correct. Colors show everywhere.
- **Bambu Studio** (observed): imports and *renders* the colors correctly, but
  never converts them into filament assignments. The model slices as a single
  material. This is the trap — the preview looks finished.
- **PrusaSlicer** (not verified here): its importer reads `basematerials` for
  whole-volume material assignment, but is not expected to turn per-triangle
  colorgroup references into MMU paint.

## What the slicers actually consume

Two mechanisms exist. Both express *slot*, not color.

### 1. Per-part assignment (whole sub-mesh → one filament)

The object is assembled from several sub-meshes; each is bound to a slot. This is
what most multi-color models on MakerWorld/Printables actually use.

Bambu Studio, verified against real project files:

```xml
<!-- 3D/3dmodel.model — a container object referencing each part -->
<model xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02"
       xmlns:p="http://schemas.microsoft.com/3dmanufacturing/production/2015/06"
       xmlns:BambuStudio="http://schemas.bambulab.com/package/2021"
       requiredextensions="p">
 <resources>
  <object id="155" p:UUID="..." type="model">
   <components>
    <component p:path="/3D/Objects/object_155.model" objectid="118" p:UUID="..." transform="..."/>
    <component p:path="/3D/Objects/object_155.model" objectid="119" p:UUID="..." transform="..."/>
   </components>
  </object>
 </resources>
</model>
```

```xml
<!-- Metadata/model_settings.config — the slot assignment -->
<object id="155">
  <metadata key="name" value="Derpy Cat Head Final 2.obj"/>
  <metadata key="extruder" value="2"/>          <!-- object default -->
  <part id="118" subtype="normal_part">          <!-- part id == component objectid -->
    <metadata key="name" value="..._1"/>
    <mesh_stat .../>
  </part>                                        <!-- no extruder → inherits 2 -->
  <part id="119" subtype="normal_part">
    <metadata key="extruder" value="7"/>         <!-- overrides the default -->
  </part>
</object>
```

Each referenced part file is related from `3D/_rels/3dmodel.model.rels`:

```xml
<Relationship Target="/3D/Objects/object_155.model" Id="rel-1"
  Type="http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel"/>
```

PrusaSlicer expresses the same idea differently — one object, triangle-range
volumes in `Metadata/Slic3r_PE_model.config`:

```xml
<object id="1">
  <volume firstid="0" lastid="1759">
    <metadata type="volume" key="extruder" value="2"/>
  </volume>
</object>
```

### 2. Per-triangle painting

Keeps the object as one solid and paints individual triangles — what the paint
brush in the slicer UI produces.

- PrusaSlicer: `slic3rpe:mmu_segmentation="..."` on `<triangle>`, namespace
  `http://schemas.slic3r.org/3mf/2017/06` (same family as `slic3rpe:custom_supports`
  and `slic3rpe:custom_seam`).
- Bambu Studio / Orca: `paint_color="..."` on `<triangle>`.

The attribute value encodes a recursive triangle-subdivision tree with a slot per
leaf, so a single triangle can carry several colors. **meshio does not write
these, and the encoding is not documented here because it has not been verified
against a real painted file.** Do not implement it from guesswork; produce a
painted sample from the slicer and decode it first.

## Slot count and geometry gotchas

**Palette size is bounded by slots.** An AMS is 4 filaments (16 with four units).
A palette larger than the available slots cannot print as-authored — reduce the
palette before export, not after.

**Splitting a solid by face color usually does not produce printable parts.** If
you color the top surface of a closed slab and then split by color, the non-base
colors come out as *flat, zero-thickness patches* and the remainder is an open
shell. Neither slices. Per-part color requires each part to be a genuine solid —
e.g. a base slab plus extruded tiles per color — which is a modeling decision
that belongs upstream of meshio.

This is why per-triangle painting (mechanism 2) is attractive for surface color:
it needs no geometry change at all.

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

## Status

| Capability | State |
|---|---|
| Per-face RGB → core colorgroup | works |
| Read production-extension / multi-part 3MF | works (geometry flattened to one mesh) |
| Multi-part object with per-part filament slot | works — `threemf.WriteBambu` |
| Per-triangle paint (`paint_color` / `mmu_segmentation`) | not implemented, encoding unverified |
| PrusaSlicer multi-material output | not implemented, needs a verified sample |

### Provenance

The Bambu structures above were read out of real Bambu Studio project files (a
multi-part model and an AMS multi-color model), not from documentation. The
PrusaSlicer structures are stated from knowledge of its importer and have **not**
been verified against a sample here — treat them as a starting point and confirm
before building on them.
