package probe

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

// closeWait là thời gian chờ tiến trình tự thoát sau khi đóng stdin, trước khi kill.
const closeWait = 2 * time.Second

// stderrCap là số byte cuối của stderr được giữ lại.
const stderrCap = 4096

type stdioT struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser

	wmu sync.Mutex

	mu      sync.Mutex
	pending map[int64]chan rpcResponse
	readErr error
	done    chan struct{} // đóng khi luồng đọc kết thúc

	waited chan struct{} // đóng khi Wait trả về
	errBuf *tailBuffer
}

// Stdio khởi động cmd (stdin/stdout do client nắm) và trả client. cmd chưa được Start; stderr bị client lấy.
// Tiến trình sống tới khi Close (hoặc ctx của cmd — dùng exec.CommandContext — kết thúc).
func Stdio(cmd *exec.Cmd) (*Client, error) {
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	eb := &tailBuffer{max: stderrCap}
	cmd.Stderr = eb
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	t := &stdioT{cmd: cmd, stdin: in, pending: map[int64]chan rpcResponse{}, done: make(chan struct{}),
		waited: make(chan struct{}), errBuf: eb}
	go t.readLoop(out)
	go func() { _ = cmd.Wait(); close(t.waited) }()
	return &Client{t: t, stderr: eb.String}, nil
}

func (t *stdioT) readLoop(r io.Reader) {
	br := bufio.NewReaderSize(r, 64*1024)
	var rerr error
	for {
		line, err := br.ReadBytes('\n')
		if s := bytes.TrimSpace(line); len(s) > 0 {
			t.dispatch(s)
		}
		if err != nil {
			rerr = err
			break
		}
	}
	t.mu.Lock()
	if errors.Is(rerr, io.EOF) {
		rerr = errors.New("upstream đã đóng stdout (tiến trình thoát)")
	}
	t.readErr = rerr
	pend := t.pending
	t.pending = map[int64]chan rpcResponse{}
	t.mu.Unlock()
	close(t.done)
	for _, ch := range pend {
		close(ch)
	}
}

func (t *stdioT) dispatch(line []byte) {
	var m rpcResponse
	if json.Unmarshal(line, &m) != nil {
		return // dòng không phải JSON (log lẫn vào stdout): bỏ qua
	}
	if m.Method != "" {
		t.serverRequest(m)
		return
	}
	if !m.isResponse() {
		return
	}
	var id int64
	if json.Unmarshal(m.ID, &id) != nil {
		return
	}
	t.mu.Lock()
	ch := t.pending[id]
	delete(t.pending, id)
	t.mu.Unlock()
	if ch != nil {
		ch <- m
	}
}

// serverRequest trả lời yêu cầu server gửi ngược: ping thì {}, còn lại báo không hỗ trợ; thông báo thì bỏ qua.
func (t *stdioT) serverRequest(m rpcResponse) {
	if len(m.ID) == 0 || string(m.ID) == "null" {
		return
	}
	var body []byte
	if m.Method == "ping" {
		body, _ = json.Marshal(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": map[string]any{}})
	} else {
		body, _ = json.Marshal(map[string]any{"jsonrpc": "2.0", "id": m.ID,
			"error": map[string]any{"code": -32601, "message": "client không hỗ trợ " + m.Method}})
	}
	_ = t.write(body)
}

func (t *stdioT) write(b []byte) error {
	t.wmu.Lock()
	defer t.wmu.Unlock()
	_, err := t.stdin.Write(append(b, '\n'))
	return err
}

func (t *stdioT) call(ctx context.Context, id int64, method string, params any) (json.RawMessage, error) {
	body, err := request(id, method, params)
	if err != nil {
		return nil, err
	}
	ch := make(chan rpcResponse, 1)
	t.mu.Lock()
	if t.readErr != nil {
		err := t.readErr
		t.mu.Unlock()
		return nil, err
	}
	t.pending[id] = ch
	t.mu.Unlock()
	drop := func() {
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
	}
	if err := t.write(body); err != nil {
		drop()
		return nil, fmt.Errorf("%s: ghi vào upstream: %w", method, err)
	}
	select {
	case m, ok := <-ch:
		if !ok {
			t.mu.Lock()
			err := t.readErr
			t.mu.Unlock()
			return nil, fmt.Errorf("%s: %w", method, err)
		}
		return m.outcome()
	case <-ctx.Done():
		drop()
		return nil, fmt.Errorf("%s: %w", method, ctx.Err())
	}
}

func (t *stdioT) notify(_ context.Context, method string, params any) error {
	body, err := notification(method, params)
	if err != nil {
		return err
	}
	return t.write(body)
}

func (t *stdioT) setProtocol(string) {}

func (t *stdioT) close() error {
	_ = t.stdin.Close()
	select {
	case <-t.waited:
		return nil
	case <-time.After(closeWait):
	}
	if t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
	}
	select {
	case <-t.waited:
	case <-time.After(closeWait):
		return errors.New("tiến trình upstream không thoát sau khi kill")
	}
	return nil
}

// tailBuffer giữ max byte cuối của luồng ghi vào (an toàn đa luồng).
type tailBuffer struct {
	mu  sync.Mutex
	max int
	b   []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if len(t.b) > t.max {
		t.b = append([]byte(nil), t.b[len(t.b)-t.max:]...)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.b)
}
