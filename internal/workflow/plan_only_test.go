package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/config"
	"bint/internal/task"
)

// planOnlyPlanned chạy b-req tới planned (không ghi cây).
func planOnlyPlanned(t *testing.T, r *Runner) string {
	t.Helper()
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepReq, Request: "Thêm tính năng phiên bản v2 cho app.txt"})
	if err != nil || res.Status != task.StatusPlanned {
		t.Fatalf("b-req: %+v %v", res, err)
	}
	return res.TaskID
}

func TestChooseIsolationIgnoresPlanOnlyPlannedTasks(t *testing.T) {
	h := newHarness(t)
	h.cfg.Workflow.Isolation = config.IsolationAuto
	r := h.runner()
	ctx := context.Background()
	req := "Thêm tính năng phiên bản v2 cho app.txt"
	cancelAll := func() {
		all, _ := h.tasks.List()
		for _, o := range all {
			if !task.Terminal(o.State.Status) {
				if _, _, err := r.SetStatus(ctx, o.State.ID, task.StatusCancelled, TransitionOptions{Reason: "dọn"}); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	iso := func() string {
		tk, _, err := r.NewTask(ctx, NewTaskOptions{Request: req, Workflow: StepFeat})
		if err != nil {
			t.Fatal(err)
		}
		return tk.State.Isolation()
	}

	// b-req đã planned: không ghi cây nên không đẩy phiếu mới vào worktree.
	planOnlyPlanned(t, r)
	if got := iso(); got != task.IsolationShared {
		t.Fatalf("b-req planned không được tính là tác vụ chưa kết thúc: %s", got)
	}
	cancelAll()

	// b-req còn created (chưa lập xong): vẫn tính.
	if _, _, err := r.NewTask(ctx, NewTaskOptions{Request: req, Workflow: StepReq}); err != nil {
		t.Fatal(err)
	}
	if got := iso(); got != task.IsolationWorktree {
		t.Fatalf("b-req created vẫn phải tính: %s", got)
	}
	cancelAll()

	// b-do planned (có bước ghi): vẫn tính.
	tk, _, err := r.NewTask(ctx, NewTaskOptions{Request: req, Workflow: StepDo, Plan: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	h.tasks.Update(tk.State.ID, time.Now(), func(t *task.Task) error { t.State.Status = task.StatusPlanned; return nil })
	if got := iso(); got != task.IsolationWorktree {
		t.Fatalf("b-do planned vẫn phải tính: %s", got)
	}
}

// Phiếu b-req planned đóng được bằng task status done mà không cần kiểm chứng/duyệt đường dẫn bảo vệ, dù cây có
// commit chạm migrations/** của người khác; phiếu có bước ghi vẫn bị chặn.
func TestPlanOnlyTaskClosesWithoutDoneGate(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	r := h.runner()
	id := planOnlyPlanned(t, r)
	h.tasks.Update(id, time.Now(), func(t *task.Task) error {
		t.Ticket.Verify.Command = "go test ./..." // chưa xác nhận, chưa chạy
		return nil
	})
	h.write("migrations/001.sql", "-- của người khác\n")
	h.git("add", "-A")
	h.git("commit", "-m", "người khác sửa migration")

	agent := &Env{Root: h.root, Cfg: h.cfg, Tasks: h.tasks, Spool: h.spool}
	got, blockers, err := agent.SetStatus(ctx, id, task.StatusDone, TransitionOptions{Reason: "kế hoạch đã dùng xong"})
	if err != nil || len(blockers) != 0 || got.State.Status != task.StatusDone {
		t.Fatalf("done phiếu chỉ lập kế hoạch: %+v %v %v", got, blockers, err)
	}
	last := got.State.History[len(got.State.History)-1]
	if last.From != task.StatusPlanned || last.To != task.StatusDone || last.Reason != "kế hoạch đã dùng xong" {
		t.Fatalf("lịch sử: %+v", last)
	}
}

func TestPlanOnlyGateStaysForTasksWithWriteStep(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	r := h.runner()
	id := planOnlyPlanned(t, r)
	// Phiếu đã chuyển sang b-do: có bước ghi.
	h.tasks.Update(id, time.Now(), func(t *task.Task) error {
		t.State.Workflow = StepDo
		t.Ticket.Verify.Command = ""
		t.Ticket.Verify.Criteria = []string{"đã đổi"}
		t.State.Criteria = []task.CriterionCheck{{Index: 1, Met: true, Evidence: "x"}}
		return nil
	})
	agent := &Env{Root: h.root, Cfg: h.cfg, Tasks: h.tasks, Spool: h.spool}
	if _, _, err := agent.SetStatus(ctx, id, task.StatusDone, TransitionOptions{}); err == nil || !strings.Contains(err.Error(), "planned → done") {
		t.Fatalf("b-do planned không được chuyển done: %v", err)
	}
	// Có thay đổi chạm đường dẫn bảo vệ: bị chặn bằng NotDoneError.
	h.write("migrations/002.sql", "-- sửa\n")
	var nd *NotDoneError
	if _, _, err := agent.SetStatus(ctx, id, task.StatusDone, TransitionOptions{}); !errors.As(err, &nd) {
		t.Fatalf("phải bị chặn bởi migrations/**: %v", err)
	}
	// Phiếu chỉ lập kế hoạch nhưng đã có Touched/PendingPaths: giữ cổng cũ.
	id2 := planOnlyPlanned(t, r)
	h.tasks.Update(id2, time.Now(), func(t *task.Task) error { t.State.PendingPaths = []string{"migrations/002.sql"}; return nil })
	if _, _, err := agent.SetStatus(ctx, id2, task.StatusDone, TransitionOptions{}); err == nil {
		t.Fatal("phiếu có PendingPaths không được đóng bằng đường tắt")
	}
}

// Phiếu chỉ lập kế hoạch ở worktree riêng vẫn cần đưa về trước khi done.
func TestPlanOnlyInWorktreeNeedsLand(t *testing.T) {
	h := newHarness(t).wtHarness()
	ctx := context.Background()
	r := h.runner()
	id := planOnlyPlanned(t, r)
	tk := loadTask(t, h, id)
	if tk.State.Origin == "" {
		t.Skip("b-req không tạo worktree trong cấu hình này")
	}
	var nd *NotDoneError
	if _, _, err := r.SetStatus(ctx, id, task.StatusDone, TransitionOptions{Reason: "xong"}); !errors.As(err, &nd) ||
		!strings.Contains(strings.Join(nd.Blockers, ";"), "task land") {
		t.Fatalf("phải đòi đưa về trước: %v", err)
	}
	if _, err := r.Land(ctx, id, LandOptions{}); err != nil {
		t.Fatalf("land: %v", err)
	}
	if got, _, err := r.SetStatus(ctx, id, task.StatusDone, TransitionOptions{Reason: "xong"}); err != nil || got.State.Status != task.StatusDone {
		t.Fatalf("done sau land: %v", err)
	}
}

// Phiếu chỉ lập kế hoạch ở worktree riêng: thay đổi chạm đường dẫn bảo vệ (sửa ngoài bước) vẫn chặn done và Land
// tới khi người duyệt; không được đưa lặng lẽ về cây gốc.
func TestPlanOnlyInWorktreeProtectedChangeBlocksLandAndDone(t *testing.T) {
	h := newHarness(t).wtHarness()
	ctx := context.Background()
	r := h.runner()
	id := planOnlyPlanned(t, r)
	tk := loadTask(t, h, id)
	if tk.State.Origin == "" {
		t.Skip("b-req không tạo worktree trong cấu hình này")
	}
	root, err := r.Env.RootFor(tk)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "migrations", "001.sql")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("alter table x;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var nd *NotDoneError
	if _, err := r.Land(ctx, id, LandOptions{}); !errors.As(err, &nd) ||
		!strings.Contains(strings.Join(nd.Blockers, ";"), "migrations/001.sql") {
		t.Fatalf("Land phải bị chặn vì đường dẫn bảo vệ chưa duyệt: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(h.root, "migrations", "001.sql")); string(got) != "--\n" {
		t.Fatalf("tệp không được tới cây gốc: %q", got)
	}
	if _, _, err := r.SetStatus(ctx, id, task.StatusDone, TransitionOptions{Reason: "xong"}); !errors.As(err, &nd) ||
		!strings.Contains(strings.Join(nd.Blockers, ";"), "migrations/001.sql") {
		t.Fatalf("done phải bị chặn: %v", err)
	}
}
