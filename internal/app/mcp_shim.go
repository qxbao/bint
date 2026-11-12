package app

import (
	"context"
	"os"
	"strings"

	"bint/internal/config"
	"bint/internal/mcpgw"
	"bint/internal/workflow"
)

// Biến môi trường tắt cửa vào gộp: shim chỉ còn công cụ của Bint, không đọc gateway.json, không khởi động daemon.
const EnvMCPNoGateway = "BINT_MCP_NO_GATEWAY"

// MCPShimOptions: đầu vào của phần gateway trong `bint mcp serve` (docs/mcp-gateway-plan.md mục 7).
type MCPShimOptions struct {
	// RunID, TaskID: lượt chạy của bước (cờ ẩn --run-id/--task-id của `serve`); rỗng thì lấy từ BINT_RUN_ID/BINT_TASK_ID.
	// Cờ được ưu tiên vì Codex lọc env của server MCP stdio, nên env có thể không tới shim.
	RunID, TaskID string
	// Exe là binary Bint để khởi động daemon (rỗng = os.Executable()).
	Exe string
}

// NewMCPShim dựng shim cho phiên `bint mcp serve` tại dir. Trả nil (không lỗi) khi không có gì để chuyển tiếp: tắt bằng
// BINT_MCP_NO_GATEWAY=1, dir không phải repo đã init, dự án không khai mcp.services, hoặc role của phiên không có service nào
// khả dụng (đã đăng ký trên máy, được cấp, không bị loại vì bước chỉ đọc). Khi đó không chạm gateway.json hay daemon.
//
// Đây chỉ là bước tính tập ứng viên để khỏi khởi động daemon vô ích; daemon tự giải role lần nữa và là nơi quyết định.
func NewMCPShim(ctx context.Context, dir string, o MCPShimOptions) (*mcpgw.Shim, error) {
	if v := strings.TrimSpace(os.Getenv(EnvMCPNoGateway)); v != "" && v != "0" && !strings.EqualFold(v, "false") {
		return nil, nil
	}
	runID, taskID := o.RunID, o.TaskID
	if runID == "" {
		runID = os.Getenv(workflow.EnvRunID)
	}
	if taskID == "" {
		taskID = os.Getenv(workflow.EnvTaskID)
	}
	ws, err := OpenWorkspace(ctx, dir, false)
	if err != nil {
		return nil, nil // không phải repo (hoặc chưa init): không có dự án để cấp quyền
	}
	if len(ws.Config.MCP.Services) == 0 {
		return nil, nil
	}
	id, err := mcpgw.ResolveIdentity(ws.Paths.Runtime, runID, taskID)
	if err != nil {
		return nil, nil // run_id không có bản ghi: daemon cũng sẽ không cấp gì
	}
	m, err := config.LoadMachineMCP()
	if err != nil {
		return nil, nil
	}
	registered := map[string]bool{}
	for k := range m.Services {
		registered[k] = true
	}
	res := ws.Config.MCP.Resolve(id.Role, id.ReadOnly, registered)
	services := res.Services()
	if len(services) == 0 {
		return nil, nil
	}
	exe := o.Exe
	if exe == "" {
		if exe, err = os.Executable(); err != nil {
			return nil, nil
		}
	}
	return mcpgw.NewShim(mcpgw.ShimOptions{
		Exe: exe, Services: services, Worktree: ws.Repo.Toplevel, RunID: runID, TaskID: taskID,
		Log: os.Stderr,
	})
}
