package evolve

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/eval"
	"bint/internal/knowledge"
	"bint/internal/runlog"
	"bint/internal/skills"
)

// Loại cơ hội tiến hoá (mục 10.2).
const (
	// KindExploration: tệp trong một khu vực được chạm lại ở nhiều lượt có nhãn mà
	// không có tri thức nào áp dụng — agent lặp lại việc tìm cùng một thông tin.
	KindExploration = "exploration"
	// KindFailureHotspot: cùng tệp bị sửa trong nhiều lượt thất bại, chưa có cảnh báo.
	KindFailureHotspot = "failure_hotspot"
	// KindStaleUsed: mục lỗi thời vẫn được truy xuất, hoặc agent nhận định đã sai.
	KindStaleUsed = "stale_used"
	// KindStaleExpired: mục lỗi thời quá knowledge.stale_deprecate_days (mục 8.4).
	KindStaleExpired = "stale_expired"
	// KindUnused: mục được truy xuất nhiều nhưng chưa từng được trích dẫn — chỉ là gợi ý;
	// tác động thực chỉ xác định được bằng đánh giá loại bỏ (mục 9.7).
	KindUnused = "unused"
	// KindDuplicate: hai mục cùng loại, phạm vi chồng nhau, tiêu đề gần trùng.
	KindDuplicate = "duplicate"
	// KindFailureLesson: một nhóm (vùng mã hoặc dấu hiệu hành vi) có ≥ min tác vụ thất bại
	// trên tập validation — rút bài học từ thất bại (kế hoạch E, reflector) → create lesson.
	KindFailureLesson = "failure_lesson"
)

// Hành động của đề xuất (mục 10.4).
const (
	ActionCreate    = "create"
	ActionUpdate    = "update"
	ActionMerge     = "merge"
	ActionDeprecate = "deprecate"
	ActionDelete    = "delete"
)

// Actions là các hành động hợp lệ.
var Actions = []string{ActionCreate, ActionUpdate, ActionMerge, ActionDeprecate, ActionDelete}

// Cách tạo đề xuất.
const (
	GenDeterministic = "deterministic" // Bint tự dựng, không gọi mô hình
	GenAgent         = "agent"         // agent evolver soạn nội dung
)

// FileStat là một tệp trong cơ hội, kèm số lượt có nhãn đã chạm và số lượt thất bại.
type FileStat struct {
	Path     string `json:"path"`
	Runs     int    `json:"runs"`
	Failures int    `json:"failures"`
	// Members: với mẫu anh em (Path chứa "*"), các tệp cụ thể đã được chạm; Runs là số lượt
	// có nhãn phân biệt chạm ít nhất một thành viên.
	Members []string `json:"members,omitempty"`
	runIDs  []string
}

// Opportunity là một cơ hội tiến hoá L1.
type Opportunity struct {
	ID  string `json:"id"`
	Key string `json:"key"`
	// Level: "" hoặc L1 = tri thức; L2 = skill (P7).
	Level     string     `json:"level,omitempty"`
	Kind      string     `json:"kind"`
	Action    string     `json:"action"`
	Generator string     `json:"generator"`
	Group     string     `json:"group,omitempty"`
	Targets   []string   `json:"targets,omitempty"`
	Files     []FileStat `json:"files,omitempty"`
	// SupportingRuns: chỉ lượt có nhãn (mục 9.6).
	SupportingRuns []string `json:"supporting_runs"`
	NewEvidence    int      `json:"new_evidence"`
	Summary        string   `json:"summary"`
	Ready          bool     `json:"ready"`
	Blocked        string   `json:"blocked,omitempty"`
}

// Group là một nhóm tác vụ tương tự (mục 10.3): MVP nhóm theo khu vực mã chính.
type Group struct {
	Module    string         `json:"module"`
	Runs      int            `json:"runs"`
	Labeled   int            `json:"labeled"`
	Success   int            `json:"success"`
	Failure   int            `json:"failure"`
	Workflows map[string]int `json:"workflows"`
	// AvgReads: số lần đọc tệp trung bình mỗi lượt (chỉ lượt có dữ liệu đọc) — chi phí khám phá.
	AvgReads *float64 `json:"avg_reads"`
	AvgCost  *float64 `json:"avg_cost_usd"`
	runs     []*Run
}

// Check là một điều kiện tiên quyết (mục 10.6).
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// Prerequisites cho biết đề xuất có thể được kiểm chứng định lượng hay chỉ là đề xuất
// L1 chưa kiểm chứng chờ người duyệt (mục 10.6).
type Prerequisites struct {
	Quantitative bool           `json:"quantitative"`
	Checks       []Check        `json:"checks"`
	Cases        map[string]int `json:"cases"` // tác vụ đã duyệt và qua kiểm chứng, theo tập
	NeededCases  int            `json:"needed_cases"`
	ProbeUSD     *float64       `json:"estimated_probe_usd"`
}

// ScanResult là kết quả `bint evolve scan`.
type ScanResult struct {
	Since         string        `json:"since"`
	Runs          int           `json:"runs"`
	Labeled       int           `json:"labeled"`
	ReadData      int           `json:"runs_with_read_data"`
	Groups        []Group       `json:"groups"`
	Opportunities []Opportunity `json:"opportunities"`
	Prerequisites Prerequisites `json:"prerequisites"`
	// failures: đơn vị thất bại đã nạp (dùng để rút bài học khi propose; không ghi JSON).
	failures []Failure
}

// ScanInput gom đầu vào của Scan.
type ScanInput struct {
	DB      *sql.DB
	Cfg     config.Config
	Items   []*knowledge.Item
	Cases   []*eval.Case
	Now     time.Time
	Window  time.Duration
	History *History // nil = nạp từ DB
	// Evolved và Files: skill tiến hoá hiện có và tệp theo dõi của repository, cho cơ hội
	// L2 (nil = không quét skill).
	Evolved []*skills.Evolved
	Files   []string
	// Runtime: thư mục trạng thái runtime (đọc phạm vi và mục tiêu của phiếu cho lượt thật
	// thất bại); rỗng = không đọc phiếu.
	Runtime string
}

// DefaultWindow là cửa sổ lịch sử mặc định khi memory.retention_days không đặt.
const DefaultWindow = 90 * 24 * time.Hour

// OpportunityID là ID ngắn ổn định của một khoá cơ hội.
func OpportunityID(key string) string {
	s := sha256.Sum256([]byte(key))
	return "op-" + hex.EncodeToString(s[:])[:8]
}

// Scan phát hiện cơ hội tiến hoá tri thức. Không gọi mô hình; chỉ đọc.
func Scan(ctx context.Context, in ScanInput) (ScanResult, error) {
	var res ScanResult
	if in.Now.IsZero() {
		in.Now = time.Now()
	}
	if in.Window <= 0 {
		in.Window = DefaultWindow
		if d := in.Cfg.Memory.RetentionDays; d > 0 {
			in.Window = time.Duration(d) * 24 * time.Hour
		}
	}
	res.Since = knowledge.Now(in.Now.Add(-in.Window))
	h := in.History
	if h == nil {
		var err error
		if h, err = LoadHistory(ctx, in.DB, in.Now, in.Window); err != nil {
			return res, err
		}
	}
	used, pending, err := proposalEvidence(ctx, in.DB)
	if err != nil {
		return res, err
	}
	m, e := in.Cfg.Evolution.Trigger.MinSimilarRuns, in.Cfg.Evolution.Trigger.MinNewEvidence
	if m < 1 {
		m = 1
	}
	if e < 1 {
		e = 1
	}

	modOf := assignModules(h.Runs, in.Cfg.Evolution.Trigger.GroupDepth, m)
	groups := map[string]*Group{}
	for _, r := range h.Runs {
		res.Runs++
		if r.Labeled() {
			res.Labeled++
		}
		if len(r.Read) > 0 {
			res.ReadData++
		}
		mod := modOf[r.ID]
		if mod == "" {
			continue // lượt không chạm tệp nào: không quy được về khu vực mã
		}
		g := groups[mod]
		if g == nil {
			g = &Group{Module: mod, Workflows: map[string]int{}}
			groups[mod] = g
		}
		g.runs = append(g.runs, r)
		g.Runs++
		g.Workflows[orUnknown(r.Workflow)]++
		switch r.Label {
		case runlog.LabelSuccess:
			g.Labeled++
			g.Success++
		case runlog.LabelFailure:
			g.Labeled++
			g.Failure++
		}
	}
	for _, g := range groups {
		var reads, costs []float64
		for _, r := range g.runs {
			if r.ReadCount > 0 {
				reads = append(reads, float64(r.ReadCount))
			}
			if r.CostUSD != nil {
				costs = append(costs, *r.CostUSD)
			}
		}
		g.AvgReads, g.AvgCost = meanPtr(reads), meanPtr(costs)
		res.Groups = append(res.Groups, *g)
	}
	sort.Slice(res.Groups, func(i, j int) bool {
		if res.Groups[i].Runs != res.Groups[j].Runs {
			return res.Groups[i].Runs > res.Groups[j].Runs
		}
		return res.Groups[i].Module < res.Groups[j].Module
	})

	active := activeItems(in.Items)
	// add tính bằng chứng mới; need > 0: cơ hội dựa trên lượt chạy, cần ≥ need lượt có
	// nhãn và ≥ e lượt có nhãn chưa dùng cho đề xuất trước cùng khoá.
	add := func(o Opportunity, need int) {
		o.ID = OpportunityID(o.Key)
		sort.Strings(o.SupportingRuns)
		if o.SupportingRuns == nil {
			o.SupportingRuns = []string{}
		}
		for _, id := range o.SupportingRuns {
			if !used[o.Key][id] {
				o.NewEvidence++
			}
		}
		if need > 0 {
			o.Ready = false
			switch {
			case len(o.SupportingRuns) < need:
				o.Blocked = fmt.Sprintf("chỉ %d lượt có nhãn làm căn cứ (cần ≥ %d, evolution.trigger)", len(o.SupportingRuns), need)
			case o.NewEvidence < e:
				o.Blocked = fmt.Sprintf("chỉ %d lượt có nhãn mới so với đề xuất trước cùng đối tượng (cần ≥ %d, evolution.trigger.min_new_evidence)", o.NewEvidence, e)
			default:
				o.Ready = true
			}
		}
		if p, ok := pending[o.Key]; ok {
			o.Ready, o.Blocked = false, "đã có đề xuất "+p+" chờ xử lý"
		}
		res.Opportunities = append(res.Opportunities, o)
	}

	// Theo nhóm: khám phá lặp lại và điểm nóng thất bại.
	for _, g := range res.Groups {
		g := groups[g.Module]
		touch := map[string]*FileStat{}
		support := map[string][]string{}
		for _, r := range g.runs {
			if !r.Labeled() {
				continue
			}
			for _, f := range r.Touched() {
				fs := touch[f]
				if fs == nil {
					fs = &FileStat{Path: f}
					touch[f] = fs
				}
				fs.Runs++
				if r.Label == runlog.LabelFailure && slices.Contains(r.Modified, f) {
					fs.Failures++
				}
				support[f] = append(support[f], r.ID)
			}
		}
		var explore, hot []FileStat
		exploreRuns, hotRuns := map[string]bool{}, map[string]bool{}
		for _, sib := range siblingStats(support, m, func(f string) bool { return covered(active, f, nil) }) {
			explore = append(explore, sib)
			for _, id := range sib.runIDs {
				exploreRuns[id] = true
			}
		}
		for _, f := range sortedKeys(touch) {
			fs := touch[f]
			if fs.Runs >= m && !covered(active, f, nil) {
				explore = append(explore, *fs)
				for _, id := range support[f] {
					exploreRuns[id] = true
				}
			}
			if fs.Failures >= e && !covered(active, f, []string{knowledge.TypeWarning, knowledge.TypeRule}) {
				hot = append(hot, *fs)
				for _, id := range support[f] {
					if r := h.ByID[id]; r != nil && r.Label == runlog.LabelFailure {
						hotRuns[id] = true
					}
				}
			}
		}
		if len(explore) > 0 {
			explore = topFiles(explore, 8)
			o := Opportunity{Key: "exploration:" + g.Module, Kind: KindExploration, Action: ActionCreate, Generator: GenAgent,
				Group: g.Module, Files: explore, SupportingRuns: sortedKeys(exploreRuns)}
			reads := ""
			if g.AvgReads != nil {
				reads = fmt.Sprintf(", trung bình %.0f lần đọc tệp mỗi lượt", *g.AvgReads)
			}
			o.Summary = fmt.Sprintf("%d tệp trong %s được chạm lại ở ≥ %d lượt có nhãn mà không có tri thức nào áp dụng%s",
				len(explore), g.Module, m, reads)
			add(o, m)
		}
		if len(hot) > 0 {
			hot = topFiles(hot, 8)
			o := Opportunity{Key: "failure:" + g.Module, Kind: KindFailureHotspot, Action: ActionCreate, Generator: GenAgent,
				Group: g.Module, Files: hot, SupportingRuns: sortedKeys(hotRuns)}
			o.Summary = fmt.Sprintf("%d tệp trong %s bị sửa ở ≥ %d lượt thất bại, chưa có cảnh báo/quy tắc nào áp dụng", len(hot), g.Module, e)
			add(o, e)
		}
	}

	// Theo skill tiến hoá (L2).
	scanSkills(in, h, groups, m, add)

	// Theo mục tri thức.
	labeledOf := func(ids []string) []string {
		var out []string
		for _, id := range ids {
			if r := h.ByID[id]; r != nil && r.Labeled() {
				out = append(out, id)
			}
		}
		return out
	}
	staleDays := in.Cfg.Knowledge.StaleDeprecateDays
	for _, it := range active {
		switch it.Status {
		case knowledge.StatusStale:
			retrieved := h.RetrievedRuns[it.ID]
			if len(retrieved) > 0 || h.Revalidation[it.ID] == "outdated" {
				why := fmt.Sprintf("mục lỗi thời vẫn được truy xuất ở %d lượt", len(retrieved))
				if h.Revalidation[it.ID] == "outdated" {
					why = "agent nhận định mục đã sai so với mã hiện tại (bint knowledge revalidate)"
				}
				add(Opportunity{Key: "stale:" + it.ID, Kind: KindStaleUsed, Action: ActionUpdate, Generator: GenAgent,
					Targets: []string{it.ID}, SupportingRuns: labeledOf(retrieved), Ready: true,
					Summary: fmt.Sprintf("%s (%s): %s", it.ID, it.Title, why)}, 0)
			}
			if staleDays > 0 && it.UpdatedAt != "" {
				if t, err := time.Parse(time.RFC3339, it.UpdatedAt); err == nil && in.Now.Sub(t) > time.Duration(staleDays)*24*time.Hour {
					add(Opportunity{Key: "expired:" + it.ID, Kind: KindStaleExpired, Action: ActionDeprecate, Generator: GenDeterministic,
						Targets: []string{it.ID}, Ready: true, SupportingRuns: []string{},
						Summary: fmt.Sprintf("%s (%s) lỗi thời quá %d ngày mà chưa được xác thực lại", it.ID, it.Title, staleDays)}, 0)
				}
			}
		case knowledge.StatusVerified, knowledge.StatusCandidate:
			// Bài học từ thất bại là hướng dẫn hành vi: hiếm khi được trích bằng [k:id], nên "không
			// trích dẫn" không phải tín hiệu vô ích. Chúng được đo bằng nhãn lượt thật và kết quả
			// đánh giá (bint experience show), không qua cơ hội unused.
			if it.LessonFromRuns() {
				continue
			}
			ret := labeledOf(h.RetrievedRuns[it.ID])
			if len(ret) >= m && len(h.CitedRuns[it.ID]) == 0 {
				o := Opportunity{Key: "unused:" + it.ID, Kind: KindUnused, Action: ActionDeprecate, Generator: GenDeterministic,
					Targets: []string{it.ID}, SupportingRuns: ret,
					Summary: fmt.Sprintf("%s (%s) được truy xuất ở %d lượt có nhãn nhưng chưa lần nào được trích dẫn; chỉ đánh giá loại bỏ mới xác định được tác động",
						it.ID, it.Title, len(ret))}
				add(o, m)
			}
		}
	}
	for _, pair := range duplicates(active) {
		a, b := pair[0], pair[1]
		add(Opportunity{Key: "duplicate:" + a.ID + "+" + b.ID, Kind: KindDuplicate, Action: ActionMerge, Generator: GenDeterministic,
			Targets: []string{a.ID, b.ID}, Ready: true, SupportingRuns: []string{},
			Summary: fmt.Sprintf("%s và %s cùng loại %s, phạm vi chồng nhau, tiêu đề gần trùng", a.ID, b.ID, a.Type)}, 0)
	}
	// Theo lượt thất bại (E1): tập validation và lượt thật có nhãn failure → bài học chung/riêng.
	fails, err := LoadFailures(ctx, in.DB, in.Cases, h, in.Runtime, in.Now.Add(-in.Window))
	if err != nil {
		return res, err
	}
	res.failures = fails
	for _, o := range failureLessonOpps(fails, m, e, used, pending) {
		add(o, 0)
	}
	sort.SliceStable(res.Opportunities, func(i, j int) bool {
		a, b := res.Opportunities[i], res.Opportunities[j]
		if a.Ready != b.Ready {
			return a.Ready
		}
		return a.Key < b.Key
	})
	res.Prerequisites, err = CheckPrerequisites(ctx, in.DB, in.Cfg, in.Cases, res.Labeled)
	return res, err
}

// CheckPrerequisites kiểm tra điều kiện tiên quyết về dữ liệu (mục 10.6).
func CheckPrerequisites(ctx context.Context, db *sql.DB, cfg config.Config, cases []*eval.Case, labeled int) (Prerequisites, error) {
	p := Prerequisites{Cases: map[string]int{}}
	ev := cfg.Evolution.Evaluation
	add := func(name string, ok bool, f string, a ...any) {
		p.Checks = append(p.Checks, Check{Name: name, OK: ok, Detail: fmt.Sprintf(f, a...)})
	}
	m := cfg.Evolution.Trigger.MinSimilarRuns
	add("labeled_runs", labeled >= m, "%d lượt có nhãn trong cửa sổ (cần ≥ %d trong nhóm tác vụ liên quan; xem từng cơ hội)", labeled, m)
	add("evaluation_enabled", ev.Enabled, "evolution.evaluation.enabled = %v", ev.Enabled)
	for _, c := range cases {
		if c.Verified() {
			p.Cases[c.Split]++
		}
	}
	// Kế hoạch Probe: R do bộ lập kế hoạch chọn (repeats = 0) hoặc cố định (> 0).
	est, err := eval.EstimateRun(ctx, db, eval.EstimateInput{Cases: p.Cases[eval.SplitTest], Arms: []string{eval.ArmBase, eval.ArmCandidate},
		Repeats: max(ev.Repeats, 1), Harness: cfg.Eval.Harness, Effect: ev.MinEffect, EffectSD: ev.EffectSD})
	if err != nil {
		return p, err
	}
	pin := eval.PlanInput{Available: p.Cases[eval.SplitTest], Effect: ev.MinEffect, SigmaB: est.SigmaB, SigmaBMeasured: est.SigmaMeasured, W: est.W,
		MaxRepeats: ev.MaxRepeats, FixedRepeats: ev.Repeats}
	if est.CostPerRun != nil {
		pin.CostPerRun = *est.CostPerRun
	}
	plan := eval.PlanProbe(pin)
	// Cỡ mẫu cần có: n(R) của kế hoạch khi khả thi, n ở R lớn nhất khi không.
	p.NeededCases = plan.Cases
	runs := plan.MaxRuns
	if !plan.Feasible {
		p.NeededCases = eval.ProbeSampleSize(plan.Sigma, ev.MinEffect)
		runs = p.NeededCases * 2 * plan.Repeats
	}
	add("validation_cases", p.Cases[eval.SplitValidation] > 0, "%d tác vụ tập validation đã kiểm chứng (để xác thực ứng viên)", p.Cases[eval.SplitValidation])
	add("test_cases", plan.Feasible,
		"%d tác vụ tập test đã kiểm chứng; cần ~%d (R = %d) để phát hiện chênh lệch %.0f điểm %% (σ_b %.2f theo %s; W %.2f theo %s)",
		p.Cases[eval.SplitTest], p.NeededCases, plan.Repeats, 100*ev.MinEffect, plan.SigmaB, sigmaNote(est.SigmaB, plan.SigmaB, est.SigmaBSource), est.W, est.WSource)
	if est.CostPerRun != nil {
		v := *est.CostPerRun * float64(runs)
		p.ProbeUSD = &v
	}
	switch {
	case p.ProbeUSD == nil:
		add("probe_budget", false, "chưa có lịch sử chi phí của harness %s để ước tính vòng Probe", cfg.Eval.Harness)
	case ev.BudgetUSD == nil:
		add("probe_budget", false, "ước tính ~$%.2f mỗi lần đối chiếu; chưa duyệt ngân sách (evolution.evaluation.budget_usd)", *p.ProbeUSD)
	default:
		add("probe_budget", *p.ProbeUSD <= *ev.BudgetUSD, "ước tính ~$%.2f mỗi lần đối chiếu; ngân sách $%.2f", *p.ProbeUSD, *ev.BudgetUSD)
	}
	p.Quantitative = true
	for _, c := range p.Checks {
		p.Quantitative = p.Quantitative && c.OK
	}
	return p, nil
}

func activeItems(items []*knowledge.Item) []*knowledge.Item {
	var out []*knowledge.Item
	for _, it := range items {
		if it.Active() {
			out = append(out, it)
		}
	}
	return out
}

// covered cho biết có mục còn hiệu lực (kể cả candidate chờ duyệt) với phạm vi đường
// dẫn cụ thể khớp tệp; types rỗng = mọi loại. Mục toàn repository không tính: chúng
// không chứa thông tin riêng của tệp.
func covered(items []*knowledge.Item, f string, types []string) bool {
	for _, it := range items {
		if it.Scope.Global() || (len(types) > 0 && !slices.Contains(types, it.Type)) {
			continue
		}
		if it.Scope.MatchScope(f) {
			return true
		}
	}
	return false
}

// siblingPattern tổng quát hoá một tệp thành mẫu tên chung trong cùng thư mục: hậu tố sau
// dấu "_" cuối ("handlers/user_handler.py" -> "handlers/*_handler.py", "a_test.go" ->
// "*_test.go") hoặc tiền tố "test_" ("test_x.py" -> "test_*.py"). Không có đặc trưng tên
// chung thì trả "" — không có mẫu theo phần mở rộng, vì quá rộng để coi là quy trình lặp lại.
func siblingPattern(f string) string {
	dir, base := path.Split(f)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if strings.HasPrefix(stem, "test_") && len(stem) > len("test_") {
		return dir + "test_*" + ext
	}
	if i := strings.LastIndex(stem, "_"); i > 0 && i < len(stem)-1 {
		return dir + "*" + stem[i:] + ext
	}
	return ""
}

// members mô tả các tệp thành viên của một mẫu anh em cho prompt (rỗng nếu là tệp lẻ).
func (f FileStat) members() string {
	if len(f.Members) == 0 {
		return ""
	}
	return " — gồm: " + truncate(strings.Join(f.Members, ", "), 300)
}

// fileSet: mọi đường dẫn tệp thật của cơ hội (tệp lẻ và thành viên của mẫu anh em), để khớp
// quan sát theo tệp.
func (o Opportunity) fileSet() map[string]bool {
	out := map[string]bool{}
	for _, f := range o.Files {
		out[f.Path] = true
		for _, m := range f.Members {
			out[m] = true
		}
	}
	return out
}

// siblingStats tìm quy trình lặp lại trên nhiều tệp anh em mà từng tệp riêng chưa đạt ngưỡng:
// gom tệp (chưa đạt ngưỡng tệp, không bị skip) theo siblingPattern; mẫu có ≥ 2 tệp và được
// ≥ minRuns lượt phân biệt chạm thì thành một đơn vị. Tệp đã đạt ngưỡng ở cấp tệp bị loại
// khỏi mẫu để không trùng cơ hội cấp tệp. support: tệp -> ID lượt có nhãn đã chạm.
func siblingStats(support map[string][]string, minRuns int, skip func(string) bool) []FileStat {
	type agg struct {
		files []string
		runs  map[string]bool
	}
	pats := map[string]*agg{}
	for _, f := range sortedKeys(support) {
		if len(support[f]) >= minRuns || skip(f) {
			continue
		}
		pt := siblingPattern(f)
		if pt == "" {
			continue
		}
		a := pats[pt]
		if a == nil {
			a = &agg{runs: map[string]bool{}}
			pats[pt] = a
		}
		a.files = append(a.files, f)
		for _, id := range support[f] {
			a.runs[id] = true
		}
	}
	var out []FileStat
	for _, pt := range sortedKeys(pats) {
		a := pats[pt]
		if len(a.files) >= 2 && len(a.runs) >= minRuns {
			out = append(out, FileStat{Path: pt, Runs: len(a.runs), Members: a.files, runIDs: sortedKeys(a.runs)})
		}
	}
	return out
}

func topFiles(fs []FileStat, n int) []FileStat {
	sort.SliceStable(fs, func(i, j int) bool {
		if fs[i].Runs != fs[j].Runs {
			return fs[i].Runs > fs[j].Runs
		}
		return fs[i].Path < fs[j].Path
	})
	if len(fs) > n {
		fs = fs[:n]
	}
	return fs
}

// duplicates tìm cặp mục cùng loại, phạm vi chồng nhau, tiêu đề gần trùng.
func duplicates(items []*knowledge.Item) [][2]*knowledge.Item {
	var out [][2]*knowledge.Item
	for i := range items {
		for j := i + 1; j < len(items); j++ {
			a, b := items[i], items[j]
			if a.Type != b.Type || !scopesOverlap(a.Scope, b.Scope) {
				continue
			}
			if jaccard(tokens(a.Title), tokens(b.Title)) >= 0.6 {
				out = append(out, [2]*knowledge.Item{a, b})
			}
		}
	}
	return out
}

func scopesOverlap(a, b knowledge.Scope) bool {
	if a.Global() || b.Global() {
		return a.Global() && b.Global()
	}
	for _, p := range a.Paths {
		if slices.Contains(b.Paths, p) || b.MatchScope(strings.TrimSuffix(strings.TrimSuffix(p, "**"), "/")) {
			return true
		}
	}
	for _, p := range b.Paths {
		if a.MatchScope(strings.TrimSuffix(strings.TrimSuffix(p, "**"), "/")) {
			return true
		}
	}
	return false
}

func tokens(s string) map[string]bool {
	out := map[string]bool{}
	for _, f := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r == '_' || r == '-' || r == '.' || r == '/' || ('0' <= r && r <= '9') || ('a' <= r && r <= 'z') || r > 127)
	}) {
		if len([]rune(f)) > 1 {
			out[f] = true
		}
	}
	return out
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if b[k] {
			inter++
		}
	}
	return float64(inter) / float64(len(a)+len(b)-inter)
}

func meanPtr(xs []float64) *float64 {
	if len(xs) == 0 {
		return nil
	}
	v := eval.Mean(xs)
	return &v
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func orUnknown(s string) string {
	if s == "" {
		return "?"
	}
	return s
}

// sigmaNote ghi nguồn σ_b, kèm ghi chú khi bộ lập kế hoạch nâng nó lên sàn mô hình lật.
func sigmaNote(assumed, used float64, src string) string {
	if used > assumed+1e-9 {
		return fmt.Sprintf("%s, nâng lên sàn mô hình lật vì tác vụ gần tất định", src)
	}
	return src
}
