package knowledge

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"bint/internal/i18n"
	kn "bint/internal/knowledge"
	"bint/internal/tui/components/footer"
	"bint/internal/tui/components/frame"
	"bint/internal/tui/components/list"
	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

// statusGlyph là ký hiệu và vai trò màu của trạng thái (có ký hiệu lẫn màu để không phụ thuộc màu).
func statusGlyph(s string) (string, theme.Role) {
	switch s {
	case kn.StatusVerified:
		return "✓", theme.OK
	case kn.StatusCandidate:
		return "◌", theme.Accent
	case kn.StatusStale:
		return "⟳", theme.Warn
	}
	return "✗", theme.Muted
}

// View vẽ màn hình trong vùng env.W × env.H (không gồm khung ngoài).
func (m *Model) View(env ui.Env) string {
	m.w, m.h = env.W, env.H
	th := env.Th
	switch {
	case m.Err != nil && !m.Loaded:
		return strings.Join([]string{"", th.S(theme.Err, "✗ ") + m.t("kn.error"), strings.Join(text.WrapHang(ui.ErrText(m.rw, m.Err), max(env.W-4, 10), "  ", "  "), "\n"),
			"", th.S(theme.Muted, m.t("kn.error.hint"))}, "\n")
	case !m.Loaded:
		return "\n  " + th.S(theme.Accent, env.Spin) + " " + th.S(theme.Muted, m.t("ui.loading"))
	}
	bar := m.tabBar(env)
	bodyEnv := env.WithSize(env.W, max(env.H-1, 1))
	var body string
	switch {
	case m.frm != nil:
		body = m.formView(bodyEnv)
	case m.sub == subList:
		body = m.listView(bodyEnv)
	default:
		body = m.panelView(bodyEnv)
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

// tabBar vẽ thanh thẻ phụ (một dòng) và thông báo bên phải. Không đủ chỗ cho cả tám thẻ thì chỉ hiện thẻ
// hiện tại kèm "‹ i/8 ›".
func (m *Model) tabBar(env ui.Env) string {
	th := env.Th
	names := make([]string, numSubs)
	total := 0
	for i, n := range subNames {
		names[i] = m.t("kn.sub." + n)
		total += text.Width(names[i]) + 3
	}
	toast := ""
	if m.toast != "" {
		role := theme.OK
		if m.toastErr {
			role = theme.Warn
		}
		toast = th.S(role, " "+m.toast+" ")
	}
	if toast == "" && m.sel.Count() > 0 && m.sub == subList {
		toast = th.S(theme.Key, " ● "+m.t("kn.list.selected", i18n.A{"count": m.sel.Count()})+" ")
	}
	var bar string
	if total+text.Width(toast) <= env.W-2 {
		var parts []string
		for i, n := range names {
			if i == m.sub {
				parts = append(parts, ui.MarkSub(i, th.Selected(" "+n+" ")))
			} else {
				parts = append(parts, ui.MarkSub(i, th.S(theme.Muted, " "+n+" ")))
			}
		}
		bar = " " + strings.Join(parts, th.S(theme.Muted, "·"))
	} else {
		bar = " " + ui.MarkSub((m.sub+numSubs-1)%numSubs, th.S(theme.Muted, "‹ ")) + th.Selected(" "+names[m.sub]+" ") +
			ui.MarkSub((m.sub+1)%numSubs, th.S(theme.Muted, fmt.Sprintf(" %d/%d ›", m.sub+1, numSubs)))
	}
	if toast != "" {
		room := env.W - text.Width(bar) - 1
		if m.toast != "" {
			toast = ui.ToastLabel(th, m.toast, m.toastErr, max(room, 0))
		} else {
			toast = text.Truncate(toast, max(room, 0))
		}
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

// ---- danh sách và chi tiết ----

// filterChips mô tả các điều kiện lọc đang bật, ví dụ "candidate+stale · nguon:lo-thu".
func (m *Model) filterChips() string {
	o := m.fopts
	var parts []string
	switch o.Status {
	case "":
	case "pending":
		parts = append(parts, m.t("kn.filter.pending"))
	default:
		parts = append(parts, o.Status)
	}
	if o.Type != "" {
		parts = append(parts, o.Type)
	}
	if o.Origin != "" {
		parts = append(parts, o.Origin)
	}
	if o.ThirdParty {
		parts = append(parts, "⚑")
	}
	if o.Batch != "" {
		b := o.Batch
		if !strings.HasPrefix(b, kn.BatchTagPrefix) {
			b = kn.BatchTagPrefix + b
		}
		parts = append(parts, b)
	}
	if o.NoBatch && o.Batch == "" {
		parts = append(parts, m.t("kn.filter.no_batch"))
	}
	if o.Path != "" {
		parts = append(parts, o.Path)
	}
	if o.Text != "" {
		parts = append(parts, "“"+o.Text+"”")
	}
	if len(parts) == 0 {
		return m.t("kn.filter.none")
	}
	return strings.Join(parts, " · ")
}

func (m *Model) listView(env ui.Env) string {
	w, h := env.W, env.H
	wide := w >= WideW
	showList, showDetail := true, wide
	if !wide && m.pane == paneDetail {
		showList, showDetail = false, true
	}
	lw, rw := w, w
	if wide {
		lw = max(w*5/12, 48)
		rw = w - lw
	}
	var lbox, rbox []string
	if showList {
		lbox = strings.Split(m.listBox(env, lw, h), "\n")
	}
	if showDetail {
		rbox = strings.Split(m.detailBox(env, rw, h), "\n")
	}
	switch {
	case showList && showDetail:
		out := make([]string, len(lbox))
		for i := range lbox {
			out[i] = lbox[i] + rbox[i]
		}
		return strings.Join(out, "\n")
	case showList:
		return strings.Join(lbox, "\n")
	}
	return strings.Join(rbox, "\n")
}

func (m *Model) listBox(env ui.Env, w, h int) string {
	th := env.Th
	iw, ih := frame.Inner(w, h)
	title := m.t("kn.list.title", i18n.A{"shown": len(m.view), "total": len(m.Data.Items)})
	chips := th.S(theme.Key, text.Truncate(m.filterChips(), max(w/2, 12)))
	var body string
	switch {
	case len(m.Data.Items) == 0:
		body = "\n " + th.S(theme.Muted, m.t("kn.empty"))
	case len(m.view) == 0:
		body = "\n " + th.S(theme.Muted, m.t("kn.empty_filtered"))
	default:
		widths := list.Widths(iw-2, 1, []int{1, 1, 1, 8, 0, 14})
		body = m.lst.Render(iw, ih, func(i int, cursor bool) string {
			m.RowsDrawn++
			it := m.Data.Items[m.view[i]]
			glyph, role := statusGlyph(it.Status)
			sel, flag := " ", " "
			if m.sel.Has(it.ID) {
				sel = "●"
			}
			if it.Origin.ThirdParty {
				flag = "⚑"
			}
			scope := "*"
			if !it.Scope.Global() {
				scope = strings.Join(it.Scope.Paths, ",")
			}
			line := list.Row(widths, 1, sel, th.S(role, glyph), flag, it.Type, it.Title, th.S(theme.Muted, scope))
			if cursor {
				return th.S(theme.Key, "▸ ") + th.Selected(stripColor(th, line))
			}
			return "  " + line
		})
	}
	return frame.Box{Title: title, TitleAt: frame.Left, Pane: "list", Focused: m.pane == paneList, Top: chips}.Render(th, w, h, body)
}

// stripColor bỏ mã màu để dòng được chọn chỉ mang kiểu "được chọn" (không lẫn màu của từng cột).
func stripColor(th theme.Theme, s string) string {
	if !th.Colored() {
		return s
	}
	return ansi.Strip(s)
}

func (m *Model) detailBox(env ui.Env, w, h int) string {
	th := env.Th
	iw, ih := frame.Inner(w, h)
	it := m.Cursor()
	m.detail.H = ih
	var lines []string
	if it == nil {
		lines = []string{"", " " + th.S(theme.Muted, m.t("kn.detail.none"))}
	} else {
		if m.detailID != it.ID {
			m.detailID = it.ID
			m.detail.Reset()
		}
		lines = m.detailLines(env, it, max(iw-1, 10))
	}
	m.detail.SetWrapped(lines, max(iw-1, 10))
	return frame.Box{Title: m.t("kn.detail.title"), TitleAt: frame.Left, Pane: "detail", Focused: m.pane == paneDetail,
		Top: th.S(theme.Muted, m.detail.Position())}.Render(th, w, h, m.detail.Render(iw, ih))
}

func (m *Model) section(env ui.Env, label string, w int) string {
	s := "── " + label + " "
	return env.Th.S(theme.Accent, s+strings.Repeat("─", max(w+1-text.Width(s), 0)))
}

// detailLines dựng nội dung chi tiết của một mục: siêu dữ liệu, nội dung, bằng chứng kèm trích đoạn dòng và
// kết quả kiểm tra. Bằng chứng/kiểm tra nạp lười: chưa có thì hiện "đang đọc".
func (m *Model) detailLines(env ui.Env, it *kn.Item, w int) []string {
	th := env.Th
	var l []string
	add := func(s ...string) { l = append(l, s...) }
	glyph, role := statusGlyph(it.Status)
	origin := it.Origin.Kind
	if it.Origin.ThirdParty {
		origin += " ⚑"
	}
	add(th.S(theme.Muted, it.ID), th.S(role, glyph)+" "+it.Type+" · "+it.Status+" · "+origin)
	for _, s := range text.Wrap(it.Title, w) {
		add(th.Bold(s))
	}
	meta := func(k, v string) {
		if strings.TrimSpace(v) == "" {
			return
		}
		pre := text.Pad(m.t("kn.meta."+k), 12) + " "
		for i, s := range text.WrapHang(v, w, pre, strings.Repeat(" ", text.Width(pre))) {
			if i == 0 {
				s = th.S(theme.Accent, s[:len(pre)]) + s[len(pre):]
			}
			add(s)
		}
	}
	scope := m.t("kn.meta.scope.global")
	if !it.Scope.Global() {
		scope = strings.Join(it.Scope.Paths, ", ")
	}
	meta("scope", scope)
	meta("tags", strings.Join(append(append(append([]string{}, it.Scope.Symbols...), it.Scope.Tags...), it.Scope.Domains...), ", "))
	src := origin
	if it.Origin.Session != "" {
		src += ", " + m.t("kn.meta.session") + " " + it.Origin.Session
	}
	if it.Origin.RunID != "" {
		src += ", " + m.t("kn.meta.run") + " " + it.Origin.RunID
	}
	meta("origin", fmt.Sprintf("%s (%s, %s)", src, it.Author, it.CreatedAt))
	meta("inputs", strings.Join(it.Origin.Inputs, ", "))
	if it.ReviewedBy != "" {
		meta("reviewer", it.ReviewedBy+" ("+it.ReviewedAt+")")
	}
	meta("reason", it.StatusReason)
	meta("confidence", it.Confidence)
	meta("file", text.Path(it.Path, max(w-14, 10)))

	add("", m.section(env, m.t("kn.section.body"), w))
	for _, s := range text.Wrap(strings.TrimSpace(it.Body), w) {
		add(s)
	}

	d, have := m.cache[it.ID]
	derr := m.cacheErr[it.ID]
	add("", m.section(env, m.t("kn.section.evidence"), w))
	if len(it.Evidence) == 0 {
		add(th.S(theme.Muted, m.t("kn.evidence.none")))
	}
	for i, e := range it.Evidence {
		extra := ""
		if e.Commit != "" {
			extra = " (commit " + short(e.Commit) + ")"
		}
		add(th.S(theme.Accent, e.String()+extra))
		switch {
		case have && i < len(d.Evidence) && d.Evidence[i].Missing:
			add(th.S(theme.Err, "    "+m.t("kn.evidence.missing")))
		case have && i < len(d.Evidence):
			for _, x := range d.Evidence[i].Lines {
				add(th.S(theme.Muted, "  │ ") + x)
			}
			if n := d.Evidence[i].More; n > 0 {
				add(th.S(theme.Muted, "  │       "+m.t("kn.evidence.more", i18n.A{"count": n})))
			}
		case derr == nil:
			add(th.S(theme.Muted, "    "+m.t("ui.loading")))
		}
	}

	add("", m.section(env, m.t("kn.section.check"), w))
	switch {
	case derr != nil:
		add(ui.ErrBlock(th, ui.ErrText(m.rw, derr), w, "")...)
	case !have:
		add(th.S(theme.Muted, m.t("ui.loading")))
	case len(d.Check.Findings) == 0:
		add(th.S(theme.OK, "✓ ") + m.t("kn.check.ok"))
	default:
		errs := 0
		for _, f := range d.Check.Findings {
			if f.Level == kn.LevelError {
				errs++
			}
		}
		if errs > 0 {
			add(th.S(theme.Err, "✗ ") + m.cat.N("kn.check.errors", errs) + th.S(theme.Muted, " · "+m.t("kn.check.technical")))
		} else {
			add(th.S(theme.Warn, "! ") + m.t("kn.check.warnings") + th.S(theme.Muted, " · "+m.t("kn.check.technical")))
		}
		for _, f := range d.Check.Findings {
			icon, r := "!", theme.Warn
			if f.Level == kn.LevelError {
				icon, r = "✗", theme.Err
			}
			pre := th.S(r, icon) + " " + m.findingLabel(f)
			add(pre)
			if iss, ok := f.IssueOf(); ok {
				// Bằng chứng lệch: dòng có cấu trúc dựng qua i18n (vị trí cũ, vị trí mới/ứng viên/số dòng, cách xử lý), không qua Rewriter.
				for _, ln := range ui.EvidenceLines(m.rw, iss) {
					for _, s := range text.Wrap(ln, max(w-4, 8)) {
						add("    " + th.S(theme.Muted, s))
					}
				}
				continue
			}
			for _, s := range text.Wrap(m.cli(f.Detail), max(w-4, 8)) {
				add("    " + th.S(theme.Muted, s))
			}
		}
	}
	return l
}

// ---- thẻ phụ ----

func (m *Model) panelView(env ui.Env) string {
	th, w, h := env.Th, env.W, env.H
	p := m.cur()
	title := m.t("kn.sub." + subNames[m.sub])
	if m.sub == subSurvey {
		title += " · " + m.t([]string{"kn.survey.inspect", "kn.survey.bootstrap", "kn.survey.skill"}[m.survM])
	}
	formH := 0
	var formBox string
	if p.form != nil {
		fields := len(p.form.Fields)
		formH = min(fields+6, max(h/2, 7))
		if m.sub == subPublish {
			formH = 6
		}
		fi, fh := frame.Inner(w, formH)
		formBox = frame.Box{Title: m.t("kn.form.title"), TitleAt: frame.Left, Focused: p.inForm}.
			Render(th, w, formH, p.form.View(env.WithSize(fi, fh), 0))
	}
	rh := h - formH
	_, ih := frame.Inner(w, rh)
	iw, _ := frame.Inner(w, rh)
	var body string
	switch {
	case p.running:
		body = "\n " + th.S(theme.Accent, env.Spin) + " " + th.S(theme.Muted, m.t("kn.running")) + "  " + th.S(theme.Key, "Esc") + " " + th.S(theme.Muted, m.t("kn.cancel"))
	case p.err != nil:
		body = "\n" + strings.Join(ui.ErrBlock(th, ui.ErrText(m.rw, p.err), max(iw-1, 8), " "), "\n")
	case !p.ran && p.lines == nil:
		body = "\n " + th.S(theme.Muted, m.t("kn.panel.hint."+subNames[m.sub]))
	default:
		p.view.SetWrapped(p.lines, max(iw-1, 10))
		body = p.view.Render(iw, ih)
	}
	top := ""
	if pos := p.view.Position(); pos != "" && !p.running {
		top = th.S(theme.Muted, pos)
	}
	resBox := frame.Box{Title: title, TitleAt: frame.Left, Pane: "view", Focused: !p.inForm, Top: top, BottomHints: m.panelFooter(env, p), BottomAt: frame.Left}.Render(th, w, rh, body)
	if formBox == "" {
		return resBox
	}
	return formBox + "\n" + resBox
}

// panelFooter là các phím của thẻ phụ ở viền dưới khung kết quả.
func (m *Model) panelFooter(env ui.Env, p *panel) []footer.Hint {
	var hints []footer.Hint
	k := func(key, label, action string) {
		hints = append(hints, footer.Hint{Key: key, Label: label, Priority: len(hints) + 1, ActionID: action})
	}
	switch {
	case p.running:
		k("Esc", m.t("kn.cancel"), "")
	case p.inForm:
		k("Enter", m.t("form.hint.submit"), "")
		k("Esc", m.t("kn.hint.leave"), "")
	default:
		if p.form != nil {
			k("Enter", m.t("kn.hint.edit_form"), "")
		} else {
			k("r", m.t("kn.hint.rerun"), p.action)
		}
		if m.sub == subPublish {
			k("w", m.t("kn.publish.write"), "bootstrap.publish")
		}
		if m.sub == subSurvey {
			k("m", m.t("kn.hint.mode"), "")
		}
		k("j/k", m.t("kn.hint.scroll"), "")
		k(", .", m.t("kn.hint.sub"), "")
		k("Esc", m.t("kn.hint.back"), "")
	}
	return env.AllowedHints(hints)
}
