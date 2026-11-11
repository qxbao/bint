package cli

import (
	"strings"
	"testing"

	"bint/internal/evolve"
)

func TestLatestProbe(t *testing.T) {
	evs := []evolve.Evaluation{
		{ID: "a", Split: "validation", Verdict: "supported", CreatedAt: "2026-01-01T00:00:00Z"},
		{ID: "b", Split: "validation", Verdict: "not_supported", CreatedAt: "2026-01-03T00:00:00Z"},
		{ID: "c", Split: "validation", Verdict: "supported", CreatedAt: "2026-01-04T00:00:00Z", Kind: evolve.EvalRegression},
		{ID: "d", Split: "test", Verdict: "supported", CreatedAt: "2026-01-05T00:00:00Z"},
	}
	if e := latestProbe(evs, "validation", ""); e == nil || e.ID != "b" {
		t.Fatalf("%+v", e)
	}
	// Kết quả trước lần áp dụng gần nhất không còn được tính.
	if e := latestProbe(evs, "validation", "2026-01-03T12:00:00Z"); e != nil {
		t.Fatalf("%+v", e)
	}
	if e := latestProbe(evs, "test", "2026-01-03T12:00:00Z"); e == nil || e.ID != "d" {
		t.Fatalf("%+v", e)
	}
}

func TestLastJSON(t *testing.T) {
	if got := string(lastJSON([]byte("tiến độ...\n[1/2] x\n{\"a\":1}\n"))); got != `{"a":1}` {
		t.Fatalf("%q", got)
	}
	if got := string(lastJSON([]byte("{\"a\":1}"))); got != `{"a":1}` {
		t.Fatalf("%q", got)
	}
}

// Thay đổi workflow (skill) đã đạt không bao giờ được vòng tự áp dụng; tri thức rủi ro thấp
// chỉ tạo mục mới thì được, tối đa một đề xuất mỗi vòng.
func TestComposeDecision(t *testing.T) {
	know := &evolve.Proposal{Risk: evolve.RiskLow, Changes: []evolve.Change{{Op: evolve.ActionCreate, ItemID: "X"}}}
	skill := &evolve.Proposal{TargetType: evolve.TargetSkill, Risk: evolve.RiskLow,
		SkillChanges: []evolve.SkillChange{{Op: evolve.ActionCreate, Name: "s"}}}
	if ok, _ := composeDecision(know, true, false); !ok {
		t.Fatal("tri thức rủi ro thấp phải được tự áp dụng")
	}
	if ok, why := composeDecision(know, true, true); ok || !strings.Contains(why, "vòng sau") {
		t.Fatal(why)
	}
	if ok, why := composeDecision(know, false, false); ok || !strings.Contains(why, "auto_low_risk") {
		t.Fatal(why)
	}
	if ok, why := composeDecision(skill, true, false); ok || !strings.Contains(why, "workflow") {
		t.Fatal(why)
	}
}

func TestFailedProbe(t *testing.T) {
	sup := &evolve.Evaluation{ID: "s", Verdict: evolve.VerdictSupported}
	inc := &evolve.Evaluation{ID: "i", Verdict: evolve.VerdictInconclusive}
	ns := &evolve.Evaluation{ID: "n", Verdict: evolve.VerdictNotSupported}
	if failedProbe(inc, nil) != nil || failedProbe(sup, nil) != nil || failedProbe(nil, nil) != nil {
		t.Fatal("inconclusive/supported không phải thua")
	}
	if e := failedProbe(sup, ns); e != ns {
		t.Fatalf("%+v", e)
	}
}
