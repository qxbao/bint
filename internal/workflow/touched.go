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
		if t.State.Landed != nil && t.State.Origin != "" {
			// Đã đưa về và worktree đã gỡ: về cây gốc (các lệnh đọc trạng thái dùng bản lưu của Landed).
			return t.State.Origin, nil
		}
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

// commitTree tạo một commit trỏ vào cây tree, cha là HEAD của root (không có cha nếu repository chưa có
// commit); không đụng nhánh nào. Trả mã commit.
func commitTree(ctx context.Context, root, tree, msg string) (string, error) {
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
		return "", fmt.Errorf("git commit-tree: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// pinTree tạo một commit rỗng-cha-HEAD trỏ vào cây tree và gắn ref để git gc không dọn cây.
func pinTree(ctx context.Context, root, tree, ref, msg string) error {
	commit, err := commitTree(ctx, root, tree, msg)
	if err != nil {
		return err
	}
	_, err = gitrepo.Run(ctx, root, "update-ref", ref, commit)
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

// fileOwner là một tác vụ khác cùng worktree có tệp trong Touched của nó; Done khi tác vụ đã kết thúc.
type fileOwner struct {
	ID   string
	Done bool
}

// owners cho mỗi tệp trong files (không thuộc Touched của t) liệt kê các tác vụ KHÁC cùng worktree có tệp đó
// trong Touched: tác vụ đang chạy, và tác vụ đã kết thúc SAU khi t được lập (kết thúc trước đó thì thay đổi của nó
// đã nằm trong BaseTree của t, không thể là nguồn của thay đổi đang chờ duyệt — review .3 của tác vụ 01M7826Q…).
// Đang chạy đứng trước, rồi theo ID giảm dần (mới trước). Dùng để checkScope nói rõ tệp cần duyệt là của ai thay
// vì khẳng định chính b-feat vừa sửa (phát hiện .2 của tác vụ 01M774RD…).
func (r *Runner) owners(t *task.Task, files []string) map[string][]fileOwner {
	all, _ := r.Tasks.List()
	out := map[string][]fileOwner{}
	for _, o := range all {
		if o.State.ID == t.State.ID || o.State.Worktree == "" || !samePath(o.State.Worktree, r.Root) {
			continue
		}
		for _, f := range files {
			if slices.Contains(o.State.Touched, f) && !slices.Contains(t.State.Touched, f) {
				done := task.Terminal(o.State.Status)
				if done && endedBefore(o.State.UpdatedAt, t.State.CreatedAt) {
					continue
				}
				out[f] = append(out[f], fileOwner{ID: o.State.ID, Done: done})
			}
		}
	}
	for f := range out {
		sort.Slice(out[f], func(i, j int) bool {
			a, b := out[f][i], out[f][j]
			if a.Done != b.Done {
				return !a.Done
			}
			return a.ID > b.ID
		})
	}
	return out
}

// maxOwnersPerFile giới hạn số tác vụ nêu cho mỗi tệp trong câu hỏi/lý do blocked; phần còn lại gộp thành
// "và N tác vụ khác" để tệp hay sửa (docs/…) không làm lý do dài hàng chục dòng.
const maxOwnersPerFile = 3

// endedBefore: thời điểm kết thúc ended (UpdatedAt của tác vụ đã kết thúc) sớm hơn created. Không đọc được thì
// coi là không (giữ ghi chú, bảo thủ).
func endedBefore(ended, created string) bool {
	e, err1 := time.Parse(time.RFC3339Nano, ended)
	c, err2 := time.Parse(time.RFC3339Nano, created)
	return err1 == nil && err2 == nil && e.Before(c)
}

// outsideFeat liệt kê tệp đổi giữa BaseTree và cây hiện tại mà không được Touched của tác vụ nào miễn
// cho t: tức là việc ngoài b-feat do Runner chạy — sửa tay bằng agent hay người, ở bất kỳ trạng thái nào
// của tác vụ. Chúng được tính vào phạm vi của t để không lọt kiểm tra đường dẫn bảo vệ/deny.
// Luật miễn trừ (phát hiện .7, mục 12.3/12.5 của isolation-subagent-plan):
//   - Touched của chính t luôn được miễn (đã là một phần của t).
//   - Touched của tác vụ KHÁC chỉ miễn khi tác vụ đó chưa kết thúc (done/cancelled thì thay đổi của nó
//     không còn được ai theo dõi) và tệp không phải đường dẫn bảo vệ/deny: sửa tay một tệp bảo vệ mà tác
//     vụ khác từng sửa không được lọt cổng done của t.
//
// Tệp có sẵn chưa commit lúc BaseTree không đổi thì không nằm ở đây.
// Giới hạn: thay đổi tay của người dùng ở cùng worktree cũng bị quy cho t (bảo thủ: chỉ hỏi duyệt);
// tệp của tác vụ đã kết thúc còn khác BaseTree của t cũng bị quy cho t.
func outsideFeat(ctx context.Context, root string, tasks task.Store, t *task.Task, protected []string) ([]string, error) {
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
	own := map[string]bool{}
	for _, f := range t.State.Touched {
		own[f] = true
	}
	others := map[string]bool{}
	all, _ := tasks.List()
	for _, o := range all {
		if o.State.ID == t.State.ID || task.Terminal(o.State.Status) || o.State.Worktree == "" || !samePath(o.State.Worktree, root) {
			continue
		}
		for _, f := range o.State.Touched {
			others[f] = true
		}
	}
	// Tệp mà tác vụ KHÁC đã đưa về chính cây này (task land) và chưa bị sửa tiếp: không quy cho t.
	landed := map[string]string{} // tệp → mã cây lúc đưa về
	for _, o := range all {
		if o.State.ID == t.State.ID || o.State.Landed == nil || o.State.Origin == "" || !samePath(o.State.Origin, root) ||
			o.State.Landed.Tree == "" {
			continue
		}
		for _, f := range o.State.Landed.Files {
			landed[f] = o.State.Landed.Tree
		}
	}
	var out []string
	for _, f := range changed {
		if own[f] {
			continue
		}
		if others[f] && len(task.CheckScope([]string{f}, t.Ticket, protected).NeedsApproval()) == 0 {
			continue
		}
		if tree, ok := landed[f]; ok && len(task.CheckScope([]string{f}, t.Ticket, protected).NeedsApproval()) == 0 &&
			sameBlob(ctx, root, tree, cur, f) {
			continue
		}
		out = append(out, f)
	}
	return out, nil
}

// sameBlob cho biết tệp rel có cùng nội dung ở hai cây (cùng vắng mặt cũng là bằng nhau).
func sameBlob(ctx context.Context, root, treeA, treeB, rel string) bool {
	blob := func(tree string) string {
		out, err := gitrepo.Run(ctx, root, "--literal-pathspecs", "rev-parse", "--verify", "-q", tree+":"+rel)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(out)
	}
	return blob(treeA) == blob(treeB)
}

// ScopeFiles là tập tệp dùng cho kiểm tra phạm vi và `bint task diff` của tác vụ t trên root: Touched
// hợp với thay đổi ngoài b-feat (outsideFeat) nếu có theo dõi, ngược lại toàn bộ thay đổi so với Base
// (phiếu cũ). protected là limits.protected_paths. Chụp cây hiện tại (chỉ thêm đối tượng Git, không đụng cây làm việc).
func ScopeFiles(ctx context.Context, root string, tasks task.Store, t *task.Task, protected []string) ([]string, error) {
	if !t.State.TracksTouched() {
		return task.ChangedFiles(ctx, root, t.State.Base)
	}
	out, err := outsideFeat(ctx, root, tasks, t, protected)
	if err != nil {
		return nil, err
	}
	return unionSorted(t.State.Touched, out), nil
}

// PathBatches chia danh sách đường dẫn thành các lô có tổng độ dài (kèm dấu cách) không quá limit.
func PathBatches(paths []string, limit int) [][]string {
	var out [][]string
	var cur []string
	n := 0
	for _, p := range paths {
		if len(cur) > 0 && n+len(p)+1 > limit {
			out, cur, n = append(out, cur), nil, 0
		}
		cur, n = append(cur, p), n+len(p)+1
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}
