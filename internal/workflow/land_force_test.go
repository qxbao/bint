package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"bint/internal/task"
)

// land --force chỉ bỏ qua điều chặn trong danh sách cho phép tường minh (task.BlockerKind.Forceable): tiêu chí đã đánh
// dấu chưa đạt và lệnh kiểm chứng chưa được người xác nhận vẫn chặn, kể cả khi lần kiểm chứng cuối không qua.
func TestLandForceKeepsHardBlockers(t *testing.T) {
	ctx := context.Background()
	setup := func(t *testing.T) (*harness, *Runner, string) {
		h := newHarness(t).wtHarness()
		h.behave = featWrites(h, nil)
		r := h.runner()
		id := h.newDo(r, NewTaskOptions{}).State.ID
		if res, err := r.Run(ctx, RunOptions{Workflow: StepFeat, TaskRef: id}); err != nil || res.Status != task.StatusInReview {
			t.Fatalf("b-feat: %+v %v", res, err)
		}
		return h, r, id
	}
	land := func(t *testing.T, r *Runner, id string) error {
		t.Helper()
		_, err := r.Land(ctx, id, LandOptions{Force: true, Reason: "sự cố ngoài mã"})
		return err
	}
	t.Run("tiêu chí chưa đạt", func(t *testing.T) {
		_, r, id := setup(t)
		setCriteriaRaw(t, r, id, "app.txt ghi v2")
		if _, err := r.CheckCriterion(id, 1, false, "chưa xong"); err != nil {
			t.Fatal(err)
		}
		var nd *NotDoneError
		if err := land(t, r, id); !errors.As(err, &nd) || !strings.Contains(err.Error(), "tiêu chí 1 chưa đạt") {
			t.Fatalf("--force không được bỏ qua tiêu chí chưa đạt: %v", err)
		}
	})
	t.Run("lệnh kiểm chứng chưa xác nhận", func(t *testing.T) {
		_, r, id := setup(t)
		if _, err := r.Tasks.Update(id, time.Now(), func(tk *task.Task) error {
			tk.Ticket.Verify.Command = "echo lenh-la-chua-xac-nhan"
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		var nd *NotDoneError
		if err := land(t, r, id); !errors.As(err, &nd) || !strings.Contains(err.Error(), "chưa được người xác nhận") {
			t.Fatalf("--force không được bỏ qua lệnh chưa xác nhận: %v", err)
		}
	})
}
