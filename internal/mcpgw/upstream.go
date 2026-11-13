package mcpgw

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
)

// upstream là một kết nối tới service MCP thật (một generation của một phân vùng).
type upstream interface {
	// ensure bảo đảm handshake đã xong (stdio: đã làm lúc khởi động; http: làm lười, dùng chung mọi phiên).
	ensure(ctx context.Context) error
	// hello trả kết quả initialize và tools/list (đã gộp trang) của generation này.
	hello() (initResult, toolsResult json.RawMessage)
	// call gửi một request tới upstream và chờ phản hồi. sess là nhãn phiên trong id đổi (gw_<gen>_<seq>_<phiên>);
	// onID nhận id đã đổi trước khi gửi (để chuyển notifications/cancelled). readOnly=false và lệnh có thể đã tới upstream
	// mà không rõ kết quả → *rpcError mã CodeUnknownOutcome; không bao giờ tự gọi lại (trừ retry của upstream HTTP cho
	// tool chỉ đọc).
	call(ctx context.Context, sess, method string, params json.RawMessage, readOnly bool, onID func(string)) (upResult, error)
	// cancel chuyển notifications/cancelled cho request đã đổi sang id gwID.
	cancel(gwID string, reason json.RawMessage)
	// routeProgress đăng ký nơi nhận notifications/progress mang progressToken (đã đổi sang token của gateway).
	routeProgress(token string, fn func(params json.RawMessage)) (unroute func())
	generation() int
	pid() int
	alive() bool
	// stop dừng chủ động (kill cả cây); không tính là chết bất thường.
	stop()
}

// upResult là phản hồi của upstream cho một request: result hoặc error (JSON-RPC) giữ nguyên byte.
type upResult struct {
	result json.RawMessage
	rpcErr json.RawMessage
}

// heldCancel là lỗi huỷ (errors.Is context.Canceled) của lời gọi đã tới upstream: settled đóng khi upstream trả lời,
// chết, hoặc tới hạn chót của lời gọi. Người gọi giữ chỗ trong nhóm (semaphore, inflight) tới lúc đó để giới hạn
// đồng thời vẫn đúng với chính upstream.
type heldCancel struct{ settled <-chan struct{} }

func (*heldCancel) Error() string { return context.Canceled.Error() }
func (*heldCancel) Unwrap() error { return context.Canceled }

// errStopped là nguyên nhân khi gateway chủ động dừng upstream (idle, restart, tắt daemon).
var errStopped = errors.New("gateway dừng upstream")

// progressTable ánh xạ progressToken (đã đổi) → nơi nhận.
type progressTable struct {
	mu sync.Mutex
	m  map[string]func(json.RawMessage)
}

func (t *progressTable) add(token string, fn func(json.RawMessage)) func() {
	t.mu.Lock()
	if t.m == nil {
		t.m = map[string]func(json.RawMessage){}
	}
	t.m[token] = fn
	t.mu.Unlock()
	return func() {
		t.mu.Lock()
		delete(t.m, token)
		t.mu.Unlock()
	}
}

// deliver chuyển params của một notifications/progress tới nơi đã đăng ký; false nếu không có.
func (t *progressTable) deliver(params json.RawMessage) bool {
	var p struct {
		Token json.RawMessage `json:"progressToken"`
	}
	if json.Unmarshal(params, &p) != nil || len(p.Token) == 0 {
		return false
	}
	var tok string
	if json.Unmarshal(p.Token, &tok) != nil {
		return false
	}
	t.mu.Lock()
	fn := t.m[tok]
	t.mu.Unlock()
	if fn == nil {
		return false
	}
	fn(params)
	return true
}

// tailBuffer giữ max byte cuối của luồng ghi vào (an toàn đa luồng): stderr của upstream.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	b   []byte
	cut bool // đã bỏ phần đầu: dòng đầu của b có thể bị cắt giữa chừng
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if len(t.b) > t.max {
		t.b = append([]byte(nil), t.b[len(t.b)-t.max:]...)
		t.cut = true
	}
	return len(p), nil
}

// Lines trả nội dung bộ đệm, bỏ dòng đầu nếu bộ đệm đã bị cắt: dòng đó có thể chỉ còn đuôi một giá trị credential mà
// ScrubSecrets (thay nguyên giá trị) không nhận ra.
func (t *tailBuffer) Lines() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := string(t.b)
	if t.cut {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			return s[i+1:]
		}
		return ""
	}
	return s
}

// handshakeParams là params initialize gateway gửi upstream: khai báo roots (gateway trả gốc worktree), không khai
// báo sampling hay elicitation.
func handshakeParams(version string) json.RawMessage {
	if version == "" {
		version = "dev"
	}
	return marshalRaw(map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{"roots": map[string]any{}},
		"clientInfo":      map[string]any{"name": "bint-gateway", "version": version},
	})
}

// mergeTools gộp các trang tools/list thành một result {"tools":[...]} (bỏ nextCursor).
func mergeTools(pages []json.RawMessage) json.RawMessage {
	var all []json.RawMessage
	for _, p := range pages {
		var r struct {
			Tools []json.RawMessage `json:"tools"`
		}
		if json.Unmarshal(p, &r) == nil {
			all = append(all, r.Tools...)
		}
	}
	if all == nil {
		all = []json.RawMessage{}
	}
	return marshalRaw(map[string]any{"tools": all})
}

// nextCursor đọc nextCursor của một trang tools/list.
func nextCursor(page json.RawMessage) string {
	var r struct {
		Next string `json:"nextCursor"`
	}
	_ = json.Unmarshal(page, &r)
	return r.Next
}
