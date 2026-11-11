package runs

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"bint/internal/app"
	"bint/internal/app/evolvesvc"
	"bint/internal/config"
	"bint/internal/evolve"
	"bint/internal/i18n"
	"bint/internal/tui/actions"
	"bint/internal/tui/components/confirm"
	"bint/internal/tui/components/form"
	"bint/internal/tui/components/input"
	"bint/internal/tui/components/list"
	"bint/internal/tui/components/viewer"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

// WideW: từ chiều rộng này danh sách và chi tiết hiện cạnh nhau; hẹp hơn thì một khung (Tab/Enter đổi).
const WideW = 100

// Các thẻ của màn hình.
const (
	tabRuns = iota
	tabLessons
	numTabs
)

var tabNames = [numTabs]string{"runs", "lessons"}

type pane int

const (
	paneList pane = iota
	paneDetail
)

// Options điều khiển màn hình.
type Options struct {
	Dir      string
	ReadOnly bool
	// Loader/Detailer/Lessons thay được trong kiểm thử; nil = nạp thật.
	Loader   Loader
	Detailer Detailer
	Lessons  LessonLoader
	// Gate cho biết hành động có dùng được không theo sổ hành động và chế độ (nil = luôn dùng được).
	Gate func(id string) actions.Availability
}

type dialog struct {
	c     confirm.Model
	onYes func(value string) tea.Cmd
}

// Model là trạng thái màn Lượt chạy.
type Model struct {
	o   Options
	cat *i18n.Catalog
	th  theme.Theme

	Data        Data
	Loaded      bool
	Loading     bool
	reloadAgain bool
	Err         error

	limit   int
	filter  string
	textIn  bool
	textOld string
	idx     []int // chỉ số các lượt đang hiện (sau lọc) trong Data.Rows
	lst     list.Model
	pane    pane
	tab     int

	detail    viewer.Model
	detailKey string
	cache     map[string]*app.RunDetail
	cacheErr  map[string]error
	asked     map[string]bool

	// Thẻ Bài học.
	lessons       []evolve.LessonRow
	lessonsLoaded bool
	lessonsLoad   bool
	lessonsErr    error
	llst          list.Model
	digest        *evolvesvc.DigestResult
	digestErr     error
	digestRef     string
	digestBusy    bool

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
	if o.Lessons == nil {
		o.Lessons = LoadLessons
	}
	return &Model{o: o, limit: app.RunsListDefaults(config.Default()).Limit, cache: map[string]*app.RunDetail{},
		cacheErr: map[string]error{}, asked: map[string]bool{}}
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
		m.setToast(m.t("rn.denied.readonly"), true)
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
	ctx, dir, loader, ro, limit := context.Background(), m.o.Dir, m.o.Loader, m.o.ReadOnly, m.limit
	return func() tea.Msg {
		d, err := loader(ctx, dir, limit, ro)
		return LoadedMsg{Data: d, Err: err}
	}
}

func (m *Model) loadLessons() tea.Cmd {
	if m.lessonsLoad {
		return nil
	}
	m.lessonsLoad = true
	ctx, dir, ll, ro := context.Background(), m.o.Dir, m.o.Lessons, m.o.ReadOnly
	return func() tea.Msg {
		rows, err := ll(ctx, dir, ro)
		return LessonsMsg{Rows: rows, Err: err}
	}
}

// rebuild dựng lại danh sách hàng theo bộ lọc, giữ con trỏ theo ID.
func (m *Model) rebuild() {
	keep := ""
	if r := m.Cursor(); r != nil {
		keep = r.Run.RunID
	}
	f := strings.ToLower(strings.TrimSpace(m.filter))
	m.idx = m.idx[:0]
	for i, r := range m.Data.Rows {
		if f != "" && !strings.Contains(strings.ToLower(r.Run.RunID+" "+r.Run.Status+" "+r.Run.Harness.String+" "+r.Run.GitBranch.String+" "+r.Label+" "+r.Run.Model.String), f) {
			continue
		}
		m.idx = append(m.idx, i)
	}
	m.lst.SetN(len(m.idx))
	for pos, i := range m.idx {
		if keep != "" && m.Data.Rows[i].Run.RunID == keep {
			m.lst.MoveTo(pos)
			break
		}
	}
}

// Cursor trả lượt dưới con trỏ (nil nếu danh sách trống).
func (m *Model) Cursor() *app.RunRow {
	if m.lst.Cursor < 0 || m.lst.Cursor >= len(m.idx) {
		return nil
	}
	return &m.Data.Rows[m.idx[m.lst.Cursor]]
}

// Visible trả các lượt đang hiện.
func (m *Model) Visible() []app.RunRow {
	out := make([]app.RunRow, 0, len(m.idx))
	for _, i := range m.idx {
		out = append(out, m.Data.Rows[i])
	}
	return out
}

// Limit là số lượt tối đa đang nạp (`--limit`).
func (m *Model) Limit() int { return m.limit }

// Tab là thẻ đang hiện: 0 = Danh sách, 1 = Bài học.
func (m *Model) Tab() int { return m.tab }

// Focus chọn lượt id (bỏ lọc, về thẻ Danh sách).
func (m *Model) Focus(id string) tea.Cmd {
	m.filter, m.tab, m.pane = "", tabRuns, paneList
	for pos, i := range m.idx {
		if m.Data.Rows[i].Run.RunID == id {
			m.lst.MoveTo(pos)
		}
	}
	return tea.Batch(m.Ensure(), m.wantDetail())
}

// ---- trạng thái màn hình (ui.Screen) ----

// Capturing: màn hình đang nhận mọi phím chữ (ô lọc, form, hộp thoại).
func (m *Model) Capturing() bool { return m.dlg != nil || m.frm != nil || m.textIn }

// NotList: màn Lượt chạy luôn giữ khung danh sách; phím của thẻ nằm ở viền dưới từng khung.
func (m *Model) NotList() bool { return false }

// Busy: có việc nền của màn hình đang chạy (nạp, hoặc form đang chờ kết quả).
func (m *Model) Busy() bool {
	return m.Loading || m.frmBusy || m.digestBusy || m.lessonsLoad && !m.lessonsLoaded
}

// WantsInterrupt: màn Lượt chạy không có việc nền dài để huỷ.
func (m *Model) WantsInterrupt() bool { return false }

func (m *Model) wantDetail() tea.Cmd {
	r := m.Cursor()
	if r == nil || m.tab != tabRuns {
		return nil
	}
	id := r.Run.RunID
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
			m.invalidate()
			cmds = append(cmds, m.wantDetail())
			if m.frm != nil {
				m.frm.SetSuggestions("runs", m.Data.IDs())
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
	case LessonsMsg:
		m.lessonsLoad, m.lessonsLoaded = false, msg.Err == nil
		m.lessonsErr = msg.Err
		if msg.Err == nil {
			m.lessons = msg.Rows
			rows := m.lessonRows()
			m.llst.SetN(len(rows))
			if len(rows) > 1 && rows[m.llst.Cursor].row == nil {
				m.moveLesson(1) // con trỏ không đứng ở tiêu đề nhóm
			}
		}
	case RefreshMsg:
		return m.Load()
	case actions.DeniedMsg:
		why := m.t(map[bool]string{true: "ui.locked.agent", false: "rn.denied.readonly"}[msg.Locked])
		m.setToast(why, true)
		if m.frmBusy && m.frm != nil {
			m.frmBusy = false
			m.frm.Err = why
		}
		m.digestBusy = false
	case actions.ResultMsg:
		return m.onResult(msg)
	}
	return nil
}

func (m *Model) onResult(msg actions.ResultMsg) tea.Cmd {
	switch msg.ID {
	case "runs.rate":
		m.frm, m.frmBusy = nil, false
		if msg.Err != nil {
			m.setToast(ui.ErrText(m.cat, msg.Err), true)
			return nil
		}
		r, _ := msg.Value.(app.RateResult)
		m.setToast(m.t("rn.rated", i18n.A{"run": r.RunID, "rating": r.Rating}), false)
		m.invalidate()
		return tea.Batch(m.Load(), m.wantDetail())
	case "runs.scan":
		m.frm, m.frmBusy = nil, false
		if msg.Err != nil {
			m.setToast(ui.ErrText(m.cat, msg.Err), true)
			return nil
		}
		r, _ := msg.Value.(scanResult)
		m.setToast(m.t("rn.scanned", i18n.A{"runs": r.Runs, "committed": r.Committed, "reverted": r.Reverted}), false)
		m.invalidate()
		return tea.Batch(m.Load(), m.wantDetail())
	case "runs.ingest":
		if msg.Err != nil {
			m.setToast(ui.ErrText(m.cat, msg.Err), true)
			return nil
		}
		r, _ := msg.Value.(ingestResult)
		m.setToast(m.t("rn.ingested", i18n.A{"files": r.Files, "events": r.Events, "duplicates": r.Duplicates, "rejected": r.Rejected,
			"skipped": r.Skipped, "interrupted": r.Interrupted}), false)
		m.invalidate()
		return tea.Batch(m.Load(), m.wantDetail())
	case "runs.lesson_digest":
		m.frm, m.frmBusy, m.digestBusy = nil, false, false
		if o, ok := msg.Opts.(evolvesvc.ExperienceDigestOptions); ok {
			m.digestRef = o.Ref
		}
		if msg.Err != nil {
			m.digest, m.digestErr = nil, msg.Err
			return nil
		}
		m.digest, m.digestErr = nil, nil
		if d, ok := msg.Value.(*evolvesvc.DigestResult); ok {
			m.digest = d
		}
		m.tab, m.pane = tabLessons, paneDetail
		m.detail.Reset()
		return m.loadLessons()
	}
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
	switch msg.String() {
	case ".":
		return m.setTab((m.tab + 1) % numTabs), true
	case ",":
		return m.setTab((m.tab + numTabs - 1) % numTabs), true
	}
	return m.listKey(msg)
}

func (m *Model) setTab(i int) tea.Cmd {
	m.tab, m.pane = i, paneList
	m.detail.Reset()
	if i == tabLessons {
		return m.loadLessons()
	}
	return m.wantDetail()
}

func (m *Model) cur() *list.Model {
	if m.tab == tabLessons {
		return &m.llst
	}
	return &m.lst
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
	case "home", "g":
		if m.pane == paneDetail {
			m.detail.Home()
			return nil, true
		}
		m.cur().Home()
		return m.wantDetail(), true
	case "end", "G":
		if m.pane == paneDetail {
			m.detail.End()
			return nil, true
		}
		m.cur().End()
		return m.wantDetail(), true
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
		if m.tab == tabRuns {
			m.textIn, m.textOld = true, m.filter
		}
		return nil, true
	case "ctrl+r", "f5":
		m.invalidate()
		cmds := []tea.Cmd{m.Load(), m.wantDetail()}
		if m.tab == tabLessons {
			m.lessonsLoad = false
			cmds = append(cmds, m.loadLessons())
		}
		return tea.Batch(cmds...), true
	case "+":
		return m.cmdRate(app.RatingGood), true
	case "-":
		return m.cmdRate(app.RatingBad), true
	case "S":
		return m.cmdScan(), true
	case "I":
		return m.cmdIngest(), true
	case "l":
		return m.cmdLimit(), true
	case "d":
		return m.cmdDigest(), true
	}
	return nil, false
}

func (m *Model) nav(d int) tea.Cmd {
	if m.pane == paneDetail {
		m.detail.Scroll(d)
		return nil
	}
	if m.tab == tabLessons {
		m.moveLesson(d)
		return nil
	}
	m.cur().Move(d)
	return m.wantDetail()
}

// moveLesson dời con trỏ thẻ Bài học d hàng, bỏ qua tiêu đề nhóm.
func (m *Model) moveLesson(d int) {
	rows := m.lessonRows()
	m.llst.SetN(len(rows))
	step := 1
	if d < 0 {
		step = -1
	}
	cur := m.llst.Cursor
	for n := 0; n != d; n += step {
		next := cur + step
		for next >= 0 && next < len(rows) && rows[next].row == nil {
			next += step
		}
		if next < 0 || next >= len(rows) {
			break
		}
		cur = next
	}
	m.llst.MoveTo(cur)
	m.detail.Reset()
}

func (m *Model) page(d int) tea.Cmd {
	if m.pane == paneDetail {
		m.detail.Page(d)
		return nil
	}
	m.cur().Page(d)
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
	}
	return nil
}

func (m *Model) openForm(id, cli string, defaults any) {
	f := form.New(id, cli, defaults)
	f.SetSuggestions("runs", m.Data.IDs())
	f.SetSuggestions("batches", nil)
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

func (m *Model) submitForm(v any) tea.Cmd {
	id := m.frmID
	switch o := v.(type) {
	case app.RunsListOptions:
		m.frm, m.limit = nil, o.Limit
		if m.limit <= 0 {
			m.limit = app.RunsListDefaults(config.Default()).Limit
		}
		m.invalidate()
		return m.Load()
	case evolvesvc.ExperienceDigestOptions:
		m.digestBusy = true
	}
	m.frmBusy = true
	return request(id, v)
}

// ---- lệnh (phím và bảng lệnh) ----

// cmdRate mở hộp nhập ghi chú rồi chấm lượt dưới con trỏ (chỉ người; kiểm ở sổ và ở dịch vụ).
func (m *Model) cmdRate(rating string) tea.Cmd {
	r := m.Cursor()
	if m.tab != tabRuns || r == nil || !m.allowed("runs.rate") {
		return nil
	}
	id := r.Run.RunID
	c := confirm.Model{Title: m.t("rn.rate.title", i18n.A{"run": id, "rating": m.t("rn.rating." + rating)}), Input: true,
		Label: m.t("rn.rate.note"), Placeholder: m.t("rn.rate.note.hint"), YesLabel: m.t("rn.rate.yes"), NoLabel: m.t("rn.dialog.cancel")}
	m.dlg = &dialog{c: c, onYes: func(note string) tea.Cmd {
		return request("runs.rate", app.RunsRateOptions{Ref: id, Rating: rating, Note: note})
	}}
	return nil
}

// cmdScan mở form quét Git (days, revert-days).
func (m *Model) cmdScan() tea.Cmd {
	if m.allowed("runs.scan") {
		m.openForm("runs.scan", "runs scan", app.RunsScanDefaults(config.Default()))
	}
	return nil
}

// cmdIngest nạp spool vào cơ sở dữ liệu ngay.
func (m *Model) cmdIngest() tea.Cmd {
	if !m.allowed("runs.ingest") {
		return nil
	}
	return request("runs.ingest", app.EventsIngestOptions{})
}

// cmdLimit mở form đổi số lượt tối đa.
func (m *Model) cmdLimit() tea.Cmd {
	if m.tab == tabRuns {
		o := app.RunsListDefaults(config.Default())
		o.Limit = m.limit
		m.openForm("runs.list", "runs list", o)
	}
	return nil
}

// cmdDigest mở form bản tóm tắt thất bại: ở thẻ Danh sách điền sẵn lượt đang chọn, ở thẻ Bài học gợi ý latest.
func (m *Model) cmdDigest() tea.Cmd {
	o := evolvesvc.ExperienceDigestDefaults(config.Default())
	o.Ref = "latest"
	if r := m.Cursor(); r != nil && m.tab == tabRuns {
		o.Ref = r.Run.RunID
	}
	m.openForm("runs.lesson_digest", "experience digest", o)
	return nil
}

// Command thực hiện hành động theo ID (từ bảng lệnh).
func (m *Model) Command(id string) tea.Cmd {
	switch id {
	case "runs.rate":
		if m.Cursor() == nil || !m.allowed("runs.rate") {
			return nil
		}
		o := app.RunsRateDefaults(config.Default())
		o.Ref, o.Rating = m.Cursor().Run.RunID, app.RatingGood
		m.openForm("runs.rate", "runs rate", o)
	case "runs.scan":
		return m.cmdScan()
	case "runs.ingest":
		return m.cmdIngest()
	case "runs.list":
		return m.cmdLimit()
	case "runs.show":
		m.pane = paneDetail
	case "runs.lesson_show":
		return m.setTab(tabLessons)
	case "runs.lesson_digest":
		return m.cmdDigest()
	case "runs.filter":
		if m.tab == tabRuns {
			m.textIn, m.textOld = true, m.filter
		}
	case "runs.refresh":
		m.invalidate()
		return tea.Batch(m.Load(), m.wantDetail())
	case "runs.next_view":
		return m.setTab((m.tab + 1) % numTabs)
	case "runs.prev_view":
		return m.setTab((m.tab + numTabs - 1) % numTabs)
	}
	return nil
}
