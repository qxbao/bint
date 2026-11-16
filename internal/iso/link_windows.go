//go:build windows

package iso

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf16"

	"golang.org/x/sys/windows"

	"bint/internal/pathutil"
)

// Junction (IO_REPARSE_TAG_MOUNT_POINT) không cần quyền admin hay Developer Mode, khác symlink thư mục.
// Go 1.23+ không còn đặt ModeSymlink cho junction khi Lstat, nên nhận diện bằng reparse tag.

func reparseTag(path string) (uint32, bool, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, false, err
	}
	var fd windows.Win32finddata
	h, err := windows.FindFirstFile(p, &fd)
	if err != nil {
		return 0, false, err
	}
	windows.FindClose(h)
	if fd.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
		return 0, false, nil
	}
	return fd.Reserved0, true, nil // Reserved0 giữ reparse tag khi có FILE_ATTRIBUTE_REPARSE_POINT
}

func isLink(path string) (bool, error) {
	tag, ok, err := reparseTag(path)
	if err != nil || !ok {
		return false, err
	}
	return tag == windows.IO_REPARSE_TAG_MOUNT_POINT || tag == windows.IO_REPARSE_TAG_SYMLINK, nil
}

func linkTarget(link string) (string, error) { return pathutil.LinkTarget(link) }

// removeLink gỡ junction/symlink thư mục: RemoveDirectory trên reparse point chỉ xoá chính nó.
func removeLink(link string) error { return os.Remove(link) }

// createLink tạo thư mục rỗng rồi biến nó thành junction tới target bằng FSCTL_SET_REPARSE_POINT.
func createLink(link, target string) error {
	if err := os.Mkdir(link, 0o700); err != nil {
		return err
	}
	if err := setJunction(link, filepath.Clean(target)); err != nil {
		os.Remove(link)
		return err
	}
	return nil
}

func setJunction(link, target string) error {
	buf, err := junctionBuffer(target)
	if err != nil {
		return err
	}
	p, err := windows.UTF16PtrFromString(link)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	var ret uint32
	return windows.DeviceIoControl(h, windows.FSCTL_SET_REPARSE_POINT, &buf[0], uint32(len(buf)), nil, 0, &ret, nil)
}

// junctionBuffer dựng REPARSE_DATA_BUFFER kiểu mount point:
//
//	uint32 ReparseTag; uint16 ReparseDataLength; uint16 Reserved;
//	uint16 SubstituteNameOffset, SubstituteNameLength, PrintNameOffset, PrintNameLength;
//	WCHAR  PathBuffer[] = substitute (\??\C:\dir) NUL print (C:\dir) NUL
func junctionBuffer(target string) ([]byte, error) {
	if !filepath.IsAbs(target) {
		return nil, fmt.Errorf("đích junction phải tuyệt đối: %q", target)
	}
	sub := utf16.Encode([]rune(`\??\` + target))
	prn := utf16.Encode([]rune(target))
	pathBytes := (len(sub) + 1 + len(prn) + 1) * 2
	dataLen := 8 + pathBytes // bốn uint16 offset/length + PathBuffer
	if dataLen > 0xffff {
		return nil, fmt.Errorf("đích junction quá dài: %q", target)
	}
	b := make([]byte, 8+dataLen)
	le := binary.LittleEndian
	le.PutUint32(b[0:], windows.IO_REPARSE_TAG_MOUNT_POINT)
	le.PutUint16(b[4:], uint16(dataLen))
	le.PutUint16(b[8:], 0)                       // SubstituteNameOffset
	le.PutUint16(b[10:], uint16(len(sub)*2))     // SubstituteNameLength (không tính NUL)
	le.PutUint16(b[12:], uint16((len(sub)+1)*2)) // PrintNameOffset
	le.PutUint16(b[14:], uint16(len(prn)*2))     // PrintNameLength
	off := 16
	for _, c := range sub {
		le.PutUint16(b[off:], c)
		off += 2
	}
	off += 2 // NUL
	for _, c := range prn {
		le.PutUint16(b[off:], c)
		off += 2
	}
	return b, nil
}
