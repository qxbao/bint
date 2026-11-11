package evolve

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"bint/internal/app/evolvesvc"
	"bint/internal/evolve"
	"bint/internal/i18n"
	"bint/internal/tui/actions"
	"bint/internal/tui/components/confirm"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

// onResult xử lý kết quả hành động: hộp xác nhận dựng từ kế hoạch chỉ đọc (plan_apply, plan_rollback), kết quả áp
// dụng/bác bỏ/quay lui, kết quả quét và tóm tắt các việc nền.
func (m *Model) onResult(msg actions.ResultMsg) tea.Cmd {
	switch msg.ID {
	case "evolve.plan_apply":
		if msg.Err != nil {
			m.setToast(ui.ErrText(m.cat, msg.Err), true)
			return nil
		}
		if pl, ok := msg.Value.(*evolvesvc.ApplyPlan); ok && pl != nil {
			m.applyDialog(pl)
		}
	case "evolve.plan_rollback":
		if msg.Err != nil {
			m.setToast(ui.ErrText(m.cat, msg.Err), true)
			return nil
		}
		if pl, ok := msg.Value.(*evolvesvc.RollbackPlan); ok && pl != nil {
			m.rollbackDialog(pl)
		}
	case "evolve.apply":
		if msg.Err != nil {
			m.setToast(ui.ErrText(m.cat, msg.Err), true)
			return nil
		}
		out, ok := msg.Value.(actions.ApplyOutcome)
		if !ok || out.Result.Proposal == nil {
			return nil
		}
		m.openInfo(m.t("ev.applied.title", i18n.A{"id": out.Result.Proposal.ID}), m.applyResultLines(out))
		m.setToast(m.t("ev.applied.toast", i18n.A{"id": out.Result.Proposal.ID}), false)
		return m.refreshAfterChange(out.Result.Proposal.ID)
	case "evolve.rollback":
		if msg.Err != nil {
			m.setToast(ui.ErrText(m.cat, msg.Err), true)
			return nil
		}
		out, ok := msg.Value.(actions.RollbackOutcome)
		if !ok || out.Result.Proposal == nil {
			return nil
		}
		m.openInfo(m.t("ev.rolledback.title", i18n.A{"id": out.Result.Proposal.ID}), m.rollbackResultLines(out))
		m.setToast(m.t("ev.rolledback.toast", i18n.A{"id": out.Result.Proposal.ID}), false)
		return m.refreshAfterChange(out.Result.Proposal.ID)
	case "evolve.reject":
		if msg.Err != nil {
			m.setToast(ui.ErrText(m.cat, msg.Err), true)
			return nil
		}
		p, ok := msg.Value.(*evolve.Proposal)
		if !ok || p == nil {
			return nil
		}
		m.setToast(m.t("ev.rejected.toast", i18n.A{"id": p.ID}), false)
		return m.refreshAfterChange(p.ID)
	case "evolve.scan":
		m.frm, m.frmBusy = nil, false
		if msg.Err != nil {
			m.setToast(ui.ErrText(m.cat, msg.Err), true)
			return nil
		}
		out, ok := msg.Value.(actions.ScanOutcome)
		if !ok {
			return nil
		}
		res := out.Result
		m.Data.Scan, m.Data.ScanErr = &res, nil
		m.olst.SetN(len(res.Opportunities))
		m.tab, m.pane = tabOpps, paneList
		m.detail.Reset()
		key := "ev.scanned.toast"
		if out.NoGit {
			key = "ev.scanned.nogit.toast"
		}
		m.setToast(m.t(key, i18n.A{"count": len(res.Opportunities)}), false)
	case "evolve.propose", "evolve.evaluate", "evolve.process", "evolve.cycle":
		return m.onJobResult(msg)
	}
	return nil
}

// refreshAfterChange nạp lại dữ liệu sau khi một đề xuất đổi trạng thái và giữ con trỏ ở đề xuất đó.
func (m *Model) refreshAfterChange(id string) tea.Cmd {
	m.invalidate()
	m.focusID = id
	m.all = true // đề xuất vừa đổi không còn ở danh sách "đang chờ": hiện mọi trạng thái để vẫn thấy nó
	m.tab = tabProposals
	return tea.Batch(m.Load(), m.wantDetail())
}

// ---- hộp áp dụng ----

func (m *Model) applyDialog(pl *evolvesvc.ApplyPlan) {
	th := m.th
	p := pl.Proposal
	if pl.Blocked != "" {
		body := []string{m.t("ev.apply.blocked."+pl.Blocked, i18n.A{"detail": pl.BlockedDetail})}
		m.openInfo(m.t("ev.apply.title", i18n.A{"id": p.ID}), body)
		return
	}
	body := []string{
		m.t("ev.apply.action", i18n.A{"action": p.Action, "changes": strings.Join(pl.Changes, ", ")}),
		m.t("ev.apply.risk", i18n.A{"risk": pl.Risk, "reasons": strings.Join(pl.RiskReasons, "; ")}),
		"",
		th.S(theme.OK, "✓ ") + m.t("ev.apply.base_ok"),
	}
	c := confirm.Model{Title: m.t("ev.apply.title", i18n.A{"id": p.ID}), Input: true, Label: m.t("ev.reason"), Placeholder: m.t("ev.apply.reason.hint"),
		YesLabel: m.t("ev.apply.yes"), NoLabel: m.t("ev.dialog.cancel")}
	iUnv, iRisk := -1, -1
	if pl.Verified {
		body = append(body, th.S(theme.OK, "✓ ")+m.t("ev.apply.verified"))
	} else {
		body = append(body, th.S(theme.Err, "✗ ")+m.t("ev.apply.unverified"))
		iUnv = len(c.Checks)
		c.Checks = append(c.Checks, m.t("ev.apply.check.unverified"))
	}
	if pl.NeedsConfirmRisk {
		body = append(body, th.S(theme.Err, "✗ ")+m.t("ev.apply.risk_high"))
		iRisk = len(c.Checks)
		c.Checks = append(c.Checks, m.t("ev.apply.check.risk", i18n.A{"reasons": strings.Join(pl.RiskReasons, "; ")}))
	}
	body = append(body, "", th.S(theme.Muted, m.t("ev.apply.preview")))
	if pl.IsSkill {
		body = append(body, th.S(theme.Muted, m.t("ev.apply.skill_sync")))
	}
	if len(c.Checks) > 0 {
		c.Danger = true
		body = append([]string{th.S(theme.Err, m.t("ev.apply.warn"))}, append([]string{""}, body...)...)
	}
	c.Body = body
	id := p.ID
	m.dlg = &dialog{c: c, onYes: func(d *confirm.Model) tea.Cmd {
		// Cờ bỏ qua kiểm tra chỉ được đặt khi người chọn ô tương ứng trong hộp cảnh báo.
		return request("evolve.apply", evolvesvc.ApplyOptions{Ref: id, Unverified: d.Checked(iUnv), ConfirmRisk: d.Checked(iRisk), Reason: d.Value()})
	}}
}

func (m *Model) applyResultLines(out actions.ApplyOutcome) []string {
	r := out.Result
	lines := []string{m.t("ev.applied.line", i18n.A{"id": r.Proposal.ID, "action": r.Proposal.Action, "adoption": r.AdoptionID, "verified": fmt.Sprint(r.Verified)}), ""}
	for _, f := range r.Files {
		lines = append(lines, "  "+fileOp(f.Before, f.After)+" "+f.Path)
	}
	for _, n := range r.Notes {
		lines = append(lines, "", m.t("ev.note", i18n.A{"note": n}))
	}
	return append(lines, m.syncLines(out.Sync)...)
}

func (m *Model) rollbackResultLines(out actions.RollbackOutcome) []string {
	r := out.Result
	lines := []string{m.t("ev.rolledback.line", i18n.A{"id": r.Proposal.ID, "adoption": r.AdoptionID}), ""}
	for _, f := range r.Files {
		lines = append(lines, "  "+fileOp(f.Before, f.After)+" "+f.Path)
	}
	return append(lines, m.syncLines(out.Sync)...)
}

// syncLines mô tả kết quả đồng bộ skill sau khi đổi đề xuất skill.
func (m *Model) syncLines(s actions.SkillSync) []string {
	switch {
	case !s.Ran:
		return nil
	case s.Err != nil:
		return []string{"", m.th.S(theme.Warn, m.t("ev.sync.err", i18n.A{"error": s.Err.Error()}))}
	case s.Install.Conflicts > 0:
		return []string{"", m.th.S(theme.Warn, m.t("ev.sync.conflicts", i18n.A{"count": s.Install.Conflicts}))}
	}
	return []string{"", m.t("ev.sync.ok")}
}

func fileOp(before, after *string) string {
	switch {
	case before == nil:
		return "+"
	case after == nil:
		return "-"
	}
	return "~"
}

// ---- hộp quay lui ----

func (m *Model) rollbackDialog(pl *evolvesvc.RollbackPlan) {
	th := m.th
	p := pl.Proposal
	body := []string{m.t("ev.rollback.intro", i18n.A{"id": p.ID, "actor": pl.Adoption.Actor})}
	for _, f := range pl.Files {
		line := "  " + fileOp(f.Applied, f.Before) + " " + f.Path
		if f.Conflict {
			line = th.S(theme.Err, line+"  ✗ "+m.t("ev.rollback.conflict"))
		}
		body = append(body, line)
		if f.Conflict {
			// Người phải thấy chính nội dung sẽ bị ghi đè trước khi chọn --force.
			body = append(body, m.rollbackDiff(th, f, 20)...)
		}
	}
	c := confirm.Model{Title: m.t("ev.rollback.title", i18n.A{"id": p.ID}), Input: true, Label: m.t("ev.reason"), Placeholder: m.t("ev.rollback.reason.hint"),
		YesLabel: m.t("ev.rollback.yes"), NoLabel: m.t("ev.dialog.cancel")}
	conflicts := pl.SeenConflicts() // tệp xung đột + dấu vân tay nội dung người đang xem
	if len(conflicts) > 0 {
		c.Danger = true
		body = append([]string{th.S(theme.Err, m.t("ev.rollback.warn", i18n.A{"count": len(conflicts)})), ""}, body...)
		c.Checks = []string{m.t("ev.rollback.check.force", i18n.A{"count": len(conflicts)})}
	}
	if pl.IsSkill {
		body = append(body, "", th.S(theme.Muted, m.t("ev.apply.skill_sync")))
	}
	c.Body = body
	id := p.ID
	m.dlg = &dialog{c: c, onYes: func(d *confirm.Model) tea.Cmd {
		o := evolvesvc.RollbackOptions{Ref: id, Reason: d.Value()}
		if len(c.Checks) > 0 && d.Checked(0) {
			// --force chỉ khi người chọn, kèm tệp xung đột và nội dung đã xem: tệp đổi giữa chừng thì dịch vụ từ chối.
			o.Force, o.ExpectConflicts = true, conflicts
		}
		return request("evolve.rollback", o)
	}}
}

// ---- tóm tắt việc nền ----

func (m *Model) onJobResult(msg actions.ResultMsg) tea.Cmd {
	if errors.Is(msg.Err, actions.ErrEvolveBusy) {
		m.setToast(ui.ErrText(m.cat, msg.Err), true) // việc thứ hai bị từ chối: chỉ báo, việc đang chạy không đổi
		return nil
	}
	var lines []string
	title := m.t("ev.job.title." + strings.TrimPrefix(msg.ID, "evolve."))
	switch v := msg.Value.(type) {
	case *evolvesvc.ProposeResult:
		lines = m.proposeLines(v)
		for _, o := range v.Outcomes {
			if o.Proposal != nil { // có đề xuất mới: mở thẻ Đề xuất tại đề xuất đầu tiên
				m.focusID, m.tab, m.pane, m.sub = o.Proposal.ID, tabProposals, paneList, subInfo
				break
			}
		}
	case *evolvesvc.EvaluateResult:
		lines = m.evaluateLines(v)
	case *evolvesvc.ProcessResult:
		lines = m.processLines(v)
	case evolvesvc.CycleReport:
		lines = m.cycleLines(v)
	}
	if msg.Err != nil {
		lines = append(lines, "", m.th.S(theme.Err, "✗ ")+ui.ErrText(m.cat, msg.Err))
		m.setToast(ui.ErrText(m.cat, msg.Err), true)
	}
	if len(lines) > 0 {
		m.openInfo(title, lines)
	}
	m.invalidate()
	return tea.Batch(m.Load(), m.wantDetail())
}

func (m *Model) proposeLines(r *evolvesvc.ProposeResult) []string {
	if len(r.Outcomes) == 0 {
		return []string{m.t("ev.res.propose.none")}
	}
	var out []string
	for _, o := range r.Outcomes {
		if p := o.Proposal; p != nil {
			out = append(out, m.t("ev.res.propose.made", i18n.A{"id": p.ID, "action": p.Action, "changes": strings.Join(evolvesvc.ChangeNames(p), ","), "risk": p.Risk, "op": o.Opportunity}))
		} else {
			out = append(out, m.t("ev.res.propose.skipped", i18n.A{"op": o.Opportunity, "why": o.Skipped}))
		}
		for _, rj := range o.Rejected {
			out = append(out, "    ✗ "+rj)
		}
	}
	if r.SpentUSD > 0 {
		out = append(out, "", m.t("ev.res.spent", i18n.A{"usd": fmt.Sprintf("%.4f", r.SpentUSD)}))
	}
	return out
}

func (m *Model) evaluateLines(r *evolvesvc.EvaluateResult) []string {
	out := []string{
		m.t("ev.res.eval.batch", i18n.A{"batch": r.Run.BatchID, "status": r.Run.Status, "done": r.Run.Done, "total": r.Run.Total}),
		m.t("ev.res.eval.verdict", i18n.A{"kind": r.Proposal.Plan.Kind, "split": r.Record.Split, "verdict": r.Verdict.Verdict, "detail": r.Verdict.Detail}),
	}
	if r.Run.StopReason != "" {
		out = append(out, m.t("ev.res.eval.stop", i18n.A{"why": r.Run.StopReason}))
	}
	for _, n := range r.Verdict.Notes {
		out = append(out, m.t("ev.note", i18n.A{"note": n}))
	}
	return out
}

func (m *Model) processLines(r *evolvesvc.ProcessResult) []string {
	out := []string{m.t("ev.res.process", i18n.A{"added": len(r.Added), "processed": len(r.Report.Processed), "waiting": len(r.Report.Waiting),
		"usd": fmt.Sprintf("%.2f", r.Report.SpentUSD)})}
	if r.Report.Locked != "" {
		out = append(out, m.th.S(theme.Warn, m.t("ev.res.locked", i18n.A{"why": r.Report.Locked})))
	}
	for _, q := range r.Report.Processed {
		line := fmt.Sprintf("  %s  %s", q.ID, q.Status)
		if q.ProposalID != "" {
			line += " → " + q.ProposalID
		}
		out = append(out, line)
	}
	return out
}

func (m *Model) cycleLines(r evolvesvc.CycleReport) []string {
	out := []string{m.t("ev.res.cycle", i18n.A{"id": r.ID, "spent": fmt.Sprintf("%.2f", r.SpentUSD), "budget": fmt.Sprintf("%.2f", r.BudgetUSD),
		"gen": fmt.Sprintf("%.2f", r.GenerationUSD), "probe": fmt.Sprintf("%.2f", r.ProbeUSD)})}
	for _, s := range r.Steps {
		out = append(out, fmt.Sprintf("  %-11s %-24s %s", s.Step, s.Target, s.Outcome))
	}
	list := func(key string, ids []string) {
		if len(ids) > 0 {
			out = append(out, m.t(key, i18n.A{"ids": strings.Join(ids, ", ")}))
		}
	}
	list("ev.res.cycle.applied", r.AutoApplied)
	list("ev.res.cycle.ready", r.Ready)
	list("ev.res.cycle.rejected", r.AutoRejected)
	list("ev.res.cycle.rollback", r.RollbackSuggested)
	if r.StopReason != "" {
		out = append(out, m.t("ev.res.cycle.stop", i18n.A{"why": r.StopReason}))
	}
	return out
}
