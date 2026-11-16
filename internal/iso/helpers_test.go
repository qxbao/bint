package iso

import (
	"os"
	"path/filepath"
	"runtime"
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
	if runtime.GOOS == "windows" {
		// Home thật trên Windows là USERPROFILE (realHomeOf); thiếu nó thì Sync/Status/OpenStore báo lỗi home.
		r.Env = append(r.Env, "USERPROFILE="+r.Home)
	}
	return r
}

// unixLayoutOnly bỏ qua test so bảng env/danh mục theo bố cục Unix (đường dẫn viết bằng "/", giá trị như
// "/real/gomod" phải tuyệt đối). Trên Windows filepath đổi dấu phân cách và coi "/x" là tương đối, nên bảng
// đó không có nghĩa; nhánh Windows được kiểm ở TestEnvWindowsTable.
func unixLayoutOnly(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("bảng bố cục Unix; Windows kiểm ở TestEnvWindowsTable")
	}
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
