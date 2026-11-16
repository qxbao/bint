package workflow

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/mcpgw"
)

func TestCopyCodegraphIndexConsistent(t *testing.T) {
	origin, wt := t.TempDir(), t.TempDir()
	dir := filepath.Join(origin, mcpgw.CodegraphDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "codegraph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, q := range []string{"PRAGMA journal_mode=WAL", "PRAGMA wal_autocheckpoint=0", "CREATE TABLE t(v TEXT)", "INSERT INTO t VALUES('chua-checkpoint')"} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for name := range map[string]bool{".gitignore": true, "codegraph.pid": true, "daemon.sock": true} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("*\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := CopyCodegraphIndex(context.Background(), origin, wt); err != nil {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(filepath.Join(wt, mcpgw.CodegraphDir))
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != ".gitignore,codegraph.db" {
		t.Fatalf("tệp trong worktree: %v", names)
	}
	c, err := sql.Open("sqlite", filepath.Join(wt, mcpgw.CodegraphDB))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var v string
	if err := c.QueryRow("SELECT v FROM t").Scan(&v); err != nil || v != "chua-checkpoint" {
		t.Fatalf("bản chép thiếu dữ liệu WAL: %q %v", v, err)
	}
}

// Hồi quy NT-MCP-SEED: .codegraph của repo gốc có tệp daemon/phụ trợ (daemon.log,
// daemon.sock, *.pid, -wal, -shm) thì worktree chỉ nhận .gitignore và codegraph.db.
func TestCopyCodegraphIndexSkipsDaemonFiles(t *testing.T) {
	origin, wt := t.TempDir(), t.TempDir()
	dir := filepath.Join(origin, mcpgw.CodegraphDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "codegraph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, q := range []string{"PRAGMA journal_mode=WAL", "PRAGMA wal_autocheckpoint=0", "CREATE TABLE t(v TEXT)", "INSERT INTO t VALUES('x')"} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	extras := []string{"daemon.log", "daemon.sock", "daemon.pid", "codegraph.pid", "codegraph.db-wal", "codegraph.db-shm"}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range extras {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			continue // -wal/-shm do SQLite tạo sẵn: giữ nguyên nội dung thật
		}
		if err := os.WriteFile(p, []byte("rác daemon\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Chốt nguồn thật sự có đủ tệp, để test không qua vì thiếu mồi.
	for _, name := range extras {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("nguồn thiếu %s: %v", name, err)
		}
	}
	if err := CopyCodegraphIndex(context.Background(), origin, wt); err != nil {
		t.Fatal(err)
	}
	ents, err := os.ReadDir(filepath.Join(wt, mcpgw.CodegraphDir))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != ".gitignore,codegraph.db" {
		t.Fatalf("tệp trong worktree: %v", names)
	}
}
