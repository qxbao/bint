package eval

import (
	"fmt"
	"sort"
	"strings"

	"bint/internal/eval"
	"bint/internal/i18n"
	"bint/internal/tui/actions"
	"bint/internal/tui/components/footer"
	"bint/internal/tui/components/frame"
	"bint/internal/tui/components/input"
	"bint/internal/tui/components/list"
	"bint/internal/tui/components/logview"
	"bint/internal/tui/components/markdown"
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
		return strings.Join([]string{"", th.S(theme.Err, "✗ ") + m.t("eval.error"), strings.Join(text.WrapHang(ui.ErrText(m.rw, m.Err), max(env.W-4, 10), "  ", "  "), "\n"),
			"", th.S(theme.Muted, m.t("eval.error.hint"))}, "\n")
	case !m.Loaded:
		return "\n  " + th.S(theme.Accent, env.Spin) + " " + th.S(theme.Muted, m.t("ui.loading"))
	}
	bar := m.tabBar(env)
	bodyEnv := env.WithSize(env.W, max(env.H-1, 1))
	var body string
	switch {
	case m.frm != nil:
		body = m.formView(bodyEnv)
	case m.tab == tabCands:
		body = m.split(bodyEnv, func(w int) string { return m.candListBox(bodyEnv, w, bodyEnv.H) }, func(w int) string { return m.caseDetailBox(bodyEnv, w, bodyEnv.H) })
	case m.tab == tabCases:
		body = m.split(bodyEnv, func(w int) string { return m.caseListBox(bodyEnv, w, bodyEnv.H) }, func(w int) string { return m.caseDetailBox(bodyEnv, w, bodyEnv.H) })
	case m.tab == tabBatches:
		body = m.split(bodyEnv, func(w int) string { return m.batchListBox(bodyEnv, w, bodyEnv.H) }, func(w int) string { return m.batchDetailBox(bodyEnv, w, bodyEnv.H) })
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
		names[k] = m.t("eval.tab." + tabNames[i])
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

// filterChip là nhãn trạng thái lọc ở viền trên của khung danh sách: ô lọc đang nhập, hoặc bộ lọc đang áp dụng.
func (m *Model) filterChip(th theme.Theme, extra string) string {
	var parts []string
	if extra != "" {
		parts = append(parts, th.S(theme.Key, extra))
	}
	switch {
	case m.textIn:
		parts = append(parts, th.S(theme.Key, "/")+m.filter+input.Cursor)
	case m.filter != "":
		parts = append(parts, th.S(theme.Key, "/"+m.filter))
	}
	return strings.Join(parts, " ")
}

// ---- thẻ Ứng viên ----

func verifyGlyph(v *eval.Verification) (string, theme.Role) {
	switch {
	case v == nil:
		return "·", theme.Muted
	case v.Status == "verified":
		return "✓", theme.OK
	case v.Status == "failed":
		return "✗", theme.Err
	}
	return "!", theme.Warn
}

func leakGlyph(l eval.LeakReport) (string, theme.Role) {
	switch l.Status {
	case "fail":
		return "✗", theme.Err
	case "warn":
		return "!", theme.Warn
	case "pass":
		return "✓", theme.OK
	}
	return "·", theme.Muted
}

func specGlyph(s *eval.SpecReport) (string, theme.Role) {
	switch {
	case s == nil:
		return "·", theme.Muted
	case s.Status == "pass":
		return "✓", theme.OK
	}
	return "!", theme.Warn
}

func subjectOf(c *eval.Case) string {
	if c.Source.Subject != "" {
		return c.Source.Subject
	}
	return strings.Join(strings.Fields(c.Prompt), " ")
}

func (m *Model) candListBox(env ui.Env, w, h int) string {
	th := env.Th
	iw, ih := frame.Inner(w, h)
	var body string
	if len(m.cidx) == 0 {
		body = "\n " + th.S(theme.Muted, m.t("eval.cands.empty"))
	} else {
		widths := list.Widths(iw-2, 1, []int{1, 13, 1, 1, 1, 0})
		body = m.clst.Render(iw, ih, func(i int, cursor bool) string {
			c := m.Data.Pending[m.cidx[i]]
			glyph, role := "◌", theme.Fg
			if c.Status == eval.StatusRejected {
				glyph, role = "✗", theme.Muted
			}
			vg, vr := verifyGlyph(c.Verification)
			lg, lr := leakGlyph(c.Leak)
			sg, sr := specGlyph(c.Spec)
			line := list.Row(widths, 1, th.S(role, glyph), th.S(theme.Muted, c.ID), th.S(vr, vg), th.S(lr, lg), th.S(sr, sg), subjectOf(c))
			if cursor {
				return th.S(theme.Key, "▸ ") + th.Selected(shared.StripColor(th, line))
			}
			return "  " + line
		})
	}
	chip := m.t("eval.filter.pending")
	if m.all {
		chip = m.t("eval.filter.all")
	}
	return frame.Box{Title: m.t("eval.cands.title", i18n.A{"count": len(m.cidx)}), TitleAt: frame.Left, Pane: "list", Focused: m.pane == paneList, Top: m.filterChip(th, chip),
		BottomHints: m.hints(env, [][3]string{{"a", "eval.hint.approve", "eval.approve"}, {"x", "eval.hint.reject", "eval.reject"}, {"m", "eval.hint.mine", "eval.mine"}, {"v", "eval.hint.verify", "eval.verify"}, {"A", "eval.hint.all", "eval.list"}}), BottomAt: frame.Left}.
		Render(th, w, h, body)
}

func (m *Model) caseListBox(env ui.Env, w, h int) string {
	th := env.Th
	iw, ih := frame.Inner(w, h)
	var body string
	if len(m.kidx) == 0 {
		body = "\n " + th.S(theme.Muted, m.t("eval.cases.empty"))
	} else {
		widths := list.Widths(iw-2, 1, []int{1, 13, 10, 0})
		body = m.kst.Render(iw, ih, func(i int, cursor bool) string {
			c := m.Data.Cases[m.kidx[i]]
			vg, vr := verifyGlyph(c.Verification)
			line := list.Row(widths, 1, th.S(vr, vg), th.S(theme.Muted, c.ID), shared.Dash(c.Split), subjectOf(c))
			if cursor {
				return th.S(theme.Key, "▸ ") + th.Selected(shared.StripColor(th, line))
			}
			return "  " + line
		})
	}
	split := m.t("eval.split.all")
	if s := splits[m.splitAt]; s != "" {
		split = m.t("eval.split.one", i18n.A{"split": s})
	}
	return frame.Box{Title: m.t("eval.cases.title", i18n.A{"count": len(m.kidx)}), TitleAt: frame.Left, Pane: "list", Focused: m.pane == paneList, Top: m.filterChip(th, split),
		BottomHints: m.hints(env, [][3]string{{"r", "eval.hint.run", "eval.run"}, {"E", "eval.hint.estimate", "eval.estimate"}, {"t", "eval.hint.split", "eval.split"}, {"p", "eval.hint.spec", "eval.spec"}}), BottomAt: frame.Left}.
		Render(th, w, h, body)
}

// caseOf trả bản tác vụ đầy đủ để vẽ chi tiết: kết quả EvalShow (đã tính lại rò rỉ) nếu đã nạp, không thì bản trong danh sách.
func (m *Model) caseOf(c *eval.Case) (*eval.Case, string, string, error) {
	if d, ok := m.cache[c.ID]; ok && d != nil && d.Case != nil {
		return d.Case, d.TestCommand, d.Path, nil
	}
	return c, "", "", nil
}

func (m *Model) caseDetailBox(env ui.Env, w, h int) string {
	th := env.Th
	iw, ih := frame.Inner(w, h)
	c := m.current()
	title := m.t("eval.detail.title")
	var content, top string
	switch {
	case c == nil:
		content = "\n " + th.S(theme.Muted, m.t("eval.detail.none"))
	default:
		title += " · " + shared.Short(c.ID)
		full, cmd, path, _ := m.caseOf(c)
		key := c.ID
		m.detail.H = ih
		if m.detailKey != key {
			m.detailKey = key
			m.detail.Reset()
		}
		var cmdErr error
		if d := m.cache[c.ID]; d != nil {
			cmdErr = d.TestCommandErr
		}
		m.detail.SetWrapped(m.caseLines(env, full, cmd, cmdErr, path, max(iw-1, 10)), max(iw-1, 10))
		content = m.detail.Render(iw, ih)
		top = th.S(theme.Muted, m.detail.Position())
	}
	return frame.Box{Title: title, TitleAt: frame.Left, Pane: "detail", Focused: m.pane == paneDetail, Top: top,
		BottomHints: m.hints(env, [][3]string{{"e", "eval.hint.edit", "eval.open_file"}, {"Enter", "eval.hint.focus", ""}}), BottomAt: frame.Left}.Render(th, w, h, content)
}

// caseLines dựng chi tiết ứng viên/tác vụ: đề bài, nguồn commit, kiểm chứng, rò rỉ, đặc tả, lý do loại, lệnh chấm.
func (m *Model) caseLines(env ui.Env, c *eval.Case, cmd string, cmdErr error, path string, lw int) []string {
	th := env.Th
	var lines []string
	add := func(key string) { lines = append(lines, "", th.S(theme.Accent, m.t(key))) }
	kv := func(key, val string) { lines = append(lines, shared.KV(th, 12, lw, m.t(key), val)...) }

	lines = append(lines, th.Bold(c.ID))
	kv("eval.f.status", m.tr("eval.st.", c.Status))
	if c.Split != "" {
		kv("eval.f.split", c.Split)
	}
	if path != "" {
		kv("eval.f.file", text.Tilde(path))
	}
	if c.RejectReason != "" {
		add("eval.d.rejected")
		lines = append(lines, text.WrapHang(c.RejectReason, lw, "  ", "  ")...)
	}

	add("eval.d.prompt")
	for _, l := range markdown.Render(th, c.Prompt, max(lw-2, 10)) {
		lines = append(lines, "  "+l)
	}

	add("eval.d.source")
	kv("eval.f.commit", shortStr(c.Source.Commit, 12)+" ("+shared.Dash(c.Source.Subject)+")")
	kv("eval.f.base", shortStr(c.Source.Base, 12))
	kv("eval.f.author", strings.TrimSpace(c.Source.Author+" "+c.Source.Date))
	kv("eval.f.stats", m.t("eval.d.stats", i18n.A{"files": c.Stats.Files, "src": c.Stats.SourceLines, "test": c.Stats.TestLines}))
	if c.OriginalMessage != "" {
		kv("eval.f.original", strings.Join(strings.Fields(c.OriginalMessage), " "))
	}

	add("eval.d.verify")
	glyph, role := verifyGlyph(c.Verification)
	if v := c.Verification; v == nil {
		lines = append(lines, "  "+th.S(role, glyph)+" "+m.t("eval.d.verify.none"))
	} else {
		lines = append(lines, "  "+th.S(role, glyph)+" "+m.tr("eval.vs.", v.Status))
		lines = append(lines, text.WrapHang(m.t("eval.d.verify.detail", i18n.A{"before": yesNo(m, v.FailBefore), "after": yesNo(m, v.PassAfter), "runs": v.AfterRuns, "at": shared.Dash(v.VerifiedAt)}), lw, "    ", "    ")...)
		if v.Detail != "" {
			lines = append(lines, text.WrapHang(m.cli(v.Detail), lw, "    ", "    ")...)
		}
	}

	add("eval.d.leak")
	glyph, role = leakGlyph(c.Leak)
	lines = append(lines, "  "+th.S(role, glyph)+" "+m.tr("eval.ls.", c.Leak.Status))
	for _, f := range c.Leak.Findings {
		lines = append(lines, text.WrapHang(fmt.Sprintf("[%s] %s: %s", f.Severity, f.Kind, m.cli(f.Detail)), lw, "    ", "      ")...)
	}

	add("eval.d.spec")
	if c.Spec == nil {
		lines = append(lines, "  "+th.S(theme.Muted, "· ")+m.t("eval.d.spec.none"))
	} else {
		glyph, role = specGlyph(c.Spec)
		lines = append(lines, "  "+th.S(role, glyph)+" "+m.tr("eval.ss.", c.Spec.Status))
		for _, f := range c.Spec.Findings {
			lines = append(lines, text.WrapHang(fmt.Sprintf("%s: %s", f.Kind, m.cli(f.Detail)), lw, "    ", "      ")...)
		}
		for _, h := range c.Spec.Interface {
			lines = append(lines, text.WrapHang(h.Signature, lw, "    ", "      ")...)
		}
	}

	add("eval.d.test")
	switch {
	case cmdErr != nil:
		lines = append(lines, text.WrapHang(th.S(theme.Warn, "! ")+ui.ErrText(m.rw, cmdErr), lw, "  ", "    ")...)
	case cmd != "":
		lines = append(lines, text.WrapHang(cmd, lw, "  ", "    ")...)
	case c.TestCommand != "":
		lines = append(lines, text.WrapHang(c.TestCommand, lw, "  ", "    ")...)
	default:
		lines = append(lines, "  "+th.S(theme.Muted, m.t("eval.d.test.default")))
	}
	if len(c.TestFiles) > 0 {
		lines = append(lines, text.WrapHang(m.t("eval.d.test.files", i18n.A{"files": strings.Join(c.TestFiles, ", ")}), lw, "  ", "    ")...)
	}
	return lines
}

func yesNo(m *Model, b bool) string {
	if b {
		return m.t("eval.yes")
	}
	return m.t("eval.no")
}

// ---- thẻ Đợt ----

func batchGlyph(status string) (string, theme.Role) {
	switch status {
	case "completed":
		return "✓", theme.OK
	case "running":
		return "▶", theme.Key
	case "cancelled", "interrupted", "budget_exhausted":
		return "↺", theme.Warn
	case "failed":
		return "✗", theme.Err
	}
	return "·", theme.Muted
}

func (m *Model) batchListBox(env ui.Env, w, h int) string {
	th := env.Th
	iw, ih := frame.Inner(w, h)
	var body string
	if len(m.bidx) == 0 {
		body = "\n " + th.S(theme.Muted, m.t("eval.batches.empty"))
	} else {
		widths := list.Widths(iw-2, 1, []int{1, 10, 14, 14, 7, 0})
		body = m.blst.Render(iw, ih, func(i int, cursor bool) string {
			b := m.Data.Batches[m.bidx[i]]
			glyph, role := batchGlyph(b.Status)
			line := list.Row(widths, 1, th.S(role, glyph), th.S(theme.Muted, shared.Short(b.BatchID)), m.tr("eval.bst.", b.Status), strings.Join(b.Arms, ","),
				fmt.Sprintf("%d×%d", len(b.CaseIDs), b.Repeats), isoTime(b.StartedAt))
			if cursor {
				return th.S(theme.Key, "▸ ") + th.Selected(shared.StripColor(th, line))
			}
			return "  " + line
		})
	}
	return frame.Box{Title: m.t("eval.batches.title", i18n.A{"count": len(m.bidx)}), TitleAt: frame.Left, Pane: "list", Focused: m.pane == paneList, Top: m.filterChip(th, ""),
		BottomHints: m.hints(env, [][3]string{{"r", "eval.hint.run", "eval.run"}, {"R", "eval.hint.resume", "eval.resume"}, {"b", "eval.hint.baseline", "eval.baseline"}, {"B", "eval.hint.bootstrap", "eval.bootstrap"}, {"C", "eval.hint.clean", "eval.clean"}}), BottomAt: frame.Left}.
		Render(th, w, h, body)
}

func (m *Model) batchDetailBox(env ui.Env, w, h int) string {
	th := env.Th
	iw, ih := frame.Inner(w, h)
	b := m.CursorBatch()
	title := m.t("eval.report.title")
	var content, top string
	switch {
	case b == nil:
		content = "\n " + th.S(theme.Muted, m.t("eval.report.none"))
	default:
		title += " · " + shared.Short(b.BatchID)
		key := b.BatchID + "|" + m.baseline
		e, ok := m.reports[key]
		switch {
		case ok && e.err != nil:
			content = "\n" + strings.Join(ui.ErrBlock(th, ui.ErrText(m.rw, e.err), max(iw-1, 10), " "), "\n")
		case !ok || e.rep == nil:
			content = "\n  " + th.S(theme.Accent, env.Spin) + " " + th.S(theme.Muted, m.t("ui.loading"))
		default:
			m.detail.H = ih
			if m.detailKey != key {
				m.detailKey = key
				m.detail.Reset()
			}
			m.detail.SetWrapped(m.reportLines(th, e.rep, max(iw-1, 10)), max(iw-1, 10))
			content = m.detail.Render(iw, ih)
			top = th.S(theme.Muted, m.detail.Position())
		}
	}
	return frame.Box{Title: title, TitleAt: frame.Left, Pane: "detail", Focused: m.pane == paneDetail, Top: top,
		BottomHints: m.hints(env, [][3]string{{"b", "eval.hint.baseline", "eval.baseline"}, {"Enter", "eval.hint.focus", ""}}), BottomAt: frame.Left}.Render(th, w, h, content)
}

// ---- thẻ Log ----

// cellGlyph là ký hiệu của một ô trong lưới tiến độ (hình dạng riêng cho từng trạng thái, màu chỉ tô thêm).
func cellGlyph(th theme.Theme, status string) string {
	switch status {
	case "":
		return th.S(theme.Muted, "·")
	case "passed":
		return th.S(theme.OK, "✓")
	case "failed":
		return th.S(theme.Err, "✗")
	case "timeout":
		return th.S(theme.Warn, "◷")
	case "skipped":
		return th.S(theme.Muted, "–")
	}
	return th.S(theme.Warn, "!")
}

// gridLines dựng lưới ô của đợt: mỗi tác vụ một dòng, mỗi nhánh một nhóm ô theo lần lặp. Dịch vụ chỉ báo lượt đã
// xong nên ô có trạng thái chờ (·) hoặc kết quả; ô "đang chạy" cần sự kiện bắt đầu lượt mà dịch vụ không phát.
func (m *Model) gridLines(th theme.Theme, r *runState, w int) []string {
	p := r.plan
	var lines []string
	lines = append(lines, text.Truncate(m.t("eval.grid.progress", i18n.A{"done": r.done, "total": r.total}), w))
	lines = append(lines, th.S(theme.Muted, text.Truncate(m.t("eval.grid.legend"), w)))
	if p == nil {
		return lines
	}
	for _, id := range p.CaseIDs {
		var groups []string
		for _, arm := range p.Arms {
			var cells strings.Builder
			for rep := 1; rep <= p.Repeats; rep++ {
				cells.WriteString(cellGlyph(th, r.cells[cellKey(id, arm, rep)]))
			}
			groups = append(groups, arm+" "+cells.String())
		}
		lines = append(lines, text.WrapHang(shared.Short(id)+"  "+strings.Join(groups, "  "), w, "", "    ")...)
	}
	return lines
}

// stepLines dựng tiến độ kiểm chứng hoặc dựng tri thức của việc không phải chạy đợt.
func (m *Model) stepLines(th theme.Theme, r *runState, w int) []string {
	var lines []string
	steps := append([]actions.EvalVerifyStep(nil), r.verify...)
	sort.Slice(steps, func(i, j int) bool { return steps[i].Index < steps[j].Index })
	for _, s := range steps {
		glyph := th.S(theme.Muted, "·")
		if s.Done {
			glyph = th.S(theme.OK, "✓")
			if s.Status != "verified" {
				glyph = th.S(theme.Err, "✗")
			}
		}
		lines = append(lines, text.Truncate(fmt.Sprintf("%s %d/%d %s", glyph, s.Index, s.Total, s.CaseID), w))
	}
	for _, s := range r.boots {
		glyph := th.S(theme.OK, "✓")
		if s.Err != nil || s.Stopped {
			glyph = th.S(theme.Err, "✗")
		}
		lines = append(lines, text.Truncate(fmt.Sprintf("%s %d/%d %s", glyph, s.Index, s.Total, shortStr(s.Base, 12)), w))
	}
	return lines
}

func (m *Model) logBox(env ui.Env, w, h int) string {
	th := env.Th
	iw, ih := frame.Inner(w, h)
	j := m.lastJob()
	if j == nil {
		return frame.Box{Title: m.t("eval.log.title"), TitleAt: frame.Left, Focused: true}.Render(th, w, h, "\n "+th.S(theme.Muted, m.t("eval.log.none")))
	}
	state := m.t("eval.log.running", i18n.A{"label": j.Label})
	top := th.S(theme.Key, "Esc") + " " + th.S(theme.Muted, m.t("eval.log.cancel"))
	if !j.Ended.IsZero() {
		state = m.t("eval.log.finished", i18n.A{"label": j.Label})
		if j.Err != nil {
			state += " — " + ui.ErrText(m.rw, j.Err)
		}
		top = ""
	}
	var secs []logview.Section
	if r := m.run; r != nil {
		if r.resume != "" {
			secs = append(secs, logview.Section{Title: m.t("eval.log.resume_title"), Lines: []string{m.cli(r.resume)}})
		}
		grid := m.gridLines(th, r, max(iw-2, 10))
		if r.plan == nil {
			grid = m.stepLines(th, r, max(iw-2, 10))
		}
		if len(grid) > 0 {
			secs = append(secs, logview.Section{Title: m.t("eval.grid.title"), Lines: grid})
		}
	}
	secs = append(secs, logview.Section{Title: state, Lines: j.Log.Lines})
	m.logTotal, m.logH = logview.Total(secs), ih
	return frame.Box{Title: m.t("eval.log.title"), TitleAt: frame.Left, Pane: "view", Focused: true, Top: top}.Render(th, w, h, m.log.Render(th, secs, iw, ih))
}
