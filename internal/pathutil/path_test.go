package pathutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCanonicalExistingAndRemovedThroughSymlink(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias with spaces")
	if err := os.Symlink(root, alias); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink privilege unavailable: %v", err)
		}
		t.Fatal(err)
	}
	file := filepath.Join(root, "directory", "File.txt")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, removed := range []bool{false, true} {
		if removed {
			if err := os.RemoveAll(filepath.Dir(file)); err != nil {
				t.Fatal(err)
			}
		}
		got, err := Canonical(filepath.Join(alias, "directory", "File.txt"))
		if err != nil || got != file {
			t.Fatalf("removed=%v: got %q, %v; want %q", removed, got, err, file)
		}
		other, err := Canonical(filepath.Join(alias, "directory", "Other.txt"))
		if err != nil || other == got {
			t.Fatalf("different paths conflated: %q, %v", other, err)
		}
	}
	loop := filepath.Join(root, "loop")
	if err := os.Symlink(loop, loop); err != nil {
		t.Fatal(err)
	}
	if _, err := Canonical(loop); err == nil {
		t.Fatal("symlink loop treated as a missing path")
	}
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink privilege unavailable: %v", err)
		}
		t.Fatal(err)
	}
}

// A write through a dangling symlink creates its target, so Canonical must
// report the target, not the link's own name.
func TestCanonicalFollowsDanglingSymlink(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"src", "migrations"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	symlinkOrSkip(t, "../migrations/999.sql", filepath.Join(root, "src", "x.sql"))
	symlinkOrSkip(t, "../migrations/new", filepath.Join(root, "src", "dir"))
	for in, want := range map[string]string{
		filepath.Join(root, "src", "x.sql"):         filepath.Join(root, "migrations", "999.sql"),
		filepath.Join(root, "src", "dir", "a", "b"): filepath.Join(root, "migrations", "new", "a", "b"),
	} {
		if got, err := Canonical(in); err != nil || got != want {
			t.Errorf("Canonical(%s) = %q, %v; want %q", in, got, err, want)
		}
	}
	// Relative targets resolve against the physical directory of the link:
	// alias -> root/src, so alias/up -> ../migrations means root/migrations.
	alias := filepath.Join(t.TempDir(), "alias")
	symlinkOrSkip(t, filepath.Join(root, "src"), alias)
	symlinkOrSkip(t, "../migrations/up.sql", filepath.Join(root, "src", "up"))
	if got, err := Canonical(filepath.Join(alias, "up")); err != nil || got != filepath.Join(root, "migrations", "up.sql") {
		t.Errorf("relative target through symlinked dir = %q, %v", got, err)
	}
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	symlinkOrSkip(t, b, a)
	symlinkOrSkip(t, a, b)
	if _, err := Canonical(a); err == nil {
		t.Error("cycle of links treated as a path")
	}
}

func TestBestEffortFallsBackToLexicalPath(t *testing.T) {
	root := t.TempDir()
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	symlinkOrSkip(t, b, a)
	symlinkOrSkip(t, a, b)
	p := filepath.Join(a, "file")
	if got := BestEffort(p); got != p {
		t.Fatalf("BestEffort(%s) = %q; want lexical fallback", p, got)
	}
}
