package actions

import (
	"testing"

	"bint/internal/testutil"
)

// TestMain dọn thư mục harness giả của testutil.Isolate khi tiến trình kiểm thử xong.
func TestMain(m *testing.M) { testutil.Main(m) }
