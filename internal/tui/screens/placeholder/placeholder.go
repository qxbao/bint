// Package placeholder là trang giữ chỗ cho sáu tab chưa có màn hình thật: nói rõ giai đoạn dự kiến
// và liệt kê lệnh CLI tương đương để người dùng không bị chặn trong lúc chờ.
package placeholder

import (
	"strings"

	"bint/internal/tui/actions"
	"bint/internal/tui/components/text"
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
	actions.Knowledge: {Phase: 1, Commands: []string{"bint knowledge list", "bint knowledge review", "bint knowledge search <query>", "bint knowledge add", "bint context <task>"}},
	actions.Tasks:     {Phase: 2, Commands: []string{"bint task new <request>", "bint task list", "bint task show <id>", "bint run <workflow> <id>"}},
	actions.Runs:      {Phase: 3, Commands: []string{"bint runs list", "bint runs show <id>", "bint runs rate <id>", "bint experience digest <batch>"}},
	actions.Eval:      {Phase: 4, Commands: []string{"bint eval mine", "bint eval approve <id>", "bint eval estimate", "bint eval run", "bint eval report"}},
	actions.Evolve:    {Phase: 3, Commands: []string{"bint evolve scan", "bint evolve propose", "bint evolve evaluate <id>", "bint evolve apply <id>", "bint evolve rollback <id>"}},
	actions.System:    {Phase: 5, Commands: []string{"bint doctor", "bint init --update", "bint sync", "bint harness list", "bint update"}},
}

// View vẽ trang giữ chỗ của tab s.
func View(env ui.Env, s actions.Screen) string {
	th := env.Th
	info := Tabs[s]
	var lines []string
	add := func(l ...string) { lines = append(lines, l...) }
	add("", "  "+th.Bold(env.T("tab."+string(s))))
	add("")
	for _, l := range text.Wrap(env.T("placeholder.coming", map[string]any{"phase": info.Phase}), max(env.W-4, 10)) {
		add("  " + th.S(theme.Muted, l))
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
