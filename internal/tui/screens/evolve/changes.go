package evolve

import (
	"fmt"
	"sort"
	"strings"

	"bint/internal/app/evolvesvc"
	"bint/internal/evolve"
	"bint/internal/hcomp"
	"bint/internal/i18n"
	"bint/internal/knowledge"
	"bint/internal/skills"
	"bint/internal/tui/components/markdown"
	"bint/internal/tui/components/text"
	"bint/internal/tui/components/viewer"
	"bint/internal/tui/screens/shared"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

// kv dựng dòng "nhãn  giá trị" có ngắt dòng treo.
func (m *Model) kv(th theme.Theme, w int, key, val string) []string {
	return shared.KV(th, 14, w, m.t("ev.f."+key), val)
}

// infoLines là thẻ con Thông tin: trạng thái, hành động, rủi ro, căn cứ, phiên bản gốc, nhãn đã đổi, cặp, áp dụng.
func (m *Model) infoLines(env ui.Env, d *evolvesvc.ShowResult, w int) []string {
	th := env.Th
	p := d.Proposal
	var out []string
	out = append(out, m.kv(th, w, "id", p.ID+" ("+d.State+")")...)
	if p.StatusReason != "" {
		out = append(out, m.kv(th, w, "status_reason", p.StatusReason)...)
	}
	out = append(out, m.kv(th, w, "action", strings.TrimSpace(p.Action+" "+strings.Join(p.TargetIDs, ", "))+" — "+m.t("ev.level", i18n.A{"level": p.Level, "target": p.TargetType}))...)
	out = append(out, m.kv(th, w, "opportunity", p.Opportunity)...)
	out = append(out, m.kv(th, w, "reason", p.Reason)...)
	if p.ExpectedBenefit != "" {
		out = append(out, m.kv(th, w, "benefit", p.ExpectedBenefit)...)
	}
	risk := p.Risk
	if len(p.RiskReasons) > 0 {
		risk += " " + strings.Join(p.RiskReasons, "; ")
	}
	role := theme.Fg
	if p.Risk == evolve.RiskHigh {
		role = theme.Err
	}
	out = append(out, m.kv(th, w, "risk", th.S(role, risk))...)
	by := []string{p.Generator}
	if p.Model != "" {
		by = append(by, p.Model)
	}
	made := strings.Join(by, " ") + ", " + shared.Time(p.CreatedAt)
	if p.CostUSD != nil {
		made += fmt.Sprintf(", $%.4f", *p.CostUSD)
	}
	out = append(out, m.kv(th, w, "made_by", made)...)
	out = append(out, m.kv(th, w, "plan", fmt.Sprintf("%s · %s — %s", p.Plan.Kind, p.Plan.Metric, p.Plan.Hypothesis))...)
	if !p.Quantitative {
		out = append(out, m.kv(th, w, "verify", th.S(theme.Warn, m.t("ev.not_quantitative")))...)
	}
	for _, n := range p.Notes {
		out = append(out, m.kv(th, w, "note", n)...)
	}
	if p.Pair != nil {
		area := ""
		if p.Pair.Area != "" {
			area = ", " + p.Pair.Area
		}
		out = append(out, m.kv(th, w, "pair", m.t("ev.pair."+p.Pair.Variant, i18n.A{"sibling": p.Pair.Sibling, "area": area}))...)
	}
	if len(p.Base) > 0 {
		out = append(out, "", th.S(theme.Accent, m.t("ev.sec.base")))
		for _, b := range p.Base {
			out = append(out, text.Truncate(fmt.Sprintf("  %s v%d %s sha256:%s", b.ID, b.Version, b.Status, b.SHA[:min(12, len(b.SHA))]), w))
		}
	}
	for _, b := range d.BaseProblems {
		out = append(out, text.WrapHang(th.S(theme.Err, "  ✗ ")+m.t("ev.base_problem", i18n.A{"id": b.ID, "detail": b.Detail}), w, "", "    ")...)
	}
	out = append(out, "", th.S(theme.Accent, m.t("ev.sec.support", i18n.A{"count": len(p.SupportingRuns)})))
	var runs []string
	for i, r := range p.SupportingRuns {
		if i == 12 {
			runs = append(runs, "…")
			break
		}
		runs = append(runs, shared.Short(r.RunID)+"("+r.Label+")")
	}
	if len(runs) > 0 {
		out = append(out, text.WrapHang("  "+strings.Join(runs, " "), w, "", "  ")...)
	}
	for _, dr := range d.LabelDrift {
		out = append(out, text.WrapHang(th.S(theme.Warn, "  ! ")+m.t("ev.label_drift", i18n.A{"drift": dr}), w, "", "    ")...)
	}
	if len(d.Adoptions) > 0 {
		out = append(out, "", th.S(theme.Accent, m.t("ev.sec.adoptions")))
		for _, a := range d.Adoptions {
			out = append(out, text.WrapHang(fmt.Sprintf("  %s  %s  %s  %s  %s", a.ID, a.Action, shared.Time(a.CreatedAt), a.Actor,
				m.t("ev.adoption.verified", i18n.A{"v": fmt.Sprint(a.Verified)})), w, "", "    ")...)
			if a.Reason != "" {
				out = append(out, text.Truncate("    "+a.Reason, w))
			}
		}
	}
	return out
}

// evalLines là thẻ con Đối chiếu: các lần đối chiếu và kết luận.
func (m *Model) evalLines(env ui.Env, d *evolvesvc.ShowResult, w int) []string {
	th := env.Th
	p := d.Proposal
	if len(d.Evaluations) == 0 {
		return []string{"", " " + th.S(theme.Muted, m.t("ev.evals.none"))}
	}
	var out []string
	for _, e := range d.Evaluations {
		ind := ""
		if e.Independent != nil {
			ind = m.t("ev.evals.independent." + fmt.Sprint(*e.Independent))
		}
		stale := ""
		if e.BaseKey != p.BaseKey() {
			stale = " " + m.t("ev.evals.stale")
		}
		kind := ""
		if k := evolve.EvidenceKind(p, e.CaseIDs, d.Cases); k != "" {
			kind = ", " + evolve.KindLabel(k)
		}
		if e.Kind == evolve.EvalRegression {
			kind += ", " + m.t("ev.evals.regression")
		}
		role := theme.Fg
		switch e.Verdict {
		case "supported":
			role = theme.OK
		case "not_supported":
			role = theme.Err
		}
		out = append(out, text.WrapHang(fmt.Sprintf("%s  %s  %s, %s%s, %s → %s%s%s", e.ID, shared.Short(e.BatchID), shared.Dash(e.Split),
			m.t("ev.evals.cases", i18n.A{"count": len(e.CaseIDs)}), kind, e.Delivery, th.S(role, shared.Dash(e.Verdict)), ind, stale), w, "", "  ")...)
		if e.Detail != "" {
			out = append(out, text.WrapHang(th.S(theme.Muted, e.Detail), w, "    ", "    ")...)
		}
	}
	return out
}

// diffLines dựng bản thay đổi dạng unified giữa hiện trạng và đề xuất, tô màu; rỗng nếu giống nhau.
func (m *Model) diffLines(th theme.Theme, cur, proposed string) []string {
	d := skills.Diff(m.t("ev.diff.current"), m.t("ev.diff.proposed"), cur, proposed)
	if d == "" {
		return nil
	}
	return viewer.ColorDiff(th, d)
}

// rollbackDiff là diff giữa nội dung hiện tại của một tệp xung đột (phần sẽ mất) và nội dung sau quay lui, thụt lề,
// tối đa limit dòng (hộp xác nhận cuộn được); rỗng nếu giống nhau.
func (m *Model) rollbackDiff(th theme.Theme, f evolvesvc.RollbackFile, limit int) []string {
	opt := func(s *string) string {
		if s == nil {
			return ""
		}
		return *s
	}
	d := skills.Diff(m.t("ev.rollback.diff.current"), m.t("ev.rollback.diff.after"), opt(f.Current), opt(f.Before))
	if d == "" {
		return nil
	}
	lines := viewer.ColorDiff(th, d)
	if n := len(lines) - limit; n > 0 {
		lines = append(lines[:limit:limit], th.S(theme.Muted, m.t("ev.rollback.diff.more", i18n.A{"count": n})))
	}
	for i, l := range lines {
		lines[i] = "      " + l
	}
	return lines
}

// changesLines là thẻ con Thay đổi: diff nội dung với hiện trạng cho tri thức và skill, khung riêng cho thành phần quy trình.
func (m *Model) changesLines(env ui.Env, d *evolvesvc.ShowResult, w int) []string {
	p := d.Proposal
	switch {
	case p.IsHarness():
		return m.harnessLines(env, d, w)
	case p.IsSkill():
		return m.skillLines(env, d, w)
	}
	return m.knowledgeLines(env, d, w)
}

func joinEvidence(es []knowledge.Evidence) string {
	var out []string
	for _, e := range es {
		out = append(out, e.String())
	}
	return strings.Join(out, ", ")
}

func (m *Model) knowledgeLines(env ui.Env, d *evolvesvc.ShowResult, w int) []string {
	th := env.Th
	byID := map[string]*knowledge.Item{}
	for _, it := range d.Items {
		byID[it.ID] = it
	}
	var out []string
	for i, ch := range d.Proposal.Changes {
		if i > 0 {
			out = append(out, "")
		}
		head := fmt.Sprintf("[%s] %s", ch.Op, ch.ItemID)
		if ch.Reason != "" {
			head += " — " + ch.Reason
		}
		out = append(out, text.WrapHang(th.Bold(head), w, "", "  ")...)
		cur := byID[ch.ItemID]
		dr := ch.Draft
		if dr == nil {
			if cur != nil {
				out = append(out, m.kv(th, w, "title", cur.Title)...)
				out = append(out, m.kv(th, w, "scope", strings.Join(cur.Scope.Paths, ", "))...)
			}
			continue
		}
		field := func(name, old, val string) {
			switch {
			case cur == nil:
				out = append(out, th.S(theme.OK, "+ ")+text.Truncate(text.Pad(m.t("ev.f."+name), 11)+val, max(w-2, 8)))
			case old != val:
				out = append(out, th.S(theme.Err, "- ")+text.Truncate(text.Pad(m.t("ev.f."+name), 11)+old, max(w-2, 8)),
					th.S(theme.OK, "+ ")+text.Truncate(text.Pad(m.t("ev.f."+name), 11)+val, max(w-2, 8)))
			default:
				out = append(out, "  "+text.Truncate(text.Pad(m.t("ev.f."+name), 11)+val, max(w-2, 8)))
			}
		}
		var oldType, oldTitle, oldPaths, oldEv, oldBody string
		if cur != nil {
			oldType, oldTitle, oldPaths, oldEv, oldBody = cur.Type, cur.Title, strings.Join(cur.Scope.Paths, ", "), joinEvidence(cur.Evidence), cur.Body
		}
		field("type", oldType, dr.Type)
		field("title", oldTitle, dr.Title)
		field("scope", oldPaths, strings.Join(dr.Scope.Paths, ", "))
		field("evidence", oldEv, strings.Join(dr.Evidence, ", "))
		out = append(out, "", th.S(theme.Accent, m.t("ev.sec.body")))
		if df := m.diffLines(th, oldBody, dr.Body); len(df) > 0 {
			out = append(out, df...)
		} else {
			out = append(out, th.S(theme.Muted, "  "+m.t("ev.diff.same")))
		}
	}
	if len(out) == 0 {
		return []string{"", " " + th.S(theme.Muted, m.t("ev.changes.none"))}
	}
	return out
}

func (m *Model) skillLines(env ui.Env, d *evolvesvc.ShowResult, w int) []string {
	th := env.Th
	p := d.Proposal
	byName := map[string]*skills.Evolved{}
	for _, e := range d.Evolved {
		byName[e.Meta.Name] = e
	}
	var out []string
	for i, c := range p.SkillChanges {
		if i > 0 {
			out = append(out, "")
		}
		head := fmt.Sprintf("[%s] skill %s", c.Op, c.Name)
		if c.MergedInto != "" {
			head += " → " + c.MergedInto
		}
		if c.Reason != "" {
			head += " — " + c.Reason
		}
		out = append(out, text.WrapHang(th.Bold(head), w, "", "  ")...)
		dr := c.Draft
		if dr == nil {
			continue
		}
		out = append(out, m.kv(th, w, "description", dr.Description)...)
		out = append(out, m.kv(th, w, "triggers", fmt.Sprintf("paths=%s keywords=%s", strings.Join(dr.Triggers.Paths, ","), strings.Join(dr.Triggers.Keywords, ",")))...)
		out = append(out, m.kv(th, w, "evidence", strings.Join(dr.Evidence, ", "))...)
		out = append(out, "", th.S(theme.Accent, "SKILL.md"))
		old := ""
		if cur := byName[c.Name]; cur != nil && p.Status != evolve.StatusApplied {
			old = string(cur.Content)
		}
		if df := m.diffLines(th, old, string(dr.Content())); len(df) > 0 {
			out = append(out, df...)
		} else {
			out = append(out, th.S(theme.Muted, "  "+m.t("ev.diff.same")))
		}
	}
	if len(out) == 0 {
		return []string{"", " " + th.S(theme.Muted, m.t("ev.changes.none"))}
	}
	return out
}

// fileText ghép nội dung các tệp của thành phần thành một văn bản để so sánh (tiêu đề tệp khi có nhiều tệp).
func fileText(c hcomp.Component) string {
	files, err := hcomp.Files(c)
	if err != nil {
		b, merr := hcomp.Marshal(c)
		if merr != nil {
			return ""
		}
		return string(b)
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var sb strings.Builder
	for _, n := range names {
		if len(names) > 1 {
			sb.WriteString("# " + n + "\n")
		}
		sb.Write(files[n])
		if !strings.HasSuffix(string(files[n]), "\n") {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func (m *Model) harnessLines(env ui.Env, d *evolvesvc.ShowResult, w int) []string {
	th := env.Th
	var out []string
	for i, ch := range d.Proposal.HarnessChanges {
		if i > 0 {
			out = append(out, "")
		}
		head := fmt.Sprintf("[%s] %s %s", ch.Op, ch.Kind, ch.ID)
		if ch.Reason != "" {
			head += " — " + ch.Reason
		}
		out = append(out, text.WrapHang(th.Bold(head), w, "", "  ")...)
		dr := ch.Draft
		if dr == nil {
			continue
		}
		c := dr.Component()
		out = append(out, m.kv(th, w, "scope", fmt.Sprintf("%s · %s", dr.Scope, m.t("ev.risk", i18n.A{"risk": hcomp.Risk(c)})))...)
		switch ch.Kind {
		case hcomp.KindGuardrail:
			out = append(out, m.kv(th, w, "rule", fmt.Sprintf("on=%s when=%s unless=%s action=%s", dr.On, m.fmtWhen(dr.When), m.fmtUnless(dr.Unless), dr.Action))...)
			out = append(out, m.kv(th, w, "message", dr.Message)...)
		case hcomp.KindGuidance:
			if dr.Title != "" {
				out = append(out, m.kv(th, w, "title", dr.Title)...)
			}
			out = append(out, "", th.S(theme.Accent, m.t("ev.sec.body")))
			out = append(out, markdown.Render(th, dr.Body, max(w-2, 10))...)
		case hcomp.KindScript:
			out = append(out, m.kv(th, w, "description", dr.Description)...)
			out = append(out, m.kv(th, w, "usage", dr.Usage)...)
			out = append(out, "", th.S(theme.Warn, "⚠ "+m.t("ev.script.warn")), th.S(theme.Accent, m.t("ev.sec.script")))
			for _, l := range strings.Split(strings.TrimRight(dr.Script, "\n"), "\n") {
				out = append(out, text.Truncate("  "+l, w))
			}
		case hcomp.KindStrategy:
			out = append(out, "", th.S(theme.Accent, "strategy.yml"))
			if b, err := hcomp.Marshal(c); err == nil {
				for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
					out = append(out, text.Truncate("  "+l, w))
				}
			}
		}
		// Diff với thành phần hiện có cùng loại và ID (nếu có).
		var cur *hcomp.Component
		for j := range d.Harness {
			if d.Harness[j].Kind == ch.Kind && d.Harness[j].ID == ch.ID {
				cur = &d.Harness[j]
			}
		}
		out = append(out, "", th.S(theme.Accent, m.t("ev.sec.diff")))
		switch {
		case cur == nil:
			out = append(out, th.S(theme.Muted, "  "+m.t("ev.harness.new")))
		default:
			if df := m.diffLines(th, fileText(*cur), fileText(c)); len(df) > 0 {
				out = append(out, df...)
			} else {
				out = append(out, th.S(theme.Muted, "  "+m.t("ev.diff.same")))
			}
		}
	}
	if len(out) == 0 {
		return []string{"", " " + th.S(theme.Muted, m.t("ev.changes.none"))}
	}
	return out
}

func (m *Model) fmtWhen(w hcomp.When) string {
	var parts []string
	if w.Edited != "" {
		parts = append(parts, "edited:"+w.Edited)
	}
	if w.Paths != "" {
		parts = append(parts, "paths:"+w.Paths)
	}
	if w.Command != "" {
		parts = append(parts, "command:"+w.Command)
	}
	if len(parts) == 0 {
		return m.t("ev.when.always")
	}
	return strings.Join(parts, ",")
}

func (m *Model) fmtUnless(u hcomp.Unless) string {
	if u.Ran == "" {
		return m.t("ev.unless.none")
	}
	return "ran:" + u.Ran
}
