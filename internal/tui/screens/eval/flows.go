package eval

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"bint/internal/app"
	"bint/internal/eval"
	"bint/internal/i18n"
	"bint/internal/tui/actions"
	"bint/internal/tui/components/confirm"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

// runState là tiến độ của việc nặng gần nhất: lưới ô (tác vụ × nhánh × lần lặp) của một đợt, các bước kiểm chứng
// hoặc dựng tri thức. Dữ liệu đến bằng thông điệp có kiểu từ việc nền (jobs.DataMsg), không phân tích chữ log.
type runState struct {
	plan  *app.EvalRunPlan
	cells map[string]string // "tác vụ|nhánh|lần" → trạng thái lượt
	done  int
	total int
	batch string
	// resume là lệnh CLI chạy tiếp đợt đã bị huỷ hoặc dừng; hiện ở đầu thẻ Log.
	resume string
	verify []actions.EvalVerifyStep
	boots  []app.EvalBootstrapStep
}

func cellKey(caseID, arm string, repeat int) string {
	return fmt.Sprintf("%s|%s|%d", caseID, arm, repeat)
}

// onData nhận thông điệp có kiểu của việc nền và cập nhật tiến độ.
func (m *Model) onData(v any) {
	if m.run == nil {
		m.run = &runState{}
	}
	r := m.run
	switch d := v.(type) {
	case app.EvalRunPlan:
		r.plan = &d
		r.cells = map[string]string{}
		for _, a := range d.Done {
			r.cells[cellKey(a.CaseID, a.Arm, a.Repeat)] = a.Status
		}
		r.done, r.total, r.batch = len(d.Done), len(d.CaseIDs)*len(d.Arms)*d.Repeats, d.BatchID
	case actions.EvalAttempt:
		if r.cells == nil {
			r.cells = map[string]string{}
		}
		r.cells[cellKey(d.Row.CaseID, d.Row.Arm, d.Row.Repeat)] = d.Row.Status
		r.done, r.total, r.batch = d.N, d.Total, d.Row.BatchID
	case actions.EvalVerifyStep:
		for i := range r.verify {
			if r.verify[i].Index == d.Index {
				r.verify[i] = d
				return
			}
		}
		r.verify = append(r.verify, d)
	case actions.EvalBootstrapProgress:
		r.boots = append(r.boots, d.Step)
	}
}

// onResult xử lý kết quả hành động: hộp kết quả duyệt/loại/ước tính/đặc tả, dọn kho đệm và tóm tắt việc nền.
func (m *Model) onResult(msg actions.ResultMsg) tea.Cmd {
	switch msg.ID {
	case "eval.approve":
		outs, _ := msg.Value.([]app.EvalApproveOutcome)
		if msg.Err != nil && len(outs) == 0 {
			m.setToast(ui.ErrText(m.rw, msg.Err), true)
			return nil
		}
		if lines := m.approveLines(outs, msg.Err); len(lines) > 0 {
			m.openInfo(m.t("eval.approve.result"), lines)
		}
		return m.refresh()
	case "eval.reject":
		done, _ := msg.Value.([]string)
		if msg.Err != nil && len(done) == 0 {
			m.setToast(ui.ErrText(m.rw, msg.Err), true)
			return nil
		}
		m.setToast(m.t("eval.rejected.toast", i18n.A{"count": len(done)}), msg.Err != nil)
		return m.refresh()
	case "eval.spec":
		m.frm, m.frmBusy = nil, false
		if msg.Err != nil {
			m.setToast(ui.ErrText(m.rw, msg.Err), true)
			return nil
		}
		if r, ok := msg.Value.(*app.EvalSpecResult); ok && r != nil {
			m.openInfo(m.t("eval.spec.result"), m.specLines(r))
		}
		return m.refresh()
	case "eval.estimate":
		m.frm, m.frmBusy = nil, false
		if msg.Err != nil {
			m.setToast(ui.ErrText(m.rw, msg.Err), true)
			return nil
		}
		if r, ok := msg.Value.(*app.EvalEstimateResult); ok && r != nil {
			m.openInfo(m.t("eval.estimate.title"), m.estimateLines(r))
		}
	case "eval.clean":
		if msg.Err != nil {
			m.setToast(ui.ErrText(m.rw, msg.Err), true)
			return nil
		}
		if r, ok := msg.Value.(app.EvalCleanResult); ok {
			m.setToast(m.t("eval.clean.toast", i18n.A{"count": r.Removed, "size": humanBytes(r.Freed)}), false)
		}
	case "eval.open_file":
		if msg.Err != nil {
			m.setToast(ui.ErrText(m.rw, msg.Err), true)
		}
		return m.refresh()
	case "eval.mine", "eval.verify", "eval.run", "eval.resume", "eval.bootstrap":
		return m.onJobResult(msg)
	}
	return nil
}

// refresh nạp lại dữ liệu và chi tiết sau khi dữ liệu đổi.
func (m *Model) refresh() tea.Cmd {
	m.invalidate()
	return tea.Batch(m.Load(), m.wantDetail())
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// ---- hộp duyệt, loại, dọn kho ----

func (m *Model) glyphLine(role theme.Role, glyph, s string) string {
	return m.th.S(role, glyph) + " " + s
}

// checkLines là các dòng điều kiện của ứng viên (kiểm chứng, rò rỉ, đặc tả) dùng ở hộp duyệt.
func (m *Model) checkLines(c *eval.Case) []string {
	var out []string
	switch {
	case c.Verified():
		out = append(out, m.glyphLine(theme.OK, "✓", m.t("eval.approve.verified")))
	case c.Verification != nil:
		out = append(out, m.glyphLine(theme.Err, "✗", m.t("eval.approve.verify_"+c.Verification.Status)))
	default:
		out = append(out, m.glyphLine(theme.Err, "✗", m.t("eval.approve.unverified")))
	}
	switch c.Leak.Status {
	case "fail":
		out = append(out, m.glyphLine(theme.Err, "✗", m.t("eval.approve.leak_fail", i18n.A{"detail": firstLeak(c.Leak)})))
	case "warn":
		out = append(out, m.glyphLine(theme.Warn, "!", m.t("eval.approve.leak_warn")))
	default:
		out = append(out, m.glyphLine(theme.OK, "✓", m.t("eval.approve.leak_ok")))
	}
	if c.Spec != nil && c.Spec.Status != "pass" {
		out = append(out, m.glyphLine(theme.Warn, "!", m.t("eval.approve.spec_warn")))
	}
	return out
}

func firstLeak(l eval.LeakReport) string {
	if len(l.Findings) == 0 {
		return ""
	}
	return l.Findings[0].Detail
}

// cmdApprove mở hộp duyệt dựng từ dữ liệu đã nạp: hợp lệ thì hộp thường; bị chặn (đã loại, rò rỉ fail, chưa kiểm
// chứng) thì hộp đỏ có ô chọn riêng cho --force/--unverified, mặc định không chọn. EvalApprove tự kiểm lại lúc ghi
// nên lệch giữa lúc xem và lúc ghi chỉ thất bại an toàn (Problem hiện nguyên văn ở hộp kết quả).
func (m *Model) cmdApprove() tea.Cmd {
	if !m.allowed("eval.approve") {
		return nil
	}
	c, cmd := m.pickedCand()
	if c == nil {
		return cmd
	}
	body := append([]string{m.t("eval.approve.intro", i18n.A{"id": c.ID, "subject": c.Source.Subject}), ""}, m.checkLines(c)...)
	dlg := confirm.Model{Title: m.t("eval.approve.title", i18n.A{"id": c.ID}), Input: true, Label: m.t("eval.approve.split"),
		Placeholder: m.t("eval.approve.split.hint"), YesLabel: m.t("eval.approve.yes"), NoLabel: m.t("eval.dialog.cancel")}
	iForce, iUnv := -1, -1
	var why []string
	if c.Status == eval.StatusRejected {
		why = append(why, m.t("eval.approve.why.rejected", i18n.A{"reason": c.RejectReason}))
	}
	if c.Leak.Status == "fail" {
		why = append(why, m.t("eval.approve.why.leak"))
	}
	if len(why) > 0 {
		iForce = len(dlg.Checks)
		dlg.Checks = append(dlg.Checks, m.t("eval.approve.check.force", i18n.A{"why": strings.Join(why, "; ")}))
	}
	if !c.Verified() {
		iUnv = len(dlg.Checks)
		dlg.Checks = append(dlg.Checks, m.t("eval.approve.check.unverified"))
	}
	if len(dlg.Checks) > 0 {
		dlg.Danger = true
		body = append([]string{m.th.S(theme.Err, m.t("eval.approve.warn")), ""}, body...)
	}
	dlg.Body = body
	id := c.ID
	m.dlg = &dialog{c: dlg, onYes: func(d *confirm.Model) tea.Cmd {
		// Cờ bỏ qua kiểm tra chỉ được đặt khi người chọn ô tương ứng trong hộp cảnh báo.
		return request("eval.approve", app.EvalApproveOptions{IDs: []string{id}, Force: d.Checked(iForce), Unverified: d.Checked(iUnv), Split: d.Value()})
	}}
	return nil
}

func (m *Model) approveLines(outs []app.EvalApproveOutcome, err error) []string {
	var lines []string
	for _, o := range outs {
		switch {
		case o.Problem != "":
			lines = append(lines, m.th.S(theme.Err, "✗ ")+m.cli(o.Problem))
		case o.Already:
			lines = append(lines, m.th.S(theme.Muted, "· ")+m.t("eval.approve.already", i18n.A{"id": o.ID}))
		default:
			lines = append(lines, m.th.S(theme.OK, "✓ ")+m.t("eval.approve.done", i18n.A{"id": o.ID, "split": o.Split, "note": o.Note}))
		}
	}
	if err != nil {
		lines = append(lines, "", m.th.S(theme.Err, "✗ ")+ui.ErrText(m.rw, err))
	}
	return lines
}

// cmdReject mở hộp loại ứng viên có ô lý do (trống = lý do mặc định của CLI).
func (m *Model) cmdReject() tea.Cmd {
	if !m.allowed("eval.reject") {
		return nil
	}
	c, cmd := m.pickedCand()
	if c == nil {
		return cmd
	}
	id := c.ID
	def := app.EvalRejectDefaults(m.Data.Config).Reason
	dlg := confirm.Model{Title: m.t("eval.reject.title", i18n.A{"id": id}), Input: true, Label: m.t("eval.reject.reason"), Placeholder: def,
		Body: []string{m.t("eval.reject.body")}, YesLabel: m.t("eval.reject.yes"), NoLabel: m.t("eval.dialog.cancel")}
	m.dlg = &dialog{c: dlg, onYes: func(d *confirm.Model) tea.Cmd {
		reason := d.Value()
		if reason == "" {
			reason = def
		}
		return request("eval.reject", app.EvalRejectOptions{IDs: []string{id}, Reason: reason})
	}}
	return nil
}

// cmdClean hỏi trước khi xoá kho đệm commit gốc (có thể dựng lại nhưng tốn thời gian).
func (m *Model) cmdClean() tea.Cmd {
	if !m.allowed("eval.clean") {
		return nil
	}
	dlg := confirm.Model{Title: m.t("eval.clean.title"), Body: []string{m.t("eval.clean.body")}, Danger: true,
		YesLabel: m.t("eval.clean.yes"), NoLabel: m.t("eval.dialog.cancel")}
	m.dlg = &dialog{c: dlg, onYes: func(*confirm.Model) tea.Cmd { return request("eval.clean", app.EvalCleanOptions{}) }}
	return nil
}

// ---- tóm tắt kết quả ----

func (m *Model) specLines(r *app.EvalSpecResult) []string {
	lines := []string{m.t("eval.spec.analyzed", i18n.A{"count": r.Analyzed})}
	keys := make([]string, 0, len(r.Counts))
	for k := range r.Counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("  %-14s %d", k, r.Counts[k]))
	}
	return lines
}

func (m *Model) estimateLines(r *app.EvalEstimateResult) []string {
	e := r.Estimate
	lines := []string{m.t("eval.estimate.runs", i18n.A{"runs": e.Runs, "arms": strings.Join(r.Arms, ","), "repeats": r.Repeats})}
	switch {
	case e.TotalCost != nil:
		lines = append(lines, m.t("eval.estimate.cost", i18n.A{"cost": fmt.Sprintf("%.2f", *e.TotalCost), "source": e.CostSource}))
	default:
		lines = append(lines, m.t("eval.estimate.cost_unknown"))
	}
	if r.BudgetUSD != nil {
		lines = append(lines, m.t("eval.estimate.budget", i18n.A{"budget": fmt.Sprintf("%.2f", *r.BudgetUSD)}))
	}
	if e.SecondsPerRun != nil {
		lines = append(lines, m.t("eval.estimate.time", i18n.A{"seconds": fmt.Sprintf("%.0f", *e.SecondsPerRun)}))
	}
	lines = append(lines, m.t("eval.estimate.sample", i18n.A{"needed": e.NeededCases, "available": e.AvailableCases, "effect": fmt.Sprintf("%.0f", e.Effect*100)}))
	return lines
}

// onJobResult tóm tắt việc nền vừa xong. Đợt chạy xong thì toast và thẻ Đợt mở báo cáo của đợt đó; đợt bị huỷ, dừng
// vì ngân sách hay hạn mức thì hộp thoại nói rõ và cho lệnh chạy tiếp (--resume).
func (m *Model) onJobResult(msg actions.ResultMsg) tea.Cmd {
	m.frm, m.frmBusy = nil, false
	if errors.Is(msg.Err, actions.ErrEvalBusy) {
		m.setToast(ui.ErrText(m.rw, msg.Err), true) // việc thứ hai bị từ chối: chỉ báo, việc đang chạy không đổi
		return nil
	}
	var lines []string
	title := m.t("eval.job.title." + strings.TrimPrefix(msg.ID, "eval."))
	switch v := msg.Value.(type) {
	case *app.EvalRunResult:
		if v == nil {
			break
		}
		if v.BatchID != "" {
			m.lastBatch = v.BatchID
			m.focusBatch = v.BatchID
		}
		if msg.Err == nil && v.Status == "completed" {
			m.tab, m.pane = tabBatches, paneDetail
			m.detail.Reset()
			m.setToast(m.t("eval.run.completed", i18n.A{"batch": v.BatchID, "done": v.Done, "total": v.Total}), false)
			return m.refresh()
		}
		lines = m.runLines(v)
		if v.BatchID != "" && m.run != nil {
			m.run.resume = "bint eval run --resume " + v.BatchID
		}
	case *app.EvalBootstrapResult:
		if v != nil {
			lines = m.bootstrapLines(v)
		}
	case *app.EvalVerifyResult:
		if v != nil {
			lines = []string{m.t("eval.res.verify", i18n.A{"total": v.Total, "failed": v.Failed})}
		}
	case *app.EvalMineResult:
		if v != nil {
			lines = m.mineLines(v)
		}
	}
	if msg.Err != nil {
		if errors.Is(msg.Err, app.ErrCancelled) && len(lines) == 0 {
			m.setToast(m.t("eval.job.declined"), false) // trả "không" ở hộp xác nhận: không có gì chạy
			return m.refresh()
		}
		lines = append(lines, "", m.th.S(theme.Err, "✗ ")+ui.ErrText(m.rw, msg.Err))
		m.setToast(ui.ErrText(m.rw, msg.Err), true)
	}
	if m.run != nil && m.run.resume != "" {
		lines = append(lines, "", m.t("eval.res.resume", i18n.A{"cmd": m.cli(m.run.resume)}))
	}
	if len(lines) > 0 {
		m.openInfo(title, lines)
	}
	return m.refresh()
}

func (m *Model) runLines(r *app.EvalRunResult) []string {
	lines := []string{m.t("eval.res.run", i18n.A{"batch": r.BatchID, "status": m.tr("eval.bst.", r.Status), "done": r.Done, "total": r.Total})}
	if r.SpentUSD > 0 {
		lines = append(lines, m.t("eval.res.spent", i18n.A{"usd": fmt.Sprintf("%.4f", r.SpentUSD)}))
	}
	if r.StopReason != "" {
		lines = append(lines, m.t("eval.res.stop", i18n.A{"why": r.StopReason}))
	}
	return lines
}

func (m *Model) bootstrapLines(r *app.EvalBootstrapResult) []string {
	lines := []string{m.t("eval.res.bootstrap", i18n.A{"cases": r.Cases, "bases": r.Bases, "todo": r.Todo, "failed": r.Failed})}
	if r.ReportsCost {
		lines = append(lines, m.t("eval.res.spent", i18n.A{"usd": fmt.Sprintf("%.4f", r.Spent)}))
	}
	return lines
}

func (m *Model) mineLines(r *app.EvalMineResult) []string {
	lines := []string{m.t("eval.res.mine", i18n.A{"scanned": r.Scanned, "new": len(r.New)})}
	keys := make([]string, 0, len(r.Skipped))
	for k := range r.Skipped {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("  %-22s %d", k, r.Skipped[k]))
	}
	if len(r.New) > 0 {
		lines = append(lines, "", m.t("eval.res.mine.dir", i18n.A{"dir": r.CandidatesDir}))
	}
	return lines
}
