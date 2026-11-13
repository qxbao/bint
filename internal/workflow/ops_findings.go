package workflow

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"bint/internal/gitrepo"
	"bint/internal/task"
)

// ErrFindingInput bọc lỗi đầu vào của thao tác trên phát hiện (ID không có, kết cục sai, thiếu commit/lý do).
// Lớp app đổi nó thành lỗi cách dùng.
var ErrFindingInput = errors.New("đầu vào phát hiện không hợp lệ")

func findingInputf(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrFindingInput, fmt.Sprintf(format, a...))
}

// FindingView là một phát hiện kèm kết cục hiệu lực.
type FindingView struct {
	TaskID  string
	Round   int
	Finding task.Finding
	Outcome task.OutcomeRecord
	// History là mọi lần đặt kết cục (rỗng = chưa từng đặt, kết cục mặc định open).
	History []task.OutcomeRecord
}

// ViewFindings nạp mọi phát hiện của một tác vụ kèm kết cục. Hỏng review.json/findings.json trả lỗi.
func ViewFindings(t *task.Task) ([]FindingView, error) {
	rev, err := t.LoadReview()
	if err != nil {
		return nil, err
	}
	out, err := t.LoadOutcomes()
	if err != nil {
		return nil, err
	}
	var views []FindingView
	for _, r := range rev.Rounds {
		for _, f := range r.Findings {
			views = append(views, FindingView{TaskID: t.State.ID, Round: r.Round, Finding: f,
				Outcome: out.Effective(f.ID), History: out.History(f.ID)})
		}
	}
	return views, nil
}

// lookupFinding tìm một phát hiện theo ID; ID không có là lỗi đầu vào.
func (e *Env) lookupFinding(id string) (*task.Task, FindingView, error) {
	t, f, round, err := e.Tasks.LoadFinding(id)
	if err != nil {
		return nil, FindingView{}, findingInputf("%v", err)
	}
	out, err := t.LoadOutcomes()
	if err != nil {
		return nil, FindingView{}, err
	}
	return t, FindingView{TaskID: t.State.ID, Round: round.Round, Finding: *f, Outcome: out.Effective(f.ID),
		History: out.History(f.ID)}, nil
}

// SetFindingOutcome đặt kết cục cho một hoặc nhiều phát hiện (ghi chỉ-thêm vào findings.json của từng tác vụ).
// Kiểm hết đầu vào trước khi ghi: lỗi đầu vào thì không ghi gì. fixed cần commit có thật (lưu SHA đầy đủ);
// rejected và deferred cần lý do. Trả về các bản ghi đã ghi.
func (e *Env) SetFindingOutcome(ctx context.Context, ids []string, outcome, commit, reason string) ([]task.OutcomeRecord, error) {
	outcome = strings.ToLower(strings.TrimSpace(outcome))
	commit, reason = strings.TrimSpace(commit), strings.TrimSpace(reason)
	if !task.ValidOutcome(outcome) {
		return nil, findingInputf("kết cục %q không hợp lệ (%s)", outcome, strings.Join(task.FindingOutcomes, " | "))
	}
	if len(ids) == 0 {
		return nil, findingInputf("thiếu ID phát hiện")
	}
	switch outcome {
	case task.OutcomeFixed:
		if commit == "" {
			return nil, findingInputf("fixed cần --commit trỏ tới commit đã sửa")
		}
		full, err := gitrepo.Run(ctx, e.Root, "rev-parse", "--verify", "-q", commit+"^{commit}")
		if err != nil || strings.TrimSpace(full) == "" {
			return nil, findingInputf("--commit %q không phải commit có thật trong repository", commit)
		}
		commit = strings.TrimSpace(full)
	case task.OutcomeRejected, task.OutcomeDeferred:
		if reason == "" {
			return nil, findingInputf("%s cần --reason", outcome)
		}
		commit = ""
	default:
		commit = ""
	}
	byTask := map[string][]task.OutcomeRecord{}
	seen := map[string]bool{}
	at, by := e.at(), e.actor()
	for _, id := range ids {
		t, v, err := e.lookupFinding(id)
		if err != nil {
			return nil, err
		}
		if seen[v.Finding.ID] {
			continue
		}
		seen[v.Finding.ID] = true
		byTask[t.State.ID] = append(byTask[t.State.ID], task.OutcomeRecord{Finding: v.Finding.ID, Outcome: outcome,
			Commit: commit, Reason: reason, By: by, At: at})
	}
	taskIDs := make([]string, 0, len(byTask))
	for id := range byTask {
		taskIDs = append(taskIDs, id)
	}
	sort.Strings(taskIDs)
	var all []task.OutcomeRecord
	var written []string
	for _, tid := range taskIDs {
		recs := byTask[tid]
		if _, err := e.Tasks.Update(tid, e.now(), func(t *task.Task) error { return t.AppendOutcomes(recs...) }); err != nil {
			if len(written) > 0 {
				return all, fmt.Errorf("%w (đã ghi: %s)", err, strings.Join(written, ", "))
			}
			return all, err
		}
		for _, r := range recs {
			written = append(written, r.Finding)
		}
		all = append(all, recs...)
	}
	return all, nil
}

// fromFindings phân giải các phát hiện làm nguồn của phiếu sửa: bỏ trùng; fixed/rejected hoặc ID không có là
// lỗi đầu vào. Trả về phát hiện theo thứ tự nhập và tập đường dẫn (bỏ trùng, đã sắp xếp).
func (e *Env) fromFindings(ids []string) ([]FindingView, []string, error) {
	var views []FindingView
	seen := map[string]bool{}
	paths := map[string]bool{}
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			continue
		}
		_, v, err := e.lookupFinding(id)
		if err != nil {
			return nil, nil, err
		}
		if seen[v.Finding.ID] {
			continue
		}
		seen[v.Finding.ID] = true
		if o := v.Outcome.Outcome; o == task.OutcomeFixed || o == task.OutcomeRejected {
			return nil, nil, findingInputf("phát hiện %s đã %s: chỉ lập phiếu sửa từ phát hiện open hoặc deferred", v.Finding.ID, o)
		}
		views = append(views, v)
		if v.Finding.Path != "" {
			paths[v.Finding.Path] = true
		}
	}
	if len(views) == 0 {
		return nil, nil, findingInputf("thiếu ID phát hiện")
	}
	sorted := make([]string, 0, len(paths))
	for p := range paths {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)
	return views, sorted, nil
}

// findingsRequest dựng mô tả phiếu sửa: phần người viết (nếu có) rồi từng phát hiện.
func findingsRequest(user string, views []FindingView) string {
	var b strings.Builder
	user = strings.TrimSpace(user)
	if user != "" {
		b.WriteString(user + "\n\n")
	} else {
		fmt.Fprintf(&b, "Sửa %d phát hiện review\n\n", len(views))
	}
	b.WriteString("Phát hiện cần sửa:\n")
	for _, v := range views {
		f := v.Finding
		loc := f.Path
		if loc != "" && f.Lines != "" {
			loc += ":" + f.Lines
		}
		fmt.Fprintf(&b, "- %s [%s/%s] %s", f.ID, f.Kind, f.Severity, f.Title)
		if loc != "" {
			fmt.Fprintf(&b, " (%s)", loc)
		}
		if v.Outcome.Outcome == task.OutcomeDeferred {
			b.WriteString(" [deferred]")
		}
		b.WriteString("\n")
		if f.Evidence != "" {
			fmt.Fprintf(&b, "  Bằng chứng: %s\n", strings.ReplaceAll(strings.TrimSpace(f.Evidence), "\n", " "))
		}
	}
	return b.String()
}
