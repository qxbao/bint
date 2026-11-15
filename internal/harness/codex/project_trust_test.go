package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"bint/internal/testutil"
)

func TestProjectTrustParsesTOML(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	t.Setenv("CODEX_HOME", home)
	quoted, _ := json.Marshal(filepath.ToSlash(root))
	for _, tc := range []struct {
		name, config string
		want         bool
	}{
		{"basic", "[projects." + string(quoted) + "]\ntrust_level = \"trusted\"\n", true},
		{"literal", "[projects.'" + filepath.ToSlash(root) + "']\ntrust_level = 'trusted'\n", true},
		{"inline", "projects = {" + string(quoted) + " = {trust_level = 'trusted'}}\n", true},
		{"commented header", "# [projects." + string(quoted) + "]\n[projects.other]\ntrust_level = \"trusted\"\n", false},
		{"indented unrelated table", "[projects." + string(quoted) + "]\ntrust_level = \"untrusted\"\n  [projects.other]\ntrust_level = \"trusted\"\n", false},
		{"trust prefix", "[projects." + string(quoted) + "]\ntrust_level = \"trusted\"garbage\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(tc.config), 0o600); err != nil {
				t.Fatal(err)
			}
			if got := ProjectTrusted(root); got != tc.want {
				t.Fatalf("ProjectTrusted = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestProjectTrustWorktreeAndExplicitDenial(t *testing.T) {
	repo := testutil.NewRepo(t, map[string]string{"README.md": "fixture\n"})
	repo, _ = filepath.EvalSymlinks(repo)
	wt := filepath.Join(t.TempDir(), "wt")
	testutil.Git(t, repo, "worktree", "add", "-q", "-b", "fixture", wt)
	wt, _ = filepath.EvalSymlinks(wt)
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	quotedRepo, _ := json.Marshal(repo)
	quotedWT, _ := json.Marshal(wt)
	config := "[projects." + string(quotedRepo) + "]\ntrust_level='trusted'\n"
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	if !ProjectTrusted(wt) {
		t.Fatal("linked worktree did not inherit main repository trust")
	}
	config += "\n[projects." + string(quotedWT) + "]\ntrust_level='untrusted'\n"
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	if ProjectTrusted(wt) {
		t.Fatal("explicit worktree untrusted must override inherited trust")
	}
}
