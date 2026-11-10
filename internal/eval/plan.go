package eval

import (
	"context"
	"database/sql"
	"fmt"
	"math"
)

// Mô hình phương sai của chênh lệch theo tác vụ khi mỗi nhánh lặp R lần:
//
//	σ²(R) = σ_b² + W/R,  W = trung bình theo tác vụ của 2·p_i(1−p_i)
//
// σ_b là độ lệch giữa các tác vụ về hiệu ứng thật (không đổi theo R); W/R là nhiễu
// lấy mẫu của hai trung bình R lần lặp, giảm khi tăng R. Tăng số lần lặp chỉ cắt được
// phần W/R, còn σ_b buộc phải bù bằng thêm tác vụ.

// DefaultW là W thận trọng khi chưa có dữ liệu: p = 0,5 ở mọi tác vụ → 2·0,25 = 0,5.
const DefaultW = 0.5

// DefaultEffectSD là σ_b mặc định (cấu hình evolution.evaluation.effect_sd).
const DefaultEffectSD = 0.15

// DefaultMaxRepeats là số lần lặp tối đa mặc định (evolution.evaluation.max_repeats).
const DefaultMaxRepeats = 8

// PassCount là số lượt đạt k trên m lượt của một tác vụ ở cùng một nhánh.
type PassCount struct{ K, M int }

// EstimateW ước lượng W từ các tác vụ có ≥ 2 lượt cùng nhánh. Với k đạt trên m lượt,
// k(m−k)/(m(m−1)) là ước lượng không chệch của p(1−p). Trả về W và số tác vụ dùng;
// chưa có tác vụ nào thì trả DefaultW và 0.
func EstimateW(counts []PassCount) (float64, int) {
	sum, n := 0.0, 0
	for _, c := range counts {
		if c.M < 2 {
			continue
		}
		sum += 2 * float64(c.K*(c.M-c.K)) / float64(c.M*(c.M-1))
		n++
	}
	if n == 0 {
		return DefaultW, 0
	}
	return sum / float64(n), n
}

// EstimateSigmaB ước lượng σ_b từ chênh lệch trung bình theo tác vụ d̄_i (mỗi nhánh
// lặp r lần): σ_b² = Var(d̄_i) − W/r, chặn dưới ở 0. NaN khi có dưới 2 tác vụ.
func EstimateSigmaB(dbar []float64, w, r float64) float64 {
	v := SD(dbar)
	if math.IsNaN(v) || r <= 0 {
		return math.NaN()
	}
	return math.Sqrt(math.Max(0, v*v-w/r))
}

// VarModel là mô hình phương sai kèm nguồn của từng con số.
type VarModel struct {
	W           float64 `json:"w"`
	WSource     string  `json:"w_source"`
	SigmaB      float64 `json:"sigma_b"`
	SigmaSource string  `json:"sigma_b_source"`
}

// Sigma2 là σ²(R).
func (m VarModel) Sigma2(r int) float64 { return m.SigmaB*m.SigmaB + m.W/float64(r) }

// minSigmaPairs: số cặp (tác vụ, nhánh so với none) tối thiểu để tin ước lượng σ_b từ lịch sử.
const minSigmaPairs = 5

// HistoricalVariance dựng mô hình phương sai từ lịch sử của harness. W từ các tác vụ
// có ≥ 2 lượt nhánh none (mọi đợt). σ_b lấy từ effectSD (mặc định DefaultEffectSD) trừ
// khi lịch sử có đủ chênh lệch ghép cặp kèm lặp để ước lượng.
func HistoricalVariance(ctx context.Context, db *sql.DB, harness string, effectSD float64) (VarModel, error) {
	if effectSD <= 0 {
		effectSD = DefaultEffectSD
	}
	m := VarModel{SigmaB: effectSD, SigmaSource: fmt.Sprintf("cấu hình evolution.evaluation.effect_sd = %.2f (không xác định được từ dữ liệu một nhánh)", effectSD)}
	rows, err := db.QueryContext(ctx, `SELECT case_id, SUM(test_passed), COUNT(*) FROM evaluation_runs
		WHERE harness = ? AND arm = ? AND test_passed IS NOT NULL GROUP BY case_id ORDER BY case_id`, harness, ArmNone)
	if err != nil {
		return m, err
	}
	var counts []PassCount
	for rows.Next() {
		var id string
		var c PassCount
		if err := rows.Scan(&id, &c.K, &c.M); err != nil {
			rows.Close()
			return m, err
		}
		counts = append(counts, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return m, err
	}
	var n int
	m.W, n = EstimateW(counts)
	if n > 0 {
		m.WSource = fmt.Sprintf("đo từ %d tác vụ có ≥2 lượt nhánh none", n)
	} else {
		m.WSource = "giả định thận trọng p = 0,5 (chưa có tác vụ nào lặp ở nhánh none)"
	}

	// σ_b: chênh lệch ghép cặp (nhánh khác − none) kèm lặp trong cùng đợt.
	pr, err := db.QueryContext(ctx, `SELECT batch_id, case_id, arm, AVG(test_passed), COUNT(*) FROM evaluation_runs
		WHERE harness = ? AND test_passed IS NOT NULL GROUP BY batch_id, case_id, arm ORDER BY batch_id, case_id, arm`, harness)
	if err != nil {
		return m, err
	}
	defer pr.Close()
	type key struct{ batch, c string }
	type cell struct {
		mean float64
		n    int
	}
	by := map[key]map[string]cell{}
	for pr.Next() {
		var k key
		var arm string
		var c cell
		if err := pr.Scan(&k.batch, &k.c, &arm, &c.mean, &c.n); err != nil {
			return m, err
		}
		if by[k] == nil {
			by[k] = map[string]cell{}
		}
		by[k][arm] = c
	}
	if err := pr.Err(); err != nil {
		return m, err
	}
	var d []float64
	rSum := 0.0
	for _, arms := range by {
		base, ok := arms[ArmNone]
		if !ok || base.n < 2 {
			continue
		}
		for arm, c := range arms {
			if arm != ArmNone && c.n >= 2 {
				d = append(d, c.mean-base.mean)
				rSum += float64(min(c.n, base.n))
			}
		}
	}
	if len(d) >= minSigmaPairs {
		if sb := EstimateSigmaB(d, m.W, rSum/float64(len(d))); !math.IsNaN(sb) {
			m.SigmaB = sb
			m.SigmaSource = fmt.Sprintf("đo từ %d cặp tác vụ có lặp (Var(d̄) − W/R, chặn dưới ở 0)", len(d))
		}
	}
	return m, nil
}

// PlanInput là đầu vào của PlanProbe.
type PlanInput struct {
	Available  int     // số tác vụ sẵn có
	Effect     float64 // mức hiệu ứng δ cần phát hiện
	SigmaB, W  float64 // mô hình phương sai
	CostPerRun float64 // chi phí mỗi lượt (USD); <= 0 = chưa biết
	MaxRepeats int     // R tối đa
	// FixedRepeats > 0 cố định R thay vì để bộ lập kế hoạch chọn.
	FixedRepeats int
}

// ProbePlan là kế hoạch một lần Probe.
type ProbePlan struct {
	Feasible bool    `json:"feasible"`
	Repeats  int     `json:"repeats"`
	Cases    int     `json:"cases"`    // n(R) khi khả thi; số tác vụ sẵn có khi không
	MaxRuns  int     `json:"max_runs"` // n·2·R
	MaxCost  float64 `json:"max_cost_usd"`
	// MinDetectable là δ nhỏ nhất phát hiện được với số tác vụ của kế hoạch ở R đã
	// chọn (khi không khả thi: số tác vụ hiện có ở R tối đa).
	MinDetectable float64 `json:"min_detectable"`
	Sigma         float64 `json:"sigma"` // σ(R)
	CostKnown     bool    `json:"cost_known"`
}

// ProbeSampleSize là n(R) = ⌈((z0.975+z0.8)·σ(R)/δ)²⌉.
func ProbeSampleSize(sigma, effect float64) int {
	if effect <= 0 || sigma <= 0 || math.IsNaN(sigma) {
		return 0
	}
	return int(math.Ceil(math.Pow((z975+z80)*sigma/effect, 2)))
}

// PlanProbe chọn số lần lặp R trong 1..MaxRepeats sao cho tổng số lượt n(R)·2·R thấp
// nhất trong các R có n(R) ≤ số tác vụ sẵn có. Không R nào thoả → Feasible = false,
// kèm δ nhỏ nhất phát hiện được với số tác vụ hiện có ở R tối đa.
func PlanProbe(in PlanInput) ProbePlan {
	maxR := in.MaxRepeats
	if maxR < 1 {
		maxR = DefaultMaxRepeats
	}
	m := VarModel{W: in.W, SigmaB: in.SigmaB}
	lo, hi := 1, maxR
	if in.FixedRepeats > 0 {
		lo, hi = in.FixedRepeats, in.FixedRepeats
	}
	fill := func(p *ProbePlan, r, n int) {
		p.Repeats, p.Cases, p.MaxRuns = r, n, n*2*r
		p.Sigma = math.Sqrt(m.Sigma2(r))
		p.MinDetectable = math.Inf(1)
		if n > 0 {
			p.MinDetectable = (z975 + z80) * p.Sigma / math.Sqrt(float64(n))
		}
		if in.CostPerRun > 0 {
			p.CostKnown, p.MaxCost = true, in.CostPerRun*float64(p.MaxRuns)
		}
	}
	var best ProbePlan
	for r := lo; r <= hi; r++ {
		n := ProbeSampleSize(math.Sqrt(m.Sigma2(r)), in.Effect)
		if n < 1 || n > in.Available {
			continue
		}
		if !best.Feasible || n*r < best.Cases*best.Repeats {
			best = ProbePlan{Feasible: true}
			fill(&best, r, n)
		}
	}
	if best.Feasible {
		return best
	}
	var p ProbePlan
	fill(&p, hi, in.Available)
	return p
}
