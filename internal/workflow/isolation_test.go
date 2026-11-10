package workflow

import (
	"bytes"
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

// P5-01: tranh chấp đồng thời không được cho hai chủ cùng giữ khoá.
func TestAcquireWriteLockExclusiveUnderContention(t *testing.T) {
	tasks := t.TempDir() + "/tasks"
	var holders, maxHolders, acquired int32
	var wg sync.WaitGroup
	for g := 0; g < 32; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 60; i++ {
				l, err := AcquireWriteLock(tasks, "/repo", "t")
				if err != nil {
					continue
				}
				atomic.AddInt32(&acquired, 1)
				n := atomic.AddInt32(&holders, 1)
				for {
					m := atomic.LoadInt32(&maxHolders)
					if n <= m || atomic.CompareAndSwapInt32(&maxHolders, m, n) {
						break
					}
				}
				time.Sleep(100 * time.Microsecond)
				atomic.AddInt32(&holders, -1)
				l.Release()
			}
		}()
	}
	wg.Wait()
	if maxHolders > 1 || acquired == 0 {
		t.Fatalf("max holders = %d, acquired = %d", maxHolders, acquired)
	}
}

// Chủ cũ không được xoá khoá của chủ mới.
func TestWriteLockReleaseOnlyByOwner(t *testing.T) {
	tasks := t.TempDir() + "/tasks"
	a, err := AcquireWriteLock(tasks, "/repo", "a")
	if err != nil {
		t.Fatal(err)
	}
	// Giả lập khoá bị thay bằng khoá khác (nội dung khác).
	b := &WriteLock{path: a.path, content: "khác\n"}
	b.Release()
	if _, err := AcquireWriteLock(tasks, "/repo", "c"); err == nil {
		t.Fatal("Release của chủ không sở hữu đã xoá khoá")
	}
	a.Release()
	l, err := AcquireWriteLock(tasks, "/repo", "c")
	if err != nil {
		t.Fatal(err)
	}
	l.Release()
}

// P5-01: nhiều tiến trình thật tranh khoá; vùng làm việc được đánh dấu bằng tệp O_EXCL,
// nếu hai tiến trình cùng vào thì tạo tệp đánh dấu sẽ thất bại.
func TestAcquireWriteLockExclusiveAcrossProcesses(t *testing.T) {
	if os.Getenv("BINT_LOCK_CHILD") == "1" {
		lockChild()
		return
	}
	tasks := filepath.Join(t.TempDir(), "tasks")
	marker := filepath.Join(t.TempDir(), "inside")
	var cmds []*exec.Cmd
	var outs []*bytes.Buffer
	for i := 0; i < 6; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestAcquireWriteLockExclusiveAcrossProcesses$")
		cmd.Env = append(os.Environ(), "BINT_LOCK_CHILD=1", "BINT_LOCK_TASKS="+tasks, "BINT_LOCK_MARKER="+marker)
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

// lockChild tranh khoá cho tới khi giữ được lockRounds lần. AcquireWriteLock cố ý trả
// lỗi ngay khi khoá đang bị giữ (không chờ), nên tiến trình con phải nghỉ ngẫu nhiên
// sau mỗi lần thất bại: thử dồn dập một số lần cố định thì một tiến trình có thể hết
// lượt thử trong vài mili giây trong khi các tiến trình khác chuyền khoá cho nhau (đói
// lượt) — đó là nguyên nhân kiểm thử từng chập chờn, không phải lỗi của khoá.
func lockChild() {
	const lockRounds = 20
	tasks, marker := os.Getenv("BINT_LOCK_TASKS"), os.Getenv("BINT_LOCK_MARKER")
	got := 0
	deadline := time.Now().Add(30 * time.Second)
	for got < lockRounds && time.Now().Before(deadline) {
		l, err := AcquireWriteLock(tasks, "/repo", "child")
		if err != nil {
			if !errors.Is(err, ErrWriteLocked) {
				fmt.Println("lỗi không mong đợi:", err)
				os.Exit(1)
			}
			time.Sleep(time.Duration(50+rand.IntN(450)) * time.Microsecond)
			continue
		}
		got++
		f, err := os.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			fmt.Println("hai tiến trình cùng giữ khoá:", err)
			os.Exit(1)
		}
		f.Close()
		time.Sleep(200 * time.Microsecond)
		os.Remove(marker)
		l.Release()
	}
	if got < lockRounds {
		fmt.Printf("chỉ lấy được khoá %d/%d lần trong 30 giây\n", got, lockRounds)
		os.Exit(1)
	}
	os.Exit(0)
}

// Khoá của tiến trình đã chết bị thu hồi: hệ điều hành nhả khoá khi tiến trình thoát,
// tệp khoá (nội dung PID chết) còn lại không cản trở.
func TestAcquireWriteLockReclaimsDeadOwner(t *testing.T) {
	tasks := filepath.Join(t.TempDir(), "tasks")
	l, err := AcquireWriteLock(tasks, "/repo", "x")
	if err != nil {
		t.Fatal(err)
	}
	deadOwner(t, l)
	l2, err := AcquireWriteLock(tasks, "/repo", "y")
	if err != nil {
		t.Fatalf("không thu hồi được khoá bỏ lại: %v", err)
	}
	l2.Release()
}

// deadOwner giả lập chủ chết: nhả khoá hệ điều hành, để lại tệp mang PID chết.
func deadOwner(t *testing.T, l *WriteLock) {
	t.Helper()
	lines := strings.Split(l.content, "\n")
	lines[0] = "2147483646"
	l.f.Close()
	if err := os.WriteFile(l.path, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
}

// R1: chủ còn sống không bao giờ bị thay dù nội dung tệp trông như khoá bỏ lại.
func TestAcquireWriteLockLiveHolderNeverReclaimed(t *testing.T) {
	tasks := filepath.Join(t.TempDir(), "tasks")
	l, err := AcquireWriteLock(tasks, "/repo", "sống")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(l.content, "\n")
	lines[0] = "2147483646"
	os.WriteFile(l.path, []byte(strings.Join(lines, "\n")), 0o600)
	for i := 0; i < 20; i++ {
		if l2, err := AcquireWriteLock(tasks, "/repo", "kẻ cướp"); err == nil {
			l2.Release()
			t.Fatal("lấy được khoá đang có chủ sống")
		} else if !errors.Is(err, ErrWriteLocked) {
			t.Fatal(err)
		}
	}
	l.Release()
	l3, err := AcquireWriteLock(tasks, "/repo", "mới")
	if err != nil {
		t.Fatal(err)
	}
	l3.Release()
}

// R1 (probe reacceptance): nhiều reclaimer đồng thời trên khoá bỏ lại (PID chết) không
// được chồng chủ; chủ mới xuất hiện giữa lúc người khác đọc và thu hồi không bị gỡ.
func TestReacceptStaleReclamationExclusive(t *testing.T) {
	tasks := filepath.Join(t.TempDir(), "tasks")
	root := t.TempDir()
	var overlap, acquired atomic.Int64
	for round := 0; round < 24; round++ {
		l, err := AcquireWriteLock(tasks, root, "dead-owner")
		if err != nil {
			t.Fatal(err)
		}
		deadOwner(t, l)
		var active atomic.Int64
		var wg sync.WaitGroup
		start := make(chan struct{})
		for w := 0; w < 24; w++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				<-start
				for j := 0; j < 30; j++ {
					own, err := AcquireWriteLock(tasks, root, fmt.Sprint(n))
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
		t.Fatalf("thu hồi khoá bỏ lại vi phạm tính độc quyền: chồng chủ=%d tổng=%d", overlap.Load(), acquired.Load())
	}
	if acquired.Load() == 0 {
		t.Fatal("không lượt nào lấy được khoá")
	}
}
