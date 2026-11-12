package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// writeStepMCPConfig ghi <runtimeDir>/mcp/<runID>.json chỉ chứa server MCP của Bint (cùng dạng
// eval/arms.go, nhưng không có --eval-config) và trả đường dẫn. exe là đường dẫn tuyệt đối của binary Bint.
func writeStepMCPConfig(runtimeDir, runID, exe string) (string, error) {
	if exe == "" {
		return "", fmt.Errorf("thiếu đường dẫn binary Bint")
	}
	dir := filepath.Join(runtimeDir, "mcp")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(map[string]any{"mcpServers": map[string]any{
		"bint": map[string]any{"command": exe, "args": []string{"mcp", "serve"}},
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

// stepMCPConfig cấp MCP của Bint cho một bước driver process (docs/mcp-gateway-plan.md mục 4). Trả đường
// dẫn tệp cấu hình và hàm dọn; path rỗng = không cấp (thiếu Spool hoặc không tạo được tệp), bước vẫn chạy như cũ.
func (r *Runner) stepMCPConfig(runID string) (path string, cleanup func()) {
	if r.Spool == "" {
		r.logf("bước %s: không có thư mục trạng thái, không cấp MCP của Bint", runID)
		return "", func() {}
	}
	exe := r.BintExe
	if exe == "" {
		var err error
		if exe, err = os.Executable(); err != nil {
			r.logf("bước %s: không xác định được binary Bint, không cấp MCP: %v", runID, err)
			return "", func() {}
		}
	}
	path, err := writeStepMCPConfig(filepath.Dir(r.Spool), runID, exe)
	if err != nil {
		r.logf("bước %s: không ghi được cấu hình MCP, bước chạy không có MCP của Bint: %v", runID, err)
		return "", func() {}
	}
	return path, func() { _ = os.Remove(path) }
}
