package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeProxy struct {
	mu        sync.Mutex
	readyN    int
	started   chan string
	release   chan struct{}
	cancelled chan string
}

func (p *fakeProxy) Ready(context.Context) { p.mu.Lock(); p.readyN++; p.mu.Unlock() }
func (p *fakeProxy) Tools() []json.RawMessage {
	return []json.RawMessage{json.RawMessage(`{"name":"cg_explore","description":"d","inputSchema":{"type":"object"},"x-extra":1}`),
		json.RawMessage(`{"name":"cg_slow","inputSchema":{"type":"object"}}`)}
}
func (p *fakeProxy) Instructions() string { return "Dịch vụ lỗi: demo." }
func (p *fakeProxy) Owns(n string) bool   { return strings.HasPrefix(n, "cg_") }
func (p *fakeProxy) Call(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	switch name {
	case "cg_slow":
		p.started <- name
		select {
		case <-p.release:
		case <-ctx.Done():
			p.cancelled <- name
			return nil, ctx.Err()
		}
		return json.RawMessage(`{"content":[{"type":"text","text":"slow xong"}]}`), nil
	case "cg_fail":
		return nil, io.ErrUnexpectedEOF
	}
	txt, _ := json.Marshal("raw " + string(args))
	return json.RawMessage(`{"content":[{"type":"text","text":` + string(txt) + `}],"structuredContent":{"k":1}}`), nil
}

func serve(t *testing.T, s *Server) (send func(string), recv func() map[string]any, closeIn func()) {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	go func() { _ = s.Serve(context.Background(), inR, outW); outW.Close() }()
	br := bufio.NewReader(outR)
	return func(l string) { _, _ = io.WriteString(inW, l+"\n") },
		func() map[string]any {
			t.Helper()
			ch := make(chan map[string]any, 1)
			go func() {
				line, err := br.ReadString('\n')
				if err != nil {
					ch <- nil
					return
				}
				var m map[string]any
				_ = json.Unmarshal([]byte(line), &m)
				ch <- m
			}()
			select {
			case m := <-ch:
				return m
			case <-time.After(5 * time.Second):
				t.Fatal("không nhận được phản hồi")
				return nil
			}
		}, func() { inW.Close() }
}

// Hai tools/call đồng thời, một cái chặn: cái kia vẫn trả; cái chặn trả khi được thả.
func TestConcurrentToolCalls(t *testing.T) {
	p := &fakeProxy{started: make(chan string, 1), release: make(chan struct{}), cancelled: make(chan string, 1)}
	s := New("bint", "t", "base")
	s.Proxy = p
	s.Register(Tool{Name: "bint_tool", InputSchema: map[string]any{"type": "object"}}, func(context.Context, json.RawMessage) (Result, error) {
		return Result{Text: "ok"}, nil
	})
	send, recv, closeIn := serve(t, s)
	send(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"cg_slow"}}`)
	<-p.started
	send(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"bint_tool","arguments":{}}}`)
	if m := recv(); m["id"].(float64) != 2 {
		t.Fatalf("phản hồi đầu phải của lời gọi 2 (lời gọi 1 đang chặn): %v", m)
	}
	send(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"cg_explore","arguments":{"a":1}}}`)
	m := recv()
	if m["id"].(float64) != 3 {
		t.Fatalf("lời gọi 3: %v", m)
	}
	res := m["result"].(map[string]any)
	if res["structuredContent"] == nil || !strings.Contains(res["content"].([]any)[0].(map[string]any)["text"].(string), `{"a":1}`) {
		t.Errorf("kết quả thô phải về nguyên vẹn: %v", res)
	}
	close(p.release)
	if m := recv(); m["id"].(float64) != 1 {
		t.Fatalf("lời gọi 1: %v", m)
	}
	closeIn()
}

// initialize chờ proxy sẵn sàng và thêm instructions; tools/list gộp; lỗi của proxy thành kết quả isError.
func TestProxyInitializeListAndError(t *testing.T) {
	p := &fakeProxy{started: make(chan string, 1), release: make(chan struct{}), cancelled: make(chan string, 1)}
	s := New("bint", "t", "Cơ bản.")
	s.Proxy = p
	RegisterTools(s, nil)
	send, recv, closeIn := serve(t, s)
	send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)
	init := recv()["result"].(map[string]any)
	if ins := init["instructions"].(string); !strings.Contains(ins, "Cơ bản.") || !strings.Contains(ins, "Dịch vụ lỗi: demo.") {
		t.Errorf("instructions: %q", ins)
	}
	if p.readyN != 1 {
		t.Errorf("Ready gọi %d lần", p.readyN)
	}
	send(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	tools := recv()["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 6+2 {
		t.Fatalf("%d tool, muốn 6 của Bint + 2 của proxy", len(tools))
	}
	if tools[6].(map[string]any)["x-extra"] == nil {
		t.Errorf("mô tả tool của proxy phải giữ nguyên trường lạ: %v", tools[6])
	}
	send(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"cg_fail"}}`)
	r := recv()["result"].(map[string]any)
	if r["isError"] != true {
		t.Errorf("lỗi của proxy phải thành isError: %v", r)
	}
	send(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"khong_co"}}`)
	if m := recv(); m["error"] == nil {
		t.Errorf("tool lạ: %v", m)
	}
	closeIn()
}

// notifications/cancelled kết thúc ctx của lời gọi tương ứng.
func TestCancelledNotificationCancelsCall(t *testing.T) {
	p := &fakeProxy{started: make(chan string, 1), release: make(chan struct{}), cancelled: make(chan string, 1)}
	s := New("bint", "t", "")
	s.Proxy = p
	send, recv, closeIn := serve(t, s)
	send(`{"jsonrpc":"2.0","id":"abc","method":"tools/call","params":{"name":"cg_slow"}}`)
	<-p.started
	send(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":"abc"}}`)
	select {
	case <-p.cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("ctx của lời gọi không bị huỷ")
	}
	_ = recv()
	closeIn()
}

// Không có Proxy: hành vi cũ.
func TestNoProxyUnchanged(t *testing.T) {
	s := New("bint", "t", "x")
	RegisterTools(s, nil)
	send, recv, closeIn := serve(t, s)
	send(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if n := len(recv()["result"].(map[string]any)["tools"].([]any)); n != 6 {
		t.Errorf("%d tool, muốn 6", n)
	}
	closeIn()
}
