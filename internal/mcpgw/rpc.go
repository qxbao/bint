package mcpgw

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Mã lỗi JSON-RPC riêng của gateway (vùng -320xx dành cho ứng dụng).
const (
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternal       = -32603

	// CodeBusy: nhóm độc quyền đang do phiên khác giữ quá thời gian chờ.
	CodeBusy = -32010
	// CodeRestartLimit: upstream đã chết quá max_restarts trong cửa sổ; chờ `bint mcp gateway restart <service>`.
	CodeRestartLimit = -32011
	// CodeUnknownOutcome: lệnh không chỉ đọc đã gửi tới upstream nhưng không rõ kết quả (timeout, upstream chết);
	// gateway không bao giờ tự gọi lại.
	CodeUnknownOutcome = -32012
	// CodeUpstream: upstream lỗi hoặc không khởi động được (lệnh chưa chắc đã chạy).
	CodeUpstream = -32013
	// CodeTimeout: lời gọi chỉ đọc quá thời gian chờ.
	CodeTimeout = -32014
	// CodeNotGranted: role của phiên không được cấp service hoặc tool này (mcp.grants, bước chỉ đọc, run_id không có bản ghi).
	CodeNotGranted = -32015
)

// rpcMsg là phong bì JSON-RPC; id, params, result, error giữ nguyên byte để chuyển tiếp không mất trường lạ.
type rpcMsg struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

func (m rpcMsg) hasID() bool { return len(m.ID) > 0 && string(m.ID) != "null" }

// isRequest: có method và id. isNotification: có method, không id. isResponse: không method, có result hoặc error.
func (m rpcMsg) isRequest() bool      { return m.Method != "" && m.hasID() }
func (m rpcMsg) isNotification() bool { return m.Method != "" && !m.hasID() }
func (m rpcMsg) isResponse() bool {
	return m.Method == "" && m.hasID() && (len(m.Result) > 0 || len(m.Error) > 0)
}

// rpcError là phần error của JSON-RPC.
type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("rpc %d: %s", e.Code, e.Message) }

func marshalRaw(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return b
}

// errorFrame dựng phản hồi lỗi cho id (id rỗng → null).
func errorFrame(id json.RawMessage, code int, msg string) []byte {
	if len(id) == 0 {
		id = json.RawMessage(`null`)
	}
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "error": rpcError{Code: code, Message: msg}})
	return b
}

// resultFrame dựng phản hồi thành công cho id.
func resultFrame(id json.RawMessage, result json.RawMessage) []byte {
	if len(result) == 0 {
		result = json.RawMessage(`{}`)
	}
	b, _ := json.Marshal(rpcMsg{JSONRPC: "2.0", ID: id, Result: result})
	return b
}

// requestFrame dựng request/notification (id rỗng → notification).
func requestFrame(id json.RawMessage, method string, params json.RawMessage) []byte {
	b, _ := json.Marshal(rpcMsg{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	return b
}

// errFrameTooLarge: một dòng vượt giới hạn frame.
var errFrameTooLarge = errors.New("frame vượt giới hạn kích thước")

// readFrame đọc một dòng (không gồm '\n') dài tối đa max byte. Dòng quá dài trả errFrameTooLarge (đã bỏ phần đọc dở:
// phía gọi phải coi luồng là hỏng).
func readFrame(br *bufio.Reader, max int) ([]byte, error) {
	var buf []byte
	for {
		chunk, err := br.ReadSlice('\n')
		if len(buf)+len(chunk) > max+1 { // +1 cho '\n'
			return nil, errFrameTooLarge
		}
		buf = append(buf, chunk...)
		switch {
		case err == nil:
			return bytes.TrimRight(buf, "\r\n"), nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF) && len(bytes.TrimSpace(buf)) > 0:
			return bytes.TrimRight(buf, "\r\n"), nil
		default:
			return nil, err
		}
	}
}
