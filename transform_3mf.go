package meshio

import (
	"fmt"
	"strconv"
	"strings"
)

// affine is a 3MF transform: a row-major 4x3 matrix stored as 12 floats
// [a b c d e f g h i j k l]. A point (x,y,z) maps (row-vector × 4x4) to:
//
//	x' = a*x + d*y + g*z + j
//	y' = b*x + e*y + h*z + k
//	z' = c*x + f*y + i*z + l
type affine [12]float32

func identityAffine() affine { return affine{1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0} }

func (m affine) apply(x, y, z float32) (float32, float32, float32) {
	return m[0]*x + m[3]*y + m[6]*z + m[9],
		m[1]*x + m[4]*y + m[7]*z + m[10],
		m[2]*x + m[5]*y + m[8]*z + m[11]
}

// mul returns the composition where (m.mul(n)).apply(v) == m.apply(n.apply(v)):
// n applied first (child), then m (parent).
func (m affine) mul(n affine) affine {
	r := func(a affine, i, j int) float32 { return a[i*3+j] } // row i (0..3), col j (0..2)
	var out affine
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			var s float32
			for k := 0; k < 3; k++ {
				s += r(n, i, k) * r(m, k, j)
			}
			out[i*3+j] = s
		}
	}
	for j := 0; j < 3; j++ {
		var s float32
		for k := 0; k < 3; k++ {
			s += r(n, 3, k) * r(m, k, j)
		}
		out[9+j] = s + r(m, 3, j)
	}
	return out
}

func parseAffine(s string) (affine, error) {
	fields := strings.Fields(s)
	if len(fields) != 12 {
		return affine{}, fmt.Errorf("meshio: 3mf transform needs 12 values, got %d", len(fields))
	}
	var m affine
	for i, f := range fields {
		v, err := strconv.ParseFloat(f, 32)
		if err != nil {
			return affine{}, fmt.Errorf("meshio: 3mf transform value %q: %w", f, err)
		}
		m[i] = float32(v)
	}
	return m, nil
}
