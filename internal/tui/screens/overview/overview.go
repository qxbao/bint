// Package overview là màn Tổng quan: trạng thái repository (status), hộp thư "Việc cần bạn", kết quả
// doctor rút gọn. Đây là màn mẫu để chủ dự án duyệt phong cách trước khi làm các màn khác.
package overview

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"bint/internal/app"
	"bint/internal/knowledge"
	"bint/internal/tui/actions"
	"bint/internal/tui/components/frame"
	"bint/internal/tui/components/logo"
	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

// Data là dữ liệu của màn Tổng quan.
type Data struct {
	Status *app.StatusReport // nil nếu chưa init
	Doctor *app.DoctorReport
	// Candidates là số mục tri thức chờ duyệt (candidate + stale).
	Candidates int
	// NotInit: repository chưa `bint init` (hiện màn khởi tạo thay cho bảng điều khiển).
	NotInit bool
}

// Inbox là một dòng của hộp thư "Việc cần bạn".
type Inbox struct {
	Level  string // "info" | "warn" | "fail"
	Key    string // khoá i18n
	Count  int
	Detail string
	Target actions.Screen
}

// Inbox dựng danh sách việc cần người xử lý từ dữ liệu hiện có.
func (d Data) Inbox() []Inbox {
	var out []Inbox
	if d.Candidates > 0 {
		out = append(out, Inbox{Level: "info", Key: "overview.inbox.knowledge", Count: d.Candidates, Target: actions.Knowledge})
	}
	if d.Status != nil && d.Status.PendingProposals > 0 {
		out = append(out, Inbox{Level: "info", Key: "overview.inbox.evolve", Count: d.Status.PendingProposals, Target: actions.Evolve})
	}
	if d.Doctor != nil {
		for _, c := range d.Doctor.Checks {
			switch c.Level {
			case app.LevelFail:
				out = append(out, Inbox{Level: "fail", Key: "overview.inbox.doctor_fail", Detail: c.Name + ": " + c.Detail, Target: actions.System})
			case app.LevelWarn:
				out = append(out, Inbox{Level: "warn", Key: "overview.inbox.doctor_warn", Detail: c.Name + ": " + c.Detail, Target: actions.System})
			}
		}
	}
	return out
}

// LoadedMsg là kết quả nạp dữ liệu.
type LoadedMsg struct {
	Data Data
	Err  error
}

// Load đọc dữ liệu của Tổng quan ở repository chứa dir. Repository chưa init cho Data.NotInit = true
// (không phải lỗi). Chạy ngoài vòng Update (trong tea.Cmd).
func Load(ctx context.Context, dir string) (Data, error) { return load(ctx, dir, true) }

// LoadNoIngest như Load nhưng không nạp spool vào cơ sở dữ liệu (chế độ chỉ đọc, mục 7.4).
func LoadNoIngest(ctx context.Context, dir string) (Data, error) { return load(ctx, dir, false) }

func load(ctx context.Context, dir string, ingest bool) (Data, error) {
	var d Data
	d.Doctor = app.Doctor(ctx, dir)
	status := app.Status
	if !ingest {
		status = app.StatusNoIngest
	}
	st, err := status(ctx, dir)
	switch {
	case errors.Is(err, app.ErrNotInitialized):
		d.NotInit = true
		return d, nil
	case err != nil:
		return d, err
	}
	d.Status = st
	if k, err := app.OpenKnowledge(ctx, dir); err == nil {
		if items, _, err := k.Items(); err == nil {
			for _, it := range items {
				if it.Status == knowledge.StatusCandidate || it.Status == knowledge.StatusStale {
					d.Candidates++
				}
			}
		}
	}
	return d, nil
}

// LoadCmd là tea.Cmd nạp dữ liệu.
func LoadCmd(ctx context.Context, dir string) tea.Cmd {
	return func() tea.Msg {
		d, err := Load(ctx, dir)
		return LoadedMsg{Data: d, Err: err}
	}
}

// NavMsg yêu cầu chuyển sang một tab.
type NavMsg struct{ Screen actions.Screen }

// Model là trạng thái màn Tổng quan.
type Model struct {
	Data    Data
	Loaded  bool
	Loading bool
	Err     error
	Cursor  int // dòng đang chọn trong hộp thư
}

// Update xử lý thông điệp của màn hình.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case LoadedMsg:
		m.Loading, m.Err = false, msg.Err
		if msg.Err == nil {
			m.Data, m.Loaded = msg.Data, true
			m.Cursor = min(m.Cursor, max(len(m.Data.Inbox())-1, 0))
		}
	case tea.KeyPressMsg:
		n := len(m.Data.Inbox())
		switch msg.String() {
		case "j", "down":
			if n > 0 {
				m.Cursor = (m.Cursor + 1) % n
			}
		case "k", "up":
			if n > 0 {
				m.Cursor = (m.Cursor - 1 + n) % n
			}
		case "enter":
			if in := m.Data.Inbox(); m.Cursor < len(in) {
				t := in[m.Cursor].Target
				return m, func() tea.Msg { return NavMsg{Screen: t} }
			}
		}
	}
	return m, nil
}

// View vẽ nội dung màn hình trong vùng env.W × env.H (không gồm khung ngoài).
func (m Model) View(env ui.Env) string {
	th := env.Th
	switch {
	case m.Err != nil:
		return strings.Join([]string{
			"", th.S(theme.Err, "✗ ") + env.T("overview.error"),
			"  " + text.Truncate(m.Err.Error(), max(env.W-4, 10)),
			"", th.S(theme.Muted, env.T("overview.error.hint")),
		}, "\n")
	case !m.Loaded:
		return "\n  " + th.S(theme.Accent, env.Spin) + " " + th.S(theme.Muted, env.T("ui.loading"))
	}
	var lines []string
	add := func(l ...string) { lines = append(lines, l...) }
	center := func(l []string) []string {
		out := make([]string, len(l))
		for i, s := range l {
			out[i] = text.Center(s, env.W)
		}
		return out
	}

	// Logo (nếu đủ chỗ) và khẩu hiệu.
	add(center(logo.Render(th, logo.Fits(env.W+4, env.H+5)))...)
	add(text.Center(th.S(theme.Muted, env.T("app.tagline")), env.W))
	add("")

	// Hộp thông tin đặt giữa, rộng theo nội dung.
	add(center(m.infoBox(env))...)
	add("")

	// Hộp thư "Việc cần bạn".
	inbox := m.Data.Inbox()
	head := th.Bold(env.T("overview.inbox.title"))
	if len(inbox) > 0 {
		head += th.S(theme.Key, fmt.Sprintf(" (%d)", len(inbox)))
	}
	add(" " + head)
	if len(inbox) == 0 {
		add("  " + th.S(theme.OK, "✓ ") + th.S(theme.Muted, env.T("overview.inbox.empty")))
	}
	for i, in := range inbox {
		icon, role := "◌", theme.Accent
		switch in.Level {
		case "warn":
			icon, role = "!", theme.Warn
		case "fail":
			icon, role = "✗", theme.Err
		}
		body := env.N(in.Key, in.Count, map[string]any{"detail": in.Detail})
		right := "→ " + env.T("tab."+string(in.Target))
		avail := env.W - 4 - text.Width(right) - 2
		body = text.Truncate(body, max(avail, 8))
		gap := strings.Repeat(" ", max(env.W-4-text.Width(body)-text.Width(right), 1))
		if i == m.Cursor {
			add("▸ " + th.S(role, icon) + " " + th.Selected(body) + gap + th.S(theme.Muted, right))
		} else {
			add("  " + th.S(role, icon) + " " + body + gap + th.S(theme.Muted, right))
		}
	}
	add("")

	// Doctor rút gọn: chỉ số đếm. Mọi cảnh báo/lỗi doctor đã nằm trong hộp thư ở trên (Inbox), nên không
	// liệt kê lại chi tiết ở đây.
	add(" " + th.Bold(env.T("overview.doctor.title")) + "  " + m.doctorSummary(th))
	if len(lines) > env.H {
		lines = lines[:env.H]
	}
	return strings.Join(centerVertically(lines, env.H), "\n")
}

// centerVertically đặt khối lines vào giữa chiều cao h khi còn thừa nhiều chỗ (từ minSpare dòng), để
// cửa sổ cao không dồn hết nội dung lên trên; thừa ít thì giữ sát trên như trước.
func centerVertically(lines []string, h int) []string {
	const minSpare = 4
	spare := h - len(lines)
	if spare < minSpare {
		return lines
	}
	return append(make([]string, spare/2), lines...)
}

func (m Model) doctorSummary(th theme.Theme) string {
	var ok, warn, fail int
	if m.Data.Doctor != nil {
		for _, c := range m.Data.Doctor.Checks {
			switch c.Level {
			case app.LevelOK, app.LevelInfo:
				ok++
			case app.LevelWarn:
				warn++
			case app.LevelFail:
				fail++
			}
		}
	}
	return th.S(theme.OK, fmt.Sprintf("✓ %d", ok)) + "  " + th.S(theme.Warn, fmt.Sprintf("! %d", warn)) + "  " + th.S(theme.Err, fmt.Sprintf("✗ %d", fail))
}

// infoBox là hộp thông tin repository, rộng vừa nội dung.
func (m Model) infoBox(env ui.Env) []string {
	th := env.Th
	st := m.Data.Status
	if st == nil {
		return nil
	}
	runs := fmt.Sprintf("%d", st.TotalRuns)
	if parts := runParts(env, st.Runs); len(parts) > 0 {
		runs += "  " + th.S(theme.Muted, strings.Join(parts, " · "))
	}
	rows := [][2]string{
		{env.T("overview.repo"), ""},
		{env.T("overview.state"), ""},
		{env.T("overview.harness"), strings.Join(st.Harnesses, ", ")},
		{env.T("overview.schema"), fmt.Sprintf("v%d", st.Schema)},
		{env.T("overview.runs"), runs},
	}
	lw := 0
	for _, r := range rows {
		lw = max(lw, text.Width(r[0]))
	}
	// Đường dẫn rút gọn (~, bỏ phần đầu bằng …) để hộp không tràn ngang: khung rộng tối đa W-2, trừ
	// viền + đệm (4) và cột nhãn cùng hai ô đệm.
	pathW := max(env.W-2-4-lw-2, 12)
	rows[0][1] = text.Path(st.Repo, pathW)
	rows[1][1] = text.Path(st.StateDir, pathW)
	var body []string
	for _, r := range rows {
		body = append(body, th.S(theme.Accent, text.Pad(r[0], lw))+"  "+r[1])
	}
	cw := text.MaxWidth(body)
	w := min(cw+4, max(env.W-2, 20))
	h := len(body) + 2
	return strings.Split(frame.Box{Title: env.T("overview.info.title"), TitleAt: frame.Left}.Render(th, w, h, strings.Join(body, "\n")), "\n")
}

// runStatusOrder là thứ tự hiển thị các trạng thái lượt chạy đã biết; trạng thái lạ xếp sau, theo tên.
var runStatusOrder = []string{"completed", "failed", "cancelled", "interrupted", "running"}

// runParts dịch số lượt chạy theo trạng thái thành các cụm ngắn ("55 xong", "5 lỗi"...); bỏ trạng thái
// có số 0. Tên trạng thái lạ (chưa có trong danh mục) hiện nguyên dạng.
func runParts(env ui.Env, runs map[string]int) []string {
	seen := map[string]bool{}
	var keys []string
	for _, k := range runStatusOrder {
		seen[k] = true
		keys = append(keys, k)
	}
	var extra []string
	for k := range runs {
		if !seen[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	keys = append(keys, extra...)
	var parts []string
	for _, k := range keys {
		n := runs[k]
		if n <= 0 {
			continue
		}
		key := "overview.runstatus." + k
		if env.Cat.Has(key) {
			parts = append(parts, env.T(key, map[string]any{"count": n}))
		} else {
			parts = append(parts, env.T("overview.runstatus.other", map[string]any{"count": n, "name": k}))
		}
	}
	return parts
}
