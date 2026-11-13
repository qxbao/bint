package actions_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"bint/internal/app"
	"bint/internal/config"
	"bint/internal/tui/actions"
)

// system.skill_test (Job): ước tính tới trước hộp xác nhận (hộp có Detail); trả không thì agent không chạy và không có kết quả;
// trả có thì chạy cả hai nhánh và trả kết quả có tỉ lệ.
func TestSkillTestJobConfirmBeforeAgent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("agent giả dùng sh")
	}
	root, _ := seed(t)
	ctx := context.Background()
	if _, err := app.NewSkill(ctx, root, app.SkillNewOptions{Name: "demo-skill"}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".bint", "skills", "demo-skill")
	skill := "---\nname: demo-skill\ndescription: Dùng khi cần thử skill.\n---\n\n# Demo\n\nLuôn viết kiểm thử trước, DAU-HIEU-TUI-12.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skill), 0o644); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(dir, "tests"), 0o755)
	if err := os.WriteFile(filepath.Join(dir, "tests", "a.yml"), []byte("prompt: Sửa ngay.\nreps: 2\nexpect:\n  - contains: kiểm thử trước\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	marker := filepath.Join(tmp, "called")
	script := filepath.Join(tmp, "agent.sh")
	sh := "echo called >> '" + filepath.ToSlash(marker) + "'\n" +
		"if grep -q 'DAU-HIEU-TUI-12' \"$BINT_EVAL_PROMPT_FILE\"; then t='viết kiểm thử trước'; else t='sửa luôn'; fi\n" +
		"echo \"{\\\"type\\\":\\\"result\\\",\\\"result\\\":\\\"$t\\\"}\"\n"
	if err := os.WriteFile(script, []byte(sh), 0o644); err != nil {
		t.Fatal(err)
	}
	env := actions.Env{Dir: root, Version: "test"}
	o := app.SkillTestDefaults(config.Default())
	o.Name, o.AgentCommand, o.Yes = "demo-skill", "sh "+filepath.ToSlash(script), true // Yes bị bỏ qua: TUI luôn hỏi

	r, v, err := runJob(t, "system.skill_test", env, o, false)
	if err != app.ErrCancelled || v != nil {
		t.Fatalf("trả không: %v %v", err, v)
	}
	if !r.asked || r.prompt.Key != app.ConfirmSkillTestKey || !strings.Contains(r.prompt.Detail, "sy.skilltest.log.estimate") {
		t.Fatalf("hộp xác nhận phải có ước tính: %+v", r.prompt)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("agent đã chạy dù người dùng từ chối")
	}

	r, v, err = runJob(t, "system.skill_test", env, o, true)
	out, ok := v.(actions.SkillTestOutcome)
	if err != nil || !ok || out.Result == nil {
		t.Fatalf("trả có: %v %#v", err, v)
	}
	w, wo := out.Result.Report.Totals["with"], out.Result.Report.Totals["without"]
	if w.Rate == nil || *w.Rate != 1 || wo.Rate == nil || *wo.Rate != 0 {
		t.Fatalf("with %+v, without %+v", w, wo)
	}
	if len(r.logs) < 5 {
		t.Fatalf("log: %v", r.logs)
	}
	if b, _ := os.ReadFile(marker); strings.Count(string(b), "called") != 4 {
		t.Fatalf("agent được gọi %q, muốn 4 lần", b)
	}
	if a, _ := actions.Default().ByID("system.skill_test"); a.JobInfo == nil || a.JobInfo(o).Label != "skill test · demo-skill" {
		t.Fatalf("nhãn việc nền: %+v", a)
	}
}
