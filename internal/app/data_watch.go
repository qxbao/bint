package app

import (
	"context"
	"hash/fnv"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
	// Gốc .bint là nơi cấu hình và tri thức được nạp (ws.Sources.Project, như OpenKnowledge), có thể là worktree chính.
	bintRoot := ws.Repo.Toplevel
	if ws.Sources.Project != "" {
		bintRoot = filepath.Dir(filepath.Dir(ws.Sources.Project))
	}
	return DataWatch{
		Trees: []string{filepath.Join(bintRoot, ".bint"), ws.Paths.Spool, task.NewStore(ws.Paths.Runtime).Dir},
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
			if watchIgnored(root, rel, d) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
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

// watchIgnored bỏ khỏi chữ ký những thứ được ghi liên tục trong lúc tác vụ chạy mà không đổi dữ liệu hiển thị:
// tệp .lock và thư mục logs của từng tác vụ (<runtime>/tasks/<id>/logs).
func watchIgnored(root, rel string, d fs.DirEntry) bool {
	if !d.IsDir() && strings.HasSuffix(d.Name(), ".lock") {
		return true
	}
	if d.IsDir() && d.Name() == "logs" && filepath.Base(root) == "tasks" {
		parts := strings.Split(filepath.ToSlash(rel), "/")
		return len(parts) == 2
	}
	return false
}
