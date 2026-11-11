package knowledge

import (
	"context"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"bint/internal/app"
	"bint/internal/config"
	"bint/internal/i18n"
	kn "bint/internal/knowledge"
	"bint/internal/tui/actions"
	"bint/internal/tui/components/confirm"
	"bint/internal/tui/components/form"
	"bint/internal/tui/components/list"
	"bint/internal/tui/components/viewer"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

// WideW: từ chiều rộng này danh sách và chi tiết hiện cạnh nhau; hẹp hơn thì một khung (Tab/Enter đổi).
const WideW = 100

// Các thẻ phụ.
const (
	subList = iota
	subSearch
	subStats
	subDiff
	subRevalidate
	subPublish
	subSurvey
	subContext
	numSubs
)

var subNames = [numSubs]string{"list", "search", "stats", "diff", "revalidate", "publish", "survey", "context"}

type pane int

const (
	paneList pane = iota
	paneDetail
)

// Options điều khiển màn hình.
type Options struct {
	Dir      string
	ReadOnly bool
	Agent    bool
	// Loader/Detailer thay được trong kiểm thử; nil = nạp thật.
	Loader   Loader
	Detailer Detailer
	// Gate cho biết hành động có dùng được không theo sổ hành động và chế độ (nil = luôn dùng được). Màn hình
	// hỏi trước khi mở form/hộp thoại của thao tác ghi để báo ngay; mô hình gốc vẫn kiểm lại khi nhận yêu cầu.
	Gate func(id string) actions.Availability
}

// dialog là hộp xác nhận đang mở và việc làm khi người dùng đồng ý.
type dialog struct {
	c     confirm.Model
	onYes func(value string) tea.Cmd // nil: hộp chỉ để đọc, Enter/Esc đóng
}

// Model là trạng thái màn Tri thức.
type Model struct {
	o   Options
	cat *i18n.Catalog
	th  theme.Theme

	Data    Data
	Loaded  bool
	Loading bool
	// reloadAgain: có yêu cầu nạp lại trong lúc đang nạp.
	reloadAgain bool
	Err         error

	// fopts là bộ lọc hiện hành (nguồn sự thật); text là ô lọc nhanh `/` (ghi vào fopts.Text).
	fopts actions.FilterOptions
	view  []int // chỉ số trong Data.Items của các mục khớp bộ lọc
	lst   list.Model
	sel   list.Selection
	pane  pane

	detail   viewer.Model
	detailID string
	cache    map[string]app.KnowledgeDetail
	cacheErr map[string]error
	asked    map[string]bool

	sub   int
	pnls  [numSubs]*panel
	surv  [3]*panel // các chế độ của thẻ Khảo sát: inspect, bootstrap, bootstrap skill
	survM int       // chế độ đang chọn của thẻ Khảo sát

	dlg      *dialog
	frm      *form.Model
	frmID    string
	frmBusy  bool
	textIn   bool
	textOld  string
	toast    string
	toastErr bool

	// RowsDrawn đếm số hàng đã vẽ (kiểm thử khẳng định danh sách ảo chỉ vẽ hàng đang hiện).
	RowsDrawn int
	// W, H là kích thước vùng nội dung lần vẽ gần nhất (cuộn theo trang).
	w, h int
}

// New dựng màn hình; gọi SetEnv trước khi vẽ.
func New(o Options) *Model {
	if o.Loader == nil {
		o.Loader = Load
	}
	m := &Model{o: o, sel: list.Selection{}, cache: map[string]app.KnowledgeDetail{}, cacheErr: map[string]error{}, asked: map[string]bool{}}
	m.initPanels()
	return m
}

// SetEnv đặt danh mục chuỗi và bảng màu (gọi lại khi đổi ngôn ngữ).
func (m *Model) SetEnv(th theme.Theme, cat *i18n.Catalog) { m.th, m.cat = th, cat }

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
		m.setToast(m.t("kn.denied.readonly"), true)
		return false
	}
	return true
}

func (m *Model) t(key string, a ...i18n.A) string { return m.cat.T(key, a...) }

// Ensure nạp danh sách lần đầu (ui.Screen).
func (m *Model) Ensure() tea.Cmd {
	if m.Loaded || m.Loading {
		return nil
	}
	return m.Load()
}

// Load nạp lại danh sách (không chạy song song: đang nạp thì bỏ qua).
func (m *Model) Load() tea.Cmd {
	if m.Loading {
		m.reloadAgain = true // thao tác ghi xong trong lúc đang nạp: nạp thêm một lần để thấy thay đổi đó
		return nil
	}
	m.Loading = true
	ctx, dir, loader := context.Background(), m.o.Dir, m.o.Loader
	return func() tea.Msg {
		d, err := loader(ctx, dir)
		return LoadedMsg{Data: d, Err: err}
	}
}

// Focus đặt bộ lọc (từ hộp thư) và về danh sách; nạp lại nếu chưa có dữ liệu.
func (m *Model) Focus(f kn.Filter) tea.Cmd {
	m.sub, m.pane, m.dlg, m.frm, m.textIn = subList, paneList, nil, nil, false
	m.fopts = actions.FilterOptions{ThirdParty: f.ThirdParty, Batch: f.Batch, NoBatch: f.NoBatch, Path: f.Path, Text: f.Text}
	switch {
	case len(f.Statuses) == 2 && slices.Contains(f.Statuses, kn.StatusCandidate) && slices.Contains(f.Statuses, kn.StatusStale):
		m.fopts.Status = "pending"
	case len(f.Statuses) == 1:
		m.fopts.Status = f.Statuses[0]
	}
	if len(f.Types) == 1 {
		m.fopts.Type = f.Types[0]
	}
	if len(f.Origins) == 1 {
		m.fopts.Origin = f.Origins[0]
	}
	m.sel.Clear()
	m.refilter()
	m.lst.Home()
	return m.Load()
}

// Filter trả bộ lọc hiện hành.
func (m *Model) Filter() kn.Filter { return m.fopts.Filter() }

// Visible trả các mục đang hiện theo bộ lọc.
func (m *Model) Visible() []*kn.Item {
	out := make([]*kn.Item, len(m.view))
	for i, ix := range m.view {
		out[i] = m.Data.Items[ix]
	}
	return out
}

// Selected trả ID các mục đang được chọn theo thứ tự danh sách.
func (m *Model) Selected() []string {
	var ids []string
	for _, ix := range m.view {
		if id := m.Data.Items[ix].ID; m.sel.Has(id) {
			ids = append(ids, id)
		}
	}
	return ids
}

// Cursor trả mục dưới con trỏ (nil nếu danh sách trống).
func (m *Model) Cursor() *kn.Item {
	if m.lst.Cursor < 0 || m.lst.Cursor >= len(m.view) {
		return nil
	}
	return m.Data.Items[m.view[m.lst.Cursor]]
}

func (m *Model) refilter() {
	keep := ""
	if it := m.Cursor(); it != nil {
		keep = it.ID
	}
	f := m.fopts.Filter()
	m.view = m.view[:0]
	for i, it := range m.Data.Items {
		if f.Match(it) {
			m.view = append(m.view, i)
		}
	}
	m.lst.SetN(len(m.view))
	if keep != "" { // giữ con trỏ theo ID sau khi đọc lại hoặc đổi bộ lọc
		for i, ix := range m.view {
			if m.Data.Items[ix].ID == keep {
				m.lst.MoveTo(i)
				break
			}
		}
	}
	// Bỏ chọn các mục không còn tồn tại.
	exist := map[string]bool{}
	for _, it := range m.Data.Items {
		exist[it.ID] = true
	}
	for id := range m.sel {
		if !exist[id] {
			delete(m.sel, id)
		}
	}
}

// Capturing cho biết màn hình đang nhận mọi phím chữ (ô nhập, form, hộp thoại): mô hình gốc không xử
// lý phím toàn cục khi đó.
func (m *Model) Capturing() bool {
	switch {
	case m.dlg != nil, m.frm != nil, m.textIn:
		return true
	case m.sub != subList:
		return m.cur().inForm
	}
	return false
}

// NotList: đang ở một thẻ phụ (không phải danh sách).
func (m *Model) NotList() bool { return m.sub != subList }

// Busy: có việc nền của màn hình đang chạy (nạp, hoặc thẻ phụ đang chạy).
func (m *Model) Busy() bool {
	if m.Loading || m.frmBusy {
		return true
	}
	for _, p := range m.pnls {
		if p != nil && p.running {
			return true
		}
	}
	return false
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
			m.cache, m.cacheErr, m.asked = map[string]app.KnowledgeDetail{}, map[string]error{}, map[string]bool{}
			m.refilter()
			for _, p := range m.pnls {
				if p != nil && p.form != nil {
					p.form.SetSuggestions("files", msg.Data.Files)
					p.form.SetSuggestions("ids", msg.Data.IDs())
				}
			}
			cmds = append(cmds, m.wantDetail())
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
		}
	case actions.DeniedMsg:
		why := m.t(map[bool]string{true: "ui.locked.agent", false: "kn.denied.readonly"}[msg.Locked])
		m.setToast(why, true)
		if m.frmBusy && m.frm != nil { // yêu cầu của form bị từ chối: không có ResultMsg nào sẽ tới để mở khoá form
			m.frmBusy = false
			m.frm.Err = why
		}
	case actions.ResultMsg:
		return m.onResult(msg)
	}
	return nil
}

// wantDetail yêu cầu nạp chi tiết của mục dưới con trỏ nếu chưa có (kiểm lười).
func (m *Model) wantDetail() tea.Cmd {
	it := m.Cursor()
	if it == nil || m.asked[it.ID] {
		return nil
	}
	m.asked[it.ID] = true
	ctx, dir, det, id, data := context.Background(), m.o.Dir, m.o.Detailer, it.ID, m.Data
	return func() tea.Msg {
		if det == nil {
			if d, ok := data.detail(it); ok {
				return DetailMsg{ID: id, Detail: d}
			}
			det = LoadDetail
		}
		d, err := det(ctx, dir, id)
		return DetailMsg{ID: id, Detail: d, Err: err}
	}
}

func (m *Model) setToast(s string, isErr bool) { m.toast, m.toastErr = s, isErr }

// request gói một yêu cầu hành động cho mô hình gốc.
func request(id string, opts any) tea.Cmd {
	return func() tea.Msg { return actions.RequestMsg{ID: id, Opts: opts} }
}

// ---- phím ----

// Key xử lý một phím; handled = false nghĩa là màn hình không dùng phím này (mô hình gốc xử lý như phím
// toàn cục: q, ?, :, L, [, ], 1-7, ←/→).
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
	if m.sub != subList && (m.cur().inForm || m.cur().running) {
		return m.panelKey(msg) // form đang mở: mọi phím chữ (kể cả "." và ",") vào ô nhập
	}
	switch msg.String() {
	case ".":
		m.setSub(m.sub + 1)
		return m.autoRun(), true
	case ",":
		m.setSub(m.sub - 1)
		return m.autoRun(), true
	}
	if m.sub != subList {
		return m.panelKey(msg)
	}
	return m.listKey(msg)
}

func (m *Model) setSub(i int) {
	m.sub = (i%numSubs + numSubs) % numSubs
}

// EnterSub chuyển tới thẻ phụ (bảng lệnh) và tự chạy thẻ đọc-không-tham-số lần đầu.
func (m *Model) EnterSub(name string) tea.Cmd {
	for i, n := range subNames {
		if n == name {
			m.setSub(i)
			return m.autoRun()
		}
	}
	return nil
}

func (m *Model) listKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	s := msg.String()
	it := m.Cursor()
	one := func(id string, opts any) (tea.Cmd, bool) {
		if it == nil || !m.allowed(id) {
			return nil, true
		}
		return request(id, opts), true
	}
	switch s {
	case "j", "down":
		return m.move(1), true
	case "k", "up":
		return m.move(-1), true
	case "pgdown", "ctrl+d":
		if m.pane == paneDetail {
			m.detail.Page(1)
			return nil, true
		}
		m.lst.Page(1)
		return m.wantDetail(), true
	case "pgup", "ctrl+u":
		if m.pane == paneDetail {
			m.detail.Page(-1)
			return nil, true
		}
		m.lst.Page(-1)
		return m.wantDetail(), true
	case "g", "home":
		m.lst.Home()
		return m.wantDetail(), true
	case "G", "end":
		m.lst.End()
		return m.wantDetail(), true
	case "tab":
		m.pane = (m.pane + 1) % 2
		return nil, true
	case "enter":
		if it != nil {
			m.pane = paneDetail
		}
		return nil, true
	case "esc":
		switch {
		case m.pane == paneDetail && m.h > 0 && m.w < WideW:
			m.pane = paneList
		case m.sel.Count() > 0:
			m.sel.Clear()
		case !m.fopts.Filter().Empty() && !isDefaultFilter(m.fopts):
			m.fopts = actions.FilterOptions{}
			m.refilter()
		default:
			m.pane = paneList
		}
		return m.wantDetail(), true
	case "a":
		return one("knowledge.approve", app.ApproveOptions{Refs: idsOf(it)})
	case "f":
		return one("knowledge.approve_refresh", app.ApproveOptions{Refs: idsOf(it)})
	case "r":
		if !m.allowed("knowledge.reject") {
			return nil, true
		}
		return m.askReason(it, "knowledge.reject", false), true
	case "d":
		if !m.allowed("knowledge.deprecate") {
			return nil, true
		}
		return m.askReason(it, "knowledge.deprecate", true), true
	case "x":
		if it != nil {
			m.sel.Toggle(it.ID)
			return m.move(1), true
		}
		return nil, true
	case "X":
		m.toggleAll()
		return nil, true
	case "A":
		if !m.allowed("knowledge.approve_batch") {
			return nil, true
		}
		return m.planBatch(kn.StatusVerified), true
	case "R":
		if !m.allowed("knowledge.reject_batch") {
			return nil, true
		}
		return m.planBatch(kn.StatusDeprecated), true
	case "e":
		if it == nil || !m.allowed("knowledge.edit") {
			return nil, true
		}
		return request("knowledge.edit", actions.EditOptions{ID: it.ID, Path: it.Path}), true
	case "n":
		if m.allowed("knowledge.add") {
			m.openForm("knowledge.add", "knowledge add", app.AddDefaults(config.Default()))
		}
		return nil, true
	case "delete":
		if !m.allowed("knowledge.rm") {
			return nil, true
		}
		o := app.RemoveDefaults(config.Default())
		if it != nil {
			o.Refs = []string{it.ID}
		}
		m.openForm("knowledge.rm", "knowledge rm", o)
		return nil, true
	case "v":
		o := app.VerifyDefaults(config.Default())
		if it != nil {
			o.Refs = []string{it.ID}
		}
		m.openForm("knowledge.verify", "knowledge verify", o)
		return nil, true
	case "/":
		m.textIn, m.textOld = true, m.fopts.Text
		return nil, true
	case "F":
		m.openForm("knowledge.filter", "", m.fopts)
		return nil, true
	case "c":
		m.fopts = actions.FilterOptions{}
		m.refilter()
		return m.wantDetail(), true
	case "T":
		m.showTransitions()
		return nil, true
	case "ctrl+r", "f5":
		return m.Load(), true
	}
	return nil, false
}

// isDefaultFilter: bộ lọc rỗng (không có gì để xoá bằng Esc).
func isDefaultFilter(f actions.FilterOptions) bool { return f == actions.FilterOptions{} }

func idsOf(it *kn.Item) []string {
	if it == nil {
		return nil
	}
	return []string{it.ID}
}

func (m *Model) move(d int) tea.Cmd {
	if m.pane == paneDetail {
		m.detail.Scroll(d)
		return nil
	}
	m.lst.Move(d)
	return m.wantDetail()
}

func (m *Model) toggleAll() {
	all := true
	for _, ix := range m.view {
		if !m.sel.Has(m.Data.Items[ix].ID) {
			all = false
			break
		}
	}
	for _, ix := range m.view {
		id := m.Data.Items[ix].ID
		if all {
			delete(m.sel, id)
		} else {
			m.sel[id] = true
		}
	}
}

func (m *Model) textKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.fopts.Text, m.textIn = m.textOld, false
	case "enter":
		m.textIn = false
	default:
		in := inputOf(m.fopts.Text)
		in.Update(msg)
		m.fopts.Text = in.Text
	}
	m.refilter()
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
	m.dlg = &dialog{c: confirm.Model{Title: title, Body: body, YesLabel: m.t("kn.dialog.close"), NoLabel: m.t("kn.dialog.close")}}
}

// askReason mở hộp nhập lý do rồi gửi yêu cầu từ chối/loại bỏ mục it.
func (m *Model) askReason(it *kn.Item, id string, required bool) tea.Cmd {
	if it == nil {
		return nil
	}
	c := confirm.Model{Title: m.t("kn.reason.title." + strings.TrimPrefix(id, "knowledge.")), Danger: true, Input: true, Label: m.t("kn.reason.label"),
		YesLabel: m.t("kn.dialog.confirm"), NoLabel: m.t("kn.dialog.cancel"), Required: required, RequiredMsg: m.t("kn.reason.required"),
		Body: []string{m.th.Bold(it.Title), m.th.S(theme.Muted, it.ID)}}
	if !required {
		c.Placeholder = m.t("kn.reject.default_reason")
	}
	def := app.DefaultRejectReason
	ref := it.ID
	m.dlg = &dialog{c: c, onYes: func(v string) tea.Cmd {
		if v == "" {
			v = def
		}
		return request(id, app.RejectOptions{Refs: []string{ref}, Reason: v})
	}}
	return nil
}

// planBatch lập kế hoạch lô cho mục được chọn (hoặc cả danh sách đang lọc) rồi mở hộp xác nhận.
func (m *Model) planBatch(to string) tea.Cmd {
	refs := m.Selected()
	if len(refs) == 0 {
		for _, ix := range m.view {
			refs = append(refs, m.Data.Items[ix].ID)
		}
	}
	if len(refs) == 0 {
		m.setToast(m.t("kn.batch.empty"), true)
		return nil
	}
	m.setToast(m.cat.N("kn.batch.planning", len(refs)), false)
	return request("knowledge.plan_batch", app.BatchOptions{Refs: refs, To: to})
}

// openForm mở form của hành động id (nhãn theo khoá form.<id>.<trường>).
func (m *Model) openForm(id, cli string, defaults any) {
	f := form.New(id, cli, defaults)
	f.SetSuggestions("files", m.Data.Files)
	f.SetSuggestions("ids", m.Data.IDs())
	f.SetSuggestions("batches", m.Data.Batches())
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
		v := m.frm.Value()
		if m.frmID == "knowledge.filter" {
			m.fopts = v.(actions.FilterOptions)
			m.frm = nil
			m.refilter()
			return m.wantDetail()
		}
		id := m.frmID
		if vo, ok := v.(app.VerifyOptions); ok && vo.Apply {
			id = "knowledge.verify_apply"
		}
		m.frmBusy = true
		return request(id, v)
	}
	return nil
}

func (m *Model) showTransitions() {
	var body []string
	for i, r := range kn.TransitionTable {
		cond := r.Condition
		if k := "transition." + itoa(i); m.cat.Has(k) {
			cond = m.t(k)
		}
		body = append(body, m.th.S(theme.Accent, r.From)+" → "+m.th.S(theme.Accent, r.To)+"  "+cond)
	}
	body = append(body, "", m.th.S(theme.Muted, m.t("kn.transitions.note")))
	m.openInfo(m.t("kn.transitions.title"), body)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// ---- kết quả hành động ----

func (m *Model) onResult(r actions.ResultMsg) tea.Cmd {
	if strings.HasPrefix(r.ID, "knowledge.") || r.ID == "inspect" || r.ID == "bootstrap" || r.ID == "bootstrap.skill" || r.ID == "context" || strings.HasPrefix(r.ID, "bootstrap.") {
		if p := m.panelFor(r.ID); p != nil {
			return m.panelResult(p, r)
		}
	}
	if m.frmBusy && m.frmID == r.ID || (m.frmBusy && r.ID == "knowledge.verify_apply" && m.frmID == "knowledge.verify") {
		m.frmBusy = false
	}
	if r.Err != nil {
		if m.frm != nil && (r.ID == m.frmID) {
			m.frm.Err = ui.ErrText(m.cat, r.Err)
			return nil
		}
		m.setToast(ui.ErrText(m.cat, r.Err), true)
		return m.Load()
	}
	switch r.ID {
	case "knowledge.approve", "knowledge.approve_refresh", "knowledge.reject", "knowledge.deprecate":
		outs, _ := r.Value.([]app.ReviewOutcome)
		return m.afterOutcomes(outs)
	case "knowledge.plan_batch":
		m.toast = ""
		plan, ok := r.Value.(app.BatchPlan)
		if !ok {
			return nil
		}
		return m.onPlan(plan)
	case "knowledge.approve_batch", "knowledge.reject_batch":
		br, _ := r.Value.(app.BatchResult)
		m.sel.Clear()
		ok := 0
		for _, o := range br.Outcomes {
			if o.Err == nil {
				ok++
			}
		}
		key := "kn.batch.done"
		if r.ID == "knowledge.reject_batch" {
			key = "kn.batch.rejected"
		}
		m.setToast(m.t(key, i18n.A{"ok": ok, "blocked": len(br.Plan.Blocked), "failed": len(br.Outcomes) - ok}), len(br.Outcomes)-ok > 0)
		return m.Load()
	case "knowledge.edit":
		m.setToast(m.t("kn.edit.done"), false)
		return m.Load()
	case "knowledge.add":
		ar, ok := r.Value.(app.AddResult)
		if !ok {
			return nil
		}
		if !ar.Saved || ar.Item == nil {
			if m.frm != nil {
				m.frm.Err = m.t("kn.add.failed") + " " + strings.Join(findingLines(m.cat, ar.Check.Findings), "; ")
			}
			return nil
		}
		m.frm = nil
		m.setToast(m.t("kn.add.done", i18n.A{"id": ar.Item.ID, "status": ar.Item.Status}), false)
		return m.Load()
	case "knowledge.rm":
		m.frm = nil
		removed, _ := r.Value.([]string)
		m.setToast(m.cat.N("kn.rm.done", len(removed)), false)
		return m.Load()
	case "knowledge.verify", "knowledge.verify_apply":
		m.frm = nil
		if rep, ok := r.Value.(app.VerifyReport); ok {
			m.showVerify(rep, r.ID == "knowledge.verify_apply")
		}
		if r.ID == "knowledge.verify_apply" {
			return m.Load()
		}
	}
	return nil
}

// afterOutcomes tóm tắt kết quả duyệt một mục (hoặc vài mục) bằng thông báo ngắn, rồi nạp lại.
func (m *Model) afterOutcomes(outs []app.ReviewOutcome) tea.Cmd {
	for _, o := range outs {
		if o.Err != nil {
			msg := ui.ErrText(m.cat, o.Err)
			if o.Check != nil && len(o.Check.Findings) > 0 {
				msg += " · " + strings.Join(findingLines(m.cat, o.Check.Findings), "; ")
			}
			m.setToast(msg, true)
			return m.Load()
		}
	}
	m.setToast(m.cat.N("kn.review.done", len(outs)), false)
	return m.Load()
}

// onPlan mở hộp xác nhận lô: số mục sẽ ghi, và các mục bị loại kèm lý do.
func (m *Model) onPlan(p app.BatchPlan) tea.Cmd {
	approve := p.To == kn.StatusVerified
	byID := map[string]*kn.Item{}
	for _, it := range m.Data.Items {
		byID[it.ID] = it
	}
	title := func(id string) string {
		if it := byID[id]; it != nil {
			return it.Title
		}
		return ""
	}
	var body []string
	key := "kn.batch.reject.title"
	if approve {
		key = "kn.batch.approve.title"
	}
	c := confirm.Model{Title: m.cat.N(key, len(p.Ready)), YesLabel: m.t("kn.dialog.confirm"), NoLabel: m.t("kn.dialog.cancel"), Danger: !approve}
	if len(p.Ready) == 0 {
		body = append(body, m.th.S(theme.Warn, m.t("kn.batch.none_ready")))
	} else {
		body = append(body, m.cat.N("kn.batch.summary", len(p.Ready)))
	}
	if len(p.Blocked) > 0 {
		body = append(body, "", m.th.S(theme.Warn, m.cat.N("kn.batch.blocked", len(p.Blocked))))
		for i, b := range p.Blocked {
			if i == 12 {
				body = append(body, "  "+m.t("kn.batch.more", i18n.A{"count": len(p.Blocked) - 12}))
				break
			}
			why := m.t("kn.blocked." + b.Reason)
			if b.Reason == app.BlockedChecks {
				var codes []string
				for _, f := range b.Findings {
					codes = append(codes, m.findingLabel(f))
				}
				why += ": " + strings.Join(codes, ", ")
			}
			body = append(body, "  "+m.th.S(theme.Err, "✗ ")+short(b.ID)+" "+title(b.ID)+" — "+why)
		}
		if approve {
			body = append(body, m.th.S(theme.Muted, m.t("kn.batch.blocked_hint")))
		}
	}
	c.Body = body
	if len(p.Ready) == 0 {
		m.dlg = &dialog{c: c}
		m.dlg.c.YesLabel, m.dlg.c.NoLabel = m.t("kn.dialog.close"), m.t("kn.dialog.close")
		return nil
	}
	ready := p.Ready
	if !approve {
		c.Input, c.Label, c.Required, c.RequiredMsg = true, m.t("kn.reason.label"), true, m.t("kn.reason.required")
	}
	to := p.To
	m.dlg = &dialog{c: c, onYes: func(reason string) tea.Cmd {
		id := "knowledge.approve_batch"
		if to == kn.StatusDeprecated {
			id = "knowledge.reject_batch"
		}
		return request(id, app.BatchOptions{Refs: ready, To: to, Reason: reason})
	}}
	return nil
}

func (m *Model) showVerify(rep app.VerifyReport, applied bool) {
	var body []string
	for _, r := range rep.Results {
		it := rep.Items[r.ID]
		icon, role := "✓", theme.OK
		if !r.OK() {
			icon, role = "✗", theme.Err
		} else if len(r.Findings) > 0 {
			icon, role = "!", theme.Warn
		}
		title := ""
		if it != nil {
			title = it.Title
		}
		body = append(body, m.th.S(role, icon)+" "+short(r.ID)+" "+title)
		for _, f := range r.Findings {
			body = append(body, "    "+m.th.S(theme.Muted, findingIcon(f.Level))+" "+m.findingLabel(f)+" — "+f.Detail)
		}
	}
	for _, t := range rep.Transitions {
		body = append(body, m.th.S(theme.Accent, "→ ")+short(t.ID)+": "+t.From+" → "+t.To)
	}
	if len(rep.Results) == 0 {
		body = append(body, m.t("kn.verify.empty"))
	}
	if len(rep.NeedsReview) > 0 {
		body = append(body, "", m.th.S(theme.Warn, m.cat.N("kn.verify.needs_review", len(rep.NeedsReview))))
	}
	title := m.t("kn.verify.title")
	if applied {
		title = m.t("kn.verify.title_apply", i18n.A{"count": len(rep.Transitions)})
	}
	m.openInfo(title, body)
}

func findingIcon(level string) string {
	if level == kn.LevelError {
		return "✗"
	}
	return "!"
}

func (m *Model) findingLabel(f kn.Finding) string {
	if k := "finding." + f.Code; m.cat.Has(k) {
		return m.t(k)
	}
	return f.Code
}

// findingLines là các finding thành chuỗi ngắn "nhãn — chi tiết".
func findingLines(cat *i18n.Catalog, fs []kn.Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, ui.FindingText(cat, f))
	}
	return out
}

// short rút ID dài (ULID) còn 10 ký tự cuối cho dễ đọc trong hộp thoại.
func short(id string) string {
	if r := []rune(id); len(r) > 10 {
		return string(r[len(r)-10:])
	}
	return id
}

// keyOf đổi ID hành động của màn hình thành phím tương đương ở chế độ danh sách.
var keyOf = map[string]string{
	"knowledge.approve": "a", "knowledge.approve_refresh": "f", "knowledge.reject": "r", "knowledge.deprecate": "d",
	"knowledge.approve_batch": "A", "knowledge.reject_batch": "R", "knowledge.edit": "e", "knowledge.add": "n",
	"knowledge.rm": "delete", "knowledge.verify": "v", "knowledge.verify_apply": "v", "knowledge.list": "F", "knowledge.filter": "/",
	"knowledge.select": "x", "knowledge.transitions": "T", "knowledge.refresh": "ctrl+r", "knowledge.show": "enter",
}

// subOf đổi ID hành động của thẻ phụ thành tên thẻ (và chế độ Khảo sát).
var subOf = map[string]string{
	"knowledge.search": "search", "knowledge.stats": "stats", "knowledge.diff": "diff", "knowledge.revalidate": "revalidate",
	"bootstrap.publish": "publish", "bootstrap.publish_preview": "publish", "context": "context",
	"inspect": "survey", "bootstrap": "survey", "bootstrap.skill": "survey",
}

// Command thực hiện hành động theo ID (từ bảng lệnh): mở thẻ phụ tương ứng hoặc làm như bấm phím tương đương.
func (m *Model) Command(id string) tea.Cmd {
	if m.dlg != nil || m.frm != nil {
		return nil
	}
	switch id {
	case "inspect", "bootstrap", "bootstrap.skill":
		m.survM = map[string]int{"inspect": 0, "bootstrap": 1, "bootstrap.skill": 2}[id]
		m.pnls[subSurvey] = m.surv[m.survM]
	case "knowledge.next_view":
		m.setSub(m.sub + 1)
		return m.autoRun()
	case "knowledge.prev_view":
		m.setSub(m.sub - 1)
		return m.autoRun()
	case "knowledge.review":
		m.sub = subList
		return m.Focus(kn.Filter{Statuses: []string{kn.StatusCandidate, kn.StatusStale}})
	}
	if sub, ok := subOf[id]; ok {
		return m.EnterSub(sub)
	}
	m.sub = subList
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
	case "delete":
		return tea.KeyPressMsg{Code: tea.KeyDelete}
	case "ctrl+r":
		return tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}
	}
	r := []rune(s)
	return tea.KeyPressMsg{Code: r[0], Text: s}
}
