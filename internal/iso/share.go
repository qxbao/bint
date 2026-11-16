package iso

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Share là một mục chia sẻ cache (docs/iso-plan.md mục 5.4): HOME giả liên kết tới thư mục thật để profile
// không nhân bản cache. Liên kết là cơ chế chính vì tiến trình con (MCP stdio của Codex) có thể bỏ biến
// env (S1); Env bổ sung cho công cụ đọc env.
type Share struct {
	Name  string
	Links []ShareLink
	Env   []ShareEnv
}

// ShareLink: InHome là đường dẫn tương đối trong HOME giả; Real là thư mục thật (tuyệt đối).
type ShareLink struct{ InHome, Real string }

// ShareEnv là biến env của mục (đặt khi mục có hiệu lực).
type ShareEnv struct{ Name, Value string }

// catalog trả danh mục share cho goos, theo đúng thứ tự config.IsoShareNames, với thư mục thật tính từ
// env thật e. Linux/macOS dựa trên bố cục XDG/Library; bản Windows là bản tốt nhất có thể, chưa kiểm trên
// máy Windows (S5 trong docs/iso-plan.md mục 11.5).
func catalog(goos string, e *envList) []Share {
	home := e.val("HOME")
	if goos == "windows" {
		home = e.val("USERPROFILE")
	}
	abs := func(k string) string {
		if v := e.val(k); filepath.IsAbs(v) {
			return v
		}
		return ""
	}
	or := func(v, def string) string {
		if v != "" {
			return v
		}
		return def
	}
	j := filepath.Join
	local := or(e.val("LOCALAPPDATA"), j(home, "AppData", "Local"))
	roaming := or(e.val("APPDATA"), j(home, "AppData", "Roaming"))
	dataHome := or(abs("XDG_DATA_HOME"), j(home, ".local", "share"))
	cache := or(abs("XDG_CACHE_HOME"), j(home, ".cache"))
	goMod := or(abs("GOMODCACHE"), j(or(firstPath(e.val("GOPATH")), j(home, "go")), "pkg", "mod"))
	cargo := or(abs("CARGO_HOME"), j(home, ".cargo"))
	bun := or(abs("BUN_INSTALL"), j(home, ".bun"))

	var out []Share
	add := func(s Share) { out = append(out, s) }
	switch goos {
	case "windows":
		npm, pnpm := j(local, "npm-cache"), j(local, "pnpm", "store")
		add(Share{Name: "cache", Links: []ShareLink{
			{".cache", cache},
			{`AppData\Local\uv\cache`, j(local, "uv", "cache")},
			{`AppData\Local\pip\Cache`, j(local, "pip", "Cache")},
			{`AppData\Local\go-build`, j(local, "go-build")},
		}, Env: []ShareEnv{{"XDG_CACHE_HOME", cache}}})
		add(Share{Name: "npm", Links: []ShareLink{{`AppData\Local\npm-cache`, npm}}, Env: []ShareEnv{{"npm_config_cache", npm}}})
		add(Share{Name: "pnpm-store", Links: []ShareLink{{`AppData\Local\pnpm\store`, pnpm}}, Env: []ShareEnv{{"npm_config_store_dir", pnpm}}})
		add(Share{Name: "uv-data", Links: []ShareLink{{`AppData\Roaming\uv\data`, j(roaming, "uv", "data")}}})
	case "darwin":
		pnpm := j(home, "Library", "pnpm", "store")
		add(Share{Name: "cache", Links: []ShareLink{
			{".cache", cache},
			{"Library/Caches", j(home, "Library", "Caches")},
		}, Env: []ShareEnv{{"XDG_CACHE_HOME", cache}}})
		add(Share{Name: "npm", Links: []ShareLink{{".npm", j(home, ".npm")}}, Env: []ShareEnv{{"npm_config_cache", j(home, ".npm")}}})
		add(Share{Name: "pnpm-store", Links: []ShareLink{{"Library/pnpm/store", pnpm}}, Env: []ShareEnv{{"npm_config_store_dir", pnpm}}})
		add(Share{Name: "uv-data", Links: []ShareLink{{".local/share/uv", j(dataHome, "uv")}}})
	default:
		pnpm := j(dataHome, "pnpm", "store")
		add(Share{Name: "cache", Links: []ShareLink{{".cache", cache}}, Env: []ShareEnv{{"XDG_CACHE_HOME", cache}}})
		add(Share{Name: "npm", Links: []ShareLink{{".npm", j(home, ".npm")}}, Env: []ShareEnv{{"npm_config_cache", j(home, ".npm")}}})
		add(Share{Name: "pnpm-store", Links: []ShareLink{{".local/share/pnpm/store", pnpm}}, Env: []ShareEnv{{"npm_config_store_dir", pnpm}}})
		add(Share{Name: "uv-data", Links: []ShareLink{{".local/share/uv", j(dataHome, "uv")}}})
	}
	add(Share{Name: "go-mod", Links: []ShareLink{{"go/pkg/mod", goMod}}, Env: []ShareEnv{{"GOMODCACHE", goMod}}})
	add(Share{Name: "cargo-registry", Links: []ShareLink{{".cargo/registry", j(cargo, "registry")}}})
	add(Share{Name: "bun", Links: []ShareLink{{".bun/install/cache", j(bun, "install", "cache")}},
		Env: []ShareEnv{{"BUN_INSTALL_CACHE_DIR", j(bun, "install", "cache")}}})
	return out
}

// firstPath lấy phần tử đầu của danh sách đường dẫn kiểu GOPATH.
func firstPath(list string) string {
	first, _, _ := strings.Cut(list, string(filepath.ListSeparator))
	return first
}

// selected trả các mục có hiệu lực của profile: chỉ khi cache là shared và mục có trong iso.share.
func (p Profile) selected(goos string, real *envList) []Share {
	var out []Share
	for _, s := range catalog(goos, real) {
		if p.Settings.shares(s.Name) {
			out = append(out, s)
		}
	}
	return out
}

// ShareResult là kết quả liên kết một mục của EnsureShares.
type ShareResult struct {
	Share  string
	Link   string // đường dẫn trong HOME giả
	Target string // thư mục thật
	State  LinkState
}

// EnsureShares tạo/kiểm liên kết chia sẻ của profile (create gọi một lần, run kiểm lại mỗi lần). Thư mục
// thật chưa có thì được tạo (0755) để liên kết không treo. Xung đột (thư mục có dữ liệu, liên kết chỗ khác)
// không ghi đè mà trả trong kết quả (State.Conflict()). Cache private thì không làm gì.
func EnsureShares(p Profile) ([]ShareResult, error) {
	return ensureShares(runtime.GOOS, p, os.Environ())
}

func ensureShares(goos string, p Profile, env []string) ([]ShareResult, error) {
	e := newEnvList(goos, env)
	restoreReal(e)
	var res []ShareResult
	var errs []error
	for _, s := range catalog(goos, e) {
		effective := p.Settings.shares(s.Name)
		for _, l := range s.Links {
			link := shareLinkPath(p, l)
			if !effective {
				// Mục không còn hiệu lực (cache private hoặc bỏ khỏi iso.share): gỡ liên kết mà GĐ trước/lần
				// trước đã tạo, chỉ khi nó trỏ đúng đích của danh mục. Dữ liệu ở đích không bị chạm; liên kết
				// trỏ chỗ khác và thư mục thật (kể cả rỗng) giữ nguyên.
				st, err := InspectLink(link, l.Real)
				if err != nil {
					errs = append(errs, fmt.Errorf("share %s: %w", s.Name, err))
					continue
				}
				if st != LinkOK {
					continue
				}
				if err := Unlink(link); err != nil {
					errs = append(errs, fmt.Errorf("share %s: %w", s.Name, err))
					continue
				}
				res = append(res, ShareResult{Share: s.Name, Link: link, Target: l.Real, State: LinkRemoved})
				continue
			}
			if err := os.MkdirAll(l.Real, 0o755); err != nil {
				errs = append(errs, fmt.Errorf("share %s: %w", s.Name, err))
				continue
			}
			st, err := EnsureLink(link, l.Real)
			if err != nil {
				errs = append(errs, fmt.Errorf("share %s: %w", s.Name, err))
				continue
			}
			res = append(res, ShareResult{Share: s.Name, Link: link, Target: l.Real, State: st})
		}
	}
	// XDG_CACHE_HOME của Env luôn là home/.cache: khi cache riêng thì thư mục phải có chỗ ghi.
	if !p.Settings.shares("cache") {
		if err := ensureOwnCache(p); err != nil {
			errs = append(errs, err)
		}
	}
	return res, errors.Join(errs...)
}

// ensureOwnCache tạo home/.cache (0700) khi chưa có gì ở đó. Có sẵn (thư mục, liên kết lạ) thì không đụng.
func ensureOwnCache(p Profile) error {
	dir := filepath.Join(p.Home, ".cache")
	if _, err := os.Lstat(dir); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.MkdirAll(dir, 0o700)
}

// shareLinkPath là đường dẫn của liên kết trong HOME giả (InHome dùng "/" hoặc "\\" làm dấu phân cách).
func shareLinkPath(p Profile, l ShareLink) string {
	return filepath.Join(p.Home, filepath.FromSlash(strings.ReplaceAll(l.InHome, `\\`, "/")))
}
