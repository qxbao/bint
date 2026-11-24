package testutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// Luồng workflow (trước b-review) chạy `bint task show` của PATH để phát hiện binary cũ hơn cấu hình
// (workflow.cliSkewCheck). Trên máy dev PATH thường có bản `go install` cũ: khi cây đang test thêm khoá cấu hình mới,
// bản đó không đọc được .bint/config.yml và kiểm thử bị chặn với lý do "binary cũ hơn cấu hình". MainWithBint dựng
// bint từ cây đang test MỘT lần ở tiến trình cha (tiến trình con của RunTests nhận đường dẫn qua envTreeBint, không
// build lại); Isolate đặt thư mục chứa nó đầu PATH nên kết quả không phụ thuộc bint cài trên máy.

// envTreeBint: thư mục chứa bint dựng từ cây hiện tại (chỉ có tệp bint/bint.exe).
const envTreeBint = "BINT_TEST_TREE_BINT_DIR"

// MainWithBint như Main nhưng trước đó dựng bint từ cây đang test (xem đầu tệp); dùng trong TestMain của gói có
// kiểm thử chạy workflow tới b-review hay cách khác gọi `bint` trên PATH.
func MainWithBint(m *testing.M) {
	cleanup, err := buildTreeBint()
	if err != nil {
		fmt.Fprintf(os.Stderr, "testutil: dựng bint từ cây hiện tại: %v\n", err)
		os.Exit(1)
	}
	code := RunTests(m)
	cleanup()
	CleanupHarness()
	os.Exit(code)
}

// UseTreeBint báo cho Isolate thư mục chứa bint đã dựng từ cây đang test (gói tự dựng bint, như internal/cli), để
// Isolate đặt nó đầu PATH như MainWithBint mà không dựng thêm lần nữa. Thư mục chỉ nên chứa bint.
func UseTreeBint(dir string) { os.Setenv(envTreeBint, dir) }

// buildTreeBint dựng bint vào một thư mục tạm và đặt envTreeBint; đã có (tiến trình con) thì không làm gì.
func buildTreeBint() (cleanup func(), err error) {
	if os.Getenv(envTreeBint) != "" {
		return func() {}, nil
	}
	// Cùng tiền tố với thư mục harness giả: thư mục bị bỏ lại (tiến trình bị giết) được sweepStaleHarness dọn.
	dir, err := os.MkdirTemp("", harnessPrefix+"bint-")
	if err != nil {
		return nil, err
	}
	// Tên dài thật: TEMP trên Windows có thể ở dạng ngắn 8.3 (xem internal/cli/e2e_test.go).
	if long, lerr := filepath.EvalSymlinks(dir); lerr == nil {
		dir = long
	}
	exe := filepath.Join(dir, "bint")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", exe, "bint/cmd/bint").CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("%v\n%s", err, out)
	}
	os.Setenv(envTreeBint, dir)
	return func() { os.RemoveAll(dir) }, nil
}
