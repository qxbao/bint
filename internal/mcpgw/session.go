package mcpgw

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"sync"
	"time"

	"bint/internal/mcpcat"
)

// sessCtx là ngữ cảnh của phiên, lấy từ header X-Bint-* lúc initialize (shim M3 đặt từ BINT_RUN_ID/BINT_TASK_ID…).
// Role KHÔNG lấy từ header X-Bint-Role: daemon tự giải (ResolveIdentity) và ghi role đã giải vào đây.
type sessCtx struct {
	RunID     string
	TaskID    string
	Role      string
	Harness   string
	SessionID string
}

// Tên header ngữ cảnh phiên.
const (
	HeaderService   = "X-Bint-Service"
	HeaderWorktree  = "X-Bint-Worktree"
	HeaderRunID     = "X-Bint-Run-Id"
	HeaderTaskID    = "X-Bint-Task-Id"
	HeaderRole      = "X-Bint-Role"
	HeaderHarness   = "X-Bint-Harness"
	HeaderSessionID = "X-Bint-Session-Id"
	headerMCPSess   = "Mcp-Session-Id"
)

// session là một phiên MCP của client với gateway (một service).
type session struct {
	id    string
	svc   mcpcat.Service
	p     *partition
	ctx   sessCtx
	root  string // gốc worktree ("" nếu không có)
	spool string // thư mục spool của repo chứa worktree ("" nếu không ghi được)
	// granted là tool (tên upstream) role của phiên được dùng ở service này, do daemon tự giải lúc initialize.
	granted grantedTools

	created time.Time
	done    chan struct{} // đóng khi phiên đóng

	mu         sync.Mutex
	lastActive time.Time
	closed     bool
	calls      map[string]*callRef // id gốc của client (đã nén) → lời gọi đang chạy, cho notifications/cancelled
	sse        chan []byte
	sseOpen    bool
	progSeq    int
}

func newSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *session) touch() {
	s.mu.Lock()
	s.lastActive = time.Now()
	s.mu.Unlock()
}

// idKey chuẩn hoá id JSON-RPC thành khoá so sánh (bỏ khoảng trắng thừa).
func idKey(id json.RawMessage) string {
	var b bytes.Buffer
	if err := json.Compact(&b, id); err != nil {
		return string(id)
	}
	return b.String()
}

// callRef là một lời gọi của client đang chạy: id đã đổi (rỗng tới khi gửi) và hàm huỷ để notifications/cancelled
// kết thúc lời gọi ngay (nhả inflight, semaphore nhóm) thay vì chờ tới CallTimeout.
type callRef struct {
	gw     string
	cancel context.CancelFunc
}

// beginCall đăng ký lời gọi theo id gốc của client; trả ctx bị huỷ khi client gửi notifications/cancelled (hoặc cha huỷ)
// và hàm kết thúc (gỡ đăng ký, giải phóng ctx).
func (s *session) beginCall(parent context.Context, clientID json.RawMessage) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	key := idKey(clientID)
	s.mu.Lock()
	s.calls[key] = &callRef{cancel: cancel}
	s.mu.Unlock()
	return ctx, func() {
		s.mu.Lock()
		delete(s.calls, key)
		s.mu.Unlock()
		cancel()
	}
}

func (s *session) trackCall(clientID json.RawMessage, gwID string) {
	s.mu.Lock()
	if c := s.calls[idKey(clientID)]; c != nil {
		c.gw = gwID
	}
	s.mu.Unlock()
}

// cancelCall huỷ lời gọi theo id gốc của client; trả id đã đổi (rỗng nếu chưa gửi) và ok=false nếu không có lời gọi.
func (s *session) cancelCall(clientID json.RawMessage) (gw string, ok bool) {
	// Huỷ trong khoá: hoặc trackCall đã gán gw trước (người gọi chuyển cancelled tới upstream), hoặc ctx đã bị huỷ trước khi
	// lệnh vào hàng đợi (vòng ghi bỏ lệnh, waiter.markWritten); không còn khe nào lệnh tới upstream mà không nhận cancelled.
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.calls[idKey(clientID)]
	if c == nil {
		return "", false
	}
	c.cancel()
	return c.gw, true
}

func (s *session) nextProgressToken() string {
	s.mu.Lock()
	s.progSeq++
	n := s.progSeq
	s.mu.Unlock()
	return "gwp_" + s.id[:8] + "_" + strconv.Itoa(n)
}

// push gửi một thông báo lên luồng SSE của phiên nếu có; bỏ nếu không có người nghe hoặc hàng đầy.
func (s *session) push(frame []byte) bool {
	s.mu.Lock()
	ch := s.sse
	open := s.sseOpen && !s.closed
	s.mu.Unlock()
	if !open {
		return false
	}
	select {
	case ch <- frame:
		return true
	default:
		return false
	}
}
