// Package knowledge là màn Tri thức: danh sách + chi tiết (bằng chứng, kết quả kiểm tra), lọc theo trạng
// thái/loại/nguồn/nhãn lô nguon:*/đường dẫn, duyệt từng mục và theo lô, thêm/xoá/kiểm tra bằng form, và
// các thẻ phụ (Tìm kiếm, Thống kê, Thay đổi, Thẩm định lại, Xuất bản AGENTS.md, Khảo sát, Ngữ cảnh).
// Mọi thao tác ghi đi qua actions.RequestMsg để mô hình gốc kiểm chế độ chỉ đọc/khoá theo sổ hành động.
package knowledge

import (
	"context"
	"time"

	"bint/internal/app"
	kn "bint/internal/knowledge"
)

// Data là dữ liệu của danh sách.
type Data struct {
	Items []*kn.Item
	// Bad là số tệp mục không đọc được.
	Bad int
	// Files là các tệp được Git theo dõi (gợi ý cho ô bằng chứng và phạm vi).
	Files []string
	// WS là không gian tri thức đã mở lúc nạp; chi tiết dựng từ đây với Items đã nạp, khỏi mở lại và đọc lại
	// mọi tệp mục mỗi lần con trỏ dời (nil: dữ liệu dựng tay trong kiểm thử).
	WS *app.KnowledgeWS
	// Stamp thay đổi mỗi lần nạp (xoá bộ nhớ đệm chi tiết).
	Stamp time.Time
}

// Batches trả các nhãn lô nguon:* trong dữ liệu.
func (d Data) Batches() []string { return kn.Batches(d.Items) }

// IDs trả ID của mọi mục.
func (d Data) IDs() []string {
	out := make([]string, len(d.Items))
	for i, it := range d.Items {
		out[i] = it.ID
	}
	return out
}

// LoadedMsg là kết quả nạp danh sách.
type LoadedMsg struct {
	Data Data
	Err  error
}

// DetailMsg là kết quả nạp chi tiết một mục.
type DetailMsg struct {
	ID     string
	Detail app.KnowledgeDetail
	Err    error
}

// maxSuggestFiles giới hạn số tệp gợi ý để bộ nhớ và thời gian nạp không phình theo repository lớn.
const maxSuggestFiles = 20000

// Loader nạp danh sách; thay được trong kiểm thử.
type Loader func(ctx context.Context, dir string) (Data, error)

// Detailer nạp chi tiết một mục (kiểm tra + trích đoạn bằng chứng) bằng cách mở lại kho; thay được trong
// kiểm thử. Mặc định (nil) màn hình dựng từ Data đã nạp thay vì gọi hàm này.
type Detailer func(ctx context.Context, dir, id string) (app.KnowledgeDetail, error)

// Load đọc mọi mục tri thức (kể cả deprecated: lọc ở phía màn hình) của repository chứa dir.
func Load(ctx context.Context, dir string) (Data, error) {
	k, err := app.OpenKnowledge(ctx, dir)
	if err != nil {
		return Data{}, err
	}
	items, bad, err := k.Items()
	if err != nil {
		return Data{}, err
	}
	return Data{Items: items, Bad: len(bad), Files: app.TrackedFiles(k, maxSuggestFiles), WS: k, Stamp: time.Now()}, nil
}

// LoadDetail kiểm tra và đọc bằng chứng của mục id (kiểm lười: chỉ mục đang xem). Mở lại kho và đọc mọi mục:
// chỉ là đường dự phòng khi chưa có Data.WS; đường thường dùng Data.detail.

func LoadDetail(ctx context.Context, dir, id string) (app.KnowledgeDetail, error) {
	k, err := app.OpenKnowledge(ctx, dir)
	if err != nil {
		return app.KnowledgeDetail{}, err
	}
	return app.ShowKnowledge(ctx, k, app.KnowledgeShowOptions{Ref: id})
}

// detail dựng chi tiết mục it từ danh sách đã nạp (không đọc lại kho): chi phí mỗi mục chỉ là kiểm tra và
// đọc bằng chứng của riêng nó.
func (d Data) detail(it *kn.Item) (app.KnowledgeDetail, bool) {
	if d.WS == nil {
		return app.KnowledgeDetail{}, false
	}
	return d.WS.DetailOf(it, d.Items), true
}
