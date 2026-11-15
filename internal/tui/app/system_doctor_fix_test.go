package app

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	bintapp "bint/internal/app"
	"bint/internal/tui/actions"
	syscreen "bint/internal/tui/screens/system"
)

func doctorFixModel(t *testing.T, ro bool) (*Model, *capt, *atomic.Int32) {
	t.Helper()
	var runs atomic.Int32
	loader := func(context.Context, string) (*bintapp.DoctorReport, error) {
		runs.Add(1)
		return &bintapp.DoctorReport{Checks: []bintapp.Check{
			{Name: "worktree mồ côi", Level: bintapp.LevelWarn, Detail: "wt-detail", Fix: &bintapp.DoctorFix{Kind: bintapp.FixWorktree, Path: "/state/worktrees/01ABC", TaskID: "01ABC"}},
			{Name: bintapp.CheckConfigTemplate, Level: bintapp.LevelWarn, Detail: "tpl-detail", Fix: &bintapp.DoctorFix{Kind: bintapp.FixConfigTemplate}},
			{Name: "skill b-do", Level: bintapp.LevelWarn, Detail: "sync-detail", Fix: &bintapp.DoctorFix{Kind: bintapp.FixSync}},
			{Name: "lỗi hook", Level: bintapp.LevelWarn, Detail: "plain-detail"},
		}}, nil
	}
	m, c := newSysModel(t, Options{ReadOnly: ro, SystemLoad: syscreen.Loaders{Doctor: loader}}, 140, 40)
	press(t, m, ",", ",", ",")
	return m, c, &runs
}

func TestSystemDoctorFixWorktreeConfirmFlow(t *testing.T) {
	m, c, runs := doctorFixModel(t, false)
	out := plain(m.Render())
	hasAll(t, out, "worktree mồ côi", "wt-detail", "sửa")
	press(t, m, "f")
	hasAll(t, plain(m.Render()), "Sửa mục doctor?", "/state/worktrees/01ABC", "bint/01ABC")
	press(t, m, "esc")
	if c.count("system.doctor_fix") != 0 {
		t.Fatal("chọn Không thì Do không được gọi")
	}
	before := runs.Load()
	press(t, m, "f", "enter")
	if c.count("system.doctor_fix") != 1 {
		t.Fatalf("chọn Có thì Do được gọi đúng một lần: %d", c.count("system.doctor_fix"))
	}
	o := c.last(t, "system.doctor_fix").(bintapp.DoctorFixOptions)
	if o.Kind != bintapp.FixWorktree || o.Path != "/state/worktrees/01ABC" || o.TaskID != "01ABC" {
		t.Fatalf("%+v", o)
	}
	m.Update(actions.ResultMsg{ID: "system.doctor_fix", Opts: o, Value: bintapp.DoctorFixResult{Kind: o.Kind, Actions: []string{"đã gỡ worktree x"}}})
	hasAll(t, plain(m.Render()), "Đã sửa: đã gỡ worktree x")
	if runs.Load() <= before {
		t.Fatal("sau khi sửa phải nạp lại doctor")
	}
}

func TestSystemDoctorFixConfigAndSyncOpenExistingPreviewFlows(t *testing.T) {
	m, c, _ := doctorFixModel(t, false)
	press(t, m, "j", "f")
	if c.count("system.config_upgrade_plan") != 1 || c.count("system.doctor_fix") != 0 || c.count("system.config_upgrade") != 0 {
		t.Fatalf("mẫu cấu hình phải đi qua xem trước: plan=%d fix=%d ghi=%d", c.count("system.config_upgrade_plan"), c.count("system.doctor_fix"), c.count("system.config_upgrade"))
	}
	press(t, m, "j", "f")
	if c.count("system.sync_plan") != 1 || c.count("system.doctor_fix") != 0 || c.count("system.sync") != 0 {
		t.Fatalf("sync phải đi qua xem trước: plan=%d fix=%d ghi=%d", c.count("system.sync_plan"), c.count("system.doctor_fix"), c.count("system.sync"))
	}
}

func TestSystemDoctorFixNoFixItemDoesNothing(t *testing.T) {
	m, c, _ := doctorFixModel(t, false)
	press(t, m, "j", "j", "j")
	out := plain(m.Render())
	hasAll(t, out, "plain-detail")
	if strings.Contains(out, "f sửa") || strings.Contains(out, "sửa mục này") {
		t.Fatalf("mục không có Fix không được gợi ý f:\n%s", out)
	}
	press(t, m, "f")
	if m.sy.Capturing() || c.count("system.doctor_fix") != 0 || c.count("system.sync_plan") != 0 || c.count("system.config_upgrade_plan") != 0 {
		t.Fatal("f trên mục không có Fix không được làm gì")
	}
}

func TestSystemDoctorFixHiddenInReadOnly(t *testing.T) {
	m, c, _ := doctorFixModel(t, true)
	out := plain(m.Render())
	hasAll(t, out, "wt-detail")
	if strings.Contains(out, "sửa mục này") || strings.Contains(out, "f sửa") {
		t.Fatalf("chế độ chỉ đọc phải ẩn gợi ý f:\n%s", out)
	}
	press(t, m, "f")
	press(t, m, "enter")
	press(t, m, "j", "f")
	press(t, m, "j", "f")
	if c.count("system.doctor_fix") != 0 || c.count("system.config_upgrade_plan") != 0 || c.count("system.sync_plan") != 0 {
		t.Fatal("chế độ chỉ đọc: f không được tới Do")
	}
}
