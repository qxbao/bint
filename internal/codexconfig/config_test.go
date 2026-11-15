package codexconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectRootAbsoluteMarkers(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(t.TempDir(), "marker")
	if err := os.WriteFile(existing, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "MISSING")
	// On Unix, leave a decoy where the old filepath.Join would look. The
	// actual absolute marker remains absent, independently of root's files.
	if filepath.VolumeName(missing) == "" {
		decoy := filepath.Join(root, missing)
		if err := os.MkdirAll(filepath.Dir(decoy), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(decoy, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name    string
		markers []string
		want    string
	}{
		{"relative", []string{".git"}, root},
		{"absolute absent", []string{missing}, child},
		{"absolute exists", []string{existing}, child},
		{"absolute beats ancestor", []string{".git", existing}, child},
		{"missing falls through", []string{missing, ".git"}, root},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := (Config{ProjectRootMarkers: tc.markers}).projectRoot(child); got != tc.want {
				t.Fatalf("projectRoot = %q; want %q", got, tc.want)
			}
		})
	}
}

func TestTrustLevelToleratesUncleanKeys(t *testing.T) {
	dir := canonical(t.TempDir())
	type proj = struct {
		TrustLevel string `toml:"trust_level"`
	}
	for _, key := range []string{dir + string(filepath.Separator), filepath.ToSlash(dir) + "/"} {
		c := Config{Projects: map[string]proj{key: {TrustLevel: "trusted"}}}
		if level, ok := c.trustLevel(dir); !ok || level != "trusted" {
			t.Fatalf("key %q: level=%q ok=%v", key, level, ok)
		}
	}
	c := Config{Projects: map[string]proj{dir + "/": {TrustLevel: "trusted"}, dir + "//": {TrustLevel: "untrusted"}}}
	if level, _ := c.trustLevel(dir); level != "untrusted" {
		t.Fatalf("conflicting keys must deny: %q", level)
	}
}

func TestDotGitRootsLinkedWorktree(t *testing.T) {
	main := canonical(t.TempDir())
	wt := canonical(t.TempDir())
	admin := filepath.Join(main, ".git", "worktrees", "wt")
	if err := os.MkdirAll(admin, 0o700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(admin, "commondir"), []byte("../..\n"), 0o600)
	os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+filepath.ToSlash(admin)+"\n"), 0o600)
	sub := filepath.Join(wt, "sub")
	os.Mkdir(sub, 0o700)
	if root, m := dotGitRoots(sub); root != wt || m != main {
		t.Fatalf("dotGitRoots = %q, %q; want %q, %q", root, m, wt, main)
	}
	if root, m := dotGitRoots(main); root != main || m != main {
		t.Fatalf("main: %q, %q", root, m)
	}
}
