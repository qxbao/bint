package tasks_test

import (
	"context"
	"strings"
	"testing"

	"bint/internal/app"
	"bint/internal/i18n"
	"bint/internal/task"
	"bint/internal/tui/actions"
	"bint/internal/tui/screens/tasks"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
	"bint/internal/workflow"
)

// setupRig: tác vụ được chọn có lệnh setup worktree với trạng thái xác nhận cho trước (Detail do Detailer dựng).
func setupRig(t *testing.T, ss workflow.SetupStatus, approved bool) *rig {
	t.Helper()
	r := &rig{t: t}
	o := tasks.Options{}
	o.Loader = func(context.Context, string, bool) (tasks.Data, error) { return data(t.TempDir()), nil }
	o.Detailer = func(ctx context.Context, root, id string, f bool) (tasks.Detail, error) {
		d, err := detailer(ctx, root, id, f)
		d.Setup = ss
		if approved && d.Task != nil {
			d.Task.State.SetupApproval = &task.Approval{Command: ss.Command, SHA256: ss.SHA256, By: "human", At: "2026-11-11T10:00:00Z"}
		}
		return d, err
	}
	o.Runs = runs
	o.Gate = func(string) actions.Availability { return actions.Available }
	r.m = tasks.New(o)
	r.m.SetEnv(theme.New(theme.Mono), ui.Rewriter{Cat: i18n.MustLoad("vi")})
	r.feed(r.m.Ensure())
	return r
}

// Thẻ Kiểm chứng hiện lệnh setup worktree và trạng thái xác nhận (chưa / đã).
func TestVerifyTabShowsSetupCommandStatus(t *testing.T) {
	pending := workflow.SetupStatus{Command: "make deps", SHA256: "x", Required: true}
	r := setupRig(t, pending, false)
	r.press(".", ".", ".") // Thông tin → Tài liệu → Tiêu chí → Kiểm chứng
	out := r.view(140, 40)
	for _, want := range []string{"Lệnh setup worktree", "make deps", "chưa được người xác nhận", "xác nhận lệnh setup worktree"} {
		if !strings.Contains(out, want) {
			t.Errorf("thẻ Kiểm chứng (chưa xác nhận) thiếu %q:\n%s", want, out)
		}
	}
	confirmed := workflow.SetupStatus{Command: "make deps", SHA256: "x", Required: true, Confirmed: true}
	r = setupRig(t, confirmed, true)
	r.press(".", ".", ".")
	out = r.view(140, 40)
	if !strings.Contains(out, "make deps") || !strings.Contains(out, "✓") || strings.Contains(out, "xác nhận lệnh setup worktree") {
		t.Errorf("thẻ Kiểm chứng (đã xác nhận):\n%s", out)
	}
	// Không có lệnh setup thì không có phần setup.
	r = setupRig(t, workflow.SetupStatus{}, false)
	r.press(".", ".", ".")
	if out := r.view(140, 40); strings.Contains(out, "Lệnh setup worktree") {
		t.Errorf("không có lệnh setup mà vẫn hiện phần setup:\n%s", out)
	}
}

// Hộp xác nhận duyệt nêu lệnh setup sẽ được xác nhận, kể cả khi đó là thứ duy nhất cần duyệt.
func TestApproveDialogShowsSetupCommand(t *testing.T) {
	r := newRig(t, tasks.Options{})
	tk := data("").Tasks[4]
	r.feed(r.m.Update(actions.ResultMsg{ID: "tasks.plan_approve", Value: app.ApprovePlan{Task: tk, SetupCommand: "make deps"}}))
	out := r.view(140, 40)
	for _, want := range []string{"Sẽ xác nhận lệnh setup worktree", "make deps", "Chỉ xác nhận nếu bạn tin lệnh này"} {
		if !strings.Contains(out, want) {
			t.Errorf("hộp duyệt thiếu %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Không có gì") {
		t.Errorf("lệnh setup chờ xác nhận không được coi là rỗng:\n%s", out)
	}
}

// Review .3 (01M785XQ…): worktree đã tạo (Required=false) mà cấu hình đổi lệnh: thẻ hiện lệnh đã duyệt và đã chạy, không phải
// lệnh mới của cấu hình.
func TestVerifyTabShowsApprovedSetupAfterConfigChange(t *testing.T) {
	r := &rig{t: t}
	o := tasks.Options{}
	o.Loader = func(context.Context, string, bool) (tasks.Data, error) { return data(t.TempDir()), nil }
	o.Detailer = func(ctx context.Context, root, id string, f bool) (tasks.Detail, error) {
		d, err := detailer(ctx, root, id, f)
		d.Setup = workflow.SetupStatus{Command: "make other", SHA256: "y"}
		if d.Task != nil {
			d.Task.State.SetupApproval = &task.Approval{Command: "make deps", SHA256: "x", By: "human", At: "2026-11-11T10:00:00Z"}
		}
		return d, err
	}
	o.Runs = runs
	o.Gate = func(string) actions.Availability { return actions.Available }
	r.m = tasks.New(o)
	r.m.SetEnv(theme.New(theme.Mono), ui.Rewriter{Cat: i18n.MustLoad("vi")})
	r.feed(r.m.Ensure())
	r.press(".", ".", ".")
	out := r.view(140, 40)
	if !strings.Contains(out, "make deps") || !strings.Contains(out, "✓") || strings.Contains(out, "make other") {
		t.Errorf("phải hiện lệnh đã duyệt (make deps), không phải lệnh cấu hình mới:\n%s", out)
	}
}
