// Package codexconfig reads the local Codex configuration needed by Bint.
// It never modifies trust or starts a model.
package codexconfig

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"bint/internal/gitrepo"
	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	// nil uses Codex's default .git marker; an explicitly empty list makes
	// cwd the project boundary, without disabling Git-main trust fallback.
	ProjectRootMarkers []string       `toml:"project_root_markers"`
	MCPServers         map[string]any `toml:"mcp_servers"`
	Projects           map[string]struct {
		TrustLevel string `toml:"trust_level"`
	} `toml:"projects"`
}

func Read(path string) (Config, error) {
	var cfg Config
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := toml.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func canonical(dir string) string {
	if dir == "" {
		return ""
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	return filepath.Clean(dir)
}

func (c Config) hasTrusted() bool {
	for _, p := range c.Projects {
		if p.TrustLevel == "trusted" {
			return true
		}
	}
	return false
}

func (c Config) trustLevel(dir string) (string, bool) {
	for _, key := range []string{dir, filepath.ToSlash(dir)} {
		if p, ok := c.Projects[key]; ok {
			return p.TrustLevel, true
		}
	}
	// Khoá viết tay có thể chưa chuẩn hoá (dấu phân cách cuối, "/" thay "\",
	// khác hoa thường ổ đĩa/thư mục trên Windows): so sau khi Clean. Nhiều khoá
	// cùng trỏ một thư mục thì mức khác "trusted" thắng, để kết quả không phụ
	// thuộc thứ tự duyệt map.
	level, found := "", false
	for key, p := range c.Projects {
		if samePath(cleanKey(key), dir) && (!found || level == "trusted") {
			level, found = p.TrustLevel, true
		}
	}
	return level, found
}

func cleanKey(key string) string {
	if key == "" {
		return ""
	}
	return filepath.Clean(filepath.FromSlash(key))
}

func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func (c Config) trusts(dir, root, main string) bool {
	// Exact layer trust wins, followed by the configured project boundary,
	// then the main Git repository. A worktree's explicit denial therefore
	// also governs its child layers, unless a child has its own entry.
	for _, candidate := range []string{dir, root, main} {
		if candidate != "" {
			if level, ok := c.trustLevel(candidate); ok {
				return level == "trusted"
			}
		}
	}
	return false
}

// markerPath follows Rust Path::join, used by Codex. Go's filepath.Join
// appends absolute paths instead of replacing the base. On Windows a prefix
// (including drive-relative paths) also replaces it, while a root without a
// prefix keeps only the base volume.
func markerPath(dir, marker string) string {
	if filepath.IsAbs(marker) || filepath.VolumeName(marker) != "" {
		return marker
	}
	if len(marker) > 0 && os.IsPathSeparator(marker[0]) {
		return filepath.VolumeName(dir) + marker
	}
	return filepath.Join(dir, marker)
}

func (c Config) projectRoot(dir string) string {
	markers := c.ProjectRootMarkers
	if markers == nil {
		markers = []string{".git"}
	}
	for current := dir; ; current = filepath.Dir(current) {
		for _, marker := range markers {
			if _, err := os.Stat(markerPath(current, marker)); err == nil {
				return current
			}
		}
		if filepath.Dir(current) == current {
			return dir
		}
	}
}

// gitTimeout giới hạn lần gọi git rev-parse; spawn git trên Windows có thể chậm
// (Defender quét), và khi hết hạn repoRoots vẫn dựng lại được từ tệp .git.
const gitTimeout = 10 * time.Second

func repoRoots(dir string) (root, main string) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	// Codex agent processes strip parent Git identity (e.g. when Bint runs in
	// a Git hook). Discovery must describe the same repository as that child.
	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(key) {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR":
			continue
		}
		env = append(env, kv)
	}
	if r, err := gitrepo.DiscoverWithEnv(ctx, dir, env); err == nil {
		main = r.MainWorktree()
		if main == "" || !r.IsLinkedWorktree() {
			// Separate Git dirs and submodules need not store metadata in
			// <root>/.git. Codex uses the current toplevel as the trust root
			// even for linked worktrees when no main root can be inferred.
			main = r.Toplevel
		}
		return canonical(r.Toplevel), canonical(main)
	}
	return dotGitRoots(dir)
}

// dotGitRoots dựng toplevel/worktree chính từ tệp .git khi không gọi được git
// (hết hạn, không có git): <top>/.git là thư mục thì top là gốc chính; là tệp
// "gitdir: <dir>" thì đọc <dir>/commondir để tìm <main>/.git.
func dotGitRoots(dir string) (root, main string) {
	for current := dir; ; current = filepath.Dir(current) {
		dotGit := filepath.Join(current, ".git")
		st, err := os.Stat(dotGit)
		if err == nil && st.IsDir() {
			return current, current
		}
		if err == nil {
			b, err := os.ReadFile(dotGit)
			gitDir, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir:")
			if err != nil || !ok {
				return dir, ""
			}
			gitDir = gitrepo.Clean(gitDir)
			if !filepath.IsAbs(gitDir) {
				gitDir = filepath.Join(current, gitDir)
			}
			common := gitDir
			if b, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
				common = gitrepo.Clean(string(b))
				if !filepath.IsAbs(common) {
					common = filepath.Join(gitDir, common)
				}
			}
			main = current
			if m := (gitrepo.Repo{CommonDir: filepath.Clean(common)}).MainWorktree(); m != "" {
				main = m
			}
			return canonical(current), canonical(main)
		}
		if filepath.Dir(current) == current {
			return dir, ""
		}
	}
}

func (c Config) Trusted(dir string) bool {
	if dir == "" || !c.hasTrusted() {
		return false
	}
	dir = canonical(dir)
	if _, ok := c.trustLevel(dir); ok {
		return c.trusts(dir, "", "")
	}
	_, main := repoRoots(dir)
	return c.trusts(dir, c.projectRoot(dir), main)
}

// TrustedDirs returns project config directories from cwd up to the current
// configured project boundary. Each layer has its own trust decision.
// When its root is known, linked worktrees inherit the main repository's trust,
// not its config files. Otherwise they use the current worktree's trust root.
func (c Config) TrustedDirs(dir string) []string {
	if dir == "" || !c.hasTrusted() {
		return nil
	}
	dir = canonical(dir)
	root := c.projectRoot(dir)
	_, main := repoRoots(dir)
	var dirs []string
	for {
		if c.trusts(dir, root, main) {
			dirs = append(dirs, dir)
		}
		parent := filepath.Dir(dir)
		if dir == root || parent == dir {
			return dirs
		}
		dir = parent
	}
}
