// Package initplaceholder là màn tạm cho repository chưa `bint init`: giải thích Bint sẽ làm gì và
// một nút "Khởi tạo với mặc định" (gọi app.Init không hỏi, đúng như `bint init` không tương tác),
// kèm lệnh CLI tương đương. Trình hướng dẫn nhiều bước thay nó ở giai đoạn 5.
package initplaceholder

import (
	"strings"

	"bint/internal/app"
	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

// Model là trạng thái màn tạm.
type Model struct {
	Dir      string
	ReadOnly bool // chế độ chỉ đọc: không khởi tạo được, chỉ hiện thông báo
	Running  bool
	Report   *app.InitReport
	Err      error
	// LoadErr: khởi tạo xong nhưng nạp lại trạng thái repository thất bại (hiện cảnh báo dưới báo cáo).
	LoadErr error
}

// Done cho biết đã khởi tạo xong (có báo cáo, không lỗi cứng).
func (m Model) Done() bool { return m.Report != nil && m.Err == nil }

// Finish ghi kết quả khởi tạo.
func (m Model) Finish(rep *app.InitReport, err error) Model {
	m.Running, m.Report, m.Err = false, rep, err
	return m
}

var stepIcon = map[app.StepStatus]struct {
	icon string
	role theme.Role
}{
	app.StepCreated:  {"+", theme.OK},
	app.StepUpdated:  {"~", theme.OK},
	app.StepOK:       {"✓", theme.OK},
	app.StepSkipped:  {"-", theme.Muted},
	app.StepWarn:     {"!", theme.Warn},
	app.StepConflict: {"✗", theme.Err},
}

// View vẽ nội dung màn hình trong env.W × env.H.
func (m Model) View(env ui.Env) string {
	th := env.Th
	var lines []string
	add := func(l ...string) { lines = append(lines, l...) }
	// wrap ngắt dòng có thụt lề; bullet: dòng đầu có "• ", dòng gói tiếp thụt treo theo chữ sau "• ".
	wrapWith := func(role theme.Role, s, first, rest string) {
		for _, l := range text.WrapHang(s, max(env.W-2, 10), first, rest) {
			add(th.S(role, l))
		}
	}
	wrap := func(role theme.Role, s string) { wrapWith(role, s, "  ", "  ") }
	bullet := func(role theme.Role, s string) { wrapWith(role, s, "  • ", "    ") }
	titleKey, introKey := "init.title", "init.intro"
	switch {
	case m.Err != nil:
		titleKey, introKey = "init.title.error", "init.intro.error"
	case m.Report != nil:
		titleKey, introKey = "init.title.done", "init.intro.done"
	}
	add("", "  "+th.Bold(env.T(titleKey)))
	add("")
	wrap(theme.Fg, env.T(introKey, map[string]any{"dir": text.Path(m.Dir, max(env.W-24, 20))}))
	add("")
	switch {
	case m.ReadOnly:
		wrap(theme.Warn, env.T("init.readonly"))
		add("")
		add("  " + th.S(theme.Muted, env.T("ui.cli_equivalent")+" ") + th.S(theme.Accent, "bint init"))
	case m.Running:
		add("  " + th.S(theme.Accent, env.Spin) + " " + env.T("init.running"))
	case m.Err != nil:
		add("  " + th.S(theme.Err, "✗ ") + env.T("init.error"))
		wrap(theme.Err, m.Err.Error())
		add("")
		wrap(theme.Muted, env.T("init.error.hint"))
	case m.Report != nil:
		add("  " + th.S(theme.OK, "✓ ") + th.Bold(env.T("init.done")))
		if m.LoadErr != nil {
			wrap(theme.Warn, env.T("init.reload_failed", map[string]any{"error": text.TildeIn(m.LoadErr.Error())}))
		}
		lines = append(lines, m.reportLines(env, len(lines))...)
	default:
		for _, k := range []string{"init.does.config", "init.does.state", "init.does.hooks", "init.does.gitignore"} {
			bullet(theme.Fg, env.T(k))
		}
		add("")
		wrap(theme.Muted, env.T("init.skills_note"))
		add("")
		add("  " + th.S(theme.Muted, env.T("ui.cli_equivalent")+" ") + th.S(theme.Accent, "bint init"))
	}
	if len(lines) > env.H {
		lines = lines[:env.H]
	}
	return strings.Join(lines, "\n")
}

// reportLines vẽ các bước và "bước tiếp theo" của báo cáo vào phần còn lại của màn hình (used là số
// dòng đã vẽ). Bước cảnh báo/xung đột đứng trước; khi không đủ chỗ thì rút gọn danh sách bước bằng
// một dòng "… và N bước khác" để các bước tiếp theo luôn hiện đủ.
func (m Model) reportLines(env ui.Env, used int) []string {
	th := env.Th
	var problems, rest []string
	for _, s := range m.Report.Steps {
		si := stepIcon[s.Status]
		l := "   " + th.S(si.role, si.icon) + " " + stepText(th, text.TildeIn(s.Name), text.TildeIn(s.Detail), max(env.W-8, 8))
		if s.Status == app.StepWarn || s.Status == app.StepConflict {
			problems = append(problems, l)
		} else {
			rest = append(rest, l)
		}
	}
	steps := append(problems, rest...)
	var next []string
	if len(m.Report.NextSteps) > 0 {
		next = append(next, "", "  "+th.Bold(env.T("init.next")))
		for i, s := range m.Report.NextSteps {
			next = append(next, "   "+th.S(theme.Key, string(rune('1'+i))+".")+" "+text.Truncate(s, max(env.W-9, 8)))
		}
	}
	if avail := env.H - used - len(next); len(steps) > avail {
		keep := max(avail-1, 0)
		more := "  " + th.S(theme.Muted, env.T("init.more_steps", map[string]any{"count": len(steps) - keep}))
		steps = append(steps[:keep:keep], more)
	}
	return append(steps, next...)
}

// stepText ghép tên và chi tiết của một bước trong w ô. Chi tiết thường là đường dẫn kèm hậu tố
// ("/…/state (schema v6)"), nên khi thiếu chỗ thì rút phần ĐẦU của chi tiết (giữ đuôi) chứ không cắt đuôi;
// chỉ khi chỗ cho chi tiết quá nhỏ mới cắt cả dòng như thường.
func stepText(th theme.Theme, name, detail string, w int) string {
	if detail == "" {
		return text.Truncate(name, w)
	}
	if room := w - text.Width(name) - 2; text.Width(detail) > room && room >= 8 {
		detail = text.TruncateHead(detail, room)
	}
	return text.Truncate(name+"  "+th.S(theme.Muted, detail), w)
}
