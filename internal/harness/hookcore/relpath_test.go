package hookcore

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRelPathThroughSymlink(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "repo alias")
	if err := os.Symlink(root, alias); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink privilege unavailable: %v", err)
		}
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "migrations"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../README.md", filepath.Join(root, "migrations", "001.sql")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../src", filepath.Join(root, "migrations", "linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("..", filepath.Join(root, "migrations", "back")); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path, want string
		inside     bool
	}{
		{filepath.Join(alias, "migrations", "001.sql"), "migrations/001.sql", true},
		{filepath.Join(root, "migrations", "001.sql"), "migrations/001.sql", true},
		{"migrations/001.sql", "migrations/001.sql", true},
		{filepath.Join(alias, "migrations", "002.sql"), "migrations/002.sql", true},
		{filepath.Join(alias, "migrations", "linked", "new.go"), "migrations/linked/new.go", true},
		{filepath.Join(alias, "migrations", "back", "new.go"), "migrations/back/new.go", true},
		{filepath.Join(alias, "README.md"), "README.md", true},
		{filepath.Join(alias, "escape", "missing.go"), "", false},
		{filepath.Join(outside, "README.md"), "", false},
	} {
		t.Run(tc.path, func(t *testing.T) {
			if got, ok := RelPath(root, alias, tc.path); got != tc.want || ok != tc.inside {
				t.Fatalf("RelPath = %q, %v; want %q, %v", got, ok, tc.want, tc.inside)
			}
		})
	}
}

// A dangling symlink must be checked under its target's name too, and
// pre/post hooks must report the same names for the edit.
func TestEditRelsDanglingSymlinkTarget(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"src", "migrations"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("../migrations/999.sql", filepath.Join(root, "src", "x.sql")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink privilege unavailable: %v", err)
		}
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "job")
	if err := os.Symlink(outside, filepath.Join(root, "src", "out")); err != nil {
		t.Fatal(err)
	}
	got := editRels(root, root, []string{"src/x.sql", "src/out"})
	want := []string{"src/x.sql", "migrations/999.sql"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("editRels = %v; want %v (src/out writes outside the repository)", got, want)
	}
}
