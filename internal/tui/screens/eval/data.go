// Package eval là màn Đánh giá (docs/tui-plan.md mục 5.5): ứng viên (khai thác, kiểm chứng fail→pass, đặc tả, duyệt/loại),
// tác vụ đã duyệt, các đợt chạy với báo cáo có biểu đồ cột tỷ lệ đạt kèm khoảng tin cậy, và log của việc nặng
// (chạy đợt, dựng tri thức, kiểm chứng) kèm lưới tiến độ ô (tác vụ × nhánh × lần lặp). Mọi thao tác ghi đi qua
// actions.RequestMsg để mô hình gốc kiểm chế độ chỉ đọc theo sổ hành động.
package eval

import (
	"context"
	"time"

	"bint/internal/app"
	"bint/internal/config"
	"bint/internal/eval"
	"bint/internal/tui/actions"
)

// Query chọn dữ liệu cần nạp: All = gồm cả ứng viên đã loại (`eval list --all`).
type Query struct{ All bool }

// Data là dữ liệu của màn hình: ứng viên chờ duyệt, tác vụ đã duyệt và các đợt chạy.
type Data struct {
	Config  config.Config
	Pending []*eval.Case
	Cases   []*eval.Case
	Batches []eval.Batch
	// CasesDir, CandidatesDir là thư mục chứa tệp tác vụ.
	CasesDir, CandidatesDir string
	Stamp                   time.Time
}

// PendingIDs trả ID ứng viên (gợi ý cho form).
func (d Data) PendingIDs() []string {
	out := make([]string, len(d.Pending))
	for i, c := range d.Pending {
		out[i] = c.ID
	}
	return out
}

// CaseIDs trả ID mọi tác vụ đã duyệt (gợi ý cho form).
func (d Data) CaseIDs() []string {
	out := make([]string, len(d.Cases))
	for i, c := range d.Cases {
		out[i] = c.ID
	}
	return out
}

// AllIDs trả ID của ứng viên và tác vụ đã duyệt.
func (d Data) AllIDs() []string { return append(d.PendingIDs(), d.CaseIDs()...) }

// BatchIDs trả ID các đợt (gợi ý cho form).
func (d Data) BatchIDs() []string {
	out := make([]string, len(d.Batches))
	for i, b := range d.Batches {
		out[i] = b.BatchID
	}
	return out
}

// LoadedMsg là kết quả nạp dữ liệu.
type LoadedMsg struct {
	Data Data
	Err  error
}

// DetailMsg là kết quả nạp chi tiết một ứng viên hoặc tác vụ (đã tính lại rò rỉ trên đề bài hiện tại).
type DetailMsg struct {
	ID     string
	Detail *app.EvalShowResult
	Err    error
}

// ReportMsg là kết quả dựng báo cáo một đợt theo mốc Baseline.
type ReportMsg struct {
	BatchID, Baseline string
	Report            *eval.Report
	Err               error
}

// RefreshMsg yêu cầu nạp lại dữ liệu, chi tiết và báo cáo (việc nền vừa đổi dữ liệu).
type RefreshMsg struct{}

// Target cho mô hình gốc biết các thông điệp này thuộc màn Đánh giá.
func (LoadedMsg) Target() actions.Screen  { return actions.Eval }
func (DetailMsg) Target() actions.Screen  { return actions.Eval }
func (ReportMsg) Target() actions.Screen  { return actions.Eval }
func (RefreshMsg) Target() actions.Screen { return actions.Eval }

// Loader, Detailer, Reporter nạp dữ liệu, chi tiết một tác vụ và báo cáo một đợt. Thay được trong kiểm thử; nil = nạp thật.
type (
	Loader   func(ctx context.Context, dir string, q Query) (Data, error)
	Detailer func(ctx context.Context, dir, id string) (*app.EvalShowResult, error)
	Reporter func(ctx context.Context, dir, batchID, baseline string) (*eval.Report, error)
)

// Load đọc ứng viên, tác vụ đã duyệt và danh sách đợt.
func Load(ctx context.Context, dir string, q Query) (Data, error) {
	e, err := app.OpenEval(ctx, dir, true)
	if err != nil {
		return Data{}, err
	}
	defer e.Close()
	lr, err := app.EvalList(e, app.EvalListOptions{All: q.All})
	if err != nil {
		return Data{}, err
	}
	rr, err := app.EvalReport(ctx, e, app.EvalReportOptions{List: true})
	if err != nil {
		return Data{}, err
	}
	return Data{Config: e.WS.Config, Pending: lr.Pending, Cases: lr.Cases, Batches: rr.Batches,
		CasesDir: lr.CasesDir, CandidatesDir: lr.CandidatesDir, Stamp: time.Now()}, nil
}

// LoadDetail đọc chi tiết tác vụ id.
func LoadDetail(ctx context.Context, dir, id string) (*app.EvalShowResult, error) {
	e, err := app.OpenEval(ctx, dir, false)
	if err != nil {
		return nil, err
	}
	defer e.Close()
	return app.EvalShow(ctx, e, app.EvalShowOptions{ID: id})
}

// LoadReport dựng báo cáo của đợt batchID (rỗng = mới nhất) theo mốc baseline (rỗng = mặc định).
func LoadReport(ctx context.Context, dir, batchID, baseline string) (*eval.Report, error) {
	e, err := app.OpenEval(ctx, dir, true)
	if err != nil {
		return nil, err
	}
	defer e.Close()
	r, err := app.EvalReport(ctx, e, app.EvalReportOptions{BatchID: batchID, Baseline: baseline})
	if err != nil {
		return nil, err
	}
	return &r.Report, nil
}
