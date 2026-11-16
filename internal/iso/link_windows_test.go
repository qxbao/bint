//go:build windows

package iso

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestJunctionDetectedByReparseTag(t *testing.T) {
	link, target := linkFixture(t)
	mustEnsure(t, link, target, LinkCreated)
	tag, ok, err := reparseTag(link)
	if err != nil || !ok || tag != windows.IO_REPARSE_TAG_MOUNT_POINT {
		t.Fatalf("reparse tag = %#x ok=%v err=%v, muốn MOUNT_POINT", tag, ok, err)
	}
	if isl, err := isLink(link); err != nil || !isl {
		t.Fatalf("isLink = %v %v", isl, err)
	}
	if isl, _ := isLink(target); isl {
		t.Fatal("thư mục thường không phải liên kết")
	}
}

func TestJunctionBufferLayout(t *testing.T) {
	b, err := junctionBuffer(`C:\x\y`)
	if err != nil {
		t.Fatal(err)
	}
	// 8 byte đầu (tag, độ dài, reserved) + 8 byte offset/length + 2 chuỗi UTF-16 có NUL.
	want := 8 + 8 + (len(`\??\C:\x\y`)+1+len(`C:\x\y`)+1)*2
	if len(b) != want {
		t.Fatalf("độ dài buffer = %d, muốn %d", len(b), want)
	}
	if _, err := junctionBuffer(`rel\x`); err == nil {
		t.Fatal("đích tương đối phải lỗi")
	}
}

func TestRemoveAllThroughJunctionNeedsUnlink(t *testing.T) {
	link, target := linkFixture(t)
	mustEnsure(t, link, target, LinkCreated)
	if err := Unlink(link); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "data.txt")); err != nil {
		t.Fatalf("Unlink xoá dữ liệu đích: %v", err)
	}
}
