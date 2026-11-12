package probe_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"bint/internal/mcpcat/probe"
	"bint/internal/mcpcat/probe/probetest"
)

func TestMain(m *testing.M) {
	probetest.MaybeRun()
	os.Exit(m.Run())
}

func fake(t *testing.T, ctx context.Context, mode, tools string) *probe.Client {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, exe)
	cmd.Env = append(os.Environ(), probetest.EnvMode+"="+mode, probetest.EnvTools+"="+tools)
	c, err := probe.Stdio(cmd)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestStdioListAndCall(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := fake(t, ctx, "ok", "codegraph_explore,codegraph_status")
	info, err := c.Initialize(ctx)
	if err != nil || info.Name != "fake" || info.ProtocolVersion == "" {
		t.Fatalf("initialize: %+v %v", info, err)
	}
	tools, err := c.ListTools(ctx)
	if err != nil || len(tools) != 2 || tools[0].Name != "codegraph_explore" {
		t.Fatalf("tools: %+v %v", tools, err)
	}
	res, err := c.CallTool(ctx, "codegraph_status", nil)
	if err != nil || res.IsError || !strings.HasPrefix(res.Text, "ok ") {
		t.Fatalf("call: %+v %v", res, err)
	}
}

func TestStdioNoTools(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := fake(t, ctx, "ok", "")
	if _, err := c.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if tools, err := c.ListTools(ctx); err != nil || len(tools) != 0 {
		t.Fatalf("tools: %+v %v", tools, err)
	}
}

func TestStdioToolErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := fake(t, ctx, "status_error", "codegraph_status")
	if _, err := c.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if res, err := c.CallTool(ctx, "codegraph_status", nil); err != nil || !res.IsError || !strings.Contains(res.Text, "disabled") {
		t.Fatalf("isError: %+v %v", res, err)
	}
	c2 := fake(t, ctx, "status_rpcerror", "codegraph_status")
	if _, err := c2.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	_, err := c2.CallTool(ctx, "codegraph_status", nil)
	var re *probe.RPCError
	if !errors.As(err, &re) || re.Code != -32000 {
		t.Fatalf("JSON-RPC lỗi: %v", err)
	}
}

func TestStdioTimeoutAndClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	c := fake(t, context.Background(), "hang", "")
	start := time.Now()
	_, err := c.Initialize(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("muốn DeadlineExceeded, nhận %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("timeout quá chậm")
	}
	start = time.Now()
	if err := c.Close(); err != nil {
		t.Fatalf("Close tiến trình treo: %v", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("Close không kill kịp")
	}
}

func TestStdioProcessExit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := fake(t, ctx, "exit", "codegraph_status")
	if _, err := c.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	_, err := c.CallTool(ctx, "codegraph_status", nil)
	if err == nil || !strings.Contains(err.Error(), "thoát") {
		t.Fatalf("tiến trình thoát giữa chừng: %v", err)
	}
}

func TestStdioStartFailure(t *testing.T) {
	if _, err := probe.Stdio(exec.Command("/không/có/lệnh/này")); err == nil {
		t.Fatal("phải lỗi khi lệnh không tồn tại")
	}
}

// ---- HTTP ----

type httpFake struct {
	sse        bool
	sawSession []string
	sawAccept  string
	deleted    bool
	tools      []string
}

func (f *httpFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		f.deleted = r.Header.Get("Mcp-Session-Id") == "sess-1"
		w.WriteHeader(http.StatusNoContent)
		return
	}
	f.sawAccept = r.Header.Get("Accept")
	f.sawSession = append(f.sawSession, r.Header.Get("Mcp-Session-Id"))
	var m struct {
		ID     *int64 `json:"id"`
		Method string `json:"method"`
	}
	buf := make([]byte, 1<<16)
	n, _ := r.Body.Read(buf)
	_ = jsonUnmarshal(buf[:n], &m)
	if m.ID == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	var result string
	switch m.Method {
	case "initialize":
		w.Header().Set("Mcp-Session-Id", "sess-1")
		result = `{"protocolVersion":"2025-06-18","serverInfo":{"name":"httpfake","version":"1"}}`
	case "tools/list":
		var ts []string
		for _, t := range f.tools {
			ts = append(ts, fmt.Sprintf(`{"name":%q}`, t))
		}
		result = `{"tools":[` + strings.Join(ts, ",") + `]}`
	default:
		result = `{"content":[{"type":"text","text":"hi"}]}`
	}
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":%s}`, *m.ID, result)
	if f.sse {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: ping\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/message\"}\n\nevent: message\ndata: %s\n\n", body)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, body)
}

func TestHTTPJSONAndSSE(t *testing.T) {
	for _, sse := range []bool{false, true} {
		t.Run(fmt.Sprintf("sse=%v", sse), func(t *testing.T) {
			f := &httpFake{sse: sse, tools: []string{"read_wiki_structure", "ask_question"}}
			srv := httptest.NewServer(f)
			defer srv.Close()
			c := probe.HTTP(srv.URL, srv.Client(), nil)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			info, err := c.Initialize(ctx)
			if err != nil || info.Name != "httpfake" {
				t.Fatalf("initialize: %+v %v", info, err)
			}
			tools, err := c.ListTools(ctx)
			if err != nil || len(tools) != 2 || tools[1].Name != "ask_question" {
				t.Fatalf("tools: %+v %v", tools, err)
			}
			res, err := c.CallTool(ctx, "ask_question", map[string]any{"q": 1})
			if err != nil || res.Text != "hi" {
				t.Fatalf("call: %+v %v", res, err)
			}
			if !strings.Contains(f.sawAccept, "text/event-stream") || !strings.Contains(f.sawAccept, "application/json") {
				t.Errorf("Accept = %q", f.sawAccept)
			}
			// Yêu cầu đầu chưa có phiên; các yêu cầu sau phải mang Mcp-Session-Id.
			if f.sawSession[0] != "" {
				t.Errorf("initialize không được có session: %v", f.sawSession)
			}
			for _, s := range f.sawSession[1:] {
				if s != "sess-1" {
					t.Errorf("thiếu Mcp-Session-Id: %v", f.sawSession)
				}
			}
			c.Close()
			if !f.deleted {
				t.Error("Close phải DELETE phiên")
			}
		})
	}
}

func TestHTTPErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()
	c := probe.HTTP(srv.URL, srv.Client(), nil)
	_, err := c.Initialize(context.Background())
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("HTTP 403: %v", err)
	}
	// Lỗi JSON-RPC trong SSE.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"jsonrpc\":\"2.0\",\"id\":1,\"error\":{\"code\":-32602,\"message\":\"sai\"}}\n\n")
	}))
	defer srv2.Close()
	_, err = probe.HTTP(srv2.URL, srv2.Client(), nil).Initialize(context.Background())
	var re *probe.RPCError
	if !errors.As(err, &re) || re.Code != -32602 {
		t.Fatalf("lỗi RPC qua SSE: %v", err)
	}
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
