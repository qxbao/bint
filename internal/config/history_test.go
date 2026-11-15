package config

import (
	"strings"
	"testing"
)

func TestHistoryDefaultsAndValidate(t *testing.T) {
	d := Default()
	if d.Context.History.Enabled || d.Context.History.MaxRuns != 10 {
		t.Fatalf("mặc định context.history = %+v, muốn enabled false, max_runs 10", d.Context.History)
	}
	for _, n := range []int{0, -1, 51} {
		bad := Default()
		bad.Context.History.MaxRuns = n
		if err := bad.Validate(); err == nil || !strings.Contains(err.Error(), "context.history.max_runs") {
			t.Fatalf("max_runs %d: muốn lỗi context.history.max_runs, được %v", n, err)
		}
	}
	for _, n := range []int{1, 50} {
		ok := Default()
		ok.Context.History.MaxRuns = n
		if err := ok.Validate(); err != nil {
			t.Fatalf("max_runs %d phải hợp lệ: %v", n, err)
		}
	}
}
