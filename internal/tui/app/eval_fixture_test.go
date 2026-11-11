package app

import (
	"context"
	"errors"
	"math"
	"time"

	bintapp "bint/internal/app"
	"bint/internal/config"
	"bint/internal/eval"
	elscreen "bint/internal/tui/screens/eval"
)

// mkEvalCase dựng một ứng viên/tác vụ mẫu (không có trên đĩa).
func mkEvalCase(id, status, split, subject, prompt string, verify string, leak string) *eval.Case {
	c := &eval.Case{Schema: eval.CaseSchemaVersion, ID: id, Status: status, Split: split,
		Source: eval.Source{Commit: id + "commit00", Base: id + "base0000", Author: "Dev", Date: "2026-11-10", Subject: subject},
		Prompt: prompt, TestFiles: []string{"internal/db/db_test.go"}, SourceFiles: []string{"internal/db/db.go"},
		Stats: eval.Stats{Files: 2, SourceLines: 30, TestLines: 20}, Leak: eval.LeakReport{Status: leak}}
	if verify != "" {
		c.Verification = &eval.Verification{Status: verify, FailBefore: true, PassAfter: verify == "verified", AfterRuns: 2, VerifiedAt: "2026-11-11T08:00:00Z"}
	}
	if leak == "fail" {
		c.Leak.Findings = []eval.LeakFinding{{Severity: "fail", Kind: "solution_code", Detail: "đề bài chứa mã của lời giải"}}
	}
	return c
}

// sampleEval là dữ liệu màn Đánh giá cố định: ba ứng viên (một đạt, một lộ lời giải chưa kiểm chứng, một đã loại), hai tác
// vụ đã duyệt và hai đợt (một xong, một đã huỷ).
func sampleEval() elscreen.Data {
	cands := []*eval.Case{
		mkEvalCase("c0001aaaa", eval.StatusCandidate, "", "Thêm cột email vào bảng người dùng", "Thêm cột `email` vào bảng người dùng và trả nó ở API.", "verified", "pass"),
		mkEvalCase("c0002bbbb", eval.StatusCandidate, "", "Sửa lỗi khoá cơ sở dữ liệu", "Sửa lỗi khoá: gọi `Close()` trong defer.", "", "fail"),
	}
	cases := []*eval.Case{
		mkEvalCase("c0003cccc", eval.StatusApproved, eval.SplitBuild, "Đổi cách tính phí", "Đổi cách tính phí theo bậc.", "verified", "pass"),
		mkEvalCase("c0004dddd", eval.StatusApproved, eval.SplitValidation, "Tách hàm Open", "Tách hàm `Open` khỏi `Init`.", "verified", "pass"),
	}
	done := eval.Batch{BatchID: "eb_01EVALDONE0000000000000001", Status: "completed", Harness: "command", LeakageMode: "economy", Arms: []string{"none", "bint"}, Repeats: 2,
		CaseIDs: []string{"c0003cccc", "c0004dddd"}, Seed: 7, StartedAt: "2026-11-11T09:00:00Z", FinishedAt: "2026-11-11T09:30:00Z"}
	stopped := eval.Batch{BatchID: "eb_01EVALSTOP0000000000000002", Status: "cancelled", Harness: "command", LeakageMode: "economy", Arms: []string{"none"}, Repeats: 3,
		CaseIDs: []string{"c0003cccc"}, Seed: 8, StartedAt: "2026-11-11T10:00:00Z"}
	return elscreen.Data{Config: config.Default(), Pending: cands, Cases: cases, Batches: []eval.Batch{stopped, done}, Stamp: time.Unix(1, 0),
		CasesDir: "/work/demo/.bint/evals/cases", CandidatesDir: "/state/demo/evals/candidates"}
}

// sampleEvalRejected là ứng viên đã loại (chỉ hiện khi bật A).
func sampleEvalRejected() *eval.Case {
	c := mkEvalCase("c0005eeee", eval.StatusRejected, "", "Đổi tên biến", "Đổi tên biến cho rõ.", "failed", "pass")
	c.RejectReason = "kiểm chứng: kiểm thử không thất bại ở commit cha"
	return c
}

func sampleEvalLoader(ctx context.Context, _ string, q elscreen.Query) (elscreen.Data, error) {
	d := sampleEval()
	if q.All {
		d.Pending = append(d.Pending, sampleEvalRejected())
	}
	return d, nil
}

func sampleEvalDetail(_ context.Context, _ string, id string) (*bintapp.EvalShowResult, error) {
	d := sampleEval()
	for _, c := range append(append([]*eval.Case{}, d.Pending...), append(d.Cases, sampleEvalRejected())...) {
		if c.ID == id {
			return &bintapp.EvalShowResult{Case: c, Approved: c.Status == eval.StatusApproved, Path: "/state/demo/evals/candidates/" + id + ".yml", TestCommand: "go test ./..."}, nil
		}
	}
	return nil, errors.New("không có tác vụ")
}

// sampleReport dựng tay báo cáo của đợt xong: hai nhánh (none 50%, bint 100%), so sánh cặp, hai tác vụ, một cảnh báo.
func sampleReport(baseline string) *eval.Report {
	nan := math.NaN()
	rep := &eval.Report{Batch: sampleEval().Batches[1]}
	rep.Arms = []eval.ArmSummary{
		{Arm: "none", Attempts: 4, Graded: 4, Passed: 2, PassRate: eval.Stat{Mean: 0.5, CI: eval.CI{Lo: 0.25, Hi: 0.75}, N: 2}, WithinCaseSD: ptr(0.35), MixedCases: 1, CasesWithRepeats: 2, Models: []string{"claude-opus-5-5"}},
		{Arm: "bint", Attempts: 4, Graded: 4, Passed: 4, PassRate: eval.Stat{Mean: 1, CI: eval.CI{Lo: nan, Hi: nan}, N: 2}},
	}
	rep.Paired = []eval.Paired{{Arm: "bint", Baseline: "none", PassDiff: eval.Stat{Mean: 0.5, CI: eval.CI{Lo: 0.1, Hi: 0.9}, N: 2}}}
	if baseline == "bint" {
		rep.Paired = []eval.Paired{{Arm: "none", Baseline: "bint", PassDiff: eval.Stat{Mean: -0.5, CI: eval.CI{Lo: -0.9, Hi: -0.1}, N: 2}}}
	}
	rep.Cases = []eval.CaseResult{
		{CaseID: "c0003cccc", Split: "build", Arms: map[string]eval.CaseArmCell{"none": {Passed: 1, Graded: 2}, "bint": {Passed: 2, Graded: 2}}},
		{CaseID: "c0004dddd", Split: "validation", Arms: map[string]eval.CaseArmCell{"none": {Passed: 1, Graded: 2, Other: 1}, "bint": {Passed: 2, Graded: 2}}},
	}
	rep.Diagnostics = []eval.Diagnostic{{Level: "warn", Kind: "leakage_economy", Detail: "chế độ economy: kết quả chỉ dùng để sàng lọc"}}
	return rep
}

func ptr(f float64) *float64 { return &f }

func sampleEvalReport(_ context.Context, _ string, id, baseline string) (*eval.Report, error) {
	if id != sampleEval().Batches[1].BatchID {
		return nil, errors.New("đợt chưa có lượt nào")
	}
	return sampleReport(baseline), nil
}
