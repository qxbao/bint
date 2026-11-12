package evolvesvc

import (
	"slices"
	"strings"
	"testing"

	"bint/internal/app"
	"bint/internal/config"
	"bint/internal/eval"
)

// Claude reflector must keep the explicit --tools "" restriction.
func TestReflectorAgentLocksTools(t *testing.T) {
	cfg := config.Default()
	cfg.Eval.Agent.AllowedTools = []string{"Read", "Bash"}
	k := &app.KnowledgeWS{Workspace: &app.Workspace{Config: cfg}}
	k.Config.Roles.Reflector = "claude"
	ag, err := ReflectorAgent(k, "", "")
	if err != nil {
		t.Fatal(err)
	}
	ca, ok := ag.(*eval.ClaudeAgent)
	if !ok || !ca.NoTools {
		t.Fatalf("reflector claude phải là ClaudeAgent{NoTools}: %#v", ag)
	}
	args := ca.ArgsFor(eval.AgentRequest{})
	if i := slices.Index(args, "--tools"); i < 0 || args[i+1] != "" || slices.Contains(args, "--allowedTools") {
		t.Fatalf("thiếu --tools \"\": %q", args)
	}

	// Lệnh tuỳ chọn (kiểm thử) không qua harness.
	if _, err := ReflectorAgent(k, "", "echo"); err != nil {
		t.Fatal(err)
	}
}

func TestCodexEvolutionFactories(t *testing.T) {
	cfg := config.Default()
	cfg.Roles.Evolver, cfg.Roles.Reflector = "codex", "codex"
	cfg.Harnesses.Codex.Binary = "fixture-codex"
	cfg.Harnesses.Codex.ExtraArgs = []string{"--search"}
	cfg.Harnesses.Codex.Models.Evolver = "configured-evolver"
	cfg.Harnesses.Codex.Models.Reflector = "configured-reflector"
	k := &app.KnowledgeWS{Workspace: &app.Workspace{Config: cfg}}
	for _, tc := range []struct {
		name, model string
		factory     func(*app.KnowledgeWS, string, string) (eval.Agent, error)
	}{
		{"evolver", "configured-evolver", EvolverAgent},
		{"reflector", "configured-reflector", ReflectorAgent},
		{"proposer", "configured-evolver", HarnessProposerAgent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ag, err := tc.factory(k, "", "")
			if err != nil {
				t.Fatal(err)
			}
			restricted, ok := ag.(*eval.CodexRestrictedAgent)
			if !ok || restricted.ReadTools != (tc.name == "evolver") || restricted.Binary != "fixture-codex" {
				t.Fatalf("incorrect capability/binary: %#v", ag)
			}
			if ag.Name() != "codex" || ag.(eval.ModelNamer).RequestedModel() != tc.model {
				t.Fatalf("wrong selection: %#v", ag)
			}
			if !strings.Contains(ag.(eval.ReadOnlyEnforcer).ReadOnlyMode(), "Bint") {
				t.Fatal("missing restricted adapter")
			}
		})
	}
}

func TestCodexEvolutionModelPrecedence(t *testing.T) {
	for _, factory := range []func(*app.KnowledgeWS, string, string) (eval.Agent, error){EvolverAgent, ReflectorAgent, HarnessProposerAgent} {
		cfg := config.Default()
		cfg.Roles.Evolver, cfg.Roles.Reflector = "codex", "codex"
		cfg.Harnesses.Codex.Models = config.RoleModels{}
		common := "common"
		cfg.Harnesses.Codex.Model = &common
		k := &app.KnowledgeWS{Workspace: &app.Workspace{Config: cfg}}
		for _, tc := range []struct{ cli, want string }{{"", "common"}, {"cli", "cli"}} {
			ag, err := factory(k, tc.cli, "")
			if err != nil || ag.(eval.ModelNamer).RequestedModel() != tc.want {
				t.Fatalf("model selection: agent=%#v err=%v want=%s", ag, err, tc.want)
			}
		}
	}
}

func TestNoToolAgentStillRejectsUnsupportedHarness(t *testing.T) {
	cfg := config.Default()
	cfg.Roles.Reflector = "antigravity"
	k := &app.KnowledgeWS{Workspace: &app.Workspace{Config: cfg}}
	if _, err := ReflectorAgent(k, "", ""); err == nil || !strings.Contains(err.Error(), "roles.reflector") {
		t.Fatalf("unsupported harness: %v", err)
	}
}
