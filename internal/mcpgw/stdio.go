package mcpgw

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const stderrCap = 4096

// stdioCfg là phần cấu hình của một upstream stdio.
type stdioCfg struct {
	gen      int
	path     string
	args     []string
	dir      string
	env      []string
	root     string // gốc worktree của phân vùng cho roots/list ("" = không có)
	maxFrame int
	queue    int
	version  string
	// onDead được gọi (một lần) khi upstream chết hoặc bị dừng, sau khi đã báo lỗi cho mọi người chờ.
	onDead func(u *stdioUp, cause error)
}

type outFrame struct {
	b []byte
	w *waiter
}

type waiter struct {
	ch      chan waitResult
	written atomic.Bool
	// state chốt cuộc đua giữa vòng ghi và người huỷ: 0 = còn trong hàng đợi, 1 = đã (bắt đầu) ghi, 2 = bỏ (huỷ trước
	// khi ghi; vòng ghi bỏ qua frame, upstream không bao giờ nhận lệnh).
	state atomic.Int32
}

// markWritten được vòng ghi gọi ngay trước khi ghi; false = người gọi đã bỏ lệnh, không ghi.
func (w *waiter) markWritten() bool {
	if !w.state.CompareAndSwap(0, 1) {
		return false
	}
	w.written.Store(true)
	return true
}

// abandon bỏ lệnh còn trong hàng đợi; false = lệnh đã (bắt đầu) được ghi tới upstream.
func (w *waiter) abandon() bool { return w.state.CompareAndSwap(0, 2) }

type waitResult struct {
	msg rpcMsg
	err error
}

// stdioUp là một tiến trình upstream stdio dùng chung cho nhiều phiên: đổi id JSON-RPC thành gw_<gen>_<seq>_<phiên>,
// hàng đợi ghi có giới hạn, giới hạn kích thước frame, trả roots/list và từ chối sampling/elicitation.
type stdioUp struct {
	cfg    stdioCfg
	cmd    *exec.Cmd
	grp    *procGroup
	stdin  io.WriteCloser
	q      chan outFrame
	stderr *tailBuffer

	mu      sync.Mutex
	pending map[string]*waiter
	seq     uint64
	deadErr error
	deadCh  chan struct{}

	readerDone chan struct{}
	progress   progressTable

	initRes, toolsRes json.RawMessage
	pidv              int
}

// startStdio khởi động tiến trình, làm handshake (initialize, initialized, tools/list) rồi trả upstream sẵn sàng.
func startStdio(ctx context.Context, cfg stdioCfg) (*stdioUp, error) {
	cmd := exec.Command(cfg.path, cfg.args...)
	cmd.Dir, cmd.Env = cfg.dir, cfg.env
	setupCmd(cmd)
	u := &stdioUp{cfg: cfg, cmd: cmd, q: make(chan outFrame, cfg.queue), stderr: &tailBuffer{max: stderrCap},
		pending: map[string]*waiter{}, deadCh: make(chan struct{}), readerDone: make(chan struct{})}
	cmd.Stderr = u.stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	u.stdin = in
	// Ống stdout tự dựng: cmd.Wait không đóng ống này, nên khung cuối của upstream vừa trả lời vừa thoát vẫn đọc được.
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdout = pw
	if err := cmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		return nil, err
	}
	pw.Close()
	u.pidv = cmd.Process.Pid
	if u.grp, err = attachProc(cmd); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		pr.Close()
		return nil, err
	}
	go u.readLoop(pr)
	go u.writeLoop()
	go func() {
		_ = cmd.Wait()
		u.grp.kill() // còn con cháu giữ ống stdout thì giết để luồng đọc thấy EOF
		select {
		case <-u.readerDone:
		case <-time.After(2 * time.Second):
		}
		u.die(errors.New("tiến trình upstream đã thoát"))
	}()
	if err := u.handshake(ctx); err != nil {
		u.die(err)
		return nil, countedErr{err} // onDead đã ghi nhận cái chết
	}
	return u, nil
}

// countedErr đánh dấu lỗi khởi động mà onDead đã tính vào bộ đếm restart.
type countedErr struct{ error }

func (e countedErr) Unwrap() error { return e.error }

func (u *stdioUp) handshake(ctx context.Context) error {
	res, err := u.rawCall(ctx, "gateway", "initialize", handshakeParams(u.cfg.version), true, nil)
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	if len(res.rpcErr) > 0 {
		return fmt.Errorf("initialize: %s", string(res.rpcErr))
	}
	u.initRes = res.result
	if err := u.enqueue(outFrame{b: requestFrame(nil, "notifications/initialized", nil)}); err != nil {
		return err
	}
	var pages []json.RawMessage
	cursor := ""
	for i := 0; i < 50; i++ {
		var params json.RawMessage
		if cursor != "" {
			params = marshalRaw(map[string]string{"cursor": cursor})
		}
		r, err := u.rawCall(ctx, "gateway", "tools/list", params, true, nil)
		if err != nil {
			return fmt.Errorf("tools/list: %w", err)
		}
		if len(r.rpcErr) > 0 {
			return fmt.Errorf("tools/list: %s", string(r.rpcErr))
		}
		pages = append(pages, r.result)
		if cursor = nextCursor(r.result); cursor == "" {
			break
		}
	}
	u.toolsRes = mergeTools(pages)
	return nil
}

func (u *stdioUp) ensure(context.Context) error {
	if !u.alive() {
		return u.deathCause()
	}
	return nil
}
func (u *stdioUp) hello() (json.RawMessage, json.RawMessage) { return u.initRes, u.toolsRes }
func (u *stdioUp) generation() int                           { return u.cfg.gen }
func (u *stdioUp) pid() int                                  { return u.pidv }

func (u *stdioUp) alive() bool {
	select {
	case <-u.deadCh:
		return false
	default:
		return true
	}
}

func (u *stdioUp) deathCause() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.deadErr
}

func (u *stdioUp) stop() { u.die(errStopped) }

func (u *stdioUp) routeProgress(token string, fn func(json.RawMessage)) func() {
	return u.progress.add(token, fn)
}

// die đánh dấu upstream chết (một lần): kill cả cây, báo lỗi cho mọi người chờ rồi gọi onDead.
func (u *stdioUp) die(cause error) {
	u.mu.Lock()
	if u.deadErr != nil {
		u.mu.Unlock()
		return
	}
	u.deadErr = cause
	pend := u.pending
	u.pending = map[string]*waiter{}
	close(u.deadCh)
	u.mu.Unlock()
	u.grp.kill()
	_ = u.stdin.Close()
	// Ghi nhận cái chết (đếm restart, xoá upstream khỏi phân vùng) TRƯỚC khi báo lỗi cho người chờ: lời gọi kế tiếp của
	// họ phải thấy trạng thái mới.
	if u.cfg.onDead != nil {
		u.cfg.onDead(u, cause)
	}
	for _, w := range pend {
		w.ch <- waitResult{err: cause}
	}
}

// enqueue xếp frame vào hàng đợi ghi; hàng đợi đầy thì upstream bị coi như chết.
func (u *stdioUp) enqueue(f outFrame) error {
	if !u.alive() {
		return u.deathCause()
	}
	select {
	case u.q <- f:
		return nil
	default:
		err := fmt.Errorf("hàng đợi ghi tới upstream đầy (%d frame)", cap(u.q))
		u.die(err)
		return err
	}
}

func (u *stdioUp) writeLoop() {
	for {
		select {
		case f := <-u.q:
			// Đánh dấu trước khi ghi: ghi dở vẫn có thể tới upstream. Lệnh đã bị huỷ khi còn trong hàng đợi thì bỏ.
			if f.w != nil && !f.w.markWritten() {
				continue
			}
			if _, err := u.stdin.Write(append(f.b, '\n')); err != nil {
				u.die(fmt.Errorf("ghi vào upstream: %w", err))
				return
			}
		case <-u.deadCh:
			return
		}
	}
}

func (u *stdioUp) readLoop(r io.Reader) {
	defer close(u.readerDone)
	br := bufio.NewReaderSize(r, 64*1024)
	for {
		line, err := readFrame(br, u.cfg.maxFrame)
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = errors.New("upstream đã đóng stdout")
			}
			u.die(err)
			return
		}
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var m rpcMsg
		if json.Unmarshal(line, &m) != nil {
			continue // dòng không phải JSON (log lẫn vào stdout)
		}
		u.dispatch(m)
	}
}

func (u *stdioUp) dispatch(m rpcMsg) {
	switch {
	case m.isRequest():
		u.serverRequest(m)
	case m.isNotification():
		if m.Method == "notifications/progress" {
			u.progress.deliver(m.Params)
		}
		// Notification khác của upstream không có người nhận ở M2 (không chuyển tới client).
	case m.isResponse():
		var id string
		if json.Unmarshal(m.ID, &id) != nil {
			return
		}
		u.mu.Lock()
		w := u.pending[id]
		delete(u.pending, id)
		u.mu.Unlock()
		if w != nil {
			w.ch <- waitResult{msg: m}
		}
		// id lạ hoặc của generation cũ: bỏ.
	}
}

// serverRequest trả lời request upstream gửi ngược: ping → {}, roots/list → gốc worktree của phân vùng; sampling,
// elicitation và method lạ bị từ chối (agent không được hỏi người qua đường này). Không chuyển tới client.
func (u *stdioUp) serverRequest(m rpcMsg) {
	var frame []byte
	switch m.Method {
	case "ping":
		frame = resultFrame(m.ID, json.RawMessage(`{}`))
	case "roots/list":
		frame = resultFrame(m.ID, rootsResult(u.cfg.root))
	default:
		frame = errorFrame(m.ID, codeMethodNotFound, "gateway không chuyển "+m.Method+" tới client")
	}
	_ = u.enqueue(outFrame{b: frame})
}

// rootsResult là result của roots/list: một gốc dạng file URI (rỗng nếu phân vùng không có worktree).
func rootsResult(root string) json.RawMessage {
	roots := []map[string]string{}
	if root != "" {
		roots = append(roots, map[string]string{"uri": fileURI(root), "name": filepath.Base(root)})
	}
	return marshalRaw(map[string]any{"roots": roots})
}

// fileURI đổi đường dẫn tuyệt đối thành file URI (Windows: file:///C:/dir).
func fileURI(p string) string {
	s := filepath.ToSlash(p)
	if !strings.HasPrefix(s, "/") {
		s = "/" + s
	}
	return (&url.URL{Scheme: "file", Path: s}).String()
}

func (u *stdioUp) call(ctx context.Context, sess, method string, params json.RawMessage, readOnly bool, onID func(string)) (upResult, error) {
	return u.rawCall(ctx, sess, method, params, readOnly, onID)
}

func (u *stdioUp) rawCall(ctx context.Context, sess, method string, params json.RawMessage, readOnly bool, onID func(string)) (upResult, error) {
	u.mu.Lock()
	if u.deadErr != nil {
		err := u.deadErr
		u.mu.Unlock()
		return upResult{}, &rpcError{Code: CodeUpstream, Message: "upstream đã chết: " + err.Error()}
	}
	u.seq++
	id := fmt.Sprintf("gw_%d_%d_%s", u.cfg.gen, u.seq, sess)
	w := &waiter{ch: make(chan waitResult, 1)}
	u.pending[id] = w
	u.mu.Unlock()
	drop := func() {
		u.mu.Lock()
		delete(u.pending, id)
		u.mu.Unlock()
	}
	if onID != nil {
		onID(id)
	}
	if err := u.enqueue(outFrame{b: requestFrame(marshalRaw(id), method, params), w: w}); err != nil {
		drop()
		return upResult{}, &rpcError{Code: CodeUpstream, Message: "không gửi được tới upstream: " + err.Error()}
	}
	select {
	case r := <-w.ch:
		if r.err != nil {
			if w.written.Load() && !readOnly {
				return upResult{}, &rpcError{Code: CodeUnknownOutcome, Message: "upstream chết sau khi nhận lệnh ghi; không rõ lệnh đã chạy chưa, gateway không tự gọi lại: " + r.err.Error()}
			}
			return upResult{}, &rpcError{Code: CodeUpstream, Message: "upstream chết: " + r.err.Error()}
		}
		return upResult{result: r.msg.Result, rpcErr: r.msg.Error}, nil
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) && !w.abandon() {
			// Client huỷ khi lệnh đã tới upstream: trả lỗi huỷ ngay nhưng lệnh có thể vẫn chạy (upstream không bắt buộc
			// tôn trọng notifications/cancelled). Người gọi giữ chỗ trong nhóm (semaphore, inflight) tới khi upstream trả
			// lời, chết, hoặc tới hạn chót của lời gọi.
			settled := make(chan struct{})
			go func() {
				defer close(settled)
				var limit <-chan time.Time
				if dl, ok := ctx.Deadline(); ok {
					t := time.NewTimer(time.Until(dl))
					defer t.Stop()
					limit = t.C
				}
				select {
				case <-w.ch:
				case <-limit:
					drop()
				}
			}()
			return upResult{}, &heldCancel{settled: settled}
		}
		drop()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			w.abandon() // còn trong hàng đợi thì không bao giờ gửi; đã ghi thì written cho biết
			if w.written.Load() && !readOnly {
				return upResult{}, &rpcError{Code: CodeUnknownOutcome, Message: "hết thời gian chờ lệnh ghi; không rõ lệnh đã chạy chưa, gateway không tự gọi lại"}
			}
			return upResult{}, &rpcError{Code: CodeTimeout, Message: "upstream không trả lời trong thời gian cho phép"}
		}
		return upResult{}, ctx.Err()
	}
}

func (u *stdioUp) cancel(gwID string, reason json.RawMessage) {
	p := map[string]any{"requestId": gwID}
	if len(reason) > 0 {
		p["reason"] = reason
	}
	_ = u.enqueue(outFrame{b: requestFrame(nil, "notifications/cancelled", marshalRaw(p))})
}
