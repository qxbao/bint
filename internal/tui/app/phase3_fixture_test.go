package app

import (
	"context"
	"database/sql"
	"errors"
	"time"

	bintapp "bint/internal/app"
	"bint/internal/app/evolvesvc"
	"bint/internal/config"
	"bint/internal/eval"
	"bint/internal/evolve"
	"bint/internal/hcomp"
	"bint/internal/knowledge"
	"bint/internal/runlog"
	evscreen "bint/internal/tui/screens/evolve"
	rnscreen "bint/internal/tui/screens/runs"
)

func ns(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }

// sampleRuns là danh sách lượt chạy cố định: một thành công, một thất bại, một chưa có nhãn.
func sampleRuns() rnscreen.Data {
	mk := func(id, status, harness, started, label, why string, ok, bad int) bintapp.RunRow {
		return bintapp.RunRow{Label: label, Reason: why, Run: runlog.Run{RunID: id, Status: status, Harness: ns(harness), Model: ns("claude-opus-5-5"),
			GitBranch: ns("main"), GitBaseCommit: ns("46e924d"), StartedAt: ns(started), FinishedAt: ns(started), Events: 5, FilesModified: 2,
			TestsOK: ok, TestsFailed: bad, DurationMS: sql.NullInt64{Int64: 125000, Valid: true}, EndReason: ns("exit")}}
	}
	return rnscreen.Data{Limit: 20, Stamp: time.Unix(1, 0), Rows: []bintapp.RunRow{
		mk("01RUN000003", "completed", "claude", "2026-11-11T10:00:00.000Z", runlog.LabelUnlabeled, "không có tín hiệu đủ mạnh", 0, 0),
		mk("01RUN000002", "failed", "codex", "2026-11-11T09:00:00.000Z", runlog.LabelFailure, "người dùng chấm bad", 1, 2),
		mk("01RUN000001", "completed", "claude", "2026-11-11T08:00:00.000Z", runlog.LabelSuccess, "người dùng chấm good", 3, 0),
	}}
}

func sampleRunDetail(_ context.Context, _ string, id string, _ bool) (*bintapp.RunDetail, error) {
	for _, r := range sampleRuns().Rows {
		if r.Run.RunID == id {
			return &bintapp.RunDetail{Run: r.Run, Label: r.Label, Reason: r.Reason,
				Signals:   []runlog.Signal{{Kind: "rating", Positive: r.Label == runlog.LabelSuccess, Strength: "strong", Source: "human", Detail: "ghi chú của người chấm"}},
				Artifacts: []runlog.Artifact{{Kind: "file.modified", Path: "internal/db/db.go", Count: 2}},
				Events: []runlog.EventRow{{EventID: "e1", Type: "session.started", Source: "hook:claude", Timestamp: "2026-11-11T08:00:00.000Z", Payload: `{"model":"claude-opus-5-5"}`},
					{EventID: "e2", Type: "file.modified", Source: "hook:claude", Timestamp: "2026-11-11T08:01:00.000Z", Payload: `{"path":"internal/db/db.go"}`}}}, nil
		}
	}
	return nil, errors.New("không có lượt")
}

func sampleLessons(context.Context, string, bool) ([]evolve.LessonRow, error) {
	return []evolve.LessonRow{
		{ID: "01LESS000001", Title: "Gọi Reset trước Retry", Status: "verified", Provenance: "failure_lesson", Runs: 3, Inserted: 7, RealSuccess: 4, RealFailure: 1, EvalPass: 5, EvalFail: 2},
		{ID: "01LESS000002", Title: "Đóng kết nối sau truy vấn", Status: "candidate", Area: "internal/db/**", Scope: []string{"internal/db/**"}, Provenance: "failure_lesson", Runs: 2, Inserted: 1},
	}, nil
}

// sampleProposals dựng các đề xuất mẫu: tri thức (create), skill, và bốn loại thành phần quy trình.
func sampleEvolveProposals() []*evolve.Proposal {
	stamp := "2026-11-11T10:00:00.000Z"
	base := func(id string) *evolve.Proposal {
		return &evolve.Proposal{Schema: evolve.ProposalSchema, ID: id, Level: evolve.LevelL1, TargetType: "knowledge", Action: evolve.ActionCreate,
			Opportunity: "op-demo", Reason: "Thất bại lặp lại ở internal/db", Generator: "agent:fake", ExpectedBenefit: "bớt thất bại lặp lại", Risk: evolve.RiskLow, Status: evolve.StatusProposed, CreatedAt: stamp,
			UpdatedAt: stamp, Plan: evolve.Plan{Kind: "superiority", Metric: "pass_rate", Hypothesis: "bớt thất bại"}, Quantitative: false,
			Base: []evolve.BaseVersion{}, SupportingRuns: []evolve.RunRef{{RunID: "01RUN000002", Label: "failure"}}}
	}
	know := base("01EP0000KNOW")
	know.Changes = []evolve.Change{{Op: evolve.ActionUpdate, ItemID: "01KNOW000003", Reason: "làm rõ", Draft: &evolve.Draft{Type: "pattern", Title: "Handler đi qua wrap() và log",
		Body: "Nội dung của mục Handler đi qua wrap().\nDòng thứ hai đã sửa.", Scope: knowledge.Scope{Paths: []string{"internal/api/**"}}, Evidence: []string{"internal/db/db.go:40-52"}}}}
	know.Action, know.TargetIDs, know.Risk, know.RiskReasons = evolve.ActionUpdate, []string{"01KNOW000003"}, evolve.RiskHigh, []string{"sửa mục đã duyệt"}
	skill := base("01EP0000SKIL")
	skill.TargetType, skill.Level, skill.Changes = evolve.TargetSkill, evolve.LevelL2, nil
	skill.SkillChanges = []evolve.SkillChange{{Op: evolve.ActionCreate, Name: "db-retry", Draft: &evolve.SkillDraft{Name: "db-retry", Description: "Thử lại truy vấn DB", Body: "# db-retry\n\nGọi Reset trước Retry.\n",
		Evidence: []string{"internal/db/db.go:3-4"}}}}
	mkh := func(id string, ch evolve.HarnessChange) *evolve.Proposal {
		p := base(id)
		p.TargetType, p.Level, p.Changes, p.HarnessChanges, p.Risk = evolve.TargetHarness, evolve.LevelL2, nil, []evolve.HarnessChange{ch}, evolve.RiskMedium
		return p
	}
	guidance := mkh("01EP0000GUID", evolve.HarnessChange{Op: evolve.ActionCreate, Kind: hcomp.KindGuidance, ID: "chay-test",
		Draft: &evolve.HarnessDraft{Kind: hcomp.KindGuidance, ID: "chay-test", Scope: "global", Title: "Chạy test trước khi kết thúc", Body: "# Chạy test\n\n- Chạy `go test ./...` trước khi dừng.\n"}})
	guardrail := mkh("01EP0000GUAR", evolve.HarnessChange{Op: evolve.ActionCreate, Kind: hcomp.KindGuardrail, ID: "khong-sua-migrations",
		Draft: &evolve.HarnessDraft{Kind: hcomp.KindGuardrail, ID: "khong-sua-migrations", Scope: "global", On: hcomp.OnPreEdit, When: hcomp.When{Edited: "migrations/**"}, Action: hcomp.ActAsk,
			Message: "Sửa migration cần người duyệt."}})
	script := mkh("01EP0000SCRI", evolve.HarnessChange{Op: evolve.ActionCreate, Kind: hcomp.KindScript, ID: "kiem-tra-nhanh",
		Draft: &evolve.HarnessDraft{Kind: hcomp.KindScript, ID: "kiem-tra-nhanh", Scope: "global", Description: "Kiểm tra nhanh", Usage: "bash kiem-tra-nhanh.sh", Script: "#!/bin/sh\ngo vet ./...\n"}})
	script.Risk = evolve.RiskHigh
	strategy := mkh("01EP0000STRA", evolve.HarnessChange{Op: evolve.ActionUpdate, Kind: hcomp.KindStrategy, ID: "strategy",
		Draft: &evolve.HarnessDraft{Kind: hcomp.KindStrategy, ID: "strategy", Scope: "global", Context: &hcomp.StrategyContext{}}})
	return []*evolve.Proposal{know, skill, guidance, guardrail, script, strategy}
}

func sampleEvolve() evscreen.Data {
	rows := []evolvesvc.ListRow{}
	for i, p := range sampleEvolveProposals() {
		r := evolvesvc.ListRow{Proposal: p, State: p.Status}
		if i == 0 {
			ind := true
			r.Evaluations = []evolve.Evaluation{{ID: "ee_1", Split: eval.SplitValidation, Verdict: "supported", Independent: &ind}}
		}
		rows = append(rows, r)
	}
	scan := &evolve.ScanResult{Since: "2026-10-12T00:00:00Z", Runs: 12, Labeled: 9, ReadData: 6,
		Groups: []evolve.Group{{Module: "internal/db", Runs: 6, Labeled: 5, Success: 2, Failure: 3, Workflows: map[string]int{"b-do": 4, "b-feat": 2}}},
		Opportunities: []evolve.Opportunity{
			{ID: "op-3f9a2c1b", Kind: "failure_hotspot", Action: "create", Generator: "agent", Summary: "internal/db bị sửa ở 3 lượt thất bại", Ready: true,
				Files: []evolve.FileStat{{Path: "internal/db/db.go", Runs: 3, Failures: 3}}},
			{ID: "op-7e1d4a90", Kind: "unused", Action: "deprecate", Generator: "deterministic", Summary: "Mục 01KNOW000003 truy xuất nhiều, chưa từng trích dẫn", Blocked: "chưa đủ lượt có nhãn"},
		},
		Prerequisites: evolve.Prerequisites{Quantitative: false, Checks: []evolve.Check{{Name: "tác vụ đánh giá", OK: false, Detail: "0 tác vụ đã duyệt (cần ≥ 20)"}, {Name: "lượt có nhãn", OK: true, Detail: "9"}}}}
	return evscreen.Data{Config: config.Default(), Rows: rows, Scan: scan, Stamp: time.Unix(1, 0),
		History: &evolvesvc.HistoryResult{Entries: []evolve.HistoryEntry{{Adoption: evolve.Adoption{ID: "ead_1", ProposalID: "01EP0000KNOW", Action: "apply", Actor: "Rev <rev@example.com>", CreatedAt: "2026-11-10T10:00:00.000Z", Reason: "đã xem"},
			ProposalAction: "update", Items: []string{"01KNOW000003"}, RetrievedSince: 4, CitedSince: 1}}},
		Queue: &evolvesvc.QueueResult{Execution: "manual", Requests: []evolve.Request{{ID: "er_1", Kind: "failure_hotspot", Summary: "internal/db bị sửa ở 3 lượt thất bại", Status: evolve.ReqPending, CreatedAt: "2026-11-11T09:00:00.000Z"}}}}
}

func sampleEvolveDetail(_ context.Context, _ string, id string, _ bool) (*evolvesvc.ShowResult, error) {
	for _, p := range sampleEvolveProposals() {
		if p.ID != id {
			continue
		}
		res := &evolvesvc.ShowResult{Proposal: p, State: p.Status, Cases: map[string]*eval.Case{}}
		res.Items = []*knowledge.Item{{ID: "01KNOW000003", Type: "pattern", Status: "verified", Title: "Handler đi qua wrap()", Body: "Nội dung của mục Handler đi qua wrap().\nDòng thứ hai.",
			Scope: knowledge.Scope{Paths: []string{"internal/api/**"}}, Evidence: []knowledge.Evidence{{Path: "internal/db/db.go", Lines: "40-52"}}}}
		if p.ID == "01EP0000KNOW" {
			ind := true
			res.Evaluations = []evolve.Evaluation{{ID: "ee_1", ProposalID: p.ID, BatchID: "eb_01BATCH", Split: eval.SplitValidation, Delivery: "bint", CaseIDs: []string{"c1", "c2"},
				Verdict: "supported", Independent: &ind, Detail: "đạt 2/2, không kém base", BaseKey: p.BaseKey()}}
		}
		return res, nil
	}
	return nil, errors.New("không có đề xuất")
}

// sampleDigest là bản tóm tắt thất bại mẫu của một đợt: hai lượt thất bại và hai nhóm (một sẵn sàng, một chưa).
func sampleDigest() *evolvesvc.DigestResult {
	return &evolvesvc.DigestResult{MinUnits: 2, Report: evolve.DigestReport{Ref: "eb_01BATCH", Kind: "batch", Skipped: 1,
		Digests: []evolve.Digest{{ID: "case-1", Unit: "case-1", Area: "internal/db", Text: "Tác vụ case-1 thất bại ở internal/db\n  tín hiệu: test_failed", Tokens: 42},
			{ID: "case-2", Unit: "case-2", Area: "internal/db", Text: "Tác vụ case-2 thất bại ở internal/db", Tokens: 30}},
		Groups: []evolve.GroupSummary{{Key: "internal/db", Group: "internal/db", Summary: "2 tác vụ", Ready: true},
			{Key: "internal/api", Group: "internal/api", Summary: "1 tác vụ", Blocked: "cần ≥ 2 tác vụ phân biệt"}}}}
}

func samplePlanApply(p *evolve.Proposal) *evolvesvc.ApplyPlan {
	return &evolvesvc.ApplyPlan{Proposal: p, State: p.Status, Verified: false, NeedsUnverified: true, Risk: evolve.RiskHigh,
		RiskReasons: []string{"sửa mục đã duyệt"}, NeedsConfirmRisk: true, Changes: evolvesvc.ChangeNames(p)}
}

func samplePlanRollback(p *evolve.Proposal) *evolvesvc.RollbackPlan {
	before, applied, cur := "trước\n", "sau\n", "sau\nsửa tay\n"
	return &evolvesvc.RollbackPlan{Proposal: p, Adoption: evolve.Adoption{ID: "ead_1", Actor: "Rev <rev@example.com>"},
		Files: []evolvesvc.RollbackFile{{Path: ".bint/knowledge/items/01KNOW000003.md", Before: &before, Applied: &applied, Current: &cur, Conflict: true},
			{Path: ".bint/knowledge/items/01KNOW000004.md", Before: &before, Applied: &applied, Current: &applied}},
		Conflicts: []string{".bint/knowledge/items/01KNOW000003.md"}}
}
