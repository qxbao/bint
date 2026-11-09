package runlog

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bint/internal/spool"
	"bint/internal/testutil"
)

func TestSignalsAndLabel(t *testing.T) {
	ctx := context.Background()
	s, sp := setup(t)
	t0 := time.Now().UTC().Add(-time.Hour)
	evs := []spool.Event{
		ev("e1", spool.SessionStarted, "S", t0, nil),
		ev("e2", spool.TestCompleted, "S", t0.Add(time.Minute), map[string]any{"ok": false, "command": "go test ./...", "confirmed": true}),
		ev("e3", spool.TestCompleted, "S", t0.Add(2*time.Minute), map[string]any{"ok": true, "command": "go test ./...", "confirmed": true}),
		ev("e4", spool.ContextRetrieved, "S", t0.Add(3*time.Minute), map[string]any{"channel": "hook:path", "ids": []any{"A", "B"}, "labels": map[string]any{"A": "rule"}}),
		ev("e5", spool.KnowledgeCited, "S", t0.Add(4*time.Minute), map[string]any{"ids": []any{"A"}}),
		ev("e6", spool.SessionEnded, "S", t0.Add(5*time.Minute), nil),
	}
	if _, err := spool.Write(sp, evs...); err != nil {
		t.Fatal(err)
	}
	if _, err := Ingest(ctx, s, sp, time.Now(), 0); err != nil {
		t.Fatal(err)
	}
	var run string
	s.DB.QueryRow(`SELECT run_id FROM runs`).Scan(&run)
	sigs, err := RunSignals(ctx, s.DB, run)
	if err != nil || len(sigs) != 2 {
		t.Fatalf("%+v %v", sigs, err)
	}
	if l, why := Label(sigs); l != LabelSuccess {
		t.Fatalf("lần kiểm thử cuối qua → success: %s %s", l, why)
	}
	var u, c int
	s.DB.QueryRow(`SELECT COUNT(*) FROM context_usages WHERE run_id = ?`, run).Scan(&u)
	s.DB.QueryRow(`SELECT COUNT(*) FROM knowledge_citations WHERE run_id = ? AND item_id = 'A'`, run).Scan(&c)
	if u != 2 || c != 1 {
		t.Fatalf("usages=%d citations=%d", u, c)
	}
	// Người chấm bad ghi đè kiểm thử; chấm lại good ghi đè bad.
	rate := func(id, r string) {
		spool.Write(sp, spool.Event{SchemaVersion: 1, EventID: id, Type: spool.RunRated, Source: "cli", Timestamp: time.Now().UTC(),
			RunID: run, Payload: map[string]any{"rating": r}})
		Ingest(ctx, s, sp, time.Now(), 0)
	}
	rate("r1", "bad")
	sigs, _ = RunSignals(ctx, s.DB, run)
	if l, _ := Label(sigs); l != LabelFailure {
		t.Fatal(l)
	}
	rate("r2", "good")
	sigs, _ = RunSignals(ctx, s.DB, run)
	if l, _ := Label(sigs); l != LabelSuccess || len(sigs) != 3 {
		t.Fatalf("%s %+v", l, sigs)
	}
	// Chỉ có tín hiệu yếu → unlabeled.
	if l, _ := Label([]Signal{{Kind: SignalCommitted, Positive: true, Strength: "weak"}}); l != LabelUnlabeled {
		t.Fatal(l)
	}
	// Kiểm thử chưa được xác nhận (medium) không đủ gán nhãn.
	if l, _ := Label([]Signal{{Kind: SignalTests, Positive: true, Strength: "medium"}}); l != LabelUnlabeled {
		t.Fatal(l)
	}
}

func TestScanGitCommitAndRevert(t *testing.T) {
	ctx := context.Background()
	s, sp := setup(t)
	repo := testutil.NewRepo(t, map[string]string{"a.go": "package a\n"})
	// Commit "init" phải có trước lượt chạy (git --since tính theo giây).
	time.Sleep(2100 * time.Millisecond)
	start := time.Now().UTC()
	if _, err := spool.Write(sp,
		ev("s1", spool.SessionStarted, "X", start, nil),
		ev("s2", spool.FileModified, "X", start.Add(time.Minute), map[string]any{"path": "a.go"}),
		ev("s3", spool.SessionEnded, "X", start.Add(time.Second), nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := Ingest(ctx, s, sp, time.Now(), 0); err != nil {
		t.Fatal(err)
	}
	var run string
	s.DB.QueryRow(`SELECT run_id FROM runs`).Scan(&run)
	s.DB.Exec(`UPDATE runs SET worktree = ? WHERE run_id = ?`, repo, run)
	os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n\nvar X = 1\n"), 0o644)
	testutil.Git(t, repo, "commit", "-qam", "thêm X")
	res, err := ScanGit(ctx, s.DB, repo, ScanOptions{})
	if err != nil || res.Committed != 1 || res.Reverted != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	sigs, _ := RunSignals(ctx, s.DB, run)
	if l, _ := Label(sigs); l != LabelUnlabeled {
		t.Fatalf("commit là tín hiệu yếu: %s", l)
	}
	testutil.Git(t, repo, "revert", "--no-edit", "HEAD")
	res, err = ScanGit(ctx, s.DB, repo, ScanOptions{})
	if err != nil || res.Reverted != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	sigs, _ = RunSignals(ctx, s.DB, run)
	if l, why := Label(sigs); l != LabelFailure {
		t.Fatalf("%s %s", l, why)
	}
	// Quét lại không tạo trùng.
	ScanGit(ctx, s.DB, repo, ScanOptions{})
	if sigs2, _ := RunSignals(ctx, s.DB, run); len(sigs2) != len(sigs) {
		t.Fatalf("trùng: %d vs %d", len(sigs2), len(sigs))
	}
}
