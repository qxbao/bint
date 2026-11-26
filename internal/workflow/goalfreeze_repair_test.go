package workflow

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"bint/internal/task"
)

// Lối sửa kiểm tra mục tiêu đóng băng khi chính test sai: kiểm toàn vẹn trước khi b-feat chạy lại (không tốn một lượt b-feat cho kết cục
// đã biết) và `--refreeze-goal` để người duyệt bản sửa rồi đóng băng lại (tệp đã xoá thì thôi đóng băng riêng tệp đó).
func TestGoalFreezeRepair(t *testing.T) {
	ctx := context.Background()
	const fixedBody = "# FROZEN-2 (đã sửa)\ntest -f done.txt\n"

	t.Run("sửa tay giữa hai lượt: chặn trước b-feat, refreeze rồi đi tiếp", func(t *testing.T) {
		feats := 0
		h := goalBox(t, true, func(ctx context.Context, h *harness, e *Env, id string) error {
			if feats++; feats == 1 {
				_, err := e.RecordQuestion(id, StepFeat, "goal/check.sh panic trong chính test?")
				return err
			}
			return h.happy(ctx, StepFeat, e, id)
		})
		r := h.runner()
		res, err := r.Run(ctx, RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(true)})
		if err != nil || res.Status != task.StatusBlocked || !loadTask(t, h, res.TaskID).State.AwaitingAnswer {
			t.Fatalf("b-feat hỏi phải dừng chờ câu trả lời: %+v %v", res, err)
		}
		h.write(frozenRel, fixedBody) // phiên điều phối sửa tay test sai
		res2, err := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true, Answer: "đã sửa test"})
		if err != nil || res2.Status != task.StatusBlocked || !strings.Contains(res2.Reason, frozenRel) || !strings.Contains(res2.Reason, "--refreeze-goal") {
			t.Fatalf("resume phải chặn ngay vì tệp đóng băng đổi và nêu --refreeze-goal: %+v %v", res2, err)
		}
		if got := strings.Join(h.calls, ","); got != "b-req,b-goal,b-feat" {
			t.Fatalf("không được chạy lại b-feat khi tệp đóng băng đã bị sửa: %s", got)
		}
		res3, err := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true, RefreezeGoal: true})
		if err != nil || res3.Status != task.StatusDone {
			t.Fatalf("resume --refreeze-goal: %+v %v calls=%v", res3, err, h.calls)
		}
		tk := loadTask(t, h, res.TaskID)
		gt := tk.State.GoalTest
		if gt == nil || len(gt.Files) != 1 || gt.Files[0].SHA256 != goalSHA(fixedBody) || !slices.Contains(tk.Ticket.Scope.Deny, frozenRel) {
			t.Fatalf("phải đóng băng lại bản đã sửa (vẫn trong scope.deny): %+v deny=%v", gt, tk.Ticket.Scope.Deny)
		}
		if g := tk.State.LastGoalCheck(); g == nil || !g.Passed || g.Voided || !tk.State.GoalChecks[0].Voided {
			t.Errorf("lần đo cũ phải bị vô hiệu, lần cuối đo trên bản mới và qua: %+v", tk.State.GoalChecks)
		}
		if !strings.Contains(strings.Join(res3.Notes, "\n"), "--refreeze-goal") {
			t.Errorf("thiếu ghi chú đóng băng lại: %v", res3.Notes)
		}
	})

	t.Run("b-feat xoá tệp đóng băng: refreeze thôi đóng băng tệp đó", func(t *testing.T) {
		h := goalBox(t, true, func(ctx context.Context, h *harness, e *Env, id string) error {
			if err := h.happy(ctx, StepFeat, e, id); err != nil {
				return err
			}
			return os.Remove(filepath.Join(h.root, filepath.FromSlash(frozenRel)))
		})
		r := h.runner()
		res, err := r.Run(ctx, RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(true)})
		if err != nil || res.Status != task.StatusBlocked || !strings.Contains(res.Reason, "bị xoá hoặc đổi tên") {
			t.Fatalf("phải chặn: %+v %v", res, err)
		}
		res2, err := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true, RefreezeGoal: true})
		if err != nil {
			t.Fatal(err)
		}
		tk := loadTask(t, h, res.TaskID)
		if tk.State.GoalTest != nil || !tk.State.GoalDropped || slices.Contains(tk.Ticket.Scope.Deny, frozenRel) {
			t.Errorf("tệp đã xoá phải thôi đóng băng: GoalTest=%+v dropped=%v deny=%v", tk.State.GoalTest, tk.State.GoalDropped, tk.Ticket.Scope.Deny)
		}
		if n := strings.Count(strings.Join(h.calls, ","), StepFeat); n != 1 || slices.Contains(h.calls, StepGoal) && strings.Count(strings.Join(h.calls, ","), StepGoal) != 1 {
			t.Errorf("không được chạy lại b-feat hay b-goal: %v (%s: %s)", h.calls, res2.Status, res2.Reason)
		}
	})

	t.Run("điều kiện của --refreeze-goal", func(t *testing.T) {
		h := goalBox(t, true, func(ctx context.Context, h *harness, e *Env, id string) error {
			_, err := e.RecordQuestion(id, StepFeat, "?")
			return err
		})
		r := h.runner()
		res, err := r.Run(ctx, RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(true)})
		if err != nil || res.Status != task.StatusBlocked {
			t.Fatalf("%+v %v", res, err)
		}
		if _, err := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true, RefreezeGoal: true}); err == nil || !strings.Contains(err.Error(), "chưa đổi") {
			t.Errorf("tệp chưa đổi phải bị từ chối: %v", err)
		}
		h.write(frozenRel, fixedBody)
		if _, err := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true, RefreezeGoal: true, DropGoal: true}); err == nil {
			t.Errorf("--refreeze-goal cùng --drop-goal phải bị từ chối")
		}
		if _, err := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: res.TaskID, RefreezeGoal: true}); err == nil {
			t.Errorf("--refreeze-goal không có resume phải bị từ chối")
		}
		if got := strings.Join(h.calls, ","); got != "b-req,b-goal,b-feat" {
			t.Errorf("lần bị từ chối không được chạy bước nào: %s", got)
		}
	})
}
