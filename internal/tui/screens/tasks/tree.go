package tasks

import (
	"strconv"
	"strings"

	"bint/internal/app"
	"bint/internal/i18n"
	"bint/internal/task"
	"bint/internal/tui/theme"
)

// treeRows xếp ts thành cây kế hoạch: phiếu gốc (không có cha, hoặc cha không nằm trong ts) và con của mỗi phiếu
// xếp theo thứ tự phụ thuộc (phiếu được phụ thuộc đứng trước, hoà thì giữ thứ tự của ts) và vẽ nhánh bằng ký
// tự nối cây (2 ô mỗi bậc). Mỗi phiếu xuất hiện đúng một lần (chống lặp).
func treeRows(ts []*task.Task) []row {
	in := make(map[string]bool, len(ts))
	for _, t := range ts {
		in[t.State.ID] = true
	}
	kids := map[string][]*task.Task{}
	var roots []*task.Task
	for _, t := range ts {
		if p := t.State.Parent; p != "" && in[p] && p != t.State.ID {
			kids[p] = append(kids[p], t)
		} else {
			roots = append(roots, t)
		}
	}
	roots = siblingOrder(roots)
	for p, k := range kids {
		kids[p] = siblingOrder(k)
	}
	var out []row
	seen := map[string]bool{}
	var walk func(t *task.Task, depth int, lead string, last bool)
	walk = func(t *task.Task, depth int, lead string, last bool) {
		if seen[t.State.ID] {
			return
		}
		seen[t.State.ID] = true
		prefix, next := "", ""
		if depth > 0 {
			if last {
				prefix, next = lead+"└ ", lead+"  "
			} else {
				prefix, next = lead+"├ ", lead+"│ "
			}
		}
		out = append(out, row{group: GroupOf(t.State.Status), t: t, depth: depth, prefix: prefix})
		var todo []*task.Task
		for _, c := range kids[t.State.ID] {
			if !seen[c.State.ID] {
				todo = append(todo, c)
			}
		}
		for i, c := range todo {
			walk(c, depth+1, next, i == len(todo)-1)
		}
	}
	for _, t := range roots {
		walk(t, 0, "", true)
	}
	for _, t := range ts { // chu trình cha–con: không có gốc, vẫn phải hiện
		walk(t, 0, "", true)
	}
	return out
}

// goalMarks là phần đầu ô mục tiêu: nhánh cây (ký tự nối), dấu phụ thuộc (◇ sẵn sàng, ⧗ chờ; chỉ phiếu có
// depends_on) và ⚑N phát hiện open (N > 0). Phiếu không có gì thì trả rỗng, hàng giữ nguyên như cũ.
func (m *Model) goalMarks(r row) string {
	var b strings.Builder
	b.WriteString(m.th.S(theme.Muted, r.prefix))
	t := r.t
	if len(t.State.DependsOn) > 0 {
		if m.Data.Ready[t.State.ID] {
			b.WriteString(m.th.S(theme.OK, "◇") + " ")
		} else {
			b.WriteString(m.th.S(theme.Warn, "⧗") + " ")
		}
	}
	if n := m.Data.OpenFindings[t.State.ID]; n > 0 {
		b.WriteString(m.th.S(theme.Warn, "⚑"+strconv.Itoa(n)) + " ")
	}
	return b.String()
}

// showFindings mở hộp chỉ đọc liệt kê phát hiện open qua mọi phiếu, nhóm theo phiếu.
func (m *Model) showFindings(fl app.FindingsList) {
	var body []string
	last := ""
	for _, v := range fl.Findings {
		if v.TaskID != last {
			if last != "" {
				body = append(body, "")
			}
			last = v.TaskID
			goal := ""
			for _, t := range m.Data.Tasks {
				if t.State.ID == v.TaskID {
					goal = t.Ticket.Goal
				}
			}
			body = append(body, m.th.S(theme.Accent, short(v.TaskID))+"  "+goal)
		}
		f := v.Finding
		line := "  " + m.th.S(theme.Warn, f.Severity) + " " + f.ID + "  " + f.Title
		if f.Path != "" {
			loc := f.Path
			if f.Lines != "" {
				loc += ":" + f.Lines
			}
			line += "  " + m.th.S(theme.Muted, loc)
		}
		body = append(body, line)
	}
	if len(body) == 0 {
		body = append(body, m.th.S(theme.OK, "✓ ")+m.t("tk.findings.none"))
	}
	if len(fl.Bad) > 0 {
		body = append(body, "", m.th.S(theme.Warn, m.t("tk.findings.bad", i18n.A{"count": len(fl.Bad)})))
	}
	m.openInfo(m.t("tk.findings.title", i18n.A{"count": len(fl.Findings)}), body)
}
