package mcpgw

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"bint/internal/config"
	"bint/internal/mcpserver"
	"bint/internal/testutil"
)

// ---- hạ tầng ----

// shimEnv: môi trường cô lập để Shim tự khởi động daemon thật (tiến trình con của binary test, __gateway) với upstream giả.
type shimEnv struct {
	t      *testing.T
	dir    string // thư mục gateway mặc định (Dir())
	logDir string
	repo   string
}

func newShimEnv(t *testing.T, services ...string) *shimEnv {
	t.Helper()
	testutil.Isolate(t)
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	e := &shimEnv{t: t, dir: dir, logDir: t.TempDir(), repo: evalDir(t, newRepo(t, false))}
	m := config.MachineMCP{Version: 1, Services: map[string]config.Registration{}}
	for _, svc := range services {
		m.Services[svc] = config.Registration{Command: os.Args[0],
			Args: []string{"__fake_upstream", "log=" + e.logPath(svc), "tools=" + fakeTools[svc]}}
	}
	if err := config.SaveMachineMCP(m); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = Stop(ctx, dir)
	})
	return e
}

func (e *shimEnv) logPath(svc string) string { return filepath.Join(e.logDir, svc+".jsonl") }

func (e *shimEnv) starts(svc string) int {
	data, _ := os.ReadFile(e.logPath(svc))
	n := 0
	for _, l := range strings.Split(string(data), "\n") {
		if strings.Contains(l, `"ev":"start"`) {
			n++
		}
	}
	return n
}

func (e *shimEnv) options(services ...string) ShimOptions {
	return ShimOptions{Dir: e.dir, Exe: os.Args[0], StartArgs: []string{"__gateway"}, Services: services, Worktree: e.repo,
		StartWait: 15 * time.Second, OpenWait: 15 * time.Second}
}

func (e *shimEnv) shim(o ShimOptions) *Shim {
	e.t.Helper()
	s, err := NewShim(o)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(s.Close)
	return s
}

// mcpClient chạy mcpserver (6 tool của Bint + Shim làm Proxy) qua io.Pipe như `bint mcp serve` qua stdio.
type mcpClient struct {
	t    *testing.T
	in   *io.PipeWriter
	out  *bufio.Reader
	next int
	done chan struct{}
}

func serveShim(t *testing.T, sh *Shim) *mcpClient {
	t.Helper()
	srv := mcpserver.New("bint", "t", mcpserver.Instructions)
	mcpserver.RegisterTools(srv, nil)
	if sh != nil {
		srv.Proxy = sh
		sh.Start()
	}
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	c := &mcpClient{t: t, in: inW, out: bufio.NewReader(outR), done: make(chan struct{})}
	go func() { _ = srv.Serve(context.Background(), inR, outW); outW.Close(); close(c.done) }()
	t.Cleanup(func() { inW.Close(); <-c.done })
	return c
}

func (c *mcpClient) rpc(method string, params any) map[string]any {
	c.t.Helper()
	c.next++
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": c.next, "method": method, "params": params})
	if _, err := c.in.Write(append(b, '\n')); err != nil {
		c.t.Fatal(err)
	}
	ch := make(chan map[string]any, 1)
	go func() {
		line, err := c.out.ReadString('\n')
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
		if m == nil {
			c.t.Fatalf("%s: luồng đóng", method)
		}
		return m
	case <-time.After(60 * time.Second):
		c.t.Fatalf("%s: không có phản hồi", method)
		return nil
	}
}

func (c *mcpClient) init() map[string]any {
	c.t.Helper()
	return c.rpc("initialize", map[string]any{"protocolVersion": "2025-06-18"})["result"].(map[string]any)
}

func (c *mcpClient) toolNames() []string {
	c.t.Helper()
	var out []string
	for _, tl := range c.rpc("tools/list", nil)["result"].(map[string]any)["tools"].([]any) {
		out = append(out, tl.(map[string]any)["name"].(string))
	}
	return out
}

func (c *mcpClient) call(name string, args any) (map[string]any, toolOut) {
	c.t.Helper()
	m := c.rpc("tools/call", map[string]any{"name": name, "arguments": args})
	res, _ := m["result"].(map[string]any)
	var out toolOut
	if cs, ok := res["content"].([]any); ok && len(cs) > 0 {
		txt, _ := cs[0].(map[string]any)["text"].(string)
		_ = json.Unmarshal([]byte(txt), &out)
	}
	return res, out
}

var bintToolNames = []string{"context_for_task", "context_for_paths", "knowledge_search", "knowledge_get", "cite_knowledge", "report_observation"}

func hasAll(have []string, want ...string) bool {
	for _, w := range want {
		if !slices.Contains(have, w) {
			return false
		}
	}
	return true
}

// ---- kiểm thử ----

// tools/list gồm 6 tool của Bint cùng tool của service được cấp với tên <tiền tố>_<tool>; gọi cg_explore tới upstream với
// tên codegraph_explore, kết quả về nguyên vẹn.
func TestShimForwardsWithPrefix(t *testing.T) {
	h := newHarness(t, nil)
	h.register("codegraph")
	repo := evalDir(t, newRepo(t, false))
	sh, err := NewShim(ShimOptions{Dir: h.dir, Services: []string{"codegraph"}, Worktree: repo})
	if err != nil {
		t.Fatal(err)
	}
	defer sh.Close()
	c := serveShim(t, sh)
	init := c.init()
	if ins := init["instructions"].(string); strings.Contains(ins, "KHÔNG dùng được") {
		t.Errorf("không có lỗi nào để báo: %q", ins)
	}
	names := c.toolNames()
	if !hasAll(names, bintToolNames...) || !hasAll(names, "cg_explore", "cg_status") || len(names) != 8 {
		t.Fatalf("tools/list: %v", names)
	}
	res, out := c.call("cg_explore", map[string]any{"echo": "DU-LIEU"})
	if res == nil || res["isError"] == true {
		t.Fatalf("kết quả: %v", res)
	}
	if out.Tool != "codegraph_explore" || out.Echo != "DU-LIEU" {
		t.Errorf("upstream phải nhận tên codegraph_explore: %+v", out)
	}
	if n := len(h.recvs("codegraph", "tools/call")); n != 1 {
		t.Errorf("upstream nhận %d lời gọi", n)
	}
	// Tên không có trong bảng (kể cả tên upstream gốc hay tên đoán mò) bị từ chối ở shim: upstream không nhận.
	for _, bad := range []string{"codegraph_explore", "cg_khong_co", "cg_do_write"} {
		if m := c.rpc("tools/call", map[string]any{"name": bad}); m["error"] == nil {
			t.Errorf("%s: phải bị từ chối: %v", bad, m)
		}
	}
	if n := len(h.recvs("codegraph", "tools/call")); n != 1 {
		t.Errorf("upstream nhận %d lời gọi sau khi gọi tên ngoài bảng, muốn 1", n)
	}
}

// Không có daemon sống thì shim tự khởi động daemon tách rời (tiến trình riêng), chờ có hạn rồi chuyển tiếp.
func TestShimStartsDetachedDaemon(t *testing.T) {
	e := newShimEnv(t, "codegraph")
	if _, err := ReadInfo(e.dir); err == nil {
		t.Fatal("chưa được có daemon")
	}
	sh := e.shim(e.options("codegraph"))
	c := serveShim(t, sh)
	c.init()
	if names := c.toolNames(); !hasAll(names, "cg_explore") {
		t.Fatalf("tools/list: %v", names)
	}
	info, err := ReadInfo(e.dir)
	if err != nil || info.PID == os.Getpid() || !processAlive(info.PID) {
		t.Fatalf("daemon phải là tiến trình riêng đang sống: %+v %v", info, err)
	}
	if _, out := c.call("cg_explore", nil); out.Tool != "codegraph_explore" {
		t.Errorf("%+v", out)
	}
	// Hai shim khởi động cùng lúc khi chưa có daemon: dùng chung một daemon (khoá gateway.lock).
}

func TestTwoShimsRaceToStartDaemon(t *testing.T) {
	e := newShimEnv(t, "codegraph")
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sh := e.shim(e.options("codegraph"))
			sh.Ready(context.Background())
			if tools := sh.Tools(); len(tools) == 0 {
				t.Errorf("shim không có tool: %q", sh.Instructions())
			}
		}()
	}
	wg.Wait()
	info, err := ReadInfo(e.dir)
	if err != nil {
		t.Fatal(err)
	}
	if st, _, err := QueryStatus(context.Background(), e.dir); err != nil || st.PID != info.PID || st.Sessions != 2 {
		t.Errorf("một daemon, hai phiên: %+v %v", st, err)
	}
}

// Daemon không lên trong hạn: shim vẫn phục vụ đủ 6 tool của Bint, instructions nêu service lỗi kèm lý do; không treo quá hạn.
func TestShimDaemonUnavailable(t *testing.T) {
	testutil.Isolate(t)
	for name, start := range map[string]func(context.Context, string) (Info, error){
		"hàm khởi động lỗi": func(context.Context, string) (Info, error) { return Info{}, errors.New("không có binary giả") },
		"không lên trong hạn": func(ctx context.Context, _ string) (Info, error) {
			<-ctx.Done()
			return Info{}, ctx.Err()
		},
	} {
		t.Run(name, func(t *testing.T) {
			sh, err := NewShim(ShimOptions{Dir: t.TempDir(), Start: start, Services: []string{"codegraph", "specmatic"},
				Worktree: t.TempDir(), StartWait: 200 * time.Millisecond})
			if err != nil {
				t.Fatal(err)
			}
			c := serveShim(t, sh)
			begin := time.Now()
			ins := c.init()["instructions"].(string)
			if d := time.Since(begin); d > 5*time.Second {
				t.Errorf("initialize mất %s, phải bị chặn bởi StartWait", d)
			}
			if !strings.Contains(ins, "Gateway MCP không khởi động được") || !strings.Contains(ins, "codegraph") || !strings.Contains(ins, "specmatic") {
				t.Errorf("instructions phải nêu service lỗi: %q", ins)
			}
			if name == "hàm khởi động lỗi" && !strings.Contains(ins, "không có binary giả") {
				t.Errorf("instructions thiếu lý do: %q", ins)
			}
			if got := c.toolNames(); !slices.Equal(got, bintToolNames) {
				t.Errorf("phải vẫn đủ 6 tool của Bint: %v", got)
			}
			if res, _ := c.call("context_for_task", nil); res == nil {
				t.Error("tool của Bint phải vẫn gọi được")
			}
		})
	}
}

// Không service ứng viên (chế độ đánh giá, repo không dùng MCP, BINT_MCP_NO_GATEWAY) thì không chạm gateway: không đọc
// gateway.json, không khởi động daemon, thư mục gateway không được tạo.
func TestShimWithoutCandidatesNeverTouchesGateway(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "gateway")
	started := false
	sh, err := NewShim(ShimOptions{Dir: dir, Start: func(context.Context, string) (Info, error) { started = true; return Info{}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	if sh.Active() {
		t.Error("không ứng viên thì Active=false")
	}
	c := serveShim(t, sh)
	if ins := c.init()["instructions"].(string); strings.Contains(ins, "Gateway") {
		t.Errorf("instructions: %q", ins)
	}
	if got := c.toolNames(); !slices.Equal(got, bintToolNames) {
		t.Errorf("tool: %v", got)
	}
	if started {
		t.Error("không được khởi động daemon")
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("không được tạo thư mục gateway")
	}
}

// Daemon bị kill giữa phiên: lời gọi kế tiếp qua cùng shim khởi động lại daemon, mở lại phiên và thành công. Nhiều lời gọi
// cùng lỗi chỉ nối lại (khởi động) một lần.
func TestShimRestartsKilledDaemon(t *testing.T) {
	e := newShimEnv(t, "codegraph")
	var startCalls atomic.Int32
	o := e.options("codegraph")
	o.Start = func(ctx context.Context, dir string) (Info, error) {
		startCalls.Add(1)
		i, _, err := StartDetached(ctx, dir, os.Args[0], []string{"__gateway"}, nil)
		return i, err
	}
	sh := e.shim(o)
	c := serveShim(t, sh)
	c.init()
	if _, out := c.call("cg_explore", nil); out.Tool != "codegraph_explore" {
		t.Fatalf("lần đầu: %+v", out)
	}
	first, _ := ReadInfo(e.dir)
	if err := killPID(first.PID); err != nil {
		t.Fatal(err)
	}
	eventually(t, "daemon cũ chết", 10*time.Second, func() bool { return !lockHeld(e.dir) })

	res, out := c.call("cg_explore", map[string]any{"echo": "sau-khi-kill"})
	if res == nil || res["isError"] == true || out.Echo != "sau-khi-kill" {
		t.Fatalf("lời gọi sau khi kill phải thành công: %v %+v", res, out)
	}
	second, err := ReadInfo(e.dir)
	if err != nil || second.PID == first.PID || second.Token == first.Token {
		t.Errorf("phải là daemon mới: %+v → %+v (%v)", first, second, err)
	}
	if n := startCalls.Load(); n != 2 {
		t.Errorf("hàm khởi động chạy %d lần, muốn 2 (kết nối đầu + nối lại)", n)
	}

	// Kill lần nữa rồi gọi đồng thời: single-flight.
	if err := killPID(second.PID); err != nil {
		t.Fatal(err)
	}
	eventually(t, "daemon thứ hai chết", 10*time.Second, func() bool { return !lockHeld(e.dir) })
	var wg sync.WaitGroup
	var failed atomic.Int32
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := sh.Call(context.Background(), "cg_explore", json.RawMessage(`{}`)); err != nil {
				t.Logf("lỗi: %v", err)
				failed.Add(1)
			}
		}()
	}
	wg.Wait()
	if failed.Load() != 0 {
		t.Errorf("%d lời gọi đồng thời thất bại", failed.Load())
	}
	if n := startCalls.Load(); n != 3 {
		t.Errorf("hàm khởi động chạy %d lần, muốn 3 (6 lời gọi đồng thời chỉ nối lại một lần)", n)
	}
}

// Cache tools/list trên đĩa: shim mới sau khi daemon khởi động lại không làm upstream được spawn nữa; service chưa từng chạy thì
// spawn đúng một lần.
func TestShimInitializeUsesDiskCache(t *testing.T) {
	e := newShimEnv(t, "codegraph")
	sh1 := e.shim(e.options("codegraph"))
	sh1.Ready(context.Background())
	if len(sh1.Tools()) == 0 {
		t.Fatalf("shim 1: %q", sh1.Instructions())
	}
	if n := e.starts("codegraph"); n != 1 {
		t.Fatalf("service chưa từng chạy: upstream spawn %d lần, muốn đúng 1", n)
	}
	sh1.Close()
	ctx := context.Background()
	if stopped, err := Stop(ctx, e.dir); err != nil || !stopped {
		t.Fatalf("Stop: %v %v", stopped, err)
	}
	sh2 := e.shim(e.options("codegraph"))
	sh2.Ready(ctx)
	if len(sh2.Tools()) == 0 {
		t.Fatalf("shim 2: %q", sh2.Instructions())
	}
	if n := e.starts("codegraph"); n != 1 {
		t.Errorf("initialize + tools/list sau khi daemon khởi động lại không được spawn upstream: %d lần", n)
	}
}

// ---- gateway giả: kiểm quy tắc gửi lại ----

type fakeGW struct {
	mu       sync.Mutex
	calls    map[string]int
	dropOnce map[string]bool // tool → ngắt kết nối (không trả lời) ở lần gọi đầu
	notFound map[string]bool // tool → trả 404 ở lần gọi đầu (chưa xử lý)
	inits    int
	ids      []string // id JSON-RPC của mọi request tools/call và initialize, theo thứ tự tới
}

func (f *fakeGW) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/admin/status", func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `{"pid":1}`) })
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var m rpcMsg
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &m)
		if m.Method == "initialize" || m.Method == "tools/call" {
			f.mu.Lock()
			f.ids = append(f.ids, string(m.ID))
			f.mu.Unlock()
		}
		switch m.Method {
		case "initialize":
			f.mu.Lock()
			f.inits++
			f.mu.Unlock()
			w.Header().Set(headerMCPSess, "sess")
			_, _ = w.Write(resultFrame(m.ID, json.RawMessage(`{"protocolVersion":"2025-06-18"}`)))
		case "tools/list":
			svc := r.Header.Get(headerMCPSess)
			_ = svc
			_, _ = w.Write(resultFrame(m.ID, json.RawMessage(`{"tools":[{"name":"backward_compatibility_check","inputSchema":{"type":"object"}},{"name":"run_contract_test","inputSchema":{"type":"object"}}]}`)))
		case "tools/call":
			var p struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(m.Params, &p)
			f.mu.Lock()
			f.calls[p.Name]++
			first := f.calls[p.Name] == 1
			drop, nf := f.dropOnce[p.Name] && first, f.notFound[p.Name] && first
			f.mu.Unlock()
			switch {
			case drop:
				hj, _ := w.(http.Hijacker)
				conn, _, _ := hj.Hijack()
				conn.Close()
			case nf:
				http.Error(w, "phiên không tồn tại", http.StatusNotFound)
			default:
				_, _ = w.Write(resultFrame(m.ID, json.RawMessage(`{"content":[{"type":"text","text":"ok"}]}`)))
			}
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	})
	return mux
}

func newFakeGateway(t *testing.T) (*fakeGW, ShimOptions) {
	t.Helper()
	f := &fakeGW{calls: map[string]int{}, dropOnce: map[string]bool{}, notFound: map[string]bool{}}
	ts := httptest.NewServer(f.handler())
	t.Cleanup(ts.Close)
	dir := t.TempDir()
	port, err := strconv.Atoi(ts.URL[strings.LastIndex(ts.URL, ":")+1:])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeInfo(dir, Info{PID: os.Getpid(), Port: port, Token: "tok"}); err != nil {
		t.Fatal(err)
	}
	return f, ShimOptions{Dir: dir, Services: []string{"specmatic"}, Worktree: t.TempDir(),
		Start: func(context.Context, string) (Info, error) {
			return Info{}, errors.New("không được khởi động")
		}}
}

// Gửi lại khi nối lại: lời gọi chưa tới daemon (404) gửi lại cả với lệnh ghi; kết nối đứt giữa chừng chỉ gửi lại với tool chỉ
// đọc, lệnh ghi không bao giờ bị gửi hai lần.
func TestShimResendRules(t *testing.T) {
	f, o := newFakeGateway(t)
	sh, err := NewShim(o)
	if err != nil {
		t.Fatal(err)
	}
	defer sh.Close()
	sh.Ready(context.Background())
	if !sh.Owns("spec_run_contract_test") || !sh.Owns("spec_backward_compatibility_check") {
		t.Fatalf("bảng tên: %q", sh.Instructions())
	}
	ctx := context.Background()

	// 404 (phiên không tồn tại): lệnh ghi được gửi lại, vì daemon chưa xử lý.
	f.mu.Lock()
	f.notFound["run_contract_test"] = true
	f.mu.Unlock()
	if _, err := sh.Call(ctx, "spec_run_contract_test", json.RawMessage(`{}`)); err != nil {
		t.Errorf("404 rồi gửi lại: %v", err)
	}
	if n := f.calls["run_contract_test"]; n != 2 {
		t.Errorf("lệnh ghi gặp 404: %d request, muốn 2", n)
	}

	// Đứt kết nối giữa chừng: tool chỉ đọc gửi lại; lệnh ghi thì không.
	f.mu.Lock()
	f.dropOnce["backward_compatibility_check"] = true
	f.dropOnce["manage_mock_server"] = true
	f.mu.Unlock()
	if _, err := sh.Call(ctx, "spec_backward_compatibility_check", json.RawMessage(`{}`)); err != nil {
		t.Errorf("tool chỉ đọc phải gửi lại: %v", err)
	}
	if n := f.calls["backward_compatibility_check"]; n != 2 {
		t.Errorf("tool chỉ đọc: %d request, muốn 2", n)
	}
	// run_resiliency_test không có trong tools/list giả: dùng run_contract_test với drop ở lần đầu của tên mới.
	f.mu.Lock()
	f.dropOnce["run_contract_test"] = true
	f.calls["run_contract_test"] = 0
	f.mu.Unlock()
	_, err = sh.Call(ctx, "spec_run_contract_test", json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "không rõ kết quả") {
		t.Errorf("lệnh ghi đứt giữa chừng: %v", err)
	}
	if n := f.calls["run_contract_test"]; n != 1 {
		t.Errorf("lệnh ghi đứt giữa chừng bị gửi %d lần, muốn đúng 1", n)
	}
	// Lời gọi kế tiếp vẫn dùng được (đã nối lại).
	if _, err := sh.Call(ctx, "spec_run_contract_test", json.RawMessage(`{}`)); err != nil {
		t.Errorf("sau khi nối lại: %v", err)
	}
}

// Tổng thời gian initialize chờ mặc định phải dưới timeout khởi động MCP 10 giây của Codex (startup_timeout_sec), nếu
// không Codex bỏ cả server bint và mất luôn tool của Bint.
func TestShimDefaultReadyBudgetUnderCodexStartupTimeout(t *testing.T) {
	const codexStartupTimeout = 10 * time.Second
	if total := DefaultShimStartWait + DefaultShimOpenWait + time.Second; total >= codexStartupTimeout-time.Second {
		t.Fatalf("initialize có thể chờ %s, phải dưới %s (còn dư ít nhất 1 giây)", total, codexStartupTimeout)
	}
}

// Phát hiện M3 .2/.11: mỗi request shim gửi lên daemon có id JSON-RPC riêng (kể cả các tools/call trên cùng một phiên).
func TestShimUniqueRequestIDs(t *testing.T) {
	f, o := newFakeGateway(t)
	sh, err := NewShim(o)
	if err != nil {
		t.Fatal(err)
	}
	defer sh.Close()
	sh.Ready(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := sh.Call(context.Background(), "spec_backward_compatibility_check", json.RawMessage(`{}`)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	f.mu.Lock()
	ids := slices.Clone(f.ids)
	f.mu.Unlock()
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("id %s bị dùng lại: %v", id, ids)
		}
		seen[id] = true
	}
	if len(ids) != 5 { // 1 initialize + 4 tools/call
		t.Fatalf("%d request, muốn 5: %v", len(ids), ids)
	}
}

// Phát hiện M3 .3: lần kết nối đầu không lên được gateway thì shim thử lại nền một lần; được thì thêm tool và báo OnToolsChanged.
func TestShimBackgroundRetryAddsTools(t *testing.T) {
	_, o := newFakeGateway(t)
	c, err := Connect(o.Dir)
	if err != nil {
		t.Fatal(err)
	}
	info := c.Info
	if err := os.Remove(filepath.Join(o.Dir, InfoFileName)); err != nil { // lần đầu không tìm thấy daemon
		t.Fatal(err)
	}
	var starts atomic.Int32
	o.Start = func(context.Context, string) (Info, error) {
		if starts.Add(1) == 1 {
			return Info{}, errors.New("daemon chưa lên")
		}
		return info, nil
	}
	o.StartWait, o.RetryDelay = 2*time.Second, 50*time.Millisecond
	sh, err := NewShim(o)
	if err != nil {
		t.Fatal(err)
	}
	defer sh.Close()
	changed := make(chan struct{}, 1)
	sh.OnToolsChanged(func() { changed <- struct{}{} })
	sh.Start()
	sh.Ready(context.Background())
	if sh.Owns("spec_run_contract_test") || !strings.Contains(sh.Instructions(), "daemon chưa lên") {
		t.Fatalf("lần đầu phải chưa có tool và nêu lỗi: %q", sh.Instructions())
	}
	select {
	case <-changed:
	case <-time.After(10 * time.Second):
		t.Fatal("không có lần thử lại nền")
	}
	if !sh.Owns("spec_run_contract_test") || !sh.Owns("spec_backward_compatibility_check") || sh.Instructions() != "" {
		t.Fatalf("sau thử lại phải có tool và hết lỗi: owns=%v instr=%q", sh.Owns("spec_run_contract_test"), sh.Instructions())
	}
	if _, err := sh.Call(context.Background(), "spec_run_contract_test", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("gọi sau thử lại: %v", err)
	}
}

// Kết nối đầu đủ tool thì không có lần thử lại nền; Close trước hạn thì lần thử lại không chạy.
func TestShimNoRetryWhenConnectedAndStopsOnClose(t *testing.T) {
	_, o := newFakeGateway(t)
	o.RetryDelay = 20 * time.Millisecond
	sh, err := NewShim(o)
	if err != nil {
		t.Fatal(err)
	}
	changed := make(chan struct{}, 1)
	sh.OnToolsChanged(func() { changed <- struct{}{} })
	sh.Start()
	sh.Ready(context.Background())
	if sh.needsRetry() {
		t.Fatal("đã đủ tool mà vẫn cần thử lại")
	}
	sh.Close()
	select {
	case <-changed:
		t.Fatal("không được báo đổi danh sách")
	case <-time.After(200 * time.Millisecond):
	}

	var starts atomic.Int32
	o2 := o
	o2.Dir = t.TempDir()
	o2.RetryDelay = 300 * time.Millisecond
	o2.StartWait = 100 * time.Millisecond
	o2.Start = func(context.Context, string) (Info, error) { starts.Add(1); return Info{}, errors.New("không lên") }
	sh2, err := NewShim(o2)
	if err != nil {
		t.Fatal(err)
	}
	sh2.Start()
	sh2.Ready(context.Background())
	sh2.Close()
	time.Sleep(500 * time.Millisecond)
	if n := starts.Load(); n != 1 {
		t.Fatalf("đã Close mà vẫn thử lại: %d lần khởi động", n)
	}
}
