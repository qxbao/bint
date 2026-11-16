package codex

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRolloutUsageCumulative(t *testing.T) {
	before := RolloutUsage(filepath.Join("testdata", "rollout-before-resume.jsonl"))
	after := RolloutUsage(filepath.Join("testdata", "rollout-resume.jsonl"))
	if before == nil || *before != (TokenUsage{Input: 55000, CachedInput: 20000, Output: 900}) {
		t.Fatalf("before: %+v", before)
	}
	// Bộ đếm là tổng cộng dồn của thread: phần của lần nối tiếp = sau − trước (không phải số `last_token_usage`).
	if after == nil || *after != (TokenUsage{Input: 61000, CachedInput: 41000, Output: 1100}) {
		t.Fatalf("after: %+v", after)
	}
}

func TestRolloutUsageMissing(t *testing.T) {
	if RolloutUsage(filepath.Join(t.TempDir(), "none.jsonl")) != nil {
		t.Fatal("không có tệp phải nil")
	}
	p := filepath.Join(t.TempDir(), "r.jsonl")
	os.WriteFile(p, []byte(`{"type":"session_meta","payload":{}}`+"\n"+`{"type":"event_msg","payload":{"type":"token_count","info":null}}`+"\n"), 0o600)
	if RolloutUsage(p) != nil {
		t.Fatal("chưa có total_token_usage phải nil (không biết, không phải 0)")
	}
}

func TestRolloutPath(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "2026", "11", "16")
	os.MkdirAll(dir, 0o755)
	const id = "01a17179-5784-7152-8335-f0af387ce18a"
	older := filepath.Join(dir, "rollout-2026-11-16T09-00-00-"+id+".jsonl")
	newer := filepath.Join(dir, "rollout-2026-11-16T10-00-00-"+id+".jsonl")
	other := filepath.Join(dir, "rollout-2026-11-16T11-00-00-01a17179-ffff-7152-8335-f0af387ce18a.jsonl")
	for _, p := range []string{older, newer, other} {
		os.WriteFile(p, nil, 0o600)
	}
	if got := RolloutPath(home, id); got != newer {
		t.Fatalf("lấy tệp mới nhất của đúng thread: %s", got)
	}
	if RolloutPath(home, "khong-co") != "" || RolloutPath(home, "*") != "" || RolloutPath(home, "") != "" {
		t.Fatal("thread không có / id lạ phải trả rỗng")
	}
	t.Setenv("CODEX_HOME", home)
	if RolloutPath("", id) != newer {
		t.Fatal("codexHome rỗng dùng CODEX_HOME")
	}
}
