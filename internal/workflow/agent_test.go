package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"bint/internal/config"
	"bint/internal/eval"
	adapters "bint/internal/harness"
)

// Mỗi bước nhận model của role mình trên harness được chọn; --model ghi đè mọi bước;
// role không đặt model thì rơi về model chung của harness.
func TestHarnessAgentsModelPerRole(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	generic := "gpt-generic"
	cfg := config.Default()
	cfg.Harnesses.Enabled = []string{"claude", "codex"}
	cfg.Roles = config.Roles{Planner: "codex", Implementer: "codex", Reviewer: "codex", Evolver: "codex"}
	cfg.Harnesses.Codex.Binary = bin
	cfg.Harnesses.Codex.Model = &generic
	cfg.Harnesses.Codex.Models = config.RoleModels{Planner: "big", Implementer: "small"}

	model := func(f AgentFactory, step string) string {
		t.Helper()
		a, err := f(step, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		return a.(eval.ModelNamer).RequestedModel()
	}
	f := HarnessAgents(cfg, "", "", adapters.Selection{})
	for step, want := range map[string]string{StepReq: "big", StepFeat: "small", StepReview: "gpt-generic"} {
		if got := model(f, step); got != want {
			t.Errorf("%s: model %q, muốn %q", step, got, want)
		}
	}
	f = HarnessAgents(cfg, "", "cli", adapters.Selection{})
	if got := model(f, StepReq); got != "cli" {
		t.Errorf("--model phải ghi đè models.<role>: %q", got)
	}
}

func TestHarnessAgentsWithSavedChoicesUsesSavedCodexModel(t *testing.T) {
	cfg := config.Default()
	cfg.Harnesses.Enabled = append(cfg.Harnesses.Enabled, "codex")
	bin := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg.Harnesses.Codex.Binary = bin
	cfg.Harnesses.Codex.Model = ptrStringAgent("new-config-model")
	f := HarnessAgentsWithChoices(cfg, "", adapters.Selection{Roles: map[string]string{"implementer": "codex"}},
		map[string]string{"implementer": "saved-model"})
	ag, err := f(StepFeat, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	namer, ok := ag.(eval.ModelNamer)
	if !ok || namer.RequestedModel() != "saved-model" {
		t.Fatalf("resume model = %v, want saved-model", ag)
	}
}

func TestHarnessAgentsWithSavedEmptyModelDoesNotPickUpNewConfig(t *testing.T) {
	cfg := config.Default()
	cfg.Harnesses.Enabled = append(cfg.Harnesses.Enabled, "codex")
	bin := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg.Harnesses.Codex.Binary = bin
	newDefault := "new-config-model"
	cfg.Harnesses.Codex.Model = &newDefault
	f := HarnessAgentsWithChoices(cfg, "", adapters.Selection{Roles: map[string]string{"implementer": "codex"}},
		map[string]string{"implementer": ""})
	ag, err := f(StepFeat, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := ag.(eval.ModelNamer).RequestedModel(); got != "" {
		t.Fatalf("empty snapshot was replaced by config model %q", got)
	}
}

func ptrStringAgent(s string) *string { return &s }

func TestHarnessAgentsSavedEmptyClaudeModel(t *testing.T) {
	cfg := config.Default()
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	cfg.Workflow.Agent.Model = ptrStringAgent("new-config-model")
	f := HarnessAgentsWithChoices(cfg, "", adapters.Selection{Roles: map[string]string{"implementer": "claude"}},
		map[string]string{"implementer": ""})
	ag, err := f(StepFeat, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ca := ag.(*eval.ClaudeAgent)
	if got := ca.RequestedModel(); got != "" {
		t.Fatalf("empty snapshot was replaced by config model %q; args %q", got, ca.ArgsFor(eval.AgentRequest{}))
	}
}
