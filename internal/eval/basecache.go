package eval

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Vòng đời kho đệm commit gốc (<eval-work>/bases/<sha>.git).
//
// Mỗi kho chỉ chứa đối tượng tới được từ commit gốc nên có thể xoá bất cứ lúc nào mà
// không ảnh hưởng thư mục làm việc đã clone: clone cục bộ dùng hardlink (không dùng
// alternates), nên tệp đối tượng của thư mục làm việc độc lập với kho đệm. Kho bị xoá
// sẽ được nạp lại ở lần dùng sau (ensureBase kiểm tra eval-base mỗi lần).

// lockBase khoá kho đệm của một thư mục base; trả hàm mở khoá.
func lockBase(dir string) func() {
	muAny, _ := baseLocks.LoadOrStore(dir, &sync.Mutex{})
	mu := muAny.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func basesDir(workRoot string) string { return filepath.Join(workRoot, "bases") }

// releaseBase dọn kho đệm của base khi bên gọi đã dùng xong (mọi lượt cần base đó đã
// clone xong). Không làm gì nếu eval.keep_bases bật; khi đó chỉ áp trần dung lượng LRU.
// Lỗi dọn không nghiêm trọng (kho còn lại sẽ bị `bint eval clean` hoặc LRU dọn sau).
func releaseBase(env Env, base string) {
	dir := filepath.Join(basesDir(env.WorkRoot), base+".git")
	if !env.Cfg.Eval.KeepBases {
		unlock := lockBase(dir)
		os.RemoveAll(dir)
		unlock()
		return
	}
	if mb := env.Cfg.Eval.BaseCacheMaxMB; mb > 0 {
		PruneBases(env.WorkRoot, int64(mb)<<20, map[string]bool{dir: true})
	}
}

// dirSize tính tổng dung lượng tệp trong một thư mục.
func dirSize(dir string) int64 {
	var n int64
	filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}

// PruneBases xoá kho đệm dùng lâu nhất (theo thời điểm sửa của thư mục kho, được cập
// nhật mỗi lần dùng) cho tới khi tổng dung lượng <= maxBytes. maxBytes <= 0 = không
// giới hạn. Kho trong protect (đường dẫn đầy đủ) không bị xoá.
func PruneBases(workRoot string, maxBytes int64, protect map[string]bool) (removed int, freed int64, err error) {
	if maxBytes <= 0 {
		return 0, 0, nil
	}
	ents, err := os.ReadDir(basesDir(workRoot))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	type base struct {
		dir  string
		size int64
		used int64
	}
	var all []base
	var total int64
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(basesDir(workRoot), e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		b := base{dir: dir, size: dirSize(dir), used: info.ModTime().UnixNano()}
		total += b.size
		all = append(all, b)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].used < all[j].used })
	for _, b := range all {
		if total <= maxBytes {
			break
		}
		if protect[b.dir] {
			continue
		}
		unlock := lockBase(b.dir)
		rerr := os.RemoveAll(b.dir)
		unlock()
		if rerr != nil {
			err = rerr
			continue
		}
		total -= b.size
		freed += b.size
		removed++
	}
	return removed, freed, err
}

// CleanBases xoá toàn bộ kho đệm commit gốc (và thư mục .tmp bỏ dở). An toàn: kho sẽ
// được nạp lại khi cần. Trả về số mục đã xoá và dung lượng giải phóng.
func CleanBases(workRoot string) (removed int, freed int64, err error) {
	ents, err := os.ReadDir(basesDir(workRoot))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	for _, e := range ents {
		dir := filepath.Join(basesDir(workRoot), e.Name())
		size := dirSize(dir)
		unlock := lockBase(dir)
		rerr := os.RemoveAll(dir)
		unlock()
		if rerr != nil {
			err = rerr
			continue
		}
		freed += size
		removed++
	}
	return removed, freed, err
}

// BasesSize trả về dung lượng và số kho đệm hiện có.
func BasesSize(workRoot string) (count int, bytes int64) {
	ents, _ := os.ReadDir(basesDir(workRoot))
	for _, e := range ents {
		if e.IsDir() {
			count++
			bytes += dirSize(filepath.Join(basesDir(workRoot), e.Name()))
		}
	}
	return
}

// repoStoreBytes ước lượng dung lượng đối tượng của repository người dùng (cận trên cho
// kích thước một kho đệm đầy đủ lịch sử) bằng `git count-objects -v`.
func repoStoreBytes(ctx context.Context, repoDir string) int64 {
	out, err := gitString(ctx, repoDir, "count-objects", "-v")
	if err != nil {
		return 0
	}
	var kib int64
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if k == "size" || k == "size-pack" {
			n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			kib += n
		}
	}
	return kib << 10
}

// checkFreeSpace dừng có thông báo rõ ràng nếu ổ chứa workRoot không đủ chỗ cho một kho
// đệm mới (ước lượng theo repository người dùng) cộng mức dự phòng eval.min_free_mb.
func checkFreeSpace(ctx context.Context, env Env, workRoot string) error {
	free, ok := freeBytes(workRoot)
	if !ok {
		return nil // hệ điều hành không cho đo: bỏ qua kiểm tra
	}
	need := repoStoreBytes(ctx, env.RepoDir) + int64(env.Cfg.Eval.MinFreeMB)<<20
	if free < need {
		count, size := BasesSize(workRoot)
		return fmt.Errorf("không đủ dung lượng đĩa để tạo kho đệm commit gốc tại %s: còn %d MB, cần khoảng %d MB (kho đệm hiện có: %d, %d MB). "+
			"Chạy `bint eval clean` để giải phóng, hoặc đặt eval.base_depth (nạp nông) / eval.base_cache_max_mb / eval.min_free_mb",
			workRoot, free>>20, need>>20, count, size>>20)
	}
	return nil
}
