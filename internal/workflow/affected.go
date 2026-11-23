package workflow

import (
	"context"
	"path/filepath"
	"strings"

	"bint/internal/affected"
	"bint/internal/gitrepo"
	"bint/internal/task"
	"bint/internal/testsel"
)

// ReasonNoBase: không có commit/ref gốc để so (nới ra full).
const ReasonNoBase = affected.ReasonNoBase

// SelectOpts là tuỳ chọn của SelectAffected.
type SelectOpts struct {
	// Head: commit đích; rỗng thì so với cây làm việc ở root.
	Head string
	// BintConfigFull: workflow.verify.bint_config_full. false thì thay đổi chỉ trong .bint/** không ảnh hưởng test.
	BintConfigFull bool
	// TasksDir: thư mục tác vụ của Bint (<state>/runtime/tasks) để đặt chỉ mục bền của bgraph cạnh state; rỗng thì bgraph dùng cache của người dùng.
	TasksDir string
	// Only: chỉ giữ đơn vị test chứa các tệp này (affected.Input.Only).
	Only []string
	// PreviouslyFailed: test đỏ ở lần trước, luôn chạy lại.
	PreviouslyFailed []string
}

// SelectAffected chọn test bị ảnh hưởng bởi thay đổi của cây làm việc ở root (hoặc commit o.Head) so với base, bằng bgraph. Sổ phiếu
// (.bint/tasks) không bao giờ là thay đổi. Không tính được thì nới ra full kèm lý do, không bao giờ thu hẹp.
func SelectAffected(ctx context.Context, root, base string, o SelectOpts) affected.Selection {
	ex := []string{task.LedgerDir + "/**"}
	if !o.BintConfigFull {
		ex = append(ex, ".bint/**")
	}
	in := testsel.Input{Root: root, Base: base, Head: o.Head, Exclude: ex, Only: o.Only, PreviouslyFailed: o.PreviouslyFailed}
	if o.TasksDir != "" {
		in.StateDir = bgraphDirFor(o.TasksDir)
	}
	return testsel.Select(ctx, in)
}

// bgraphDirFor là thư mục chỉ mục bền của bgraph (<state>/bgraph) ứng với thư mục tác vụ tasksDir (<state>/runtime/tasks). Nằm ngoài cây
// DataWatch theo dõi nên cập nhật chỉ mục không kích hoạt nạp lại TUI.
func bgraphDirFor(tasksDir string) string {
	return filepath.Join(filepath.Dir(filepath.Dir(tasksDir)), "bgraph")
}

// selectFor chọn test của tác vụ t so với base (rỗng: mốc tác vụ) trên cây làm việc root, theo cấu hình của Env.
func (e *Env) selectFor(ctx context.Context, t *task.Task, root, base string, only []string) affected.Selection {
	if base == "" {
		base = t.State.Base
	}
	return SelectAffected(ctx, root, base, SelectOpts{BintConfigFull: e.Cfg.Workflow.Verify.BintConfigFull, TasksDir: e.Tasks.Dir, Only: only})
}

// testRun là lệnh chạy cho một phần chọn: script của bgraph (kèm workflow.verify.runner_args, xem withRunnerArgs), verify.command của phiếu
// khi scope full, hoặc không chạy gì khi none.
func testRun(sel affected.Selection, cmd string, runnerArgs map[string][]string) (ran string, run bool) {
	switch sel.Scope {
	case affected.TierNone:
		return "", false
	case affected.TierFull:
		return cmd, true
	}
	if s := withRunnerArgs(sel, cmd, runnerArgs).Script(); s != "" {
		return s, true
	}
	return cmd, true
}

// fullReason mô tả vì sao bgraph đòi chạy cả bộ (rỗng khi không phải full).
func fullReason(sel affected.Selection) string {
	if sel.Scope != affected.TierFull {
		return ""
	}
	var parts []string
	for _, w := range sel.Widening {
		if w.Tier != affected.TierFull {
			continue
		}
		s := w.Reason
		if w.Path != "" {
			s += " " + w.Path
		}
		if w.Detail != "" {
			s += " (" + w.Detail + ")"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, "; ")
}

// greenCommit trả commit (con của mốc tác vụ) chứa cây tree, giữ bằng ref refs/bint/green/<id> để git gc không xoá: dùng làm mốc của kiểm hẹp
// "kể từ lần kiểm xanh trước". Lỗi (sandbox chặn ghi .git) thì trả rỗng và người gọi so với mốc tác vụ.
func greenCommit(ctx context.Context, root string, t *task.Task, tree string) string {
	if tree == "" || strings.TrimSpace(t.State.Base) == "" {
		return ""
	}
	c, err := commitTree(ctx, root, tree, "bint: kiểm hẹp xanh")
	if err != nil {
		return ""
	}
	if _, err := gitrepo.Run(ctx, root, "update-ref", "refs/bint/green/"+t.State.ID, c); err != nil {
		return ""
	}
	return c
}
