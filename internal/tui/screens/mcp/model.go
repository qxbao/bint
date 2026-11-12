package mcp

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"bint/internal/app"
	"bint/internal/i18n"
	"bint/internal/mcpcat"
	"bint/internal/tui/actions"
	"bint/internal/tui/components/confirm"
	"bint/internal/tui/components/form"
	"bint/internal/tui/components/list"
	"bint/internal/tui/components/viewer"
	"bint/internal/tui/jobs"
	"bint/internal/tui/screens/overview"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

// WideW: từ chiều rộng này danh sách và chi tiết hiện cạnh nhau; hẹp hơn thì một khung (Tab/Enter đổi).
const WideW = 100

// jobIDs là các hành động nền của màn hình (log hiện ở khung chi tiết khi đang chạy).
var jobIDs = []string{"mcp.check", "mcp.index", "mcp.restart"}

type pane int

const (
	paneList pane = iota
	paneDetail
)

// Options điều khiển màn hình.
type Options struct {
	Dir     string
	Version string
	// Load thay được trong kiểm thử; trường nil = nạp thật từ dịch vụ.
	Load Loaders
	// Gate cho biết hành động có dùng được không theo sổ hành động và chế độ (nil = luôn dùng được). Màn hình không tự kiểm chế độ chỉ đọc.
	Gate func(id string) actions.Availability
	// Jobs là bảng việc nền của mô hình gốc (chỉ đọc): log của việc check/index/restart đang chạy (nil = không có log trực tiếp).
	Jobs *jobs.Table
}

type dialog struct {
	c     confirm.Model
	onYes func() tea.Cmd
}

type slot struct {
	Loaded, Loading, again bool
	Err                    error
}

// outPane là kết quả của một hành động (log, kiểm tra, lập chỉ mục) hiện ở khung chi tiết thay cho chi tiết mặc định; Esc đóng.
type outPane struct {
	Title string
	Lines []string
}

// Model là trạng thái màn MCP.
type Model struct {
	o   Options
	cat *i18n.Catalog
	rw  ui.Rewriter
	th  theme.Theme
	ld  Loaders

	tab   int
	slots [numTabs]slot

	data   Data
	rows   []Row
	idx    []int // chỉ số trong rows của các dòng đang hiện (sau lọc dự án)
	grants GrantsData
	tools  []app.MCPTool
	// checks là kết quả check trong phiên này (không lưu xuống đĩa), theo ID service.
	checks   map[string]app.MCPCheckResult
	projOnly bool

	lst  [numTabs]list.Model
	pane pane
	det  viewer.Model
	out  *outPane

	dlg     *dialog
	frm     *form.Model
	frmSvc  string // service đang chọn lúc mở form thêm (cho dòng biến còn thiếu)
	frmBusy bool
	pending int // số yêu cầu đang chờ kết quả

	toast    string
	toastErr bool
	w, h     int
}

// New dựng màn hình; gọi SetEnv trước khi vẽ.
func New(o Options) *Model {
	return &Model{o: o, ld: o.Load.withDefaults(), checks: map[string]app.MCPCheckResult{}}
}

// SetEnv đặt danh mục chuỗi và bảng màu (gọi lại khi đổi ngôn ngữ).
func (m *Model) SetEnv(th theme.Theme, rw ui.Rewriter) { m.th, m.rw, m.cat = th, rw, rw.Cat }

func (m *Model) t(key string, a ...i18n.A) string { return m.cat.T(key, a...) }

func (m *Model) setToast(s string, isErr bool) { m.toast, m.toastErr = s, isErr }

func (m *Model) fail(err error) { m.setToast(ui.ErrText(m.rw, err), true) }

// allowed kiểm hành động theo sổ; bị khoá/ẩn thì báo lý do bằng toast. Đây là nơi duy nhất màn hình hỏi chế độ (mục 7.4).
func (m *Model) allowed(id string) bool {
	if m.o.Gate == nil {
		return true
	}
	switch m.o.Gate(id) {
	case actions.Locked:
		m.setToast(m.t("ui.locked.agent"), true)
		return false
	case actions.Hidden:
		m.setToast(m.t("mc.denied.readonly"), true)
		return false
	}
	return true
}

func (m *Model) request(id string, opts any) tea.Cmd {
	m.pending++
	return func() tea.Msg { return actions.RequestMsg{ID: id, Opts: opts} }
}

// runningJob trả việc nền đang chạy của màn hình (nil nếu không có).
func (m *Model) runningJob() *jobs.Job {
	if m.o.Jobs == nil {
		return nil
	}
	for _, id := range jobIDs {
		if j := m.o.Jobs.Running(id); j != nil {
			return j
		}
	}
	return nil
}

// Tab trả thẻ phụ đang hiện.
func (m *Model) Tab() int { return m.tab }

// Rows trả các dòng dịch vụ đang hiện (đã lọc).
func (m *Model) Rows() []Row {
	out := make([]Row, 0, len(m.idx))
	for _, i := range m.idx {
		out = append(out, m.rows[i])
	}
	return out
}

// Cursor trả dòng dịch vụ dưới con trỏ (nil nếu trống hoặc không ở thẻ Dịch vụ).
func (m *Model) Cursor() *Row {
	c := m.lst[TabServices].Cursor
	if c < 0 || c >= len(m.idx) {
		return nil
	}
	return &m.rows[m.idx[c]]
}

// ---- nạp dữ liệu ----

// Ensure nạp dữ liệu của thẻ đang hiện lần đầu (ui.Screen).
func (m *Model) Ensure() tea.Cmd { return m.ensureTab(m.tab) }

func (m *Model) ensureTab(tab int) tea.Cmd {
	if m.slots[tab].Loaded || m.slots[tab].Loading {
		return nil
	}
	return m.load(tab)
}

func (m *Model) load(tab int) tea.Cmd {
	if m.slots[tab].Loading {
		m.slots[tab].again = true
		return nil
	}
	m.slots[tab].Loading = true
	ctx, dir, ver, ld := context.Background(), m.o.Dir, m.o.Version, m.ld
	return func() tea.Msg {
		var v any
		var err error
		switch tab {
		case TabServices:
			v, err = ld.Services(ctx, dir)
		case TabGrants:
			v, err = ld.Grants(ctx, dir)
		case TabTools:
			v, err = ld.Tools(ctx, dir, ver)
		}
		return LoadedMsg{Tab: tab, Value: v, Err: err}
	}
}

// reloadAll nạp lại các thẻ đã từng nạp (dữ liệu cũ vẫn hiện cho tới khi dữ liệu mới về).
func (m *Model) reloadAll() tea.Cmd {
	var cmds []tea.Cmd
	for tab := range m.slots {
		if tab == m.tab || m.slots[tab].Loaded {
			cmds = append(cmds, m.load(tab))
		}
	}
	return tea.Batch(cmds...)
}

// rebuild suy lại trạng thái các dòng (sau khi nạp dữ liệu, có kết quả check hoặc đổi bộ lọc), giữ con trỏ theo ID service.
func (m *Model) rebuild() {
	keep := ""
	if r := m.Cursor(); r != nil {
		keep = r.Svc.ID
	}
	m.rows = Rows(m.data, m.checks)
	m.idx = Visible(m.rows, m.projOnly)
	m.lst[TabServices].SetN(len(m.idx))
	for pos, i := range m.idx {
		if keep != "" && m.rows[i].Svc.ID == keep {
			m.lst[TabServices].MoveTo(pos)
			break
		}
	}
}

// ---- trạng thái màn hình (ui.Screen) ----

// Capturing: màn hình đang nhận mọi phím chữ (form, hộp thoại).
func (m *Model) Capturing() bool { return m.dlg != nil || m.frm != nil }

// NotList: phím của từng thẻ nằm ở viền dưới của khung (chân phím chung chỉ giữ phím ghim và toàn cục).
func (m *Model) NotList() bool { return true }

// WantsInterrupt: có việc nền của màn hình đang chạy (Esc huỷ việc đó).
func (m *Model) WantsInterrupt() bool { return m.dlg == nil && m.frm == nil && m.runningJob() != nil }

// Busy: có việc nạp dữ liệu hoặc yêu cầu đang chạy.
func (m *Model) Busy() bool {
	if m.pending > 0 || m.runningJob() != nil {
		return true
	}
	for _, s := range m.slots {
		if s.Loading {
			return true
		}
	}
	return false
}

// Toast trả nguyên văn thông báo hiện tại (ui.Toaster).
func (m *Model) Toast() (string, bool) { return m.toast, m.toastErr }

// SubTab trả tên thẻ phụ đang chọn (ui.SubTabber).
func (m *Model) SubTab() string { return m.t("mc.tab." + TabNames[m.tab]) }

// HelpTab trả mã thẻ phụ đang xem (ui.Helper).
func (m *Model) HelpTab() string { return TabNames[m.tab] }

// ---- thông điệp ----

// Update xử lý thông điệp không phải phím; trả lệnh kế tiếp.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case LoadedMsg:
		return m.onLoaded(msg)
	case actions.DeniedMsg:
		m.pending = max(m.pending-1, 0)
		why := m.t(map[bool]string{true: "ui.locked.agent", false: "mc.denied.readonly"}[msg.Locked])
		m.setToast(why, true)
		if m.frmBusy && m.frm != nil {
			m.frmBusy = false
			m.frm.Err = why
		}
	case actions.ResultMsg:
		m.pending = max(m.pending-1, 0)
		return m.onResult(msg)
	}
	return nil
}

func (m *Model) onLoaded(msg LoadedMsg) tea.Cmd {
	s := &m.slots[msg.Tab]
	s.Loading, s.Err = false, msg.Err
	var next tea.Cmd
	if s.again {
		s.again = false
		next = m.load(msg.Tab)
	}
	if msg.Err != nil {
		return next
	}
	s.Loaded = true
	switch msg.Tab {
	case TabServices:
		if d, ok := msg.Value.(Data); ok {
			m.data = d
			m.rebuild()
		}
	case TabGrants:
		if g, ok := msg.Value.(GrantsData); ok {
			m.grants = g
			m.lst[TabGrants].SetN(len(g.Roles))
		}
	case TabTools:
		if tl, ok := msg.Value.([]app.MCPTool); ok {
			m.tools = tl
			m.lst[TabTools].SetN(len(tl))
		}
	}
	return next
}

// finishForm đóng form đang chờ kết quả.
func (m *Model) finishForm() {
	m.frm, m.frmBusy = nil, false
}

func (m *Model) onResult(msg actions.ResultMsg) tea.Cmd {
	switch msg.ID {
	case "mcp.add":
		if msg.Err != nil {
			if m.frm != nil { // giữ form để sửa lại (vd. tên service lạ, lệnh không hợp)
				m.frmBusy = false
				m.frm.Err = ui.ErrText(m.rw, msg.Err)
				return nil
			}
			m.fail(msg.Err)
			return nil
		}
		m.finishForm()
		r, ok := msg.Value.(app.MCPAddResult)
		if !ok {
			return m.reloadAll()
		}
		if r.Check.Service != "" {
			m.checks[r.Service] = r.Check
			m.rebuild()
		}
		status := r.Check.Status
		if status == "" {
			status = app.MCPStatusOK
		}
		m.setToast(m.t("mc.add.done", i18n.A{"service": r.Service, "status": m.t("mc.chk." + status)}), status == app.MCPStatusFail || status == app.MCPStatusDegraded)
		if r.InstallHint != "" {
			m.showOut(m.t("mc.add.title", i18n.A{"service": r.Service}), m.addLines(r))
		}
		return m.reloadAll()
	case "mcp.remove":
		if msg.Err != nil {
			m.fail(msg.Err)
			return nil
		}
		r, _ := msg.Value.(app.MCPRemoveResult)
		o, _ := msg.Opts.(app.MCPRemoveOptions)
		id := r.Service
		if id == "" {
			id = o.Service
		}
		delete(m.checks, id)
		m.rebuild()
		key := "mc.remove.done"
		if r.ProjectUses {
			key = "mc.remove.done_used"
		}
		m.setToast(m.t(key, i18n.A{"service": id}), r.ProjectUses)
		return m.reloadAll()
	case "mcp.check":
		out, _ := msg.Value.(actions.MCPCheckOutcome)
		for _, r := range out.Results {
			m.checks[r.Service] = r
		}
		m.rebuild()
		if len(out.Log) > 0 || len(out.Results) > 0 {
			m.showOut(m.t("mc.check.title"), m.checkLines(out))
		}
		if msg.Err != nil {
			m.fail(msg.Err)
			return nil
		}
		bad := 0
		for _, r := range out.Results {
			if r.Status != app.MCPStatusOK {
				bad++
			}
		}
		m.setToast(m.t("mc.check.done", i18n.A{"count": len(out.Results), "bad": bad}), bad > 0)
	case "mcp.index":
		out, _ := msg.Value.(actions.MCPIndexOutcome)
		if len(out.Log) > 0 || out.Result.Service != "" {
			m.showOut(m.t("mc.index.title"), m.indexLines(out))
		}
		if msg.Err != nil {
			m.fail(msg.Err)
			return nil
		}
		m.setToast(m.t("mc.index.done", i18n.A{"action": out.Result.Action}), false)
		return m.reloadAll()
	case "mcp.restart":
		out, _ := msg.Value.(actions.MCPRestartOutcome)
		if msg.Err != nil {
			m.fail(msg.Err)
			return nil
		}
		m.setToast(m.t("mc.restart.done", i18n.A{"service": out.Result.Service, "stopped": out.Result.Stopped}), false)
		return m.reloadAll()
	case "mcp.log":
		if msg.Err != nil {
			m.fail(msg.Err)
			return nil
		}
		if r, ok := msg.Value.(app.MCPGatewayLogResult); ok {
			m.showOut(m.t("mc.log.title", i18n.A{"service": r.Service}), m.logLines(r))
		}
	case "mcp.gateway_start":
		if msg.Err != nil {
			m.fail(msg.Err)
			return nil
		}
		r, _ := msg.Value.(app.MCPGatewayStartResult)
		key := "mc.gw.started"
		if !r.Started {
			key = "mc.gw.already"
		}
		m.setToast(m.t(key, i18n.A{"pid": r.PID, "port": r.Port}), false)
		return m.reloadAll()
	case "mcp.gateway_stop":
		if msg.Err != nil {
			m.fail(msg.Err)
			return nil
		}
		r, _ := msg.Value.(app.MCPGatewayStopResult)
		key := "mc.gw.stopped"
		if !r.Stopped {
			key = "mc.gw.not_running"
		}
		m.setToast(m.t(key), false)
		return m.reloadAll()
	}
	return nil
}

func (m *Model) showOut(title string, lines []string) {
	m.out, m.pane = &outPane{Title: title, Lines: lines}, paneDetail
	m.det.Reset()
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
	if j := m.runningJob(); j != nil && (s == "esc" || s == "ctrl+c") {
		id := j.Action
		return func() tea.Msg { return actions.CancelMsg{ID: id} }, true
	}
	switch s {
	case ".":
		return m.setTab((m.tab + 1) % numTabs), true
	case ",":
		return m.setTab((m.tab + numTabs - 1) % numTabs), true
	case "e":
		return m.cmdConfigEdit(), true
	}
	if m.tab == TabServices {
		if cmd, ok := m.serviceKey(s); ok {
			return cmd, true
		}
	} else if s == "enter" {
		m.pane = paneDetail
		return nil, true
	}
	return m.listKey(s)
}

func (m *Model) serviceKey(s string) (tea.Cmd, bool) {
	switch s {
	case "enter":
		m.out, m.pane = nil, paneDetail
		return nil, true
	case "p":
		m.toggleFilter()
		return nil, true
	case "a":
		return m.cmdAdd(), true
	case "x":
		return m.cmdRemove(), true
	case "c":
		return m.cmdCheck(), true
	case "i":
		return m.cmdIndex(), true
	case "R":
		return m.cmdRestart(), true
	case "l":
		return m.cmdLog(), true
	case "G":
		return m.cmdGatewayStart(), true
	case "S":
		return m.cmdGatewayStop(), true
	}
	return nil, false
}

func (m *Model) listKey(s string) (tea.Cmd, bool) {
	lst := &m.lst[m.tab]
	switch s {
	case "j", "down":
		m.nav(1)
		return nil, true
	case "k", "up":
		m.nav(-1)
		return nil, true
	case "pgdown", "ctrl+d":
		m.page(1)
		return nil, true
	case "pgup", "ctrl+u":
		m.page(-1)
		return nil, true
	case "home", "g":
		if m.pane == paneDetail {
			m.det.Home()
		} else {
			lst.Home()
			m.det.Reset()
		}
		return nil, true
	case "end", "G":
		if m.pane == paneDetail {
			m.det.End()
		} else {
			lst.End()
			m.det.Reset()
		}
		return nil, true
	case "tab":
		m.pane = (m.pane + 1) % 2
		return nil, true
	case "esc":
		switch {
		case m.out != nil:
			m.out, m.pane = nil, paneList
			m.det.Reset()
		case m.pane == paneDetail:
			m.pane = paneList
		}
		return nil, true
	case "ctrl+r", "f5":
		m.out = nil
		return m.reload(), true
	}
	return nil, false
}

func (m *Model) reload() tea.Cmd { return m.load(m.tab) }

func (m *Model) nav(d int) {
	if m.pane == paneDetail {
		m.det.Scroll(d)
		return
	}
	m.lst[m.tab].Move(d)
	m.det.Reset()
	m.out = nil
}

func (m *Model) page(d int) {
	if m.pane == paneDetail {
		m.det.Page(d)
		return
	}
	m.lst[m.tab].Page(d)
	m.det.Reset()
	m.out = nil
}

func (m *Model) setTab(i int) tea.Cmd {
	m.tab, m.pane, m.out = i, paneList, nil
	m.det.Reset()
	return m.ensureTab(i)
}

func (m *Model) toggleFilter() {
	m.projOnly = !m.projOnly
	m.rebuild()
	m.det.Reset()
}

// ---- lệnh ----

func (m *Model) busyJob(id string) bool {
	if m.o.Jobs != nil && m.o.Jobs.Running(id) != nil {
		m.setToast(m.t("mc.err.job_running"), true)
		return true
	}
	return false
}

func (m *Model) cmdAdd() tea.Cmd {
	if !m.allowed("mcp.add") {
		return nil
	}
	o := app.MCPAddOptions{}
	m.frmSvc = ""
	if r := m.Cursor(); r != nil {
		o.Service, m.frmSvc = r.Svc.ID, r.Svc.ID
	}
	f := form.New("mcp.add", "mcp add", o)
	f.SetSuggestions("mcp_services", mcpcat.IDs())
	m.frm, m.frmBusy = &f, false
	return nil
}

func (m *Model) cmdRemove() tea.Cmd {
	if !m.allowed("mcp.remove") {
		return nil
	}
	r := m.Cursor()
	if r == nil {
		return nil
	}
	if !r.Svc.Registered {
		m.setToast(m.t("mc.err.not_registered", i18n.A{"service": r.Svc.ID}), true)
		return nil
	}
	body := []string{m.t("mc.remove.body", i18n.A{"service": r.Svc.ID}), "", m.th.S(theme.Muted, m.t("mc.remove.secrets", i18n.A{"path": m.data.SecretsFile}))}
	if r.Svc.ProjectUses {
		body = append(body, "", m.th.S(theme.Warn, m.t("mc.remove.used")))
	}
	id := r.Svc.ID
	m.dlg = &dialog{c: confirm.Model{Title: m.t("mc.remove.title", i18n.A{"service": id}), Body: body, Danger: true,
		YesLabel: m.t("mc.remove.yes"), NoLabel: m.t("mc.no")},
		onYes: func() tea.Cmd { return m.request("mcp.remove", app.MCPRemoveOptions{Service: id}) }}
	return nil
}

func (m *Model) cmdCheck() tea.Cmd {
	if !m.allowed("mcp.check") || m.busyJob("mcp.check") {
		return nil
	}
	o := app.MCPCheckOptions{}
	if r := m.Cursor(); r != nil && r.Svc.Registered {
		o.Service = r.Svc.ID
	}
	return m.request("mcp.check", o)
}

func (m *Model) cmdIndex() tea.Cmd {
	if !m.allowed("mcp.index") || m.busyJob("mcp.index") {
		return nil
	}
	r := m.Cursor()
	switch {
	case r == nil:
		return nil
	case r.Svc.ID != "codegraph":
		m.setToast(m.t("mc.err.index_only"), true)
		return nil
	case !r.Svc.Registered:
		m.setToast(m.t("mc.err.not_registered", i18n.A{"service": r.Svc.ID}), true)
		return nil
	}
	return m.request("mcp.index", app.MCPIndexOptions{Service: r.Svc.ID})
}

func (m *Model) cmdRestart() tea.Cmd {
	if !m.allowed("mcp.restart") || m.busyJob("mcp.restart") {
		return nil
	}
	r := m.Cursor()
	switch {
	case r == nil:
		return nil
	case m.data.Gateway == nil:
		m.setToast(m.t("mc.err.gw_down"), true)
		return nil
	}
	id := r.Svc.ID
	body := []string{m.t("mc.restart.body", i18n.A{"service": id})}
	m.dlg = &dialog{c: confirm.Model{Title: m.t("mc.restart.title", i18n.A{"service": id}), Body: body, Danger: true,
		YesLabel: m.t("mc.restart.yes"), NoLabel: m.t("mc.no")},
		onYes: func() tea.Cmd { return m.request("mcp.restart", app.MCPGatewayRestartOptions{Service: id}) }}
	return nil
}

func (m *Model) cmdLog() tea.Cmd {
	if !m.allowed("mcp.log") {
		return nil
	}
	r := m.Cursor()
	if r == nil {
		return nil
	}
	return m.request("mcp.log", app.MCPGatewayLogOptions{Service: r.Svc.ID, Lines: 50})
}

func (m *Model) cmdGatewayStart() tea.Cmd {
	if !m.allowed("mcp.gateway_start") {
		return nil
	}
	if m.data.Gateway != nil {
		m.setToast(m.t("mc.gw.already", i18n.A{"pid": m.data.Gateway.PID, "port": m.data.Gateway.Port}), false)
		return nil
	}
	return m.request("mcp.gateway_start", app.MCPGatewayStartOptions{})
}

func (m *Model) cmdGatewayStop() tea.Cmd {
	if !m.allowed("mcp.gateway_stop") {
		return nil
	}
	if m.data.Gateway == nil {
		m.setToast(m.t("mc.gw.not_running"), false)
		return nil
	}
	m.dlg = &dialog{c: confirm.Model{Title: m.t("mc.stop.title"), Body: []string{m.t("mc.stop.body")}, Danger: true,
		YesLabel: m.t("mc.stop.yes"), NoLabel: m.t("mc.no")},
		onYes: func() tea.Cmd { return m.request("mcp.gateway_stop", app.MCPGatewayStopOptions{}) }}
	return nil
}

// cmdConfigEdit nhờ mô hình gốc mở luồng sửa cấu hình của màn Hệ thống (system.config_edit: chọn lớp, $EDITOR, Validate khi lưu, gộp ba
// chiều). Màn MCP không chép luồng đó; cả hai hành động đều là Writes nên bị ẩn ở chế độ chỉ đọc.
func (m *Model) cmdConfigEdit() tea.Cmd {
	if !m.allowed("mcp.config_edit") {
		return nil
	}
	return func() tea.Msg { return overview.NavMsg{Screen: actions.System, ConfigEdit: true} }
}

// ---- hộp thoại và form ----

func (m *Model) dialogKey(msg tea.KeyPressMsg) tea.Cmd {
	d := m.dlg
	switch d.c.Update(msg) {
	case confirm.Yes:
		m.dlg = nil
		if d.onYes != nil {
			return d.onYes()
		}
	case confirm.No:
		m.dlg = nil
	}
	return nil
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
		m.frmBusy = true
		return m.request("mcp.add", m.frm.Value())
	}
	return nil
}

// Command thực hiện hành động theo ID (từ bảng lệnh).
func (m *Model) Command(id string) tea.Cmd {
	on := func(tab int, f func() tea.Cmd) tea.Cmd {
		m.tab = tab
		return tea.Batch(m.ensureTab(tab), f())
	}
	switch id {
	case "mcp.next_view":
		return m.setTab((m.tab + 1) % numTabs)
	case "mcp.prev_view":
		return m.setTab((m.tab + numTabs - 1) % numTabs)
	case "mcp.refresh":
		m.out = nil
		return m.reload()
	case "mcp.filter":
		m.tab = TabServices
		m.toggleFilter()
		return m.ensureTab(TabServices)
	case "mcp.list", "mcp.gateway_status":
		return m.setTab(TabServices)
	case "mcp.grants":
		return m.setTab(TabGrants)
	case "mcp.tools":
		return m.setTab(TabTools)
	case "mcp.add":
		return on(TabServices, m.cmdAdd)
	case "mcp.remove":
		return on(TabServices, m.cmdRemove)
	case "mcp.check":
		return on(TabServices, m.cmdCheck)
	case "mcp.index":
		return on(TabServices, m.cmdIndex)
	case "mcp.restart":
		return on(TabServices, m.cmdRestart)
	case "mcp.log":
		return on(TabServices, m.cmdLog)
	case "mcp.gateway_start":
		return on(TabServices, m.cmdGatewayStart)
	case "mcp.gateway_stop":
		return on(TabServices, m.cmdGatewayStop)
	case "mcp.config_edit":
		return m.cmdConfigEdit()
	}
	return nil
}

// ---- dựng dòng kết quả ----

func (m *Model) addLines(r app.MCPAddResult) []string {
	out := []string{m.th.S(theme.Accent, r.Service), ""}
	if r.InstallHint != "" {
		out = append(out, m.th.S(theme.Warn, m.t("mc.add.install")), "  "+r.InstallHint, "")
	}
	for _, w := range r.Warnings {
		out = append(out, m.th.S(theme.Warn, "! ")+w)
	}
	return out
}

func (m *Model) checkLines(o actions.MCPCheckOutcome) []string {
	out := append([]string(nil), o.Log...)
	for _, r := range o.Results {
		out = append(out, "", m.th.S(m.chkRole(r.Status), "• "+r.Service+" — "+m.t("mc.chk."+r.Status)))
		if len(r.Tools) > 0 {
			out = append(out, "  "+m.t("mc.chk.tools", i18n.A{"count": len(r.Tools)})+": "+strings.Join(r.Tools, ", "))
		}
		if len(r.Missing) > 0 {
			out = append(out, "  "+m.t("mc.chk.missing")+": "+strings.Join(r.Missing, ", "))
		}
		if len(r.Extra) > 0 {
			out = append(out, "  "+m.t("mc.chk.extra")+": "+strings.Join(r.Extra, ", "))
		}
		if len(r.MissingEnv) > 0 {
			out = append(out, "  "+m.t("mc.chk.missing_env")+": "+strings.Join(r.MissingEnv, ", "))
		}
		for _, w := range r.Warnings {
			out = append(out, "  "+m.th.S(theme.Warn, "! ")+w)
		}
	}
	return out
}

func (m *Model) chkRole(status string) theme.Role {
	switch status {
	case app.MCPStatusOK:
		return theme.OK
	case app.MCPStatusDegraded:
		return theme.Warn
	}
	return theme.Err
}

func (m *Model) indexLines(o actions.MCPIndexOutcome) []string {
	out := append([]string(nil), o.Log...)
	if o.Result.Service == "" {
		return out
	}
	out = append(out, "", m.th.S(theme.OK, m.t("mc.index.action", i18n.A{"action": o.Result.Action, "cwd": o.Result.Cwd})))
	if o.Result.ExcludeAdded {
		out = append(out, m.t("mc.index.exclude", i18n.A{"file": o.Result.ExcludeFile}))
	}
	return out
}

func (m *Model) logLines(r app.MCPGatewayLogResult) []string {
	var out []string
	for _, u := range r.Upstreams {
		state := m.t("mc.log.stopped")
		if u.Running {
			state = m.t("mc.log.running")
		}
		out = append(out, m.th.S(theme.Accent, m.t("mc.log.stderr", i18n.A{"partition": u.Partition, "state": state})))
		if u.Stderr == "" {
			out = append(out, "  "+m.th.S(theme.Muted, m.t("mc.log.empty")))
		}
		for _, l := range strings.Split(u.Stderr, "\n") {
			if l != "" {
				out = append(out, "  "+l)
			}
		}
		out = append(out, "")
	}
	out = append(out, m.th.S(theme.Accent, m.t("mc.log.file", i18n.A{"path": r.LogFile, "count": len(r.Lines)})))
	if len(r.Lines) == 0 {
		out = append(out, "  "+m.th.S(theme.Muted, m.t("mc.log.empty")))
	}
	for _, l := range r.Lines {
		out = append(out, "  "+l)
	}
	if r.Note != "" {
		out = append(out, "", m.th.S(theme.Warn, m.t("mc.log.note", i18n.A{"note": r.Note})))
	}
	return out
}
