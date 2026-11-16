package iso

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"bint/internal/config"
)

func TestStoreRootPrecedence(t *testing.T) {
	r := newFakeReal(t)
	cfgHome := filepath.Join(t.TempDir(), "cfg-iso")
	envHome := filepath.Join(t.TempDir(), "env-iso")
	cfg := config.Default().Iso

	// mặc định Linux theo XDG_DATA_HOME, rồi ~/.local/share
	xdg := filepath.Join(r.Dir, "xdgdata")
	cases := []struct {
		name string
		goos string
		cfg  string
		env  []string
		want string
	}{
		{"BINT_ISO_HOME thắng iso.home", "linux", cfgHome, append(r.Env, "BINT_ISO_HOME="+envHome), envHome},
		{"iso.home thắng mặc định", "linux", cfgHome, r.Env, cfgHome},
		{"auto + XDG_DATA_HOME", "linux", "auto", append(r.Env, "XDG_DATA_HOME="+xdg), filepath.Join(xdg, "bint", "iso")},
		{"auto không XDG", "linux", "auto", r.Env, filepath.Join(r.Home, ".local", "share", "bint", "iso")},
		{"auto XDG tương đối bị bỏ", "linux", "auto", append(r.Env, "XDG_DATA_HOME=rel"), filepath.Join(r.Home, ".local", "share", "bint", "iso")},
		{"macOS", "darwin", "auto", r.Env, filepath.Join(r.Home, "Library", "Application Support", "bint", "iso")},
		{"Windows LOCALAPPDATA", "windows", "auto", []string{"USERPROFILE=" + r.Home, "LOCALAPPDATA=" + filepath.Join(r.Dir, "local")}, filepath.Join(r.Dir, "local", "bint", "iso")},
		{"Windows mặc định (khoá khác hoa thường)", "windows", "auto", []string{"userprofile=" + r.Home}, filepath.Join(r.Home, "AppData", "Local", "bint", "iso")},
	}
	for _, c := range cases {
		cfg.Home = c.cfg
		s, err := openStore(c.goos, cfg, c.env)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if s.Root != c.want {
			t.Errorf("%s: root = %q, muốn %q", c.name, s.Root, c.want)
		}
	}

	// lỗi: BINT_ISO_HOME tương đối, iso.home tương đối, không có home
	cfg.Home = "auto"
	if _, err := openStore("linux", cfg, append(r.Env, "BINT_ISO_HOME=rel")); err == nil {
		t.Error("BINT_ISO_HOME tương đối phải lỗi")
	}
	cfg.Home = "rel"
	if _, err := openStore("linux", cfg, r.Env); err == nil {
		t.Error("iso.home tương đối phải lỗi")
	}
	cfg.Home = "auto"
	if _, err := openStore("linux", cfg, []string{"PATH=/bin"}); err == nil {
		t.Error("không có home phải lỗi")
	}
	cfg.Cache = "bad"
	if _, err := openStore("linux", cfg, r.Env); err == nil {
		t.Error("cache lạ phải lỗi")
	}
}

// Trong profile (HOME giả), mặc định phải tính từ home thật và BINT_ISO_HOME giữ nguyên store.
func TestOpenStoreFromInsideProfile(t *testing.T) {
	r := newFakeReal(t)
	cfg := config.Default().Iso
	outer := testStore(t, nil)
	p := mustCreate(t, outer, Codex, "a")
	env, err := Env(p, r.Env)
	if err != nil {
		t.Fatal(err)
	}
	inner, err := OpenStore(cfg, env)
	if err != nil {
		t.Fatal(err)
	}
	if inner.Root != outer.Root {
		t.Errorf("store lồng %q khác %q", inner.Root, outer.Root)
	}
	// Bỏ BINT_ISO_HOME: mặc định phải theo HOME thật, không phải HOME giả của profile.
	var noHome []string
	for _, s := range env {
		if !strings.HasPrefix(s, "BINT_ISO_HOME=") {
			noHome = append(noHome, s)
		}
	}
	s2, err := OpenStore(cfg, noHome)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(s2.Root, p.Home) {
		t.Errorf("store %q nằm trong HOME giả %q", s2.Root, p.Home)
	}
	if !strings.HasPrefix(s2.Root, r.Dir) {
		t.Errorf("store %q không theo HOME thật %q", s2.Root, r.Home)
	}
}

func TestCreateLoadList(t *testing.T) {
	s := testStore(t, nil)
	if ps, err := s.List(); err != nil || len(ps) != 0 {
		t.Fatalf("store trống: %v %v", ps, err)
	}
	p := mustCreate(t, s, Codex, "work")
	if p.Dir != filepath.Join(s.Root, "profiles", "codex--work") || p.Home != filepath.Join(p.Dir, "home") {
		t.Fatalf("đường dẫn: %+v", p)
	}
	for _, d := range []string{"", "home", "home/.config", "home/.local/share", "home/.local/state", "home/.codex"} {
		fi, err := os.Stat(filepath.Join(p.Dir, filepath.FromSlash(d)))
		if err != nil || !fi.IsDir() {
			t.Fatalf("thiếu thư mục %q: %v", d, err)
		}
		if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o700 {
			t.Errorf("%q mode %o, muốn 0700", d, fi.Mode().Perm())
		}
	}
	if fi, err := os.Stat(p.MetaPath()); err != nil {
		t.Fatal(err)
	} else if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("profile.json mode %o, muốn 0600", fi.Mode().Perm())
	}
	if _, err := s.Create(Codex, "work"); !errors.Is(err, ErrExists) {
		t.Errorf("tạo trùng: %v", err)
	}
	got, err := s.Load(Codex, "work")
	if err != nil {
		t.Fatal(err)
	}
	if got.Meta.Version != MetaVersion || got.Meta.Tool != Codex || got.Meta.Name != "work" || got.Meta.CreatedAt.IsZero() {
		t.Errorf("meta: %+v", got.Meta)
	}
	if got.ID() != "codex/work" || got.StoreRoot != s.Root {
		t.Errorf("profile: %+v", got)
	}
	if _, err := s.Load(Claude, "work"); !errors.Is(err, ErrNotFound) {
		t.Errorf("không có: %v", err)
	}
	mustCreate(t, s, Claude, "default")
	mustCreate(t, s, Codex, "a--b")
	// mục lạ trong profiles/ bị bỏ qua
	if err := os.Mkdir(filepath.Join(s.Root, "profiles", "random"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Root, "profiles", "codex--x.txt"), nil, 0o600); err == nil {
		// tệp tên đúng dạng nhưng không phải thư mục: List báo lỗi, không panic
		ps, err := s.List()
		if err == nil || len(ps) != 3 {
			t.Errorf("tệp lạ: %d profile, err=%v", len(ps), err)
		}
		os.Remove(filepath.Join(s.Root, "profiles", "codex--x.txt"))
	}
	ps, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, q := range ps {
		ids = append(ids, q.ID())
	}
	if strings.Join(ids, ",") != "claude/default,codex/a--b,codex/work" {
		t.Errorf("List = %v", ids)
	}
}

func TestCreateRejectsBadInput(t *testing.T) {
	s := testStore(t, nil)
	for _, n := range []string{"", ".", "..", "a/b", strings.Repeat("x", 65)} {
		if _, err := s.Create(Codex, n); err == nil {
			t.Errorf("Create(%q) phải lỗi", n)
		}
		if _, err := s.Load(Codex, n); err == nil {
			t.Errorf("Load(%q) phải lỗi", n)
		}
	}
	if _, err := s.Create("agy", "x"); err == nil {
		t.Error("tool lạ phải lỗi")
	}
	if _, err := os.Stat(s.profilesDir()); err == nil {
		if ents, _ := os.ReadDir(s.profilesDir()); len(ents) != 0 {
			t.Errorf("đầu vào sai không được tạo thư mục: %v", ents)
		}
	}
}

func TestLoadRejectsBadProfiles(t *testing.T) {
	s := testStore(t, nil)
	p := mustCreate(t, s, Codex, "ok")

	t.Run("version lạ", func(t *testing.T) {
		q := mustCreate(t, s, Codex, "ver")
		q.Meta.Version = 9
		if err := q.SaveMeta(); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Load(Codex, "ver"); err == nil {
			t.Error("phải từ chối version lạ")
		}
	})
	t.Run("tool/name lệch thư mục", func(t *testing.T) {
		q := mustCreate(t, s, Codex, "lech")
		q.Meta.Name = "khac"
		if err := q.SaveMeta(); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Load(Codex, "lech"); err == nil {
			t.Error("phải từ chối tên lệch")
		}
	})
	t.Run("lb.mode lạ", func(t *testing.T) {
		q := mustCreate(t, s, Codex, "lb")
		q.Meta.LB.Mode = "weird"
		if err := q.SaveMeta(); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Load(Codex, "lb"); err == nil {
			t.Error("phải từ chối lb.mode lạ")
		}
	})
	t.Run("thiếu profile.json", func(t *testing.T) {
		if err := os.MkdirAll(filepath.Join(s.Dir(Claude, "nometa"), "home"), 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Load(Claude, "nometa"); err == nil {
			t.Error("phải lỗi khi thiếu profile.json")
		}
	})
	if runtime.GOOS == "windows" {
		return // symlink thư mục cần quyền riêng
	}
	t.Run("thư mục profile là symlink ra ngoài store", func(t *testing.T) {
		outside := t.TempDir()
		if err := os.MkdirAll(filepath.Join(outside, "home"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := writeMeta(filepath.Join(outside, "profile.json"), Meta{Version: MetaVersion, Tool: Claude, Name: "evil"}); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, s.Dir(Claude, "evil")); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Load(Claude, "evil"); err == nil {
			t.Error("phải từ chối profile là symlink")
		}
		ps, err := s.List()
		if err == nil {
			t.Error("List phải báo profile hỏng")
		}
		for _, q := range ps {
			if q.Name == "evil" {
				t.Error("profile symlink lọt vào List")
			}
		}
	})
	t.Run("home là symlink ra ngoài store", func(t *testing.T) {
		q := mustCreate(t, s, Claude, "homelink")
		if err := os.Remove(filepath.Join(q.Dir, "home", ".config")); err != nil {
			t.Fatal(err)
		}
		_ = os.Remove(filepath.Join(q.Dir, "home", ".local", "share"))
		_ = os.Remove(filepath.Join(q.Dir, "home", ".local", "state"))
		_ = os.Remove(filepath.Join(q.Dir, "home", ".local"))
		_ = os.Remove(filepath.Join(q.Dir, "home", ".claude"))
		if err := os.Remove(q.Home); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(t.TempDir(), q.Home); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Load(Claude, "homelink"); err == nil {
			t.Error("phải từ chối home trỏ ra ngoài store")
		}
	})
	if _, err := s.Load(Codex, "ok"); err != nil {
		t.Fatalf("profile tốt vẫn phải nạp được: %v (%s)", err, p.Dir)
	}
}
