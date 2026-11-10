package eval

import (
	"math"
	"testing"
)

func TestPlanProbeFeasible(t *testing.T) {
	p := PlanProbe(PlanInput{Available: 30, Effect: 0.2, SigmaB: 0.15, W: 0.48, CostPerRun: 0.2, MaxRepeats: 8})
	if !p.Feasible {
		t.Fatalf("mong đợi khả thi: %+v", p)
	}
	// R=3 cần 36 tác vụ > 30 nên R nhỏ nhất khả thi là 4; chi phí n·R thấp nhất ở đó.
	if p.Repeats < 4 || p.Cases != 28 {
		t.Fatalf("R=%d n=%d, mong đợi R>=4, n=28", p.Repeats, p.Cases)
	}
	if p.MaxRuns != p.Cases*2*p.Repeats || math.Abs(p.MaxCost-0.2*float64(p.MaxRuns)) > 1e-9 {
		t.Fatalf("số lượt/chi phí sai: %+v", p)
	}
}

func TestPlanProbeInfeasible(t *testing.T) {
	p := PlanProbe(PlanInput{Available: 30, Effect: 0.1, SigmaB: 0.15, W: 0.48, MaxRepeats: 8})
	if p.Feasible {
		t.Fatalf("mong đợi không khả thi: %+v", p)
	}
	// δ nhỏ nhất = (z.975+z.8)·σ(8)/√30 ≈ 0.147
	if math.Abs(p.MinDetectable-0.1469) > 0.002 || p.Repeats != 8 {
		t.Fatalf("δ nhỏ nhất %.4f, R=%d", p.MinDetectable, p.Repeats)
	}
}

func TestPlanProbeFixedRepeats(t *testing.T) {
	p := PlanProbe(PlanInput{Available: 100, Effect: 0.2, SigmaB: 0.15, W: 0.48, MaxRepeats: 8, FixedRepeats: 2})
	if !p.Feasible || p.Repeats != 2 || p.Cases != 52 {
		t.Fatalf("%+v", p)
	}
}

func TestEstimateW(t *testing.T) {
	// k=1,m=2 → 2·1·1/(2·1) = 1,0 ; k=0,m=3 → 0 ; tác vụ m=1 bị bỏ.
	w, n := EstimateW([]PassCount{{K: 1, M: 2}, {K: 0, M: 3}, {K: 1, M: 1}})
	if n != 2 || math.Abs(w-0.5) > 1e-9 {
		t.Fatalf("W=%v n=%d", w, n)
	}
	w, n = EstimateW(nil)
	if n != 0 || w != DefaultW {
		t.Fatalf("khi chưa có dữ liệu: W=%v n=%d", w, n)
	}
	if w, _ := EstimateW([]PassCount{{K: 2, M: 4}}); math.Abs(w-2*2.0*2/(4*3)) > 1e-9 {
		t.Fatalf("W=%v", w)
	}
}

func TestEstimateSigmaB(t *testing.T) {
	d := []float64{0.2, -0.2, 0.2, -0.2, 0.2, -0.2}
	v := SD(d)
	want := math.Sqrt(v*v - 0.01)
	if got := EstimateSigmaB(d, 0.04, 4); math.Abs(got-want) > 1e-9 {
		t.Fatalf("σ_b=%v want %v", got, want)
	}
	if got := EstimateSigmaB([]float64{0.1, 0.1, 0.1}, 0.5, 1); got != 0 {
		t.Fatalf("phải chặn dưới ở 0, được %v", got)
	}
}
