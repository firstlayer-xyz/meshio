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
