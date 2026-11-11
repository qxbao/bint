package app

import (
	"context"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/runlog"
	"bint/internal/spool"
	"bint/internal/store"
	"bint/internal/ulid"
)

// Dịch vụ lượt chạy dùng chung cho CLI và TUI (docs/tui-plan.md mục 7.3): `runs list/show/rate/scan` và
// `events ingest` có kiểu Options (thẻ `flag`/`arg`/`form`), Validate() và Defaults(cfg). CLI chỉ còn phân
// tích cờ và in; kiểm chỉ-dành-cho-người của `runs rate` nằm ở đây.

// RunsWS là không gian lượt chạy: workspace đã khởi tạo và cơ sở dữ liệu trạng thái đang mở.
type RunsWS struct {
	WS    *Workspace
	Store *store.Store
	// Ingest là kết quả nạp spool khi mở (rỗng nếu mở không nạp).
	Ingest runlog.IngestResult
}

// OpenRuns mở không gian lượt chạy của repository chứa dir. ingest = true nạp spool trước khi đọc để kết
// quả gồm các sự kiện mới nhất; false (chế độ chỉ đọc của TUI) không ghi gì vào cơ sở dữ liệu (mở cơ sở dữ
// liệu vẫn có thể áp migration nếu schema cũ). Người gọi phải Close.
func OpenRuns(ctx context.Context, dir string, ingest bool) (*RunsWS, error) {
	ws, err := OpenWorkspace(ctx, dir, true)
	if err != nil {
		return nil, err
	}
	s, err := ws.OpenStore(ctx)
	if err != nil {
		return nil, err
	}
	r := &RunsWS{WS: ws, Store: s}
	if ingest {
		if r.Ingest, err = ws.Ingest(ctx, s, time.Now()); err != nil {
			s.Close()
			return nil, err
		}
	}
	return r, nil
}

// Close đóng cơ sở dữ liệu.
func (r *RunsWS) Close() { r.Store.Close() }

// ---- runs list ----

// RunsListOptions: `bint runs list`. Việc nạp spool do OpenRuns quyết định (TUI chỉ đọc mở với ingest=false).
type RunsListOptions struct {
	Limit int `flag:"limit" form:""`
}

// RunsListDefaults: tối đa 20 lượt (như mặc định của cờ).
func RunsListDefaults(config.Config) RunsListOptions { return RunsListOptions{Limit: 20} }

func (o RunsListOptions) Validate() error { return nil }

// RunRow là một lượt chạy kèm nhãn kết quả và lý do (nhãn suy ra từ tín hiệu).
type RunRow struct {
	Run    runlog.Run
	Label  string
	Reason string
}

// ListRuns liệt kê lượt chạy, mới nhất trước, kèm nhãn và lý do của từng lượt.
func ListRuns(ctx context.Context, r *RunsWS, o RunsListOptions) ([]RunRow, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	runs, err := runlog.ListRuns(ctx, r.Store.DB, o.Limit)
	if err != nil {
		return nil, err
	}
	out := make([]RunRow, 0, len(runs))
	for _, run := range runs {
		sigs, err := runlog.RunSignals(ctx, r.Store.DB, run.RunID)
		if err != nil {
			return nil, err
		}
		label, why := runlog.Label(sigs)
		out = append(out, RunRow{Run: run, Label: label, Reason: why})
	}
	return out, nil
}

// ---- runs show ----

// RunsShowOptions: `bint runs show <run-id>` (chấp nhận tiền tố ID).
type RunsShowOptions struct {
	Ref string `arg:"run-id" form:"required,suggest=runs"`
}

func RunsShowDefaults(config.Config) RunsShowOptions { return RunsShowOptions{} }

func (o RunsShowOptions) Validate() error {
	if strings.TrimSpace(o.Ref) == "" {
		return Usagef("cần ID lượt chạy")
	}
	return nil
}

// RunDetail là chi tiết một lượt chạy: lượt, nhãn + lý do, tín hiệu kết quả, đầu ra và sự kiện.
type RunDetail struct {
	Run       runlog.Run
	Label     string
	Reason    string
	Signals   []runlog.Signal
	Artifacts []runlog.Artifact
	Events    []runlog.EventRow
}

// ShowRun đọc chi tiết một lượt chạy.
func ShowRun(ctx context.Context, r *RunsWS, o RunsShowOptions) (*RunDetail, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	run, err := runlog.GetRun(ctx, r.Store.DB, o.Ref)
	if err != nil {
		return nil, err
	}
	d := &RunDetail{Run: run}
	if d.Events, err = runlog.RunEvents(ctx, r.Store.DB, run.RunID); err != nil {
		return nil, err
	}
	if d.Artifacts, err = runlog.RunArtifacts(ctx, r.Store.DB, run.RunID); err != nil {
		return nil, err
	}
	if d.Signals, err = runlog.RunSignals(ctx, r.Store.DB, run.RunID); err != nil {
		return nil, err
	}
	d.Label, d.Reason = runlog.Label(d.Signals)
	return d, nil
}

// ---- runs rate ----

// Điểm hợp lệ của `runs rate`.
const (
	RatingGood = "good"
	RatingBad  = "bad"
)

// RunsRateOptions: `bint runs rate <run-id> good|bad`.
type RunsRateOptions struct {
	Ref    string `arg:"run-id" form:"required,suggest=runs"`
	Rating string `arg:"rating" form:"required,choices=good|bad"`
	Note   string `flag:"note" form:""`
}

func RunsRateDefaults(config.Config) RunsRateOptions { return RunsRateOptions{} }

// Validate: điểm phải là good hoặc bad (không phân biệt hoa thường).
func (o RunsRateOptions) Validate() error {
	if strings.TrimSpace(o.Ref) == "" {
		return Usagef("cần ID lượt chạy")
	}
	switch strings.ToLower(o.Rating) {
	case RatingGood, RatingBad:
		return nil
	}
	return Usagef("điểm phải là good hoặc bad")
}

// RateResult là kết quả chấm một lượt chạy.
type RateResult struct {
	RunID  string
	Rating string
}

// RateRun chấm kết quả một lượt chạy: ghi sự kiện `run.rated` vào spool rồi nạp (tín hiệu mạnh, mục 9.6).
// Thứ tự lỗi như CLI cũ: tham số sai (mã 2) trước, rồi chỉ-dành-cho-người, rồi mới mở workspace. Chấm lại thì
// lần sau ghi đè lần trước. Chỉ dành cho người: agent không tự chấm mình.
func RateRun(ctx context.Context, dir string, o RunsRateOptions) (RateResult, error) {
	var res RateResult
	if err := o.Validate(); err != nil {
		return res, err
	}
	if err := RequireHuman("bint runs rate"); err != nil {
		return res, err
	}
	r, err := OpenRuns(ctx, dir, true)
	if err != nil {
		return res, err
	}
	defer r.Close()
	run, err := runlog.GetRun(ctx, r.Store.DB, o.Ref)
	if err != nil {
		return res, err
	}
	rating := strings.ToLower(o.Rating)
	now := time.Now()
	ev := spool.Event{SchemaVersion: spool.SchemaVersion, EventID: "ev_rate_" + ulid.New(), Type: spool.RunRated,
		Source: "cli", Timestamp: now.UTC(), RunID: run.RunID,
		Payload: map[string]any{"rating": rating, "note": o.Note, "by": GitIdentity(ctx, r.WS.Repo.Toplevel)}}
	if _, err := spool.Write(r.WS.Paths.Spool, ev); err != nil {
		return res, err
	}
	if _, err := r.WS.Ingest(ctx, r.Store, now); err != nil {
		return res, err
	}
	return RateResult{RunID: run.RunID, Rating: rating}, nil
}

// ---- runs scan ----

// RunsScanOptions: `bint runs scan`.
type RunsScanOptions struct {
	Days       int `flag:"days" form:""`
	RevertDays int `flag:"revert-days" form:""`
}

// RunsScanDefaults: xét 30 ngày gần nhất, hoàn tác trong 14 ngày sau commit.
func RunsScanDefaults(config.Config) RunsScanOptions {
	return RunsScanOptions{Days: 30, RevertDays: 14}
}

func (o RunsScanOptions) Validate() error { return nil }

// ScanRuns quét lịch sử Git: gắn commit với lượt chạy và phát hiện thay đổi bị hoàn tác (ghi tín hiệu).
func ScanRuns(ctx context.Context, r *RunsWS, o RunsScanOptions) (runlog.ScanResult, error) {
	if err := o.Validate(); err != nil {
		return runlog.ScanResult{}, err
	}
	return runlog.ScanGit(ctx, r.Store.DB, r.WS.Repo.Toplevel, runlog.ScanOptions{
		Lookback: time.Duration(o.Days) * 24 * time.Hour, RevertDays: o.RevertDays})
}

// ---- events ingest ----

// EventsIngestOptions: `bint events ingest` (không có tham số).
type EventsIngestOptions struct{}

func EventsIngestDefaults(config.Config) EventsIngestOptions { return EventsIngestOptions{} }
func (o EventsIngestOptions) Validate() error                { return nil }

// IngestEvents nạp sự kiện từ spool vào cơ sở dữ liệu (loại trùng theo event_id). Mở với ingest = true đã nạp;
// kết quả là của lần nạp đó.
func IngestEvents(_ context.Context, r *RunsWS, o EventsIngestOptions) (runlog.IngestResult, error) {
	if err := o.Validate(); err != nil {
		return runlog.IngestResult{}, err
	}
	return r.Ingest, nil
}
