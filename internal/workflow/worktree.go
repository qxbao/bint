package workflow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/eval"
	"bint/internal/flow"
	"bint/internal/gitrepo"
	"bint/internal/pathglob"
	"bint/internal/pathutil"
	"bint/internal/state"
	"bint/internal/task"
)

// Worktree riêng của tác vụ (docs/isolation-subagent-plan.md mục 5): chọn chế độ lúc lập phiếu, tạo worktree
// lười ở lượt `bint run` đầu tiên. Đưa thay đổi về cây gốc và dọn dẹp nằm ở land.go.

// IsolationChoice là chế độ cô lập được chọn lúc lập phiếu.
type IsolationChoice struct {
	Mode   string   // task.IsolationShared | task.IsolationWorktree
	Reason string   // task.IsolationReason*
	Tasks  []string // tác vụ gây ra concurrent_task
}

// NormalizeIsolationFlag chuẩn hoá giá trị cờ: ""/auto → ""; shared|worktree giữ nguyên.
func NormalizeIsolationFlag(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", config.IsolationAuto:
		return "", nil
	case task.IsolationShared:
		return task.IsolationShared, nil
	case task.IsolationWorktree:
		return task.IsolationWorktree, nil
	}
	return "", fmt.Errorf("chế độ cô lập %q không hợp lệ (shared | worktree)", s)
}

// chooseIsolation chọn chế độ cô lập cho phiếu mới lập ở e.Root. flag là lựa chọn tường minh của người dùng
// (--worktree/--shared), thắng cấu hình. Quy tắc auto (mục 5.1): có tác vụ khác chưa done/cancelled mà State.Worktree
// là cây này và đang ở chế độ shared (phiếu cũ không ghi chế độ = shared) ⇒ worktree; ngược lại shared. Tác vụ
// bỏ dở (created/failed/blocked/interrupted) cũng tính vì có thể còn thay đổi dở trên cây.
func (e *Env) chooseIsolation(flag string) (IsolationChoice, error) {
	flag, err := NormalizeIsolationFlag(flag)
	if err != nil {
		return IsolationChoice{}, err
	}
	if flag != "" {
		return IsolationChoice{Mode: flag, Reason: task.IsolationReasonUser}, nil
	}
	switch e.Cfg.Workflow.Isolation {
	case config.IsolationShared:
		return IsolationChoice{Mode: task.IsolationShared, Reason: task.IsolationReasonConfig}, nil
	case config.IsolationWorktree:
		return IsolationChoice{Mode: task.IsolationWorktree, Reason: task.IsolationReasonConfig}, nil
	}
	if ids := e.sharedTasksOn(e.Root, ""); len(ids) > 0 {
		return IsolationChoice{Mode: task.IsolationWorktree, Reason: task.IsolationReasonConcurrent, Tasks: ids}, nil
	}
	return IsolationChoice{Mode: task.IsolationShared, Reason: task.IsolationReasonDefault}, nil
}

// sharedTasksOn liệt kê (đã sắp xếp) ID tác vụ chưa kết thúc đang ở chế độ shared trên cây root, trừ tác vụ except
// và phiếu chỉ lập kế hoạch (planOnlyTask: không ghi cây). Dùng chung cho chooseIsolation (phiếu mới) và
// sharedWriteConflicts (phiếu sắp ghi) để hai nơi cùng một điều kiện.
func (e *Env) sharedTasksOn(root, except string) []string {
	all, _ := e.Tasks.List()
	var ids []string
	for _, o := range all {
		if o.State.ID == except || task.Terminal(o.State.Status) || o.State.Worktree == "" || !samePath(o.State.Worktree, root) ||
			o.State.Isolation() != task.IsolationShared || e.planOnlyTask(o) {
			continue
		}
		ids = append(ids, o.State.ID)
	}
	sort.Strings(ids)
	return ids
}

// sharedWriteConflicts trả ID các tác vụ khiến phiếu t chưa được vào bước ghi đầu tiên (docs: phát hiện .8). Phiếu
// b-req ở cây chung không bị tính khi chọn chế độ cô lập (chooseIsolation bỏ phiếu chỉ lập kế hoạch), nhưng Run đổi
// workflow của nó sang b-do/b-feat khi chạy tiếp: tới lúc đó phải kiểm lại, nếu không hai tác vụ cùng ghi cây chung.
//
// Chỉ áp cho phiếu shared do auto chọn (IsolationReasonDefault) chưa từng in_progress: --shared của người và cấu hình
// workflow.isolation là chấp nhận có chủ đích; phiếu cũ không có Classification giữ hành vi cũ. Tác vụ khác đang
// blocked mà chưa từng in_progress (bị chính cổng này giữ lại) chưa ghi gì nên không tính, nếu không hai phiếu
// chặn nhau mãi.
func (e *Env) sharedWriteConflicts(t *task.Task) []string {
	st := &t.State
	if st.Isolation() != task.IsolationShared || st.Worktree == "" || st.EverInProgress() ||
		st.Classification == nil || st.Classification.IsolationReason != task.IsolationReasonDefault {
		return nil
	}
	var out []string
	for _, id := range e.sharedTasksOn(st.Worktree, st.ID) {
		if o, err := e.Tasks.Load(id); err == nil && o.State.Status == task.StatusBlocked && !o.State.EverInProgress() {
			continue
		}
		out = append(out, id)
	}
	return out
}

// planOnlyTask: phiếu chỉ lập kế hoạch đã xong bước lập: có workflow mà flow không có bước ghi (b-req, hoặc flow tuỳ
// biến không có bước implement), đang planned, chưa từng vào in_progress và không có thay đổi gán cho nó (Touched,
// PendingPaths). Phiếu như vậy không ghi cây nên không tranh cây với phiếu khác (chooseIsolation) và không có mã
// để kiểm chứng hay duyệt (cổng done). Flow không nạp được thì coi như không chỉ lập kế hoạch (giữ cổng nghiêm hơn).
func (e *Env) planOnlyTask(t *task.Task) bool {
	st := &t.State
	if st.Status != task.StatusPlanned || st.Workflow == "" || st.EverInProgress() || len(st.Touched) > 0 || len(st.PendingPaths) > 0 {
		return false
	}
	fd, err := e.resolveFlow(st.Workflow)
	return err == nil && !fd.HasKind(flow.KindImplement)
}

// planGate là kết quả planGateFor: phiếu có được hưởng cổng done của phiếu chỉ lập kế hoạch hay không.
type planGate struct {
	// PlanOnly: planOnlyTask và (ở worktree) diff so với Base rỗng hoặc chỉ gồm mục tri thức candidate mới.
	PlanOnly bool
	// Stray: phiếu chỉ lập kế hoạch (planOnlyTask) nhưng worktree có thay đổi khác; Err: không tính được diff
	// (coi như có thay đổi, cổng nghiêm hơn).
	Stray []string
	Err   error
}

// planGateFor quyết định cổng done/land của phiếu t. Ở cây chung chỉ xét trạng thái (planOnlyTask). Ở worktree
// còn đòi diff so với State.Base chỉ gồm mục tri thức candidate mới (cùng quy tắc miễn trừ của bước chỉ đọc):
// thay đổi mã do orchestrator/agent sửa ngoài bước không được đưa về cây gốc mà thiếu kiểm chứng.
func (e *Env) planGateFor(ctx context.Context, t *task.Task) planGate {
	if !e.planOnlyTask(t) {
		return planGate{}
	}
	st := &t.State
	if st.Isolation() != task.IsolationWorktree || st.Origin == "" || st.Landed != nil {
		// Chưa có worktree, hoặc đã đưa về (Land đã chạy cổng này; worktree có thể đã gỡ).
		return planGate{PlanOnly: true}
	}
	root, err := e.RootFor(t)
	var stray []string
	if err == nil {
		var snap string
		if snap, err = WorktreeSnapshot(ctx, root); err == nil {
			stray, err = nonCandidateChanges(ctx, root, st.Base, snap)
		}
	}
	if err != nil {
		return planGate{Err: err}
	}
	return planGate{PlanOnly: len(stray) == 0, Stray: stray}
}

// worktreesDir là thư mục chứa worktree của các tác vụ (<state>/worktrees).
func (e *Env) worktreesDir() string {
	return state.WorktreesDirFor(e.Tasks.Dir)
}

// WorktreePath là đường dẫn worktree của tác vụ id.
func (e *Env) WorktreePath(id string) string { return filepath.Join(e.worktreesDir(), id) }

// BranchFor là tên nhánh của worktree tác vụ id.
func BranchFor(id string) string { return "bint/" + id }

// localManagedFiles là tệp cục bộ do Bint quản lý, thường bị gitignore (phạm vi local của hook, lớp cấu hình
// local): không có trong commit gốc nên phải chép sang worktree, nếu không agent mất hook (spool, guardrail)
// và lớp cấu hình riêng của máy này.
var localManagedFiles = []string{
	".bint/local.yml",
	".claude/settings.local.json",
	".codex/hooks.json",
	".agents/hooks.json",
}

// copyLimit chặn số tệp một mẫu workflow.worktree.copy được chép (mẫu quá rộng như "**").
const copyLimit = 20000

// copyIntoWorktree chép từ origin sang wt các tệp cục bộ do Bint quản lý và tệp khớp patterns. Chỉ chép tệp
// chưa có ở wt (tệp đã theo dõi có sẵn trong commit gốc, đúng nội dung đang sửa dở ở cây gốc). Bỏ qua liên kết
// tượng trưng và tệp đặc biệt. Trả danh sách tệp đã chép và ghi chú.
func copyIntoWorktree(origin, wt string, patterns []string) (copied, notes []string, err error) {
	seen := map[string]bool{}
	add := func(rel string) error {
		rel = path.Clean(filepath.ToSlash(rel))
		if seen[rel] || rel == "." || strings.HasPrefix(rel, "../") || rel == ".git" || strings.HasPrefix(rel, ".git/") {
			return nil
		}
		seen[rel] = true
		src, dst := filepath.Join(origin, filepath.FromSlash(rel)), filepath.Join(wt, filepath.FromSlash(rel))
		fi, lerr := os.Lstat(src)
		if lerr != nil {
			return nil
		}
		if !fi.Mode().IsRegular() {
			if fi.Mode()&fs.ModeSymlink != 0 {
				notes = append(notes, "bỏ qua liên kết tượng trưng khi chép: "+rel)
			}
			return nil
		}
		if _, serr := os.Lstat(dst); serr == nil {
			return nil
		}
		if err := copyFile(src, dst, fi.Mode().Perm()); err != nil {
			return fmt.Errorf("chép %s: %w", rel, err)
		}
		copied = append(copied, rel)
		return nil
	}
	for _, rel := range localManagedFiles {
		if err := add(rel); err != nil {
			return copied, notes, err
		}
	}
	for _, pat := range patterns {
		pat = path.Clean(strings.ReplaceAll(strings.TrimSpace(pat), `\`, "/"))
		if pat == "." || pat == "" {
			continue
		}
		if !pathglob.HasMeta(pat) {
			// Tên cụ thể: tệp, hoặc thư mục (chép cả cây).
			fi, serr := os.Lstat(filepath.Join(origin, filepath.FromSlash(pat)))
			if serr != nil {
				continue
			}
			if !fi.IsDir() {
				if err := add(pat); err != nil {
					return copied, notes, err
				}
				continue
			}
			n := 0
			werr := filepath.WalkDir(filepath.Join(origin, filepath.FromSlash(pat)), func(p string, d fs.DirEntry, werr error) error {
				if werr != nil || d.IsDir() {
					return nil
				}
				if n++; n > copyLimit {
					return errTooManyCopies
				}
				rel, _ := filepath.Rel(origin, p)
				return add(rel)
			})
			if err := copyWalkErr(werr, pat, &notes); err != nil {
				return copied, notes, err
			}
			continue
		}
		// Mẫu glob: duyệt từ tiền tố cố định của mẫu; không có "**" thì chỉ xuống đúng số cấp của mẫu.
		segs := strings.Split(pat, "/")
		prefix := []string{}
		for _, sg := range segs[:len(segs)-1] {
			if pathglob.HasMeta(sg) {
				break
			}
			prefix = append(prefix, sg)
		}
		deep := strings.Contains(pat, "**")
		startDir := filepath.Join(append([]string{origin}, prefix...)...)
		n := 0
		werr := filepath.WalkDir(startDir, func(p string, d fs.DirEntry, werr error) error {
			if werr != nil {
				return nil
			}
			rel, _ := filepath.Rel(origin, p)
			rel = filepath.ToSlash(rel)
			if d.IsDir() {
				if rel == ".git" || (!deep && strings.Count(rel, "/")+1 >= len(segs)) {
					return filepath.SkipDir
				}
				return nil
			}
			if !pathglob.Compile(pat).Match(rel) {
				return nil
			}
			if n++; n > copyLimit {
				return errTooManyCopies
			}
			return add(rel)
		})
		if err := copyWalkErr(werr, pat, &notes); err != nil {
			return copied, notes, err
		}
	}
	sort.Strings(copied)
	return copied, notes, nil
}

var errTooManyCopies = errors.New("quá nhiều tệp")

func copyWalkErr(err error, pat string, notes *[]string) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errTooManyCopies):
		*notes = append(*notes, fmt.Sprintf("workflow.worktree.copy: mẫu %q khớp quá %d tệp, chỉ chép phần đầu", pat, copyLimit))
		return nil
	}
	return err
}

func copyFile(src, dst string, perm fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm|0o200)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// ensureWorktree tạo worktree riêng cho tác vụ ở chế độ worktree chưa có worktree (mục 5.2), rồi đổi Root của
// Runner sang đó. Gọi sau khi đã nhận quyền điều phối, với r.Root = cây gốc. Trả false nếu workflow phải dừng
// (đã đặt trạng thái failed/blocked/interrupted).
//
// Các bước: ảnh chụp cây gốc dưới khoá chia sẻ (chờ b-feat đang chạy của tác vụ khác xong để base không chứa tệp
// dở dang) → commit "bint base <id>" cha HEAD → refs/bint/tasks/<id>/base → git worktree add -b bint/<id> →
// chép tệp cục bộ → setup_command. Lỗi ở bất kỳ bước nào: dọn phần đã tạo rồi failed (resume tạo lại từ đầu).
func (r *Runner) ensureWorktree(ctx context.Context, rs *runState) bool {
	st := &rs.t.State
	if st.Isolation() != task.IsolationWorktree || st.Origin != "" || st.Landed != nil {
		return true
	}
	id, origin := st.ID, r.Root
	wt := r.WorktreePath(id)
	// Lệnh setup chạy theo quyền người dùng: chưa được người xác nhận (băm đúng lệnh sắp chạy) thì dừng ở đây,
	// trước khi dọn/khoá/chụp cây và tạo worktree. Lệnh chạy ở dưới là chính r.Cfg đã băm ở đây (cùng lượt) nên
	// không có khoảng hở giữa lúc kiểm và lúc chạy.
	if ss := r.SetupStatus(rs.t); ss.Pending() {
		if r.Cfg.Security.RequireApprovalForCommands {
			r.block(rs, fmt.Sprintf("workflow.worktree.setup_command chưa được người xác nhận: %s — người xem lệnh rồi duyệt: bint task approve %s --setup-sha %s, sau đó bint run resume %s",
				ss.Command, id, ss.SHA256[:MinSetupSHAPrefix], id))
			return false
		}
		rs.res.Notes = append(rs.res.Notes, "workflow.worktree.setup_command chạy không cần người xác nhận (security.require_approval_for_commands tắt): "+ss.Command)
	}
	fail := func(format string, a ...any) bool {
		r.cleanupWorktree(context.WithoutCancel(ctx), id, origin, wt)
		if ctx.Err() != nil {
			r.interrupt(rs, "người dùng huỷ khi tạo worktree")
			return false
		}
		r.fail(rs, "worktree", fmt.Sprintf(format, a...))
		return false
	}
	// Dọn dấu vết của lần tạo dở trước đó (Origin còn rỗng nghĩa là chưa từng tạo xong).
	r.cleanupWorktree(ctx, id, origin, wt)

	lock, ok := r.lockStep(ctx, rs, "worktree", LockShared)
	if !ok {
		return false
	}
	snap, err := WorktreeSnapshot(ctx, origin)
	var base string
	if err == nil {
		base, err = commitTree(ctx, origin, snap, "bint base "+id)
	}
	if err == nil {
		_, err = gitrepo.Run(ctx, origin, "update-ref", baseRef(id), base)
	}
	lock.Release()
	if err != nil {
		return fail("chụp cây gốc để tạo worktree: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(wt), 0o700); err != nil {
		return fail("tạo thư mục worktree: %v", err)
	}
	if err := gitrepo.WorktreeAdd(ctx, origin, wt, BranchFor(id), base); err != nil {
		return fail("git worktree add: %v", err)
	}
	copied, notes, err := copyIntoWorktree(origin, wt, r.Cfg.Workflow.Worktree.Copy)
	if err != nil {
		return fail("chép tệp cục bộ sang worktree: %v", err)
	}
	rs.res.Notes = append(rs.res.Notes, notes...)
	if cmd := strings.TrimSpace(r.Cfg.Workflow.Worktree.SetupCommand); cmd != "" {
		log := rs.t.LogPath("worktree-setup.log")
		os.MkdirAll(filepath.Dir(log), 0o700)
		r.logf("worktree: chạy setup_command: %s", cmd)
		env := append(os.Environ(), EnvTaskID+"="+id, "BINT_ORIGIN_ROOT="+origin)
		res := eval.RunShell(ctx, wt, cmd, time.Duration(r.Cfg.Workflow.TimeoutMinutes)*time.Minute, log, env)
		switch {
		case ctx.Err() != nil:
			return fail("huỷ")
		case res.TimedOut:
			return fail("workflow.worktree.setup_command quá workflow.timeout_minutes (%d phút); nhật ký: %s", r.Cfg.Workflow.TimeoutMinutes, log)
		case !res.OK():
			return fail("workflow.worktree.setup_command lỗi (mã %d): %s; nhật ký: %s", res.ExitCode, cmd, log)
		}
	}
	nt, err := r.Tasks.Update(id, r.now(), func(t *task.Task) error {
		t.State.Origin, t.State.Worktree, t.State.Branch, t.State.Base = origin, wt, BranchFor(id), base
		return nil
	})
	if err != nil {
		return fail("ghi thông tin worktree vào phiếu: %v", err)
	}
	rs.t = nt
	if len(copied) > 0 {
		r.logf("worktree: đã chép %d tệp cục bộ", len(copied))
	}
	r.logf("Tác vụ chạy trong worktree riêng %s (nhánh %s, từ cây gốc %s)", wt, BranchFor(id), origin)
	cp := *r.Env
	cp.Root = wt
	r.Env = &cp
	return true
}

// cleanupWorktree gỡ mọi thứ Bint đã tạo cho tác vụ id: worktree, nhánh bint/<id>, ref base, tệp khoá của
// worktree. Lỗi không dừng việc dọn; trả các ghi chú lỗi (để người dọn tay). Idempotent.
func (e *Env) cleanupWorktree(ctx context.Context, id, origin, wt string) []string {
	var notes []string
	manual := func(what string, err error) {
		notes = append(notes, fmt.Sprintf("%s lỗi: %v", what, err))
	}
	if _, err := os.Stat(wt); err == nil || !errors.Is(err, fs.ErrNotExist) {
		if err := gitrepo.WorktreeRemove(ctx, origin, wt); err != nil {
			// Git từ chối (ví dụ thư mục không còn là worktree): xoá thư mục nếu nó nằm đúng chỗ của Bint.
			if isUnder(e.worktreesDir(), wt) {
				if rerr := os.RemoveAll(wt); rerr != nil {
					manual("gỡ worktree "+wt, rerr)
				}
			} else {
				manual("gỡ worktree "+wt, err)
			}
		}
	}
	gitrepo.Run(ctx, origin, "worktree", "prune")
	if err := gitrepo.DeleteBranch(ctx, origin, BranchFor(id)); err != nil {
		manual("xoá nhánh "+BranchFor(id), err)
	}
	if err := deleteTaskRefs(ctx, origin, id); err != nil {
		manual("xoá ref giữ cây", err)
	}
	if err := e.removeLockFiles(wt); err != nil {
		manual("xoá tệp khoá của worktree", err)
	}
	if len(notes) > 0 {
		notes = append(notes, fmt.Sprintf("dọn tay: git -C %q worktree remove --force %q; git -C %q branch -D %s; git -C %q update-ref -d %s; git -C %q update-ref -d %s",
			origin, wt, origin, BranchFor(id), origin, baseRef(id), origin, baseTreeRef(id)))
	}
	return notes
}

// removeLockFiles xoá tệp khoá của worktree wt khi lấy được khoá độc quyền không chờ (không bước nào đang
// chạy trên đó). Xoá tệp khoá khi còn người giữ sẽ cho người đến sau lấy khoá song song (isolation.go), nên
// bận thì để lại cho doctor.
func (e *Env) removeLockFiles(wt string) error {
	p := lockPath(e.Tasks.Dir, wt)
	if _, err := os.Stat(p); errors.Is(err, fs.ErrNotExist) {
		os.RemoveAll(p + ".d")
		removeLeaseFiles(p)
		return nil
	}
	l, err := AcquireLock(e.Tasks.Dir, wt, "dọn worktree", LockExclusive)
	if err != nil {
		return err
	}
	defer l.Release()
	os.RemoveAll(p + ".d")
	removeLeaseFiles(p) // hợp đồng còn hạn đã chặn AcquireLock ở trên; tới đây chỉ còn hợp đồng hết hạn
	return removeHeldLockFile(l, p)
}

// isUnder cho biết p nằm dưới dir.
func isUnder(dir, p string) bool {
	var err error
	dir, err = pathutil.Canonical(dir)
	if err != nil {
		return false
	}
	p, err = pathutil.Canonical(p)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(p))
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(filepath.ToSlash(rel), "../") && !filepath.IsAbs(rel)
}
