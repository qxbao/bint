package evolve

import (
	"fmt"
	"slices"
	"sort"

	"bint/internal/pathglob"
	"bint/internal/runlog"
	"bint/internal/skills"
)

// Loại cơ hội tiến hoá skill (mục 10.2, 10.5).
const (
	// KindSkillCandidate: một khu vực mã có quy trình làm việc được xác nhận hiệu quả ở
	// nhiều lượt (nhãn thành công) mà chưa có skill nào bao phủ — gói quy trình đó thành skill.
	KindSkillCandidate = "skill_candidate"
	// KindSkillMerge: hai skill tiến hoá trùng chức năng và chồng điều kiện kích hoạt.
	KindSkillMerge = "skill_merge"
	// KindSkillStale: skill tiến hoá mà điều kiện kích hoạt không còn khớp tệp nào.
	KindSkillStale = "skill_stale"
	// KindSkillIgnored: skill thoả điều kiện kích hoạt ở nhiều lượt nhưng hầu như không được
	// kích hoạt (sự kiện skill.activated) — mô tả có thể chưa khớp cách agent tìm skill (mục 10.2).
	KindSkillIgnored = "skill_ignored"
)

// skillEventHarnesses: harness có công cụ gọi skill đi qua hook, nên có sự kiện skill.activated.
// Lượt của harness khác không là bằng chứng "bị bỏ qua": thiếu sự kiện không có nghĩa không kích hoạt.
var skillEventHarnesses = []string{"claude"}

// skillIgnoredRate: tỷ lệ lượt đủ điều kiện mà skill được kích hoạt, dưới mức này coi là bị bỏ qua.
const skillIgnoredRate = 0.2

// skillMergeSimilarity: mô tả tương đồng từ ngưỡng này (và chồng kích hoạt) thì đề xuất gộp.
const skillMergeSimilarity = 0.5

// scanSkills thêm cơ hội L2 vào kết quả quét. Cơ hội "skill bị bỏ qua" chỉ dựa vào lượt của
// harness có sự kiện skill.activated (skillEventHarnesses) và chỉ khi lịch sử có ít nhất một
// sự kiện như vậy; sự kiện không hỗ trợ không được tổng hợp giả (mục 13.2).
func scanSkills(in ScanInput, h *History, groups map[string]*Group, minRuns int, add func(Opportunity, int)) {
	var active []*skills.Evolved
	for _, e := range in.Evolved {
		if e.Active() {
			active = append(active, e)
		}
	}
	covers := func(f string) bool {
		for _, e := range active {
			if stale, _ := e.Stale(in.Files); !stale && pathglob.MatchAny(e.Meta.Triggers.Paths, f) {
				return true
			}
		}
		return false
	}
	// Quy trình hiệu quả lặp lại → skill mới.
	for _, mod := range sortedKeys(groups) {
		g := groups[mod]
		var good []string
		touch := map[string]*FileStat{}
		for _, r := range g.runs {
			if r.Label != runlog.LabelSuccess {
				continue
			}
			good = append(good, r.ID)
			for _, f := range r.Touched() {
				if touch[f] == nil {
					touch[f] = &FileStat{Path: f}
				}
				touch[f].Runs++
			}
		}
		var files []FileStat
		for _, f := range sortedKeys(touch) {
			if touch[f].Runs >= minRuns && !covers(f) {
				files = append(files, *touch[f])
			}
		}
		if len(files) == 0 || len(good) < minRuns || g.Failure*2 > g.Labeled {
			continue
		}
		o := Opportunity{Key: "skill:" + mod, Level: LevelL2, Kind: KindSkillCandidate, Action: ActionCreate, Generator: GenAgent,
			Group: mod, Files: topFiles(files, 8), SupportingRuns: good}
		o.Summary = fmt.Sprintf("%d lượt thành công trong %s chạm cùng %d tệp (≥ %d lượt/tệp) mà chưa có skill nào bao phủ: gói quy trình thành skill chuyên biệt",
			len(good), mod, len(files), minRuns)
		add(o, minRuns)
	}
	// Skill trùng chức năng → gộp; skill lỗi thời → loại bỏ.
	sort.Slice(active, func(i, j int) bool { return active[i].Meta.Name < active[j].Meta.Name })
	for _, e := range active {
		if stale, why := e.Stale(in.Files); stale {
			add(Opportunity{Key: "skill-stale:" + e.Meta.Name, Level: LevelL2, Kind: KindSkillStale, Action: ActionDeprecate,
				Generator: GenDeterministic, Targets: []string{e.Meta.Name}, Ready: true, SupportingRuns: []string{},
				Summary: fmt.Sprintf("skill %s lỗi thời: %s", e.Meta.Name, why)}, 0)
		}
	}
	if h.SkillData {
		for _, e := range active {
			if stale, _ := e.Stale(in.Files); stale || len(e.Meta.Triggers.Paths) == 0 {
				continue
			}
			var eligible []string
			activated := 0
			for _, r := range h.Runs {
				if !r.Labeled() || !slices.Contains(skillEventHarnesses, r.Harness) || len(e.Meta.Triggers.MatchPaths(r.Touched())) == 0 {
					continue
				}
				eligible = append(eligible, r.ID)
				if slices.Contains(r.Skills, e.Meta.Name) {
					activated++
				}
			}
			if len(eligible) >= minRuns && float64(activated) < skillIgnoredRate*float64(len(eligible)) {
				add(Opportunity{Key: "skill-ignored:" + e.Meta.Name, Level: LevelL2, Kind: KindSkillIgnored, Action: ActionUpdate,
					Generator: GenAgent, Targets: []string{e.Meta.Name}, SupportingRuns: eligible,
					Summary: fmt.Sprintf("skill %s được kích hoạt ở %d/%d lượt đủ điều kiện (< %.0f%%): mô tả có thể chưa khớp cách agent tìm skill",
						e.Meta.Name, activated, len(eligible), 100*skillIgnoredRate)}, minRuns)
			}
		}
	}
	for i := range active {
		for j := i + 1; j < len(active); j++ {
			a, b := active[i], active[j]
			if sim := jaccard(tokens(a.Doc().Description), tokens(b.Doc().Description)); sim >= skillMergeSimilarity && triggersOverlap(a.Meta.Triggers, b.Meta.Triggers, in.Files) {
				add(Opportunity{Key: "skill-merge:" + a.Meta.Name + "+" + b.Meta.Name, Level: LevelL2, Kind: KindSkillMerge, Action: ActionMerge,
					Generator: GenDeterministic, Targets: []string{a.Meta.Name, b.Meta.Name}, Ready: true, SupportingRuns: []string{},
					Summary: fmt.Sprintf("skill %s và %s trùng chức năng (mô tả tương đồng %.0f%%) và chồng điều kiện kích hoạt: harness dễ chọn nhầm",
						a.Meta.Name, b.Meta.Name, 100*sim)}, 0)
			}
		}
	}
}

// triggersOverlap: hai skill có thể cùng kích hoạt cho một tệp. Có danh sách tệp thì
// đối chiếu theo tệp thật; không thì theo mẫu.
func triggersOverlap(a, b skills.Triggers, files []string) bool {
	if len(a.Paths) == 0 || len(b.Paths) == 0 {
		return len(a.Paths) == 0 && len(b.Paths) == 0 // cùng chỉ có từ khoá
	}
	for _, f := range files {
		if pathglob.MatchAny(a.Paths, f) && pathglob.MatchAny(b.Paths, f) {
			return true
		}
	}
	for _, p := range a.Paths {
		if slices.Contains(b.Paths, p) {
			return true
		}
	}
	return false
}
