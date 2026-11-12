package workflow

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"time"

	"bint/internal/gitrepo"
	"bint/internal/state"
	"bint/internal/task"
)

// ErrWorktreeGone: thư mục làm việc ghi trong phiếu (State.Worktree) không còn hoặc không còn
// là gốc một worktree Git.
var ErrWorktreeGone = errors.New("thư mục làm việc của tác vụ không còn")

// RootFor trả về thư mục chạy cho tác vụ t: State.Worktree nếu có (phiếu cũ không có thì Root).
// Thư mục phải tồn tại và là gốc một worktree Git; nếu không thì lỗi bọc ErrWorktreeGone kèm
// đường dẫn — không lùi về Root vì sẽ chạy trên nhầm cây.
func (e *Env) RootFor(t *task.Task) (string, error) {
	w := t.State.Worktree
	if w == "" || samePath(w, e.Root) {
		return e.Root, nil
	}
	fi, err := os.Stat(w)
	if err != nil || !fi.IsDir() {
		return "", fmt.Errorf("%w: %s", ErrWorktreeGone, w)
	}
	top, err := gitrepo.Run(context.Background(), w, "rev-parse", "--show-toplevel")
	if err != nil || !samePath(top, w) {
		return "", fmt.Errorf("%w: %s (không còn là gốc worktree Git)", ErrWorktreeGone, w)
	}
	return w, nil
}

func samePath(a, b string) bool {
	return state.NormalizePath(strings.TrimSpace(a)) == state.NormalizePath(strings.TrimSpace(b))
}

// DiffPaths liệt kê tệp khác nhau giữa hai mã cây (hoặc commit), giới hạn trong pathspec nếu có.
// --no-renames để đổi tên hiện cả tên cũ lẫn tên mới (nếu không, tệp bị đổi tên khỏi đường dẫn
// bảo vệ sẽ lọt kiểm tra phạm vi). Lỗi git được trả về, không nuốt.
func DiffPaths(ctx context.Context, root, before, after string, pathspec ...string) ([]string, error) {
	args := []string{"--literal-pathspecs", "diff", "--name-only", "--no-renames", "-z", before, after}
	if len(pathspec) > 0 {
		args = append(args, "--")
		args = append(args, pathspec...)
	}
	out, err := gitrepo.Run(ctx, root, args...)
	if err != nil {
		return nil, err
	}
	var ps []string
	for p := range strings.SplitSeq(out, "\x00") {
		if p != "" {
			ps = append(ps, p)
		}
	}
	return ps, nil
}

// pinTree tạo một commit rỗng-cha-HEAD trỏ vào cây tree và gắn ref để git gc không dọn cây.
func pinTree(ctx context.Context, root, tree, ref, msg string) error {
	args := []string{"-C", root, "commit-tree", tree, "-m", msg}
	if head := gitrepo.HeadCommit(ctx, root); head != "" {
		args = append(args, "-p", head)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=bint", "GIT_AUTHOR_EMAIL=bint@localhost",
		"GIT_COMMITTER_NAME=bint", "GIT_COMMITTER_EMAIL=bint@localhost")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git commit-tree: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	commit := strings.TrimSpace(stdout.String())
	_, err := gitrepo.Run(ctx, root, "update-ref", ref, commit)
	return err
}

func baseRef(id string) string { return "refs/bint/tasks/" + id + "/base" }

// unionSorted hợp hai danh sách đường dẫn: sắp xếp, bỏ trùng.
func unionSorted(a, b []string) []string {
	out := slices.Clone(a)
	out = append(out, b...)
	sort.Strings(out)
	return slices.Compact(out)
}

// beginTouched chụp cây ngay sau khi có khoá độc quyền, trước agent b-feat. Lần b-feat đầu tiên của
// tác vụ (firstFeat: tác vụ chưa từng in_progress) đặt BaseTree. Tác vụ đã có b-feat từ trước khi có
// BaseTree (hoặc làm tay) giữ chế độ cũ suốt đời: đặt muộn sẽ bỏ sót tệp đã sửa ở lượt trước, kể cả
// đường dẫn bảo vệ. Trả "" khi không theo dõi (không phải lỗi) hoặc chụp lỗi (quay về chế độ cũ).
func (r *Runner) beginTouched(ctx context.Context, rs *runState, firstFeat bool) string {
	if rs.t.State.BaseTree == "" && !firstFeat {
		return ""
	}
	snap, err := WorktreeSnapshot(ctx, r.Root)
	if err != nil {
		r.dropTouched(rs, "chụp cây làm việc trước b-feat: "+err.Error())
		return ""
	}
	if rs.t.State.BaseTree != "" {
		return snap
	}
	id := rs.t.State.ID
	if err := pinTree(ctx, r.Root, snap, baseRef(id), "bint: cây gốc của tác vụ "+id); err != nil {
		r.dropTouched(rs, "giữ cây gốc của tác vụ: "+err.Error())
		return ""
	}
	t, err := r.Tasks.Update(id, r.now(), func(t *task.Task) error {
		t.State.BaseTree, t.State.Touched = snap, nil
		return nil
	})
	if err != nil {
		r.dropTouched(rs, "ghi cây gốc của tác vụ: "+err.Error())
		return ""
	}
	rs.t = t
	return snap
}

// endTouched chụp cây sau agent b-feat (kể cả khi agent lỗi hoặc bị huỷ: ctx của lần chụp không bị
// huỷ theo) và hợp các tệp đã đổi vào Touched.
func (r *Runner) endTouched(ctx context.Context, rs *runState, before string) {
	if before == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
	defer cancel()
	after, err := WorktreeSnapshot(ctx, r.Root)
	var changed []string
	if err == nil {
		changed, err = DiffPaths(ctx, r.Root, before, after)
	}
	if err != nil {
		r.dropTouched(rs, "chụp cây làm việc sau b-feat: "+err.Error())
		return
	}
	t, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error {
		t.State.Touched = unionSorted(t.State.Touched, changed)
		return nil
	})
	if err != nil {
		r.dropTouched(rs, "ghi tập tệp của tác vụ: "+err.Error())
		return
	}
	rs.t = t
}

// dropTouched bỏ theo dõi tập tệp (quay về chế độ cũ: tính theo toàn bộ thay đổi so với Base) và ghi chú.
func (r *Runner) dropTouched(rs *runState, why string) {
	id := rs.t.State.ID
	if t, err := r.Tasks.Update(id, r.now(), func(t *task.Task) error {
		t.State.BaseTree, t.State.Touched = "", nil
		return nil
	}); err == nil {
		rs.t = t
	}
	gitrepo.Run(context.Background(), r.Root, "update-ref", "-d", baseRef(id))
	rs.res.Notes = append(rs.res.Notes, "không theo dõi riêng tệp của tác vụ ("+why+"): phạm vi tính theo toàn bộ thay đổi so với base")
}

// overlaps trả về, theo từng tác vụ khác chưa kết thúc trên cùng worktree có theo dõi tập tệp, các tệp
// mà cả hai cùng sửa.
func (r *Runner) overlaps(t *task.Task) map[string][]string {
	if !t.State.TracksTouched() || len(t.State.Touched) == 0 {
		return nil
	}
	all, _ := r.Tasks.List()
	out := map[string][]string{}
	for _, o := range all {
		if o.State.ID == t.State.ID || task.Terminal(o.State.Status) || !o.State.TracksTouched() || o.State.Worktree == "" ||
			!samePath(o.State.Worktree, r.Root) {
			continue
		}
		var common []string
		for _, f := range t.State.Touched {
			if slices.Contains(o.State.Touched, f) {
				common = append(common, f)
			}
		}
		if len(common) > 0 {
			out[o.State.ID] = common
		}
	}
	return out
}

// outsideFeat liệt kê tệp đổi giữa BaseTree và cây hiện tại mà không thuộc Touched của tác vụ nào trên
// cùng worktree (kể cả tác vụ đã kết thúc): tức là việc ngoài b-feat do Runner chạy — sửa tay bằng
// agent hay người, ở bất kỳ trạng thái nào của tác vụ. Chúng được tính vào phạm vi của t để không lọt
// kiểm tra đường dẫn bảo vệ/deny. Tệp có sẵn chưa commit lúc BaseTree không đổi thì không nằm ở đây.
// Giới hạn: thay đổi tay của người dùng ở cùng worktree cũng bị quy cho t (bảo thủ: chỉ hỏi duyệt).
func outsideFeat(ctx context.Context, root string, tasks task.Store, t *task.Task) ([]string, error) {
	if !t.State.TracksTouched() {
		return nil, nil
	}
	cur, err := WorktreeSnapshot(ctx, root)
	if err != nil {
		return nil, err
	}
	changed, err := DiffPaths(ctx, root, t.State.BaseTree, cur)
	if err != nil {
		return nil, err
	}
	if len(changed) == 0 {
		return nil, nil
	}
	claimed := map[string]bool{}
	for _, f := range t.State.Touched {
		claimed[f] = true
	}
	all, _ := tasks.List()
	for _, o := range all {
		if o.State.ID == t.State.ID || o.State.Worktree == "" || !samePath(o.State.Worktree, root) {
			continue
		}
		for _, f := range o.State.Touched {
			claimed[f] = true
		}
	}
	var out []string
	for _, f := range changed {
		if !claimed[f] {
			out = append(out, f)
		}
	}
	return out, nil
}

// ScopeFiles là tập tệp dùng cho kiểm tra phạm vi và `bint task diff` của tác vụ t trên root: Touched
// hợp với thay đổi ngoài b-feat (outsideFeat) nếu có theo dõi, ngược lại toàn bộ thay đổi so với Base
// (phiếu cũ). Chụp cây hiện tại (chỉ thêm đối tượng Git, không đụng cây làm việc).
func ScopeFiles(ctx context.Context, root string, tasks task.Store, t *task.Task) ([]string, error) {
	if !t.State.TracksTouched() {
		return task.ChangedFiles(ctx, root, t.State.Base)
	}
	out, err := outsideFeat(ctx, root, tasks, t)
	if err != nil {
		return nil, err
	}
	return unionSorted(t.State.Touched, out), nil
}
