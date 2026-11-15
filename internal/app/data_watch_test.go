package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bint/internal/testutil"
)

func TestDataWatchSignature(t *testing.T) {
	root := t.TempDir()
	know := filepath.Join(root, ".bint", "knowledge", "items")
	tasks := filepath.Join(root, ".bint", "tasks")
	db := filepath.Join(root, "state.db")
	for _, d := range []string{know, tasks} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	w := DataWatch{Trees: []string{filepath.Join(root, ".bint"), filepath.Join(root, "missing-spool")}, Files: []string{db, db + "-wal"}}
	s0 := w.Signature()
	if w.Signature() != s0 {
		t.Fatal("chữ ký phải ổn định khi không đổi")
	}
	check := func(what string, act func()) {
		t.Helper()
		before := w.Signature()
		act()
		if w.Signature() == before {
			t.Fatalf("%s phải làm chữ ký đổi", what)
		}
	}
	item := filepath.Join(know, "a.md")
	check("thêm tệp knowledge", func() { os.WriteFile(item, []byte("x"), 0o644) })
	check("sửa tệp knowledge", func() { os.WriteFile(item, []byte("xyz"), 0o644) })
	check("đổi mtime", func() { os.Chtimes(item, time.Now(), time.Now().Add(-time.Hour)) })
	check("thêm tệp tác vụ", func() { os.WriteFile(filepath.Join(tasks, "t.yml"), []byte("id: 1"), 0o644) })
	check("tạo DB", func() { os.WriteFile(db, []byte("sqlite"), 0o644) })
	check("DB đổi kích thước", func() { os.WriteFile(db, []byte("sqlite-bigger"), 0o644) })
	check("WAL xuất hiện", func() { os.WriteFile(db+"-wal", []byte("w"), 0o644) })
	check("xoá tệp", func() { os.Remove(item) })
}

func TestResolveDataWatchNotInitialized(t *testing.T) {
	if _, ok := ResolveDataWatch(context.Background(), t.TempDir()); ok {
		t.Fatal("thư mục không phải repo đã init thì ok=false")
	}
}

// Ở worktree phụ không có .bint, cấu hình và tri thức nạp từ worktree chính: chữ ký phải theo dõi .bint ở đó.
func TestResolveDataWatchLinkedWorktreeWatchesMainBint(t *testing.T) {
	_, root, _ := taskRepo(t)
	wt := filepath.Join(t.TempDir(), "linked")
	testutil.Git(t, root, "worktree", "add", "-q", "-b", "side", wt, "HEAD")
	if _, err := os.Stat(filepath.Join(wt, ".bint")); err == nil {
		t.Skip("worktree phụ đã có .bint (được commit)")
	}
	w, ok := ResolveDataWatch(context.Background(), wt)
	if !ok {
		t.Fatal("ResolveDataWatch ở worktree phụ phải ok")
	}
	know := filepath.Join(root, ".bint", "knowledge")
	if err := os.MkdirAll(know, 0o755); err != nil {
		t.Fatal(err)
	}
	s0 := w.Signature()
	item := filepath.Join(know, "new.md")
	if err := os.WriteFile(item, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s1 := w.Signature()
	if s1 == s0 {
		t.Fatal("thêm tệp tri thức ở worktree chính phải làm chữ ký đổi")
	}
	if err := os.WriteFile(item, []byte("xyz-longer"), 0o644); err != nil {
		t.Fatal(err)
	}
	if w.Signature() == s1 {
		t.Fatal("sửa tệp tri thức ở worktree chính phải làm chữ ký đổi")
	}
}

func TestDataWatchSignatureIgnoresTaskLogsAndLocks(t *testing.T) {
	root := t.TempDir()
	tasks := filepath.Join(root, "runtime", "tasks")
	logs := filepath.Join(tasks, "T1", "logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	w := DataWatch{Trees: []string{tasks}}
	s0 := w.Signature()
	if err := os.WriteFile(filepath.Join(logs, "verify-1.log"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tasks, "T1", "x.lock"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, _ := os.OpenFile(filepath.Join(logs, "verify-1.log"), os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString("more")
	f.Close()
	if w.Signature() != s0 {
		t.Fatal("ghi logs/.lock không được làm chữ ký đổi")
	}
	if err := os.WriteFile(filepath.Join(tasks, "T1", "state.yml"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if w.Signature() == s0 {
		t.Fatal("tệp trạng thái phải làm chữ ký đổi")
	}
}
