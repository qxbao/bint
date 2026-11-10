package hookcore

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func gitRepo(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if out, err := exec.Command("git", "-C", d, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git: %v %s", err, out)
	}
	os.WriteFile(filepath.Join(d, "big.bin"), []byte(strings.Repeat("x", 4096)), 0o644)
	os.WriteFile(filepath.Join(d, "small.txt"), []byte("hi"), 0o644)
	return d
}

func TestSnapshotExpiredContextGivesNothing(t *testing.T) {
	d := gitRepo(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(2 * time.Millisecond)
	if s, ok := takeSnapshot(ctx, d); ok || s != nil {
		t.Fatalf("quá hạn phải không có ảnh: %v %v", s, ok)
	}
}

func TestSnapshotHashBudgetFallsBackToSizeMtime(t *testing.T) {
	d := gitRepo(t)
	old := hashBudget
	hashBudget = 100 // nhỏ hơn big.bin
	defer func() { hashBudget = old }()
	s, ok := takeSnapshot(context.Background(), d)
	if !ok {
		t.Fatal("không chụp được")
	}
	if got := s["big.bin"]; !strings.Contains(got, ":4096:") {
		t.Errorf("big.bin phải dùng size+mtime: %q", got)
	}
	if got := s["small.txt"]; strings.Count(got, ":") != 1 {
		t.Errorf("small.txt phải được băm: %q", got)
	}
}

func TestSaveSnapshotCleansOrphans(t *testing.T) {
	rt := t.TempDir()
	orphan := filepath.Join(rt, "shellsnap-old.json")
	os.WriteFile(orphan, []byte("{}"), 0o644)
	past := time.Now().Add(-2 * snapMaxAge)
	os.Chtimes(orphan, past, past)
	saveSnapshot(rt, "claude", "s", "t1", treeSnapshot{})
	if _, err := os.Stat(orphan); err == nil {
		t.Error("ảnh mồ côi chưa được dọn")
	}
	if _, ok := loadSnapshot(rt, "claude", "s", "t1"); !ok {
		t.Error("ảnh mới phải đọc được")
	}
	if _, ok := loadSnapshot(rt, "claude", "s", "t2"); ok {
		t.Error("khoá khác tool_use_id không được dùng chung ảnh")
	}
}
