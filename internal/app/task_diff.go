package app

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"bint/internal/config"
	"bint/internal/gitrepo"
	"bint/internal/task"
	"bint/internal/workflow"
)

// ---- task diff ----

// emptyTreeSHA là mã cây rỗng của Git (SHA-1): mốc so sánh khi repository chưa có commit.
const emptyTreeSHA = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// TaskDiffOptions: `bint task diff <task-id> [--stat|--name-only]`.
type TaskDiffOptions struct {
	Ref      string `arg:"task-id" form:"required,suggest=tasks"`
	Stat     bool   `flag:"stat"`
	NameOnly bool   `flag:"name-only"`
}

func TaskDiffDefaults(config.Config) TaskDiffOptions { return TaskDiffOptions{} }

// Validate: cần ID; --stat và --name-only loại trừ nhau.
func (o TaskDiffOptions) Validate() error {
	if strings.TrimSpace(o.Ref) == "" {
		return Usagef("cần ID tác vụ")
	}
	if o.Stat && o.NameOnly {
		return Usagef("--stat và --name-only loại trừ nhau")
	}
	return nil
}

// TaskDiffResult là kết quả `task diff`.
type TaskDiffResult struct {
	Task *task.Task
	// From là mốc so sánh: mã cây BaseTree, hoặc commit base của phiếu cũ.
	From string
	// Paths là các tệp khác nhau (luôn điền, kể cả khi Text là diff đầy đủ).
	Paths []string
	// Legacy: phiếu lập trước khi Bint theo dõi tệp riêng của từng tác vụ; so trên toàn cây.
	Legacy bool
	// Note là lời giải thích kèm kết quả (phiếu cũ, tác vụ chưa sửa tệp nào); rỗng nếu không có gì.
	Note string
	// Text là kết quả của `git diff` (diff đầy đủ, --stat hoặc --name-only theo Options).
	Text string
}

// DiffTask cho biết tác vụ đã đổi gì: so mã cây BaseTree (ảnh chụp trước b-feat đầu tiên, gồm cả thay đổi
// chưa commit có sẵn) với ảnh chụp cây làm việc hiện tại, giới hạn trong Touched (các tệp b-feat của tác vụ
// đã đổi; gồm tệp mới). Nhờ đó không lẫn thay đổi có sẵn hay của tác vụ khác. Phiếu cũ (không có BaseTree)
// so commit base với cây hiện tại trên toàn cây. Không lấy khoá và không ghi cây làm việc (chỉ thêm đối
// tượng Git, như ảnh chụp của bước chỉ đọc).
func DiffTask(ctx context.Context, ws *TaskWS, o TaskDiffOptions) (TaskDiffResult, error) {
	var res TaskDiffResult
	if err := o.Validate(); err != nil {
		return res, err
	}
	t, err := ws.Tasks.Load(o.Ref)
	if err != nil {
		return res, err
	}
	res.Task = t
	root, err := ws.RootFor(t)
	if err != nil {
		return res, err
	}
	var spec []string
	var landedTree string
	switch {
	case t.State.Landed != nil && t.State.Landed.Tree != "":
		// Đã đưa về cây gốc (worktree có thể đã gỡ): so mốc ban đầu với ảnh chụp lúc đưa về, trên tệp đã đưa về.
		res.From, landedTree = t.State.BaseTree, t.State.Landed.Tree
		if res.From == "" {
			res.From = t.State.Base
		}
		if spec = t.State.Landed.Files; len(spec) == 0 {
			res.Note = "tác vụ đã đưa về cây gốc nhưng không có tệp nào"
			return res, nil
		}
		res.Note = "tác vụ đã đưa về cây gốc: diff giữa mốc đầu và ảnh chụp lúc đưa về"
	case t.State.TracksTouched():
		res.From = t.State.BaseTree
		if spec, err = workflow.ScopeFiles(ctx, root, ws.Tasks, t, ws.Cfg.Limits.ProtectedPaths); err != nil {
			return res, err
		}
		if len(spec) == 0 {
			res.Note = "tác vụ chưa sửa tệp nào (b-feat chưa chạy hoặc không đổi gì)"
			return res, nil
		}
	default:
		res.Legacy = true
		res.From = t.State.Base
		if res.From == "" {
			res.From = gitrepo.HeadCommit(ctx, root)
		}
		if res.From == "" {
			res.From = emptyTreeSHA
		}
		res.Note = fmt.Sprintf("phiếu lập trước khi Bint theo dõi tệp riêng của từng tác vụ: so commit %s với cây hiện tại "+
			"trên toàn cây (có thể lẫn thay đổi không thuộc tác vụ này)", shortRev(res.From))
	}
	cur := landedTree
	if cur == "" {
		if cur, err = workflow.WorktreeSnapshot(ctx, root); err != nil {
			return res, fmt.Errorf("chụp cây làm việc: %w", err)
		}
	}
	// Không đưa spec vào dòng lệnh git: tác vụ sửa hàng trăm tệp vượt giới hạn dòng lệnh (Windows 32 KB).
	// Liệt kê toàn bộ rồi lọc theo spec trong Go; diff đầy đủ thì chia lô pathspec.
	all, err := workflow.DiffPaths(ctx, root, res.From, cur)
	if err != nil {
		return res, err
	}
	if len(spec) == 0 {
		res.Paths = all
	} else {
		in := make(map[string]bool, len(spec))
		for _, f := range spec {
			in[f] = true
		}
		for _, f := range all {
			if in[f] {
				res.Paths = append(res.Paths, f)
			}
		}
	}
	switch {
	case len(res.Paths) == 0:
		return res, nil
	case o.NameOnly:
		res.Text = strings.Join(res.Paths, "\n") + "\n"
		return res, nil
	}
	base := []string{"--literal-pathspecs", "diff", "--no-color", "--no-ext-diff", "--no-renames"}
	if o.Stat {
		base = append(base, "--stat")
	}
	base = append(base, res.From, cur)
	var sb strings.Builder
	for _, batch := range pathBatches(res.Paths, maxPathspecBytes) {
		out, derr := gitrepo.Run(ctx, root, append(append(slices.Clone(base), "--"), batch...)...)
		if derr != nil {
			return res, derr
		}
		sb.WriteString(out)
	}
	res.Text = sb.String()
	if len(res.Paths) > 0 && len(pathBatches(res.Paths, maxPathspecBytes)) > 1 && o.Stat {
		res.Note = strings.TrimSpace(res.Note + " (nhiều tệp: bản thống kê in theo từng lô, không có dòng tổng chung)")
	}
	return res, nil
}

// maxPathspecBytes là tổng độ dài pathspec tối đa mỗi lần gọi git (dưới giới hạn dòng lệnh Windows 32767).
const maxPathspecBytes = 16000

// pathBatches chia danh sách đường dẫn thành các lô có tổng độ dài (kèm dấu cách) không quá limit.
func pathBatches(paths []string, limit int) [][]string { return workflow.PathBatches(paths, limit) }

func shortRev(s string) string {
	if len(s) > 10 {
		return s[:10]
	}
	return s
}
