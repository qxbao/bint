package initwizard

import (
	"bint/internal/app"
	"bint/internal/i18n"
	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

var stepIcon = map[app.StepStatus]struct {
	icon string
	role theme.Role
}{
	app.StepCreated:  {"+", theme.OK},
	app.StepUpdated:  {"~", theme.OK},
	app.StepOK:       {"✓", theme.OK},
	app.StepSkipped:  {"-", theme.Muted},
	app.StepWarn:     {"!", theme.Warn},
	app.StepConflict: {"✗", theme.Err},
}

// doneView vẽ kết quả: báo cáo các bước và "bước tiếp theo" chọn được (thành công) hoặc lỗi kèm hướng xử lý.
func (m *Model) doneView(env ui.Env) string {
	th := env.Th
	var lines []string
	add := func(l ...string) { lines = append(lines, l...) }
	wrap := func(role theme.Role, s string) {
		for _, l := range text.WrapHang(s, max(env.W-2, 10), "  ", "  ") {
			add(th.S(role, l))
		}
	}
	dir := text.Path(m.o.Dir, max(env.W-24, 20))
	switch {
	case m.denied && m.err == nil && m.report == nil:
		add("", "  "+th.Bold(m.t("wiz.title.error")), "")
		wrap(theme.Warn, m.t("ui.locked.agent"))
	case m.err != nil:
		add("", "  "+th.Bold(m.t("wiz.title.error")), "")
		wrap(theme.Fg, m.t("wiz.intro.error", i18n.A{"dir": dir}))
		add("", "  "+th.S(theme.Err, "✗ ")+m.t("wiz.error"))
		wrap(theme.Err, text.TildeIn(ui.ErrText(m.rw, m.err)))
		add("")
		wrap(theme.Muted, m.t("wiz.error.hint"))
	default:
		add("", "  "+th.Bold(m.t("wiz.title.done")), "")
		wrap(theme.Fg, m.t("wiz.intro.done", i18n.A{"dir": dir}))
		add("")
		add("  " + th.S(theme.OK, "✓ ") + th.Bold(m.t("wiz.done")))
		if m.loadErr != nil {
			wrap(theme.Warn, m.t("wiz.reload_failed", i18n.A{"error": text.TildeIn(m.loadErr.Error())}))
		}
		lines = append(lines, m.reportLines(env, len(lines)+1)...)
	}
	foot := m.doneHints(th, env.W-4)
	for len(lines) < env.H-1 {
		lines = append(lines, "")
	}
	if len(lines) > env.H-1 {
		lines = lines[:env.H-1]
	}
	return fit(append(lines, foot), env.H)
}

func (m *Model) doneHints(th theme.Theme, maxW int) string {
	if m.err != nil || m.report == nil {
		return "  " + m.hints(th, maxW, "wiz.hint.back", "Esc", "wiz.hint.recheck", "r")
	}
	return "  " + m.hints(th, maxW, "wiz.hint.continue", "Enter", "wiz.hint.pick", "j/k")
}

// reportLines vẽ các bước và "bước tiếp theo" của báo cáo vào phần còn lại của màn hình (used là số dòng đã vẽ, gồm dòng gợi ý
// cuối). Bước cảnh báo/xung đột đứng trước; khi không đủ chỗ thì rút gọn danh sách bước bằng một dòng "… và N bước khác" để các
// bước tiếp theo luôn hiện đủ.
func (m *Model) reportLines(env ui.Env, used int) []string {
	th := env.Th
	var problems, rest []string
	for _, s := range m.report.Steps {
		si := stepIcon[s.Status]
		l := "   " + th.S(si.role, si.icon) + " " + stepText(th, text.TildeIn(s.Name), m.cli(text.TildeIn(s.Detail)), max(env.W-8, 8))
		if s.Status == app.StepWarn || s.Status == app.StepConflict {
			problems = append(problems, l)
		} else {
			rest = append(rest, l)
		}
	}
	steps := append(problems, rest...)
	var next []string
	if len(m.report.NextSteps) > 0 {
		next = append(next, "", "  "+th.Bold(m.t("wiz.next")))
		for i, s := range m.report.NextSteps {
			marker, num := "   ", th.S(theme.Key, string(rune('1'+i))+".")
			body := text.Truncate(m.cli(s), max(env.W-9, 8))
			if i == m.next {
				marker, body = " "+th.S(theme.Key, "▸")+" ", th.Selected(body)
			}
			next = append(next, marker+num+" "+body)
		}
	}
	if avail := env.H - used - len(next); len(steps) > avail {
		keep := max(avail-1, 0)
		more := "  " + th.S(theme.Muted, m.t("wiz.more_steps", i18n.A{"count": len(steps) - keep}))
		steps = append(steps[:keep:keep], more)
	}
	return append(steps, next...)
}

// stepText ghép tên và chi tiết của một bước trong w ô. Chi tiết thường là đường dẫn kèm hậu tố ("/…/state (schema v6)"), nên khi
// thiếu chỗ thì rút phần ĐẦU của chi tiết (giữ đuôi) chứ không cắt đuôi; chỉ khi chỗ cho chi tiết quá nhỏ mới cắt cả dòng.
func stepText(th theme.Theme, name, detail string, w int) string {
	if detail == "" {
		return text.Truncate(name, w)
	}
	if room := w - text.Width(name) - 2; text.Width(detail) > room && room >= 8 {
		detail = text.TruncateHead(detail, room)
	}
	return text.Truncate(name+"  "+th.S(theme.Muted, detail), w)
}
