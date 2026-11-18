package workflow

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"bint/internal/gitrepo"
	"bint/internal/task"
)

// Ngân sách diff (workflow.max_diff_lines): số dòng thêm+xoá trên tệp của tác vụ, không tính tệp kiểm tra mục tiêu đóng băng. Mỗi lát đo so với
// điểm xanh (sliceGate); khi không chia lát, phần b-feat đã làm đo so với mốc tác vụ (BaseTree) và kết quả đỏ ghi vào State.BudgetChecks.

// diffLines đếm dòng thêm+xoá giữa hai cây, bỏ tệp đóng băng và (khi tác vụ theo dõi tệp riêng) tệp ngoài Touched. Tệp nhị phân tính 0.
func diffLines(ctx context.Context, root, from, to string, t *task.Task) (int, error) {
	out, err := gitrepo.Run(ctx, root, "diff", "--numstat", "--no-renames", "-z", from, to)
	if err != nil {
		return 0, err
	}
	frozen := t.State.GoalTest.Paths()
	total := 0
	// -z: "<thêm>\t<xoá>\t<đường dẫn>\0"
	for rec := range strings.SplitSeq(out, "\x00") {
		parts := strings.SplitN(rec, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		path := parts[2]
		if slices.Contains(frozen, path) || task.IsLedgerPath(path) {
			continue
		}
		if t.State.TracksTouched() && len(t.State.Touched) > 0 && !slices.Contains(t.State.Touched, path) {
			continue
		}
		a, _ := strconv.Atoi(parts[0]) // "-" (nhị phân) → 0
		d, _ := strconv.Atoi(parts[1])
		total += a + d
	}
	return total, nil
}

// budgetOver đo diff từ cây from tới cây hiện tại; over=true khi vượt trần. msg là số đo (rỗng khi ngân sách tắt hoặc không đo được).
func (r *Runner) budgetOver(ctx context.Context, rs *runState, from, what string) (over bool, msg string) {
	limit := r.Cfg.Workflow.MaxDiffLines
	if limit <= 0 || from == "" {
		return false, ""
	}
	snap, err := WorktreeSnapshot(ctx, r.Root)
	if err != nil {
		return false, ""
	}
	n, err := diffLines(ctx, r.Root, from, snap, rs.t)
	if err != nil {
		return false, ""
	}
	msg = fmt.Sprintf("%d dòng thêm+xoá %s, trần workflow.max_diff_lines = %d (không tính tệp kiểm tra mục tiêu đóng băng)", n, what, limit)
	return n > limit, msg
}

// budgetGate chạy sau bước b-feat khi không chia lát: ghi kết quả đo vào State.BudgetChecks (đỏ vào vòng sửa qua redVerification).
func (r *Runner) budgetGate(ctx context.Context, rs *runState) {
	if r.Cfg.Workflow.MaxDiffLines <= 0 || rs.activeSlice() != nil {
		return
	}
	from, what := rs.t.State.BaseTree, "so với mốc tác vụ"
	if sg := rs.t.State.SliceGreen; len(rs.t.State.Slices) > 0 && sg != nil && sg.Tree != "" {
		// Tác vụ đã chia lát (các lát đã chốt, đây là vòng sửa sau review): mỗi lát đã qua ngân sách riêng, nên đo phần b-feat này so với điểm xanh
		// gần nhất chứ không cộng dồn mọi lát từ mốc tác vụ.
		from, what = sg.Tree, "so với điểm xanh"
	}
	over, msg := r.budgetOver(ctx, rs, from, what)
	if msg == "" {
		return
	}
	exit := 0
	if over {
		exit = 1
	}
	v := task.Verification{At: r.at(), Command: "ngân sách diff: " + msg, Confirmed: true, ExitCode: exit, Passed: !over, By: r.actor(), RunID: r.RunID}
	if nt, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error {
		t.State.BudgetChecks = append(t.State.BudgetChecks, v)
		return nil
	}); err == nil {
		rs.t = nt
	}
	r.logf("  ngân sách diff: %s → %s", msg, passWord(!over))
}

// isBudgetCheck cho biết v là lần đo ngân sách diff gần nhất của t.
func isBudgetCheck(t *task.Task, v *task.Verification) bool {
	return v != nil && v == t.State.LastBudgetCheck()
}
