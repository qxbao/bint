package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"unicode"

	"bint/internal/config"
	"bint/internal/iso"
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
	// Harness và SessionID chỉ dùng ghi nhận; quyền lấy từ run binding.
	Harness, SessionID string
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
	// Định danh phiên chỉ để ghi nhận: sai thì bỏ (ghi stderr), không làm `bint mcp serve` thoát và mất công cụ của Bint.
	harness, sessionID, err := mcpShimSessionIdentity(o.Harness, o.SessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gateway MCP: bỏ qua định danh phiên: %v\n", err)
		harness, sessionID = "", ""
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
		Exe: exe, Services: services, Worktree: ws.Repo.Toplevel, RunID: runID, TaskID: taskID, Harness: harness, SessionID: sessionID,
		Log: os.Stderr, ExeModTime: mcpgw.ExeModTime(), Env: daemonEnv(),
	})
}

// Chỉ CLAUDE_CODE_SESSION_ID đã có nguồn đối chiếu trong workflow/ops.go.
// Không trộn nửa cặp tường minh với môi trường hoặc suy session từ transport.
//
// Fallback từ môi trường chỉ dùng khi tiến trình rõ ràng do Claude Code khởi chạy (CLAUDECODE đặt, không có dấu của harness
// khác): Codex/Gemini mở từ shell trong Claude Code thừa hưởng CLAUDE_CODE_SESSION_ID và sẽ bị ghi nhầm harness=claude.
// Giá trị được đọc một lần khi server khởi động, nên sau /clear hay resume trong cùng tiến trình MCP có thể là phiên cũ;
// harness truyền --harness/--session-id tường minh thì không có giới hạn này.
func mcpShimSessionIdentity(harness, session string) (string, string, error) {
	if harness == "" && session == "" {
		session = strings.TrimSpace(os.Getenv("CLAUDE_CODE_SESSION_ID"))
		if session == "" || !launchedByClaude() {
			return "", "", nil
		}
		harness = "claude"
	}
	if strings.IndexFunc(harness, unicode.IsControl) >= 0 || strings.IndexFunc(session, unicode.IsControl) >= 0 {
		return "", "", fmt.Errorf("định danh phiên MCP không hợp lệ")
	}
	harness, session = strings.TrimSpace(harness), strings.TrimSpace(session)
	if (harness != "claude" && harness != "codex" && harness != "antigravity") || session == "" {
		return "", "", fmt.Errorf("định danh phiên MCP không hợp lệ")
	}
	return harness, session, nil
}

// otherHarnessEnv: biến do harness khác Claude đặt cho tiến trình con.
var otherHarnessEnv = []string{"CODEX_SANDBOX", "CODEX_SANDBOX_NETWORK_DISABLED", "CODEX_THREAD_ID", "CODEX_SESSION_ID", "GEMINI_CLI"}

func launchedByClaude() bool {
	if v := strings.TrimSpace(os.Getenv("CLAUDECODE")); v == "" || v == "0" {
		return false
	}
	for _, k := range otherHarnessEnv {
		if os.Getenv(k) != "" {
			return false
		}
	}
	return true
}

// daemonEnv là env để khởi động daemon gateway: trong profile `bint iso` thì iso.RealEnv(os.Environ()) (HOME/TMPDIR/XDG thật,
// không giữ env của profile đầu tiên cho một daemon dùng chung); ngoài profile thì nil (kế thừa như trước).
func daemonEnv() []string {
	if env := os.Environ(); iso.InProfile(env) {
		return iso.RealEnv(env)
	}
	return nil
}
