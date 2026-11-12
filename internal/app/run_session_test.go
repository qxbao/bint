package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"bint/internal/task"
	"bint/internal/workflow"
)

// G3a: cờ --driver và dịch vụ driver session (docs/isolation-subagent-plan.md mục 6).

func TestDriverFlagValidate(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"new mặc định", TaskNewOptions{Request: "x", Driver: "auto"}.Validate(), ""},
		{"new không ghi driver", TaskNewOptions{Request: "x"}.Validate(), ""},
		{"new --driver session", TaskNewOptions{Request: "x", Driver: "session"}.Validate(), ""},
		{"new driver lạ", TaskNewOptions{Request: "x", Driver: "thread"}.Validate(), "driver"},
		{"run --driver session", RunWorkflowOptions{Workflow: "b-do", Request: "x", Driver: "session"}.Validate(), ""},
		{"run auto + --task", RunWorkflowOptions{Workflow: "b-do", Task: "T", Driver: "auto"}.Validate(), ""},
		{"run --driver + --task", RunWorkflowOptions{Workflow: "b-do", Task: "T", Driver: "session"}.Validate(), "--task"},
		{"run driver lạ", RunWorkflowOptions{Workflow: "b-do", Request: "x", Driver: "x"}.Validate(), "driver"},
		{"next thiếu id", RunNextOptions{}.Validate(), "cần ID"},
		{"complete thiếu --run", RunCompleteOptions{Ref: "T"}.Validate(), "--run"},
		{"complete thiếu id", RunCompleteOptions{Run: "R"}.Validate(), "cần ID"},
		{"abort thiếu id", RunAbortOptions{}.Validate(), "cần ID"},
		{"complete đủ", RunCompleteOptions{Ref: "T", Run: "R"}.Validate(), ""},
	}
	for _, c := range cases {
		switch {
		case c.want == "" && c.err != nil:
			t.Errorf("%s: %v", c.name, c.err)
		case c.want != "":
			var ue UsageError
			if !errors.As(c.err, &ue) || !strings.Contains(c.err.Error(), c.want) {
				t.Errorf("%s: muốn UsageError chứa %q, được %v", c.name, c.want, c.err)
			}
		}
	}
}

// Ba nguồn chọn driver lúc lập phiếu qua dịch vụ: cờ, cấu hình, mặc định.
func TestNewTaskDriverSources(t *testing.T) {
	ws, _, _ := taskRepo(t)
	ctx := context.Background()
	mk := func(driver string) *task.Task {
		o := TaskNewDefaults(ws.K.Config)
		o.Request, o.Driver, o.NoPlan = "Sửa hàm A trong a.go", driver, true
		c, err := NewTask(ctx, ws, o)
		if err != nil {
			t.Fatal(err)
		}
		return c.Task
	}
	if c := mk("auto").State.Classification; c.EffectiveDriver() != task.DriverProcess || c.DriverReason != task.DriverReasonDefault {
		t.Fatalf("mặc định: %+v", c)
	}
	if c := mk("session").State.Classification; c.EffectiveDriver() != task.DriverSession || c.DriverReason != task.DriverReasonUser {
		t.Fatalf("--driver session: %+v", c)
	}
	ws.Cfg.Workflow.Driver = "session"
	if c := mk("auto").State.Classification; c.EffectiveDriver() != task.DriverSession || c.DriverReason != task.DriverReasonConfig {
		t.Fatalf("workflow.driver: %+v", c)
	}
}

func TestRunSessionServices(t *testing.T) {
	ws, _, id := taskRepo(t)
	ctx := context.Background()
	if _, _, err := ApproveTask(ctx, ws, TaskApproveOptions{Ref: id}); err != nil { // người xác nhận lệnh kiểm chứng
		t.Fatal(err)
	}
	a, err := RunNext(ctx, ws, RunNextOptions{Ref: id})
	if err != nil || a.Action != workflow.ActionAgent || a.RunID == "" {
		t.Fatalf("next: %+v %v", a, err)
	}
	if _, err := RunComplete(ctx, ws, RunCompleteOptions{Ref: id, Run: a.RunID + "x"}); !errors.Is(err, workflow.ErrRunMismatch) {
		t.Fatalf("complete sai run: %v", err)
	}
	if _, err := RunComplete(ctx, ws, RunCompleteOptions{Ref: id}); err == nil {
		t.Fatal("thiếu --run phải lỗi")
	}
	b, err := RunAbort(ctx, ws, RunAbortOptions{Ref: id, Reason: "thử"})
	if err != nil || b.Status != task.StatusInterrupted {
		t.Fatalf("abort: %+v %v", b, err)
	}
	// Không chạy lồng trong một bước workflow khác.
	ws.RunID = "01HRUN"
	if _, err := RunNext(ctx, ws, RunNextOptions{Ref: id}); err == nil || !strings.Contains(err.Error(), "lồng") {
		t.Fatalf("chạy lồng: %v", err)
	}
}
