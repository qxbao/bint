package runlog

import (
	"context"
	"testing"
	"time"
)

func TestDailyCountsFillsGapsAndCountsFailures(t *testing.T) {
	ctx := context.Background()
	s, _ := setup(t)
	now := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	ins := func(id, status string, at time.Time) {
		t.Helper()
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO runs (run_id, status, started_at) VALUES (?, ?, ?)`, id, status,
			at.Format("2006-01-02T15:04:05.000Z")); err != nil {
			t.Fatal(err)
		}
	}
	ins("a", "completed", now.Add(-1*time.Hour))
	ins("b", "failed", now.Add(-2*time.Hour))
	ins("c", "interrupted", now.AddDate(0, 0, -2))
	ins("d", "completed", now.AddDate(0, 0, -30)) // ngoài cửa sổ
	days, err := DailyCounts(ctx, s.DB, 3, now)
	if err != nil {
		t.Fatal(err)
	}
	want := []Day{{"2026-11-19", 1, 1}, {"2026-11-20", 0, 0}, {"2026-11-21", 2, 1}}
	if len(days) != len(want) {
		t.Fatalf("%+v", days)
	}
	for i := range want {
		if days[i] != want[i] {
			t.Errorf("ngày %d: %+v, muốn %+v", i, days[i], want[i])
		}
	}
}
