package iso

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/config"
)

func TestParseID(t *testing.T) {
	tool, name, err := ParseID("codex/work.1")
	if err != nil || tool != Codex || name != "work.1" {
		t.Fatalf("%v %q %v", tool, name, err)
	}
	for _, bad := range []string{"", "codex", "codex/", "agy/x", "codex/..", "codex/a/b", "/x", "codex/a b"} {
		if _, _, err := ParseID(bad); err == nil {
			t.Errorf("ParseID(%q) phải lỗi", bad)
		}
	}
}

func TestCreateFailureLeavesNoDirectory(t *testing.T) {
	s := testStore(t, nil)
	createFault = func(string) error { return os.ErrPermission }
	t.Cleanup(func() { createFault = func(string) error { return nil } })
	if _, err := s.Create(Codex, "w"); err == nil {
		t.Fatal("phải lỗi")
	}
	if _, err := os.Lstat(s.Dir(Codex, "w")); !os.IsNotExist(err) {
		t.Fatalf("Create lỗi giữa chừng không được để lại thư mục: %v", err)
	}
	createFault = func(string) error { return nil }
	if _, err := s.Create(Codex, "w"); err != nil {
		t.Fatalf("tạo lại sau lỗi phải được (không ErrExists): %v", err)
	}
	// Trùng vẫn là ErrExists và không bị dọn nhầm.
	if _, err := s.Create(Codex, "w"); err == nil || !strings.Contains(err.Error(), ErrExists.Error()) {
		t.Fatalf("err = %v", err)
	}
	if _, err := s.Load(Codex, "w"); err != nil {
		t.Fatalf("profile đã có bị hỏng sau Create trùng: %v", err)
	}
}

func snapshotTree(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil {
			rel, _ := filepath.Rel(root, p)
			fi, _ := os.Lstat(p)
			out = append(out, rel+"|"+fi.Mode().String())
		}
		return nil
	})
	return out
}

func TestStatusIsReadOnly(t *testing.T) {
	s := testStore(t, nil)
	p := mustCreate(t, s, Codex, "w")
	mustCreate(t, s, Claude, "c")
	real := newFakeReal(t)
	before := snapshotTree(t, s.Root)
	rows, err := Status(s, real.Env)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	if after := snapshotTree(t, s.Root); strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Fatalf("status đã ghi vào store:\nbefore=%v\nafter=%v", before, after)
	}
	if _, err := os.Stat(p.LockPath()); !os.IsNotExist(err) {
		t.Fatalf("status không được tạo session.lock: %v", err)
	}
	if _, err := os.Stat(real.Tmp); !os.IsNotExist(err) {
		t.Fatalf("status không được tạo thư mục thật: %v", err)
	}
	r := rows[1] // codex/w
	if r.ID != "codex/w" || r.Auth || r.Session || r.TempExists || r.DaemonAlive || r.LBMode != "" {
		t.Fatalf("%+v", r)
	}
	if len(r.Links) == 0 || r.Links[0].State != LinkCreated {
		t.Fatalf("liên kết chưa có phải báo LinkCreated (chạy sẽ tạo): %+v", r.Links)
	}
}

func TestStatusReportsAuthSessionAndLinks(t *testing.T) {
	s := testStore(t, nil)
	p := mustCreate(t, s, Codex, "w")
	p.Meta.LB = LB{Mode: LBLinked, URL: "http://127.0.0.1:2455"}
	if err := p.SaveMeta(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.Home, ".codex", "auth.json"), []byte(`{"x":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(p.Home, ".cache")
	if err := os.MkdirAll(cache, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "f"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if rel, busy, err := p.Lock(false); err != nil || busy {
		t.Fatal(err, busy)
	} else {
		defer rel()
	}
	rows, err := Status(s, newFakeReal(t).Env)
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	r := rows[0]
	if !r.Auth || !r.Session || r.LBMode != LBLinked || r.LBURL != "http://127.0.0.1:2455" {
		t.Fatalf("%+v", r)
	}
	if len(r.LinkIssues) == 0 || !strings.Contains(r.LinkIssues[0], "conflict-data") {
		t.Fatalf("thư mục .cache có dữ liệu phải báo xung đột: %+v", r.LinkIssues)
	}
}

func TestShellInit(t *testing.T) {
	s := testStore(t, nil)
	profiles := []Profile{mustCreate(t, s, Codex, "work"), mustCreate(t, s, Claude, "my_acct-2"), mustCreate(t, s, Codex, "a.b")}
	cases := map[string][]string{
		"bash": {`codex-work() { command bint iso run codex --name work -- "$@"; }`, `claude-my_acct-2() { command bint iso run claude --name my_acct-2 -- "$@"; }`},
		"zsh":  {`codex-work() { command bint iso run codex --name work -- "$@"; }`},
		"fish": {`function codex-work; command bint iso run codex --name work -- $argv; end`},
		"pwsh": {`function codex-work { & bint iso run codex --name work -- @args }`},
	}
	for sh, wants := range cases {
		out, err := ShellInit(sh, profiles)
		if err != nil {
			t.Fatal(sh, err)
		}
		for _, w := range wants {
			if !strings.Contains(out, w) {
				t.Errorf("%s thiếu %q trong:\n%s", sh, w, out)
			}
		}
		if strings.Contains(out, "codex-a.b") && !strings.Contains(out, "# bỏ qua codex/a.b") {
			t.Errorf("%s: tên có dấu chấm phải bị bỏ qua kèm chú thích:\n%s", sh, out)
		}
		if strings.Contains(out, "a.b() ") || strings.Contains(out, "function codex-a.b") {
			t.Errorf("%s: không được tạo hàm tên có dấu chấm:\n%s", sh, out)
		}
	}
	if _, err := ShellInit("tcsh", profiles); err == nil {
		t.Fatal("shell lạ phải lỗi")
	}
	if out, _ := ShellInit("bash", nil); !strings.Contains(out, "chưa có profile") {
		t.Fatalf("không có profile: %q", out)
	}
}

func TestLoadGlobalIgnoresRepoConfig(t *testing.T) {
	// LoadGlobal đã có test riêng ở gói config; ở đây chỉ kiểm cấu hình mặc định dùng được cho OpenStore.
	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SettingsFrom(cfg.Iso); err != nil {
		t.Fatal(err)
	}
}
