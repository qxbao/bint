package tasks

import (
	"slices"
	"strings"

	"bint/internal/i18n"
	"bint/internal/task"
	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
)

// showGraph mở hộp chỉ đọc Đồ thị phụ thuộc của kế hoạch chứa phiếu đang chọn: các bậc theo thứ tự topo, mỗi phiếu
// kèm trạng thái, dấu sẵn sàng/chờ, phụ thuộc chưa xong; phiếu sẵn sàng kế tiếp; chu trình và phụ thuộc không đọc
// được hiện riêng. Chỉ tính từ m.Data (không gọi ra ngoài).
func (m *Model) showGraph() {
	cur := m.Cursor()
	if cur == nil {
		return
	}
	plan := planOf(m.Data.Tasks, cur.State.ID)
	if plan == nil {
		m.openInfo(m.t("tk.graph.title", i18n.A{"id": short(cur.State.ID)}),
			[]string{m.t("tk.graph.solo", i18n.A{"id": short(cur.State.ID)})})
		return
	}
	status := map[string]string{}
	for _, t := range m.Data.Tasks {
		status[t.State.ID] = t.State.Status
	}
	g := layers(plan, m.Data.Tasks)

	var body []string
	var next *task.Task
	hasChild := map[string]bool{}
	for _, t := range plan {
		hasChild[t.State.Parent] = true
	}
	// Tiếp theo: phiếu sẵn sàng chưa bắt đầu (created/planned), cũ nhất trước. Phiếu cha (có con) không phải việc để
	// chạy; phiếu đang chạy/review/blocked không gợi ý `bint run b-do --task` (lệnh đó lập lượt mới).
	for _, t := range plan {
		s := t.State.Status
		if m.Data.Ready[t.State.ID] && !hasChild[t.State.ID] && (s == task.StatusCreated || s == task.StatusPlanned) &&
			(next == nil || t.State.ID < next.State.ID) {
			next = t
		}
	}
	if next != nil {
		body = append(body, m.th.S(theme.Accent, m.t("tk.graph.next"))+" "+short(next.State.ID)+"  "+
			m.th.S(theme.Muted, m.cli("bint run b-do --task "+next.State.ID)))
	} else {
		body = append(body, m.th.S(theme.Muted, m.t("tk.graph.next_none")))
	}
	for i, lv := range g.Levels {
		body = append(body, "", m.th.S(theme.Accent, m.t("tk.graph.level", i18n.A{"n": i + 1})))
		for _, t := range lv {
			body = append(body, m.graphLines(t, cur, status)...)
		}
	}
	if len(g.Cycle) > 0 {
		body = append(body, "", m.th.S(theme.Err, m.t("tk.graph.cycle")))
		for _, t := range g.Cycle {
			body = append(body, m.graphLines(t, cur, status)...)
		}
	}
	if len(g.Missing) > 0 {
		body = append(body, "", m.th.S(theme.Warn, m.t("tk.graph.missing")))
		for _, t := range g.Missing {
			var ids []string
			for _, d := range t.State.DependsOn {
				if _, ok := status[d]; !ok {
					ids = append(ids, short(d))
				}
			}
			body = append(body, "  "+short(t.State.ID)+"  "+m.th.S(theme.Muted, strings.Join(ids, ", ")))
		}
	}
	m.openInfo(m.t("tk.graph.title", i18n.A{"id": short(rootID(m.Data.Tasks, cur.State.ID))}), body)
}

// graphLines là dòng của một phiếu trong hộp đồ thị: đánh dấu đang chọn, trạng thái, ID, mục tiêu, dấu sẵn sàng/chờ,
// rồi (nếu có) dòng "chờ:" liệt kê phụ thuộc chưa done.
func (m *Model) graphLines(t, cur *task.Task, status map[string]string) []string {
	glyph, role := statusGlyph(t.State.Status)
	pick := "  "
	if t.State.ID == cur.State.ID {
		pick = m.th.S(theme.Accent, "▸ ")
	}
	mark := ""
	if len(t.State.DependsOn) > 0 {
		if m.Data.Ready[t.State.ID] {
			mark = " " + m.th.S(theme.OK, "◇ "+m.t("tk.graph.ready"))
		} else if !task.Terminal(t.State.Status) {
			mark = " " + m.th.S(theme.Warn, "⧗ "+m.t("tk.graph.waiting"))
		}
	} else if m.Data.Ready[t.State.ID] {
		mark = " " + m.th.S(theme.OK, "◇ "+m.t("tk.graph.ready"))
	}
	line := pick + m.th.S(role, glyph) + " " + short(t.State.ID) + "  " + text.Truncate(t.Ticket.Goal, 40) +
		"  " + m.th.S(theme.Muted, m.statusName(t.State.Status)) + mark
	out := []string{line}
	var wait []string
	for _, d := range t.State.DependsOn {
		if s, ok := status[d]; ok && s != task.StatusDone {
			wait = append(wait, short(d)+" ("+m.statusName(s)+")")
		}
	}
	if len(wait) > 0 {
		slices.Sort(wait)
		out = append(out, "      "+m.th.S(theme.Muted, m.t("tk.graph.wait_for", i18n.A{"ids": strings.Join(wait, ", ")})))
	}
	return out
}

// rootID leo parent từ id tới phiếu gốc (dừng khi cha vắng hoặc có vòng).
func rootID(ts []*task.Task, id string) string {
	by := make(map[string]*task.Task, len(ts))
	for _, t := range ts {
		by[t.State.ID] = t
	}
	seen := map[string]bool{id: true}
	for {
		t := by[id]
		if t == nil || t.State.Parent == "" || seen[t.State.Parent] || by[t.State.Parent] == nil {
			return id
		}
		id = t.State.Parent
		seen[id] = true
	}
}
