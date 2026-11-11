package evolve

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"bint/internal/eval"
	"bint/internal/hcomp"
	"bint/internal/pathglob"
)

// HarnessSets trả về tập thành phần quy trình của hai nhánh đối chiếu một đề xuất quy trình:
// before là tập đang có hiệu lực (verified + dựng sẵn), after là tập đó sau khi áp đề xuất
// (thành phần tạo mới được coi như đã duyệt).
func HarnessSets(lib *hcomp.Library, p *Proposal) (before, after hcomp.Set, err error) {
	before = lib.Select(hcomp.ModeOn)
	comps, err := ApplyHarnessChanges(p, before.Components, true)
	if err != nil {
		return before, after, err
	}
	after = hcomp.Set{Components: comps}
	return before, after, nil
}

// HarnessVariant dựng cặp nhánh base/candidate cho đề xuất quy trình: cùng tập tri thức, khác ở
// tập thành phần quy trình (before cho base, after cho candidate). Quy trình chỉ đi qua nhánh bint
// (hook, MCP và guardrail), nên Delivery luôn là bint. Chứa mã lời giải thì nhánh không khả dụng
// cho tác vụ đó (kiểm trong eval.prepareArm).
func HarnessVariant(p *Proposal, before, after hcomp.Set) *eval.Variant {
	return &eval.Variant{
		Delivery: eval.ArmBint,
		Label:    p.ID,
		Harness: func(_ context.Context, arm string, _ *eval.Case, _ *eval.LeakInput) (hcomp.Set, error) {
			if arm == eval.ArmCandidate {
				return after, nil
			}
			return before, nil
		},
	}
}

// componentAffects cho biết thành phần có tác động tới tác vụ không: phạm vi domain và điều kiện
// glob của guardrail phải khớp tệp của tác vụ; còn lại (chỉ dẫn chung, chiến lược, script) áp dụng
// cho mọi tác vụ.
func componentAffects(c hcomp.Component, files []string) bool {
	match := func(glob string) bool {
		p := pathglob.Compile(glob)
		for _, f := range files {
			if p.Match(f) {
				return true
			}
		}
		return false
	}
	if g := c.Domain(); g != "" && !match(g) {
		return false
	}
	if c.Kind == hcomp.KindGuardrail {
		for _, g := range []string{c.When.Edited, c.When.Paths} {
			if g != "" && !match(g) {
				return false
			}
		}
	}
	return true
}

// SelectHarnessCases chọn tác vụ chịu ảnh hưởng của đề xuất quy trình (như SelectCases): bỏ tác vụ
// nguồn của đề xuất (không chấm lại trên chính nó), tác vụ có lời giải đến từ lượt làm căn cứ, tác
// vụ mà nhánh không khả dụng, và tác vụ mà thành phần thay đổi không áp dụng tới (H4: bản chung
// đo trên mẫu toàn validation, bản riêng chỉ trên tác vụ khớp vùng của nó).
func SelectHarnessCases(ctx context.Context, env eval.Env, mode string, p *Proposal, h *History, cases []*eval.Case, before, after hcomp.Set) ([]CaseImpact, error) {
	var out []CaseImpact
	byRef := func(set hcomp.Set) map[string]hcomp.Component {
		m := map[string]hcomp.Component{}
		for _, c := range set.Components {
			m[harnessRef(c.Kind, c.ID)] = c
		}
		return m
	}
	b, a := byRef(before), byRef(after)
	for _, c := range cases {
		ci := CaseImpact{CaseID: c.ID, Split: c.Split, Case: c}
		if slices.Contains(p.EvalCases, c.ID) {
			ci.Reason = "tác vụ là nguồn của đề xuất: không chấm lại trên chính nó"
			out = append(out, ci)
			continue
		}
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
		if env.Variant != nil && env.Variant.Harness != nil {
			if _, err := env.Variant.Harness(ctx, eval.ArmCandidate, c, &leak); err != nil {
				if errors.Is(err, eval.ErrArmUnavailable) {
					ci.Reason = err.Error()
					out = append(out, ci)
					continue
				}
				return nil, err
			}
		}
		files := append(append([]string{}, c.SourceFiles...), c.TestFiles...)
		var hit []string
		for _, ch := range p.HarnessChanges {
			ref := harnessRef(ch.Kind, ch.ID)
			for _, comp := range []map[string]hcomp.Component{b, a} {
				if x, ok := comp[ref]; ok && componentAffects(x, files) && !slices.Contains(hit, ref) {
					hit = append(hit, ref)
				}
			}
		}
		if len(hit) == 0 {
			ci.Reason = "thành phần thay đổi không áp dụng tới tệp của tác vụ (phạm vi hoặc điều kiện không khớp)"
		} else {
			ci.Affected, ci.Reason = true, fmt.Sprintf("thành phần %s áp dụng tới tác vụ", strings.Join(hit, ", "))
		}
		out = append(out, ci)
	}
	return out, nil
}
