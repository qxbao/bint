package actions_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"bint/internal/app"
	"bint/internal/config"
	"bint/internal/testutil"
	"bint/internal/tui/actions"
	"bint/internal/tui/jobs"
)

// Hành động vòng đời giai đoạn 5b trên repository thật: init.plan/init.run (trình hướng dẫn), system.repo_state/plan/apply (Cài
// đặt repo), nâng cấu hình, và system.update (nền, hàm giả).
func TestInitActionsPlanThenRunWithRealServices(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	env := actions.Env{Dir: root, BintPath: "bint", Version: "test"}
	opts := app.InitFlagOptions{Harnesses: []string{"claude"}, Hooks: true, HooksScope: "project", MCP: true, GitMode: "track"}

	r := do(t, "init.plan", env, opts)
	pl, ok := r.Value.(app.InitPlan)
	if r.Err != nil || !ok || !pl.FirstInit || pl.Changes() == 0 {
		t.Fatalf("init.plan: %+v", r)
	}
	if _, err := os.Stat(config.ProjectPath(root)); err == nil {
		t.Fatal("init.plan không được ghi cấu hình")
	}
	r = do(t, "init.run", env, opts)
	rep, ok := r.Value.(*app.InitReport)
	if r.Err != nil || !ok || len(rep.Steps) == 0 || !rep.FirstInit {
		t.Fatalf("init.run: %+v", r)
	}
	st, err := app.ReadInitState(context.Background(), root)
	if err != nil || st.NotInit || !st.HooksOn() || st.HooksScope() != "project" || !st.MCPOn() || st.GitMode != "track" {
		t.Fatalf("trạng thái sau init.run: %+v %v", st, err)
	}
	// Options sai: UsageError từ Validate, không ghi.
	if r := do(t, "init.run", env, app.InitFlagOptions{Hooks: true, NoHooks: true}); !app.IsUsage(r.Err) {
		t.Fatalf("hooks + no-hooks phải là UsageError: %v", r.Err)
	}
	if r := do(t, "init.plan", env, app.InitFlagOptions{GitMode: "nham"}); !app.IsUsage(r.Err) {
		t.Fatalf("git sai: %v", r.Err)
	}
}

func TestSystemRepoActionsWithRealServices(t *testing.T) {
	root, _ := seed(t)
	env := actions.Env{Dir: root, BintPath: "bint", Version: "test"}
	r := do(t, "system.repo_state", env, nil)
	st, ok := r.Value.(app.InitState)
	if r.Err != nil || !ok || st.NotInit || len(st.Enabled) == 0 {
		t.Fatalf("repo_state: %+v", r)
	}
	// Plan chỉ đọc; apply chỉ nhận phần đổi (MCP) và để nguyên phần khác.
	change := app.InitFlagOptions{MCP: true}
	pr := do(t, "system.repo_plan", env, change)
	pl, ok := pr.Value.(app.InitPlan)
	if pr.Err != nil || !ok || pl.Changes() == 0 {
		t.Fatalf("repo_plan: %+v", pr)
	}
	if before, _ := app.ReadInitState(context.Background(), root); before.MCPOn() {
		t.Fatal("repo_plan không được đăng ký MCP")
	}
	ar := do(t, "system.repo_apply", env, change)
	if _, ok := ar.Value.(*app.InitReport); ar.Err != nil || !ok {
		t.Fatalf("repo_apply: %+v", ar)
	}
	if after, _ := app.ReadInitState(context.Background(), root); !after.MCPOn() {
		t.Fatal("repo_apply phải đăng ký MCP")
	}
}

func TestSystemConfigUpgradeActionsWithRealServices(t *testing.T) {
	root, _ := seed(t)
	env := actions.Env{Dir: root, BintPath: "bint", Version: "test"}
	// Mẫu hiện tại: không có gì để nâng.
	r := do(t, "system.config_upgrade_plan", env, nil)
	if pl, ok := r.Value.(app.ConfigUpgradePlan); r.Err != nil || !ok || pl.Changed {
		t.Fatalf("mẫu hiện tại: %+v", r)
	}
	p := config.ProjectPath(root)
	b, _ := os.ReadFile(p)
	cur := string(b)
	i, j := strings.Index(cur, "  # models:"), strings.Index(cur, "  codex:")
	old := cur[:i] + cur[j:]
	if err := os.WriteFile(p, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	r = do(t, "system.config_upgrade_plan", env, nil)
	pl, _ := r.Value.(app.ConfigUpgradePlan)
	up, _ := config.Upgrade([]byte(old))
	if r.Err != nil || !pl.Changed || string(pl.Content) != string(up.Content) {
		t.Fatalf("plan: %+v", r)
	}
	if got, _ := os.ReadFile(p); string(got) != old {
		t.Fatal("plan không được ghi")
	}
	r = do(t, "system.config_upgrade", env, nil)
	rep, _ := r.Value.(*app.InitReport)
	if got, _ := os.ReadFile(p); r.Err != nil || string(got) != string(up.Content) || app.ConfigBackupPath(rep) == "" {
		t.Fatalf("config_upgrade: %+v", r)
	}
}

// system.update là việc nền: log go install tách dòng (kể cả dòng cuối không xuống dòng) vào cầu việc, kết quả mang cả log và
// UpdateResult; hàm cập nhật tiêm qua Env.Update (không chạy go install thật); lỗi vẫn trả log.
func TestSystemUpdateJobStreamsLogAndResult(t *testing.T) {
	a, ok := actions.Default().ByID("system.update")
	if !ok || a.Job == nil || a.CLIPath != "update" || !a.Writes || a.JobInfo == nil {
		t.Fatalf("system.update phải là việc nền ghi có CLIPath update: %+v", a)
	}
	if fl := a.Flags(); len(fl) != 2 || fl[0] != "pull" || fl[1] != "source" {
		t.Fatalf("cờ của system.update: %v", fl)
	}
	run := func(fn func(context.Context, app.UpdateOptions) (app.UpdateResult, error), opts app.UpdateOptions) (actions.UpdateOutcome, []string, error) {
		msgs := make(chan tea.Msg, 64)
		br := jobs.NewBridge(context.Background(), 1, msgs)
		v, err := a.Job(context.Background(), actions.Env{Update: fn}, opts, br)
		close(msgs)
		var lines []string
		for m := range msgs {
			if l, ok := m.(jobs.LogMsg); ok {
				lines = append(lines, l.Line)
			}
		}
		return v.(actions.UpdateOutcome), lines, err
	}
	res := app.UpdateResult{Target: "/x/bint", OldRev: "a", NewRev: "b"}
	var seen app.UpdateOptions
	oc, lines, err := run(func(_ context.Context, o app.UpdateOptions) (app.UpdateResult, error) {
		seen = o
		o.Out.Write([]byte("go: downloading x\nbuilding\r\nlast"))
		return res, nil
	}, app.UpdateOptions{Pull: true})
	if err != nil || oc.Result != res || !seen.Pull || seen.Out == nil {
		t.Fatalf("kết quả: %+v %v %+v", oc, err, seen)
	}
	want := []string{"go: downloading x", "building", "last"}
	if strings.Join(lines, "|") != strings.Join(want, "|") || strings.Join(oc.Log, "|") != strings.Join(want, "|") {
		t.Fatalf("log: %v / %v", lines, oc.Log)
	}
	oc, _, err = run(func(_ context.Context, o app.UpdateOptions) (app.UpdateResult, error) {
		o.Out.Write([]byte("lỗi biên dịch\n"))
		return app.UpdateResult{}, errors.New("go install: exit 1")
	}, app.UpdateOptions{})
	if err == nil || len(oc.Log) != 1 || oc.Log[0] != "lỗi biên dịch" {
		t.Fatalf("lỗi vẫn phải trả log: %+v %v", oc, err)
	}
	// Nguồn sai bị Validate chặn trước khi cập nhật.
	called := false
	_, _, err = run(func(context.Context, app.UpdateOptions) (app.UpdateResult, error) {
		called = true
		return app.UpdateResult{}, nil
	},
		app.UpdateOptions{Source: t.TempDir()})
	if !app.IsUsage(err) || called {
		t.Fatalf("nguồn không hợp lệ: %v called=%v", err, called)
	}
}
