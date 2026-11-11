// Package runs là màn Lượt chạy (docs/tui-plan.md mục 5.4): danh sách lượt chạy với chi tiết (nhãn, tín hiệu, đầu ra,
// sự kiện), chấm good/bad kèm ghi chú, quét Git, nạp spool, và thẻ Bài học (thư viện bài học và bản tóm tắt thất bại).
// Mọi thao tác ghi đi qua actions.RequestMsg để mô hình gốc kiểm chế độ chỉ đọc/khoá theo sổ hành động.
package runs

import (
	"context"
	"time"

	"bint/internal/app"
	"bint/internal/app/evolvesvc"
	"bint/internal/config"
	"bint/internal/evolve"
	"bint/internal/runlog"
	"bint/internal/tui/actions"
)

// Data là danh sách lượt chạy (mới nhất trước, giới hạn Limit).
type Data struct {
	Rows  []app.RunRow
	Limit int
	Stamp time.Time
}

// IDs trả ID của các lượt (gợi ý cho form).
func (d Data) IDs() []string {
	out := make([]string, len(d.Rows))
	for i, r := range d.Rows {
		out[i] = r.Run.RunID
	}
	return out
}

// LoadedMsg là kết quả nạp danh sách.
type LoadedMsg struct {
	Data Data
	Err  error
}

// DetailMsg là kết quả nạp chi tiết một lượt.
type DetailMsg struct {
	ID     string
	Detail *app.RunDetail
	Err    error
}

// LessonsMsg là kết quả nạp thư viện bài học.
type LessonsMsg struct {
	Rows []evolve.LessonRow
	Err  error
}

// RefreshMsg yêu cầu nạp lại danh sách và chi tiết.
type RefreshMsg struct{}

// Target cho mô hình gốc biết các thông điệp này thuộc màn Lượt chạy.
func (LoadedMsg) Target() actions.Screen  { return actions.Runs }
func (DetailMsg) Target() actions.Screen  { return actions.Runs }
func (LessonsMsg) Target() actions.Screen { return actions.Runs }
func (RefreshMsg) Target() actions.Screen { return actions.Runs }

// Loader nạp danh sách; Detailer nạp chi tiết một lượt; LessonLoader nạp thư viện bài học. Thay được trong kiểm
// thử; nil = nạp thật. readOnly: không nạp spool vào cơ sở dữ liệu.
type (
	Loader       func(ctx context.Context, dir string, limit int, readOnly bool) (Data, error)
	Detailer     func(ctx context.Context, dir, id string, readOnly bool) (*app.RunDetail, error)
	LessonLoader func(ctx context.Context, dir string, readOnly bool) ([]evolve.LessonRow, error)
)

// Load đọc danh sách lượt chạy của repository chứa dir.
func Load(ctx context.Context, dir string, limit int, readOnly bool) (Data, error) {
	r, err := app.OpenRuns(ctx, dir, !readOnly)
	if err != nil {
		return Data{}, err
	}
	defer r.Close()
	o := app.RunsListDefaults(config.Default())
	if limit > 0 {
		o.Limit = limit
	}
	rows, err := app.ListRuns(ctx, r, o)
	if err != nil {
		return Data{}, err
	}
	return Data{Rows: rows, Limit: o.Limit, Stamp: time.Now()}, nil
}

// LoadDetail đọc chi tiết lượt id.
func LoadDetail(ctx context.Context, dir, id string, readOnly bool) (*app.RunDetail, error) {
	r, err := app.OpenRuns(ctx, dir, !readOnly)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return app.ShowRun(ctx, r, app.RunsShowOptions{Ref: id})
}

// LoadLessons đọc thư viện bài học (chung trước, riêng nhóm theo vùng).
func LoadLessons(ctx context.Context, dir string, readOnly bool) ([]evolve.LessonRow, error) {
	e, err := evolvesvc.OpenIngest(ctx, dir, !readOnly)
	if err != nil {
		return nil, err
	}
	defer e.Close()
	return evolvesvc.Experience(ctx, e, evolvesvc.ExperienceShowOptions{})
}

// Kết quả của hành động quét/nạp (kiểu của dịch vụ, đặt tên ngắn cho màn hình).
type (
	scanResult   = runlog.ScanResult
	ingestResult = runlog.IngestResult
)
