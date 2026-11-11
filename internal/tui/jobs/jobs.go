// Package jobs là hạ tầng việc nền của TUI (docs/tui-plan.md mục 7.2): bảng việc đang chạy, cầu nối câu hỏi
// giữa chừng của dịch vụ (Confirm/Ask chạy trong goroutine của việc) với vòng Update (hộp thoại), và đuôi
// log của bước agent đang chạy. Goroutine của việc không chạm trạng thái giao diện: mọi thứ đi qua kênh
// thành tea.Msg (PromptMsg, LogMsg, StepMsg, DoneMsg).
package jobs

import (
	"context"
	"io"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"bint/internal/app"
	"bint/internal/workflow"
)

// Loại việc.
const (
	// KindWorkflow: run/resume; mọi việc loại này dùng chung khoá "workflow" (mỗi lúc một việc, theo khoá ghi
	// worktree của workflow).
	KindWorkflow = "workflow"
	KindVerify   = "verify"
	// KindAction: hành động có tham số chạy nền thông thường (không log, không câu hỏi).
	KindAction = "action"
	// KindInit: khởi tạo repository.
	KindInit = "init"
	// KindEvolve: việc nặng của màn Tiến hoá (propose, evaluate, process, cycle; có thể gọi mô hình và tốn tiền);
	// mỗi lúc một việc loại này trong tiến trình TUI.
	KindEvolve = "evolve"
	// KindEval: việc nặng của màn Đánh giá (run, bootstrap, verify, mine có kiểm chứng; chạy tác vụ bằng agent, có thể
	// tốn tiền); mỗi lúc một việc loại này trong tiến trình TUI. Tách khỏi KindEvolve vì hai bên ghi đợt riêng.
	KindEval = "eval"
)

// PromptKind phân biệt câu hỏi có/không và câu hỏi trả lời tự do.
type PromptKind int

const (
	Confirm PromptKind = iota
	Ask
)

// Prompt là một câu hỏi giữa chừng. Key (khoá i18n) và Params để giao diện dịch; Text là bản tiếng Việt
// nguyên văn (dự phòng khi chưa có khoá trong danh mục); Detail/Paths là nội dung đi kèm.
type Prompt struct {
	Kind   PromptKind
	Key    string
	Text   string
	Params map[string]any
	Paths  []string
	Detail string
}

// Answer là câu trả lời của người: Yes cho Confirm, Text cho Ask.
type Answer struct {
	Yes  bool
	Text string
}

// PromptMsg đưa một câu hỏi tới vòng Update. Reply có đệm 1 nên Update không bao giờ bị chặn khi việc đã bỏ đi.
type PromptMsg struct {
	Job    int
	Prompt Prompt
	Reply  chan<- Answer
}

// LogMsg là một dòng tiến trình của orchestrator (Runner.Logf).
type LogMsg struct {
	Job  int
	Line string
}

// StepMsg báo bước agent bắt đầu/kết thúc kèm đường dẫn log của bước.
type StepMsg struct {
	Job   int
	Event workflow.StepEvent
}

// DoneMsg báo việc kết thúc (luôn là thông điệp cuối của việc).
type DoneMsg struct {
	Job   int
	Value any
	Err   error
}

// DataMsg là thông điệp có kiểu do việc nền gửi cho màn hình sở hữu nó (vd. kế hoạch ô và kết quả từng lượt của
// `eval run`), để màn hình không phải phân tích chữ log (docs/tui-plan.md mục 7).
type DataMsg struct {
	Job   int
	Value any
}

// TailMsg nhắc đọc thêm đuôi log của bước đang chạy.
type TailMsg struct{ Job int }

// Bridge là đầu việc của một job: dịch vụ gọi Confirm/Ask/Log/OnStep ở goroutine của việc; Bridge gửi lên kênh
// rồi chờ trả lời hoặc huỷ. Huỷ context khi câu hỏi còn chờ thì hàm hỏi trả "không"/rỗng ngay.
type Bridge struct {
	Job int
	ctx context.Context
	out chan<- tea.Msg
}

// NewBridge dựng cầu cho việc id, gửi thông điệp vào out.
func NewBridge(ctx context.Context, id int, out chan<- tea.Msg) *Bridge {
	return &Bridge{Job: id, ctx: ctx, out: out}
}

func (b *Bridge) send(m tea.Msg) bool {
	select {
	case b.out <- m:
		return true
	case <-b.ctx.Done():
		return false
	}
}

func (b *Bridge) ask(p Prompt) (Answer, bool) {
	reply := make(chan Answer, 1)
	if !b.send(PromptMsg{Job: b.Job, Prompt: p, Reply: reply}) {
		return Answer{}, false
	}
	select {
	case a := <-reply:
		return a, true
	case <-b.ctx.Done():
		return Answer{}, false
	}
}

// ConfirmQuestion hỏi có/không câu hỏi của workflow; huỷ hoặc không thể hỏi thì trả false.
func (b *Bridge) ConfirmQuestion(q workflow.Question) bool {
	a, ok := b.ask(Prompt{Kind: Confirm, Key: q.Key, Text: q.Text, Params: q.Params, Paths: q.Paths})
	return ok && a.Yes
}

// AskQuestion hỏi câu trả lời tự do của workflow; huỷ thì trả rỗng.
func (b *Bridge) AskQuestion(q workflow.Question) string {
	a, ok := b.ask(Prompt{Kind: Ask, Key: q.Key, Text: q.Text, Params: q.Params, Paths: q.Paths})
	if !ok {
		return ""
	}
	return a.Text
}

// ConfirmRequest hỏi có/không một ConfirmRequest của dịch vụ (eval run, bootstrap publish...).
func (b *Bridge) ConfirmRequest(r app.ConfirmRequest) bool {
	a, ok := b.ask(Prompt{Kind: Confirm, Key: r.Key, Text: r.Text, Detail: r.Detail})
	return ok && a.Yes
}

// Log gửi một dòng tiến trình (bỏ khi việc đã bị huỷ).
func (b *Bridge) Log(line string) { b.send(LogMsg{Job: b.Job, Line: line}) }

// Send gửi một giá trị có kiểu về màn hình sở hữu việc (thành DataMsg; bỏ khi việc đã bị huỷ).
func (b *Bridge) Send(v any) { b.send(DataMsg{Job: b.Job, Value: v}) }

// OnStep báo bước agent bắt đầu/kết thúc.
func (b *Bridge) OnStep(ev workflow.StepEvent) { b.send(StepMsg{Job: b.Job, Event: ev}) }

// Callbacks dựng RunCallbacks của dịch vụ chạy workflow từ cầu.
func (b *Bridge) Callbacks() app.RunCallbacks {
	return app.RunCallbacks{Confirm: b.ConfirmQuestion, Ask: b.AskQuestion, Log: b.Log, OnStep: b.OnStep}
}

// Finish gửi DoneMsg (thông điệp cuối của việc). Không phụ thuộc huỷ context: việc bị huỷ vẫn phải báo xong để
// bảng việc dọn được. Gửi có hạn giờ để goroutine không treo mãi khi không còn ai đọc.
func (b *Bridge) Finish(v any, err error) {
	select {
	case b.out <- DoneMsg{Job: b.Job, Value: v, Err: err}:
	case <-time.After(10 * time.Second):
	}
}

// Wait là tea.Cmd đọc thông điệp kế tiếp của kênh việc nền; người gọi phải đăng ký lại sau mỗi thông điệp
// (chỉ một Wait chạy tại một thời điểm).
func Wait(ch <-chan tea.Msg) tea.Cmd { return func() tea.Msg { return <-ch } }

// ---- bảng việc ----

// Info mô tả một việc cho bảng việc nền (do hành động khai báo theo tham số).
type Info struct {
	Kind   string
	Label  string
	TaskID string
	// Resume là lệnh CLI tiếp tục việc nếu bị huỷ; rỗng nếu không tiếp tục được.
	Resume string
}

// Job là một việc nền đang chạy.
type Job struct {
	ID     int
	Action string // ID hành động (vd. "tasks.run")
	Kind   string
	Label  string // mô tả ngắn cho người ("b-do · 01M7…")
	TaskID string
	// Resume là lệnh CLI tiếp tục việc nếu bị huỷ ("bint run resume <id>"); rỗng nếu việc không tiếp tục được.
	Resume  string
	Cancel  context.CancelFunc
	Started time.Time
	// Opts là tham số của hành động (trả lại trong ResultMsg).
	Opts any
	// Ended và Err chỉ có ở việc đã xong (Table.Last).
	Ended time.Time
	Err   error

	// Log là các dòng của orchestrator; Tail là đuôi log của bước agent đang chạy.
	Log  *Lines
	Tail *Lines
	Step workflow.StepEvent // bước đang chạy (Step rỗng = chưa có)
	tail *tailer
}

// Table là bảng việc đang chạy; chỉ vòng Update đọc/ghi.
type Table struct {
	next int
	list []*Job
	done []*Job // vài việc đã xong gần nhất (giữ log để xem lại)
}

// maxDone là số việc đã xong được giữ lại.
const maxDone = 8

// Add thêm việc mới (ID tự tăng từ 1) và trả nó.
func (t *Table) Add(j Job) *Job {
	t.next++
	j.ID = t.next
	j.Started = time.Now()
	if j.Log == nil {
		j.Log = NewLines(2000)
	}
	if j.Tail == nil {
		j.Tail = NewLines(500)
	}
	p := &j
	t.list = append(t.list, p)
	return p
}

// Get tìm việc theo ID.
func (t *Table) Get(id int) *Job {
	for _, j := range t.list {
		if j.ID == id {
			return j
		}
	}
	return nil
}

// Remove bỏ việc khỏi bảng đang chạy (khi nhận DoneMsg) và giữ nó trong danh sách đã xong (kèm lỗi) để xem log.
func (t *Table) Remove(id int, err error) {
	for i, j := range t.list {
		if j.ID == id {
			t.list = append(t.list[:i], t.list[i+1:]...)
			j.Ended, j.Err, j.tail = time.Now(), err, nil
			if j.Kind != KindWorkflow && j.Kind != KindVerify && j.Kind != KindEvolve && j.Kind != KindEval {
				return // việc thường không giữ lại log
			}
			t.done = append(t.done, j)
			if len(t.done) > maxDone {
				t.done = t.done[len(t.done)-maxDone:]
			}
			return
		}
	}
}

// Last trả việc workflow/kiểm chứng đã xong gần nhất của tác vụ (nil nếu không có).
func (t *Table) Last(taskID string) *Job {
	for i := len(t.done) - 1; i >= 0; i-- {
		if t.done[i].TaskID == taskID {
			return t.done[i]
		}
	}
	return nil
}

// LastOfKind trả việc đã xong gần nhất cùng loại (nil nếu không có): màn Tiến hoá xem lại log việc vừa xong.
func (t *Table) LastOfKind(kind string) *Job {
	for i := len(t.done) - 1; i >= 0; i-- {
		if t.done[i].Kind == kind {
			return t.done[i]
		}
	}
	return nil
}

// All trả các việc đang chạy theo thứ tự bắt đầu.
func (t *Table) All() []*Job { return append([]*Job(nil), t.list...) }

// Len là số việc đang chạy.
func (t *Table) Len() int { return len(t.list) }

// Running tìm việc đang chạy của hành động action (nil nếu không có).
func (t *Table) Running(action string) *Job {
	for _, j := range t.list {
		if j.Action == action {
			return j
		}
	}
	return nil
}

// OfKind tìm việc đang chạy cùng loại (dùng cho khoá "mỗi lúc một việc workflow").
func (t *Table) OfKind(kind string) *Job {
	for _, j := range t.list {
		if j.Kind == kind {
			return j
		}
	}
	return nil
}

// ForTask tìm việc đang chạy của tác vụ (nil nếu không có); ưu tiên việc workflow.
func (t *Table) ForTask(id string) *Job {
	var found *Job
	for _, j := range t.list {
		if j.TaskID == id {
			if j.Kind == KindWorkflow {
				return j
			}
			found = j
		}
	}
	return found
}

// Unassigned trả việc workflow đang chạy chưa biết tác vụ của nó (chạy từ mô tả mới, trước bước đầu tiên).
func (t *Table) Unassigned() *Job {
	for _, j := range t.list {
		if j.Kind == KindWorkflow && j.TaskID == "" {
			return j
		}
	}
	return nil
}

// CancelAll huỷ mọi việc.
func (t *Table) CancelAll() {
	for _, j := range t.list {
		if j.Cancel != nil {
			j.Cancel()
		}
	}
}

// ---- dòng log giới hạn ----

// Lines là bộ đệm các dòng log, giữ tối đa Max dòng cuối.
type Lines struct {
	Max   int
	Lines []string
	// Dropped là số dòng cũ đã bị bỏ.
	Dropped int
}

// NewLines dựng bộ đệm giữ tối đa max dòng.
func NewLines(max int) *Lines { return &Lines{Max: max} }

// Add thêm dòng (bỏ dòng cũ nhất khi vượt giới hạn).
func (l *Lines) Add(lines ...string) {
	l.Lines = append(l.Lines, lines...)
	if over := len(l.Lines) - l.Max; l.Max > 0 && over > 0 {
		l.Lines = append([]string(nil), l.Lines[over:]...)
		l.Dropped += over
	}
}

// ---- đuôi log ----

// tailer đọc phần mới của một tệp log (ghi dần bởi tiến trình agent).
type tailer struct {
	path string
	off  int64
	part string // phần dòng chưa kết thúc
}

// maxTailRead giới hạn số byte đọc mỗi lần để log lớn không làm đứng giao diện.
const maxTailRead = 256 << 10

// StartTail bắt đầu đọc đuôi log của bước ev từ đầu tệp (xoá đuôi cũ).
func (j *Job) StartTail(ev workflow.StepEvent) {
	j.Step = ev
	j.Tail = NewLines(500)
	j.tail = &tailer{path: ev.Log}
	j.ReadTail()
}

// StopTail đọc nốt phần còn lại rồi dừng đuôi (bước đã kết thúc).
func (j *Job) StopTail() {
	j.ReadTail()
	if j.tail != nil && j.tail.part != "" {
		j.Tail.Add(j.tail.part)
	}
	j.tail = nil
}

// Tailing cho biết còn đuôi log đang theo dõi.
func (j *Job) Tailing() bool { return j.tail != nil }

// ReadTail đọc các byte mới của tệp log và thêm các dòng đã hoàn chỉnh vào Tail.
func (j *Job) ReadTail() {
	t := j.tail
	if t == nil || t.path == "" {
		return
	}
	f, err := os.Open(t.path)
	if err != nil {
		return // tệp chưa được tạo
	}
	defer f.Close()
	if fi, err := f.Stat(); err == nil && fi.Size() < t.off {
		t.off, t.part = 0, "" // tệp bị cắt/ghi lại
	}
	if _, err := f.Seek(t.off, io.SeekStart); err != nil {
		return
	}
	buf, _ := io.ReadAll(io.LimitReader(f, maxTailRead))
	t.off += int64(len(buf))
	if len(buf) == 0 {
		return
	}
	data := t.part + strings.ReplaceAll(string(buf), "\r\n", "\n")
	parts := strings.Split(data, "\n")
	t.part = parts[len(parts)-1]
	for _, l := range parts[:len(parts)-1] {
		j.Tail.Add(l)
	}
}

// TailTick là tea.Cmd gửi TailMsg sau every.
func TailTick(job int, every time.Duration) tea.Cmd {
	return tea.Tick(every, func(time.Time) tea.Msg { return TailMsg{Job: job} })
}
