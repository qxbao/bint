//go:build !windows

package iso

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"bint/internal/config"
)

// harnessEnv dựng store, profile và PATH chỉ có harness giả `<tool>` chạy script body (sh).
type harnessEnv struct {
	S    *Store
	P    Profile
	Real fakeReal
	Env  []string
	Dir  string // thư mục chứa tệp mà script ghi
}

func newHarness(t *testing.T, tool Tool, body string) harnessEnv {
	t.Helper()
	s := testStore(t, nil)
	real := newFakeReal(t)
	bin := filepath.Join(real.Dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(bin, string(tool)), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	env := append([]string{}, real.Env...)
	for i, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			env[i] = "PATH=" + bin + ":/usr/bin:/bin"
		}
	}
	out := t.TempDir()
	env = append(env, "OUT="+out)
	return harnessEnv{S: s, P: mustCreate(t, s, tool, "w"), Real: real, Env: env, Dir: out}
}

func (h harnessEnv) run(t *testing.T, args ...string) (int, error) {
	t.Helper()
	return Run(h.P, Spec{Args: args, Env: h.Env, Stdin: strings.NewReader(""), Stdout: os.Stderr, Stderr: os.Stderr, Wait: time.Second})
}

func (h harnessEnv) read(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(h.Dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func waitFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("hết hạn chờ %s", path)
}

func TestRunEnvArgsAndExitCode(t *testing.T) {
	h := newHarness(t, Codex, `{ echo "HOME=$HOME"; echo "TMPDIR=$TMPDIR"; echo "CODEX_HOME=$CODEX_HOME"; echo "STATE=$BINT_STATE_DIR";
  [ -d "$TMPDIR" ] && echo TMP_EXISTS; for a in "$@"; do echo "ARG:$a"; done; } > "$OUT/env.txt"; exit 3`)
	code, err := h.run(t, "exec", "--", "-c", "a b", "--flag=x", "")
	if err != nil || code != 3 {
		t.Fatalf("code=%d err=%v, muốn 3 và không lỗi", code, err)
	}
	got := h.read(t, "env.txt")
	tmp := h.P.tempDirFrom(h.Env)
	for _, want := range []string{
		"HOME=" + h.P.Home, "TMPDIR=" + tmp, "CODEX_HOME=" + filepath.Join(h.P.Home, ".codex"), "STATE=" + h.Real.State,
		"TMP_EXISTS", "ARG:exec\nARG:--\nARG:-c\nARG:a b\nARG:--flag=x\nARG:\n",
	} {
		if !strings.Contains(got+"\n", want) {
			t.Errorf("thiếu %q trong:\n%s", want, got)
		}
	}
	if _, err := os.Lstat(tmp); !os.IsNotExist(err) {
		t.Fatalf("phiên cuối thoát thì temp phải biến mất: %v", err)
	}
}

func TestRunExitCodeFromSignalDeath(t *testing.T) {
	h := newHarness(t, Claude, `kill -TERM $$; sleep 5`)
	if code, err := h.run(t); err != nil || code != 128+int(syscall.SIGTERM) {
		t.Fatalf("code=%d err=%v, muốn 143", code, err)
	}
	h = newHarness(t, Claude, `exit 0`)
	if code, err := h.run(t); err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
}

func TestRunMissingBinaryAndStoreBinarySkipped(t *testing.T) {
	h := newHarness(t, Codex, `echo REAL > "$OUT/who"`)
	// Bản trong store đứng trước trên PATH phải bị bỏ qua.
	storeBin := filepath.Join(h.S.Root, "bin")
	if err := os.MkdirAll(storeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storeBin, "codex"), []byte("#!/bin/sh\necho STORE > \"$OUT/who\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Thư mục ngoài store nhưng bin/codex là symlink vào store cũng bị bỏ qua.
	linkDir := filepath.Join(h.Real.Dir, "linkbin")
	if err := os.MkdirAll(linkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(storeBin, "codex"), filepath.Join(linkDir, "codex")); err != nil {
		t.Fatal(err)
	}
	for i, e := range h.Env {
		if strings.HasPrefix(e, "PATH=") {
			h.Env[i] = "PATH=" + storeBin + ":" + linkDir + ":" + strings.TrimPrefix(e, "PATH=")
		}
	}
	if code, err := h.run(t); err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if got := strings.TrimSpace(h.read(t, "who")); got != "REAL" {
		t.Fatalf("chạy nhầm bản %q, muốn REAL", got)
	}

	if _, err := LookHarness(Claude, []string{"PATH=" + storeBin + ":" + linkDir}, h.S.Root); err == nil || !strings.Contains(err.Error(), "claude") {
		t.Fatalf("không có claude ngoài store: err = %v", err)
	}
	// PATH tương đối bị bỏ qua.
	if _, err := LookHarness(Codex, []string{"PATH=bin:."}, h.S.Root); err == nil {
		t.Fatal("PATH tương đối không được dùng")
	}
}

func TestRunForwardsSIGTERMAndSwallowsSIGINT(t *testing.T) {
	h := newHarness(t, Codex, `trap 'echo got-term > "$OUT/term"; exit 7' TERM
trap 'echo got-int > "$OUT/int"' INT
echo ready > "$OUT/ready"
while [ ! -e "$OUT/stop" ]; do sleep 0.05; done
exit 9`)
	type res struct {
		code int
		err  error
	}
	done := make(chan res, 1)
	go func() {
		c, e := h.run(t)
		done <- res{c, e}
	}()
	waitFile(t, filepath.Join(h.Dir, "ready"))

	// SIGINT tới riêng bint: bint không được thoát trước con và không được chuyển cho con.
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-done:
		t.Fatalf("Run thoát sau SIGINT trong khi con còn chạy: %+v", r)
	case <-time.After(300 * time.Millisecond):
	}
	if _, err := os.Stat(filepath.Join(h.Dir, "int")); err == nil {
		t.Fatal("SIGINT gửi riêng cho bint không được chuyển cho con (con tự nhận từ terminal)")
	}

	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-done:
		if r.err != nil || r.code != 7 {
			t.Fatalf("code=%d err=%v, muốn 7 (con bắt SIGTERM)", r.code, r.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("SIGTERM không được chuyển cho con")
	}
	if _, err := os.Stat(filepath.Join(h.Dir, "term")); err != nil {
		t.Fatal("con không nhận SIGTERM")
	}
}

func TestTwoSessionsLastOneRemovesTemp(t *testing.T) {
	h := newHarness(t, Codex, `touch "$OUT/started.$1"; while [ ! -e "$OUT/release.$1" ]; do sleep 0.05; done`)
	tmp := h.P.tempDirFrom(h.Env)
	done := map[string]chan int{"A": make(chan int, 1), "B": make(chan int, 1)}
	for id, ch := range done {
		go func() {
			code, err := h.run(t, id)
			if err != nil {
				code = -1
			}
			ch <- code
		}()
	}
	waitFile(t, filepath.Join(h.Dir, "started.A"))
	waitFile(t, filepath.Join(h.Dir, "started.B"))

	// A thoát trước (Run của A đã trả về, gồm cả finishSession): temp còn vì B vẫn dùng.
	if err := os.WriteFile(filepath.Join(h.Dir, "release.A"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if c := <-done["A"]; c != 0 {
		t.Fatalf("mã thoát của A = %d", c)
	}
	if _, err := os.Lstat(tmp); err != nil {
		t.Fatalf("phiên thoát trước phải để nguyên temp: %v", err)
	}
	// B thoát sau: temp biến mất.
	if err := os.WriteFile(filepath.Join(h.Dir, "release.B"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if c := <-done["B"]; c != 0 {
		t.Fatalf("mã thoát của B = %d", c)
	}
	if _, err := os.Lstat(tmp); !os.IsNotExist(err) {
		t.Fatalf("phiên cuối thoát thì temp phải biến mất: %v", err)
	}
}

func TestRunRefusesWhileExclusiveHeld(t *testing.T) {
	h := newHarness(t, Codex, `exit 0`)
	rel, busy, err := h.P.Lock(true)
	if err != nil || busy {
		t.Fatal(err, busy)
	}
	defer rel()
	_, err = Run(h.P, Spec{Env: h.Env, Wait: 300 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), ErrBusy.Error()) {
		t.Fatalf("err = %v, muốn ErrBusy", err)
	}
}

func TestRunWarnsOnShareConflictAndCreatesTempBeforeSpawn(t *testing.T) {
	h := newHarness(t, Codex, `[ -d "$TMPDIR" ] && echo yes > "$OUT/tmp"`)
	// Thư mục .cache có dữ liệu: xung đột, chạy vẫn tiếp tục và có cảnh báo.
	cache := filepath.Join(h.P.Home, ".cache")
	if err := os.MkdirAll(cache, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "x"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	var logs []string
	code, err := Run(h.P, Spec{Env: h.Env, Wait: time.Second, Logf: func(f string, a ...any) { logs = append(logs, f) }})
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if len(logs) == 0 {
		t.Fatal("phải cảnh báo liên kết chia sẻ xung đột")
	}
	if strings.TrimSpace(h.read(t, "tmp")) != "yes" {
		t.Fatal("TMPDIR phải tồn tại trước khi spawn")
	}
}

// bint bị kill -9: khoá S mất nhưng harness con còn chạy. Hồ sơ runs/ giữ profile bận cho tới khi con thoát.
func TestOrphanHarnessKeepsProfileBusy(t *testing.T) {
	s := testStore(t, func(c *config.Iso) { c.GCInterval = "0s" })
	real := newFakeReal(t)
	p := mustCreate(t, s, Codex, "w")
	env := append([]string{}, real.Env...)

	sess, err := Begin(p, env, nil)
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command("sleep", "60")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	reaped := false
	defer func() {
		if !reaped {
			_ = child.Process.Kill()
			_, _ = child.Process.Wait()
		}
	}()
	if err := sess.Track(child.Process.Pid); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(sess.Temp, "dang-dung")
	if err := os.WriteFile(marker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Giả lập bint chết: khoá S mất, hồ sơ runs/ còn.
	if err := sess.release(); err != nil {
		t.Fatal(err)
	}

	rep, err := GC(s, GCOptions{Env: env})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Actions) != 1 || rep.Actions[0].Kind != GCBusy {
		t.Fatalf("gc phải bỏ qua profile: %+v", rep.Actions)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("gc xoá TMPDIR của harness còn chạy: %v", err)
	}
	if err := RemoveProfile(p); !errors.Is(err, ErrBusy) {
		t.Fatalf("RemoveProfile = %v, muốn ErrBusy", err)
	}
	if active, err := p.SessionActive(); err != nil || !active {
		t.Fatalf("SessionActive = %v %v, muốn true", active, err)
	}
	// Phiên khác cùng profile bắt đầu rồi kết thúc: phiên đó không phải phiên cuối, TMPDIR còn.
	other, err := Begin(p, env, nil)
	if err != nil {
		t.Fatal(err)
	}
	other.End()
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("phiên khác kết thúc đã xoá TMPDIR của harness còn chạy: %v", err)
	}
	if _, err := os.Stat(p.Home); err != nil {
		t.Fatal(err)
	}

	// Harness thoát: gc xoá TMPDIR và hồ sơ.
	_ = child.Process.Kill()
	_, _ = child.Process.Wait()
	reaped = true
	if _, err := GC(s, GCOptions{Env: env}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sess.Temp); !os.IsNotExist(err) {
		t.Fatalf("harness đã thoát: TMPDIR phải bị xoá: %v", err)
	}
	if _, err := os.Stat(p.runPath(child.Process.Pid)); !os.IsNotExist(err) {
		t.Fatalf("hồ sơ của tiến trình đã chết phải bị xoá: %v", err)
	}
	sess.untrack()
}

// Hồ sơ của tiến trình đã chết (hoặc pid bị dùng lại: start_ticks lệch) không làm profile bận và bị xoá.
func TestLockDropsDeadRunRecord(t *testing.T) {
	s := testStore(t, nil)
	p := mustCreate(t, s, Codex, "w")
	if err := os.MkdirAll(p.RunsDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name string, rec runRecord) string {
		data, _ := json.Marshal(rec)
		path := filepath.Join(p.RunsDir(), name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	dead := exec.Command("true")
	if err := dead.Run(); err != nil {
		t.Fatal(err)
	}
	files := []string{
		write("dead.json", runRecord{PID: dead.Process.Pid, BintPID: 1}),
		write("garbage.json", runRecord{}), // pid 0
	}
	if err := os.WriteFile(filepath.Join(p.RunsDir(), "broken.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	files = append(files, filepath.Join(p.RunsDir(), "broken.json"))
	if ticks, ok := procStartTicks(os.Getpid()); ok {
		files = append(files, write("reused.json", runRecord{PID: os.Getpid(), StartTicks: ticks + 1}))
	}
	rel, busy, err := p.Lock(true)
	if err != nil || busy {
		t.Fatalf("hồ sơ của tiến trình đã chết không được làm bận: busy=%v err=%v", busy, err)
	}
	rel()
	for _, f := range files {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("%s phải bị xoá: %v", f, err)
		}
	}
	// Tiến trình đang sống (chính test này) thì bận, hồ sơ còn.
	live := write("live.json", runRecord{PID: os.Getpid()})
	if _, busy, err := p.Lock(true); err != nil || !busy {
		t.Fatalf("hồ sơ của tiến trình sống phải bận: busy=%v err=%v", busy, err)
	}
	if _, err := os.Stat(live); err != nil {
		t.Fatalf("hồ sơ sống không được xoá: %v", err)
	}
}

// Run ghi hồ sơ trong lúc harness chạy và xoá khi kết thúc.
func TestRunTracksHarness(t *testing.T) {
	h := newHarness(t, Codex, `for i in $(seq 200); do ls "$BINT_ISO_HOME/profiles/codex--w/runs" > "$OUT/runs.txt" 2>/dev/null && [ -s "$OUT/runs.txt" ] && break; sleep 0.05; done`)
	if code, err := h.run(t); err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if got := strings.TrimSpace(h.read(t, "runs.txt")); !strings.HasSuffix(got, ".json") {
		t.Fatalf("lúc chạy phải có hồ sơ runs/<pid>.json, ls = %q", got)
	}
	if ents, _ := os.ReadDir(h.P.RunsDir()); len(ents) != 0 {
		t.Fatalf("hết phiên hồ sơ phải bị xoá: %v", ents)
	}
}
