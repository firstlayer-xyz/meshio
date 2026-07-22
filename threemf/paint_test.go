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
