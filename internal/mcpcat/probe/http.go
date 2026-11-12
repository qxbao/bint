package probe

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
)

// maxHTTPBody chặn phản hồi quá lớn.
const maxHTTPBody = 8 << 20

type httpT struct {
	url    string
	hc     *http.Client
	header http.Header

	mu       sync.Mutex
	session  string
	protocol string
}

// HTTP trả client Streamable HTTP tới url. hc nil = http.DefaultClient; header là header cố định thêm vào mọi
// yêu cầu (vd. Authorization). Phản hồi JSON và SSE đều được nhận; Mcp-Session-Id được giữ lại.
func HTTP(url string, hc *http.Client, header http.Header) *Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &Client{t: &httpT{url: url, hc: hc, header: header}}
}

func (t *httpT) setProtocol(v string) {
	t.mu.Lock()
	t.protocol = v
	t.mu.Unlock()
}

func (t *httpT) newRequest(ctx context.Context, method string, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, t.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for k, vs := range t.header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	t.mu.Lock()
	if t.session != "" {
		req.Header.Set("Mcp-Session-Id", t.session)
	}
	if t.protocol != "" {
		req.Header.Set("MCP-Protocol-Version", t.protocol)
	}
	t.mu.Unlock()
	return req, nil
}

func (t *httpT) post(ctx context.Context, body []byte) (*http.Response, error) {
	req, err := t.newRequest(ctx, http.MethodPost, body)
	if err != nil {
		return nil, err
	}
	resp, err := t.hc.Do(req)
	if err != nil {
		return nil, err
	}
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		t.mu.Lock()
		t.session = sid
		t.mu.Unlock()
	}
	return resp, nil
}

func (t *httpT) call(ctx context.Context, id int64, method string, params any) (json.RawMessage, error) {
	body, err := request(id, method, params)
	if err != nil {
		return nil, err
	}
	resp, err := t.post(ctx, body)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("%s: HTTP %d %s", method, resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	switch mt {
	case "text/event-stream":
		m, err := readSSE(resp.Body, id)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", method, err)
		}
		return m.outcome()
	default: // application/json (hoặc thiếu Content-Type)
		data, err := io.ReadAll(io.LimitReader(resp.Body, maxHTTPBody))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", method, err)
		}
		data = bytes.TrimSpace(data)
		if len(data) > 0 && data[0] == '[' { // lô: lấy phần tử có id của ta
			var batch []rpcResponse
			if err := json.Unmarshal(data, &batch); err != nil {
				return nil, fmt.Errorf("%s: phản hồi không đọc được: %w", method, err)
			}
			for _, m := range batch {
				if m.isResponse() && m.idIs(id) {
					return m.outcome()
				}
			}
			return nil, fmt.Errorf("%s: phản hồi không có id %d", method, id)
		}
		var m rpcResponse
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("%s: phản hồi không đọc được: %w", method, err)
		}
		if !m.isResponse() || !m.idIs(id) {
			return nil, fmt.Errorf("%s: phản hồi không khớp id %d", method, id)
		}
		return m.outcome()
	}
}

// readSSE đọc luồng SSE tới khi gặp phản hồi JSON-RPC có id mong muốn.
func readSSE(r io.Reader, id int64) (rpcResponse, error) {
	br := bufio.NewReaderSize(io.LimitReader(r, maxHTTPBody), 64*1024)
	var data []string
	flush := func() (rpcResponse, bool) {
		if len(data) == 0 {
			return rpcResponse{}, false
		}
		payload := strings.Join(data, "\n")
		data = nil
		var m rpcResponse
		if json.Unmarshal([]byte(payload), &m) != nil {
			return rpcResponse{}, false
		}
		return m, m.isResponse() && m.idIs(id)
	}
	for {
		line, err := br.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "":
			if m, ok := flush(); ok {
				return m, nil
			}
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
		if err != nil {
			if m, ok := flush(); ok {
				return m, nil
			}
			if errors.Is(err, io.EOF) {
				return rpcResponse{}, fmt.Errorf("luồng SSE kết thúc không có phản hồi cho id %d", id)
			}
			return rpcResponse{}, err
		}
	}
}

func (t *httpT) notify(ctx context.Context, method string, params any) error {
	body, err := notification(method, params)
	if err != nil {
		return err
	}
	resp, err := t.post(ctx, body)
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s: HTTP %d", method, resp.StatusCode)
	}
	return nil
}

// close kết thúc phiên nếu server đã cấp Mcp-Session-Id (DELETE; lỗi bị bỏ qua vì server có thể không hỗ trợ).
func (t *httpT) close() error {
	t.mu.Lock()
	sid := t.session
	t.mu.Unlock()
	if sid == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), closeWait)
	defer cancel()
	req, err := t.newRequest(ctx, http.MethodDelete, nil)
	if err != nil {
		return nil
	}
	if resp, err := t.hc.Do(req); err == nil {
		_ = resp.Body.Close()
	}
	return nil
}
