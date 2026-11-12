package system

import (
	"context"
	"errors"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"bint/internal/app"
	"bint/internal/config"
	"bint/internal/hcomp"
	"bint/internal/i18n"
	"bint/internal/tui/actions"
	"bint/internal/tui/components/confirm"
	"bint/internal/tui/components/form"
	"bint/internal/tui/components/input"
	"bint/internal/tui/components/list"
	"bint/internal/tui/components/viewer"
	"bint/internal/tui/jobs"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

// WideW: từ chiều rộng này danh sách và chi tiết hiện cạnh nhau; hẹp hơn thì một khung (Tab/Enter đổi).
const WideW = 100

type pane int

const (
	paneList pane = iota
	paneDetail
)

// Options điều khiển màn hình.
type Options struct {
	Dir      string
	Version  string
	ReadOnly bool
	// Load thay được trong kiểm thử; trường nil = nạp thật từ dịch vụ.
	Load Loaders
	// Gate cho biết hành động có dùng được không theo sổ hành động và chế độ (nil = luôn dùng được).
	Gate func(id string) actions.Availability
	// Jobs là bảng việc nền của mô hình gốc (chỉ đọc): thẻ Cập nhật hiện log go install đang chạy (nil = không có log trực tiếp).
	Jobs *jobs.Table
	// CanRestart: nền tảng khởi động lại được bằng exec (Unix); Windows chỉ hiện hộp "đóng và mở lại bint".
	CanRestart bool
}

type dialog struct {
	c     confirm.Model
	onYes func(value string) tea.Cmd
	onNo  func() tea.Cmd
}

type slot struct {
	Loaded, Loading bool
	Err             error
}

// outPane là kết quả của một hành động đọc (render, nội dung/diff/phiên bản skill, check, regress, sync) hiện ở
// khung chi tiết thay cho chi tiết mặc định; Esc đóng.
type outPane struct {
	Title string
	Lines []string
}

// Model là trạng thái màn Hệ thống.
type Model struct {
	o   Options
	cat *i18n.Catalog
	rw  ui.Rewriter
	th  theme.Theme
	ld  Loaders

	tab   int
	slots [numTabs]slot
	rtSl  slot // trạng thái hook/MCP (thẻ Harness) nạp riêng vì có thể gọi chương trình ngoài
	// again: có yêu cầu nạp lại trong lúc đang nạp; nạp thêm một lần khi lần này xong (không để kết quả cũ ghi đè mới).
	again   [numTabs]bool
	againRT bool

	cfg    app.ConfigViewData
	har    HarnessData
	rt     []app.HarnessStatus
	comps  ComponentsData
	skills app.SkillList
	doctor *app.DoctorReport
	// Thẻ Cài đặt repo: trạng thái init và giá trị khởi đầu của form đang mở (để so lấy phần đã đổi). Thẻ Cập nhật: thông tin bản
	// đang chạy và phiên cập nhật gần nhất.
	setup     app.InitState
	setupInit app.InitFlagOptions
	// Chạy Cài đặt repo nhiều bước (app.SetupSteps): các bước, kế hoạch đã nhận, bước đang chờ kết quả, báo cáo gộp.
	setupSteps   []app.InitFlagOptions
	setupPlans   []app.InitPlan
	setupIdx     int
	setupRep     []string
	setupProblem bool
	// setupPending: bảng lệnh yêu cầu mở form khi thẻ chưa nạp xong; onLoaded mở sau khi nạp.
	setupPending bool
	updInfo      UpdateInfo
	upd          updateState
	// versions: phiên bản harness đã lấy (V), theo tên; verBusy: tên đang lấy.
	versions map[string]string
	verBusy  map[string]bool

	lst  [numTabs]list.Model
	pane pane
	det  viewer.Model
	out  *outPane

	// Thẻ Thành phần: bộ lọc loại/trạng thái (form F) và chữ (/).
	kindF, statusF string
	filter         string
	textIn         bool
	textOld        string
	compIdx        []int
	focusKind      hcomp.Kind
	focusID        string
	// Thẻ Skill: xem bản gốc (--core) hay đã ghép lớp phủ.
	coreView bool
	// Thẻ Doctor: chỉ hiện cảnh báo/lỗi.
	onlyIssues bool
	docIdx     []int
	// Thẻ Chỉ mục: kết quả lần dựng lại gần nhất.
	idxRes *app.IndexRebuildResult
	// Sửa cấu hình: phiên đang mở ($EDITOR) hoặc đang chờ xác nhận.
	edit *app.ConfigEdit

	dlg      *dialog
	frm      *form.Model
	frmID    string
	frmBusy  bool
	pending  int // số yêu cầu đang chờ kết quả
	toast    string
	toastErr bool

	w, h int
}

// New dựng màn hình; gọi SetEnv trước khi vẽ.
func New(o Options) *Model {
	return &Model{o: o, ld: o.Load.withDefaults(), versions: map[string]string{}, verBusy: map[string]bool{}}
}

// SetEnv đặt danh mục chuỗi và bảng màu (gọi lại khi đổi ngôn ngữ).
func (m *Model) SetEnv(th theme.Theme, rw ui.Rewriter) { m.th, m.rw, m.cat = th, rw, rw.Cat }

func (m *Model) t(key string, a ...i18n.A) string { return m.cat.T(key, a...) }

func (m *Model) setToast(s string, isErr bool) { m.toast, m.toastErr = s, isErr }

func (m *Model) allowed(id string) bool {
	if m.o.Gate == nil {
		return true
	}
	switch m.o.Gate(id) {
	case actions.Locked:
		m.setToast(m.t("ui.locked.agent"), true)
		return false
	case actions.Hidden:
		m.setToast(m.t("sy.denied.readonly"), true)
		return false
	}
	return true
}

func (m *Model) request(id string, opts any) tea.Cmd {
	m.pending++
	return func() tea.Msg { return actions.RequestMsg{ID: id, Opts: opts} }
}

// Tab trả thẻ con đang hiện.
func (m *Model) Tab() int { return m.tab }

// ---- nạp dữ liệu ----

// Ensure nạp dữ liệu của thẻ đang hiện lần đầu (ui.Screen).
func (m *Model) Ensure() tea.Cmd { return m.ensureTab(m.tab) }

func (m *Model) ensureTab(tab int) tea.Cmd {
	if tab == TabLanguage { // dùng chung dữ liệu với thẻ Cấu hình
		tab = TabConfig
	}
	if m.slots[tab].Loaded || m.slots[tab].Loading {
		if tab == TabHarness && !m.rtSl.Loaded && !m.rtSl.Loading {
			return m.loadRuntime()
		}
		return nil
	}
	return m.load(tab)
}

// load nạp lại dữ liệu của một thẻ trong tea.Cmd (Doctor và harness có thể chậm: gọi git, chương trình harness).
func (m *Model) load(tab int) tea.Cmd {
	if tab == TabLanguage {
		tab = TabConfig
	}
	if tab == TabIndex { // thẻ Chỉ mục không có dữ liệu để nạp
		m.slots[tab].Loaded = true
		return nil
	}
	if m.slots[tab].Loading {
		m.again[tab] = true
		return nil
	}
	m.slots[tab].Loading = true
	ctx, dir, ver, ld := context.Background(), m.o.Dir, m.o.Version, m.ld
	cmd := func() tea.Msg {
		var v any
		var err error
		switch tab {
		case TabConfig:
			v, err = ld.Config(ctx, dir)
		case TabHarness:
			v, err = ld.Harness(ctx, dir)
		case TabComponents:
			v, err = ld.Components(ctx, dir)
		case TabSkills:
			v, err = ld.Skills(ctx, dir)
		case TabDoctor:
			v, err = ld.Doctor(ctx, dir)
		case TabSetup:
			v, err = ld.Init(ctx, dir)
		case TabUpdate:
			v, err = ld.UpdateInfo(ctx, dir, ver)
		}
		return LoadedMsg{Tab: tab, Value: v, Err: err}
	}
	if tab == TabHarness {
		return tea.Batch(cmd, m.loadRuntime())
	}
	return cmd
}

func (m *Model) loadRuntime() tea.Cmd {
	if m.rtSl.Loading {
		m.againRT = true
		return nil
	}
	m.rtSl.Loading = true
	ctx, dir, rt := context.Background(), m.o.Dir, m.ld.Runtime
	return func() tea.Msg {
		st, err := rt(ctx, dir)
		return RuntimeMsg{Status: st, Err: err}
	}
}

// reload nạp lại thẻ tab; dữ liệu cũ vẫn hiện cho tới khi dữ liệu mới về (không nhấp nháy "đang tải").
func (m *Model) reload(tab int) tea.Cmd {
	if tab == TabLanguage {
		tab = TabConfig
	}
	return m.load(tab)
}

// rowCount là số hàng của danh sách ở thẻ tab.
func (m *Model) rowCount(tab int) int {
	switch tab {
	case TabConfig:
		return 1 + len(m.cfg.Groups)
	case TabHarness:
		return len(m.har.Infos)
	case TabComponents:
		return len(m.compIdx)
	case TabSkills:
		return len(m.skills.Rows)
	case TabLanguage:
		return len(config.UILanguages)
	case TabDoctor:
		return len(m.docIdx)
	}
	return 0
}

func (m *Model) syncList(tab int) { m.lst[tab].SetN(m.rowCount(tab)) }

// rebuildComponents lọc danh sách thành phần theo loại, trạng thái và chữ, giữ con trỏ theo ID.
//
// Khi đang chờ dữ liệu mới cho một yêu cầu Focus (focusID != ""), con trỏ nhắm thành phần theo focus: không tìm thấy thì báo toast và bỏ
// yêu cầu, trừ khi còn một lần nạp nữa đang xếp hàng (laterLoad: dữ liệu vừa về có thể cũ hơn yêu cầu).
func (m *Model) rebuildComponents(laterLoad bool) {
	keepKind, keepID := hcomp.Kind(""), ""
	if d := m.CursorComponent(); d != nil {
		keepKind, keepID = d.Component.Kind, d.Component.ID
	}
	want := m.focusID
	if want != "" {
		keepKind, keepID = m.focusKind, want
	}
	f := strings.ToLower(strings.TrimSpace(m.filter))
	m.compIdx = m.compIdx[:0]
	for i, d := range m.comps.Details {
		c := d.Component
		if (m.kindF != "" && string(c.Kind) != m.kindF) || (m.statusF != "" && c.Status != m.statusF) {
			continue
		}
		if f != "" && !strings.Contains(strings.ToLower(string(c.Kind)+" "+c.ID+" "+c.Status+" "+d.Row.Detail+" "+c.Scope), f) {
			continue
		}
		m.compIdx = append(m.compIdx, i)
	}
	m.syncList(TabComponents)
	found := false
	for pos, i := range m.compIdx {
		c := m.comps.Details[i].Component
		if keepID != "" && c.ID == keepID && (keepKind == "" || c.Kind == keepKind) {
			m.lst[TabComponents].MoveTo(pos)
			found = true
			break
		}
	}
	if want == "" || (!found && laterLoad) {
		return
	}
	m.focusKind, m.focusID = "", ""
	if !found {
		m.setToast(m.t("sy.comp.notfound", i18n.A{"id": want}), true)
	}
}

// CursorComponent trả thành phần dưới con trỏ ở thẻ Thành phần (nil nếu trống).
func (m *Model) CursorComponent() *app.ComponentDetail {
	if m.focusID != "" { // đang chờ dữ liệu mới cho yêu cầu Focus: danh sách hiện tại có thể cũ, không để hành động nhắm nhầm
		return nil
	}
	c := m.lst[TabComponents].Cursor
	if c < 0 || c >= len(m.compIdx) {
		return nil
	}
	return &m.comps.Details[m.compIdx[c]]
}

func (m *Model) rebuildDoctor() {
	m.docIdx = m.docIdx[:0]
	if m.doctor != nil {
		for i, c := range m.doctor.Checks {
			if m.onlyIssues && c.Level != app.LevelWarn && c.Level != app.LevelFail {
				continue
			}
			m.docIdx = append(m.docIdx, i)
		}
	}
	m.syncList(TabDoctor)
}

// Focus mở thẻ Thành phần tại thành phần (kind, id), bỏ bộ lọc (từ hộp thư).
func (m *Model) Focus(kind, id string) tea.Cmd {
	m.kindF, m.statusF, m.filter, m.out, m.pane = "", "", "", nil, paneList
	m.tab = TabComponents
	m.focusKind, m.focusID = hcomp.Kind(kind), id
	m.det.Reset()
	if sl := m.slots[TabComponents]; sl.Loaded || sl.Loading { // danh sách đã nạp (hoặc đang nạp, có thể đã cũ) có thể cũ (thành phần mới sinh sau đó): nạp lại, đặt con trỏ khi dữ liệu mới về
		return m.load(TabComponents)
	}
	return m.ensureTab(TabComponents)
}

// OpenTab mở thẻ con theo tên (vd. "doctor" từ dòng doctor của hộp thư).
func (m *Model) OpenTab(name string) tea.Cmd {
	i := TabIndexOf(name)
	if i < 0 {
		return nil
	}
	return m.setTab(i)
}

func (m *Model) setTab(i int) tea.Cmd {
	m.tab, m.pane, m.out = i, paneList, nil
	m.det.Reset()
	return m.ensureTab(i)
}

// ---- trạng thái màn hình (ui.Screen) ----

// Capturing: màn hình đang nhận mọi phím chữ (ô lọc, form, hộp thoại).
func (m *Model) Capturing() bool { return m.dlg != nil || m.frm != nil || m.textIn }

// NotList: phím của từng thẻ nằm ở viền dưới của khung (chân phím chung chỉ giữ phím ghim và toàn cục).
func (m *Model) NotList() bool { return true }

// WantsInterrupt: đang cập nhật Bint (Esc huỷ việc nền).
func (m *Model) WantsInterrupt() bool { return m.upd.running }

// Busy: có việc nạp dữ liệu hoặc yêu cầu đang chạy.
func (m *Model) Busy() bool {
	if m.pending > 0 || m.rtSl.Loading {
		return true
	}
	for _, s := range m.slots {
		if s.Loading {
			return true
		}
	}
	return false
}

// ---- thông điệp ----

// Update xử lý thông điệp không phải phím; trả lệnh kế tiếp.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case LoadedMsg:
		return m.onLoaded(msg)
	case RuntimeMsg:
		m.rtSl.Loading = false
		m.rtSl.Err = msg.Err
		if msg.Err == nil {
			m.rt, m.rtSl.Loaded = msg.Status, true
		}
		if m.againRT {
			m.againRT = false
			return m.loadRuntime()
		}
	case actions.DeniedMsg:
		m.pending = max(m.pending-1, 0)
		m.upd.running = false
		why := m.t(map[bool]string{true: "ui.locked.agent", false: "sy.denied.readonly"}[msg.Locked])
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
	if m.again[msg.Tab] {
		m.again[msg.Tab] = false
		next = m.load(msg.Tab)
	}
	if msg.Err != nil {
		if msg.Tab == TabComponents && next == nil {
			m.focusKind, m.focusID = "", "" // nạp lỗi: bỏ yêu cầu Focus đang chờ
		}
		if msg.Tab == TabSetup && m.setupPending { // nạp lỗi: không mở form, báo lỗi
			return tea.Batch(next, m.openSetup(false))
		}
		return next
	}
	s.Loaded = true
	switch msg.Tab {
	case TabConfig:
		m.cfg = msg.Value.(app.ConfigViewData)
		m.syncList(TabConfig)
		m.syncList(TabLanguage)
		m.lst[TabLanguage].MoveTo(languageIndex(m.cfg.Effective.UI.Language))
	case TabHarness:
		m.har = msg.Value.(HarnessData)
		m.syncList(TabHarness)
	case TabComponents:
		m.comps = msg.Value.(ComponentsData)
		m.rebuildComponents(next != nil)
	case TabSkills:
		m.skills = msg.Value.(app.SkillList)
		m.syncList(TabSkills)
	case TabDoctor:
		m.doctor = msg.Value.(*app.DoctorReport)
		m.rebuildDoctor()
	case TabSetup:
		m.setup = msg.Value.(app.InitState)
		if m.setupPending {
			if m.frm == nil && m.dlg == nil {
				return tea.Batch(next, m.openSetup(false))
			}
			// Đang có hộp hoặc form khác: bỏ yêu cầu mở form đang chờ (kẻo form bật lên ở lần nạp lại sau, ví dụ Ctrl+R), báo bằng toast.
			m.setupPending = false
			m.setToast(m.t("sy.setup.dropped"), false)
		}
	case TabUpdate:
		m.updInfo = msg.Value.(UpdateInfo)
	}
	return next
}

func languageIndex(lang string) int {
	for i, l := range config.UILanguages {
		if l == lang {
			return i
		}
	}
	return 0
}

// finishForm đóng form đang chờ kết quả.
func (m *Model) finishForm(id string) bool {
	if m.frm != nil && m.frmID == id {
		m.frm, m.frmBusy = nil, false
		return true
	}
	return false
}

func (m *Model) fail(err error) { m.setToast(ui.ErrText(m.rw, err), true) }

func (m *Model) onResult(msg actions.ResultMsg) tea.Cmd {
	switch msg.ID {
	case "system.config_edit":
		return m.onConfigEdited(msg)
	case "system.open_file":
		o, _ := msg.Opts.(actions.OpenFileOptions)
		if m.edit != nil && o.Path == m.edit.TempPath { // sửa lại bản tạm sau khi bị báo lỗi
			return m.afterEditor(msg.Err)
		}
		if msg.Err != nil {
			m.fail(msg.Err)
		}
	case "system.config_check":
		return m.onConfigChecked(msg)
	case "system.config_rebase":
		return m.onConfigRebased(msg)
	case "system.config_save":
		m.dlg = nil
		if msg.Err != nil {
			return m.onConfigSaveRejected(msg)
		}
		m.edit = nil
		m.setToast(m.t("sy.config.saved"), false)
		return m.reload(TabConfig)
	case "system.language_save":
		m.finishForm(msg.ID)
		if msg.Err != nil {
			m.fail(msg.Err)
			return nil
		}
		r, _ := msg.Value.(app.UILanguageResult)
		o, _ := msg.Opts.(app.UILanguageOptions)
		key := "sy.lang.saved"
		if !r.Changed {
			key = "sy.lang.unchanged"
		}
		m.setToast(m.t(key, i18n.A{"lang": o.Language, "path": r.Path}), false)
		lm := actions.LanguageMsg{Lang: r.Effective, Chosen: o.Language, Layer: string(r.ShadowedBy)}
		if lm.Lang == "" { // dịch vụ không trả giá trị hiệu lực (lỗi ghép lớp): dùng giá trị vừa chọn
			lm.Lang = o.Language
		}
		return tea.Batch(m.reload(TabConfig), func() tea.Msg { return lm })
	case "system.harness_version":
		v, _ := msg.Value.(actions.VersionResult)
		delete(m.verBusy, v.Name)
		if msg.Err == nil {
			m.versions[v.Name] = v.Version
		}
	case "system.component_add":
		m.finishForm(msg.ID)
		if msg.Err != nil {
			m.fail(msg.Err)
			return nil
		}
		c, _ := msg.Value.(hcomp.Component)
		m.setToast(m.t("sy.comp.added", i18n.A{"id": c.ID}), false)
		m.focusKind, m.focusID = c.Kind, c.ID
		return m.reload(TabComponents)
	case "system.component_approve", "system.component_deprecate":
		if msg.Err != nil {
			m.fail(msg.Err)
			return nil
		}
		c, _ := msg.Value.(hcomp.Component)
		m.setToast(m.t("sy.comp.reviewed", i18n.A{"id": c.ID, "status": c.Status}), false)
		m.focusKind, m.focusID = c.Kind, c.ID
		return m.reload(TabComponents)
	case "system.render":
		m.finishForm(msg.ID)
		if msg.Err != nil {
			m.fail(msg.Err)
			return nil
		}
		s, _ := msg.Value.(string)
		m.showOut(m.t("sy.render.title"), strings.Split(strings.TrimRight(s, "\n"), "\n"))
	case "system.skill_show":
		if msg.Err != nil {
			m.fail(msg.Err)
			return nil
		}
		v, _ := msg.Value.(app.SkillContent)
		m.showOut(m.t("sy.skill.content.title", i18n.A{"name": v.Name}), strings.Split(strings.TrimRight(string(v.Content), "\n"), "\n"))
	case "system.skill_overlay":
		if msg.Err != nil {
			m.fail(msg.Err)
			return nil
		}
		v, _ := msg.Value.(app.OverlayResult)
		key := "sy.skill.overlay.created"
		if v.Existed {
			key = "sy.skill.overlay.exists"
		}
		m.setToast(m.t(key, i18n.A{"path": v.Path}), false)
		if v.Abs == "" {
			return m.reload(TabSkills)
		}
		return tea.Batch(m.request("system.open_file", actions.OpenFileOptions{Path: v.Abs}), m.reload(TabSkills))
	case "system.skill_diff":
		m.finishForm(msg.ID)
		if msg.Err != nil {
			m.fail(msg.Err)
			return nil
		}
		v, _ := msg.Value.(app.SkillDiffResult)
		switch {
		case v.OnlyVersion:
			m.showOut(m.t("sy.skill.diff.title", i18n.A{"name": v.Name}), []string{m.t("sy.skill.diff.only", i18n.A{"version": v.Current})})
		case v.Same:
			m.showOut(m.t("sy.skill.diff.title", i18n.A{"name": v.Name}), []string{m.t("sy.skill.diff.same", i18n.A{"from": v.From, "to": v.To})})
		default:
			m.showOut(m.t("sy.skill.diff.title", i18n.A{"name": v.Name}), coloredDiff(m.th, v.Diff))
		}
	case "system.skill_versions":
		if msg.Err != nil {
			m.fail(msg.Err)
			return nil
		}
		m.showOut(m.t("sy.skill.versions.title"), m.versionLines(msg.Value))
	case "system.skill_check":
		m.finishForm(msg.ID)
		if msg.Err != nil {
			m.fail(msg.Err)
			return nil
		}
		m.showOut(m.t("sy.skill.check.title"), m.checkLines(msg.Value))
	case "system.skill_regress":
		m.finishForm(msg.ID)
		if msg.Err != nil {
			m.fail(msg.Err)
			return nil
		}
		m.showOut(m.t("sy.skill.regress.title"), m.regressLines(msg.Value))
	case "system.repo_plan":
		return m.onRepoPlan(msg)
	case "system.repo_apply":
		return m.onRepoApply(msg)
	case "system.config_upgrade_plan":
		return m.onUpgradePlan(msg)
	case "system.config_upgrade":
		return m.onUpgradeDone(msg)
	case "system.update":
		return m.onUpdateDone(msg)
	case "system.sync_plan":
		return m.onSyncPlan(msg)
	case "system.sync":
		if msg.Err != nil {
			m.fail(msg.Err)
			return nil
		}
		si, _ := msg.Value.(app.SkillInstall)
		m.showOut(m.t("sy.sync.result.title"), m.syncResultLines(si))
		m.setToast(m.t("sy.sync.done", i18n.A{"conflicts": si.Conflicts}), si.Conflicts > 0)
		return m.reload(TabSkills)
	case "system.index_rebuild":
		if msg.Err != nil {
			m.fail(msg.Err)
			return nil
		}
		r, _ := msg.Value.(app.IndexRebuildResult)
		m.idxRes = &r
		m.setToast(m.t("sy.index.done", i18n.A{"total": r.Total, "bad": len(r.Errors)}), len(r.Errors) > 0)
	}
	return nil
}

func (m *Model) showOut(title string, lines []string) {
	m.out, m.pane = &outPane{Title: title, Lines: lines}, paneDetail
	m.det.Reset()
}

// ---- sửa cấu hình ----

// onConfigEdited xử lý kết thúc của ExecMsg $EDITOR (Opts là ConfigEditState) hoặc lỗi chuẩn bị bản tạm (ConfigEditOptions).
func (m *Model) onConfigEdited(msg actions.ResultMsg) tea.Cmd {
	m.finishForm(msg.ID)
	st, ok := msg.Opts.(actions.ConfigEditState)
	if !ok { // lỗi ngay từ bước chuẩn bị (vd. worktree phụ)
		if msg.Err != nil {
			m.fail(msg.Err)
		}
		return nil
	}
	e := st.Edit
	m.edit = &e
	return m.afterEditor(msg.Err)
}

// afterEditor đọc bản tạm sau khi trình soạn thảo thoát và yêu cầu kiểm tra.
func (m *Model) afterEditor(err error) tea.Cmd {
	e := m.edit
	if e == nil {
		return nil
	}
	if err != nil {
		os.Remove(e.TempPath)
		m.edit = nil
		m.fail(err)
		return nil
	}
	data, rerr := os.ReadFile(e.TempPath)
	if rerr != nil {
		m.edit = nil
		m.fail(rerr)
		return nil
	}
	return m.request("system.config_check", actions.ConfigApplyArgs{Edit: *e, Data: data})
}

// onConfigSaveRejected: ghi bị từ chối (tệp gốc đã đổi hoặc Validate lỗi lúc ghi). Bản tạm còn nguyên; hộp cho
// sửa lại (sau ErrConfigChanged thì gộp ba chiều lên tệp gốc mới rồi mở lại) hoặc bỏ (xoá bản tạm).
func (m *Model) onConfigSaveRejected(msg actions.ResultMsg) tea.Cmd {
	a, _ := msg.Opts.(actions.ConfigApplyArgs)
	return m.showConfigRejected(a.Edit, msg.Err, errors.Is(msg.Err, app.ErrConfigChanged))
}

// showConfigRejected mở hộp "Sửa lại / Bỏ" cho một lần ghi, kiểm hoặc gộp bị lỗi; bản tạm luôn còn nguyên tới khi Bỏ.
// rebase: Sửa lại phải gộp lên tệp gốc mới trước khi mở trình soạn thảo (cũng là cách thử lại khi chính lần gộp lỗi).
func (m *Model) showConfigRejected(e app.ConfigEdit, err error, rebase bool) tea.Cmd {
	m.edit = &e
	body := append([]string{m.th.S(theme.Err, m.t("sy.config.rejected"))}, wrapText(ui.ErrText(m.rw, err), 72)...)
	body = append(body, "", m.th.S(theme.Muted, m.t("sy.config.rejected.kept", i18n.A{"path": e.TempPath})))
	m.dlg = &dialog{c: confirm.Model{Title: m.t("sy.config.rejected.title"), Body: body, Danger: true,
		YesLabel: m.t("sy.config.reedit"), NoLabel: m.t("sy.config.discard")},
		onYes: func(string) tea.Cmd {
			if rebase {
				return m.request("system.config_rebase", actions.ConfigApplyArgs{Edit: e})
			}
			return m.request("system.open_file", actions.OpenFileOptions{Path: e.TempPath})
		},
		onNo: func() tea.Cmd {
			os.Remove(e.TempPath)
			m.edit = nil
			m.setToast(m.t("sy.config.discarded"), false)
			return nil
		}}
	return nil
}

// onConfigRebased nhận kết quả gộp: thành công thì mở lại bản tạm (đã gộp, hoặc mang dấu xung đột) trong trình soạn thảo;
// lỗi thì bản tạm còn nguyên và hộp Sửa lại/Bỏ mở lại kèm lỗi.
func (m *Model) onConfigRebased(msg actions.ResultMsg) tea.Cmd {
	a, _ := msg.Opts.(actions.ConfigApplyArgs)
	if msg.Err != nil {
		return m.showConfigRejected(a.Edit, msg.Err, true)
	}
	rb, ok := msg.Value.(app.ConfigRebase)
	if !ok { // không có kết quả gộp thì không chuỗi hoá yêu cầu mở tệp (xem TestReadOnlyAndAgentNeverReachDo)
		return nil
	}
	e := a.Edit
	m.edit = &e
	switch {
	case rb.Conflicts > 0:
		m.setToast(m.t("sy.config.rebase.conflict", i18n.A{"count": rb.Conflicts}), true)
	case rb.Merged:
		m.setToast(m.t("sy.config.rebase.merged"), false)
	}
	return m.request("system.open_file", actions.OpenFileOptions{Path: e.TempPath})
}

func (m *Model) onConfigChecked(msg actions.ResultMsg) tea.Cmd {
	a, _ := msg.Opts.(actions.ConfigApplyArgs)
	drop := func() {
		os.Remove(a.Edit.TempPath)
		m.edit = nil
	}
	if msg.Err != nil { // kiểm lỗi (không phải nội dung sai): giữ bản tạm, cho sửa lại hoặc bỏ như khi ghi bị từ chối
		return m.showConfigRejected(a.Edit, msg.Err, false)
	}
	c, _ := msg.Value.(app.ConfigCheck)
	switch {
	case !c.Changed:
		drop()
		m.setToast(m.t("sy.config.nochange"), false)
	case !c.Valid: // lỗi hiện trong màn; tệp gốc chưa bị đụng tới. Cho sửa lại hoặc bỏ.
		body := append([]string{m.th.S(theme.Err, m.t("sy.config.invalid"))}, wrapText(c.Err, 72)...)
		m.dlg = &dialog{c: confirm.Model{Title: m.t("sy.config.invalid.title"), Body: body, Danger: true,
			YesLabel: m.t("sy.config.reedit"), NoLabel: m.t("sy.config.discard")},
			onYes: func(string) tea.Cmd {
				return m.request("system.open_file", actions.OpenFileOptions{Path: a.Edit.TempPath})
			},
			onNo: func() tea.Cmd { drop(); m.setToast(m.t("sy.config.discarded"), false); return nil }}
	default:
		body := append([]string{m.th.S(theme.Muted, a.Edit.Path)}, coloredDiff(m.th, c.Diff)...)
		args := a
		m.dlg = &dialog{c: confirm.Model{Title: m.t("sy.config.confirm.title"), Body: body,
			YesLabel: m.t("sy.config.write"), NoLabel: m.t("sy.config.discard")},
			onYes: func(string) tea.Cmd { return m.request("system.config_save", args) },
			onNo:  func() tea.Cmd { drop(); m.setToast(m.t("sy.config.discarded"), false); return nil }}
	}
	return nil
}

// ---- sync ----

func (m *Model) onSyncPlan(msg actions.ResultMsg) tea.Cmd {
	if msg.Err != nil {
		m.fail(msg.Err)
		return nil
	}
	plan, _ := msg.Value.(app.SyncPlan)
	o, _ := msg.Opts.(app.SyncOptions)
	switch {
	case plan.NoTargets:
		m.setToast(m.t("sy.sync.notargets"), true)
		return nil
	case plan.Changes == 0 && plan.Conflicts == 0:
		m.setToast(m.t("sy.sync.uptodate"), false)
		return nil
	}
	body := m.syncPlanLines(plan)
	title, danger := m.t("sy.sync.preview.title"), false
	if o.Force {
		danger = true
		title = m.t("sy.sync.force.title")
		body = append([]string{m.th.S(theme.Err, m.t("sy.sync.force.warn")), ""}, body...)
	}
	m.dlg = &dialog{c: confirm.Model{Title: title, Body: body, Danger: danger, YesLabel: m.t("sy.sync.write"), NoLabel: m.t("sy.cancel")},
		onYes: func(string) tea.Cmd { return m.request("system.sync", o) }}
	return nil
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
	if k := msg.String(); m.upd.running && (k == "esc" || k == "ctrl+c") {
		return func() tea.Msg { return actions.CancelMsg{ID: "system.update"} }, true
	}
	switch msg.String() {
	case ".":
		return m.setTab((m.tab + 1) % numTabs), true
	case ",":
		return m.setTab((m.tab + numTabs - 1) % numTabs), true
	}
	if cmd, ok := m.tabKey(msg.String()); ok {
		return cmd, true
	}
	return m.listKey(msg.String())
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
			m.out = nil
			m.pane = paneList
			m.det.Reset()
		case m.pane == paneDetail:
			m.pane = paneList
		case m.tab == TabComponents && m.filter != "":
			m.filter = ""
			m.rebuildComponents(true)
		}
		return nil, true
	case "ctrl+r", "f5":
		m.out = nil
		return m.reload(m.tab), true
	}
	return nil, false
}

func (m *Model) nav(d int) {
	if m.pane == paneDetail {
		m.det.Scroll(d)
		return
	}
	m.lst[m.tab].Move(d)
	m.det.Reset()
	if m.out != nil && m.tab != TabComponents && m.tab != TabSkills {
		m.out = nil
	}
}

func (m *Model) page(d int) {
	if m.pane == paneDetail {
		m.det.Page(d)
		return
	}
	m.lst[m.tab].Page(d)
	m.det.Reset()
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
	m.rebuildComponents(true)
	return nil
}

// tabKey xử lý phím riêng của từng thẻ.
func (m *Model) tabKey(s string) (tea.Cmd, bool) {
	switch m.tab {
	case TabConfig:
		if s == "U" {
			return m.cmdConfigUpgrade(), true
		}
		if s == "e" {
			return m.cmdConfigEdit(), true
		}
		if s == "enter" {
			m.pane = paneDetail
			return nil, true
		}
	case TabHarness:
		switch s {
		case "V":
			return m.cmdVersion(), true
		case "enter":
			m.pane = paneDetail
			return nil, true
		}
	case TabComponents:
		return m.componentKey(s)
	case TabSkills:
		return m.skillKey(s)
	case TabIndex:
		if s == "b" || s == "enter" {
			return m.cmdIndexRebuild(), true
		}
	case TabLanguage:
		if s == "enter" || s == "s" {
			return m.cmdLanguage(), true
		}
	case TabSetup:
		switch s {
		case "e", "enter":
			return m.cmdSetupEdit(), true
		case "U":
			return m.cmdConfigUpgrade(), true
		}
	case TabUpdate:
		switch s {
		case "u", "enter":
			return m.cmdUpdate(), true
		case "R":
			return m.cmdRestart(), true
		}
	case TabDoctor:
		switch s {
		case "w":
			m.onlyIssues = !m.onlyIssues
			m.rebuildDoctor()
			m.det.Reset()
			return nil, true
		case "enter":
			m.pane = paneDetail
			return nil, true
		}
	}
	return nil, false
}

func (m *Model) componentKey(s string) (tea.Cmd, bool) {
	switch s {
	case "enter":
		m.out, m.pane = nil, paneDetail
		return nil, true
	case "/":
		m.textIn, m.textOld = true, m.filter
		return nil, true
	case "f":
		m.openForm("system.component_list", "harness component list", app.ComponentListOptions{Kind: hcomp.Kind(m.kindF), Status: m.statusF})
		return nil, true
	case "n":
		return m.cmdComponentAdd(), true
	case "a":
		return m.cmdReview(true), true
	case "d":
		return m.cmdReview(false), true
	case "R":
		return m.cmdRender(), true
	}
	return nil, false
}

func (m *Model) skillKey(s string) (tea.Cmd, bool) {
	switch s {
	case "enter":
		return m.cmdSkillShow(), true
	case "C":
		m.coreView = !m.coreView
		return m.cmdSkillShow(), true
	case "o":
		return m.cmdSkillOverlay(), true
	case "d":
		return m.cmdSkillForm("system.skill_diff", "skill diff"), true
	case "v":
		return m.cmdSkillVersions(), true
	case "c":
		return m.cmdSkillForm("system.skill_check", "skill check"), true
	case "g":
		m.openForm("system.skill_regress", "skill regress", evolvesvcRegressDefaults())
		return nil, true
	case "s":
		return m.cmdSyncPlan(app.SyncDefaults(config.Default())), true
	case "F":
		return m.cmdSyncPlan(app.SyncOptions{Force: true}), true
	}
	return nil, false
}

// ---- hộp thoại và form ----

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
		if d.onNo != nil {
			return d.onNo()
		}
	}
	return nil
}

func (m *Model) openForm(id, cli string, defaults any) {
	f := form.New(id, cli, defaults)
	f.SetSuggestions("harnesses", []string{"claude", "codex", "antigravity"})
	f.SetSuggestions("component_templates", m.comps.Templates)
	f.SetSuggestions("components", m.componentIDs())
	f.SetSuggestions("skills", m.skillNames(""))
	f.SetSuggestions("core_skills", m.skillNames("core"))
	f.SetSuggestions("evolved_skills", m.skillNames("evolved"))
	m.frm, m.frmID, m.frmBusy = &f, id, false
}

func (m *Model) componentIDs() []string {
	var out []string
	for _, d := range m.comps.Details {
		out = append(out, d.Component.ID)
	}
	return out
}

func (m *Model) skillNames(kind string) []string {
	var out []string
	for _, r := range m.skills.Rows {
		if kind == "" || r.Kind == kind {
			out = append(out, r.Name)
		}
	}
	return out
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

func (m *Model) submitForm(v any) tea.Cmd {
	if o, ok := v.(app.ComponentListOptions); ok { // bộ lọc là trạng thái của màn hình, không có hành động ghi/đọc riêng
		m.frm = nil
		m.kindF, m.statusF = string(o.Kind), o.Status
		m.rebuildComponents(true)
		return nil
	}
	switch o := v.(type) {
	case app.InitFlagOptions:
		if m.frmID == "system.repo_apply" {
			return m.submitSetup(o)
		}
	case app.UpdateOptions:
		return m.submitUpdate(o)
	}
	m.frmBusy = true
	return m.request(m.frmID, v)
}

// Command thực hiện hành động theo ID (từ bảng lệnh).
func (m *Model) Command(id string) tea.Cmd {
	switch id {
	case "system.next_view":
		return m.setTab((m.tab + 1) % numTabs)
	case "system.prev_view":
		return m.setTab((m.tab + numTabs - 1) % numTabs)
	case "system.refresh":
		m.out = nil
		return m.reload(m.tab)
	case "system.filter":
		if m.tab == TabComponents {
			m.textIn, m.textOld = true, m.filter
		}
	case "system.config_edit", "system.config_check", "system.config_rebase", "system.config_save", "system.config_view":
		m.tab = TabConfig
		if id == "system.config_edit" {
			return tea.Batch(m.ensureTab(TabConfig), m.cmdConfigEdit())
		}
		return m.ensureTab(TabConfig)
	case "system.language_save":
		m.tab = TabLanguage
		return tea.Batch(m.ensureTab(TabLanguage), m.cmdLanguage())
	case "system.harness_list", "system.harness_show", "system.harness_runtime":
		return m.setTab(TabHarness)
	case "system.harness_version":
		m.tab = TabHarness
		return tea.Batch(m.ensureTab(TabHarness), m.cmdVersion())
	case "system.component_list", "system.component_show":
		return m.setTab(TabComponents)
	case "system.component_add":
		m.tab = TabComponents
		return tea.Batch(m.ensureTab(TabComponents), m.cmdComponentAdd())
	case "system.component_approve":
		m.tab = TabComponents
		return tea.Batch(m.ensureTab(TabComponents), m.cmdReview(true))
	case "system.component_deprecate":
		m.tab = TabComponents
		return tea.Batch(m.ensureTab(TabComponents), m.cmdReview(false))
	case "system.render":
		m.tab = TabComponents
		return tea.Batch(m.ensureTab(TabComponents), m.cmdRender())
	case "system.skill_list", "system.skill_show":
		return m.setTab(TabSkills)
	case "system.skill_overlay":
		m.tab = TabSkills
		return tea.Batch(m.ensureTab(TabSkills), m.cmdSkillOverlay())
	case "system.skill_diff":
		m.tab = TabSkills
		return tea.Batch(m.ensureTab(TabSkills), m.cmdSkillForm("system.skill_diff", "skill diff"))
	case "system.skill_versions":
		m.tab = TabSkills
		return tea.Batch(m.ensureTab(TabSkills), m.cmdSkillVersions())
	case "system.skill_check":
		m.tab = TabSkills
		return tea.Batch(m.ensureTab(TabSkills), m.cmdSkillForm("system.skill_check", "skill check"))
	case "system.skill_regress":
		m.tab = TabSkills
		m.openForm("system.skill_regress", "skill regress", evolvesvcRegressDefaults())
		return m.ensureTab(TabSkills)
	case "system.sync_plan", "system.sync":
		m.tab = TabSkills
		return tea.Batch(m.ensureTab(TabSkills), m.cmdSyncPlan(app.SyncDefaults(config.Default())))
	case "system.index_rebuild":
		m.tab = TabIndex
		return m.cmdIndexRebuild()
	case "system.doctor":
		return m.setTab(TabDoctor)
	case "system.repo_state":
		return m.setTab(TabSetup)
	case "system.repo_plan", "system.repo_apply":
		m.tab = TabSetup
		return tea.Batch(m.ensureTab(TabSetup), m.openSetup(true))
	case "system.config_upgrade_plan", "system.config_upgrade":
		return m.OpenConfigUpgrade()
	case "system.update":
		m.tab = TabUpdate
		return tea.Batch(m.ensureTab(TabUpdate), m.cmdUpdate())
	case "system.doctor_filter":
		m.tab = TabDoctor
		m.onlyIssues = !m.onlyIssues
		m.rebuildDoctor()
		return m.ensureTab(TabDoctor)
	}
	return nil
}

// cli viết lại đoạn nhắc lệnh CLI trong thông điệp của tầng dịch vụ thành phím/hành động TUI (ui.Rewrite).
func (m *Model) cli(s string) string { return m.rw.Rewrite(s) }
