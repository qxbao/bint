package task

import (
	"os"
	"strconv"
	"time"
)

// ClaimRunner ghi tiến trình hiện tại là tiến trình điều phối tác vụ. Trả lỗi nếu
// một tiến trình khác còn sống đang điều phối.
func (t *Task) ClaimRunner(runID string, now time.Time) error {
	if r := t.State.Runner; r != nil && RunnerAlive(r) && r.PID != os.Getpid() {
		return &BusyError{Runner: *r}
	}
	host, _ := os.Hostname()
	t.State.Runner = &Runner{PID: os.Getpid(), Host: host, RunID: runID, Started: Now(now)}
	return nil
}

// BusyError: tác vụ đang được tiến trình khác điều phối.
type BusyError struct{ Runner Runner }

func (e *BusyError) Error() string {
	return "tác vụ đang được `bint run` điều phối (pid " + strconv.Itoa(e.Runner.PID) + ", lượt " + e.Runner.RunID + ")"
}

// RunnerAlive cho biết tiến trình điều phối còn sống. Tiến trình ở máy khác (thư
// mục trạng thái dùng chung qua mạng) được coi là còn sống.
func RunnerAlive(r *Runner) bool {
	if r == nil {
		return false
	}
	host, _ := os.Hostname()
	if r.Host != "" && host != "" && r.Host != host {
		return true
	}
	return processAlive(r.PID)
}

// OwnedByOther cho biết tác vụ đang được một tiến trình `bint run` khác điều phối.
func (t *Task) OwnedByOther() bool {
	r := t.State.Runner
	return r != nil && r.PID != os.Getpid() && RunnerAlive(r)
}

// Reconcile phát hiện tiến trình điều phối đã chết mà không kết thúc tác vụ (bị
// kill, mất điện): tác vụ đang hoạt động chuyển interrupted. Trả về run ID của
// tiến trình đó (để đóng lượt chạy) và true nếu đã thay đổi.
func (t *Task) Reconcile(now time.Time) (string, bool) {
	r := t.State.Runner
	if r == nil || RunnerAlive(r) {
		return "", false
	}
	t.State.Runner = nil
	if Active(t.State.Status) {
		t.State.Apply(StatusInterrupted, "bint", "tiến trình bint run (pid "+strconv.Itoa(r.PID)+") kết thúc bất thường", Now(now))
		t.State.Errors = append(t.State.Errors, ErrorRecord{Message: "tiến trình điều phối kết thúc bất thường", At: Now(now), By: "bint", RunID: r.RunID})
	}
	return r.RunID, true
}
