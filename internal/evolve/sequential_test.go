package evolve

import (
	"math"
	"slices"
	"testing"

	"bint/internal/eval"
)

func pairedOfStats(mean, sd float64, n int) eval.Paired {
	return eval.Paired{Arm: eval.ArmCandidate, Baseline: eval.ArmBase,
		PassDiff: eval.Stat{Mean: mean, CI: eval.CI{Lo: mean - 0.1, Hi: mean + 0.1}, N: n}, SDDiff: &sd}
}

func TestSeqLooksAndBoundary(t *testing.T) {
	for _, tc := range []struct {
		r    int
		want []int
	}{{1, []int{1}}, {3, []int{1, 2, 3}}, {4, []int{1, 2, 3, 4}}, {8, []int{2, 4, 6, 8}}, {6, []int{2, 3, 5, 6}}} {
		if got := SeqLooks(tc.r); !slices.Equal(got, tc.want) {
			t.Errorf("SeqLooks(%d) = %v, mong đợi %v", tc.r, got, tc.want)
		}
	}
	// Biên giảm dần, điểm cuối bằng hằng số hiệu chỉnh c_K.
	if z1, z4 := SeqBoundary(4, 1), SeqBoundary(4, 4); math.Abs(z1-2*2.024) > 1e-9 || z4 != 2.024 {
		t.Fatalf("z1=%v z4=%v", z1, z4)
	}
	if SeqBoundary(1, 1) != 1.96 {
		t.Fatal("K=1 phải là 1,96")
	}
}

func TestSeqDecideSuperiority(t *testing.T) {
	plan := Plan{Kind: "superiority", Metric: "pass_rate", MinEffect: 0.15}
	// +0,5, SD 0,3, n=25: se=0,06; z1(K=4)=4,05 → cận dưới 0,257 > 0: dừng thắng ngay điểm nhìn 1.
	if st := SeqDecide(pairedOfStats(0.5, 0.3, 25), plan, 1, 4); !st.Stop || st.Verdict != VerdictSupported {
		t.Fatalf("%+v", st)
	}
	// +0,2, SD 0,4, n=25: se=0,08; z1=4,05 → cận dưới −0,12: chưa dừng; z3=2,34 → +0,013 > 0: dừng ở điểm nhìn 3.
	p := pairedOfStats(0.2, 0.4, 25)
	if st := SeqDecide(p, plan, 1, 4); st.Stop {
		t.Fatalf("điểm nhìn 1 không được dừng: %+v", st)
	}
	if st := SeqDecide(p, plan, 3, 4); !st.Stop || st.Verdict != VerdictSupported {
		t.Fatalf("điểm nhìn 3: %+v", st)
	}
	// Ít tác vụ → không dừng sớm dù chênh lệch lớn.
	if st := SeqDecide(pairedOfStats(1, 0.01, 3), plan, 1, 4); st.Stop {
		t.Fatalf("n=3 không được dừng sớm: %+v", st)
	}
}

func TestSeqDecideFutility(t *testing.T) {
	plan := Plan{Kind: "superiority", Metric: "pass_rate", MinEffect: 0.15}
	// +0,01, SD 0,2, n=40: se=0,0316; cận trên CI95 = 0,072 < 0,15 → futility tại điểm nhìn 1.
	st := SeqDecide(pairedOfStats(0.01, 0.2, 40), plan, 1, 4)
	if !st.Stop || st.Verdict != VerdictNotSupported {
		t.Fatalf("%+v", st)
	}
	// Cận trên còn trên min_effect → tiếp tục.
	if st := SeqDecide(pairedOfStats(0.1, 0.4, 20), plan, 1, 4); st.Stop {
		t.Fatalf("chưa được dừng: %+v", st)
	}
	// Điểm cuối luôn kết luận: không thắng → not_supported.
	if st := SeqDecide(pairedOfStats(0.1, 0.4, 20), plan, 4, 4); !st.Stop || st.Verdict != VerdictNotSupported {
		t.Fatalf("%+v", st)
	}
}

func TestSeqDecideNonInferiority(t *testing.T) {
	plan := Plan{Kind: "non_inferiority", Metric: "pass_rate", Margin: 0.10, MinEffect: 0.15}
	// 0, SD 0,1, n=25: se=0,02; z1=4,05 → cận dưới −0,081 > −0,10: không kém, dừng supported ngay.
	if st := SeqDecide(pairedOfStats(0, 0.1, 25), plan, 1, 4); !st.Stop || st.Verdict != VerdictSupported {
		t.Fatalf("%+v", st)
	}
	// −0,3, SD 0,1, n=25: cận trên CI95 = −0,26 < −0,10 → futility (chắc chắn kém).
	if st := SeqDecide(pairedOfStats(-0.3, 0.1, 25), plan, 1, 4); !st.Stop || st.Verdict != VerdictNotSupported {
		t.Fatalf("%+v", st)
	}
	// −0,02, SD 0,4, n=25: se=0,08 → chưa đủ để kết luận ở điểm nhìn 1.
	if st := SeqDecide(pairedOfStats(-0.02, 0.4, 25), plan, 1, 4); st.Stop {
		t.Fatalf("%+v", st)
	}
}

// Đợt dừng sớm theo kế hoạch (chỉ có ghi chú info "stopped_early") không bị chặn như đợt
// chạy dở ("incomplete" là cảnh báo chặn).
func TestComputeVerdictSeqNotBlocked(t *testing.T) {
	plan := Plan{Kind: "superiority", Metric: "pass_rate", MinEffect: 0.15}
	p := pairedOfStats(0.5, 0.3, 25)
	mk := func(d eval.Diagnostic) eval.Report {
		return eval.Report{Paired: []eval.Paired{p}, Diagnostics: []eval.Diagnostic{d}}
	}
	stopped := mk(eval.Diagnostic{Level: "info", Kind: "stopped_early", Detail: "dừng sớm"})
	v := ComputeVerdictSeq(stopped, plan, SeqInfo{K: 4, J: 1, Repeat: 2, Repeats: 8})
	if v.Verdict != VerdictSupported {
		t.Fatalf("%+v", v)
	}
	notes := ""
	for _, n := range v.Notes {
		notes += n
	}
	if want := "dừng sớm tại điểm nhìn 1/4 (lần lặp 2/8)"; !contains(notes, want) {
		t.Fatalf("thiếu %q trong %q", want, notes)
	}
	incomplete := mk(eval.Diagnostic{Level: "warn", Kind: "incomplete", Detail: "x"})
	if v := ComputeVerdict(incomplete, plan); v.Verdict != VerdictInconclusive {
		t.Fatalf("đợt chạy dở phải bị chặn: %+v", v)
	}
	// Điểm nhìn cuối: ghi số điểm nhìn.
	vf := ComputeVerdictSeq(stopped, plan, SeqInfo{K: 4, J: 4, Repeat: 8, Repeats: 8})
	if vf.Verdict != VerdictSupported || len(vf.Notes) == 0 {
		t.Fatalf("%+v", vf)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
