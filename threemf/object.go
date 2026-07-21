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
