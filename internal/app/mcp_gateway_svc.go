package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/mcpgw"
)

// Tầng dịch vụ gateway MCP (docs/mcp-gateway-plan.md mục 6, M2): start, stop, status, restart. start và status chạy được
// trong phiên agent (M3 để shim tự khởi động daemon); stop và restart chỉ dành cho người (D5): restart một upstream cắt lời
// gọi đang chạy của phiên khác. Daemon không bao giờ do hook khởi động.

// MCPGatewayStartOptions: `bint mcp gateway start [--foreground]`.
type MCPGatewayStartOptions struct {
	// Foreground không thành ô form: TUI luôn khởi động tách rời (chạy tại chỗ sẽ chặn giao diện).
	Foreground bool `flag:"foreground" form:"hidden"`
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

// ---- mcp gateway log ----

// Giới hạn của `bint mcp gateway log`.
const (
	mcpGatewayLogDefaultLines = 50
	mcpGatewayLogMaxLines     = 1000
	// mcpGatewayLogTailBytes là số byte cuối gateway.log được đọc (tệp tăng mãi, không đọc cả tệp).
	mcpGatewayLogTailBytes = 256 << 10
)

// MCPGatewayLogOptions: `bint mcp gateway log <service> [--lines N]`.
type MCPGatewayLogOptions struct {
	Service string `arg:"service" form:"required"`
	Lines   int    `flag:"lines" form:""`
}

func MCPGatewayLogDefaults(config.Config) MCPGatewayLogOptions {
	return MCPGatewayLogOptions{Lines: mcpGatewayLogDefaultLines}
}

func (o MCPGatewayLogOptions) Validate() error {
	if _, err := mcpService(o.Service); err != nil {
		return err
	}
	if o.Lines < 0 || o.Lines > mcpGatewayLogMaxLines {
		return Usagef("--lines phải trong khoảng 0..%d (0 = mặc định %d)", mcpGatewayLogMaxLines, mcpGatewayLogDefaultLines)
	}
	return nil
}

// MCPGatewayLogUpstream là phần đuôi stderr của upstream của một phân vùng đang chạy.
type MCPGatewayLogUpstream struct {
	Partition string `json:"partition"`
	Running   bool   `json:"running"`
	Stderr    string `json:"stderr,omitempty"`
}

// MCPGatewayLogResult là log gần nhất của một service. Mọi văn bản đã che giá trị credential ở phía daemon.
type MCPGatewayLogResult struct {
	Service   string                  `json:"service"`
	Upstreams []MCPGatewayLogUpstream `json:"upstreams"`
	LogFile   string                  `json:"log_file"`
	// Lines là các dòng của gateway.log thuộc service (mới nhất cuối), tối đa Lines dòng.
	Lines []string `json:"lines"`
	// Note giải thích phần thiếu (daemon không chạy, daemon bản cũ chưa có /admin/log, tệp log chưa có).
	Note string `json:"note,omitempty"`
}

// MCPGatewayLog trả stderr gần nhất của các upstream đang chạy (hỏi daemon qua /admin/log) và các dòng gateway.log của
// service. Daemon không chạy hoặc là bản cũ (404) thì chỉ còn phần gateway.log, kèm Note. Chỉ đọc; agent chạy được.
func MCPGatewayLog(ctx context.Context, o MCPGatewayLogOptions) (MCPGatewayLogResult, error) {
	res := MCPGatewayLogResult{Upstreams: []MCPGatewayLogUpstream{}, Lines: []string{}}
	if err := o.Validate(); err != nil {
		return res, err
	}
	if o.Lines == 0 {
		o.Lines = mcpGatewayLogDefaultLines
	}
	res.Service = o.Service
	dir, err := mcpgw.Dir()
	if err != nil {
		return res, err
	}
	res.LogFile = filepath.Join(dir, mcpgw.LogFileName)
	var notes []string
	c, err := mcpgw.Connect(dir)
	if err == nil {
		var parts []mcpgw.PartitionLog
		parts, err = c.Log(ctx, o.Service)
		for _, p := range parts {
			res.Upstreams = append(res.Upstreams, MCPGatewayLogUpstream{Partition: p.Partition, Running: p.Running, Stderr: p.Stderr})
		}
	}
	var se *mcpgw.StatusError
	switch {
	case err == nil:
	case errors.Is(err, mcpgw.ErrNotRunning):
		notes = append(notes, "gateway không chạy: chỉ có phần gateway.log")
	case errors.As(err, &se) && se.Code == http.StatusNotFound:
		notes = append(notes, "gateway đang chạy là bản cũ chưa có /admin/log (khởi động lại bằng `bint mcp gateway stop` rồi `start`): chỉ có phần gateway.log")
	default:
		notes = append(notes, "không lấy được stderr của upstream: "+err.Error())
	}
	lines, err := mcpGatewayLogLines(res.LogFile, o.Service, o.Lines)
	switch {
	case errors.Is(err, os.ErrNotExist):
		notes = append(notes, "chưa có tệp gateway.log")
	case err != nil:
		return res, err
	}
	res.Lines = lines
	res.Note = strings.Join(notes, "; ")
	return res, nil
}

// mcpGatewayLogLines đọc đuôi tệp log và giữ tối đa n dòng cuối có dạng `<RFC3339> <service> (` (định dạng Daemon.logf: mọi
// dòng gắn với một service đều mở đầu bằng `<service> (`, kể cả từ chối mở phiên và lỗi ghi cache tools/list).
func mcpGatewayLogLines(path, service string, n int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return []string{}, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return []string{}, err
	}
	off := int64(0)
	if fi.Size() > mcpGatewayLogTailBytes {
		off = fi.Size() - mcpGatewayLogTailBytes
	}
	data := make([]byte, fi.Size()-off)
	if _, err := f.ReadAt(data, off); err != nil && !errors.Is(err, io.EOF) {
		return []string{}, err
	}
	text := string(data)
	if off > 0 { // dòng đầu có thể bị cắt giữa chừng
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			text = text[i+1:]
		}
	}
	out := []string{}
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimRight(l, "\r")
		ts, rest, ok := strings.Cut(l, " ")
		if !ok || !strings.HasPrefix(rest, service+" (") {
			continue
		}
		if _, err := time.Parse(time.RFC3339, ts); err != nil {
			continue
		}
		out = append(out, l)
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out, nil
}
