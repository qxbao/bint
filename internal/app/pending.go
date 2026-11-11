package app

import (
	"context"
	"fmt"
	"sort"

	"bint/internal/eval"
	"bint/internal/hcomp"
	"bint/internal/knowledge"
	"bint/internal/task"
)

// Tên các nguồn của hộp thư "Việc cần bạn" (PendingError.Source).
const (
	PendingSrcKnowledge  = "knowledge"
	PendingSrcComponents = "components"
	PendingSrcTasks      = "tasks"
	PendingSrcProposals  = "proposals"
	PendingSrcEval       = "eval"
	PendingSrcDoctor     = "doctor"
)

// PendingBatch là các mục tri thức chờ duyệt của một lô `nguon:<lô>`.
type PendingBatch struct {
	Tag       string
	Candidate int
	Stale     int
}

// Count là tổng số mục chờ của lô.
func (b PendingBatch) Count() int { return b.Candidate + b.Stale }

// PendingComponent là thành phần harness ở trạng thái candidate.
type PendingComponent struct{ Kind, ID string }

// PendingTask là tác vụ đang blocked (chờ người).
type PendingTask struct{ ID, Goal, Reason string }

// PendingProposal là đề xuất tiến hoá đang chờ duyệt.
type PendingProposal struct{ ID, Action string }

// PendingError là lỗi đọc một nguồn; các nguồn khác vẫn có dữ liệu.
type PendingError struct {
	Source string
	Err    error
}

// PendingWorkReport là mọi việc đang chờ người ở một repository (docs/tui-plan.md mục 4.3.1).
type PendingWorkReport struct {
	// KnowledgeBatches: mục candidate + stale tách theo lô, sắp theo nhãn; KnowledgeOther: các mục chờ
	// không thuộc lô nào (một dòng).
	KnowledgeBatches []PendingBatch
	KnowledgeOther   int
	Components       []PendingComponent
	BlockedTasks     []PendingTask
	Proposals        []PendingProposal
	EvalCandidates   []string
	// Doctor: chỉ các kiểm tra mức warn và fail (gồm cấu hình theo mẫu cũ, skill lệch phiên bản);
	// DoctorAll là báo cáo đầy đủ (để Tổng quan đếm mà không chạy doctor lần hai).
	Doctor    []Check
	DoctorAll *DoctorReport
	Errors    []PendingError
}

// KnowledgeTotal là tổng số mục tri thức chờ duyệt.
func (r PendingWorkReport) KnowledgeTotal() int {
	n := r.KnowledgeOther
	for _, b := range r.KnowledgeBatches {
		n += b.Count()
	}
	return n
}

// Total là tổng số dòng việc (không tính lỗi đọc nguồn).
func (r PendingWorkReport) Total() int {
	n := len(r.Components) + len(r.BlockedTasks) + len(r.Proposals) + len(r.EvalCandidates) + len(r.Doctor)
	if r.KnowledgeOther > 0 {
		n++
	}
	return n + len(r.KnowledgeBatches)
}

// ErrorOf trả lỗi đọc của nguồn src (nil nếu đọc được).
func (r PendingWorkReport) ErrorOf(src string) error {
	for _, e := range r.Errors {
		if e.Source == src {
			return e.Err
		}
	}
	return nil
}

// PendingWork đọc sáu nguồn việc chờ người ở repository chứa dir. Chỉ đọc: không nạp spool, không ghi
// gì. Nguồn đọc lỗi được ghi vào Errors và không làm mất các nguồn khác. Repository chưa init cho lỗi
// ErrNotInitialized (người gọi hiện màn khởi tạo thay vì hộp thư).
func PendingWork(ctx context.Context, dir string) (*PendingWorkReport, error) {
	rep := &PendingWorkReport{}
	ws, err := OpenWorkspace(ctx, dir, true)
	if err != nil {
		return nil, err
	}
	fail := func(src string, err error) { rep.Errors = append(rep.Errors, PendingError{Source: src, Err: err}) }

	// 1. Tri thức chờ duyệt, tách theo lô.
	if k, err := OpenKnowledge(ctx, dir); err != nil {
		fail(PendingSrcKnowledge, err)
	} else if items, _, err := k.Items(); err != nil {
		fail(PendingSrcKnowledge, err)
	} else {
		byBatch := map[string]*PendingBatch{}
		for _, it := range items {
			if !it.Pending() {
				continue
			}
			tag := it.Scope.Batch()
			if tag == "" {
				rep.KnowledgeOther++
				continue
			}
			b := byBatch[tag]
			if b == nil {
				b = &PendingBatch{Tag: tag}
				byBatch[tag] = b
			}
			if it.Status == knowledge.StatusStale {
				b.Stale++
			} else {
				b.Candidate++
			}
		}
		for _, b := range byBatch {
			rep.KnowledgeBatches = append(rep.KnowledgeBatches, *b)
		}
		sort.Slice(rep.KnowledgeBatches, func(i, j int) bool { return rep.KnowledgeBatches[i].Tag < rep.KnowledgeBatches[j].Tag })
	}

	// 2. Thành phần harness candidate.
	if cw, err := OpenComponents(ctx, dir); err != nil {
		fail(PendingSrcComponents, err)
	} else {
		for _, c := range cw.Lib.Components {
			if c.Status == hcomp.StatusCandidate && !c.Builtin {
				rep.Components = append(rep.Components, PendingComponent{Kind: string(c.Kind), ID: c.ID})
			}
		}
	}

	// 3. Tác vụ blocked.
	tasks, bad := task.NewStore(ws.Paths.Runtime).List()
	for _, t := range tasks {
		if t.State.Status == task.StatusBlocked {
			rep.BlockedTasks = append(rep.BlockedTasks, PendingTask{ID: t.State.ID, Goal: t.Ticket.Goal, Reason: t.State.Reason})
		}
	}
	if len(bad) > 0 { // giữ các tác vụ đọc được, vẫn báo tệp hỏng (tác vụ blocked hỏng sẽ không hiện trong hộp thư)
		err := bad[0]
		if len(bad) > 1 {
			err = fmt.Errorf("%w (và %d tệp tác vụ hỏng khác)", bad[0], len(bad)-1)
		}
		fail(PendingSrcTasks, err)
	}

	// 4. Đề xuất tiến hoá chờ duyệt.
	if s, err := ws.OpenStore(ctx); err != nil {
		fail(PendingSrcProposals, err)
	} else {
		rows, err := s.DB.QueryContext(ctx, `SELECT proposal_id, action FROM evolution_proposals WHERE status = 'proposed' ORDER BY created_at`)
		if err != nil {
			fail(PendingSrcProposals, err)
		} else {
			for rows.Next() {
				var p PendingProposal
				if err := rows.Scan(&p.ID, &p.Action); err != nil {
					fail(PendingSrcProposals, err)
					break
				}
				rep.Proposals = append(rep.Proposals, p)
			}
			if err := rows.Err(); err != nil {
				fail(PendingSrcProposals, err)
			}
			rows.Close()
		}
		s.Close()
	}

	// 5. Ứng viên đánh giá.
	if e, err := OpenEval(ctx, dir, false); err != nil {
		fail(PendingSrcEval, err)
	} else if cands, err := e.Cases.Candidates(); err != nil {
		fail(PendingSrcEval, err)
	} else {
		for _, c := range cands {
			if c.Status == eval.StatusCandidate {
				rep.EvalCandidates = append(rep.EvalCandidates, c.ID)
			}
		}
	}

	// 6. Doctor: cảnh báo và lỗi.
	rep.DoctorAll = Doctor(ctx, dir)
	for _, c := range rep.DoctorAll.Checks {
		if c.Level == LevelWarn || c.Level == LevelFail {
			rep.Doctor = append(rep.Doctor, c)
		}
	}
	return rep, nil
}
