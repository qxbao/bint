package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"bint/internal/config"
	"bint/internal/mcpgw"
)

// Tầng dịch vụ gateway MCP (docs/mcp-gateway-plan.md mục 6, M2): start, stop, status, restart. start và status chạy được
// trong phiên agent (M3 để shim tự khởi động daemon); stop và restart chỉ dành cho người (D5): restart một upstream cắt lời
// gọi đang chạy của phiên khác. Daemon không bao giờ do hook khởi động.

// MCPGatewayStartOptions: `bint mcp gateway start [--foreground]`.
type MCPGatewayStartOptions struct {
	Foreground bool `flag:"foreground" form:""`
}

func MCPGatewayStartDefaults(config.Config) MCPGatewayStartOptions { return MCPGatewayStartOptions{} }
func (o MCPGatewayStartOptions) Validate() error                   { return nil }

// MCPGatewayStartResult là kết quả khởi động.
type MCPGatewayStartResult struct {
	// Started=false nghĩa là đã có daemon chạy (không khởi động thêm).
	Started bool   `json:"started"`
	PID     int    `json:"pid"`
	Port    int    `json:"port"`
	Dir     string `json:"dir"`
	LogFile string `json:"log_file"`
}

// MCPGatewayStart khởi động daemon tách rời (ghi gateway.log) hoặc, với Foreground, chạy tại chỗ tới khi ctx kết thúc hoặc
// có /admin/stop. announce (nếu có) nhận kết quả ngay khi daemon sẵn sàng (chế độ foreground gọi trước khi chặn).
func MCPGatewayStart(ctx context.Context, version string, o MCPGatewayStartOptions, log io.Writer, announce func(MCPGatewayStartResult)) (MCPGatewayStartResult, error) {
	if err := o.Validate(); err != nil {
		return MCPGatewayStartResult{}, err
	}
	dir, err := mcpgw.Dir()
	if err != nil {
		return MCPGatewayStartResult{}, err
	}
	res := MCPGatewayStartResult{Dir: dir, LogFile: dir + string(os.PathSeparator) + mcpgw.LogFileName}
	if o.Foreground {
		d, err := mcpgw.Start(mcpgw.Options{Dir: dir, Version: version, Log: log})
		if err != nil {
			return res, err
		}
		res.Started, res.PID, res.Port = true, d.Info().PID, d.Info().Port
		if announce != nil {
			announce(res)
		}
		select {
		case <-ctx.Done():
		case <-d.Done():
		}
		d.Close()
		return res, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return res, fmt.Errorf("không xác định được tệp thực thi của bint: %w", err)
	}
	info, started, err := mcpgw.StartDetached(ctx, dir, exe, []string{"mcp", "gateway", "start", "--foreground"}, nil)
	if err != nil {
		return res, err
	}
	res.Started, res.PID, res.Port = started, info.PID, info.Port
	return res, nil
}

// MCPGatewayStopOptions: `bint mcp gateway stop` (chỉ người).
type MCPGatewayStopOptions struct{}

func MCPGatewayStopDefaults(config.Config) MCPGatewayStopOptions { return MCPGatewayStopOptions{} }
func (o MCPGatewayStopOptions) Validate() error                  { return nil }

// MCPGatewayStopResult: Stopped=false nếu không có daemon chạy.
type MCPGatewayStopResult struct {
	Stopped bool `json:"stopped"`
}

// MCPGatewayStop tắt daemon (kill cả cây upstream). Chỉ người.
func MCPGatewayStop(ctx context.Context, o MCPGatewayStopOptions) (MCPGatewayStopResult, error) {
	if err := o.Validate(); err != nil {
		return MCPGatewayStopResult{}, err
	}
	if err := RequireHuman("bint mcp gateway stop"); err != nil {
		return MCPGatewayStopResult{}, err
	}
	dir, err := mcpgw.Dir()
	if err != nil {
		return MCPGatewayStopResult{}, err
	}
	stopped, err := mcpgw.Stop(ctx, dir)
	return MCPGatewayStopResult{Stopped: stopped}, err
}

// MCPGatewayStatusOptions: `bint mcp gateway status`.
type MCPGatewayStatusOptions struct{}

func MCPGatewayStatusDefaults(config.Config) MCPGatewayStatusOptions {
	return MCPGatewayStatusOptions{}
}
func (o MCPGatewayStatusOptions) Validate() error { return nil }

// MCPGatewayStatusResult: Running=false khi chưa có daemon (không phải lỗi).
type MCPGatewayStatusResult struct {
	Running bool          `json:"running"`
	Dir     string        `json:"dir"`
	Status  *mcpgw.Status `json:"status,omitempty"`
}

// MCPGatewayStatus hỏi daemon qua /admin/status: PID, RSS/PSS, số phiên, lời gọi gần nhất, lỗi của từng upstream.
func MCPGatewayStatus(ctx context.Context, o MCPGatewayStatusOptions) (MCPGatewayStatusResult, error) {
	if err := o.Validate(); err != nil {
		return MCPGatewayStatusResult{}, err
	}
	dir, err := mcpgw.Dir()
	if err != nil {
		return MCPGatewayStatusResult{}, err
	}
	res := MCPGatewayStatusResult{Dir: dir}
	st, _, err := mcpgw.QueryStatus(ctx, dir)
	switch {
	case errors.Is(err, mcpgw.ErrNotRunning):
		return res, nil
	case err != nil:
		return res, err
	}
	res.Running, res.Status = true, &st
	return res, nil
}

// MCPGatewayRestartOptions: `bint mcp gateway restart <service>` (chỉ người).
type MCPGatewayRestartOptions struct {
	Service string `arg:"service" form:"required"`
}

func MCPGatewayRestartDefaults(config.Config) MCPGatewayRestartOptions {
	return MCPGatewayRestartOptions{}
}

func (o MCPGatewayRestartOptions) Validate() error {
	_, err := mcpService(o.Service)
	return err
}

// MCPGatewayRestartResult là kết quả restart.
type MCPGatewayRestartResult struct {
	Service string `json:"service"`
	// Stopped là số upstream đang chạy của service đã bị dừng; lời gọi sau khởi động mới (generation mới).
	Stopped int `json:"stopped"`
}

// MCPGatewayRestart dừng mọi upstream của service và xoá trạng thái lỗi, bộ đếm restart. Chỉ người: lời gọi đang chạy của
// các phiên khác trên upstream đó nhận lỗi.
func MCPGatewayRestart(ctx context.Context, o MCPGatewayRestartOptions) (MCPGatewayRestartResult, error) {
	if err := o.Validate(); err != nil {
		return MCPGatewayRestartResult{}, err
	}
	if err := RequireHuman("bint mcp gateway restart"); err != nil {
		return MCPGatewayRestartResult{}, err
	}
	dir, err := mcpgw.Dir()
	if err != nil {
		return MCPGatewayRestartResult{}, err
	}
	c, err := mcpgw.Connect(dir)
	if err != nil {
		return MCPGatewayRestartResult{}, fmt.Errorf("%w (khởi động bằng `bint mcp gateway start`)", err)
	}
	r, err := c.Restart(ctx, o.Service)
	if err != nil {
		return MCPGatewayRestartResult{}, err
	}
	return MCPGatewayRestartResult{Service: r.Service, Stopped: r.Stopped}, nil
}
