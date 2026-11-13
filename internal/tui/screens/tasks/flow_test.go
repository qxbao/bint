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
)

// flowRig dựng màn Tác vụ có danh sách flow tuỳ biến; wf, step (nếu có) đặt workflow và bước flow của tác vụ 01TASK1.
func flowRig(t *testing.T, flows []string, wf, step string) *rig {
	t.Helper()
	r := &rig{t: t}
	o := tasks.Options{}
	set := func(tk *task.Task) {
		if tk.State.ID == "01TASK1" && wf != "" {
			tk.State.Workflow, tk.State.FlowStep = wf, step
		}
	}
	o.Loader = func(context.Context, string, bool) (tasks.Data, error) {
		d := data(t.TempDir())
		d.Flows = flows
		for _, tk := range d.Tasks {
			set(tk)
		}
		return d, nil
	}
	o.Detailer = func(ctx context.Context, root, id string, ro bool) (tasks.Detail, error) {
		d, err := detailer(ctx, root, id, ro)
		if d.Task != nil {
			set(d.Task)
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

// Form run chọn được flow tuỳ biến cùng workflow gốc và gửi RunWorkflowOptions.Workflow = tên flow.
func TestRunFormPicksCustomFlow(t *testing.T) {
	r := flowRig(t, []string{"tdd"}, "", "")
	r.press("r")
	if out := r.view(140, 34); !strings.Contains(out, "tdd") {
		t.Fatalf("form run thiếu flow tuỳ biến:\n%s", out)
	}
	r.press("right", "right", "right", "right") // b-do → b-req → b-feat → b-review → tdd
	r.press("ctrl+s")
	req := r.lastReq()
	ro, ok := req.Opts.(app.RunWorkflowOptions)
	if req.ID != "tasks.run" || !ok || ro.Workflow != "tdd" || ro.Task != "01TASK1" {
		t.Fatalf("run: %+v", req)
	}
}

// Tác vụ của flow tuỳ biến: form run mặc định đúng flow của nó (kể cả khi flow không còn trong danh sách lint sạch).
func TestRunFormDefaultsToTaskCustomFlow(t *testing.T) {
	for _, flows := range [][]string{{"tdd"}, nil} {
		r := flowRig(t, flows, "tdd", "")
		r.press("r", "ctrl+s")
		ro, ok := r.lastReq().Opts.(app.RunWorkflowOptions)
		if !ok || ro.Workflow != "tdd" {
			t.Fatalf("flows=%v: workflow mặc định phải là tdd, được %+v", flows, r.lastReq())
		}
	}
}

// Không có flow tuỳ biến: danh sách workflow của thẻ choices giữ nguyên (không có tdd); form task new cũng chọn được flow.
func TestFormsWithoutCustomFlowsKeepStaticChoices(t *testing.T) {
	r := flowRig(t, nil, "", "")
	r.press("n")
	if out := r.view(140, 34); strings.Contains(out, "tdd") {
		t.Fatalf("không có flow tuỳ biến mà form hiện tdd:\n%s", out)
	}
	r.press("esc")
	r = flowRig(t, []string{"tdd"}, "", "")
	r.press("n")
	if out := r.view(140, 34); !strings.Contains(out, "tdd") {
		t.Fatalf("form task new thiếu flow tuỳ biến:\n%s", out)
	}
}

// Chi tiết tác vụ hiện bước hiện tại của flow.
func TestDetailShowsFlowStep(t *testing.T) {
	r := flowRig(t, []string{"tdd"}, "tdd", "green")
	out := r.view(140, 40)
	if !strings.Contains(out, "Bước flow") || !strings.Contains(out, "green") {
		t.Fatalf("thẻ Phiếu thiếu bước flow:\n%s", out)
	}
}
