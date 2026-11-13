package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"bint/internal/fsafe"
	"bint/internal/testutil"
)

const appFlow = "schema: 1\nname: tdd\ndescription: Viết kiểm thử trước\nsteps:\n  - {id: red, kind: implement, skill: b-feat}\n  - {id: review, kind: review, skill: b-review, fix: red}\n"

func writeAppFlow(t *testing.T, root, name, body string) {
	t.Helper()
	p := filepath.Join(root, ".bint", "flows", name+".yml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// PlanSync liệt kê skill điều phối của flow tuỳ biến như Sync thật sẽ xuất bản, và không ghi gì vào repository.
func TestPlanSyncListsFlowSkill(t *testing.T) {
	root := sysRepo(t, InitOptions{})
	ctx := context.Background()
	writeAppFlow(t, root, "tdd", appFlow)
	before := snapshotTree(t, root)
	plan, err := PlanSync(ctx, root, "test", SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if after := snapshotTree(t, root); after != before {
		t.Fatal("PlanSync không được ghi gì")
	}
	created := map[string]bool{}
	for _, f := range plan.Files {
		if f.Skill == "tdd" && f.Result == fsafe.Created && strings.Contains(f.Diff, "bint run tdd --task") {
			created[f.Path] = true
		}
	}
	if !created[".claude/skills/tdd/SKILL.md"] {
		t.Fatalf("kế hoạch phải liệt kê skill điều phối: %+v", plan.Files)
	}
	si, err := Sync(ctx, root, "test", SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if si.Conflicts != plan.Conflicts {
		t.Fatalf("xung đột: kế hoạch %d, Sync %d", plan.Conflicts, si.Conflicts)
	}
	if !strings.Contains(read(t, filepath.Join(root, ".claude/skills/tdd/SKILL.md")), "disable-model-invocation: true") {
		t.Fatal("skill điều phối chưa được xuất bản")
	}
	// Xuất bản xong, kế hoạch không còn việc gì cho flow; xoá flow thì kế hoạch báo gỡ.
	plan, _ = PlanSync(ctx, root, "test", SyncOptions{})
	for _, f := range plan.Files {
		if f.Skill == "tdd" && f.Result != fsafe.Unchanged {
			t.Fatalf("flow đã xuất bản: %+v", f)
		}
	}
	os.Remove(filepath.Join(root, ".bint", "flows", "tdd.yml"))
	plan, _ = PlanSync(ctx, root, "test", SyncOptions{})
	removed := 0
	for _, f := range plan.Files {
		if f.Skill == "tdd" && f.Result == fsafe.Removed {
			removed++
		}
	}
	if removed != 1 {
		t.Fatalf("kế hoạch phải báo gỡ skill điều phối: %+v", plan.Files)
	}
}

// GĐ1 .8: chỉ bit thực thi của tệp phụ đổi (nội dung giữ nguyên) thì PlanSync vẫn báo Updated như Sync thật.
func TestPlanSyncSeesExecBitChange(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows không có bit thực thi")
	}
	root := sysRepo(t, InitOptions{})
	ctx := context.Background()
	if _, err := NewSkill(ctx, root, SkillNewOptions{Name: "demo-skill"}); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, ".bint", "skills", "demo-skill", "scripts", "run.sh")
	os.MkdirAll(filepath.Dir(script), 0o755)
	if err := os.WriteFile(script, []byte("echo x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(ctx, root, "test", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanSync(ctx, root, "test", SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var updated int
	for _, f := range plan.Files {
		if strings.HasSuffix(f.Path, "demo-skill/scripts/run.sh") && f.Result == fsafe.Updated {
			updated++
		}
	}
	if updated == 0 {
		t.Fatalf("PlanSync phải báo Updated cho script đổi bit thực thi: %+v", plan.Files)
	}
	if _, err := Sync(ctx, root, "test", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	plan, _ = PlanSync(ctx, root, "test", SyncOptions{})
	for _, f := range plan.Files {
		if f.Result == fsafe.Updated {
			t.Fatalf("sau sync không còn gì để cập nhật: %+v", f)
		}
	}
}

// --git local: skill điều phối của flow đã xuất bản không hiện trong git status.
func TestGitLocalHidesPublishedFlowSkill(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	mustInit(t, InitOptions{Dir: root, Harnesses: []string{"claude", "codex"}, Hooks: no(), MCP: no(), GitMode: GitLocal})
	writeAppFlow(t, root, "tdd", appFlow)
	if _, err := Sync(context.Background(), root, "test", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	mustInit(t, InitOptions{Dir: root, Harnesses: []string{"claude", "codex"}, Hooks: no(), MCP: no(), GitMode: GitLocal})
	if st := gitOut(t, root, "status", "--porcelain", "--untracked-files=all"); st != "" {
		t.Fatalf("git status phải sạch ở chế độ local:\n%s", st)
	}
}
