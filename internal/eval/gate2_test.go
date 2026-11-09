package eval

import "testing"

func pd(arm, base string, mean, lo, hi float64) Paired {
	return Paired{Arm: arm, Baseline: base, PassDiff: Stat{Mean: mean, CI: CI{lo, hi}, N: 20}}
}

func TestDiagnoseGate2Order(t *testing.T) {
	f, tr := false, true
	rows := []AttemptRow{
		{AttemptID: "a1", Arm: ArmBint, TestPassed: &f},
		{AttemptID: "a2", Arm: ArmBint, TestPassed: &tr},
	}
	lowCov := map[string]AttemptContext{"a1": {Ideal: 4, IdealRetrieved: 1, Retrieved: 2}, "a2": {Ideal: 2, IdealRetrieved: 0, Retrieved: 1}}
	highCov := map[string]AttemptContext{"a1": {Ideal: 2, IdealRetrieved: 2, IdealCited: 0, Retrieved: 2}, "a2": {Ideal: 2, IdealRetrieved: 2, Retrieved: 2}}
	cases := []struct {
		name string
		rep  Report
		cu   map[string]AttemptContext
		want string
	}{
		{"measurement", Report{Diagnostics: []Diagnostic{{"warn", "ci_wide", "rộng"}}}, lowCov, CauseMeasurement},
		{"passed", Report{Paired: []Paired{pd(ArmBint, ArmMinimal, 0.1, 0.02, 0.2)}}, lowCov, CausePassed},
		{"no ideal", Report{Paired: []Paired{pd(ArmBint, ArmMinimal, 0, -0.1, 0.1)}}, lowCov, CauseUndetermined},
		{"content", Report{Paired: []Paired{pd(ArmBint, ArmMinimal, 0, -0.1, 0.1), pd(ArmIdeal, ArmMinimal, 0.02, -0.05, 0.1)}}, lowCov, CauseContent},
		{"retrieval", Report{Paired: []Paired{pd(ArmBint, ArmMinimal, 0, -0.1, 0.1), pd(ArmIdeal, ArmMinimal, 0.2, 0.1, 0.3)}}, lowCov, CauseRetrievalMiss},
		{"not applied", Report{Paired: []Paired{pd(ArmBint, ArmMinimal, 0, -0.1, 0.1), pd(ArmIdeal, ArmMinimal, 0.2, 0.1, 0.3)}}, highCov, CauseNotApplied},
	}
	for _, c := range cases {
		g := diagnoseGate2(c.rep, rows, c.cu)
		if g.Cause != c.want {
			t.Errorf("%s: %s (%s) %v", c.name, g.Cause, g.Detail, g.Checks)
		}
	}
	// Áp dụng đủ nhưng bint kém hơn tối giản → nhiễu.
	cited := map[string]AttemptContext{"a1": {Ideal: 2, IdealRetrieved: 2, IdealCited: 2, Retrieved: 2}}
	g := diagnoseGate2(Report{Paired: []Paired{pd(ArmBint, ArmMinimal, -0.05, -0.15, 0.05), pd(ArmIdeal, ArmMinimal, 0.2, 0.1, 0.3)}}, rows, cited)
	if g.Cause != CauseNoise {
		t.Fatalf("%s %v", g.Cause, g.Checks)
	}
}
