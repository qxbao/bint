package fsafe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type env struct {
	dir, file, backups string
	m                  *Manifest
}

func newEnv(t *testing.T) *env {
	d := t.TempDir()
	m, err := LoadManifest(filepath.Join(d, "state", "managed.json"))
	if err != nil {
		t.Fatal(err)
	}
	return &env{dir: d, file: filepath.Join(d, "AGENTS.md"), backups: filepath.Join(d, "state", "backups"), m: m}
}

func (e *env) sync(t *testing.T, inner string, force bool) Result {
	t.Helper()
	r, err := SyncBlock(BlockSync{Path: e.file, RelName: "AGENTS.md", Markers: HTMLMarkers, Inner: inner,
		Force: force, BackupDir: e.backups, Manifest: e.m, Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func read(t *testing.T, p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCreateUpdateUnchanged(t *testing.T) {
	e := newEnv(t)
	if r := e.sync(t, "v1", false); r.Outcome != Created {
		t.Fatal(r)
	}
	if r := e.sync(t, "v1", false); r.Outcome != Unchanged {
		t.Fatal(r)
	}
	r := e.sync(t, "v2", false)
	if r.Outcome != Updated || r.BackupPath == "" {
		t.Fatal(r)
	}
	if !strings.Contains(read(t, r.BackupPath), "v1") {
		t.Fatal("backup should hold previous content")
	}
	if !strings.Contains(read(t, e.file), "v2") {
		t.Fatal("not updated")
	}
}

func TestPreservesUserContentOutsideBlock(t *testing.T) {
	e := newEnv(t)
	user := "# Dự án\r\n\r\nQuy tắc của tôi.\r\n"
	os.WriteFile(e.file, []byte(user), 0o644)
	if r := e.sync(t, "a\nb", false); r.Outcome != Appended {
		t.Fatal(r)
	}
	got := read(t, e.file)
	if !strings.HasPrefix(got, user) {
		t.Fatalf("user content changed:\n%q", got)
	}
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Fatalf("line endings not preserved: %q", got)
	}
	// Người dùng thêm nội dung sau khối; cập nhật khối không động tới phần đó.
	os.WriteFile(e.file, []byte(got+"Phần cuối.\r\n"), 0o644)
	e.sync(t, "c", false)
	got = read(t, e.file)
	if !strings.HasPrefix(got, user) || !strings.HasSuffix(got, "Phần cuối.\r\n") || strings.Contains(got, "a\r\nb") {
		t.Fatalf("got %q", got)
	}
}

func TestConflictWhenUserEditsBlock(t *testing.T) {
	e := newEnv(t)
	e.sync(t, "v1", false)
	edited := strings.Replace(read(t, e.file), "v1", "v1 sửa tay", 1)
	os.WriteFile(e.file, []byte(edited), 0o644)
	if r := e.sync(t, "v2", false); r.Outcome != Conflict {
		t.Fatalf("want conflict, got %v", r)
	}
	if read(t, e.file) != edited {
		t.Fatal("file must not change on conflict")
	}
	r := e.sync(t, "v2", true)
	if r.Outcome != Updated || !strings.Contains(read(t, r.BackupPath), "sửa tay") {
		t.Fatalf("force: %v", r)
	}
}

func TestConflictWhenManifestUnknown(t *testing.T) {
	// Khối có sẵn (ví dụ do người khác commit) nhưng máy này chưa từng ghi: không ghi đè.
	e := newEnv(t)
	os.WriteFile(e.file, []byte("<!-- bint:begin -->\nkhác\n<!-- bint:end -->\n"), 0o644)
	if r := e.sync(t, "mới", false); r.Outcome != Conflict {
		t.Fatal(r)
	}
}

func TestMalformedBlock(t *testing.T) {
	e := newEnv(t)
	os.WriteFile(e.file, []byte("x\n<!-- bint:begin -->\nno end\n"), 0o644)
	if r := e.sync(t, "v", false); r.Outcome != Conflict {
		t.Fatal(r)
	}
}

func TestManifestRoundTrip(t *testing.T) {
	e := newEnv(t)
	e.sync(t, "v1", false)
	if err := e.m.Save(); err != nil {
		t.Fatal(err)
	}
	m2, err := LoadManifest(filepath.Join(e.dir, "state", "managed.json"))
	if err != nil {
		t.Fatal(err)
	}
	e.m = m2
	if r := e.sync(t, "v2", false); r.Outcome != Updated {
		t.Fatalf("manifest lost: %v", r)
	}
}
