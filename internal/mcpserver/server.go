// Package mcpserver là MCP server của Context Engine (`bint mcp serve`, mục 2.3,
// 8.2): JSON-RPC 2.0 qua stdio, mỗi thông điệp một dòng. Chỉ hiện thực phần giao
// thức cần cho công cụ (initialize, ping, tools/list, tools/call).
//
// Các công cụ đều chỉ đọc, trừ cite_knowledge và report_observation (ghi nhận);
// không công cụ nào thay đổi được trạng thái hiệu lực của tri thức.
package mcpserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// SupportedVersions là các phiên bản giao thức MCP server chấp nhận (mới nhất trước).
var SupportedVersions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

// Tool là mô tả một công cụ.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations map[string]any `json:"annotations,omitempty"`
}

// Result là kết quả một lần gọi công cụ.
type Result struct {
	Text       string
	Structured any
	IsError    bool
}

// Handler xử lý một công cụ.
type Handler func(ctx context.Context, args json.RawMessage) (Result, error)

// Proxy là nguồn công cụ bổ sung của server: công cụ của service ngoài do gateway chuyển tiếp (internal/mcpgw.Shim).
// Server không biết gì về gateway; chỉ gộp danh sách và chuyển lời gọi cho công cụ mà Owns báo là của proxy.
type Proxy interface {
	// Ready chờ proxy kết nối xong (có hạn riêng của proxy; ctx là cận trên). Gọi ở initialize trước Tools/Instructions.
	Ready(ctx context.Context)
	// Tools là mô tả công cụ dạng thô (đã đặt tên tới agent).
	Tools() []json.RawMessage
	// Instructions là đoạn bổ sung cho instructions của initialize (ví dụ service lỗi); rỗng nếu không có.
	Instructions() string
	// Owns cho biết tên công cụ thuộc proxy.
	Owns(name string) bool
	// Call chuyển lời gọi và trả nguyên văn phần result của tools/call. Lỗi trả về thành kết quả isError cho agent.
	Call(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error)
}

// ToolsNotifier là phần tuỳ chọn của Proxy có danh sách công cụ đổi được sau initialize (vd. shim nối lại nền được gateway).
// Server khi đó khai capabilities.tools.listChanged=true và gửi notifications/tools/list_changed mỗi lần f được gọi (sau khi
// client đã gửi notifications/initialized).
type ToolsNotifier interface {
	OnToolsChanged(f func())
}

// Server là MCP server qua stdio.
type Server struct {
	Name         string
	Version      string
	Instructions string
	// Proxy (nếu có) cấp thêm công cụ chuyển tiếp; công cụ đăng ký bằng Register luôn được ưu tiên khi trùng tên.
	Proxy    Proxy
	tools    []Tool
	handlers map[string]Handler
	// listChanged: Proxy là ToolsNotifier (đặt trong Serve).
	listChanged bool
	// EOFGrace: hết stdin thì chờ các lời gọi đang chạy tối đa ngần này rồi huỷ chúng (0 = DefaultEOFGrace). Client gửi yêu
	// cầu rồi đóng stdin ngay vẫn nhận đủ kết quả nhanh; lời gọi chuyển tiếp dài không giữ tiến trình sau khi harness đóng phiên.
	EOFGrace time.Duration
}

// DefaultEOFGrace là thời gian chờ lời gọi đang chạy sau khi hết stdin.
const DefaultEOFGrace = 5 * time.Second

// New tạo server.
func New(name, version, instructions string) *Server {
	return &Server{Name: name, Version: version, Instructions: instructions, handlers: map[string]Handler{}}
}

// Register thêm một công cụ.
func (s *Server) Register(t Tool, h Handler) {
	s.tools = append(s.tools, t)
	s.handlers[t.Name] = h
}

// Tools trả về danh sách công cụ.
func (s *Server) Tools() []Tool { return s.tools }

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Serve đọc yêu cầu từ in và ghi phản hồi ra out cho tới khi in đóng, rồi chờ các lời gọi công cụ đang chạy.
//
// Mọi yêu cầu trừ tools/call được xử lý tuần tự theo thứ tự tới (initialize xong mới tới tools/list). tools/call chạy
// đồng thời: lời gọi chuyển tiếp dài (contract test của specmatic) không được chặn các lời gọi khác lẫn
// notifications/cancelled; thông báo huỷ kết thúc ctx của lời gọi tương ứng.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64*1024), 16<<20)
	var mu sync.Mutex
	enc := json.NewEncoder(out)
	write := func(r response) {
		mu.Lock()
		defer mu.Unlock()
		enc.Encode(r)
	}
	// Hết stdin là harness đã đóng phiên: chờ các lời gọi còn chạy trong EOFGrace rồi huỷ chúng (lời gọi chuyển tiếp dài).
	ctx, cancelAll := context.WithCancel(ctx)
	var wg sync.WaitGroup
	defer func() {
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		grace := s.EOFGrace
		if grace <= 0 {
			grace = DefaultEOFGrace
		}
		t := time.NewTimer(grace)
		defer t.Stop()
		select {
		case <-done:
		case <-t.C:
		}
		cancelAll()
		<-done
	}()
	var initialized atomic.Bool
	if n, ok := s.Proxy.(ToolsNotifier); ok && s.Proxy != nil {
		s.listChanged = true
		n.OnToolsChanged(func() {
			if !initialized.Load() || ctx.Err() != nil {
				return // trước notifications/initialized thì tools/list sắp tới đã có công cụ mới
			}
			mu.Lock()
			defer mu.Unlock()
			enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/tools/list_changed"})
		})
	}
	run := &inflight{calls: map[string]context.CancelFunc{}, initialized: &initialized}
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Batch (JSON-RPC mảng) không còn trong MCP 2025-06-18 nhưng vẫn chấp nhận.
		if strings.HasPrefix(line, "[") {
			var batch []json.RawMessage
			if err := json.Unmarshal([]byte(line), &batch); err != nil {
				write(response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32700, "parse error"}})
				continue
			}
			for _, m := range batch {
				s.serveOne(ctx, m, write, &wg, run)
			}
			continue
		}
		s.serveOne(ctx, []byte(line), write, &wg, run)
	}
	return sc.Err()
}

// inflight theo dõi tools/call đang chạy theo id để notifications/cancelled kết thúc được.
type inflight struct {
	mu    sync.Mutex
	calls map[string]context.CancelFunc
	// initialized được đặt khi client gửi notifications/initialized (từ đó server được gửi thông báo).
	initialized *atomic.Bool
}

func idKey(id json.RawMessage) string {
	var b bytes.Buffer
	if json.Compact(&b, id) != nil {
		return string(id)
	}
	return b.String()
}

func (f *inflight) add(id json.RawMessage, cancel context.CancelFunc) {
	f.mu.Lock()
	f.calls[idKey(id)] = cancel
	f.mu.Unlock()
}

func (f *inflight) done(id json.RawMessage) {
	f.mu.Lock()
	if c := f.calls[idKey(id)]; c != nil {
		c()
	}
	delete(f.calls, idKey(id))
	f.mu.Unlock()
}

func (f *inflight) cancel(id json.RawMessage) {
	f.mu.Lock()
	c := f.calls[idKey(id)]
	f.mu.Unlock()
	if c != nil {
		c()
	}
}

// serveOne xử lý một thông điệp: tools/call chạy trong goroutine riêng, còn lại chạy ngay tại chỗ.
func (s *Server) serveOne(ctx context.Context, raw []byte, write func(response), wg *sync.WaitGroup, run *inflight) {
	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		write(response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32700, "parse error"}})
		return
	}
	isNotification := len(req.ID) == 0 || string(req.ID) == "null"
	if isNotification && req.Method == "notifications/initialized" && run.initialized != nil {
		run.initialized.Store(true)
	}
	if isNotification && req.Method == "notifications/cancelled" {
		var p struct {
			RequestID json.RawMessage `json:"requestId"`
		}
		if json.Unmarshal(req.Params, &p) == nil && len(p.RequestID) > 0 {
			run.cancel(p.RequestID)
		}
		return
	}
	if req.Method != "tools/call" || isNotification {
		if r, ok := s.handleReq(ctx, req); ok {
			write(r)
		}
		return
	}
	cctx, cancel := context.WithCancel(ctx)
	run.add(req.ID, cancel)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer run.done(req.ID)
		if r, ok := s.handleReq(cctx, req); ok {
			write(r)
		}
	}()
}

// handleReq xử lý một yêu cầu đã parse; ok=false với thông báo (không có id).
func (s *Server) handleReq(ctx context.Context, req request) (response, bool) {
	isNotification := len(req.ID) == 0 || string(req.ID) == "null"
	res, rerr := s.dispatch(ctx, req)
	if isNotification {
		return response{}, false
	}
	r := response{JSONRPC: "2.0", ID: req.ID}
	if rerr != nil {
		r.Error = rerr
	} else {
		r.Result = res
	}
	return r, true
}

func (s *Server) dispatch(ctx context.Context, req request) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		json.Unmarshal(req.Params, &p)
		v := SupportedVersions[0]
		if slices.Contains(SupportedVersions, p.ProtocolVersion) {
			v = p.ProtocolVersion
		}
		instr := s.Instructions
		if s.Proxy != nil {
			s.Proxy.Ready(ctx)
			if extra := s.Proxy.Instructions(); extra != "" {
				instr = strings.TrimRight(instr, "\n") + "\n" + extra
			}
		}
		return map[string]any{
			"protocolVersion": v,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": s.listChanged}},
			"serverInfo":      map[string]any{"name": s.Name, "version": s.Version},
			"instructions":    instr,
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		if s.Proxy == nil {
			return map[string]any{"tools": s.tools}, nil
		}
		all := make([]any, 0, len(s.tools))
		for _, t := range s.tools {
			all = append(all, t)
		}
		for _, t := range s.Proxy.Tools() {
			all = append(all, t)
		}
		return map[string]any{"tools": all}, nil
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &rpcError{-32602, "tham số không hợp lệ"}
		}
		h, ok := s.handlers[p.Name]
		if !ok && s.Proxy != nil && s.Proxy.Owns(p.Name) {
			if len(p.Arguments) == 0 {
				p.Arguments = json.RawMessage("{}")
			}
			res, err := s.Proxy.Call(ctx, p.Name, p.Arguments)
			if err != nil {
				return map[string]any{"content": []map[string]any{{"type": "text", "text": "lỗi: " + err.Error()}}, "isError": true}, nil
			}
			if !json.Valid(res) {
				return map[string]any{"content": []map[string]any{{"type": "text", "text": "lỗi: kết quả chuyển tiếp không phải JSON"}}, "isError": true}, nil
			}
			return res, nil
		}
		if !ok {
			return nil, &rpcError{-32602, fmt.Sprintf("không có công cụ %q", p.Name)}
		}
		if len(p.Arguments) == 0 {
			p.Arguments = json.RawMessage("{}")
		}
		r, err := h(ctx, p.Arguments)
		if err != nil {
			// Lỗi của công cụ trả về trong kết quả để agent đọc được (không phải lỗi giao thức).
			r = Result{Text: "lỗi: " + err.Error(), IsError: true}
		}
		out := map[string]any{"content": []map[string]any{{"type": "text", "text": r.Text}}}
		if r.Structured != nil {
			out["structuredContent"] = r.Structured
		}
		if r.IsError {
			out["isError"] = true
		}
		return out, nil
	}
	if strings.HasPrefix(req.Method, "notifications/") {
		return nil, nil
	}
	return nil, &rpcError{-32601, "method not found: " + req.Method}
}
