package eval

import (
	"fmt"
	"math"
	"strings"
	"time"

	"bint/internal/eval"
	"bint/internal/i18n"
	"bint/internal/tui/components/barchart"
	"bint/internal/tui/components/text"
	"bint/internal/tui/screens/shared"
	"bint/internal/tui/theme"
)

// Định dạng số và khoảng tin cậy của báo cáo (viết lại ở đây, không import internal/cli).

func pct(v float64) string {
	if math.IsNaN(v) {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", 100*v)
}

func ciStr(c eval.CI, scale float64, format string) string {
	if !c.Valid() {
		return "[-]"
	}
	return fmt.Sprintf("["+format+", "+format+"]", c.Lo*scale, c.Hi*scale)
}

func statStr(s *eval.Stat, format string) string {
	if s == nil {
		return "-"
	}
	return fmt.Sprintf(format+" %s", s.Mean, ciStr(s.CI, 1, format))
}

// isoTime định dạng thời điểm RFC3339 của đợt theo giờ địa phương; rỗng → "-".
func isoTime(s string) string {
	if s == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.Local().Format("2006-01-02 15:04")
}

// reportLines dựng các dòng của báo cáo một đợt: thông tin đợt, biểu đồ cột tỷ lệ đạt + CI95 theo nhánh, bảng nhánh,
// so sánh theo cặp, bảng theo tác vụ, guardrail, chẩn đoán độ tin cậy và chẩn đoán cổng 2.
func (m *Model) reportLines(th theme.Theme, rep *eval.Report, w int) []string {
	var lines []string
	head := func(key string) { lines = append(lines, "", th.S(theme.Accent, m.t(key))) }
	b := rep.Batch
	lines = append(lines, th.Bold(m.t("eval.rep.batch", i18n.A{"id": b.BatchID, "status": m.tr("eval.bst.", b.Status)})))
	lines = append(lines, text.WrapHang(m.t("eval.rep.setup", i18n.A{"harness": b.Harness, "version": shared.Dash(b.HarnessVersion), "model": shared.Dash(b.RequestedModel),
		"leak": b.LeakageMode, "cases": len(b.CaseIDs), "repeats": b.Repeats, "seed": b.Seed}), w, "  ", "    ")...)
	lines = append(lines, text.WrapHang(m.t("eval.rep.when", i18n.A{"start": isoTime(b.StartedAt), "end": isoTime(b.FinishedAt), "git": shared.Dash(shortStr(b.GitHead, 10))}), w, "  ", "    ")...)
	if base := m.baselineOf(rep); base != "" {
		lines = append(lines, "  "+m.t("eval.rep.baseline", i18n.A{"arm": base}))
	}

	head("eval.rep.chart")
	rows := make([]barchart.Row, 0, len(rep.Arms))
	for _, a := range rep.Arms {
		rows = append(rows, barchart.Row{Label: a.Arm, Value: a.PassRate.Mean, Lo: a.PassRate.CI.Lo, Hi: a.PassRate.CI.Hi, Valid: a.PassRate.CI.Valid(),
			Note: fmt.Sprintf("%d/%d", a.Passed, a.Graded)})
	}
	for _, l := range barchart.Render(th, rows, max(w-2, 12)) {
		lines = append(lines, "  "+l)
	}
	lines = append(lines, text.WrapHang(th.S(theme.Muted, m.t("eval.rep.chart.legend")), w, "  ", "  ")...)

	head("eval.rep.arms")
	for _, a := range rep.Arms {
		lines = append(lines, text.WrapHang(m.t("eval.rep.arm", i18n.A{"arm": a.Arm, "rate": pct(a.PassRate.Mean), "ci": ciStr(a.PassRate.CI, 100, "%.0f"),
			"graded": a.Graded, "attempts": a.Attempts, "errors": a.Errors, "skipped": a.Skipped, "timeouts": a.Timeouts}), w, "  ", "      ")...)
		detail := m.t("eval.rep.arm.detail", i18n.A{"cost": statStr(a.Cost, "%.3f"), "time": statStr(a.Duration, "%.0f"), "turns": statStr(a.Turns, "%.1f")})
		if a.WithinCaseSD != nil {
			detail += "; " + m.t("eval.rep.arm.sd", i18n.A{"sd": fmt.Sprintf("%.2f", *a.WithinCaseSD), "mixed": a.MixedCases, "cases": a.CasesWithRepeats})
		}
		if len(a.Models) > 0 {
			detail += "; " + m.t("eval.rep.arm.models", i18n.A{"models": strings.Join(a.Models, ",")})
		}
		lines = append(lines, text.WrapHang(detail, w, "      ", "      ")...)
	}

	if len(rep.Paired) > 0 {
		head("eval.rep.paired")
		for _, p := range rep.Paired {
			lines = append(lines, text.WrapHang(m.t("eval.rep.pair", i18n.A{"arm": p.Arm, "base": p.Baseline, "n": p.PassDiff.N,
				"diff": fmt.Sprintf("%+.1f", 100*p.PassDiff.Mean), "ci": ciStr(p.PassDiff.CI, 100, "%+.1f")}), w, "  ", "      ")...)
			extra := m.t("eval.rep.pair.detail", i18n.A{"cost": statStr(p.CostDiff, "%+.3f"), "time": statStr(p.TimeDiff, "%+.0f"), "turns": statStr(p.TurnDiff, "%+.1f")})
			lines = append(lines, text.WrapHang(extra, w, "      ", "      ")...)
		}
	}

	head("eval.rep.cases")
	if len(rep.Cases) == 0 {
		lines = append(lines, "  "+th.S(theme.Muted, m.t("eval.rep.cases.none")))
	}
	for _, c := range rep.Cases {
		var cells []string
		for _, a := range rep.Arms {
			cell := c.Arms[a.Arm]
			s := fmt.Sprintf("%s %d/%d", a.Arm, cell.Passed, cell.Graded)
			if cell.Other > 0 {
				s += " " + m.t("eval.rep.cell.other", i18n.A{"count": cell.Other})
			}
			cells = append(cells, s)
		}
		lines = append(lines, text.WrapHang(fmt.Sprintf("%s  %s  %s", shared.Short(c.CaseID), text.Pad(shared.Dash(c.Split), 10), strings.Join(cells, "  ")), w, "  ", "      ")...)
	}

	if len(rep.Guardrails) > 0 {
		head("eval.rep.guardrails")
		for _, g := range rep.Guardrails {
			lines = append(lines, text.WrapHang(m.t("eval.rep.guardrail", i18n.A{"arm": g.Arm, "name": g.Guardrail, "attempts": g.Attempts, "eligible": g.Eligible,
				"fired": g.Fired, "count": g.FiredCount, "followed": g.Followed, "blocked": g.Blocked, "bypass": g.Bypass}), w, "  ", "      ")...)
		}
	}

	head("eval.rep.diag")
	if len(rep.Diagnostics) == 0 {
		lines = append(lines, "  "+th.S(theme.OK, "✓ ")+m.t("eval.rep.diag.none"))
	}
	for _, d := range rep.Diagnostics {
		icon := th.S(theme.Muted, "i")
		if d.Level == "warn" {
			icon = th.S(theme.Warn, "!")
		}
		lines = append(lines, text.WrapHang(icon+" "+m.cli(d.Detail), w, "  ", "    ")...)
	}

	if g := rep.Gate2; g != nil {
		head("eval.rep.gate2")
		for _, c := range g.Checks {
			lines = append(lines, text.WrapHang("· "+c, w, "  ", "    ")...)
		}
		lines = append(lines, text.WrapHang("→ "+g.Cause+": "+m.cli(g.Detail), w, "  ", "    ")...)
		for _, a := range rep.Arms {
			if a.Arm == eval.ArmIdeal {
				lines = append(lines, text.WrapHang(th.S(theme.Muted, m.t("eval.rep.gate2.ideal")), w, "  ", "    ")...)
				break
			}
		}
	}
	return lines
}

// baselineOf trả nhánh làm mốc của báo cáo: mốc đang chọn, không thì mốc mặc định của BuildReport (none, hoặc nhánh đầu).
func (m *Model) baselineOf(rep *eval.Report) string {
	if m.baseline != "" {
		return m.baseline
	}
	if len(rep.Paired) > 0 {
		return rep.Paired[0].Baseline
	}
	return ""
}

func shortStr(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
