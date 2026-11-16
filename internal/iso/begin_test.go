//go:build !windows

package iso

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/config"
)

// Begin dựng Env của profile, tạo TMPDIR trước khi spawn, giữ khoá S tới End; End dọn temp và nhả khoá (idempotent).
func TestBeginEndLifecycle(t *testing.T) {
	s := testStore(t, func(c *config.Iso) { c.GCInterval = "0s" })
	real := newFakeReal(t)
	p := mustCreate(t, s, Codex, "w")

	sess, err := Begin(p, real.Env, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := envMap(sess.Env)
	if m["HOME"] != p.Home || m["CODEX_HOME"] != p.ConfigDir() || m["BINT_STATE_DIR"] != real.State || m["BINT_CONFIG_HOME"] != real.Cfg {
		t.Fatalf("env profile sai: HOME=%q CODEX_HOME=%q STATE=%q CFG=%q", m["HOME"], m["CODEX_HOME"], m["BINT_STATE_DIR"], m["BINT_CONFIG_HOME"])
	}
	if sess.Temp == "" || sess.Temp != m["TMPDIR"] {
		t.Fatalf("Temp=%q TMPDIR=%q", sess.Temp, m["TMPDIR"])
	}
	if fi, err := os.Stat(sess.Temp); err != nil || !fi.IsDir() {
		t.Fatalf("TMPDIR phải có sẵn trước khi spawn: %v", err)
	}
	// Khoá S đang giữ: X (gc/remove) bị từ chối.
	if _, busy, err := p.Lock(true); err != nil || !busy {
		t.Fatalf("khoá X lúc đang có phiên: busy=%v err=%v, muốn busy", busy, err)
	}
	if err := os.WriteFile(filepath.Join(sess.Temp, "x"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	sess.End()
	sess.End() // idempotent
	if _, err := os.Stat(sess.Temp); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temp phải bị dọn sau End: %v", err)
	}
	release, busy, err := p.Lock(true)
	if err != nil || busy {
		t.Fatalf("khoá X sau End: busy=%v err=%v", busy, err)
	}
	release()
}

// Hai phiên cùng profile: chỉ phiên cuối cùng xoá temp.
func TestBeginTwoSessionsKeepTempUntilLast(t *testing.T) {
	s := testStore(t, func(c *config.Iso) { c.GCInterval = "0s" })
	real := newFakeReal(t)
	p := mustCreate(t, s, Claude, "w")
	a, err := Begin(p, real.Env, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Begin(p, real.Env, nil)
	if err != nil {
		t.Fatal(err)
	}
	a.End()
	if _, err := os.Stat(b.Temp); err != nil {
		t.Fatalf("phiên còn lại mất temp: %v", err)
	}
	b.End()
	if _, err := os.Stat(b.Temp); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temp phải bị dọn khi phiên cuối kết thúc: %v", err)
	}
}

func TestBeginFailureReleasesLock(t *testing.T) {
	s := testStore(t, nil)
	p := mustCreate(t, s, Codex, "w")
	// Env không có home: Env() lỗi, khoá S không được giữ lại.
	if _, err := Begin(p, []string{"PATH=/bin"}, nil); err == nil {
		t.Fatal("muốn lỗi khi không dựng được env")
	}
	release, busy, err := p.Lock(true)
	if err != nil || busy {
		t.Fatalf("khoá còn bị giữ sau Begin lỗi: busy=%v err=%v", busy, err)
	}
	release()
}

func TestProfileConfigDir(t *testing.T) {
	s := testStore(t, nil)
	for tool, dir := range map[Tool]string{Codex: ".codex", Claude: ".claude"} {
		p := mustCreate(t, s, tool, "w")
		if got, want := p.ConfigDir(), filepath.Join(p.Home, dir); got != want {
			t.Errorf("%s: ConfigDir=%q, muốn %q", tool, got, want)
		}
	}
}

func TestResolveProfile(t *testing.T) {
	s := testStore(t, nil)
	mustCreate(t, s, Codex, "work")
	p, err := ResolveProfile(config.Default().Iso, Codex, "work", os.Environ())
	if err != nil || p.ID() != "codex/work" {
		t.Fatalf("p=%v err=%v", p.ID(), err)
	}
	_, err = ResolveProfile(config.Default().Iso, Codex, "none", os.Environ())
	if !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "bint iso create codex --name none") {
		t.Fatalf("muốn ErrNotFound kèm gợi ý bint iso create, nhận %v", err)
	}
}

func TestHarnessArgs(t *testing.T) {
	s := testStore(t, nil)
	ctx := context.Background()
	srv, _ := lbServer(t, 200)

	// Không phải Codex linked: không có đối số.
	for _, p := range []Profile{mustCreate(t, s, Claude, "w"), mustCreate(t, s, Codex, "direct")} {
		if args, err := HarnessArgs(ctx, p, nil); err != nil || len(args) != 0 {
			t.Fatalf("%s: args=%v err=%v, muốn rỗng", p.ID(), args, err)
		}
	}
	linked := mustCreate(t, s, Codex, "linked")
	linked.Meta.LB = LB{Mode: LBLinked, URL: srv.URL}
	args, err := HarnessArgs(ctx, linked, []string{"PATH=/bin"})
	if err != nil {
		t.Fatal(err)
	}
	if want := CodexLBArgs(srv.URL, false); strings.Join(args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args=%v, muốn %v", args, want)
	}
	// Có CODEX_LB_API_KEY trong env gốc thì thêm env_key.
	args, err = HarnessArgs(ctx, linked, []string{"CODEX_LB_API_KEY=k"})
	if err != nil || !strings.Contains(strings.Join(args, " "), `env_key="CODEX_LB_API_KEY"`) {
		t.Fatalf("args=%v err=%v, muốn có env_key", args, err)
	}
	// LB không sẵn sàng: ErrLBUnavailable.
	bad, _ := lbServer(t, 503)
	linked.Meta.LB.URL = bad.URL
	if _, err := HarnessArgs(ctx, linked, nil); !errors.Is(err, ErrLBUnavailable) {
		t.Fatalf("muốn ErrLBUnavailable, nhận %v", err)
	}
}

// Hai store cùng codex/default chạy đồng thời: phiên cuối của store A kết thúc không được xoá TMPDIR của B
// (khoá S nằm trong từng store, TMPDIR phải tách theo store).
func TestFinishSessionOtherStoreKeepsTemp(t *testing.T) {
	a := testStore(t, func(c *config.Iso) { c.GCInterval = "0s" })
	real := newFakeReal(t)
	b := &Store{Root: filepath.Join(real.Dir, "storeB"), Settings: a.Settings}
	pa := mustCreate(t, a, Codex, "default")
	pb := mustCreate(t, b, Codex, "default")

	sb, err := Begin(pb, real.Env, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sb.End()
	sa, err := Begin(pa, real.Env, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sa.Temp == sb.Temp {
		t.Fatalf("hai store dùng chung TMPDIR %q", sa.Temp)
	}
	marker := filepath.Join(sb.Temp, "dang-dung")
	if err := os.WriteFile(marker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	sa.End()
	if _, err := os.Stat(sa.Temp); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("TMPDIR của A phải bị dọn: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("phiên cuối của store A đã xoá TMPDIR của store B: %v", err)
	}
}
