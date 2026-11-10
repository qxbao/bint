package task

import (
	"fmt"
	"slices"
	"strings"
)

// Trạng thái tác vụ.
const (
	StatusCreated     = "created"     // đã lập phiếu (Classify)
	StatusPlanned     = "planned"     // b-req xong
	StatusInProgress  = "in_progress" // b-feat đang/đã thực thi
	StatusInReview    = "in_review"   // chờ hoặc đang review
	StatusDone        = "done"        // hoàn thành, có bằng chứng
	StatusBlocked     = "blocked"     // chờ người (duyệt, trả lời, ngân sách)
	StatusInterrupted = "interrupted" // tiến trình điều phối kết thúc bất thường / bị huỷ giữa chừng
	StatusFailed      = "failed"      // bước lỗi không tự phục hồi
	StatusCancelled   = "cancelled"
)

// Statuses theo thứ tự trình bày.
var Statuses = []string{StatusCreated, StatusPlanned, StatusInProgress, StatusInReview, StatusDone,
	StatusBlocked, StatusInterrupted, StatusFailed, StatusCancelled}

// activeStatuses là các trạng thái công việc đang tiến triển.
var activeStatuses = []string{StatusCreated, StatusPlanned, StatusInProgress, StatusInReview}

// Active cho biết trạng thái có đang tiến triển không.
func Active(s string) bool { return slices.Contains(activeStatuses, s) }

// Terminal cho biết trạng thái kết thúc.
func Terminal(s string) bool { return s == StatusDone || s == StatusCancelled }

// Paused: chờ tiếp tục (bint run resume hoặc người xử lý).
func Paused(s string) bool {
	return s == StatusBlocked || s == StatusInterrupted || s == StatusFailed
}

var transitions = map[string][]string{
	StatusCreated:     {StatusPlanned, StatusInProgress, StatusInReview, StatusBlocked, StatusInterrupted, StatusFailed, StatusCancelled},
	StatusPlanned:     {StatusInProgress, StatusBlocked, StatusInterrupted, StatusFailed, StatusCancelled},
	StatusInProgress:  {StatusInReview, StatusDone, StatusBlocked, StatusInterrupted, StatusFailed, StatusCancelled},
	StatusInReview:    {StatusDone, StatusInProgress, StatusBlocked, StatusInterrupted, StatusFailed, StatusCancelled},
	StatusBlocked:     {StatusCreated, StatusPlanned, StatusInProgress, StatusInReview, StatusDone, StatusCancelled},
	StatusInterrupted: {StatusCreated, StatusPlanned, StatusInProgress, StatusInReview, StatusDone, StatusCancelled},
	StatusFailed:      {StatusCreated, StatusPlanned, StatusInProgress, StatusInReview, StatusCancelled},
	StatusDone:        {},
	StatusCancelled:   {},
}

// CanTransition cho biết bảng chuyển trạng thái có cho phép from → to không.
func CanTransition(from, to string) bool { return slices.Contains(transitions[from], to) }

// TransitionRow là một dòng của bảng chuyển trạng thái (cho `bint task transitions`).
type TransitionRow struct{ From, To, Note string }

// TransitionTable trả về bảng chuyển trạng thái kèm điều kiện.
func TransitionTable() []TransitionRow {
	notes := map[string]string{
		StatusDone:        "cần bằng chứng (xem `bint task status --help`); người có thể --force",
		StatusBlocked:     "chờ người: duyệt đường dẫn bảo vệ, xác nhận lệnh kiểm chứng, trả lời câu hỏi, ngân sách",
		StatusInterrupted: "bint run bị huỷ hoặc tiến trình chết; tiếp tục bằng bint run resume",
		StatusCancelled:   "kết thúc, không tiếp tục được",
	}
	var out []TransitionRow
	for _, from := range Statuses {
		for _, to := range transitions[from] {
			out = append(out, TransitionRow{From: from, To: to, Note: notes[to]})
		}
	}
	return out
}

// Apply chuyển trạng thái (đã kiểm tra bảng) và ghi lịch sử. Khi rời một trạng thái
// hoạt động sang blocked/interrupted/failed, trạng thái cũ được nhớ để tiếp tục.
func (s *State) Apply(to, by, reason, at string) error {
	if s.Status == to {
		s.Reason = reason
		return nil
	}
	if !CanTransition(s.Status, to) {
		return fmt.Errorf("không chuyển được %s → %s (xem `bint task transitions`)", s.Status, to)
	}
	if Paused(to) && Active(s.Status) {
		s.ResumeFrom = s.Status
	}
	if Active(to) || Terminal(to) {
		s.ResumeFrom = ""
	}
	s.History = append(s.History, Transition{From: s.Status, To: to, At: at, By: by, Reason: reason})
	s.Status, s.Reason = to, reason
	return nil
}

// DoneInput là trạng thái hiện tại cần cho kiểm tra hoàn thành.
type DoneInput struct {
	// Worktree là fingerprint mã hiện tại ("" nếu không xác định được).
	Worktree string
	// VerifyConfirmed: lệnh kiểm chứng hiện tại đã được người xác nhận.
	VerifyConfirmed bool
}

// DoneBlockers trả về lý do chưa được coi là hoàn thành (rỗng = đủ bằng chứng).
//
// Quy tắc (mục 6.2: không tự coi là xong khi chưa đủ bằng chứng):
//   - có thay đổi chạm đường dẫn bảo vệ/deny chưa được người duyệt → chặn;
//   - phiếu có lệnh kiểm chứng: lệnh phải đã được người xác nhận, lần chạy cuối qua,
//     đúng lệnh hiện tại và trên đúng trạng thái mã hiện tại;
//   - không có lệnh kiểm chứng: mọi tiêu chí chấp nhận phải được đối chiếu là đạt
//     kèm bằng chứng; không có cả hai thì không có bằng chứng nào → chặn.
func DoneBlockers(t *Task, in DoneInput) []string {
	var out []string
	st := &t.State
	if len(st.PendingPaths) > 0 {
		out = append(out, "thay đổi chạm đường dẫn bảo vệ/deny chưa được duyệt: "+strings.Join(st.PendingPaths, ", ")+" (bint task approve)")
	}
	cmd := strings.TrimSpace(t.Ticket.Verify.Command)
	if cmd != "" {
		v := st.LastVerification()
		switch {
		case !in.VerifyConfirmed:
			out = append(out, "lệnh kiểm chứng chưa được người xác nhận (bint task approve)")
		case v == nil:
			out = append(out, "chưa chạy lệnh kiểm chứng (bint task verify)")
		case v.Command != cmd:
			out = append(out, "lệnh kiểm chứng đã đổi sau lần chạy cuối (bint task verify)")
		case !v.Passed:
			out = append(out, "lần kiểm chứng cuối không qua")
		case in.Worktree == "" || v.Worktree != in.Worktree:
			out = append(out, "mã đã thay đổi sau lần kiểm chứng cuối (bint task verify)")
		}
	}
	crit := t.Ticket.Verify.Criteria
	if cmd == "" && len(crit) == 0 {
		out = append(out, "phiếu không có lệnh kiểm chứng hay tiêu chí chấp nhận: không có bằng chứng (người có thể --force)")
	}
	for i := range crit {
		c := st.CriterionStatus(i + 1)
		switch {
		case c != nil && !c.Met:
			// Tiêu chí bị đánh dấu chưa đạt luôn chặn, kể cả khi lệnh kiểm chứng qua.
			out = append(out, fmt.Sprintf("tiêu chí %d chưa đạt", i+1))
		case c == nil && cmd == "":
			out = append(out, fmt.Sprintf("tiêu chí %d chưa được đối chiếu", i+1))
		}
	}
	return out
}
