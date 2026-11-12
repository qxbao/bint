package app

import (
	"bint/internal/task"
	"bint/internal/workflow"
	"context"
	"testing"
)

func TestSessionCreationSnapshotsExplicitChoices(t *testing.T) {
	ws, _, _ := taskRepo(t)
	ws.K.Config.Harnesses.Enabled = append(ws.K.Config.Harnesses.Enabled, "codex")
	res, err := RunWorkflow(context.Background(), ws, RunWorkflowOptions{Workflow: workflow.StepDo, Request: "Update a.go", Driver: "session", NoPlan: true, Harness: "codex", Model: "saved-model", VerifyCommand: "git --version", VerifyCommandSet: true})
	if err != nil {
		t.Fatal(err)
	}
	tk, err := ws.Tasks.Load(res.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if got := tk.State.AgentChoices["implementer"]; got.Harness != "codex" || got.Model != "saved-model" {
		t.Fatalf("choice: %+v", got)
	}
	a, err := RunNext(context.Background(), ws, RunNextOptions{Ref: res.TaskID})
	if err != nil || a.Action != workflow.ActionProcess {
		t.Fatalf("next: %+v %v", a, err)
	}
}

func TestSessionNextUsesSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name, harness, model, action string
		disabled                     bool
	}{
		{"codex", "codex", "saved", workflow.ActionProcess, false},
		{"claude-model", "claude", "saved", workflow.ActionAgent, false},
		{"claude-empty-model", "claude", "", workflow.ActionAgent, false},
		{"disabled", "codex", "saved", workflow.ActionFailed, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws, _, id := taskRepo(t)
			if tc.disabled {
				ws.Cfg.Harnesses.Enabled = []string{"claude"}
			} else {
				ws.Cfg.Harnesses.Enabled = append(ws.Cfg.Harnesses.Enabled, "codex")
			}
			ws.Cfg.Roles.Implementer = "claude"
			ws.Cfg.Harnesses.Claude.Models.Implementer = "changed"
			_, err := ws.Tasks.Update(id, ws.Now(), func(tk *task.Task) error {
				tk.State.Classification = &task.Classification{Plan: false}
				tk.State.AgentChoices = map[string]task.AgentChoice{"implementer": {Harness: tc.harness, Model: tc.model}}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			a, err := RunNext(context.Background(), ws, RunNextOptions{Ref: id})
			if err != nil || a.Action != tc.action {
				t.Fatalf("next: %+v %v", a, err)
			}
			if tc.action == workflow.ActionAgent {
				tk, err := ws.Tasks.Load(id)
				if err != nil {
					t.Fatal(err)
				}
				if a.Model != tc.model || tk.State.PendingStep == nil || tk.State.PendingStep.Model != tc.model {
					t.Fatalf("action=%+v pending=%+v", a, tk.State.PendingStep)
				}
			}
		})
	}
}
