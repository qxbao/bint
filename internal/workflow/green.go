package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"bint/internal/gitrepo"
	"bint/internal/task"
)

// Điểm verify xanh (benchmark Bint-vs-plain lần 3: bị cắt giữa vòng sửa thì cây làm việc ở trạng thái dở, không có gì để giao).
//
// Mỗi lần kiểm chứng qua mà có ảnh chụp cây (Verification.Tree) thì ghim ảnh chụp đó bằng ref riêng của tác vụ để git gc không dọn
// và để quay về được. Không thêm trường vào state.json: điểm xanh cuối suy ra từ Verifications (cuối cùng có Passed và Tree), ref
// chỉ giữ đối tượng. Ref xoá cùng base/base-tree khi tác vụ kết thúc (deleteTaskRefs).

// greenRef giữ ảnh chụp cây ở lần kiểm chứng xanh gần nhất; beforeRestoreRef giữ ảnh chụp cây ngay trước khi RestoreGreen --apply
// ghi đè tệp (đường lui của người dùng).
func greenRef(id string) string         { return "refs/bint/tasks/" + id + "/green" }
func beforeRestoreRef(id string) string { return "refs/bint/tasks/" + id + "/before-restore" }

// sliceGreenRef giữ điểm xanh của lát mỏng (slice.go); sliceDiscardRef giữ cây của lát N bị bỏ (git restore --source=<ref> -- <tệp> để lấy lại).
func sliceGreenRef(id string) string { return "refs/bint/tasks/" + id + "/slice-green" }
func sliceDiscardRef(id string, n int) string {
	return fmt.Sprintf("refs/bint/tasks/%s/slice-%d-discarded", id, n)
}

// greenPoint là điểm quay về của tác vụ: lần kiểm chứng xanh cuối hoặc điểm xanh của lát mỏng, cái nào mới hơn.
type greenPoint struct {
	At, Command, Scope, Tree string
	Ref                      string // ref giữ ảnh chụp
}

// lastGreenPoint chọn điểm xanh mới hơn giữa greenVerification và State.SliceGreen (nil nếu chưa có cái nào). Lát mỏng chốt trước khi
// kiểm chứng đầy đủ xanh nên điểm của lát thường mới hơn; kiểm chứng xanh sau đó (vòng sửa) thì điểm của nó mới hơn.
func lastGreenPoint(t *task.Task) *greenPoint {
	var p *greenPoint
	if v := greenVerification(t); v != nil {
		p = &greenPoint{At: v.At, Command: v.Command, Scope: v.Scope, Tree: v.Tree, Ref: greenRef(t.State.ID)}
	}
	if sg := t.State.SliceGreen; sg != nil && sg.Tree != "" && (p == nil || sg.At >= p.At) {
		p = &greenPoint{At: sg.At, Command: "lát mỏng (ô đỏ của kiểm tra mục tiêu giảm, test cũ xanh)", Tree: sg.Tree, Ref: sliceGreenRef(t.State.ID)}
	}
	return p
}

// greenVerification trả lần kiểm chứng xanh cuối có ảnh chụp cây (nil nếu chưa có). Lần không chạy lệnh nào (scope none) không tính.
func greenVerification(t *task.Task) *task.Verification {
	for i := len(t.State.Verifications) - 1; i >= 0; i-- {
		if v := &t.State.Verifications[i]; v.Passed && v.Tree != "" && v.Scope != "none" {
			return v
		}
	}
	return nil
}

// pinGreen ghim ảnh chụp của lần kiểm chứng v nếu nó xanh. Lỗi không chặn kiểm chứng (chỉ mất khả năng quay về).
func (e *Env) pinGreen(ctx context.Context, root, id string, v task.Verification) {
	if !v.Passed || v.Tree == "" || v.Scope == "none" {
		return
	}
	if err := pinTree(ctx, root, v.Tree, greenRef(id), "bint: điểm verify xanh của tác vụ "+id); err != nil && e.Out != nil {
		fmt.Fprintf(e.Out, "! không ghim được điểm verify xanh: %v\n", err)
	}
}

// GreenInfo là điểm verify xanh cuối của tác vụ và những gì đã đổi kể từ đó.
type GreenInfo struct {
	At      string `json:"at"`
	Command string `json:"command"`
	Scope   string `json:"scope,omitempty"`
	// Tree là mã cây ảnh chụp; Pinned: còn ref giữ nó (quay về được).
	Tree   string `json:"tree"`
	Pinned bool   `json:"pinned"`
	// ChangedSince là tệp khác nhau giữa điểm xanh và cây hiện tại (rỗng: cây vẫn như lúc xanh).
	ChangedSince []string `json:"changed_since"`
	// Note: lý do không tính được ChangedSince (không tìm thấy thư mục làm việc, lỗi git).
	Note string `json:"note,omitempty"`
}

// OpenFinding là phát hiện còn mở (kết cục open) của vòng review mới nhất.
type OpenFinding struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Path     string `json:"path,omitempty"`
	Round    int    `json:"round"`
}

// GreenReport dựng điểm verify xanh cuối và các phát hiện còn mở của vòng review mới nhất. Chỉ đọc (không đụng cây làm việc,
// chỉ chụp bằng chỉ mục tạm). Cả hai có thể nil/rỗng.
func (e *Env) GreenReport(ctx context.Context, t *task.Task) (*GreenInfo, []OpenFinding) {
	return e.greenInfo(ctx, t), openFindings(t)
}

func (e *Env) greenInfo(ctx context.Context, t *task.Task) *GreenInfo {
	v := lastGreenPoint(t)
	if v == nil {
		return nil
	}
	g := &GreenInfo{At: v.At, Command: v.Command, Scope: v.Scope, Tree: v.Tree, ChangedSince: []string{}}
	root, err := e.RootFor(t)
	if err != nil {
		g.Note = err.Error()
		return g
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
	defer cancel()
	if got, err := gitrepo.Run(ctx, root, "rev-parse", "--verify", "-q", v.Ref+"^{tree}"); err == nil && strings.TrimSpace(got) == v.Tree {
		g.Pinned = true
	}
	cur, err := WorktreeSnapshot(ctx, root)
	if err != nil {
		g.Note = "chụp cây hiện tại: " + err.Error()
		return g
	}
	changed, err := DiffPaths(ctx, root, v.Tree, cur)
	if err != nil {
		g.Note = "so với điểm xanh: " + err.Error()
		return g
	}
	if changed != nil {
		g.ChangedSince = changed
	}
	return g
}

func openFindings(t *task.Task) []OpenFinding {
	views, err := ViewFindings(t)
	if err != nil || len(views) == 0 {
		return nil
	}
	last := 0
	for _, v := range views {
		last = max(last, v.Round)
	}
	var out []OpenFinding
	for _, v := range views {
		if v.Round != last || (v.Outcome.Outcome != "" && v.Outcome.Outcome != task.OutcomeOpen) {
			continue
		}
		f := v.Finding
		out = append(out, OpenFinding{ID: f.ID, Kind: f.Kind, Severity: f.Severity, Title: f.Title, Path: f.Path, Round: v.Round})
	}
	return out
}

// attachGreen gắn điểm xanh và phát hiện còn mở vào kết quả khi tác vụ dừng chờ người hoặc hỏng/bị ngắt.
func (r *Runner) attachGreen(rs *runState) {
	switch rs.t.State.Status {
	case task.StatusBlocked, task.StatusInterrupted, task.StatusFailed:
		rs.res.LastGreen, rs.res.OpenFindings = r.Env.GreenReport(context.Background(), rs.t)
	}
}

// RestoreResult là kết quả (hoặc kế hoạch, khi chưa --apply) của RestoreGreen.
type RestoreResult struct {
	TaskID string `json:"task_id"`
	Tree   string `json:"green_tree"`
	// Applied: đã ghi đè tệp; false là chỉ xem trước.
	Applied bool `json:"applied"`
	// Restore: tệp của tác vụ khác điểm xanh, sẽ (hoặc đã) được đưa về đúng nội dung điểm xanh (tệp tạo sau điểm xanh thì bị xoá).
	Restore []string `json:"restore"`
	// Skipped: tệp khác điểm xanh nhưng không thuộc tác vụ (không theo dõi được là của tác vụ): giữ nguyên.
	Skipped []string `json:"skipped"`
	// Backup là ref giữ ảnh chụp cây ngay trước khi ghi đè (chỉ khi Applied): git restore --source=<ref> -- <tệp> để lùi lại.
	Backup string `json:"backup,omitempty"`
}

// ErrNoGreen: tác vụ chưa có điểm verify xanh để quay về.
var ErrNoGreen = errors.New("tác vụ chưa có điểm verify xanh (lần kiểm chứng qua có ảnh chụp cây)")

// RestoreGreen đưa các tệp CỦA TÁC VỤ về nội dung ở điểm verify xanh cuối. Mặc định (apply=false) chỉ liệt kê; apply=true mới ghi.
// Chỉ đụng tệp khác điểm xanh mà thuộc tác vụ (Touched của tác vụ có theo dõi tệp, hoặc mọi tệp của worktree riêng của tác vụ); tệp
// khác giữ nguyên và được liệt kê ở Skipped. Trước khi ghi, ảnh chụp cây hiện tại được ghim ở beforeRestoreRef để lùi lại. Chỉ cho tác
// vụ đang dừng (blocked/interrupted/failed) và lấy khoá độc quyền của cây. Quyền "chỉ người" do lớp app kiểm (RequireHuman).
func (e *Env) RestoreGreen(ctx context.Context, ref string, apply bool) (*RestoreResult, error) {
	t, err := e.Tasks.Load(ref)
	if err != nil {
		return nil, err
	}
	id := t.State.ID
	if !task.Paused(t.State.Status) {
		return nil, fmt.Errorf("tác vụ %s đang %s: chỉ quay về điểm xanh khi tác vụ dừng (blocked, interrupted hoặc failed)", id, t.State.Status)
	}
	v := lastGreenPoint(t)
	if v == nil {
		return nil, ErrNoGreen
	}
	root, err := e.RootFor(t)
	if err != nil {
		return nil, err
	}
	if _, err := gitrepo.Run(ctx, root, "cat-file", "-e", v.Tree+"^{tree}"); err != nil {
		return nil, fmt.Errorf("ảnh chụp %s của điểm xanh không còn trong repository (đã bị git gc?): %w", v.Tree, err)
	}
	if apply {
		lock, lerr := AcquireLock(e.Tasks.Dir, root, "restore-green "+id, LockExclusive)
		if lerr != nil {
			return nil, fmt.Errorf("không lấy được khoá độc quyền của cây (có bước khác đang chạy?): %w", lerr)
		}
		defer lock.Release()
	}
	return restoreTree(ctx, root, t, v.Tree, apply, beforeRestoreRef(id))
}

// restoreTree đưa các tệp CỦA TÁC VỤ t về nội dung ở cây tree (luật chọn tệp: xem RestoreGreen). Người gọi giữ khoá độc quyền của cây khi
// apply. backupRef khác rỗng thì ghim cây hiện tại ở đó trước khi ghi (chỉ khi có tệp để khôi phục); rỗng thì người gọi đã tự ghim.
func restoreTree(ctx context.Context, root string, t *task.Task, tree string, apply bool, backupRef string) (*RestoreResult, error) {
	cur, err := WorktreeSnapshot(ctx, root)
	if err != nil {
		return nil, err
	}
	changed, err := DiffPaths(ctx, root, tree, cur)
	if err != nil {
		return nil, err
	}
	res := &RestoreResult{TaskID: t.State.ID, Tree: tree, Restore: []string{}, Skipped: []string{}}
	isolated := t.State.Origin != "" && t.State.Worktree != ""
	for _, f := range changed {
		if isolated || (t.State.TracksTouched() && slices.Contains(t.State.Touched, f)) {
			res.Restore = append(res.Restore, f)
		} else {
			res.Skipped = append(res.Skipped, f)
		}
	}
	if !apply || len(res.Restore) == 0 {
		return res, nil
	}
	if backupRef != "" {
		if err := pinTree(ctx, root, cur, backupRef, "bint: cây trước khi quay về điểm xanh của tác vụ "+t.State.ID); err != nil {
			return nil, fmt.Errorf("không ghim được cây hiện tại làm đường lui, không ghi đè gì: %w", err)
		}
		res.Backup = backupRef
	}
	for _, f := range res.Restore {
		abs := filepath.Join(root, filepath.FromSlash(f))
		if _, err := gitrepo.Run(ctx, root, "--literal-pathspecs", "cat-file", "-e", tree+":"+f); err != nil {
			// Không có ở điểm xanh: tệp tạo sau đó.
			if rerr := os.Remove(abs); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
				return res, fmt.Errorf("xoá %s: %w (đường lui: %s)", f, rerr, res.Backup)
			}
			continue
		}
		// --worktree (mặc định của restore): không đụng chỉ mục của người dùng.
		if _, err := gitrepo.Run(ctx, root, "--literal-pathspecs", "restore", "--source="+tree, "--worktree", "--", f); err != nil {
			return res, fmt.Errorf("khôi phục %s: %w (đường lui: %s)", f, err, res.Backup)
		}
	}
	res.Applied = true
	return res, nil
}
