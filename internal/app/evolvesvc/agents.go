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
	if name == "claude" || name == "" {
		cfg := k.Config.Eval.Agent
		cfg.AllowedTools = []string{"Read", "Grep", "Glob"}
		cfg.PermissionMode = "dontAsk"
		cfg.ExtraArgs = nil
		return &eval.ClaudeAgent{Cfg: cfg, Model: model}, nil
	}
	return harness.NewAgent(name, harness.AgentOptions{Config: k.Config, ReadOnly: true, Model: model})
}

// ReflectorAgent dựng agent reflector (rút bài học từ thất bại, kế hoạch E2): harness theo
// roles.reflector, model theo harnesses.<h>.models.reflector. Không có công cụ và chạy trong
// thư mục rỗng: đầu vào chỉ là bản tóm tắt đã lọc.
//
// Ràng buộc "không công cụ" được ép bằng cờ tường minh của Claude Code (--tools ""), vì tập test
// không bao giờ được là đầu vào của reflector. Harness khác chưa có cơ chế tắt hết công cụ tương
// đương (sandbox chỉ-đọc vẫn đọc được toàn hệ tệp, gồm định nghĩa tác vụ tập test), nên
// roles.reflector khác claude bị từ chối thay vì chạy với ràng buộc lỏng hơn.
func ReflectorAgent(k *app.KnowledgeWS, model, command string) (eval.Agent, error) {
	return noToolAgent(k, "reflector", k.Config.Roles.Reflector, model, command)
}

// HarnessProposerAgent dựng agent đề xuất thay đổi quy trình (H3): harness theo roles.evolver,
// model theo harnesses.<h>.models.evolver. Cùng ràng buộc như reflector — không công cụ, thư mục
// rỗng, chỉ claude — vì tập test không bao giờ được là đầu vào của agent sinh đề xuất, mà định
// nghĩa tác vụ (gồm tập test) nằm trong .bint/evals/ nên Read/Grep đọc được.
func HarnessProposerAgent(k *app.KnowledgeWS, model, command string) (eval.Agent, error) {
	return noToolAgent(k, "evolver", k.Config.Roles.Evolver, model, command)
}

// noToolAgent dựng agent Claude Code không có công cụ cho một role (reflector | evolver).
func noToolAgent(k *app.KnowledgeWS, role, harnessRole, model, command string) (eval.Agent, error) {
	if command != "" {
		return &eval.CommandAgent{Command: command}, nil
	}
	name := harnessRole
	if name == "" {
		name = k.Config.Harnesses.Default
	}
	if name != "claude" && name != "" {
		return nil, fmt.Errorf("roles.%s=%q: chỉ harness claude khoá được công cụ của %s (--tools \"\"); "+
			"harness %s chưa có cơ chế tắt hết công cụ tương đương nên không dùng được cho role này", role, name, role, name)
	}
	model = harness.RoleModel(k.Config, "claude", role, model)
	cfg := k.Config.Eval.Agent
	cfg.AllowedTools = nil
	cfg.PermissionMode = "dontAsk"
	cfg.ExtraArgs = nil
	return &eval.ClaudeAgent{Cfg: cfg, Model: model, NoTools: true}, nil
}

// newAgent dựng agent chạy tác vụ cho các lượt đối chiếu (harness h, chế độ đánh giá).
func newAgent(cfg config.Config, h, model string, bare bool) (eval.Agent, error) {
	return harness.NewAgent(h, harness.AgentOptions{Config: cfg, Eval: true, Model: model,
		MaxCost: cfg.Limits.PerRun.MaxCostUSD, Bare: bare})
}
