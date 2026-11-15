package tasks

import (
	"strings"

	"bint/internal/app"
	"bint/internal/flow"
	"bint/internal/i18n"
	"bint/internal/task"
	"bint/internal/tui/components/meter"
	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

// stepView là một bước trong đường ống của khối spotlight.
type stepView struct {
	name   string
	glyph  string
	role   theme.Role
	cost   string // rỗng: không hiện chi phí (chưa nạp lượt chạy hoặc bước chưa chạy)
	costOK bool
}

// pipelineSteps dựng các bước của đường ống: từ bảng bước của app.TaskRuns khi đã nạp, không thì từ flow nhúng (chưa có
// chi phí). Bước chưa chạy là ○, bước hiện tại của phiếu là ▶, Optional bị bỏ qua là –.
func pipelineSteps(t *task.Task, r app.TaskRunsResult, loaded bool) []stepView {
	steps := r.Steps
	var def flow.Def
	haveDef := false
	if d, ok := flow.GetBuiltin(t.State.Workflow); ok {
		def, haveDef = d, true
	}
	if !loaded && haveDef {
		steps = nil
		for _, s := range def.Steps {
			steps = append(steps, app.TaskStepCost{Step: s.ID, InFlow: true})
		}
	}
	optional := map[string]bool{}
	if haveDef {
		for _, s := range def.Steps {
			optional[s.ID] = s.Optional
		}
	}
	skipPlan := t.State.Classification != nil && !t.State.Classification.Plan
	out := make([]stepView, 0, len(steps))
	for _, s := range steps {
		v := stepView{name: s.Step, glyph: "○", role: theme.Muted}
		switch {
		case s.Count == 0 && optional[s.Step] && skipPlan:
			v.glyph = "–"
		case s.LastStatus == "completed":
			v.glyph, v.role = "✓", theme.OK
		case s.LastStatus == "failed":
			v.glyph, v.role = "✗", theme.Err
		case s.LastStatus == "interrupted":
			v.glyph, v.role = "~", theme.Warn
		case s.LastStatus == "running":
			v.glyph, v.role = "▶", theme.Accent
		}
		if s.Count == 0 && v.glyph == "○" && s.Step == t.State.FlowStep && !task.Terminal(t.State.Status) {
			v.glyph, v.role = "▶", theme.Accent
		}
		if loaded && s.Count > 0 {
			v.cost, v.costOK = meter.Money(s.CostUSD, s.CostMissing), true
		}
		out = append(out, v)
	}
	return out
}

// spotlightLines là khối nổi bật ở đầu thẻ Phiếu: trạng thái dạng badge, ID, tổng chi phí; mục tiêu đậm; đường ống
// các bước flow với ký hiệu trạng thái và chi phí từng bước (gói theo từng bước khi hẹp).
func (m *Model) spotlightLines(env ui.Env, t *task.Task, w int) []string {
	th := env.Th
	st := t.State
	_, role := statusGlyph(st.Status)
	r, loaded := m.runs[st.ID]
	if m.runsErr[st.ID] != nil {
		loaded = false
	}
	badge := meter.Badge(th, role, m.statusName(st.Status))
	head := badge + " " + th.S(theme.Muted, short(st.ID))
	if loaded && r.TotalUSD != nil || loaded && r.TotalPartial {
		cost := m.t("tk.spot.total") + " " + meter.Money(r.TotalUSD, r.TotalPartial)
		if pad := w - text.Width(ansiStrip(head)) - text.Width(cost) - 2; pad >= 0 {
			head += strings.Repeat(" ", pad+2) + th.S(theme.Key, cost)
		}
	}
	l := []string{text.Truncate(head, w)}
	if st.Reason != "" {
		l = append(l, th.S(theme.Muted, text.Truncate(m.cli(st.Reason), w)))
	}
	for _, s := range text.Wrap(t.Ticket.Goal, w) {
		l = append(l, th.Bold(s))
	}
	steps := pipelineSteps(t, r, loaded)
	if len(steps) == 0 {
		return l
	}
	l = append(l, "")
	var plain, styled []string
	for _, s := range steps {
		p, sy := s.glyph+" "+s.name, th.S(s.role, s.glyph)+" "+s.name
		if s.costOK {
			p += " " + s.cost
			sy += " " + th.S(theme.Muted, s.cost)
		}
		plain, styled = append(plain, p), append(styled, sy)
	}
	const sep = " → "
	if text.Width(strings.Join(plain, sep)) <= w {
		return append(l, strings.Join(styled, th.S(theme.Muted, sep)))
	}
	for i, s := range styled { // hẹp: mỗi bước một dòng
		l = append(l, text.Truncate(s, w))
		_ = i
	}
	return l
}

// tokens định dạng "vào/ra" gọn (vd "1.2k vào · 340 ra").
func (m *Model) tokens(in, out int64) string {
	if in == 0 && out == 0 {
		return "-"
	}
	return m.t("tk.runs.tokens", i18n.A{"in": compact(in), "out": compact(out)})
}

func compact(n int64) string {
	switch {
	case n >= 1_000_000:
		return trimZero(float64(n)/1e6) + "M"
	case n >= 1000:
		return trimZero(float64(n)/1e3) + "k"
	}
	return itoa(n)
}
