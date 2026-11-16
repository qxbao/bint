package iso

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/pathutil"
)

// ErrNotFound / ErrExists: profile không có / đã có.
var (
	ErrNotFound = errors.New("profile không tồn tại")
	ErrExists   = errors.New("profile đã tồn tại")
)

// Store là thư mục chứa mọi profile: <Root>/profiles/<tool>--<name>/.
type Store struct {
	Root     string
	Settings Settings
}

// OpenStore chọn gốc store theo thứ tự BINT_ISO_HOME (trong env) > iso.home > mặc định theo OS
// (Linux ${XDG_DATA_HOME:-~/.local/share}/bint/iso, macOS ~/Library/Application Support/bint/iso,
// Windows %LOCALAPPDATA%\bint\iso). env là env của tiến trình (os.Environ()); đang trong profile thì mặc định
// tính từ env thật (RealEnv) để profile lồng dùng cùng một store. Không tạo thư mục nào.
func OpenStore(cfg config.Iso, env []string) (*Store, error) {
	return openStore(runtime.GOOS, cfg, env)
}

func openStore(goos string, cfg config.Iso, env []string) (*Store, error) {
	set, err := SettingsFrom(cfg)
	if err != nil {
		return nil, err
	}
	e := newEnvList(goos, env)
	restoreReal(e)
	root, err := storeRoot(goos, cfg.Home, e)
	if err != nil {
		return nil, err
	}
	return &Store{Root: root, Settings: set}, nil
}

func storeRoot(goos, cfgHome string, e *envList) (string, error) {
	switch {
	case e.val(EnvHome) != "":
		return requireAbs(EnvHome, e.val(EnvHome))
	case cfgHome != "" && cfgHome != "auto":
		return requireAbs("iso.home", cfgHome)
	}
	home, err := realHomeOf(e)
	if err != nil {
		return "", err
	}
	switch goos {
	case "windows":
		base := e.val("LOCALAPPDATA")
		if base == "" {
			base = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(base, "bint", "iso"), nil
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "bint", "iso"), nil
	}
	if d := e.val("XDG_DATA_HOME"); d != "" && filepath.IsAbs(d) {
		return filepath.Join(d, "bint", "iso"), nil
	}
	return filepath.Join(home, ".local", "share", "bint", "iso"), nil
}

func requireAbs(what, p string) (string, error) {
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("%s: phải là đường dẫn tuyệt đối, nhận %q", what, p)
	}
	return filepath.Clean(p), nil
}

func (s *Store) profilesDir() string { return filepath.Join(s.Root, "profiles") }

// Dir là thư mục của profile (chưa kiểm tồn tại).
func (s *Store) Dir(t Tool, name string) string {
	return filepath.Join(s.profilesDir(), dirName(t, name))
}

func (s *Store) profile(t Tool, name string, m Meta) Profile {
	dir := s.Dir(t, name)
	return Profile{Tool: t, Name: name, StoreRoot: s.Root, Dir: dir, Home: filepath.Join(dir, "home"), Meta: m, Settings: s.Settings}
}

func (s *Store) check(t Tool, name string) error {
	if _, err := ParseTool(string(t)); err != nil {
		return err
	}
	return ValidName(name)
}

// createFault là điểm chèn lỗi của test sau khi đã tạo thư mục profile (không đặt lỗi ở sản phẩm).
var createFault = func(string) error { return nil }

// Create tạo profile mới (thư mục 0700, home/ 0700 cùng .config, .local/share, .local/state và thư mục cấu
// hình của tool, profile.json 0600). ErrExists nếu đã có.
func (s *Store) Create(t Tool, name string) (Profile, error) {
	if err := s.check(t, name); err != nil {
		return Profile{}, err
	}
	if err := os.MkdirAll(s.profilesDir(), 0o700); err != nil {
		return Profile{}, err
	}
	p := s.profile(t, name, Meta{Version: MetaVersion, Tool: t, Name: name, CreatedAt: time.Now().UTC().Truncate(time.Second)})
	if err := os.Mkdir(p.Dir, 0o700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return Profile{}, fmt.Errorf("%w: %s", ErrExists, p.ID())
		}
		return Profile{}, err
	}
	// Lỗi sau khi Mkdir thành công: dọn thư mục vừa tạo (chỉ của lần Create này) để không để lại profile
	// nửa vời mà Load sẽ từ chối còn Create sau đó báo ErrExists.
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(p.Dir)
		}
	}()
	dirs := []string{p.Home}
	for _, d := range []string{".config", filepath.Join(".local", "share"), filepath.Join(".local", "state"), t.spec().ConfigDir} {
		dirs = append(dirs, filepath.Join(p.Home, d))
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return Profile{}, err
		}
	}
	if err := createFault(p.Dir); err != nil {
		return Profile{}, err
	}
	if err := p.SaveMeta(); err != nil {
		return Profile{}, err
	}
	ok = true
	return p, nil
}

// Load nạp profile. Từ chối khi thư mục profile hay home/ là symlink/junction, hoặc đường dẫn thật của chúng
// nằm ngoài store; profile.json phải đúng phiên bản và khớp tool/name. ErrNotFound nếu không có.
func (s *Store) Load(t Tool, name string) (Profile, error) {
	if err := s.check(t, name); err != nil {
		return Profile{}, err
	}
	p := s.profile(t, name, Meta{})
	fi, err := os.Lstat(p.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return Profile{}, fmt.Errorf("%w: %s", ErrNotFound, p.ID())
	}
	if err != nil {
		return Profile{}, err
	}
	if !fi.IsDir() {
		return Profile{}, fmt.Errorf("%s không phải thư mục thường (symlink hoặc tệp)", p.Dir)
	}
	if err := s.contained(p.Dir, p.Home); err != nil {
		return Profile{}, err
	}
	if p.Meta, err = readMeta(p.MetaPath(), t, name); err != nil {
		return Profile{}, err
	}
	return p, nil
}

// contained kiểm dir và home nằm thật sự dưới <store>/profiles (sau khi giải symlink) và home là thư mục.
func (s *Store) contained(dir, home string) error {
	base, err := pathutil.Canonical(s.profilesDir())
	if err != nil {
		return err
	}
	for _, p := range []string{dir, home} {
		fi, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if !fi.IsDir() {
			return fmt.Errorf("%s không phải thư mục thường", p)
		}
		real, err := pathutil.Canonical(p)
		if err != nil {
			return err
		}
		if !within(real, base) {
			return fmt.Errorf("%s trỏ ra ngoài store (%s)", p, real)
		}
	}
	return nil
}

func within(p, dir string) bool {
	rel, err := filepath.Rel(dir, p)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// List liệt kê profile theo tên thư mục. Mục không có dạng <tool>--<name> bị bỏ qua; mục có dạng đúng nhưng
// hỏng (symlink ra ngoài, profile.json sai…) không vào danh sách mà được gộp vào lỗi trả về (errors.Join),
// kèm các profile nạp được. Store chưa có thì danh sách rỗng, không lỗi.
func (s *Store) List() ([]Profile, error) {
	ents, err := os.ReadDir(s.profilesDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Profile
	var errs []error
	for _, de := range ents {
		t, name, perr := parseDirName(de.Name())
		if perr != nil {
			continue
		}
		p, lerr := s.Load(t, name)
		if lerr != nil {
			errs = append(errs, lerr)
			continue
		}
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b Profile) int { return strings.Compare(a.ID(), b.ID()) })
	return out, errors.Join(errs...)
}

// ResolveProfile nạp profile của tool theo cấu hình: mở store (OpenStore) rồi Load. Profile chưa có thì lỗi bọc
// ErrNotFound và nêu lệnh tạo (bint iso create); dùng cho bước bint run / bint eval, nơi không được tự tạo
// profile hay âm thầm quay về tài khoản thật.
func ResolveProfile(cfg config.Iso, t Tool, name string, env []string) (Profile, error) {
	st, err := OpenStore(cfg, env)
	if err != nil {
		return Profile{}, err
	}
	return st.LoadHinted(t, name)
}

// LoadHinted như Load nhưng khi chưa có thì lỗi nêu lệnh `bint iso create`.
func (s *Store) LoadHinted(t Tool, name string) (Profile, error) {
	p, err := s.Load(t, name)
	if errors.Is(err, ErrNotFound) {
		return p, hintedNotFound{msg: fmt.Sprintf("profile %s/%s chưa có; tạo bằng: bint iso create %s --name %s", t, name, t, name)}
	}
	return p, err
}

// hintedNotFound là ErrNotFound kèm lời nhắc lệnh tạo (errors.Is(err, ErrNotFound) vẫn đúng).
type hintedNotFound struct{ msg string }

func (e hintedNotFound) Error() string        { return e.msg }
func (e hintedNotFound) Is(target error) bool { return target == ErrNotFound }
