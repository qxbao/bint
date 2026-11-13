package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/testutil"
)

func TestCustomSkillServices(t *testing.T) {
	root := sysRepo(t, InitOptions{})
	ctx := context.Background()

	// Tên sai quy tắc → lỗi cách dùng, không tạo gì.
	for _, bad := range []string{"b-mine", "B", "overlays", "evolved", "b-do", "a/b", ""} {
		if _, err := NewSkill(ctx, root, SkillNewOptions{Name: bad}); err == nil || !IsUsage(err) {
			t.Errorf("NewSkill(%q): muốn lỗi cách dùng, được %v", bad, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".bint", "skills", "overlays", "SKILL.md")); err == nil {
		t.Fatal("không được tạo skill tên dành riêng")
	}
	r1, err := NewSkill(ctx, root, SkillNewOptions{Name: "demo-skill"})
	if err != nil || r1.Existed || r1.Path != ".bint/skills/demo-skill/SKILL.md" {
		t.Fatalf("%v %+v", err, r1)
	}
	if err := os.WriteFile(r1.Abs, append([]byte(read(t, r1.Abs)), "\nsửa tay\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	r2, err := NewSkill(ctx, root, SkillNewOptions{Name: "demo-skill"})
	if err != nil || !r2.Existed || !strings.HasSuffix(read(t, r1.Abs), "sửa tay\n") {
		t.Fatalf("không được ghi đè: %v %+v", err, r2)
	}

	// Lint: tên → một kết quả; không tên → mọi skill tuỳ biến; skill gốc/lạ → lỗi cách dùng.
	res, err := LintSkills(ctx, root, SkillLintOptions{Name: "demo-skill"})
	if err != nil || len(res) != 1 || !res[0].OK() || res[0].Errors == nil || res[0].Warnings == nil {
		t.Fatalf("%v %+v", err, res)
	}
	if all, err := LintSkills(ctx, root, SkillLintOptions{}); err != nil || len(all) != 1 {
		t.Fatalf("%v %+v", err, all)
	}
	for _, name := range []string{"b-do", "khong-co"} {
		if _, err := LintSkills(ctx, root, SkillLintOptions{Name: name}); err == nil || !IsUsage(err) {
			t.Errorf("LintSkills(%q): muốn lỗi cách dùng, được %v", name, err)
		}
	}
	os.WriteFile(r1.Abs, []byte("---\nname: demo-skill\ndescription: x\n---\n\n[l](nope.md)\n"), 0o644)
	if res, _ = LintSkills(ctx, root, SkillLintOptions{Name: "demo-skill"}); res[0].OK() || len(res[0].Warnings) == 0 {
		t.Fatalf("phải có lỗi liên kết và cảnh báo mô tả: %+v", res)
	}

	// List / Show.
	os.WriteFile(r1.Abs, []byte("---\nname: demo-skill\ndescription: Dùng khi thử.\n---\n\nThân.\n"), 0o644)
	os.WriteFile(filepath.Join(filepath.Dir(r1.Abs), "extra.md"), []byte("x"), 0o644)
	k, _ := OpenKnowledge(ctx, root)
	lst, err := ListSkills(k, SkillListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var row *SkillRow
	for i, r := range lst.Rows {
		if r.Name == "demo-skill" {
			row = &lst.Rows[i]
		}
	}
	if row == nil || row.Kind != "custom" || row.Files != 1 || row.Problem != "" || row.Targets[".claude/skills"] != "missing" {
		t.Fatalf("dòng custom: %+v", row)
	}
	if _, err := Sync(ctx, root, "test", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	lst, _ = ListSkills(k, SkillListOptions{})
	for _, r := range lst.Rows {
		if r.Name == "demo-skill" && (r.Targets[".claude/skills"] != "current" || r.Locked == "") {
			t.Fatalf("sau sync: %+v", r)
		}
	}
	sc, err := ShowSkill(ctx, root, SkillShowOptions{Name: "demo-skill"})
	if err != nil || sc.Kind != "custom" || !strings.Contains(string(sc.Content), "Thân.") {
		t.Fatalf("%v %+v", err, sc)
	}
}

// PlanSync phải thấy đúng cái Sync thật thấy: skill tuỳ biến có symlink không được xuất bản (xung đột) ở cả hai.
func TestPlanSyncMatchesSyncWithSymlinkInCustomSkill(t *testing.T) {
	root := sysRepo(t, InitOptions{})
	ctx := context.Background()
	if _, err := NewSkill(ctx, root, SkillNewOptions{Name: "demo-skill"}); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "ngoai.md")
	os.WriteFile(outside, []byte("x"), 0o644)
	if err := os.Symlink(outside, filepath.Join(root, ".bint", "skills", "demo-skill", "link.md")); err != nil {
		t.Skipf("không tạo được symlink: %v", err)
	}
	before := snapshotTree(t, root)
	plan, err := PlanSync(ctx, root, "test", SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if after := snapshotTree(t, root); after != before {
		t.Fatal("PlanSync không được ghi gì")
	}
	if plan.Conflicts != 1 {
		t.Fatalf("kế hoạch phải báo xung đột vì symlink: %+v", plan)
	}
	si, err := Sync(ctx, root, "test", SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if si.Conflicts != plan.Conflicts {
		t.Fatalf("xung đột: kế hoạch %d, Sync %d", plan.Conflicts, si.Conflicts)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "skills", "demo-skill")); err == nil {
		t.Fatal("skill có symlink không được xuất bản")
	}
	// Symlink trong thư mục xuất bản của harness không được khiến PlanSync ghi xuyên qua nó.
	real := filepath.Join(t.TempDir(), "real")
	os.MkdirAll(real, 0o755)
	os.Remove(filepath.Join(root, ".bint", "skills", "demo-skill", "link.md"))
	if err := os.Symlink(real, filepath.Join(root, ".claude", "skills", "demo-skill")); err != nil {
		t.Skip(err)
	}
	if _, err := PlanSync(ctx, root, "test", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if ents, _ := os.ReadDir(real); len(ents) != 0 {
		t.Fatalf("PlanSync đã ghi xuyên symlink vào %s", real)
	}
}

// --git local: skill tuỳ biến đã xuất bản (kể cả tệp phụ ở thư mục con) không hiện trong git status.
func TestGitLocalHidesPublishedCustomSkill(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	mustInit(t, InitOptions{Dir: root, Harnesses: []string{"claude", "codex"}, Hooks: no(), MCP: no(), GitMode: GitLocal})
	ctx := context.Background()
	if _, err := NewSkill(ctx, root, SkillNewOptions{Name: "demo-skill"}); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(root, ".bint", "skills", "demo-skill", "references", "deep"), 0o755)
	os.WriteFile(filepath.Join(root, ".bint", "skills", "demo-skill", "references", "deep", "a.md"), []byte("a"), 0o644)
	if _, err := Sync(ctx, root, "test", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{".claude/skills", ".agents/skills"} {
		if _, err := os.Stat(filepath.Join(root, d, "demo-skill", "references", "deep", "a.md")); err != nil {
			t.Fatalf("%s: %v", d, err)
		}
	}
	// Chạy lại init cập nhật phạm vi loại trừ theo core.lock.
	mustInit(t, InitOptions{Dir: root, Harnesses: []string{"claude", "codex"}, Hooks: no(), MCP: no(), GitMode: GitLocal})
	if st := gitOut(t, root, "status", "--porcelain", "--untracked-files=all"); st != "" {
		t.Fatalf("git status phải sạch ở chế độ local:\n%s", st)
	}
}
