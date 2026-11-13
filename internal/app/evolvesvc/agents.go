package evolvesvc

import (
	"fmt"

	"bint/internal/app"
	"bint/internal/config"
	"bint/internal/eval"
	"bint/internal/harness"
)

// EvolverAgent dựng agent evolver: harness theo roles.evolver, model theo
// harnesses.<h>.models.evolver (model ghi đè), chỉ được đọc.
func EvolverAgent(k *app.KnowledgeWS, model, command string) (eval.Agent, error) {
	if command != "" {
		return &eval.CommandAgent{Command: command}, nil
	}
	name := k.Config.Roles.Evolver
	if name == "" {
		name = k.Config.Harnesses.Default
	}
	model = harness.RoleModel(k.Config, name, "evolver", model)
	if name == "codex" {
		model = codexModel(k.Config, model)
		return &eval.CodexRestrictedAgent{Binary: k.Config.Harnesses.Codex.Binary, Model: model, ReadTools: true}, nil
	}
	if name == "claude" || name == "" {
		cfg := k.Config.Eval.Agent
		cfg.AllowedTools = []string{"Read", "Grep", "Glob"}
		cfg.PermissionMode = "dontAsk"
		cfg.ExtraArgs = nil
		return &eval.ClaudeAgent{Cfg: cfg, Model: model}, nil
	}
	return nil, fmt.Errorf("roles.evolver=%q: evolver cần giới hạn công cụ đọc ở Read/Grep/Glob; harness %s chưa có allowlist công cụ tương đương nên không dùng được cho role này", name, name)
}

// ReflectorAgent dựng agent reflector (rút bài học từ thất bại, kế hoạch E2): harness theo
// roles.reflector, model theo harnesses.<h>.models.reflector. Không có công cụ và chạy trong
// thư mục rỗng: đầu vào chỉ là bản tóm tắt đã lọc.
//
// Claude uses --tools ""; Codex uses an isolated, environment-free app-server
// thread with a constrained model catalog and a per-run tool isolation probe.
// Read-only sandbox alone is insufficient: it can still read eval definitions.
func ReflectorAgent(k *app.KnowledgeWS, model, command string) (eval.Agent, error) {
	return noToolAgent(k, "reflector", k.Config.Roles.Reflector, model, command)
}

// HarnessProposerAgent dựng agent đề xuất thay đổi quy trình (H3): harness theo roles.evolver,
// model theo harnesses.<h>.models.evolver. Cùng ràng buộc như reflector — không công cụ, thư mục
// rỗng — vì tập test không bao giờ được là đầu vào của agent sinh đề xuất, mà định
// nghĩa tác vụ (gồm tập test) nằm trong .bint/evals/ nên Read/Grep đọc được.
func HarnessProposerAgent(k *app.KnowledgeWS, model, command string) (eval.Agent, error) {
	return noToolAgent(k, "evolver", k.Config.Roles.Evolver, model, command)
}

// noToolAgent builds a tool-free Claude/Codex agent for reflector or evolver.
func noToolAgent(k *app.KnowledgeWS, role, harnessRole, model, command string) (eval.Agent, error) {
	if command != "" {
		return &eval.CommandAgent{Command: command}, nil
	}
	name := harnessRole
	if name == "" {
		name = k.Config.Harnesses.Default
	}
	if name == "codex" {
		return &eval.CodexRestrictedAgent{Binary: k.Config.Harnesses.Codex.Binary, Model: codexModel(k.Config, harness.RoleModel(k.Config, name, role, model))}, nil
	}
	if name != "claude" && name != "" {
		return nil, fmt.Errorf("roles.%s=%q: chỉ Claude/Codex có cơ chế khoá công cụ của %s; "+
			"harness %s chưa có cơ chế tắt hết công cụ tương đương nên không dùng được cho role này", role, name, role, name)
	}
	model = harness.RoleModel(k.Config, "claude", role, model)
	cfg := k.Config.Eval.Agent
	cfg.AllowedTools = nil
	cfg.PermissionMode = "dontAsk"
	cfg.ExtraArgs = nil
	return &eval.ClaudeAgent{Cfg: cfg, Model: model, NoTools: true}, nil
}

func codexModel(cfg config.Config, selected string) string {
	if selected == "" && cfg.Harnesses.Codex.Model != nil {
		return *cfg.Harnesses.Codex.Model
	}
	return selected
}

// newAgent dựng agent chạy tác vụ cho các lượt đối chiếu (harness h, chế độ đánh giá).
func newAgent(cfg config.Config, h, model string, bare bool) (eval.Agent, error) {
	return harness.NewAgent(h, harness.AgentOptions{Config: cfg, Eval: true, Model: model,
		MaxCost: cfg.Limits.PerRun.MaxCostUSD, Bare: bare})
}
