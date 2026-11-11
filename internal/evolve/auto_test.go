package evolve

import (
	"context"
	"strings"
	"testing"

	"bint/internal/store"
)

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

// Vòng tiến hoá chỉ tự bác bỏ đề xuất đang chờ mà lần đối chiếu của chính nó kết luận
// not_supported; inconclusive hay đối chiếu của đề xuất khác thì không.
func TestAutoReject(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	t.Setenv("CLAUDECODE", "1") // không cần người: chạy được cả trong phiên agent
	ts := store.FormatTime(now)
	p := &Proposal{Schema: ProposalSchema, ID: "ep_1", Level: LevelL1, TargetType: TargetKnowledge, Action: ActionCreate, Opportunity: "o",
		Status: StatusProposed, Risk: RiskLow, CreatedAt: ts, UpdatedAt: ts}
	if err := SaveProposal(ctx, f.db, p); err != nil {
		t.Fatal(err)
	}
	inc := &Evaluation{ID: "ee_1", ProposalID: p.ID, Split: "validation", Verdict: VerdictInconclusive}
	other := &Evaluation{ID: "ee_2", ProposalID: "ep_khac", Split: "validation", Verdict: VerdictNotSupported}
	for _, ev := range []*Evaluation{nil, inc, other} {
		if err := AutoReject(ctx, f.db, p, ev, now); err == nil {
			t.Fatalf("%+v: phải từ chối", ev)
		}
	}
	ns := &Evaluation{ID: "ee_3", ProposalID: p.ID, BatchID: "eb_1", Split: "test", Verdict: VerdictNotSupported}
	if err := AutoReject(ctx, f.db, p, ns, now); err != nil {
		t.Fatal(err)
	}
	got, err := GetProposal(ctx, f.db, p.ID)
	if err != nil || got.Status != StatusRejected || !strings.HasPrefix(got.StatusReason, AutoActor+": không đạt trên test") {
		t.Fatalf("%+v %v", got, err)
	}
	if err := AutoReject(ctx, f.db, got, ns, now); err == nil {
		t.Fatal("đề xuất đã bác bỏ không bác bỏ lại được")
	}
}
