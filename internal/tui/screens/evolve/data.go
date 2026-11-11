// Package evolve là màn Tiến hoá (docs/tui-plan.md mục 5.6): cơ hội (kết quả scan), đề xuất (danh sách, chi tiết với
// diff so với hiện trạng, khung riêng cho đề xuất quy trình, kết quả đối chiếu), lịch sử và hàng đợi; propose/
// evaluate/process/cycle chạy nền có log trực tiếp; áp dụng, bác bỏ, quay lui qua hộp xác nhận dựng từ hàm chỉ đọc
// PlanApply/PlanRollback. Mọi thao tác ghi đi qua actions.RequestMsg để mô hình gốc kiểm chế độ chỉ đọc/khoá.
package evolve

import (
	"context"
	"time"

	"bint/internal/app/evolvesvc"
	"bint/internal/config"
	"bint/internal/evolve"
	"bint/internal/tui/actions"
)

// Query chọn dữ liệu cần nạp: All = mọi trạng thái của đề xuất (`evolve list --all`), QueueAll = cả yêu cầu đã xử lý
// (`evolve queue --all`).
type Query struct{ All, QueueAll bool }

// Data là dữ liệu của màn hình: đề xuất, lịch sử, hàng đợi và kết quả quét gần nhất (không quét Git khi nạp).
type Data struct {
	Config  config.Config
	Rows    []evolvesvc.ListRow
	History *evolvesvc.HistoryResult
	Queue   *evolvesvc.QueueResult
	Scan    *evolve.ScanResult
	ScanErr error
	Stamp   time.Time
}

// IDs trả ID của các đề xuất đang nạp (gợi ý cho form).
func (d Data) IDs() []string {
	out := make([]string, len(d.Rows))
	for i, r := range d.Rows {
		out[i] = r.Proposal.ID
	}
	return out
}

// Opportunities trả các cơ hội của lần quét gần nhất.
func (d Data) Opportunities() []evolve.Opportunity {
	if d.Scan == nil {
		return nil
	}
	return d.Scan.Opportunities
}

// OpportunityIDs trả ID các cơ hội (gợi ý cho form propose).
func (d Data) OpportunityIDs() []string {
	var out []string
	for _, o := range d.Opportunities() {
		out = append(out, o.ID)
	}
	return out
}

// LoadedMsg là kết quả nạp dữ liệu.
type LoadedMsg struct {
	Data Data
	Err  error
}

// DetailMsg là kết quả nạp chi tiết một đề xuất.
type DetailMsg struct {
	ID     string
	Detail *evolvesvc.ShowResult
	Err    error
}

// RefreshMsg yêu cầu nạp lại dữ liệu và chi tiết (việc nền vừa đổi đề xuất).
type RefreshMsg struct{}

// Target cho mô hình gốc biết các thông điệp này thuộc màn Tiến hoá.
func (LoadedMsg) Target() actions.Screen  { return actions.Evolve }
func (DetailMsg) Target() actions.Screen  { return actions.Evolve }
func (RefreshMsg) Target() actions.Screen { return actions.Evolve }

// Loader nạp dữ liệu; Detailer nạp chi tiết một đề xuất. Thay được trong kiểm thử; nil = nạp thật. readOnly: không
// nạp spool vào cơ sở dữ liệu và không quét Git.
type (
	Loader   func(ctx context.Context, dir string, q Query, readOnly bool) (Data, error)
	Detailer func(ctx context.Context, dir, id string, readOnly bool) (*evolvesvc.ShowResult, error)
)

// Load đọc đề xuất, lịch sử, hàng đợi và quét cơ hội (không quét Git: tín hiệu Git đã ghi vẫn được dùng).
func Load(ctx context.Context, dir string, q Query, readOnly bool) (Data, error) {
	e, err := evolvesvc.OpenIngest(ctx, dir, !readOnly)
	if err != nil {
		return Data{}, err
	}
	defer e.Close()
	d := Data{Config: e.K.Config, Stamp: time.Now()}
	if d.Rows, err = evolvesvc.List(ctx, e, evolvesvc.ListOptions{All: q.All}); err != nil {
		return Data{}, err
	}
	if d.History, err = evolvesvc.History(ctx, e, evolvesvc.HistoryOptions{}); err != nil {
		return Data{}, err
	}
	if d.Queue, err = evolvesvc.Queue(e, evolvesvc.QueueOptions{All: q.QueueAll}); err != nil {
		return Data{}, err
	}
	if res, err := evolvesvc.Scan(ctx, e, evolvesvc.ScanOptions{NoGit: true}); err != nil {
		d.ScanErr = err
	} else {
		d.Scan = &res
	}
	return d, nil
}

// LoadDetail đọc chi tiết đề xuất id.
func LoadDetail(ctx context.Context, dir, id string, readOnly bool) (*evolvesvc.ShowResult, error) {
	e, err := evolvesvc.OpenIngest(ctx, dir, !readOnly)
	if err != nil {
		return nil, err
	}
	defer e.Close()
	return evolvesvc.Show(ctx, e, evolvesvc.ShowOptions{ID: id})
}
