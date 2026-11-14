// Package codexconfig reads the local Codex configuration needed by Bint.
// It never modifies trust or starts a model.
package codexconfig

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"bint/internal/gitrepo"
	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	MCPServers map[string]any `toml:"mcp_servers"`
	Projects   map[string]struct {
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

func (c Config) trusts(dir, main string) bool {
	// Codex checks an exact directory entry first, then the main repository.
	// Explicit untrusted entries override the repository default. An arbitrary
	// trusted parent outside the repository does not confer trust.
	if level, ok := c.trustLevel(dir); ok {
		return level == "trusted"
	}
	level, _ := c.trustLevel(main)
	return main != "" && level == "trusted"
}

func repoRoots(dir string) (root, main string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if r, err := gitrepo.Discover(ctx, dir); err == nil {
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
		return c.trusts(dir, "")
	}
	_, main := repoRoots(dir)
	return c.trusts(dir, main)
}

// TrustedDirs returns project config directories from cwd up to the current
// worktree root. Each layer has its own trust decision, as in Codex 0.169.
// When its root is known, linked worktrees inherit the main repository's trust,
// not its config files. Otherwise they use the current worktree's trust root.
func (c Config) TrustedDirs(dir string) []string {
	if dir == "" || !c.hasTrusted() {
		return nil
	}
	dir = canonical(dir)
	root, main := repoRoots(dir)
	var dirs []string
	for {
		if c.trusts(dir, main) {
			dirs = append(dirs, dir)
		}
		parent := filepath.Dir(dir)
		if dir == root || parent == dir {
			return dirs
		}
		dir = parent
	}
}
