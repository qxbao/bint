package iso

import (
	"errors"
	"os"
	"runtime"
)

// ProbeEnv dựng env của profile cho một lần chạy harness ngắn do bint tự làm (hooks/list của sync): như Run
// nhưng không lấy khoá phiên (caller đang giữ khoá X của Sync) và trả hàm dọn thư mục temp nếu chính nó tạo ra.
func ProbeEnv(p Profile, base []string) (env []string, cleanup func(), err error) {
	if base == nil {
		base = os.Environ()
	}
	env, err = Env(p, base)
	if err != nil {
		return nil, nil, err
	}
	tmp := newEnvList(runtime.GOOS, env).val(tempEnvKey())
	if tmp == "" {
		return nil, nil, errors.New("env của profile thiếu thư mục temp")
	}
	_, statErr := os.Lstat(tmp)
	existed := statErr == nil
	if err := ensureTempDir(tmp); err != nil {
		return nil, nil, err
	}
	return env, func() {
		if !existed && !p.DaemonAlive() {
			_ = removeTree(tmp)
		}
	}, nil
}
