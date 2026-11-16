package iso

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/config"
	"bint/internal/testutil"
)

// testStore mở store tạm (BINT_ISO_HOME do testutil.Isolate đặt) với cấu hình mặc định, tuỳ chỉnh bằng mut.
func testStore(t *testing.T, mut func(*config.Iso)) *Store {
	t.Helper()
	testutil.Isolate(t)
	// Isolate không đổi TMPDIR: test GC mà không có Env riêng sẽ quét /tmp/bint-iso-<uid> thật và xoá temp mồ côi
	// của profile thật (k:01M7HJD6RY80D6JNW51B9DCKDA). Luôn trỏ TMPDIR vào thư mục riêng của test.
	t.Setenv("TMPDIR", t.TempDir())
	cfg := config.Default().Iso
	if mut != nil {
		mut(&cfg)
	}
	s, err := OpenStore(cfg, os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func mustCreate(t *testing.T, s *Store, tool Tool, name string) Profile {
	t.Helper()
	p, err := s.Create(tool, name)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// fakeReal là máy "thật" giả: thư mục tạm và env tối thiểu (Unix).
type fakeReal struct {
	Dir, Home, State, Cfg, Tmp string
	Env                        []string
}

func newFakeReal(t *testing.T) fakeReal {
	t.Helper()
	d := t.TempDir()
	r := fakeReal{Dir: d, Home: filepath.Join(d, "home"), State: filepath.Join(d, "state"), Cfg: filepath.Join(d, "cfg"), Tmp: filepath.Join(d, "tmp")}
	r.Env = []string{
		"HOME=" + r.Home, "PATH=/usr/bin:/bin", "LANG=C",
		"BINT_STATE_DIR=" + r.State, "BINT_CONFIG_HOME=" + r.Cfg, "TMPDIR=" + r.Tmp,
		"XDG_CACHE_HOME=" + filepath.Join(d, "xcache"),
	}
	return r
}

// envMap đổi env thành map (khoá nguyên văn).
func envMap(env []string) map[string]string {
	m := map[string]string{}
	for _, s := range env {
		if k, v, ok := cutEnv(s); ok {
			m[k] = v
		}
	}
	return m
}

func hasKeyFold(m map[string]string, k string) bool {
	for mk := range m {
		if strings.EqualFold(mk, k) {
			return true
		}
	}
	return false
}
