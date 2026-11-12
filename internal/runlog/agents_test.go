package runlog

import (
	"context"
	"testing"
	"time"

	"bint/internal/spool"
	"bint/internal/store"
)

func hookEv(id, typ, session string, ts time.Time, p map[string]any) spool.Event {
	e := ev(id, typ, session, ts, p)
	e.Source = "hook:claude"
	return e
}

func finishedEv(id, run, agent string, ts time.Time) spool.Event {
	e := ev(id, spool.WorkflowFinished, "", ts, map[string]any{"status": "completed", "agent_id": agent})
	e.Source, e.Harness, e.SessionID, e.RunID = "bint-run", "", "", run
	return e
}

func runOfEvent(t *testing.T, s *store.Store, id string) string {
	t.Helper()
	var run string
	if err := s.DB.QueryRow(`SELECT run_id FROM events WHERE event_id = ?`, id).Scan(&run); err != nil {
		t.Fatalf("%s: %v", id, err)
	}
	return run
}

func artifactCount(t *testing.T, s *store.Store, run, kind, path string) int {
	t.Helper()
	var n int
	err := s.DB.QueryRow(`SELECT COALESCE(SUM(count),0) FROM run_artifacts WHERE run_id = ? AND kind = ? AND path = ?`, run, kind, path).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func ingestAll(t *testing.T, s *store.Store, sp string, now time.Time, evs ...[]spool.Event) {
	t.Helper()
	for _, batch := range evs {
		if _, err := spool.Write(sp, batch...); err != nil {
			t.Fatal(err)
		}
		if _, err := Ingest(context.Background(), s, sp, now, 0); err != nil {
			t.Fatal(err)
		}
	}
}

// Hook tới SAU workflow.finished: sự kiện của subagent gắn thẳng vào lượt con; sự kiện không có agent_id (hoặc agent_id khác)
// ở lại lượt phiên cha.
func TestAgentEventsAfterFinishedGoToChildRun(t *testing.T) {
	s, sp := setup(t)
	t0 := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	const child = "01CHILDRUN00000000000000AA"
	ingestAll(t, s, sp, t0,
		[]spool.Event{hookEv("s1", spool.SessionStarted, "S", t0, nil)},
		[]spool.Event{finishedEv("wf1", child, "agent-1", t0.Add(time.Second))},
		[]spool.Event{
			hookEv("h1", spool.FileModified, "S", t0.Add(2*time.Second), map[string]any{"path": "a.go", "agent_id": "agent-1"}),
			hookEv("h2", spool.FileModified, "S", t0.Add(3*time.Second), map[string]any{"path": "b.go"}),
			hookEv("h3", spool.FileModified, "S", t0.Add(3*time.Second), map[string]any{"path": "c.go", "agent_id": "agent-2"}),
		})
	parent := runOfEvent(t, s, "s1")
	if parent == child {
		t.Fatal("lượt phiên cha không được trùng lượt con")
	}
	if got := runOfEvent(t, s, "h1"); got != child {
		t.Fatalf("h1 phải thuộc lượt con %s, được %s", child, got)
	}
	for _, id := range []string{"h2", "h3"} {
		if got := runOfEvent(t, s, id); got != parent {
			t.Fatalf("%s phải ở lượt phiên cha, được %s", id, got)
		}
	}
	if artifactCount(t, s, child, "file.modified", "a.go") != 1 {
		t.Fatal("artifacts của lượt con thiếu a.go")
	}
}

// Hook tới TRƯỚC workflow.finished (thường gặp: bint task của chính subagent tự nạp spool): khi nạp workflow.finished có
// agent_id, các dòng events, bảng khoá theo event_id và artifacts chuyển sang lượt con; sự kiện không agent_id không bị chuyển.
func TestAgentEventsBeforeFinishedAreMovedToChildRun(t *testing.T) {
	s, sp := setup(t)
	t0 := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	const child = "01CHILDRUN00000000000000BB"
	ingestAll(t, s, sp, t0,
		[]spool.Event{
			hookEv("s1", spool.SessionStarted, "S", t0, nil),
			hookEv("h1", spool.FileModified, "S", t0.Add(time.Second), map[string]any{"path": "a.go", "agent_id": "agent-1"}),
			hookEv("h2", spool.FileModified, "S", t0.Add(2*time.Second), map[string]any{"path": "b.go"}),
			hookEv("h3", spool.SkillActivated, "S", t0.Add(3*time.Second), map[string]any{"skill": "b-feat", "agent_id": "agent-1"}),
			hookEv("h4", spool.FileRead, "S", t0.Add(4*time.Second), map[string]any{"paths": []any{"r.go"}, "agent_id": "agent-1"}),
		})
	parent := runOfEvent(t, s, "s1")
	if got := runOfEvent(t, s, "h1"); got != parent {
		t.Fatalf("trước workflow.finished h1 ở lượt phiên cha: %s", got)
	}
	ingestAll(t, s, sp, t0, []spool.Event{finishedEv("wf1", child, "agent-1", t0.Add(10*time.Second))})
	for _, id := range []string{"h1", "h3", "h4"} {
		if got := runOfEvent(t, s, id); got != child {
			t.Fatalf("%s phải chuyển sang lượt con, được %s", id, got)
		}
	}
	if got := runOfEvent(t, s, "h2"); got != parent {
		t.Fatalf("h2 không có agent_id phải ở lại lượt phiên cha: %s", got)
	}
	var skillRun string
	if err := s.DB.QueryRow(`SELECT run_id FROM run_skill_usages WHERE event_id = 'h3'`).Scan(&skillRun); err != nil || skillRun != child {
		t.Fatalf("run_skill_usages phải theo lượt con: %q %v", skillRun, err)
	}
	if artifactCount(t, s, child, "file.modified", "a.go") != 1 || artifactCount(t, s, child, "file.read", "r.go") != 1 {
		t.Fatal("artifacts của lượt con thiếu a.go / r.go")
	}
	// Bản ở lượt phiên cha giữ nguyên; b.go không sang lượt con.
	if artifactCount(t, s, parent, "file.modified", "a.go") != 1 || artifactCount(t, s, child, "file.modified", "b.go") != 0 {
		t.Fatal("artifacts lượt phiên cha bị đổi / b.go lọt sang lượt con")
	}
	// Nạp lại cùng workflow.finished (trùng event_id) không chuyển hay nhân đôi gì.
	ingestAll(t, s, sp, t0, []spool.Event{finishedEv("wf1", child, "agent-1", t0.Add(10*time.Second))})
	if artifactCount(t, s, child, "file.modified", "a.go") != 1 {
		t.Fatal("nạp lại không được nhân artifacts")
	}
}
