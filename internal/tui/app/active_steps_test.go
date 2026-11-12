package app

import (
	"strings"
	"testing"
	"time"

	"bint/internal/tui/actions"
	"bint/internal/workflow"
)

// H8 / AC-5 (docs/isolation-subagent-plan.md 4.5): cây đang có bước `bint run` chạy thì thanh trên báo
// "<bước> của <tác vụ> đang chạy trên cây này"; thao tác ghi vẫn được phép. Dùng khoá thật của workflow và dịch vụ thật.

const activeTask = "01M772P7DKB3PBRBH7VPE614X6"

func (f *fixture) lock(owner string, mode workflow.LockMode) *workflow.Lock {
	f.t.Helper()
	l, err := workflow.AcquireLock(f.ws.Tasks.Dir, f.ws.Root, owner, mode)
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(l.Release)
	return l
}

func (f *fixture) refreshActive(want int) {
	f.t.Helper()
	f.pump(func() bool { return !f.m.activeLoading }, nil) // chờ lần đọc đang bay (có thể đọc trước khi lấy khoá)
	f.pump(func() bool { return !f.m.activeLoading && len(f.m.active) == want }, f.m.loadActive())
}

func TestActiveStepStatusLine(t *testing.T) {
	f := newFixture(t, Options{})
	const vi, en = "b-review của 01M772P7 đang chạy trên cây này", "b-review of 01M772P7 is running on this tree"
	if r := plain(f.m.Render()); strings.Contains(r, "đang chạy trên cây này") {
		t.Fatalf("chưa có khoá thì không có dòng trạng thái:\n%s", r)
	}

	l := f.lock(activeTask+" b-review", workflow.LockShared)
	f.refreshActive(1)
	if r := plain(f.m.Render()); !strings.Contains(r, vi) {
		t.Fatalf("thiếu %q:\n%s", vi, r)
	}

	// Thao tác ghi vẫn được phép khi có bước đang chạy.
	if got := f.m.gate("system.language_save"); got != actions.Available {
		t.Fatalf("system.language_save phải còn Available khi có bước chạy: %v", got)
	}

	f.m.applyLanguage("en")
	if r := plain(f.m.Render()); !strings.Contains(r, en) {
		t.Fatalf("thiếu bản en %q:\n%s", en, r)
	}
	f.m.applyLanguage("vi")

	// Hai bước chạy: dạng gộp.
	f.lock("01M772P7DKB3PBRBH7VPE614X7 b-req", workflow.LockShared)
	f.refreshActive(2)
	if r := plain(f.m.Render()); !strings.Contains(r, "2 bước đang chạy trên cây này") {
		t.Fatalf("dạng gộp:\n%s", r)
	}

	l.Release()
	f.refreshActive(1)
}

func TestActiveStepStatusLineDisappearsWhenLockReleased(t *testing.T) {
	f := newFixture(t, Options{})
	l := f.lock(activeTask+" b-feat", workflow.LockExclusive)
	f.refreshActive(1)
	if r := plain(f.m.Render()); !strings.Contains(r, "b-feat của 01M772P7 đang chạy trên cây này") {
		t.Fatalf("thiếu dòng:\n%s", r)
	}
	l.Release()
	f.refreshActive(0)
	if r := plain(f.m.Render()); strings.Contains(r, "đang chạy trên cây này") {
		t.Fatalf("hết bước thì dòng phải biến mất:\n%s", r)
	}
}

// ActivePoll > 0: TUI tự phát hiện bước mới bắt đầu mà không cần người làm mới.
func TestActiveStepPolling(t *testing.T) {
	f := newFixture(t, Options{ActivePoll: 10 * time.Millisecond})
	f.lock(activeTask+" b-review", workflow.LockShared)
	f.pump(func() bool { return len(f.m.active) == 1 }, nil)
	if r := plain(f.m.Render()); !strings.Contains(r, "b-review của 01M772P7 đang chạy trên cây này") {
		t.Fatalf("thiếu dòng sau khi thăm dò:\n%s", r)
	}
}
