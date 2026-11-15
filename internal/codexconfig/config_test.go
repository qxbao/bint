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
