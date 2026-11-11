//go:build !windows

package tui

import (
	"errors"
	"os"
	"slices"
	"testing"
)

// restartSelf exec đúng tệp với cùng đối số và môi trường; relaunch lấy đối số từ Options.Args (mặc định os.Args) và môi trường
// của tiến trình. Không exec thật: execFn được thay bằng hàm giả.
func TestRelaunchExecsInstalledBinaryWithSameArgsAndEnv(t *testing.T) {
	var gotPath string
	var gotArgs, gotEnv []string
	old := execFn
	execFn = func(path string, args, env []string) error {
		gotPath, gotArgs, gotEnv = path, args, env
		return errors.New("exec giả không trả về")
	}
	t.Cleanup(func() { execFn = old })
	t.Setenv("BINT_RESTART_TEST", "1")

	err := relaunch("/home/u/go/bin/bint", Options{Args: []string{"bint", "--lang", "en", "--read-only"}})
	if err == nil || err.Error() != "exec giả không trả về" {
		t.Fatalf("lỗi exec phải được trả về: %v", err)
	}
	if gotPath != "/home/u/go/bin/bint" || !slices.Equal(gotArgs, []string{"bint", "--lang", "en", "--read-only"}) {
		t.Fatalf("exec nhận path=%q args=%v", gotPath, gotArgs)
	}
	if !slices.Contains(gotEnv, "BINT_RESTART_TEST=1") || len(gotEnv) != len(os.Environ()) {
		t.Fatalf("môi trường phải là môi trường hiện tại: %d biến", len(gotEnv))
	}
	// Args mặc định = os.Args.
	if err := relaunch("/x/bint", Options{}); err == nil || !slices.Equal(gotArgs, os.Args) {
		t.Fatalf("args mặc định phải là os.Args: %v", gotArgs)
	}
	// Không có đối số: dùng chính đường dẫn làm argv[0].
	if err := restartSelf("/x/bint", nil, nil); err == nil || !slices.Equal(gotArgs, []string{"/x/bint"}) {
		t.Fatalf("argv[0] mặc định: %v", gotArgs)
	}
	if !RestartSupported {
		t.Fatal("Unix phải hỗ trợ khởi động lại")
	}
}
