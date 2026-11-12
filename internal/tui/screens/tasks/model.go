package tasks

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"bint/internal/app"
	"bint/internal/config"
	"bint/internal/i18n"
	"bint/internal/task"
	"bint/internal/tui/actions"
	"bint/internal/tui/components/confirm"
	"bint/internal/tui/components/form"
	"bint/internal/tui/components/input"
	"bint/internal/tui/components/list"
	"bint/internal/tui/components/logview"
	"bint/internal/tui/components/viewer"
	"bint/internal/tui/jobs"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

// WideW: từ chiều rộng này danh sách và chi tiết hiện cạnh nhau; hẹp hơn thì một khung (Tab/Enter đổi).
const WideW = 100

// Các thẻ con của khung chi tiết.
const (
	subInfo = iota
	subDocs
	subCriteria
	subVerify
	subReview
	subErrors
	subRuns
	subLog
	numSubs
)

var subNames = [numSubs]string{"info", "docs", "criteria", "verify", "review", "errors", "runs", "log"}

// docNames là các tài liệu của b-req theo thứ tự hiển thị.
var docNames = []string{"requirement", "plan", "tasks"}

type pane int

const (
	paneList pane = iota
	paneDetail
)

// Options điều khiển màn hình.
type Options struct {
	Dir      string
	ReadOnly bool
	// Loader/Detailer/Runs thay được trong kiểm thử; nil = nạp thật.
	Loader   Loader
	Detailer Detailer
	Runs     RunsLoader
	// Gate cho biết hành động có dùng được không theo sổ hành động và chế độ (nil = luôn dùng được).
	Gate func(id string) actions.Availability
	// Jobs là bảng việc nền của mô hình gốc (chỉ đọc): màn hình hiện log và biết việc nào đang chạy.
	Jobs *jobs.Table
}

// dialog là hộp xác nhận đang mở và việc làm khi người dùng đồng ý.
type dialog struct {
	c     confirm.Model
	onYes func(value string) tea.Cmd // nil: hộp chỉ để đọc, Enter/Esc đóng
}

// row là một hàng của danh sách: tiêu đề nhóm (t == nil) hoặc một tác vụ.
type row struct {
	group int
	t     *task.Task
}

// Model là trạng thái màn Tác vụ.
type Model struct {
	o   Options
	cat *i18n.Catalog
	rw  ui.Rewriter
	th  theme.Theme

	Data        Data
	Loaded      bool
	Loading     bool
	reloadAgain bool
	Err         error

	// showAll: hiện cả done/cancelled (như `task list --all`); filter là chữ lọc nhanh `/`.
	showAll bool
	filter  string
	rows    []row
	lst     list.Model
	pane    pane
	// focusID: tác vụ cần chọn khi dữ liệu nạp xong (từ hộp thư).
	focusID string

	sub    int
	doc    int // tài liệu đang xem ở thẻ Tài liệu
	detail viewer.Model
	// detailKey nhận diện nội dung đang hiện (tác vụ, thẻ) để đặt lại cuộn khi đổi.
	detailKey string
	log       logview.Model
	// logTotal, logH là số dòng và chiều cao của khung log lần vẽ gần nhất (cho cuộn).
	logTotal, logH int
	find           int // phát hiện đang chọn ở thẻ Review

	cache    map[string]Detail
	cacheErr map[string]error
	asked    map[string]bool
	runs     map[string]app.TaskRunsResult
	runsErr  map[string]error
	runsAsk  map[string]bool

	dlg      *dialog
	frm      *form.Model
	frmID    string
	frmBusy  bool
	textIn   bool
	textOld  string
	toast    string
	toastErr bool

	// w, h là kích thước vùng nội dung lần vẽ gần nhất.
	w, h int
}

// New dựng màn hình; gọi SetEnv trước khi vẽ.
func New(o Options) *Model {
	if o.Loader == nil {
		ro := o.ReadOnly
		o.Loader = func(ctx context.Context, dir string, _ bool) (Data, error) { return Load(ctx, dir, ro) }
	}
	if o.Detailer == nil {
		o.Detailer = LoadDetail
	}
	if o.Runs == nil {
		o.Runs = LoadRuns
	}
	if o.Jobs == nil {
		o.Jobs = &jobs.Table{}
	}
	m := &Model{o: o, log: logview.New(), cache: map[string]Detail{}, cacheErr: map[string]error{}, asked: map[string]bool{},
		runs: map[string]app.TaskRunsResult{}, runsErr: map[string]error{}, runsAsk: map[string]bool{}}
	return m
}

// SetEnv đặt danh mục chuỗi và bảng màu (gọi lại khi đổi ngôn ngữ).
func (m *Model) SetEnv(th theme.Theme, rw ui.Rewriter) { m.th, m.rw, m.cat = th, rw, rw.Cat }

func (m *Model) t(key string, a ...i18n.A) string { return m.cat.T(key, a...) }

// allowed kiểm sổ hành động cho id; không được thì báo bằng thông báo ngắn.
func (m *Model) allowed(id string) bool {
	if m.o.Gate == nil {
		return true
	}
	switch m.o.Gate(id) {
	case actions.Locked:
		m.setToast(m.t("ui.locked.agent"), true)
		return false
	case actions.Hidden:
		m.setToast(m.t("tk.denied.readonly"), true)
		return false
	}
	return true
}

func (m *Model) setToast(s string, isErr bool) { m.toast, m.toastErr = s, isErr }

func request(id string, opts any) tea.Cmd {
	return func() tea.Msg { return actions.RequestMsg{ID: id, Opts: opts} }
}

// ---- nạp dữ liệu ----

// Ensure nạp danh sách lần đầu (ui.Screen).
func (m *Model) Ensure() tea.Cmd {
	if m.Loaded || m.Loading {
		return nil
	}
	return m.Load()
}

// Load nạp lại danh sách (không chạy song song: đang nạp thì nạp thêm một lần khi xong).
func (m *Model) Load() tea.Cmd {
	if m.Loading {
		m.reloadAgain = true
		return nil
	}
	m.Loading = true
	ctx, dir, loader, ro := context.Background(), m.o.Dir, m.o.Loader, m.o.ReadOnly
	return func() tea.Msg {
		d, err := loader(ctx, dir, ro)
		return LoadedMsg{Data: d, Err: err}
	}
}

// Focus chọn tác vụ id (từ hộp thư): bỏ lọc, về danh sách; nạp lại nếu cần.
func (m *Model) Focus(id string) tea.Cmd {
	m.focusID, m.filter, m.sub, m.pane = id, "", subInfo, paneList
	m.dlg, m.frm, m.textIn = nil, nil, false
	if m.selectID(id) {
		m.focusID = ""
		return tea.Batch(m.Load(), m.wantDetail())
	}
	return m.Load()
}

// selectID đưa con trỏ tới tác vụ id (bật hiện tất cả nếu nó đã xong/huỷ); false nếu chưa có trong dữ liệu.
func (m *Model) selectID(id string) bool {
	for _, t := range m.Data.Tasks {
		if t.State.ID != id {
			continue
		}
		if GroupOf(t.State.Status) == groupFinished {
			m.showAll = true
		}
		m.rebuild()
		for i, r := range m.rows {
			if r.t != nil && r.t.State.ID == id {
				m.lst.MoveTo(i)
				return true
			}
		}
	}
	return false
}

// rebuild dựng lại các hàng (tiêu đề nhóm + tác vụ) theo bộ lọc, giữ con trỏ theo ID.
func (m *Model) rebuild() {
	keep := ""
	if t := m.Cursor(); t != nil {
		keep = t.State.ID
	}
	var ts []*task.Task
	f := strings.ToLower(strings.TrimSpace(m.filter))
	for _, t := range m.Data.Tasks {
		if !m.showAll && GroupOf(t.State.Status) == groupFinished {
			continue
		}
		if f != "" && !strings.Contains(strings.ToLower(t.State.ID+" "+t.State.Status+" "+t.State.Workflow+" "+t.Ticket.Goal), f) {
			continue
		}
		ts = append(ts, t)
	}
	sortTasks(ts)
	m.rows = m.rows[:0]
	last := -1
	for _, t := range ts {
		if g := GroupOf(t.State.Status); g != last {
			m.rows = append(m.rows, row{group: g})
			last = g
		}
		m.rows = append(m.rows, row{group: last, t: t})
	}
	m.lst.SetN(len(m.rows))
	idx := -1 // con trỏ giữ tác vụ đang chọn, không thì về tác vụ đầu tiên (không phải tiêu đề nhóm)
	for i, r := range m.rows {
		if r.t == nil {
			continue
		}
		if idx < 0 {
			idx = i
		}
		if keep != "" && r.t.State.ID == keep {
			idx = i
			break
		}
	}
	if idx >= 0 {
		m.lst.MoveTo(idx)
	}
}

// Cursor trả tác vụ dưới con trỏ (nil nếu danh sách trống).
func (m *Model) Cursor() *task.Task {
	if m.lst.Cursor < 0 || m.lst.Cursor >= len(m.rows) {
		return nil
	}
	return m.rows[m.lst.Cursor].t
}

// Visible trả các tác vụ đang hiện theo thứ tự danh sách.
func (m *Model) Visible() []*task.Task {
	var out []*task.Task
	for _, r := range m.rows {
		if r.t != nil {
			out = append(out, r.t)
		}
	}
	return out
}

// ShowAll cho biết đang hiện cả done/cancelled.
func (m *Model) ShowAll() bool { return m.showAll }

// move dời con trỏ d hàng, bỏ qua tiêu đề nhóm.
func (m *Model) move(d int) {
	if d == 0 || len(m.rows) == 0 {
		return
	}
	step := 1
	if d < 0 {
		step = -1
	}
	cur := m.lst.Cursor
	for n := 0; n != d; n += step {
		next := cur + step
		for next >= 0 && next < len(m.rows) && m.rows[next].t == nil {
			next += step
		}
		if next < 0 || next >= len(m.rows) {
			break
		}
		cur = next
	}
	m.lst.MoveTo(cur)
}

func (m *Model) home() {
	for i, r := range m.rows {
		if r.t != nil {
			m.lst.MoveTo(i)
			m.lst.Top = 0
			return
		}
	}
}

func (m *Model) end() {
	for i := len(m.rows) - 1; i >= 0; i-- {
		if m.rows[i].t != nil {
			m.lst.MoveTo(i)
			return
		}
	}
}

// ---- trạng thái màn hình (ui.Screen) ----

// Capturing: màn hình đang nhận mọi phím chữ (ô lọc, form, hộp thoại).
func (m *Model) Capturing() bool { return m.dlg != nil || m.frm != nil || m.textIn }

// NotList: màn Tác vụ luôn giữ danh sách; thẻ con nằm trong khung chi tiết.
func (m *Model) NotList() bool { return false }

// Busy: có việc nền của màn hình đang chạy (nạp, hoặc form đang chờ kết quả).
func (m *Model) Busy() bool { return m.Loading || m.frmBusy }

// jobHere trả việc nền đang chạy của tác vụ dưới con trỏ; việc workflow chạy từ mô tả mới chưa biết tác vụ của
// nó thì coi như của tác vụ đang chọn (để xem log và huỷ được).
func (m *Model) jobHere() *jobs.Job {
	t := m.Cursor()
	if t == nil {
		return nil
	}
	if j := m.o.Jobs.ForTask(t.State.ID); j != nil {
		return j
	}
	return m.o.Jobs.Unassigned()
}

// WantsInterrupt: đang xem log của một việc đang chạy; Esc/Ctrl+C huỷ việc đó thay vì lùi/thoát.
func (m *Model) WantsInterrupt() bool { return m.sub == subLog && m.jobHere() != nil && !m.Capturing() }

// wantDetail yêu cầu nạp chi tiết (và lượt chạy khi đang ở thẻ đó) của tác vụ dưới con trỏ nếu chưa có.
func (m *Model) wantDetail() tea.Cmd {
	t := m.Cursor()
	if t == nil {
		return nil
	}
	id := t.State.ID
	var cmds []tea.Cmd
	if !m.asked[id] {
		m.asked[id] = true
		ctx, dir, det, ro := context.Background(), m.o.Dir, m.o.Detailer, m.o.ReadOnly
		cmds = append(cmds, func() tea.Msg {
			d, err := det(ctx, dir, id, ro)
			return DetailMsg{ID: id, Detail: d, Err: err}
		})
	}
	if m.sub == subRuns && !m.runsAsk[id] {
		m.runsAsk[id] = true
		ctx, dir, rl, ro := context.Background(), m.o.Dir, m.o.Runs, m.o.ReadOnly
		cmds = append(cmds, func() tea.Msg {
			r, err := rl(ctx, dir, id, ro)
			return RunsMsg{ID: id, Runs: r, Err: err}
		})
	}
	return tea.Batch(cmds...)
}

// invalidate buộc nạp lại chi tiết và lượt chạy (giữ nội dung cũ hiện tới khi có bản mới).
func (m *Model) invalidate() {
	m.asked, m.runsAsk = map[string]bool{}, map[string]bool{}
}

// Update xử lý thông điệp không phải phím; trả lệnh kế tiếp.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case LoadedMsg:
		m.Loading = false
		m.Err = msg.Err
		var cmds []tea.Cmd
		if msg.Err == nil {
			m.Data, m.Loaded = msg.Data, true
			m.rebuild()
			if m.focusID != "" && m.selectID(m.focusID) {
				m.focusID = ""
			}
			m.invalidate()
			cmds = append(cmds, m.wantDetail())
			if m.frm != nil {
				m.suggest(m.frm)
			}
		}
		if m.reloadAgain {
			m.reloadAgain = false
			cmds = append(cmds, m.Load())
		}
		return tea.Batch(cmds...)
	case DetailMsg:
		if msg.Err != nil {
			m.cacheErr[msg.ID] = msg.Err
		} else {
			m.cache[msg.ID] = msg.Detail
			delete(m.cacheErr, msg.ID)
		}
	case RunsMsg:
		if msg.Err != nil {
			m.runsErr[msg.ID] = msg.Err
		} else {
			m.runs[msg.ID] = msg.Runs
			delete(m.runsErr, msg.ID)
		}
	case RefreshMsg:
		return m.Load()
	case actions.DeniedMsg:
		why := m.t(map[bool]string{true: "ui.locked.agent", false: "tk.denied.readonly"}[msg.Locked])
		m.setToast(why, true)
		if m.frmBusy && m.frm != nil {
			m.frmBusy = false
			m.frm.Err = why
		}
	case actions.ResultMsg:
		return m.onResult(msg)
	}
	return nil
}

// suggest cấp gợi ý (tệp, tác vụ, phát hiện) cho form đang mở.
func (m *Model) suggest(f *form.Model) {
	f.SetSuggestions("files", m.Data.Files)
	f.SetSuggestions("tasks", m.Data.IDs())
	var ids []string
	for _, d := range m.cache {
		if d.Review != nil {
			for _, r := range d.Review.Rounds {
				for _, fd := range r.Findings {
					ids = append(ids, fd.ID)
				}
			}
		}
	}
	f.SetSuggestions("findings", ids)
}

// ---- phím ----

// Key xử lý một phím; handled = false nghĩa là màn hình không dùng phím này.
func (m *Model) Key(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch {
	case m.dlg != nil:
		return m.dialogKey(msg), true
	case m.frm != nil:
		return m.formKey(msg), true
	case m.textIn:
		return m.textKey(msg), true
	}
	m.toast = ""
	s := msg.String()
	if s == "esc" || s == "ctrl+c" {
		if j := m.jobHere(); j != nil && m.sub == subLog {
			action := j.Action
			return func() tea.Msg { return actions.CancelMsg{ID: action} }, true
		}
	}
	switch s {
	case ".":
		m.setSub(m.nextSub(1))
		return m.wantDetail(), true
	case ",":
		m.setSub(m.nextSub(-1))
		return m.wantDetail(), true
	}
	return m.listKey(msg)
}

// visibleSubs là các thẻ con đang hiện: thẻ Log chỉ khi tác vụ có việc nền (đang chạy hoặc vừa xong).
func (m *Model) visibleSubs() []int {
	out := make([]int, 0, numSubs)
	for i := 0; i < numSubs; i++ {
		if i == subLog && !m.hasLog() {
			continue
		}
		out = append(out, i)
	}
	return out
}

// hasLog: tác vụ dưới con trỏ có log việc nền để xem.
func (m *Model) hasLog() bool {
	t := m.Cursor()
	return t != nil && (m.jobHere() != nil || m.o.Jobs.Last(t.State.ID) != nil)
}

func (m *Model) nextSub(d int) int {
	vs := m.visibleSubs()
	cur := 0
	for i, s := range vs {
		if s == m.sub {
			cur = i
		}
	}
	return vs[(cur+d+len(vs))%len(vs)]
}

func (m *Model) setSub(i int) {
	m.sub = max(0, min(i, numSubs-1))
	m.detail.Reset()
}

// ShowLog chuyển sang thẻ Log của tác vụ dưới con trỏ (khi vừa bắt đầu một việc nền).
func (m *Model) ShowLog() {
	m.sub, m.pane = subLog, paneDetail
	m.log = logview.New()
}

func (m *Model) listKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	s := msg.String()
	t := m.Cursor()
	switch s {
	case "j", "down":
		return m.nav(1), true
	case "k", "up":
		return m.nav(-1), true
	case "pgdown", "ctrl+d":
		return m.page(1), true
	case "pgup", "ctrl+u":
		return m.page(-1), true
	case "g", "home":
		if m.pane == paneDetail {
			m.detail.Home()
			return nil, true
		}
		m.home()
		return m.wantDetail(), true
	case "G", "end":
		if m.pane == paneDetail {
			m.detail.End()
			return nil, true
		}
		m.end()
		return m.wantDetail(), true
	case "tab":
		m.pane = (m.pane + 1) % 2
		return nil, true
	case "enter":
		if t == nil {
			return nil, true
		}
		if m.pane == paneDetail && m.sub == subReview {
			return m.openExcerpt(), true
		}
		m.pane = paneDetail
		return nil, true
	case "esc":
		switch {
		case m.pane == paneDetail:
			m.pane = paneList
		case m.filter != "":
			m.filter = ""
			m.rebuild()
		}
		return m.wantDetail(), true
	case "/":
		m.textIn, m.textOld = true, m.filter
		return nil, true
	case "A":
		m.showAll = !m.showAll
		m.rebuild()
		return m.wantDetail(), true
	case "ctrl+r", "f5":
		m.invalidate()
		return tea.Batch(m.Load(), m.wantDetail()), true
	case "d":
		if m.sub == subDocs {
			m.doc = (m.doc + 1) % len(docNames)
			m.detail.Reset()
			return nil, true
		}
	case "n":
		return m.cmdNew(), true
	case "r":
		return m.cmdRun(), true
	case "c":
		return m.cmdResume(), true
	case "a":
		return m.cmdApprove(), true
	case "s":
		return m.cmdStatus(), true
	case "v":
		return m.cmdVerify(), true
	case "l":
		return m.cmdLint(), true
	case "T":
		m.showTransitions()
		return nil, true
	case "e":
		return m.cmdEdit(), true
	case "i":
		return m.cmdError(), true
	case "m":
		return m.cmdCriteria(), true
	case "w":
		return m.cmdReviewEdit(), true
	case "K":
		return m.cmdKnowledge(), true
	}
	return nil, false
}

// nav dời con trỏ danh sách, hoặc (khung chi tiết đang chọn) cuộn nội dung / dời phát hiện ở thẻ Review.
func (m *Model) nav(d int) tea.Cmd {
	if m.pane == paneDetail {
		switch m.sub {
		case subReview:
			m.moveFinding(d)
		case subLog:
			m.scrollLog(d)
		default:
			m.detail.Scroll(d)
		}
		return nil
	}
	m.move(d)
	return m.wantDetail()
}

func (m *Model) page(d int) tea.Cmd {
	if m.pane == paneDetail {
		if m.sub == subLog {
			m.scrollLog(d * max(m.h-4, 4))
			return nil
		}
		m.detail.Page(d)
		return nil
	}
	m.lst.Page(d)
	if r := m.lst.Cursor; r >= 0 && r < len(m.rows) && m.rows[r].t == nil { // rơi vào tiêu đề nhóm
		m.move(map[bool]int{true: 1, false: -1}[d > 0])
	}
	return m.wantDetail()
}

func (m *Model) textKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.filter, m.textIn = m.textOld, false
	case "enter":
		m.textIn = false
	default:
		in := input.New(m.filter)
		in.Update(msg)
		m.filter = in.Text
	}
	m.rebuild()
	return m.wantDetail()
}

// ---- hộp thoại ----

func (m *Model) dialogKey(msg tea.KeyPressMsg) tea.Cmd {
	d := m.dlg
	switch d.c.Update(msg) {
	case confirm.Yes:
		m.dlg = nil
		if d.onYes != nil {
			return d.onYes(d.c.Value())
		}
	case confirm.No:
		m.dlg = nil
	}
	return nil
}

func (m *Model) openInfo(title string, body []string) {
	m.dlg = &dialog{c: confirm.Model{Title: title, Body: body, YesLabel: m.t("tk.dialog.close"), NoLabel: m.t("tk.dialog.close")}}
}

// ---- form ----

// openForm mở form của hành động id (nhãn theo khoá form.<id>.<trường>).
func (m *Model) openForm(id, cli string, defaults any, setup func(*form.Model)) {
	f := form.New(id, cli, defaults)
	m.suggest(&f)
	if setup != nil {
		setup(&f)
	}
	m.frm, m.frmID, m.frmBusy = &f, id, false
}

func (m *Model) formKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.frmBusy {
		if k := msg.String(); k == "esc" || k == "ctrl+c" {
			m.frm, m.frmBusy = nil, false
		}
		return nil
	}
	switch m.frm.Update(msg) {
	case form.Cancel:
		m.frm = nil
	case form.Copy:
		return tea.SetClipboard(m.frm.CLI())
	case form.Submit:
		return m.submitForm(m.frm.Value())
	}
	return nil
}

// submitForm gửi giá trị form đã qua Validate: việc nền (run, resume) đóng form và chuyển sang thẻ Log; còn lại
// giữ form với dấu "đang chạy" tới khi có kết quả.
func (m *Model) submitForm(v any) tea.Cmd {
	id := m.frmID
	switch o := v.(type) {
	case app.RunWorkflowOptions:
		if strings.TrimSpace(o.Task) == "" && strings.TrimSpace(o.Request) == "" {
			m.frm.Err = m.t("tk.run.need_target")
			return nil
		}
		m.frm = nil
		if o.Task != "" {
			m.selectID(o.Task)
		}
		m.ShowLog()
		return request(id, o)
	case app.RunResumeOptions:
		m.frm = nil
		m.ShowLog()
		return request(id, o)
	case app.TaskStatusOptions:
		if o.Force {
			m.confirmForce(o)
			return nil
		}
	}
	m.frmBusy = true
	return request(id, v)
}

// confirmForce mở hộp đỏ cảnh báo trước khi người ép chuyển trạng thái (--force bỏ qua bằng chứng của done).
func (m *Model) confirmForce(o app.TaskStatusOptions) {
	body := []string{m.th.S(theme.Err, m.t("tk.force.warn")), ""}
	if d, ok := m.cache[o.Ref]; ok && o.To == task.StatusDone {
		if len(d.Blockers) == 0 {
			body = append(body, m.t("tk.force.no_blockers"))
		} else {
			body = append(body, m.th.S(theme.Warn, m.t("tk.force.blockers")))
			for _, b := range d.Blockers {
				body = append(body, "  - "+m.cli(b))
			}
		}
	}
	c := confirm.Model{Title: m.t("tk.force.title"), Body: body, Danger: true, YesLabel: m.t("tk.force.yes"), NoLabel: m.t("tk.dialog.cancel")}
	m.dlg = &dialog{c: c, onYes: func(string) tea.Cmd {
		m.frmBusy = true
		return request("tasks.status", o)
	}}
}

// ---- lệnh (phím và bảng lệnh) ----

// cmdNew mở form tạo tác vụ.
func (m *Model) cmdNew() tea.Cmd {
	if m.allowed("tasks.new") {
		m.openForm("tasks.new", "task new", app.TaskNewDefaults(config.Default()), nil)
	}
	return nil
}

// cmdRun mở form chạy workflow: trên tác vụ đang chọn, hoặc từ mô tả mới nếu không có tác vụ nào.
func (m *Model) cmdRun() tea.Cmd {
	if !m.allowed("tasks.run") {
		return nil
	}
	o := app.RunWorkflowDefaults(config.Default())
	o.Workflow = "b-do"
	if t := m.Cursor(); t != nil {
		o.Task = t.State.ID
		if wf := t.State.Workflow; wf != "" {
			o.Workflow = wf
		}
	}
	m.openForm("tasks.run", "run", o, nil)
	return nil
}

// cmdResume mở form tiếp tục tác vụ đang chọn (chỉ khi nó đang tạm dừng).
func (m *Model) cmdResume() tea.Cmd {
	t := m.Cursor()
	if t == nil || !m.allowed("tasks.resume") {
		return nil
	}
	if !task.Paused(t.State.Status) {
		m.setToast(m.t("tk.resume.not_paused", i18n.A{"status": t.State.Status}), true)
		return nil
	}
	o := app.RunResumeDefaults(config.Default())
	o.Ref = t.State.ID
	m.openForm("tasks.resume", "run resume", o, nil)
	return nil
}

// cmdApprove tính điều sẽ được duyệt (chỉ đọc) rồi mở hộp xác nhận khi có kết quả.
func (m *Model) cmdApprove() tea.Cmd {
	t := m.Cursor()
	if t == nil || !m.allowed("tasks.approve") {
		return nil
	}
	return request("tasks.plan_approve", app.TaskApproveOptions{Ref: t.State.ID})
}

// cmdStatus mở form đổi trạng thái, chỉ đưa ra các trạng thái đích hợp lệ theo bảng chuyển.
func (m *Model) cmdStatus() tea.Cmd {
	t := m.Cursor()
	if t == nil || !m.allowed("tasks.status") {
		return nil
	}
	targets := task.TransitionsFrom(t.State.Status)
	if len(targets) == 0 {
		m.setToast(m.t("tk.status.none", i18n.A{"status": t.State.Status}), true)
		return nil
	}
	o := app.TaskStatusDefaults(config.Default())
	o.Ref = t.State.ID
	m.openForm("tasks.status", "task status", o, func(f *form.Model) { f.SetChoices("status", targets) })
	return nil
}

// cli viết lại đoạn nhắc lệnh CLI trong thông điệp của tầng dịch vụ thành phím/hành động TUI (ui.Rewrite).
func (m *Model) cli(s string) string { return m.rw.Rewrite(s) }
