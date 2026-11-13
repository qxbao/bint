package app

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
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

// ErrTaskTreePruned: cây (hoặc commit) làm mốc so sánh của tác vụ không còn trong repository nên không dựng lại
// được diff. Bint xoá ref giữ cây (refs/bint/tasks/<id>/base, base-tree) khi tác vụ kết thúc hoặc đưa về cây
// gốc, nên git gc có thể dọn các cây đó về sau.
var ErrTaskTreePruned = errors.New("cây so sánh của tác vụ đã bị dọn")

// maxPrunedFiles là số tệp tối đa nêu trong thông báo ErrTaskTreePruned.
const maxPrunedFiles = 20

// TaskDiffOptions: `bint task diff <task-id> [--stat|--name-only] [--all]`.
type TaskDiffOptions struct {
	Ref      string `arg:"task-id" form:"required,suggest=tasks"`
	Stat     bool   `flag:"stat"`
	NameOnly bool   `flag:"name-only"`
	// All: tác vụ có theo dõi Touched cũng hiện tệp không thuộc Touched (người/tác vụ khác đổi vào cùng cây, sửa tay
	// ngoài b-feat). Mặc định chỉ hiện tệp thuộc Touched.
	All bool `flag:"all"`
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
	// Excluded là các tệp đổi so với BaseTree nhưng không thuộc Touched của tác vụ, bị loại khỏi diff mặc định
	// (chỉ điền khi tác vụ theo dõi Touched và không có --all).
	Excluded []string
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
	useSpec := false // true: chỉ giữ tệp thuộc spec (kể cả spec rỗng)
	hideOthers := false
	switch {
	case t.State.Landed != nil && t.State.Landed.Tree != "":
		// Đã đưa về cây gốc (worktree có thể đã gỡ): so mốc ban đầu với ảnh chụp lúc đưa về, trên tệp đã đưa về.
		res.From, landedTree = t.State.BaseTree, t.State.Landed.Tree
		if res.From == "" {
			res.From = t.State.Base
		}
		if err := checkTreesPresent(ctx, root, t, res.From, landedTree); err != nil {
			return res, err
		}
		useSpec = true
		if spec = t.State.Landed.Files; len(spec) == 0 {
			res.Note = "tác vụ đã đưa về cây gốc nhưng không có tệp nào"
			return res, nil
		}
		res.Note = "tác vụ đã đưa về cây gốc: diff giữa mốc đầu và ảnh chụp lúc đưa về"
	case t.State.TracksTouched():
		res.From = t.State.BaseTree
		if err := checkTreesPresent(ctx, root, t, res.From, ""); err != nil {
			return res, err
		}
		useSpec = true
		if o.All || t.State.InWorktree() {
			// Toàn bộ phạm vi của tác vụ: Touched cùng thay đổi ngoài b-feat quy cho nó (cổng phạm vi dùng tập này).
			// Tác vụ worktree luôn dùng tập này: worktree riêng không có người viết khác, và Land đưa mọi tệp trong
			// đó về cây gốc, nên ẩn tệp ngoài Touched sẽ giấu khỏi reviewer đúng những gì sắp được đưa về.
			if spec, err = workflow.ScopeFiles(ctx, root, ws.Tasks, t, ws.Cfg.Limits.ProtectedPaths); err != nil {
				return res, err
			}
			if len(spec) == 0 {
				res.Note = "tác vụ chưa sửa tệp nào (b-feat chưa chạy hoặc không đổi gì)"
				return res, nil
			}
		} else {
			// Mặc định chỉ tệp của chính tác vụ: người hay tác vụ khác commit/sửa vào cùng cây sau BaseTree làm
			// reviewer kết luận sai nếu lẫn vào đây.
			spec, hideOthers = t.State.Touched, true
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
	if !useSpec {
		res.Paths = all
	} else {
		in := make(map[string]bool, len(spec))
		for _, f := range spec {
			in[f] = true
		}
		for _, f := range all {
			switch {
			case in[f]:
				res.Paths = append(res.Paths, f)
			case hideOthers:
				res.Excluded = append(res.Excluded, f)
			}
		}
	}
	if hideOthers {
		if len(t.State.Touched) == 0 {
			res.Note = "tác vụ chưa sửa tệp nào (b-feat chưa chạy hoặc không đổi gì)"
		}
		if n := len(res.Excluded); n > 0 {
			res.Note = strings.TrimSpace(res.Note + " " + excludedNote(res.Excluded))
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

// excludedNote là dòng ghi chú về các tệp bị loại khỏi diff mặc định (tên vẫn liệt kê để người xem biết cần --all không).
func excludedNote(files []string) string {
	shown := files
	if len(shown) > maxPrunedFiles {
		shown = shown[:maxPrunedFiles]
	}
	msg := fmt.Sprintf("đã loại %d tệp không thuộc tác vụ: %s", len(files), strings.Join(shown, ", "))
	if len(files) > len(shown) {
		msg += fmt.Sprintf(" và %d tệp nữa", len(files)-len(shown))
	}
	return msg + " (--all để xem toàn bộ)"
}

// checkTreesPresent kiểm các mốc so sánh (from, landed; "" và cây rỗng của Git được bỏ qua) còn trong repository
// bằng `git cat-file -e` TRƯỚC khi diff, để cây đã bị git gc dọn trả ErrTaskTreePruned có lời giải thích thay vì
// lỗi thô của git (không đoán qua chuỗi stderr của git, vốn phụ thuộc phiên bản và ngôn ngữ). Chỉ coi là "đã bị
// dọn" khi git chạy được trong repository nhưng cat-file thoát khác 0; lỗi khác (không chạy được git...) trả nguyên.
func checkTreesPresent(ctx context.Context, root string, t *task.Task, revs ...string) error {
	if _, err := gitrepo.Run(ctx, root, "rev-parse", "--git-dir"); err != nil {
		return nil // lỗi môi trường: để bước diff báo lỗi gốc
	}
	for _, rev := range revs {
		if rev == "" || rev == emptyTreeSHA {
			continue
		}
		_, err := gitrepo.Run(ctx, root, "cat-file", "-e", rev+"^{tree}")
		var ee *exec.ExitError
		if err == nil || !errors.As(err, &ee) {
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return prunedError(t, rev)
	}
	return nil
}

// prunedError dựng lỗi bọc ErrTaskTreePruned: nêu tác vụ, trạng thái, mã cây thiếu, nguyên nhân và các tệp đã biết.
func prunedError(t *task.Task, rev string) error {
	st := t.State
	var why string
	switch {
	case task.Terminal(st.Status):
		why = fmt.Sprintf("tác vụ %s đã %s (%s); Bint xoá ref giữ cây (refs/bint/tasks/%s/) khi tác vụ kết thúc và git gc đã dọn cây %s sau khi tác vụ kết thúc",
			st.ID, st.Status, st.UpdatedAt, st.ID, shortRev(rev))
	case st.Landed != nil:
		why = fmt.Sprintf("tác vụ %s (trạng thái %s) đã đưa về cây gốc; Bint xoá ref giữ cây (refs/bint/tasks/%s/) khi đưa về và git gc đã dọn cây %s",
			st.ID, st.Status, st.ID, shortRev(rev))
	default:
		why = fmt.Sprintf("tác vụ %s (trạng thái %s): cây %s không còn trong repository (ref refs/bint/tasks/%s/base-tree bị xoá hoặc hỏng, rồi git gc đã dọn cây)",
			st.ID, st.Status, shortRev(rev), st.ID)
	}
	files := st.Touched
	if st.Landed != nil && len(st.Landed.Files) > 0 {
		files = st.Landed.Files
	}
	msg := why + "; không dựng lại được diff"
	if len(files) > 0 {
		shown := files
		if len(shown) > maxPrunedFiles {
			shown = shown[:maxPrunedFiles]
		}
		msg += ". Tệp đã biết của tác vụ: " + strings.Join(shown, ", ")
		if len(files) > len(shown) {
			msg += fmt.Sprintf(" và %d tệp nữa", len(files)-len(shown))
		}
	}
	return fmt.Errorf("%w: %s", ErrTaskTreePruned, msg)
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
