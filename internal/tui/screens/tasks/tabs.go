package tasks

import (
	"fmt"
	"os"
	"strings"
	"time"

	"bint/internal/i18n"
	"bint/internal/task"
	"bint/internal/tui/components/markdown"
	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

// tabLines dựng nội dung thẻ con hiện tại; sel là chỉ số dòng cần giữ trong vùng nhìn (-1: không có).
func (m *Model) tabLines(env ui.Env, t *task.Task, w int) ([]string, int) {
	d, have := m.cache[t.State.ID]
	derr := m.cacheErr[t.State.ID]
	switch m.sub {
	case subDocs:
		return m.docsLines(env, t, d, have, derr, w), -1
	case subCriteria:
		return m.criteriaLines(env, t, w), -1
	case subVerify:
		return m.verifyLines(env, t, w), -1
	case subReview:
		return m.reviewLines(env, t, d, have, derr, w)
	case subErrors:
		return m.errorLines(env, t, w), -1
	case subRuns:
		return m.runsLines(env, t, w), -1
	}
	return m.infoLines(env, t, d, have, derr, w), -1
}

func (m *Model) section(env ui.Env, label string, w int) string {
	s := "── " + label + " "
	return env.Th.S(theme.Accent, s+strings.Repeat("─", max(w+1-text.Width(s), 0)))
}

// kv dựng dòng "nhãn   giá trị" với thụt treo khi giá trị dài.
func (m *Model) kv(env ui.Env, key, value string, w int) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	pre := text.Pad(m.t("tk.meta."+key), 12) + " "
	var out []string
	for i, s := range text.WrapHang(value, w, pre, strings.Repeat(" ", text.Width(pre))) {
		if i == 0 {
			s = env.Th.S(theme.Accent, s[:len(pre)]) + s[len(pre):]
		}
		out = append(out, s)
	}
	return out
}

func fmtAt(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.Local().Format("2006-01-02 15:04")
}

// ---- Phiếu ----

func (m *Model) infoLines(env ui.Env, t *task.Task, d Detail, have bool, derr error, w int) []string {
	th := env.Th
	var l []string
	add := func(s ...string) { l = append(l, s...) }
	st, tk := t.State, t.Ticket
	glyph, role := statusGlyph(st.Status)
	head := th.S(role, glyph) + " " + m.statusName(st.Status)
	if st.Reason != "" {
		head += th.S(theme.Muted, " — "+m.cli(st.Reason))
	}
	add(th.S(theme.Muted, st.ID), head)
	for _, s := range text.Wrap(tk.Goal, w) {
		add(th.Bold(s))
	}
	add("")
	add(m.kv(env, "workflow", st.Workflow, w)...)
	if st.Base != "" {
		base := shortSHA(st.Base)
		if st.Branch != "" {
			base += " (" + st.Branch + ")"
		}
		add(m.kv(env, "base", base, w)...)
	}
	if c := st.Classification; c != nil {
		add(m.kv(env, "classify", c.Reason, w)...)
		if c.Plan {
			depth := c.EffectiveDepth()
			if c.DepthReason != "" {
				depth += " (" + c.DepthReason + ")"
			}
			add(m.kv(env, "depth", depth, w)...)
		}
	}
	if c := st.Classification; c != nil && (c.Isolation != "" || st.Origin != "") {
		iso := c.EffectiveIsolation()
		if c.IsolationReason != "" {
			iso += " (" + c.IsolationReason + ")"
		}
		if len(c.IsolationTasks) > 0 {
			iso += " · " + strings.Join(c.IsolationTasks, ", ")
		}
		add(m.kv(env, "isolation", iso, w)...)
	}
	if st.Origin != "" {
		wt := st.Worktree
		if st.Branch != "" {
			wt += " · " + st.Branch
		}
		add(m.kv(env, "worktree", wt, w)...)
		add(m.kv(env, "origin", st.Origin, w)...)
		switch l := st.Landed; {
		case l == nil && st.AwaitingLand:
			add(m.kv(env, "landed", m.t("tk.iso.landed.awaiting"), w)...)
		case l == nil:
			add(m.kv(env, "landed", m.t("tk.iso.landed.none"), w)...)
		default:
			add(m.kv(env, "landed", m.t("tk.iso.landed.yes", i18n.A{"count": len(l.Files), "at": fmtAt(l.At)}), w)...)
			if len(l.Conflicts) > 0 {
				add(m.kv(env, "landed", m.t("tk.iso.conflicts", i18n.A{"files": strings.Join(l.Conflicts, ", ")}), w)...)
			}
		}
	}
	add(m.kv(env, "autonomy", tk.Autonomy, w)...)
	if st.ResumeFrom != "" {
		add(m.kv(env, "resume", st.ResumeFrom+" ("+m.cli("bint run resume "+st.ID)+")", w)...)
	}
	if st.Runner != nil {
		add(m.kv(env, "runner", fmt.Sprintf("pid %d · %s", st.Runner.PID, st.Runner.RunID), w)...)
	}
	add(m.kv(env, "updated", fmtAt(st.UpdatedAt), w)...)
	add("", m.section(env, m.t("tk.section.scope"), w))
	scope := m.t("tk.scope.unset")
	if len(tk.Scope.Allow) > 0 {
		scope = strings.Join(tk.Scope.Allow, ", ")
	}
	add(m.kv(env, "allow", scope, w)...)
	add(m.kv(env, "deny", strings.Join(tk.Scope.Deny, ", "), w)...)
	add(m.kv(env, "refs", strings.Join(tk.References, ", "), w)...)
	add(m.kv(env, "constraints", strings.Join(tk.Constraints, "; "), w)...)
	if len(st.PendingPaths) > 0 {
		add("", m.section(env, m.t("tk.section.pending"), w))
		for _, p := range st.PendingPaths {
			add(th.S(theme.Warn, "  "+p))
		}
		add(th.S(theme.Muted, m.t("tk.pending.hint")))
	}
	if p := st.ProtectedApproval; p != nil {
		add(m.kv(env, "approved", p.By+": "+strings.Join(p.Paths, ", "), w)...)
	}
	add("", m.section(env, m.t("tk.section.assess"), w))
	switch {
	case derr != nil:
		add(ui.ErrBlock(th, ui.ErrText(m.rw, derr), w, "")...)
	case !have:
		add(th.S(theme.Muted, m.t("ui.loading")))
	default:
		a := d.Assessment
		risk := a.Risk
		if len(a.Protected) > 0 {
			risk += " · " + strings.Join(a.Protected, ", ")
		}
		add(m.kv(env, "risk", risk, w)...)
		for _, q := range a.Questions {
			for i, s := range text.WrapHang(q, w, th.S(theme.Accent, "? "), "  ") {
				_ = i
				add(s)
			}
		}
		for _, n := range a.Notes {
			for _, s := range text.WrapHang(m.cli(n), w, th.S(theme.Warn, "! "), "  ") {
				add(s)
			}
		}
		if len(d.Blockers) > 0 && !task.Terminal(st.Status) {
			add("", m.section(env, m.t("tk.section.blockers"), w))
			for _, b := range d.Blockers {
				for _, s := range text.WrapHang(m.cli(b), w, th.S(theme.Warn, "- "), "  ") {
					add(s)
				}
			}
		}
	}
	if tk.Request != "" {
		add("", m.section(env, m.t("tk.section.request"), w))
		for _, s := range text.Wrap(strings.TrimSpace(tk.Request), w) {
			add(s)
		}
	}
	if len(st.History) > 0 {
		add("", m.section(env, m.t("tk.section.history"), w))
		for i := len(st.History) - 1; i >= 0 && len(st.History)-i <= 8; i-- {
			h := st.History[i]
			from := h.From
			if from == "" {
				from = "∅"
			}
			add(th.S(theme.Muted, fmtAt(h.At)+"  ") + from + " → " + h.To + th.S(theme.Muted, " ("+h.By+") "+m.cli(h.Reason)))
		}
	}
	return l
}

func shortSHA(s string) string {
	if len(s) > 10 {
		return s[:10]
	}
	return s
}

// ---- Tài liệu ----

func (m *Model) docsLines(env ui.Env, t *task.Task, d Detail, have bool, derr error, w int) []string {
	th := env.Th
	var l []string
	add := func(s ...string) { l = append(l, s...) }
	// Thanh chọn tài liệu.
	var tabs []string
	for i, n := range docNames {
		mark := " "
		if _, ok := d.Docs[n]; ok {
			mark = "•"
		}
		if i == m.doc {
			tabs = append(tabs, th.Selected(" "+mark+n+" "))
		} else {
			tabs = append(tabs, th.S(theme.Muted, " "+mark+n+" "))
		}
	}
	add(strings.Join(tabs, ""))
	switch {
	case derr != nil:
		add(append([]string{""}, ui.ErrBlock(th, ui.ErrText(m.rw, derr), w, "")...)...)
		return l
	case !have:
		add("", th.S(theme.Muted, m.t("ui.loading")))
		return l
	}
	// Kết quả task lint.
	if d.Lint != nil {
		rep := *d.Lint
		if len(rep.Errors)+len(rep.Warnings)+len(rep.OpenQuestions) == 0 {
			add(th.S(theme.OK, "✓ ") + m.t("tk.lint.ok"))
		}
		for _, i := range rep.Errors {
			add(ui.ErrBlock(th, "["+i.Code+"] "+i.Artifact+": "+m.cli(i.Detail), w, "")...)
		}
		for _, i := range rep.Warnings {
			add(th.S(theme.Warn, "! ") + "[" + i.Code + "] " + i.Artifact + ": " + m.cli(i.Detail))
		}
		for _, i := range rep.OpenQuestions {
			add(th.S(theme.Accent, "? ") + "[" + i.Code + "] " + i.Artifact + ": " + m.cli(i.Detail))
		}
	}
	add("", m.section(env, docNames[m.doc]+".md", w))
	body, ok := d.Docs[docNames[m.doc]]
	if !ok {
		add(th.S(theme.Muted, m.t("tk.docs.missing")))
		return l
	}
	add(markdown.Render(th, body, w)...)
	return l
}

// ---- Tiêu chí ----

func (m *Model) criteriaLines(env ui.Env, t *task.Task, w int) []string {
	th := env.Th
	var l []string
	crit := t.Ticket.Verify.Criteria
	if len(crit) == 0 {
		return []string{"", " " + th.S(theme.Muted, m.t("tk.criteria.none"))}
	}
	met := 0
	for i := range crit {
		if cs := t.State.CriterionStatus(i + 1); cs != nil && cs.Met {
			met++
		}
	}
	l = append(l, th.S(theme.Muted, m.t("tk.criteria.summary", i18n.A{"met": met, "total": len(crit)})), "")
	for i, c := range crit {
		mark, role := "?", theme.Muted
		cs := t.State.CriterionStatus(i + 1)
		if cs != nil {
			mark, role = "✗", theme.Err
			if cs.Met {
				mark, role = "✓", theme.OK
			}
		}
		pre := th.S(role, mark) + fmt.Sprintf(" %d. ", i+1)
		l = append(l, text.WrapHang(c, w, pre, strings.Repeat(" ", 5))...)
		if cs != nil {
			ev := th.S(theme.Muted, "      "+m.t("tk.criteria.evidence")+": ") + cs.Evidence
			l = append(l, text.WrapHang(cs.Evidence, w, "      "+th.S(theme.Muted, m.t("tk.criteria.evidence")+": "), "        ")...)
			_ = ev
			l = append(l, th.S(theme.Muted, "      "+cs.By+" · "+fmtAt(cs.At)))
		}
	}
	return l
}

// ---- Kiểm chứng ----

func (m *Model) verifyLines(env ui.Env, t *task.Task, w int) []string {
	th := env.Th
	var l []string
	add := func(s ...string) { l = append(l, s...) }
	cmd := strings.TrimSpace(t.Ticket.Verify.Command)
	if cmd == "" {
		return []string{"", " " + th.S(theme.Muted, m.t("tk.verify.no_command"))}
	}
	confirmed := t.State.VerifyApproval != nil && t.State.VerifyApproval.Command == cmd
	add(m.kv(env, "command", cmd, w)...)
	if confirmed {
		a := t.State.VerifyApproval
		add(m.kv(env, "confirmed", th.S(theme.OK, "✓ ")+a.By+" · "+fmtAt(a.At), w)...)
	} else {
		add(m.kv(env, "confirmed", th.S(theme.Warn, "✗ "+m.t("tk.verify.not_confirmed")), w)...)
		add(th.S(theme.Muted, m.t("tk.verify.approve_hint")))
	}
	if len(t.State.Verifications) == 0 {
		add("", th.S(theme.Muted, m.t("tk.verify.none")))
		return l
	}
	add("", m.section(env, m.t("tk.section.runs"), w))
	for i := len(t.State.Verifications) - 1; i >= 0; i-- {
		v := t.State.Verifications[i]
		icon, role := "✓", theme.OK
		res := m.t("tk.pass")
		if !v.Passed {
			icon, role, res = "✗", theme.Err, m.t("tk.fail")
		}
		line := th.S(role, icon) + " " + res + th.S(theme.Muted, fmt.Sprintf(" · %s %d · %s", m.t("tk.verify.exit"), v.ExitCode, fmtAt(v.At)))
		if v.TimedOut {
			line += th.S(theme.Warn, " · "+m.t("tk.verify.timeout"))
		}
		if !v.Confirmed {
			line += th.S(theme.Muted, " · "+m.t("tk.verify.unconfirmed"))
		}
		add(line)
		if v.Log != "" {
			add(th.S(theme.Muted, "  "+text.Path(v.Log, max(w-4, 10))))
		}
	}
	// Đuôi log của lần chạy gần nhất.
	if last := t.State.LastVerification(); last != nil && last.Log != "" {
		if b, err := os.ReadFile(last.Log); err == nil {
			ls := strings.Split(strings.TrimRight(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n"), "\n")
			if len(ls) > 20 {
				ls = ls[len(ls)-20:]
			}
			add("", m.section(env, m.t("tk.section.verify_log"), w))
			for _, x := range ls {
				add(th.S(theme.Muted, "│ ") + strings.ReplaceAll(x, "\t", "    "))
			}
		}
	}
	return l
}

// ---- Review ----

func (m *Model) reviewLines(env ui.Env, t *task.Task, d Detail, have bool, derr error, w int) ([]string, int) {
	th := env.Th
	var l []string
	add := func(s ...string) { l = append(l, s...) }
	switch {
	case derr != nil:
		return append([]string{""}, ui.ErrBlock(th, ui.ErrText(m.rw, derr), w, "")...), -1
	case !have:
		return []string{"", " " + th.S(theme.Muted, m.t("ui.loading"))}, -1
	case d.Review == nil || d.Review.Last() == nil:
		return []string{"", " " + th.S(theme.Muted, m.t("tk.review.none"))}, -1
	}
	rev := d.Review
	last := rev.Last()
	add(th.S(theme.Muted, m.t("tk.review.round", i18n.A{"round": last.Round, "total": len(rev.Rounds), "at": fmtAt(last.At), "by": last.By})))
	for _, s := range text.Wrap(last.Summary, w) {
		add(th.Bold(s))
	}
	fs := sortedFindings(rev)
	m.find = max(0, min(m.find, max(len(fs)-1, 0)))
	add("", m.section(env, m.cat.N("tk.review.findings", len(fs)), w))
	sel := -1
	if len(fs) == 0 {
		add(th.S(theme.OK, "✓ ") + m.t("tk.review.no_findings"))
	}
	for i, f := range fs {
		loc := f.Path
		if f.Lines != "" {
			loc += ":" + f.Lines
		}
		sevRole := theme.Muted
		switch f.Severity {
		case "critical":
			sevRole = theme.Err
		case "high":
			sevRole = theme.Err
		case "medium":
			sevRole = theme.Warn
		}
		sev := text.Pad("["+m.sevName(f.Severity)+"]", 11)
		line := th.S(sevRole, sev) + " " + f.Title
		if loc != "" {
			line += th.S(theme.Muted, "  "+loc)
		}
		if i == m.find {
			sel = len(l)
			line = th.S(theme.Key, "▸ ") + th.Selected(stripColor(th, text.Fit(strings.TrimSpace(sev+" "+f.Title+"  "+loc), max(w-2, 4))))
		} else {
			line = "  " + line
		}
		add(line)
	}
	if len(fs) > 0 {
		f := fs[m.find]
		add("", m.section(env, f.ID, w))
		add(th.S(theme.Muted, m.kindName(f.Kind)+" · "+m.sevName(f.Severity)))
		for _, s := range text.Wrap(f.Title, w) {
			add(th.Bold(s))
		}
		if f.Path != "" {
			loc := f.Path
			if f.Lines != "" {
				loc += ":" + f.Lines
			}
			add(th.S(theme.Accent, loc))
		}
		if f.Evidence != "" {
			add(m.kv(env, "evidence", f.Evidence, w)...)
		}
		if f.Criterion > 0 {
			add(m.kv(env, "criterion", fmt.Sprintf("AC-%d", f.Criterion), w)...)
		}
		if f.Body != "" {
			add("")
			add(markdown.Render(th, f.Body, w)...)
		}
	}
	return l, sel
}

func (m *Model) sevName(s string) string {
	if k := "tk.sev." + s; m.cat.Has(k) {
		return m.t(k)
	}
	return s
}

func (m *Model) kindName(s string) string {
	if k := "tk.kind." + s; m.cat.Has(k) {
		return m.t(k)
	}
	return s
}

// ---- Lỗi ----

func (m *Model) errorLines(env ui.Env, t *task.Task, w int) []string {
	th := env.Th
	errs := t.State.Errors
	if len(errs) == 0 {
		return []string{"", " " + th.S(theme.Muted, m.t("tk.errors.none"))}
	}
	var l []string
	for i := len(errs) - 1; i >= 0; i-- {
		e := errs[i]
		head := fmtAt(e.At)
		if e.Step != "" {
			head += " · " + e.Step
		}
		l = append(l, th.S(theme.Accent, head+" · "+e.By))
		for _, s := range text.Wrap(e.Message, w-2) {
			l = append(l, "  "+s)
		}
		l = append(l, "")
	}
	return l
}

// ---- Lượt chạy ----

func (m *Model) runsLines(env ui.Env, t *task.Task, w int) []string {
	th := env.Th
	id := t.State.ID
	if err := m.runsErr[id]; err != nil {
		return append([]string{""}, ui.ErrBlock(th, ui.ErrText(m.rw, err), w, "")...)
	}
	r, ok := m.runs[id]
	if !ok {
		return []string{"", " " + th.S(theme.Muted, m.t("ui.loading"))}
	}
	if len(r.Runs) == 0 {
		return []string{"", " " + th.S(theme.Muted, m.t("tk.runs.none"))}
	}
	var l []string
	for _, x := range r.Runs {
		cost := "-"
		if x.CostUSD != nil {
			cost = fmt.Sprintf("%.4f", *x.CostUSD)
		}
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
		l = append(l, text.Truncate(th.S(theme.Muted, short(x.RunID))+" "+th.S(role, text.Pad(x.Status, 11))+" "+text.Pad(wf, 9)+" "+text.Pad(x.Duration, 7)+" "+cost, w))
		if x.Reason != "" {
			l = append(l, th.S(theme.Muted, "    "+m.cli(x.Reason)))
		}
	}
	if task.Paused(t.State.Status) {
		l = append(l, "", th.S(theme.Muted, m.t("tk.runs.resume", i18n.A{"cmd": m.cli("bint run resume " + id)})))
	}
	return l
}
