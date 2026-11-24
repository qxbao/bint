package app

import (
	"testing"

	"bint/internal/testutil"
)

// TestMain dọn thư mục harness giả của testutil.Isolate khi tiến trình kiểm thử xong. Kiểm thử chạy b-do tới b-review
// (vd. TestMilestoneTaskToDone) gọi `bint` trên PATH: MainWithBint đặt bản dựng từ cây này lên đầu PATH.
func TestMain(m *testing.M) { testutil.MainWithBint(m) }
