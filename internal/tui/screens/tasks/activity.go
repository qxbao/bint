package tasks

import (
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"bint/internal/task"
	"bint/internal/tui/actions"
	"bint/internal/tui/components/text"
	"bint/internal/tui/screens/shared"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

// Theo dõi realtime: (1) State.Activity — lệnh kiểm chứng/cổng mục tiêu/kiểm hẹp Bint đang chạy — hiện ngay ở khối "Đang chạy" kèm
// thời gian và đuôi log (cập nhật theo nhịp Options.Poll); (2) tác vụ có tiến trình điều phối (State.Runner) được nạp mềm định kỳ
// để trạng thái "kết thúc bất thường" (Reconcile khi đọc) hiện ngay khi tiến trình chết, không phải chờ lần chạy kế tiếp.

// runnerTickMsg kích lần nạp mềm định kỳ.
type runnerTickMsg struct{}

// Target cho mô hình gốc biết thông điệp thuộc màn Tác vụ.
func (runnerTickMsg) Target() actions.Screen { return actions.Tasks }

// liveActivity trả hoạt động đang chạy của t (nil nếu không có hoặc tiến trình chạy nó đã chết).
func liveActivity(t *task.Task) *task.Activity {
	a := t.State.Activity
	if a == nil || (a.PID > 0 && !task.RunnerAlive(&task.Runner{PID: a.PID})) {
		return nil
	}
	return a
}

// pollRunners hẹn lần nạp mềm kế tiếp nếu có tác vụ đang được điều phối (một chuỗi nhịp tại một thời điểm).
func (m *Model) pollRunners() tea.Cmd {
	if m.runnerTicking || m.o.Poll <= 0 {
		return nil
	}
	for _, t := range m.Data.Tasks {
		if t.State.Runner != nil || t.State.Activity != nil {
			m.runnerTicking = true
			return tea.Tick(m.o.Poll, func(time.Time) tea.Msg { return runnerTickMsg{} })
		}
	}
	return nil
}

// activityLines là khối "Đang chạy" của tác vụ t (rỗng nếu không có việc dài đang chạy).
func (m *Model) activityLines(env ui.Env, t *task.Task, w int) []string {
	a := liveActivity(t)
	if a == nil {
		return nil
	}
	th := env.Th
	label := map[string]string{task.ActivityVerify: m.t("tk.activity.verify"), task.ActivityGoal: m.t("tk.activity.goal"),
		task.ActivityCheck: m.t("tk.activity.check"), task.ActivityRerun: m.t("tk.activity.rerun")}[a.Kind]
	if label == "" {
		label = a.Kind
	}
	elapsed := ""
	if at, ok := shared.ParseTime(a.Started); ok {
		elapsed = " · " + shared.Now().Sub(at).Round(time.Second).String()
	}
	out := []string{"", m.section(env, m.t("tk.section.activity"), w)}
	out = append(out, text.WrapHang(th.S(theme.Key, env.Spin+" ")+th.Bold(label)+th.S(theme.Muted, elapsed), w, "", "  ")...)
	out = append(out, text.WrapHang(a.Command, w, "  ", "  ")...)
	if a.Log != "" {
		out = append(out, th.S(theme.Muted, "  "+m.t("tk.activity.log")+" "+text.Path(a.Log, max(w-8, 10))))
		for _, l := range logTail(a.Log, 6) {
			out = append(out, th.S(theme.Muted, "  │ ")+text.Truncate(l, max(w-4, 8)))
		}
	}
	return out
}

// logTail trả n dòng không rỗng cuối của tệp log (đọc tối đa 16 KB cuối, bỏ mã màu); lỗi đọc trả rỗng.
func logTail(path string, n int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil
	}
	const max = 16 << 10
	off := st.Size() - max
	if off < 0 {
		off = 0
	}
	buf := make([]byte, st.Size()-off)
	if _, err := f.ReadAt(buf, off); err != nil && len(buf) == 0 {
		return nil
	}
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(string(buf), "\r", ""), "\n") {
		if l = strings.TrimRight(ansi.Strip(l), " \t"); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}
