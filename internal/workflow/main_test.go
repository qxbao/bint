package workflow

import (
	"testing"

	"bint/internal/testutil"
)

// TestMain dựng bint từ cây (luồng tới b-review gọi `bint` trên PATH, cliSkewCheck) và dọn thư mục harness giả của
// testutil.Isolate khi tiến trình kiểm thử xong.
func TestMain(m *testing.M) { testutil.MainWithBint(m) }
