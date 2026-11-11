// Package initwizard là trình hướng dẫn init nhiều bước cho repository chưa `bint init` (docs/tui-plan.md mục 7.6): harness →
// hook + phạm vi → MCP → AGENTS.md → skill → Git (track | local, có giải thích hệ quả) → xem trước (app.PlanInit) → chạy
// (app.Init) → báo cáo kèm bước tiếp theo. Màn hình không tự gọi dịch vụ: nó gửi actions.RequestMsg (init.plan, init.run) và nhận
// actions.ResultMsg qua sổ hành động như các màn khác; chế độ chỉ đọc do sổ hành động quyết định (hành động ghi bị ẩn).
package initwizard

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"bint/internal/app"
	"bint/internal/detect"
	"bint/internal/i18n"
	"bint/internal/tui/actions"
	"bint/internal/tui/screens/overview"
	"bint/internal/tui/theme"
)

// Các bước hỏi (bước xem trước là bước cuối trước khi chạy).
const (
	stepHarness = iota
	stepHooks
	stepMCP
	stepAgents
	stepSkills
	stepGit
	stepPreview
	numSteps
)

var stepNames = [numSteps]string{"harness", "hooks", "mcp", "agents", "skills", "git", "preview"}

// Phạm vi hook theo thứ tự hàng của bước hook (sau hai hàng Có/Không).
var scopes = []string{"local", "project", "user"}

// Options điều khiển màn hình.
type Options struct {
	Dir      string
	ReadOnly bool
	// Detect thay được trong kiểm thử; nil = detect.Harnesses.
	Detect func() []detect.Harness
}

// ContinueMsg báo mô hình gốc rời trình hướng dẫn sau khi khởi tạo xong; Nav khác nil = đi tới màn tương ứng của bước tiếp theo.
type ContinueMsg struct{ Nav *overview.NavMsg }

// RecheckMsg yêu cầu mô hình gốc nạp lại trạng thái repository (sau khi khởi tạo thất bại, có thể đã init một phần).
type RecheckMsg struct{}

// Model là trạng thái trình hướng dẫn.
type Model struct {
	o   Options
	th  theme.Theme
	cat *i18n.Catalog

	detected []detect.Harness
	sel      []bool // harness được chọn, theo detected
	hooks    bool
	scope    string
	mcp      bool
	agents   bool
	skills   bool
	git      string

	step int
	cur  int
	msg  string // lỗi kiểm tra tại chỗ (chưa chọn harness)

	plan     *app.InitPlan
	planErr  error
	planning bool

	running bool
	done    bool
	report  *app.InitReport
	err     error
	next    int
	// LoadErr: khởi tạo xong nhưng nạp lại trạng thái repository thất bại (hiện cảnh báo dưới báo cáo).
	loadErr error

	denied bool
}

// New dựng trình hướng dẫn với lựa chọn mặc định.
func New(o Options) *Model {
	m := &Model{o: o}
	m.Reset()
	return m
}

// Reset đưa trình hướng dẫn về bước đầu với lựa chọn mặc định (phát hiện lại harness trên máy).
func (m *Model) Reset() {
	detectFn := m.o.Detect
	if detectFn == nil {
		detectFn = detect.Harnesses
	}
	m.detected = detectFn()
	m.setDefaults()
	m.step, m.cur, m.msg = stepHarness, 0, ""
	m.plan, m.planErr, m.planning = nil, nil, false
	m.running, m.done, m.report, m.err, m.next, m.loadErr, m.denied = false, false, nil, nil, 0, nil, false
}

// setDefaults đặt lựa chọn mặc định: harness có trên máy (không có thì harness đầu), hook bật phạm vi local, MCP và AGENTS.md
// tắt, skill bật, Git track.
func (m *Model) setDefaults() {
	m.sel = make([]bool, len(m.detected))
	any := false
	for i, d := range m.detected {
		if d.Present {
			m.sel[i], any = true, true
		}
	}
	if !any && len(m.sel) > 0 {
		m.sel[0] = true
	}
	m.hooks, m.scope, m.mcp, m.agents, m.skills, m.git = true, "local", false, false, true, app.GitTrack
}

// SetLoadErr ghi lỗi nạp lại trạng thái sau khi khởi tạo (mô hình gốc gọi trước khi vẽ).
func (m *Model) SetLoadErr(err error) { m.loadErr = err }

// Done cho biết đã khởi tạo xong (có báo cáo, không lỗi cứng).
func (m *Model) Done() bool { return m.done && m.err == nil }

// Running cho biết đang chạy init.
func (m *Model) Running() bool { return m.running }

// Step trả chỉ số bước hiện tại (0 = harness … 6 = xem trước); dùng trong kiểm thử.
func (m *Model) Step() int { return m.step }

// Report trả báo cáo khởi tạo (nil nếu chưa chạy xong).
func (m *Model) Report() *app.InitReport { return m.report }

// Options trả Options của init suy ra từ các lựa chọn: Hooks và GitMode luôn tường minh nên app.Init không hỏi giữa chừng.
func (m *Model) Options() app.InitFlagOptions {
	o := app.InitFlagOptions{HooksScope: m.scope, GitMode: m.git, AgentsMD: m.agents, NoSkills: !m.skills, MCP: m.mcp}
	for i, d := range m.detected {
		if m.sel[i] {
			o.Harnesses = append(o.Harnesses, d.Name)
		}
	}
	if m.hooks {
		o.Hooks = true
	} else {
		o.NoHooks = true
	}
	return o
}

// ---- ui.Screen ----

// SetEnv đặt bảng màu và danh mục chuỗi.
func (m *Model) SetEnv(th theme.Theme, cat *i18n.Catalog) { m.th, m.cat = th, cat }

func (m *Model) t(key string, a ...i18n.A) string { return m.cat.T(key, a...) }

// Ensure: không có dữ liệu để nạp.
func (m *Model) Ensure() tea.Cmd { return nil }

// Capturing: các phím chữ của trình hướng dẫn (j, k, d, b, space) không chiếm phím toàn cục (q, ?, :, 1-7).
func (m *Model) Capturing() bool { return false }

// WantsInterrupt: luôn sai — Esc không thuộc phím toàn cục nên tới thẳng Key (lùi một bước), còn Ctrl+C vẫn thoát.
func (m *Model) WantsInterrupt() bool { return false }

// Busy: đang tính kế hoạch hoặc đang chạy init.
func (m *Model) Busy() bool { return m.planning || m.running }

// NotList: trình hướng dẫn tự vẽ gợi ý phím ở cuối khung.
func (m *Model) NotList() bool { return true }

// Command thực hiện hành động theo ID (từ bảng lệnh): init.run = dùng mặc định rồi tới bước xem trước.
func (m *Model) Command(id string) tea.Cmd {
	if id == "init.run" && !m.o.ReadOnly && !m.running && !m.done {
		return m.useDefaults()
	}
	return nil
}

func (m *Model) useDefaults() tea.Cmd {
	m.setDefaults()
	m.msg = ""
	return m.gotoPreview()
}

func (m *Model) gotoPreview() tea.Cmd {
	m.step, m.cur = stepPreview, 0
	m.plan, m.planErr, m.planning = nil, nil, true
	o := m.Options()
	return func() tea.Msg { return actions.RequestMsg{ID: "init.plan", Opts: o} }
}

// Update xử lý kết quả hành động và từ chối của sổ hành động.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case actions.DeniedMsg:
		m.denied = true
		m.done = m.done || m.planning || m.running
		m.planning, m.running = false, false
	case actions.ResultMsg:
		switch msg.ID {
		case "init.plan":
			if m.step != stepPreview {
				return nil // người dùng đã lùi
			}
			m.planning = false
			m.planErr = msg.Err
			if pl, ok := msg.Value.(app.InitPlan); ok && msg.Err == nil {
				m.plan = &pl
			}
		case "init.run":
			m.running, m.done = false, true
			m.report, m.err = nil, msg.Err
			if rep, ok := msg.Value.(*app.InitReport); ok {
				m.report = rep
			}
		}
	}
	return nil
}

// ---- hàng của từng bước ----

type row struct {
	label string // khoá i18n (hoặc chữ thô khi raw)
	raw   bool
	on    bool // đang chọn
	dim   bool // mờ (phạm vi hook khi hook tắt)
	hint  string
	group int // hàng cùng nhóm là radio; -1 = hộp chọn độc lập
}

func (m *Model) rows() []row {
	yn := func(v bool) []row {
		return []row{{label: "wiz.yes", on: v}, {label: "wiz.no", on: !v}}
	}
	switch m.step {
	case stepHarness:
		var out []row
		for i, d := range m.detected {
			hint := "wiz.harness.absent"
			if d.Present {
				hint = "wiz.harness.present"
			}
			out = append(out, row{label: d.Name, raw: true, on: m.sel[i], hint: hint, group: -1})
		}
		return out
	case stepHooks:
		out := yn(m.hooks)
		for _, s := range scopes {
			out = append(out, row{label: "wiz.hooks.scope." + s, on: m.scope == s, dim: !m.hooks, group: 1})
		}
		return out
	case stepMCP:
		return yn(m.mcp)
	case stepAgents:
		return yn(m.agents)
	case stepSkills:
		return yn(m.skills)
	case stepGit:
		return []row{{label: "wiz.git.track", on: m.git == app.GitTrack}, {label: "wiz.git.local", on: m.git == app.GitLocal}}
	}
	return nil
}

// pick áp lựa chọn của hàng i (Space): hàng radio chọn hàng đó, hộp chọn độc lập đảo trạng thái. Di chuyển con trỏ không đổi lựa chọn.
func (m *Model) pick(i int) {
	switch m.step {
	case stepHarness:
		if i >= 0 && i < len(m.sel) {
			m.sel[i] = !m.sel[i]
		}
	case stepHooks:
		switch {
		case i == 0 || i == 1:
			m.hooks = i == 0
		case i-2 < len(scopes):
			m.scope = scopes[i-2]
		}
	case stepMCP:
		m.mcp = i == 0
	case stepAgents:
		m.agents = i == 0
	case stepSkills:
		m.skills = i == 0
	case stepGit:
		m.git = []string{app.GitTrack, app.GitLocal}[i]
	}
	m.msg = ""
}

// enter đặt con trỏ về hàng đang chọn của bước (lần đầu vào bước).
func (m *Model) enter(step int) {
	m.step, m.msg = step, ""
	m.cur = 0
	for i, r := range m.rows() {
		if r.on {
			m.cur = i
			break
		}
	}
}

// ---- phím ----

// Key xử lý một phím; handled = false nghĩa là màn hình không dùng phím này.
func (m *Model) Key(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if m.o.ReadOnly {
		return nil, false
	}
	s := msg.String()
	switch {
	case m.running:
		return nil, true
	case m.done:
		return m.doneKey(s)
	case s == "r": // nạp lại trạng thái repository (kiểm tra lại đã init chưa), ở mọi bước
		return func() tea.Msg { return RecheckMsg{} }, true
	case m.step == stepPreview:
		return m.previewKey(s)
	}
	switch s {
	case "enter":
		return m.forward(), true
	case "esc", "b", "backspace":
		if m.step > 0 {
			m.enter(m.step - 1)
		}
		return nil, m.step > 0 || s == "b" || s == "backspace"
	case "d":
		return m.useDefaults(), true
	case "j", "down":
		m.move(1)
		return nil, true
	case "k", "up":
		m.move(-1)
		return nil, true
	case " ", "space", "x":
		m.pick(m.cur)
		return nil, true
	}
	return nil, false
}

func (m *Model) move(d int) {
	n := len(m.rows())
	if n == 0 {
		return
	}
	m.cur = (m.cur + d + n) % n
}

// forward sang bước kế (kiểm tra bước hiện tại); bước Git sang xem trước và yêu cầu kế hoạch.
func (m *Model) forward() tea.Cmd {
	if m.step == stepHarness {
		any := false
		for _, v := range m.sel {
			any = any || v
		}
		if !any {
			m.msg = m.t("wiz.harness.none")
			return nil
		}
	}
	if m.step == stepGit {
		return m.gotoPreview()
	}
	m.enter(m.step + 1)
	return nil
}

func (m *Model) previewKey(s string) (tea.Cmd, bool) {
	switch s {
	case "esc", "b", "backspace":
		m.plan, m.planErr, m.planning = nil, nil, false
		m.enter(stepGit)
		return nil, true
	case "enter":
		if m.planning || m.planErr != nil || m.plan == nil {
			return nil, true
		}
		m.running = true
		o := m.Options()
		return func() tea.Msg { return actions.RequestMsg{ID: "init.run", Opts: o} }, true
	}
	return nil, false
}

func (m *Model) doneKey(s string) (tea.Cmd, bool) {
	if m.err != nil || m.report == nil {
		switch s {
		case "esc", "b", "backspace":
			m.done, m.err, m.report = false, nil, nil
			m.plan = nil
			return m.gotoPreview(), true
		case "r":
			return func() tea.Msg { return RecheckMsg{} }, true
		}
		return nil, false
	}
	steps := m.report.NextSteps
	switch s {
	case "j", "down":
		if len(steps) > 0 {
			m.next = (m.next + 1) % len(steps)
		}
		return nil, true
	case "k", "up":
		if len(steps) > 0 {
			m.next = (m.next + len(steps) - 1) % len(steps)
		}
		return nil, true
	case "enter":
		var nav *overview.NavMsg
		if m.next < len(steps) {
			nav = NavFor(steps[m.next])
		}
		return func() tea.Msg { return ContinueMsg{Nav: nav} }, true
	case "r":
		return func() tea.Msg { return RecheckMsg{} }, true
	}
	return nil, false
}

// NavFor ánh xạ một dòng "bước tiếp theo" của báo cáo sang màn hình tương ứng (nil = chỉ là chữ): doctor → Hệ thống › Doctor,
// runs list → Lượt chạy, knowledge review → Tri thức.
func NavFor(step string) *overview.NavMsg {
	switch {
	case strings.Contains(step, "bint doctor"):
		return &overview.NavMsg{Screen: actions.System, SystemTab: "doctor"}
	case strings.Contains(step, "bint runs list"):
		return &overview.NavMsg{Screen: actions.Runs}
	case strings.Contains(step, "bint knowledge review"):
		return &overview.NavMsg{Screen: actions.Knowledge}
	}
	return nil
}
