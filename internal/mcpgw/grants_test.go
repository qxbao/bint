package mcpgw

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"bint/internal/spool"
	"bint/internal/ulid"
)

// repoWithGrants dựng repo đã init Bint, khai mọi service trong mcp.services và grants theo yaml (thụt hai khoảng trắng).
func repoWithGrants(t *testing.T, grants string) (string, string) {
	t.Helper()
	cfg := allServicesConfig()
	if grants != "" {
		cfg += "  grants:\n" + grants
	}
	repo := evalDir(t, newRepoCfg(t, true, cfg))
	return repo, markInit(t, repo).Runtime
}

func (c *client) toolNames() []string {
	c.h.t.Helper()
	m := c.rpc(900, "tools/list", nil)
	var r struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(m.Result, &r); err != nil {
		c.h.t.Fatalf("tools/list: %s", m.Result)
	}
	var out []string
	for _, tl := range r.Tools {
		out = append(out, tl.Name)
	}
	return out
}

func bindingFor(t *testing.T, runtime, role string, readOnly bool) (runID, taskID string) {
	t.Helper()
	runID, taskID = ulid.New(), ulid.New()
	if _, err := WriteRunBinding(runtime, RunBinding{RunID: runID, TaskID: taskID, Step: "b-x", Role: role, ReadOnly: readOnly}); err != nil {
		t.Fatal(err)
	}
	return
}

// Role do daemon tự giải từ bản ghi lượt chạy; header X-Bint-Role giả bị bỏ qua. Bước chỉ đọc (b-review) với
// grants specmatic.* không thấy tool ghi vào cây làm việc.
func TestReviewerDoesNotSeeWriteTools(t *testing.T) {
	h := newHarness(t, nil)
	h.register("specmatic")
	repo, runtime := repoWithGrants(t, "    reviewer: [specmatic.*]\n    implementer: [specmatic.*]\n")
	runID, taskID := bindingFor(t, runtime, "reviewer", true)

	// Header role giả "implementer" (và interactive) không đổi được kết quả.
	for _, fake := range []string{"implementer", "interactive", "reviewer"} {
		c := h.open("specmatic", map[string]string{HeaderWorktree: repo, HeaderRunID: runID, HeaderTaskID: taskID, HeaderRole: fake})
		names := c.toolNames()
		if !slices.Equal(names, []string{"backward_compatibility_check"}) {
			t.Errorf("header role %q: tools/list của reviewer = %v, muốn chỉ backward_compatibility_check", fake, names)
		}
		for _, bad := range []string{"manage_mock_server", "run_contract_test", "run_resiliency_test"} {
			if slices.Contains(names, bad) {
				t.Errorf("reviewer thấy %s", bad)
			}
		}
	}

	// Cùng grants, bước ghi (implementer, không chỉ đọc): thấy đủ.
	runI, taskI := bindingFor(t, runtime, "implementer", false)
	c := h.open("specmatic", map[string]string{HeaderWorktree: repo, HeaderRunID: runI, HeaderTaskID: taskI})
	if names := c.toolNames(); len(names) != 3 || !slices.Contains(names, "manage_mock_server") {
		t.Errorf("implementer: %v", names)
	}
}

// Lời gọi ngoài quyền (nhóm không được cấp, tool bị loại vì bước chỉ đọc, tên đoán mò) bị từ chối khi gọi thẳng HTTP; upstream
// không nhận lời gọi; mcp.called ghi outcome denied với role đã giải.
func TestDeniedCallsNeverReachUpstream(t *testing.T) {
	h := newHarness(t, nil)
	h.register("specmatic")
	repo, runtime := repoWithGrants(t, "    reviewer: [specmatic.*]\n")
	runID, taskID := bindingFor(t, runtime, "reviewer", true)
	c := h.open("specmatic", map[string]string{HeaderWorktree: repo, HeaderRunID: runID, HeaderTaskID: taskID})

	for _, tool := range []string{"manage_mock_server", "run_contract_test", "tool_doan_mo", "../x", ""} {
		m, _ := c.call(1, tool, map[string]any{"echo": "x"})
		if tool == "" {
			if rpcCode(m) != codeInvalidParams {
				t.Errorf("tên rỗng: %s", m.Error)
			}
			continue
		}
		if rpcCode(m) != CodeNotGranted {
			t.Errorf("%q: mã %d (%s), muốn %d", tool, rpcCode(m), rpcErrText(m), CodeNotGranted)
		}
	}
	if n := len(h.recvs("specmatic", "tools/call")); n != 0 {
		t.Errorf("upstream nhận %d lời gọi bị từ chối", n)
	}
	// Tool được cấp vẫn chạy.
	if m, out := c.call(2, "backward_compatibility_check", nil); len(m.Error) > 0 || out.Tool != "backward_compatibility_check" {
		t.Errorf("tool được cấp: %s", m.Error)
	}
	evs := spoolFiles(t, filepath.Join(filepath.Dir(runtime), "spool"))
	denied := 0
	for _, ev := range evs {
		if ev.Payload["outcome"] == OutcomeDenied {
			denied++
			if ev.RunID != runID || ev.Payload["role"] != "reviewer" || ev.Payload["task_id"] != taskID {
				t.Errorf("sự kiện denied: run=%s payload=%v", ev.RunID, ev.Payload)
			}
		}
	}
	if denied != 4 {
		t.Errorf("%d sự kiện denied, muốn 4", denied)
	}
	_ = spool.MCPCalled
}

// run_id không có bản ghi (hoặc không phải ULID, hoặc task_id lệch) thì không cấp service nào; không run_id thì interactive (D4).
func TestRoleResolutionFailsClosed(t *testing.T) {
	h := newHarness(t, nil)
	h.register("codegraph")
	repo, runtime := repoWithGrants(t, "    implementer: [codegraph.*]\n")
	runID, taskID := bindingFor(t, runtime, "implementer", false)

	cases := map[string]map[string]string{
		"không có bản ghi":    {HeaderRunID: ulid.New(), HeaderRole: "implementer"},
		"không phải ULID":     {HeaderRunID: "../../etc/passwd"},
		"task_id lệch":        {HeaderRunID: runID, HeaderTaskID: ulid.New()},
		"run_id rỗng + role":  nil, // không run_id: interactive, được mọi service của dự án (D4) → mở được
		"bản ghi đúng, đủ id": {HeaderRunID: runID, HeaderTaskID: taskID},
	}
	for name, hdr := range cases {
		hd := map[string]string{HeaderWorktree: repo}
		for k, v := range hdr {
			hd[k] = v
		}
		c, resp := h.tryOpen("codegraph", hd)
		wantOpen := name == "run_id rỗng + role" || name == "bản ghi đúng, đủ id"
		if (c != nil) != wantOpen {
			t.Errorf("%s: mở được=%v, muốn %v (%s)", name, c != nil, wantOpen, resp)
		}
		if !wantOpen && !strings.Contains(resp, "-32015") {
			t.Errorf("%s: muốn mã -32015: %s", name, resp)
		}
	}
	if n := len(h.starts("codegraph")); n > 1 {
		t.Errorf("phiên bị từ chối không được spawn upstream: %d lần khởi động", n)
	}
}

// D4: không run_id thì role interactive, được mọi service có trong mcp.services VÀ đã đăng ký; không có trong dự án hoặc chưa đăng ký thì không.
func TestInteractiveIsProjectScoped(t *testing.T) {
	h := newHarness(t, nil)
	h.register("codegraph")
	h.register("specmatic")
	// Dự án chỉ khai codegraph.
	repo := evalDir(t, newRepoCfg(t, true, "mcp:\n  services:\n    codegraph: {}\n    jira: {}\n"))
	c := h.open("codegraph", worktreeHdr(repo))
	if names := c.toolNames(); !slices.Contains(names, "codegraph_explore") {
		t.Errorf("interactive: %v", names)
	}
	if _, msg := h.tryOpen("specmatic", worktreeHdr(repo)); !strings.Contains(msg, "-32015") {
		t.Errorf("specmatic đã đăng ký nhưng dự án không khai: %s", msg)
	}
	if _, msg := h.tryOpen("jira", worktreeHdr(repo)); !strings.Contains(msg, "chưa đăng ký") {
		t.Errorf("jira khai trong dự án nhưng chưa đăng ký: %s", msg)
	}
	// Không có worktree: không biết dự án nào → không cấp.
	if _, msg := h.tryOpen("codegraph", map[string]string{HeaderWorktree: noWorktree}); msg == "" {
		t.Error("thiếu worktree phải bị từ chối")
	}
	// Repo không có mcp.services: interactive không được gì.
	bare := evalDir(t, newRepoCfg(t, false, ""))
	if c, msg := h.tryOpen("codegraph", worktreeHdr(bare)); c != nil || !strings.Contains(msg, "-32015") {
		t.Errorf("repo không khai service: %s", msg)
	}
	// grants.interactive khai báo thì theo khai báo.
	narrow := evalDir(t, newRepoCfg(t, false, allServicesConfig()+"  grants:\n    interactive: [codegraph.explore]\n"))
	cn := h.open("codegraph", worktreeHdr(narrow))
	if names := cn.toolNames(); !slices.Equal(names, []string{"codegraph_explore"}) {
		t.Errorf("grants.interactive: %v", names)
	}
}

// Cache tools/list lưu trên đĩa theo (service, revision): daemon khởi động lại mở phiên không spawn upstream.
func TestToolsCacheSurvivesDaemonRestart(t *testing.T) {
	h := newHarness(t, nil)
	h.register("codegraph")
	repo := evalDir(t, newRepo(t, false))
	c := h.open("codegraph", worktreeHdr(repo))
	if n := len(h.starts("codegraph")); n != 1 {
		t.Fatalf("service chưa chạy lần nào: upstream spawn %d lần, muốn đúng 1", n)
	}
	c.close()
	files, _ := filepath.Glob(filepath.Join(h.dir, ToolsCacheDirName, "codegraph-*.json"))
	if len(files) != 1 {
		t.Fatalf("cache đĩa: %v", files)
	}
	if st, err := os.Stat(files[0]); err != nil || (!isWindows && st.Mode().Perm() != 0o600) {
		t.Errorf("quyền tệp cache: %v %v", st, err)
	}

	h.d.Close()
	d2, err := Start(Options{Dir: h.dir, Version: "test", Machine: h.loadMachine, Backoff: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d2.Close)
	h.d = d2
	c2 := h.open("codegraph", worktreeHdr(repo))
	if names := c2.toolNames(); !slices.Contains(names, "codegraph_explore") {
		t.Errorf("tools/list từ cache đĩa: %v", names)
	}
	if n := len(h.starts("codegraph")); n != 1 {
		t.Errorf("sau khi daemon khởi động lại, initialize + tools/list không được spawn upstream: %d lần", n)
	}

	// Revision đổi (đăng ký đổi) thì cache cũ không dùng: upstream được spawn lại để lấy danh sách.
	h.register("codegraph", "extra=1")
	h.open("codegraph", worktreeHdr(repo))
	if n := len(h.starts("codegraph")); n != 2 {
		t.Errorf("revision mới: upstream spawn %d lần, muốn 2", n)
	}
}
