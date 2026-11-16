package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"bint/internal/config"
	"bint/internal/iso"
)

func isoDoctorCheck(t *testing.T, root string) (Check, bool) {
	t.Helper()
	for _, c := range Doctor(context.Background(), root).Checks {
		if c.Name == "iso" {
			return c, true
		}
	}
	return Check{}, false
}

func isoTestProfile(t *testing.T) iso.Profile {
	t.Helper()
	st, err := iso.OpenStore(config.Default().Iso, os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	p, err := st.Create(iso.Codex, "work")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDoctorIsoRow(t *testing.T) {
	root := sysRepo(t, InitOptions{})
	if c, ok := isoDoctorCheck(t, root); ok {
		t.Fatalf("store chưa có profile mà doctor có dòng iso: %+v", c)
	}

	p := isoTestProfile(t)
	c, ok := isoDoctorCheck(t, root)
	if !ok {
		t.Fatal("store có profile mà doctor không có dòng iso")
	}
	if c.Level != LevelInfo || !strings.Contains(c.Detail, "1 profile") || !strings.Contains(c.Detail, "bint iso gc") {
		t.Fatalf("dòng iso = %+v", c)
	}

	// Dữ liệu người dùng trong HOME giả ⇒ warn, nêu tên mục.
	proj := filepath.Join(p.Home, "Projects", "x")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "a.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, _ = isoDoctorCheck(t, root)
	if c.Level != LevelWarn || !strings.Contains(c.Detail, "Projects/") || !strings.Contains(c.Detail, "không tự xoá") {
		t.Fatalf("có Projects/ phải warn: %+v", c)
	}
}

// Liên kết chia sẻ xung đột (thư mục có dữ liệu ở đúng chỗ liên kết) ⇒ warn.
func TestDoctorIsoWarnsOnShareConflict(t *testing.T) {
	root := sysRepo(t, InitOptions{})
	p := isoTestProfile(t)
	cache := filepath.Join(p.Home, ".npm")
	if runtime.GOOS == "windows" {
		cache = filepath.Join(p.Home, "AppData", "Local", "npm-cache") // chỗ liên kết npm của danh mục Windows
	}
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, ok := isoDoctorCheck(t, root)
	if !ok || c.Level != LevelWarn || !strings.Contains(c.Detail, "conflict-data") {
		t.Fatalf("dòng iso = %+v (ok=%v)", c, ok)
	}
}
