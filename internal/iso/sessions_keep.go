package iso

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"bint/internal/state"
	"bint/internal/task"
)

// OpenTaskSessions quét mọi kho tác vụ dưới thư mục trạng thái thật (<state>/<repo>/runtime/tasks) và trả hàm
// cho biết một ID phiên harness có đang được tác vụ chưa kết thúc tham chiếu (State.LastStep.SessionID) hay
// không: GC không được xoá phiên đó vì `bint run --resume` còn cần. env là env của tiến trình (đang trong
// profile thì tính từ env thật).
//
// Giới hạn: repo đặt `state.dir` tuỳ biến (ngoài thư mục trạng thái mặc định) không được thấy. Có tác vụ đọc
// lỗi thì trả cả hàm (đã gom được gì) lẫn lỗi; người gọi quyết định, và an toàn là giữ mọi phiên.
func OpenTaskSessions(env []string) (func(id string) bool, error) {
	if env == nil {
		env = os.Environ()
	}
	e := newEnvList(runtime.GOOS, env)
	restoreReal(e)
	base, err := state.BaseDirFrom(e.get)
	if err != nil {
		return nil, err
	}
	keep := map[string]bool{}
	var errs []error
	repos, err := os.ReadDir(base)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, r := range repos {
		if !r.IsDir() {
			continue
		}
		tasks, bad := task.NewStore(filepath.Join(base, r.Name(), "runtime")).List()
		errs = append(errs, bad...)
		for _, t := range tasks {
			if task.Terminal(t.State.Status) {
				continue
			}
			if ls := t.State.LastStep; ls != nil && ls.SessionID != "" {
				keep[ls.SessionID] = true
			}
		}
	}
	var out error
	if len(errs) > 0 {
		out = fmt.Errorf("đọc tác vụ để biết phiên cần giữ: %w", errors.Join(errs...))
	}
	return func(id string) bool { return keep[id] }, out
}
