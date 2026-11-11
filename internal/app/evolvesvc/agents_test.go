package evolvesvc

import (
	"slices"
	"strings"
	"testing"

	"bint/internal/app"
	"bint/internal/config"
	"bint/internal/eval"
)

// reflectorAgent với Claude phải khoá hết công cụ bằng cờ tường minh; harness khác bị từ chối.
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

	k.Config.Roles.Reflector = "codex"
	if _, err := ReflectorAgent(k, "", ""); err == nil || !strings.Contains(err.Error(), "roles.reflector") {
		t.Fatalf("harness khác claude phải bị từ chối: %v", err)
	}
	// Lệnh tuỳ chọn (kiểm thử) không qua harness.
	if _, err := ReflectorAgent(k, "", "echo"); err != nil {
		t.Fatal(err)
	}
}
