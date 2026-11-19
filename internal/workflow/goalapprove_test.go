package workflow

import (
	"context"
	"slices"
	"strings"
	"testing"

	"bint/internal/task"
)

// Hồi quy phát hiện review 01M7PD9T2ASG8XXMEM7SJ2G6C1.1: dialect Codex có ProtectedPathDeny nên hook chặn b-goal ghi tệp test vào đường dẫn
// bảo vệ TRƯỚC khi có tệp; người duyệt bằng `bint task approve --path <tệp>` rồi resume. Trước sửa, ApproveWith đặt ResumeFrom=in_progress nên
// resume nhảy thẳng sang b-feat: b-goal không bao giờ chạy, GoalTest nil và tác vụ done không có kiểm tra mục tiêu độc lập.
// (TestGoalFixRound #7 chỉ phủ đường agent ghi thẳng tệp rồi checkScope dừng chờ duyệt.)
func TestGoalHookDenyThenApprovePathRerunsGoal(t *testing.T) {
	ctx := context.Background()
	h := goalBox(t, true, nil)
	h.cfg.Limits.ProtectedPaths = append(h.cfg.Limits.ProtectedPaths, "goal/**")
	h.denyGoal = []string{frozenRel}
	r := h.runner()
	res, err := r.Run(ctx, RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(true)})
	if err != nil || res.Status != task.StatusBlocked {
		t.Fatalf("hook deny b-goal phải dừng chờ duyệt: %+v %v calls=%v", res, err, h.calls)
	}
	tk := loadTask(t, h, res.TaskID)
	if tk.State.Status != task.StatusBlocked || tk.State.ResumeFrom != task.StatusPlanned || tk.State.GoalTest != nil ||
		!slices.Contains(tk.State.PendingPaths, frozenRel) {
		t.Fatalf("trạng thái sau deny: status=%s resume_from=%s goal=%+v pending=%v", tk.State.Status, tk.State.ResumeFrom, tk.State.GoalTest, tk.State.PendingPaths)
	}

	if _, a, err := r.Approve(res.TaskID, frozenRel); err != nil || !slices.Contains(a.Paths, frozenRel) {
		t.Fatalf("approve --path: %+v %v", a, err)
	}
	if got := loadTask(t, h, res.TaskID).State.ResumeFrom; got != task.StatusPlanned {
		t.Fatalf("approve --path khi b-goal bị chặn phải giữ resume_from=planned để b-goal chạy lại, được %q", got)
	}

	res2, err := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true})
	if err != nil || res2.Status != task.StatusDone {
		t.Fatalf("resume sau duyệt phải tới done: %+v %v calls=%v", res2, err, h.calls)
	}
	if got := strings.Join(h.calls, ","); got != "b-req,b-goal,b-goal,b-feat,b-review" {
		t.Errorf("b-goal phải chạy lại sau duyệt rồi mới b-feat: %s", got)
	}
	tk = loadTask(t, h, res.TaskID)
	if gt := tk.State.GoalTest; gt == nil || !slices.Equal(gt.Paths(), []string{frozenRel}) || gt.Files[0].SHA256 != goalSHA(frozenBody) {
		t.Errorf("phải có kiểm tra mục tiêu đóng băng sau b-goal chạy lại: %+v", gt)
	}
	if last := tk.State.LastGoalCheck(); last == nil || !last.Passed {
		t.Errorf("cổng mục tiêu cuối phải qua: %+v", tk.State.GoalChecks)
	}
}
