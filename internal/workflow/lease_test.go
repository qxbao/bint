package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/task"
	"bint/internal/ulid"
)

// Hợp đồng thuê khoá theo thời gian (docs/isolation-subagent-plan.md mục 6.3).

func newLease(taskID, step string, mode LockMode, until time.Time) leaseEntry {
	return leaseEntry{Task: taskID, Step: step, RunID: ulid.New(), Mode: leaseModeName(mode),
		At: time.Now().UTC().Format(time.RFC3339Nano), Until: until.UTC().Format(time.RFC3339Nano)}
}

func TestLeaseBlocksOtherTaskByModeTable(t *testing.T) {
	st, root := t.TempDir(), t.TempDir()
	tasksDir := filepath.Join(st, "runtime", "tasks")
	a, b := ulid.New(), ulid.New()
	until := time.Now().Add(time.Hour)

	cases := []struct {
		name         string
		hold, want   LockMode
		blocked      bool
		holderIsSelf bool
	}{
		{"độc quyền chặn độc quyền", LockExclusive, LockExclusive, true, false},
		{"độc quyền chặn chia sẻ", LockExclusive, LockShared, true, false},
		{"chia sẻ chặn độc quyền", LockShared, LockExclusive, true, false},
		{"chia sẻ + chia sẻ cùng được", LockShared, LockShared, false, false},
		{"hợp đồng của chính tác vụ không chặn", LockExclusive, LockExclusive, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := grantLease(tasksDir, root, newLease(a, "b-feat", c.hold, until)); err != nil {
				t.Fatal(err)
			}
			defer releaseLease(tasksDir, root, a, "")
			owner := b + " b-feat"
			if c.holderIsSelf {
				owner = a + " b-feat"
			}
			l, err := AcquireLock(tasksDir, root, owner, c.want)
			if c.blocked {
				if !errors.Is(err, ErrLocked) {
					t.Fatalf("muốn ErrLocked, được %v", err)
				}
				if !strings.Contains(err.Error(), a) || !strings.Contains(err.Error(), "subagent") {
					t.Fatalf("lỗi phải nêu chủ hợp đồng: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("muốn lấy được khoá: %v", err)
			}
			l.Release()
		})
	}
}

// Hết hạn xác định theo thời gian: hợp đồng quá hạn không chặn, kể cả khi không có tiến trình nào liên quan.
func TestLeaseExpiresByTimeNotByPID(t *testing.T) {
	st, root := t.TempDir(), t.TempDir()
	tasksDir := filepath.Join(st, "runtime", "tasks")
	a, b := ulid.New(), ulid.New()
	advance := fakeLeaseClock(t)
	if err := grantLease(tasksDir, root, newLease(a, "b-feat", LockExclusive, time.Now().Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(leaseFilePath(lockPath(tasksDir, root)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(raw)), "pid") {
		t.Fatalf("hợp đồng không được mang PID (hết hạn theo thời gian):\n%s", raw)
	}
	if _, err := AcquireLock(tasksDir, root, b+" b-feat", LockExclusive); !errors.Is(err, ErrLocked) {
		t.Fatalf("hợp đồng còn hạn phải chặn: %v", err)
	}
	advance(2 * time.Hour)
	l, err := AcquireLock(tasksDir, root, b+" b-feat", LockExclusive)
	if err != nil {
		t.Fatalf("hợp đồng hết hạn phải nhả: %v", err)
	}
	l.Release()
}

// WaitLock chờ tới khi hợp đồng hết hạn (đồng hồ đẩy về phía trước giữa chừng).
func TestWaitLockWaitsForLeaseExpiry(t *testing.T) {
	st, root := t.TempDir(), t.TempDir()
	tasksDir := filepath.Join(st, "runtime", "tasks")
	a, b := ulid.New(), ulid.New()
	advance := fakeLeaseClock(t)
	oldMin, oldMax := lockPollMin, lockPollMax
	lockPollMin, lockPollMax = 5*time.Millisecond, 10*time.Millisecond
	defer func() { lockPollMin, lockPollMax = oldMin, oldMax }()
	if err := grantLease(tasksDir, root, newLease(a, "b-feat", LockExclusive, time.Now().Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	waited := make(chan string, 1)
	go func() {
		time.Sleep(80 * time.Millisecond)
		advance(2 * time.Hour)
	}()
	l, err := WaitLock(context.Background(), tasksDir, root, b+" b-feat", LockExclusive, WaitOptions{
		Timeout: 10 * time.Second, OnWait: func(h string) { waited <- h }})
	if err != nil {
		t.Fatal(err)
	}
	l.Release()
	select {
	case h := <-waited:
		if !strings.Contains(h, a) {
			t.Fatalf("OnWait phải nêu tác vụ giữ hợp đồng: %q", h)
		}
	default:
		t.Fatal("phải có chờ")
	}
}

// grantLease từ chối khi xung đột và dọn hợp đồng hết hạn; release theo lượt chỉ xoá đúng lượt đó.
func TestGrantAndReleaseLease(t *testing.T) {
	st, root := t.TempDir(), t.TempDir()
	tasksDir := filepath.Join(st, "runtime", "tasks")
	a, b := ulid.New(), ulid.New()
	until := time.Now().Add(time.Hour)
	ea := newLease(a, "b-feat", LockExclusive, until)
	if err := grantLease(tasksDir, root, ea); err != nil {
		t.Fatal(err)
	}
	if err := grantLease(tasksDir, root, newLease(b, "b-req", LockShared, until)); !errors.Is(err, ErrLocked) {
		t.Fatalf("xung đột phải bị từ chối: %v", err)
	}
	// Hợp đồng mới của chính tác vụ thay hợp đồng cũ.
	ea2 := newLease(a, "b-review", LockShared, until)
	if err := grantLease(tasksDir, root, ea2); err != nil {
		t.Fatal(err)
	}
	if got := activeLeases(lockPath(tasksDir, root), time.Now()); len(got) != 1 || got[0].Step != "b-review" {
		t.Fatalf("hợp đồng: %+v", got)
	}
	if err := releaseLease(tasksDir, root, a, "lượt-khác"); err != nil {
		t.Fatal(err)
	}
	if got := activeLeases(lockPath(tasksDir, root), time.Now()); len(got) != 1 {
		t.Fatalf("release sai lượt không được xoá: %+v", got)
	}
	if err := releaseLease(tasksDir, root, a, ea2.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(leaseFilePath(lockPath(tasksDir, root))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("hết hợp đồng thì xoá tệp: %v", err)
	}
	// Tệp hợp đồng hỏng: coi như không có (không chặn vĩnh viễn).
	if err := os.WriteFile(leaseFilePath(lockPath(tasksDir, root)), []byte("{hỏng"), 0o600); err != nil {
		t.Fatal(err)
	}
	if l, err := AcquireLock(tasksDir, root, b+" b-feat", LockExclusive); err != nil {
		t.Fatalf("tệp hỏng: %v", err)
	} else {
		l.Release()
	}
}

// ActiveSteps liệt kê bước chạy bằng subagent (hợp đồng còn hạn, không có tiến trình).
func TestActiveStepsListsLeases(t *testing.T) {
	st, root := t.TempDir(), t.TempDir()
	tasksDir := filepath.Join(st, "runtime", "tasks")
	a := ulid.New()
	if got := ActiveSteps(tasksDir, root); len(got) != 0 {
		t.Fatalf("chưa có gì: %+v", got)
	}
	if err := grantLease(tasksDir, root, newLease(a, "b-feat", LockExclusive, time.Now().Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	got := ActiveSteps(tasksDir, root)
	if len(got) != 1 || got[0].TaskID != a || got[0].Step != "b-feat" || got[0].PID != 0 || got[0].Mode != LockExclusive.String() {
		t.Fatalf("ActiveSteps: %+v", got)
	}
}

// `bint run` kiểu tiến trình (b-feat của tác vụ khác) chờ hợp đồng còn hạn và lấy được khi nó hết hạn; --no-wait
// thì blocked ngay; next của tác vụ khác thấy hợp đồng còn hạn ⇒ blocked (mục 4.2).
func TestProcessRunAndNextRespectSessionLease(t *testing.T) {
	h := newHarness(t)
	advance := fakeLeaseClock(t)
	r := h.runner()
	a := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	b := h.newDo(r, NewTaskOptions{})
	c := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	ctx := context.Background()

	if act, err := r.Next(ctx, NextOptions{TaskRef: a.State.ID}); err != nil || act.Action != ActionAgent {
		t.Fatalf("next của A: %+v %v", act, err)
	}
	// B (driver process) --no-wait: blocked ngay vì hợp đồng của A.
	res, err := h.runner().Run(ctx, RunOptions{Workflow: StepFeat, TaskRef: b.State.ID, NoWait: true})
	if err != nil || res.Status != task.StatusBlocked || !strings.Contains(res.Reason, "subagent") || !strings.Contains(res.Reason, a.State.ID) {
		t.Fatalf("B --no-wait: %+v %v", res, err)
	}
	if len(h.calls) != 0 {
		t.Fatalf("agent của B không được chạy: %v", h.calls)
	}
	// next của C (tác vụ khác, session) thấy hợp đồng còn hạn ⇒ blocked.
	act, err := r.Next(ctx, NextOptions{TaskRef: c.State.ID, NoWait: true})
	if err != nil || act.Action != ActionBlocked || !strings.Contains(act.Reason, a.State.ID) {
		t.Fatalf("next của C: %+v %v", act, err)
	}
	// Có chờ: B chờ rồi lấy được khi hợp đồng hết hạn.
	h.behave = featWrites(h, nil)
	oldMin, oldMax := lockPollMin, lockPollMax
	lockPollMin, lockPollMax = 5*time.Millisecond, 10*time.Millisecond
	defer func() { lockPollMin, lockPollMax = oldMin, oldMax }()
	go func() {
		time.Sleep(100 * time.Millisecond)
		advance(2 * time.Hour)
	}()
	res, err = h.runner().Run(ctx, RunOptions{Workflow: StepFeat, TaskRef: b.State.ID, Resume: true})
	if err != nil || res.Status != task.StatusInReview || len(h.calls) != 1 {
		t.Fatalf("B sau hạn: %+v %v calls=%v", res, err, h.calls)
	}
}

// Hai bước chỉ đọc (session + process) cùng chạy được như mục 4.2.
func TestSessionReadonlyLeasesShare(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	a := h.newSession(r, NewTaskOptions{}) // b-req (chỉ đọc)
	b := h.newSession(r, NewTaskOptions{})
	ctx := context.Background()
	for _, tk := range []*task.Task{a, b} {
		act, err := r.Next(ctx, NextOptions{TaskRef: tk.State.ID, NoWait: true})
		if err != nil || act.Action != ActionAgent || act.Step != StepReq || !act.ReadOnly {
			t.Fatalf("next %s: %+v %v", tk.State.ID, act, err)
		}
	}
}
