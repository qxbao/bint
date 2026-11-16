//go:build windows

package iso

import "testing"

func TestEnsureTempWindows(t *testing.T) {
	t.Setenv("TEMP", t.TempDir())
	t.Setenv("TMP", t.TempDir())
	p := mustCreate(t, testStore(t, nil), Codex, "w")
	dir, err := p.EnsureTemp()
	if err != nil || dir == "" {
		t.Fatalf("%q %v", dir, err)
	}
}
