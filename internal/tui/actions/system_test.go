package actions_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/app"
	"bint/internal/app/evolvesvc"
	"bint/internal/config"
	"bint/internal/hcomp"
	"bint/internal/tui/actions"
)

// Hành động ghi của tab Hệ thống đều khai Writes; hành động đọc thì không (chỉ-đọc ẩn mọi hành động ghi, mục 7.4).
func TestSystemActionsWritesFlags(t *testing.T) {
	reg := actions.Default()
	writes := map[string]bool{
		"system.config_edit": true, "system.config_save": true, "system.language_save": true, "system.component_add": true,
		"system.component_approve": true, "system.component_deprecate": true, "system.skill_overlay": true, "system.skill_new": true, "system.open_file": true,
		"system.sync": true, "system.index_rebuild": true,
		// Giai đoạn 5b: chạy lại init, nâng cấu hình và cập nhật Bint đều ghi.
		"system.repo_apply": true, "system.config_upgrade": true, "system.update": true,
	}
	n := 0
	for _, a := range reg.All() {
		if a.Screen != actions.System {
			continue
		}
		n++
		if a.Writes != writes[a.ID] {
			t.Errorf("%s: Writes = %v, muốn %v", a.ID, a.Writes, writes[a.ID])
		}
		if want := a.ID == "system.component_approve" || a.ID == "system.component_deprecate"; a.HumanOnly != want {
			t.Errorf("%s: HumanOnly = %v, muốn %v", a.ID, a.HumanOnly, want)
		}
		if a.Check(actions.Mode{ReadOnly: true}) == actions.Available == a.Writes {
			t.Errorf("%s: chế độ chỉ đọc phải ẩn hành động ghi và chỉ chúng", a.ID)
		}
	}
	if n < 30 {
		t.Fatalf("chỉ có %d hành động system", n)
	}
	for id := range writes {
		if _, ok := reg.ByID(id); !ok {
			t.Errorf("thiếu hành động %s", id)
		}
	}
}

func TestSystemReadActionsWithRealServices(t *testing.T) {
	root, _ := seed(t)
	env := actions.Env{Dir: root, Version: "test"}
	if r := do(t, "system.config_view", env, nil); r.Err != nil || len(r.Value.(app.ConfigViewData).Groups) != 14 {
		t.Fatalf("config_view: %+v", r)
	}
	if r := do(t, "system.harness_list", env, app.HarnessListOptions{NoVersion: true}); r.Err != nil {
		t.Fatalf("harness_list: %v", r.Err)
	}
	if r := do(t, "system.harness_show", env, app.HarnessShowOptions{Name: "claude"}); r.Err != nil {
		t.Fatalf("harness_show: %v", r.Err)
	}
	if r := do(t, "system.harness_show", env, app.HarnessShowOptions{Name: "khong-co"}); !app.IsUsage(r.Err) {
		t.Fatalf("harness lạ: %v", r.Err)
	}
	if r := do(t, "system.harness_runtime", env, nil); r.Err != nil {
		t.Fatalf("harness_runtime: %v", r.Err)
	}
	if r := do(t, "system.harness_version", env, actions.HarnessVersionOptions{Name: "claude"}); r.Err != nil || r.Value.(actions.VersionResult).Name != "claude" {
		t.Fatalf("harness_version: %+v", r)
	}
	if r := do(t, "system.component_list", env, app.ComponentListOptions{}); r.Err != nil {
		t.Fatalf("component_list: %v", r.Err)
	}
	if r := do(t, "system.render", env, app.RenderDefaults(config.Default())); r.Err != nil || !strings.Contains(r.Value.(string), "Harness: claude") {
		t.Fatalf("render: %+v", r)
	}
	if r := do(t, "system.skill_list", env, app.SkillListOptions{}); r.Err != nil || len(r.Value.(app.SkillList).Rows) == 0 {
		t.Fatalf("skill_list: %+v", r)
	}
	if r := do(t, "system.skill_show", env, app.SkillShowOptions{Name: "b-do", Core: true}); r.Err != nil || len(r.Value.(app.SkillContent).Content) == 0 {
		t.Fatalf("skill_show: %+v", r)
	}
	if r := do(t, "system.skill_regress", env, evolvesvc.SkillRegressOptions{Model: "m"}); r.Err != nil {
		t.Fatalf("skill_regress: %v", r.Err)
	}
	if r := do(t, "system.skill_check", env, evolvesvc.SkillCheckOptions{Target: "khong-co"}); r.Err == nil {
		t.Fatal("skill_check skill lạ phải lỗi")
	}
	if r := do(t, "system.sync_plan", env, app.SyncOptions{}); r.Err != nil {
		t.Fatalf("sync_plan: %v", r.Err)
	}
	if _, ok := actions.Default().ByID("system.mcp_tools"); ok {
		t.Fatal("system.mcp_tools đã chuyển sang màn MCP (mcp.tools)")
	}
	if r := do(t, "system.doctor", env, nil); r.Err != nil || len(r.Value.(*app.DoctorReport).Checks) == 0 {
		t.Fatalf("doctor: %+v", r)
	}
}

func TestSystemWriteActionsWithRealServices(t *testing.T) {
	root, _ := seed(t)
	env := actions.Env{Dir: root, Version: "test"}

	// Thành phần: thêm từ mẫu (candidate), duyệt (người), loại bỏ.
	if r := do(t, "system.component_add", env, app.AddComponentOptions{Template: "test-before-stop"}); r.Err != nil {
		t.Fatalf("component_add: %v", r.Err)
	}
	if r := do(t, "system.component_approve", env, app.ComponentReviewOptions{ID: "test-before-stop"}); r.Err != nil || r.Value.(hcomp.Component).Status != hcomp.StatusVerified {
		t.Fatalf("component_approve: %+v", r)
	}
	if r := do(t, "system.component_deprecate", env, app.ComponentReviewOptions{ID: "test-before-stop"}); r.Err != nil {
		t.Fatalf("component_deprecate: %v", r.Err)
	}
	// Lớp phủ: tạo, rồi chỉ báo đã có.
	if r := do(t, "system.skill_overlay", env, app.SkillOverlayOptions{Name: "b-do"}); r.Err != nil || r.Value.(app.OverlayResult).Existed {
		t.Fatalf("skill_overlay: %+v", r)
	}
	if r := do(t, "system.skill_overlay", env, app.SkillOverlayOptions{Name: "b-do"}); r.Err != nil || !r.Value.(app.OverlayResult).Existed {
		t.Fatalf("skill_overlay lần hai: %+v", r)
	}
	// Skill tuỳ biến: tạo mẫu, lần hai không ghi đè, rồi kiểm (mẫu qua lint không lỗi; thiếu tên thì lỗi cách dùng).
	if r := do(t, "system.skill_new", env, app.SkillNewOptions{Name: "demo-skill"}); r.Err != nil || r.Value.(app.SkillNewResult).Existed {
		t.Fatalf("skill_new: %+v", r)
	}
	if r := do(t, "system.skill_new", env, app.SkillNewOptions{Name: "demo-skill"}); r.Err != nil || !r.Value.(app.SkillNewResult).Existed {
		t.Fatalf("skill_new lần hai: %+v", r)
	}
	if r := do(t, "system.skill_lint", env, app.SkillLintOptions{}); r.Err != nil || len(r.Value.([]app.SkillLintResult)) != 1 || !r.Value.([]app.SkillLintResult)[0].OK() {
		t.Fatalf("skill_lint: %+v", r)
	}
	if r := do(t, "system.skill_new", env, app.SkillNewOptions{Name: "b-bad"}); r.Err == nil {
		t.Fatal("tên b- phải bị từ chối")
	}
	// Chỉ mục.
	if r := do(t, "system.index_rebuild", env, app.IndexRebuildOptions{}); r.Err != nil || r.Value.(app.IndexRebuildResult).Total != 2 {
		t.Fatalf("index_rebuild: %+v", r)
	}
	// Ngôn ngữ.
	if r := do(t, "system.language_save", env, app.UILanguageOptions{Language: "en", Layer: "local"}); r.Err != nil {
		t.Fatalf("language_save: %v", r.Err)
	}
	if b, _ := os.ReadFile(config.LocalPath(root)); !strings.Contains(string(b), "language: en") {
		t.Fatalf("local.yml: %q", b)
	}
	// Sync: xoá một skill đã xuất bản, sync ghi lại.
	target := filepath.Join(root, ".claude", "skills", "b-do", "SKILL.md")
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if r := do(t, "system.sync", env, app.SyncOptions{}); r.Err != nil {
		t.Fatalf("sync: %v", r.Err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("sync phải ghi lại skill: %v", err)
	}
}

// Sửa cấu hình qua trình soạn thảo giả: bản sao tạm được mở, sau đó kiểm và ghi.
func TestSystemConfigEditFlowWithFakeEditor(t *testing.T) {
	root, _ := seed(t)
	var gotPath string
	env := actions.Env{Dir: root, Editor: func(p string) *execCmd { gotPath = p; return newCmd("true") }}
	a, _ := actions.Default().ByID("system.config_edit")
	msg := a.Do(context.Background(), env, actions.ConfigEditOptions{Layer: "project"})
	em, ok := msg.(actions.ExecMsg)
	if !ok || gotPath == "" || em.Cmd == nil {
		t.Fatalf("%T %v", msg, msg)
	}
	st := em.Opts.(actions.ConfigEditState)
	defer os.Remove(st.Edit.TempPath)
	orig, _ := os.ReadFile(config.ProjectPath(root))
	bad := []byte(strings.Replace(string(orig), "language: auto", "language: fr", 1))
	if r := do(t, "system.config_check", env, actions.ConfigApplyArgs{Edit: st.Edit, Data: bad}); r.Err != nil || r.Value.(app.ConfigCheck).Valid {
		t.Fatalf("nội dung sai phải bị báo: %+v", r)
	}
	if r := do(t, "system.config_save", env, actions.ConfigApplyArgs{Edit: st.Edit, Data: bad}); r.Err == nil {
		t.Fatal("ghi nội dung sai phải lỗi")
	}
	if b, _ := os.ReadFile(config.ProjectPath(root)); string(b) != string(orig) {
		t.Fatal("tệp gốc không được đổi")
	}
	good := []byte(strings.Replace(string(orig), "language: auto", "language: vi", 1))
	if r := do(t, "system.config_save", env, actions.ConfigApplyArgs{Edit: st.Edit, Data: good}); r.Err != nil {
		t.Fatalf("%v", r.Err)
	}
	if b, _ := os.ReadFile(config.ProjectPath(root)); string(b) != string(good) {
		t.Fatal("tệp phải được ghi")
	}
}
