package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"bint/internal/mcpserver"
	"bint/internal/runlog"
	"bint/internal/spool"
	"bint/internal/task"
	"bint/internal/workflow"
)

// Lịch sử lượt chạy theo đường dẫn cho agent (mục B4): hai công cụ MCP history_for_paths và run_get, dựng xác định từ
// SQLite (lượt chạy) và tệp phiếu (phát hiện review). Không gọi mô hình, không ghi tri thức hay phiếu; chỉ nạp
// spool (idempotent, giống các lệnh đọc của CLI). Chạy trong MCP server, không trong hook.

var _ mcpserver.HistoryBackend = (*RepoBackend)(nil)

// historyIngest nạp spool để lượt vừa chạy cũng hiện; lỗi nạp không làm hỏng truy vấn.
func (b *RepoBackend) historyIngest(ctx context.Context, c *ContextWS) {
	_, _ = c.K.Ingest(ctx, c.Store, time.Now())
}

// historyOpen trả ContextWS dùng được, hoặc nil khi repository chưa có dữ liệu lượt chạy trên máy (chỉ mục trong bộ nhớ).
func (b *RepoBackend) historyOpen(ctx context.Context) (*ContextWS, error) {
	c, err := b.engine(ctx)
	if err != nil {
		return nil, err
	}
	if c.Memory {
		return nil, nil
	}
	b.historyIngest(ctx, c)
	return c, nil
}

// taskFindings nạp phát hiện của phiếu (có bộ nhớ đệm theo lời gọi). Phiếu không có hoặc hỏng cho danh sách rỗng.
func (b *RepoBackend) taskFindings(c *ContextWS, cache map[string][]workflow.FindingView, taskID string) []workflow.FindingView {
	if taskID == "" {
		return nil
	}
	if v, ok := cache[taskID]; ok {
		return v
	}
	var views []workflow.FindingView
	if t, err := taskStore(c.K.Workspace).Load(taskID); err == nil {
		if v, err := workflow.ViewFindings(t); err == nil {
			views = v
		}
	}
	cache[taskID] = views
	return views
}

func findingInfo(v workflow.FindingView) mcpserver.FindingInfo {
	return mcpserver.FindingInfo{ID: v.Finding.ID, Severity: v.Finding.Severity, Outcome: v.Outcome.Outcome,
		Title: v.Finding.Title, Path: v.Finding.Path}
}

func findingMatches(f task.Finding, paths []string) bool {
	p := strings.Trim(strings.ReplaceAll(f.Path, "\\", "/"), "/")
	if p == "" {
		return false
	}
	for _, q := range paths {
		q = strings.Trim(strings.ReplaceAll(q, "\\", "/"), "/")
		if q == "" || q == "." || p == q || strings.HasPrefix(p, q+"/") {
			return true
		}
	}
	return false
}

func (b *RepoBackend) maxRuns(c *ContextWS) int {
	n := c.K.Config.Context.History.MaxRuns
	if n < 1 {
		n = 10
	}
	return n
}

// HistoryForPaths trả các lượt chạy đã sửa (hoặc đọc) đường dẫn, mới nhất trước.
func (b *RepoBackend) HistoryForPaths(ctx context.Context, q mcpserver.HistoryQuery) (mcpserver.HistoryResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, err := b.historyOpen(ctx)
	if err != nil {
		return mcpserver.HistoryResult{}, err
	}
	if c == nil {
		return mcpserver.HistoryResult{NoData: true}, nil
	}
	paths := relPaths(c.K.Root, q.Paths)
	if len(paths) == 0 {
		return mcpserver.HistoryResult{}, fmt.Errorf("paths phải là tệp hoặc thư mục trong repository")
	}
	limit := q.Limit
	if limit <= 0 {
		limit = b.maxRuns(c)
	}
	if limit > mcpserver.MaxHistoryLimit {
		limit = mcpserver.MaxHistoryLimit
	}
	kinds := []string{runlog.ArtifactModified}
	if q.IncludeReads {
		kinds = append(kinds, runlog.ArtifactRead)
	}
	touches, err := runlog.RunsTouching(ctx, c.Store.DB, paths, kinds, limit)
	if err != nil {
		return mcpserver.HistoryResult{}, err
	}
	cache := map[string][]workflow.FindingView{}
	res := mcpserver.HistoryResult{Runs: make([]mcpserver.RunBrief, 0, len(touches))}
	for _, t := range touches {
		sigs, err := runlog.RunSignals(ctx, c.Store.DB, t.RunID)
		if err != nil {
			return mcpserver.HistoryResult{}, err
		}
		label, _ := runlog.Label(sigs)
		failed, err := runlog.FailedTestCommands(ctx, c.Store.DB, t.RunID)
		if err != nil {
			return mcpserver.HistoryResult{}, err
		}
		if len(failed) > mcpserver.MaxFailedTestsRun {
			failed = failed[:mcpserver.MaxFailedTestsRun]
		}
		br := mcpserver.RunBrief{RunID: t.RunID, StartedAt: t.StartedAt, TaskID: t.TaskID, Workflow: t.Workflow,
			Harness: t.Harness, Model: t.Model, Label: label, Files: t.Files, FilesTotal: len(t.Files),
			FailedTests: failed, Findings: []mcpserver.FindingInfo{}}
		if len(br.Files) > mcpserver.MaxFilesPerRun {
			br.Files = br.Files[:mcpserver.MaxFilesPerRun]
		}
		if br.FailedTests == nil {
			br.FailedTests = []string{}
		}
		for _, v := range b.taskFindings(c, cache, t.TaskID) {
			if !findingMatches(v.Finding, paths) {
				continue
			}
			br.FindingsTotal++
			if len(br.Findings) < mcpserver.MaxFindingsPerRun {
				br.Findings = append(br.Findings, findingInfo(v))
			}
		}
		res.Runs = append(res.Runs, br)
	}
	return res, nil
}

// RunGet trả chi tiết một lượt chạy theo ID hoặc tiền tố.
func (b *RepoBackend) RunGet(ctx context.Context, ref string) (mcpserver.RunInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, err := b.historyOpen(ctx)
	if err != nil {
		return mcpserver.RunInfo{}, err
	}
	if c == nil {
		return mcpserver.RunInfo{NoData: true}, nil
	}
	run, err := runlog.GetRun(ctx, c.Store.DB, ref)
	if err != nil { // ErrRunNotFound/ErrAmbiguous thành lỗi công cụ (isError) ở mcpserver
		return mcpserver.RunInfo{}, err
	}
	meta, err := runlog.GetRunMeta(ctx, c.Store.DB, run.RunID)
	if err != nil {
		return mcpserver.RunInfo{}, err
	}
	sigs, err := runlog.RunSignals(ctx, c.Store.DB, run.RunID)
	if err != nil {
		return mcpserver.RunInfo{}, err
	}
	label, reason := runlog.Label(sigs)
	arts, err := runlog.RunArtifacts(ctx, c.Store.DB, run.RunID)
	if err != nil {
		return mcpserver.RunInfo{}, err
	}
	events, err := runlog.RunEvents(ctx, c.Store.DB, run.RunID)
	if err != nil {
		return mcpserver.RunInfo{}, err
	}
	info := mcpserver.RunInfo{RunID: run.RunID, Status: run.Status, Label: label, LabelReason: reason,
		TaskID: meta.TaskID.String, Workflow: run.Workflow.String, BaseCommit: run.GitBaseCommit.String,
		Harness: run.Harness.String, Model: run.Model.String, StartedAt: run.StartedAt.String,
		Modified: []string{}, Read: []string{}, Tests: []mcpserver.TestInfo{}, Signals: []mcpserver.SignalInfo{},
		Findings: []mcpserver.FindingInfo{}}
	for _, a := range arts {
		switch a.Kind {
		case runlog.ArtifactModified:
			info.ModifiedTotal++
			if len(info.Modified) < 2*mcpserver.MaxFilesPerRun {
				info.Modified = append(info.Modified, a.Path)
			}
		case runlog.ArtifactRead:
			info.ReadTotal++
			if len(info.Read) < 2*mcpserver.MaxFilesPerRun {
				info.Read = append(info.Read, a.Path)
			}
		}
	}
	for _, e := range events {
		if e.Type != spool.TestCompleted {
			continue
		}
		var p struct {
			Command string `json:"command"`
			OK      bool   `json:"ok"`
		}
		if json.Unmarshal([]byte(e.Payload), &p) != nil {
			continue
		}
		if len(info.Tests) < 10 {
			info.Tests = append(info.Tests, mcpserver.TestInfo{Command: p.Command, OK: p.OK})
		}
	}
	for _, s := range sigs {
		info.Signals = append(info.Signals, mcpserver.SignalInfo{Kind: s.Kind, Positive: s.Positive, Strength: s.Strength, Detail: s.Detail})
	}
	for _, v := range b.taskFindings(c, map[string][]workflow.FindingView{}, info.TaskID) {
		info.FindingsTotal++
		if len(info.Findings) < 2*mcpserver.MaxFindingsPerRun {
			info.Findings = append(info.Findings, findingInfo(v))
		}
	}
	return info, nil
}
