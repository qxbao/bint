package workflow

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"bint/internal/task"
)

// shortPoll rút khoảng thăm dò chờ khoá cho kiểm thử.
func shortPoll(t *testing.T) {
	t.Helper()
	oldMin, oldMax := lockPollMin, lockPollMax
	lockPollMin, lockPollMax = time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() { lockPollMin, lockPollMax = oldMin, oldMax })
}

type logSink struct {
	mu    sync.Mutex
	lines []string
	wait  chan struct{} // đóng khi gặp dòng "chờ khoá" đầu tiên
	once  sync.Once
}

func newLogSink() *logSink { return &logSink{wait: make(chan struct{})} }

func (l *logSink) logf(format string, a ...any) {
	line := strings.TrimSpace(fmt.Sprintf(format, a...))
	l.mu.Lock()
	l.lines = append(l.lines, line)
	l.mu.Unlock()
	if strings.Contains(line, "chờ khoá") {
		l.once.Do(func() { close(l.wait) })
	}
}

func (l *logSink) all() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

// AC-1: hai Runner với agent giả trong cùng thư mục tạm. b-review của A không fail khi B có b-feat chờ;
// B chờ tới lúc A trả khoá rồi mới chạy.
func TestConcurrentReviewDoesNotFailWhenFeatWaits(t *testing.T) {
	shortPoll(t)
	h := newHarness(t)
	ctx := context.Background()

	// A: b-feat xong → in_review.
	resA, err := h.runner().Run(ctx, RunOptions{Workflow: StepFeat, Request: "Thêm tính năng phiên bản v2 cho app.txt"})
	if err != nil || resA.Status != task.StatusInReview {
		t.Fatalf("A b-feat: %+v %v", resA, err)
	}
	idA := resA.TaskID

	var evMu sync.Mutex
	var events []string
	ev := func(s string) { evMu.Lock(); events = append(events, s); evMu.Unlock() }
	started, release := make(chan struct{}), make(chan struct{})
	happy := h.behave
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		switch {
		case step == StepReview && id == idA:
			ev("A review bắt đầu")
			close(started)
			<-release
			ev("A review xong")
		case step == StepFeat && id != idA:
			ev("B b-feat bắt đầu")
			h.write("b.txt", "của B\n")
			return nil
		}
		return happy(ctx, step, e, id)
	}

	type out struct {
		res *Result
		err error
	}
	doneA, doneB := make(chan out, 1), make(chan out, 1)
	go func() {
		r := h.runner()
		res, err := r.Run(ctx, RunOptions{Workflow: StepReview, TaskRef: idA})
		doneA <- out{res, err}
	}()
	<-started

	sink := newLogSink()
	go func() {
		r := h.runner()
		r.Logf = sink.logf
		res, err := r.Run(ctx, RunOptions{Workflow: StepFeat, Request: "Thêm tệp b.txt cho tác vụ B"})
		doneB <- out{res, err}
	}()
	select {
	case <-sink.wait:
	case o := <-doneB:
		t.Fatalf("B không chờ khoá mà đã kết thúc: %+v %v\n%s", o.res, o.err, sink.all())
	case <-time.After(20 * time.Second):
		t.Fatalf("B không báo chờ khoá:\n%s", sink.all())
	}
	// B đang chờ: chưa chạy b-feat.
	evMu.Lock()
	if strings.Contains(strings.Join(events, ","), "B b-feat") {
		evMu.Unlock()
		t.Fatal("B chạy b-feat khi A còn giữ khoá chia sẻ")
	}
	evMu.Unlock()
	close(release)

	oa := <-doneA
	if oa.err != nil || oa.res.Status != task.StatusInReview {
		t.Fatalf("A review: %+v %v", oa.res, oa.err)
	}
	last := oa.res.Steps[len(oa.res.Steps)-1]
	if last.Step != StepReview || last.Status != "completed" || last.Error != "" {
		t.Fatalf("b-review của A phải completed, không lỗi chỉ đọc: %+v", last)
	}
	ob := <-doneB
	if ob.err != nil || ob.res.Status != task.StatusInReview {
		t.Fatalf("B: %+v %v\n%s", ob.res, ob.err, sink.all())
	}
	evMu.Lock()
	got := strings.Join(events, " | ")
	evMu.Unlock()
	if got != "A review bắt đầu | A review xong | B b-feat bắt đầu" {
		t.Fatalf("thứ tự: %s", got)
	}
	if !strings.Contains(sink.all(), idA) {
		t.Fatalf("log chờ khoá phải nêu chủ khoá (tác vụ A):\n%s", sink.all())
	}
}

// --no-wait: B không chờ A mà blocked ngay; A không bị ảnh hưởng.
func TestConcurrentNoWaitBlocksImmediately(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	resA, err := h.runner().Run(ctx, RunOptions{Workflow: StepFeat, Request: "Thêm tính năng phiên bản v2 cho app.txt"})
	if err != nil || resA.Status != task.StatusInReview {
		t.Fatalf("A b-feat: %+v %v", resA, err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	happy := h.behave
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepReview {
			close(started)
			<-release
		}
		return happy(ctx, step, e, id)
	}
	doneA := make(chan *Result, 1)
	go func() {
		res, _ := h.runner().Run(ctx, RunOptions{Workflow: StepReview, TaskRef: resA.TaskID})
		doneA <- res
	}()
	<-started
	start := time.Now()
	resB, err := h.runner().Run(ctx, RunOptions{Workflow: StepFeat, Request: "Việc khác", NoWait: true})
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	if resB.Status != task.StatusBlocked || !strings.Contains(resB.Reason, resA.TaskID) || time.Since(start) > 5*time.Second {
		t.Fatalf("B phải blocked ngay, nêu chủ khoá: %+v (%v)", resB, time.Since(start))
	}
	if ra := <-doneA; ra.Status != task.StatusInReview || ra.Steps[len(ra.Steps)-1].Status != "completed" {
		t.Fatalf("A: %+v", ra)
	}
}

// Hết hạn chờ khoá: blocked với lý do "chờ khoá quá N phút"; Ctrl-C khi chờ: interrupted.
func TestWaitLockCancelInterruptsTask(t *testing.T) {
	shortPoll(t)
	h := newHarness(t)
	lock, err := AcquireLock(h.tasks.Dir, h.root, "phiên khác", LockExclusive)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()

	ctx, cancel := context.WithCancel(context.Background())
	sink := newLogSink()
	done := make(chan *Result, 1)
	go func() {
		r := h.runner()
		r.Logf = sink.logf
		res, _ := r.Run(ctx, RunOptions{Workflow: StepFeat, Request: "Thêm tính năng phiên bản v2 cho app.txt"})
		done <- res
	}()
	select {
	case <-sink.wait:
	case <-time.After(20 * time.Second):
		t.Fatal("không báo chờ khoá")
	}
	cancel()
	res := <-done
	if res.Status != task.StatusInterrupted || !strings.Contains(res.Reason, "chờ khoá") || strings.Contains(strings.Join(h.calls, ","), StepFeat) {
		t.Fatalf("huỷ khi chờ: %+v %v", res, h.calls)
	}
}

// Hết hạn chờ khoá của Runner: blocked, lý do "chờ khoá quá N phút: <chủ>".
func TestWaitLockExpiryBlocksTask(t *testing.T) {
	shortPoll(t)
	old := lockWaitUnit
	lockWaitUnit = 40 * time.Millisecond
	defer func() { lockWaitUnit = old }()
	h := newHarness(t)
	h.cfg.Workflow.TimeoutMinutes = 1
	lock, err := AcquireLock(h.tasks.Dir, h.root, "phiên khác", LockExclusive)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepFeat, Request: "Thêm tính năng phiên bản v2 cho app.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != task.StatusBlocked || !strings.Contains(res.Reason, "chờ khoá quá 1 phút") || !strings.Contains(res.Reason, "phiên khác") {
		t.Fatalf("hết hạn chờ: %+v", res)
	}
}
