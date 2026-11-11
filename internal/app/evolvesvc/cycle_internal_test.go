package evolvesvc

import (
	"errors"
	"strings"
	"testing"

	"bint/internal/eval"
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

// processRunner giữ lỗi của đợt (hết hạn mức, hết ngân sách...) cạnh kết quả dở dang để probe
// quyết định; probe dùng verdict và bỏ lỗi đó như `evolve evaluate --json` cũ (xem
// TestCycleUsesVerdictOfPartialBatch).
func TestCycleEvaluateResultKeepsRunErr(t *testing.T) {
	partial := &EvaluateResult{RunErr: eval.ErrQuota}
	res, err := cycleEvaluateResult(partial, nil)
	if res != partial || !errors.Is(err, eval.ErrQuota) {
		t.Fatalf("res=%v err=%v", res, err)
	}
	if res, err := cycleEvaluateResult(&EvaluateResult{}, nil); res == nil || err != nil {
		t.Fatalf("đợt sạch: res=%v err=%v", res, err)
	}
	boom := errors.New("boom")
	if res, err := cycleEvaluateResult(nil, boom); res != nil || err != boom {
		t.Fatalf("không chạy được: res=%v err=%v", res, err)
	}
}
