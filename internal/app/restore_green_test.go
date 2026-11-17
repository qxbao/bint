package app

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// `bint task restore-green` ghi đè tệp về điểm verify xanh: chỉ người chạy được (kể cả xem trước); phiên agent bị từ chối trước
// khi chạm tới tác vụ. Người chạy thì qua cổng quyền và gặp điều kiện của lõi (tác vụ phải đang dừng).
func TestRestoreGreenIsHumanOnly(t *testing.T) {
	ws, _, _ := taskRepo(t)
	ctx := context.Background()
	o := TaskNewDefaults(ws.K.Config)
	o.Request, o.NoPlan = "Sửa hàm A trong a.go", true
	c, err := NewTask(ctx, ws, o)
	if err != nil {
		t.Fatal(err)
	}
	id := c.Task.State.ID

	asAgent(t)
	for _, apply := range []bool{false, true} {
		if _, err := RestoreGreenTask(ctx, ws, TaskRestoreGreenOptions{Ref: id, Apply: apply}); !errors.Is(err, ErrAgentForbidden) {
			t.Errorf("phiên agent (apply=%v): %v", apply, err)
		}
	}
	if err := (TaskRestoreGreenOptions{}).Validate(); err == nil {
		t.Error("thiếu ID tác vụ phải là lỗi dùng")
	}

	asHuman(t)
	if _, err := RestoreGreenTask(ctx, ws, TaskRestoreGreenOptions{Ref: id}); err == nil || errors.Is(err, ErrAgentForbidden) ||
		!strings.Contains(err.Error(), "tác vụ dừng") {
		t.Fatalf("người chạy trên tác vụ chưa dừng phải gặp điều kiện của lõi: %v", err)
	}
}
