package mcpgw

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// worktreeHdr là header ngữ cảnh tối thiểu của phiên cần worktree.
func worktreeHdr(root string) map[string]string { return map[string]string{HeaderWorktree: root} }

// 6 client cùng id JSON-RPC, độ trễ khác nhau, một upstream: không lẫn phản hồi, id tới upstream là gw_<gen>_<seq>_<phiên>.
func TestSixClientsSameID(t *testing.T) {
	h := newHarness(t, nil)
	h.register("codegraph")
	repo := evalDir(t, newRepo(t, false))
	const n = 6
	clients := make([]*client, n)
	for i := range clients {
		clients[i] = h.open("codegraph", worktreeHdr(repo))
	}
	type res struct {
		m   rpcMsg
		out toolOut
	}
	got := make([]res, n)
	var wg sync.WaitGroup
	for i, c := range clients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Cùng id "7" ở cả 6 phiên; client 0 chậm nhất để phản hồi tới ngược thứ tự gửi.
			m, out := c.call(7, "codegraph_explore", map[string]any{"echo": fmt.Sprintf("c%d", i), "sleep_ms": (n - i) * 60})
			got[i] = res{m, out}
		}()
	}
	wg.Wait()
	pids := map[int]bool{}
	seen := map[string]bool{}
	for i, r := range got {
		if len(r.m.Error) > 0 {
			t.Fatalf("client %d: %s", i, r.m.Error)
		}
		if string(r.m.ID) != "7" {
			t.Errorf("client %d nhận id %s, muốn 7 (id gốc phải được trả lại)", i, r.m.ID)
		}
		if r.out.Echo != fmt.Sprintf("c%d", i) {
			t.Errorf("client %d nhận phản hồi của %q", i, r.out.Echo)
		}
		re := regexp.MustCompile(`^gw_1_\d+_` + clients[i].sid + `$`)
		if !re.MatchString(r.out.ID) {
			t.Errorf("client %d: upstream nhận id %q, muốn gw_1_<seq>_%s", i, r.out.ID, clients[i].sid)
		}
		if seen[r.out.ID] {
			t.Errorf("id %q trùng giữa hai client", r.out.ID)
		}
		seen[r.out.ID] = true
		pids[r.out.PID] = true
	}
	if len(pids) != 1 {
		t.Errorf("%d tiến trình upstream, muốn 1", len(pids))
	}
	if s := h.starts("codegraph"); len(s) != 1 {
		t.Errorf("upstream khởi động %d lần, muốn 1", len(s))
	}
}

// notifications/cancelled tới upstream với id đã đổi; initialize/tools/list của phiên sau lấy từ cache (một initialize mỗi generation).
func TestCancelledAndCache(t *testing.T) {
	h := newHarness(t, nil)
	h.register("codegraph")
	repo := evalDir(t, newRepo(t, false))
	a := h.open("codegraph", worktreeHdr(repo))
	b := h.open("codegraph", worktreeHdr(repo))
	c := h.open("codegraph", worktreeHdr(repo))
	for _, cl := range []*client{b, c} {
		m := cl.rpc(2, "tools/list", map[string]any{})
		if !strings.Contains(string(m.Result), "codegraph_explore") {
			t.Errorf("tools/list: %s", m.Result)
		}
	}
	if n := len(h.recvs("codegraph", "initialize")); n != 1 {
		t.Errorf("upstream nhận %d initialize, muốn 1 cho 3 phiên", n)
	}
	if n := len(h.recvs("codegraph", "tools/list")); n != 1 {
		t.Errorf("upstream nhận %d tools/list, muốn 1", n)
	}

	done := make(chan rpcMsg, 1)
	go func() {
		m, _ := a.call("c1", "codegraph_explore", map[string]any{"sleep_ms": 10000})
		done <- m
	}()
	var upID string
	eventually(t, "upstream nhận tools/call", 5*time.Second, func() bool {
		r := h.recvs("codegraph", "tools/call")
		if len(r) == 0 {
			return false
		}
		upID, _ = r[0]["id"].(string)
		return true
	})
	resp, _ := a.post(map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": map[string]any{"requestId": "c1", "reason": "thôi"}})
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("notification: HTTP %d, muốn 202", resp.StatusCode)
	}
	select {
	case m := <-done:
		// Gateway kết thúc lời gọi ngay khi nhận cancelled (không chờ phản hồi của upstream): lỗi là đủ.
		if len(m.Error) == 0 {
			t.Errorf("lời gọi bị huỷ phải kết thúc bằng lỗi, nhận %s", m.Result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("lời gọi không kết thúc sau notifications/cancelled")
	}
	// Gateway kết thúc lời gọi trước khi ghi cancelled tới upstream (ghi bất đồng bộ), nên phải chờ.
	var can []map[string]any
	eventually(t, "upstream nhận notifications/cancelled", 5*time.Second, func() bool {
		can = h.recvs("codegraph", "notifications/cancelled")
		return len(can) > 0
	})
	if len(can) != 1 || can[0]["request_id"] != upID || !strings.HasPrefix(upID, "gw_1_") {
		t.Errorf("upstream nhận cancelled %v, muốn requestId = id đã đổi %q", can, upID)
	}
	// id không biết thì bỏ qua, không gửi tới upstream.
	a.post(map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": map[string]any{"requestId": "khong-co"}})
	time.Sleep(50 * time.Millisecond)
	if n := len(h.recvs("codegraph", "notifications/cancelled")); n != 1 {
		t.Errorf("cancelled của id lạ bị chuyển tới upstream (%d)", n)
	}
}

// roots/list của upstream được gateway trả gốc worktree; sampling/elicitation bị từ chối bằng lỗi JSON-RPC.
func TestServerRequests(t *testing.T) {
	h := newHarness(t, nil)
	h.register("codegraph")
	repo := evalDir(t, newRepo(t, false))
	c := h.open("codegraph", worktreeHdr(repo))
	m, out := c.call(1, "codegraph_explore", map[string]any{"roots": true, "sampling": true})
	if len(m.Error) > 0 {
		t.Fatalf("lỗi: %s", m.Error)
	}
	var roots struct {
		Roots []struct{ URI, Name string } `json:"roots"`
	}
	if err := json.Unmarshal(out.Extra["roots"], &roots); err != nil || len(roots.Roots) != 1 {
		t.Fatalf("roots/list trả %s", out.Extra["roots"])
	}
	if want := fileURI(repo); roots.Roots[0].URI != want {
		t.Errorf("roots/list = %q, muốn %q", roots.Roots[0].URI, want)
	}
	if !strings.HasPrefix(roots.Roots[0].URI, "file://") {
		t.Errorf("roots/list không phải file URI: %q", roots.Roots[0].URI)
	}
	for _, k := range []string{"sampling", "elicitation"} {
		var e rpcError
		if err := json.Unmarshal(out.Extra[k], &e); err != nil || e.Code != codeMethodNotFound {
			t.Errorf("%s: upstream nhận %s, muốn lỗi JSON-RPC %d", k, out.Extra[k], codeMethodNotFound)
		}
	}
}

// progressToken của client được đổi sang token của gateway và thông báo tiến độ của upstream đến SSE của phiên chủ.
func TestProgressRelay(t *testing.T) {
	h := newHarness(t, nil)
	h.register("codegraph")
	repo := evalDir(t, newRepo(t, false))
	c := h.open("codegraph", worktreeHdr(repo))
	req, _ := http.NewRequest(http.MethodGet, h.d.URL()+"/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+h.d.Token())
	req.Header.Set(headerMCPSess, c.sid)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("GET /mcp: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	lines := make(chan string, 16)
	go func() {
		buf := make([]byte, 4096)
		var acc string
		for {
			n, err := resp.Body.Read(buf)
			acc += string(buf[:n])
			for {
				i := strings.Index(acc, "\n\n")
				if i < 0 {
					break
				}
				lines <- acc[:i]
				acc = acc[i+2:]
			}
			if err != nil {
				return
			}
		}
	}()
	// Luồng thứ hai của cùng phiên bị từ chối.
	r2, _ := http.NewRequest(http.MethodGet, h.d.URL()+"/mcp", nil)
	r2.Header.Set("Authorization", "Bearer "+h.d.Token())
	r2.Header.Set(headerMCPSess, c.sid)
	if resp2, err := http.DefaultClient.Do(r2); err == nil {
		if resp2.StatusCode != http.StatusConflict {
			t.Errorf("GET thứ hai: %d, muốn 409", resp2.StatusCode)
		}
		resp2.Body.Close()
	}
	m := c.rpc(3, "tools/call", map[string]any{"name": "codegraph_explore", "arguments": map[string]any{"progress": true}, "_meta": map[string]any{"progressToken": "tok-1"}})
	if len(m.Error) > 0 {
		t.Fatal(string(m.Error))
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case l := <-lines:
			if strings.Contains(l, "notifications/progress") {
				if !strings.Contains(l, `"progressToken":"tok-1"`) {
					t.Errorf("progressToken không được đổi ngược: %s", l)
				}
				return
			}
		case <-deadline:
			t.Fatal("không thấy notifications/progress trên SSE")
		}
	}
}

// Frame từ upstream vượt giới hạn: upstream bị coi như chết, lời gọi sau chạy trên generation mới.
func TestFrameTooLarge(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.MaxFrame = 64 << 10 })
	h.register("codegraph")
	repo := evalDir(t, newRepo(t, false))
	c := h.open("codegraph", worktreeHdr(repo))
	m, _ := c.call(1, "codegraph_explore", map[string]any{"bigframe": 200 << 10})
	if rpcCode(m) != CodeUpstream {
		t.Fatalf("frame quá cỡ: %s, muốn lỗi upstream", m.Error)
	}
	_, out := c.call(2, "codegraph_explore", map[string]any{"echo": "sau"})
	if !strings.HasPrefix(out.ID, "gw_2_") || out.Echo != "sau" {
		t.Errorf("lời gọi sau: id %q (muốn generation 2), echo %q", out.ID, out.Echo)
	}
	if n := len(h.starts("codegraph")); n != 2 {
		t.Errorf("%d lần khởi động, muốn 2", n)
	}
}

// Hàng đợi ghi tới upstream đầy: upstream bị coi như chết.
func TestWriteQueueFull(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.WriteQueue = 2 })
	h.register("codegraph")
	repo := evalDir(t, newRepo(t, false))
	c := h.open("codegraph", worktreeHdr(repo))
	// Upstream ngừng đọc stdin sau lời gọi stall; các frame lớn kế tiếp lấp ống rồi hàng đợi.
	stalled := make(chan rpcMsg, 1)
	go func() {
		m, _ := c.call("stall", "codegraph_explore", map[string]any{"stall": true})
		stalled <- m
	}()
	eventually(t, "upstream nhận lời gọi stall", 5*time.Second, func() bool { return len(h.recvs("codegraph", "tools/call")) == 1 })
	big := strings.Repeat("a", 256<<10)
	var wg sync.WaitGroup
	errs := make(chan rpcMsg, 8)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m, _ := c.call(100+i, "codegraph_explore", map[string]any{"echo": big})
			errs <- m
		}()
	}
	wg.Wait()
	close(errs)
	failed := 0
	for m := range errs {
		if len(m.Error) > 0 {
			failed++
		}
	}
	if failed != 6 {
		t.Errorf("%d/6 lời gọi lỗi, muốn cả 6 khi upstream bị coi như chết", failed)
	}
	select {
	case m := <-stalled:
		if len(m.Error) == 0 {
			t.Error("lời gọi stall phải lỗi khi upstream chết")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("lời gọi stall không kết thúc")
	}
	_, out := c.call("sau", "codegraph_explore", map[string]any{"echo": "ok"})
	if !strings.HasPrefix(out.ID, "gw_2_") {
		t.Errorf("lời gọi sau chạy với id %q, muốn generation 2", out.ID)
	}
}

// Body POST vượt giới hạn bị từ chối với 413.
func TestBodyTooLarge(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.MaxBody = 1024 })
	h.register("codegraph")
	repo := evalDir(t, newRepo(t, false))
	c := h.open("codegraph", worktreeHdr(repo))
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"codegraph_explore","arguments":{"echo":%q}}}`, strings.Repeat("a", 4096))
	resp, _ := h.do(http.MethodPost, "/mcp", map[string]string{headerMCPSess: c.sid}, []byte(body))
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("body quá lớn: HTTP %d, muốn 413", resp.StatusCode)
	}
	if n := len(h.recvs("codegraph", "tools/call")); n != 0 {
		t.Errorf("body bị từ chối vẫn tới upstream (%d)", n)
	}
}

func TestSessionErrors(t *testing.T) {
	h := newHarness(t, nil)
	h.register("codegraph")
	// không có Mcp-Session-Id và không phải initialize: 400; phiên lạ: 404
	resp, _ := h.do(http.MethodPost, "/mcp", nil, []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if resp.StatusCode != 400 {
		t.Errorf("thiếu phiên: %d, muốn 400", resp.StatusCode)
	}
	resp, _ = h.do(http.MethodPost, "/mcp", map[string]string{headerMCPSess: "la"}, []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if resp.StatusCode != 404 {
		t.Errorf("phiên lạ: %d, muốn 404", resp.StatusCode)
	}
	// batch bị từ chối
	repo := evalDir(t, newRepo(t, false))
	c := h.open("codegraph", worktreeHdr(repo))
	resp, _ = h.do(http.MethodPost, "/mcp", map[string]string{headerMCPSess: c.sid}, []byte(`[{"jsonrpc":"2.0","id":1,"method":"ping"}]`))
	if resp.StatusCode != 400 {
		t.Errorf("batch: %d, muốn 400", resp.StatusCode)
	}
	// service chưa đăng ký, thiếu worktree
	if _, msg := h.tryOpen("jira", nil); !strings.Contains(msg, "chưa đăng ký") {
		t.Errorf("service chưa đăng ký: %s", msg)
	}
	if _, msg := h.tryOpen("codegraph", nil); !strings.Contains(msg, HeaderWorktree) {
		t.Errorf("thiếu worktree: %s", msg)
	}
	if _, msg := h.tryOpen("khong-co", nil); !strings.Contains(msg, "danh mục") {
		t.Errorf("service lạ: %s", msg)
	}
	// method lạ và ping
	if m := c.rpc(5, "ping", nil); string(m.Result) != "{}" {
		t.Errorf("ping: %s", m.Result)
	}
	if m := c.rpc(6, "sampling/createMessage", nil); rpcCode(m) != codeMethodNotFound {
		t.Errorf("method lạ: %s", m.Error)
	}
	c.close()
	if m := func() rpcMsg {
		_, b := c.post(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"})
		var m rpcMsg
		json.Unmarshal(b, &m)
		return m
	}(); rpcCode(m) == 0 && len(m.Result) > 0 {
		t.Error("phiên đã DELETE vẫn dùng được")
	}
}
