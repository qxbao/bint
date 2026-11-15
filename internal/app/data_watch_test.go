package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
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
