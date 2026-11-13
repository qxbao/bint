package evolve

// Đề xuất tiến hoá skill (P7, L2; mục 10.5). Dùng cùng bảng đề xuất, kế hoạch đánh giá
// chốt trước, phiên bản gốc, kiểm toán áp dụng/quay lui như L1; khác ở đối tượng:
// skill chuyên biệt trong .bint/skills/evolved. Skill gốc không bao giờ là đối tượng
// của đề xuất (bất biến, mục 6.6) — tuỳ biến chúng đi qua lớp phủ do người viết.

import (
	"fmt"
	"slices"
	"strings"

	"bint/internal/app"
	"bint/internal/knowledge"
	"bint/internal/skills"
	"bint/internal/ulid"
)

// Mức và loại đối tượng của đề xuất skill.
const (
	LevelL1         = "L1"
	LevelL2         = "L2"
	TargetKnowledge = "knowledge"
	TargetSkill     = "skill"
)

// IsSkill cho biết đề xuất sửa skill (L2).
func (p *Proposal) IsSkill() bool { return p.TargetType == TargetSkill }

// SkillDraft là nội dung một skill do đề xuất tạo hoặc sửa. Chỉ gồm nội dung: trạng
// thái, nguồn gốc, phiên bản và người duyệt do Bint quyết định khi áp dụng.
type SkillDraft struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Body        string          `json:"body"`
	Triggers    skills.Triggers `json:"triggers"`
	Evidence    []string        `json:"evidence"` // path[:a-b][#symbol][@commit]
	Inputs      []string        `json:"inputs,omitempty"`
}

// Content dựng SKILL.md của bản nháp.
func (d *SkillDraft) Content() []byte { return skills.BuildContent(d.Name, d.Description, d.Body) }

// SkillChange là một thay đổi skill trong đề xuất.
type SkillChange struct {
	Op    string      `json:"op"` // create | update | deprecate | delete
	Name  string      `json:"name"`
	Draft *SkillDraft `json:"draft,omitempty"`
	// MergedInto: với deprecate là một nửa của phép hợp nhất — skill nhận phần kiến thức.
	MergedInto string `json:"merged_into,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// SkillBaseOf ghi phiên bản gốc của các skill (băm SKILL.md + skill.yaml).
func SkillBaseOf(root string, targets []*skills.Evolved) ([]BaseVersion, error) {
	var out []BaseVersion
	for _, e := range targets {
		sha, err := skills.EvolvedSHA(root, e.Meta.Name)
		if err != nil {
			return nil, err
		}
		out = append(out, BaseVersion{ID: e.Meta.Name, Version: e.Meta.Version, Status: e.Meta.Status, SHA: sha})
	}
	return out, nil
}

// CheckBaseWS so phiên bản gốc với tệp hiện tại cho cả đề xuất tri thức lẫn skill.
func (p *Proposal) CheckBaseWS(k *app.KnowledgeWS) ([]BaseProblem, error) {
	if p.IsHarness() {
		return p.checkHarnessBase(k)
	}
	if !p.IsSkill() {
		return p.CheckBase(k.Store)
	}
	var out []BaseProblem
	for _, b := range p.Base {
		sha, err := skills.EvolvedSHA(k.Root, b.ID)
		if err != nil {
			return nil, err
		}
		switch {
		case sha == "" && b.SHA != "":
			out = append(out, BaseProblem{b.ID, "skill đã bị xoá"})
		case sha != b.SHA:
			out = append(out, BaseProblem{b.ID, fmt.Sprintf("skill đã thay đổi kể từ khi đề xuất (phiên bản gốc %d)", b.Version)})
		}
	}
	for _, c := range p.SkillChanges {
		if c.Op == ActionCreate {
			if skills.CustomExists(k.Root, c.Name) {
				out = append(out, BaseProblem{c.Name, "tên trùng skill tuỳ biến (.bint/skills/" + c.Name + "/)"})
			} else if sha, err := skills.EvolvedSHA(k.Root, c.Name); err != nil {
				return nil, err
			} else if sha != "" {
				out = append(out, BaseProblem{c.Name, "skill cần tạo đã tồn tại"})
			}
		}
	}
	return out, nil
}

func indexEvolved(list []*skills.Evolved) map[string]*skills.Evolved {
	m := map[string]*skills.Evolved{}
	for _, e := range list {
		m[e.Meta.Name] = e
	}
	return m
}

// validateSkillChanges kiểm tra ranh giới của đề xuất L2: chỉ đụng tới skill tiến hoá,
// chỉ các thao tác cho phép, nội dung qua kiểm tra xác định được.
func validateSkillChanges(p *Proposal, byName map[string]*skills.Evolved, src knowledge.Source, root string) error {
	if p.TargetType != TargetSkill || p.Level != LevelL2 {
		return fmt.Errorf("đề xuất skill phải có level L2 và target_type skill")
	}
	if !slices.Contains(Actions, p.Action) {
		return fmt.Errorf("hành động %q không hợp lệ", p.Action)
	}
	if len(p.SkillChanges) == 0 || len(p.Changes) != 0 {
		return fmt.Errorf("đề xuất skill phải có skill_changes và không có candidate_changes của tri thức")
	}
	after := map[string]*skills.Evolved{}
	for n, e := range byName {
		after[n] = e
	}
	for _, c := range p.SkillChanges {
		if _, isCore := skills.Get(c.Name); isCore {
			return fmt.Errorf("%s là skill gốc: bất biến, chỉ tuỳ biến được qua lớp phủ (mục 6.6)", c.Name)
		}
		cur := byName[c.Name]
		switch c.Op {
		case ActionCreate:
			if cur != nil {
				return fmt.Errorf("create: skill %s đã tồn tại", c.Name)
			}
			if skills.CustomExists(root, c.Name) {
				return fmt.Errorf("create: tên %s trùng skill tuỳ biến (.bint/skills/%s/)", c.Name, c.Name)
			}
			if c.Draft == nil {
				return fmt.Errorf("create cần nội dung (%s)", c.Name)
			}
		case ActionUpdate:
			if cur == nil || !cur.Active() {
				return fmt.Errorf("update cần skill đang hoạt động (%s)", c.Name)
			}
			if c.Draft == nil {
				return fmt.Errorf("update cần nội dung (%s)", c.Name)
			}
		case ActionDeprecate:
			if cur == nil || !cur.Active() {
				return fmt.Errorf("deprecate cần skill đang hoạt động (%s)", c.Name)
			}
		case ActionDelete:
			if cur == nil || cur.Active() {
				return fmt.Errorf("delete chỉ áp dụng cho skill đã deprecated (%s); skill đang hoạt động dùng deprecate để giữ dấu vết", c.Name)
			}
		default:
			return fmt.Errorf("thao tác %q không hợp lệ", c.Op)
		}
		if c.MergedInto != "" && (byName[c.MergedInto] == nil || c.Op != ActionDeprecate) {
			return fmt.Errorf("merged_into %q phải là skill hiện có và đi cùng deprecate", c.MergedInto)
		}
		if c.Draft != nil {
			if c.Draft.Name != c.Name {
				return fmt.Errorf("tên trong nội dung (%s) khác tên thay đổi (%s)", c.Draft.Name, c.Name)
			}
			after[c.Name] = &skills.Evolved{Meta: skills.EvolvedMeta{Name: c.Name, Status: skills.EvActive, Triggers: c.Draft.Triggers}, Content: c.Draft.Content()}
		} else if c.Op == ActionDeprecate || c.Op == ActionDelete {
			delete(after, c.Name)
		}
	}
	// Kiểm tra nội dung sau khi mọi thay đổi (để phép hợp nhất không tự báo trùng với nửa bị bỏ).
	for _, c := range p.SkillChanges {
		if c.Draft == nil {
			continue
		}
		var others []*skills.Evolved
		for n, e := range after {
			if n != c.Name && e.Active() {
				others = append(others, e)
			}
		}
		if probs := CheckSkillDraft(c.Draft, src, others); len(probs) > 0 {
			return fmt.Errorf("skill %s không qua kiểm tra xác định được: %s", c.Name, strings.Join(probs, "; "))
		}
	}
	return nil
}

// overlapThreshold: độ tương đồng mô tả (Jaccard) từ ngưỡng này trở lên coi là trùng
// chức năng — hai skill trùng làm harness chọn nhầm.
const overlapThreshold = 0.6

// CheckSkillDraft kiểm tra xác định được một bản nháp skill: định dạng và ràng buộc
// (skills.CheckDraft), bằng chứng/ký hiệu/ví dụ mã/đường dẫn khớp mã thật (cùng bộ kiểm
// tra của tri thức), điều kiện kích hoạt khớp tệp thật, và không trùng chức năng với
// skill khác. others là các skill đang hoạt động còn lại.
func CheckSkillDraft(d *SkillDraft, src knowledge.Source, others []*skills.Evolved) []string {
	probs := skills.CheckDraft(d.Content(), d.Triggers)
	if len(d.Evidence) == 0 {
		probs = append(probs, "thiếu bằng chứng: skill phải trỏ tới mã thật làm căn cứ")
	}
	it := &knowledge.Item{Schema: knowledge.SchemaVersion, ID: ulid.New(), Type: knowledge.TypePattern, Status: knowledge.StatusCandidate,
		Title: d.Name, Body: d.Body, Scope: knowledge.Scope{Paths: d.Triggers.Paths},
		Origin: knowledge.Origin{Kind: knowledge.OriginAgent, Inputs: d.Inputs}, Version: 1}
	for _, spec := range d.Evidence {
		e, err := app.ParseEvidenceSpec(spec)
		if err != nil {
			probs = append(probs, fmt.Sprintf("bằng chứng %q: %v", spec, err))
			continue
		}
		it.Evidence = append(it.Evidence, e)
	}
	if src != nil {
		for i := range it.Evidence {
			if b, ok := src.Read(it.Evidence[i].Path); ok {
				it.Evidence[i] = knowledge.NarrowEvidence(it.Evidence[i], b)
				if fp, err := knowledge.FingerprintWith(it.Evidence[i], b, "symbol"); err == nil {
					it.Evidence[i].Fingerprint = fp
				}
			}
		}
		for _, f := range (knowledge.Verifier{Src: src}).Verify(it).Findings {
			if f.Level == knowledge.LevelError && f.Code != knowledge.CodeSchema {
				probs = append(probs, f.Code+": "+f.Detail)
			}
		}
	}
	for _, o := range others {
		if sim := jaccard(tokens(d.Description), tokens(o.Doc().Description)); sim >= overlapThreshold {
			probs = append(probs, fmt.Sprintf("mô tả trùng chức năng với skill %s (tương đồng %.0f%%): đề xuất hợp nhất thay vì tạo thêm", o.Meta.Name, 100*sim))
		}
	}
	return probs
}

// assessSkillRisk tính mức rủi ro từ nội dung thay đổi (không tin mức agent tự khai).
func assessSkillRisk(p *Proposal, byName map[string]*skills.Evolved) (string, []string) {
	risk, why := RiskLow, []string{}
	order := map[string]int{RiskLow: 0, RiskMedium: 1, RiskHigh: 2}
	raise := func(r, reason string) {
		if order[r] > order[risk] {
			risk = r
		}
		why = append(why, reason)
	}
	for _, c := range p.SkillChanges {
		if c.Draft != nil {
			// Skill là chỉ dẫn hành vi nằm trong ngữ cảnh của agent: tạo/sửa luôn tối thiểu mức trung bình.
			raise(RiskMedium, fmt.Sprintf("%s skill %s: chỉ dẫn hành vi cho agent, soạn từ dữ liệu không đáng tin", c.Op, c.Name))
			for _, r := range skills.CommandRisks(c.Draft.Body) {
				raise(RiskHigh, fmt.Sprintf("skill %s %s", c.Name, r))
			}
		}
		if cur := byName[c.Name]; cur != nil && (c.Op == ActionDeprecate || c.Op == ActionDelete) && c.MergedInto == "" {
			raise(RiskLow, fmt.Sprintf("%s skill %s", c.Op, c.Name))
		}
	}
	return risk, why
}

// SkillChangeNames liệt kê "thao tác:tên" của đề xuất skill.
func SkillChangeNames(p *Proposal) []string {
	var out []string
	for _, c := range p.SkillChanges {
		out = append(out, c.Op+":"+c.Name)
	}
	return out
}
