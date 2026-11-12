// Package probe là client MCP tối thiểu (viết tay, như internal/mcpserver) để kiểm tra service MCP:
// initialize, tools/list và tools/call qua stdio (JSON-RPC theo dòng) hoặc Streamable HTTP (phản hồi JSON hoặc SSE).
package probe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
)

// ProtocolVersion là phiên bản giao thức MCP mà client xin khi initialize.
const ProtocolVersion = "2025-06-18"

// ClientName là tên client gửi trong clientInfo.
const ClientName = "bint"

// Tool là một tool trong tools/list.
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ServerInfo là kết quả initialize rút gọn.
type ServerInfo struct {
	Name            string
	Version         string
	ProtocolVersion string
}

// CallResult là kết quả tools/call. IsError: server báo lỗi ở mức công cụ (isError); Text gộp các khối text.
type CallResult struct {
	IsError bool
	Text    string
}

// RPCError là lỗi JSON-RPC do server trả về.
type RPCError struct {
	Code    int
	Message string
}

func (e *RPCError) Error() string { return fmt.Sprintf("lỗi JSON-RPC %d: %s", e.Code, e.Message) }

// transport là một kênh JSON-RPC: call gửi yêu cầu và chờ phản hồi cùng id; notify gửi thông báo (không phản hồi).
type transport interface {
	call(ctx context.Context, id int64, method string, params any) (json.RawMessage, error)
	notify(ctx context.Context, method string, params any) error
	// setProtocol báo phiên bản giao thức đã thương lượng (HTTP đặt thành header).
	setProtocol(v string)
	close() error
}

// Client là một phiên MCP tới một service.
type Client struct {
	t      transport
	nextID atomic.Int64
	// Stderr trả về phần cuối stderr của tiến trình stdio (đã cắt ngắn); rỗng với HTTP.
	stderr func() string
}

// Stderr trả về phần cuối stderr của upstream (chỉ stdio), tối đa vài KB. Có thể chứa dữ liệu nhạy cảm:
// người gọi phải qua detect.Redact trước khi hiển thị.
func (c *Client) Stderr() string {
	if c.stderr == nil {
		return ""
	}
	return c.stderr()
}

func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return c.t.call(ctx, c.nextID.Add(1), method, params)
}

// Initialize gửi initialize rồi thông báo initialized.
func (c *Client) Initialize(ctx context.Context) (ServerInfo, error) {
	res, err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": ClientName, "version": "check"},
	})
	if err != nil {
		return ServerInfo{}, err
	}
	var r struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return ServerInfo{}, fmt.Errorf("initialize: phản hồi không đọc được: %w", err)
	}
	if r.ProtocolVersion != "" {
		c.t.setProtocol(r.ProtocolVersion)
	}
	if err := c.t.notify(ctx, "notifications/initialized", nil); err != nil {
		return ServerInfo{}, err
	}
	return ServerInfo{Name: r.ServerInfo.Name, Version: r.ServerInfo.Version, ProtocolVersion: r.ProtocolVersion}, nil
}

// maxToolPages chặn vòng lặp vô hạn khi server trả nextCursor mãi.
const maxToolPages = 50

// ListTools gọi tools/list, theo nextCursor đến hết.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	var out []Tool
	var cursor string
	for range maxToolPages {
		var params any
		if cursor != "" {
			params = map[string]any{"cursor": cursor}
		}
		res, err := c.call(ctx, "tools/list", params)
		if err != nil {
			return nil, err
		}
		var r struct {
			Tools      []Tool `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if err := json.Unmarshal(res, &r); err != nil {
			return nil, fmt.Errorf("tools/list: phản hồi không đọc được: %w", err)
		}
		out = append(out, r.Tools...)
		if r.NextCursor == "" {
			return out, nil
		}
		cursor = r.NextCursor
	}
	return out, errors.New("tools/list: quá nhiều trang")
}

// CallTool gọi một tool. Lỗi JSON-RPC trả về *RPCError; isError của kết quả nằm trong CallResult.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (CallResult, error) {
	if args == nil {
		args = map[string]any{}
	}
	res, err := c.call(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return CallResult{}, err
	}
	var r struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return CallResult{}, fmt.Errorf("tools/call: phản hồi không đọc được: %w", err)
	}
	out := CallResult{IsError: r.IsError}
	for _, c := range r.Content {
		if c.Type == "text" || c.Type == "" {
			if out.Text != "" {
				out.Text += "\n"
			}
			out.Text += c.Text
		}
	}
	return out, nil
}

// Close đóng phiên: stdio dừng tiến trình (chờ có giới hạn rồi kill), HTTP kết thúc phiên nếu có.
func (c *Client) Close() error { return c.t.close() }

// rpcResponse là một thông điệp JSON-RPC nhận được.
type rpcResponse struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (r rpcResponse) isResponse() bool {
	return r.Method == "" && len(r.ID) > 0 && string(r.ID) != "null"
}

func (r rpcResponse) idIs(id int64) bool { return string(r.ID) == fmt.Sprint(id) }

func (r rpcResponse) outcome() (json.RawMessage, error) {
	if r.Error != nil {
		return nil, &RPCError{Code: r.Error.Code, Message: r.Error.Message}
	}
	return r.Result, nil
}

func request(id int64, method string, params any) ([]byte, error) {
	m := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		m["params"] = params
	}
	return json.Marshal(m)
}

func notification(method string, params any) ([]byte, error) {
	m := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		m["params"] = params
	}
	return json.Marshal(m)
}
