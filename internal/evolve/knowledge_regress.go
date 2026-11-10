package evolve

import (
	"context"
	"database/sql"
	"fmt"
	"path"
	"strings"

	"bint/internal/eval"
	"bint/internal/knowledge"
)

// BeforeItems dựng lại các mục tri thức TRƯỚC khi đề xuất được áp dụng, từ bản ghi kiểm toán
// (nội dung tệp trước/sau). Giá trị nil = mục chưa tồn tại trước đề xuất (do đề xuất tạo).
func BeforeItems(ctx context.Context, db *sql.DB, p *Proposal) (map[string]*knowledge.Item, error) {
	ads, err := Adoptions(ctx, db, p.ID)
	if err != nil {
		return nil, err
	}
	for _, a := range ads {
		if a.Action != "apply" {
			continue
		}
		out := map[string]*knowledge.Item{}
		for _, f := range a.Files {
			if !strings.HasPrefix(f.Path, knowledge.ItemsRel+"/") {
				continue
			}
			id := strings.TrimSuffix(path.Base(f.Path), ".md")
			if f.Before == nil {
				out[id] = nil
				continue
			}
			it, err := knowledge.Parse([]byte(*f.Before))
			if err != nil {
				return nil, fmt.Errorf("bản trước đề xuất của %s: %w", id, err)
			}
			out[id] = it
		}
		return out, nil
	}
	return nil, fmt.Errorf("không có bản ghi áp dụng của %s", p.ID)
}

// RegressionVariant dựng cặp nhánh để kiểm tra hồi quy khi đổi mô hình một đề xuất TRI THỨC đã
// áp dụng (mục 12.6): base = tập tri thức của tác vụ với đề xuất được hoàn lại (before),
// candidate = tập hiện tại (đã có đề xuất). Cùng kế hoạch đánh giá như lúc đối chiếu gốc.
// after là các mục của đề xuất ở bản hiện tại (theo ID). Mục được trả về từ before phải áp dụng được tại commit gốc của tác vụ (bằng chứng khớp, không
// chứa mã lời giải), nếu không nhánh không khả dụng cho tác vụ đó.
func RegressionVariant(p *Proposal, delivery, repoDir string, before, after map[string]*knowledge.Item) *eval.Variant {
	return &eval.Variant{
		Delivery: delivery,
		Label:    p.ID,
		// Mục do đề xuất tạo/sửa có thể còn ở candidate (chờ duyệt): câu hỏi là "cải tiến này có còn
		// giúp không khi đã được dùng", nên coi như đã duyệt — chỉ trong bản sao đánh giá, như lúc đối chiếu gốc.
		Candidate: func(ctx context.Context, items []*knowledge.Item, c *eval.Case, leak *eval.LeakInput) ([]*knowledge.Item, error) {
			// Tri thức hiện có tạo sau commit gốc của tác vụ bị lọc khỏi tập (chống rò rỉ): đặt lại các
			// mục của đề xuất từ bản hiện tại, với cùng điều kiện áp dụng như lúc đối chiếu gốc.
			var out []*knowledge.Item
			for _, it := range items {
				if _, touched := before[it.ID]; !touched {
					out = append(out, it)
				}
			}
			for id := range before {
				it := after[id]
				if it == nil {
					continue
				}
				if len(it.Evidence) > 0 && !eval.EvidenceHoldsAt(ctx, repoDir, c, it) {
					return nil, unavailable("bằng chứng của %s không khớp tại commit gốc %s", id, shortSHA(c.Source.Base))
				}
				if leak != nil {
					if r := eval.CheckLeak(it.Title+"\n"+it.Body, *leak); r.Status == "fail" {
						return nil, unavailable("nội dung %s chứa mã của lời giải (%s)", id, r.Findings[0].Detail)
					}
				}
				cp := *it
				if cp.Status == knowledge.StatusCandidate {
					cp.Status, cp.ReviewedBy = knowledge.StatusVerified, evalReviewer
				}
				out = append(out, &cp)
			}
			return out, nil
		},
		Base: func(ctx context.Context, items []*knowledge.Item, c *eval.Case, leak *eval.LeakInput) ([]*knowledge.Item, error) {
			restored := map[string]*knowledge.Item{}
			for id, b := range before {
				if b == nil {
					continue
				}
				if len(b.Evidence) > 0 && !eval.EvidenceHoldsAt(ctx, repoDir, c, b) {
					return nil, unavailable("bằng chứng của %s (trước đề xuất) không khớp tại commit gốc %s", id, shortSHA(c.Source.Base))
				}
				if leak != nil {
					if r := eval.CheckLeak(b.Title+"\n"+b.Body, *leak); r.Status == "fail" {
						return nil, unavailable("nội dung %s (trước đề xuất) chứa mã của lời giải (%s)", id, r.Findings[0].Detail)
					}
				}
				restored[id] = b
			}
			var out []*knowledge.Item
			seen := map[string]bool{}
			for _, it := range items {
				id := it.ID
				b, touched := before[id]
				switch {
				case !touched:
					out = append(out, it)
				case b == nil: // do đề xuất tạo: bỏ
				default:
					out = append(out, restored[id])
					seen[id] = true
				}
			}
			for id, b := range restored {
				if !seen[id] && b != nil {
					out = append(out, b)
				}
			}
			return out, nil
		},
	}
}
