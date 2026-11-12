package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/config"
	"bint/internal/harness"
	"bint/internal/skills"
	"bint/internal/testutil"
	"bint/internal/workflow"
)

func agentPath(root, name string) string { return filepath.Join(root, ".claude", "agents", name+".md") }

// bint init / init --update sinh ba định nghĩa subagent, khoá phiên bản trong core.lock, ghi vào nhật ký ghi (H8); sửa tay thì
// sync báo xung đột; thiếu thì sync tạo lại.
func TestInitInstallsSubagentDefinitions(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	rep := mustInit(t, InitOptions{Dir: root, Harnesses: []string{"claude"}, Hooks: no()})
	names := []string{harness.SubagentPlanner, harness.SubagentImplementer, harness.SubagentReviewer}
	for _, n := range names {
		if _, err := os.Stat(agentPath(root, n)); err != nil {
			t.Fatalf("thiếu %s: %v", n, err)
		}
		if s := stepOf(rep, "subagent "+n); s.Status != StepCreated {
			t.Fatalf("bước init của %s: %+v", n, s)
		}
	}
	lock, err := skills.ReadLock(root)
	if err != nil || len(lock.Agents) != 3 || lock.Agents[harness.SubagentReviewer].SHA256 == "" ||
		len(lock.Agents[harness.SubagentReviewer].Published) != 1 {
		t.Fatalf("core.lock thiếu agents: %+v %v", lock.Agents, err)
	}
	// Nhật ký ghi (H8): ghi của init được ghi nhận để bước chỉ đọc không coi là vi phạm.
	ws, err := OpenWorkspace(context.Background(), root, false)
	if err != nil {
		t.Fatal(err)
	}
	recs, err := workflow.ReadWrites(workflow.WritesLogPath(ws.Paths.Runtime), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	logged := false
	for _, r := range recs {
		if strings.HasSuffix(r.Path, ".claude/agents/bint-planner.md") {
			logged = true
		}
	}
	if !logged {
		t.Fatalf("nhật ký ghi không có .claude/agents/bint-planner.md: %+v", recs)
	}
	// Lần hai không đổi gì.
	rep = mustInit(t, InitOptions{Dir: root, Harnesses: []string{"claude"}, Hooks: no()})
	if s := stepOf(rep, "subagent bint-planner"); s.Status != StepOK {
		t.Fatalf("lần hai: %+v", s)
	}
	// Sửa tay ⇒ xung đột (giữ nguyên nội dung); mất ⇒ sync tạo lại.
	p := agentPath(root, harness.SubagentPlanner)
	os.WriteFile(p, []byte("của người dùng\n"), 0o644)
	plan, err := PlanSync(context.Background(), root, "test", SyncOptions{})
	if err != nil || plan.Conflicts != 1 {
		t.Fatalf("PlanSync phải thấy một xung đột: %+v %v", plan, err)
	}
	if _, err := Sync(context.Background(), root, "test", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "của người dùng\n" {
		t.Fatalf("sync không được ghi đè bản sửa tay: %q", b)
	}
	os.Remove(agentPath(root, harness.SubagentImplementer))
	if _, err := Sync(context.Background(), root, "test", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(agentPath(root, harness.SubagentImplementer)); err != nil {
		t.Fatalf("sync phải tạo lại: %v", err)
	}
}

// Không bật Claude Code thì không sinh .claude/agents.
func TestInitSkipsSubagentsWithoutClaude(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	mustInit(t, InitOptions{Dir: root, Harnesses: []string{"codex"}, Hooks: no()})
	if _, err := os.Stat(filepath.Join(root, ".claude", "agents")); err == nil {
		t.Fatal("không có Claude thì không được sinh .claude/agents")
	}
	if len(harness.SubagentFiles(config.Default())) != 3 {
		t.Fatal("cấu hình mặc định (claude) phải có ba subagent")
	}
}
