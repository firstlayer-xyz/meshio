package meshio

import "testing"

func approx(a, b float32) bool { d := a - b; return d < 1e-4 && d > -1e-4 }

func TestParseAffine_Identity(t *testing.T) {
	m, err := parseAffine("1 0 0 0 1 0 0 0 1 0 0 0")
	if err != nil {
		t.Fatal(err)
	}
	x, y, z := m.apply(3, 4, 5)
	if !approx(x, 3) || !approx(y, 4) || !approx(z, 5) {
		t.Errorf("identity.apply = %v,%v,%v want 3,4,5", x, y, z)
	}
}

func TestParseAffine_Translation(t *testing.T) {
	m, err := parseAffine("1 0 0 0 1 0 0 0 1 10 20 30")
	if err != nil {
		t.Fatal(err)
	}
	x, y, z := m.apply(1, 2, 3)
	if !approx(x, 11) || !approx(y, 22) || !approx(z, 33) {
		t.Errorf("translate.apply = %v,%v,%v want 11,22,33", x, y, z)
	}
}

func TestParseAffine_Bad(t *testing.T) {
	if _, err := parseAffine("1 2 3"); err == nil {
		t.Error("want error for <12 values")
	}
}

// mul(A,B).apply(v) must equal A.apply(B.apply(v)) — order matters.
func TestAffineMul_Order(t *testing.T) {
	scale2 := affine{2, 0, 0, 0, 2, 0, 0, 0, 2, 0, 0, 0}
	transX := affine{1, 0, 0, 0, 1, 0, 0, 0, 1, 1, 0, 0}
	x, _, _ := scale2.mul(transX).apply(1, 0, 0) // translate(1->2) then scale(->4)
	if !approx(x, 4) {
		t.Errorf("scale2∘transX .x = %v want 4", x)
	}
	x2, _, _ := transX.mul(scale2).apply(1, 0, 0) // scale(1->2) then translate(->3)
	if !approx(x2, 3) {
		t.Errorf("transX∘scale2 .x = %v want 3", x2)
	}
}
