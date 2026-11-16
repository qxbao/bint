//go:build !windows

package iso

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureLinkUnixIsSymlinkAndAcceptsRelative(t *testing.T) {
	link, target := linkFixture(t)
	mustEnsure(t, link, target, LinkCreated)
	fi, err := os.Lstat(link)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("phải là symlink: %v %v", fi, err)
	}
	if got, _ := os.Readlink(link); got != target {
		t.Fatalf("Readlink = %q, muốn %q", got, target)
	}
	// Symlink tương đối do tay người tạo, cùng đích: vẫn là LinkOK.
	rel := filepath.Join(filepath.Dir(link), "rel")
	r, _ := filepath.Rel(filepath.Dir(rel), target)
	if err := os.Symlink(r, rel); err != nil {
		t.Fatal(err)
	}
	mustEnsure(t, rel, target, LinkOK)
}

func TestEnsureLinkDanglingTargetIsAccepted(t *testing.T) {
	d := t.TempDir()
	link, target := filepath.Join(d, "l"), filepath.Join(d, "chua-co")
	mustEnsure(t, link, target, LinkCreated) // đích chưa có: EnsureLink không tạo đích
	mustEnsure(t, link, target, LinkOK)
}

func TestSamePathThroughAlias(t *testing.T) {
	d := t.TempDir()
	real := filepath.Join(d, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(d, "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	if !samePath(alias, real) || samePath(real, filepath.Join(d, "khac")) {
		t.Fatal("samePath sai")
	}
}
