//go:build !windows

package iso

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveProfileKeepsLinkTargets(t *testing.T) {
	_, p, _ := tmpProfile(t, Codex, "w")
	canary := t.TempDir()
	if err := os.WriteFile(filepath.Join(canary, "keep"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, l := range []string{".cache", filepath.Join(".codex", "packages"), filepath.Join(".local", "share", "uv")} {
		if _, err := EnsureLink(filepath.Join(p.Home, l), canary); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(p.Home, ".codex", "auth.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	tmp, err := p.EnsureTemp()
	if err != nil {
		t.Fatal(err)
	}
	if err := RemoveProfile(p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(canary, "keep")); err != nil {
		t.Fatalf("remove đi xuyên liên kết và xoá dữ liệu đích: %v", err)
	}
	for _, gone := range []string{p.Dir, tmp} {
		if _, err := os.Lstat(gone); !os.IsNotExist(err) {
			t.Errorf("%s phải bị xoá: %v", gone, err)
		}
	}
}

func TestRemoveProfileRefusals(t *testing.T) {
	_, p, _ := tmpProfile(t, Codex, "w")
	rel, busy, err := p.Lock(false)
	if err != nil || busy {
		t.Fatal(err, busy)
	}
	if err := RemoveProfile(p); !errors.Is(err, ErrBusy) {
		t.Fatalf("có phiên: err = %v, muốn ErrBusy", err)
	}
	rel()
	ticks, _ := procStartTicks(os.Getpid())
	writeDaemonPid(t, p, os.Getpid(), ticks)
	if err := RemoveProfile(p); !errors.Is(err, ErrDaemonAlive) {
		t.Fatalf("còn daemon: err = %v, muốn ErrDaemonAlive", err)
	}
	if _, err := os.Stat(p.Dir); err != nil {
		t.Fatalf("từ chối mà vẫn xoá: %v", err)
	}
	// Profile giả mạo đường dẫn: từ chối.
	fake := p
	fake.Dir = t.TempDir()
	if err := RemoveProfile(fake); err == nil {
		t.Fatal("thư mục ngoài store phải bị từ chối")
	}
}
