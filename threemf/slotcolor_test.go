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

// TestSlotColorPaletteOrderIsDeterministic guards the sort.Ints(slots) call in
// slotColorPalette. Without it, Go's randomized map iteration order would let
// the palette come out in a different order on different runs, and 3MF output
// would not be byte-reproducible.
//
// It uses three slots with three *distinct* colors so the palette order is
// exactly the slot visit order: any unsorted visit order produces a different
// palette than sorted order. A single call to slotColorPalette is not enough
// to catch a missing sort, since Go's randomized map iteration could still
// happen to visit the slots in ascending order by chance (1 in 6 for 3
// slots). Looping 100 times drives the chance of never observing an
// unsorted iteration down to a negligible ((1/6)^100), so if the sort is
// removed this test will fail on close to its first run, essentially every
// time.
func TestSlotColorPaletteOrderIsDeterministic(t *testing.T) {
	m := map[int]string{1: "#FF0000", 2: "#00FF00", 3: "#0000FF"}
	want := []string{"#FF0000FF", "#00FF00FF", "#0000FFFF"}

	for i := 0; i < 100; i++ {
		palette, _ := slotColorPalette(m)
		if len(palette) != len(want) {
			t.Fatalf("iteration %d: palette = %v, want %v", i, palette, want)
		}
		for j := range want {
			if palette[j] != want[j] {
				t.Fatalf("iteration %d: palette = %v, want %v (slot visit order was not ascending)", i, palette, want)
			}
		}
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
