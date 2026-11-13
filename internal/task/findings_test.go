package task

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOutcomesLedger(t *testing.T) {
	s := newStore(t)
	tk, _ := s.Create(Ticket{Goal: "g"}, State{}, time.Now())
	// review.json cũ (không có trường kết cục, không có findings.json): mọi phát hiện open.
	sub := Submission{Summary: "s", Findings: []Finding{{Kind: "bug", Severity: "low", Title: "t", Evidence: "e"}}}
	if _, err := tk.AddRound(sub, "agent", "", "t"); err != nil {
		t.Fatal(err)
	}
	o, err := tk.LoadOutcomes()
	if err != nil || len(o.Records) != 0 {
		t.Fatalf("sổ trống: %+v %v", o, err)
	}
	id := tk.State.ID + ".1"
	if got := o.Effective(id); got.Outcome != OutcomeOpen {
		t.Fatalf("mặc định phải open: %+v", got)
	}
	reviewBefore, _ := os.ReadFile(filepath.Join(tk.Dir, ReviewFile))

	if err := tk.AppendOutcomes(OutcomeRecord{Finding: id, Outcome: OutcomeDeferred, Reason: "sau", By: "a", At: "t1"}); err != nil {
		t.Fatal(err)
	}
	if err := tk.AppendOutcomes(OutcomeRecord{Finding: id, Outcome: OutcomeFixed, Commit: "abc", By: "a", At: "t2"}); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(filepath.Join(tk.Dir, ReviewFile)); string(after) != string(reviewBefore) {
		t.Fatal("review.json không được đổi khi đặt kết cục")
	}
	// Vòng mới không làm mất kết cục.
	if _, err := tk.AddRound(sub, "agent", "", "t"); err != nil {
		t.Fatal(err)
	}
	o, _ = tk.LoadOutcomes()
	if len(o.History(id)) != 2 || o.Effective(id).Outcome != OutcomeFixed || o.Effective(tk.State.ID+".2").Outcome != OutcomeOpen {
		t.Fatalf("%+v", o)
	}
	if _, f, _, err := s.LoadFinding(strings.ToLower(id)); err != nil || f.ID != id {
		t.Fatalf("LoadFinding: %v", err)
	}
	if _, _, _, err := s.LoadFinding(tk.State.ID + ".9"); err == nil {
		t.Fatal("ID không có phải lỗi")
	}
	// Sổ hỏng báo lỗi.
	os.WriteFile(filepath.Join(tk.Dir, OutcomeFile), []byte("{"), 0o600)
	if _, err := tk.LoadOutcomes(); err == nil {
		t.Fatal("findings.json hỏng phải lỗi")
	}
	// outcome gửi trong review bị từ chối như khoá lạ (JSON và YAML).
	for _, in := range []string{
		`{"summary":"x","findings":[{"kind":"bug","severity":"low","title":"t","evidence":"e","outcome":"fixed"}]}`,
		"summary: x\nfindings:\n  - kind: bug\n    severity: low\n    title: t\n    evidence: e\n    outcome: fixed\n",
	} {
		if _, errs := ParseSubmission([]byte(in), t.TempDir()); len(errs) == 0 {
			t.Fatalf("outcome phải bị từ chối: %s", in)
		}
	}
}
