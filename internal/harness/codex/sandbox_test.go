package codex

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSandboxAllowsWrite(t *testing.T) {
	repo := t.TempDir()
	stateBase := t.TempDir()
	stateDir := filepath.Join(stateBase, "repo-abc")
	for _, tc := range []struct {
		name, config string
		dir          string
		want         bool
	}{
		{"không có cấu hình", "", stateDir, false},
		{"thư mục trong repository", "", filepath.Join(repo, ".bint", "state"), true},
		{"writable_roots chứa thư mục cha", "[sandbox_workspace_write]\nwritable_roots = ['" + stateBase + "']\n", stateDir, true},
		{"writable_roots khác", "[sandbox_workspace_write]\nwritable_roots = ['" + filepath.Join(stateBase, "other") + "']\n", stateDir, false},
		{"tiền tố tên không phải thư mục cha", "[sandbox_workspace_write]\nwritable_roots = ['" + filepath.Join(stateBase, "repo") + "']\n", stateDir, false},
		{"danger-full-access", "sandbox_mode = \"danger-full-access\"\n", stateDir, true},
		{"TOML hỏng", "[sandbox_workspace_write\n", stateDir, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("CODEX_HOME", home)
			if tc.config != "" {
				if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(tc.config), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if got := SandboxAllowsWrite(repo, tc.dir); got != tc.want {
				t.Fatalf("SandboxAllowsWrite = %v, muốn %v", got, tc.want)
			}
		})
	}
}
