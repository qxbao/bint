package cli

import (
	"testing"

	"bint/internal/evolve"
)

func TestLatestProbe(t *testing.T) {
	evs := []evolve.Evaluation{
		{ID: "a", Split: "validation", Verdict: "supported", CreatedAt: "2026-01-01T00:00:00Z"},
		{ID: "b", Split: "validation", Verdict: "not_supported", CreatedAt: "2026-01-03T00:00:00Z"},
		{ID: "c", Split: "validation", Verdict: "supported", CreatedAt: "2026-01-04T00:00:00Z", Kind: evolve.EvalRegression},
		{ID: "d", Split: "test", Verdict: "supported", CreatedAt: "2026-01-05T00:00:00Z"},
	}
	if e := latestProbe(evs, "validation", ""); e == nil || e.ID != "b" {
		t.Fatalf("%+v", e)
	}
	// Kết quả trước lần áp dụng gần nhất không còn được tính.
	if e := latestProbe(evs, "validation", "2026-01-03T12:00:00Z"); e != nil {
		t.Fatalf("%+v", e)
	}
	if e := latestProbe(evs, "test", "2026-01-03T12:00:00Z"); e == nil || e.ID != "d" {
		t.Fatalf("%+v", e)
	}
}

func TestLastJSON(t *testing.T) {
	if got := string(lastJSON([]byte("tiến độ...\n[1/2] x\n{\"a\":1}\n"))); got != `{"a":1}` {
		t.Fatalf("%q", got)
	}
	if got := string(lastJSON([]byte("{\"a\":1}"))); got != `{"a":1}` {
		t.Fatalf("%q", got)
	}
}
