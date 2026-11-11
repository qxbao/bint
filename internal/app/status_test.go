package app

import (
	"context"
	"testing"
	"time"

	"bint/internal/spool"
	"bint/internal/testutil"
	"bint/internal/ulid"
)

// StatusNoIngest (chế độ chỉ đọc của TUI) không nạp spool vào SQLite; Status thì có.
func TestStatusNoIngestLeavesSpoolAlone(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	testutil.Git(t, root, "config", "user.name", "Rev")
	testutil.Git(t, root, "config", "user.email", "rev@example.com")
	ctx := context.Background()
	off := false
	if _, err := Init(ctx, InitOptions{Dir: root, Hooks: &off, NoSkills: true, BintPath: "bint"}); err != nil {
		t.Fatal(err)
	}
	ws, err := OpenWorkspace(ctx, root, true)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-time.Hour)
	ev := func(typ string, ts time.Time, p map[string]any) spool.Event {
		return spool.Event{EventID: ulid.New(), Type: typ, Source: "hook:claude", Timestamp: ts, Harness: "claude", SessionID: "s1", Payload: p}
	}
	if _, err := spool.Write(ws.Paths.Spool, ev(spool.SessionStarted, at, map[string]any{"model": "m"}),
		ev(spool.SessionEnded, at.Add(time.Minute), map[string]any{"reason": "exit"})); err != nil {
		t.Fatal(err)
	}
	st, err := StatusNoIngest(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if st.TotalRuns != 0 || st.Ingest.Events != 0 {
		t.Fatalf("không được nạp spool: %+v", st)
	}
	st, err = Status(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if st.TotalRuns != 1 || st.Ingest.Events == 0 {
		t.Fatalf("Status phải nạp spool: %+v", st)
	}
}
