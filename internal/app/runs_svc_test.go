package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"bint/internal/config"
	"bint/internal/spool"
	"bint/internal/testutil"
	"bint/internal/ulid"
)

// runsRepo dựng repo đã init và ghi một phiên vào spool (chưa nạp).
func runsRepo(t *testing.T) (string, string) {
	t.Helper()
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n", "a.go": "package a\n\nfunc A() {}\n"})
	testutil.Git(t, root, "config", "user.name", "Rev")
	testutil.Git(t, root, "config", "user.email", "rev@example.com")
	mustInit(t, InitOptions{Dir: root, Hooks: no(), NoSkills: true})
	ws, err := OpenWorkspace(context.Background(), root, true)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-time.Hour)
	ev := func(typ string, ts time.Time, p map[string]any) spool.Event {
		return spool.Event{EventID: ulid.New(), Type: typ, Source: "hook:claude", Timestamp: ts, Harness: "claude", SessionID: "sess1", Payload: p}
	}
	if _, err := spool.Write(ws.Paths.Spool,
		ev(spool.SessionStarted, at, map[string]any{"model": "claude-opus-5-5"}),
		ev(spool.FileModified, at.Add(time.Minute), map[string]any{"path": "a.go"}),
		ev(spool.SessionEnded, at.Add(2*time.Minute), map[string]any{"reason": "exit"})); err != nil {
		t.Fatal(err)
	}
	return root, ws.Paths.Spool
}

func TestRunsOptionsValidateAndDefaults(t *testing.T) {
	cfg := config.Default()
	if d := RunsListDefaults(cfg); d.Limit != 20 {
		t.Errorf("limit mặc định: %+v", d)
	}
	if d := RunsScanDefaults(cfg); d.Days != 30 || d.RevertDays != 14 {
		t.Errorf("scan mặc định: %+v", d)
	}
	cases := []struct {
		name string
		err  error
		want bool // true = UsageError
	}{
		{"show thiếu ID", RunsShowOptions{}.Validate(), true},
		{"show ok", RunsShowOptions{Ref: "run_x"}.Validate(), false},
		{"rate thiếu ID", RunsRateOptions{Rating: "good"}.Validate(), true},
		{"rate điểm lạ", RunsRateOptions{Ref: "run_x", Rating: "meh"}.Validate(), true},
		{"rate điểm rỗng", RunsRateOptions{Ref: "run_x"}.Validate(), true},
		{"rate good", RunsRateOptions{Ref: "run_x", Rating: "good"}.Validate(), false},
		{"rate BAD hoa", RunsRateOptions{Ref: "run_x", Rating: "BAD"}.Validate(), false},
	}
	for _, tc := range cases {
		if got := IsUsage(tc.err); got != tc.want {
			t.Errorf("%s: UsageError=%v, mong %v (%v)", tc.name, got, tc.want, tc.err)
		}
	}
}

func TestOpenRunsNoIngestLeavesSpool(t *testing.T) {
	root, _ := runsRepo(t)
	ctx := context.Background()
	ro, err := OpenRuns(ctx, root, false)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := ListRuns(ctx, ro, RunsListDefaults(config.Default()))
	ro.Close()
	if err != nil || len(rows) != 0 {
		t.Fatalf("không nạp spool thì chưa có lượt: %v %v", err, rows)
	}
	rw, err := OpenRuns(ctx, root, true)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()
	if rw.Ingest.Events != 3 {
		t.Fatalf("nạp spool: %+v", rw.Ingest)
	}
	res, err := IngestEvents(ctx, rw, EventsIngestOptions{})
	if err != nil || res.Events != 3 {
		t.Fatalf("IngestEvents: %v %+v", err, res)
	}
	rows, err = ListRuns(ctx, rw, RunsListDefaults(config.Default()))
	if err != nil || len(rows) != 1 || rows[0].Label == "" {
		t.Fatalf("danh sách: %v %+v", err, rows)
	}
	d, err := ShowRun(ctx, rw, RunsShowOptions{Ref: rows[0].Run.RunID[:12]})
	if err != nil || d.Run.RunID != rows[0].Run.RunID || len(d.Events) != 3 || len(d.Artifacts) != 1 {
		t.Fatalf("chi tiết: %v %+v", err, d)
	}
	if _, err := ShowRun(ctx, rw, RunsShowOptions{}); !IsUsage(err) {
		t.Fatalf("thiếu ID: %v", err)
	}
}

func TestRateRunHumanOnlyAndWritesSignal(t *testing.T) {
	root, _ := runsRepo(t)
	ctx := context.Background()
	rw, err := OpenRuns(ctx, root, true)
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := ListRuns(ctx, rw, RunsListDefaults(config.Default()))
	rw.Close()
	id := rows[0].Run.RunID

	// Tham số sai (mã 2) thắng kiểm chỉ-dành-cho-người, giữ thứ tự của CLI cũ.
	asAgent(t)
	if _, err := RateRun(ctx, root, RunsRateOptions{Ref: id, Rating: "meh"}); !IsUsage(err) {
		t.Fatalf("điểm sai trong phiên agent: %v", err)
	}
	if _, err := RateRun(ctx, root, RunsRateOptions{Ref: id, Rating: "good"}); !errors.Is(err, ErrAgentForbidden) {
		t.Fatalf("CLAUDECODE=1: %v", err)
	}
	asHuman(t)
	res, err := RateRun(ctx, root, RunsRateOptions{Ref: id[:14], Rating: "BAD", Note: "hỏng"})
	if err != nil || res.RunID != id || res.Rating != "bad" {
		t.Fatalf("người chấm: %v %+v", err, res)
	}
	rw, _ = OpenRuns(ctx, root, true)
	defer rw.Close()
	d, err := ShowRun(ctx, rw, RunsShowOptions{Ref: id})
	if err != nil || d.Label != "failure" {
		t.Fatalf("nhãn sau khi chấm bad: %v %q (%s)", err, d.Label, d.Reason)
	}
}

func TestScanRunsOnEmptyHistory(t *testing.T) {
	root, _ := runsRepo(t)
	ctx := context.Background()
	rw, err := OpenRuns(ctx, root, true)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()
	res, err := ScanRuns(ctx, rw, RunsScanDefaults(config.Default()))
	if err != nil || res.Runs != 1 {
		t.Fatalf("quét: %v %+v", err, res)
	}
}
