// Package placeholder là trang giữ chỗ cho các tab chưa có màn hình thật: nói rõ giai đoạn dự kiến
// và liệt kê lệnh CLI tương đương để người dùng không bị chặn trong lúc chờ.
package placeholder

import (
	"strings"

	"bint/internal/i18n"
	"bint/internal/tui/actions"
	"bint/internal/tui/components/text"
	"bint/internal/tui/screens/overview"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

// Info mô tả một tab chưa làm.
type Info struct {
	Phase    int
	Commands []string
}

// Tabs là thông tin các tab giữ chỗ (khớp giai đoạn trong docs/tui-plan.md mục 9).
var Tabs = map[actions.Screen]Info{
	actions.System: {Phase: 5, Commands: []string{"bint doctor", "bint init --update", "bint sync", "bint harness list", "bint update"}},
}

// maxPendingCommands giới hạn số lệnh CLI liệt kê cho mỗi dòng việc đang chờ.
const maxPendingCommands = 6

// View vẽ trang giữ chỗ của tab s: giai đoạn dự kiến, các việc đang chờ ở tab này (từ hộp thư) kèm lệnh
// CLI xử lý từng việc, rồi các lệnh CLI tương đương chung.
func View(env ui.Env, s actions.Screen, pending []overview.Inbox) string {
	th := env.Th
	info := Tabs[s]
	var lines []string
	add := func(l ...string) { lines = append(lines, l...) }
	add("", "  "+th.Bold(env.T("tab."+string(s))))
	add("")
	for _, l := range text.Wrap(env.T("placeholder.coming", map[string]any{"phase": info.Phase}), max(env.W-4, 10)) {
		add("  " + th.S(theme.Muted, l))
	}
	if len(pending) > 0 {
		add("", "  "+th.S(theme.Accent, env.T("placeholder.pending")))
		for _, in := range pending {
			icon, role := "◌", theme.Accent
			switch in.Level {
			case "warn":
				icon, role = "!", theme.Warn
			case "fail":
				icon, role = "✗", theme.Err
			}
			add("  " + th.S(role, icon) + " " + text.Truncate(env.N(in.Key, in.Count, i18n.A{"detail": in.Detail}), max(env.W-6, 8)))
			for i, c := range in.Commands {
				if i == maxPendingCommands {
					add("      " + th.S(theme.Muted, env.T("placeholder.more", i18n.A{"count": len(in.Commands) - maxPendingCommands})))
					break
				}
				add("      " + th.S(theme.Key, "$") + " " + c)
			}
		}
	}
	add("", "  "+th.S(theme.Accent, env.T("placeholder.meanwhile")))
	for _, c := range info.Commands {
		add("    " + th.S(theme.Key, "$") + " " + c)
	}
	if len(lines) > env.H {
		lines = lines[:env.H]
	}
	return strings.Join(lines, "\n")
}
