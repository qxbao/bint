package evolve

import (
	"fmt"
	"sort"
	"strings"

	"bint/internal/eval"
	"bint/internal/evolve"
	"bint/internal/i18n"
	"bint/internal/tui/components/footer"
	"bint/internal/tui/components/frame"
	"bint/internal/tui/components/list"
	"bint/internal/tui/components/logview"
	"bint/internal/tui/components/text"
	"bint/internal/tui/screens/shared"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

// View vẽ màn hình trong vùng env.W × env.H (không gồm khung ngoài).
func (m *Model) View(env ui.Env) string {
	m.w, m.h = env.W, env.H
	th := env.Th
	switch {
	case m.Err != nil && !m.Loaded:
		return strings.Join([]string{"", th.S(theme.Err, "✗ ") + m.t("ev.error"), strings.Join(text.WrapHang(ui.ErrText(m.rw, m.Err), max(env.W-4, 10), "  ", "  "), "\n"),
			"", th.S(theme.Muted, m.t("ev.error.hint"))}, "\n")
	case !m.Loaded:
		return "\n  " + th.S(theme.Accent, env.Spin) + " " + th.S(theme.Muted, m.t("ui.loading"))
	}
	bar := m.tabBar(env)
	bodyEnv := env.WithSize(env.W, max(env.H-1, 1))
	var body string
	switch {
	case m.frm != nil:
		body = m.formView(bodyEnv)
	case m.tab == tabOpps:
		body = m.split(bodyEnv, func(w int) string { return m.oppListBox(bodyEnv, w, bodyEnv.H) }, func(w int) string { return m.oppDetailBox(bodyEnv, w, bodyEnv.H) })
	case m.tab == tabProposals:
		body = m.split(bodyEnv, func(w int) string { return m.propListBox(bodyEnv, w, bodyEnv.H) }, func(w int) string { return m.propDetailBox(bodyEnv, w, bodyEnv.H) })
	case m.tab == tabHistory:
		body = m.historyBox(bodyEnv, bodyEnv.W, bodyEnv.H)
	case m.tab == tabQueue:
		body = m.queueBox(bodyEnv, bodyEnv.W, bodyEnv.H)
	default:
		body = m.logBox(bodyEnv, bodyEnv.W, bodyEnv.H)
	}
	lines := strings.Split(bar+"\n"+body, "\n")
	if m.dlg != nil {
		bw, bh := min(env.W-4, 90), min(env.H-2, 26)
		box := m.dlg.c.View(th, bw, bh)
		lines = text.Overlay(lines, box, (env.W-bw)/2, max((env.H-len(box))/2, 1))
	}
	for len(lines) < env.H {
		lines = append(lines, "")
	}
	return strings.Join(lines[:env.H], "\n")
}

func (m *Model) tabBar(env ui.Env) string {
	th := env.Th
	ids := m.visibleTabs()
	names := make([]string, len(ids))
	sel := 0
	for k, i := range ids {
		names[k] = m.t("ev.tab." + tabNames[i])
		if i == tabLog && m.job() != nil {
			names[k] = env.Spin + " " + names[k]
		}
		if i == m.tab {
			sel = k
		}
	}
	bar := ui.SubBar(th, names, ids, sel, env.W-1)
	if m.toast != "" {
		if need := min(text.Width(m.toast)+4, env.W/2); env.W-text.Width(bar)-1 < need {
			bar = ui.SubBar(th, names, ids, sel, env.W-1-need) // chừa chỗ cho thông báo thay vì bắt thông báo bị cắt
		}
		toast := ui.ToastLabel(th, m.toast, m.toastErr, max(env.W-text.Width(bar)-1, 0))
		bar += strings.Repeat(" ", max(env.W-text.Width(bar)-text.Width(toast), 1)) + toast
	}
	return text.Fit(bar, env.W)
}

func (m *Model) formView(env ui.Env) string {
	iw, ih := frame.Inner(env.W, env.H)
	title := m.frmID
	if k := "action." + m.frmID + ".title"; m.cat.Has(k) {
		title = m.t(k)
	}
	return frame.Box{Title: title, TitleAt: frame.Left, Focused: true}.Render(env.Th, env.W, env.H, m.frm.View(env.WithSize(iw, ih), 0))
}

// split chia vùng thành khung trái và phải (≥ WideW cột), hoặc một khung theo pane.
func (m *Model) split(env ui.Env, left, right func(w int) string) string {
	w := env.W
	if w >= WideW {
		lw := max(w/2, 52)
		l, r := strings.Split(left(lw), "\n"), strings.Split(right(w-lw), "\n")
		out := make([]string, len(l))
		for i := range l {
			out[i] = l[i] + r[i]
		}
		return strings.Join(out, "\n")
	}
	if m.pane == paneDetail {
		return right(w)
	}
	return left(w)
}

// ---- thẻ Cơ hội ----

func (m *Model) oppListBox(env ui.Env, w, h int) string {
	th := env.Th
	iw, ih := frame.Inner(w, h)
	ops := m.Data.Opportunities()
	var body string
	switch {
	case m.Data.ScanErr != nil && m.Data.Scan == nil:
		body = "\n" + strings.Join(ui.ErrBlock(th, ui.ErrText(m.rw, m.Data.ScanErr), max(iw-1, 10), " "), "\n")
	case len(ops) == 0:
		body = "\n " + th.S(theme.Muted, m.t("ev.opps.empty"))
	default:
		widths := list.Widths(iw-2, 1, []int{1, 8, 16, 9, 0})
		body = m.olst.Render(iw, ih, func(i int, cursor bool) string {
			o := ops[i]
			glyph, role := "◌", theme.Muted
			if o.Ready {
				glyph, role = "●", theme.OK
			}
			line := list.Row(widths, 1, th.S(role, glyph), th.S(theme.Muted, o.ID), o.Kind, o.Action, o.Summary)
			if cursor {
				return th.S(theme.Key, "▸ ") + th.Selected(shared.StripColor(th, line))
			}
			return "  " + line
		})
	}
	return frame.Box{Title: m.t("ev.opps.title", i18n.A{"count": len(ops)}), TitleAt: frame.Left, Pane: "list", Focused: m.pane == paneList,
		BottomHints: m.hints(env, [][3]string{{"s", "ev.hint.scan", "evolve.scan"}, {"p", "ev.hint.propose", "evolve.propose"}}), BottomAt: frame.Left}.Render(th, w, h, body)
}

// hints dựng chân phím của khung (kiểu Plain, tự rút theo độ rộng khung và bấm được bằng chuột).
// hints dựng chân phím trong khung từ các bộ {phím, khoá nhãn, ID hành động ("" = phím giao diện thuần)} và bỏ phím mà sổ không cho dùng
// (chỉ đọc, phiên agent) qua env.Allowed.
func (m *Model) hints(env ui.Env, hs [][3]string) []footer.Hint {
	out := make([]footer.Hint, len(hs))
	for i, h := range hs {
		out[i] = footer.Hint{Key: h[0], Label: m.t(h[1]), Priority: i + 1, ActionID: h[2]}
	}
	return env.AllowedHints(out)
}

func (m *Model) oppDetailBox(env ui.Env, w, h int) string {
	th := env.Th
	iw, ih := frame.Inner(w, h)
	lw := max(iw-1, 10)
	var lines []string
	if res := m.Data.Scan; res != nil {
		lines = append(lines, th.S(theme.Accent, m.t("ev.scan.window")), "  "+m.t("ev.scan.runs", i18n.A{"since": shared.Short(res.Since[:min(10, len(res.Since))]), "runs": res.Runs, "labeled": res.Labeled, "read": res.ReadData}))
		lines = append(lines, "", th.S(theme.Accent, m.t("ev.scan.prereq")))
		for _, c := range res.Prerequisites.Checks {
			icon, role := "✓", theme.OK
			if !c.OK {
				icon, role = "✗", theme.Err
			}
			lines = append(lines, text.WrapHang(fmt.Sprintf("  %s %s %s", th.S(role, icon), text.Pad(c.Name, 19), m.cli(c.Detail)), lw, "", "      ")...)
		}
		if res.Prerequisites.Quantitative {
			lines = append(lines, text.WrapHang("→ "+m.t("ev.scan.quant"), lw, "  ", "    ")...)
		} else {
			lines = append(lines, text.WrapHang(th.S(theme.Warn, "→ "+m.t("ev.scan.not_quant")), lw, "  ", "    ")...)
		}
		if len(res.Groups) > 0 {
			lines = append(lines, "", th.S(theme.Accent, m.t("ev.scan.groups")))
			for _, g := range res.Groups {
				var wf []string
				keys := make([]string, 0, len(g.Workflows))
				for k := range g.Workflows {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					wf = append(wf, fmt.Sprintf("%s=%d", k, g.Workflows[k]))
				}
				lines = append(lines, text.WrapHang(fmt.Sprintf("%s  %s  %s", g.Module, m.t("ev.scan.group", i18n.A{"runs": g.Runs, "labeled": g.Labeled, "ok": g.Success, "bad": g.Failure}), strings.Join(wf, " ")), lw, "  ", "    ")...)
			}
		}
	}
	if o := m.CursorOpportunity(); o != nil {
		lines = append(lines, "", th.S(theme.Accent, m.t("ev.opp.detail", i18n.A{"id": o.ID})))
		state := th.S(theme.OK, m.t("ev.opp.ready"))
		if !o.Ready {
			state = th.S(theme.Warn, m.t("ev.opp.blocked", i18n.A{"why": o.Blocked}))
		}
		lvl := "L1"
		if o.Level != "" {
			lvl = o.Level
		}
		lines = append(lines, text.WrapHang(fmt.Sprintf("  %s %s → %s [%s] %s", lvl, o.Kind, o.Action, o.Generator, state), lw, "", "    ")...)
		lines = append(lines, text.WrapHang("  "+o.Summary, lw, "", "    ")...)
		for _, f := range o.Files {
			lines = append(lines, text.WrapHang(m.t("ev.opp.file", i18n.A{"path": f.Path, "runs": f.Runs, "failures": f.Failures}), lw, "    ", "    ")...)
			if len(f.Members) > 0 {
				lines = append(lines, text.WrapHang("      "+m.t("ev.opp.members", i18n.A{"members": strings.Join(f.Members, ", ")}), lw, "", "        ")...)
			}
		}
	}
	m.detail.H = ih
	m.detail.SetWrapped(lines, max(iw-1, 10))
	return frame.Box{Title: m.t("ev.opps.detail"), TitleAt: frame.Left, Pane: "detail", Focused: m.pane == paneDetail, Top: th.S(theme.Muted, m.detail.Position())}.
		Render(th, w, h, m.detail.Render(iw, ih))
}

// ---- thẻ Đề xuất ----

func latestVerdict(evs []evolve.Evaluation, split string) string {
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Split == split && evs[i].Verdict != "" {
			v := evs[i].Verdict
			if evs[i].Independent != nil && !*evs[i].Independent {
				v += "*"
			}
			return v
		}
	}
	return "-"
}

func stateGlyph(state string) (string, theme.Role) {
	switch state {
	case evolve.StatusProposed:
		return "◌", theme.Fg
	case evolve.StatusApplied:
		return "✓", theme.OK
	case evolve.StatusRejected:
		return "✗", theme.Err
	case evolve.StatusRolledBack:
		return "↺", theme.Warn
	case "outdated":
		return "!", theme.Warn
	}
	return "·", theme.Muted
}

func (m *Model) propListBox(env ui.Env, w, h int) string {
	th := env.Th
	iw, ih := frame.Inner(w, h)
	chip := m.t("ev.filter.pending")
	if m.all {
		chip = m.t("ev.filter.all")
	}
	var body string
	if len(m.Data.Rows) == 0 {
		body = "\n " + th.S(theme.Muted, m.t("ev.props.empty"))
	} else {
		widths := list.Widths(iw-2, 1, []int{1, 6, 8, 9, 6, 9, 9, 0})
		body = m.plst.Render(iw, ih, func(i int, cursor bool) string {
			r := m.Data.Rows[i]
			p := r.Proposal
			glyph, role := stateGlyph(r.State)
			risk := p.Risk
			if risk == evolve.RiskHigh {
				risk = th.S(theme.Err, risk)
			}
			line := list.Row(widths, 1, th.S(role, glyph), th.S(theme.Muted, p.ID[max(len(p.ID)-6, 0):]), r.State, p.Action, risk,
				latestVerdict(r.Evaluations, eval.SplitValidation), latestVerdict(r.Evaluations, eval.SplitTest), p.Reason)
			if cursor {
				return th.S(theme.Key, "▸ ") + th.Selected(shared.StripColor(th, line))
			}
			return "  " + line
		})
	}
	return frame.Box{Title: m.t("ev.props.title", i18n.A{"count": len(m.Data.Rows)}), TitleAt: frame.Left, Pane: "list", Focused: m.pane == paneList, Top: th.S(theme.Key, chip),
		BottomHints: m.hints(env, [][3]string{{"a", "ev.hint.apply", "evolve.apply"}, {"x", "ev.hint.reject", "evolve.reject"}, {"u", "ev.hint.rollback", "evolve.rollback"}, {"E", "ev.hint.evaluate", "evolve.evaluate"}, {"A", "ev.hint.all", "evolve.list"}}), BottomAt: frame.Left}.
		Render(th, w, h, body)
}

func (m *Model) propDetailBox(env ui.Env, w, h int) string {
	th := env.Th
	iw, ih := frame.Inner(w, h)
	r := m.Cursor()
	title := m.t("ev.detail.title")
	var content, top string
	switch {
	case r == nil:
		content = "\n " + th.S(theme.Muted, m.t("ev.detail.none"))
	default:
		id := r.Proposal.ID
		title += " · " + shared.Short(id)
		d, ok := m.cache[id]
		switch {
		case m.cacheErr[id] != nil:
			content = "\n" + strings.Join(ui.ErrBlock(th, ui.ErrText(m.rw, m.cacheErr[id]), max(iw-1, 10), " "), "\n")
		case !ok:
			content = "\n  " + th.S(theme.Accent, env.Spin) + " " + th.S(theme.Muted, m.t("ui.loading"))
		default:
			key := id + "/" + subNames[m.sub]
			m.detail.H = ih - 1
			if m.detailKey != key {
				m.detailKey = key
				m.detail.Reset()
			}
			var lines []string
			switch m.sub {
			case subChanges:
				lines = m.changesLines(env, d, max(iw-1, 10))
			case subEvals:
				lines = m.evalLines(env, d, max(iw-1, 10))
			default:
				lines = m.infoLines(env, d, max(iw-1, 10))
			}
			m.detail.SetWrapped(lines, max(iw-1, 10))
			content = m.subBar(env, iw) + "\n" + m.detail.Render(iw, max(ih-1, 1))
			top = th.S(theme.Muted, m.detail.Position())
		}
	}
	return frame.Box{Title: title, TitleAt: frame.Left, Pane: "detail", Focused: m.pane == paneDetail, Top: top,
		BottomHints: m.hints(env, [][3]string{{"v", "ev.hint.sub", ""}, {"Enter", "ev.hint.focus", ""}}), BottomAt: frame.Left}.Render(th, w, h, content)
}

// subBar là thanh thẻ con của chi tiết đề xuất.
func (m *Model) subBar(env ui.Env, w int) string {
	th := env.Th
	var parts []string
	for i := 0; i < numSubs; i++ {
		n := " " + m.t("ev.sub."+subNames[i]) + " "
		if i == m.sub {
			parts = append(parts, th.Selected(n))
		} else {
			parts = append(parts, th.S(theme.Muted, n))
		}
	}
	return text.Fit(strings.Join(parts, th.S(theme.Muted, "·")), w)
}

// ---- Lịch sử, Hàng đợi, Log ----

func (m *Model) historyBox(env ui.Env, w, h int) string {
	th := env.Th
	iw, ih := frame.Inner(w, h)
	lw := max(iw-1, 10)
	var lines []string
	hs := m.Data.History
	if hs != nil && hs.Cycles > 0 {
		sum := hs.CycleTotals
		lines = append(lines, text.WrapHang(m.t("ev.hist.cycles", i18n.A{"n": hs.Cycles, "spent": fmt.Sprintf("%.2f", sum.SpentUSD), "gen": fmt.Sprintf("%.2f", sum.GenerationUSD),
			"probe": fmt.Sprintf("%.2f", sum.ProbeUSD), "applied": len(sum.AutoApplied), "rolled": len(sum.RolledBack), "rejected": len(sum.AutoRejected)}), lw, "", "  ")...)
		lines = append(lines, "")
	}
	if hs == nil || len(hs.Entries) == 0 {
		lines = append(lines, th.S(theme.Muted, m.t("ev.hist.none")))
	} else {
		for _, e := range hs.Entries {
			kind := ""
			if e.EvidenceKind != "" {
				kind = " " + evolve.KindLabel(e.EvidenceKind)
			}
			lines = append(lines, text.WrapHang(fmt.Sprintf("%s  %-8s %s %s (%s)  %s  %s%s", shared.Time(e.CreatedAt), e.Action, shared.Short(e.ProposalID), e.ProposalAction,
				strings.Join(e.Items, ","), e.Actor, m.t("ev.adoption.verified", i18n.A{"v": fmt.Sprint(e.Verified)}), kind), lw, "", "    ")...)
			if e.Reason != "" {
				lines = append(lines, text.WrapHang(m.cli(e.Reason), lw, "    ", "    ")...)
			}
			if e.ModelStatus != "" {
				lines = append(lines, text.WrapHang(m.t("ev.hist.model", i18n.A{"status": m.cli(e.ModelStatus)}), lw, "    ", "    ")...)
			}
			if e.Action == "apply" {
				lines = append(lines, text.WrapHang(m.t("ev.hist.after", i18n.A{"retrieved": e.RetrievedSince, "cited": e.CitedSince}), lw, "    ", "    ")...)
			}
			lines = append(lines, text.WrapHang(m.t("ev.hist.cost", i18n.A{"gen": optUSD(e.GenerationUSD, "%.4f"), "probe": optUSD(e.ProbeUSD, "%.2f")}), lw, "    ", "    ")...)
		}
	}
	m.hist.H = ih
	m.hist.SetWrapped(lines, max(iw-1, 10))
	return frame.Box{Title: m.t("ev.hist.title"), TitleAt: frame.Left, Pane: "view", Focused: true, Top: th.S(theme.Muted, m.hist.Position())}.Render(th, w, h, m.hist.Render(iw, ih))
}

func optUSD(v *float64, f string) string {
	if v == nil {
		return "-"
	}
	return "$" + fmt.Sprintf(f, *v)
}

func (m *Model) queueBox(env ui.Env, w, h int) string {
	th := env.Th
	iw, ih := frame.Inner(w, h)
	lw := max(iw-1, 10)
	var lines []string
	q := m.Data.Queue
	chip := m.t("ev.filter.pending")
	if m.queueAll {
		chip = m.t("ev.filter.all")
	}
	if q != nil {
		lines = append(lines, th.S(theme.Accent, m.t("ev.queue.execution", i18n.A{"mode": q.Execution})), "")
	}
	if q == nil || len(q.Requests) == 0 {
		lines = append(lines, th.S(theme.Muted, m.t("ev.queue.none")))
	} else {
		for _, r := range q.Requests {
			lines = append(lines, text.WrapHang(fmt.Sprintf("%s  %s  %s  %s  %s", r.ID, shared.Time(r.CreatedAt), text.Pad(r.Status, 8), text.Pad(r.Kind, 15), r.Summary), lw, "", "    ")...)
			if r.Note != "" || r.ProposalID != "" {
				lines = append(lines, text.WrapHang(strings.TrimSpace(m.cli(r.Note)+" "+r.ProposalID), lw, "    ", "    ")...)
			}
		}
	}
	m.queue.H = ih
	m.queue.SetWrapped(lines, max(iw-1, 10))
	return frame.Box{Title: m.t("ev.queue.title"), TitleAt: frame.Left, Pane: "view", Focused: true, Top: th.S(theme.Key, chip) + " " + th.S(theme.Muted, m.queue.Position()),
		BottomHints: m.hints(env, [][3]string{{"P", "ev.hint.process", "evolve.process"}, {"A", "ev.hint.all", "evolve.list"}}), BottomAt: frame.Left}.Render(th, w, h, m.queue.Render(iw, ih))
}

func (m *Model) logBox(env ui.Env, w, h int) string {
	th := env.Th
	iw, ih := frame.Inner(w, h)
	j := m.lastJob()
	if j == nil {
		return frame.Box{Title: m.t("ev.log.title"), TitleAt: frame.Left, Focused: true}.Render(th, w, h, "\n "+th.S(theme.Muted, m.t("ev.log.none")))
	}
	state := m.t("ev.log.running", i18n.A{"label": j.Label})
	top := th.S(theme.Key, "Esc") + " " + th.S(theme.Muted, m.t("ev.log.cancel"))
	if !j.Ended.IsZero() {
		state = m.t("ev.log.finished", i18n.A{"label": j.Label})
		if j.Err != nil {
			state += " — " + ui.ErrText(m.rw, j.Err)
		}
		top = ""
	}
	secs := []logview.Section{{Title: state, Lines: j.Log.Lines}}
	m.logTotal, m.logH = logview.Total(secs), ih
	return frame.Box{Title: m.t("ev.log.title"), TitleAt: frame.Left, Pane: "view", Focused: true, Top: top}.Render(th, w, h, m.log.Render(th, secs, iw, ih))
}
