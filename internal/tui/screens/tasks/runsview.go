package tasks

import (
	"fmt"
	"strings"

	"bint/internal/app"
	"bint/internal/task"
	"bint/internal/tui/components/meter"
	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

// stepGlyph là ký hiệu trạng thái lượt gần nhất của một bước.
func stepGlyph(s app.TaskStepCost) (string, theme.Role) {
	switch s.LastStatus {
	case "completed":
		return "✓", theme.OK
	case "failed":
		return "✗", theme.Err
	case "interrupted":
		return "~", theme.Warn
	case "running":
		return "▶", theme.Accent
	}
	return "○", theme.Muted
}

// stepCostLines là bảng chi phí theo bước: thanh tỉ lệ theo bước tốn nhất, chi phí (? khi không biết), số lượt, token;
// dòng cuối là tổng (≥ khi chỉ là tổng một phần).
func (m *Model) stepCostLines(env ui.Env, r app.TaskRunsResult, w int) []string {
	th := env.Th
	if len(r.Steps) == 0 {
		return nil
	}
	l := []string{m.section(env, m.t("tk.section.step_costs"), w)}
	nameW, maxCost := 0, 0.0
	var inTot, outTot int64
	for _, s := range r.Steps {
		nameW = max(nameW, text.Width(s.Step))
		if s.CostUSD != nil {
			maxCost = max(maxCost, *s.CostUSD)
		}
		inTot += s.InputTokens
		outTot += s.OutputTokens
	}
	nameW = min(nameW, 14)
	const moneyW, countW = 9, 4
	tokW := text.Width(m.tokens(inTot, outTot))
	fixed := 2 + nameW + 1 + moneyW + 1 + countW // ký hiệu + tên + tiền + số lượt
	barW := min(12, w-fixed-2)
	showTok := barW >= 12 && w >= fixed+2+12+1+tokW
	barPart := func(v *float64) string {
		if barW < 4 {
			return ""
		}
		ratio := 0.0
		if v != nil && maxCost > 0 {
			ratio = *v / maxCost
		}
		return " " + meter.Bar(th, ratio, barW, theme.Accent)
	}
	for _, s := range r.Steps {
		g, role := stepGlyph(s)
		money, count, tok := "-", "", ""
		if s.Count > 0 {
			money = meter.Money(s.CostUSD, s.CostMissing)
			count = fmt.Sprintf("×%d", s.Count)
			if showTok {
				tok = " " + th.S(theme.Muted, m.tokens(s.InputTokens, s.OutputTokens))
			}
		}
		mrole := theme.Fg
		if s.Count == 0 {
			mrole = theme.Muted
		} else if s.CostUSD == nil || s.CostMissing {
			mrole = theme.Warn
		}
		line := th.S(role, g) + " " + text.Fit(s.Step, nameW) + barPart(s.CostUSD) + " " +
			th.S(mrole, text.Pad(money, moneyW)) + " " + th.S(theme.Muted, text.Pad(count, countW)) + tok
		l = append(l, text.Truncate(line, w))
	}
	total := meter.Money(r.TotalUSD, r.TotalPartial)
	line := th.Bold(text.Pad(m.t("tk.runs.total"), 2+nameW))
	if barW >= 4 {
		line += strings.Repeat(" ", barW+1)
	}
	line += " " + th.Bold(text.Pad(total, moneyW))
	if showTok {
		line += " " + th.S(theme.Muted, text.Pad("", countW)) + " " + th.S(theme.Muted, m.tokens(inTot, outTot))
	}
	l = append(l, text.Truncate(line, w))
	if r.TotalPartial || r.TotalUSD == nil {
		l = append(l, th.S(theme.Muted, text.Truncate(m.t("tk.runs.unknown_hint"), w)))
	}
	return l
}

// runListLines là danh sách lượt chạy nhóm theo lượt điều phối: lượt con thụt vào dưới lượt cha, lượt mồ côi (cha không
// thuộc tác vụ) ở cuối.
func (m *Model) runListLines(env ui.Env, runs []app.TaskRunRow, w int) []string {
	th := env.Th
	ids := map[string]bool{}
	for _, x := range runs {
		ids[x.RunID] = true
	}
	children := map[string][]app.TaskRunRow{}
	var roots, orphans []app.TaskRunRow
	for _, x := range runs {
		switch {
		case x.Parent == "":
			roots = append(roots, x)
		case ids[x.Parent]:
			children[x.Parent] = append(children[x.Parent], x)
		default:
			orphans = append(orphans, x)
		}
	}
	var l []string
	emit := func(x app.TaskRunRow, indent string) {
		role := theme.Fg
		switch x.Status {
		case "completed":
			role = theme.OK
		case "failed":
			role = theme.Err
		case "interrupted":
			role = theme.Warn
		}
		wf := x.Workflow
		if wf == "" {
			wf = "-"
		}
		cost := meter.Money(x.CostUSD, false)
		l = append(l, text.Truncate(indent+th.S(theme.Muted, short(x.RunID))+" "+th.S(role, text.Pad(x.Status, 11))+" "+text.Pad(wf, 9)+" "+text.Pad(x.Duration, 7)+" "+cost, w))
		if x.Reason != "" {
			l = append(l, text.Truncate(th.S(theme.Muted, indent+"    "+m.cli(x.Reason)), w))
		}
	}
	for _, x := range roots {
		emit(x, "")
		for _, c := range children[x.RunID] {
			emit(c, th.S(theme.Muted, "  └ "))
		}
	}
	for _, x := range orphans {
		emit(x, th.S(theme.Muted, "└ "))
	}
	return l
}

// severityBadges là dòng badge đếm phát hiện theo mức độ (chỉ mức có số lớn hơn 0), cắt gọn theo độ rộng w.
func (m *Model) severityBadges(env ui.Env, fs []task.Finding, w int) string {
	th := env.Th
	count := map[string]int{}
	for _, f := range fs {
		count[f.Severity]++
	}
	var parts []string
	width := 0
	for _, sev := range task.FindingSeverities {
		n := count[sev]
		if n == 0 {
			continue
		}
		role := theme.Muted
		switch sev {
		case "critical", "high":
			role = theme.Err
		case "medium":
			role = theme.Warn
		}
		label := fmt.Sprintf("%s %d", m.sevName(sev), n)
		if width+meter.BadgeWidth(label)+1 > w && len(parts) > 0 {
			break
		}
		width += meter.BadgeWidth(label) + 1
		parts = append(parts, meter.Badge(th, role, label))
	}
	return text.Truncate(strings.Join(parts, " "), w)
}
