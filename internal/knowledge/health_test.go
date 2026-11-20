package knowledge

import "testing"

func TestHealthOf(t *testing.T) {
	cases := []struct {
		name string
		r    Result
		want Health
	}{
		{"sạch", Result{}, HealthOK},
		{"cảnh báo", Result{Findings: []Finding{{Level: LevelWarn, Code: CodeRelocated}}}, HealthWarn},
		{"lỗi", Result{Findings: []Finding{{Level: LevelError, Code: CodeFingerprint}}}, HealthError},
		{"lỗi thắng cảnh báo", Result{Findings: []Finding{{Level: LevelWarn}, {Level: LevelError}}}, HealthError},
	}
	for _, c := range cases {
		if got := HealthOf(c.r); got != c.want {
			t.Errorf("%s: HealthOf = %v, muốn %v", c.name, got, c.want)
		}
	}
}
