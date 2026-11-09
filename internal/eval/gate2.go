package eval

import "fmt"

// Nguyên nhân khi Bint đầy đủ không tốt hơn nhánh tối giản (mục 12.9), theo thứ tự xét.
const (
	CauseMeasurement   = "measurement"    // đo lường không đủ tin cậy
	CauseContent       = "content"        // nội dung tri thức không giúp
	CauseRetrievalMiss = "retrieval_miss" // truy xuất bỏ sót
	CauseNotApplied    = "not_applied"    // truy xuất đúng nhưng không được áp dụng
	CauseNoise         = "noise"          // ngữ cảnh gây nhiễu
	CausePassed        = "passed"         // Bint đầy đủ tốt hơn tối giản
	CauseUndetermined  = "undetermined"   // thiếu dữ liệu để tách nguyên nhân
)

// Gate2Diagnosis là báo cáo chẩn đoán (mục 12.9). Chỉ là dữ liệu cho người ra quyết
// định; không tự kích hoạt thay đổi. Chẩn đoán chỉ nên chạy trên tập validation.
type Gate2Diagnosis struct {
	Cause  string   `json:"cause"`
	Detail string   `json:"detail"`
	Checks []string `json:"checks"`
	// IdealCoverage: tỷ lệ mục "đúng" được nhánh bint truy xuất (trên các lượt có tập đúng).
	IdealCoverage *float64 `json:"ideal_coverage"`
	// Precision: tỷ lệ mục được truy xuất thuộc tập đúng.
	Precision *float64 `json:"precision"`
	// CitedWhenRetrievedInFailures: ở lượt bint thất bại, tỷ lệ mục đúng đã truy xuất
	// được agent trích dẫn.
	CitedWhenRetrievedInFailures *float64 `json:"cited_when_retrieved_in_failures"`
}

// measurementKinds là các cảnh báo làm phép đo không đủ tin cậy để kết luận.
var measurementKinds = map[string]bool{
	"incomplete": true, "leakage_economy": true, "ci_unavailable": true, "ci_wide": true, "model_changed": true,
	"harness_changed": true, "errors": true, "case_changed": true, "unverified": true,
}

// ratio trả về a/b, nil khi b = 0.
func ratio(a, b int) *float64 {
	if b == 0 {
		return nil
	}
	v := float64(a) / float64(b)
	return &v
}

func diagnoseGate2(rep Report, rows []AttemptRow, cu map[string]AttemptContext) Gate2Diagnosis {
	var g Gate2Diagnosis
	check := func(f string, a ...any) { g.Checks = append(g.Checks, fmt.Sprintf(f, a...)) }
	find := func(arm, base string) *Paired {
		for i := range rep.Paired {
			if rep.Paired[i].Arm == arm && rep.Paired[i].Baseline == base {
				return &rep.Paired[i]
			}
		}
		return nil
	}
	better := func(p *Paired) bool { return p != nil && p.PassDiff.CI.Valid() && p.PassDiff.CI.Lo > 0 }
	fmtP := func(p *Paired) string {
		if p == nil {
			return "không có"
		}
		if !p.PassDiff.CI.Valid() {
			return fmt.Sprintf("%+.3f (không tính được CI, %d tác vụ)", p.PassDiff.Mean, p.PassDiff.N)
		}
		return fmt.Sprintf("%+.3f, CI95 [%+.3f, %+.3f], %d tác vụ", p.PassDiff.Mean, p.PassDiff.CI.Lo, p.PassDiff.CI.Hi, p.PassDiff.N)
	}

	// Số liệu truy xuất của nhánh bint.
	var ideal, idealRet, ret, idealRetFail, idealCitedFail int
	for _, r := range rows {
		if r.Arm != ArmBint {
			continue
		}
		a, ok := cu[r.AttemptID]
		if !ok {
			continue
		}
		ret += a.Retrieved
		if a.Ideal > 0 {
			ideal += a.Ideal
			idealRet += a.IdealRetrieved
		}
		if r.TestPassed != nil && !*r.TestPassed {
			idealRetFail += a.IdealRetrieved
			idealCitedFail += a.IdealCited
		}
	}
	g.IdealCoverage = ratio(idealRet, ideal)
	g.Precision = ratio(idealRet, ret)
	g.CitedWhenRetrievedInFailures = ratio(idealCitedFail, idealRetFail)

	// 1. Đo lường.
	for _, d := range rep.Diagnostics {
		if d.Level == "warn" && measurementKinds[d.Kind] {
			g.Cause, g.Detail = CauseMeasurement, "đo lường không đủ tin cậy: "+d.Detail+" — sửa phép đo, chưa kết luận gì về Bint"
			check("độ tin cậy: %s", d.Kind)
			return g
		}
	}
	check("độ tin cậy của phép đo: không có cảnh báo chặn")

	bm := find(ArmBint, ArmMinimal)
	im := find(ArmIdeal, ArmMinimal)
	check("bint − minimal: %s", fmtP(bm))
	check("ideal − minimal: %s", fmtP(im))
	if better(bm) {
		g.Cause, g.Detail = CausePassed, "Bint đầy đủ tốt hơn tối giản về tỷ lệ đạt (cận dưới CI95 > 0); đối chiếu ngưỡng đã chốt ở docs/gate-2.md"
		return g
	}
	// 2. Nội dung tri thức.
	if im == nil {
		g.Cause, g.Detail = CauseUndetermined, "không có nhánh ideal: chạy thêm --arms minimal,bint,ideal trên tập validation để tách nguyên nhân"
		return g
	}
	if !better(im) {
		g.Cause, g.Detail = CauseContent, "nhánh tham chiếu lý tưởng không tốt hơn tối giản: nội dung tri thức không giúp — sửa truy xuất sẽ vô ích; xem lại loại tri thức được sinh, hoặc thu hẹp/dừng"
		return g
	}
	// 3. Truy xuất bỏ sót.
	if g.IdealCoverage == nil {
		g.Cause, g.Detail = CauseUndetermined, "không có dữ liệu truy xuất của nhánh bint (nhật ký ngữ cảnh rỗng)"
		return g
	}
	check("độ phủ tập đúng của nhánh bint: %.0f%%", 100**g.IdealCoverage)
	if *g.IdealCoverage < 0.5 {
		g.Cause, g.Detail = CauseRetrievalMiss, fmt.Sprintf("ideal tốt hơn tối giản nhưng bint thì không; bint chỉ truy xuất %.0f%% mục đúng — sửa truy xuất: phạm vi đường dẫn, thẻ, xếp hạng", 100**g.IdealCoverage)
		return g
	}
	// 4. Truy xuất đúng nhưng không được áp dụng.
	if c := g.CitedWhenRetrievedInFailures; c != nil {
		check("ở lượt thất bại, mục đúng đã truy xuất được trích dẫn: %.0f%%", 100**c)
		if *c < 0.5 {
			g.Cause, g.Detail = CauseNotApplied, "mục đúng có trong ngữ cảnh nhưng ít được trích dẫn ở lượt thất bại — sửa cách trình bày: vị trí, độ dài, mức ưu tiên"
			return g
		}
	}
	// 5. Ngữ cảnh gây nhiễu.
	noisy := bm != nil && bm.PassDiff.Mean < 0
	if bm != nil && ((bm.CostDiff != nil && bm.CostDiff.Mean > 0) || (bm.TurnDiff != nil && bm.TurnDiff.Mean > 0)) {
		check("bint tốn hơn tối giản (chi phí hoặc số lượt tăng)")
		noisy = true
	}
	if g.Precision != nil {
		check("độ chính xác truy xuất: %.0f%%", 100**g.Precision)
		if *g.Precision < 0.3 {
			noisy = true
		}
	}
	if noisy {
		g.Cause, g.Detail = CauseNoise, "ngữ cảnh gây nhiễu — giảm ngân sách ngữ cảnh, lọc chặt hơn, ưu tiên chèn theo đường dẫn"
		return g
	}
	g.Cause, g.Detail = CauseUndetermined, "không nguyên nhân nào được xác nhận rõ; cần thêm tác vụ hoặc lần lặp"
	return g
}
