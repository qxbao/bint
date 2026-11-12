package runs

import (
	"fmt"
	"strings"
	"time"

	"bint/internal/app"
	"bint/internal/evolve"
	"bint/internal/i18n"
	"bint/internal/runlog"
	"bint/internal/tui/components/footer"
	"bint/internal/tui/components/frame"
	"bint/internal/tui/components/list"
	"bint/internal/tui/components/text"
	"bint/internal/tui/screens/shared"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

// labelGlyph là ký hiệu và vai trò màu của nhãn kết quả (có ký hiệu lẫn màu để không phụ thuộc màu).
func labelGlyph(label string) (string, theme.Role) {
	switch label {
	case runlog.LabelSuccess:
		return "✓", theme.OK
	case runlog.LabelFailure:
		return "✗", theme.Err
	}
	return "·", theme.Muted
}

// View vẽ màn hình trong vùng env.W × env.H (không gồm khung ngoài).
func (m *Model) View(env ui.Env) string {
	m.w, m.h = env.W, env.H
	th := env.Th
	switch {
	case m.Err != nil && !m.Loaded:
		return strings.Join([]string{"", th.S(theme.Err, "✗ ") + m.t("rn.error"), strings.Join(text.WrapHang(ui.ErrText(m.rw, m.Err), max(env.W-4, 10), "  ", "  "), "\n"),
			"", th.S(theme.Muted, m.t("rn.error.hint"))}, "\n")
	case !m.Loaded:
		return "\n  " + th.S(theme.Accent, env.Spin) + " " + th.S(theme.Muted, m.t("ui.loading"))
	}
	bar := m.tabBar(env)
	bodyEnv := env.WithSize(env.W, max(env.H-1, 1))
	var body string
	switch {
	case m.frm != nil:
		body = m.formView(bodyEnv)
	case m.tab == tabLessons:
		body = m.lessonsView(bodyEnv)
	default:
		body = m.runsView(bodyEnv)
	}
	lines := strings.Split(bar+"\n"+body, "\n")
	if m.dlg != nil {
		bw, bh := min(env.W-4, 84), min(env.H-2, 24)
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
	ids := make([]int, numTabs)
	names := make([]string, numTabs)
	for i := 0; i < numTabs; i++ {
		ids[i], names[i] = i, m.t("rn.tab."+tabNames[i])
	}
	sel := m.tab
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

func (m *Model) runsView(env ui.Env) string {
	return m.split(env,
		func(w int) string { return m.listBox(env, w, env.H) },
		func(w int) string { return m.detailBox(env, w, env.H) })
}

func (m *Model) filterChips() string {
	var parts []string
	if m.filter != "" {
		parts = append(parts, "“"+m.filter+"”")
	}
	if m.textIn {
		parts = append(parts, "/"+m.filter+"▏")
	}
	parts = append(parts, m.t("rn.limit", i18n.A{"limit": m.limit}))
	return strings.Join(parts, " · ")
}

func (m *Model) listBox(env ui.Env, w, h int) string {
	th := env.Th
	iw, ih := frame.Inner(w, h)
	title := m.t("rn.list.title", i18n.A{"shown": len(m.idx), "total": len(m.Data.Rows)})
	chips := th.S(theme.Key, text.Truncate(m.filterChips(), max(w/2, 12)))
	var body string
	switch {
	case len(m.Data.Rows) == 0:
		body = "\n " + th.S(theme.Muted, m.t("rn.empty"))
	case len(m.idx) == 0:
		body = "\n " + th.S(theme.Muted, m.t("rn.empty_filtered"))
	default:
		widths := list.Widths(iw-2, 1, []int{1, 6, 11, 7, 11, 0})
		body = m.lst.Render(iw, ih, func(i int, cursor bool) string {
			r := m.Data.Rows[m.idx[i]]
			glyph, role := labelGlyph(r.Label)
			id := shared.Short(r.Run.RunID)
			line := list.Row(widths, 1, th.S(role, glyph), th.S(theme.Muted, id[max(len(id)-6, 0):]), r.Run.Status,
				shared.Dash(r.Run.Harness.String), shared.TimeShort(r.Run.StartedAt.String), m.labelName(r.Label))
			if cursor {
				return th.S(theme.Key, "▸ ") + th.Selected(shared.StripColor(th, line))
			}
			return "  " + line
		})
	}
	return frame.Box{Title: title, TitleAt: frame.Left, Pane: "list", Focused: m.pane == paneList, Top: chips,
		BottomHints: m.listFooter(env), BottomAt: frame.Left}.Render(th, w, h, body)
}

func (m *Model) listFooter(env ui.Env) []footer.Hint {
	var hints []footer.Hint
	k := func(key, label, action string) {
		hints = append(hints, footer.Hint{Key: key, Label: label, Priority: len(hints) + 1, ActionID: action})
	}
	k("+/-", m.t("rn.hint.rate"), "runs.rate")
	k("S", m.t("rn.hint.scan"), "runs.scan")
	k("I", m.t("rn.hint.ingest"), "runs.ingest")
	k("d", m.t("rn.hint.digest"), "runs.lesson_digest")
	return env.AllowedHints(hints)
}

func (m *Model) labelName(label string) string {
	if k := "rn.label." + label; m.cat.Has(k) {
		return m.t(k)
	}
	return label
}

func (m *Model) detailBox(env ui.Env, w, h int) string {
	th := env.Th
	iw, ih := frame.Inner(w, h)
	r := m.Cursor()
	title := m.t("rn.detail.title")
	var content, top string
	switch {
	case r == nil:
		content = "\n " + th.S(theme.Muted, m.t("rn.detail.none"))
	default:
		id := r.Run.RunID
		title += " · " + shared.Short(id)
		d, ok := m.cache[id]
		switch {
		case m.cacheErr[id] != nil:
			content = "\n" + strings.Join(ui.ErrBlock(th, ui.ErrText(m.rw, m.cacheErr[id]), max(iw-1, 10), " "), "\n")
		case !ok:
			content = "\n  " + th.S(theme.Accent, env.Spin) + " " + th.S(theme.Muted, m.t("ui.loading"))
		default:
			m.detail.H = ih
			if m.detailKey != id {
				m.detailKey = id
				m.detail.Reset()
			}
			m.detail.SetWrapped(m.detailLines(env, d, max(iw-1, 10)), max(iw-1, 10))
			content = m.detail.Render(iw, ih)
			top = th.S(theme.Muted, m.detail.Position())
		}
	}
	return frame.Box{Title: title, TitleAt: frame.Left, Pane: "detail", Focused: m.pane == paneDetail, Top: top}.Render(th, w, h, content)
}

func fmtDuration(r runlog.Run) string {
	if !r.DurationMS.Valid {
		return "-"
	}
	return (time.Duration(r.DurationMS.Int64) * time.Millisecond).Round(time.Second).String()
}

// detailLines dựng nội dung chi tiết lượt: trạng thái, nhãn, tín hiệu, đầu ra và sự kiện.
func (m *Model) detailLines(env ui.Env, d *app.RunDetail, w int) []string {
	th := env.Th
	run := d.Run
	var out []string
	kv := func(key, val string) { out = append(out, shared.KV(th, 12, w, m.t("rn.f."+key), shared.Dash(val))...) }
	kv("run", run.RunID)
	status := run.Status
	if run.EndReason.Valid {
		status += " (" + run.EndReason.String + ")"
	}
	kv("status", status)
	kv("harness", shared.Dash(run.Harness.String)+"  "+m.t("rn.f.session")+" "+shared.Dash(run.SessionID.String))
	kv("model", run.Model.String)
	kv("git", shared.Dash(run.GitBranch.String)+" @ "+shared.Dash(run.GitBaseCommit.String))
	kv("worktree", run.Worktree.String)
	kv("time", shared.Time(run.StartedAt.String)+" → "+shared.Time(run.FinishedAt.String)+" ("+fmtDuration(run)+")")
	glyph, role := labelGlyph(d.Label)
	out = append(out, th.S(theme.Accent, text.Pad(m.t("rn.f.label"), 12))+th.S(role, glyph+" "+m.labelName(d.Label)))
	out = append(out, text.WrapHang(d.Reason, w, strings.Repeat(" ", 12), strings.Repeat(" ", 12))...)
	out = append(out, "", th.S(theme.Accent, m.t("rn.sec.signals", i18n.A{"count": len(d.Signals)})))
	if len(d.Signals) == 0 {
		out = append(out, "  "+th.S(theme.Muted, m.t("rn.none")))
	}
	for _, s := range d.Signals {
		pol, prole := "+", theme.OK
		if !s.Positive {
			pol, prole = "-", theme.Err
		}
		out = append(out, text.WrapHang(fmt.Sprintf("%s %s  %s  %s  %s", th.S(prole, pol), text.Pad(s.Kind, 10), text.Pad(s.Strength, 7), text.Pad(s.Source, 8), s.Detail),
			w, "  ", "      ")...)
	}
	out = append(out, "", th.S(theme.Accent, m.t("rn.sec.artifacts", i18n.A{"count": len(d.Artifacts)})))
	if len(d.Artifacts) == 0 {
		out = append(out, "  "+th.S(theme.Muted, m.t("rn.none")))
	}
	for _, a := range d.Artifacts {
		out = append(out, fmt.Sprintf("  %s %s (×%d)", text.Pad(a.Kind, 14), a.Path, a.Count))
	}
	out = append(out, "", th.S(theme.Accent, m.t("rn.sec.events", i18n.A{"count": len(d.Events)})))
	for _, e := range d.Events {
		out = append(out, fmt.Sprintf("  %s  %s %s", shared.Time(e.Timestamp), text.Pad(e.Type, 16), e.Payload))
	}
	return out
}

// ---- thẻ Bài học ----

func (m *Model) lessonsView(env ui.Env) string {
	th := env.Th
	switch {
	case m.lessonsErr != nil && !m.lessonsLoaded:
		return "\n" + strings.Join(ui.ErrBlock(th, ui.ErrText(m.rw, m.lessonsErr), max(env.W-4, 10), " "), "\n")
	case !m.lessonsLoaded:
		return "\n  " + th.S(theme.Accent, env.Spin) + " " + th.S(theme.Muted, m.t("ui.loading"))
	}
	return m.split(env,
		func(w int) string { return m.lessonListBox(env, w, env.H) },
		func(w int) string { return m.lessonDetailBox(env, w, env.H) })
}

// lessonRowKind: 0 = tiêu đề nhóm (chung/riêng vùng), 1 = bài học. Hàng của danh sách bài học gồm cả hai.
type lessonRow struct {
	head string // khác rỗng: tiêu đề nhóm
	area string
	row  *evolve.LessonRow
}

// lessonRows nhóm thư viện theo vùng (thư viện đã xếp: chung trước, riêng theo vùng).
func (m *Model) lessonRows() []lessonRow {
	var out []lessonRow
	area := "\x00"
	for i := range m.lessons {
		r := &m.lessons[i]
		if r.Area != area {
			area = r.Area
			head := m.t("rn.lesson.general")
			if area != "" {
				head = m.t("rn.lesson.area", i18n.A{"area": area})
			}
			out = append(out, lessonRow{head: head, area: area})
		}
		out = append(out, lessonRow{row: r})
	}
	return out
}

// LessonCount là số bài học trong thư viện.
func (m *Model) LessonCount() int { return len(m.lessons) }

// LessonGroups là các nhóm vùng theo thứ tự hiển thị ("" = chung).
func (m *Model) LessonGroups() []string {
	var out []string
	for _, r := range m.lessonRows() {
		if r.head != "" {
			out = append(out, r.area)
		}
	}
	return out
}

func (m *Model) lessonListBox(env ui.Env, w, h int) string {
	th := env.Th
	iw, ih := frame.Inner(w, h)
	rows := m.lessonRows()
	m.llst.SetN(len(rows))
	var body string
	if len(m.lessons) == 0 {
		body = "\n " + th.S(theme.Muted, m.t("rn.lesson.empty"))
	} else {
		body = m.llst.Render(iw, ih, func(i int, cursor bool) string {
			r := rows[i]
			if r.row == nil {
				return " " + th.S(theme.Accent, text.Truncate(r.head, max(iw-2, 4)))
			}
			l := r.row
			line := fmt.Sprintf("[%s] %s  %s", l.Status, l.Title, th.S(theme.Muted, m.t("rn.lesson.counts", i18n.A{"inserted": l.Inserted, "pass": l.EvalPass, "fail": l.EvalFail})))
			if cursor {
				return th.S(theme.Key, "▸ ") + th.Selected(shared.StripColor(th, line))
			}
			return "  " + line
		})
	}
	return frame.Box{Title: m.t("rn.lesson.title", i18n.A{"count": len(m.lessons)}), TitleAt: frame.Left, Pane: "list", Focused: m.pane == paneList,
		BottomHints: env.AllowedHints([]footer.Hint{{Key: "d", Label: m.t("rn.hint.digest"), Priority: 1, ActionID: "runs.lesson_digest"}}), BottomAt: frame.Left}.Render(th, w, h, body)
}

// lessonAt trả bài học dưới con trỏ ở thẻ Bài học (nil nếu đang ở tiêu đề nhóm hoặc trống).
func (m *Model) lessonAt() *evolve.LessonRow {
	rows := m.lessonRows()
	if m.llst.Cursor < 0 || m.llst.Cursor >= len(rows) {
		return nil
	}
	return rows[m.llst.Cursor].row
}

func (m *Model) lessonDetailBox(env ui.Env, w, h int) string {
	th := env.Th
	iw, ih := frame.Inner(w, h)
	var lines []string
	if l := m.lessonAt(); l != nil {
		kv := func(key, val string) { lines = append(lines, shared.KV(th, 20, iw-1, m.t("rn.lf."+key), val)...) }
		lines = append(lines, th.Bold(l.Title), "")
		kv("id", l.ID)
		kv("status", l.Status)
		area := m.t("rn.lesson.general")
		if l.Area != "" {
			area = l.Area
		}
		kv("area", area)
		kv("provenance", shared.Dash(l.Provenance))
		kv("inserted", fmt.Sprint(l.Inserted))
		kv("real", m.t("rn.lesson.real", i18n.A{"ok": l.RealSuccess, "bad": l.RealFailure}))
		kv("eval", m.t("rn.lesson.eval", i18n.A{"pass": l.EvalPass, "fail": l.EvalFail}))
		kv("runs", fmt.Sprint(l.Runs))
		if l.Status != "verified" {
			lines = append(lines, "", th.S(theme.Warn, m.t("rn.lesson.candidate_note")))
		}
	} else {
		lines = append(lines, "", " "+th.S(theme.Muted, m.t("rn.lesson.select")))
	}
	lines = append(lines, "", th.S(theme.Accent, m.t("rn.digest.title")))
	lines = append(lines, m.digestLines(env, iw-1)...)
	m.detail.H = ih
	m.detail.SetWrapped(lines, max(iw-1, 10))
	return frame.Box{Title: m.t("rn.digest.box"), TitleAt: frame.Left, Pane: "detail", Focused: m.pane == paneDetail, Top: th.S(theme.Muted, m.detail.Position())}.
		Render(th, w, h, m.detail.Render(iw, ih))
}

// digestLines dựng bản tóm tắt thất bại gần nhất (hoặc gợi ý/lỗi).
func (m *Model) digestLines(env ui.Env, w int) []string {
	th := env.Th
	switch {
	case m.digestBusy:
		return []string{"  " + th.S(theme.Accent, env.Spin) + " " + th.S(theme.Muted, m.t("ui.loading"))}
	case m.digestErr != nil:
		return text.WrapHang(th.S(theme.Err, "✗ ")+ui.ErrText(m.rw, m.digestErr), w, "  ", "    ")
	case m.digest == nil:
		return []string{"  " + th.S(theme.Muted, m.t("rn.digest.hint"))}
	}
	rep := m.digest.Report
	var out []string
	if rep.Kind == "batch" {
		out = append(out, "  "+m.t("rn.digest.batch", i18n.A{"ref": rep.Ref, "count": len(rep.Digests)}))
		if rep.Skipped > 0 {
			out = append(out, text.WrapHang(th.S(theme.Muted, m.t("rn.digest.skipped", i18n.A{"count": rep.Skipped})), w, "  ", "  ")...)
		}
	} else {
		out = append(out, "  "+m.t("rn.digest.run", i18n.A{"ref": rep.Ref}))
	}
	if len(rep.Digests) == 0 {
		return append(out, "  "+th.S(theme.Muted, m.t("rn.digest.none")))
	}
	for _, d := range rep.Digests {
		out = append(out, "")
		for _, l := range strings.Split(strings.TrimRight(d.Text, "\n"), "\n") {
			out = append(out, "  "+l)
		}
		out = append(out, "  "+th.S(theme.Muted, m.t("rn.digest.tokens", i18n.A{"tokens": d.Tokens})))
	}
	if len(rep.Groups) > 0 {
		out = append(out, "", "  "+th.S(theme.Accent, m.t("rn.digest.groups", i18n.A{"min": m.digest.MinUnits})))
		for _, g := range rep.Groups {
			state := th.S(theme.OK, m.t("rn.digest.ready"))
			if !g.Ready {
				state = th.S(theme.Warn, m.t("rn.digest.blocked", i18n.A{"why": g.Blocked}))
			}
			out = append(out, text.WrapHang(fmt.Sprintf("- %s — %s [%s]", g.Group, g.Summary, state), w, "  ", "    ")...)
		}
	}
	return out
}

// LessonCursor trả bài học dưới con trỏ ở thẻ Bài học (nil nếu con trỏ không ở hàng bài học, ví dụ tiêu đề nhóm).
func (m *Model) LessonCursor() *evolve.LessonRow {
	rows := m.lessonRows()
	if m.llst.Cursor < 0 || m.llst.Cursor >= len(rows) {
		return nil
	}
	return rows[m.llst.Cursor].row
}
