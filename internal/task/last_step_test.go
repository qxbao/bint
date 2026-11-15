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
