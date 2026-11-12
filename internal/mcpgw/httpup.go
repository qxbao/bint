package mcpgw

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// httpUp là upstream MCP qua Streamable HTTP (deepwiki, context7): một handshake chung cho mọi phiên, gửi lại
// Mcp-Session-Id server cấp, retry có backoff CHỈ cho tool chỉ đọc.
type httpUp struct {
	url      string
	client   *http.Client
	version  string
	retries  int
	backoff  time.Duration
	maxFrame int

	hmu      sync.Mutex // tuần tự hoá handshake
	mu       sync.Mutex
	ready    bool
	sessID   string
	protocol string
	gen      int
	seq      uint64
	initRes  json.RawMessage
	toolsRes json.RawMessage
	progress progressTable
}

func newHTTPUp(url, version string, o Options) *httpUp {
	return &httpUp{url: url, version: version, client: &http.Client{}, retries: o.Retries, backoff: o.Backoff,
		maxFrame: o.MaxFrame, gen: 1}
}

func (h *httpUp) alive() bool                    { return true }
func (h *httpUp) pid() int                       { return 0 }
func (h *httpUp) stop()                          {}
func (h *httpUp) generation() int                { h.mu.Lock(); defer h.mu.Unlock(); return h.gen }
func (h *httpUp) cancel(string, json.RawMessage) {}
func (h *httpUp) hello() (json.RawMessage, json.RawMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.initRes, h.toolsRes
}
func (h *httpUp) routeProgress(token string, fn func(json.RawMessage)) func() {
	return h.progress.add(token, fn)
}

// reset buộc handshake lại ở lần gọi sau (server báo phiên hết hạn); generation tăng 1.
func (h *httpUp) reset(sessID string) {
	h.mu.Lock()
	if h.ready && h.sessID == sessID {
		h.ready = false
		h.gen++
	}
	h.mu.Unlock()
}

type httpReply struct {
	status int
	header http.Header
	msg    rpcMsg
}

// errHTTPStatus là lỗi trạng thái HTTP của upstream.
type errHTTPStatus struct{ code int }

func (e errHTTPStatus) Error() string { return fmt.Sprintf("upstream HTTP %d", e.code) }

// post gửi một frame; với request (có id) đọc phản hồi JSON hoặc SSE tới khi gặp phản hồi có id đó.
func (h *httpUp) post(ctx context.Context, frame []byte, wantID string, sessID, protocol string) (httpReply, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url, bytes.NewReader(frame))
	if err != nil {
		return httpReply{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sessID != "" {
		req.Header.Set("Mcp-Session-Id", sessID)
	}
	if protocol != "" {
		req.Header.Set("MCP-Protocol-Version", protocol)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return httpReply{}, err
	}
	defer resp.Body.Close()
	rep := httpReply{status: resp.StatusCode, header: resp.Header}
	if resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return rep, errHTTPStatus{resp.StatusCode}
	}
	if wantID == "" {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return rep, nil
	}
	ct := resp.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "text/event-stream") {
		m, err := h.readSSE(resp.Body, wantID)
		rep.msg = m
		return rep, err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(h.maxFrame)+1))
	if err != nil {
		return rep, err
	}
	if len(body) > h.maxFrame {
		return rep, errFrameTooLarge
	}
	if err := json.Unmarshal(body, &rep.msg); err != nil {
		return rep, fmt.Errorf("phản hồi upstream không phải JSON-RPC: %w", err)
	}
	return rep, nil
}

// readSSE đọc luồng SSE; chuyển notifications/progress cho nơi đã đăng ký và trả phản hồi có id wantID.
func (h *httpUp) readSSE(r io.Reader, wantID string) (rpcMsg, error) {
	br := bufio.NewReaderSize(r, 64*1024)
	var data bytes.Buffer
	flush := func() (rpcMsg, bool) {
		if data.Len() == 0 {
			return rpcMsg{}, false
		}
		var m rpcMsg
		err := json.Unmarshal(data.Bytes(), &m)
		data.Reset()
		if err != nil {
			return rpcMsg{}, false
		}
		if m.isNotification() && m.Method == "notifications/progress" {
			h.progress.deliver(m.Params)
			return rpcMsg{}, false
		}
		var id string
		if m.isResponse() && json.Unmarshal(m.ID, &id) == nil && id == wantID {
			return m, true
		}
		return rpcMsg{}, false
	}
	for {
		line, err := readFrame(br, h.maxFrame)
		if err != nil {
			if m, ok := flush(); ok {
				return m, nil
			}
			if errors.Is(err, io.EOF) {
				return rpcMsg{}, errors.New("luồng SSE của upstream kết thúc không có phản hồi")
			}
			return rpcMsg{}, err
		}
		s := string(line)
		switch {
		case s == "":
			if m, ok := flush(); ok {
				return m, nil
			}
		case strings.HasPrefix(s, "data:"):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(s, "data:"), " "))
		}
	}
}

func (h *httpUp) nextID(sess string) (string, int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	return fmt.Sprintf("gw_%d_%d_%s", h.gen, h.seq, sess), h.gen
}

// ensure làm handshake nếu chưa có (initialize, initialized, tools/list); mọi phiên dùng chung kết quả.
func (h *httpUp) ensure(ctx context.Context) error {
	h.hmu.Lock()
	defer h.hmu.Unlock()
	h.mu.Lock()
	if h.ready {
		h.mu.Unlock()
		return nil
	}
	h.mu.Unlock()
	id, _ := h.nextID("gateway")
	rep, err := h.post(ctx, requestFrame(marshalRaw(id), "initialize", handshakeParams(h.version)), id, "", "")
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	if len(rep.msg.Error) > 0 {
		return fmt.Errorf("initialize: %s", string(rep.msg.Error))
	}
	sessID := rep.header.Get("Mcp-Session-Id")
	var ir struct {
		Protocol string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(rep.msg.Result, &ir)
	if _, err := h.post(ctx, requestFrame(nil, "notifications/initialized", nil), "", sessID, ir.Protocol); err != nil {
		return fmt.Errorf("initialized: %w", err)
	}
	var pages []json.RawMessage
	cursor := ""
	for i := 0; i < 50; i++ {
		var params json.RawMessage
		if cursor != "" {
			params = marshalRaw(map[string]string{"cursor": cursor})
		}
		tid, _ := h.nextID("gateway")
		r, err := h.post(ctx, requestFrame(marshalRaw(tid), "tools/list", params), tid, sessID, ir.Protocol)
		if err != nil {
			return fmt.Errorf("tools/list: %w", err)
		}
		if len(r.msg.Error) > 0 {
			return fmt.Errorf("tools/list: %s", string(r.msg.Error))
		}
		pages = append(pages, r.msg.Result)
		if cursor = nextCursor(r.msg.Result); cursor == "" {
			break
		}
	}
	h.mu.Lock()
	h.ready, h.sessID, h.protocol = true, sessID, ir.Protocol
	h.initRes, h.toolsRes = rep.msg.Result, mergeTools(pages)
	h.mu.Unlock()
	return nil
}

func (h *httpUp) state() (sessID, protocol string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sessID, h.protocol
}

// retryable: lỗi mạng, 5xx hoặc 429.
func retryable(err error) bool {
	var hs errHTTPStatus
	if errors.As(err, &hs) {
		return hs.code >= 500 || hs.code == http.StatusTooManyRequests
	}
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, errFrameTooLarge)
}

func (h *httpUp) call(ctx context.Context, sess, method string, params json.RawMessage, readOnly bool, onID func(string)) (upResult, error) {
	attempts := 1
	if readOnly {
		attempts += h.retries
	}
	var lastErr error
	sent := false
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			d := h.backoff << (attempt - 1)
			select {
			case <-time.After(d):
			case <-ctx.Done():
				return upResult{}, h.mapErr(ctx, ctx.Err(), readOnly, sent)
			}
		}
		if err := h.ensure(ctx); err != nil {
			lastErr = err
			if readOnly && retryable(err) {
				continue
			}
			return upResult{}, &rpcError{Code: CodeUpstream, Message: "upstream HTTP không sẵn sàng: " + err.Error()}
		}
		sessID, protocol := h.state()
		id, _ := h.nextID(sess)
		if onID != nil {
			onID(id)
		}
		sent = true
		rep, err := h.post(ctx, requestFrame(marshalRaw(id), method, params), id, sessID, protocol)
		if err == nil {
			return upResult{result: rep.msg.Result, rpcErr: rep.msg.Error}, nil
		}
		lastErr = err
		var hs errHTTPStatus
		if errors.As(err, &hs) && hs.code == http.StatusNotFound && sessID != "" {
			h.reset(sessID) // phiên upstream hết hạn: handshake lại ở lần sau
			if !readOnly {
				return upResult{}, &rpcError{Code: CodeUpstream, Message: "phiên upstream hết hạn (HTTP 404); lệnh chưa được thực hiện, gọi lại để handshake mới"}
			}
			continue
		}
		if readOnly && retryable(err) {
			continue
		}
		break
	}
	return upResult{}, h.mapErr(ctx, lastErr, readOnly, sent)
}

func (h *httpUp) mapErr(ctx context.Context, err error, readOnly, sent bool) error {
	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		return ctx.Err()
	}
	var hs errHTTPStatus
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		if sent && !readOnly {
			return &rpcError{Code: CodeUnknownOutcome, Message: "hết thời gian chờ lệnh ghi; không rõ lệnh đã chạy chưa, gateway không tự gọi lại"}
		}
		return &rpcError{Code: CodeTimeout, Message: "upstream không trả lời trong thời gian cho phép"}
	case errors.As(err, &hs) && hs.code >= 400 && hs.code < 500 && hs.code != http.StatusTooManyRequests:
		return &rpcError{Code: CodeUpstream, Message: err.Error()}
	case !readOnly && sent:
		return &rpcError{Code: CodeUnknownOutcome, Message: "lệnh ghi tới upstream HTTP lỗi (" + err.Error() + "); không rõ lệnh đã chạy chưa, gateway không tự gọi lại"}
	default:
		return &rpcError{Code: CodeUpstream, Message: "upstream HTTP: " + err.Error()}
	}
}
