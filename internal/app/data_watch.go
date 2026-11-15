package app

import (
	"context"
	"hash/fnv"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	"bint/internal/task"
)

// DataWatch là thứ cần để TUI biết dữ liệu repository có đổi không mà không phải nạp lại: danh sách đường dẫn
// theo dõi, dựng một lần bằng ResolveDataWatch. Chỉ stat (không mở SQLite, không ghi, không đọc nội dung tệp).
type DataWatch struct {
	// Trees là các thư mục quét đệ quy (mỗi tệp góp tên, kích thước, mtime); Files là tệp lẻ (DB và WAL của nó).
	Trees, Files []string
}

// ResolveDataWatch dựng DataWatch cho repository chứa dir: cây .bint (tri thức, tác vụ, đề xuất, cấu hình),
// tệp SQLite và WAL, thư mục spool, thư mục tác vụ runtime. Chưa init hoặc không mở được thì ok=false.
func ResolveDataWatch(ctx context.Context, dir string) (DataWatch, bool) {
	ws, err := OpenWorkspace(ctx, dir, true)
	if err != nil {
		return DataWatch{}, false
	}
	return DataWatch{
		Trees: []string{filepath.Join(ws.Repo.Toplevel, ".bint"), ws.Paths.Spool, task.NewStore(ws.Paths.Runtime).Dir},
		Files: []string{ws.Paths.DB, ws.Paths.DB + "-wal"},
	}, true
}

// Signature băm (tên, kích thước, mtime) của mọi tệp được theo dõi. Đường dẫn không tồn tại tính là rỗng.
// Hai lần gọi cho cùng một trạng thái đĩa cho cùng kết quả; thêm, xoá, sửa tệp làm kết quả đổi.
func (w DataWatch) Signature() uint64 {
	h := fnv.New64a()
	add := func(rel string, info fs.FileInfo) {
		h.Write([]byte(rel))
		if info != nil && !info.IsDir() {
			h.Write([]byte{0})
			h.Write([]byte(strconv.FormatInt(info.Size(), 10)))
			h.Write([]byte{0})
			h.Write([]byte(strconv.FormatInt(info.ModTime().UnixNano(), 10)))
		}
		h.Write([]byte{1})
	}
	for _, root := range w.Trees {
		h.Write([]byte("T" + root + "\x01"))
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // đường dẫn biến mất giữa chừng: bỏ qua
			}
			rel, rerr := filepath.Rel(root, p)
			if rerr != nil {
				rel = p
			}
			info, ierr := d.Info()
			if ierr != nil {
				info = nil
			}
			add(rel, info)
			return nil
		})
	}
	for _, f := range w.Files {
		h.Write([]byte("F" + f + "\x01"))
		if info, err := os.Stat(f); err == nil {
			add("", info)
		}
	}
	return h.Sum64()
}
