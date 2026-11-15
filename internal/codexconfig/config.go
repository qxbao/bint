// Package codexconfig reads the local Codex configuration needed by Bint.
// It never modifies trust or starts a model.
package codexconfig

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	return "", false
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

func repoRoots(dir string) (root, main string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
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
	return dir, ""
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
