package evolve

import "testing"

// Chỉ đề xuất rủi ro thấp, chỉ tạo mục mới (vào candidate) mới được vòng tiến hoá tự áp dụng.
func TestAutoApplicable(t *testing.T) {
	create := Change{Op: ActionCreate, ItemID: "X"}
	for _, tc := range []struct {
		p    Proposal
		want bool
	}{
		{Proposal{Risk: RiskLow, Changes: []Change{create}}, true},
		{Proposal{Risk: RiskMedium, Changes: []Change{create}}, false},
		{Proposal{Risk: RiskLow, Changes: []Change{create, {Op: ActionDeprecate, ItemID: "Y"}}}, false},
		{Proposal{Risk: RiskLow}, false},
		{Proposal{Risk: RiskLow, SkillChanges: []SkillChange{{Op: ActionCreate, Name: "s"}}}, false},
	} {
		if got, why := AutoApplicable(&tc.p); got != tc.want {
			t.Errorf("%+v → %v (%s)", tc.p, got, why)
		}
	}
}
