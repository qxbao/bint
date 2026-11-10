package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bint/internal/runlog"
	"bint/internal/testutil"
)

func artifactsFn(t *testing.T, root string) func() []runlog.Artifact {
	t.Helper()
	ws := mustWS(t, root)
	s, err := ws.OpenStore(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return func() []runlog.Artifact {
		if _, err := ws.Ingest(context.Background(), s, time.Now()); err != nil {
			t.Fatal(err)
		}
		runs, err := runlog.ListRuns(context.Background(), s.DB, 10)
		if err != nil || len(runs) != 1 {
			t.Fatalf("runs=%d err=%v", len(runs), err)
		}
		arts, err := runlog.RunArtifacts(context.Background(), s.DB, runs[0].RunID)
		if err != nil {
			t.Fatal(err)
		}
		return arts
	}
}

func modified(arts []runlog.Artifact) map[string]int {
	m := map[string]int{}
	for _, a := range arts {
		if a.Kind == "file.modified" {
			m[a.Path] = a.Count
		}
	}
	return m
}

// F7: tệp sửa qua Bash được ghi file.modified; Edit rồi Bash không ghi trùng.
func TestBashFileChangesRecorded(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n", "a.go": "package a\n", "b.go": "package b\n"})
	mustInit(t, InitOptions{Dir: root, Hooks: yes()})
	arts := artifactsFn(t, root)
	const sid = "s1"
	runHook(t, hookInput(sid, "SessionStart", root, map[string]any{"source": "startup"}))

	// Edit: b.go
	os.WriteFile(filepath.Join(root, "b.go"), []byte("package b // edit\n"), 0o644)
	runHook(t, hookInput(sid, "PostToolUse", root, map[string]any{"tool_name": "Edit", "tool_use_id": "t1",
		"tool_input": map[string]any{"file_path": filepath.Join(root, "b.go")}}))

	bash := func(id string, run func()) {
		in := map[string]any{"tool_name": "Bash", "tool_use_id": id, "tool_input": map[string]any{"command": "sed -i x a.go"}}
		runHook(t, hookInput(sid, "PreToolUse", root, in))
		run()
		runHook(t, hookInput(sid, "PostToolUse", root, in))
	}
	// Sửa a.go, tạo tệp mới, không đụng b.go.
	bash("t2", func() {
		os.WriteFile(filepath.Join(root, "a.go"), []byte("package a // sed\n"), 0o644)
		os.WriteFile(filepath.Join(root, "new.txt"), []byte("hi\n"), 0o644)
	})
	// Lệnh không đổi gì: không thêm sự kiện.
	bash("t3", func() {})
	// Xoá tệp mới.
	bash("t4", func() { os.Remove(filepath.Join(root, "new.txt")) })

	got := modified(arts())
	if got["a.go"] != 1 {
		t.Errorf("a.go = %d, want 1 (%v)", got["a.go"], got)
	}
	if got["b.go"] != 1 {
		t.Errorf("b.go bị ghi trùng hoặc thiếu: %d (%v)", got["b.go"], got)
	}
	if got["new.txt"] != 2 { // tạo rồi xoá: hai thay đổi khác nhau
		t.Errorf("new.txt = %d, want 2 (%v)", got["new.txt"], got)
	}
}

// Hai lệnh Bash song song (Pre A, Pre B, Post A, Post B) cùng phiên: cả hai tệp đều được ghi.
func TestParallelBashKeyedByToolUseID(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n", "a.go": "package a\n", "b.go": "package b\n"})
	mustInit(t, InitOptions{Dir: root, Hooks: yes()})
	arts := artifactsFn(t, root)
	runHook(t, hookInput("s3", "SessionStart", root, nil))
	in := func(id string) map[string]any {
		return map[string]any{"tool_name": "Bash", "tool_use_id": id, "tool_input": map[string]any{"command": "x"}}
	}
	runHook(t, hookInput("s3", "PreToolUse", root, in("A")))
	runHook(t, hookInput("s3", "PreToolUse", root, in("B")))
	os.WriteFile(filepath.Join(root, "a.go"), []byte("package a // A\n"), 0o644)
	runHook(t, hookInput("s3", "PostToolUse", root, in("A")))
	os.WriteFile(filepath.Join(root, "b.go"), []byte("package b // B\n"), 0o644)
	runHook(t, hookInput("s3", "PostToolUse", root, in("B")))
	got := modified(arts())
	if got["a.go"] != 1 || got["b.go"] != 1 {
		t.Errorf("thiếu sự kiện: %v", got)
	}
}

// Không có ảnh chụp trước (hook Pre chưa cài): không tổng hợp giả.
func TestBashWithoutPreSnapshotRecordsNothing(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n", "a.go": "package a\n"})
	mustInit(t, InitOptions{Dir: root, Hooks: yes()})
	arts := artifactsFn(t, root)
	runHook(t, hookInput("s2", "SessionStart", root, nil))
	os.WriteFile(filepath.Join(root, "a.go"), []byte("package a // x\n"), 0o644)
	runHook(t, hookInput("s2", "PostToolUse", root, map[string]any{"tool_name": "Bash", "tool_use_id": "t",
		"tool_input": map[string]any{"command": "echo"}}))
	if got := modified(arts()); len(got) != 0 {
		t.Errorf("không được ghi: %v", got)
	}
}

// F8: transcript có usage -> token của lượt; không có -> NULL; cost_usd luôn NULL.
func TestSessionEndTokensFromTranscript(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	mustInit(t, InitOptions{Dir: root, Hooks: yes()})
	ws := mustWS(t, root)
	s, err := ws.OpenStore(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	tr := filepath.Join(t.TempDir(), "t.jsonl")
	os.WriteFile(tr, []byte(`{"type":"user","message":{"content":"hi"}}
{"type":"assistant","message":{"id":"m1","usage":{"input_tokens":10,"cache_creation_input_tokens":5,"cache_read_input_tokens":100,"output_tokens":1}}}
{"type":"assistant","message":{"id":"m1","usage":{"input_tokens":10,"cache_creation_input_tokens":5,"cache_read_input_tokens":100,"output_tokens":20}}}
{"type":"assistant","message":{"id":"m2","usage":{"input_tokens":3,"output_tokens":7}}}
`), 0o644)
	runHook(t, hookInput("with", "SessionStart", root, nil))
	runHook(t, hookInput("with", "SessionEnd", root, map[string]any{"reason": "other", "transcript_path": tr}))
	runHook(t, hookInput("without", "SessionStart", root, nil))
	runHook(t, hookInput("without", "SessionEnd", root, map[string]any{"reason": "other", "transcript_path": filepath.Join(t.TempDir(), "none.jsonl")}))
	if _, err := ws.Ingest(context.Background(), s, time.Now()); err != nil {
		t.Fatal(err)
	}
	check := func(sid string, in, out *int64) {
		t.Helper()
		var gi, gout *int64
		var c *float64
		if err := s.DB.QueryRow(`SELECT input_tokens, output_tokens, cost_usd FROM runs WHERE harness_session_id = ?`, sid).Scan(&gi, &gout, &c); err != nil {
			t.Fatal(err)
		}
		eq := func(a, b *int64) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }
		if !eq(gi, in) || !eq(gout, out) || c != nil {
			t.Errorf("%s: in=%v out=%v cost=%v", sid, gi, gout, c)
		}
	}
	i, o := int64(118), int64(27)
	check("with", &i, &o)
	check("without", nil, nil)
}
