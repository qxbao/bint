package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/task"
	tkscreen "bint/internal/tui/screens/tasks"
	"bint/internal/tui/theme"
)

// Lệnh kiểm chứng đang chạy (State.Activity) hiện ngay ở chi tiết tác vụ: tên việc, lệnh, đường dẫn log và đuôi log.
func TestTaskActivityShowsRunningVerifyAndLogTail(t *testing.T) {
	log := filepath.Join(t.TempDir(), "verify-3.log")
	if err := os.WriteFile(log, []byte("ok  \tbint/internal/app\t56s\n=== RUN   TestSomethingSlow\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	loader := func(context.Context, string, bool) (tkscreen.Data, error) {
		d := sampleTasks()
		for _, tk := range d.Tasks { // tác vụ nào đang được chọn cũng có việc đang chạy
			tk.State.Activity = &task.Activity{Kind: task.ActivityVerify, Command: "go test ./...", Log: log,
				Started: task.Now(time.Now().Add(-90 * time.Second)), PID: os.Getpid()}
		}
		return d, nil
	}
	m := newModel(t, Options{Mode: theme.Mono, Lang: "vi", TasksLoader: loader}, sampleData(), 160, 45)
	press(t, m, "3")
	out := plain(m.Render())
	for _, want := range []string{"Đang chạy", "Kiểm chứng", "go test ./...", "verify-3.log", "TestSomethingSlow"} {
		if !strings.Contains(out, want) {
			t.Errorf("thiếu %q ở chi tiết tác vụ đang kiểm chứng:\n%s", want, out)
		}
	}
}
