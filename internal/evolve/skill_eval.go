package evolve

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"bint/internal/eval"
	"bint/internal/knowledge"
	"bint/internal/skills"
)

// SkillState là một skill như agent sẽ thấy ở một nhánh đối chiếu.
type SkillState struct {
	Name     string
	Content  []byte
	Triggers skills.Triggers
	Evidence []skills.EvidenceRef
}

// SkillSet là tập skill đang hoạt động của một nhánh, theo tên.
type SkillSet map[string]SkillState

func stateOf(e *skills.Evolved) SkillState {
	return SkillState{Name: e.Meta.Name, Content: e.Content, Triggers: e.Meta.Triggers, Evidence: e.Meta.Evidence}
}

// CurrentSet là tập skill tiến hoá đang hoạt động trong repository.
func CurrentSet(list []*skills.Evolved) SkillSet {
	out := SkillSet{}
	for _, e := range list {
		if e.Active() {
			out[e.Meta.Name] = stateOf(e)
		}
	}
	return out
}

// ApplyToSet trả về tập skill sau khi áp các thay đổi của đề xuất lên before.
func ApplyToSet(p *Proposal, before SkillSet, src knowledge.Source, fpPref string) (SkillSet, error) {
	after := SkillSet{}
	for n, s := range before {
		after[n] = s
	}
	for _, c := range p.SkillChanges {
		switch c.Op {
		case ActionDeprecate, ActionDelete:
			delete(after, c.Name)
		case ActionCreate, ActionUpdate:
			refs, err := evidenceRefs(c.Draft.Evidence, src, fpPref)
			if err != nil {
				return nil, err
			}
			after[c.Name] = SkillState{Name: c.Name, Content: c.Draft.Content(), Triggers: c.Draft.Triggers, Evidence: refs}
		}
	}
	return after, nil
}

// ReverseSet dựng lại tập skill TRƯỚC khi đề xuất đã áp dụng, từ tập hiện tại: bỏ skill
// do đề xuất tạo, trả skill bị sửa về phiên bản trước (versions/N-1.md), thêm lại skill
// bị loại bỏ. Dùng để kiểm tra hồi quy khi đổi mô hình (mục 12.6).
func ReverseSet(root string, p *Proposal, current []*skills.Evolved) (SkillSet, error) {
	byName := indexEvolved(current)
	before := CurrentSet(current)
	for _, c := range p.SkillChanges {
		cur := byName[c.Name]
		switch c.Op {
		case ActionCreate:
			delete(before, c.Name)
		case ActionUpdate:
			if cur == nil || cur.Meta.Version < 2 {
				return nil, fmt.Errorf("skill %s không có phiên bản trước để đối chiếu", c.Name)
			}
			prev, err := skills.ReadVersion(root, c.Name, cur.Meta.Version-1)
			if err != nil {
				return nil, err
			}
			s := stateOf(cur)
			s.Content = prev
			before[c.Name] = s
		case ActionDeprecate:
			if cur != nil {
				before[c.Name] = stateOf(cur)
			}
		}
	}
	return before, nil
}

// SkillVariant dựng cặp nhánh base/candidate cho đề xuất skill: cùng tập tri thức, khác
// ở tập skill đặt vào thư mục làm việc (before cho base, after cho candidate). Mỗi skill
// được đặt phải áp dụng được tại commit gốc của tác vụ: bằng chứng khớp và nội dung không
// chứa mã lời giải (giống tri thức mới, mục 12.3). Skill không thoả điều kiện bị bỏ khỏi
// CẢ HAI nhánh nếu không thuộc đề xuất; skill của đề xuất không thoả → nhánh không khả dụng.
func SkillVariant(p *Proposal, delivery, repoDir string, before, after SkillSet) *eval.Variant {
	changed := map[string]bool{}
	for _, c := range p.SkillChanges {
		changed[c.Name] = true
	}
	return &eval.Variant{
		Delivery: delivery,
		Label:    p.ID,
		Skills: func(ctx context.Context, arm string, c *eval.Case, leak *eval.LeakInput) (map[string][]byte, error) {
			set := before
			if arm == eval.ArmCandidate {
				set = after
			}
			out := map[string][]byte{}
			for n, s := range set {
				if err := skillApplicable(ctx, repoDir, c, leak, s); err != nil {
					if changed[n] {
						return nil, unavailable("%v", err)
					}
					continue
				}
				out[n] = s.Content
			}
			return out, nil
		},
	}
}

func skillApplicable(ctx context.Context, repoDir string, c *eval.Case, leak *eval.LeakInput, s SkillState) error {
	it := &knowledge.Item{Evidence: make([]knowledge.Evidence, len(s.Evidence))}
	for i, r := range s.Evidence {
		it.Evidence[i] = knowledge.Evidence{Path: r.Path, Lines: r.Lines, Symbol: r.Symbol, Commit: r.Commit, Fingerprint: r.Fingerprint}
	}
	if !eval.EvidenceHoldsAt(ctx, repoDir, c, it) {
		return fmt.Errorf("bằng chứng của skill %s không khớp tại commit gốc %s", s.Name, shortSHA(c.Source.Base))
	}
	if leak != nil {
		if r := eval.CheckLeak(string(s.Content), *leak); r.Status == "fail" {
			return fmt.Errorf("nội dung skill %s chứa mã của lời giải (%s)", s.Name, r.Findings[0].Detail)
		}
	}
	return nil
}

// skillImpact cho biết đề xuất skill có ảnh hưởng tác vụ không: skill bị thay đổi (cũ hoặc
// mới) thoả điều kiện kích hoạt với tệp hoặc đề bài của tác vụ, và tập skill của hai
// nhánh áp dụng được.
func skillImpact(ctx context.Context, env eval.Env, p *Proposal, c *eval.Case, leak *eval.LeakInput, before, after SkillSet) (bool, string, error) {
	files := append(append([]string{}, c.SourceFiles...), c.TestFiles...)
	var hit []string
	for _, ch := range p.SkillChanges {
		for _, set := range []SkillSet{before, after} {
			if s, ok := set[ch.Name]; ok && s.Triggers.Hits(files, c.Prompt) && !slices.Contains(hit, ch.Name) {
				hit = append(hit, ch.Name)
			}
		}
	}
	if len(hit) == 0 {
		return false, "điều kiện kích hoạt của skill thay đổi không khớp tệp hay đề bài của tác vụ", nil
	}
	if env.Variant != nil && env.Variant.Skills != nil {
		if _, err := env.Variant.Skills(ctx, eval.ArmCandidate, c, leak); err != nil {
			if errors.Is(err, eval.ErrArmUnavailable) {
				return false, err.Error(), nil
			}
			return false, "", err
		}
	}
	return true, fmt.Sprintf("skill %s thoả điều kiện kích hoạt với tác vụ", strings.Join(hit, ", ")), nil
}

// ---- Kiểm tra điều kiện kích hoạt ----

// Ngưỡng của kiểm tra điều kiện kích hoạt (xác định được, ghi rõ trong báo cáo).
const (
	minTriggerRecall    = 0.5  // tỷ lệ lượt thành công làm căn cứ mà skill kích hoạt được
	maxFalseTriggerRate = 0.35 // tỷ lệ lượt có nhãn khác (không làm căn cứ) mà skill cũng kích hoạt
	minNegativesToJudge = 3
)

// TriggerReport là kết quả kiểm tra điều kiện kích hoạt của một skill (mục P7).
type TriggerReport struct {
	Skill string         `json:"skill"`
	OK    bool           `json:"ok"`
	Paths map[string]int `json:"paths"` // mẫu → số tệp hiện có khớp
	Dead  []string       `json:"dead_paths,omitempty"`
	// Positives: lượt thành công làm căn cứ; Hit: số lượt mà điều kiện kích hoạt khớp tệp đã chạm.
	Positives    int `json:"positives"`
	PositiveHits int `json:"positive_hits"`
	Negatives    int `json:"negatives"`
	FalseHits    int `json:"false_hits"`
	// Kích hoạt thực tế (sự kiện skill.activated, chỉ harness có hook công cụ skill): số lượt đủ
	// điều kiện và số lượt skill thực sự được kích hoạt. Eligible = 0: không có dữ liệu.
	Eligible  int      `json:"eligible_runs,omitempty"`
	Activated int      `json:"activated_runs,omitempty"`
	Overlaps  []string `json:"overlaps,omitempty"`
	Problems  []string `json:"problems"`
	Notes     []string `json:"notes,omitempty"`
}

// CheckTriggerConditions đối chiếu điều kiện kích hoạt với cây mã và lịch sử: mẫu còn khớp
// tệp thật, kích hoạt đủ ở các lượt làm căn cứ (recall), không kích hoạt tràn lan ở các
// lượt có nhãn không liên quan (false trigger), không chồng chức năng với skill khác.
// Đây là kiểm tra theo đường dẫn: harness quyết định kích hoạt thật theo description, và
// Bint chưa nhận được sự kiện skill.activated để đo kích hoạt thực tế.
func CheckTriggerConditions(name, desc string, tr skills.Triggers, files []string, supporting []string, h *History, others []*skills.Evolved) TriggerReport {
	r := TriggerReport{Skill: name, Paths: map[string]int{}, Problems: []string{}}
	r.Problems = append(r.Problems, skills.CheckTriggers(tr)...)
	for _, p := range tr.Paths {
		n := len(skills.Triggers{Paths: []string{p}}.MatchPaths(files))
		r.Paths[p] = n
		if n == 0 && files != nil {
			r.Dead = append(r.Dead, p)
			r.Problems = append(r.Problems, fmt.Sprintf("đường dẫn kích hoạt %q không khớp tệp nào trong repository", p))
		}
	}
	if h != nil && len(tr.Paths) > 0 {
		sup := map[string]bool{}
		for _, id := range supporting {
			sup[id] = true
		}
		for _, run := range h.Runs {
			if !run.Labeled() {
				continue
			}
			hit := len(tr.MatchPaths(run.Touched())) > 0
			switch {
			case sup[run.ID]:
				r.Positives++
				if hit {
					r.PositiveHits++
				}
			default:
				r.Negatives++
				if hit {
					r.FalseHits++
				}
			}
		}
		if r.Positives > 0 {
			if rec := float64(r.PositiveHits) / float64(r.Positives); rec < minTriggerRecall {
				r.Problems = append(r.Problems, fmt.Sprintf("kích hoạt được ở %d/%d lượt thành công làm căn cứ (%.0f%% < %.0f%%): điều kiện quá hẹp hoặc sai khu vực",
					r.PositiveHits, r.Positives, 100*rec, 100*minTriggerRecall))
			}
		} else {
			r.Notes = append(r.Notes, "không có lượt chạy làm căn cứ: không đo được tỷ lệ kích hoạt đúng")
		}
		if r.Negatives >= minNegativesToJudge {
			if fr := float64(r.FalseHits) / float64(r.Negatives); fr > maxFalseTriggerRate {
				r.Problems = append(r.Problems, fmt.Sprintf("kích hoạt cả ở %d/%d lượt có nhãn không liên quan (%.0f%% > %.0f%%): điều kiện quá rộng",
					r.FalseHits, r.Negatives, 100*fr, 100*maxFalseTriggerRate))
			}
		} else {
			r.Notes = append(r.Notes, fmt.Sprintf("chỉ %d lượt có nhãn không liên quan (cần ≥ %d): không đo được tỷ lệ kích hoạt nhầm", r.Negatives, minNegativesToJudge))
		}
	} else if len(tr.Paths) == 0 {
		r.Notes = append(r.Notes, "chỉ có từ khoá kích hoạt: không đối chiếu được với lịch sử lượt chạy (không có đề bài của lượt)")
	}
	if h != nil && len(tr.Paths) > 0 {
		for _, run := range h.Runs {
			if run.Labeled() && slices.Contains(skillEventHarnesses, run.Harness) && len(tr.MatchPaths(run.Touched())) > 0 {
				r.Eligible++
				if slices.Contains(run.Skills, name) {
					r.Activated++
				}
			}
		}
		switch {
		case !h.SkillData:
			r.Notes = append(r.Notes, "chưa có sự kiện skill.activated nào (chỉ Claude Code phát qua hook): không đo được kích hoạt thực tế")
		case r.Eligible >= minNegativesToJudge && float64(r.Activated) < skillIgnoredRate*float64(r.Eligible):
			r.Problems = append(r.Problems, fmt.Sprintf("chỉ được kích hoạt ở %d/%d lượt đủ điều kiện (< %.0f%%): mô tả có thể chưa khớp cách agent tìm skill",
				r.Activated, r.Eligible, 100*skillIgnoredRate))
		}
	}
	for _, o := range others {
		if !o.Active() || o.Meta.Name == name {
			continue
		}
		sim := jaccard(tokens(desc), tokens(o.Doc().Description))
		switch {
		case sim >= overlapThreshold:
			r.Overlaps = append(r.Overlaps, fmt.Sprintf("%s (mô tả tương đồng %.0f%%)", o.Meta.Name, 100*sim))
			r.Problems = append(r.Problems, fmt.Sprintf("trùng chức năng với skill %s (mô tả tương đồng %.0f%%): harness dễ chọn nhầm; hợp nhất", o.Meta.Name, 100*sim))
		case sim >= 0.35 && triggersOverlap(tr, o.Meta.Triggers, files):
			r.Overlaps = append(r.Overlaps, fmt.Sprintf("%s (mô tả tương đồng %.0f%%, chồng đường dẫn)", o.Meta.Name, 100*sim))
		}
	}
	sort.Strings(r.Overlaps)
	r.OK = len(r.Problems) == 0
	return r
}

// DraftsOf trả về bản nháp skill (có nội dung) của đề xuất.
func DraftsOf(p *Proposal) []*SkillDraft {
	var out []*SkillDraft
	for _, c := range p.SkillChanges {
		if c.Draft != nil {
			out = append(out, c.Draft)
		}
	}
	return out
}

// SelectSkillCases chọn tác vụ chịu ảnh hưởng của đề xuất skill (giống SelectCases của tri
// thức): bỏ tác vụ có lời giải đến từ lượt làm căn cứ (rò rỉ), tác vụ mà nhánh không khả
// dụng, và tác vụ không thoả điều kiện kích hoạt của skill bị thay đổi.
func SelectSkillCases(ctx context.Context, env eval.Env, mode string, p *Proposal, h *History, cases []*eval.Case, before, after SkillSet) ([]CaseImpact, error) {
	var out []CaseImpact
	for _, c := range cases {
		ci := CaseImpact{CaseID: c.ID, Split: c.Split, Case: c}
		if h != nil {
			if why := DerivedFrom(p, h, c); why != "" {
				ci.Reason = why
				out = append(out, ci)
				continue
			}
		}
		leak, err := eval.BuildLeakInput(ctx, env.RepoDir, c)
		if err != nil {
			return nil, err
		}
		if _, _, err := eval.CaseKnowledge(ctx, env, mode, c, &leak); err != nil {
			if errors.Is(err, eval.ErrArmUnavailable) {
				ci.Reason = err.Error()
				out = append(out, ci)
				continue
			}
			return nil, err
		}
		ci.Affected, ci.Reason, err = skillImpact(ctx, env, p, c, &leak, before, after)
		if err != nil {
			return nil, err
		}
		out = append(out, ci)
	}
	return out, nil
}
