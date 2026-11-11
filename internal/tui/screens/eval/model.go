package eval

import (
	"context"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"bint/internal/app"
	"bint/internal/eval"
	"bint/internal/i18n"
	"bint/internal/tui/actions"
	"bint/internal/tui/components/confirm"
	"bint/internal/tui/components/form"
	"bint/internal/tui/components/input"
	"bint/internal/tui/components/list"
	"bint/internal/tui/components/logview"
	"bint/internal/tui/components/viewer"
	"bint/internal/tui/jobs"
	"bint/internal/tui/theme"
)

// WideW: từ chiều rộng này danh sách và chi tiết hiện cạnh nhau; hẹp hơn thì một khung (Tab/Enter đổi).
const WideW = 100

// Các thẻ của màn hình. Thẻ Log chỉ hiện khi có việc nền của Đánh giá (đang chạy hoặc vừa xong).
const (
	tabCands = iota
	tabCases
	tabBatches
	tabLog
	numTabs
)

var tabNames = [numTabs]string{"cands", "cases", "batches", "log"}

type pane int

const (
	paneList pane = iota
	paneDetail
)

// splits là vòng lọc tập con của thẻ Tác vụ: "" = mọi tập.
var splits = []string{"", eval.SplitBuild, eval.SplitValidation, eval.SplitTest}

// Options điều khiển màn hình.
type Options struct {
	Dir      string
	ReadOnly bool
	// Loader/Detailer/Reporter thay được trong kiểm thử; nil = nạp thật.
	Loader   Loader
	Detailer Detailer
	Reporter Reporter
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

type reportEntry struct {
	rep *eval.Report
	err error
}

// Model là trạng thái màn Đánh giá.
type Model struct {
	o   Options
	cat *i18n.Catalog
	th  theme.Theme

	Data        Data
	Loaded      bool
	Loading     bool
	reloadAgain bool
	Err         error

	tab     int
	pane    pane
	all     bool // ứng viên: gồm cả đã loại (`eval list --all`)
	splitAt int  // thẻ Tác vụ: chỉ số trong splits
	focusID string
	// focusBatch: đợt cần chọn khi dữ liệu nạp xong (đợt vừa chạy xong).
	focusBatch string

	filter  string
	textIn  bool
	textOld string
	cidx    []int // chỉ số ứng viên đang hiện (sau lọc)
	kidx    []int // chỉ số tác vụ đang hiện
	bidx    []int // chỉ số đợt đang hiện
	clst    list.Model
	kst     list.Model
	blst    list.Model

	detail    viewer.Model
	detailKey string
	log       logview.Model
	logTotal  int
	logH      int

	cache    map[string]*app.EvalShowResult
	cacheErr map[string]error
	asked    map[string]bool

	baseline  string
	reports   map[string]reportEntry
	askedRep  map[string]bool
	run       *runState
	dlg       *dialog
	frm       *form.Model
	frmID     string
	frmBusy   bool
	toast     string
	toastErr  bool
	w, h      int
	lastBatch string // đợt vừa chạy xong trong phiên (để thẻ Đợt mở báo cáo của nó)
}

// New dựng màn hình; gọi SetEnv trước khi vẽ.
func New(o Options) *Model {
	if o.Loader == nil {
		o.Loader = Load
	}
	if o.Detailer == nil {
		o.Detailer = LoadDetail
	}
	if o.Reporter == nil {
		o.Reporter = LoadReport
	}
	if o.Jobs == nil {
		o.Jobs = &jobs.Table{}
	}
	return &Model{o: o, log: logview.New(), cache: map[string]*app.EvalShowResult{}, cacheErr: map[string]error{}, asked: map[string]bool{},
		reports: map[string]reportEntry{}, askedRep: map[string]bool{}}
}

// SetEnv đặt danh mục chuỗi và bảng màu (gọi lại khi đổi ngôn ngữ).
func (m *Model) SetEnv(th theme.Theme, cat *i18n.Catalog) { m.th, m.cat = th, cat }

func (m *Model) t(key string, a ...i18n.A) string { return m.cat.T(key, a...) }

// tr dịch giá trị dữ liệu (trạng thái...) theo khoá prefix+value; không có khoá thì giữ nguyên giá trị.
func (m *Model) tr(prefix, v string) string {
	if v == "" {
		return "-"
	}
	if k := prefix + v; m.cat.Has(k) {
		return m.cat.T(k)
	}
	return v
}

func (m *Model) allowed(id string) bool {
	if m.o.Gate == nil {
		return true
	}
	switch m.o.Gate(id) {
	case actions.Locked:
		m.setToast(m.t("ui.locked.agent"), true)
		return false
	case actions.Hidden:
		m.setToast(m.t("eval.denied.readonly"), true)
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
	ctx, dir, loader, q := context.Background(), m.o.Dir, m.o.Loader, Query{All: m.all}
	return func() tea.Msg {
		d, err := loader(ctx, dir, q)
		return LoadedMsg{Data: d, Err: err}
	}
}

// Focus mở thẻ Ứng viên với ứng viên id được chọn (từ hộp thư); hiện cả ứng viên đã loại nếu id không còn chờ.
func (m *Model) Focus(id string) tea.Cmd {
	m.focusID, m.tab, m.pane = id, tabCands, paneList
	m.filter, m.textIn = "", false
	m.dlg, m.frm = nil, nil
	if m.Loaded {
		m.rebuild()
		if m.selectCand(id) {
			m.focusID = ""
			return m.wantDetail()
		}
		if !m.all {
			m.all = true
		}
	}
	return m.Load()
}

// selectCand đưa con trỏ ứng viên tới id; false nếu chưa có trong danh sách đang hiện.
func (m *Model) selectCand(id string) bool {
	for pos, i := range m.cidx {
		if m.Data.Pending[i].ID == id {
			m.clst.MoveTo(pos)
			return true
		}
	}
	return false
}

func (m *Model) selectBatch(id string) bool {
	for pos, i := range m.bidx {
		if m.Data.Batches[i].BatchID == id {
			m.blst.MoveTo(pos)
			return true
		}
	}
	return false
}

// cursors là ID dưới con trỏ của ba danh sách, chụp trước khi dữ liệu hoặc bộ lọc đổi để giữ con trỏ theo ID.
type cursors struct{ cand, kase, batch string }

func (m *Model) snapshotCursors() cursors {
	var k cursors
	if c := m.Cand(); c != nil {
		k.cand = c.ID
	}
	if c := m.CursorCase(); c != nil {
		k.kase = c.ID
	}
	if b := m.CursorBatch(); b != nil {
		k.batch = b.BatchID
	}
	return k
}

// rebuild dựng lại các danh sách theo bộ lọc (và tập con ở thẻ Tác vụ), giữ con trỏ theo ID.
func (m *Model) rebuild() { m.rebuildKeep(m.snapshotCursors()) }

// rebuildKeep như rebuild nhưng giữ con trỏ theo ID đã chụp từ trước (khi Data vừa bị thay).
func (m *Model) rebuildKeep(k cursors) {
	keepC, keepK, keepB := k.cand, k.kase, k.batch
	f := strings.ToLower(strings.TrimSpace(m.filter))
	match := func(parts ...string) bool {
		return f == "" || strings.Contains(strings.ToLower(strings.Join(parts, " ")), f)
	}
	m.cidx, m.kidx, m.bidx = m.cidx[:0], m.kidx[:0], m.bidx[:0]
	for i, c := range m.Data.Pending {
		if match(c.ID, c.Status, c.Prompt, c.Source.Subject, c.RejectReason) {
			m.cidx = append(m.cidx, i)
		}
	}
	for i, c := range m.Data.Cases {
		if s := splits[m.splitAt]; s != "" && c.Split != s {
			continue
		}
		if match(c.ID, c.Split, c.Prompt, c.Source.Subject) {
			m.kidx = append(m.kidx, i)
		}
	}
	for i, b := range m.Data.Batches {
		if match(b.BatchID, b.Status, b.Harness, strings.Join(b.Arms, ",")) {
			m.bidx = append(m.bidx, i)
		}
	}
	m.clst.SetN(len(m.cidx))
	m.kst.SetN(len(m.kidx))
	m.blst.SetN(len(m.bidx))
	for pos, i := range m.cidx {
		if keepC != "" && m.Data.Pending[i].ID == keepC {
			m.clst.MoveTo(pos)
		}
	}
	for pos, i := range m.kidx {
		if keepK != "" && m.Data.Cases[i].ID == keepK {
			m.kst.MoveTo(pos)
		}
	}
	for pos, i := range m.bidx {
		if keepB != "" && m.Data.Batches[i].BatchID == keepB {
			m.blst.MoveTo(pos)
		}
	}
}

// Cand trả ứng viên dưới con trỏ (nil nếu danh sách trống).
func (m *Model) Cand() *eval.Case {
	if m.clst.Cursor < 0 || m.clst.Cursor >= len(m.cidx) || m.cidx[m.clst.Cursor] >= len(m.Data.Pending) {
		return nil
	}
	return m.Data.Pending[m.cidx[m.clst.Cursor]]
}

// CursorCase trả tác vụ đã duyệt dưới con trỏ.
func (m *Model) CursorCase() *eval.Case {
	if m.kst.Cursor < 0 || m.kst.Cursor >= len(m.kidx) || m.kidx[m.kst.Cursor] >= len(m.Data.Cases) {
		return nil
	}
	return m.Data.Cases[m.kidx[m.kst.Cursor]]
}

// CursorBatch trả đợt dưới con trỏ.
func (m *Model) CursorBatch() *eval.Batch {
	if m.blst.Cursor < 0 || m.blst.Cursor >= len(m.bidx) || m.bidx[m.blst.Cursor] >= len(m.Data.Batches) {
		return nil
	}
	return &m.Data.Batches[m.bidx[m.blst.Cursor]]
}

// current trả tác vụ/ứng viên dưới con trỏ ở thẻ Ứng viên hoặc Tác vụ (nil ở thẻ khác).
func (m *Model) current() *eval.Case {
	switch m.tab {
	case tabCands:
		return m.Cand()
	case tabCases:
		return m.CursorCase()
	}
	return nil
}

// pickedCand trả ứng viên dưới con trỏ khi thẻ Ứng viên đang hiện. Ở thẻ khác, con trỏ ứng viên bị ẩn nên không
// thao tác lên nó: chuyển sang thẻ Ứng viên và nhắc chọn rồi bấm lại.
func (m *Model) pickedCand() (*eval.Case, tea.Cmd) {
	if m.tab != tabCands {
		cmd := m.setTab(tabCands)
		m.setToast(m.t("eval.need_cand"), true)
		return nil, cmd
	}
	c := m.Cand()
	if c == nil {
		m.setToast(m.t("eval.need_cand"), true)
	}
	return c, nil
}

// Dialog mô tả hộp thoại đang mở (cho kiểm thử): Open, Danger (hộp đỏ), và các ô chọn đang được chọn.
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

// Tab là thẻ đang hiện (0 Ứng viên, 1 Tác vụ, 2 Đợt, 3 Log).
func (m *Model) Tab() int { return m.tab }

// ShowAll cho biết thẻ Ứng viên đang hiện cả ứng viên đã loại.
func (m *Model) ShowAll() bool { return m.all }

// Baseline là mốc so sánh đang dùng của báo cáo ("" = mặc định của báo cáo).
func (m *Model) Baseline() string { return m.baseline }

// ---- trạng thái màn hình (ui.Screen) ----

// Capturing: màn hình đang nhận mọi phím chữ (form, hộp thoại, ô lọc).
func (m *Model) Capturing() bool { return m.dlg != nil || m.frm != nil || m.textIn }

// NotList: màn Đánh giá giữ chân phím chung ở mọi thẻ.
func (m *Model) NotList() bool { return false }

// Busy: có việc của màn hình đang chạy (nạp, form chờ kết quả, hoặc việc Đánh giá nặng).
func (m *Model) Busy() bool { return m.Loading || m.frmBusy || m.job() != nil }

// job trả việc Đánh giá nặng đang chạy (nil nếu không có).
func (m *Model) job() *jobs.Job { return m.o.Jobs.OfKind(jobs.KindEval) }

// lastJob trả việc đang chạy, không thì việc vừa xong (để xem log).
func (m *Model) lastJob() *jobs.Job {
	if j := m.job(); j != nil {
		return j
	}
	return m.o.Jobs.LastOfKind(jobs.KindEval)
}

// WantsInterrupt: đang xem log của việc đang chạy; Esc/Ctrl+C huỷ việc đó thay vì lùi/thoát.
func (m *Model) WantsInterrupt() bool { return m.tab == tabLog && m.job() != nil && !m.Capturing() }

// wantDetail nạp chi tiết ứng viên/tác vụ dưới con trỏ, hoặc báo cáo của đợt dưới con trỏ.
func (m *Model) wantDetail() tea.Cmd {
	ctx, dir := context.Background(), m.o.Dir
	switch m.tab {
	case tabCands, tabCases:
		c := m.current()
		if c == nil || m.asked[c.ID] {
			return nil
		}
		id := c.ID
		m.asked[id] = true
		det := m.o.Detailer
		return func() tea.Msg {
			d, err := det(ctx, dir, id)
			return DetailMsg{ID: id, Detail: d, Err: err}
		}
	case tabBatches:
		b := m.CursorBatch()
		if b == nil {
			return nil
		}
		id, base := b.BatchID, m.baseline
		key := id + "|" + base
		if m.askedRep[key] {
			return nil
		}
		m.askedRep[key] = true
		rp := m.o.Reporter
		return func() tea.Msg {
			r, err := rp(ctx, dir, id, base)
			return ReportMsg{BatchID: id, Baseline: base, Report: r, Err: err}
		}
	}
	return nil
}

func (m *Model) invalidate() {
	m.asked, m.askedRep = map[string]bool{}, map[string]bool{}
}

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

// ShowLog chuyển sang thẻ Log và bắt đầu một lưới tiến độ mới (khi vừa bắt đầu một việc nền).
func (m *Model) ShowLog() {
	m.tab, m.pane = tabLog, paneDetail
	m.log = logview.New()
	m.run = &runState{}
}

// Update xử lý thông điệp không phải phím; trả lệnh kế tiếp.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case LoadedMsg:
		m.Loading = false
		m.Err = msg.Err
		var cmds []tea.Cmd
		if msg.Err == nil {
			keep := m.snapshotCursors()
			m.Data, m.Loaded = msg.Data, true
			m.rebuildKeep(keep)
			if m.focusID != "" && m.selectCand(m.focusID) {
				m.focusID = ""
			}
			if m.focusBatch != "" && m.selectBatch(m.focusBatch) {
				m.focusBatch = ""
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
	case ReportMsg:
		m.reports[msg.BatchID+"|"+msg.Baseline] = reportEntry{rep: msg.Report, err: msg.Err}
	case RefreshMsg:
		m.invalidate()
		return tea.Batch(m.Load(), m.wantDetail())
	case jobs.DataMsg:
		m.onData(msg.Value)
	case actions.DeniedMsg:
		why := m.t(map[bool]string{true: "ui.locked.agent", false: "eval.denied.readonly"}[msg.Locked])
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

// suggest cấp gợi ý (ứng viên, tác vụ, đợt, nhánh, harness) cho form đang mở.
func (m *Model) suggest(f *form.Model) {
	f.SetSuggestions("candidates", m.Data.PendingIDs())
	f.SetSuggestions("eval_ids", m.Data.AllIDs())
	f.SetSuggestions("batches", m.Data.BatchIDs())
	f.SetSuggestions("arms", []string{eval.ArmNone, eval.ArmMinimal, eval.ArmBint, eval.ArmIdeal})
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
	case m.textIn:
		return m.textKey(msg), true
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
	switch m.tab {
	case tabCases:
		return &m.kst
	case tabBatches:
		return &m.blst
	}
	return &m.clst
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
		switch {
		case m.pane == paneDetail:
			m.pane = paneList
		case m.filter != "":
			m.filter = ""
			m.rebuild()
		}
		return m.wantDetail(), true
	case "/":
		if m.tab != tabLog {
			m.textIn, m.textOld = true, m.filter
		}
		return nil, true
	case "ctrl+r", "f5":
		m.invalidate()
		return tea.Batch(m.Load(), m.wantDetail()), true
	case "A":
		m.all = !m.all
		return m.Load(), true
	case "t":
		m.splitAt = (m.splitAt + 1) % len(splits)
		m.rebuild()
		return m.wantDetail(), true
	case "m":
		return m.cmdMine(), true
	case "v":
		return m.cmdVerify(false), true
	case "V":
		return m.cmdVerify(true), true
	case "p":
		return m.cmdSpec(), true
	case "a":
		return m.cmdApprove(), true
	case "x":
		return m.cmdReject(), true
	case "e":
		return m.cmdOpenFile(), true
	case "r":
		return m.cmdRun(), true
	case "R":
		return m.cmdResume(), true
	case "E":
		return m.cmdEstimate(), true
	case "b":
		return m.cmdBaseline(), true
	case "B":
		return m.cmdBootstrap(), true
	case "C":
		return m.cmdClean(), true
	}
	return nil, false
}

func (m *Model) nav(d int) tea.Cmd {
	switch {
	case m.tab == tabLog:
		m.log.Scroll(d, m.logTotal, m.logH)
		return nil
	case m.pane == paneDetail:
		m.detail.Scroll(d)
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
	case m.pane == paneDetail:
		m.detail.Page(d)
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
	case m.pane == paneDetail:
		if top {
			m.detail.Home()
		} else {
			m.detail.End()
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
	m.dlg = &dialog{c: confirm.Model{Title: title, Body: body, YesLabel: m.t("eval.dialog.close"), NoLabel: m.t("eval.dialog.close")}}
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

// submitForm gửi giá trị form đã qua Validate: việc nền (run, bootstrap, verify, mine có kiểm chứng) đóng form và
// chuyển sang thẻ Log; đổi mốc báo cáo xử lý tại chỗ; còn lại giữ form tới khi có kết quả.
func (m *Model) submitForm(v any) tea.Cmd {
	id := m.frmID
	switch o := v.(type) {
	case app.EvalRunOptions, app.EvalBootstrapOptions, app.EvalVerifyOptions:
		m.frm = nil
		m.ShowLog()
		return request(id, v)
	case app.EvalMineOptions:
		if o.Verify {
			m.frm = nil
			m.ShowLog()
			return request(id, v)
		}
	case app.EvalReportOptions:
		m.frm = nil
		m.baseline = o.Baseline
		m.tab, m.pane = tabBatches, paneDetail
		if o.BatchID != "" {
			for pos, i := range m.bidx {
				if strings.HasPrefix(m.Data.Batches[i].BatchID, o.BatchID) {
					m.blst.MoveTo(pos)
				}
			}
		}
		m.detail.Reset()
		return m.wantDetail()
	}
	m.frmBusy = true
	return request(id, v)
}

// ---- lệnh (phím và bảng lệnh) ----

// idsOf điền sẵn danh sách ID cho form: tác vụ/ứng viên dưới con trỏ.
func (m *Model) idsOf() []string {
	if c := m.current(); c != nil {
		return []string{c.ID}
	}
	return nil
}

func (m *Model) cmdMine() tea.Cmd {
	if m.allowed("eval.mine") {
		m.openForm("eval.mine", "eval mine", app.EvalMineDefaults(m.Data.Config))
	}
	return nil
}

// cmdVerify mở form kiểm chứng fail→pass; điền sẵn mục dưới con trỏ, hoặc --all khi all.
func (m *Model) cmdVerify(all bool) tea.Cmd {
	if !m.allowed("eval.verify") {
		return nil
	}
	o := app.EvalVerifyDefaults(m.Data.Config)
	if all {
		o.All = true
	} else {
		o.IDs = m.idsOf()
	}
	m.openForm("eval.verify", "eval verify", o)
	return nil
}

func (m *Model) cmdSpec() tea.Cmd {
	if !m.allowed("eval.spec") {
		return nil
	}
	o := app.EvalSpecDefaults(m.Data.Config)
	o.IDs = m.idsOf()
	o.Approved = m.tab == tabCases // tác vụ đã duyệt cần --approved (đổi đề bài làm đổi fingerprint)
	m.openForm("eval.spec", "eval spec", o)
	return nil
}

func (m *Model) cmdRun() tea.Cmd {
	if !m.allowed("eval.run") {
		return nil
	}
	if m.job() != nil {
		m.setToast(m.t("err.eval_busy"), true)
		return m.setTab(tabLog)
	}
	o := app.EvalRunDefaults(m.Data.Config)
	if c := m.current(); c != nil && m.tab == tabCases {
		o.CaseIDs = []string{c.ID}
	}
	m.openForm("eval.run", "eval run", o)
	return nil
}

// cmdResume mở form chạy tiếp đợt dưới con trỏ ở thẻ Đợt (chỉ khi chưa xong). Form dùng chung ô với eval run để đặt lại
// ngân sách hoặc lệnh agent tuỳ chỉnh (đợt chạy bằng agent lệnh phải tiếp tục bằng chính harness đó); các ô chọn tác vụ,
// nhánh và lần lặp bị bỏ qua vì đợt giữ nguyên kế hoạch cũ. Hộp xác nhận hiện ở việc nền như khi chạy mới.
func (m *Model) cmdResume() tea.Cmd {
	if !m.allowed("eval.resume") {
		return nil
	}
	if m.tab != tabBatches {
		cmd := m.setTab(tabBatches)
		m.setToast(m.t("eval.need_batch"), true)
		return cmd
	}
	b := m.CursorBatch()
	switch {
	case b == nil:
		m.setToast(m.t("eval.need_batch"), true)
		return nil
	case b.Status == "completed":
		m.setToast(m.t("eval.resume.done"), true)
		return nil
	case m.job() != nil:
		m.setToast(m.t("err.eval_busy"), true)
		return m.setTab(tabLog)
	}
	o := app.EvalRunDefaults(m.Data.Config)
	o.Resume = b.BatchID
	if b.Harness != "" && b.Harness != "command" {
		o.Harness = b.Harness
	}
	m.openForm("eval.resume", "eval run --resume "+b.BatchID, o)
	return nil
}

func (m *Model) cmdEstimate() tea.Cmd {
	if !m.allowed("eval.estimate") {
		return nil
	}
	o := app.EvalEstimateDefaults(m.Data.Config)
	if c := m.current(); c != nil && m.tab == tabCases {
		o.CaseIDs = []string{c.ID}
	}
	m.openForm("eval.estimate", "eval estimate", o)
	return nil
}

// cmdBaseline mở form đổi mốc so sánh của báo cáo (và đợt cần xem).
func (m *Model) cmdBaseline() tea.Cmd {
	if m.tab != tabBatches {
		cmd := m.setTab(tabBatches)
		m.setToast(m.t("eval.need_batch"), true)
		return cmd
	}
	o := app.EvalReportDefaults(m.Data.Config)
	o.Baseline = m.baseline
	if b := m.CursorBatch(); b != nil {
		o.BatchID = b.BatchID
	}
	m.openForm("eval.report", "eval report", o)
	return nil
}

func (m *Model) cmdBootstrap() tea.Cmd {
	if !m.allowed("eval.bootstrap") {
		return nil
	}
	if m.job() != nil {
		m.setToast(m.t("err.eval_busy"), true)
		return m.setTab(tabLog)
	}
	m.openForm("eval.bootstrap", "eval bootstrap", app.EvalBootstrapDefaults(m.Data.Config))
	return nil
}

// cmdOpenFile mở tệp ứng viên/tác vụ dưới con trỏ trong $EDITOR (sửa đề bài).
func (m *Model) cmdOpenFile() tea.Cmd {
	if !m.allowed("eval.open_file") {
		return nil
	}
	c := m.current()
	if c == nil {
		m.setToast(m.t("eval.need_cand"), true)
		return nil
	}
	dir := m.Data.CandidatesDir
	if m.tab == tabCases {
		dir = m.Data.CasesDir
	}
	return request("eval.open_file", actions.EditOptions{Path: filepath.Join(dir, c.ID+".yml")})
}

// Command thực hiện hành động theo ID (từ bảng lệnh).
func (m *Model) Command(id string) tea.Cmd {
	switch id {
	case "eval.mine":
		return m.cmdMine()
	case "eval.verify":
		return m.cmdVerify(false)
	case "eval.spec":
		return m.cmdSpec()
	case "eval.approve":
		return m.cmdApprove()
	case "eval.reject":
		return m.cmdReject()
	case "eval.run":
		return m.cmdRun()
	case "eval.resume":
		return m.cmdResume()
	case "eval.estimate":
		return m.cmdEstimate()
	case "eval.bootstrap":
		return m.cmdBootstrap()
	case "eval.clean":
		return m.cmdClean()
	case "eval.open_file":
		return m.cmdOpenFile()
	case "eval.baseline":
		return m.cmdBaseline()
	case "eval.list":
		m.tab = tabCands
		m.all = !m.all
		return m.Load()
	case "eval.show":
		m.pane = paneDetail
		return m.wantDetail()
	case "eval.report":
		return m.setTab(tabBatches)
	case "eval.split":
		m.splitAt = (m.splitAt + 1) % len(splits)
		m.tab = tabCases
		m.rebuild()
		return m.wantDetail()
	case "eval.filter":
		if m.tab != tabLog {
			m.textIn, m.textOld = true, m.filter
		}
	case "eval.refresh":
		return m.Update(RefreshMsg{})
	case "eval.next_view":
		return m.setTab(m.nextTab(1))
	case "eval.prev_view":
		return m.setTab(m.nextTab(-1))
	}
	return nil
}
