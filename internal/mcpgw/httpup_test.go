package mcpgw

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeMCPHTTP là server MCP Streamable HTTP giả thay deepwiki/context7.
type fakeMCPHTTP struct {
	mu       sync.Mutex
	inits    int
	sessID   string
	calls    map[string]int      // tool → số request tools/call
	sessions map[string][]string // tool → Mcp-Session-Id của từng request
	fail     map[string][]int    // tool → danh sách trạng thái lỗi lần lượt (0 = đứt kết nối)
	sse      bool
}

func newFakeMCPHTTP() *fakeMCPHTTP {
	return &fakeMCPHTTP{sessID: "srv-1", calls: map[string]int{}, sessions: map[string][]string{}, fail: map[string][]int{}}
}

func (f *fakeMCPHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var m rpcMsg
	_ = json.Unmarshal(body, &m)
	f.mu.Lock()
	defer f.mu.Unlock()
	reply := func(result any) {
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result})
		if f.sse {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: message\ndata: " + string(b) + "\n\n"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	}
	if m.Method == "tools/call" {
		var p struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(m.Params, &p)
		f.calls[p.Name]++
		f.sessions[p.Name] = append(f.sessions[p.Name], r.Header.Get("Mcp-Session-Id"))
	}
	if m.Method != "initialize" && r.Header.Get("Mcp-Session-Id") != f.sessID {
		http.Error(w, "phiên không hợp lệ", http.StatusNotFound)
		return
	}
	switch m.Method {
	case "initialize":
		f.inits++
		w.Header().Set("Mcp-Session-Id", f.sessID)
		reply(map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "serverInfo": map[string]any{"name": "http-fake"}})
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	case "tools/list":
		reply(map[string]any{"tools": []map[string]any{{"name": "read_wiki_contents"}}})
	case "tools/call":
		var p struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(m.Params, &p)
		if q := f.fail[p.Name]; len(q) > 0 {
			f.fail[p.Name] = q[1:]
			if q[0] == 0 {
				if hj, ok := w.(http.Hijacker); ok {
					c, _, _ := hj.Hijack()
					c.Close()
				}
				return
			}
			http.Error(w, "lỗi giả", q[0])
			return
		}
		reply(map[string]any{"content": []map[string]any{{"type": "text", "text": "ok " + p.Name}}})
	default:
		http.Error(w, "?", http.StatusBadRequest)
	}
}

func httpHarness(t *testing.T) (*harness, *fakeMCPHTTP) {
	t.Helper()
	f := newFakeMCPHTTP()
	ts := httptest.NewServer(f)
	t.Cleanup(ts.Close)
	h := newHarness(t, nil)
	h.setReg("deepwiki", regHTTP(ts.URL))
	return h, f
}

// Một handshake chung cho mọi phiên; Mcp-Session-Id của server được gửi lại ở mọi lời gọi.
func TestHTTPUpstreamSharedHandshake(t *testing.T) {
	h, f := httpHarness(t)
	var cs []*client
	for i := 0; i < 3; i++ {
		cs = append(cs, h.open("deepwiki", nil))
	}
	for i, c := range cs {
		m := c.rpc(i+1, "tools/call", map[string]any{"name": "read_wiki_contents", "arguments": map[string]any{}})
		if len(m.Error) > 0 || !strings.Contains(string(m.Result), "ok read_wiki_contents") {
			t.Fatalf("client %d: %s %s", i, m.Result, m.Error)
		}
		if string(m.ID) != itoaI(i+1) {
			t.Errorf("id %s", m.ID)
		}
	}
	if f.initCount() != 1 {
		t.Errorf("%d initialize tới upstream, muốn 1 cho 3 phiên", f.inits)
	}
	for _, s := range f.sessionsOf("read_wiki_contents") {
		if s != "srv-1" {
			t.Errorf("lời gọi gửi Mcp-Session-Id %q, muốn srv-1", s)
		}
	}
	if m := cs[0].rpc(9, "tools/list", nil); !strings.Contains(string(m.Result), "read_wiki_contents") {
		t.Errorf("tools/list: %s", m.Result)
	}
	// Không sinh tiến trình cục bộ.
	for _, u := range h.d.Status().Upstreams {
		if u.PID != 0 {
			t.Errorf("upstream http có PID %d", u.PID)
		}
	}
}

func TestHTTPUpstreamSSEResponse(t *testing.T) {
	h, f := httpHarness(t)
	f.setSSE()
	c := h.open("deepwiki", nil)
	m := c.rpc(1, "tools/call", map[string]any{"name": "read_wiki_contents"})
	if len(m.Error) > 0 || !strings.Contains(string(m.Result), "ok read_wiki_contents") {
		t.Errorf("phản hồi SSE: %s %s", m.Result, m.Error)
	}
}

// Tool chỉ đọc gặp lỗi mạng, 5xx, 429 thì retry có backoff; tool không chỉ đọc không bao giờ retry.
func TestHTTPUpstreamRetry(t *testing.T) {
	h, f := httpHarness(t)
	c := h.open("deepwiki", nil)

	f.setFail("read_wiki_contents", []int{503, 429, 0})
	m := c.rpc(1, "tools/call", map[string]any{"name": "read_wiki_contents"})
	if len(m.Error) > 0 {
		t.Fatalf("tool chỉ đọc phải thành công sau 503/429/đứt kết nối: %s", m.Error)
	}
	if f.count("read_wiki_contents") != 4 {
		t.Errorf("%d request, muốn 4 (3 lỗi + 1 thành công)", f.count("read_wiki_contents"))
	}

	// Hết lượt retry: lỗi (1 + Retries request).
	f.setFail("read_wiki_contents", []int{500, 500, 500, 500, 500, 500})
	before := f.count("read_wiki_contents")
	m = c.rpc(2, "tools/call", map[string]any{"name": "read_wiki_contents"})
	if len(m.Error) == 0 {
		t.Error("lỗi kéo dài phải trả lỗi")
	}
	if n := f.count("read_wiki_contents") - before; n != 1+DefaultRetries {
		t.Errorf("%d request, muốn %d", n, 1+DefaultRetries)
	}
	f.setFail("read_wiki_contents", nil)

	// 4xx (khác 429) không retry.
	f.setFail("read_wiki_contents", []int{400})
	before = f.count("read_wiki_contents")
	m = c.rpc(3, "tools/call", map[string]any{"name": "read_wiki_contents"})
	if len(m.Error) == 0 || f.count("read_wiki_contents")-before != 1 {
		t.Errorf("4xx: lỗi=%s request=%d, muốn lỗi sau đúng 1 request", m.Error, f.count("read_wiki_contents")-before)
	}

	// Tool không chỉ đọc (không có trong ReadOnly của danh mục): đúng một request, lỗi không rõ kết quả.
	for _, st := range []int{503, 429, 0} {
		f.setFail("write_thing", []int{st})
		before = f.count("write_thing")
		m = c.rpc(10+st, "tools/call", map[string]any{"name": "write_thing"})
		if n := f.count("write_thing") - before; n != 1 {
			t.Errorf("trạng thái %d: lệnh ghi tới upstream %d lần, muốn đúng 1", st, n)
		}
		if rpcCode(m) != CodeUnknownOutcome {
			t.Errorf("trạng thái %d: %s, muốn CodeUnknownOutcome", st, m.Error)
		}
	}
}

// Server báo phiên hết hạn (404): tool chỉ đọc handshake lại rồi thành công; tool ghi không gửi lại.
func TestHTTPUpstreamSessionExpired(t *testing.T) {
	h, f := httpHarness(t)
	c := h.open("deepwiki", nil)
	f.mu.Lock()
	f.sessID = "srv-2" // phiên cũ srv-1 không còn hợp lệ; initialize kế tiếp cấp srv-2
	f.mu.Unlock()
	m := c.rpc(1, "tools/call", map[string]any{"name": "read_wiki_contents"})
	if len(m.Error) > 0 {
		t.Fatalf("handshake lại sau 404: %s", m.Error)
	}
	if f.initCount() != 2 {
		t.Errorf("%d initialize, muốn 2", f.inits)
	}
	if got := f.sessionsOf("read_wiki_contents"); got[len(got)-1] != "srv-2" {
		t.Errorf("không gửi Mcp-Session-Id mới: %v", got)
	}
	f.mu.Lock()
	f.sessID = "srv-3"
	f.mu.Unlock()
	before := f.count("write_thing")
	m = c.rpc(2, "tools/call", map[string]any{"name": "write_thing"})
	if len(m.Error) == 0 || f.count("write_thing")-before != 1 {
		t.Errorf("lệnh ghi gặp 404: lỗi=%s request=%d, muốn lỗi sau đúng 1 request", m.Error, f.count("write_thing")-before)
	}
	if m = c.rpc(3, "tools/call", map[string]any{"name": "write_thing"}); len(m.Error) > 0 {
		t.Errorf("lần sau handshake mới phải chạy: %s", m.Error)
	}
}

func (f *fakeMCPHTTP) count(tool string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[tool]
}

func (f *fakeMCPHTTP) initCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.inits
}

func (f *fakeMCPHTTP) sessionsOf(tool string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.sessions[tool]...)
}

func (f *fakeMCPHTTP) setFail(tool string, q []int) {
	f.mu.Lock()
	f.fail[tool] = q
	f.mu.Unlock()
}

func (f *fakeMCPHTTP) setSSE() {
	f.mu.Lock()
	f.sse = true
	f.mu.Unlock()
}
