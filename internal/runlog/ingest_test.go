package runlog

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"bint/internal/spool"
	"bint/internal/store"
)

func setup(t *testing.T) (*store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	sp := filepath.Join(dir, "spool")
	os.MkdirAll(sp, 0o700)
	s, err := store.Open(context.Background(), filepath.Join(dir, "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, sp
}

func ev(id, typ, session string, ts time.Time, p map[string]any) spool.Event {
	return spool.Event{SchemaVersion: 1, EventID: id, Type: typ, Source: "test",
		Timestamp: ts, Harness: "claude", SessionID: session, Payload: p}
}

func TestIngestLifecycleAndDedup(t *testing.T) {
	ctx := context.Background()
	s, sp := setup(t)
	t0 := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)

	mustWrite := func(evs ...spool.Event) {
		if _, err := spool.Write(sp, evs...); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(ev("e1", spool.SessionStarted, "S1", t0, map[string]any{"model": "m1", "git_head": "abc", "git_branch": "main"}))
	mustWrite(ev("e2", spool.FileModified, "S1", t0.Add(time.Second), map[string]any{"path": "a.go"}))
	mustWrite(ev("e3", spool.FileModified, "S1", t0.Add(2*time.Second), map[string]any{"path": "a.go"}))
	mustWrite(ev("e4", spool.TestCompleted, "S1", t0.Add(3*time.Second), map[string]any{"command": "go test ./...", "ok": false}))
	mustWrite(ev("e5", spool.TestCompleted, "S1", t0.Add(4*time.Second), map[string]any{"command": "go test ./...", "ok": true}))
	mustWrite(ev("e6", spool.SessionEnded, "S1", t0.Add(10*time.Second), map[string]any{"reason": "other"}))
	// Trùng lặp (ví dụ hook được cài ở hai phạm vi).
	mustWrite(ev("e2", spool.FileModified, "S1", t0.Add(time.Second), map[string]any{"path": "a.go"}))

	res, err := Ingest(ctx, s, sp, t0.Add(time.Minute), DefaultStaleAfter)
	if err != nil {
		t.Fatal(err)
	}
	if res.Events != 6 || res.Duplicates != 1 || res.Files != 7 {
		t.Fatalf("result = %+v", res)
	}
	if left, _ := spool.Pending(sp); len(left) != 0 {
		t.Fatalf("spool not drained: %v", left)
	}
	runs, err := ListRuns(ctx, s.DB, 10)
	if err != nil || len(runs) != 1 {
		t.Fatalf("runs = %v, err = %v", runs, err)
	}
	r := runs[0]
	if r.Status != "completed" || r.DurationMS.Int64 != 10000 || r.Model.String != "m1" ||
		r.GitBaseCommit.String != "abc" || r.EndReason.String != "other" {
		t.Fatalf("run = %+v", r)
	}
	if r.FilesModified != 1 || r.TestsOK != 1 || r.TestsFailed != 1 || r.Events != 6 {
		t.Fatalf("aggregates = %+v", r)
	}
	arts, _ := RunArtifacts(ctx, s.DB, r.RunID)
	if len(arts) != 2 || arts[0].Path != "a.go" || arts[0].Count != 2 {
		t.Fatalf("artifacts = %+v", arts)
	}

	// Nạp lại lần nữa không làm gì.
	res, _ = Ingest(ctx, s, sp, t0.Add(time.Minute), DefaultStaleAfter)
	if res.Files != 0 || res.Events != 0 {
		t.Fatalf("second ingest = %+v", res)
	}

	// Resume: session.started mới hơn đưa lượt chạy về running, giữ started_at cũ.
	mustWrite(ev("e7", spool.SessionStarted, "S1", t0.Add(time.Hour), map[string]any{"source": "resume"}))
	Ingest(ctx, s, sp, t0.Add(time.Hour), DefaultStaleAfter)
	r, _ = GetRun(ctx, s.DB, r.RunID[:8])
	if r.Status != "running" || r.StartedAt.String != store.FormatTime(t0) || r.FinishedAt.Valid {
		t.Fatalf("after resume = %+v", r)
	}
}

func TestOutOfOrderEnd(t *testing.T) {
	ctx := context.Background()
	s, sp := setup(t)
	t0 := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	// Sự kiện kết thúc được nạp trước sự kiện bắt đầu.
	spool.Write(sp, ev("end", spool.SessionEnded, "S", t0.Add(5*time.Second), nil))
	Ingest(ctx, s, sp, t0, 0)
	spool.Write(sp, ev("start", spool.SessionStarted, "S", t0, nil))
	Ingest(ctx, s, sp, t0, 0)
	runs, _ := ListRuns(ctx, s.DB, 10)
	if len(runs) != 1 || runs[0].Status != "completed" || runs[0].StartedAt.String != store.FormatTime(t0) {
		t.Fatalf("runs = %+v", runs)
	}
}

func TestInterruptedAndRejected(t *testing.T) {
	ctx := context.Background()
	s, sp := setup(t)
	t0 := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	spool.Write(sp, ev("a", spool.SessionStarted, "S", t0, nil))
	os.WriteFile(filepath.Join(sp, "01ZZZZZZZZZZZZZZZZZZZZZZZZ.jsonl"), []byte("{not json\n"), 0o600)
	res, err := Ingest(ctx, s, sp, t0.Add(13*time.Hour), 12*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if res.Rejected != 1 || res.Interrupted != 1 {
		t.Fatalf("res = %+v", res)
	}
	if spool.RejectedCount(sp) != 1 {
		t.Fatal("rejected file not kept")
	}
}

// Nhiều "hook" ghi song song và nhiều tiến trình nạp song song: không mất, không trùng.
func TestConcurrentWritersAndIngesters(t *testing.T) {
	ctx := context.Background()
	s, sp := setup(t)
	t0 := time.Now().UTC()
	const sessions, perSession = 8, 25
	var wg sync.WaitGroup
	for i := 0; i < sessions; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sid := string(rune('A' + i))
			for j := 0; j < perSession; j++ {
				id := sid + "-" + time.Duration(j).String()
				e := ev(id, spool.FileModified, sid, t0.Add(time.Duration(j)*time.Millisecond), map[string]any{"path": "f.go"})
				if _, err := spool.Write(sp, e); err != nil {
					t.Error(err)
				}
				if j%5 == 0 { // ghi trùng có chủ đích
					spool.Write(sp, e)
				}
			}
		}(i)
	}
	stop := make(chan struct{})
	var iwg sync.WaitGroup
	for k := 0; k < 3; k++ {
		iwg.Add(1)
		go func() {
			defer iwg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := Ingest(ctx, s, sp, time.Now(), 0); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(stop)
	iwg.Wait()
	if _, err := Ingest(ctx, s, sp, time.Now(), 0); err != nil {
		t.Fatal(err)
	}
	var n, runs int
	s.DB.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&n)
	s.DB.QueryRow(`SELECT COUNT(*) FROM runs`).Scan(&runs)
	if n != sessions*perSession || runs != sessions {
		t.Fatalf("events = %d (want %d), runs = %d (want %d)", n, sessions*perSession, runs, sessions)
	}
}
