package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	bintapp "bint/internal/app"
	"bint/internal/fsafe"
	"bint/internal/tui/actions"
	syscreen "bint/internal/tui/screens/system"
	"bint/internal/tui/theme"
)

// sysAt dựng mô hình đã ở tab Hệ thống, thẻ con tab (0 = Cấu hình), rồi áp keys.
func sysAt(t *testing.T, o Options, w, h, tab int, keys ...string) *Model {
	t.Helper()
	m := newModel(t, o, sampleData(), w, h)
	press(t, m, "7")
	for i := 0; i < tab; i++ {
		press(t, m, ".")
	}
	press(t, m, keys...)
	return m
}

// systemScreens liệt kê các ảnh chụp văn bản của màn Hệ thống: mỗi thẻ con và các trạng thái chính (khung chi tiết, hộp thoại, form).
func systemScreens(t *testing.T, lang string, w, h int) map[string]func() *Model {
	o := Options{Mode: theme.Mono, Lang: lang}
	plan := bintapp.SyncPlan{Changes: 2, Conflicts: 1, Files: []bintapp.SyncFile{
		{Skill: "b-do", Path: ".claude/skills/b-do/SKILL.md", Result: fsafe.Updated, Diff: "--- .claude/skills/b-do/SKILL.md (hiện tại)\n+++ .claude/skills/b-do/SKILL.md (sau sync)\n@@ -1,2 +1,2 @@\n tên\n-cũ\n+mới\n"},
		{Skill: "b-feat", Path: ".claude/skills/b-feat/SKILL.md", Result: fsafe.Conflict, Detail: "skill đã bị sửa ngoài Bint"},
		{Skill: "api-guide", Path: ".claude/skills/api-guide/SKILL.md", Result: fsafe.Created, Diff: "+++ .claude/skills/api-guide/SKILL.md (sau sync)\n+nội dung\n"}}}
	return map[string]func() *Model{
		"system_config":         func() *Model { return sysAt(t, o, w, h, 0) },
		"system_config_group":   func() *Model { return sysAt(t, o, w, h, 0, "j", "j", "enter") },
		"system_config_edit":    func() *Model { return sysAt(t, o, w, h, 0, "e") },
		"system_harness":        func() *Model { return sysAt(t, o, w, h, 1) },
		"system_harness_detail": func() *Model { return sysAt(t, o, w, h, 1, "enter") },
		"system_components":     func() *Model { return sysAt(t, o, w, h, 2) },
		"system_component_detail": func() *Model {
			return sysAt(t, o, w, h, 2, "j", "j", "enter")
		},
		"system_component_add": func() *Model { return sysAt(t, o, w, h, 2, "n") },
		"system_script_approve": func() *Model { // hộp đỏ duyệt script
			return sysAt(t, o, w, h, 2, "j", "j", "a")
		},
		"system_skills":       func() *Model { return sysAt(t, o, w, h, 3) },
		"system_skill_detail": func() *Model { return sysAt(t, o, w, h, 3, "j", "enter") },
		"system_skill_diff":   func() *Model { return sysAt(t, o, w, h, 3, "j", "j", "d") },
		"system_sync_preview": func() *Model {
			m := sysAt(t, o, w, h, 3)
			m.Update(actions.ResultMsg{ID: "system.sync_plan", Opts: bintapp.SyncOptions{}, Value: plan})
			return m
		},
		"system_sync_force": func() *Model {
			m := sysAt(t, o, w, h, 3)
			m.Update(actions.ResultMsg{ID: "system.sync_plan", Opts: bintapp.SyncOptions{Force: true}, Value: plan})
			return m
		},
		"system_index":         func() *Model { return sysAt(t, o, w, h, 4) },
		"system_index_confirm": func() *Model { return sysAt(t, o, w, h, 4, "b") },
		"system_mcp":           func() *Model { return sysAt(t, o, w, h, 5) },
		"system_language":      func() *Model { return sysAt(t, o, w, h, 6) },
		"system_language_form": func() *Model { return sysAt(t, o, w, h, 6, "j", "enter") },
		"system_doctor":        func() *Model { return sysAt(t, o, w, h, 7) },
		"system_doctor_issues": func() *Model { return sysAt(t, o, w, h, 7, "w") },
	}
}

// Ảnh chụp văn bản của màn Hệ thống ở 80×24 và 140×40, vi/en/locale giả; không dòng nào vượt chiều rộng.
func TestGoldenSystemScreens(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {140, 40}} {
		for _, lang := range []string{"vi", "en", "xx"} {
			for name, build := range systemScreens(t, lang, sz[0], sz[1]) {
				out := plain(build().Render())
				for i, l := range strings.Split(out, "\n") {
					if w := ansi.StringWidth(l); w > sz[0] {
						t.Errorf("%s %dx%d %s: dòng %d rộng %d > %d: %q", name, sz[0], sz[1], lang, i, w, sz[0], l)
					}
				}
				if n := len(strings.Split(out, "\n")); n > sz[1] {
					t.Errorf("%s %dx%d %s: %d dòng > %d", name, sz[0], sz[1], lang, n, sz[1])
				}
				checkGolden(t, fmt.Sprintf("%s_%dx%d_%s.txt", name, sz[0], sz[1], lang), out+"\n")
			}
		}
	}
}

// Không chuỗi nào ở locale giả hay vi/en còn là khoá chưa dịch (dạng "sy.x.y" hay "action.system.…"): mọi thẻ và trạng thái.
func TestSystemScreensHaveNoUntranslatedKeys(t *testing.T) {
	for _, lang := range []string{"vi", "en", "xx"} {
		for name, build := range systemScreens(t, lang, 140, 40) {
			out := plain(build().Render())
			for _, bad := range []string{"sy.", "action.system.", "form.system."} {
				if strings.Contains(out, bad) {
					t.Errorf("%s (%s) còn khoá chưa dịch %q:\n%s", name, lang, bad, out)
				}
			}
		}
	}
}

// Ảnh chụp có mã màu của thẻ Thành phần (chi tiết script có cảnh báo) và Harness ở từng chế độ màu.
func TestGoldenSystemColorModes(t *testing.T) {
	for _, mode := range allModes() {
		m := sysAt(t, Options{Mode: mode, Lang: "vi"}, 140, 40, 2, "j", "j")
		checkGolden(t, fmt.Sprintf("system_components_140x40_vi_%s.ansi.txt", mode), strings.ReplaceAll(m.Render(), "\x1b", "␛")+"\n")
		m = sysAt(t, Options{Mode: mode, Lang: "vi"}, 140, 40, 1)
		checkGolden(t, fmt.Sprintf("system_harness_140x40_vi_%s.ansi.txt", mode), strings.ReplaceAll(m.Render(), "\x1b", "␛")+"\n")
	}
}

var _ = syscreen.TabConfig
