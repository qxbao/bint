package eval

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
)

// Stat là trung bình kèm khoảng tin cậy 95% và số tác vụ làm căn cứ.
type Stat struct {
	Mean float64 `json:"mean"`
	CI   CI      `json:"ci"`
	N    int     `json:"n"`
}

// ArmSummary tóm tắt một nhánh trong một đợt.
type ArmSummary struct {
	Arm      string `json:"arm"`
	Attempts int    `json:"attempts"`
	Graded   int    `json:"graded"` // lượt chấm được (passed/failed/timeout)
	Passed   int    `json:"passed"`
	Errors   int    `json:"errors"`
	Skipped  int    `json:"skipped"`
	Timeouts int    `json:"timeouts"`
	// PassRate là trung bình theo tác vụ của tỷ lệ đạt (mỗi tác vụ nặng như nhau).
	PassRate Stat  `json:"pass_rate"`
	Cost     *Stat `json:"cost_usd,omitempty"`
	Duration *Stat `json:"agent_seconds,omitempty"`
	Turns    *Stat `json:"turns,omitempty"`
	// Độ dao động giữa các lần lặp: độ lệch chuẩn trung bình của kết quả đạt/không
	// trong cùng một tác vụ, và số tác vụ có kết quả không nhất quán.
	WithinCaseSD     *float64 `json:"within_case_sd"`
	MixedCases       int      `json:"mixed_cases"`
	CasesWithRepeats int      `json:"cases_with_repeats"`
	Models           []string `json:"models"`
}

// Paired là so sánh theo cặp một nhánh với mốc trên cùng tập tác vụ.
type Paired struct {
	Arm      string   `json:"arm"`
	Baseline string   `json:"baseline"`
	PassDiff Stat     `json:"pass_rate_diff"`
	SDDiff   *float64 `json:"sd_diff"`
	CostDiff *Stat    `json:"cost_usd_diff,omitempty"`
	TimeDiff *Stat    `json:"agent_seconds_diff,omitempty"`
	TurnDiff *Stat    `json:"turns_diff,omitempty"`
}

// CaseResult là kết quả theo tác vụ.
type CaseResult struct {
	CaseID string                 `json:"case_id"`
	Split  string                 `json:"split,omitempty"`
	Arms   map[string]CaseArmCell `json:"arms"`
}

// CaseArmCell là số lượt đạt / chấm được của một tác vụ ở một nhánh.
type CaseArmCell struct {
	Passed int `json:"passed"`
	Graded int `json:"graded"`
	Other  int `json:"other"` // lỗi hoặc bỏ qua
}

// Diagnostic là một nhận định về độ tin cậy của phép đo (mục 12.9). Báo cáo chỉ
// là dữ liệu cho người ra quyết định, không tự kích hoạt thay đổi.
type Diagnostic struct {
	Level  string `json:"level"` // warn | info
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

// Report là báo cáo của một đợt.
type Report struct {
	Batch       Batch        `json:"batch"`
	Arms        []ArmSummary `json:"arms"`
	Paired      []Paired     `json:"paired"`
	Cases       []CaseResult `json:"cases"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// reportSeed cố định để báo cáo tái lập được.
const reportSeed = 20261109

// CIWarnWidth: khoảng tin cậy của chênh lệch tỷ lệ đạt rộng hơn mức này thì chưa
// đủ để kết luận về mức cải thiện thực tế thường gặp (vài điểm phần trăm).
const CIWarnWidth = 0.30

// BuildReport tổng hợp báo cáo cho một đợt. current là các tác vụ hiện có (để
// phát hiện tác vụ bị sửa sau khi chạy); có thể nil.
func BuildReport(ctx context.Context, db *sql.DB, b Batch, baseline string, current map[string]*Case) (Report, error) {
	rows, err := Attempts(ctx, db, "batch_id = ?", b.BatchID)
	if err != nil {
		return Report{}, err
	}
	rep := Report{Batch: b}
	arms := slices.Clone(b.Arms)
	if baseline == "" {
		baseline = arms[0]
		if slices.Contains(arms, ArmNone) {
			baseline = ArmNone
		}
	}

	type agg struct{ pass, cost, dur, turns []float64 }
	// byCase[case][arm]
	byCase := map[string]map[string]*agg{}
	cells := map[string]map[string]*CaseArmCell{}
	splits := map[string]string{}
	sums := map[string]*ArmSummary{}
	for _, arm := range arms {
		sums[arm] = &ArmSummary{Arm: arm}
	}
	models := map[string]map[string]bool{}
	harnessVersions := map[string]bool{}
	fingerprints := map[string]map[string]bool{}
	touched := 0
	for _, r := range rows {
		s := sums[r.Arm]
		if s == nil {
			s = &ArmSummary{Arm: r.Arm}
			sums[r.Arm] = s
			arms = append(arms, r.Arm)
		}
		s.Attempts++
		if byCase[r.CaseID] == nil {
			byCase[r.CaseID] = map[string]*agg{}
			cells[r.CaseID] = map[string]*CaseArmCell{}
		}
		if byCase[r.CaseID][r.Arm] == nil {
			byCase[r.CaseID][r.Arm] = &agg{}
			cells[r.CaseID][r.Arm] = &CaseArmCell{}
		}
		splits[r.CaseID] = r.CaseSplit
		if fingerprints[r.CaseID] == nil {
			fingerprints[r.CaseID] = map[string]bool{}
		}
		fingerprints[r.CaseID][r.CaseFingerprint] = true
		if r.HarnessVersion != "" {
			harnessVersions[r.HarnessVersion] = true
		}
		g, cell := byCase[r.CaseID][r.Arm], cells[r.CaseID][r.Arm]
		switch r.Status {
		case "skipped":
			s.Skipped++
			cell.Other++
			continue
		case "error", "running":
			s.Errors++
			cell.Other++
			continue
		case "timeout":
			s.Timeouts++
		}
		if r.TestPassed == nil {
			s.Errors++
			cell.Other++
			continue
		}
		s.Graded++
		cell.Graded++
		v := 0.0
		if *r.TestPassed {
			v = 1
			s.Passed++
			cell.Passed++
		}
		g.pass = append(g.pass, v)
		if r.CostUSD != nil {
			g.cost = append(g.cost, *r.CostUSD)
		}
		if r.AgentDurationMS != nil {
			g.dur = append(g.dur, float64(*r.AgentDurationMS)/1000)
		}
		if r.NumTurns != nil {
			g.turns = append(g.turns, float64(*r.NumTurns))
		}
		if r.Model != "" {
			if models[r.Arm] == nil {
				models[r.Arm] = map[string]bool{}
			}
			primary, _, _ := strings.Cut(r.Model, ",")
			models[r.Arm][primary] = true
		}
		if r.TouchedTests != nil && *r.TouchedTests {
			touched++
		}
	}

	caseIDs := make([]string, 0, len(byCase))
	for id := range byCase {
		caseIDs = append(caseIDs, id)
	}
	sort.Strings(caseIDs)

	perCase := func(arm string, pick func(*agg) []float64) map[string]float64 {
		out := map[string]float64{}
		for _, id := range caseIDs {
			if g := byCase[id][arm]; g != nil {
				if xs := pick(g); len(xs) > 0 {
					out[id] = Mean(xs)
				}
			}
		}
		return out
	}
	stat := func(m map[string]float64) *Stat {
		if len(m) == 0 {
			return nil
		}
		xs := make([]float64, 0, len(m))
		for _, id := range caseIDs {
			if v, ok := m[id]; ok {
				xs = append(xs, v)
			}
		}
		return &Stat{Mean: Mean(xs), CI: BootstrapMeanCI(xs, BootstrapIters, 0.05, reportSeed), N: len(xs)}
	}
	pickPass := func(g *agg) []float64 { return g.pass }
	pickCost := func(g *agg) []float64 { return g.cost }
	pickDur := func(g *agg) []float64 { return g.dur }
	pickTurns := func(g *agg) []float64 { return g.turns }

	for _, arm := range arms {
		s := sums[arm]
		if p := stat(perCase(arm, pickPass)); p != nil {
			s.PassRate = *p
		} else {
			s.PassRate = Stat{Mean: math.NaN(), CI: CI{math.NaN(), math.NaN()}}
		}
		s.Cost, s.Duration, s.Turns = stat(perCase(arm, pickCost)), stat(perCase(arm, pickDur)), stat(perCase(arm, pickTurns))
		var sds []float64
		for _, id := range caseIDs {
			g := byCase[id][arm]
			if g == nil || len(g.pass) < 2 {
				continue
			}
			s.CasesWithRepeats++
			sd := SD(g.pass)
			sds = append(sds, sd)
			if sd > 0 {
				s.MixedCases++
			}
		}
		s.WithinCaseSD = finite(Mean(sds))
		for m := range models[arm] {
			s.Models = append(s.Models, m)
		}
		sort.Strings(s.Models)
		rep.Arms = append(rep.Arms, *s)
	}

	paired := func(arm string, pick func(*agg) []float64) (*Stat, float64) {
		a, base := perCase(arm, pick), perCase(baseline, pick)
		var d []float64
		for _, id := range caseIDs {
			va, okA := a[id]
			vb, okB := base[id]
			if okA && okB {
				d = append(d, va-vb)
			}
		}
		if len(d) == 0 {
			return nil, math.NaN()
		}
		return &Stat{Mean: Mean(d), CI: BootstrapMeanCI(d, BootstrapIters, 0.05, reportSeed), N: len(d)}, SD(d)
	}
	for _, arm := range arms {
		if arm == baseline {
			continue
		}
		pd, sd := paired(arm, pickPass)
		if pd == nil {
			continue
		}
		p := Paired{Arm: arm, Baseline: baseline, PassDiff: *pd, SDDiff: finite(sd)}
		p.CostDiff, _ = paired(arm, pickCost)
		p.TimeDiff, _ = paired(arm, pickDur)
		p.TurnDiff, _ = paired(arm, pickTurns)
		rep.Paired = append(rep.Paired, p)
	}

	for _, id := range caseIDs {
		cr := CaseResult{CaseID: id, Split: splits[id], Arms: map[string]CaseArmCell{}}
		for arm, c := range cells[id] {
			cr.Arms[arm] = *c
		}
		rep.Cases = append(rep.Cases, cr)
	}
	rep.Diagnostics = diagnose(rep, b, rows, models, harnessVersions, fingerprints, current, touched)
	return rep, nil
}

func diagnose(rep Report, b Batch, rows []AttemptRow, models map[string]map[string]bool, hv map[string]bool,
	fps map[string]map[string]bool, current map[string]*Case, touched int) []Diagnostic {
	var ds []Diagnostic
	warn := func(kind, f string, a ...any) { ds = append(ds, Diagnostic{"warn", kind, fmt.Sprintf(f, a...)}) }
	info := func(kind, f string, a ...any) { ds = append(ds, Diagnostic{"info", kind, fmt.Sprintf(f, a...)}) }

	if b.Status != "completed" {
		warn("incomplete", "đợt ở trạng thái %s: thiếu lượt chạy, kết quả có thể lệch", b.Status)
	}
	if b.LeakageMode == LeakEconomy {
		warn("leakage_economy", "chế độ economy: kết quả chỉ dùng để sàng lọc, không dùng cho cổng quyết định")
	}
	for _, p := range rep.Paired {
		if !p.PassDiff.CI.Valid() {
			warn("ci_unavailable", "%s vs %s: chỉ %d tác vụ chấm được ở cả hai nhánh, không tính được khoảng tin cậy", p.Arm, p.Baseline, p.PassDiff.N)
		} else if p.PassDiff.CI.Width() > CIWarnWidth {
			warn("ci_wide", "%s vs %s: khoảng tin cậy rộng %.0f điểm %% (%d tác vụ) — cần thêm tác vụ hoặc lần lặp trước khi kết luận",
				p.Arm, p.Baseline, 100*p.PassDiff.CI.Width(), p.PassDiff.N)
		}
	}
	all := map[string]bool{}
	for _, m := range models {
		for k := range m {
			all[k] = true
		}
	}
	if len(all) > 1 {
		warn("model_changed", "có %d mô hình khác nhau trong đợt (%s): phép so sánh không còn cùng điều kiện", len(all), strings.Join(sortedKeys(all), ", "))
	}
	if len(hv) > 1 {
		warn("harness_changed", "phiên bản harness đổi giữa chừng: %s", strings.Join(sortedKeys(hv), ", "))
	}
	errs, total := 0, 0
	for _, r := range rows {
		total++
		if r.Status == "error" || r.Status == "running" {
			errs++
		}
	}
	if total > 0 && float64(errs)/float64(total) > 0.1 {
		warn("errors", "%d/%d lượt lỗi hạ tầng (không chấm được); xem log trong thư mục eval-work/logs", errs, total)
	}
	for _, a := range rep.Arms {
		if a.Skipped > 0 {
			info("skipped", "nhánh %s: %d lượt bị bỏ qua (nhánh không khả dụng cho tác vụ)", a.Arm, a.Skipped)
		}
		if a.CasesWithRepeats == 0 && a.Graded > 0 {
			warn("no_repeats", "nhánh %s: không tác vụ nào có ≥2 lần lặp chấm được, không đo được độ dao động", a.Arm)
		}
		if a.PassRate.N >= 3 && (a.PassRate.Mean >= 0.95 || a.PassRate.Mean <= 0.05) {
			info("ceiling_floor", "nhánh %s đạt %.0f%%: tập tác vụ quá dễ hoặc quá khó, ít khả năng phân biệt các nhánh", a.Arm, 100*a.PassRate.Mean)
		}
	}
	changed, unverified := 0, 0
	for id, set := range fps {
		if len(set) > 1 {
			changed++
			continue
		}
		if c, ok := current[id]; ok {
			for fp := range set {
				if fp != c.Fingerprint() {
					changed++
				}
			}
			if !c.Verified() {
				unverified++
			}
		}
	}
	if changed > 0 {
		warn("case_changed", "%d tác vụ đã bị sửa (đề bài/lệnh chấm) so với lúc chạy; không gộp kết quả trước và sau khi sửa", changed)
	}
	if unverified > 0 {
		warn("unverified", "%d tác vụ chưa qua kiểm chứng fail→pass (bint eval verify): lệnh chấm có thể không phân biệt lời giải", unverified)
	}
	if touched > 0 {
		info("touched_tests", "%d lượt agent có sửa tệp kiểm thử (tệp kiểm thử chấm điểm vẫn được ghi đè bằng bản của commit)", touched)
	}
	info("scope", "P1 chỉ chẩn đoán được nhóm \"đo lường không đủ tin cậy\" (mục 12.9); các nguyên nhân còn lại cần nhánh tham chiếu lý tưởng và nhánh bint (P2–P3)")
	return ds
}

// finite trả về nil cho NaN/Inf (không đo được), để JSON ghi null thay vì lỗi.
func finite(v float64) *float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return &v
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// EstimateInput là đầu vào cho ước tính trước khi chạy (mục 12.5).
type EstimateInput struct {
	Cases      int
	Arms       []string
	Repeats    int
	Harness    string
	Model      string
	CostPerRun *float64 // ghi đè khi chưa có lịch sử
	Effect     float64  // chênh lệch tỷ lệ đạt muốn phát hiện (0..1)
}

// Estimate là ước tính cỡ mẫu và chi phí.
type Estimate struct {
	Runs           int      `json:"runs"`
	CostPerRun     *float64 `json:"cost_per_run_usd"`
	CostSource     string   `json:"cost_source"`
	TotalCost      *float64 `json:"total_cost_usd"`
	SecondsPerRun  *float64 `json:"seconds_per_run"`
	SDDiff         float64  `json:"sd_diff"`
	SDSource       string   `json:"sd_source"`
	Effect         float64  `json:"effect"`
	NeededCases    int      `json:"needed_cases"`
	AvailableCases int      `json:"available_cases"`
}

// EstimateRun tính số lượt, chi phí = tác vụ × lần lặp × nhánh × chi phí trung bình
// mỗi lượt, và số tác vụ cần để phát hiện Effect với độ dao động đã đo.
func EstimateRun(ctx context.Context, db *sql.DB, in EstimateInput) (Estimate, error) {
	e := Estimate{Runs: in.Cases * len(in.Arms) * in.Repeats, Effect: in.Effect, AvailableCases: in.Cases}
	switch {
	case in.CostPerRun != nil:
		e.CostPerRun, e.CostSource = in.CostPerRun, "do người dùng nhập"
	default:
		avg, n, err := HistoricalCost(ctx, db, in.Harness, in.Model)
		if err != nil {
			return e, err
		}
		if n > 0 {
			e.CostPerRun = &avg
			e.CostSource = fmt.Sprintf("trung bình %d lượt trước", n)
		} else {
			e.CostSource = "chưa có lịch sử"
		}
	}
	if e.CostPerRun != nil {
		t := *e.CostPerRun * float64(e.Runs)
		e.TotalCost = &t
	}
	var avgDur sql.NullFloat64
	if err := db.QueryRowContext(ctx, `SELECT AVG(duration_ms) FROM evaluation_runs WHERE harness = ? AND status IN ('passed','failed','timeout')`,
		in.Harness).Scan(&avgDur); err != nil {
		return e, err
	}
	if avgDur.Valid {
		s := avgDur.Float64 / 1000
		e.SecondsPerRun = &s
	}
	sd, n, err := historicalSDDiff(ctx, db, in.Harness)
	if err != nil {
		return e, err
	}
	if n >= 3 && sd > 0 {
		e.SDDiff, e.SDSource = sd, fmt.Sprintf("đo từ %d cặp tác vụ trước", n)
	} else {
		e.SDDiff, e.SDSource = AssumedSDDiff, "giả định thận trọng (chưa đủ dữ liệu)"
	}
	e.NeededCases = PairedSampleSize(e.SDDiff, in.Effect)
	return e, nil
}

// historicalSDDiff ước lượng độ lệch chuẩn của chênh lệch tỷ lệ đạt theo tác vụ
// giữa hai nhánh bất kỳ trong cùng đợt, gộp mọi đợt trước của harness.
func historicalSDDiff(ctx context.Context, db *sql.DB, harness string) (float64, int, error) {
	rows, err := db.QueryContext(ctx, `SELECT batch_id, case_id, arm, AVG(test_passed) FROM evaluation_runs
		WHERE harness = ? AND test_passed IS NOT NULL GROUP BY batch_id, case_id, arm ORDER BY batch_id, case_id, arm`, harness)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	type key struct{ batch, c string }
	m := map[key]map[string]float64{}
	var order []key
	for rows.Next() {
		var k key
		var arm string
		var v float64
		if err := rows.Scan(&k.batch, &k.c, &arm, &v); err != nil {
			return 0, 0, err
		}
		if m[k] == nil {
			m[k] = map[string]float64{}
			order = append(order, k)
		}
		m[k][arm] = v
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	var diffs []float64
	for _, k := range order {
		arms := m[k]
		base, ok := arms[ArmNone]
		if !ok {
			continue
		}
		for arm, v := range arms {
			if arm != ArmNone {
				diffs = append(diffs, v-base)
			}
		}
	}
	if len(diffs) < 2 {
		return 0, len(diffs), nil
	}
	return SD(diffs), len(diffs), nil
}

// MarshalJSON ghi mean = null khi không tính được.
func (s Stat) MarshalJSON() ([]byte, error) {
	type out struct {
		Mean *float64 `json:"mean"`
		CI   CI       `json:"ci95"`
		N    int      `json:"n"`
	}
	return json.Marshal(out{Mean: finite(s.Mean), CI: s.CI, N: s.N})
}
