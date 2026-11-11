package evolve

// Tách chung/riêng trong vòng tiến hoá (H4). Mỗi cơ hội quy trình (và bài học từ thất bại)
// cho tối đa hai đề xuất anh em: một bản CHUNG (mọi vùng mã) và một bản RIÊNG (một vùng).
// Probe đo hai bản trên tập tác vụ khác nhau: bản chung trên mẫu của toàn validation (trừ tác
// vụ nguồn), bản riêng chỉ trên tác vụ thuộc vùng của nó. Compose chọn theo kết quả và KHÔNG
// BAO GIỜ gộp hai bản rồi áp dụng: bản gộp phải là một đề xuất mới và đối chiếu lại.

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"bint/internal/eval"
	"bint/internal/pathglob"
)

// Các biến thể của cặp.
const (
	PairGlobal = "global"
	PairDomain = "domain"
)

// Pair gắn một đề xuất vào cặp chung/riêng.
type Pair struct {
	// Key: khoá cơ hội sinh ra cặp.
	Key string `json:"key"`
	// Variant: global (bản chung) | domain (bản riêng).
	Variant string `json:"variant"`
	// Sibling: ID đề xuất anh em.
	Sibling string `json:"sibling"`
	// Area: glob vùng mã của bản riêng ("" với bản chung).
	Area string `json:"area,omitempty"`
}

// PairProbe là kết quả đối chiếu của một nửa cặp, đủ để Compose.
type PairProbe struct {
	// Supported: đã supported độc lập trên tập test.
	Supported bool
	// AreaEffect: hiệu ứng (tỷ lệ đạt candidate − base) trên các tác vụ thuộc vùng của bản
	// riêng. Với bản riêng đó là hiệu ứng của chính nó; với bản chung là hiệu ứng của nó trên
	// các tác vụ của vùng đó. nil = không đo được (không có tác vụ của vùng trong đợt đối chiếu).
	AreaEffect *float64
}

// ComposePair chọn nửa cặp được giữ (PairGlobal | PairDomain | "" = không nửa nào):
//
//   - chỉ một bản đạt: giữ bản đó;
//   - cả hai đạt: giữ bản chung nếu hiệu ứng của nó trên tác vụ của vùng không kém bản riêng
//     quá margin (non_inferiority_margin); ngược lại giữ bản riêng. Không đo được hiệu ứng của
//     bản chung trên vùng thì giữ bản riêng (phạm vi hẹp hơn, ít rủi ro hơn);
//   - không bản nào đạt: không giữ.
func ComposePair(global, domain PairProbe, margin float64) (keep, why string) {
	switch {
	case !global.Supported && !domain.Supported:
		return "", "không bản nào đạt"
	case global.Supported && !domain.Supported:
		return PairGlobal, "chỉ bản chung đạt"
	case !global.Supported && domain.Supported:
		return PairDomain, "chỉ bản riêng đạt"
	}
	if global.AreaEffect == nil || domain.AreaEffect == nil {
		return PairDomain, "cả hai đạt nhưng không đo được hiệu ứng của bản chung trên vùng: giữ bản riêng (phạm vi hẹp hơn)"
	}
	if *global.AreaEffect >= *domain.AreaEffect-margin {
		return PairGlobal, fmt.Sprintf("cả hai đạt; hiệu ứng bản chung trên vùng (%+.1f điểm %%) không kém bản riêng (%+.1f) quá %.0f điểm %%: giữ bản chung",
			100**global.AreaEffect, 100**domain.AreaEffect, 100*margin)
	}
	return PairDomain, fmt.Sprintf("cả hai đạt; bản chung kém bản riêng trên vùng quá %.0f điểm %% (%+.1f so với %+.1f): giữ bản riêng",
		100*margin, 100**global.AreaEffect, 100**domain.AreaEffect)
}

// AreaEffect tính hiệu ứng (tỷ lệ đạt candidate − base, trung bình theo tác vụ) của một đợt
// đối chiếu trên các tác vụ thuộc vùng area (glob khớp tệp mã của tác vụ). nil nếu không có tác
// vụ nào của vùng chấm được ở cả hai nhánh. area rỗng = mọi tác vụ của đợt.
func AreaEffect(ctx context.Context, db *sql.DB, batchID, area string, cases map[string]*eval.Case) (*float64, error) {
	rows, err := eval.Attempts(ctx, db, "batch_id = ?", batchID)
	if err != nil {
		return nil, err
	}
	type cell struct{ pass, graded int }
	per := map[string]map[string]*cell{}
	for _, r := range rows {
		if r.TestPassed == nil || (r.Arm != eval.ArmBase && r.Arm != eval.ArmCandidate) {
			continue
		}
		if area != "" && !caseInArea(cases[r.CaseID], area) {
			continue
		}
		if per[r.CaseID] == nil {
			per[r.CaseID] = map[string]*cell{}
		}
		c := per[r.CaseID][r.Arm]
		if c == nil {
			c = &cell{}
			per[r.CaseID][r.Arm] = c
		}
		c.graded++
		if *r.TestPassed {
			c.pass++
		}
	}
	var ids []string
	for id := range per {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var sum float64
	n := 0
	for _, id := range ids {
		b, c := per[id][eval.ArmBase], per[id][eval.ArmCandidate]
		if b == nil || c == nil {
			continue
		}
		sum += float64(c.pass)/float64(c.graded) - float64(b.pass)/float64(b.graded)
		n++
	}
	if n == 0 {
		return nil, nil
	}
	m := sum / float64(n)
	return &m, nil
}

// caseInArea: tác vụ có tệp mã khớp glob của vùng.
func caseInArea(c *eval.Case, area string) bool {
	if c == nil {
		return false
	}
	p := pathglob.Compile(area)
	for _, f := range c.SourceFiles {
		if p.Match(f) {
			return true
		}
	}
	return false
}

// PairAreaOf trả về glob vùng của bản riêng của cặp (đề xuất chung dùng glob của anh em).
func PairAreaOf(p *Proposal) string {
	if p.Pair != nil {
		return p.Pair.Area
	}
	return ""
}

// RejectSibling bác bỏ nửa cặp không được chọn khi Compose. Không cần người: bác bỏ không sửa
// tệp nào (như AutoReject) và cơ hội gốc mở lại khi có bằng chứng mới.
func RejectSibling(ctx context.Context, db *sql.DB, p *Proposal, chosen, why string, now time.Time) error {
	if p.Status != StatusProposed {
		return fmt.Errorf("đề xuất %s ở trạng thái %s: chỉ bác bỏ được đề xuất đang chờ", p.ID, p.Status)
	}
	reason := fmt.Sprintf("%s: nửa cặp chung/riêng không được chọn (giữ %s) — %s", AutoActor, chosen, why)
	p.Status, p.StatusReason = StatusRejected, reason
	return SetStatus(ctx, db, p.ID, StatusRejected, reason, now)
}

// pairProposals gắn Pair cho hai đề xuất anh em vừa dựng (bản chung và bản riêng). area là
// glob vùng của bản riêng.
func pairProposals(key string, global, domain *Proposal, area string) {
	global.Pair = &Pair{Key: key, Variant: PairGlobal, Sibling: domain.ID, Area: area}
	domain.Pair = &Pair{Key: key, Variant: PairDomain, Sibling: global.ID, Area: area}
}

// harnessAreaScope: glob vùng của bản riêng trong đề xuất quy trình (phạm vi domain của thành
// phần đầu tiên có phạm vi riêng).
func harnessAreaScope(p *Proposal) string {
	for _, ch := range p.HarnessChanges {
		if ch.Draft != nil {
			if g := ch.Draft.Component().Domain(); g != "" {
				return g
			}
		}
	}
	return ""
}
