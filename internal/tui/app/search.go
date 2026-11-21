package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"bint/internal/tui/actions"
	"bint/internal/tui/components/palette"
	"bint/internal/tui/screens/shared"
)

// Tìm mọi nơi (Ctrl+F): hộp như bảng lệnh nhưng trên dữ liệu — mọi mục tri thức (kể cả deprecated), mọi tác vụ (kể cả đã xong/huỷ),
// lượt chạy và ứng viên đánh giá đã nạp. Enter chuyển tới màn tương ứng và đặt con trỏ vào mục. Mở hộp thì các màn chưa nạp được nạp
// ở nền; danh sách tự bổ sung khi dữ liệu về.

// Tiền tố ID của dòng tìm thấy theo nguồn.
const (
	findKnowledge = "kn:"
	findTask      = "tk:"
	findRun       = "rn:"
	findEval      = "el:"
)

// openFind mở hộp tìm mọi nơi và nạp các màn chưa có dữ liệu.
func (m *Model) openFind() tea.Cmd {
	p := palette.New(m.findItems())
	m.pal, m.palFind = &p, true
	var cmds []tea.Cmd
	for _, s := range []actions.Screen{actions.Knowledge, actions.Tasks, actions.Runs, actions.Eval} {
		if sc := m.screens[s]; sc != nil {
			cmds = append(cmds, sc.Ensure())
		}
	}
	return tea.Batch(cmds...)
}

// refreshFind dựng lại danh sách của hộp tìm (dữ liệu vừa về) và giữ chữ đang gõ.
func (m *Model) refreshFind() {
	if m.pal == nil || !m.palFind {
		return
	}
	q := m.pal.Query
	p := palette.New(m.findItems())
	p.Type(q)
	m.pal = &p
}

// findItems là các dòng tìm được từ dữ liệu các màn đã nạp.
func (m *Model) findItems() []palette.Item {
	var out []palette.Item
	group := func(s actions.Screen) string { return m.cat.T("tab." + string(s)) }
	for _, it := range m.kn.Data.Items {
		out = append(out, palette.Item{ID: findKnowledge + it.ID, Title: it.Title, Group: group(actions.Knowledge),
			Keywords: strings.Join([]string{it.ID, it.Type, it.Status, strings.Join(it.Scope.Paths, " ")}, " "), Key: it.Status})
	}
	for _, t := range m.tk.Data.Tasks {
		title := t.Ticket.Goal
		if title == "" {
			title = firstLine(t.Ticket.Request)
		}
		out = append(out, palette.Item{ID: findTask + t.State.ID, Title: title, Group: group(actions.Tasks),
			Keywords: t.State.ID + " " + string(t.State.Status), Key: string(t.State.Status)})
	}
	for _, r := range m.rn.Data.Rows {
		title := shared.Short(r.Run.RunID) + " · " + r.Run.Harness.String + " · " + shared.Time(r.Run.StartedAt.String)
		if r.Reason != "" {
			title += " · " + r.Reason
		}
		out = append(out, palette.Item{ID: findRun + r.Run.RunID, Title: title, Group: group(actions.Runs),
			Keywords: r.Run.RunID + " " + r.Run.Status + " " + r.Label, Key: r.Run.Status})
	}
	for _, c := range m.el.Data.Pending {
		out = append(out, palette.Item{ID: findEval + c.ID, Title: c.Source.Subject, Group: group(actions.Eval), Keywords: c.ID, Key: c.Status})
	}
	return out
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

// findJump chuyển tới mục id của hộp tìm.
func (m *Model) findJump(id string) (tea.Model, tea.Cmd) {
	switch {
	case strings.HasPrefix(id, findKnowledge):
		m.goTo(actions.Knowledge)
		return m, tea.Batch(m.kn.FocusID(strings.TrimPrefix(id, findKnowledge)), m.startTick())
	case strings.HasPrefix(id, findTask):
		m.goTo(actions.Tasks)
		return m, tea.Batch(m.tk.Focus(strings.TrimPrefix(id, findTask)), m.startTick())
	case strings.HasPrefix(id, findRun):
		m.goTo(actions.Runs)
		return m, tea.Batch(m.rn.Focus(strings.TrimPrefix(id, findRun)), m.startTick())
	case strings.HasPrefix(id, findEval):
		m.goTo(actions.Eval)
		return m, tea.Batch(m.el.Focus(strings.TrimPrefix(id, findEval)), m.startTick())
	}
	return m, nil
}
