package evolve

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"bint/internal/fslock"
)

const (
	lockHelperEnv     = "BINT_TEST_EVOLVE_LOCK_DIR"
	lockHelperModeEnv = "BINT_TEST_EVOLVE_LOCK_MODE" // "" = tranh khoá nhiều lần; "hold" = giữ khoá tới khi bị kill
	lockHelperLimit   = 30 * time.Second             // hạn của tiến trình con
	lockParentLimit   = 90 * time.Second             // hạn cha chờ con (đủ cho -race)
)

// holdLockHelper: lấy khoá, báo "HELD <pid>" rồi giữ cho tới khi stdin đóng, bị kill hoặc quá hạn.
func holdLockHelper(dir string) {
	if _, err := AcquireLock(dir, "tiến trình con giữ khoá"); err != nil {
		fmt.Printf("RESULT error %v\n", err)
		return
	}
	fmt.Printf("HELD %d\n", os.Getpid())
	time.AfterFunc(lockHelperLimit, func() { os.Exit(3) })
	io.Copy(io.Discard, os.Stdin)
}

// TestEvolveLockHelperProcess là tiến trình con của TestAcquireLockExclusiveAcrossProcesses (tự gọi
// lại binary test). Nó lấy khoá nhiều lần; trong vùng găng nó tạo tệp "inside" bằng O_EXCL —
// tệp đã có nghĩa là một tiến trình khác cũng đang ở trong vùng găng (khoá không loại trừ).
func TestEvolveLockHelperProcess(t *testing.T) {
	dir := os.Getenv(lockHelperEnv)
	if dir == "" {
		t.Skip("chỉ chạy như tiến trình con")
	}
	if os.Getenv(lockHelperModeEnv) == "hold" {
		holdLockHelper(dir)
		return
	}
	iters, _ := strconv.Atoi(os.Getenv("BINT_TEST_EVOLVE_LOCK_ITERS"))
	deadline := time.Now().Add(lockHelperLimit)
	var got, overlap int
	for got < iters {
		if time.Now().After(deadline) {
			fmt.Printf("RESULT timeout %d\n", got)
			return
		}
		l, err := AcquireLock(dir, fmt.Sprintf("pid%d", os.Getpid()))
		if err != nil {
			if !errors.Is(err, ErrLocked) {
				fmt.Printf("RESULT error %v\n", err)
				return
			}
			time.Sleep(time.Millisecond)
			continue
		}
		got++
		marker := filepath.Join(dir, "inside")
		m, err := os.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			overlap++
		} else {
			m.Close()
			time.Sleep(200 * time.Microsecond)
			os.Remove(marker)
		}
		l.Release()
	}
	fmt.Printf("RESULT ok %d %d\n", got, overlap)
}

// Nhiều TIẾN TRÌNH tranh khoá: không lần nào hai tiến trình cùng ở trong vùng găng (P6-01).
func TestAcquireLockExclusiveAcrossProcesses(t *testing.T) {
	dir := t.TempDir()
	const procs, iters = 5, 60
	var wg sync.WaitGroup
	outs := make([]string, procs)
	for i := 0; i < procs; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), lockParentLimit)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestEvolveLockHelperProcess$")
			cmd.Env = append(os.Environ(), lockHelperEnv+"="+dir, "BINT_TEST_EVOLVE_LOCK_ITERS="+strconv.Itoa(iters))
			var b bytes.Buffer
			cmd.Stdout, cmd.Stderr = &b, &b
			if err := cmd.Run(); err != nil {
				b.WriteString("\nrun: " + err.Error())
				if ctx.Err() != nil {
					b.WriteString(" (quá hạn của cha)")
				}
			}
			outs[i] = b.String()
		}(i)
	}
	wg.Wait()
	total := 0
	for i, o := range outs {
		var got, overlap int
		idx := strings.Index(o, "RESULT ok ")
		if idx < 0 {
			t.Fatalf("tiến trình %d không báo kết quả:\n%s", i, o)
		}
		fmt.Sscanf(o[idx:], "RESULT ok %d %d", &got, &overlap)
		if overlap > 0 {
			t.Fatalf("tiến trình %d thấy %d lần khoá không loại trừ được:\n%s", i, overlap, o)
		}
		total += got
	}
	if total != procs*iters {
		t.Fatalf("lấy khoá %d lần, cần %d", total, procs*iters)
	}
}

// Nhiều goroutine (mỗi lần AcquireLock mở tệp riêng) cũng phải loại trừ nhau. Test phải thật sự lấy
// được khoá (nếu mọi lần đều lỗi thì "không chồng lấn" là vô nghĩa), và mọi lỗi khác ErrLocked là lỗi.
func TestAcquireLockExclusiveGoroutines(t *testing.T) {
	root := t.TempDir()
	var active, overlap, acquired atomic.Int64
	start := make(chan struct{})
	var wg sync.WaitGroup
	for n := 0; n < 16; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			for j := 0; j < 40; j++ {
				l, err := AcquireLock(root, fmt.Sprint(n))
				if err != nil {
					if !errors.Is(err, ErrLocked) {
						t.Errorf("goroutine %d: lỗi khác ErrLocked: %v", n, err)
						return
					}
					continue
				}
				acquired.Add(1)
				if active.Add(1) > 1 {
					overlap.Add(1)
				}
				time.Sleep(100 * time.Microsecond)
				active.Add(-1)
				l.Release()
			}
		}(n)
	}
	close(start)
	wg.Wait()
	if acquired.Load() == 0 {
		t.Fatal("không goroutine nào lấy được khoá: test không kiểm được gì")
	}
	if overlap.Load() > 0 {
		t.Fatalf("khoá không loại trừ: %d lần chồng lấn trên %d lần lấy", overlap.Load(), acquired.Load())
	}
}

// Tiến trình đang giữ khoá bị kill (không kịp Release): hệ điều hành nhả khoá, lấy lại được; trong lúc
// còn sống thì ErrLocked nêu đúng PID của nó; tệp khoá không bị xoá. Không dùng syscall riêng nền tảng.
func TestEvolveLockKilledHolderReleases(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), lockParentLimit)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestEvolveLockHelperProcess$")
	cmd.Env = append(os.Environ(), lockHelperEnv+"="+dir, lockHelperModeEnv+"=hold")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	killed := false
	defer func() {
		if !killed {
			cmd.Process.Kill()
			cmd.Wait()
		}
		stdin.Close()
	}()
	lines := make(chan string, 8)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	var held string
	select {
	case held = <-lines:
	case <-time.After(lockHelperLimit):
		t.Fatal("tiến trình con không báo đã giữ khoá trong hạn")
	}
	if !strings.HasPrefix(held, "HELD ") {
		t.Fatalf("tiến trình con không giữ được khoá: %q", held)
	}
	childPID := strings.TrimPrefix(held, "HELD ")

	_, err = AcquireLock(dir, "người đến sau")
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("khoá đang bị giữ phải ErrLocked, được %v", err)
	}
	if !strings.Contains(err.Error(), childPID) {
		t.Fatalf("ErrLocked phải nêu PID %s của chủ: %v", childPID, err)
	}

	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	killed = true
	cmd.Wait() // lỗi "signal: killed" là bình thường

	// Hệ điều hành nhả khoá khi tiến trình chết; cho một khoảng ngắn phòng trễ.
	var l *Lock
	deadline := time.Now().Add(5 * time.Second)
	for {
		l, err = AcquireLock(dir, "sau khi con chết")
		if err == nil || !errors.Is(err, ErrLocked) || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("khoá của tiến trình đã bị kill phải lấy lại được: %v", err)
	}
	l.Release()
	if _, err := os.Stat(filepath.Join(dir, "locks", "evolve.lock")); err != nil {
		t.Fatalf("tệp khoá phải còn: %v", err)
	}
}

// Release xoá thông tin chủ khoá (review .4): tệp khoá rỗng sau khi trả.
func TestLockReleaseClearsHolderInfo(t *testing.T) {
	rt := t.TempDir()
	lockFile := filepath.Join(rt, "locks", "evolve.lock")
	l, err := AcquireLock(rt, "chủ cũ")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(lockFile); !strings.Contains(string(b), "chủ cũ") {
		t.Fatalf("khi đang giữ, tệp khoá phải ghi chủ: %q", b)
	}
	l.Release()
	if b, err := os.ReadFile(lockFile); err != nil || len(b) != 0 {
		t.Fatalf("sau Release tệp khoá phải còn và rỗng: %q %v", b, err)
	}
}

// Chủ mới vừa lấy khoá nhưng chưa kịp ghi thông tin của mình: người đến sau không được thấy chủ cũ.
func TestLockBusyMessageOmitsPreviousHolder(t *testing.T) {
	rt := t.TempDir()
	lockFile := filepath.Join(rt, "locks", "evolve.lock")
	l, err := AcquireLock(rt, "chủ cũ")
	if err != nil {
		t.Fatal(err)
	}
	l.Release()
	f, err := os.OpenFile(lockFile, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if busy, err := fslock.Try(f, true); err != nil || busy {
		t.Fatalf("chủ mới phải lấy được khoá: busy=%v err=%v", busy, err)
	}
	_, err = AcquireLock(rt, "người đến sau")
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("phải ErrLocked, được %v", err)
	}
	if strings.Contains(err.Error(), "chủ cũ") || strings.Contains(err.Error(), strconv.Itoa(os.Getpid())) {
		t.Fatalf("thông báo bận không được nêu chủ cũ: %v", err)
	}
}

// Tệp khoá còn lại của PID đã chết (hoặc nội dung rác) không chặn; khoá đang giữ trả ErrLocked
// kèm chủ; Release không xoá tệp và nhả khoá.
func TestAcquireLockStaleFileAndBusy(t *testing.T) {
	rt := t.TempDir()
	lockFile := filepath.Join(rt, "locks", "evolve.lock")
	if err := os.MkdirAll(filepath.Dir(lockFile), 0o700); err != nil {
		t.Fatal(err)
	}
	// PID rất lớn: chắc chắn không có tiến trình nào.
	if err := os.WriteFile(lockFile, []byte("2147483646\n2020-01-01T00:00:00Z\nbint evolve apply cũ\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := AcquireLock(rt, "chủ mới")
	if err != nil {
		t.Fatalf("tệp khoá của PID chết phải lấy được: %v", err)
	}
	_, err = AcquireLock(rt, "người đến sau")
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("phải ErrLocked, được %v", err)
	}
	if !strings.Contains(err.Error(), strconv.Itoa(os.Getpid())) || !strings.Contains(err.Error(), "chủ mới") {
		t.Fatalf("thiếu thông tin chủ khoá: %v", err)
	}
	l.Release()
	l.Release() // gọi lặp không sao
	if _, err := os.Stat(lockFile); err != nil {
		t.Fatalf("Release không được xoá tệp khoá: %v", err)
	}
	l2, err := AcquireLock(rt, "sau khi trả")
	if err != nil {
		t.Fatalf("sau Release phải lấy được: %v", err)
	}
	l2.Release()
	// Tệp khoá rỗng/rác cũng không chặn.
	os.WriteFile(lockFile, []byte("rác"), 0o600)
	l3, err := AcquireLock(rt, "x")
	if err != nil {
		t.Fatal(err)
	}
	l3.Release()
}
