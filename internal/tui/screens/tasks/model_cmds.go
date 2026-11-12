package tasks

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"bint/internal/app"
	"bint/internal/config"
	"bint/internal/i18n"
	"bint/internal/task"
	"bint/internal/tui/actions"
	"bint/internal/tui/components/confirm"
	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
	"bint/internal/workflow"
)

// cmdVerify chạy lệnh kiểm chứng của tác vụ đang chọn (việc nền).
func (m *Model) cmdVerify() tea.Cmd {
	t := m.Cursor()
	if t == nil || !m.allowed("tasks.verify") {
		return nil
	}
	m.ShowLog()
	return request("tasks.verify", app.TaskVerifyOptions{Ref: t.State.ID})
}

// cmdLint kiểm tài liệu của tác vụ đang chọn (chỉ đọc), kết quả hiện trong hộp thoại.
func (m *Model) cmdLint() tea.Cmd {
	t := m.Cursor()
	if t == nil {
		return nil
	}
	return request("tasks.lint", app.TaskLintOptions{Ref: t.State.ID})
}

// cmdEdit: ở thẻ Tài liệu sửa tài liệu đang xem trong $EDITOR; ở thẻ Review mở tệp của phát hiện tại dòng của nó;
// còn lại mở form sửa phiếu.
func (m *Model) cmdEdit() tea.Cmd {
	t := m.Cursor()
	if t == nil {
		return nil
	}
	switch m.sub {
	case subDocs:
		if !m.allowed("tasks.write") {
			return nil
		}
		return request("tasks.edit_doc", actions.EditDocOptions{Ref: t.State.ID, Name: docNames[m.doc]})
	case subReview:
		f := m.currentFinding()
		if f == nil || f.Path == "" {
			return nil
		}
		return request("tasks.open_file", actions.EditOptions{ID: f.ID, Path: filepath.Join(m.Data.Root, filepath.FromSlash(f.Path)), Line: firstLine(f.Lines)})
	}
	if !m.allowed("tasks.set") {
		return nil
	}
	m.openForm("tasks.set", "task set", app.TaskSetFrom(t), nil)
	return nil
}

// cmdError mở form ghi lỗi cho tác vụ đang chọn.
func (m *Model) cmdError() tea.Cmd {
	t := m.Cursor()
	if t == nil || !m.allowed("tasks.error") {
		return nil
	}
	o := app.TaskErrorDefaults(config.Default())
	o.Ref = t.State.ID
	m.openForm("tasks.error", "task error", o, nil)
	return nil
}

// cmdCriteria mở form đối chiếu một tiêu chí; tiêu chí đầu tiên chưa đạt được chọn sẵn.
func (m *Model) cmdCriteria() tea.Cmd {
	t := m.Cursor()
	if t == nil || !m.allowed("tasks.criteria") {
		return nil
	}
	if len(t.Ticket.Verify.Criteria) == 0 {
		m.setToast(m.t("tk.criteria.none"), true)
		return nil
	}
	o := app.TaskCriteriaDefaults(config.Default())
	o.Ref, o.Index = t.State.ID, 1
	for i := range t.Ticket.Verify.Criteria {
		if cs := t.State.CriterionStatus(i + 1); cs == nil || !cs.Met {
			o.Index = i + 1
			break
		}
	}
	m.openForm("tasks.criteria", "task criteria", o, nil)
	return nil
}

// cmdReviewEdit mở mẫu JSON của một vòng review trong $EDITOR.
func (m *Model) cmdReviewEdit() tea.Cmd {
	t := m.Cursor()
	if t == nil || !m.allowed("tasks.review") {
		return nil
	}
	return request("tasks.review_edit", actions.EditDocOptions{Ref: t.State.ID})
}

// cmdKnowledge chuyển phát hiện đang chọn (thẻ Review) thành mục tri thức: form Thêm tri thức điền sẵn.
func (m *Model) cmdKnowledge() tea.Cmd {
	f := m.currentFinding()
	if f == nil {
		m.setToast(m.t("tk.knowledge.no_finding"), true)
		return nil
	}
	if !m.allowed("tasks.add_knowledge") {
		return nil
	}
	return request("tasks.finding_prefill", app.AddOptions{FromReview: f.ID})
}

// showTransitions mở hộp liệt kê bảng chuyển trạng thái tác vụ.
func (m *Model) showTransitions() {
	var body []string
	for _, r := range app.TaskTransitions() {
		line := m.th.S(theme.Accent, r.From) + " → " + m.th.S(theme.Accent, r.To)
		if r.Note != "" {
			if k := "tk.trans." + r.To; m.cat.Has(k) {
				line += "  " + m.t(k)
			}
		}
		body = append(body, line)
	}
	body = append(body, "", m.th.S(theme.Muted, m.t("tk.transitions.note")))
	m.openInfo(m.t("tk.transitions.title"), body)
}

// ---- thẻ Review ----

// sortedFindings là các phát hiện của vòng review gần nhất, xếp critical → high → medium → low (ổn định).
func sortedFindings(rev *task.Review) []task.Finding {
	last := rev.Last()
	if last == nil {
		return nil
	}
	out := append([]task.Finding(nil), last.Findings...)
	rank := func(s string) int {
		for i, v := range task.FindingSeverities {
			if v == s {
				return i
			}
		}
		return len(task.FindingSeverities)
	}
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i].Severity) < rank(out[j].Severity) })
	return out
}

// currentFinding trả phát hiện đang chọn ở thẻ Review (nil nếu không có).
func (m *Model) currentFinding() *task.Finding {
	t := m.Cursor()
	if t == nil {
		return nil
	}
	d, ok := m.cache[t.State.ID]
	if !ok || d.Review == nil {
		return nil
	}
	fs := sortedFindings(d.Review)
	if len(fs) == 0 {
		return nil
	}
	m.find = max(0, min(m.find, len(fs)-1))
	return &fs[m.find]
}

func (m *Model) moveFinding(d int) {
	t := m.Cursor()
	if t == nil {
		return
	}
	if dd, ok := m.cache[t.State.ID]; ok && dd.Review != nil {
		m.find = max(0, min(m.find+d, max(len(sortedFindings(dd.Review))-1, 0)))
	}
}

func (m *Model) scrollLog(d int) { m.log.Scroll(d, m.logTotal, m.logH) }

// firstLine là dòng đầu của chuỗi dòng "a-b" hoặc "a" (0 nếu không đọc được).
func firstLine(lines string) int {
	a, _, ok := strings.Cut(lines, "-")
	if !ok {
		a = lines
	}
	n, _ := strconv.Atoi(strings.TrimSpace(a))
	return n
}

// lineRange đọc "a-b" hoặc "a" thành (a, b); không đọc được thì (0, 0).
func lineRange(lines string) (int, int) {
	a, b, ok := strings.Cut(lines, "-")
	x, _ := strconv.Atoi(strings.TrimSpace(a))
	y := x
	if ok {
		y, _ = strconv.Atoi(strings.TrimSpace(b))
	}
	if x <= 0 || y < x {
		return 0, 0
	}
	return x, y
}

const (
	excerptContext = 3
	excerptMax     = 60
)

// openExcerpt mở trích đoạn tệp của phát hiện đang chọn quanh dòng `lines` (chỉ đọc, dùng được ở --read-only).
func (m *Model) openExcerpt() tea.Cmd {
	f := m.currentFinding()
	if f == nil {
		return nil
	}
	title := f.Path
	if f.Lines != "" {
		title += ":" + f.Lines
	}
	if f.Path == "" {
		m.openInfo(m.t("tk.excerpt.title"), []string{m.t("tk.excerpt.no_path")})
		return nil
	}
	b, err := os.ReadFile(filepath.Join(m.Data.Root, filepath.FromSlash(f.Path)))
	if err != nil {
		m.openInfo(title, []string{m.th.S(theme.Err, m.t("tk.excerpt.missing"))})
		return nil
	}
	all := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	from, to := lineRange(f.Lines)
	lo, hi := 1, min(len(all), excerptMax)
	if from > 0 {
		lo, hi = max(1, from-excerptContext), min(len(all), to+excerptContext)
		if hi-lo+1 > excerptMax {
			hi = lo + excerptMax - 1
		}
	}
	var body []string
	for i := lo; i <= hi; i++ {
		mark, role := "  ", theme.Muted
		if from > 0 && i >= from && i <= to {
			mark, role = "▸ ", theme.Key
		}
		line := strings.ReplaceAll(all[i-1], "\t", "    ")
		body = append(body, m.th.S(role, mark+text.Pad(strconv.Itoa(i), 5))+" "+line)
	}
	if hi < len(all) && from == 0 {
		body = append(body, m.th.S(theme.Muted, m.t("tk.excerpt.more", i18n.A{"count": len(all) - hi})))
	}
	m.openInfo(title, body)
	return nil
}

// ---- kết quả hành động ----

func (m *Model) onResult(r actions.ResultMsg) tea.Cmd {
	if r.Err != nil && r.ID != "tasks.edit_doc" && r.ID != "tasks.review_edit" && r.ID != "tasks.open_file" {
		return m.onError(r)
	}
	if m.frmBusy && m.frmID == r.ID {
		m.frmBusy = false
	}
	switch r.ID {
	case "tasks.new":
		c, ok := r.Value.(app.TaskCreated)
		if !ok {
			return nil
		}
		m.frm = nil
		m.setToast(m.t("tk.new.done", i18n.A{"id": c.Task.State.ID}), false)
		m.focusID = c.Task.State.ID
		return m.Load()
	case "tasks.set", "tasks.write", "tasks.criteria", "tasks.error", "tasks.review":
		m.frm = nil
		m.setToast(m.t(doneKey[r.ID]), false)
		m.invalidate()
		return tea.Batch(m.Load(), m.wantDetail())
	case "tasks.status":
		m.frm = nil
		if sr, ok := r.Value.(app.TaskStatusResult); ok && sr.Task != nil {
			m.setToast(m.t("tk.status.done", i18n.A{"id": short(sr.Task.State.ID), "status": sr.Task.State.Status}), false)
		}
		m.invalidate()
		return tea.Batch(m.Load(), m.wantDetail())
	case "tasks.lint":
		if lr, ok := r.Value.(app.TaskLinted); ok {
			m.showLint(lr)
		}
	case "tasks.plan_approve":
		if p, ok := r.Value.(app.ApprovePlan); ok {
			m.confirmApprove(p)
		}
	case "tasks.approve":
		m.setToast(m.t("tk.approve.done"), false)
		m.invalidate()
		return tea.Batch(m.Load(), m.wantDetail())
	case "tasks.verify":
		if v, ok := r.Value.(app.TaskVerified); ok {
			if v.Result.Skipped != "" {
				m.setToast(m.t("tk.verify.skipped"), true)
			} else {
				m.setToast(m.t("tk.verify.result", i18n.A{"result": m.t(map[bool]string{true: "tk.pass", false: "tk.fail"}[v.Result.Verification.Passed])}), !v.Result.Verification.Passed)
			}
		}
		m.invalidate()
		return tea.Batch(m.Load(), m.wantDetail())
	case "tasks.run", "tasks.resume":
		if res, ok := r.Value.(*workflow.Result); ok && res != nil && res.TaskID != "" {
			m.focusID = res.TaskID // chạy từ mô tả mới: chọn tác vụ vừa được lập
		}
		m.invalidate()
		return tea.Batch(m.Load(), m.wantDetail())
	case "tasks.edit_doc", "tasks.review_edit":
		return m.afterEditor(r)
	case "tasks.open_file":
		if r.Err == nil {
			m.setToast(m.t("tk.edit.done"), false)
		}
	case "tasks.finding_prefill":
		if o, ok := r.Value.(app.AddOptions); ok {
			m.openForm("tasks.add_knowledge", "knowledge add", o, nil)
		}
	case "tasks.add_knowledge":
		ar, ok := r.Value.(app.AddResult)
		if !ok {
			return nil
		}
		if !ar.Saved || ar.Item == nil {
			if m.frm != nil {
				var parts []string
				for _, f := range ar.Check.Findings {
					parts = append(parts, f.Code+": "+m.cli(f.Detail))
				}
				m.frm.Err = m.t("tk.knowledge.failed") + " " + strings.Join(parts, "; ")
			}
			return nil
		}
		m.frm = nil
		m.setToast(m.t("tk.knowledge.done", i18n.A{"id": ar.Item.ID, "status": ar.Item.Status}), false)
	}
	return nil
}

// doneKey là khoá thông báo khi một hành động ghi thành công.
var doneKey = map[string]string{"tasks.set": "tk.set.done", "tasks.write": "tk.write.done", "tasks.criteria": "tk.criteria.done",
	"tasks.error": "tk.error.done", "tasks.review": "tk.review.done"}

// onError hiện lỗi của hành động: ở form đang mở nếu form đó gửi nó, nếu không thì thông báo.
func (m *Model) onError(r actions.ResultMsg) tea.Cmd {
	if m.frm != nil && m.frmID == r.ID {
		m.frmBusy = false
		m.frm.Err = ui.ErrText(m.rw, r.Err)
		return nil
	}
	m.frmBusy = false
	m.setToast(ui.ErrText(m.rw, r.Err), true)
	m.invalidate()
	return tea.Batch(m.Load(), m.wantDetail())
}

// afterEditor đọc kết quả sửa trong $EDITOR và gửi yêu cầu ghi nếu nội dung thay đổi.
func (m *Model) afterEditor(r actions.ResultMsg) tea.Cmd {
	o, ok := r.Opts.(actions.EditDocOptions)
	if !ok {
		return nil
	}
	defer os.Remove(o.Path)
	if r.Err != nil {
		m.setToast(ui.ErrText(m.rw, r.Err), true)
		return nil
	}
	b, err := os.ReadFile(o.Path)
	if err != nil {
		m.setToast(ui.ErrText(m.rw, err), true)
		return nil
	}
	content := string(b)
	if content == o.Before || strings.TrimSpace(content) == "" {
		m.setToast(m.t("tk.edit.unchanged"), false)
		return nil
	}
	if r.ID == "tasks.review_edit" {
		return request("tasks.review", app.TaskReviewOptions{Ref: o.Ref, JSON: content})
	}
	return request("tasks.write", app.TaskWriteOptions{Ref: o.Ref, Name: o.Name, Content: content})
}

// confirmApprove mở hộp xác nhận duyệt dựng từ kế hoạch chỉ đọc: lệnh kiểm chứng sẽ được xác nhận và các đường
// dẫn sẽ được duyệt. Không có gì để duyệt thì hộp chỉ để đọc.
func (m *Model) confirmApprove(p app.ApprovePlan) {
	c := confirm.Model{Title: m.t("tk.approve.title"), YesLabel: m.t("tk.approve.yes"), NoLabel: m.t("tk.dialog.cancel")}
	c.Body = append(c.Body, m.th.Bold(short(p.Task.State.ID))+" "+p.Task.Ticket.Goal, "")
	if p.Empty() {
		c.Body = append(c.Body, m.t("tk.approve.nothing"))
		c.YesLabel, c.NoLabel = m.t("tk.dialog.close"), m.t("tk.dialog.close")
		m.dlg = &dialog{c: c}
		return
	}
	if p.VerifyCommand != "" {
		c.Body = append(c.Body, m.th.S(theme.Accent, m.t("tk.approve.command")), "  "+m.th.S(theme.Key, p.VerifyCommand), m.th.S(theme.Muted, "  "+m.t("tk.approve.command_hint")), "")
	}
	if len(p.Paths) > 0 {
		c.Danger = true
		c.Body = append(c.Body, m.th.S(theme.Warn, m.cat.N("tk.approve.paths", len(p.Paths))))
		for _, f := range p.Paths {
			c.Body = append(c.Body, "  "+m.th.S(theme.Warn, f))
		}
		c.Body = append(c.Body, m.th.S(theme.Muted, "  "+m.t("tk.approve.paths_hint")))
	}
	if p.Resumable {
		c.Body = append(c.Body, "", m.th.S(theme.Muted, m.t("tk.approve.resume", i18n.A{"cmd": m.cli("bint run resume " + p.Task.State.ID)})))
	}
	c.Input, c.Label = true, m.t("tk.approve.extra")
	ref := p.Task.State.ID
	m.dlg = &dialog{c: c, onYes: func(extra string) tea.Cmd {
		var paths []string
		for _, x := range strings.Split(extra, ",") {
			if x = strings.TrimSpace(x); x != "" {
				paths = append(paths, x)
			}
		}
		return request("tasks.approve", app.TaskApproveOptions{Ref: ref, Paths: paths})
	}}
}

// showLint hiện kết quả kiểm tài liệu trong hộp thoại.
func (m *Model) showLint(lr app.TaskLinted) {
	var body []string
	rep := lr.Report
	for _, i := range rep.Errors {
		body = append(body, m.th.S(theme.Err, "✗ ")+"["+i.Code+"] "+i.Artifact+": "+m.cli(i.Detail))
	}
	for _, i := range rep.Warnings {
		body = append(body, m.th.S(theme.Warn, "! ")+"["+i.Code+"] "+i.Artifact+": "+m.cli(i.Detail))
	}
	for _, i := range rep.OpenQuestions {
		body = append(body, m.th.S(theme.Accent, "? ")+"["+i.Code+"] "+i.Artifact+": "+m.cli(i.Detail))
	}
	if len(body) == 0 {
		body = append(body, m.th.S(theme.OK, "✓ ")+m.t("tk.lint.ok"))
	} else if len(rep.OpenQuestions) > 0 {
		body = append(body, "", m.th.S(theme.Muted, m.t("tk.lint.answer", i18n.A{"cmd": m.cli("bint run resume " + lr.Task.State.ID)})))
	}
	m.openInfo(m.t("tk.lint.title", i18n.A{"id": short(lr.Task.State.ID)}), body)
}

// short rút ID dài (ULID) còn 10 ký tự cuối cho dễ đọc.
func short(id string) string {
	if r := []rune(id); len(r) > 10 {
		return string(r[len(r)-10:])
	}
	return id
}

// keyOf đổi ID hành động của màn hình thành phím tương đương ở chế độ danh sách (cho Command).
var keyOf = map[string]string{
	"tasks.new": "n", "tasks.run": "r", "tasks.resume": "c", "tasks.approve": "a", "tasks.status": "s", "tasks.verify": "v",
	"tasks.lint": "l", "tasks.transitions": "T", "tasks.criteria": "m", "tasks.error": "i", "tasks.review_edit": "w",
	"tasks.add_knowledge": "K", "tasks.list": "A", "tasks.filter": "/", "tasks.refresh": "ctrl+r", "tasks.show": "enter",
}

// Command thực hiện hành động theo ID (từ bảng lệnh): chọn thẻ con tương ứng hoặc làm như bấm phím tương đương.
func (m *Model) Command(id string) tea.Cmd {
	if m.dlg != nil || m.frm != nil {
		return nil
	}
	m.pane = paneList
	switch id {
	case "tasks.next_view":
		m.setSub(m.nextSub(1))
		return m.wantDetail()
	case "tasks.prev_view":
		m.setSub(m.nextSub(-1))
		return m.wantDetail()
	case "tasks.runs":
		m.setSub(subRuns)
		return m.wantDetail()
	case "tasks.set":
		t := m.Cursor()
		if t == nil || !m.allowed("tasks.set") {
			return nil
		}
		m.openForm("tasks.set", "task set", app.TaskSetFrom(t), nil)
		return nil
	case "tasks.write", "tasks.edit_doc":
		m.setSub(subDocs)
		return m.cmdEdit()
	case "tasks.review":
		m.setSub(subReview)
		return m.cmdReviewEdit()
	case "tasks.add_knowledge":
		m.setSub(subReview)
		return tea.Batch(m.wantDetail(), m.cmdKnowledge())
	}
	if k, ok := keyOf[id]; ok {
		cmd, _ := m.listKey(keyMsgOf(k))
		return cmd
	}
	return nil
}

// keyMsgOf dựng phím từ tên (cho Command).
func keyMsgOf(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "ctrl+r":
		return tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}
	}
	r := []rune(s)
	return tea.KeyPressMsg{Code: r[0], Text: s}
}
