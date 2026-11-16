//go:build !windows

package iso

import (
	"os"
	"path/filepath"
	"testing"
)

func tempProfile(t *testing.T) (Profile, string) {
	t.Helper()
	base := t.TempDir()
	t.Setenv("TMPDIR", base)
	t.Setenv(EnvProfile, "")
	p := mustCreate(t, testStore(t, nil), Codex, "w")
	t.Setenv("TMPDIR", base) // Isolate không đổi TMPDIR, giữ chắc chắn
	return p, base
}

func TestEnsureTempCreates0700(t *testing.T) {
	p, base := tempProfile(t)
	dir, err := p.EnsureTemp()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, tempParentName(), "codex--w"); dir != want {
		t.Fatalf("dir = %q, muốn %q", dir, want)
	}
	for _, d := range []string{dir, filepath.Dir(dir)} {
		fi, err := os.Stat(d)
		if err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o700 {
			t.Fatalf("%s: %v mode=%v", d, err, fi)
		}
	}
	if d2, err := p.EnsureTemp(); err != nil || d2 != dir {
		t.Fatalf("gọi lại phải an toàn: %q %v", d2, err)
	}
	// Thư mục bị dọn mất (kill -9, tmpfiles): EnsureTemp tạo lại — Codex không tự tạo (S3).
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := p.EnsureTemp(); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureTempRejectsSymlinkParent(t *testing.T) {
	p, base := tempProfile(t)
	evil := t.TempDir()
	if err := os.Symlink(evil, filepath.Join(base, tempParentName())); err != nil {
		t.Fatal(err)
	}
	if _, err := p.EnsureTemp(); err == nil {
		t.Fatal("cha là symlink phải bị từ chối")
	}
	if ents, _ := os.ReadDir(evil); len(ents) != 0 {
		t.Fatalf("không được ghi qua symlink: %v", ents)
	}
}

func TestEnsureTempRejectsLooseMode(t *testing.T) {
	p, base := tempProfile(t)
	parent := filepath.Join(base, tempParentName())
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := p.EnsureTemp(); err == nil {
		t.Fatal("cha mode 0755 phải bị từ chối")
	}
	if fi, _ := os.Stat(parent); fi.Mode().Perm() != 0o755 {
		t.Fatal("không được chmod thư mục lạ")
	}
}

func TestEnsureTempRejectsFileParent(t *testing.T) {
	p, base := tempProfile(t)
	if err := os.WriteFile(filepath.Join(base, tempParentName()), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := p.EnsureTemp(); err == nil {
		t.Fatal("cha là tệp phải bị từ chối")
	}
}

func TestEnsureTempRejectsSymlinkProfileDir(t *testing.T) {
	p, base := tempProfile(t)
	parent := filepath.Join(base, tempParentName())
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(parent, "codex--w")); err != nil {
		t.Fatal(err)
	}
	if _, err := p.EnsureTemp(); err == nil {
		t.Fatal("thư mục temp của profile là symlink phải bị từ chối")
	}
}
