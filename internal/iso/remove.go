package iso

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrDaemonAlive: profile còn daemon app-server Codex đang chạy; dừng nó trước khi xoá.
var ErrDaemonAlive = errors.New("profile còn daemon app-server Codex đang chạy")

// RemoveProfile xoá profile p: lấy khoá X không chờ (đang có phiên ⇒ ErrBusy), từ chối khi còn daemon Codex
// (ErrDaemonAlive), xoá thư mục temp rồi cả cây profile mà không đi vào liên kết chia sẻ — symlink/junction
// trong home/ chỉ bị gỡ chính nó, dữ liệu thật ở đích (cache của người dùng) không bị chạm.
// Chỉ nhận profile do Store.Load/Create trả về (đường dẫn phải đúng <store>/profiles/<tool>--<name>).
func RemoveProfile(p Profile) error {
	if want := filepath.Join(p.StoreRoot, "profiles", dirName(p.Tool, p.Name)); filepath.Clean(p.Dir) != want {
		return fmt.Errorf("từ chối xoá %q: không phải thư mục profile của store", p.Dir)
	}
	release, busy, err := p.Lock(true)
	if err != nil {
		return err
	}
	if busy {
		return fmt.Errorf("%w: %s", ErrBusy, p.ID())
	}
	defer release()
	if p.DaemonAlive() {
		return fmt.Errorf("%w: %s (dừng bằng `codex app-server daemon stop` với CODEX_HOME=%s)", ErrDaemonAlive, p.ID(), filepath.Join(p.Home, p.Tool.spec().ConfigDir))
	}
	if err := removeTree(p.TempDir()); err != nil {
		return fmt.Errorf("xoá temp của %s: %w", p.ID(), err)
	}
	// Giữ session.lock tới cuối: Windows không xoá được tệp đang mở.
	ents, err := os.ReadDir(p.Dir)
	if err != nil {
		return err
	}
	var errs []error
	for _, de := range ents {
		if de.Name() == filepath.Base(p.LockPath()) {
			continue
		}
		if err := removeTree(filepath.Join(p.Dir, de.Name())); err != nil {
			errs = append(errs, err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("xoá %s: %w", p.ID(), err)
	}
	_ = release()
	if err := removeTree(p.LockPath()); err != nil {
		return err
	}
	return os.Remove(p.Dir)
}
