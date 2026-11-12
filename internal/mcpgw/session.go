package mcpgw

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"sync"
	"time"

	"bint/internal/mcpcat"
)

// sessCtx là ngữ cảnh của phiên, lấy từ header X-Bint-* lúc initialize (shim M3 đặt từ BINT_RUN_ID/BINT_TASK_ID…).
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

	created time.Time
	done    chan struct{} // đóng khi phiên đóng

	mu         sync.Mutex
	lastActive time.Time
	closed     bool
	calls      map[string]string // id gốc của client (đã nén) → id của gateway, cho notifications/cancelled
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

func (s *session) trackCall(clientID json.RawMessage, gwID string) {
	s.mu.Lock()
	s.calls[idKey(clientID)] = gwID
	s.mu.Unlock()
}

func (s *session) untrackCall(clientID json.RawMessage) {
	s.mu.Lock()
	delete(s.calls, idKey(clientID))
	s.mu.Unlock()
}

func (s *session) gwID(clientID json.RawMessage) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.calls[idKey(clientID)]
	return id, ok
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
