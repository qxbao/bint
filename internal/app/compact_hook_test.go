package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/ctxengine"
	"bint/internal/harness/claude"
	"bint/internal/knowledge"
	"bint/internal/testutil"
)

// Sau khi harness nén ngữ cảnh (SessionStart source=compact), tri thức đã chèn theo đường
// dẫn không còn trong ngữ cảnh của agent: hook phải quên "mục đã chèn" của phiên để lần
// đọc sau chèn lại. Các nguồn khác (startup, resume) không đụng tới bộ nhớ phiên.
func TestPathHookReinjectsAfterCompact(t *testing.T) {
	testutil.Isolate(t)
	for _, k := range agentEnvVars {
		t.Setenv(k, "")
	}
	ctx := context.Background()
	root := testutil.NewRepo(t, map[string]string{"internal/db/db.go": "package db\n\nfunc Open() {}\n"})
	mustInit(t, InitOptions{Dir: root, Hooks: yes(), MCP: no()})
	k, err := OpenKnowledge(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	res, err := AddKnowledge(ctx, k, AddOptions{Type: "rule", Title: "Không mở DB trong handler", Body: "Dùng pool chung.",
		Paths: []string{"internal/db/**"}, Evidence: []string{"internal/db/db.go:3#Open"}, Origin: knowledge.OriginHuman})
	if err != nil || !res.Saved {
		t.Fatalf("%+v %v", res.Check, err)
	}
	marker := "[k:" + res.Item.ID + "]"

	n := 0
	read := func() string {
		n++
		return runHook(t, hookInput("S1", "PreToolUse", root, map[string]any{"tool_name": "Read", "tool_use_id": fmt.Sprintf("tu_%d", n),
			"tool_input": map[string]any{"file_path": filepath.Join(root, "internal", "db", "db.go")}})).Output
	}
	start := func(source string) {
		t.Helper()
		runHook(t, hookInput("S1", "SessionStart", root, map[string]any{"source": source}))
	}
	if out := read(); !strings.Contains(out, marker) {
		t.Fatalf("lần đọc đầu phải chèn %s: %q", marker, out)
	}
	if out := read(); out != "" {
		t.Fatalf("không lặp lại trong phiên: %q", out)
	}
	for _, src := range []string{"startup", "resume", ""} {
		start(src)
		if out := read(); out != "" {
			t.Fatalf("source=%q không được xoá bộ nhớ phiên: %q", src, out)
		}
	}
	start("compact")
	if out := read(); !strings.Contains(out, marker) {
		t.Fatalf("sau compact phải chèn lại %s: %q", marker, out)
	}
	if out := read(); out != "" {
		t.Fatalf("sau khi chèn lại thì không lặp nữa: %q", out)
	}

	// Không xoá được bộ nhớ phiên (ở đây: đường dẫn là thư mục không rỗng) thì hook vẫn không
	// được báo lỗi: runHook báo t.Error nếu HandleHook trả lỗi.
	mem := ctxengine.NewSessionMemory(k.Paths.Runtime, claude.HarnessName, "S1")
	if err := os.Remove(mem.Path); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(mem.Path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mem.Path, "keep"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	start("compact")
}
