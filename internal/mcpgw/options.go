package mcpgw

import (
	"io"
	"time"

	"bint/internal/config"
	"bint/internal/mcpcat"
)

// Giá trị mặc định của giới hạn (docs/mcp-gateway-plan.md, plan M2 mục 5). Options ghi đè được cho kiểm thử.
const (
	DefaultIdleTimeout   = 10 * time.Minute
	DefaultMaxRestarts   = 3
	DefaultRestartWindow = 10 * time.Minute
	DefaultCallTimeout   = 5 * time.Minute
	DefaultStartTimeout  = 30 * time.Second
	DefaultMaxFrame      = 8 << 20
	DefaultMaxBody       = 8 << 20
	DefaultWriteQueue    = 64
	DefaultSessionTTL    = time.Hour
	DefaultExclusiveTTL  = 10 * time.Minute
	DefaultExclusiveWait = 30 * time.Second
	DefaultRetries       = 3
	DefaultBackoff       = 500 * time.Millisecond
)

// Options cấu hình daemon. Giá trị 0 nghĩa là dùng mặc định.
type Options struct {
	// Dir là thư mục gateway (khoá, gateway.json); mặc định Dir().
	Dir string
	// Addr là địa chỉ nghe; mặc định 127.0.0.1:0 (cổng động). Chỉ chấp nhận địa chỉ loopback.
	Addr string
	// Version ghi vào gateway.json và status.
	Version string
	// Log nhận nhật ký của daemon (không bao giờ chứa giá trị credential); nil = bỏ.
	Log io.Writer

	// Machine nạp đăng ký service trên máy (mỗi lần mở phiên, D3); mặc định config.LoadMachineMCP.
	Machine func() (config.MachineMCP, error)
	// Lookup tra danh mục; mặc định mcpcat.Lookup.
	Lookup func(id string) (mcpcat.Service, bool)

	IdleTimeout   time.Duration // khi đăng ký không đặt idle_timeout
	MaxRestarts   int           // khi đăng ký không đặt max_restarts
	RestartWindow time.Duration
	CallTimeout   time.Duration
	StartTimeout  time.Duration
	MaxFrame      int // kích thước tối đa một frame từ upstream
	MaxBody       int // kích thước tối đa body POST của client
	WriteQueue    int // số frame chờ ghi tối đa tới một upstream stdio
	SessionTTL    time.Duration
	ExclusiveTTL  time.Duration
	ExclusiveWait time.Duration
	Retries       int
	Backoff       time.Duration
}

func (o *Options) fill() {
	if o.Machine == nil {
		o.Machine = config.LoadMachineMCP
	}
	if o.Lookup == nil {
		o.Lookup = mcpcat.Lookup
	}
	if o.Log == nil {
		o.Log = io.Discard
	}
	if o.Addr == "" {
		o.Addr = "127.0.0.1:0"
	}
	if o.IdleTimeout <= 0 {
		o.IdleTimeout = DefaultIdleTimeout
	}
	if o.MaxRestarts <= 0 {
		o.MaxRestarts = DefaultMaxRestarts
	}
	if o.RestartWindow <= 0 {
		o.RestartWindow = DefaultRestartWindow
	}
	if o.CallTimeout <= 0 {
		o.CallTimeout = DefaultCallTimeout
	}
	if o.StartTimeout <= 0 {
		o.StartTimeout = DefaultStartTimeout
	}
	if o.MaxFrame <= 0 {
		o.MaxFrame = DefaultMaxFrame
	}
	if o.MaxBody <= 0 {
		o.MaxBody = DefaultMaxBody
	}
	if o.WriteQueue <= 0 {
		o.WriteQueue = DefaultWriteQueue
	}
	if o.SessionTTL <= 0 {
		o.SessionTTL = DefaultSessionTTL
	}
	if o.ExclusiveTTL <= 0 {
		o.ExclusiveTTL = DefaultExclusiveTTL
	}
	if o.ExclusiveWait <= 0 {
		o.ExclusiveWait = DefaultExclusiveWait
	}
	if o.Retries <= 0 {
		o.Retries = DefaultRetries
	}
	if o.Backoff <= 0 {
		o.Backoff = DefaultBackoff
	}
}
