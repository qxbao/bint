package workflow

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

func lockChild() {
	tasks, marker := os.Getenv("BINT_LOCK_TASKS"), os.Getenv("BINT_LOCK_MARKER")
	got := 0
	for i := 0; i < 300; i++ {
		l, err := AcquireWriteLock(tasks, "/repo", "child")
		if err != nil {
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
	if got == 0 {
		fmt.Println("không lấy được khoá lần nào")
		os.Exit(1)
	}
	os.Exit(0)
}

// Khoá của tiến trình đã chết bị thu hồi.
func TestAcquireWriteLockReclaimsDeadOwner(t *testing.T) {
	tasks := filepath.Join(t.TempDir(), "tasks")
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Run() // tiến trình đã thoát → PID chết
	dead := cmd.Process.Pid
	l, err := AcquireWriteLock(tasks, "/repo", "x")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(l.path)
	lines := strings.Split(string(b), "\n")
	lines[0] = strconv.Itoa(dead)
	os.WriteFile(l.path, []byte(strings.Join(lines, "\n")), 0o600)
	l2, err := AcquireWriteLock(tasks, "/repo", "y")
	if err != nil {
		t.Fatalf("không thu hồi được khoá bỏ lại: %v", err)
	}
	l2.Release()
}
