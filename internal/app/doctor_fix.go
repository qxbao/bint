package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"bint/internal/gitrepo"
	"bint/internal/state"
	"bint/internal/task"
	"bint/internal/workflow"
)

// DoctorFixOptions chọn việc sửa của một kết quả doctor (TUI: thẻ Doctor, phím f). Không có cờ CLI: đây không phải lệnh.
type DoctorFixOptions struct {
	Kind   DoctorFixKind
	Path   string
	TaskID string
}

// DoctorFixResult liệt kê các việc đã làm.
type DoctorFixResult struct {
	Kind    DoctorFixKind
	Actions []string
}

// Validate kiểm loại sửa mà FixDoctor tự chạy; config_template và sync đi qua luồng xem trước riêng.
func (o DoctorFixOptions) Validate() error {
	switch o.Kind {
	case FixWorktree:
		if strings.TrimSpace(o.Path) == "" {
			return fmt.Errorf("sửa worktree mồ côi: thiếu đường dẫn")
		}
	case FixSpool:
	default:
		return fmt.Errorf("loại sửa %q không chạy qua dịch vụ này (dùng luồng xem trước tương ứng)", o.Kind)
	}
	return nil
}

// DescribeDoctorFix trả các việc FixDoctor sẽ làm, để hộp xác nhận nêu rõ đường dẫn và nhánh bị xoá.
func DescribeDoctorFix(ctx context.Context, dir string, o DoctorFixOptions) ([]string, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	if o.Kind == FixSpool {
		return []string{"nạp các tệp spool vào cơ sở dữ liệu (events ingest)"}, nil
	}
	ws, err := OpenWorkspace(ctx, dir, true)
	if err != nil {
		return nil, err
	}
	env := &workflow.Env{Root: ws.Repo.Toplevel, Tasks: task.NewStore(ws.Paths.Runtime)}
	orph, err := findFixableOrphan(ctx, env, ws.Paths, o)
	if err != nil {
		return nil, err
	}
	return env.CleanupCommands(*orph), nil
}

// FixDoctor thực hiện một việc sửa. Với worktree mồ côi, trạng thái được tính lại lúc chạy (không tin báo cáo cũ).
func FixDoctor(ctx context.Context, dir string, o DoctorFixOptions) (DoctorFixResult, error) {
	res := DoctorFixResult{Kind: o.Kind}
	if err := o.Validate(); err != nil {
		return res, err
	}
	if o.Kind == FixSpool {
		r, err := OpenRuns(ctx, dir, true)
		if err != nil {
			return res, err
		}
		defer r.Close()
		ir, err := IngestEvents(ctx, r, EventsIngestOptions{})
		if err != nil {
			return res, err
		}
		res.Actions = append(res.Actions, fmt.Sprintf("đã nạp spool (%d sự kiện mới)", ir.Events))
		return res, nil
	}
	ws, err := OpenWorkspace(ctx, dir, true)
	if err != nil {
		return res, err
	}
	env := &workflow.Env{Root: ws.Repo.Toplevel, Tasks: task.NewStore(ws.Paths.Runtime)}
	orph, err := findFixableOrphan(ctx, env, ws.Paths, o)
	if err != nil {
		return res, err
	}
	if err := gitrepo.WorktreeRemove(ctx, env.Root, orph.Path); err != nil {
		return res, fmt.Errorf("gỡ worktree %s: %w", orph.Path, err)
	}
	if _, err := os.Stat(orph.Path); err == nil {
		return res, fmt.Errorf("worktree %s đã được git bỏ nhưng thư mục vẫn còn: xoá tay sau khi kiểm tra nội dung", orph.Path)
	}
	res.Actions = append(res.Actions, "đã gỡ worktree "+orph.Path)
	if orph.TaskID != "" {
		br := workflow.BranchFor(orph.TaskID)
		if err := gitrepo.DeleteBranch(ctx, env.Root, br); err != nil {
			return res, fmt.Errorf("xoá nhánh %s: %w", br, err)
		}
		res.Actions = append(res.Actions, "đã xoá nhánh "+br)
	}
	return res, nil
}

// findFixableOrphan tìm lại worktree mồ côi khớp o lúc này; từ chối khi không còn mồ côi, nằm ngoài thư mục worktrees của
// Bint, hoặc là Unlanded/AwaitingMerge (thay đổi của tác vụ chỉ còn ở đó).
func findFixableOrphan(ctx context.Context, env *workflow.Env, paths state.Paths, o DoctorFixOptions) (*workflow.OrphanWorktree, error) {
	want := state.NormalizePath(o.Path)
	dir := state.WorktreesDirFor(env.Tasks.Dir)
	rel, err := filepath.Rel(state.NormalizePath(dir), want)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return nil, fmt.Errorf("%s nằm ngoài thư mục worktree của Bint (%s): không xoá", o.Path, dir)
	}
	orphans, err := env.OrphanWorktrees(ctx)
	if err != nil {
		return nil, err
	}
	for i := range orphans {
		g := orphans[i]
		if state.NormalizePath(g.Path) != want || g.TaskID != o.TaskID {
			continue
		}
		if g.Unlanded || g.AwaitingMerge {
			return nil, fmt.Errorf("worktree %s còn thay đổi của tác vụ chưa đưa về (đang chờ lấy ra hoặc ghép tay): không xoá", g.Path)
		}
		return &g, nil
	}
	return nil, fmt.Errorf("%s không còn là worktree mồ côi (tác vụ có thể đang dùng, hoặc đã được dọn): không xoá", o.Path)
}
