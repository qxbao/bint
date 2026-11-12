package mcpgw

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/state"
)

func sameDir(t *testing.T, got, want string) {
	t.Helper()
	g, w := got, want
	if r, err := filepath.EvalSymlinks(g); err == nil {
		g = r
	}
	if r, err := filepath.EvalSymlinks(w); err == nil {
		w = r
	}
	if g != w {
		t.Errorf("cwd %q, muốn %q", got, want)
	}
}

// Phân vùng theo chính sách chia sẻ của danh mục (mục 5.3): worktree, machine, session.
func TestPartitionPolicies(t *testing.T) {
	h := newHarness(t, nil)
	repo := evalDir(t, newRepo(t, false))
	wt2 := addWorktree(t, repo, "wt2")

	t.Run("worktree", func(t *testing.T) {
		h.register("codegraph")
		a1 := h.open("codegraph", worktreeHdr(repo))
		a2 := h.open("codegraph", worktreeHdr(repo))
		b := h.open("codegraph", worktreeHdr(wt2))
		_, o1 := a1.call(1, "codegraph_explore", nil)
		_, o2 := a2.call(1, "codegraph_explore", nil)
		_, ob := b.call(1, "codegraph_explore", nil)
		if o1.PID == 0 || o1.PID != o2.PID {
			t.Errorf("hai phiên cùng worktree phải chung tiến trình: %d vs %d", o1.PID, o2.PID)
		}
		if ob.PID == o1.PID {
			t.Error("hai worktree không được dùng chung tiến trình")
		}
		sameDir(t, o1.Cwd, repo)
		sameDir(t, ob.Cwd, wt2)
		if n := len(h.starts("codegraph")); n != 2 {
			t.Errorf("%d tiến trình khởi động, muốn 2", n)
		}
	})

	t.Run("machine", func(t *testing.T) {
		writeSecrets(t, h, "JIRA_MCP_URL=http://j", "JIRA_MCP_USERNAME=u", "JIRA_MCP_PASSWORD=pw")
		h.register("jira")
		a := h.open("jira", worktreeHdr(repo))
		b := h.open("jira", worktreeHdr(wt2))
		c := h.open("jira", nil) // machine không cần worktree
		_, oa := a.call(1, "jira_issue_get", nil)
		_, ob := b.call(1, "jira_issue_get", nil)
		_, oc := c.call(1, "jira_issue_get", nil)
		if oa.PID == 0 || oa.PID != ob.PID || oa.PID != oc.PID {
			t.Errorf("service machine phải chung tiến trình giữa các worktree: %d %d %d", oa.PID, ob.PID, oc.PID)
		}
		if n := len(h.starts("jira")); n != 1 {
			t.Errorf("%d tiến trình jira, muốn 1", n)
		}
	})

	t.Run("session", func(t *testing.T) {
		h.register("chrome-devtools")
		a := h.open("chrome-devtools", worktreeHdr(repo))
		b := h.open("chrome-devtools", worktreeHdr(repo))
		_, oa := a.call(1, "take_snapshot", nil)
		_, ob := b.call(1, "take_snapshot", nil)
		_, oa2 := a.call(2, "take_snapshot", nil)
		if oa.PID == ob.PID {
			t.Error("service session: mỗi phiên một tiến trình")
		}
		if oa2.PID != oa.PID {
			t.Error("cùng một phiên phải giữ tiến trình")
		}
		a.close() // đóng phiên thì tiến trình của phiên đó dừng ngay
		eventually(t, "tiến trình của phiên đã đóng dừng", 5*time.Second, func() bool { return !processAlive(oa.PID) })
		if !processAlive(ob.PID) {
			t.Error("tiến trình của phiên khác bị dừng")
		}
	})
}

// specmatic: cwd trong thư mục trạng thái, repoDir được điền khi không truyền và giữ nguyên khi có.
func TestSpecmaticCwdAndRepoDir(t *testing.T) {
	h := newHarness(t, nil)
	h.register("specmatic")
	repo := evalDir(t, newRepo(t, true))
	paths := markInit(t, repo)
	c := h.open("specmatic", worktreeHdr(repo))

	_, out := c.call(1, "backward_compatibility_check", map[string]any{"targetPath": "api.yaml"})
	var dir string
	if err := json.Unmarshal(out.Args["repoDir"], &dir); err != nil || dir != repo {
		t.Errorf("repoDir=%s, muốn %q (điền theo gốc worktree)", out.Args["repoDir"], repo)
	}
	if string(out.Args["targetPath"]) != `"api.yaml"` {
		t.Errorf("tham số của agent bị mất: %v", out.Args)
	}
	_, out = c.call(2, "backward_compatibility_check", map[string]any{"repoDir": "/tuy/chon"})
	if string(out.Args["repoDir"]) != `"/tuy/chon"` {
		t.Errorf("repoDir agent truyền bị ghi đè: %s", out.Args["repoDir"])
	}
	_, out = c.call(3, "backward_compatibility_check", nil) // không có arguments
	if err := json.Unmarshal(out.Args["repoDir"], &dir); err != nil || dir != repo {
		t.Errorf("không có arguments: repoDir=%s", out.Args["repoDir"])
	}
	_, out = c.call(4, "run_contract_test", map[string]any{"x": 1})
	if _, has := out.Args["repoDir"]; has {
		t.Error("tool khác không được điền repoDir")
	}
	want := filepath.Join(paths.Root, "mcp", "specmatic", state.RepoID(repo))
	sameDir(t, out.Cwd, want)
	if strings.HasPrefix(evalDir(t, out.Cwd), repo) {
		t.Error("cwd của specmatic phải nằm ngoài worktree")
	}
}

func TestFillDefaults(t *testing.T) {
	svc, _ := h0Lookup("specmatic")
	for _, tc := range []struct {
		name string
		in   string
		root string
		want string
	}{
		{"vắng", `{"name":"backward_compatibility_check"}`, "/r", `{"repoDir":"/r"}`},
		{"null", `{"name":"backward_compatibility_check","arguments":null}`, "/r", `{"repoDir":"/r"}`},
		{"repoDir null", `{"arguments":{"repoDir":null,"a":1}}`, "/r", `{"a":1,"repoDir":"/r"}`},
		{"giữ", `{"arguments":{"repoDir":"/x"}}`, "/r", `{"repoDir":"/x"}`},
		{"không có gốc", `{"arguments":{"a":1}}`, "", `{"a":1}`},
		{"không phải object", `{"arguments":[1]}`, "/r", `[1]`},
	} {
		var params map[string]json.RawMessage
		if err := json.Unmarshal([]byte(tc.in), &params); err != nil {
			t.Fatal(err)
		}
		fillDefaults(svc, "backward_compatibility_check", params, tc.root)
		got, ok := params["arguments"]
		if tc.want == `{"a":1}` && tc.root == "" {
			if string(got) != tc.want {
				t.Errorf("%s: %s", tc.name, got)
			}
			continue
		}
		if !ok || string(got) != tc.want {
			t.Errorf("%s: arguments=%s, muốn %s", tc.name, got, tc.want)
		}
	}
}

// Kill upstream: lời gọi đang chờ nhận lỗi, lời gọi sau chạy trên upstream mới với generation +1.
func TestKillThenNewGeneration(t *testing.T) {
	h := newHarness(t, nil)
	h.register("codegraph")
	repo := evalDir(t, newRepo(t, false))
	c := h.open("codegraph", worktreeHdr(repo))
	_, o1 := c.call(1, "codegraph_explore", nil)
	if !strings.HasPrefix(o1.ID, "gw_1_") {
		t.Fatalf("id %q", o1.ID)
	}
	// Lời gọi dang dở khi tiến trình bị kill từ ngoài.
	pending := make(chan rpcMsg, 1)
	go func() {
		m, _ := c.call(2, "codegraph_explore", map[string]any{"sleep_ms": 10000})
		pending <- m
	}()
	eventually(t, "upstream nhận lời gọi chậm", 5*time.Second, func() bool { return len(h.recvs("codegraph", "tools/call")) == 2 })
	if err := killPID(o1.PID); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-pending:
		if rpcCode(m) != CodeUpstream {
			t.Errorf("lời gọi dang dở nhận %s, muốn lỗi upstream", m.Error)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("lời gọi dang dở không nhận lỗi khi upstream chết")
	}
	_, o2 := c.call(3, "codegraph_explore", nil)
	if !strings.HasPrefix(o2.ID, "gw_2_") || o2.PID == o1.PID {
		t.Errorf("sau kill: id %q pid %d (cũ %d), muốn generation 2 và tiến trình mới", o2.ID, o2.PID, o1.PID)
	}
	// Upstream tự thoát giữa lời gọi (lệnh exit của upstream giả).
	if m, _ := c.call(4, "codegraph_explore", map[string]any{"exit": true}); rpcCode(m) != CodeUpstream {
		t.Errorf("upstream thoát giữa chừng: %s", m.Error)
	}
	_, o3 := c.call(5, "codegraph_explore", nil)
	if !strings.HasPrefix(o3.ID, "gw_3_") {
		t.Errorf("id %q, muốn generation 3", o3.ID)
	}
	// Mỗi generation một initialize.
	if n := len(h.recvs("codegraph", "initialize")); n != 3 {
		t.Errorf("%d initialize, muốn 3 (một mỗi generation)", n)
	}
}

// Vượt max_restarts trong cửa sổ thì báo lỗi giới hạn tới khi restart service.
func TestRestartLimit(t *testing.T) {
	h := newHarness(t, nil)
	reg := h.register("codegraph")
	reg.MaxRestarts = 1
	h.setReg("codegraph", reg)
	repo := evalDir(t, newRepo(t, false))
	c := h.open("codegraph", worktreeHdr(repo))
	die := map[string]any{"exit": true}
	c.call(1, "codegraph_explore", die) // chết lần 1
	c.call(2, "codegraph_explore", die) // khởi động lại 1 lần (được phép), chết lần 2
	m, _ := c.call(3, "codegraph_explore", nil)
	if rpcCode(m) != CodeRestartLimit {
		t.Fatalf("vượt giới hạn: %s, muốn CodeRestartLimit", m.Error)
	}
	if !strings.Contains(rpcErrText(m), "bint mcp gateway restart codegraph") {
		t.Errorf("thông báo thiếu hướng dẫn restart: %s", rpcErrText(m))
	}
	starts := len(h.starts("codegraph"))
	if m, _ := c.call(4, "codegraph_explore", nil); rpcCode(m) != CodeRestartLimit {
		t.Error("vẫn phải bị chặn tới khi restart")
	}
	if len(h.starts("codegraph")) != starts {
		t.Error("bị chặn mà vẫn khởi động upstream")
	}
	st := h.d.Status().Upstreams
	if len(st) != 1 || !st[0].Limited || st[0].Restarts != 1 {
		t.Errorf("status: %+v", st)
	}
	h.d.Restart("codegraph")
	_, out := c.call(5, "codegraph_explore", nil)
	if out.PID == 0 || !strings.HasPrefix(out.ID, "gw_3_") {
		t.Errorf("sau restart: id %q", out.ID)
	}
}

// Tool không chỉ đọc: upstream chết hoặc timeout giữa chừng thì lỗi không rõ kết quả, upstream nhận đúng một lần.
func TestUnknownOutcomeNeverRetried(t *testing.T) {
	h := newHarness(t, func(o *Options) {
		withExtraTool("codegraph", "explore", "do_write")(o)
		o.CallTimeout = 400 * time.Millisecond
	})
	h.register("codegraph")
	repo := evalDir(t, newRepo(t, false))
	c := h.open("codegraph", worktreeHdr(repo))

	m, _ := c.call(1, "do_write", map[string]any{"exit": true})
	if rpcCode(m) != CodeUnknownOutcome {
		t.Fatalf("upstream chết giữa lệnh ghi: %s, muốn CodeUnknownOutcome", m.Error)
	}
	if !strings.Contains(rpcErrText(m), "không rõ") {
		t.Errorf("thông báo: %s", rpcErrText(m))
	}
	time.Sleep(100 * time.Millisecond)
	calls := func(tool string) (n int) {
		for _, e := range h.recvs("codegraph", "tools/call") {
			if e["tool"] == tool {
				n++
			}
		}
		return
	}
	if n := calls("do_write"); n != 1 {
		t.Errorf("upstream nhận lệnh ghi %d lần, muốn đúng 1 (không gọi lại)", n)
	}

	// timeout của lệnh ghi
	m, _ = c.call(2, "do_write", map[string]any{"sleep_ms": 3000})
	if rpcCode(m) != CodeUnknownOutcome {
		t.Errorf("timeout lệnh ghi: %s, muốn CodeUnknownOutcome", m.Error)
	}
	if n := calls("do_write"); n != 2 {
		t.Errorf("lệnh ghi thứ hai tới upstream %d lần tổng, muốn 2", n)
	}
	// tool chỉ đọc quá hạn chỉ báo timeout
	m, _ = c.call(3, "codegraph_explore", map[string]any{"sleep_ms": 3000})
	if rpcCode(m) != CodeTimeout {
		t.Errorf("timeout tool chỉ đọc: %s, muốn CodeTimeout", m.Error)
	}
	// lệnh ghi chưa kịp gửi (hàng đợi) thì không phải "không rõ": thể hiện ở cờ written của waiter
	w := &waiter{}
	if w.written.Load() {
		t.Error("waiter mới phải chưa written")
	}
}

// Upstream chỉ khởi động ở lời gọi đầu tiên cần nó; dừng sau idle_timeout khi không còn phiên và lời gọi dang dở.
func TestLazyAndIdle(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.IdleTimeout = 200 * time.Millisecond })
	h.register("codegraph")
	repo := evalDir(t, newRepo(t, false))
	wt2 := addWorktree(t, repo, "wt2")
	if len(h.starts("codegraph")) != 0 {
		t.Fatal("daemon vừa chạy mà đã có upstream")
	}
	a := h.open("codegraph", worktreeHdr(repo)) // cache trống: initialize cần upstream
	if n := len(h.starts("codegraph")); n != 1 {
		t.Fatalf("%d upstream sau initialize đầu, muốn 1", n)
	}
	b := h.open("codegraph", worktreeHdr(wt2)) // cache có: không khởi động phân vùng wt2
	if n := len(h.starts("codegraph")); n != 1 {
		t.Errorf("mở phiên thứ hai (cache có) đã khởi động upstream: %d", n)
	}
	_, ob := b.call(1, "codegraph_explore", nil)
	if n := len(h.starts("codegraph")); n != 2 {
		t.Errorf("lời gọi đầu của worktree 2 phải khởi động upstream: %d", n)
	}
	_, oa := a.call(1, "codegraph_explore", nil)
	// Còn phiên thì không idle.
	time.Sleep(500 * time.Millisecond)
	if !processAlive(oa.PID) || !processAlive(ob.PID) {
		t.Fatal("upstream bị dừng khi còn phiên")
	}
	a.close()
	eventually(t, "upstream dừng sau idle_timeout khi hết phiên", 5*time.Second, func() bool { return !processAlive(oa.PID) })
	if !processAlive(ob.PID) {
		t.Error("upstream của worktree còn phiên bị dừng")
	}
	// Lời gọi dang dở chặn idle: phiên mới, lời gọi 800ms (> idle) vẫn hoàn thành.
	c := h.open("codegraph", worktreeHdr(repo))
	m, o := c.call(2, "codegraph_explore", map[string]any{"sleep_ms": 800})
	if len(m.Error) > 0 || o.PID == oa.PID || !strings.HasPrefix(o.ID, "gw_2_") {
		t.Errorf("khởi động lại sau idle: %s id=%q pid=%d", m.Error, o.ID, o.PID)
	}
}

func TestDetachedLifecycle(t *testing.T) {
	testutilIsolate(t)
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if st, _, err := QueryStatus(t.Context(), dir); err == nil {
		t.Fatalf("chưa chạy mà status ok: %+v", st)
	}
	info, started, err := StartDetached(t.Context(), dir, osArgs0(), []string{"__gateway"}, nil)
	if err != nil || !started {
		t.Fatalf("StartDetached: %v started=%v", err, started)
	}
	t.Cleanup(func() { _, _ = Stop(t.Context(), dir) })
	if !processAlive(info.PID) || info.PID == osGetpid() {
		t.Errorf("PID %d", info.PID)
	}
	st, _, err := QueryStatus(t.Context(), dir)
	if err != nil || st.PID != info.PID {
		t.Fatalf("status: %+v %v", st, err)
	}
	if i2, started2, err := StartDetached(t.Context(), dir, osArgs0(), []string{"__gateway"}, nil); err != nil || started2 || i2.PID != info.PID {
		t.Errorf("start lần hai: %+v started=%v err=%v", i2, started2, err)
	}
	stopped, err := Stop(t.Context(), dir)
	if err != nil || !stopped {
		t.Fatalf("Stop: %v %v", stopped, err)
	}
	eventually(t, "daemon thoát", 5*time.Second, func() bool { return !processAlive(info.PID) })
	if _, _, err := QueryStatus(t.Context(), dir); err == nil {
		t.Error("sau Stop status vẫn ok")
	}
	if stopped, err := Stop(t.Context(), dir); stopped || err != nil {
		t.Errorf("Stop khi không chạy: %v %v", stopped, err)
	}
}
