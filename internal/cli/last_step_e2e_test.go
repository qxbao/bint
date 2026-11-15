package cli_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"bint/internal/app"
	"bint/internal/task"
)

// Giai đoạn 1 của docs/step-continuation-plan.md: run status và task show hiển thị bước dừng gần nhất.
func TestLastStepShownByRunStatusAndTaskShow(t *testing.T) {
	root := newLintRepo(t)
	id := newTaskID(t, root, "--no-plan", "Đổi app.txt")

	for _, text := range []string{"Bước dừng:"} {
		if out, _ := run(t, root, "", "task", "show", id); strings.Contains(out, text) {
			t.Fatalf("phiếu chưa có bước dừng không được in dòng này:\n%s", out)
		}
	}

	ws, err := app.OpenWorkspace(context.Background(), root, true)
	if err != nil {
		t.Fatal(err)
	}
	store := task.NewStore(ws.Paths.Runtime)
	if _, err := store.Update(id, time.Now(), func(tk *task.Task) error {
		tk.State.LastStep = &task.StepStop{Step: "b-feat", RunID: "01RUNCHILD", Harness: "claude", StopClass: "external",
			SessionID: "sess-1", Fingerprint: "abc", Continuable: true}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{{"task", "show", id}, {"run", "status", id}} {
		out, code := run(t, root, "", args...)
		if code != 0 || !strings.Contains(out, "Bước dừng:") || !strings.Contains(out, "b-feat") ||
			!strings.Contains(out, "lớp dừng external") || !strings.Contains(out, "nối tiếp được: có") {
			t.Fatalf("%v: %d\n%s", args, code, out)
		}
	}

	var show struct {
		LastStep *task.StepStop `json:"last_step"`
		State    struct {
			LastStep *task.StepStop `json:"last_step"`
		} `json:"state"`
	}
	out, _ := run(t, root, "", "task", "show", id, "--json")
	if err := json.Unmarshal([]byte(out), &show); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, ls := range []*task.StepStop{show.LastStep, show.State.LastStep} {
		if ls == nil || ls.StopClass != "external" || !ls.Continuable || ls.SessionID != "sess-1" || ls.Step != "b-feat" {
			t.Fatalf("task show --json: %+v\n%s", ls, out)
		}
	}
	var st struct {
		LastStep *task.StepStop `json:"last_step"`
	}
	out, _ = run(t, root, "", "run", "status", id, "--json")
	if err := json.Unmarshal([]byte(out), &st); err != nil || st.LastStep == nil || st.LastStep.StopClass != "external" || !st.LastStep.Continuable {
		t.Fatalf("run status --json: %v %+v\n%s", err, st.LastStep, out)
	}

	// Không nối tiếp được: văn bản nói "không".
	if _, err := store.Update(id, time.Now(), func(tk *task.Task) error {
		tk.State.LastStep = &task.StepStop{Step: "b-feat", RunID: "01RUNCHILD", StopClass: "intrinsic"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if out, _ := run(t, root, "", "run", "status", id); !strings.Contains(out, "lớp dừng intrinsic, nối tiếp được: không") {
		t.Fatalf("%s", out)
	}
}
