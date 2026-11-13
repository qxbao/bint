package app

import (
	"context"
	"errors"
	"testing"

	"bint/internal/config"
	"bint/internal/eval"
	"bint/internal/task"
	"bint/internal/workflow"
)

func TestAgentChoicesResumeKeepsSavedHarnessAndModel(t *testing.T) {
	cfg := config.Default()
	cfg.Harnesses.Enabled = append(cfg.Harnesses.Enabled, "codex")
	cfg.Harnesses.Codex.Model = ptrString("new-default")
	prior := map[string]task.AgentChoice{
		"planner":     {Harness: "codex", Model: "gpt-saved"},
		"implementer": {Harness: "codex", Model: "gpt-saved"},
		"reviewer":    {Harness: "codex", Model: "gpt-saved"},
	}
	got, err := agentChoices(cfg, prior, "", nil, "", "b-do")
	if err != nil {
		t.Fatal(err)
	}
	for role, choice := range got {
		if choice.Harness != "codex" || choice.Model != "gpt-saved" {
			t.Errorf("%s changed on resume: %+v", role, choice)
		}
	}
}

func TestAgentChoicesResumeHonorsExplicitOverrides(t *testing.T) {
	cfg := config.Default()
	cfg.Harnesses.Enabled = append(cfg.Harnesses.Enabled, "codex")
	prior := map[string]task.AgentChoice{
		"planner":     {Harness: "codex", Model: "saved"},
		"implementer": {Harness: "codex", Model: "saved"},
		"reviewer":    {Harness: "codex", Model: "saved"},
	}
	got, err := agentChoices(cfg, prior, "", []string{"implementer=claude"}, "explicit", "b-do")
	if err != nil {
		t.Fatal(err)
	}
	if got["planner"].Harness != "codex" || got["planner"].Model != "explicit" {
		t.Fatalf("global model override: %+v", got["planner"])
	}
	if got["implementer"].Harness != "claude" || got["implementer"].Model != "explicit" {
		t.Fatalf("role override: %+v", got["implementer"])
	}
}

func TestAgentChoicesClaudeSnapshotsWorkflowModel(t *testing.T) {
	cfg := config.Default()
	cfg.Workflow.Agent.Model = ptrString("workflow-model")
	cfg.Eval.Agent.Model = ptrString("eval-model")
	cfg.Harnesses.Claude.Models = config.RoleModels{}

	got, err := agentChoices(cfg, nil, "", nil, "", "b-feat")
	if err != nil {
		t.Fatal(err)
	}
	if got["implementer"].Model != "workflow-model" {
		t.Fatalf("implementer model = %q, want workflow model", got["implementer"].Model)
	}
}

func TestAgentChoicesSingleWorkflowIgnoresUnusedDisabledRole(t *testing.T) {
	cfg := config.Default()
	cfg.Harnesses.Enabled = []string{"claude"}
	cfg.Roles.Reviewer = "codex"

	got, err := agentChoices(cfg, nil, "", nil, "", "b-feat")
	if err != nil {
		t.Fatal(err)
	}
	if got["implementer"].Harness != "claude" {
		t.Fatalf("implementer harness = %q, want claude", got["implementer"].Harness)
	}
}

func ptrString(s string) *string { return &s }

func TestAgentChoicesUnstartedRolesUseConfiguredModel(t *testing.T) {
	cfg := config.Default()
	cfg.Harnesses.Enabled = append(cfg.Harnesses.Enabled, "codex")
	cfg.Roles.Implementer = "codex"
	cfg.Harnesses.Codex.Models.Implementer = "configured-implementer"
	prior, err := agentChoices(cfg, nil, "", nil, "", "b-req")
	if err != nil {
		t.Fatal(err)
	}
	for _, legacy := range []bool{false, true} {
		if legacy {
			// Snapshots written before this fix contain empty, unselected roles.
			prior["implementer"] = task.AgentChoice{}
		}
		got, err := agentChoices(cfg, prior, "", nil, "", "b-do")
		if err != nil {
			t.Fatal(err)
		}
		if got["implementer"].Model != "configured-implementer" {
			t.Fatalf("legacy=%v: unstarted role = %+v", legacy, got["implementer"])
		}
	}
}

func TestRunWorkflowSkipsDisabledRolesThatWillNotRun(t *testing.T) {
	for _, mode := range []string{"existing-no-plan", "new-no-plan", "new-classified-no-plan", "planned", "resume"} {
		t.Run(mode, func(t *testing.T) {
			ws, root, id := taskRepo(t)
			ws.K.Config.Roles.Planner = "codex"
			ws.K.Config.Roles.Reviewer = "codex"
			ws.K.Config.Harnesses.Enabled = []string{"claude"}
			current, err := ws.Tasks.Load(id)
			if err != nil {
				t.Fatal(err)
			}
			current.State.Classification = &task.Classification{Plan: false}
			if mode == "planned" || mode == "resume" {
				current.State.Status = task.StatusPlanned
				current.State.Classification.Plan = true
			}
			if mode == "resume" {
				current.State.Workflow = workflow.StepDo
				current.State.Status = task.StatusBlocked
				current.State.ResumeFrom = task.StatusPlanned
			}
			if _, err := ws.Tasks.Update(id, ws.Now(), func(tt *task.Task) error {
				tt.State = current.State
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			cb := RunCallbacks{Agent: func(workflow.StepSpec, *float64, *int) (eval.Agent, error) {
				return fakeStepAgent{root}, nil
			}}
			if mode == "resume" {
				_, err = ResumeWorkflow(context.Background(), ws, RunResumeOptions{Ref: id, NoReview: true, RunCallbacks: cb})
			} else {
				o := RunWorkflowOptions{Workflow: workflow.StepDo, Task: id, NoReview: true, RunCallbacks: cb}
				if mode == "new-no-plan" {
					o.Task, o.Request, o.NoPlan = "", "Sửa hàm A", true
				}
				if mode == "new-classified-no-plan" {
					o.Task, o.Request = "", "Sửa chính tả trong a.go"
				}
				_, err = RunWorkflow(context.Background(), ws, o)
			}
			if err != nil {
				t.Fatalf("unused disabled roles must not prevent the implementer from running: %v", err)
			}
		})
	}
}

func TestRunWorkflowSnapshotsPlannerAfterPlanningOverrides(t *testing.T) {
	for _, mode := range []string{"explicit-depth", "protected-no-plan", "new-protected-no-plan"} {
		t.Run(mode, func(t *testing.T) {
			ws, _, id := taskRepo(t)
			ws.K.Config.Harnesses.Enabled = append(ws.K.Config.Harnesses.Enabled, "codex")
			ws.K.Config.Roles.Planner = "codex"
			ws.K.Config.Harnesses.Codex.Models.Planner = "configured-planner"
			_, err := ws.Tasks.Update(id, ws.Now(), func(tt *task.Task) error {
				tt.State.Classification = &task.Classification{Plan: false}
				if mode == "protected-no-plan" {
					tt.Ticket.Scope.Allow = []string{".github/workflows/check.yml"}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			o := RunWorkflowOptions{Workflow: workflow.StepDo, Task: id, Depth: task.DepthDeep,
				RunCallbacks: RunCallbacks{Agent: func(spec workflow.StepSpec, _ *float64, _ *int) (eval.Agent, error) {
					if step := spec.ID; step != workflow.StepReq {
						t.Errorf("first step = %s, want b-req", step)
					}
					return nil, errors.New("stop before executing planner")
				}}}
			if mode == "protected-no-plan" || mode == "new-protected-no-plan" {
				ws.Human = false
				o.Depth, o.NoPlan = "", true
				if mode == "new-protected-no-plan" {
					o.Allow = []string{".github/workflows/check.yml"}
				}
			}
			_, err = RunWorkflow(context.Background(), ws, o)
			if err != nil {
				t.Fatal(err)
			}
			ticket, err := ws.Tasks.Load(id)
			if err != nil {
				t.Fatal(err)
			}
			if got := ticket.State.AgentChoices["planner"]; got.Harness != "codex" || got.Model != "configured-planner" {
				t.Fatalf("planner was selected before planning overrides: %+v", got)
			}
		})
	}
}

func TestRunWorkflowAgentCommandIgnoresUnusedHarness(t *testing.T) {
	ws, root, id := taskRepo(t)
	ws.K.Config.Roles.Implementer = "codex"
	ws.K.Config.Harnesses.Enabled = []string{"claude"}
	_, err := RunWorkflow(context.Background(), ws, RunWorkflowOptions{Workflow: workflow.StepFeat, Task: id, AgentCommand: "true", RunCallbacks: RunCallbacks{Agent: func(workflow.StepSpec, *float64, *int) (eval.Agent, error) { return fakeStepAgent{root}, nil }}})
	if err != nil {
		t.Fatalf("custom command must not resolve unused disabled harness: %v", err)
	}
}

func TestRunWorkflowNewProtectedScopeRequiresEnabledPlanner(t *testing.T) {
	ws, _, id := taskRepo(t)
	ws.Human = false
	ws.K.Config.Roles.Planner = "codex"
	ws.K.Config.Harnesses.Enabled = []string{"claude"}
	called := false
	_, err := RunWorkflow(context.Background(), ws, RunWorkflowOptions{Workflow: workflow.StepDo, Task: id, NoPlan: true, Allow: []string{".github/workflows/check.yml"}, RunCallbacks: RunCallbacks{Agent: func(workflow.StepSpec, *float64, *int) (eval.Agent, error) { called = true; return nil, nil }}})
	if err == nil || called {
		t.Fatalf("disabled required planner: err=%v agent called=%v", err, called)
	}
}
