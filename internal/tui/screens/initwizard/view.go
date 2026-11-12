package initwizard

import (
	"strings"

	"bint/internal/app"
	"bint/internal/config"
	"bint/internal/i18n"
	"bint/internal/tui/components/footer"
	"bint/internal/tui/components/form"
	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

var changeIcon = map[app.PlanChange]struct {
	icon string
	role theme.Role
}{
	app.PlanCreate: {"+", theme.OK},
	app.PlanUpdate: {"~", theme.OK},
	app.PlanRemove: {"-", theme.Warn},
	app.PlanKeep:   {"·", theme.Muted},
}

// View vẽ màn hình trong env.W × env.H: tiêu đề + nội dung bước, dòng gợi ý phím ở cuối.
func (m *Model) View(env ui.Env) string {
	th := env.Th
	var head, body, foot []string
	wrapWith := func(dst *[]string, role theme.Role, s, first, rest string) {
		for _, l := range text.WrapHang(s, max(env.W-2, 10), first, rest) {
			*dst = append(*dst, th.S(role, l))
		}
	}
	para := func(dst *[]string, role theme.Role, s string) { wrapWith(dst, role, s, "  ", "  ") }
	dir := text.Path(m.o.Dir, max(env.W-24, 20))

	switch {
	case m.o.ReadOnly:
		head = append(head, "", "  "+th.Bold(m.t("wiz.title")), "")
		para(&head, theme.Fg, m.t("wiz.intro", i18n.A{"dir": dir}))
		head = append(head, "")
		para(&head, theme.Warn, m.t("init.readonly"))
		head = append(head, "", "  "+th.S(theme.Muted, m.t("ui.cli_equivalent")+" ")+th.S(theme.Accent, "bint init"))
		return fit(append(head, body...), env.H)
	case m.running:
		head = append(head, "", "  "+th.Bold(m.t("wiz.title")), "")
		body = append(body, "  "+th.S(theme.Accent, env.Spin)+" "+m.t("wiz.running"))
		return fit(append(head, body...), env.H)
	case m.done:
		return m.doneView(env)
	}

	head = append(head, "", "  "+th.Bold(m.t("wiz.title"))+"  "+th.S(theme.Muted, m.t("wiz.step", i18n.A{
		"n": m.step + 1, "total": numSteps, "name": m.t("wiz.step." + stepNames[m.step])})))
	if m.step == stepHarness {
		head = append(head, "")
		para(&head, theme.Muted, m.t("wiz.intro", i18n.A{"dir": dir}))
	}
	head = append(head, "")

	if m.step == stepPreview {
		body, foot = m.previewView(env)
	} else {
		body = m.askView(env)
		foot = []string{"  " + m.hints(th, env.W-4, "wiz.hint.next", "Enter", "wiz.hint.back", "Esc", "wiz.hint.toggle", "Space", "wiz.hint.default", "d")}
	}
	room := env.H - len(head) - len(foot)
	if len(body) > room {
		keep := max(room-1, 0)
		more := "  " + th.S(theme.Muted, m.t("wiz.more", i18n.A{"count": len(body) - keep}))
		body = append(body[:keep:keep], more)
	}
	out := append(head, body...)
	for len(out) < env.H-len(foot) {
		out = append(out, "")
	}
	return fit(append(out, foot...), env.H)
}

// fit cắt hoặc kéo dài các dòng cho đúng h dòng.
func fit(lines []string, h int) string {
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

// hints dựng dòng gợi ý phím từ các cặp (khoá nhãn, phím), vừa maxW ô (rút nhãn trước khi bỏ phím) và bấm được bằng chuột.
func (m *Model) hints(th theme.Theme, maxW int, pairs ...string) string {
	var hs []footer.Hint
	for i := 0; i+1 < len(pairs); i += 2 {
		hs = append(hs, footer.Hint{Key: pairs[i+1], Label: m.t(pairs[i]), Priority: len(hs) + 1})
	}
	return strings.TrimSuffix(strings.TrimPrefix(footer.Inline(th, hs, maxW), " "), " ")
}

// askView vẽ mô tả và các hàng của bước hỏi.
func (m *Model) askView(env ui.Env) []string {
	th := env.Th
	var out []string
	for _, l := range text.WrapHang(m.t("wiz."+stepNames[m.step]+".desc"), max(env.W-2, 10), "  ", "  ") {
		out = append(out, th.S(theme.Fg, l))
	}
	out = append(out, "")
	rs := m.rows()
	for i, r := range rs {
		if m.step == stepHooks && i == 2 { // nhóm phạm vi
			out = append(out, "", "  "+th.S(theme.Muted, m.t("wiz.hooks.scope")))
		}
		label := r.label
		if !r.raw {
			label = m.t(r.label)
		}
		box := "( )"
		if r.group == -1 {
			box = "[ ]"
			if r.on {
				box = "[x]"
			}
		} else if r.on {
			box = "(•)"
		}
		line := box + " " + label
		if r.hint != "" {
			line += "  " + th.S(theme.Muted, m.t(r.hint))
		}
		switch {
		case i == m.cur:
			out = append(out, th.S(theme.Key, "▸ ")+th.Selected(text.Truncate(line, max(env.W-4, 8))))
		case r.dim:
			out = append(out, "  "+th.S(theme.Muted, text.Truncate(line, max(env.W-4, 8))))
		default:
			out = append(out, "  "+text.Truncate(line, max(env.W-4, 8)))
		}
	}
	if m.msg != "" {
		out = append(out, "", "  "+th.S(theme.Err, "✗ ")+m.msg)
	}
	if m.step == stepGit {
		out = append(out, "")
		key := "wiz.git." + m.git + ".desc"
		for _, l := range text.WrapHang(m.t(key), max(env.W-4, 10), "    ", "    ") {
			out = append(out, th.S(theme.Muted, l))
		}
	}
	return out
}

// previewView vẽ bước xem trước: danh sách tệp dự kiến, dòng CLI tương đương và gợi ý phím.
func (m *Model) previewView(env ui.Env) (body, foot []string) {
	th := env.Th
	switch {
	case m.planning:
		body = append(body, "  "+th.S(theme.Accent, env.Spin)+" "+th.S(theme.Muted, m.t("wiz.preview.planning")))
	case m.planErr != nil:
		body = append(body, "  "+th.S(theme.Err, "✗ ")+m.t("wiz.preview.error"))
		for _, l := range text.WrapHang(ui.ErrText(m.rw, m.planErr), max(env.W-4, 10), "    ", "    ") {
			body = append(body, th.S(theme.Err, l))
		}
	case m.plan != nil:
		for _, l := range text.WrapHang(m.t("wiz.preview.desc"), max(env.W-2, 10), "  ", "  ") {
			body = append(body, th.S(theme.Fg, l))
		}
		body = append(body, "")
		// Mục đổi trước, mục giữ nguyên sau.
		var changes, keeps []app.PlanItem
		for _, it := range m.plan.Items {
			if it.Change == app.PlanKeep {
				keeps = append(keeps, it)
			} else {
				changes = append(changes, it)
			}
		}
		for _, it := range append(changes, keeps...) {
			ci := changeIcon[it.Change]
			path := text.TildeIn(it.Path)
			detail := m.cli(text.TildeIn(it.Detail))
			line := "  " + th.S(ci.role, ci.icon) + " " + m.t("wiz.change."+string(it.Change)) + "  " + path
			if w := text.Width(line); detail != "" && w+2 < env.W-8 {
				line += "  " + th.S(theme.Muted, text.Truncate(detail, env.W-w-4))
			}
			body = append(body, text.Truncate(line, env.W))
		}
	}
	cli := form.CLIFor("init", app.InitDefaults(config.Default()), m.Options())
	// Dòng CLI dài được ngắt dòng (không cắt: người dùng phải đọc đủ các cờ sẽ chạy), tối đa 3 dòng.
	cliLines := text.WrapHang(m.t("ui.cli_equivalent")+" "+cli, max(env.W-2, 20), "  ", "      ")
	if len(cliLines) > 3 {
		cliLines = append(cliLines[:2:2], text.Truncate(strings.Join(cliLines[2:], " "), max(env.W-2, 20)))
	}
	for i, l := range cliLines {
		if i == 0 {
			l = th.S(theme.Muted, "  "+m.t("ui.cli_equivalent")+" ") + strings.TrimPrefix(l, "  "+m.t("ui.cli_equivalent")+" ")
		}
		foot = append(foot, l)
	}
	run := m.t("wiz.hint.run")
	foot = append(foot, "  "+th.S(theme.Muted, m.t("wiz.preview.estimate")))
	foot = append(foot, "  "+th.S(theme.Key, "Enter")+" "+th.S(theme.Muted, run)+"  "+th.S(theme.Key, "Esc")+" "+th.S(theme.Muted, m.t("wiz.hint.back")))
	return body, foot
}
