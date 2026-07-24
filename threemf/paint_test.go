package threemf

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
		{5, "2C"},  // (5-3)=2
		{6, "3C"},  // (6-3)=3
		{7, "4C"},  // (7-3)=4
		{8, "5C"},  // (8-3)=5
		{9, "6C"},  // (9-3)=6
		{10, "7C"}, // (10-3)=7
		{11, "8C"}, // (11-3)=8
		{12, "9C"}, // (12-3)=9
		{13, "AC"}, // (13-3)=10=A
		{14, "BC"}, // (14-3)=11=B
		{15, "CC"}, // (15-3)=12=C
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

// paintString documents that it panics above maxPaintSlot, and that
// callers must validate first because reaching that path is a bug, not bad
// input. Nothing previously exercised either side of that boundary: a stray
// off-by-one in the switch (slot < maxPaintSlot instead of slot <=
// maxPaintSlot) would make the last valid slot panic, and a deleted default
// arm would make the first invalid slot silently return "" -- indistinguishable
// from "unpainted", which is exactly the kind of silent-wrong-output bug this
// feature exists to prevent.
func TestPaintStringBoundary(t *testing.T) {
	t.Run("maxPaintSlot is still valid", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("paintString(%d) panicked: %v", maxPaintSlot, r)
			}
		}()
		if got, want := paintString(maxPaintSlot), "DC"; got != want {
			t.Errorf("paintString(%d) = %q, want %q", maxPaintSlot, got, want)
		}
	})

	t.Run("maxPaintSlot+1 panics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("paintString(%d) did not panic", maxPaintSlot+1)
			}
		}()
		paintString(maxPaintSlot + 1)
	})
}
