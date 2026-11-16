//go:build !windows

package iso

import (
	"os"
	"syscall"
)

// fileID nhận diện một tệp để đếm hardlink một lần.
type fileID struct{ dev, ino uint64 }

// diskUsage trả dung lượng đĩa của mục (số khối × 512 như `du`) và, với tệp có nhiều liên kết cứng, khoá để
// chỉ đếm một lần (dedupe=true).
func diskUsage(fi os.FileInfo) (size int64, id fileID, dedupe bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fi.Size(), fileID{}, false
	}
	return int64(st.Blocks) * 512, fileID{dev: uint64(st.Dev), ino: uint64(st.Ino)}, !fi.IsDir() && uint64(st.Nlink) > 1 //nolint:unconvert
}
