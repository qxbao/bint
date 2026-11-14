package workflow

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"bint/internal/gitrepo"
	"bint/internal/spool"
	"bint/internal/state"
	"bint/internal/task"
)

// Đưa thay đổi của tác vụ worktree về cây gốc (docs/isolation-subagent-plan.md mục 5.3).
//
// Thay đổi đi bằng bản vá nhị phân từ commit Base tới ảnh chụp worktree, giới hạn trong tệp của tác vụ
// (ScopeFiles), áp lên cây gốc KHÔNG qua chỉ mục: thay đổi hiện ra dưới dạng chưa commit như chế độ shared,
// không đụng HEAD hay chỉ mục của người dùng. Không áp sạch thì cây gốc không đổi.

// LandOptions điều khiển Env.Land.
type LandOptions struct {
	// ThreeWay: tệp không áp sạch được trộn bằng `git merge-file` và để lại dấu xung đột trong tệp ở cây gốc.
	// Chỉ người (ErrAgent).
	ThreeWay bool
	// Keep giữ worktree sau khi đưa về (để xem lại).
	Keep bool
	// Orchestrator: do chính Runner gọi khi finalize (bỏ kiểm "tác vụ đang được bint run khác điều phối").
	Orchestrator bool
}

// LandResult là kết quả đưa về.
type LandResult struct {
	Task *task.Task
	// Files là tệp của tác vụ đã đưa về; Conflicts (--3way) là tệp còn dấu xung đột hoặc chưa trộn được;
	// Unmerged ⊂ Conflicts là tệp KHÔNG trộn được (cây gốc giữ nguyên, không có dấu xung đột): bản của tác vụ
	// chỉ còn ở worktree nên worktree được giữ lại.
	Files     []string
	Conflicts []string
	Unmerged  []string
	// Removed: worktree đã gỡ.
	Removed bool
	Notes   []string
}

// LandConflictError: bản vá không áp sạch lên cây gốc. Cây gốc không bị đụng.
type LandConflictError struct {
	TaskID string
	Files  []string
	Detail string
}

func (e *LandConflictError) Error() string {
	return "xung đột khi đưa về: " + strings.Join(e.Files, ", ") +
		" (cây gốc không bị đụng; sửa các tệp đó ở cây gốc cho khớp rồi `bint task land " + e.TaskID +
		"`, hoặc người chạy `bint task land " + e.TaskID + " --3way` để lại dấu xung đột trong tệp cần gỡ)"
}

// ErrDiscardRequired: tác vụ còn thay đổi chưa đưa về, cần --discard để huỷ.
type ErrDiscardRequired struct {
	TaskID string
	Files  []string
}

func (e *ErrDiscardRequired) Error() string {
	list := strings.Join(e.Files, ", ")
	if len(e.Files) > 8 {
		list = strings.Join(e.Files[:8], ", ") + fmt.Sprintf(" … (+%d)", len(e.Files)-8)
	}
	return fmt.Sprintf("worktree của tác vụ còn %d tệp thay đổi chưa đưa về cây gốc (%s): huỷ sẽ mất chúng. "+
		"Đưa về bằng `bint task land %s`, hoặc người dùng `bint task cancel %s --discard` để bỏ", len(e.Files), list, e.TaskID, e.TaskID)
}

// doneBlockersFor gom điều chặn done của tác vụ t: bằng chứng (task.DoneBlockers), thay đổi chạm đường dẫn
// bảo vệ/deny chưa duyệt. in/inErr là kết quả DoneInputDetailed; pg là planGateFor tính trước (ngoài khoá phiếu).
func (e *Env) doneBlockersFor(ctx context.Context, t *task.Task, in task.DoneInput, inErr error, pg planGate) []string {
	if pg.PlanOnly && e.planOnlyTask(t) { // kiểm lại: trạng thái phiếu có thể đã đổi từ lúc tính pg
		// Phiếu chỉ lập kế hoạch: không có mã để kiểm chứng. Ở cây chung UnapprovedChanges (diff so với Base) sẽ gán
		// nhầm commit của người khác cho nó nên bỏ qua. Ở worktree riêng không có ai khác ghi: thay đổi chạm đường
		// dẫn bảo vệ/deny/ngoài phạm vi (do orchestrator hay agent sửa ngoài bước) vẫn cần người duyệt, nếu không
		// Land sẽ đưa chúng về cây gốc không qua cổng nào.
		var blockers []string
		if t.State.NeedsLand() && !in.IgnoreLanding {
			blockers = append(blockers, "chưa đưa thay đổi về cây gốc "+t.State.Origin+" (bint task land "+t.State.ID+")")
		}
		if t.State.Isolation() == task.IsolationWorktree {
			pend, err := e.UnapprovedChanges(ctx, t)
			switch {
			case err != nil:
				blockers = append(blockers, "không xác định được tệp đã sửa: "+err.Error())
			case len(pend) > 0:
				blockers = append(blockers, "thay đổi chạm đường dẫn bảo vệ/deny chưa được duyệt: "+strings.Join(pend, ", ")+" (bint task approve)")
			}
		}
		return blockers
	}
	in.VerifyConfirmed = e.VerifyConfirmed(t)
	blockers := task.DoneBlockers(t, in)
	if e.planOnlyTask(t) { // phiếu chỉ lập kế hoạch ở worktree nhưng có thay đổi khác: cổng đầy đủ + lý do rõ nghĩa
		blockers = append(blockers, strayBlocker(t.State.ID, pg))
	}
	// Tính lại từ diff hiện tại, không tin PendingPaths (chỉ Runner điền).
	pend, err := e.UnapprovedChanges(ctx, t)
	switch {
	case inErr != nil:
		blockers = append(blockers, inErr.Error())
	case err != nil:
		blockers = append(blockers, "không xác định được tệp đã sửa: "+err.Error())
	case len(pend) > 0:
		blockers = append(blockers, "thay đổi chạm đường dẫn bảo vệ/deny chưa được duyệt: "+strings.Join(pend, ", ")+" (bint task approve)")
	}
	return blockers
}

// strayBlocker nêu vì sao phiếu chỉ lập kế hoạch không được hưởng cổng nhẹ: worktree có thay đổi ngoài mục tri
// thức candidate (hoặc không tính được diff).
func strayBlocker(id string, pg planGate) string {
	what := "worktree có thay đổi ngoài mục tri thức candidate: " + strings.Join(pg.Stray, ", ")
	if pg.Err != nil {
		what = "không xác định được thay đổi của worktree: " + pg.Err.Error()
	}
	return what + fmt.Sprintf("; phiếu chỉ lập kế hoạch không đưa mã về cây gốc: chạy `bint run b-feat --task %s` (hoặc b-do) "+
		"để triển khai và kiểm chứng, hoặc người huỷ bằng `bint task cancel %s --discard`", id, id)
}

// Land đưa thay đổi của tác vụ worktree về cây gốc (State.Origin). Chỉ đưa về khi điều kiện done (trừ chính "đã
// đưa về") đã đủ, kiểm ở worktree: sau khi gỡ worktree không còn chỗ chạy kiểm chứng. Không đổi trạng thái tác
// vụ (finalize của b-do hoặc `bint task status <id> done` làm bước đó).
func (e *Env) Land(ctx context.Context, ref string, o LandOptions) (*LandResult, error) {
	if o.ThreeWay && !e.Human {
		return nil, fmt.Errorf("%w: bint task land --3way", ErrAgent)
	}
	t, err := e.Tasks.Load(ref)
	if err != nil {
		return nil, err
	}
	st := &t.State
	switch {
	case st.Isolation() != task.IsolationWorktree:
		return nil, fmt.Errorf("tác vụ %s chạy thẳng trên cây của nó (chế độ %s): không có gì để đưa về", st.ID, st.Isolation())
	case st.Origin == "":
		return nil, fmt.Errorf("tác vụ %s chưa có worktree (chưa chạy bint run lần nào): không có gì để đưa về", st.ID)
	case st.Landed != nil:
		return nil, fmt.Errorf("tác vụ %s đã đưa về cây gốc lúc %s", st.ID, st.Landed.At)
	case task.Terminal(st.Status):
		return nil, fmt.Errorf("tác vụ %s đã %s", st.ID, st.Status)
	case !o.Orchestrator && t.OwnedByOther():
		return nil, fmt.Errorf("tác vụ đang được `bint run` điều phối (lượt %s): chờ lượt đó xong hoặc dùng `bint run resume` để Bint tự đưa về", st.Runner.RunID)
	}
	wt, err := e.RootFor(t)
	if err != nil {
		return nil, err
	}
	origin := st.Origin

	pg := e.planGateFor(ctx, t)
	in, inErr := e.DoneInputDetailed(ctx, t)
	in.IgnoreLanding = true
	if blockers := e.doneBlockersFor(ctx, t, in, inErr, pg); len(blockers) > 0 {
		return nil, &NotDoneError{Blockers: blockers}
	}

	scope, err := ScopeFiles(ctx, wt, e.Tasks, t, e.Cfg.Limits.ProtectedPaths)
	if err != nil {
		return nil, err
	}
	snap, err := WorktreeSnapshot(ctx, wt)
	if err != nil {
		return nil, fmt.Errorf("chụp worktree: %w", err)
	}
	changed, err := DiffPaths(ctx, wt, st.Base, snap)
	if err != nil {
		return nil, err
	}
	inScope := map[string]bool{}
	for _, f := range scope {
		inScope[f] = true
	}
	var files []string
	for _, f := range changed {
		if inScope[f] {
			files = append(files, f)
		}
	}
	sort.Strings(files)

	res := &LandResult{Files: files}
	fp := task.WorktreeFingerprint(ctx, wt)

	// Khoá độc quyền của cây gốc: không chen giữa hai ảnh chụp của bước chỉ đọc đang chạy ở đó.
	mins := e.Cfg.Workflow.TimeoutMinutes
	lock, err := WaitLock(ctx, e.Tasks.Dir, origin, st.ID+" land", LockExclusive, WaitOptions{
		Timeout: time.Duration(mins) * lockWaitUnit,
		OnWait: func(holders string) {
			if holders == "" {
				holders = "chủ khoá chưa rõ"
			}
			if e.Out != nil {
				fmt.Fprintf(e.Out, "⏳ chờ khoá cây gốc do %s\n", holders)
			}
		},
	})
	if err != nil {
		if errors.Is(err, ErrLocked) {
			return nil, fmt.Errorf("chờ khoá cây gốc quá %d phút: %w", mins, err)
		}
		return nil, err
	}
	var conflicts, unmerged []string
	func() {
		defer lock.Release()
		// Nạp lại dưới khoá: một lượt land khác (tay hoặc finalize của bint run resume) có thể đã đưa về trong lúc
		// chờ khoá; áp lần hai sẽ báo xung đột giả (hoặc áp trùng hunk).
		cur, lerr := e.Tasks.Load(st.ID)
		switch {
		case lerr != nil:
			err = lerr
		case cur.State.Landed != nil:
			err = fmt.Errorf("tác vụ %s đã đưa về cây gốc lúc %s", st.ID, cur.State.Landed.At)
		case task.Terminal(cur.State.Status):
			err = fmt.Errorf("tác vụ %s đã %s", st.ID, cur.State.Status)
		default:
			conflicts, unmerged, err = e.applyLanding(ctx, origin, wt, st.Base, snap, files, o.ThreeWay)
		}
	}()
	if err != nil {
		var ce *LandConflictError
		if errors.As(err, &ce) {
			ce.TaskID = st.ID
		}
		return nil, err
	}
	res.Conflicts, res.Unmerged = conflicts, unmerged
	// Tệp không trộn được: bản của tác vụ chỉ còn trong worktree (tree object của ảnh chụp không có ref nào giữ)
	// nên không gỡ worktree dù không có --keep.
	keep := o.Keep || len(unmerged) > 0
	landing := &task.Landing{At: e.at(), By: e.actor(), Files: files, Conflicts: conflicts, Unmerged: unmerged,
		ThreeWay: o.ThreeWay, Fingerprint: fp, Tree: snap, Kept: keep}
	if len(unmerged) > 0 && !o.Keep {
		landing.Note = "giữ worktree vì không trộn được: " + strings.Join(unmerged, ", ")
		res.Notes = append(res.Notes, fmt.Sprintf("không trộn được %s (cây gốc giữ nguyên): bản của tác vụ còn ở worktree %s "+
			"(lấy bằng `git -C %s show %s:<tệp>`); worktree được giữ lại", strings.Join(unmerged, ", "), wt, shellQuote(wt), snap))
	}
	nt, err := e.Tasks.Update(st.ID, e.now(), func(t *task.Task) error {
		t.State.Landed = landing
		t.State.AwaitingLand = false
		t.State.PendingPaths = nil
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("đã áp thay đổi lên cây gốc nhưng không ghi được vào phiếu: %w", err)
	}
	t = nt
	if !keep {
		if notes := e.cleanupWorktree(ctx, st.ID, origin, wt); len(notes) > 0 {
			res.Notes = append(res.Notes, "gỡ worktree lỗi: "+strings.Join(notes, "; "))
			if nt, err := e.Tasks.Update(st.ID, e.now(), func(t *task.Task) error {
				t.State.Landed.Note = strings.Join(notes, "; ")
				return nil
			}); err == nil {
				t = nt
			}
		} else {
			res.Removed = true
		}
	}
	res.Task = t
	e.emit(t, spool.TaskStatus, map[string]any{"status": t.State.Status, "landed": true, "files": len(files),
		"conflicts": len(conflicts), "three_way": o.ThreeWay, "kept": keep})
	return res, nil
}

// applyLanding áp bản vá Base→snap (giới hạn trong files) lên cây gốc origin, dưới khoá độc quyền của người gọi.
// Trả các tệp còn xung đột (--3way): conflicts gồm tệp có dấu xung đột và tệp không trộn được; unmerged chỉ
// gồm tệp không trộn được. Không áp sạch và không --3way: *LandConflictError, cây gốc không đổi.
func (e *Env) applyLanding(ctx context.Context, origin, wt, base, snap string, files []string, threeWay bool) (conflicts, unmerged []string, err error) {
	if len(files) == 0 {
		return nil, nil, nil
	}
	patch, err := landPatch(ctx, wt, base, snap, files)
	if err != nil {
		return nil, nil, err
	}
	check := func(p string) error { return gitApply(ctx, origin, p, "--check") }
	if err := check(patch); err == nil {
		return nil, nil, gitApply(ctx, origin, patch)
	} else if !threeWay {
		bad := conflictFiles(err.Error(), files)
		return nil, nil, &LandConflictError{Files: bad, Detail: err.Error()}
	} else {
		return e.threeWayLanding(ctx, origin, wt, base, snap, files, conflictFiles(err.Error(), files))
	}
}

// threeWayLanding: áp phần sạch bằng `git apply`, trộn từng tệp xung đột bằng `git merge-file` (ours = tệp ở cây
// gốc, base = blob ở Base, theirs = blob ở ảnh chụp worktree). Không dùng `git apply --3way` vì nó ngầm ghi chỉ
// mục của người dùng. Tệp nhị phân hoặc bị xoá/sửa ở một phía thì giữ nguyên ở cây gốc và được liệt kê.
func (e *Env) threeWayLanding(ctx context.Context, origin, wt, base, snap string, files, bad []string) (conflicts, unmerged []string, err error) {
	badSet := map[string]bool{}
	for _, f := range bad {
		badSet[f] = true
	}
	var clean []string
	for _, f := range files {
		if !badSet[f] {
			clean = append(clean, f)
		}
	}
	if len(clean) > 0 {
		patch, err := landPatch(ctx, wt, base, snap, clean)
		if err != nil {
			return nil, nil, err
		}
		if err := gitApply(ctx, origin, patch, "--check"); err != nil {
			return nil, nil, &LandConflictError{Files: files, Detail: err.Error()}
		}
		if err := gitApply(ctx, origin, patch); err != nil {
			return nil, nil, err
		}
	}
	for _, f := range bad {
		switch mergeOne(ctx, origin, base, snap, f) {
		case mergeMarkers:
			conflicts = append(conflicts, f)
		case mergeSkipped:
			conflicts = append(conflicts, f)
			unmerged = append(unmerged, f)
		}
	}
	return conflicts, unmerged, nil
}

type mergeOutcome int

const (
	mergeClean   mergeOutcome = iota // trộn sạch, không còn dấu xung đột
	mergeMarkers                     // tệp ở cây gốc có dấu xung đột để người gỡ
	mergeSkipped                     // không trộn được: tệp ở cây gốc giữ nguyên
)

// mergeOne trộn một tệp bằng git merge-file: mergeClean khi trộn sạch, mergeMarkers khi để lại dấu xung đột,
// mergeSkipped khi không trộn được (tệp ở cây gốc giữ nguyên).
func mergeOne(ctx context.Context, origin, base, snap, rel string) mergeOutcome {
	ours := filepath.Join(origin, filepath.FromSlash(rel))
	if fi, err := os.Lstat(ours); err != nil || !fi.Mode().IsRegular() {
		return mergeSkipped // người đã xoá tệp ở cây gốc
	}
	theirs, err := gitrepo.Run(ctx, origin, "--literal-pathspecs", "show", snap+":"+rel)
	if err != nil {
		return mergeSkipped // worktree xoá tệp mà cây gốc có sửa
	}
	baseBlob, err := gitrepo.Run(ctx, origin, "--literal-pathspecs", "show", base+":"+rel)
	if err != nil {
		baseBlob = "" // cả hai phía cùng thêm tệp
	}
	if strings.ContainsRune(theirs, 0) || strings.ContainsRune(baseBlob, 0) {
		return mergeSkipped
	}
	if cur, err := os.ReadFile(ours); err != nil || bytes.IndexByte(cur, 0) >= 0 {
		return mergeSkipped
	} else if string(cur) == theirs {
		return mergeClean // hai phía đã cùng một nội dung
	}
	dir, err := os.MkdirTemp("", "bint-merge-*")
	if err != nil {
		return mergeSkipped
	}
	defer os.RemoveAll(dir)
	bp, tp := filepath.Join(dir, "base"), filepath.Join(dir, "theirs")
	if os.WriteFile(bp, []byte(baseBlob), 0o600) != nil || os.WriteFile(tp, []byte(theirs), 0o600) != nil {
		return mergeSkipped
	}
	cmd := exec.CommandContext(ctx, "git", "-C", origin, "merge-file", "-L", mergeLabelOrigin, "-L", mergeLabelBase, "-L", mergeLabelWorktree, ours, bp, tp)
	err = cmd.Run()
	if err == nil {
		return mergeClean
	}
	// Mã thoát 1..127: số xung đột (đã ghi dấu vào tệp); lỗi khác (mã âm/255, không chạy được): tệp không bị ghi.
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() >= 1 && ee.ExitCode() <= 127 {
		return mergeMarkers
	}
	return mergeSkipped
}

// landPatch dựng bản vá nhị phân Base→snap giới hạn trong files (chia lô pathspec rồi nối: mỗi lô là bản vá hợp lệ).
func landPatch(ctx context.Context, wt, base, snap string, files []string) (string, error) {
	var sb strings.Builder
	for _, batch := range PathBatches(files, 16000) {
		args := append([]string{"--literal-pathspecs", "diff", "--binary", "--full-index", "--no-color", "--no-ext-diff", "--no-renames",
			base, snap, "--"}, batch...)
		out, err := gitrepo.Run(ctx, wt, args...)
		if err != nil {
			return "", err
		}
		sb.WriteString(out)
	}
	return sb.String(), nil
}

// gitApply áp bản vá lên cây làm việc của origin; không có --index/--cached nên không đụng chỉ mục.
func gitApply(ctx context.Context, origin, patch string, extra ...string) error {
	f, err := os.CreateTemp("", "bint-land-*.patch")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(patch); err != nil {
		f.Close()
		return err
	}
	f.Close()
	args := append([]string{"-C", origin, "apply", "--whitespace=nowarn"}, extra...)
	cmd := exec.CommandContext(ctx, "git", append(args, f.Name())...)
	// conflictFiles phân tích thông báo lỗi: ép bản tiếng Anh, git có bản dịch (vi.po...) sẽ không khớp biểu thức.
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANGUAGE=")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return errors.New(msg)
	}
	return nil
}

var (
	applyFailedRe = regexp.MustCompile(`(?m)^error: patch failed: (.+):\d+\r?$`)
	applyFileRe   = regexp.MustCompile(`(?m)^error: (.+): (?:patch does not apply|already exists in working directory|No such file or directory|does not exist in working directory)\r?$`)
)

// conflictFiles lấy tên tệp không áp được từ thông báo của `git apply --check`, giới hạn trong files; không
// phân tích được thì coi mọi tệp là nghi vấn.
func conflictFiles(stderr string, files []string) []string {
	in := map[string]bool{}
	for _, f := range files {
		in[f] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, re := range []*regexp.Regexp{applyFailedRe, applyFileRe} {
		for _, m := range re.FindAllStringSubmatch(stderr, -1) {
			if f := m[1]; in[f] && !seen[f] {
				seen[f] = true
				out = append(out, f)
			}
		}
	}
	if len(out) == 0 {
		return slices.Clone(files)
	}
	sort.Strings(out)
	return out
}

// Nhãn dấu xung đột do mergeOne ghi (git merge-file -L): chỉ dấu mang nhãn này mới là xung đột của land, dòng
// "<<<<<<< " có sẵn trong tệp (fixture, tài liệu về git) không chặn done.
const (
	mergeLabelOrigin   = "cây gốc"
	mergeLabelBase     = "base"
	mergeLabelWorktree = "worktree"
)

var conflictMarkerRe = regexp.MustCompile(`(?m)^<{7} ` + regexp.QuoteMeta(mergeLabelOrigin) + `\r?$`)

// landConflictsLeft liệt kê tệp còn xung đột ở cây gốc sau `land --3way`: còn dấu xung đột, hoặc không trộn được
// (người phải ghép tay, Bint không biết khi nào xong nên chặn done tới khi người --force).
func landConflictsLeft(t *task.Task) []string {
	l := t.State.Landed
	if l == nil || len(l.Conflicts) == 0 {
		return nil
	}
	var out []string
	for _, f := range l.Conflicts {
		if slices.Contains(l.Unmerged, f) {
			out = append(out, f)
			continue
		}
		b, err := os.ReadFile(filepath.Join(t.State.Origin, filepath.FromSlash(f)))
		if err == nil && conflictMarkerRe.Match(b) {
			out = append(out, f)
		}
	}
	return out
}

// worktreeDirty liệt kê tệp của tác vụ ở worktree chưa đưa về cây gốc (rỗng nếu worktree đã mất).
func (e *Env) worktreeDirty(ctx context.Context, t *task.Task) ([]string, error) {
	wt, err := e.RootFor(t)
	if err != nil {
		if errors.Is(err, ErrWorktreeGone) {
			return nil, nil
		}
		return nil, err
	}
	return ScopeFiles(ctx, wt, e.Tasks, t, e.Cfg.Limits.ProtectedPaths)
}

// checkCancelWorktree kiểm điều kiện huỷ tác vụ có worktree riêng: còn thay đổi chưa đưa về thì cần --discard
// (chỉ người). Tác vụ đã đưa về thì không còn gì để mất.
func (e *Env) checkCancelWorktree(ctx context.Context, t *task.Task, discard bool) error {
	if discard && !e.Human {
		return fmt.Errorf("%w: bint task cancel --discard", ErrAgent)
	}
	if t.State.Origin == "" || discard {
		return nil
	}
	if l := t.State.Landed; l != nil {
		// Đã đưa về; nhưng tệp không trộn được (--3way) thì bản của tác vụ chỉ còn ở worktree: gỡ nó là mất.
		if len(l.Unmerged) > 0 {
			return &ErrDiscardRequired{TaskID: t.State.ID, Files: slices.Clone(l.Unmerged)}
		}
		return nil
	}
	files, err := e.worktreeDirty(ctx, t)
	if err != nil {
		files = []string{"(không đọc được worktree: " + err.Error() + ")"}
	}
	if len(files) > 0 {
		return &ErrDiscardRequired{TaskID: t.State.ID, Files: files}
	}
	return nil
}

// removeWorktreeAfterCancel gỡ worktree của tác vụ vừa huỷ; lỗi dọn được ghi vào phiếu (task error).
func (e *Env) removeWorktreeAfterCancel(ctx context.Context, t *task.Task) {
	if t.State.Origin == "" {
		return
	}
	if notes := e.cleanupWorktree(context.WithoutCancel(ctx), t.State.ID, t.State.Origin, t.State.Worktree); len(notes) > 0 {
		e.RecordError(t.State.ID, "cancel", "gỡ worktree lỗi: "+strings.Join(notes, "; "))
	}
}

// OrphanWorktree là một worktree của Bint không còn ứng với tác vụ chưa kết thúc.
type OrphanWorktree struct {
	Path   string
	TaskID string // "" nếu không có tác vụ nào mang ID đó
	Status string // trạng thái tác vụ ("" nếu không có)
	Kept   bool   // tác vụ đã đưa về và giữ worktree bằng --keep (hoặc vì còn tệp không trộn được)
	// Unlanded: tác vụ đã done mà thay đổi chưa đưa về cây gốc (done --force của bản cũ): worktree là nơi duy nhất
	// còn giữ chúng, KHÔNG được gợi ý xoá.
	Unlanded bool
	// AwaitingMerge: worktree giữ bản của tác vụ cho tệp không trộn được và tác vụ chưa kết thúc (người còn đang
	// ghép tay): KHÔNG được gợi ý xoá.
	AwaitingMerge bool
	// Prunable: git báo mục đã mất thư mục (git worktree prune).
	Prunable bool
	Reason   string
}

// OrphanWorktrees tìm worktree mồ côi: thư mục trong <state>/worktrees không ứng với tác vụ chưa kết thúc, và
// mục `prunable` của `git worktree list` ở trong thư mục đó.
func (e *Env) OrphanWorktrees(ctx context.Context) ([]OrphanWorktree, error) {
	var out []OrphanWorktree
	seen := map[string]bool{}
	dir := e.worktreesDir()
	ents, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	for _, d := range ents {
		if !d.IsDir() {
			continue
		}
		p := filepath.Join(dir, d.Name())
		seen[state.NormalizePath(p)] = true
		o := e.classifyOrphan(d.Name(), p)
		if o != nil {
			out = append(out, *o)
		}
	}
	if list, err := gitrepo.WorktreeList(ctx, e.Root); err == nil {
		for _, w := range list {
			if !w.Prunable || !isUnder(dir, w.Path) {
				continue
			}
			id := filepath.Base(w.Path)
			if seen[state.NormalizePath(w.Path)] {
				// Thư mục còn nhưng git coi là prunable: đã báo ở trên nếu mồ côi; thêm cờ.
				for i := range out {
					if samePath(out[i].Path, w.Path) {
						out[i].Prunable = true
					}
				}
				continue
			}
			o := e.classifyOrphan(id, w.Path)
			if o == nil {
				o = &OrphanWorktree{Path: w.Path, TaskID: id, Reason: "thư mục đã mất nhưng git còn ghi mục worktree"}
			}
			o.Prunable = true
			out = append(out, *o)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// classifyOrphan trả nil nếu worktree p của tác vụ id còn được dùng (tác vụ chưa kết thúc).
func (e *Env) classifyOrphan(id, p string) *OrphanWorktree {
	t, err := e.Tasks.Load(id)
	if err != nil {
		return &OrphanWorktree{Path: p, TaskID: id, Reason: "không có tác vụ nào mang ID này"}
	}
	st := t.State
	switch {
	case st.Landed != nil && st.Landed.Kept:
		reason := "tác vụ đã đưa về, worktree được giữ lại theo yêu cầu khi đưa về"
		if len(st.Landed.Unmerged) > 0 {
			reason = "worktree được giữ vì còn bản của tác vụ cho tệp không trộn được (" + strings.Join(st.Landed.Unmerged, ", ") +
				"): lấy nội dung trước khi xoá"
		}
		awaiting := len(st.Landed.Unmerged) > 0 && !task.Terminal(st.Status)
		if awaiting {
			reason += "; tác vụ còn " + st.Status + ", đang chờ người ghép tay"
		}
		return &OrphanWorktree{Path: p, TaskID: id, Status: st.Status, Kept: true, AwaitingMerge: awaiting, Reason: reason}
	case st.Status == task.StatusDone && st.Landed == nil && st.Origin != "":
		return &OrphanWorktree{Path: p, TaskID: id, Status: st.Status, Unlanded: true,
			Reason: "tác vụ đã done nhưng thay đổi CHƯA đưa về cây gốc (còn trong worktree): đừng xoá trước khi lấy ra"}
	case task.Terminal(st.Status):
		return &OrphanWorktree{Path: p, TaskID: id, Status: st.Status, Reason: "tác vụ đã " + st.Status}
	case st.Isolation() != task.IsolationWorktree || st.Origin == "":
		return &OrphanWorktree{Path: p, TaskID: id, Status: st.Status, Reason: "tác vụ không dùng worktree này (chưa tạo xong hoặc chế độ shared)"}
	case st.Landed != nil:
		return &OrphanWorktree{Path: p, TaskID: id, Status: st.Status, Reason: "tác vụ đã đưa về"}
	}
	return nil
}

// CleanupCommands trả các lệnh git để người dọn tay một worktree mồ côi.
func (e *Env) CleanupCommands(o OrphanWorktree) []string {
	cmds := []string{}
	if o.Unlanded || o.AwaitingMerge {
		// Chỉ lệnh xem/lấy lại thay đổi; xoá worktree sẽ mất chúng.
		wt := shellQuote(o.Path)
		return []string{"git -C " + wt + " status", "git -C " + wt + " diff HEAD"}
	}
	if o.Prunable {
		cmds = append(cmds, "git -C "+shellQuote(e.Root)+" worktree prune")
	} else {
		cmds = append(cmds, "git -C "+shellQuote(e.Root)+" worktree remove --force "+shellQuote(o.Path))
	}
	if o.TaskID != "" {
		cmds = append(cmds, "git -C "+shellQuote(e.Root)+" branch -D "+BranchFor(o.TaskID))
	}
	return cmds
}

func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t'\"$`\\&|;<>()*?#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
