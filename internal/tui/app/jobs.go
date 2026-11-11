package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"bint/internal/i18n"
	"bint/internal/task"
	"bint/internal/tui/actions"
	"bint/internal/tui/components/confirm"
	"bint/internal/tui/components/text"
	"bint/internal/tui/jobs"
	tkscreen "bint/internal/tui/screens/tasks"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
	"bint/internal/workflow"
)

// Việc nền của mô hình gốc (docs/tui-plan.md mục 7.2): bắt đầu việc, đọc thông điệp từ kênh (câu hỏi, log, bước,
// xong), hộp thoại câu hỏi giữa chừng và hộp thoại thoát khi còn việc.

const (
	defaultExitGrace = 15 * time.Second
	defaultTailEvery = 400 * time.Millisecond
)

// exitTimeoutMsg: hết hạn chờ việc kết thúc sau khi huỷ lúc thoát.
type exitTimeoutMsg struct{}

func (m *Model) tailEvery() time.Duration {
	if m.opts.TailEvery > 0 {
		return m.opts.TailEvery
	}
	return defaultTailEvery
}

// pump đăng ký đọc thông điệp kế tiếp của kênh việc nền. Không đăng ký khi đã có một lệnh chờ, khi đang hiện
// câu hỏi (thông điệp sau chờ trong kênh tới khi người trả lời) hoặc khi hết việc.
func (m *Model) pump() tea.Cmd {
	if m.pumping || m.prompt != nil || m.jobs.Len() == 0 {
		return nil
	}
	m.pumping = true
	return jobs.Wait(m.jobCh)
}

// startJob chạy Action.Job trong goroutine riêng với cầu hỏi/log; kết quả về bằng jobs.DoneMsg.
func (m *Model) startJob(a actions.Action, opts any) (tea.Model, tea.Cmd) {
	info := jobs.Info{Kind: jobs.KindAction}
	if a.JobInfo != nil {
		info = a.JobInfo(opts)
	}
	// Mỗi lúc một việc workflow, một việc nặng của Tiến hoá và một việc nặng của Đánh giá.
	for kind, busy := range map[string]error{jobs.KindWorkflow: actions.ErrWorkflowBusy, jobs.KindEvolve: actions.ErrEvolveBusy, jobs.KindEval: actions.ErrEvalBusy} {
		if info.Kind == kind && m.jobs.OfKind(kind) != nil {
			id := a.ID
			return m, func() tea.Msg { return actions.ResultMsg{ID: id, Opts: opts, Err: busy} }
		}
	}
	ctx, cancel := context.WithCancel(m.ctx)
	j := m.jobs.Add(jobs.Job{Action: a.ID, Kind: info.Kind, Label: info.Label, TaskID: info.TaskID, Resume: info.Resume, Cancel: cancel, Opts: opts})
	br := jobs.NewBridge(ctx, j.ID, m.jobCh)
	env, run := m.env(), a.Job
	go func() {
		defer func() {
			if r := recover(); r != nil {
				br.Finish(nil, fmt.Errorf("internal error: %v", r))
			}
		}()
		v, err := run(ctx, env, opts, br)
		br.Finish(v, err)
	}()
	return m, tea.Batch(m.pump(), m.startTick())
}

// onStep cập nhật đuôi log của việc khi bước agent bắt đầu/kết thúc.
func (m *Model) onStep(msg jobs.StepMsg) tea.Cmd {
	j := m.jobs.Get(msg.Job)
	if j == nil {
		return nil
	}
	if j.TaskID == "" && msg.Event.TaskID != "" {
		j.TaskID = msg.Event.TaskID // việc chạy từ mô tả mới: tác vụ chỉ biết khi bước đầu bắt đầu
	}
	refresh := m.tk.Update(tkscreen.RefreshMsg{}) // trạng thái tác vụ vừa đổi: nạp lại danh sách và chi tiết
	if msg.Event.Done {
		j.StopTail()
		return refresh
	}
	j.StartTail(msg.Event)
	return tea.Batch(refresh, jobs.TailTick(j.ID, m.tailEvery()))
}

// onJobDone dọn việc đã xong, báo thông báo ở mọi màn hình và chuyển kết quả cho màn hình sở hữu hành động.
func (m *Model) onJobDone(msg jobs.DoneMsg) (tea.Model, tea.Cmd) {
	m.pumping = false
	j := m.jobs.Get(msg.Job)
	if j == nil {
		return m, m.pump()
	}
	m.jobs.Remove(msg.Job, msg.Err)
	if j.Cancel != nil {
		j.Cancel()
	}
	if m.prompt != nil && m.prompt.job == msg.Job {
		m.prompt = nil
	}
	cmds := []tea.Cmd{m.onResult(actions.ResultMsg{ID: j.Action, Opts: j.Opts, Value: msg.Value, Err: msg.Err})}
	m.toast = m.doneToast(j, msg)
	if m.exiting && m.jobs.Len() == 0 {
		m.quitting = true
		return m, tea.Quit
	}
	return m, tea.Batch(append(cmds, m.pump())...)
}

// onResult chuyển ResultMsg cho màn hình sở hữu hành động và nạp lại Tổng quan khi hành động ghi thành công.
func (m *Model) onResult(msg actions.ResultMsg) tea.Cmd {
	if j := m.jobs.Running(msg.ID); j != nil && j.Kind == jobs.KindAction {
		if j.Cancel != nil {
			j.Cancel()
		}
		m.jobs.Remove(j.ID, msg.Err)
	}
	var cmds []tea.Cmd
	a, ok := m.reg.ByID(msg.ID)
	if ok {
		if sc := m.screens[a.Screen]; sc != nil {
			cmds = append(cmds, sc.Update(msg))
		}
		if a.Writes && msg.Err == nil {
			cmds = append(cmds, m.load()) // số việc chờ ở Tổng quan và huy hiệu tab đổi theo thao tác ghi
		}
	}
	cmds = append(cmds, m.tickIfBusy())
	return tea.Batch(cmds...)
}

// doneToast là thông báo khi một việc nền kết thúc (hiện ở mọi màn hình).
func (m *Model) doneToast(j *jobs.Job, msg jobs.DoneMsg) string {
	label := j.Label
	if label == "" {
		label = j.Action
	}
	switch {
	case msg.Err != nil:
		return m.cat.T("job.done.err", i18n.A{"label": label, "error": ui.ErrText(m.cat, msg.Err)})
	}
	if res, ok := msg.Value.(*workflow.Result); ok && res != nil {
		switch res.Status {
		case task.StatusBlocked, task.StatusFailed, task.StatusInterrupted:
			return m.cat.T("job.done.attention", i18n.A{"label": label, "status": res.Status})
		}
		return m.cat.T("job.done.status", i18n.A{"label": label, "status": res.Status})
	}
	return m.cat.T("job.done.ok", i18n.A{"label": label})
}

// ---- câu hỏi giữa chừng ----

// promptState là câu hỏi đang hiện.
type promptState struct {
	job   int
	dlg   confirm.Model
	ask   bool
	reply chan<- jobs.Answer
}

// onPrompt hiện hộp thoại cho câu hỏi của việc. Việc đã bỏ đi hoặc đang thoát thì trả "không" ngay.
func (m *Model) onPrompt(msg jobs.PromptMsg) (tea.Model, tea.Cmd) {
	m.pumping = false
	j := m.jobs.Get(msg.Job)
	if j == nil || m.exiting {
		msg.Reply <- jobs.Answer{} // Reply có đệm 1: không chặn
		return m, m.pump()
	}
	m.prompt = &promptState{job: msg.Job, dlg: m.promptDialog(j, msg.Prompt), ask: msg.Prompt.Kind == jobs.Ask, reply: msg.Reply}
	return m, nil
}

func (m *Model) promptDialog(j *jobs.Job, p jobs.Prompt) confirm.Model {
	c := confirm.Model{YesLabel: m.cat.T("prompt.yes"), NoLabel: m.cat.T("prompt.no")}
	args := i18n.A(p.Params)
	if args == nil {
		args = i18n.A{}
	}
	title, body := m.cat.T("prompt.title"), strings.Split(strings.TrimSpace(p.Text), "\n")
	if k := "prompt." + p.Key; p.Key != "" && m.cat.Has(k+".title") {
		title, body = m.cat.T(k+".title", args), strings.Split(m.cat.T(k+".body", args), "\n")
	}
	c.Title = title
	if j.Label != "" {
		c.Body = append(c.Body, m.th.S(theme.Muted, j.Label), "")
	}
	c.Body = append(c.Body, body...)
	if len(p.Paths) > 0 {
		c.Body = append(c.Body, "")
		for _, f := range p.Paths {
			c.Body = append(c.Body, "  "+m.th.S(theme.Warn, f))
		}
	}
	if d := strings.TrimSpace(p.Detail); d != "" {
		c.Body = append(c.Body, "", d)
	}
	switch {
	case p.Kind == jobs.Ask:
		c.Input, c.Label, c.YesLabel, c.NoLabel = true, m.cat.T("prompt.answer"), m.cat.T("prompt.send"), m.cat.T("prompt.skip")
	case p.Key == workflow.QConfirmScope:
		c.Danger = true // duyệt thay đổi chạm đường dẫn bảo vệ: hộp đỏ
	}
	return c
}

func (m *Model) promptKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := m.prompt
	switch p.dlg.Update(msg) {
	case confirm.Yes:
		p.reply <- jobs.Answer{Yes: true, Text: p.dlg.Value()}
	case confirm.No:
		p.reply <- jobs.Answer{}
	default:
		return m, nil
	}
	m.prompt = nil
	return m, m.pump()
}

// ---- thoát khi còn việc nền ----

func (m *Model) openExitDialog() {
	c := confirm.Model{Title: m.cat.T("exit.title"), Danger: true, YesLabel: m.cat.T("exit.yes"), NoLabel: m.cat.T("exit.no")}
	if m.restartAsk != "" {
		c.Title, c.YesLabel, c.NoLabel = m.cat.T("restart.busy.title"), m.cat.T("restart.busy.yes"), m.cat.T("restart.busy.no")
	}
	c.Body = append(c.Body, m.cat.N("exit.body", max(m.jobs.Len(), 1)), "")
	for _, j := range m.jobs.All() {
		label := j.Label
		if label == "" {
			label = j.Action
		}
		c.Body = append(c.Body, "• "+m.th.Bold(label))
		if j.Resume != "" {
			c.Body = append(c.Body, "    "+m.th.S(theme.Muted, m.cat.T("exit.resume", i18n.A{"cmd": j.Resume})))
		}
	}
	if m.jobs.Len() == 0 && m.wiz.Running() { // khởi tạo chưa vào bảng (kiểm thử dựng trạng thái tay)
		c.Body = append(c.Body, "• "+m.th.Bold(m.cat.T("init.group")))
	}
	c.Body = append(c.Body, "", m.th.S(theme.Muted, m.cat.T("exit.hint")))
	m.exitDlg = &c
}

func (m *Model) exitKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch m.exitDlg.Update(msg) {
	case confirm.No:
		m.exitDlg, m.restartAsk = nil, ""
	case confirm.Yes:
		m.exitDlg = nil
		m.restartPath, m.restartAsk = m.restartAsk, ""
		m.exiting = true
		if p := m.prompt; p != nil { // câu hỏi đang chờ: trả "không"
			p.reply <- jobs.Answer{}
			m.prompt = nil
		}
		m.jobs.CancelAll()
		m.toast = m.cat.T("exit.stopping")
		if m.jobs.Len() == 0 && !m.wiz.Running() {
			m.quitting = true
			return m, tea.Quit
		}
		grace := m.opts.ExitGrace
		if grace <= 0 {
			grace = defaultExitGrace
		}
		return m, tea.Batch(m.pump(), tea.Tick(grace, func(time.Time) tea.Msg { return exitTimeoutMsg{} }))
	}
	return m, nil
}

// overlayDialog vẽ hộp thoại của việc nền (câu hỏi hoặc thoát) lên các dòng đã dựng; ok = có hộp để vẽ.
func (m *Model) overlayDialog(lines []string, w, h int) ([]string, bool) {
	var c *confirm.Model
	switch {
	case m.exitDlg != nil:
		c = m.exitDlg
	case m.prompt != nil:
		c = &m.prompt.dlg
	default:
		return lines, false
	}
	bw, bh := min(w-4, 84), min(h-4, 26)
	box := c.View(m.th, bw, bh)
	return text.Overlay(lines, box, (w-bw)/2, max((h-len(box))/2, 1)), true
}
