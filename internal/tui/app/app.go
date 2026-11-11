// Package app là mô hình gốc (Bubble Tea) của TUI: khung bảy tab, chân phím theo ngữ cảnh, bảng lệnh,
// trợ giúp, đổi ngôn ngữ ngay trong phiên và việc nạp dữ liệu nền. Mọi chuỗi giao diện đi qua i18n,
// mọi hành động đi qua sổ hành động.
package app

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"bint/internal/i18n"
	"bint/internal/tui/actions"
	"bint/internal/tui/components/footer"
	"bint/internal/tui/components/frame"
	"bint/internal/tui/components/help"
	"bint/internal/tui/components/palette"
	"bint/internal/tui/components/tabs"
	"bint/internal/tui/components/text"
	"bint/internal/tui/screens/initplaceholder"
	"bint/internal/tui/screens/overview"
	"bint/internal/tui/screens/placeholder"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

// Kích thước tối thiểu để vẽ giao diện; nhỏ hơn thì hiện thông báo yêu cầu phóng to.
const (
	MinW, MinH = 60, 20
	// NarrowW: dưới ngưỡng này tab rút gọn.
	NarrowW = 100
)

// Options điều khiển mô hình.
type Options struct {
	Dir      string // thư mục repository
	Version  string
	BintPath string
	// Lang là ngôn ngữ đã chọn ("vi", "en", hoặc "xx" cho locale giả); rỗng = mặc định.
	Lang string
	Mode theme.Mode
	// Agent: TUI mở trong phiên agent (hành động chỉ-dành-cho-người bị khoá).
	Agent bool
	// ReadOnly: ẩn mọi hành động ghi. Chế độ tiện dụng, không phải ranh giới bảo mật.
	ReadOnly bool
	// Registry thay được trong kiểm thử; nil = actions.Default().
	Registry *actions.Registry
	// Loader thay được trong kiểm thử; nil = nạp thật từ repository.
	Loader func(ctx context.Context, dir string) (overview.Data, error)
	// SpinnerFPS là chu kỳ khung của vòng quay; 0 = mặc định (thay được trong kiểm thử).
	SpinnerFPS time.Duration
}

// Model là mô hình gốc.
type Model struct {
	opts Options
	reg  *actions.Registry
	cat  *i18n.Catalog
	th   theme.Theme
	ctx  context.Context

	w, h int
	tab  int

	ov  overview.Model
	ini initplaceholder.Model

	spin    spinner.Model
	ticking bool // đang có chuỗi tick của vòng quay

	// holdInit: khởi tạo vừa xong; giữ màn Khởi tạo (báo cáo bước, cảnh báo, bước tiếp theo) đến
	// khi người dùng bấm tiếp, dù Tổng quan đã nạp lại và không còn NotInit.
	holdInit bool
	// initDone: khởi tạo trong phiên này đã thành công, nên khi lần nạp lại sau đó LỖI (Tổng quan còn
	// giữ dữ liệu cũ có NotInit) vẫn không coi là chưa init. Lần nạp THÀNH CÔNG trả NotInit thì tin
	// kết quả đó (ví dụ .bint/ bị xoá ở nơi khác) và hiện lại màn Khởi tạo.
	initDone bool
	// reloadPending: có yêu cầu nạp lại trong lúc đang nạp; nạp thêm một lần khi lần này xong.
	reloadPending bool
	// quitAsked: đã nhắc "đang khởi tạo" một lần; nhấn q/Ctrl+C lần nữa thì thoát thật.
	quitAsked bool

	pal      *palette.Model
	showHelp bool
	toast    string
	quitting bool
}

// New dựng mô hình. ctx là ngữ cảnh của các việc nền.
func New(ctx context.Context, opts Options) *Model {
	if opts.Registry == nil {
		opts.Registry = actions.Default()
	}
	lang := opts.Lang
	if lang == "" {
		lang = i18n.Default
	}
	sp := spinner.MiniDot
	if opts.SpinnerFPS > 0 {
		sp.FPS = opts.SpinnerFPS
	}
	return &Model{opts: opts, reg: opts.Registry, cat: i18n.MustLoad(lang), th: theme.New(opts.Mode), ctx: ctx,
		spin: spinner.New(spinner.WithSpinner(sp)),
		ini:  initplaceholder.Model{Dir: opts.Dir, ReadOnly: opts.ReadOnly}}
}

// SetSize đặt kích thước (dùng trong kiểm thử; khi chạy thật kích thước đến qua tea.WindowSizeMsg).
func (m *Model) SetSize(w, h int) { m.w, m.h = w, h }

// Lang trả ngôn ngữ đang dùng.
func (m *Model) Lang() string { return m.cat.Lang() }

// Tab trả chỉ số tab đang chọn.
func (m *Model) Tab() int { return m.tab }

// Screen trả màn hình đang hiện (màn khởi tạo thay Tổng quan khi repository chưa init).
func (m *Model) Screen() actions.Screen {
	if m.tab == 0 && (m.needsInit() || m.holdInit) {
		return actions.Init
	}
	return actions.Screens[m.tab]
}

func (m *Model) needsInit() bool {
	return m.ov.Loaded && m.ov.Data.NotInit && !(m.initDone && m.ov.Err != nil)
}

func (m *Model) mode() actions.Mode {
	return actions.Mode{ReadOnly: m.opts.ReadOnly, Agent: m.opts.Agent}
}

func (m *Model) env() actions.Env {
	return actions.Env{Dir: m.opts.Dir, Version: m.opts.Version, BintPath: m.opts.BintPath}
}

// busy: có việc nền đang chạy (nạp dữ liệu hoặc khởi tạo).
func (m *Model) busy() bool { return m.ov.Loading || m.ini.Running }

// startTick bắt đầu chuỗi tick của vòng quay nếu chưa chạy.
func (m *Model) startTick() tea.Cmd {
	if m.ticking {
		return nil
	}
	m.ticking = true
	return func() tea.Msg { return m.spin.Tick() }
}

// load nạp dữ liệu Tổng quan. Đang nạp thì không khởi thêm goroutine (mỗi lần nạp có thể nạp spool
// vào SQLite): chỉ đánh dấu để nạp lại một lần khi lần hiện tại xong, nên kết quả cũ không ghi đè mới.
func (m *Model) load() tea.Cmd {
	if m.ov.Loading {
		m.reloadPending = true
		return nil
	}
	m.ov.Loading = true
	loader := m.opts.Loader
	if loader == nil {
		loader = overview.Load
		if m.opts.ReadOnly {
			loader = overview.LoadNoIngest // chế độ chỉ đọc không ghi SQLite (mục 7.4)
		}
	}
	ctx, dir := m.ctx, m.opts.Dir
	load := func() tea.Msg {
		d, err := loader(ctx, dir)
		return overview.LoadedMsg{Data: d, Err: err}
	}
	return tea.Batch(load, m.startTick())
}

// Init nạp dữ liệu ban đầu.
func (m *Model) Init() tea.Cmd { return m.load() }

// Update xử lý thông điệp.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case spinner.TickMsg:
		if !m.busy() { // hết việc: dừng chuỗi tick
			m.ticking = false
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case overview.LoadedMsg:
		m.ov, _ = m.ov.Update(msg)
		if msg.Err == nil && msg.Data.NotInit {
			m.initDone = false // kết quả đáng tin gần nhất là chưa init: lần nạp lỗi sau đó không được che nó
		}
		if m.reloadPending {
			m.reloadPending = false
			return m, m.load()
		}
	case actions.InitDoneMsg:
		m.ini = m.ini.Finish(msg.Report, msg.Err)
		m.quitAsked = false
		if msg.Err == nil {
			m.initDone = true
			m.holdInit = true  // giữ báo cáo đến khi người dùng bấm tiếp (init.continue)
			return m, m.load() // nạp lại Tổng quan: sau khi khởi tạo sẽ hiện bảng điều khiển
		}
	case overview.NavMsg:
		m.goTo(msg.Screen)
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m *Model) goTo(s actions.Screen) {
	for i, sc := range actions.Screens {
		if sc == s {
			m.tab = i
			return
		}
	}
}

func (m *Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := msg.String()
	m.toast = ""
	if s != "q" && s != "ctrl+c" {
		m.quitAsked = false
	}
	switch {
	case m.pal != nil:
		return m.paletteKey(msg)
	case m.showHelp:
		switch s {
		case "esc", "ctrl+c", "?", "q", "enter":
			m.showHelp = false
		}
		return m, nil
	}
	switch s {
	case "ctrl+c", "q":
		return m.run("app.quit")
	case "?":
		return m.run("app.help")
	case ":", "ctrl+k":
		return m.run("app.palette")
	case "L":
		return m.run("app.language")
	case "]", "tab", "right":
		return m.run("app.next_tab")
	case "[", "shift+tab", "left":
		return m.run("app.prev_tab")
	case "1", "2", "3", "4", "5", "6", "7":
		return m.run(actions.NavID(actions.Screens[int(s[0]-'1')]))
	}
	// Phím riêng của màn hình hiện tại: tra sổ hành động theo phím, rồi chuyển cho màn hình.
	for _, a := range m.reg.For(m.Screen(), m.mode()) {
		if a.Screen != actions.Global && a.Key != "" && strings.EqualFold(a.Key, s) && m.applicable(a) {
			return m.run(a.ID)
		}
	}
	if m.Screen() == actions.Overview {
		var cmd tea.Cmd
		m.ov, cmd = m.ov.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) paletteKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch s := msg.String(); s {
	case "esc", "ctrl+c":
		m.pal = nil
	case "enter":
		it, ok := m.pal.Selected()
		m.pal = nil
		if ok {
			if it.Locked != "" {
				m.toast = it.Locked
				return m, nil
			}
			return m.run(it.ID)
		}
	case "up", "ctrl+p":
		m.pal.Move(-1)
	case "down", "ctrl+n":
		m.pal.Move(1)
	case "backspace":
		m.pal.Backspace()
	case "ctrl+u":
		m.pal.Clear()
	default:
		if msg.Text != "" {
			m.pal.Type(msg.Text)
		}
	}
	return m, nil
}

// run thực hiện hành động theo ID. Hành động bị khoá hoặc ẩn không chạy (kiểm tra theo sổ).
func (m *Model) run(id string) (tea.Model, tea.Cmd) {
	a, ok := m.reg.ByID(id)
	if ok {
		switch a.Check(m.mode()) {
		case actions.Hidden:
			return m, nil
		case actions.Locked:
			m.toast = m.cat.T("ui.locked.agent")
			return m, nil
		}
	}
	if ok && !m.applicable(a) {
		return m, nil // hành động của màn khác hoặc không hợp với trạng thái hiện tại (vd. init khi đã init)
	}
	switch id {
	case "app.quit":
		if m.ini.Running && !m.quitAsked {
			// Đang khởi tạo (việc nền ghi): đừng thoát ngay kẻo repo khởi tạo dở. Nhấn lần nữa để thoát.
			m.quitAsked = true
			m.toast = m.cat.T("init.quit_wait")
			return m, nil
		}
		m.quitting = true
		return m, tea.Quit
	case "app.help":
		m.showHelp = true
	case "app.palette":
		p := palette.New(m.paletteItems())
		m.pal = &p
	case "app.language":
		m.cycleLanguage()
	case "app.next_tab":
		m.tab = (m.tab + 1) % len(actions.Screens)
	case "app.prev_tab":
		m.tab = (m.tab - 1 + len(actions.Screens)) % len(actions.Screens)
	case "overview.refresh", "overview.doctor", "init.recheck":
		return m, m.load()
	case "init.continue":
		m.holdInit = false
		m.ini = initplaceholder.Model{Dir: m.opts.Dir, ReadOnly: m.opts.ReadOnly}
	case "init.defaults":
		if m.ini.Running || !ok || a.Run == nil {
			return m, nil
		}
		m.ini.Running, m.ini.Report, m.ini.Err = true, nil, nil
		ctx, env, run := m.ctx, m.env(), a.Run
		return m, tea.Batch(func() tea.Msg { return run(ctx, env) }, m.startTick())
	default:
		if strings.HasPrefix(id, "nav.") {
			m.goTo(actions.Screen(strings.TrimPrefix(id, "nav.")))
		}
	}
	return m, nil
}

// cycleLanguage đổi ngôn ngữ ngay (không cần khởi động lại): vi → en → vi.
func (m *Model) cycleLanguage() {
	next := i18n.Vietnamese
	if m.cat.Lang() == i18n.Vietnamese {
		next = i18n.English
	}
	m.cat = i18n.MustLoad(next)
}

// applicable cho biết hành động có dùng được ở trạng thái hiện tại không, ngoài kiểm tra theo Mode.
// Hành động của màn tạm Khởi tạo chỉ dùng khi đang ở màn đó, và theo trạng thái của nó: "Khởi tạo"
// khi chưa chạy và chưa có báo cáo đang giữ (repository phải còn chưa init), "Tiếp tục" chỉ khi đã
// khởi tạo xong và Tổng quan đã nạp lại. Bảng lệnh, chân phím, trợ giúp và run() cùng dùng hàm này.
func (m *Model) applicable(a actions.Action) bool {
	if a.Screen != actions.Init {
		return true
	}
	if m.Screen() != actions.Init {
		return false
	}
	switch a.ID {
	case "init.defaults":
		return m.needsInit() && !m.holdInit
	case "init.continue":
		return m.holdInit && !m.ov.Loading && !m.ini.Running
	}
	return !m.holdInit
}

func (m *Model) paletteItems() []palette.Item {
	var items []palette.Item
	for _, a := range m.reg.Visible(m.mode()) {
		if !m.applicable(a) {
			continue
		}
		it := palette.Item{ID: a.ID, Title: m.cat.T(a.TitleKey), Keywords: a.Keywords(), Key: a.Key}
		if a.Screen != actions.Global {
			it.Group = m.groupName(a.Screen)
		}
		if a.Check(m.mode()) == actions.Locked {
			it.Locked = m.cat.T("ui.locked.agent")
		}
		items = append(items, it)
	}
	return items
}

func (m *Model) groupName(s actions.Screen) string {
	if s == actions.Init {
		return m.cat.T("init.group")
	}
	return m.cat.T("tab." + string(s))
}

// Hints trả các phím của chân phím theo ngữ cảnh hiện tại, chưa lọc theo độ rộng. Thứ tự hiển thị:
// phím riêng của màn hình, rồi phím toàn cục, cuối cùng các phím ghim (trợ giúp, thoát).
func (m *Model) Hints() []footer.Hint {
	var screen, global, pinned []footer.Hint
	for _, a := range m.reg.For(m.Screen(), m.mode()) {
		if !a.Footer || a.Check(m.mode()) != actions.Available || !m.applicable(a) {
			continue
		}
		h := footer.Hint{Key: a.Key, Label: m.cat.T(a.LabelKey), Priority: a.Priority, Pinned: a.Pinned}
		switch {
		case a.Pinned:
			pinned = append(pinned, h)
		case a.Screen == actions.Global:
			global = append(global, h)
		default:
			screen = append(screen, h)
		}
	}
	return append(append(screen, global...), pinned...)
}

// View vẽ giao diện.
func (m *Model) View() tea.View {
	v := tea.NewView(m.Render())
	v.AltScreen = true
	return v
}

// Render trả nội dung đã vẽ (mỗi dòng rộng đúng m.w ô khi đủ chỗ).
func (m *Model) Render() string {
	w, h := m.w, m.h
	if w <= 0 || h <= 0 {
		return ""
	}
	if w < MinW || h < MinH {
		return m.tooSmall(w, h)
	}
	env := ui.Env{Th: m.th, Cat: m.cat, W: w, H: h, Spin: m.spin.View()}

	header := m.header(env)
	mainH := h - 3
	iw, ih := frame.Inner(w, mainH)
	body := m.screenBody(env.WithSize(iw, ih))
	if m.pal != nil || m.showHelp {
		// Hộp phủ đang mở: che nền trong khung nội dung để chữ của màn bên dưới không lộ (bị cắt ngang)
		// ở hai bên và dưới hộp.
		body = ""
	}
	main := frame.Box{
		Title: m.screenTitle(), TitleAt: frame.Left, Focused: true,
		Top:    m.toastLabel(),
		Bottom: footer.Line(m.th, m.Hints(), w-6), BottomAt: frame.Left,
	}.Render(m.th, w, mainH, body)
	lines := strings.Split(header+"\n"+main, "\n")

	switch {
	case m.pal != nil:
		bw, bh := min(w-4, 76), min(h-6, 16)
		box := strings.Split(m.pal.View(m.th, bw, bh, m.cat.T("palette.title"), m.cat.T("palette.empty")), "\n")
		lines = text.Overlay(lines, box, (w-bw)/2, 4)
	case m.showHelp:
		bw, bh := min(w-4, 84), min(h-7, 26)
		box := strings.Split(help.Render(m.th, bw, bh, m.cat.T("help.title"), m.helpGroups(), m.cat.T("help.more")), "\n")
		lines = text.Overlay(lines, box, (w-bw)/2, 4)
	}
	return strings.Join(lines, "\n")
}

func (m *Model) tooSmall(w, h int) string {
	msg := m.cat.T("ui.too_small", i18n.A{"minw": MinW, "minh": MinH, "w": w, "h": h})
	lines := text.Wrap(msg, max(w, 1))
	for len(lines) < h {
		lines = append(lines, "")
	}
	out := make([]string, 0, h)
	pad := max((h-len(lines))/2, 0)
	for i := 0; i < h; i++ {
		src := i - pad
		if src >= 0 && src < len(lines) {
			out = append(out, text.Fit(lines[src], w))
		} else {
			out = append(out, strings.Repeat(" ", w))
		}
	}
	return strings.Join(out, "\n")
}

func (m *Model) screenTitle() string {
	if m.Screen() == actions.Init {
		return m.cat.T("init.group")
	}
	return m.cat.T("tab." + string(actions.Screens[m.tab]))
}

func (m *Model) toastLabel() string {
	if m.toast == "" {
		return ""
	}
	return m.th.S(theme.Warn, " "+m.toast+" ")
}

func (m *Model) screenBody(env ui.Env) string {
	switch s := m.Screen(); s {
	case actions.Init:
		ini := m.ini
		if m.holdInit && !m.ov.Loading { // đang nạp lại thì ov.Err còn là lỗi của lần trước
			ini.LoadErr = m.ov.Err // khởi tạo xong mà nạp lại lỗi: nói rõ, đừng im lặng
		}
		return ini.View(env)
	case actions.Overview:
		return m.ov.View(env)
	default:
		return placeholder.View(env, s)
	}
}

// header vẽ thanh trên: tiêu đề bint + phiên bản trên viền, thanh tab, đường dẫn/cảnh báo trên viền dưới.
func (m *Model) header(env ui.Env) string {
	w := env.W
	items := make([]tabs.Item, len(actions.Screens))
	for i, s := range actions.Screens {
		items[i] = tabs.Item{Label: m.cat.T("tab." + string(s)), Short: m.cat.T("tab." + string(s) + ".short")}
	}
	if m.ov.Loaded && m.ov.Data.Candidates > 0 {
		items[1].Badge = m.ov.Data.Candidates // Tri thức
	}
	bar := tabs.Render(m.th, w-4, items, m.tab, w < NarrowW)

	var flags []string
	if m.opts.ReadOnly {
		flags = append(flags, m.th.S(theme.Key, "["+m.cat.T("ui.readonly")+"]"))
	}
	if m.opts.Agent {
		flags = append(flags, m.th.S(theme.Warn, "["+m.cat.T("ui.agent_session")+"]"))
	}
	bottom := m.th.S(theme.Muted, " "+text.Path(m.opts.Dir, max(w/2, 20))+" ")
	if m.ov.Loaded && m.ov.Data.Status != nil && m.ov.Data.Status.LinkedWorktree {
		main := m.ov.Data.Status.MainWorktree
		bottom = m.th.S(theme.Warn, " "+m.cat.T("ui.worktree", i18n.A{"main": text.Path(main, max(w/2, 20))})+" ")
	}
	return frame.Box{
		Title: "bint " + m.opts.Version, TitleAt: frame.Center, Top: strings.Join(flags, " "),
		Bottom: bottom, BottomAt: frame.Right,
	}.Render(m.th, w, 3, bar)
}

func (m *Model) helpGroups() []help.Group {
	global := help.Group{Title: m.cat.T("help.global")}
	byScreen := map[actions.Screen]*help.Group{}
	var order []actions.Screen
	navAdded := false
	for _, a := range m.reg.Visible(m.mode()) {
		if !m.applicable(a) {
			continue // hành động của màn tạm chỉ hiện khi đang ở đó và hợp trạng thái
		}
		if strings.HasPrefix(a.ID, "nav.") { // 7 dòng chuyển tab gộp thành một
			if !navAdded {
				navAdded = true
				global.Rows = append(global.Rows, help.Row{Key: "1-7", Title: m.cat.T("help.nav")})
			}
			continue
		}
		row := help.Row{Key: a.Key, Title: m.cat.T(a.TitleKey)}
		if a.CLIPath != "" {
			row.CLI = "bint " + a.CLIPath
		}
		if a.Check(m.mode()) == actions.Locked {
			row.Locked = m.cat.T("ui.locked.agent")
		}
		if a.Screen == actions.Global {
			global.Rows = append(global.Rows, row)
			continue
		}
		g := byScreen[a.Screen]
		if g == nil {
			g = &help.Group{Title: m.groupName(a.Screen)}
			byScreen[a.Screen] = g
			order = append(order, a.Screen)
		}
		g.Rows = append(g.Rows, row)
	}
	// Nhóm của màn hình đang xem đứng đầu, rồi toàn cục, rồi các màn hình khác.
	var groups []help.Group
	cur := m.Screen()
	if g := byScreen[cur]; g != nil {
		groups = append(groups, *g)
	}
	groups = append(groups, global)
	for _, s := range order {
		if s != cur {
			groups = append(groups, *byScreen[s])
		}
	}
	return groups
}
