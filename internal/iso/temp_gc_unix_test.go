//go:build !windows

package iso

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// tmpProfile dựng profile Codex trong store tạm với TMPDIR thật là thư mục tạm riêng (profile.TempDir() nằm trong đó).
func tmpProfile(t *testing.T, tool Tool, name string) (*Store, Profile, string) {
	t.Helper()
	base := t.TempDir()
	t.Setenv("TMPDIR", base)
	s := testStore(t, nil)
	t.Setenv("TMPDIR", base)
	t.Setenv(EnvProfile, "")
	return s, mustCreate(t, s, tool, name), base
}

func age(t *testing.T, path string, d time.Duration) {
	t.Helper()
	old := time.Now().Add(-d)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
}

func writeDaemonPid(t *testing.T, p Profile, pid int, ticks uint64) {
	t.Helper()
	dir := filepath.Dir(p.daemonPidPath())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	var d struct {
		PID             int `json:"pid"`
		ProcessIdentity struct {
			StartTicks uint64 `json:"startTicks"`
		} `json:"processIdentity"`
	}
	d.PID = pid
	d.ProcessIdentity.StartTicks = ticks
	b, _ := json.Marshal(d)
	if err := os.WriteFile(p.daemonPidPath(), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDaemonAlive(t *testing.T) {
	_, p, _ := tmpProfile(t, Codex, "w")
	if p.DaemonAlive() {
		t.Fatal("không có daemon.pid thì không có daemon")
	}
	// Tiến trình này đang sống, danh tính khớp (nếu đo được) hoặc không khai.
	ticks, _ := procStartTicks(os.Getpid())
	writeDaemonPid(t, p, os.Getpid(), ticks)
	if !p.DaemonAlive() {
		t.Fatal("pid sống phải là daemon sống")
	}
	// Pid sống nhưng startTicks khác: pid bị dùng lại.
	if ticks != 0 {
		writeDaemonPid(t, p, os.Getpid(), ticks+1)
		if p.DaemonAlive() {
			t.Fatal("startTicks lệch thì pid đã bị dùng lại, không phải daemon")
		}
	}
	writeDaemonPid(t, p, 0x7ffffffe, 1)
	if p.DaemonAlive() {
		t.Fatal("pid không tồn tại thì daemon đã chết")
	}
	// Không chắc ⇒ coi là sống (hướng an toàn).
	if err := os.WriteFile(p.daemonPidPath(), []byte("{hỏng"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !p.DaemonAlive() {
		t.Fatal("daemon.pid hỏng phải coi là còn sống")
	}
	// Claude không có daemon dù có tệp.
	_, c, _ := tmpProfile(t, Claude, "w")
	if c.DaemonAlive() {
		t.Fatal("claude không có daemon")
	}
}

// Quan sát GĐ2: daemon rảnh không giữ khoá trên daemon.pid.lock nên khoá không phải tín hiệu sống.
func TestDaemonAliveIgnoresLockFile(t *testing.T) {
	_, p, _ := tmpProfile(t, Codex, "w")
	dir := filepath.Dir(p.daemonPidPath())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "daemon.pid.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if p.DaemonAlive() {
		t.Fatal("chỉ có daemon.pid.lock (không khoá, không pid) thì không có daemon")
	}
}

func TestFinishSessionRemovesWholeDirWithoutDaemon(t *testing.T) {
	_, p, _ := tmpProfile(t, Codex, "w")
	tmp, err := p.EnsureTemp()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "f"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "codex-bwrap-synthetic-mount-targets-1000", "d"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := finishSession(p, tmp); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(tmp); !os.IsNotExist(err) {
		t.Fatalf("temp phải biến mất khi không có daemon: %v", err)
	}
}

func TestFinishSessionKeepsDirAndSandboxWhileDaemonAlive(t *testing.T) {
	_, p, _ := tmpProfile(t, Codex, "w")
	ticks, _ := procStartTicks(os.Getpid())
	writeDaemonPid(t, p, os.Getpid(), ticks)
	tmp, err := p.EnsureTemp()
	if err != nil {
		t.Fatal(err)
	}
	mk := func(name string, old bool) string {
		path := filepath.Join(tmp, name)
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "f"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if old {
			age(t, path, 48*time.Hour)
		}
		return path
	}
	oldPlain := mk("old-work", true)
	freshPlain := mk("fresh-work", false)
	oldSandbox := mk("codex-bwrap-synthetic-mount-targets-1000", true)
	oldFile := filepath.Join(tmp, "old.txt")
	if err := os.WriteFile(oldFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	age(t, oldFile, 48*time.Hour)

	if err := finishSession(p, tmp); err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{oldPlain, oldFile} {
		if _, err := os.Lstat(gone); !os.IsNotExist(err) {
			t.Errorf("%s cũ hơn tmp_ttl phải bị xoá (%v)", gone, err)
		}
	}
	for _, kept := range []string{tmp, freshPlain, oldSandbox} {
		if _, err := os.Lstat(kept); err != nil {
			t.Errorf("%s phải được giữ khi daemon còn sống: %v", kept, err)
		}
	}
}

func TestFinishSessionLeavesTempWhenAnotherSessionHoldsS(t *testing.T) {
	_, p, _ := tmpProfile(t, Codex, "w")
	tmp, _ := p.EnsureTemp()
	other, busy, err := p.Lock(false) // phiên khác
	if err != nil || busy {
		t.Fatalf("lock: %v busy=%v", err, busy)
	}
	if err := finishSession(p, tmp); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(tmp); err != nil {
		t.Fatalf("còn phiên khác thì temp phải còn: %v", err)
	}
	other()
	if err := finishSession(p, tmp); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(tmp); !os.IsNotExist(err) {
		t.Fatalf("phiên cuối thoát thì temp phải biến mất: %v", err)
	}
}

func TestPruneOldDoesNotFollowSymlinks(t *testing.T) {
	dir := t.TempDir()
	canary := t.TempDir()
	if err := os.WriteFile(filepath.Join(canary, "keep"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "lnk")
	if err := os.Symlink(canary, link); err != nil {
		t.Fatal(err)
	}
	// Mục con của thư mục cũ chứa symlink tới canary.
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(canary, filepath.Join(sub, "inner")); err != nil {
		t.Fatal(err)
	}
	age(t, sub, 48*time.Hour)
	age(t, dir, 48*time.Hour)
	picked, err := pruneOld(dir, time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(canary, "keep")); err != nil {
		t.Fatalf("xoá theo tuổi đi xuyên liên kết và xoá mất dữ liệu đích: %v (picked %v)", err, picked)
	}
	if _, err := os.Lstat(sub); !os.IsNotExist(err) {
		t.Fatalf("sub cũ phải bị xoá: %v", err)
	}
}

func TestCollectTempDryRunBusyAndOrphan(t *testing.T) {
	s, p, base := tmpProfile(t, Codex, "w")
	other := mustCreate(t, s, Claude, "c")
	tmp, _ := p.EnsureTemp()
	tmp2, _ := other.EnsureTemp()
	if err := os.WriteFile(filepath.Join(tmp, "f"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(base, tempParentName(), "codex--gone")
	if err := os.MkdirAll(orphan, 0o700); err != nil {
		t.Fatal(err)
	}
	fresh := filepath.Join(base, tempParentName(), "codex--fresh")
	if err := os.MkdirAll(fresh, 0o700); err != nil {
		t.Fatal(err)
	}
	stranger := filepath.Join(base, tempParentName(), "không-phải-profile")
	if err := os.MkdirAll(stranger, 0o700); err != nil {
		t.Fatal(err)
	}
	age(t, orphan, 48*time.Hour)
	age(t, stranger, 48*time.Hour)

	// phiên đang chạy trên profile w
	rel, busy, err := p.Lock(false)
	if err != nil || busy {
		t.Fatal(err, busy)
	}
	byProfile := func(acts []TempAction) map[string]TempAction {
		m := map[string]TempAction{}
		for _, a := range acts {
			m[a.Profile] = a
		}
		return m
	}
	acts, err := CollectTemp(s, TempOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	m := byProfile(acts)
	if m["codex/w"].Kind != TempBusy || m["claude/c"].Kind != TempRemoveAll || m["codex/gone"].Kind != TempOrphan {
		t.Fatalf("dry-run: %+v", acts)
	}
	if _, ok := m["codex/fresh"]; ok {
		t.Fatalf("thư mục mồ côi còn mới (store khác có thể đang dùng) không được đụng: %+v", acts)
	}
	for _, a := range acts {
		if a.Removed {
			t.Fatalf("dry-run không được xoá: %+v", a)
		}
	}
	for _, path := range []string{tmp, tmp2, orphan, fresh, stranger} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("dry-run đã xoá %s: %v", path, err)
		}
	}

	acts, err = CollectTemp(s, TempOptions{})
	if err != nil {
		t.Fatal(err)
	}
	m = byProfile(acts)
	if !m["claude/c"].Removed || !m["codex/gone"].Removed || m["codex/w"].Removed {
		t.Fatalf("gc: %+v", acts)
	}
	if _, err := os.Lstat(tmp); err != nil {
		t.Fatalf("profile đang có phiên giữ khoá S phải được bỏ qua: %v", err)
	}
	for _, path := range []string{tmp2, orphan} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("%s phải bị xoá: %v", path, err)
		}
	}
	for _, path := range []string{fresh, stranger} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("%s không được đụng: %v", path, err)
		}
	}

	rel()
	if _, err := CollectTemp(s, TempOptions{Only: "claude/c"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(tmp); err != nil {
		t.Fatalf("--profile claude/c không được đụng codex/w: %v", err)
	}
	if _, err := CollectTemp(s, TempOptions{Only: "codex/w"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(tmp); !os.IsNotExist(err) {
		t.Fatalf("hết phiên thì gc xoá temp: %v", err)
	}
}
