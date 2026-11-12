package mcpgw

import (
	"bint/internal/ulid"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"bint/internal/spool"
)

func spoolFiles(t *testing.T, dir string) []spool.Event {
	t.Helper()
	files, err := spool.Pending(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []spool.Event
	for _, f := range files {
		evs, err := spool.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, evs...)
	}
	return out
}

// Mỗi tools/call ghi một mcp.called vào spool của repo chứa worktree; không có đối số hay nội dung kết quả.
func TestRecordMCPCalled(t *testing.T) {
	h := newHarness(t, nil)
	h.register("codegraph")
	repo := evalDir(t, newRepoCfg(t, true, allServicesConfig()+"  grants:\n    implementer: [codegraph.*]\n"))
	paths := markInit(t, repo)
	runID, taskID := ulid.New(), ulid.New()
	bindRun(t, paths, RunBinding{RunID: runID, TaskID: taskID, Step: "b-feat", Role: "implementer"})
	c := h.open("codegraph", map[string]string{HeaderWorktree: repo, HeaderRunID: runID, HeaderTaskID: taskID, HeaderRole: "reviewer"}) // header role giả bị bỏ qua
	c.rpc(1, "tools/list", nil)
	c.rpc(2, "ping", nil)
	c.call(3, "codegraph_explore", map[string]any{"echo": "DOI-SO-BI-MAT"})
	c.call(4, "codegraph_status", map[string]any{"rpc_error": true})
	c.call(5, "ham_la", nil)

	evs := spoolFiles(t, paths.Spool)
	if len(evs) != 3 {
		t.Fatalf("%d sự kiện, muốn 3 (mỗi tools/call một sự kiện; tools/list và ping không ghi)", len(evs))
	}
	// Tên tệp spool là ULID nên thứ tự đọc không chắc theo thứ tự gọi (cùng mili giây): tra theo tên tool.
	byTool := map[string]spool.Event{}
	for _, ev := range evs {
		byTool[ev.Payload["tool"].(string)] = ev
	}
	e := byTool["codegraph_explore"]
	if e.Type != spool.MCPCalled || e.RunID != runID || e.Source == "" || e.Worktree != repo {
		t.Errorf("sự kiện: %+v", e)
	}
	want := map[string]any{"task_id": taskID, "role": "implementer", "service": "codegraph", "tool": "codegraph_explore", "group": "explore", "outcome": OutcomeOK}
	for k, v := range want {
		if e.Payload[k] != v {
			t.Errorf("payload[%s]=%v, muốn %v", k, e.Payload[k], v)
		}
	}
	if _, ok := e.Payload["duration_ms"].(float64); !ok {
		t.Errorf("thiếu duration_ms: %v", e.Payload)
	}
	if p := byTool["codegraph_status"].Payload; p["outcome"] != OutcomeRPCErr || p["group"] != "query" {
		t.Errorf("lỗi JSON-RPC của upstream: %v", p)
	}
	if p := byTool["ham_la"].Payload; p["group"] != "" || p["outcome"] != OutcomeDenied {
		t.Errorf("tool lạ phải bị từ chối và ghi nhận: %v", p)
	}
	files, _ := spool.Pending(paths.Spool)
	for _, f := range files {
		b, _ := os.ReadFile(f)
		if strings.Contains(string(b), "DOI-SO-BI-MAT") {
			t.Errorf("spool chứa đối số của lời gọi: %s", b)
		}
	}
	if h.d.Status().DroppedEvents != 0 {
		t.Error("không có sự kiện nào bị bỏ")
	}
}

func TestRecordSkips(t *testing.T) {
	h := newHarness(t, nil)
	h.register("codegraph")

	// Repo chưa init Bint: không ghi (không tạo thư mục trạng thái), đếm số sự kiện bỏ.
	raw := evalDir(t, newRepo(t, false))
	c := h.open("codegraph", map[string]string{HeaderWorktree: raw, HeaderHarness: "claude", HeaderSessionID: "s0"})
	c.call(1, "codegraph_explore", nil)
	if h.d.Status().DroppedEvents != 1 {
		t.Errorf("DroppedEvents=%d, muốn 1 (repo chưa init)", h.d.Status().DroppedEvents)
	}

	// Repo đã init nhưng phiên thiếu cả run_id lẫn cặp harness + session: không ghi.
	repo := evalDir(t, newRepo(t, true))
	paths := markInit(t, repo)
	for _, hdr := range []map[string]string{
		{HeaderWorktree: repo},
		{HeaderWorktree: repo, HeaderHarness: "claude"},
		{HeaderWorktree: repo, HeaderSessionID: "s1"},
	} {
		h.open("codegraph", hdr).call(1, "codegraph_explore", nil)
	}
	if n := len(spoolFiles(t, paths.Spool)); n != 0 {
		t.Errorf("%d sự kiện cho phiên thiếu định danh lượt, muốn 0", n)
	}
	if h.d.Status().DroppedEvents != 4 {
		t.Errorf("DroppedEvents=%d, muốn 4", h.d.Status().DroppedEvents)
	}

	// Có harness + session (không run_id): ghi, gắn lượt theo cặp đó.
	h.open("codegraph", map[string]string{HeaderWorktree: repo, HeaderHarness: "claude", HeaderSessionID: "s1"}).call(1, "codegraph_explore", nil)
	evs := spoolFiles(t, paths.Spool)
	if len(evs) != 1 || evs[0].Harness != "claude" || evs[0].SessionID != "s1" || evs[0].RunID != "" {
		t.Errorf("sự kiện theo harness + session: %+v", evs)
	}
}

// internal/mcpgw không phụ thuộc gói store hay driver SQLite: daemon không mở CSDL.
func TestNoSQLiteDependency(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", ".")
	cmd.Env = append(os.Environ(), "GOFLAGS=")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Skipf("go list không chạy được: %v\n%s", err, out)
	}
	for _, dep := range strings.Fields(string(out)) {
		l := strings.ToLower(dep)
		if strings.Contains(l, "bint/internal/store") || strings.Contains(l, "sqlite") || l == "database/sql" || strings.Contains(l, "bint/internal/app") {
			t.Errorf("internal/mcpgw phụ thuộc %s", dep)
		}
	}
}

// Hook không bao giờ khởi động daemon gateway: mã trong internal/harness và internal/app/hook.go không tham chiếu mcpgw
// hay lệnh gateway [k:01M746NPTW1B304MRQQPBCAFAM].
func TestHookDoesNotReferenceGateway(t *testing.T) {
	root := filepath.Join("..", "harness")
	files := []string{filepath.Join("..", "app", "hook.go")}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 5 {
		t.Fatalf("chỉ thấy %d tệp để quét: %v", len(files), files)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if hit := gatewayRef(string(data)); hit != "" {
			t.Errorf("%s tham chiếu gateway: %q", f, hit)
		}
	}
}

// gatewayRef trả đoạn đầu tiên trong src tham chiếu gói mcpgw hoặc lệnh gateway của bint mcp.
func gatewayRef(src string) string {
	for _, needle := range []string{"internal/mcpgw", "mcpgw.", "mcp gateway", "MCPGateway", `"gateway"`} {
		if strings.Contains(src, needle) {
			return needle
		}
	}
	return ""
}

func TestGatewayRefScanner(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want bool
	}{
		{`import "bint/internal/mcpgw"`, true},
		{`x := mcpgw.Start(opts)`, true},
		{`exec.Command("bint", "mcp", "gateway", "start")`, true},
		{`// chạy bint mcp gateway start`, true},
		{`app.MCPGatewayStart(ctx, o)`, true},
		{`import "bint/internal/spool"`, false},
	} {
		if got := gatewayRef(tc.src) != ""; got != tc.want {
			t.Errorf("%q: bắt=%v, muốn %v", tc.src, got, tc.want)
		}
	}
}

// status sau một lời gọi: PID, generation, lời gọi gần nhất, RSS (Linux), số phiên; qua AdminClient như CLI.
func TestStatusAfterCall(t *testing.T) {
	h := newHarness(t, nil)
	writeSecrets(t, h, "JIRA_MCP_URL=http://j", "JIRA_MCP_USERNAME=u", "JIRA_MCP_PASSWORD=pw-trong-status")
	h.register("codegraph")
	h.register("jira")
	repo := evalDir(t, newRepo(t, false))
	c := h.open("codegraph", worktreeHdr(repo))
	_, out := c.call(1, "codegraph_explore", nil)
	h.open("jira", nil).call(1, "jira_issue_get", nil)

	ac, err := Connect(h.dir)
	if err != nil {
		t.Fatal(err)
	}
	st, err := ac.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if st.PID != os.Getpid() || st.Sessions != 2 || len(st.Upstreams) != 2 {
		t.Fatalf("status: %+v", st)
	}
	var cg UpstreamStatus
	for _, u := range st.Upstreams {
		if u.Service == "codegraph" {
			cg = u
		}
	}
	if !cg.Running || cg.PID != out.PID || cg.Generation != 1 || cg.Sessions != 1 || cg.Share != "worktree" {
		t.Errorf("codegraph: %+v", cg)
	}
	if cg.LastCall == nil || cg.LastCall.Tool != "codegraph_explore" || cg.LastCall.Outcome != OutcomeOK {
		t.Errorf("lời gọi gần nhất: %+v", cg.LastCall)
	}
	if runtime.GOOS == "linux" && (cg.RSSKB == 0 || cg.PSSKB == 0) {
		t.Errorf("RSS/PSS = %d/%d, muốn khác 0 trên Linux", cg.RSSKB, cg.PSSKB)
	}
	b, _ := ac.HTTP.Get(ac.Info.URL() + "/admin/status") // không token: 401
	if b.StatusCode != 401 {
		t.Errorf("status không token: %d", b.StatusCode)
	}
	raw := mustJSON(st)
	if strings.Contains(raw, "pw-trong-status") || strings.Contains(raw, ac.Info.Token) {
		t.Errorf("status lộ bí mật: %s", raw)
	}
	// restart qua admin: dừng upstream của service, trạng thái chạy được lại.
	rr, err := ac.Restart(t.Context(), "codegraph")
	if err != nil || rr.Stopped != 1 {
		t.Errorf("restart: %+v %v", rr, err)
	}
	if _, err := ac.Restart(t.Context(), "khong-co"); err == nil {
		t.Error("restart service lạ phải lỗi")
	}
	_, out2 := c.call(2, "codegraph_explore", nil)
	if out2.PID == out.PID || !strings.HasPrefix(out2.ID, "gw_2_") {
		t.Errorf("sau restart: pid %d id %q", out2.PID, out2.ID)
	}
}
