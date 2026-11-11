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
