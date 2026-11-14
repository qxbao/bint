package app

import (
	"context"
	"errors"
	"strings"

	"bint/internal/config"
	"bint/internal/task"
	"bint/internal/workflow"
)

// Dịch vụ kết cục phát hiện review (B1 phần 1): `bint findings list|set`. Kết cục nằm trong findings.json của
// từng tác vụ (task.OutcomeFile), không sửa review.json.

// FindingsListOptions: `bint findings list [--open] [--task <id>]`.
type FindingsListOptions struct {
	Open bool   `flag:"open"`
	Task string `flag:"task" form:"suggest=tasks"`
}

func FindingsListDefaults(config.Config) FindingsListOptions { return FindingsListOptions{} }
func (o FindingsListOptions) Validate() error                { return nil }

// FindingsList là danh sách phát hiện qua mọi tác vụ (kể cả done/cancelled) và lỗi đọc từng tác vụ.
type FindingsList struct {
	Findings []workflow.FindingView
	Bad      []error
}

// ListFindings liệt kê phát hiện; Open chỉ giữ phát hiện có kết cục hiệu lực open. Tác vụ có review.json hoặc
// findings.json hỏng vào Bad, không làm lệnh thất bại.
func ListFindings(_ context.Context, ws *TaskWS, o FindingsListOptions) (FindingsList, error) {
	var res FindingsList
	var ts []*task.Task
	if strings.TrimSpace(o.Task) != "" {
		t, err := ws.Tasks.Load(o.Task)
		if err != nil {
			return res, UsageError{err}
		}
		ts = []*task.Task{t}
	} else {
		var bad []error
		ts, bad = ws.Tasks.List()
		res.Bad = append(res.Bad, bad...)
	}
	c := CollectFindings(ts, o.Open)
	res.Findings = c.Findings
	res.Bad = append(res.Bad, c.Bad...)
	return res, nil
}

// CollectFindings gom phát hiện của ts (open: chỉ phát hiện có kết cục hiệu lực open); tác vụ hỏng vào Bad.
// Dùng chung cho ListFindings và màn Tác vụ của TUI (đếm phát hiện open).
func CollectFindings(ts []*task.Task, open bool) FindingsList {
	var res FindingsList
	// List trả mới nhất trước; phát hiện hiển thị theo thứ tự cũ → mới trong từng tác vụ, tác vụ mới trước.
	for _, t := range ts {
		views, err := workflow.ViewFindings(t)
		if err != nil {
			res.Bad = append(res.Bad, err)
			continue
		}
		for _, v := range views {
			if open && v.Outcome.Outcome != task.OutcomeOpen {
				continue
			}
			res.Findings = append(res.Findings, v)
		}
	}
	return res
}

// FindingsSetOptions: `bint findings set <kết-cục> <id-phát-hiện>...`.
type FindingsSetOptions struct {
	Outcome string   `arg:"outcome" form:"required,choices=open|fixed|rejected|deferred"`
	IDs     []string `arg:"finding-id"`
	Commit  string   `flag:"commit"`
	Reason  string   `flag:"reason"`
}

func FindingsSetDefaults(config.Config) FindingsSetOptions { return FindingsSetOptions{} }

// Validate kiểm đầu vào không cần đọc dữ liệu (commit có thật và ID có tồn tại do SetFindings kiểm).
func (o FindingsSetOptions) Validate() error {
	if !task.ValidOutcome(strings.ToLower(strings.TrimSpace(o.Outcome))) {
		return Usagef("kết cục %q không hợp lệ (%s)", o.Outcome, strings.Join(task.FindingOutcomes, " | "))
	}
	if len(o.IDs) == 0 {
		return Usagef("cần ít nhất một ID phát hiện (dạng <task-id>.<n>)")
	}
	switch strings.ToLower(strings.TrimSpace(o.Outcome)) {
	case task.OutcomeFixed:
		if strings.TrimSpace(o.Commit) == "" {
			return Usagef("fixed cần --commit trỏ tới commit đã sửa")
		}
	case task.OutcomeRejected, task.OutcomeDeferred:
		if strings.TrimSpace(o.Reason) == "" {
			return Usagef("%s cần --reason", strings.ToLower(strings.TrimSpace(o.Outcome)))
		}
	}
	return nil
}

// SetFindings đặt kết cục; lỗi đầu vào (ID không có, commit không có thật) là lỗi cách dùng và không ghi gì.
func SetFindings(ctx context.Context, ws *TaskWS, o FindingsSetOptions) ([]task.OutcomeRecord, error) {
	if err := RequireHuman("bint findings set"); err != nil {
		return nil, err
	}
	if err := o.Validate(); err != nil {
		return nil, err
	}
	recs, err := ws.SetFindingOutcome(ctx, o.IDs, o.Outcome, o.Commit, o.Reason)
	if errors.Is(err, workflow.ErrFindingInput) {
		return nil, UsageError{err}
	}
	return recs, err
}
