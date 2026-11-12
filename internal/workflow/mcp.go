package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"bint/internal/config"
	adapters "bint/internal/harness"
	"bint/internal/mcpgw"
)

// writeStepMCPConfig ghi <stateRoot>/mcp/<runID>.json chỉ chứa server MCP của Bint và trả đường dẫn. Server là cửa vào gộp
// `bint mcp serve`; run/task đặt vào ĐỐI SỐ (--run-id, --task-id) chứ không chỉ env, vì Codex lọc env của server MCP stdio.
// exe là đường dẫn tuyệt đối của binary Bint.
func writeStepMCPConfig(stateRoot, runID, taskID, exe string) (string, error) {
	if exe == "" {
		return "", fmt.Errorf("thiếu đường dẫn binary Bint")
	}
	dir := filepath.Join(stateRoot, "mcp")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	args := []string{"mcp", "serve", "--run-id", runID}
	if taskID != "" {
		args = append(args, "--task-id", taskID)
	}
	b, err := json.MarshalIndent(map[string]any{"mcpServers": map[string]any{
		"bint": map[string]any{"command": exe, "args": args},
	}}, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, runID+".json")
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// stepMCP cấp MCP cho một bước driver process (docs/mcp-gateway-plan.md mục 4 và 7): ghi cấu hình MCP (server bint =
// `bint mcp serve` kèm run/task), ghi bản ghi lượt chạy để daemon gateway tự giải role của bước (xoá sau bước), và tính tập
// tool của service ngoài mà role của bước được cấp (allowlist của Claude, approval của Codex) bằng chính config.MCP.Resolve
// mà daemon dùng. path rỗng = không cấp (thiếu Spool hoặc không tạo được tệp), bước vẫn chạy như cũ.
func (r *Runner) stepMCP(sr *stepRun, taskID string) (path string, tools []string, cleanup func()) {
	runID := sr.child
	if r.Spool == "" {
		r.logf("bước %s: không có thư mục trạng thái, không cấp MCP của Bint", runID)
		return "", nil, func() {}
	}
	exe := r.BintExe
	if exe == "" {
		var err error
		if exe, err = os.Executable(); err != nil {
			r.logf("bước %s: không xác định được binary Bint, không cấp MCP: %v", runID, err)
			return "", nil, func() {}
		}
	}
	stateRoot := filepath.Dir(r.Spool)
	path, err := writeStepMCPConfig(stateRoot, runID, taskID, exe)
	if err != nil {
		r.logf("bước %s: không ghi được cấu hình MCP, bước chạy không có MCP của Bint: %v", runID, err)
		return "", nil, func() {}
	}
	runtimeDir := filepath.Join(stateRoot, "runtime")
	role := adapters.StepRole(sr.step)
	if _, err := mcpgw.WriteRunBinding(runtimeDir, mcpgw.RunBinding{RunID: runID, TaskID: taskID, Step: sr.step,
		Role: role, ReadOnly: sr.readOnly, Worktree: r.Root}); err != nil {
		// Không có bản ghi thì daemon không cấp service nào (đóng an toàn): bước vẫn có tool của Bint.
		r.logf("bước %s: không ghi được bản ghi lượt chạy, bước không có tool của service ngoài: %v", runID, err)
	} else {
		tools = StepMCPTools(r.Cfg, role, sr.readOnly)
	}
	return path, tools, func() {
		_ = os.Remove(path)
		mcpgw.RemoveRunBinding(runtimeDir, runID)
	}
}

// StepMCPTools trả tên tool (tên tới agent, không có tiền tố mcp__bint__) của service ngoài mà bước của role được cấp: grants
// của role giao mcp.services giao service đã đăng ký trên máy, bỏ tool ghi vào cây làm việc khi bước chỉ đọc.
func StepMCPTools(cfg config.Config, role string, readOnly bool) []string {
	if len(cfg.MCP.Services) == 0 {
		return nil
	}
	m, err := config.LoadMachineMCP()
	if err != nil {
		return nil
	}
	registered := map[string]bool{}
	for id := range m.Services {
		registered[id] = true
	}
	var out []string
	for _, t := range cfg.MCP.Resolve(role, readOnly, registered).Granted {
		if !slices.Contains(out, t.Agent) {
			out = append(out, t.Agent)
		}
	}
	return out
}
