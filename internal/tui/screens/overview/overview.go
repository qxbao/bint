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
	"bint/internal/tui/components/list"
	"bint/internal/tui/components/logo"
	"bint/internal/tui/components/text"
	"bint/internal/tui/components/zone"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

// Data là dữ liệu của màn Tổng quan.
type Data struct {
	Status *app.StatusReport // nil nếu chưa init
	// Doctor là báo cáo doctor đầy đủ (để đếm); Pending là mọi việc đang chờ người (hộp thư).
	Doctor  *app.DoctorReport
	Pending *app.PendingWorkReport
	// Candidates là số mục tri thức chờ duyệt (candidate + stale): số trên tab Tri thức.
	Candidates int
	// NotInit: repository chưa `bint init` (hiện màn khởi tạo thay cho bảng điều khiển).
	NotInit bool
	// Binary và Revision là bản Bint đang chạy (mô hình gốc điền sau khi nạp: tầng nạp không biết phiên bản binary);
	// LockVersion là bint_version trong core.lock; ConfigOutdated: cấu hình theo mẫu cũ (ConfigAdded là các khoá còn thiếu).
	Binary, Revision string
	LockVersion      string
	ConfigOutdated   bool
	ConfigAdded      []string
}

// LockMismatch: core.lock ghi phiên bản khác binary đang chạy (skill xuất bản bằng bản Bint khác → bint sync).
func (d Data) LockMismatch() bool {
	return d.Binary != "" && d.LockVersion != "" && d.LockVersion != d.Binary
}

// Inbox là một dòng của hộp thư "Việc cần bạn".
type Inbox struct {
	Level  string // "info" | "warn" | "fail"
	Key    string // khoá i18n
	Count  int
	Detail string
	Target actions.Screen
	// Filter: bộ lọc đặt sẵn ở màn Tri thức (chỉ với Target = Knowledge).
	Filter *knowledge.Filter
	// TaskID: tác vụ cần chọn ở màn Tác vụ (chỉ với Target = Tasks và dòng của một tác vụ).
	TaskID string
	// ProposalID: đề xuất cần chọn ở màn Tiến hoá (chỉ với Target = Evolve và dòng của một đề xuất).
	ProposalID string
	// EvalCaseID: ứng viên đánh giá cần chọn ở màn Đánh giá (chỉ với Target = Eval và dòng của một ứng viên).
	EvalCaseID string
	// ComponentID/ComponentKind: thành phần harness candidate cần mở ở màn Hệ thống (dòng của một thành phần).
	ComponentID, ComponentKind string
	// SystemTab: thẻ con của màn Hệ thống cần mở (vd. "doctor" cho dòng cảnh báo doctor).
	SystemTab string
	// ConfigUpgrade: mở thẳng hộp nâng cấu hình (dòng doctor báo cấu hình theo mẫu cũ).
	ConfigUpgrade bool
	// Commands là lệnh CLI xử lý từng việc của dòng.
	Commands []string
}

const maxTaskRows, maxProposalRows, maxEvalRows, maxComponentRows = 5, 5, 5, 5

// shortID rút ID dài thành 10 ký tự cuối (phần phân biệt của ULID); ID ngắn hơn giữ nguyên.
func shortID(id string) string {
	if r := []rune(id); len(r) > 10 {
		return string(r[len(r)-10:])
	}
	return id
}

// sourceTarget là tab xử lý lỗi đọc của từng nguồn.
var sourceTarget = map[string]actions.Screen{
	app.PendingSrcKnowledge: actions.Knowledge, app.PendingSrcComponents: actions.System, app.PendingSrcTasks: actions.Tasks,
	app.PendingSrcProposals: actions.Evolve, app.PendingSrcEval: actions.Eval, app.PendingSrcDoctor: actions.System,
}

// Inbox dựng danh sách việc cần người xử lý từ sáu nguồn của mục 4.3.1: tri thức chờ duyệt (một dòng mỗi
// lô nguon:*, phần còn lại một dòng), thành phần harness candidate, tác vụ blocked, đề xuất tiến hoá,
// ứng viên đánh giá và cảnh báo/lỗi doctor; nguồn đọc lỗi thành một dòng lỗi riêng.
func (d Data) Inbox() []Inbox {
	p := d.Pending
	if p == nil {
		return nil
	}
	var out []Inbox
	pend := []string{knowledge.StatusCandidate, knowledge.StatusStale}
	for _, b := range p.KnowledgeBatches {
		out = append(out, Inbox{Level: "info", Key: "overview.inbox.batch", Count: b.Count(), Detail: b.Tag, Target: actions.Knowledge,
			Filter: &knowledge.Filter{Statuses: pend, Batch: b.Tag}})
	}
	if p.KnowledgeOther > 0 {
		out = append(out, Inbox{Level: "info", Key: "overview.inbox.knowledge", Count: p.KnowledgeOther, Target: actions.Knowledge,
			Filter: &knowledge.Filter{Statuses: pend, NoBatch: true}})
	}
	for i, c := range p.Components {
		if i == maxComponentRows {
			out = append(out, Inbox{Level: "info", Key: "overview.inbox.components_more", Count: len(p.Components) - maxComponentRows, Target: actions.System,
				SystemTab: "components", Commands: []string{"bint harness component list --status candidate"}})
			break
		}
		out = append(out, Inbox{Level: "info", Key: "overview.inbox.component", Detail: strings.TrimSpace(c.Kind + " " + c.ID), Target: actions.System,
			ComponentID: c.ID, ComponentKind: c.Kind, Commands: []string{"bint harness component approve " + c.ID}})
	}
	for i, t := range p.BlockedTasks {
		if i == maxTaskRows {
			out = append(out, Inbox{Level: "info", Key: "overview.inbox.tasks_more", Count: len(p.BlockedTasks) - maxTaskRows, Target: actions.Tasks,
				Commands: []string{"bint task list"}})
			break
		}
		detail := t.Goal
		if detail == "" {
			detail = t.ID
		}
		out = append(out, Inbox{Level: "warn", Key: "overview.inbox.task", Detail: detail, Target: actions.Tasks, TaskID: t.ID,
			Commands: []string{"bint task show " + t.ID}})
	}
	for i, e := range p.Proposals {
		if i == maxProposalRows {
			out = append(out, Inbox{Level: "info", Key: "overview.inbox.proposals_more", Count: len(p.Proposals) - maxProposalRows, Target: actions.Evolve,
				Commands: []string{"bint evolve list"}})
			break
		}
		id := e.ID
		if r := []rune(id); len(r) > 10 {
			id = string(r[len(r)-10:])
		}
		out = append(out, Inbox{Level: "info", Key: "overview.inbox.proposal", Detail: strings.TrimSpace(e.Action + " " + id), Target: actions.Evolve,
			ProposalID: e.ID, Commands: []string{"bint evolve show " + e.ID}})
	}
	for i, id := range p.EvalCandidates {
		if i == maxEvalRows {
			out = append(out, Inbox{Level: "info", Key: "overview.inbox.eval_more", Count: len(p.EvalCandidates) - maxEvalRows, Target: actions.Eval,
				Commands: []string{"bint eval list"}})
			break
		}
		out = append(out, Inbox{Level: "info", Key: "overview.inbox.eval", Detail: shortID(id), Target: actions.Eval, EvalCaseID: id,
			Commands: []string{"bint eval show " + id}})
	}
	if d.LockMismatch() {
		out = append(out, Inbox{Level: "warn", Key: "overview.inbox.version_skew", Detail: d.LockVersion + " ≠ " + d.Binary, Target: actions.System,
			SystemTab: "skills", Commands: []string{"bint sync"}})
	}
	for _, c := range p.Doctor {
		in := Inbox{Level: "warn", Key: "overview.inbox.doctor_warn", Detail: c.Name + ": " + c.Detail, Target: actions.System, SystemTab: "doctor", Commands: []string{"bint doctor"}}
		if c.Level == app.LevelFail {
			in.Level, in.Key = "fail", "overview.inbox.doctor_fail"
		}
		if strings.Contains(c.Detail, "init --update") {
			in.Commands = append(in.Commands, "bint init --update")
		}
		if c.Name == app.CheckConfigTemplate { // mẫu cũ: Enter mở thẳng hộp nâng cấu hình thay vì thẻ Doctor
			in.ConfigUpgrade, in.SystemTab = true, ""
		}
		out = append(out, in)
	}
	for _, e := range p.Errors {
		out = append(out, Inbox{Level: "fail", Key: "overview.inbox.source_error", Detail: e.Source + ": " + e.Err.Error(),
			Target: sourceTarget[e.Source], Commands: []string{"bint doctor"}})
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
	status := app.Status
	if !ingest {
		status = app.StatusNoIngest
	}
	st, err := status(ctx, dir)
	switch {
	case errors.Is(err, app.ErrNotInitialized):
		d.NotInit = true
		d.Doctor = app.Doctor(ctx, dir)
		return d, nil
	case err != nil:
		d.Doctor = app.Doctor(ctx, dir)
		return d, err
	}
	d.Status = st
	// Hộp thư đọc sáu nguồn, mỗi nguồn lỗi riêng; chỉ lỗi mở repository mới làm hỏng cả lần nạp.
	p, err := app.PendingWork(ctx, dir)
	switch {
	case errors.Is(err, app.ErrNotInitialized):
		d.NotInit, d.Status = true, nil
		return d, nil
	case err != nil:
		return d, err
	}
	d.Pending, d.Doctor = p, p.DoctorAll
	d.Candidates = p.KnowledgeTotal()
	if vs, err := app.VersionInfo(ctx, dir, ""); err == nil {
		d.LockVersion, d.ConfigOutdated, d.ConfigAdded = vs.LockVersion, vs.ConfigOutdated, vs.ConfigAdded
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

// NavMsg yêu cầu chuyển sang một tab; Filter (nếu có) là bộ lọc đặt sẵn cho màn Tri thức, TaskID là tác vụ cần
// chọn ở màn Tác vụ, ProposalID là đề xuất cần chọn ở màn Tiến hoá, EvalCaseID là ứng viên cần chọn ở màn Đánh giá.
type NavMsg struct {
	Screen     actions.Screen
	Filter     *knowledge.Filter
	TaskID     string
	ProposalID string
	EvalCaseID string
	// ComponentID/ComponentKind là thành phần cần chọn ở màn Hệ thống; SystemTab là thẻ con cần mở.
	ComponentID, ComponentKind string
	SystemTab                  string
	// ConfigUpgrade: mở hộp nâng cấu hình của màn Hệ thống.
	ConfigUpgrade bool
	// ConfigEdit: mở luồng sửa cấu hình (system.config_edit) của màn Hệ thống; màn MCP gửi khi người dùng bấm sửa cấu hình quyền.
	ConfigEdit bool
}

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
				cur := in[m.Cursor]
				nav := NavMsg{Screen: cur.Target, Filter: cur.Filter, TaskID: cur.TaskID, ProposalID: cur.ProposalID, EvalCaseID: cur.EvalCaseID,
					ComponentID: cur.ComponentID, ComponentKind: cur.ComponentKind, SystemTab: cur.SystemTab, ConfigUpgrade: cur.ConfigUpgrade}
				return m, func() tea.Msg { return nav }
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
			strings.Join(text.WrapHang(m.Err.Error(), max(env.W-4, 10), "  ", "  "), "\n"),
			"", th.S(theme.Muted, env.T("overview.error.hint")),
		}, "\n")
	case !m.Loaded:
		return "\n  " + th.S(theme.Accent, env.Spin) + " " + th.S(theme.Muted, env.T("ui.loading"))
	}
	out := m.body(env, true)
	if len(out) > env.H { // thiếu chỗ (cửa sổ thấp): bỏ chữ "bint." và khẩu hiệu trang trí để nhường chỗ cho thông tin và hộp thư
		out = m.body(env, false)
	}
	if len(out) > env.H {
		out = out[:env.H]
	}
	return strings.Join(centerVertically(out, env.H), "\n")
}

// body dựng các dòng của màn Tổng quan; decor = có chữ "bint." (hoặc logo) và khẩu hiệu ở đầu.
func (m Model) body(env ui.Env, decor bool) []string {
	th := env.Th
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
	if decor {
		add(center(logo.Render(th, logo.Fits(env.W+4, env.H+5)))...)
		add(text.Center(th.S(theme.Muted, env.T("app.tagline")), env.W))
		add("")
	}

	// Hộp thông tin đặt giữa, rộng theo nội dung.
	add(center(m.infoBox(env))...)
	add("")

	// Hộp thư "Việc cần bạn": cửa sổ cuộn quanh dòng đang chọn khi nhiều hơn chỗ trống (còn chỗ cho dòng doctor).
	inbox := m.Data.Inbox()
	head := th.Bold(env.T("overview.inbox.title"))
	if len(inbox) > 0 {
		head += th.S(theme.Key, fmt.Sprintf(" (%d)", len(inbox)))
	}
	add(" " + head)
	if len(inbox) == 0 {
		add("  " + th.S(theme.OK, "✓ ") + th.S(theme.Muted, env.T("overview.inbox.empty")))
	}
	// Hàng đang chọn bị rút gọn (có …) thì hiện nguyên văn ngắt dòng ngay dưới danh sách (tối đa 3 dòng), để không mất nội dung.
	var fullLines []string
	if m.Cursor < len(inbox) {
		in := inbox[m.Cursor]
		b := env.N(in.Key, in.Count, map[string]any{"detail": env.Rewriter().Rewrite(in.Detail)})
		if text.Width(b) > max(env.W-4-text.Width("→ "+env.T("tab."+string(in.Target)))-2, 8) {
			fullLines = text.Wrap(b, max(env.W-6, 10))
			if len(fullLines) > 3 {
				fullLines = append(fullLines[:2], text.Truncate(fullLines[2]+"…", max(env.W-6, 10)))
			}
		}
	}
	room := max(env.H-len(lines)-3-len(fullLines), map[bool]int{true: 3, false: 2}[decor]) // chừa dòng trống, dòng doctor, một dòng báo "còn n dòng" và nguyên văn hàng đang chọn
	from := 0
	if len(inbox) > room {
		from = max(0, min(m.Cursor-room/2, len(inbox)-room))
	}
	to := min(from+room, len(inbox))
	for i := from; i < to; i++ {
		in := inbox[i]
		icon, role := "◌", theme.Accent
		switch in.Level {
		case "warn":
			icon, role = "!", theme.Warn
		case "fail":
			icon, role = "✗", theme.Err
		}
		body := env.N(in.Key, in.Count, map[string]any{"detail": env.Rewriter().Rewrite(in.Detail)})
		right := "→ " + env.T("tab."+string(in.Target))
		avail := env.W - 4 - text.Width(right) - 2
		body = text.Truncate(body, max(avail, 8))
		gap := strings.Repeat(" ", max(env.W-4-text.Width(body)-text.Width(right), 1))
		row := "  " + th.S(role, icon) + " " + body + gap + th.S(theme.Muted, right)
		if i == m.Cursor {
			row = "▸ " + th.S(role, icon) + " " + th.Selected(body) + gap + th.S(theme.Muted, right)
		}
		// Hàng bấm được (cả dòng) và nằm trong khung "inbox" để bánh xe cuộn đúng hộp thư.
		add(zone.Mark("p:inbox", zone.Mark(list.RowZone(i), text.Pad(row, env.W))))
	}
	for _, l := range fullLines {
		add("    " + th.S(theme.Muted, l))
	}
	if len(inbox) > to-from {
		add("  " + th.S(theme.Muted, env.T("overview.inbox.window", map[string]any{"from": from + 1, "to": to, "total": len(inbox)})))
	}
	add("")

	// Doctor rút gọn: chỉ số đếm. Mọi cảnh báo/lỗi doctor đã nằm trong hộp thư ở trên (Inbox), nên không
	// liệt kê lại chi tiết ở đây.
	add(" " + th.Bold(env.T("overview.doctor.title")) + "  " + m.doctorSummary(th))
	return lines
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
	// Giá trị dài (như thống kê lượt chạy) ngắt dòng thay vì bị cắt: dòng tiếp thụt theo cột giá trị.
	vw := max(env.W-2-4-lw-2, 12)
	var body []string
	for _, r := range rows {
		vals := text.WrapHang(r[1], vw, "", "")
		if len(vals) == 0 {
			vals = []string{""}
		}
		for i, v := range vals {
			label := strings.Repeat(" ", lw)
			if i == 0 {
				label = th.S(theme.Accent, text.Pad(r[0], lw))
			}
			body = append(body, label+"  "+v)
		}
	}
	// Phiên bản binary, mẫu cấu hình và core.lock nằm trên viền dưới của hộp (không thêm dòng khi vừa: cửa sổ 80×24 còn đủ chỗ cho hộp
	// thư). Không vừa thì phần thừa chuyển thành dòng trong hộp (core.lock trước, rồi mẫu cấu hình, rồi phiên bản) thay vì bị cắt.
	ver, tpl, lock := m.versionText(th, env), m.templateOnly(th, env), m.lockText(th, env)
	room := max(env.W-2, 20) - 4
	mk := func(parts ...string) string {
		if len(parts) == 0 {
			return ""
		}
		return " " + strings.Join(parts, th.S(theme.Muted, " · ")) + " "
	}
	verSeg, tplSeg, lockSeg := env.T("overview.version")+" "+ver, tpl, th.S(theme.Muted, "core.lock ")+lock
	foot := mk(verSeg, tplSeg, lockSeg)
	switch {
	case text.Width(foot) <= room:
	case text.Width(mk(verSeg, tplSeg)) <= room:
		foot = mk(verSeg, tplSeg)
		body = append(body, th.S(theme.Accent, "core.lock")+"  "+lock)
	case text.Width(mk(verSeg)) <= room:
		foot = mk(verSeg)
		body = append(body, th.S(theme.Accent, env.T("overview.template.label"))+"  "+tpl, th.S(theme.Accent, "core.lock")+"  "+lock)
	default:
		foot = ""
		body = append(body, th.S(theme.Accent, env.T("overview.version"))+"  "+ver, th.S(theme.Accent, env.T("overview.template.label"))+"  "+tpl,
			th.S(theme.Accent, "core.lock")+"  "+lock)
	}
	cw := text.MaxWidth(body)
	w := min(max(cw+4, text.Width(foot)+4), max(env.W-2, 20))
	h := len(body) + 2
	return strings.Split(frame.Box{Title: env.T("overview.info.title"), TitleAt: frame.Left, Bottom: foot, BottomAt: frame.Left}.Render(th, w, h, strings.Join(body, "\n")), "\n")
}

// versionText: phiên bản binary kèm revision (nếu có).
func (m Model) versionText(th theme.Theme, env ui.Env) string {
	d := m.Data
	v := d.Binary
	if v == "" {
		v = env.T("overview.version.unknown")
	}
	if d.Revision != "" {
		v += " " + th.S(theme.Muted, "("+d.Revision+")")
	}
	return v
}

// templateOnly: mẫu cấu hình (✓ hay cũ).
func (m Model) templateOnly(th theme.Theme, env ui.Env) string {
	if m.Data.ConfigOutdated {
		return th.S(theme.Warn, "! "+env.T("overview.template.old"))
	}
	return th.S(theme.OK, "✓ "+env.T("overview.template.ok"))
}

// lockText: bint_version của core.lock (cảnh báo khi khác binary).
func (m Model) lockText(th theme.Theme, env ui.Env) string {
	d := m.Data
	lock := env.T("overview.lock.none")
	if d.LockVersion != "" {
		lock = d.LockVersion
	}
	if d.LockMismatch() {
		lock = th.S(theme.Warn, lock+" ≠ "+d.Binary)
	}
	return lock
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
