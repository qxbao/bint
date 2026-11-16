//go:build !windows

package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Harness giả chờ tới khi $OUT/release.<id> xuất hiện (id = đối số đầu), báo $OUT/started.<id>.
const waitingBody = `id="$1"
trap 'echo term > "$OUT/term.$id"; exit 7' TERM
trap 'echo int > "$OUT/int.$id"' INT
touch "$OUT/started.$id"
while [ ! -e "$OUT/release.$id" ]; do sleep 0.05; done
exit 0`

func isoWaitFor(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("hết hạn chờ %s", path)
}

func isoRelease(t *testing.T, e isoEnv, id string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(e.Out, "release."+id), nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func tempDirOf(e isoEnv, tool, name string) string {
	return filepath.Join(e.Tmp, "bint-iso-"+itoa(os.Getuid()), tool+"--"+name)
}

func TestIsoSignalSIGTERMForwardedAndSIGINTSwallowed(t *testing.T) {
	e := newIsoEnv(t, map[string]string{"claude": waitingBody})
	dir := t.TempDir()
	run(t, dir, "", "iso", "create", "claude", "--name", "w")
	b := startBint(t, dir, "iso", "run", "claude", "--name", "w", "--", "A")
	isoWaitFor(t, filepath.Join(e.Out, "started.A"))

	// SIGINT tới riêng bint: bint sống tiếp, con không bị chạm.
	if err := b.cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)
	if err := b.cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("bint thoát sau SIGINT trong khi con còn chạy: %v\n%s", err, b.out)
	}
	if _, err := os.Stat(filepath.Join(e.Out, "int.A")); err == nil {
		t.Fatal("SIGINT gửi riêng cho bint không được chuyển cho con")
	}

	// SIGTERM: chuyển cho con; con thoát 7 ⇒ bint thoát 7 và không in lỗi.
	if err := b.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if code := b.wait(t); code != 7 {
		t.Fatalf("code %d, muốn 7\n%s", code, b.out)
	}
	if _, err := os.Stat(filepath.Join(e.Out, "term.A")); err != nil {
		t.Fatal("con không nhận SIGTERM")
	}
	if strings.Contains(b.out.String(), "lỗi:") {
		t.Fatalf("không được in lỗi:\n%s", b.out)
	}
	if _, err := os.Stat(tempDirOf(e, "claude", "w")); !os.IsNotExist(err) {
		t.Fatalf("hết phiên thì temp phải biến mất: %v", err)
	}
}

func TestIsoSignalChildKilledBySignalGives128Plus(t *testing.T) {
	e := newIsoEnv(t, map[string]string{"claude": `kill -HUP $$; sleep 5`})
	_ = e
	dir := t.TempDir()
	run(t, dir, "", "iso", "create", "claude", "--name", "w")
	out, code := run(t, dir, "", "iso", "run", "claude", "--name", "w")
	if code != 128+int(syscall.SIGHUP) || strings.Contains(out, "lỗi:") {
		t.Fatalf("code %d, muốn %d và không in lỗi\n%s", code, 128+int(syscall.SIGHUP), out)
	}
}

func TestIsoTwoSessionsLastRemovesTemp(t *testing.T) {
	e := newIsoEnv(t, map[string]string{"claude": waitingBody})
	dir := t.TempDir()
	run(t, dir, "", "iso", "create", "claude", "--name", "w")
	a := startBint(t, dir, "iso", "run", "claude", "--name", "w", "--", "A")
	b := startBint(t, dir, "iso", "run", "claude", "--name", "w", "--", "B")
	isoWaitFor(t, filepath.Join(e.Out, "started.A"))
	isoWaitFor(t, filepath.Join(e.Out, "started.B"))
	tmp := tempDirOf(e, "claude", "w")
	if _, err := os.Stat(tmp); err != nil {
		t.Fatalf("temp phải có khi chạy: %v", err)
	}

	// status thấy phiên đang chạy; gc bỏ qua profile đang có phiên giữ khoá S.
	if out, code := run(t, dir, "", "iso", "status"); code != 0 || !strings.Contains(out, "đang chạy") {
		t.Fatalf("status: %d\n%s", code, out)
	}
	if out, code := run(t, dir, "", "iso", "gc"); code != 0 || !strings.Contains(out, "bỏ qua (đang có phiên)") {
		t.Fatalf("gc: %d\n%s", code, out)
	}
	if _, err := os.Stat(tmp); err != nil {
		t.Fatalf("gc xoá temp của profile đang có phiên: %v", err)
	}
	if out, code := run(t, dir, "", "iso", "remove", "claude", "--name", "w", "--yes"); code == 0 || !strings.Contains(out, "đang có phiên") {
		t.Fatalf("remove khi có phiên: %d\n%s", code, out)
	}

	isoRelease(t, e, "A")
	if code := a.wait(t); code != 0 {
		t.Fatalf("A: %d\n%s", code, a.out)
	}
	if _, err := os.Stat(tmp); err != nil {
		t.Fatalf("phiên thoát trước phải để nguyên temp: %v", err)
	}
	isoRelease(t, e, "B")
	if code := b.wait(t); code != 0 {
		t.Fatalf("B: %d\n%s", code, b.out)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatalf("phiên cuối thoát thì temp phải biến mất: %v", err)
	}
}

func TestIsoKillParentLeavesTempAndGCCleans(t *testing.T) {
	e := newIsoEnv(t, map[string]string{"claude": waitingBody})
	dir := t.TempDir()
	run(t, dir, "", "iso", "create", "claude", "--name", "w")
	a := startBint(t, dir, "iso", "run", "claude", "--name", "w", "--", "K")
	isoWaitFor(t, filepath.Join(e.Out, "started.K"))
	tmp := tempDirOf(e, "claude", "w")
	if err := os.WriteFile(filepath.Join(tmp, "scratch"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := a.cmd.Process.Kill(); err != nil { // SIGKILL: bint không kịp dọn
		t.Fatal(err)
	}
	isoRelease(t, e, "K") // con (mồ côi) thoát; phải làm trước Wait vì nó còn giữ đầu ghi của pipe stdout
	_ = a.cmd.Wait()
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(tmp); err != nil {
		t.Fatalf("kill -9 cha phải để lại temp: %v", err)
	}

	out, code := run(t, dir, "", "iso", "gc", "--dry-run")
	if code != 0 || !strings.Contains(out, "sẽ xoá") || !strings.Contains(out, tmp) {
		t.Fatalf("gc --dry-run: %d\n%s", code, out)
	}
	if _, err := os.Stat(tmp); err != nil {
		t.Fatalf("--dry-run đã xoá temp: %v", err)
	}
	if out, code := run(t, dir, "", "iso", "gc", "--profile", "claude/other"); code != 0 {
		t.Fatalf("gc --profile khác: %d\n%s", code, out)
	}
	if _, err := os.Stat(tmp); err != nil {
		t.Fatal("gc --profile profile khác không được đụng temp này")
	}
	if out, code := run(t, dir, "", "iso", "gc"); code != 0 || !strings.Contains(out, "xoá") {
		t.Fatalf("gc: %d\n%s", code, out)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatalf("gc phải xoá temp mồ côi: %v", err)
	}
	if out, code := run(t, dir, "", "iso", "gc", "--profile", "bad"); code != 2 {
		t.Fatalf("--profile sai: %d\n%s", code, out)
	}
}
