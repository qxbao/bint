package iso

import (
	"os"
	"path/filepath"
	"testing"
)

// Các ca dưới đây chạy trên mọi OS (Windows dùng junction); test cụ thể của Unix nằm ở link_unix_test.go.

func linkFixture(t *testing.T) (link, target string) {
	t.Helper()
	d := t.TempDir()
	target = filepath.Join(d, "real")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "data.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(d, "home", ".cache"), target
}

func mustEnsure(t *testing.T, link, target string, want LinkState) {
	t.Helper()
	got, err := EnsureLink(link, target)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("EnsureLink(%s) = %v, muốn %v", link, got, want)
	}
}

func TestEnsureLinkCreateAndIdempotent(t *testing.T) {
	link, target := linkFixture(t)
	mustEnsure(t, link, target, LinkCreated) // cha của link chưa có: được tạo
	if isl, err := IsLink(link); err != nil || !isl {
		t.Fatalf("IsLink = %v, %v", isl, err)
	}
	if b, err := os.ReadFile(filepath.Join(link, "data.txt")); err != nil || string(b) != "keep" {
		t.Fatalf("đọc qua liên kết: %q %v", b, err)
	}
	mustEnsure(t, link, target, LinkOK)
}

func TestEnsureLinkReplacesEmptyDir(t *testing.T) {
	link, target := linkFixture(t)
	if err := os.MkdirAll(link, 0o755); err != nil {
		t.Fatal(err)
	}
	mustEnsure(t, link, target, LinkReplacedEmpty)
	if isl, _ := IsLink(link); !isl {
		t.Fatal("phải là liên kết sau khi thay")
	}
}

func TestEnsureLinkKeepsDataDir(t *testing.T) {
	link, target := linkFixture(t)
	if err := os.MkdirAll(link, 0o755); err != nil {
		t.Fatal(err)
	}
	mine := filepath.Join(link, "mine.txt")
	if err := os.WriteFile(mine, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustEnsure(t, link, target, LinkConflictData)
	if isl, _ := IsLink(link); isl {
		t.Fatal("thư mục có dữ liệu không được bị thay")
	}
	if _, err := os.Stat(mine); err != nil {
		t.Fatalf("dữ liệu bị mất: %v", err)
	}
}

func TestEnsureLinkKeepsFile(t *testing.T) {
	link, target := linkFixture(t)
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(link, []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustEnsure(t, link, target, LinkConflictData)
	if b, _ := os.ReadFile(link); string(b) != "file" {
		t.Fatal("tệp bị đụng tới")
	}
}

func TestEnsureLinkKeepsForeignLink(t *testing.T) {
	link, target := linkFixture(t)
	other := filepath.Join(t.TempDir(), "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	mustEnsure(t, link, other, LinkCreated)
	mustEnsure(t, link, target, LinkConflictLink)
	if cur, err := linkTarget(link); err != nil || !samePath(cur, other) {
		t.Fatalf("liên kết lạ bị đổi: %q %v", cur, err)
	}
}

func TestEnsureLinkRejectsRelativeTarget(t *testing.T) {
	link, _ := linkFixture(t)
	if _, err := EnsureLink(link, "rel/target"); err == nil {
		t.Fatal("đích tương đối phải lỗi")
	}
}

func TestUnlinkKeepsTargetData(t *testing.T) {
	link, target := linkFixture(t)
	mustEnsure(t, link, target, LinkCreated)
	if err := Unlink(link); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("liên kết còn: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(target, "data.txt")); err != nil || string(b) != "keep" {
		t.Fatalf("dữ liệu đích bị chạm: %q %v", b, err)
	}
	if err := Unlink(link); err != nil {
		t.Fatalf("gỡ lần hai (không còn gì) phải im lặng: %v", err)
	}
}

func TestUnlinkRefusesRealDir(t *testing.T) {
	link, _ := linkFixture(t)
	if err := os.MkdirAll(link, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Unlink(link); err == nil {
		t.Fatal("thư mục thật phải bị từ chối")
	}
	if _, err := os.Stat(link); err != nil {
		t.Fatal("thư mục bị xoá")
	}
}

func TestLinkStateString(t *testing.T) {
	for s, want := range map[LinkState]string{LinkCreated: "created", LinkOK: "ok", LinkReplacedEmpty: "replaced-empty", LinkConflictData: "conflict-data", LinkConflictLink: "conflict-link"} {
		if s.String() != want {
			t.Errorf("%d: %q", s, s.String())
		}
		if s.Conflict() != (s == LinkConflictData || s == LinkConflictLink) {
			t.Errorf("Conflict(%v)", s)
		}
	}
}
