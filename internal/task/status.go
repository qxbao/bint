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

// TransitionsFrom trả các trạng thái đích hợp lệ từ from theo bảng chuyển (cho form đổi trạng thái).
func TransitionsFrom(from string) []string { return slices.Clone(transitions[from]) }

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

// ClosePlan đóng phiếu chỉ lập kế hoạch đang planned thành done và ghi lịch sử. Không phải đường tắt trong bảng
// chuyển (planned → done vẫn không có ở transitions): chỉ nơi gọi đã kiểm phiếu không có bước ghi nào mới dùng.
func (s *State) ClosePlan(by, reason, at string) error {
	if s.Status != StatusPlanned {
		return fmt.Errorf("không đóng được kế hoạch ở trạng thái %s (cần planned)", s.Status)
	}
	s.History = append(s.History, Transition{From: StatusPlanned, To: StatusDone, At: at, By: by, Reason: reason})
	s.Status, s.Reason, s.ResumeFrom = StatusDone, reason, ""
	return nil
}

// DoneInput là trạng thái hiện tại cần cho kiểm tra hoàn thành.
type DoneInput struct {
	// Worktree là fingerprint mã hiện tại ("" nếu không xác định được).
	Worktree string
	// VerifyConfirmed: lệnh kiểm chứng hiện tại đã được người xác nhận.
	VerifyConfirmed bool
	// OutsideSinceVerify là các tệp đã đổi kể từ lần kiểm chứng cuối mà không thuộc Touched của tác vụ
	// (thường là việc của tác vụ khác trên cùng worktree). Chỉ để làm rõ thông báo.
	OutsideSinceVerify []string
	// TouchedSinceVerify là các tệp đã đổi kể từ lần kiểm chứng cuối mà thuộc Touched của tác vụ. Chỉ DoneInputDetailed
	// điền (bộ nhớ, không lưu): Finalize chỉ tự kiểm chứng lại khi mảng này rỗng còn OutsideSinceVerify khác rỗng.
	TouchedSinceVerify []string
	// IgnoreLanding bỏ điều kiện "đã đưa về cây gốc" (dùng khi kiểm cổng done ngay trước khi đưa về).
	IgnoreLanding bool
	// LandConflicts là tệp ở cây gốc còn xung đột sau `land --3way` (còn dấu xung đột hoặc không trộn được):
	// chưa done được cho tới khi người gỡ xong.
	LandConflicts []string
}

// BlockerKind phân loại điều chặn done. Người chỉ được bỏ qua (land --force) loại thuộc danh sách cho phép tường minh
// Forceable; loại mới thêm sau này mặc định là điều chặn cứng cho tới khi được đưa vào danh sách.
type BlockerKind int

const (
	BlockLand             BlockerKind = iota // chưa đưa về cây gốc
	BlockLandConflict                        // tệp còn xung đột sau land --3way
	BlockPendingPaths                        // đường dẫn bảo vệ/deny chưa duyệt
	BlockVerifyUnconfirm                     // lệnh kiểm chứng chưa được người xác nhận
	BlockVerifyMissing                       // chưa chạy lệnh kiểm chứng
	BlockVerifyChanged                       // lệnh đổi sau lần chạy cuối
	BlockVerifyFailed                        // lần kiểm chứng cuối không qua
	BlockVerifyStale                         // mã đổi sau lần kiểm chứng cuối
	BlockNoEvidence                          // phiếu không có lệnh kiểm chứng lẫn tiêu chí
	BlockCriterionUnmet                      // tiêu chí đã đánh dấu chưa đạt
	BlockCriterionUnchecked                  // tiêu chí chưa đối chiếu
)

// Forceable cho biết người có thể bỏ qua điều chặn loại k bằng land --force: chỉ danh sách cho phép tường minh (kiểm
// chứng không qua/cũ/chưa chạy/đổi lệnh, tiêu chí chưa đối chiếu, thiếu bằng chứng). Mọi loại khác, kể cả loại thêm
// sau này, KHÔNG bỏ qua được.
func (k BlockerKind) Forceable() bool {
	switch k {
	case BlockVerifyMissing, BlockVerifyChanged, BlockVerifyFailed, BlockVerifyStale, BlockNoEvidence, BlockCriterionUnchecked:
		return true
	}
	return false
}

// Blocker là một điều chặn done kèm loại.
type Blocker struct {
	Kind BlockerKind
	Text string
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
	bs := DoneBlockersTyped(t, in)
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = b.Text
	}
	return out
}

// DoneBlockersTyped như DoneBlockers nhưng mỗi điều chặn mang loại.
func DoneBlockersTyped(t *Task, in DoneInput) []Blocker {
	var out []Blocker
	add := func(k BlockerKind, s string) { out = append(out, Blocker{k, s}) }
	st := &t.State
	if st.NeedsLand() && !in.IgnoreLanding {
		add(BlockLand, "chưa đưa thay đổi về cây gốc "+st.Origin+" (bint task land "+st.ID+")")
	}
	if len(in.LandConflicts) > 0 {
		add(BlockLandConflict, "tệp ở cây gốc còn xung đột sau khi đưa về bằng ba chiều: "+strings.Join(in.LandConflicts, ", ")+
			" (gỡ dấu <<<<<<< / ghép tay phần của tác vụ rồi "+st.ResumeCommand()+"; người đã xử lý xong tệp không trộn được thì xác nhận bằng bint task status "+st.ID+" done kèm cờ ép)")
	}
	if len(st.PendingPaths) > 0 {
		add(BlockPendingPaths, "thay đổi chạm đường dẫn bảo vệ/deny chưa được duyệt: "+strings.Join(st.PendingPaths, ", ")+" (bint task approve)")
	}
	cmd := strings.TrimSpace(t.Ticket.Verify.Command)
	if cmd != "" {
		v := st.LastVerification()
		switch {
		case !in.VerifyConfirmed:
			add(BlockVerifyUnconfirm, "lệnh kiểm chứng chưa được người xác nhận (bint task approve)")
		case v == nil:
			add(BlockVerifyMissing, "chưa chạy lệnh kiểm chứng (bint task verify)")
		case v.Command != cmd:
			add(BlockVerifyChanged, "lệnh kiểm chứng đã đổi sau lần chạy cuối (bint task verify)")
		case !v.Passed:
			add(BlockVerifyFailed, "lần kiểm chứng cuối không qua")
		case in.Worktree == "" || v.Worktree != in.Worktree:
			if len(in.OutsideSinceVerify) > 0 {
				add(BlockVerifyStale, "thay đổi không thuộc tác vụ: "+strings.Join(in.OutsideSinceVerify, ", ")+
					"; chạy lại bint task verify "+st.ID)
			} else {
				add(BlockVerifyStale, "mã đã thay đổi sau lần kiểm chứng cuối (bint task verify)")
			}
		}
	}
	crit := t.Ticket.Verify.Criteria
	if cmd == "" && len(crit) == 0 {
		add(BlockNoEvidence, "phiếu không có lệnh kiểm chứng hay tiêu chí chấp nhận: không có bằng chứng (người có thể --force)")
	}
	for i := range crit {
		c := st.CriterionStatus(i + 1)
		switch {
		case c != nil && !c.Met:
			// Tiêu chí bị đánh dấu chưa đạt luôn chặn, kể cả khi lệnh kiểm chứng qua.
			add(BlockCriterionUnmet, fmt.Sprintf("tiêu chí %d chưa đạt", i+1))
		case c == nil && cmd == "":
			add(BlockCriterionUnchecked, fmt.Sprintf("tiêu chí %d chưa được đối chiếu", i+1))
		}
	}
	return out
}
