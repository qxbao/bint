package iso

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"bint/internal/pathutil"
)

// LinkState là kết quả của EnsureLink.
type LinkState int

const (
	LinkCreated       LinkState = iota // chưa có gì ở link: đã tạo liên kết mới
	LinkOK                             // đã là liên kết trỏ đúng đích
	LinkReplacedEmpty                  // link là thư mục rỗng: đã thay bằng liên kết
	LinkConflictData                   // link là thư mục có dữ liệu hoặc tệp: không đụng tới
	LinkConflictLink                   // link là liên kết trỏ chỗ khác: không đụng tới
	LinkRemoved                        // mục không còn hiệu lực (cache private, bỏ khỏi iso.share): đã gỡ liên kết đúng đích danh mục
)

func (s LinkState) String() string {
	switch s {
	case LinkCreated:
		return "created"
	case LinkOK:
		return "ok"
	case LinkReplacedEmpty:
		return "replaced-empty"
	case LinkConflictData:
		return "conflict-data"
	case LinkConflictLink:
		return "conflict-link"
	case LinkRemoved:
		return "removed"
	}
	return fmt.Sprintf("LinkState(%d)", int(s))
}

// Conflict cho biết link bị giữ nguyên vì đã có dữ liệu hay liên kết khác.
func (s LinkState) Conflict() bool { return s == LinkConflictData || s == LinkConflictLink }

// ErrNotLink: Unlink gặp thứ không phải liên kết (không xoá).
var ErrNotLink = errors.New("không phải liên kết")

// EnsureLink bảo đảm link là liên kết (symlink trên Unix, junction trên Windows) tới target (đường dẫn
// tuyệt đối). Không bao giờ ghi đè dữ liệu: thư mục có dữ liệu, tệp, hoặc liên kết trỏ chỗ khác chỉ được báo
// qua trạng thái (error = nil). Thư mục cha của link được tạo (0700). Target không được tạo ở đây.
func EnsureLink(link, target string) (LinkState, error) {
	if !filepath.IsAbs(target) {
		return 0, fmt.Errorf("đích liên kết phải là đường dẫn tuyệt đối: %q", target)
	}
	fi, err := os.Lstat(link)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
			return 0, err
		}
		if err := createLink(link, target); err != nil {
			return 0, fmt.Errorf("tạo liên kết %s -> %s: %w", link, target, err)
		}
		return LinkCreated, nil
	case err != nil:
		return 0, err
	}
	isl, err := isLink(link)
	if err != nil {
		return 0, err
	}
	if isl {
		cur, err := linkTarget(link)
		if err != nil {
			return 0, err
		}
		if samePath(cur, target) {
			return LinkOK, nil
		}
		return LinkConflictLink, nil
	}
	if !fi.IsDir() {
		return LinkConflictData, nil
	}
	ents, err := os.ReadDir(link)
	if err != nil {
		return 0, err
	}
	if len(ents) > 0 {
		return LinkConflictData, nil
	}
	if err := os.Remove(link); err != nil { // chỉ xoá thư mục rỗng
		return 0, err
	}
	if err := createLink(link, target); err != nil {
		return 0, fmt.Errorf("thay thư mục rỗng %s bằng liên kết tới %s: %w", link, target, err)
	}
	return LinkReplacedEmpty, nil
}

// InspectLink là EnsureLink chỉ-đọc: cùng phân loại nhưng không tạo, thay hay xoá gì. LinkCreated nghĩa là
// chưa có gì ở link (EnsureLink sẽ tạo), LinkReplacedEmpty là thư mục rỗng (EnsureLink sẽ thay).
func InspectLink(link, target string) (LinkState, error) {
	if !filepath.IsAbs(target) {
		return 0, fmt.Errorf("đích liên kết phải là đường dẫn tuyệt đối: %q", target)
	}
	fi, err := os.Lstat(link)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return LinkCreated, nil
	case err != nil:
		return 0, err
	}
	isl, err := isLink(link)
	if err != nil {
		return 0, err
	}
	if isl {
		cur, err := linkTarget(link)
		if err != nil {
			return 0, err
		}
		if samePath(cur, target) {
			return LinkOK, nil
		}
		return LinkConflictLink, nil
	}
	if !fi.IsDir() {
		return LinkConflictData, nil
	}
	ents, err := os.ReadDir(link)
	if err != nil {
		return 0, err
	}
	if len(ents) > 0 {
		return LinkConflictData, nil
	}
	return LinkReplacedEmpty, nil
}

// Unlink gỡ liên kết ở link mà không chạm dữ liệu ở đích. Không có gì ở link thì thôi (nil); gặp thư mục
// hoặc tệp thật thì ErrNotLink và không xoá. `remove` profile phải gọi Unlink cho mọi liên kết trước khi xoá
// cây: os.RemoveAll đi xuyên junction trên Windows sẽ xoá cache thật.
func Unlink(link string) error {
	if _, err := os.Lstat(link); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	isl, err := isLink(link)
	if err != nil {
		return err
	}
	if !isl {
		return fmt.Errorf("%w: %s", ErrNotLink, link)
	}
	return removeLink(link)
}

// IsLink cho biết path là symlink hoặc junction (không theo liên kết).
func IsLink(path string) (bool, error) { return isLink(path) }

// samePath so hai đường dẫn sau khi giải symlink theo khả năng; Windows/macOS không phân biệt hoa thường.
func samePath(a, b string) bool {
	a, b = pathutil.BestEffort(a), pathutil.BestEffort(b)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
