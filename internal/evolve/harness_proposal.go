package evolve

// Đề xuất tiến hoá thành phần quy trình (H3, L2): chỉ dẫn dùng công cụ, guardrail, script và
// chiến lược ngữ cảnh trong .bint/harness. Cùng bảng đề xuất, phiên bản gốc (băm tệp), kế
// hoạch đánh giá chốt trước, kiểm toán áp dụng/quay lui như skill; khác ở đối tượng. Mức rủi
// ro do Bint tính theo nội dung (hcomp.Risk), không do agent tự khai; đề xuất không đặt được
// trạng thái, nguồn gốc hay phiên bản — các trường đó do Bint quyết định khi áp dụng.

import (
	"fmt"
	"slices"
	"strings"

	"bint/internal/app"
	"bint/internal/hcomp"
	"bint/internal/knowledge"
	"bint/internal/pathglob"
)

// TargetHarness là loại đối tượng của đề xuất thành phần quy trình.
const TargetHarness = "harness"

// IsHarness cho biết đề xuất sửa thành phần quy trình.
func (p *Proposal) IsHarness() bool { return p.TargetType == TargetHarness }

// HarnessDraft là nội dung một thành phần do đề xuất tạo hoặc sửa. Chỉ gồm nội dung; trạng
// thái, nguồn gốc, phiên bản và người duyệt do Bint đặt khi áp dụng.
type HarnessDraft struct {
	Kind  hcomp.Kind `json:"kind"`
	ID    string     `json:"id"`
	Scope string     `json:"scope"`
	// Guidance.
	Title    string   `json:"title,omitempty"`
	Requires []string `json:"requires,omitempty"`
	Body     string   `json:"body,omitempty"`
	// Guardrail.
	On      string       `json:"on,omitempty"`
	When    hcomp.When   `json:"when,omitempty"`
	Unless  hcomp.Unless `json:"unless,omitempty"`
	Action  string       `json:"action,omitempty"`
	Message string       `json:"message,omitempty"`
	// Script: Script là nội dung .sh; Bint không bao giờ tự chạy nó.
	Description string `json:"description,omitempty"`
	Usage       string `json:"usage,omitempty"`
	Script      string `json:"script,omitempty"`
	// Strategy.
	Context *hcomp.StrategyContext `json:"context,omitempty"`
}

// Component dựng thành phần từ bản nháp: candidate, origin agent, phiên bản 1 (Bint đặt lại
// khi áp dụng).
func (d *HarnessDraft) Component() hcomp.Component {
	c := hcomp.Component{Kind: d.Kind,
		Meta:  hcomp.Meta{ID: d.ID, Scope: d.Scope, Status: hcomp.StatusCandidate, Origin: hcomp.OriginAgent, Version: 1},
		Title: d.Title, Requires: d.Requires, Body: d.Body,
		On: d.On, When: d.When, Unless: d.Unless, Action: d.Action, Message: d.Message,
		Description: d.Description, Usage: d.Usage, Context: d.Context}
	if d.Script != "" {
		c.Source = []byte(d.Script)
	}
	return c
}

// HarnessChange là một thay đổi thành phần trong đề xuất.
type HarnessChange struct {
	Op     string        `json:"op"` // create | update | deprecate
	Kind   hcomp.Kind    `json:"kind"`
	ID     string        `json:"id"`
	Draft  *HarnessDraft `json:"draft,omitempty"`
	Reason string        `json:"reason,omitempty"`
}

// HarnessChangeNames liệt kê "thao tác:loại/id" của đề xuất quy trình.
func HarnessChangeNames(p *Proposal) []string {
	var out []string
	for _, c := range p.HarnessChanges {
		out = append(out, fmt.Sprintf("%s:%s/%s", c.Op, c.Kind, c.ID))
	}
	return out
}

func harnessRef(kind hcomp.Kind, id string) string { return string(kind) + "/" + id }

// HarnessBaseOf ghi phiên bản gốc của các thành phần mà đề xuất sửa (băm tệp).
func HarnessBaseOf(dir string, comps []hcomp.Component) ([]BaseVersion, error) {
	var out []BaseVersion
	for _, c := range comps {
		sha, err := hcomp.FileSHA(dir, c)
		if err != nil {
			return nil, err
		}
		out = append(out, BaseVersion{ID: harnessRef(c.Kind, c.ID), Version: c.Version, Status: c.Status, SHA: sha})
	}
	return out, nil
}

// checkHarnessBase so phiên bản gốc với tệp hiện tại (như CheckBase của tri thức).
func (p *Proposal) checkHarnessBase(k *app.KnowledgeWS) ([]BaseProblem, error) {
	dir := hcomp.Dir(k.Root)
	var out []BaseProblem
	for _, b := range p.Base {
		kind, id, _ := strings.Cut(b.ID, "/")
		sha, err := hcomp.FileSHA(dir, hcomp.Component{Kind: hcomp.Kind(kind), Meta: hcomp.Meta{ID: id}})
		if err != nil {
			return nil, err
		}
		switch {
		case sha == "" && b.SHA != "":
			out = append(out, BaseProblem{b.ID, "thành phần đã bị xoá"})
		case sha != b.SHA:
			out = append(out, BaseProblem{b.ID, fmt.Sprintf("thành phần đã thay đổi kể từ khi đề xuất (phiên bản gốc %d)", b.Version)})
		}
	}
	for _, c := range p.HarnessChanges {
		if c.Op != ActionCreate {
			continue
		}
		sha, err := hcomp.FileSHA(dir, hcomp.Component{Kind: c.Kind, Meta: hcomp.Meta{ID: c.ID}})
		if err != nil {
			return nil, err
		}
		if sha != "" {
			out = append(out, BaseProblem{harnessRef(c.Kind, c.ID), "thành phần cần tạo đã tồn tại"})
		}
	}
	return out, nil
}

// ApplyHarnessChanges trả về danh sách thành phần sau khi áp các thay đổi của đề xuất lên
// before. Thành phần tạo mới có trạng thái verified (asVerified) — dùng để đối chiếu "như thể
// đã được duyệt" — hoặc candidate (ghi vào repository). update giữ trạng thái và nguồn gốc
// của bản gốc, tăng phiên bản; deprecate gỡ thành phần khỏi tập (đối chiếu loại bỏ).
func ApplyHarnessChanges(p *Proposal, before []hcomp.Component, asVerified bool) ([]hcomp.Component, error) {
	after := slices.Clone(before)
	find := func(k hcomp.Kind, id string) int {
		return slices.IndexFunc(after, func(c hcomp.Component) bool { return c.Kind == k && c.ID == id })
	}
	for _, ch := range p.HarnessChanges {
		i := find(ch.Kind, ch.ID)
		switch ch.Op {
		case ActionDeprecate:
			if i >= 0 {
				after = slices.Delete(after, i, i+1)
			}
		case ActionCreate, ActionUpdate:
			if ch.Draft == nil {
				return nil, fmt.Errorf("%s %s cần nội dung", ch.Op, harnessRef(ch.Kind, ch.ID))
			}
			c := ch.Draft.Component()
			if ch.Op == ActionCreate && asVerified {
				c.Status = hcomp.StatusVerified
			}
			if ch.Op == ActionUpdate && i >= 0 {
				c.Status, c.Origin, c.Version, c.ApprovedBy = after[i].Status, after[i].Origin, after[i].Version+1, after[i].ApprovedBy
			}
			if i >= 0 {
				after[i] = c
			} else {
				after = append(after, c)
			}
		default:
			return nil, fmt.Errorf("thao tác %q không hợp lệ", ch.Op)
		}
	}
	return after, nil
}

// scopeHasFiles cho biết glob (hoặc đường dẫn) khớp ít nhất một tệp thật trong cây mã. src nil
// = không kiểm (chỉ dùng trong kiểm thử).
func scopeHasFiles(glob string, src knowledge.Source) bool {
	if src == nil || glob == "" {
		return true
	}
	if !strings.ContainsAny(glob, "*?[{") {
		return len(pruneScopePaths([]string{glob}, src)) == 1 && knowledge.HasFileOrDir(src, strings.TrimSuffix(glob, "/"))
	}
	p := pathglob.Compile(glob)
	for _, f := range src.Files() {
		if p.Match(f) {
			return true
		}
	}
	return false
}

// validateHarnessChanges kiểm tra ranh giới và nội dung của đề xuất quy trình (không I/O ngoài src):
// thao tác hợp lệ, thành phần qua hcomp.Check (parse, mẫu lệnh hợp lệ, phạm vi), phạm vi và điều
// kiện glob khớp tệp có thật, không trùng hay mâu thuẫn với guardrail sẵn có hoặc trong cùng đề xuất.
func validateHarnessChanges(p *Proposal, lib *hcomp.Library, src knowledge.Source) error {
	if p.TargetType != TargetHarness || p.Level != LevelL2 {
		return fmt.Errorf("đề xuất quy trình phải có level L2 và target_type harness")
	}
	if !slices.Contains(Actions, p.Action) {
		return fmt.Errorf("hành động %q không hợp lệ", p.Action)
	}
	if len(p.HarnessChanges) == 0 || len(p.Changes) != 0 || len(p.SkillChanges) != 0 {
		return fmt.Errorf("đề xuất quy trình phải có harness_changes và không có thay đổi tri thức hay skill")
	}
	builtin := map[string]bool{}
	for _, b := range hcomp.Builtins() {
		builtin[harnessRef(b.Kind, b.ID)] = true
	}
	seen := map[string]bool{}
	var newGuards []hcomp.Component
	for _, ch := range p.HarnessChanges {
		ref := harnessRef(ch.Kind, ch.ID)
		if !slices.Contains(hcomp.Kinds, ch.Kind) || !hcomp.ValidID(ch.ID) && ch.Kind != hcomp.KindStrategy {
			return fmt.Errorf("thành phần %s không hợp lệ", ref)
		}
		if seen[ref] {
			return fmt.Errorf("%s xuất hiện hai lần trong đề xuất", ref)
		}
		seen[ref] = true
		cur, exists := lib.Find(ch.Kind, ch.ID)
		switch ch.Op {
		case ActionCreate:
			if exists {
				return fmt.Errorf("create: %s đã tồn tại (dùng update)", ref)
			}
			if builtin[ref] {
				return fmt.Errorf("create: %s trùng bản dựng sẵn", ref)
			}
		case ActionUpdate:
			if !exists || cur.Status == hcomp.StatusDeprecated {
				return fmt.Errorf("update cần thành phần đang có hiệu lực hoặc chờ duyệt (%s)", ref)
			}
		case ActionDeprecate:
			if !exists || cur.Status == hcomp.StatusDeprecated {
				return fmt.Errorf("deprecate cần thành phần chưa bị loại bỏ (%s)", ref)
			}
			if ch.Draft != nil {
				return fmt.Errorf("deprecate không kèm nội dung (%s)", ref)
			}
			continue
		default:
			return fmt.Errorf("thao tác %q không hợp lệ (create | update | deprecate)", ch.Op)
		}
		d := ch.Draft
		if d == nil {
			return fmt.Errorf("%s cần nội dung (%s)", ch.Op, ref)
		}
		if d.Kind != ch.Kind || d.ID != ch.ID {
			return fmt.Errorf("nội dung (%s) khác thành phần của thay đổi (%s)", harnessRef(d.Kind, d.ID), ref)
		}
		c := d.Component()
		if err := hcomp.Check(c); err != nil {
			return err
		}
		if g := c.Domain(); g != "" && !scopeHasFiles(g, src) {
			return fmt.Errorf("%s: phạm vi %s không khớp tệp nào trong cây mã", ref, c.Scope)
		}
		if c.Kind == hcomp.KindGuardrail {
			for _, g := range []string{c.When.Edited, c.When.Paths} {
				if g != "" && !scopeHasFiles(g, src) {
					return fmt.Errorf("%s: điều kiện %q không khớp tệp nào trong cây mã", ref, g)
				}
			}
			var others []hcomp.Component
			for _, e := range lib.Components {
				if e.Kind == hcomp.KindGuardrail && e.ID != c.ID {
					others = append(others, e)
				}
			}
			if cf := hcomp.FindConflict(append(others, newGuards...), c); cf != nil {
				return fmt.Errorf("%s: %s", ref, cf)
			}
			newGuards = append(newGuards, c)
		}
	}
	return nil
}

// assessHarnessRisk tính rủi ro theo bảng H3: guidance và guardrail remind thấp; guardrail ask
// và strategy trung bình; guardrail deny và script cao. Sửa hay loại bỏ thành phần đã duyệt
// (verified) tối thiểu trung bình — nó đang có hiệu lực với agent.
func assessHarnessRisk(p *Proposal, lib *hcomp.Library) (string, []string) {
	risk, why := hcomp.RiskLow, []string{}
	raise := func(r, reason string) {
		risk = hcomp.MaxRisk(risk, r)
		why = append(why, reason)
	}
	for _, ch := range p.HarnessChanges {
		ref := harnessRef(ch.Kind, ch.ID)
		cur, exists := lib.Find(ch.Kind, ch.ID)
		var c hcomp.Component
		switch {
		case ch.Draft != nil:
			c = ch.Draft.Component()
		case exists:
			c = cur
		default:
			continue
		}
		r := hcomp.Risk(c)
		if ch.Op != ActionCreate && exists && cur.Status == hcomp.StatusVerified {
			r = hcomp.MaxRisk(r, hcomp.RiskMedium)
		}
		label := map[hcomp.Kind]string{hcomp.KindGuidance: "chỉ dẫn", hcomp.KindGuardrail: "guardrail " + c.Action,
			hcomp.KindScript: "script (mã thực thi)", hcomp.KindStrategy: "chiến lược ngữ cảnh"}[ch.Kind]
		raise(r, fmt.Sprintf("%s %s %s (%s)", ch.Op, label, ref, r))
	}
	return risk, why
}
