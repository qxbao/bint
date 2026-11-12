package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"bint/internal/config"
	"bint/internal/mcpgw"
	"bint/internal/ulid"
)

// NewMCPShim tính tập ứng viên theo role: nil (không chạm gateway) khi tắt, không có service khả dụng hoặc run_id không có
// bản ghi; có service khả dụng thì Shim hoạt động.
func TestNewMCPShimCandidates(t *testing.T) {
	f := newMCPFx(t, "  services: {specmatic: {}}\n  grants:\n    reviewer: [specmatic.*]\n    implementer: [specmatic.mock]\n")
	if err := config.SaveMachineMCP(config.MachineMCP{Services: map[string]config.Registration{"specmatic": {Command: "specmatic-mcp"}}}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ws, err := OpenWorkspace(ctx, f.root, false)
	if err != nil {
		t.Fatal(err)
	}
	bind := func(role string, ro bool) string {
		b := mcpgw.RunBinding{RunID: ulid.New(), TaskID: ulid.New(), Step: "x", Role: role, ReadOnly: ro}
		if _, err := mcpgw.WriteRunBinding(ws.Paths.Runtime, b); err != nil {
			t.Fatal(err)
		}
		return b.RunID
	}
	t.Setenv(EnvMCPNoGateway, "")
	gwDir := filepath.Join(os.Getenv("BINT_STATE_DIR"), "gateway")

	// interactive (D4 ngầm hoặc grants.interactive không khai): dự án khai specmatic và máy đã đăng ký → có ứng viên.
	sh, err := NewMCPShim(ctx, f.root, MCPShimOptions{Exe: "/x/bint"})
	if err != nil || sh == nil || !sh.Active() {
		t.Fatalf("interactive: %v %v", sh, err)
	}
	// reviewer (bước chỉ đọc) vẫn có compat; implementer chỉ có mock.
	for _, role := range []struct {
		name string
		ro   bool
	}{{"reviewer", true}, {"implementer", false}} {
		sh, err := NewMCPShim(ctx, f.root, MCPShimOptions{RunID: bind(role.name, role.ro), Exe: "/x/bint"})
		if err != nil || sh == nil {
			t.Errorf("%s: %v %v", role.name, sh, err)
		}
	}
	// run_id không có bản ghi: nil.
	if sh, err := NewMCPShim(ctx, f.root, MCPShimOptions{RunID: ulid.New()}); sh != nil || err != nil {
		t.Errorf("run_id không bản ghi: %v %v", sh, err)
	}
	// planner không có grants → không có gì.
	if sh, _ := NewMCPShim(ctx, f.root, MCPShimOptions{RunID: bind("planner", true)}); sh != nil {
		t.Error("role không được cấp gì thì không có shim")
	}
	// BINT_MCP_NO_GATEWAY tắt hẳn.
	t.Setenv(EnvMCPNoGateway, "1")
	if sh, err := NewMCPShim(ctx, f.root, MCPShimOptions{}); sh != nil || err != nil {
		t.Errorf("NO_GATEWAY: %v %v", sh, err)
	}
	// Ngoài repo: nil.
	t.Setenv(EnvMCPNoGateway, "")
	if sh, err := NewMCPShim(ctx, t.TempDir(), MCPShimOptions{}); sh != nil || err != nil {
		t.Errorf("ngoài repo: %v %v", sh, err)
	}
	if _, err := os.Stat(gwDir); err == nil {
		t.Error("NewMCPShim không được tạo thư mục gateway")
	}
}
