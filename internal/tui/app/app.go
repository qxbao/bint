// Package app là mô hình gốc (Bubble Tea) của TUI: khung tám tab, chân phím theo ngữ cảnh, bảng lệnh,
// trợ giúp, đổi ngôn ngữ ngay trong phiên và việc nạp dữ liệu nền. Mọi chuỗi giao diện đi qua i18n,
// mọi hành động đi qua sổ hành động.
package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	bintapp "bint/internal/app"
	"bint/internal/app/evolvesvc"
	"bint/internal/config"
	"bint/internal/detect"
	"bint/internal/i18n"
	"bint/internal/tui/actions"
	"bint/internal/tui/clipboard"
	"bint/internal/tui/components/confirm"
	"bint/internal/tui/components/footer"
	"bint/internal/tui/components/help"
	"bint/internal/tui/components/palette"
	"bint/internal/tui/components/tabs"
	"bint/internal/tui/components/text"
	"bint/internal/tui/components/zone"
	"bint/internal/tui/jobs"
	elscreen "bint/internal/tui/screens/eval"
	evscreen "bint/internal/tui/screens/evolve"
	"bint/internal/tui/screens/initwizard"
	knscreen "bint/internal/tui/screens/knowledge"
	mcscreen "bint/internal/tui/screens/mcp"
	"bint/internal/tui/screens/overview"
	rnscreen "bint/internal/tui/screens/runs"
	syscreen "bint/internal/tui/screens/system"
	tkscreen "bint/internal/tui/screens/tasks"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
	"bint/internal/workflow"
)

// Kích thước tối thiểu để vẽ giao diện; nhỏ hơn thì hiện thông báo yêu cầu phóng to.
const (
	MinW, MinH = 60, 20
	// NarrowW: dưới ngưỡng này tab rút gọn.
	NarrowW = 100
)

// Options điều khiển mô hình.
type Options struct {
	Dir string // thư mục repository
	// Clipboard thay được trong kiểm thử (đọc cho Ctrl+V, ghi khi chọn bằng chuột); nil = clipboard hệ thống.
	Clipboard clipboard.Clipboard
	Version   string
	BintPath  string
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
	// KnowledgeLoader/KnowledgeDetailer thay được trong kiểm thử; nil = nạp thật.
	KnowledgeLoader   knscreen.Loader
	KnowledgeDetailer knscreen.Detailer
	// TasksLoader/TasksDetailer/TasksRuns thay được trong kiểm thử; nil = nạp thật.
	TasksLoader   tkscreen.Loader
	TasksDetailer tkscreen.Detailer
	TasksRuns     tkscreen.RunsLoader
	// TasksRunsDebounce là độ trễ gom lần nạp lượt chạy của thẻ Phiếu khi dời con trỏ (0 = mặc định).
	TasksRunsDebounce time.Duration
	// RunsLoader/RunsDetailer/RunsLessons thay được trong kiểm thử; nil = nạp thật.
	RunsLoader   rnscreen.Loader
	RunsDetailer rnscreen.Detailer
	RunsLessons  rnscreen.LessonLoader
	// EvalLoader/EvalDetailer/EvalReporter thay được trong kiểm thử; nil = nạp thật.
	EvalLoader   elscreen.Loader
	EvalDetailer elscreen.Detailer
	EvalReporter elscreen.Reporter
	// EvolveLoader/EvolveDetailer thay được trong kiểm thử; nil = nạp thật.
	EvolveLoader   evscreen.Loader
	EvolveDetailer evscreen.Detailer
	// SystemLoad thay các hàm nạp dữ liệu của màn Hệ thống (trường nil = nạp thật); thay được trong kiểm thử.
	SystemLoad syscreen.Loaders
	// MCPLoad thay các hàm nạp dữ liệu của màn MCP (trường nil = nạp thật); thay được trong kiểm thử.
	MCPLoad mcscreen.Loaders
	// Getenv thay os.Getenv khi chọn ngôn ngữ sau khi lưu ui.language; nil = os.Getenv.
	Getenv func(string) string
	// LangFlag là giá trị cờ --lang (rỗng = không chỉ định); cờ thắng mọi giá trị ui.language vừa ghi.
	LangFlag string
	// Agent thay cách dựng agent của từng bước workflow (nil = harness theo cấu hình); tiêm agent giả trong kiểm thử.
	AgentFactory workflow.AgentFactory
	// CycleRunner thay cách chạy các bước của `evolve cycle` (nil = trong tiến trình); tiêm runner giả trong kiểm thử.
	CycleRunner evolvesvc.CycleRunner
	// LedgerWarnings lấy (và xoá) cảnh báo sổ phiếu .bint/tasks đang chờ (lỗi ghi, xung đột, thay đổi chờ nhập);
	// TUI hiện chúng thành thông báo lỗi sau mỗi lần Update. nil = không có.
	LedgerWarnings func() []string
	// Layout là ui.layout đọc sẵn lúc mở TUI (tỉ lệ pane theo màn); nil = đọc ở nền khi Init.
	Layout map[string]int
	// ExitGrace là thời gian chờ việc nền kết thúc sau khi huỷ lúc thoát; 0 = 15 giây.
	ExitGrace time.Duration
	// TailEvery là chu kỳ đọc đuôi log của bước agent đang chạy; 0 = 400 ms.
	TailEvery time.Duration
	// Editor dựng lệnh mở tệp trong trình soạn thảo (thay được trong kiểm thử); nil = $VISUAL/$EDITOR.
	Editor func(path string) *exec.Cmd
	// DetectHarnesses thay cách phát hiện harness trên máy của trình hướng dẫn init (nil = detect.Harnesses); thay được trong kiểm thử.
	DetectHarnesses func() []detect.Harness
	// Update thay cách cập nhật Bint của hành động system.update (nil = app.UpdateSelf); tiêm hàm giả trong kiểm thử.
	Update func(ctx context.Context, o bintapp.UpdateOptions) (bintapp.UpdateResult, error)
	// CanRestart: nền tảng tự khởi động lại được bằng exec (Unix); Windows chỉ yêu cầu người dùng mở lại bint.
	CanRestart bool
	// SpinnerFPS là chu kỳ khung của vòng quay; 0 = mặc định (thay được trong kiểm thử).
	SpinnerFPS time.Duration
	// ActivePoll là chu kỳ thăm dò bước `bint run` đang chạy trên cây này (dòng trạng thái ở thanh trên); 0 = không thăm
	// dò (chỉ nạp lúc nạp dữ liệu). Kiểm thử để 0 để drive() không lặp vô hạn.
	ActivePoll time.Duration
	// DataPoll là chu kỳ thăm dò thay đổi dữ liệu repository (stat .bint, SQLite, spool; không mở SQLite, không ghi):
	// đổi thì Tổng quan và màn đang hiện tự nạp lại, các màn chuyên biệt khác chỉ bị đánh dấu cũ; 0 = tắt (kiểm thử).
	DataPoll time.Duration
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
	wiz *initwizard.Model
	kn  *knscreen.Model
	tk  *tkscreen.Model
	rn  *rnscreen.Model
	ev  *evscreen.Model
	el  *elscreen.Model
	sy  *syscreen.Model
	mc  *mcscreen.Model
	// screens là các màn hình chuyên biệt theo tab (giao diện chung ui.Screen); mô hình gốc không rẽ nhánh theo màn.
	screens map[actions.Screen]ui.Screen

	// jobs là bảng việc nền đang chạy (hành động có tham số, workflow, kiểm chứng, khởi tạo).
	jobs *jobs.Table
	// jobCh là kênh thông điệp của việc nền (câu hỏi, log, bước, xong); pumping: đã có một jobs.Wait đang chờ.
	jobCh   chan tea.Msg
	pumping bool
	// prompt là câu hỏi giữa chừng đang hiện (hộp thoại); chỉ một câu một lúc, câu sau chờ trong kênh.
	prompt *promptState
	// exitDlg là hộp thoại thoát khi còn việc nền; exiting: đã chọn thoát và đang chờ việc kết thúc.
	exitDlg *confirm.Model
	exiting bool
	// restartAsk: hộp thoại đang mở là "khởi động lại" (đường dẫn tệp sẽ exec); restartPath: đã chọn khởi động lại, tui.Run exec
	// tệp này sau khi Bubble Tea thoát.
	restartAsk, restartPath string

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
	reloadSoft    bool // lần nạp bù đang chờ chỉ do thăm dò dữ liệu yêu cầu

	// active là các bước `bint run` đang giữ khoá cây làm việc này (đọc từ tệp chủ khoá); activeLoading: đang đọc;
	// activeTicking: đã hẹn lần thăm dò kế (Options.ActivePoll > 0).
	active                       []workflow.ActiveStep
	activeLoading, activeTicking bool
	// activeWatch dựng ở load() (cả khi nạp lại) để lượt thăm dò chỉ đọc tệp chủ khoá, không mở lại workspace.
	activeWatch *bintapp.ActiveStepsWatch

	// Thăm dò dữ liệu (Options.DataPoll): dataWatch dựng khi cần; dataSig là chữ ký lần thấy gần nhất (dataSigSet: đã có
	// nền); dataLoading: đang tính chữ ký; dataTicking: đã hẹn lượt kế; dataPending: có thay đổi chưa nạp (đang Capturing).
	dataWatch                                         *bintapp.DataWatch
	dataSig                                           uint64
	dataSigSet, dataLoading, dataTicking, dataPending bool
	// dataRebase: có mẫu chữ ký lấy trong lúc Tổng quan đang nạp; mẫu đầu tiên sau khi nạp xong chỉ ghi nền.
	dataRebase bool

	pal      *palette.Model
	showHelp bool
	helpTop  int // dòng đầu đang xem của hộp trợ giúp (cuộn)
	toast    string
	toastErr bool // toast của mô hình gốc là lỗi/cảnh báo (false: thông báo thường, ví dụ app.mouse)
	quitting bool

	// Chuột (mouse.go): vùng bấm của lần vẽ gần nhất, tắt/bật trong phiên, lịch sử thông báo và hộp Thông báo đang mở.
	zones    []zone.Zone
	mouseOff bool
	// Tỉ lệ pane (layout.go): đang kéo đường chia, số thứ tự lần hẹn ghi, các màn có tỉ lệ chưa ghi.
	palFind     bool // bảng lệnh đang là hộp tìm mọi nơi (search.go)
	dragSplit   bool
	layoutSeq   int
	layoutDirty map[string]bool
	layoutReady bool
	notes       notices
	// Chọn/chép bằng chuột (selection.go) và clipboard (paste.go).
	sel  selection
	clip clipboard.Clipboard
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
	m := &Model{opts: opts, reg: opts.Registry, cat: i18n.MustLoad(lang), th: theme.New(opts.Mode), ctx: ctx,
		spin: spinner.New(spinner.WithSpinner(sp)), jobs: &jobs.Table{}, jobCh: make(chan tea.Msg, 1024),
		wiz: initwizard.New(initwizard.Options{Dir: opts.Dir, ReadOnly: opts.ReadOnly, Detect: opts.DetectHarnesses})}
	m.clip = opts.Clipboard
	if m.clip == nil {
		m.clip = clipboard.System()
	}
	m.kn = knscreen.New(knscreen.Options{Dir: opts.Dir, ReadOnly: opts.ReadOnly, Agent: opts.Agent, Ctx: ctx,
		Loader: opts.KnowledgeLoader, Detailer: opts.KnowledgeDetailer, Gate: m.gate})
	m.tk = tkscreen.New(tkscreen.Options{Dir: opts.Dir, ReadOnly: opts.ReadOnly, Loader: opts.TasksLoader, Detailer: opts.TasksDetailer,
		Runs: opts.TasksRuns, RunsDebounce: opts.TasksRunsDebounce, Gate: m.gate, Jobs: m.jobs})
	m.rn = rnscreen.New(rnscreen.Options{Dir: opts.Dir, ReadOnly: opts.ReadOnly, Loader: opts.RunsLoader, Detailer: opts.RunsDetailer,
		Lessons: opts.RunsLessons, Gate: m.gate})
	m.ev = evscreen.New(evscreen.Options{Dir: opts.Dir, ReadOnly: opts.ReadOnly, Loader: opts.EvolveLoader, Detailer: opts.EvolveDetailer,
		Gate: m.gate, Jobs: m.jobs})
	m.el = elscreen.New(elscreen.Options{Dir: opts.Dir, ReadOnly: opts.ReadOnly, Loader: opts.EvalLoader, Detailer: opts.EvalDetailer,
		Reporter: opts.EvalReporter, Gate: m.gate, Jobs: m.jobs})
	m.sy = syscreen.New(syscreen.Options{Dir: opts.Dir, Version: opts.Version, ReadOnly: opts.ReadOnly, Load: opts.SystemLoad, Gate: m.gate,
		Jobs: m.jobs, CanRestart: opts.CanRestart})
	m.mc = mcscreen.New(mcscreen.Options{Dir: opts.Dir, Version: opts.Version, Load: opts.MCPLoad, Gate: m.gate, Jobs: m.jobs})
	m.screens = map[actions.Screen]ui.Screen{actions.Knowledge: m.kn, actions.Tasks: m.tk, actions.Runs: m.rn, actions.Eval: m.el, actions.Evolve: m.ev,
		actions.System: m.sy, actions.MCP: m.mc, actions.Init: m.wiz}
	for _, sc := range m.screens {
		sc.SetEnv(m.th, m.rw())
	}
	if opts.Layout != nil {
		m.applyLayout(opts.Layout)
	}
	return m
}

// gate cho biết hành động id dùng được không theo sổ và chế độ hiện tại (cho màn hình báo sớm).
// actionAllowed cho biết hành động id dùng được ở chế độ hiện tại (ui.Env.Allowed: chân phím trong khung tra sổ qua đây).
func (m *Model) actionAllowed(id string) bool { return m.gate(id) == actions.Available }

func (m *Model) gate(id string) actions.Availability {
	if a, ok := m.reg.ByID(id); ok {
		return a.Check(m.mode())
	}
	return actions.Available
}

// SetSize đặt kích thước (dùng trong kiểm thử; khi chạy thật kích thước đến qua tea.WindowSizeMsg).
func (m *Model) SetSize(w, h int) { m.w, m.h = w, h }

// RestartPath trả tệp cần exec lại sau khi thoát (rỗng = thoát bình thường).
func (m *Model) RestartPath() string { return m.restartPath }

// restart xử lý system.restart: không còn việc nền thì ghi đường dẫn và thoát; còn việc thì hỏi "huỷ an toàn rồi khởi động lại".
func (m *Model) restart(path string) (tea.Model, tea.Cmd) {
	if path == "" || !m.opts.CanRestart || m.exiting {
		return m, nil
	}
	if m.jobs.Len() > 0 || m.wiz.Running() {
		m.restartAsk = path
		m.openExitDialog()
		return m, nil
	}
	m.restartPath, m.quitting = path, true
	return m, tea.Quit
}

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
	return actions.Env{Dir: m.opts.Dir, Version: m.opts.Version, BintPath: m.opts.BintPath, ReadOnly: m.opts.ReadOnly, Editor: m.opts.Editor,
		Agent: m.opts.AgentFactory, CycleRunner: m.opts.CycleRunner, Update: m.opts.Update, Cat: m.cat}
}

// busy: có việc nền đang chạy (nạp dữ liệu, khởi tạo, việc nền có tham số hoặc việc của một màn hình).
func (m *Model) busy() bool {
	if m.ov.Loading || m.jobs.Len() > 0 {
		return true
	}
	for _, sc := range m.screens {
		if sc.Busy() {
			return true
		}
	}
	return false
}

// ensure nạp dữ liệu lần đầu của màn hình chuyên biệt khi người dùng tới tab của nó.
func (m *Model) ensure() tea.Cmd {
	sc := m.screens[m.Screen()]
	if sc == nil {
		return nil
	}
	cmd := sc.Ensure()
	if cmd == nil {
		return nil
	}
	return tea.Batch(cmd, m.startTick())
}

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
func (m *Model) load() tea.Cmd { return m.loadMode(false) }

// loadMode nạp Tổng quan; soft: do thăm dò dữ liệu tự kích hoạt (lỗi không thay dữ liệu đang hiện bằng trang lỗi).
func (m *Model) loadMode(soft bool) tea.Cmd {
	if m.ov.Loading {
		m.reloadSoft = soft && (!m.reloadPending || m.reloadSoft) // một yêu cầu nạp tay làm lần nạp bù thành nạp tay
		m.reloadPending = true
		return nil
	}
	m.ov.Loading = true
	loader := m.opts.Loader
	ctx, dir := m.ctx, m.opts.Dir
	var doctor tea.Cmd
	if loader == nil {
		// Nạp nhanh (chế độ chỉ đọc không ghi SQLite, mục 7.4) rồi Doctor — nguồn chậm nhất — chạy song song và bổ sung khi xong,
		// để Tổng quan hiện ngay thay vì chờ cả doctor.
		loader = overview.LoadFast(!m.opts.ReadOnly)
		doctor = overview.LoadDoctor(ctx, dir)
	}
	load := func() tea.Msg {
		d, err := loader(ctx, dir)
		return overview.LoadedMsg{Data: d, Err: err, Soft: soft}
	}
	return tea.Batch(load, doctor, m.loadActive(true), m.startTick())
}

// activeStepsMsg: kết quả đọc các bước đang chạy trên cây làm việc; watch != nil khi vừa dựng lại.
type activeStepsMsg struct {
	steps []workflow.ActiveStep
	watch *bintapp.ActiveStepsWatch
}

// activeTickMsg: tới hạn thăm dò bước đang chạy.
type activeTickMsg struct{}

// loadActive đọc các bước đang chạy trên cây này (một lần; không chồng hai lần đọc). resolve (từ load()) dựng
// lại ActiveStepsWatch; lượt thăm dò chỉ gọi Steps() trên watch đã có (chưa có thì dựng).
func (m *Model) loadActive(resolve bool) tea.Cmd {
	if m.activeLoading {
		return nil
	}
	m.activeLoading = true
	ctx, dir, w := m.ctx, m.opts.Dir, m.activeWatch
	return func() tea.Msg {
		if w == nil || resolve {
			nw, ok := bintapp.ResolveActiveSteps(ctx, dir)
			if !ok {
				if w == nil {
					return activeStepsMsg{}
				}
				return activeStepsMsg{steps: w.Steps()} // không dựng lại được: giữ watch cũ
			}
			w = &nw
			return activeStepsMsg{steps: nw.Steps(), watch: w}
		}
		return activeStepsMsg{steps: w.Steps()}
	}
}

// dataTickMsg: tới hạn thăm dò thay đổi dữ liệu.
type dataTickMsg struct{}

// dataSigMsg: kết quả tính chữ ký dữ liệu; watch != nil khi vừa dựng; ok=false: chưa dựng được watch (chưa init).
type dataSigMsg struct {
	sig   uint64
	watch *bintapp.DataWatch
	ok    bool
	// duringLoad: mẫu bắt đầu lấy khi Tổng quan đang nạp.
	duringLoad bool
}

// startDataPoll hẹn lượt thăm dò dữ liệu đầu tiên (không chồng: đã hẹn hoặc đang tính thì thôi).
func (m *Model) startDataPoll() tea.Cmd {
	if m.opts.DataPoll <= 0 || m.dataTicking || m.dataLoading {
		return nil
	}
	m.dataTicking = true
	return tea.Tick(m.opts.DataPoll, func(time.Time) tea.Msg { return dataTickMsg{} })
}

// sampleData tính chữ ký dữ liệu trong goroutine (chỉ stat). Chuỗi thăm dò không chồng: lượt kế chỉ hẹn khi kết quả về.
func (m *Model) sampleData() tea.Cmd {
	if m.dataLoading {
		return nil
	}
	m.dataLoading = true
	ctx, dir, w, busy := m.ctx, m.opts.Dir, m.dataWatch, m.ov.Loading
	return func() tea.Msg {
		if w == nil {
			nw, ok := bintapp.ResolveDataWatch(ctx, dir)
			if !ok {
				return dataSigMsg{}
			}
			w = &nw
		}
		return dataSigMsg{sig: w.Signature(), watch: w, ok: true, duringLoad: busy}
	}
}

// dataCaptured: có thứ đang nhận phím hoặc giữ dữ liệu cần ổn định (form, hộp thoại, ô lọc, bảng lệnh, trợ giúp,
// khởi tạo); không tự nạp lại lúc đó, thay đổi được hoãn tới lượt thăm dò sau khi đóng.
func (m *Model) dataCaptured() bool {
	if m.notes.dlg != nil || m.exitDlg != nil || m.exiting || m.prompt != nil || m.pal != nil || m.showHelp || m.holdInit || m.needsInit() {
		return true
	}
	sc := m.screens[m.Screen()]
	return sc != nil && sc.Capturing()
}

func (m *Model) onDataSig(msg dataSigMsg) (tea.Model, tea.Cmd) {
	m.dataLoading = false
	next := m.startDataPoll()
	if !msg.ok {
		return m, next
	}
	m.dataWatch = msg.watch
	if !m.dataSigSet {
		m.dataSig, m.dataSigSet = msg.sig, true // lần đầu chỉ ghi nền
		return m, next
	}
	// Chính lần nạp Tổng quan ghi vào dữ liệu được theo dõi (mở SQLite tạo rồi xoá state.sqlite-wal, nạp spool): mẫu lấy
	// trong lúc nạp thấy trạng thái tạm, so với nền sẽ báo đổi và kích lần nạp kế — vòng lặp không dứt khi lần nạp chậm
	// (kiểm thử -race). Bỏ qua các mẫu đó và lấy nền mới từ mẫu đầu tiên sau khi nạp xong; thay đổi từ ngoài đúng lúc
	// đang nạp vì thế chỉ được thấy ở lần đổi kế tiếp.
	if msg.duringLoad || m.ov.Loading {
		m.dataRebase = true
		return m, next
	}
	if m.dataRebase {
		m.dataRebase, m.dataSig = false, msg.sig
		return m, next
	}
	if msg.sig != m.dataSig {
		m.dataSig, m.dataPending = msg.sig, true
	}
	if !m.dataPending || m.dataCaptured() {
		return m, next
	}
	m.dataPending = false
	return m, tea.Batch(m.reloadData(), next)
}

// reloadData nạp lại khi dữ liệu đổi: Tổng quan và màn đang hiện nạp mềm, các màn chuyên biệt khác chỉ bị đánh dấu cũ
// (nạp khi người dùng tới tab).
func (m *Model) reloadData() tea.Cmd {
	cmds := []tea.Cmd{m.loadMode(true)}
	cur := m.Screen()
	for s, sc := range m.screens {
		r, ok := sc.(ui.Refresher)
		if !ok {
			continue
		}
		if s == cur {
			cmds = append(cmds, r.Refresh())
		} else {
			r.MarkStale()
		}
	}
	cmds = append(cmds, m.tickIfBusy())
	return tea.Batch(cmds...)
}

// Init nạp dữ liệu ban đầu.
func (m *Model) Init() tea.Cmd {
	if m.layoutReady {
		return m.load()
	}
	return tea.Batch(m.load(), m.loadLayout())
}

// Update xử lý thông điệp.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	mm, cmd := m.update(msg)
	if _, key := msg.(tea.KeyPressMsg); !key && m.palFind {
		m.refreshFind() // dữ liệu của các màn vừa về: bổ sung dòng cho hộp tìm
	}
	m.harvestLedgerWarnings()
	m.harvestNotices()
	return mm, cmd
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft && m.splitPress(tea.Mouse(msg)) {
			return m, nil // nhấn trúng đường chia: kéo đổi tỉ lệ thay cho chọn chữ
		}
		m.selPress(tea.Mouse(msg)) // chỉ ghi điểm neo: hành động của vùng bấm chạy khi thả (selRelease) nếu không kéo
		return m, nil
	case tea.MouseMotionMsg:
		if m.splitMotion(tea.Mouse(msg)) {
			return m, nil
		}
		m.selMotion(tea.Mouse(msg))
		return m, nil
	case tea.MouseReleaseMsg:
		if cmd, ok := m.splitRelease(tea.Mouse(msg)); ok {
			return m, cmd
		}
		return m, m.selRelease(tea.Mouse(msg))
	case layoutLoadedMsg:
		m.applyLayout(msg.layout)
		return m, nil
	case layoutSaveMsg:
		return m, m.saveLayout(msg.seq)
	case layoutSavedMsg:
		if msg.err != nil {
			m.setToast(m.cat.T("ui.layout.save_err", i18n.A{"err": msg.err.Error()}), true)
		}
		return m, nil
	case tea.PasteMsg:
		m.clearSelection()
		return m.paste(msg.Content)
	case clipMsg:
		return m.onClip(msg)
	case tea.MouseWheelMsg:
		m.clearSelection() // vùng đã chép nằm ở toạ độ cũ: cuộn làm nó lệch
		return m.wheel(tea.Mouse(msg))
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.clearSelection() // đổi kích thước: toạ độ vùng đã chép không còn đúng
	case spinner.TickMsg:
		if !m.busy() { // hết việc: dừng chuỗi tick
			m.ticking = false
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case overview.DoctorMsg:
		m.ov, _ = m.ov.Update(msg)
		return m, nil
	case overview.LoadedMsg:
		msg.Data.Binary, msg.Data.Revision = m.opts.Version, bintapp.SelfRevision()
		m.ov, _ = m.ov.Update(msg)
		if msg.Err == nil && msg.Data.NotInit {
			m.initDone = false // kết quả đáng tin gần nhất là chưa init: lần nạp lỗi sau đó không được che nó
		}
		if msg.Err == nil && msg.Data.NotInit {
			m.dataWatch, m.dataSigSet = nil, false // chưa init: dựng lại watch khi có .bint
		}
		if m.reloadPending {
			m.reloadPending = false
			return m, m.loadMode(m.reloadSoft)
		}
		return m, m.startDataPoll()
	case dataTickMsg:
		m.dataTicking = false
		return m, m.sampleData()
	case dataSigMsg:
		return m.onDataSig(msg)
	case activeStepsMsg:
		m.active, m.activeLoading = msg.steps, false
		if msg.watch != nil {
			m.activeWatch = msg.watch
		}
		if m.opts.ActivePoll > 0 && !m.activeTicking {
			m.activeTicking = true
			return m, tea.Tick(m.opts.ActivePoll, func(time.Time) tea.Msg { return activeTickMsg{} })
		}
	case activeTickMsg:
		m.activeTicking = false
		return m, m.loadActive(false)
	case initwizard.ContinueMsg:
		m.holdInit = false
		m.wiz.Reset()
		if msg.Nav != nil {
			return m.Update(*msg.Nav)
		}
		m.tab = 0
		return m, nil
	case initwizard.RecheckMsg:
		return m, m.load()
	case overview.NavMsg:
		if msg.ConfigEdit && !m.actionAllowed("system.config_edit") {
			return m, nil // chế độ chỉ đọc hoặc khoá: không chuyển màn, màn gửi đã báo lý do
		}
		m.goTo(msg.Screen)
		switch {
		case msg.Screen == actions.Knowledge && msg.KnowledgeSub != "":
			return m, tea.Batch(m.kn.EnterSub(msg.KnowledgeSub), m.startTick())
		case msg.Screen == actions.Knowledge && msg.Filter != nil:
			return m, tea.Batch(m.kn.Focus(*msg.Filter), m.startTick())
		case msg.Screen == actions.Tasks && msg.TaskID != "":
			return m, tea.Batch(m.tk.Focus(msg.TaskID), m.startTick())
		case msg.Screen == actions.Eval && msg.EvalCaseID != "":
			return m, tea.Batch(m.el.Focus(msg.EvalCaseID), m.startTick())
		case msg.Screen == actions.Evolve && msg.ProposalID != "":
			return m, tea.Batch(m.ev.Focus(msg.ProposalID), m.startTick())
		case msg.Screen == actions.System && msg.ComponentID != "":
			return m, tea.Batch(m.sy.Focus(msg.ComponentKind, msg.ComponentID), m.startTick())
		case msg.Screen == actions.System && msg.ConfigEdit:
			return m, tea.Batch(m.sy.Command("system.config_edit"), m.startTick())
		case msg.Screen == actions.System && msg.ConfigUpgrade:
			return m, tea.Batch(m.sy.OpenConfigUpgrade(), m.startTick())
		case msg.Screen == actions.System && msg.SystemTab != "":
			return m, tea.Batch(m.sy.OpenTab(msg.SystemTab), m.startTick())
		}
		return m, m.ensure()
	case actions.Targeted:
		if sc := m.screens[msg.Target()]; sc != nil {
			return m, tea.Batch(sc.Update(msg), m.tickIfBusy())
		}
	case actions.LanguageMsg:
		m.setLanguage(msg)
	case actions.DeniedMsg:
		return m, tea.Batch(m.toScreenOf(msg.ID, msg), m.tickIfBusy())
	case actions.RequestMsg:
		return m.request(msg)
	case actions.CancelMsg:
		if j := m.jobs.Running(msg.ID); j != nil && j.Cancel != nil {
			j.Cancel()
			if p := m.prompt; p != nil && p.job == j.ID { // câu hỏi của việc bị huỷ: trả "không" và đọc tiếp để nhận DoneMsg
				p.reply <- jobs.Answer{}
				m.prompt = nil
				return m, m.pump()
			}
		}
	case actions.ExecMsg:
		id, opts := msg.ID, msg.Opts
		return m, tea.ExecProcess(msg.Cmd, func(err error) tea.Msg { return actions.ResultMsg{ID: id, Opts: opts, Err: err} })
	case actions.ResultMsg:
		if msg.ID == "init.run" && msg.Err == nil {
			m.initDone = true
			m.holdInit = true // giữ báo cáo đến khi người dùng bấm tiếp (init.continue)
		}
		cmd := m.onResult(msg)
		if m.exiting && m.jobs.Len() == 0 {
			m.quitting = true
			return m, tea.Quit
		}
		return m, cmd
	case jobs.PromptMsg:
		return m.onPrompt(msg)
	case jobs.LogMsg:
		m.pumping = false
		if j := m.jobs.Get(msg.Job); j != nil {
			j.Log.Add(msg.Line)
		}
		return m, m.pump()
	case jobs.DataMsg:
		m.pumping = false
		var cmd tea.Cmd
		if j := m.jobs.Get(msg.Job); j != nil {
			cmd = m.toScreenOf(j.Action, msg)
		}
		return m, tea.Batch(cmd, m.pump())
	case jobs.StepMsg:
		m.pumping = false
		return m, tea.Batch(m.onStep(msg), m.pump())
	case jobs.TailMsg:
		if j := m.jobs.Get(msg.Job); j != nil && j.Tailing() {
			j.ReadTail()
			return m, jobs.TailTick(j.ID, m.tailEvery())
		}
	case jobs.DoneMsg:
		return m.onJobDone(msg)
	case exitTimeoutMsg:
		m.quitting = true
		return m, tea.Quit
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

// tickIfBusy bắt đầu chuỗi tick của vòng quay nếu có việc nền.
func (m *Model) tickIfBusy() tea.Cmd {
	if m.busy() {
		return m.startTick()
	}
	return nil
}

// toScreenOf chuyển thông điệp cho màn hình sở hữu hành động id (theo sổ hành động).
func (m *Model) toScreenOf(id string, msg tea.Msg) tea.Cmd {
	if a, ok := m.reg.ByID(id); ok {
		if sc := m.screens[a.Screen]; sc != nil {
			return sc.Update(msg)
		}
	}
	return nil
}

// request xử lý yêu cầu chạy hành động có tham số của một màn hình: tra sổ (chế độ chỉ đọc ẩn, phiên agent
// khoá) rồi mới chạy Action.Job (nền, có log và câu hỏi) hoặc Action.Do trong tea.Cmd, với context huỷ được.
// Sổ là nơi duy nhất quyết định (mục 7.4).
func (m *Model) request(r actions.RequestMsg) (tea.Model, tea.Cmd) {
	if r.ID == "system.restart" { // giao diện thuần: mô hình gốc thoát Bubble Tea, tui.Run exec lại sau đó
		o, _ := r.Opts.(actions.RestartOptions)
		return m.restart(o.Path)
	}
	a, ok := m.reg.ByID(r.ID)
	if !ok || (a.Do == nil && a.Job == nil) {
		return m, nil
	}
	switch a.Check(m.mode()) {
	case actions.Hidden:
		return m, m.toScreenOf(r.ID, actions.DeniedMsg{ID: r.ID})
	case actions.Locked:
		return m, m.toScreenOf(r.ID, actions.DeniedMsg{ID: r.ID, Locked: true})
	}
	if m.exiting || m.jobs.Running(r.ID) != nil {
		return m, nil
	}
	if a.Job != nil {
		return m.startJob(a, r.Opts)
	}
	ctx, cancel := context.WithCancel(m.ctx)
	label := ""
	if a.JobInfo != nil {
		label = a.JobInfo(r.Opts).Label
	}
	m.jobs.Add(jobs.Job{Action: r.ID, Kind: jobs.KindAction, Label: label, Cancel: cancel, Opts: r.Opts})
	env, do, opts := m.env(), a.Do, r.Opts
	return m, tea.Batch(func() tea.Msg { return do(ctx, env, opts) }, m.startTick())
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
	m.clearSelection()
	if s == "ctrl+v" && m.pasteTarget() {
		m.setToast("", false)
		return m, m.readClipboard()
	}
	if s != "!" { // "!" mở hộp Thông báo: giữ thông báo hiện tại để hộp ghi nhận đủ
		m.setToast("", false)
	}
	switch {
	case m.notes.dlg != nil:
		return m.notesKey(msg)
	case m.exiting:
		if s == "ctrl+c" { // đã chọn thoát và đang chờ việc kết thúc: Ctrl+C lần nữa thoát ngay
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil
	case m.exitDlg != nil:
		return m.exitKey(msg)
	case m.prompt != nil:
		return m.promptKey(msg)
	case m.pal != nil:
		return m.paletteKey(msg)
	case m.showHelp:
		return m.helpKey(s)
	}
	if sc := m.screens[m.Screen()]; sc != nil {
		switch {
		case sc.Capturing():
			cmd, _ := sc.Key(msg)
			return m, tea.Batch(cmd, m.tickIfBusy())
		case (s == "ctrl+c" || s == "esc") && sc.WantsInterrupt():
			cmd, _ := sc.Key(msg)
			return m, cmd
		case !globalKey(s):
			if cmd, ok := sc.Key(msg); ok {
				return m, tea.Batch(cmd, m.tickIfBusy())
			}
		}
	}
	switch s {
	case "<", ">", "z", "=":
		if cmd, ok := m.splitKey(s); ok {
			return m, cmd
		}
	case "ctrl+c", "q":
		return m.run("app.quit")
	case "?":
		return m.run("app.help")
	case ":", "ctrl+k":
		return m.run("app.palette")
	case "ctrl+f":
		return m.run("app.find")
	case "L":
		return m.run("app.language")
	case "!":
		return m.run("app.notifications")
	case "]", "tab", "right":
		return m.run("app.next_tab")
	case "[", "shift+tab", "left":
		return m.run("app.prev_tab")
	case "1", "2", "3", "4", "5", "6", "7", "8":
		return m.run(actions.NavID(actions.Screens[int(s[0]-'1')]))
	}
	// Phím riêng của màn hình hiện tại: tra sổ hành động theo phím, rồi chuyển cho màn hình. Màn Tri thức
	// tự xử lý phím của nó ở trên (phân biệt hoa/thường: a ≠ A).
	for _, a := range m.reg.For(m.Screen(), m.mode()) {
		if m.screens[m.Screen()] != nil {
			break // màn hình chuyên biệt tự xử lý phím của nó (phân biệt hoa/thường)
		}
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

// globalKey: phím toàn cục mà màn hình không được chiếm (điều hướng tab, thoát, trợ giúp, bảng lệnh, ngôn ngữ).
func globalKey(s string) bool {
	switch s {
	case "q", "ctrl+c", "?", ":", "ctrl+k", "L", "!", "[", "]", "left", "right", "shift+tab", "1", "2", "3", "4", "5", "6", "7", "8",
		"<", ">", "z", "=", "ctrl+f":
		return true
	}
	return false
}

func (m *Model) paletteKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch s := msg.String(); s {
	case "esc", "ctrl+c":
		m.pal, m.palFind = nil, false
	case "enter":
		it, ok := m.pal.Selected()
		find := m.palFind
		m.pal, m.palFind = nil, false
		if ok && find {
			return m.findJump(it.ID)
		}
		if ok {
			if it.Locked != "" {
				m.setToast(it.Locked, true)
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
			m.setToast(m.cat.T("ui.locked.agent"), true)
			return m, nil
		}
	}
	if ok && !m.applicable(a) {
		return m, nil // hành động của màn khác hoặc không hợp với trạng thái hiện tại (vd. init khi đã init)
	}
	switch id {
	case "app.quit":
		if m.jobs.Len() > 0 || m.wiz.Running() {
			// Còn việc nền (workflow, kiểm chứng, khởi tạo...): hộp thoại đầy đủ, không thoát ngay kẻo bỏ dở việc.
			m.openExitDialog()
			return m, nil
		}
		m.quitting = true
		return m, tea.Quit
	case "app.help":
		m.showHelp, m.helpTop = true, 0
	case "app.palette":
		p := palette.New(m.paletteItems())
		m.pal, m.palFind = &p, false
	case "app.find":
		return m, m.openFind()
	case "app.language":
		m.cycleLanguage()
	case "app.mouse":
		m.mouseOff = !m.mouseOff
		if m.mouseOff {
			m.setToast(m.cat.T("ui.mouse.off"), false)
		} else {
			m.setToast(m.cat.T("ui.mouse.on"), false)
		}
	case "app.notifications":
		m.openNotices()
	case "app.split_grow", "app.split_shrink", "app.split_zoom", "app.split_reset":
		key := map[string]string{"app.split_grow": ">", "app.split_shrink": "<", "app.split_zoom": "z", "app.split_reset": "="}[id]
		cmd, _ := m.splitKey(key)
		return m, cmd
	case "app.next_tab":
		m.tab = (m.tab + 1) % len(actions.Screens)
		return m, m.ensure()
	case "app.prev_tab":
		m.tab = (m.tab - 1 + len(actions.Screens)) % len(actions.Screens)
		return m, m.ensure()
	case "overview.refresh", "overview.doctor", "init.recheck":
		return m, m.load()
	case "init.continue":
		m.holdInit = false
		m.wiz.Reset()
	default:
		switch {
		case strings.HasPrefix(id, "nav."):
			m.goTo(actions.Screen(strings.TrimPrefix(id, "nav.")))
			return m, m.ensure()
		case ok && m.screens[a.Screen] != nil: // chọn từ bảng lệnh: tới màn hình chuyên biệt rồi thực hiện
			m.goTo(a.Screen)
			cmd := m.screens[a.Screen].Command(id)
			return m, tea.Batch(cmd, m.ensure(), m.tickIfBusy())
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
	m.applyLanguage(next)
}

// rw dựng bộ viết lại thông điệp của mô hình: danh mục chuỗi hiện tại, sổ hành động và chế độ chỉ đọc của chính mô hình này.
func (m *Model) rw() ui.Rewriter {
	return ui.Rewriter{Cat: m.cat, Reg: m.reg, ReadOnly: m.opts.ReadOnly}
}

// setToast là chỗ DUY NHẤT gán toast của mô hình gốc: văn bản và cờ lỗi đi cùng nhau để một toast không thừa hưởng cờ của toast
// trước (xoá = setToast("", false)). Test quét nguồn (TestToastOnlyAssignedInSetToast) không cho gán m.toast ở nơi khác.
func (m *Model) setToast(text string, isErr bool) { m.toast, m.toastErr = text, isErr }

// applyLanguage nạp danh mục chuỗi của lang cho mọi màn hình.
func (m *Model) applyLanguage(lang string) {
	m.cat = i18n.MustLoad(lang)
	for _, sc := range m.screens {
		sc.SetEnv(m.th, m.rw())
	}
}

// setLanguage đổi ngôn ngữ ngay trong phiên sau khi ghi ui.language: vi/en dùng thẳng lựa chọn của người dùng,
// auto thì chọn lại theo BINT_LANG, LC_ALL/LANG (mục 7.1).
// setLanguage chọn lại ngôn ngữ giao diện theo đúng thứ tự đã công bố: --lang > BINT_LANG > ui.language hiệu lực (msg.Lang, đã
// ghép lớp) > LC_ALL/LC_MESSAGES/LANG > vi. Khi kết quả khác giá trị người dùng vừa chọn (msg.Chosen) vì một bậc cao hơn che nó thì
// báo bằng toast nêu nguồn che.
func (m *Model) setLanguage(msg actions.LanguageMsg) {
	getenv := m.opts.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	cfg := msg.Lang
	if cfg == "" {
		cfg = "auto"
	}
	lang, from := i18n.ResolveFrom(m.opts.LangFlag, getenv("BINT_LANG"), cfg, getenv("LC_ALL"), getenv("LC_MESSAGES"), getenv("LANG"))
	if i18n.Supported(lang) {
		m.applyLanguage(lang)
	}
	if msg.Chosen == "" || msg.Chosen == "auto" || lang == msg.Chosen {
		return
	}
	src := ""
	switch {
	case from == i18n.FromFlag:
		src = "--lang"
	case from == i18n.FromEnv:
		src = "BINT_LANG"
	case msg.Layer != "":
		src = filepath.ToSlash(filepath.Join(config.DirName, config.LocalFileName))
	}
	if src != "" {
		m.setToast(m.cat.T("sy.lang.shadowed", i18n.A{"chosen": msg.Chosen, "used": lang, "source": src}), true)
	}
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
	case "init.run":
		return m.needsInit() && !m.holdInit && !m.wiz.Running()
	case "init.plan":
		return false // bước nội bộ của trình hướng dẫn
	case "init.continue":
		return m.holdInit && !m.ov.Loading && !m.wiz.Running()
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
	sc := m.screens[m.Screen()]
	if m.prompt != nil || m.exitDlg != nil || m.exiting {
		return nil // hộp thoại có nút riêng
	}
	if sc != nil && sc.Capturing() {
		return []footer.Hint{{Key: "Esc", Label: m.cat.T("kn.hint.leave"), Pinned: true}}
	}
	var screen, global, pinned []footer.Hint
	for _, a := range m.reg.For(m.Screen(), m.mode()) {
		if !a.Footer || a.Check(m.mode()) != actions.Available || !m.applicable(a) {
			continue
		}
		if sc != nil && sc.NotList() && !a.Pinned && a.Screen != actions.Global {
			continue // thẻ phụ có phím riêng ở viền dưới khung kết quả
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
	v := tea.NewView(m.highlight(m.Render()))
	v.AltScreen = true
	if !m.mouseOff {
		v.MouseMode = tea.MouseModeCellMotion // chuột bật mặc định (mục 4.3.7); app.mouse tắt/bật trong phiên
	}
	return v
}

// Render trả nội dung đã vẽ (mỗi dòng rộng đúng m.w ô khi đủ chỗ). Vẽ xong gỡ mọi dấu vùng chuột khỏi đầu ra và giữ danh
// sách vùng cho sự kiện chuột kế tiếp (zone.Scan).
func (m *Model) Render() string {
	zone.Begin()
	raw := m.render()
	zone.End()
	out, zs := zone.Scan(raw)
	m.zones = m.filterModal(zs)
	return m.th.NoColor(out) // NO_COLOR: nội dung ngoài (log, tệp, diff) mang màu riêng cũng bị bỏ màu
}

// newEnv dựng ngữ cảnh vẽ cho vùng w × h.
func (m *Model) newEnv(w, h int) ui.Env {
	return ui.Env{Th: m.th, Cat: m.cat, RW: m.rw(), W: w, H: h, Spin: m.spin.View(), Allowed: m.actionAllowed}
}

func (m *Model) render() string {
	w, h := m.w, m.h
	if w <= 0 || h <= 0 {
		return ""
	}
	if w < MinW || h < MinH {
		return m.tooSmall(w, h)
	}
	env := m.newEnv(w, h)

	// Bố cục: một dòng thanh tab, vùng nội dung không khung bao (mỗi pane tự có viền), một dòng chân phím.
	header := m.header(env)
	bw, bh := m.bodySize()
	body := m.screenBody(env.WithSize(bw, bh))
	if m.pal != nil || m.showHelp || m.notes.dlg != nil {
		// Hộp phủ đang mở: che nền để chữ của màn bên dưới không lộ (bị cắt ngang) ở hai bên và dưới hộp.
		body = ""
	}
	lines := append([]string{header}, text.Block(body, bw, bh)...)
	lines = append(lines, m.footerLine(w))

	switch {
	case m.notes.dlg != nil:
		bw, bh := min(w-4, 90), min(h-4, 26)
		box := m.notes.dlg.View(m.th, bw, bh)
		lines = text.Overlay(lines, box, (w-bw)/2, max((h-len(box))/2, 1))
	case m.pal != nil:
		bw, bh := min(w-4, 76), min(h-6, 16)
		title, empty := m.cat.T("palette.title"), m.cat.T("palette.empty")
		if m.palFind {
			bw, bh = min(w-4, 110), min(h-4, 24) // hộp tìm rộng hơn: tiêu đề tri thức/tác vụ dài
			title, empty = m.cat.T("find.title"), m.cat.T("find.empty")
		}
		box := strings.Split(m.pal.View(m.th, bw, bh, title, empty), "\n")
		lines = text.Overlay(lines, markModal(box), (w-bw)/2, 2)
	case m.showHelp:
		bw, bh := m.helpSize()
		box := strings.Split(help.Render(m.th, bw, bh, m.cat.T("help.title"), m.helpGroups(), m.helpTop, m.helpPos), "\n")
		lines = text.Overlay(lines, markModal(box), (w-bw)/2, 2)
	default:
		lines, _ = m.overlayDialog(lines, w, h)
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

// toastLabel dựng thông báo ở bên phải dòng chân phím (chừa toastHintRoom ô cho các phím). Dài quá chỗ thì rút gọn bằng "…" kèm "[!]":
// phím ! hoặc bấm vào đó mở hộp Thông báo hiện đủ nội dung.
func (m *Model) toastLabel() string {
	if m.toast == "" {
		return ""
	}
	return ui.ToastLabel(m.th, m.rw().Rewrite(m.toast), m.toastErr, m.w-2-toastHintRoom)
}

// toastHintRoom là số ô tối thiểu dành cho chân phím khi có thông báo.
const toastHintRoom = 24

func (m *Model) screenBody(env ui.Env) string {
	switch s := m.Screen(); s {
	case actions.Init:
		var loadErr error
		if m.holdInit && !m.ov.Loading { // đang nạp lại thì ov.Err còn là lỗi của lần trước
			loadErr = m.ov.Err // khởi tạo xong mà nạp lại lỗi: nói rõ, đừng im lặng
		}
		m.wiz.SetLoadErr(loadErr)
		return m.wiz.View(env)
	case actions.Overview:
		return m.ov.View(env)
	case actions.Knowledge, actions.Tasks, actions.Runs, actions.Eval, actions.Evolve, actions.System, actions.MCP:
		return m.screens[s].View(env)
	default:
		return ""
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
	if m.ov.Loaded && m.ov.Data.Pending != nil {
		items[2].Badge = len(m.ov.Data.Pending.BlockedTasks) // Tác vụ chờ người
	}
	var flags []string
	if m.opts.ReadOnly {
		flags = append(flags, m.th.S(theme.Key, "["+m.cat.T("ui.readonly")+"]"))
	}
	if m.opts.Agent {
		flags = append(flags, m.th.S(theme.Warn, "["+m.cat.T("ui.agent_session")+"]"))
	}
	if l := m.activeLabel(); l != "" {
		flags = append(flags, m.th.S(theme.Warn, l))
	}
	if n := m.jobs.Len(); n > 0 {
		flags = append(flags, m.th.S(theme.Key, m.spin.View()+" "+m.cat.N("job.running", n)))
	}
	// Phần phải: cờ trạng thái, đường dẫn repo, "bint <phiên bản>". Thiếu chỗ thì rút đường dẫn trước, rồi bỏ cờ; nhãn tab được ưu
	// tiên giữ (tabs.Render tự chuyển nhãn rút gọn khi hẹp).
	ver := m.th.S(theme.Muted, "bint "+m.opts.Version)
	pathRole, path := theme.Muted, m.opts.Dir
	if m.ov.Loaded && m.ov.Data.Status != nil && m.ov.Data.Status.LinkedWorktree {
		pathRole, path = theme.Warn, "" // worktree phụ: chữ cảnh báo thay cho đường dẫn
	}
	fl := strings.Join(flags, " ")
	// right dựng phần phải vừa room ô: đủ (cờ, đường dẫn, phiên bản) → bỏ đường dẫn → bỏ cờ → chỉ phiên bản; "" nếu không vừa.
	right := func(room int) string {
		withFlags := func(r string) string {
			if fl == "" {
				return r
			}
			return fl + " " + r
		}
		for _, cand := range []string{withFlags(ver), ver} {
			if text.Width(cand) > room {
				continue
			}
			if pw := room - text.Width(cand) - 3; pw >= 8 {
				var p string
				if path == "" {
					pre := text.Width(m.cat.T("ui.worktree", i18n.A{"main": ""}))
					p = text.Truncate(m.cat.T("ui.worktree", i18n.A{"main": text.Path(m.ov.Data.Status.MainWorktree, max(pw-pre, 8))}), pw)
				} else {
					p = text.Path(path, pw)
				}
				return m.th.S(pathRole, p) + m.th.S(theme.Muted, " · ") + cand
			}
			return cand
		}
		return ""
	}
	// Nhãn tab đầy đủ được ưu tiên: chỉ rút gọn nhãn (compact) khi phần phải không còn chỗ cho phiên bản.
	compact := w < NarrowW
	natural := tabs.Render(m.th, w, items, m.tab, compact)
	r := right(w - 2 - text.Width(natural) - 2)
	// Worktree phụ: cảnh báo quan trọng hơn nhãn tab đầy đủ — thiếu chỗ (bị rút gọn) thì rút nhãn tab.
	wtCut := path == "" && !strings.Contains(r, text.Path(m.ov.Data.Status.MainWorktree, 1<<10))
	if (r == "" || wtCut) && !compact {
		compact = true
		natural = tabs.Render(m.th, w, items, m.tab, true)
		r = right(w - 2 - text.Width(natural) - 2)
	}
	bar := tabs.Render(m.th, w-2-text.Width(r)-1, items, m.tab, compact)
	gap := max(w-2-text.Width(bar)-text.Width(r), 1)
	return text.Fit(" "+bar+strings.Repeat(" ", gap)+r+" ", w)
}

// bodySize là cỡ vùng nội dung của màn hình: trọn chiều rộng, trừ dòng thanh tab và dòng chân phím.
func (m *Model) bodySize() (w, h int) { return m.w, max(m.h-2, 0) }

// footerLine là dòng chân phím (không viền); thông báo (toast) nằm bên phải, rút gọn kèm [!] khi thiếu chỗ.
func (m *Model) footerLine(w int) string {
	toast := m.toastLabel()
	hints := footer.Line(m.th, m.Hints(), max(w-2-text.Width(toast)-1, 10))
	gap := max(w-1-text.Width(hints)-text.Width(toast), 1)
	return text.Fit(" "+hints+strings.Repeat(" ", gap)+toast, w)
}

// activeLabel là dòng trạng thái "<bước> của <tác vụ> đang chạy trên cây này" (rỗng nếu không có bước nào).
// Thao tác ghi vẫn được phép (docs/isolation-subagent-plan.md 4.5): đây chỉ là thông tin báo trước.
func (m *Model) activeLabel() string {
	switch len(m.active) {
	case 0:
		return ""
	case 1:
		a := m.active[0]
		task := a.TaskID
		if len(task) > 8 {
			task = task[:8]
		}
		return m.cat.T("ui.step_running", i18n.A{"step": a.Step, "task": task})
	}
	return m.cat.N("ui.steps_running", len(m.active))
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
		if strings.HasPrefix(a.ID, "nav.") { // 8 dòng chuyển tab gộp thành một
			if !navAdded {
				navAdded = true
				global.Rows = append(global.Rows, help.Row{Key: "1-8", Title: m.cat.T("help.nav")})
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
	global.Rows = append(global.Rows,
		help.Row{Key: "Ctrl+V", Title: m.cat.T("help.paste")},
		help.Row{Key: m.cat.T("help.key.drag"), Title: m.cat.T("help.select")},
		help.Row{Key: m.cat.T("help.key.shift_drag"), Title: m.cat.T("help.select_native")})
	// Ngữ cảnh hiện tại (thẻ phụ, giới thiệu, phím của thẻ) đứng đầu, rồi nhóm của màn hình đang xem, rồi toàn cục, rồi các màn hình khác.
	var groups []help.Group
	cur := m.Screen()
	if g, ok := m.helpContext(); ok {
		if sg := byScreen[cur]; sg != nil && sg.Title == g.Title {
			// Màn không có thẻ phụ (Tổng quan, Khởi tạo): ngữ cảnh và hành động của màn là một nhóm, không lặp tiêu đề.
			g.Rows = append(g.Rows, sg.Rows...)
			delete(byScreen, cur)
		}
		groups = append(groups, g)
	}
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
