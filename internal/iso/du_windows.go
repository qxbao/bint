//go:build windows

package iso

import "os"

// fileID nhận diện một tệp để đếm hardlink một lần (Windows: không khử hardlink, nên không dùng).
type fileID struct{}

// diskUsage trên Windows là kích thước tệp; thư mục tính 0 và hardlink không được khử trùng (số đo không so
// với `du` được, chỉ để so sánh tương đối giữa các nhóm).
func diskUsage(fi os.FileInfo) (size int64, id fileID, dedupe bool) {
	if fi.IsDir() {
		return 0, fileID{}, false
	}
	return fi.Size(), fileID{}, false
}
