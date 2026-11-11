package tasks

import (
	"fmt"
	"strings"

	"bint/internal/i18n"
	"bint/internal/task"
	"bint/internal/tui/components/frame"
	"bint/internal/tui/components/list"
	"bint/internal/tui/components/logview"
	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"

	"github.com/charmbracelet/x/ansi"
)

// statusGlyph là ký hiệu và vai trò màu của trạng thái (có ký hiệu lẫn màu để không phụ thuộc màu).
func statusGlyph(s string) (string, theme.Role) {
	switch s {
	case task.StatusDone:
		return "✓", theme.OK
	case task.StatusInProgress:
		return "▶", theme.Accent
	case task.StatusInReview:
		return "◐", theme.Accent
	case task.StatusBlocked:
		return "!", theme.Warn
	case task.StatusInterrupted:
		return "~", theme.Warn
	case task.StatusFailed:
		return "✗", theme.Err
	case task.StatusCancelled:
		return "–", theme.Muted
	case task.StatusPlanned:
		return "◌", theme.Fg
	}
	return "○", theme.Fg
}

// View vẽ màn hình trong vùng env.W × env.H (không gồm khung ngoài).
func (m *Model) View(env ui.Env) string {
	m.w, m.h = env.W, env.H
	th := env.Th
	switch {
	case m.Err != nil && !m.Loaded:
		return strings.Join([]string{"", th.S(theme.Err, "✗ ") + m.t("tk.error"), "  " + text.Truncate(ui.ErrText(m.cat, m.Err), max(env.W-4, 10)),
			"", th.S(theme.Muted, m.t("tk.error.hint"))}, "\n")
	case !m.Loaded:
		return "\n  " + th.S(theme.Accent, env.Spin) + " " + th.S(theme.Muted, m.t("ui.loading"))
	}
	bar := m.tabBar(env)
	bodyEnv := env.WithSize(env.W, max(env.H-1, 1))
	var body string
	if m.frm != nil {
		body = m.formView(bodyEnv)
	} else {
		body = m.listView(bodyEnv)
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

// tabBar vẽ thanh thẻ con (một dòng) và thông báo bên phải.
func (m *Model) tabBar(env ui.Env) string {
	th := env.Th
	vs := m.visibleSubs()
	names := make([]string, len(vs))
	total := 0
	for i, s := range vs {
		names[i] = m.t("tk.sub." + subNames[s])
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
	cur := 0
	for i, s := range vs {
		if s == m.sub {
			cur = i
		}
	}
	var bar string
	if total+text.Width(toast) <= env.W-2 {
		var parts []string
		for i, n := range names {
			if i == cur {
				parts = append(parts, th.Selected(" "+n+" "))
			} else {
				parts = append(parts, th.S(theme.Muted, " "+n+" "))
			}
		}
		bar = " " + strings.Join(parts, th.S(theme.Muted, "·"))
	} else {
		bar = " " + th.S(theme.Muted, "‹ ") + th.Selected(" "+names[cur]+" ") + th.S(theme.Muted, fmt.Sprintf(" %d/%d ›", cur+1, len(vs)))
	}
	if toast != "" {
		room := env.W - text.Width(bar) - 1
		toast = text.Truncate(toast, max(room, 0))
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

// filterChips mô tả các điều kiện lọc đang bật.
func (m *Model) filterChips() string {
	var parts []string
	if m.showAll {
		parts = append(parts, m.t("tk.filter.all"))
	}
	if m.filter != "" {
		parts = append(parts, "“"+m.filter+"”")
	}
	if m.textIn {
		parts = append(parts, "/"+m.filter+"▏")
	}
	if len(parts) == 0 {
		return m.t("tk.filter.none")
	}
	return strings.Join(parts, " · ")
}

func (m *Model) groupTitle(g, n int) string {
	return m.t("tk.group."+[]string{"waiting", "active", "planned", "finished"}[g], i18n.A{"count": n})
}

func (m *Model) listBox(env ui.Env, w, h int) string {
	th := env.Th
	iw, ih := frame.Inner(w, h)
	shown := len(m.Visible())
	title := m.t("tk.list.title", i18n.A{"shown": shown, "total": len(m.Data.Tasks)})
	chips := th.S(theme.Key, text.Truncate(m.filterChips(), max(w/2, 12)))
	counts := map[int]int{}
	for _, r := range m.rows {
		if r.t != nil {
			counts[r.group]++
		}
	}
	var body string
	switch {
	case len(m.Data.Tasks) == 0:
		body = "\n " + th.S(theme.Muted, m.t("tk.empty"))
	case shown == 0:
		body = "\n " + th.S(theme.Muted, m.t("tk.empty_filtered"))
	default:
		widths := list.Widths(iw-2, 1, []int{1, 11, 6, 0})
		body = m.lst.Render(iw, ih, func(i int, cursor bool) string {
			r := m.rows[i]
			if r.t == nil {
				return " " + th.S(theme.Accent, text.Truncate(m.groupTitle(r.group, counts[r.group]), max(iw-2, 4)))
			}
			t := r.t
			glyph, role := statusGlyph(t.State.Status)
			if m.o.Jobs.ForTask(t.State.ID) != nil {
				glyph, role = "⟳", theme.Key
			}
			line := list.Row(widths, 1, th.S(role, glyph), m.statusName(t.State.Status), th.S(theme.Muted, short(t.State.ID)[max(len(short(t.State.ID))-6, 0):]), t.Ticket.Goal)
			if cursor {
				return th.S(theme.Key, "▸ ") + th.Selected(stripColor(th, line))
			}
			return "  " + line
		})
	}
	return frame.Box{Title: title, TitleAt: frame.Left, Focused: m.pane == paneList, Top: chips}.Render(th, w, h, body)
}

func (m *Model) statusName(s string) string {
	if k := "tk.status." + s; m.cat.Has(k) {
		return m.t(k)
	}
	return s
}

// stripColor bỏ mã màu để dòng được chọn chỉ mang kiểu "được chọn".
func stripColor(th theme.Theme, s string) string {
	if !th.Colored() {
		return s
	}
	return ansi.Strip(s)
}

func (m *Model) detailBox(env ui.Env, w, h int) string {
	th := env.Th
	iw, ih := frame.Inner(w, h)
	t := m.Cursor()
	title := m.t("tk.detail.title")
	var content string
	top := ""
	switch {
	case t == nil:
		content = "\n " + th.S(theme.Muted, m.t("tk.detail.none"))
	case m.sub == subLog:
		content = m.logBody(env, t, iw, ih)
		if j := m.jobHere(); j != nil {
			top = th.S(theme.Key, "Esc") + " " + th.S(theme.Muted, m.t("tk.log.cancel"))
		}
	default:
		title += " · " + short(t.State.ID)
		key := t.State.ID + "/" + subNames[m.sub]
		m.detail.H = ih
		if m.detailKey != key {
			m.detailKey = key
			m.detail.Reset()
		}
		lines, sel := m.tabLines(env, t, max(iw-1, 10))
		m.detail.SetLines(lines)
		if sel >= 0 { // giữ dòng được chọn (phát hiện review) trong vùng nhìn
			if sel < m.detail.Top {
				m.detail.Top = sel
			} else if sel >= m.detail.Top+ih {
				m.detail.Top = sel - ih + 1
			}
		}
		content = m.detail.Render(iw, ih)
		top = th.S(theme.Muted, m.detail.Position())
	}
	return frame.Box{Title: title, TitleAt: frame.Left, Focused: m.pane == paneDetail, Top: top,
		Bottom: m.detailFooter(env, t), BottomAt: frame.Left}.Render(th, w, h, content)
}

// detailFooter là các phím riêng của thẻ con hiện tại ở viền dưới khung chi tiết.
func (m *Model) detailFooter(env ui.Env, t *task.Task) string {
	if t == nil {
		return ""
	}
	th := env.Th
	var hints []string
	k := func(key, label string) { hints = append(hints, th.S(theme.Key, key)+" "+th.S(theme.Muted, label)) }
	switch m.sub {
	case subDocs:
		k("d", m.t("tk.hint.doc"))
		k("e", m.t("tk.hint.edit_doc"))
	case subReview:
		k("j/k", m.t("tk.hint.finding"))
		k("Enter", m.t("tk.hint.excerpt"))
		k("e", m.t("tk.hint.open_editor"))
		k("K", m.t("tk.hint.knowledge"))
		k("w", m.t("tk.hint.review_edit"))
	case subCriteria:
		k("m", m.t("tk.hint.criteria"))
	case subErrors:
		k("i", m.t("tk.hint.error"))
	case subVerify:
		k("v", m.t("tk.hint.verify"))
	}
	if len(hints) == 0 {
		return ""
	}
	return " " + strings.Join(hints, "  ") + " "
}

// logBody vẽ thẻ Log: dòng của orchestrator ở trên, đuôi log của bước agent đang chạy ở dưới.
func (m *Model) logBody(env ui.Env, t *task.Task, w, h int) string {
	th := env.Th
	j := m.jobHere()
	if j == nil {
		j = m.o.Jobs.Last(t.State.ID)
	}
	if j == nil {
		return "\n " + th.S(theme.Muted, m.t("tk.log.none"))
	}
	state := m.t("tk.log.running", i18n.A{"label": j.Label})
	if !j.Ended.IsZero() {
		state = m.t("tk.log.finished", i18n.A{"label": j.Label})
		if j.Err != nil {
			state += " — " + ui.ErrText(m.cat, j.Err)
		}
	}
	secs := []logview.Section{{Title: state, Lines: j.Log.Lines}}
	if len(j.Tail.Lines) > 0 || j.Tailing() {
		secs = append(secs, logview.Section{Title: m.t("tk.log.step", i18n.A{"step": j.Step.Step}), Lines: j.Tail.Lines})
	}
	m.logTotal, m.logH = logview.Total(secs), h
	return m.log.Render(th, secs, w, h)
}
