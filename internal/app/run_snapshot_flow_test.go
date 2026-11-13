package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"bint/internal/eval"
	"bint/internal/task"
	"bint/internal/workflow"
)

// planningAgent là agent giả của bước b-req: ghi bộ artifact hợp lệ cho tiêu chí "A đúng" của taskRepo; các bước khác không làm gì.
type planningAgent struct {
	ws *TaskWS
	id string
}

func (a planningAgent) Name() string                   { return "fake" }
func (a planningAgent) Version(context.Context) string { return "" }
func (a planningAgent) ReportsCost() bool              { return false }
func (a planningAgent) Run(_ context.Context, req eval.AgentRequest) eval.AgentResult {
	res := eval.AgentResult{Duration: time.Millisecond, Model: "fake-model"}
	if !strings.Contains(req.Prompt, `step="`+workflow.StepReq+`"`) {
		return res
	}
	docs := map[string]string{
		"requirement": "# Yêu cầu\n## Mục tiêu và phạm vi\nĐổi a.go\n## Tiêu chí chấp nhận\n- [ ] AC-1: A đúng\n## Ngoài phạm vi\nKhông có\n## Câu hỏi mở\n",
		"plan":        "# Kế hoạch\nSửa a.go\n",
		"tasks":       "- [ ] TSK-1 [AC-1]: sửa a.go — xong khi: A đúng\n",
	}
	for n, body := range docs {
		if _, _, err := a.ws.WriteArtifact(a.id, n, body); err != nil {
			res.ExitCode, res.Error = 1, err.Error()
			return res
		}
	}
	return res
}

// Hồi quy tri thức k:01M772E6Z8WN8SPECM8X8WQTPC (GĐ3 của docs/custom-flow-plan.md): b-req --task rồi b-do --task với
// roles.implementer=codex và model cấu hình khác mặc định CLI. Lượt b-req không được lưu role chưa chạy như lựa chọn
// có Harness rỗng (nó chặn model cấu hình ở lượt b-do sau); lượt b-do phải chụp implementer = codex/model cấu hình.
// legacy: phiếu cũ đã có {} cho implementer (bản ghi trước khi sửa).
func TestRunWorkflowReqThenDoSnapshotsConfiguredImplementerModel(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "fresh"
		if legacy {
			name = "legacy-empty-implementer"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			ws, _, id := taskRepo(t)
			cfg := &ws.K.Config
			cfg.Harnesses.Enabled = append(cfg.Harnesses.Enabled, "codex")
			cfg.Roles.Implementer = "codex"
			cfg.Harnesses.Codex.Models.Implementer = "gpt-config"
			var steps []string
			cb := RunCallbacks{Agent: func(step string, _ *float64, _ *int) (eval.Agent, error) {
				steps = append(steps, step)
				return planningAgent{ws, id}, nil
			}}

			res, err := RunWorkflow(ctx, ws, RunWorkflowOptions{Workflow: workflow.StepReq, Task: id, RunCallbacks: cb})
			if err != nil || res.Status != task.StatusPlanned {
				t.Fatalf("b-req: %+v %v", res, err)
			}
			tk, err := ws.Tasks.Load(id)
			if err != nil {
				t.Fatal(err)
			}
			if c := tk.State.AgentChoices["planner"]; c.Harness != "claude" {
				t.Fatalf("planner đã chạy phải được chụp: %+v", tk.State.AgentChoices)
			}
			for _, role := range []string{"implementer", "reviewer"} {
				if c, ok := tk.State.AgentChoices[role]; ok && c.Harness == "" {
					t.Fatalf("role %s chưa chạy bị lưu như lựa chọn rỗng: %+v", role, tk.State.AgentChoices)
				}
			}
			if legacy {
				if _, err := ws.Tasks.Update(id, ws.Now(), func(tt *task.Task) error {
					tt.State.AgentChoices["implementer"] = task.AgentChoice{}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}

			steps = nil
			if _, err = RunWorkflow(ctx, ws, RunWorkflowOptions{Workflow: workflow.StepDo, Task: id, NoReview: true, RunCallbacks: cb}); err != nil {
				t.Fatal(err)
			}
			if len(steps) == 0 || steps[0] != workflow.StepFeat {
				t.Fatalf("b-do sau b-req phải vào thẳng b-feat: %v", steps)
			}
			tk, err = ws.Tasks.Load(id)
			if err != nil {
				t.Fatal(err)
			}
			if c := tk.State.AgentChoices["implementer"]; c.Harness != "codex" || c.Model != "gpt-config" {
				t.Fatalf("implementer phải là codex/gpt-config, được %+v (mọi role: %+v)", c, tk.State.AgentChoices)
			}
			if c := tk.State.AgentChoices["planner"]; c.Harness != "claude" {
				t.Fatalf("planner đã chụp phải giữ nguyên: %+v", c)
			}
		})
	}
}
