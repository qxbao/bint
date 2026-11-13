package mcpgw

import (
	"bufio"
	"bytes"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// terminalGateWriter exposes a complete JSON-RPC frame to the test before its
// Write returns, matching a peer that has received the frame while the sender
// is still unwinding from Flush.
type terminalGateWriter struct {
	frames        chan []byte
	continueFrame chan struct{}
	release       chan struct{}
	releaseOnce   sync.Once
	callID        string
}

func (w *terminalGateWriter) Write(p []byte) (int, error) {
	frame := append([]byte(nil), p...)
	w.frames <- frame
	var msg rpcMsg
	if json.Unmarshal(bytes.TrimSpace(frame), &msg) == nil && string(msg.ID) == `"`+w.callID+`"` {
		<-w.release
	} else {
		<-w.continueFrame
	}
	return len(p), nil
}

func TestFakeCallActiveLifecycle(t *testing.T) {
	tests := []struct {
		name            string
		args            string
		params          string
		terminalErrCode int
	}{
		{name: "success"},
		{name: "rpc error", args: `{"rpc_error":true}`, terminalErrCode: -32000},
		{name: "cancel", args: `{"sleep_ms":1}`, terminalErrCode: -32800},
		{name: "progress remains active", args: `{"progress":true}`, params: `{"_meta":{"progressToken":"p"}}`},
		{name: "server request remains active", args: `{"roots":true}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			writer := &terminalGateWriter{frames: make(chan []byte, 4), continueFrame: make(chan struct{}, 2), release: make(chan struct{}), callID: "call-1"}
			defer func() {
				writer.releaseOnce.Do(func() { close(writer.release) })
				close(writer.continueFrame)
			}()
			f := &fake{opt: map[string]string{}, out: bufio.NewWriter(writer), cancels: map[string]chan struct{}{}, replies: map[string]chan json.RawMessage{}}
			args := tc.args
			if args == "" {
				args = `{}`
			}
			if tc.name == "cancel" {
				f.cancelSignals = make(chan chan struct{}, 1)
			}
			params := json.RawMessage(tc.params)
			if len(params) == 0 {
				params = json.RawMessage(`{}`)
			}
			done := make(chan struct{})
			go func() {
				f.call(rpcMsg{ID: json.RawMessage(`"call-1"`), Params: params}, "call-1", "tool", json.RawMessage(args))
				close(done)
			}()
			if tc.name == "cancel" {
				select {
				case cancel := <-f.cancelSignals:
					close(cancel)
				case <-time.After(5 * time.Second):
					t.Fatal("call did not register cancellation channel")
				}
			}

			for {
				select {
				case frame := <-writer.frames:
					var msg rpcMsg
					if err := json.Unmarshal(bytes.TrimSpace(frame), &msg); err != nil {
						t.Fatalf("invalid emitted frame %q: %v", frame, err)
					}
					if msg.Method == "notifications/progress" {
						if got := f.cur.Load(); got != 1 {
							t.Fatalf("progress emitted with active=%d, want 1", got)
						}
						writer.continueFrame <- struct{}{}
						continue
					}
					if msg.Method == "roots/list" {
						if got := f.cur.Load(); got != 1 {
							t.Fatalf("server request emitted with active=%d, want 1", got)
						}
						f.handle(rpcMsg{ID: msg.ID, Result: json.RawMessage(`{"roots":[]}`)})
						writer.continueFrame <- struct{}{}
						continue
					}
					if string(msg.ID) == `"call-1"` {
						if tc.terminalErrCode != 0 {
							var rpcError struct {
								Code int `json:"code"`
							}
							if err := json.Unmarshal(msg.Error, &rpcError); err != nil || rpcError.Code != tc.terminalErrCode {
								t.Errorf("%s terminal response error=%s, want code %d", tc.name, msg.Error, tc.terminalErrCode)
							}
							if len(msg.Result) != 0 {
								t.Errorf("%s terminal response unexpectedly has result %s", tc.name, msg.Result)
							}
						} else if len(msg.Result) == 0 || len(msg.Error) != 0 {
							t.Errorf("%s terminal response result=%s error=%s, want result and no error", tc.name, msg.Result, msg.Error)
						}
						if got := f.cur.Load(); got != 0 {
							t.Errorf("terminal response reached peer while active=%d, want 0", got)
						}
						select {
						case <-done:
							t.Fatal("call returned before terminal write was released")
						default:
						}
						goto terminalSeen
					}
				case <-time.After(5 * time.Second):
					t.Fatal("timed out waiting for terminal response")
				}
			}
		terminalSeen:
			writer.releaseOnce.Do(func() { close(writer.release) })
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("call did not unwind after terminal write release")
			}
			if got := f.cur.Load(); got != 0 {
				t.Errorf("active after call unwind=%d, want 0", got)
			}
		})
	}
}
