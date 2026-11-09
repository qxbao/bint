// Package mcpserver là MCP server của Context Engine (`bint mcp serve`, mục 2.3,
// 8.2): JSON-RPC 2.0 qua stdio, mỗi thông điệp một dòng. Chỉ hiện thực phần giao
// thức cần cho công cụ (initialize, ping, tools/list, tools/call).
//
// Các công cụ đều chỉ đọc, trừ cite_knowledge và report_observation (ghi nhận);
// không công cụ nào thay đổi được trạng thái hiệu lực của tri thức.
package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
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

// Server là MCP server qua stdio.
type Server struct {
	Name         string
	Version      string
	Instructions string
	tools        []Tool
	handlers     map[string]Handler
}

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

// Serve đọc yêu cầu từ in và ghi phản hồi ra out cho tới khi in đóng.
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
				if r, ok := s.handle(ctx, m); ok {
					write(r)
				}
			}
			continue
		}
		if r, ok := s.handle(ctx, []byte(line)); ok {
			write(r)
		}
	}
	return sc.Err()
}

// handle xử lý một thông điệp; ok=false với thông báo (không có id).
func (s *Server) handle(ctx context.Context, raw []byte) (response, bool) {
	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		return response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32700, "parse error"}}, true
	}
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
		return map[string]any{
			"protocolVersion": v,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": s.Name, "version": s.Version},
			"instructions":    s.Instructions,
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": s.tools}, nil
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &rpcError{-32602, "tham số không hợp lệ"}
		}
		h, ok := s.handlers[p.Name]
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
