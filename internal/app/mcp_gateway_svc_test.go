package app

import (
	"errors"
	"testing"

	"bint/internal/mcpgw"
	"bint/internal/testutil"
)

// stop và restart chỉ dành cho người (D5); status và validate chạy được trong phiên agent.
func TestMCPGatewayHumanOnly(t *testing.T) {
	testutil.Isolate(t)
	asAgent(t)
	ctx := t.Context()
	if _, err := MCPGatewayStop(ctx, MCPGatewayStopOptions{}); !errors.Is(err, ErrAgentForbidden) {
		t.Errorf("stop trong phiên agent: %v, muốn ErrAgentForbidden", err)
	}
	if _, err := MCPGatewayRestart(ctx, MCPGatewayRestartOptions{Service: "codegraph"}); !errors.Is(err, ErrAgentForbidden) {
		t.Errorf("restart trong phiên agent: %v, muốn ErrAgentForbidden", err)
	}
	if r, err := MCPGatewayStatus(ctx, MCPGatewayStatusOptions{}); err != nil || r.Running {
		t.Errorf("status khi chưa chạy: %+v %v", r, err)
	}

	asHuman(t)
	if _, err := MCPGatewayRestart(ctx, MCPGatewayRestartOptions{Service: "khong-co"}); err == nil {
		t.Error("service lạ phải lỗi")
	}
	if _, err := MCPGatewayRestart(ctx, MCPGatewayRestartOptions{Service: "codegraph"}); !errors.Is(err, errNotRunning()) {
		t.Errorf("restart khi chưa có daemon: %v", err)
	}
	if r, err := MCPGatewayStop(ctx, MCPGatewayStopOptions{}); err != nil || r.Stopped {
		t.Errorf("stop khi chưa chạy: %+v %v", r, err)
	}
}

func errNotRunning() error { return mcpgw.ErrNotRunning }
