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
