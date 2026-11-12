package mcpgw

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"bint/internal/state"
)

// stallTransport treo mọi request tới khi context huỷ (hoặc stall đóng): upstream HTTP treo lúc handshake.
type stallTransport struct{ stall chan struct{} }

func (s stallTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	select {
	case <-r.Context().Done():
		return nil, r.Context().Err()
	case <-s.stall:
		return nil, errors.New("đóng")
	}
}

// .1: handshake lại (sau restart) gặp 503/429/đứt kết nối thì tool chỉ đọc vẫn retry và thành công; tool ghi không retry.
func TestHTTPHandshakeRetryReadOnly(t *testing.T) {
	h, f := httpHarness(t)
	c := h.open("deepwiki", nil) // cache đã có; upstream HTTP đầu tiên chưa bị dừng
	h.d.Restart("deepwiki")      // httpUp mới, chưa handshake
	f.mu.Lock()
	f.initFail = []int{503, 429, 0}
	f.mu.Unlock()
	before := f.initTryCount()
	m := c.rpc(1, "tools/call", map[string]any{"name": "read_wiki_contents"})
	if len(m.Error) > 0 {
		t.Fatalf("tool chỉ đọc phải retry handshake: %s", m.Error)
	}
	if n := f.initTryCount() - before; n != 4 {
		t.Errorf("%d request initialize, muốn 4 (3 lỗi + 1 thành công)", n)
	}

	// Tool ghi: handshake lỗi thì dừng ngay, không retry, không có lệnh nào tới upstream.
	h.d.Restart("deepwiki")
	f.mu.Lock()
	f.initFail = []int{503, 503}
	f.mu.Unlock()
	before, calls := f.initTryCount(), f.count("write_thing")
	m = c.rpc(2, "tools/call", map[string]any{"name": "write_thing"})
	if len(m.Error) == 0 {
		t.Error("handshake lỗi với tool ghi phải trả lỗi")
	}
	if n := f.initTryCount() - before; n != 1 || f.count("write_thing") != calls {
		t.Errorf("tool ghi: %d request initialize (muốn 1), %d lệnh ghi tới upstream (muốn 0)", n, f.count("write_thing")-calls)
	}
}

// .1: handshake HTTP treo không được giữ chân khách tới khi client tự huỷ: bị cắt ở StartTimeout.
func TestHTTPHandshakeStartTimeout(t *testing.T) {
	up := newHTTPUp("http://127.0.0.1:1/", "t", Options{StartTimeout: 50 * time.Millisecond, MaxFrame: 1 << 20})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	srvStall := make(chan struct{})
	t.Cleanup(func() { close(srvStall) })
	up.client.Transport = stallTransport{stall: srvStall}
	start := time.Now()
	err := up.ensure(ctx)
	if err == nil || ctx.Err() != nil || time.Since(start) > 2*time.Second {
		t.Fatalf("ensure phải bị cắt bởi StartTimeout: err=%v sau %s (ctx.Err=%v)", err, time.Since(start), ctx.Err())
	}
}

// .3: phân vùng session đã detach không nhận upstream mới.
func TestDetachedSessionPartitionRefusesUpstream(t *testing.T) {
	h := newHarness(t, nil)
	h.register("chrome-devtools")
	repo := evalDir(t, newRepo(t, false))
	a := h.open("chrome-devtools", worktreeHdr(repo))
	_, out := a.call(1, "take_snapshot", nil)
	var p *partition
	h.d.mu.Lock()
	for _, x := range h.d.parts {
		p = x
	}
	h.d.mu.Unlock()
	if p == nil {
		t.Fatal("không có phân vùng")
	}
	a.close()
	eventually(t, "tiến trình của phiên dừng", 5*time.Second, func() bool { return !processAlive(out.PID) })
	starts := len(h.starts("chrome-devtools"))
	if _, err := p.acquire(t.Context()); err == nil {
		t.Fatal("phân vùng đã detach vẫn trả upstream")
	}
	if _, err := p.acquireLazy(t.Context()); err == nil {
		t.Fatal("phân vùng đã detach vẫn trả upstream (lazy)")
	}
	if n := len(h.starts("chrome-devtools")); n != starts {
		t.Errorf("khởi động thêm %d tiến trình cho phân vùng mồ côi", n-starts)
	}
	p.mu.Lock()
	up, inflight := p.up, p.inflight
	p.mu.Unlock()
	if up != nil || inflight != 0 {
		t.Errorf("phân vùng mồ côi giữ up=%v inflight=%d", up, inflight)
	}
}

// .4: notifications/cancelled kết thúc lời gọi đang chờ dù upstream bỏ qua huỷ: nhả semaphore nhóm contract
// (MaxConcurrent 1) và inflight, outcome là canceled.
func TestCancelledEndsPendingCall(t *testing.T) {
	h := newHarness(t, nil)
	h.register("specmatic")
	repo := evalDir(t, newRepo(t, true))
	markInit(t, repo)
	a := h.open("specmatic", worktreeHdr(repo))
	b := h.open("specmatic", worktreeHdr(repo))

	// Upstream bỏ qua notifications/cancelled và chạy lệnh đầu tới hết (sleep_ms).
	const firstMS = 1500
	t0 := time.Now()
	done := make(chan rpcMsg, 1)
	go func() {
		done <- a.rpc("c1", "tools/call", map[string]any{"name": "run_contract_test",
			"arguments": map[string]any{"sleep_ms": firstMS, "no_cancel": true}})
	}()
	eventually(t, "upstream nhận lời gọi", 5*time.Second, func() bool { return len(h.recvs("specmatic", "tools/call")) == 1 })

	// Lời gọi thứ hai cùng nhóm bị chặn bởi semaphore.
	second := make(chan rpcMsg, 1)
	go func() {
		second <- b.rpc("b1", "tools/call", map[string]any{"name": "run_contract_test", "arguments": map[string]any{"echo": "hai"}})
	}()
	select {
	case m := <-second:
		t.Fatalf("lời gọi thứ hai không bị semaphore chặn: %s", m.Result)
	case <-time.After(300 * time.Millisecond):
	}

	a.post(map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": map[string]any{"requestId": "c1"}})
	select {
	case m := <-done:
		if len(m.Error) == 0 {
			t.Errorf("lời gọi bị huỷ phải kết thúc bằng lỗi, được %s", m.Result)
		}
		if el := time.Since(t0); el >= firstMS*time.Millisecond {
			t.Errorf("lời gọi bị huỷ phải trả lỗi ngay, không chờ upstream (%s)", el)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("lời gọi không kết thúc sau notifications/cancelled")
	}
	// Upstream vẫn chạy lệnh đầu: chỗ trong nhóm contract (MaxConcurrent 1) chỉ được nhả khi upstream xong lệnh đó,
	// nên lời gọi thứ hai không được tới upstream trước lúc ấy.
	select {
	case m := <-second:
		if len(m.Error) > 0 {
			t.Errorf("lời gọi thứ hai sau khi nhả semaphore: %s", m.Error)
		}
		if el := time.Since(t0); el < firstMS*time.Millisecond {
			t.Errorf("lời gọi thứ hai chạy khi upstream còn chạy lệnh đã huỷ (sau %s, lệnh đầu %d ms)", el, firstMS)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("semaphore nhóm contract không được nhả sau khi upstream xong lệnh đã huỷ")
	}
	eventually(t, "inflight về 0, outcome canceled", 5*time.Second, func() bool {
		for _, u := range h.d.Status().Upstreams {
			if u.Service == "specmatic" && u.Inflight == 0 && u.LastCall != nil {
				return true
			}
		}
		return false
	})
}

// .4: huỷ khi lời gọi còn đang chờ semaphore (chưa tới upstream) cũng kết thúc ngay.
func TestCancelledWhileWaitingForSemaphore(t *testing.T) {
	h := newHarness(t, nil)
	h.register("specmatic")
	repo := evalDir(t, newRepo(t, true))
	markInit(t, repo)
	a := h.open("specmatic", worktreeHdr(repo))
	b := h.open("specmatic", worktreeHdr(repo))
	first := make(chan rpcMsg, 1)
	go func() {
		first <- a.rpc("c1", "tools/call", map[string]any{"name": "run_contract_test", "arguments": map[string]any{"sleep_ms": 60000, "no_cancel": true}})
	}()
	eventually(t, "upstream nhận lời gọi", 5*time.Second, func() bool { return len(h.recvs("specmatic", "tools/call")) == 1 })
	waiting := make(chan rpcMsg, 1)
	go func() {
		waiting <- b.rpc("b1", "tools/call", map[string]any{"name": "run_contract_test"})
	}()
	time.Sleep(300 * time.Millisecond)
	b.post(map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": map[string]any{"requestId": "b1"}})
	select {
	case m := <-waiting:
		if len(m.Error) == 0 {
			t.Errorf("phải là lỗi: %s", m.Result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("lời gọi đang chờ semaphore không kết thúc sau notifications/cancelled")
	}
	if n := len(h.recvs("specmatic", "tools/call")); n != 1 {
		t.Errorf("lời gọi bị huỷ khi chờ vẫn tới upstream (%d request)", n)
	}
	eventually(t, "outcome canceled", 5*time.Second, func() bool {
		for _, u := range h.d.Status().Upstreams {
			if u.Service == "specmatic" && u.LastCall != nil && u.LastCall.Outcome == OutcomeCanceled {
				return true
			}
		}
		return false
	})
}

// .5: daemon tách rời chạy với cwd trung tính (thư mục gateway), không phải cwd của lệnh gọi.
func TestStartDetachedNeutralCwd(t *testing.T) {
	testutilIsolate(t)
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	caller := t.TempDir()
	old, _ := os.Getwd()
	if err := os.Chdir(caller); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	info, started, err := StartDetached(t.Context(), dir, osArgs0(), []string{"__gateway"}, nil)
	if err != nil || !started {
		t.Fatalf("StartDetached: %v started=%v", err, started)
	}
	t.Cleanup(func() { _, _ = Stop(t.Context(), dir) })
	cwd, err := os.Readlink(filepath.Join("/proc", itoaI(info.PID), "cwd"))
	if err != nil {
		t.Skipf("không đọc được cwd của tiến trình (cần /proc): %v", err)
	}
	if sameFile(t, cwd, caller) {
		t.Errorf("daemon kế thừa cwd của lệnh gọi %s", caller)
	}
	if !sameFile(t, cwd, dir) {
		t.Errorf("cwd của daemon = %s, muốn thư mục gateway %s", cwd, dir)
	}
}

func sameFile(t *testing.T, a, b string) bool {
	t.Helper()
	ia, e1 := os.Stat(a)
	ib, e2 := os.Stat(b)
	return e1 == nil && e2 == nil && os.SameFile(ia, ib)
}

// .5: BINT_STATE_DIR tương đối vẫn trỏ cùng thư mục sau khi đổi cwd của daemon.
func TestStartDetachedRelativeStateDir(t *testing.T) {
	testutilIsolate(t)
	base := t.TempDir()
	old, _ := os.Getwd()
	if err := os.Chdir(base); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	t.Setenv(state.EnvStateDir, "rel-state")
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	info, started, err := StartDetached(t.Context(), dir, osArgs0(), []string{"__gateway"}, nil)
	if err != nil || !started {
		t.Fatalf("StartDetached: %v started=%v", err, started)
	}
	t.Cleanup(func() { _, _ = Stop(t.Context(), dir) })
	if i, err := ReadInfo(dir); err != nil || i.PID != info.PID {
		t.Errorf("gateway.json không nằm ở %s: %+v %v", dir, i, err)
	}
	if !filepath.IsAbs(dir) || !strings.HasPrefix(dir, base) {
		t.Errorf("dir %s không phân giải theo cwd của lệnh gọi %s", dir, base)
	}
}

// .2: killOnce chạy hành động giải phóng đúng một lần dù bị gọi đồng thời (mô phỏng die() và goroutine Wait cùng kill).
func TestKillOnceConcurrent(t *testing.T) {
	for i := 0; i < 200; i++ {
		var k killOnce
		var n atomic.Int32
		var wg sync.WaitGroup
		start := make(chan struct{})
		for j := 0; j < 8; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				k.do(func() { n.Add(1) })
			}()
		}
		close(start)
		wg.Wait()
		if n.Load() != 1 {
			t.Fatalf("hành động giải phóng chạy %d lần, muốn 1", n.Load())
		}
	}
}

// Huỷ khi lệnh còn trong hàng đợi: vòng ghi không bao giờ gửi lệnh đó; đã ghi thì không bỏ được (phải giữ chỗ).
func TestWaiterAbandonVersusWrite(t *testing.T) {
	w := &waiter{ch: make(chan waitResult, 1)}
	if !w.abandon() {
		t.Fatal("lệnh còn trong hàng đợi phải bỏ được")
	}
	if w.markWritten() || w.written.Load() {
		t.Fatal("lệnh đã bỏ không được ghi tới upstream")
	}
	w2 := &waiter{ch: make(chan waitResult, 1)}
	if !w2.markWritten() || !w2.written.Load() {
		t.Fatal("lệnh chưa bỏ phải ghi được")
	}
	if w2.abandon() {
		t.Fatal("lệnh đã ghi không được coi là bỏ")
	}
}
