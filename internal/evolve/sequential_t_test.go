package evolve

import (
	"math"
	"testing"
)

func TestTFromZ(t *testing.T) {
	// Phân vị 0,975 của Student-t: ν=4 → 2,776; ν=10 → 2,228; ν=29 → 2,045.
	for _, c := range []struct{ nu, want float64 }{{4, 2.776}, {10, 2.228}, {29, 2.045}, {1000, 1.962}} {
		if got := tFromZ(1.959964, c.nu); math.Abs(got-c.want)/c.want > 0.01 {
			t.Errorf("ν=%v: %.3f, muốn %.3f", c.nu, got, c.want)
		}
	}
}
