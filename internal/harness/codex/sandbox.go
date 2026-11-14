package codex

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// sandboxConfig là phần của config.toml (Codex) quyết định phạm vi ghi của sandbox.
type sandboxConfig struct {
	SandboxMode           string `toml:"sandbox_mode"`
	SandboxWorkspaceWrite struct {
		WritableRoots []string `toml:"writable_roots"`
	} `toml:"sandbox_workspace_write"`
}

// SandboxAllowsWrite cho biết phiên Codex tương tác (sandbox workspace-write) có ghi được dir hay không,
// dựa trên ~/.codex/config.toml và <repo>/.codex/config.toml (chỉ khi dự án được tin cậy).
// Sandbox mặc định chỉ cho ghi cây làm việc và thư mục tạm, nên lệnh `bint` do agent chạy không ghi
// được thư mục trạng thái nằm ngoài repository (spool, SQLite) trừ khi nó nằm trong writable_roots.
// Không xét cờ dòng lệnh (--sandbox, --add-dir) hay profile: chỉ là ước lượng cho doctor.
func SandboxAllowsWrite(repoRoot, dir string) bool {
	if within(repoRoot, dir) {
		return true
	}
	var files []string
	if h, err := Home(); err == nil {
		files = append(files, filepath.Join(h, "config.toml"))
	}
	if ProjectTrusted(repoRoot) {
		files = append(files, ConfigPath(repoRoot))
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var c sandboxConfig
		if toml.Unmarshal(b, &c) != nil {
			continue
		}
		if c.SandboxMode == "danger-full-access" {
			return true
		}
		for _, r := range c.SandboxWorkspaceWrite.WritableRoots {
			if within(r, dir) {
				return true
			}
		}
	}
	return false
}

// SandboxSnippet là đoạn TOML cần thêm vào ~/.codex/config.toml để sandbox ghi được dir.
func SandboxSnippet(dir string) string {
	return "[sandbox_workspace_write]\nwritable_roots = ['" + dir + "']"
}

// within: dir nằm trong (hoặc trùng) root.
func within(root, dir string) bool {
	if root == "" || !filepath.IsAbs(root) {
		return false
	}
	r, d := normPath(root), normPath(dir)
	return d == r || strings.HasPrefix(d, strings.TrimSuffix(r, "/")+"/")
}

func normPath(p string) string {
	p = filepath.ToSlash(filepath.Clean(p))
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		p = strings.ToLower(p)
	}
	return p
}
