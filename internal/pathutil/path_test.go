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
