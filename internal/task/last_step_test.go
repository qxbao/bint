package task

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// State.LastStep tương thích ngược: phiếu cũ thiếu last_step vẫn nạp được, và vòng ghi/đọc giữ nguyên các trường.
func TestLastStepBackwardCompatAndRoundTrip(t *testing.T) {
	s := NewStore(t.TempDir())
	tk, err := s.Create(Ticket{Goal: "g"}, State{Status: StatusPlanned, Workflow: "b-do"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// Phiếu đã lưu không có last_step (omitempty): đúng định dạng phiếu cũ.
	raw, err := os.ReadFile(filepath.Join(s.Dir, tk.State.ID, StateFile))
	if err != nil || strings.Contains(string(raw), "last_step") {
		t.Fatalf("state không được có last_step khi nil: %v\n%s", err, raw)
	}
	got, err := s.Load(tk.State.ID)
	if err != nil || got.State.LastStep != nil {
		t.Fatalf("nạp phiếu cũ: %v %+v", err, got)
	}
	want := &StepStop{Step: "b-feat", RunID: "r1", Harness: "codex", StopClass: "timeout", SessionID: "thread-1",
		Fingerprint: "tree", Continuable: true, At: "2026-11-16T00:00:00Z"}
	if _, err := s.Update(tk.State.ID, time.Now(), func(x *Task) error { x.State.LastStep = want; return nil }); err != nil {
		t.Fatal(err)
	}
	got, err = s.Load(tk.State.ID)
	if err != nil || !reflect.DeepEqual(got.State.LastStep, want) {
		t.Fatalf("vòng ghi/đọc: %v %+v", err, got.State.LastStep)
	}
	var m map[string]json.RawMessage
	raw, _ = os.ReadFile(filepath.Join(s.Dir, tk.State.ID, StateFile))
	if json.Unmarshal(raw, &m) != nil || m["last_step"] == nil {
		t.Fatalf("state.json phải có last_step:\n%s", raw)
	}
}

func TestAnsweredQuestions(t *testing.T) {
	req := "# Yêu cầu\n## Tiêu chí chấp nhận\n- [ ] AC-1: x\n## Câu hỏi mở\n" +
		"- [x] Q-1: dùng bảng nào? — Trả lời: bảng users\n" +
		"- [ ] Q-2: chưa trả lời — Trả lời: nháp\n" +
		"- [x] Q-3: đánh dấu nhưng rỗng — Trả lời:\n" +
		"- [X] Q-4: viết hoa X - Trả lời: được\n" +
		"```\n- [x] Q-9: trong code fence — Trả lời: bỏ\n```\n" +
		"## Ngoài phạm vi\n- [x] Q-8: sai mục — Trả lời: bỏ\n"
	got := AnsweredQuestions(req)
	want := []string{"- [x] Q-1: dùng bảng nào? — Trả lời: bảng users", "- [X] Q-4: viết hoa X - Trả lời: được"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %q, want %q", got, want)
	}
	if AnsweredQuestions("# Yêu cầu\n") != nil {
		t.Fatal("không có câu hỏi thì nil")
	}
}

// Trường nối tiếp mới round-trip qua JSON, và JSON cũ không có chúng vẫn nạp được.
func TestContinuationFieldsRoundTrip(t *testing.T) {
	in := State{LastStep: &StepStop{Step: "b-feat", Via: "subagent", Continuations: 2, OriginRunID: "r0",
		Usage: &UsageMark{Transcript: "t.jsonl", Turns: 3, Models: []UsageModel{{Model: "m", Input: 1, Output: 2}}}},
		PendingStep: &PendingStep{RunID: "r1", ContinueAgentID: "A1", FullPrompt: "full", Continuations: 2, OriginRunID: "r0",
			UsageBase: &UsageMark{Transcript: "t.jsonl", Turns: 3}}}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out State
	if err := json.Unmarshal(b, &out); err != nil || !reflect.DeepEqual(in, out) {
		t.Fatalf("round-trip: %v\n%+v\n%+v", err, in, out)
	}
	var old State
	if err := json.Unmarshal([]byte(`{"last_step":{"step":"b-feat","run_id":"r","stop_class":"external","continuable":true},"pending_step":{"run_id":"r","step":"b-feat","mode":"exclusive","prompt":"p","started_at":"x","lease_until":"y"}}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.LastStep.Via != "" || old.LastStep.Usage != nil || old.PendingStep.ContinueAgentID != "" {
		t.Fatalf("%+v", old)
	}
}
