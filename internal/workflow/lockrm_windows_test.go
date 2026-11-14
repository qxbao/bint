//go:build windows

package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveHeldLockFileSharingViolationKeepsFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.lock")
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	other, err := os.Open(p) // handle của người khác: Remove báo lỗi chia sẻ
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := removeHeldLockFile(&Lock{}, p); err != nil {
		t.Fatalf("lỗi chia sẻ phải bị bỏ qua: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("tệp phải còn: %v", err)
	}
}

func TestRemoveHeldLockFileReturnsOtherErrors(t *testing.T) {
	p := filepath.Join(t.TempDir(), "dir.lock")
	if err := os.MkdirAll(filepath.Join(p, "con"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := removeHeldLockFile(&Lock{}, p); err == nil {
		t.Fatal("thư mục không rỗng: muốn lỗi được trả về")
	}
}

func TestRemoveHeldLockFileMissingAndRemovable(t *testing.T) {
	dir := t.TempDir()
	if err := removeHeldLockFile(&Lock{}, filepath.Join(dir, "khong-co")); err != nil {
		t.Fatalf("tệp không tồn tại: %v", err)
	}
	p := filepath.Join(dir, "y.lock")
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := removeHeldLockFile(&Lock{}, p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("tệp phải bị xoá: %v", err)
	}
}
