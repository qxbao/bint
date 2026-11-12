package workflow

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func mustLock(t *testing.T, tasks, root, owner string, mode LockMode) *Lock {
	t.Helper()
	l, err := AcquireLock(tasks, root, owner, mode)
	if err != nil {
		t.Fatalf("AcquireLock(%s, %v): %v", owner, mode, err)
	}
	return l
}

func wantLocked(t *testing.T, tasks, root string, mode LockMode) {
	t.Helper()
	l, err := AcquireLock(tasks, root, "kẻ xin", mode)
	if err == nil {
		l.Release()
		t.Fatalf("lấy được khoá %v đang bị chặn", mode)
	}
	if !errors.Is(err, ErrLocked) || !errors.Is(err, ErrWriteLocked) {
		t.Fatalf("lỗi không bọc ErrLocked: %v", err)
	}
}

// Chia sẻ + chia sẻ được; chia sẻ chặn độc quyền và ngược lại.
func TestLockSharedAndExclusiveMatrix(t *testing.T) {
	tasks := filepath.Join(t.TempDir(), "tasks")
	a := mustLock(t, tasks, "/repo", "a", LockShared)
	b := mustLock(t, tasks, "/repo", "b", LockShared)
	wantLocked(t, tasks, "/repo", LockExclusive)
	a.Release()
	wantLocked(t, tasks, "/repo", LockExclusive) // b còn giữ
	b.Release()
	x := mustLock(t, tasks, "/repo", "x", LockExclusive)
	wantLocked(t, tasks, "/repo", LockShared)
	wantLocked(t, tasks, "/repo", LockExclusive)
	x.Release()
	s := mustLock(t, tasks, "/repo", "s", LockShared)
	s.Release()
	// Worktree khác: khoá riêng.
	y := mustLock(t, tasks, t.TempDir(), "y", LockExclusive)
	y.Release()
	if _, err := AcquireLock(tasks, "/repo", "z", LockMode(0)); err == nil {
		t.Fatal("chế độ khoá không hợp lệ phải bị từ chối")
	}
}

// AcquireWriteLock là vỏ của khoá độc quyền.
func TestAcquireWriteLockIsExclusive(t *testing.T) {
	tasks := filepath.Join(t.TempDir(), "tasks")
	l, err := AcquireWriteLock(tasks, "/repo", "w")
	if err != nil {
		t.Fatal(err)
	}
	wantLocked(t, tasks, "/repo", LockShared)
	l.Release()
}

// Release không xoá tệp khoá (xoá khi người đọc khác còn giữ sẽ cho phép hai chủ độc quyền).
func TestLockReleaseKeepsLockFile(t *testing.T) {
	tasks := filepath.Join(t.TempDir(), "tasks")
	a := mustLock(t, tasks, "/repo", "a", LockShared)
	b := mustLock(t, tasks, "/repo", "b", LockShared)
	p := lockPath(tasks, "/repo")
	a.Release()
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("Release xoá tệp khoá: %v", err)
	}
	wantLocked(t, tasks, "/repo", LockExclusive)
	b.Release()
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("Release xoá tệp khoá: %v", err)
	}
	a.Release() // gọi lại không làm gì
	var nilLock *Lock
	nilLock.Release()
	if ents, _ := os.ReadDir(p + ".d"); len(ents) != 0 {
		t.Fatalf("tệp chủ khoá còn sót: %v", ents)
	}
}

// Tệp chủ khoá: có khi đang giữ, dùng để báo lỗi; mục của tiến trình chết bị ẩn.
func TestLockHolderInfo(t *testing.T) {
	tasks := filepath.Join(t.TempDir(), "tasks")
	a := mustLock(t, tasks, "/repo", "tác vụ A b-feat", LockExclusive)
	defer a.Release()
	_, err := AcquireLock(tasks, "/repo", "x", LockShared)
	if err == nil || !strings.Contains(err.Error(), "tác vụ A b-feat") || !strings.Contains(err.Error(), "có quyền ghi") {
		t.Fatalf("lỗi thiếu chủ khoá: %v", err)
	}
	dead, _ := json.Marshal(holderInfo{PID: 2147483646, Owner: "ma", Mode: "chỉ đọc"})
	ghost := filepath.Join(lockPath(tasks, "/repo")+".d", "ghost.json")
	if err := os.WriteFile(ghost, dead, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = AcquireLock(tasks, "/repo", "x", LockShared)
	if err == nil || strings.Contains(err.Error(), "ma giữ") {
		t.Fatalf("mục của tiến trình chết phải bị ẩn: %v", err)
	}
	if _, err := os.Stat(ghost); err == nil {
		t.Fatal("mục của tiến trình chết phải được dọn")
	}
}

// Bất biến dưới tranh chấp: tối đa một chủ độc quyền, và độc quyền không cùng lúc với chia sẻ. Mỗi goroutine
// tranh khoá tới khi giữ được đủ số lần (nghỉ ngẫu nhiên sau mỗi lần thất bại): người đọc liên tục chồng nhau
// có thể làm người ghi đói lượt (cố ý, xem docs/workflows.md), nên kiểm thử không được giả định mỗi lần thử
// đều thành công.
func TestLockMixedContentionInvariant(t *testing.T) {
	tasks := filepath.Join(t.TempDir(), "tasks")
	var readers, writers, bad, gotX, gotS atomic.Int32
	var wg sync.WaitGroup
	deadline := time.Now().Add(30 * time.Second)
	for g := 0; g < 24; g++ {
		wg.Add(1)
		mode, want := LockShared, 40
		if g%4 == 0 {
			mode, want = LockExclusive, 10
		}
		go func() {
			defer wg.Done()
			for got := 0; got < want && time.Now().Before(deadline); {
				l, err := AcquireLock(tasks, "/repo", "t", mode)
				if err != nil {
					time.Sleep(time.Duration(20+rand.IntN(300)) * time.Microsecond)
					continue
				}
				got++
				if mode == LockExclusive {
					gotX.Add(1)
					if writers.Add(1) > 1 || readers.Load() > 0 {
						bad.Add(1)
					}
					time.Sleep(100 * time.Microsecond)
					writers.Add(-1)
				} else {
					gotS.Add(1)
					readers.Add(1)
					if writers.Load() > 0 {
						bad.Add(1)
					}
					time.Sleep(100 * time.Microsecond)
					readers.Add(-1)
				}
				l.Release()
				time.Sleep(time.Duration(rand.IntN(200)) * time.Microsecond)
			}
		}()
	}
	wg.Wait()
	if bad.Load() > 0 || gotX.Load() != 6*10 || gotS.Load() != 18*40 {
		t.Fatalf("vi phạm bất biến=%d, độc quyền=%d (mong 60), chia sẻ=%d (mong 720)", bad.Load(), gotX.Load(), gotS.Load())
	}
}

// Thu hồi khoá bỏ lại: nhiều người tranh khoá ngay sau khi chủ "chết" (hệ điều hành nhả
// khoá, tệp chủ còn lại) không được chồng chủ độc quyền.
func TestLockDeadOwnerReleasedNoOverlap(t *testing.T) {
	tasks := filepath.Join(t.TempDir(), "tasks")
	root := t.TempDir()
	var overlap, acquired atomic.Int64
	for round := 0; round < 24; round++ {
		l := mustLock(t, tasks, root, "dead-owner", LockExclusive)
		l.f.Close() // giả lập tiến trình chết: khoá nhả, tệp chủ còn
		var active atomic.Int64
		var wg sync.WaitGroup
		start := make(chan struct{})
		for w := 0; w < 24; w++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				<-start
				for j := 0; j < 30; j++ {
					own, err := AcquireLock(tasks, root, fmt.Sprint(n), LockExclusive)
					if err != nil {
						continue
					}
					acquired.Add(1)
					if active.Add(1) > 1 {
						overlap.Add(1)
					}
					time.Sleep(150 * time.Microsecond)
					active.Add(-1)
					own.Release()
				}
			}(w)
		}
		close(start)
		wg.Wait()
	}
	if overlap.Load() > 0 {
		t.Fatalf("chồng chủ=%d tổng=%d", overlap.Load(), acquired.Load())
	}
	if acquired.Load() == 0 {
		t.Fatal("không lượt nào lấy được khoá")
	}
}

// Chủ còn sống không bao giờ bị thay dù tệp chủ trông như của tiến trình chết (R1).
func TestLockLiveHolderNeverReclaimed(t *testing.T) {
	tasks := filepath.Join(t.TempDir(), "tasks")
	l := mustLock(t, tasks, "/repo", "sống", LockExclusive)
	if err := os.WriteFile(l.holderFile, []byte(`{"pid":2147483646}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		wantLocked(t, tasks, "/repo", LockExclusive)
		wantLocked(t, tasks, "/repo", LockShared)
	}
	l.Release()
	mustLock(t, tasks, "/repo", "mới", LockExclusive).Release()
}

// WaitLock: chờ tới khi chủ trả khoá, báo OnWait, rồi lấy được.
func TestWaitLockWaitsThenAcquires(t *testing.T) {
	oldMin, oldMax := lockPollMin, lockPollMax
	lockPollMin, lockPollMax = time.Millisecond, 5*time.Millisecond
	defer func() { lockPollMin, lockPollMax = oldMin, oldMax }()
	tasks := filepath.Join(t.TempDir(), "tasks")
	a := mustLock(t, tasks, "/repo", "A b-review", LockShared)
	var waits []string
	var mu sync.Mutex
	got := make(chan *Lock, 1)
	go func() {
		l, err := WaitLock(context.Background(), tasks, "/repo", "B b-feat", LockExclusive, WaitOptions{
			Timeout: 10 * time.Second,
			OnWait:  func(h string) { mu.Lock(); waits = append(waits, h); mu.Unlock() },
		})
		if err != nil {
			t.Error(err)
		}
		got <- l
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := len(waits)
		mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("OnWait không được gọi")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case <-got:
		t.Fatal("lấy được khoá khi chủ còn giữ")
	default:
	}
	a.Release()
	l := <-got
	if l == nil {
		t.Fatal("không lấy được khoá sau khi chủ trả")
	}
	l.Release()
	mu.Lock()
	defer mu.Unlock()
	if len(waits) != 1 || !strings.Contains(waits[0], "A b-review") {
		t.Fatalf("OnWait = %q", waits)
	}
}

func TestWaitLockTimeoutAndCancel(t *testing.T) {
	oldMin, oldMax := lockPollMin, lockPollMax
	lockPollMin, lockPollMax = time.Millisecond, 5*time.Millisecond
	defer func() { lockPollMin, lockPollMax = oldMin, oldMax }()
	tasks := filepath.Join(t.TempDir(), "tasks")
	x := mustLock(t, tasks, "/repo", "X", LockExclusive)
	defer x.Release()
	start := time.Now()
	_, err := WaitLock(context.Background(), tasks, "/repo", "y", LockShared, WaitOptions{Timeout: 40 * time.Millisecond})
	if !errors.Is(err, ErrLocked) || time.Since(start) < 30*time.Millisecond {
		t.Fatalf("hết hạn: err=%v sau %v", err, time.Since(start))
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	_, err = WaitLock(ctx, tasks, "/repo", "y", LockShared, WaitOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("huỷ ctx: %v", err)
	}
}

// Nhiều tiến trình thật tranh khoá hỗn hợp; vùng làm việc được đánh dấu bằng tệp: độc quyền
// tạo tệp bằng O_EXCL trong thư mục độc quyền, chia sẻ đếm tệp; hai tiến trình cùng vào
// vùng không tương thích thì thất bại.
func TestLockMixedAcrossProcesses(t *testing.T) {
	if os.Getenv("BINT_LOCK_CHILD") == "1" {
		lockChild()
		return
	}
	tasks := filepath.Join(t.TempDir(), "tasks")
	marks := t.TempDir()
	var cmds []*exec.Cmd
	var outs []*bytes.Buffer
	for i := 0; i < 6; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestLockMixedAcrossProcesses$")
		cmd.Env = append(os.Environ(), "BINT_LOCK_CHILD=1", "BINT_LOCK_TASKS="+tasks, "BINT_LOCK_MARKS="+marks,
			fmt.Sprintf("BINT_LOCK_KIND=%d", i%3))
		out := &bytes.Buffer{}
		cmd.Stdout, cmd.Stderr = out, out
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cmds, outs = append(cmds, cmd), append(outs, out)
	}
	for i, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Errorf("tiến trình %d: %v\n%s", i, err, outs[i])
		}
	}
}

// lockChild tranh khoá cho tới khi giữ được lockRounds lần (kind 0 = độc quyền, còn lại =
// chia sẻ). Nghỉ ngẫu nhiên sau mỗi lần thất bại để không đói lượt.
func lockChild() {
	const lockRounds = 20
	tasks, marks := os.Getenv("BINT_LOCK_TASKS"), os.Getenv("BINT_LOCK_MARKS")
	mode := LockShared
	if os.Getenv("BINT_LOCK_KIND") == "0" {
		mode = LockExclusive
	}
	exclusiveMark := filepath.Join(marks, "exclusive")
	sharedMark := filepath.Join(marks, fmt.Sprintf("shared-%d", os.Getpid()))
	got := 0
	deadline := time.Now().Add(30 * time.Second)
	for got < lockRounds && time.Now().Before(deadline) {
		l, err := AcquireLock(tasks, "/repo", "child", mode)
		if err != nil {
			if !errors.Is(err, ErrLocked) {
				fmt.Println("lỗi không mong đợi:", err)
				os.Exit(1)
			}
			time.Sleep(time.Duration(50+rand.IntN(450)) * time.Microsecond)
			continue
		}
		got++
		if mode == LockExclusive {
			if ents, _ := filepath.Glob(filepath.Join(marks, "shared-*")); len(ents) > 0 {
				fmt.Println("độc quyền cùng lúc với chia sẻ:", ents)
				os.Exit(1)
			}
			f, err := os.OpenFile(exclusiveMark, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err != nil {
				fmt.Println("hai tiến trình cùng giữ khoá độc quyền:", err)
				os.Exit(1)
			}
			f.Close()
			time.Sleep(200 * time.Microsecond)
			os.Remove(exclusiveMark)
		} else {
			if err := os.WriteFile(sharedMark, nil, 0o600); err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
			if _, err := os.Stat(exclusiveMark); err == nil {
				fmt.Println("chia sẻ cùng lúc với độc quyền")
				os.Exit(1)
			}
			time.Sleep(200 * time.Microsecond)
			os.Remove(sharedMark)
		}
		l.Release()
	}
	if got < lockRounds {
		fmt.Printf("chỉ lấy được khoá %d/%d lần trong 30 giây\n", got, lockRounds)
		os.Exit(1)
	}
	os.Exit(0)
}

// Tiến trình giữ khoá (chia sẻ hoặc độc quyền) bị kill thì hệ điều hành nhả khoá.
func TestLockReleasedWhenHolderProcessKilled(t *testing.T) {
	if os.Getenv("BINT_LOCK_HOLD") == "1" {
		mode := LockShared
		if os.Getenv("BINT_LOCK_KIND") == "x" {
			mode = LockExclusive
		}
		if _, err := AcquireLock(os.Getenv("BINT_LOCK_TASKS"), "/repo", "con", mode); err != nil {
			fmt.Println("LOI", err)
			os.Exit(1)
		}
		fmt.Println("HELD")
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	for _, kind := range []string{"s", "x"} {
		tasks := filepath.Join(t.TempDir(), "tasks")
		cmd := exec.Command(os.Args[0], "-test.run=^TestLockReleasedWhenHolderProcessKilled$")
		cmd.Env = append(os.Environ(), "BINT_LOCK_HOLD=1", "BINT_LOCK_TASKS="+tasks, "BINT_LOCK_KIND="+kind)
		out, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		line, _ := bufio.NewReader(out).ReadString('\n')
		if strings.TrimSpace(line) != "HELD" {
			cmd.Process.Kill()
			t.Fatalf("tiến trình con không giữ được khoá: %q", line)
		}
		wantLocked(t, tasks, "/repo", LockExclusive)
		if kind == "x" {
			wantLocked(t, tasks, "/repo", LockShared)
		}
		cmd.Process.Kill()
		cmd.Wait()
		l, err := AcquireLock(tasks, "/repo", "sau khi chết", LockExclusive)
		if err != nil {
			t.Fatalf("khoá (%s) của tiến trình chết không tự nhả: %v", kind, err)
		}
		l.Release()
	}
}
