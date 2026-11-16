package iso

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"bint/internal/fsafe"
	"bint/internal/pathutil"
)

// ErrBusy: profile đang được dùng (khoá phiên không lấy được); xem Run, RemoveProfile.
var ErrBusy = errors.New("profile đang có phiên chạy")

// DefaultSessionWait là thời gian chờ khoá S khi gc/remove đang giữ X.
const DefaultSessionWait = 10 * time.Second

// Spec là một lần chạy harness trong profile.
type Spec struct {
	Args   []string // đối số của harness, giữ nguyên
	Env    []string // env gốc (mặc định os.Environ()), đưa qua Env(p, …)
	Dir    string   // thư mục làm việc ("" = thư mục hiện tại)
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// Wait là thời gian chờ khoá S khi gc/remove đang giữ X (mặc định DefaultSessionWait).
	Wait time.Duration
	// Logf nhận cảnh báo không chặn (liên kết chia sẻ xung đột, dọn temp lỗi). Có thể nil.
	Logf func(format string, args ...any)
}

func (s Spec) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}

// Run chạy harness của profile p theo docs/iso-plan.md mục 5.3 và trả mã thoát của con (Unix: 128+n khi con chết
// vì tín hiệu n). Chỉ trả error khi không chạy được (không có binary, không lấy được khoá, lỗi dựng env…).
//
// Thứ tự: tìm binary trên PATH thật (bỏ thư mục trong store) → khoá S (chia sẻ, chờ có giới hạn) → Env →
// tạo TMPDIR (lấy từ chính env đã dựng, một nguồn) → kiểm liên kết chia sẻ → spawn → chờ → nhả S → thử X và dọn
// temp. bint là tiến trình cha, không exec thay thế (D10): hai phiên cùng profile dùng chung thư mục temp và
// chỉ phiên cuối cùng xoá nó. Tín hiệu: xem notifySignals (signal_unix.go).
func Run(p Profile, spec Spec) (int, error) {
	base := spec.Env
	if base == nil {
		base = os.Environ()
	}
	bin, err := LookHarness(p.Tool, base, p.StoreRoot)
	if err != nil {
		return 0, err
	}
	sess, err := begin(p, base, spec.Wait, spec.Logf)
	if err != nil {
		return 0, err
	}
	defer sess.End() // idempotent: nhả sớm bên dưới để dọn temp lấy được X
	env := sess.Env

	cmd := exec.Command(bin, spec.Args...)
	cmd.Env = env
	cmd.Dir = spec.Dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = spec.Stdin, spec.Stdout, spec.Stderr
	if cmd.Stdin == nil {
		cmd.Stdin = os.Stdin
	}
	if cmd.Stdout == nil {
		cmd.Stdout = os.Stdout
	}
	if cmd.Stderr == nil {
		cmd.Stderr = os.Stderr
	}
	attach, stop := notifySignals()
	defer stop()
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("không chạy được %s: %w", bin, err)
	}
	attach(cmd.Process)
	if err := sess.Track(cmd.Process.Pid); err != nil {
		spec.logf("không ghi được hồ sơ harness của %s: %v", p.ID(), err)
	}
	werr := cmd.Wait()
	stop()
	code := 0
	if cmd.ProcessState != nil {
		code = exitCode(cmd.ProcessState)
	}
	var ee *exec.ExitError
	if werr != nil && !errors.As(werr, &ee) {
		return code, fmt.Errorf("chờ %s: %w", bin, werr)
	}
	sess.End()
	return code, nil
}

// Session là một phiên chạy harness trong profile đã được chuẩn bị: khoá S đang giữ, Env đã dựng, thư mục temp
// đã tạo, liên kết chia sẻ đã kiểm. Bên gọi tự spawn harness với Env rồi gọi End (Run làm đúng như vậy; eval và
// workflow dùng Begin/End cho bước chạy dài, docs/iso-plan.md mục 5.7).
type Session struct {
	Profile Profile
	// Env là env của tiến trình con (kết quả của Env(p, base)).
	Env []string
	// Temp là thư mục temp của profile (TMPDIR trong Env), đã tạo.
	Temp string

	base    []string
	logf    func(format string, args ...any)
	release func() error
	tracked []int // pid harness đã ghi hồ sơ runs/ (Track)
	once    sync.Once
}

// Begin chuẩn bị một phiên của profile p: lấy khoá S (chờ DefaultSessionWait khi gc/remove giữ X), dựng Env từ
// base (nil = os.Environ()), tạo TMPDIR trước khi spawn, kiểm liên kết chia sẻ và bản cài. Phải gọi End khi con
// kết thúc (kể cả khi không spawn được): End nhả S rồi thử dọn temp và GC tự động. logf nhận cảnh báo không
// chặn (có thể nil).
func Begin(p Profile, base []string, logf func(format string, args ...any)) (*Session, error) {
	if base == nil {
		base = os.Environ()
	}
	return begin(p, base, 0, logf)
}

func begin(p Profile, base []string, wait time.Duration, logf func(format string, args ...any)) (*Session, error) {
	spec := Spec{Logf: logf}
	release, err := acquireSession(p, wait)
	if err != nil {
		return nil, err
	}
	s := &Session{Profile: p, base: base, logf: logf, release: release}
	ok := false
	defer func() {
		if !ok {
			_ = release()
		}
	}()
	env, err := Env(p, base)
	if err != nil {
		return nil, err
	}
	tmp := newEnvList(runtime.GOOS, env).val(tempEnvKey())
	if tmp == "" {
		return nil, errors.New("env của profile thiếu thư mục temp")
	}
	if err := ensureTempDir(tmp); err != nil {
		return nil, fmt.Errorf("thư mục temp của profile %s: %w", p.ID(), err)
	}
	shares, err := ensureShares(runtime.GOOS, p, base)
	if err != nil {
		spec.logf("liên kết chia sẻ của %s: %v", p.ID(), err)
	}
	inst, err := ensureInstall(runtime.GOOS, p, base)
	if err != nil {
		spec.logf("bản cài của %s: %v", p.ID(), err)
	}
	for _, r := range append(shares, inst...) {
		if r.State.Conflict() {
			spec.logf("liên kết chia sẻ %s giữ nguyên (%s): %s", r.Link, r.State, "đã có dữ liệu hoặc liên kết khác")
		}
	}
	s.Env, s.Temp = env, tmp
	ok = true
	return s, nil
}

// End kết thúc phiên: nhả khoá S, thử lấy X để dọn thư mục temp (chỉ phiên cuối cùng xoá được) rồi chạy GC tự
// động khi đến hạn. Gọi nhiều lần cũng được (chỉ lần đầu có tác dụng); lỗi chỉ thành cảnh báo qua logf.
func (s *Session) End() {
	s.once.Do(func() {
		s.untrack() // trước khi nhả S: hồ sơ chỉ bị xoá khi còn giữ S
		_ = s.release()
		spec := Spec{Logf: s.logf}
		if err := finishSession(s.Profile, s.Temp); err != nil {
			spec.logf("dọn temp của %s: %v", s.Profile.ID(), err)
		}
		autoGC(s.Profile, s.base, spec)
	})
}

// gcStampName là tệp trong gốc store ghi lần GC tự động gần nhất (mtime là mốc, nội dung là RFC 3339 cho người đọc).
const gcStampName = "gc.stamp"

// autoGC chạy GC không --deep sau phiên khi gc.stamp cũ hơn iso.gc_interval (0 = tắt), rồi ghi lại gc.stamp.
// Chạy sau khi nhả khoá S để profile vừa dùng cũng được dọn và không làm chậm lúc khởi động harness. Lỗi chỉ
// thành cảnh báo: mã thoát của harness không đổi. Hai `run` cùng lúc có thể cùng chạy GC — vô hại, vì từng
// profile bị khoá X.
func autoGC(p Profile, env []string, spec Spec) {
	if p.Settings.GCInterval <= 0 {
		return
	}
	stamp := filepath.Join(p.StoreRoot, gcStampName)
	if fi, err := os.Stat(stamp); err == nil && time.Since(fi.ModTime()) < p.Settings.GCInterval {
		return
	}
	keep, err := OpenTaskSessions(env)
	if err != nil {
		spec.logf("GC tự động: %v (giữ mọi phiên)", err)
		keep = func(string) bool { return true }
	}
	store := &Store{Root: p.StoreRoot, Settings: p.Settings}
	if _, err := GC(store, GCOptions{KeepSession: keep, Env: env}); err != nil {
		spec.logf("GC tự động: %v", err)
	}
	if err := fsafe.WriteAtomic(stamp, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600); err != nil {
		spec.logf("GC tự động: không ghi được %s: %v", stamp, err)
	}
}

// tempEnvKey là biến temp mà Run đọc lại từ env đã dựng: TMP trên Windows, TMPDIR nơi khác (Env đặt cả ba).
func tempEnvKey() string {
	if runtime.GOOS == "windows" {
		return "TMP"
	}
	return "TMPDIR"
}

// acquireSession lấy khoá S của profile, thử lại mỗi 200 ms tới wait khi X đang bị gc/remove giữ.
func acquireSession(p Profile, wait time.Duration) (func() error, error) {
	if wait <= 0 {
		wait = DefaultSessionWait
	}
	deadline := time.Now().Add(wait)
	for {
		release, busy, err := p.Lock(false)
		if err != nil {
			return nil, err
		}
		if !busy {
			return release, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("%w: %s đang bị gc hoặc remove giữ khoá (chờ %s)", ErrBusy, p.ID(), wait)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// LookHarness tìm binary của tool trên PATH của env (PATH thật), bỏ mọi thư mục nằm trong store và mọi kết
// quả mà liên kết dẫn vào store: harness do người dùng cài, không phải bản chép trong profile (D5). Trả
// đường dẫn tuyệt đối; PATH có mục tương đối thì bỏ qua mục đó.
func LookHarness(t Tool, env []string, storeRoot string) (string, error) {
	e := newEnvList(runtime.GOOS, env)
	restoreReal(e)
	store := pathutil.BestEffort(storeRoot)
	name := string(t)
	for _, dir := range filepath.SplitList(e.val("PATH")) {
		if dir == "" || !filepath.IsAbs(dir) || inStore(dir, store) {
			continue
		}
		path, err := exec.LookPath(filepath.Join(dir, name))
		if err != nil || !filepath.IsAbs(path) || inStore(path, store) {
			continue
		}
		return path, nil
	}
	return "", fmt.Errorf("không tìm thấy %q trên PATH (ngoài thư mục của store %s)", name, storeRoot)
}

// inStore cho biết path (sau khi giải liên kết theo khả năng) nằm trong store.
func inStore(path, store string) bool {
	if within(path, store) {
		return true
	}
	return within(pathutil.BestEffort(path), store)
}
