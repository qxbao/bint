package system

import (
	"strings"

	"bint/internal/app"
	"bint/internal/i18n"
	"bint/internal/tui/components/frame"
	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

// singlePane vẽ khung một ngăn của thẻ (Cài đặt repo, Cập nhật): kết quả hành động (out) thay nội dung khi có.
func (m *Model) singlePane(env ui.Env, title string, lines []string, hints string) string {
	th := env.Th
	if m.out != nil {
		iw, _ := frame.Inner(env.W, env.H)
		return m.detailBox(env, env.W, env.H, m.out.Title, fitLines(m.out.Lines, max(iw-1, 10)))
	}
	iw, ih := frame.Inner(env.W, env.H)
	lines = fitLines(lines, iw)
	if len(lines) > ih {
		lines = lines[:ih]
	}
	return frame.Box{Title: title, TitleAt: frame.Left, Focused: true, Bottom: hints, BottomAt: frame.Left}.Render(th, env.W, env.H, strings.Join(lines, "\n"))
}

// kv dựng dòng "nhãn: giá trị" với nhãn căn đều.
func kv(th theme.Theme, w int, label, value string) string {
	return " " + th.S(theme.Accent, text.Pad(label, w)) + " " + value
}

// ---- Cài đặt repo ----

func (m *Model) setupView(env ui.Env) string {
	th := env.Th
	iw, _ := frame.Inner(env.W, env.H)
	st := m.setup
	hints := hintsLine(th, [2]string{"e", m.t("sy.setup.hint.edit")}, [2]string{"U", m.t("sy.setup.hint.upgrade")})
	if st.NotInit {
		return m.singlePane(env, m.t("sy.setup.title"), []string{"", " " + th.S(theme.Warn, m.t("sy.setup.notinit"))}, "")
	}
	lw := 14
	out := []string{""}
	repo := text.Path(st.Root, max(iw-lw-4, 16))
	if st.LinkedWorktree {
		repo += "  " + th.S(theme.Warn, m.t("sy.setup.worktree", i18n.A{"main": text.Path(st.Main, max(iw/3, 12))}))
	}
	out = append(out, kv(th, lw, m.t("sy.setup.repo"), repo), "")
	out = append(out, " "+th.S(theme.Muted, m.t("sy.setup.harnesses")))
	for _, h := range st.Harnesses {
		state := th.S(theme.Muted, m.t("sy.setup.h.disabled"))
		if h.Enabled {
			state = th.S(theme.OK, m.t("sy.setup.h.enabled"))
		}
		if !h.Present {
			state += th.S(theme.Muted, " · "+m.t("sy.setup.h.absent"))
		}
		hook := th.S(theme.Muted, m.t("sy.setup.na"))
		if h.HasHooks {
			hook = th.S(theme.Warn, m.t("sy.setup.hook.none"))
			if len(h.HookScopes) > 0 {
				hook = th.S(theme.OK, strings.Join(h.HookScopes, "+"))
			}
		}
		mcp := th.S(theme.Muted, m.t("sy.setup.na"))
		if h.HasMCP {
			mcp = th.S(theme.Muted, m.t("sy.setup.mcp.off"))
			if h.MCP {
				mcp = th.S(theme.OK, m.t("sy.setup.mcp.on"))
			}
		}
		out = append(out, "  "+text.Pad(h.Name, 12)+" "+state)
		out = append(out, "    "+th.S(theme.Accent, "hook")+" "+hook+"   "+th.S(theme.Accent, "MCP")+" "+mcp)
	}
	out = append(out, "")
	git := m.t("sy.setup.git.track")
	switch st.GitMode {
	case app.GitLocal:
		git = m.t("sy.setup.git.local")
	case "":
		git += th.S(theme.Muted, " ("+m.t("sy.setup.git.unset")+")")
	}
	out = append(out, kv(th, lw, "Git", git))
	agents := th.S(theme.Muted, m.t("sy.setup.agents.off"))
	if st.AgentsBlock {
		agents = th.S(theme.OK, m.t("sy.setup.agents.on"))
	}
	out = append(out, kv(th, lw, "AGENTS.md", agents))
	tpl := th.S(theme.OK, "✓ "+m.t("sy.setup.template.ok"))
	if st.ConfigOutdated {
		tpl = th.S(theme.Warn, "! "+m.t("sy.setup.template.old", i18n.A{"keys": orDash(strings.Join(st.ConfigAdded, ", "))}))
	}
	out = append(out, kv(th, lw, m.t("sy.setup.template"), tpl), "")
	for _, l := range text.Wrap(m.t("sy.setup.about"), max(iw-2, 10)) {
		out = append(out, " "+th.S(theme.Muted, l))
	}
	return m.singlePane(env, m.t("sy.setup.title"), out, hints)
}

// ---- Cập nhật Bint ----

func (m *Model) updateView(env ui.Env) string {
	th := env.Th
	iw, ih := frame.Inner(env.W, env.H)
	lw := 12
	hints := hintsLine(th, [2]string{"u", m.t("sy.update.hint.update")})
	if m.upd.done && m.upd.err == nil && m.upd.out.Result.Target != "" {
		hints = hintsLine(th, [2]string{"u", m.t("sy.update.hint.update")}, [2]string{"R", m.t("sy.update.hint.restart")})
	}
	if m.upd.running {
		hints = hintsLine(th, [2]string{"Esc", m.t("sy.update.hint.cancel")})
	}
	info := m.updInfo
	ver := info.Version
	if info.Revision != "" {
		ver += " (" + info.Revision + ")"
	}
	out := []string{"", kv(th, lw, m.t("sy.update.running_ver"), ver)}
	if info.Exe != "" {
		out = append(out, kv(th, lw, m.t("sy.update.exe"), text.Path(info.Exe, max(iw-lw-4, 16))))
	}
	if info.SourceErr != nil {
		out = append(out, kv(th, lw, m.t("sy.update.source"), th.S(theme.Warn, m.t("sy.update.nosource"))))
	} else {
		out = append(out, kv(th, lw, m.t("sy.update.source"), text.Path(info.Source, max(iw-lw-4, 16))))
	}
	out = append(out, "")
	switch {
	case m.upd.running:
		out = append(out, " "+th.S(theme.Accent, env.Spin)+" "+m.t("sy.update.running"))
		var lines []string
		if m.o.Jobs != nil {
			if j := m.o.Jobs.Running("system.update"); j != nil {
				lines = j.Log.Lines
			}
		}
		out = append(out, m.logTail(th, lines, ih-len(out)-1, iw)...)
	case m.upd.done:
		out = append(out, m.updateResultLines(th, iw, ih-len(out))...)
	default:
		for _, l := range text.Wrap(m.t("sy.update.about"), max(iw-2, 10)) {
			out = append(out, " "+th.S(theme.Muted, l))
		}
	}
	return m.singlePane(env, m.t("sy.update.title"), out, hints)
}

// logTail hiện n dòng cuối của log (dòng cũ hơn được bỏ).
func (m *Model) logTail(th theme.Theme, lines []string, n, w int) []string {
	if n < 1 {
		return nil
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, "  "+th.S(theme.Muted, text.Truncate(l, w-3)))
	}
	return out
}

// updateResultLines dựng kết quả lần cập nhật gần nhất (kể cả lỗi): bản cũ → mới, tệp, bản sao lưu, cảnh báo Mismatch, log.
func (m *Model) updateResultLines(th theme.Theme, iw, room int) []string {
	r, err := m.upd.out.Result, m.upd.err
	var out []string
	if r.Pulled != "" {
		out = append(out, " "+m.t("sy.update.pulled", i18n.A{"out": strings.Split(r.Pulled, "\n")[0]}))
	}
	if err != nil {
		out = append(out, " "+th.S(theme.Err, "✗ ")+m.t("sy.update.failed"))
		for _, l := range wrapText(ui.ErrText(m.cat, err), max(iw-4, 10)) {
			out = append(out, "   "+th.S(theme.Err, l))
		}
	} else {
		out = append(out, " "+th.S(theme.OK, "✓ ")+m.t("sy.update.versions", i18n.A{"old": orUnknown(m, r.OldRev), "new": orUnknown(m, r.NewRev)}))
		out = append(out, "   "+th.S(theme.Muted, text.Path(r.Target, max(iw-6, 16))))
		if r.OldBackup != "" {
			out = append(out, " "+m.t("sy.update.backup", i18n.A{"path": text.Path(r.OldBackup, max(iw-30, 12))}))
		}
		if r.Mismatch {
			for _, l := range text.WrapHang("! "+m.t("sy.update.mismatch", i18n.A{"current": text.Tilde(r.Current)}), max(iw-2, 10), " ", "   ") {
				out = append(out, th.S(theme.Warn, l))
			}
		}
		out = append(out, " "+th.S(theme.Muted, m.t("sy.update.pending")))
		for _, l := range text.Wrap(m.t("sy.update.skills_hint"), max(iw-2, 10)) {
			out = append(out, " "+th.S(theme.Muted, l))
		}
	}
	if left := room - len(out) - 2; left > 0 && len(m.upd.out.Log) > 0 {
		out = append(out, "", " "+th.S(theme.Accent, m.t("sy.update.log")))
		out = append(out, m.logTail(th, m.upd.out.Log, left, iw)...)
	}
	return out
}
