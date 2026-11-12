package mcpgw

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"bint/internal/config"
	"bint/internal/gitrepo"
	"bint/internal/state"
	"bint/internal/testutil"
)

// harness dựng daemon thật trên cổng động với upstream giả; mọi thứ nằm trong thư mục tạm (testutil.Isolate).
type harness struct {
	t       *testing.T
	d       *Daemon
	dir     string
	logDir  string
	secrets string

	mu      sync.Mutex
	machine config.MachineMCP
}

var fakeTools = map[string]string{
	"codegraph":       "codegraph_explore,codegraph_status",
	"specmatic":       "backward_compatibility_check,run_contract_test,manage_mock_server",
	"jira":            "jira_issue_get,jira_issue_create",
	"chrome-devtools": "take_snapshot,click",
	"deepwiki":        "read_wiki_contents",
}

func newHarness(t *testing.T, mod func(*Options)) *harness {
	t.Helper()
	testutil.Isolate(t)
	h := &harness{t: t, dir: t.TempDir(), logDir: t.TempDir()}
	h.secrets = filepath.Join(h.logDir, "secrets.env")
	h.machine = config.MachineMCP{Version: 1, Services: map[string]config.Registration{}, SecretsFile: h.secrets}
	opts := Options{Dir: h.dir, Version: "test", Machine: h.loadMachine, Backoff: time.Millisecond}
	if mod != nil {
		mod(&opts)
	}
	d, err := Start(opts)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	h.d = d
	t.Cleanup(d.Close)
	return h
}

func (h *harness) loadMachine() (config.MachineMCP, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	m := h.machine
	m.Services = map[string]config.Registration{}
	for k, v := range h.machine.Services {
		m.Services[k] = v
	}
	return m, nil
}

func (h *harness) logPath(svc string) string { return filepath.Join(h.logDir, svc+".jsonl") }

// register đăng ký service chạy bằng upstream giả; extra là tham số dòng lệnh thêm của upstream giả.
func (h *harness) register(svc string, extra ...string) config.Registration {
	args := append([]string{"__fake_upstream", "log=" + h.logPath(svc), "tools=" + fakeTools[svc]}, extra...)
	r := config.Registration{Command: os.Args[0], Args: args}
	h.setReg(svc, r)
	return r
}

func (h *harness) setReg(svc string, r config.Registration) {
	h.mu.Lock()
	h.machine.Services[svc] = r
	h.mu.Unlock()
}

// events đọc các dòng nhật ký của upstream giả (mọi tiến trình của service).
func (h *harness) events(svc string) []map[string]any {
	data, err := os.ReadFile(h.logPath(svc))
	if err != nil {
		return nil
	}
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if json.Unmarshal([]byte(l), &m) == nil {
			out = append(out, m)
		}
	}
	return out
}

func filterEv(evs []map[string]any, key, val string) []map[string]any {
	var out []map[string]any
	for _, e := range evs {
		if s, _ := e[key].(string); s == val {
			out = append(out, e)
		}
	}
	return out
}

func (h *harness) starts(svc string) []map[string]any { return filterEv(h.events(svc), "ev", "start") }

func (h *harness) recvs(svc, method string) []map[string]any {
	return filterEv(filterEv(h.events(svc), "ev", "recv"), "method", method)
}

// ---- client HTTP ----

func (h *harness) do(method, path string, hdr map[string]string, body []byte) (*http.Response, []byte) {
	h.t.Helper()
	req, err := http.NewRequest(method, h.d.URL()+path, bytes.NewReader(body))
	if err != nil {
		h.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+h.d.Token())
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

type client struct {
	h   *harness
	sid string
	hdr map[string]string
}

// open mở một phiên (initialize) cho service; lỗi thì dừng kiểm thử.
func (h *harness) open(svc string, hdr map[string]string) *client {
	h.t.Helper()
	c, resp := h.tryOpen(svc, hdr)
	if c == nil {
		h.t.Fatalf("initialize %s: %s", svc, resp)
	}
	return c
}

func (h *harness) tryOpen(svc string, hdr map[string]string) (*client, string) {
	h.t.Helper()
	hd := map[string]string{HeaderService: svc, "Content-Type": "application/json"}
	for k, v := range hdr {
		hd[k] = v
	}
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{
		"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "1"}}})
	resp, b := h.do(http.MethodPost, "/mcp", hd, body)
	var m rpcMsg
	_ = json.Unmarshal(b, &m)
	sid := resp.Header.Get(headerMCPSess)
	if resp.StatusCode != 200 || sid == "" || len(m.Error) > 0 {
		return nil, string(b)
	}
	return &client{h: h, sid: sid, hdr: hd}, string(b)
}

func (c *client) post(body any) (*http.Response, []byte) {
	c.h.t.Helper()
	b, _ := json.Marshal(body)
	return c.h.do(http.MethodPost, "/mcp", map[string]string{headerMCPSess: c.sid, "Content-Type": "application/json"}, b)
}

func (c *client) rpc(id any, method string, params any) rpcMsg {
	c.h.t.Helper()
	resp, b := c.post(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	var m rpcMsg
	if err := json.Unmarshal(b, &m); err != nil {
		c.h.t.Fatalf("%s: HTTP %d, phản hồi không phải JSON-RPC: %q", method, resp.StatusCode, b)
	}
	return m
}

type toolOut struct {
	PID           int                        `json:"pid"`
	Cwd           string                     `json:"cwd"`
	ID            string                     `json:"id"`
	Tool          string                     `json:"tool"`
	Args          map[string]json.RawMessage `json:"args"`
	MaxConcurrent int                        `json:"max_concurrent"`
	Echo          string                     `json:"echo"`
	Extra         map[string]json.RawMessage `json:"extra"`
}

// call gọi tool và giải khối text JSON của upstream giả; lỗi JSON-RPC trả qua rpcMsg.
func (c *client) call(id any, tool string, args any) (rpcMsg, toolOut) {
	c.h.t.Helper()
	m := c.rpc(id, "tools/call", map[string]any{"name": tool, "arguments": args})
	var out toolOut
	if len(m.Result) > 0 {
		var r struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		if json.Unmarshal(m.Result, &r) == nil && len(r.Content) > 0 {
			_ = json.Unmarshal([]byte(r.Content[0].Text), &out)
		}
	}
	return m, out
}

func (c *client) close() {
	c.h.t.Helper()
	resp, _ := c.h.do(http.MethodDelete, "/mcp", map[string]string{headerMCPSess: c.sid}, nil)
	if resp.StatusCode != http.StatusNoContent {
		c.h.t.Errorf("DELETE /mcp: HTTP %d", resp.StatusCode)
	}
}

func rpcCode(m rpcMsg) int {
	var e rpcError
	_ = json.Unmarshal(m.Error, &e)
	return e.Code
}

func rpcErrText(m rpcMsg) string {
	var e rpcError
	_ = json.Unmarshal(m.Error, &e)
	return e.Message
}

// ---- git ----

// newRepo tạo repo git có một commit, đã init Bint (có repo.json trong thư mục trạng thái) nếu initBint.
func newRepo(t *testing.T, initBint bool) string {
	t.Helper()
	dir := testutil.NewRepo(t, nil)
	if initBint {
		markInit(t, dir)
	}
	return dir
}

func markInit(t *testing.T, dir string) state.Paths {
	t.Helper()
	repo, err := gitrepo.Discover(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Load(repo.Toplevel, false)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := state.Resolve(repo, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.Ensure(repo); err != nil {
		t.Fatal(err)
	}
	return paths
}

// addWorktree tạo worktree git thứ hai của repo.
func addWorktree(t *testing.T, repo, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	testutil.Git(t, repo, "worktree", "add", "-q", "-b", name, p)
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

func evalDir(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func eventually(t *testing.T, what string, within time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("quá %s mà chưa thấy: %s", within, what)
}
