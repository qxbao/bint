package evolve

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"bint/internal/app/evolvesvc"
	"bint/internal/config"
	"bint/internal/evolve"
	"bint/internal/i18n"
	"bint/internal/tui/actions"
	"bint/internal/tui/components/confirm"
	"bint/internal/tui/components/form"
	"bint/internal/tui/components/list"
	"bint/internal/tui/components/logview"
	"bint/internal/tui/components/viewer"
	"bint/internal/tui/jobs"
	"bint/internal/tui/theme"
)

// WideW: từ chiều rộng này danh sách và chi tiết hiện cạnh nhau; hẹp hơn thì một khung (Tab/Enter đổi).
const WideW = 100

// Các thẻ của màn hình. Thẻ Log chỉ hiện khi có việc nền của Tiến hoá (đang chạy hoặc vừa xong).
const (
	tabOpps = iota
	tabProposals
	tabHistory
	tabQueue
	tabLog
	numTabs
)

var tabNames = [numTabs]string{"opps", "proposals", "history", "queue", "log"}

// Các thẻ con của chi tiết đề xuất.
const (
	subInfo = iota
	subChanges
	subEvals
	numSubs
)

var subNames = [numSubs]string{"info", "changes", "evals"}

type pane int

const (
	paneList pane = iota
	paneDetail
)

// Options điều khiển màn hình.
type Options struct {
	Dir      string
	ReadOnly bool
	// Loader/Detailer thay được trong kiểm thử; nil = nạp thật.
	Loader   Loader
	Detailer Detailer
	// Gate cho biết hành động có dùng được không theo sổ hành động và chế độ (nil = luôn dùng được).
	Gate func(id string) actions.Availability
	// Jobs là bảng việc nền của mô hình gốc (chỉ đọc): màn hình hiện log và biết việc nào đang chạy.
	Jobs *jobs.Table
}

// dialog là hộp xác nhận đang mở và việc làm khi người dùng đồng ý.
type dialog struct {
	c     confirm.Model
	onYes func(d *confirm.Model) tea.Cmd // nil: hộp chỉ để đọc, Enter/Esc đóng
}

// Model là trạng thái màn Tiến hoá.
type Model struct {
	o   Options
	cat *i18n.Catalog
	th  theme.Theme

	Data        Data
	Loaded      bool
	Loading     bool
	reloadAgain bool
	Err         error

	tab      int
	all      bool // đề xuất: mọi trạng thái (`evolve list --all`)
	queueAll bool // hàng đợi: cả yêu cầu đã xử lý (`evolve queue --all`)
	olst     list.Model
	plst     list.Model
	pane     pane
	focusID  string
	sub      int

	detail    viewer.Model
	detailKey string
	hist      viewer.Model
	queue     viewer.Model
	log       logview.Model
	logTotal  int
	logH      int

	cache    map[string]*evolvesvc.ShowResult
	cacheErr map[string]error
	asked    map[string]bool

	dlg      *dialog
	frm      *form.Model
	frmID    string
	frmBusy  bool
	toast    string
	toastErr bool

	w, h int
}

// New dựng màn hình; gọi SetEnv trước khi vẽ.
func New(o Options) *Model {
	if o.Loader == nil {
		o.Loader = Load
	}
	if o.Detailer == nil {
		o.Detailer = LoadDetail
	}
	if o.Jobs == nil {
		o.Jobs = &jobs.Table{}
	}
	return &Model{o: o, log: logview.New(), cache: map[string]*evolvesvc.ShowResult{}, cacheErr: map[string]error{}, asked: map[string]bool{}}
}

// SetEnv đặt danh mục chuỗi và bảng màu (gọi lại khi đổi ngôn ngữ).
func (m *Model) SetEnv(th theme.Theme, cat *i18n.Catalog) { m.th, m.cat = th, cat }

func (m *Model) t(key string, a ...i18n.A) string { return m.cat.T(key, a...) }

func (m *Model) allowed(id string) bool {
	if m.o.Gate == nil {
		return true
	}
	switch m.o.Gate(id) {
	case actions.Locked:
		m.setToast(m.t("ui.locked.agent"), true)
		return false
	case actions.Hidden:
		m.setToast(m.t("ev.denied.readonly"), true)
		return false
	}
	return true
}

func (m *Model) setToast(s string, isErr bool) { m.toast, m.toastErr = s, isErr }

func request(id string, opts any) tea.Cmd {
	return func() tea.Msg { return actions.RequestMsg{ID: id, Opts: opts} }
}

// ---- nạp dữ liệu ----

// Ensure nạp dữ liệu lần đầu (ui.Screen).
func (m *Model) Ensure() tea.Cmd {
	if m.Loaded || m.Loading {
		return nil
	}
	return m.Load()
}

// Load nạp lại dữ liệu (không chạy song song: đang nạp thì nạp thêm một lần khi xong).
func (m *Model) Load() tea.Cmd {
	if m.Loading {
		m.reloadAgain = true
		return nil
	}
	m.Loading = true
	ctx, dir, loader, ro, q := context.Background(), m.o.Dir, m.o.Loader, m.o.ReadOnly, Query{All: m.all, QueueAll: m.queueAll}
	return func() tea.Msg {
		d, err := loader(ctx, dir, q, ro)
		return LoadedMsg{Data: d, Err: err}
	}
}

// Focus mở thẻ Đề xuất tại đề xuất id (từ hộp thư): về danh sách; hiện mọi trạng thái nếu đề xuất không còn chờ.
func (m *Model) Focus(id string) tea.Cmd {
	m.focusID, m.tab, m.pane, m.sub = id, tabProposals, paneList, subInfo
	m.dlg, m.frm = nil, nil
	if m.Loaded && m.selectID(id) {
		m.focusID = ""
		return m.wantDetail()
	}
	if m.Loaded && !m.all {
		m.all = true // đề xuất có thể không còn ở trạng thái chờ: nạp lại với mọi trạng thái
	}
	// Đang nạp thì Load chỉ hẹn nạp thêm một lần khi xong (với m.all mới); không chạy hai lần nạp song song,
	// nên kết quả cũ không ghi đè được kết quả mới.
	return m.Load()
}

// selectID đưa con trỏ đề xuất tới id; false nếu chưa có trong dữ liệu.
func (m *Model) selectID(id string) bool {
	for i, r := range m.Data.Rows {
		if r.Proposal.ID == id {
			m.plst.MoveTo(i)
			return true
		}
	}
	return false
}

// Cursor trả đề xuất dưới con trỏ (nil nếu danh sách trống).
func (m *Model) Cursor() *evolvesvc.ListRow {
	if m.plst.Cursor < 0 || m.plst.Cursor >= len(m.Data.Rows) {
		return nil
	}
	return &m.Data.Rows[m.plst.Cursor]
}

// pickedProposal trả đề xuất dưới con trỏ khi thẻ Đề xuất đang hiện. Ở thẻ khác, con trỏ đề xuất bị ẩn nên
// không thao tác lên nó: chuyển sang thẻ Đề xuất và nhắc chọn rồi bấm lại.
func (m *Model) pickedProposal() (*evolvesvc.ListRow, tea.Cmd) {
	if m.tab != tabProposals {
		cmd := m.setTab(tabProposals)
		m.setToast(m.t("ev.need_proposal"), true)
		return nil, cmd
	}
	r := m.Cursor()
	if r == nil {
		m.setToast(m.t("ev.need_proposal"), true)
	}
	return r, nil
}

// CursorOpportunity trả cơ hội dưới con trỏ ở thẻ Cơ hội (nil nếu không có).
func (m *Model) CursorOpportunity() *evolve.Opportunity {
	ops := m.Data.Opportunities()
	if m.olst.Cursor < 0 || m.olst.Cursor >= len(ops) {
		return nil
	}
	return &ops[m.olst.Cursor]
}

// Dialog mô tả hộp thoại đang mở (cho kiểm thử): Open, Danger (hộp đỏ), số ô chọn, và ô nào đang được chọn.
func (m *Model) Dialog() (open, danger bool, checks []bool) {
	if m.dlg == nil {
		return false, false, nil
	}
	out := make([]bool, len(m.dlg.c.Checks))
	for i := range out {
		out[i] = m.dlg.c.Checked(i)
	}
	return true, m.dlg.c.Danger, out
}

// Tab là thẻ đang hiện (0 Cơ hội, 1 Đề xuất, 2 Lịch sử, 3 Hàng đợi, 4 Log).
func (m *Model) Tab() int { return m.tab }

// ShowAll cho biết thẻ Đề xuất đang hiện mọi trạng thái.
func (m *Model) ShowAll() bool { return m.all }

// ---- trạng thái màn hình (ui.Screen) ----

// Capturing: màn hình đang nhận mọi phím chữ (form, hộp thoại).
func (m *Model) Capturing() bool { return m.dlg != nil || m.frm != nil }

// NotList: màn Tiến hoá giữ chân phím chung ở mọi thẻ.
func (m *Model) NotList() bool { return false }

// Busy: có việc nền của màn hình đang chạy (nạp, hoặc form đang chờ kết quả, hoặc việc Tiến hoá nặng).
func (m *Model) Busy() bool { return m.Loading || m.frmBusy || m.job() != nil }

// job trả việc Tiến hoá nặng đang chạy (nil nếu không có).
func (m *Model) job() *jobs.Job { return m.o.Jobs.OfKind(jobs.KindEvolve) }

// lastJob trả việc đang chạy, không thì việc vừa xong (để xem log).
func (m *Model) lastJob() *jobs.Job {
	if j := m.job(); j != nil {
		return j
	}
	return m.o.Jobs.LastOfKind(jobs.KindEvolve)
}

// WantsInterrupt: đang xem log của việc đang chạy; Esc/Ctrl+C huỷ việc đó thay vì lùi/thoát.
func (m *Model) WantsInterrupt() bool { return m.tab == tabLog && m.job() != nil && !m.Capturing() }

func (m *Model) wantDetail() tea.Cmd {
	r := m.Cursor()
	if r == nil || m.tab != tabProposals {
		return nil
	}
	id := r.Proposal.ID
	if m.asked[id] {
		return nil
	}
	m.asked[id] = true
	ctx, dir, det, ro := context.Background(), m.o.Dir, m.o.Detailer, m.o.ReadOnly
	return func() tea.Msg {
		d, err := det(ctx, dir, id, ro)
		return DetailMsg{ID: id, Detail: d, Err: err}
	}
}

func (m *Model) invalidate() { m.asked = map[string]bool{} }

// visibleTabs là các thẻ đang hiện: Log chỉ khi có việc nền (đang chạy hoặc vừa xong).
func (m *Model) visibleTabs() []int {
	out := make([]int, 0, numTabs)
	for i := 0; i < numTabs; i++ {
		if i == tabLog && m.lastJob() == nil {
			continue
		}
		out = append(out, i)
	}
	return out
}

func (m *Model) nextTab(d int) int {
	vs := m.visibleTabs()
	cur := 0
	for i, s := range vs {
		if s == m.tab {
			cur = i
		}
	}
	return vs[(cur+d+len(vs))%len(vs)]
}

func (m *Model) setTab(i int) tea.Cmd {
	m.tab, m.pane = i, paneList
	m.detail.Reset()
	return m.wantDetail()
}

// ShowLog chuyển sang thẻ Log (khi vừa bắt đầu một việc nền).
func (m *Model) ShowLog() {
	m.tab, m.pane = tabLog, paneDetail
	m.log = logview.New()
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
			m.olst.SetN(len(m.Data.Opportunities()))
			m.plst.SetN(len(m.Data.Rows))
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
	case RefreshMsg:
		m.invalidate()
		return tea.Batch(m.Load(), m.wantDetail())
	case actions.DeniedMsg:
		why := m.t(map[bool]string{true: "ui.locked.agent", false: "ev.denied.readonly"}[msg.Locked])
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

// suggest cấp gợi ý (đề xuất, cơ hội, harness) cho form đang mở.
func (m *Model) suggest(f *form.Model) {
	f.SetSuggestions("proposals", m.Data.IDs())
	f.SetSuggestions("opportunities", m.Data.OpportunityIDs())
	f.SetSuggestions("harnesses", m.Data.Config.Harnesses.Enabled)
}

// ---- phím ----

// Key xử lý một phím; handled = false nghĩa là màn hình không dùng phím này.
func (m *Model) Key(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch {
	case m.dlg != nil:
		return m.dialogKey(msg), true
	case m.frm != nil:
		return m.formKey(msg), true
	}
	m.toast = ""
	s := msg.String()
	if s == "esc" || s == "ctrl+c" {
		if j := m.job(); j != nil && m.tab == tabLog {
			action := j.Action
			return func() tea.Msg { return actions.CancelMsg{ID: action} }, true
		}
	}
	switch s {
	case ".":
		return m.setTab(m.nextTab(1)), true
	case ",":
		return m.setTab(m.nextTab(-1)), true
	}
	return m.listKey(msg)
}

func (m *Model) cur() *list.Model {
	if m.tab == tabOpps {
		return &m.olst
	}
	return &m.plst
}

func (m *Model) listKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	s := msg.String()
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
		return m.edge(true), true
	case "G", "end":
		return m.edge(false), true
	case "tab":
		m.pane = (m.pane + 1) % 2
		return nil, true
	case "enter":
		m.pane = paneDetail
		return nil, true
	case "esc":
		if m.pane == paneDetail {
			m.pane = paneList
		}
		return m.wantDetail(), true
	case "ctrl+r", "f5":
		m.invalidate()
		return tea.Batch(m.Load(), m.wantDetail()), true
	case "A":
		switch m.tab {
		case tabQueue:
			m.queueAll = !m.queueAll
		default:
			m.all = !m.all
		}
		return m.Load(), true
	case "v":
		m.sub = (m.sub + 1) % numSubs
		m.detail.Reset()
		return nil, true
	case "s":
		return m.cmdScan(), true
	case "p":
		return m.cmdPropose(), true
	case "E":
		return m.cmdEvaluate(), true
	case "a":
		return m.cmdApply(), true
	case "x":
		return m.cmdReject(), true
	case "u":
		return m.cmdRollback(), true
	case "P":
		return m.cmdProcess(), true
	case "C":
		return m.cmdCycle(), true
	}
	return nil, false
}

// scroller là viewer đang hiển thị ở thẻ hiện tại (chi tiết đề xuất, lịch sử, hàng đợi).
func (m *Model) scroller() *viewer.Model {
	switch m.tab {
	case tabHistory:
		return &m.hist
	case tabQueue:
		return &m.queue
	}
	return &m.detail
}

func (m *Model) fullPane() bool { return m.tab == tabHistory || m.tab == tabQueue }

func (m *Model) nav(d int) tea.Cmd {
	switch {
	case m.tab == tabLog:
		m.log.Scroll(d, m.logTotal, m.logH)
		return nil
	case m.fullPane() || m.pane == paneDetail:
		m.scroller().Scroll(d)
		return nil
	}
	m.cur().Move(d)
	m.detail.Reset()
	return m.wantDetail()
}

func (m *Model) page(d int) tea.Cmd {
	switch {
	case m.tab == tabLog:
		m.log.Scroll(d*max(m.logH-2, 3), m.logTotal, m.logH)
		return nil
	case m.fullPane() || m.pane == paneDetail:
		m.scroller().Page(d)
		return nil
	}
	m.cur().Page(d)
	m.detail.Reset()
	return m.wantDetail()
}

func (m *Model) edge(top bool) tea.Cmd {
	switch {
	case m.tab == tabLog:
		return nil
	case m.fullPane() || m.pane == paneDetail:
		if top {
			m.scroller().Home()
		} else {
			m.scroller().End()
		}
		return nil
	}
	if top {
		m.cur().Home()
	} else {
		m.cur().End()
	}
	m.detail.Reset()
	return m.wantDetail()
}

// ---- hộp thoại và form ----

func (m *Model) dialogKey(msg tea.KeyPressMsg) tea.Cmd {
	d := m.dlg
	switch d.c.Update(msg) {
	case confirm.Yes:
		m.dlg = nil
		if d.onYes != nil {
			return d.onYes(&d.c)
		}
	case confirm.No:
		m.dlg = nil
	}
	return nil
}

func (m *Model) openInfo(title string, body []string) {
	m.dlg = &dialog{c: confirm.Model{Title: title, Body: body, YesLabel: m.t("ev.dialog.close"), NoLabel: m.t("ev.dialog.close")}}
}

func (m *Model) openForm(id, cli string, defaults any) {
	f := form.New(id, cli, defaults)
	m.suggest(&f)
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

// submitForm gửi giá trị form đã qua Validate: việc nền (propose, evaluate, process, cycle) đóng form và chuyển sang
// thẻ Log; scan giữ form tới khi có kết quả.
func (m *Model) submitForm(v any) tea.Cmd {
	id := m.frmID
	switch v.(type) {
	case evolvesvc.ProposeOptions, evolvesvc.EvaluateOptions, evolvesvc.ProcessOptions, evolvesvc.CycleOptions:
		m.frm = nil
		m.ShowLog()
		return request(id, v)
	}
	m.frmBusy = true
	return request(id, v)
}

// ---- lệnh (phím và bảng lệnh) ----

func (m *Model) cmdScan() tea.Cmd {
	if m.allowed("evolve.scan") {
		m.openForm("evolve.scan", "evolve scan", evolvesvc.ScanDefaults(config.Default()))
	}
	return nil
}

// cmdPropose mở form tạo đề xuất; ở thẻ Cơ hội điền sẵn cơ hội đang chọn.
func (m *Model) cmdPropose() tea.Cmd {
	if !m.allowed("evolve.propose") {
		return nil
	}
	o := evolvesvc.ProposeDefaults(m.Data.Config)
	if op := m.CursorOpportunity(); op != nil && m.tab == tabOpps {
		o.Refs = []string{op.ID}
	}
	m.openForm("evolve.propose", "evolve propose", o)
	return nil
}

// cmdEvaluate mở form đối chiếu đề xuất đang chọn (mặc định theo cấu hình evolution.evaluation).
func (m *Model) cmdEvaluate() tea.Cmd {
	if !m.allowed("evolve.evaluate") {
		return nil
	}
	r, cmd := m.pickedProposal()
	if r == nil {
		return cmd
	}
	o := evolvesvc.EvaluateDefaults(m.Data.Config)
	o.ID = r.Proposal.ID
	m.openForm("evolve.evaluate", "evolve evaluate", o)
	return nil
}

func (m *Model) cmdProcess() tea.Cmd {
	if m.allowed("evolve.process") {
		m.openForm("evolve.process", "evolve process", evolvesvc.ProcessDefaults(m.Data.Config))
	}
	return nil
}

func (m *Model) cmdCycle() tea.Cmd {
	if m.allowed("evolve.cycle") {
		m.openForm("evolve.cycle", "evolve cycle", evolvesvc.CycleDefaults(m.Data.Config))
	}
	return nil
}

// cmdApply tính điều kiện áp dụng (chỉ đọc) rồi mở hộp xác nhận khi có kết quả.
func (m *Model) cmdApply() tea.Cmd {
	if !m.allowed("evolve.apply") {
		return nil
	}
	r, cmd := m.pickedProposal()
	if r == nil {
		return cmd
	}
	return request("evolve.plan_apply", evolvesvc.ApplyOptions{Ref: r.Proposal.ID})
}

// cmdRollback tính tệp sẽ khôi phục và xung đột (chỉ đọc) rồi mở hộp xác nhận.
func (m *Model) cmdRollback() tea.Cmd {
	if !m.allowed("evolve.rollback") {
		return nil
	}
	r, cmd := m.pickedProposal()
	if r == nil {
		return cmd
	}
	return request("evolve.plan_rollback", evolvesvc.RollbackOptions{Ref: r.Proposal.ID})
}

// cmdReject mở hộp bác bỏ có ô lý do.
func (m *Model) cmdReject() tea.Cmd {
	if !m.allowed("evolve.reject") {
		return nil
	}
	r, cmd := m.pickedProposal()
	if r == nil {
		return cmd
	}
	id := r.Proposal.ID
	c := confirm.Model{Title: m.t("ev.reject.title", i18n.A{"id": id}), Input: true, Label: m.t("ev.reason"), Placeholder: m.t("ev.reject.reason.hint"),
		Body: []string{m.t("ev.reject.body", i18n.A{"state": r.State})}, YesLabel: m.t("ev.reject.yes"), NoLabel: m.t("ev.dialog.cancel")}
	m.dlg = &dialog{c: c, onYes: func(d *confirm.Model) tea.Cmd {
		return request("evolve.reject", evolvesvc.RejectOptions{Ref: id, Reason: d.Value()})
	}}
	return nil
}

// Command thực hiện hành động theo ID (từ bảng lệnh).
func (m *Model) Command(id string) tea.Cmd {
	switch id {
	case "evolve.scan":
		return m.cmdScan()
	case "evolve.propose":
		return m.cmdPropose()
	case "evolve.evaluate":
		return m.cmdEvaluate()
	case "evolve.apply":
		return m.cmdApply()
	case "evolve.reject":
		return m.cmdReject()
	case "evolve.rollback":
		return m.cmdRollback()
	case "evolve.process":
		return m.cmdProcess()
	case "evolve.cycle":
		return m.cmdCycle()
	case "evolve.list", "evolve.show":
		m.tab = tabProposals
		if id == "evolve.list" {
			m.all = !m.all
			return m.Load()
		}
		m.pane = paneDetail
	case "evolve.history":
		return m.setTab(tabHistory)
	case "evolve.queue":
		return m.setTab(tabQueue)
	case "evolve.next_sub":
		m.sub = (m.sub + 1) % numSubs
		m.detail.Reset()
	case "evolve.refresh":
		return m.Update(RefreshMsg{})
	case "evolve.next_view":
		return m.setTab(m.nextTab(1))
	case "evolve.prev_view":
		return m.setTab(m.nextTab(-1))
	}
	return nil
}
