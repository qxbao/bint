package cli_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"bint/internal/config"
	"bint/internal/testutil"
)

// Quản lý MCP của M1 qua CLI thật: list --json, grants, add/remove/check/index (không cần service thật).
func TestMCPManageEndToEnd(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	t.Setenv("PATH", t.TempDir()+string(os.PathListSeparator)+os.Getenv("PATH"))
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	// Khối mcp của dự án đặt ở lớp cục bộ.
	if err := os.WriteFile(filepath.Join(root, ".bint", "local.yml"), []byte(
		"mcp:\n  services: {codegraph: {}, specmatic: {required: false}}\n  grants:\n    planner: [codegraph.explore, specmatic.compat]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// list --json: 6 service, chưa đăng ký, dự án dùng codegraph (bắt buộc) và specmatic (tuỳ chọn).
	out, code := run(t, root, "", "mcp", "list", "--json")
	var list []struct {
		ID          string `json:"id"`
		Registered  bool   `json:"registered"`
		ProjectUses bool   `json:"project_uses"`
		Required    bool   `json:"required"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &list) != nil || len(list) != 6 {
		t.Fatalf("list --json: %d\n%s", code, out)
	}
	for _, s := range list {
		wantUse := s.ID == "codegraph" || s.ID == "specmatic"
		if s.Registered || s.ProjectUses != wantUse || (s.ID == "codegraph" && !s.Required) || (s.ID == "specmatic" && s.Required) {
			t.Errorf("list: %+v", s)
		}
	}
	if out, code := run(t, root, "", "mcp", "list"); code != 0 || !strings.Contains(out, "codegraph") || !strings.Contains(out, "bắt buộc") {
		t.Fatalf("list: %d\n%s", code, out)
	}

	// grants: role planner có tên tool tới agent; service chưa đăng ký bị báo; interactive mặc định rỗng.
	out, code = run(t, root, "", "mcp", "grants")
	if code != 0 || !strings.Contains(out, "cg_explore") || !strings.Contains(out, "spec_backward_compatibility_check") ||
		!strings.Contains(out, "chưa đăng ký") || !strings.Contains(out, "interactive") {
		t.Fatalf("grants: %d\n%s", code, out)
	}
	out, code = run(t, root, "", "mcp", "grants", "--role", "planner", "--json")
	var roles []struct {
		Role  string `json:"role"`
		Tools []struct {
			Agent string `json:"agent"`
		} `json:"tools"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &roles) != nil || len(roles) != 1 || roles[0].Role != "planner" || len(roles[0].Tools) != 2 {
		t.Fatalf("grants --role: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "mcp", "grants", "--role", "nobody"); code != 2 {
		t.Fatalf("role lạ phải là lỗi cú pháp: %d\n%s", code, out)
	}

	// add: PATH không có codegraph → in lệnh cài gợi ý, check fail nhưng add vẫn ghi đăng ký.
	out, code = run(t, root, "", "mcp", "add", "codegraph")
	if code != 0 || !strings.Contains(out, "npm i -g @colbymchenry/codegraph@1.6.2") || !strings.Contains(out, "fail") {
		t.Fatalf("add: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "mcp", "add", "gitnexus"); code != 2 || !strings.Contains(out, "không có trong danh mục") {
		t.Fatalf("add ngoài danh mục: %d\n%s", code, out)
	}
	// check service fail thì thoát khác 0.
	if out, code := run(t, root, "", "mcp", "check"); code == 0 || !strings.Contains(out, "codegraph") {
		t.Fatalf("check: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "mcp", "check", "specmatic"); code != 2 || !strings.Contains(out, "chưa đăng ký") {
		t.Fatalf("check service chưa đăng ký: %d\n%s", code, out)
	}
	// index: service khác codegraph bị từ chối.
	if out, code := run(t, root, "", "mcp", "index", "jira"); code != 2 {
		t.Fatalf("index jira: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "mcp", "remove", "codegraph"); code != 0 {
		t.Fatalf("remove: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "mcp", "remove", "codegraph"); code == 0 || !strings.Contains(out, "chưa đăng ký") {
		t.Fatalf("remove lần 2: %d\n%s", code, out)
	}
	// doctor: dự án khai báo codegraph mà máy chưa đăng ký, chưa có chỉ mục.
	out, _ = run(t, root, "", "doctor")
	if !strings.Contains(out, "MCP codegraph") || !strings.Contains(out, "bint mcp index codegraph") {
		t.Fatalf("doctor:\n%s", out)
	}
}

func TestMCPManageAgentForbidden(t *testing.T) {
	testutil.Isolate(t)
	asAgent(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	for _, args := range [][]string{{"add", "codegraph"}, {"remove", "codegraph"}, {"index", "codegraph"}} {
		out, code := run(t, root, "", append([]string{"mcp"}, args...)...)
		if code == 0 || !strings.Contains(out, "chỉ dành cho người dùng") {
			t.Errorf("mcp %s trong phiên agent: %d\n%s", args[0], code, out)
		}
	}
	if out, code := run(t, root, "", "mcp", "list"); code != 0 {
		t.Errorf("list trong phiên agent: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "mcp", "check"); code != 0 {
		t.Errorf("check trong phiên agent: %d\n%s", code, out)
	}
}

// bint mcp grants --role reviewer: tool ghi vào cây làm việc bị loại và có cảnh báo.
func TestMCPGrantsWarnsReadOnlyRole(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	if err := os.WriteFile(filepath.Join(root, ".bint", "local.yml"), []byte(
		"mcp:\n  services: {specmatic: {}}\n  grants:\n    reviewer: [specmatic.*]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := run(t, root, "", "mcp", "grants", "--role", "reviewer")
	if code != 0 || !strings.Contains(out, "spec_backward_compatibility_check") || !strings.Contains(out, "spec_manage_mock_server") ||
		!strings.Contains(out, "! role reviewer chạy ở bước chỉ đọc") {
		t.Fatalf("grants --role reviewer: %d\n%s", code, out)
	}
	// Dòng liệt kê tool không chứa tool ghi (tên chỉ xuất hiện trong cảnh báo).
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "spec_manage_mock_server") && !strings.Contains(l, "!") {
			t.Errorf("tool ghi vẫn được liệt kê như được cấp: %q", l)
		}
	}
}

// serveToolNames chạy `bint mcp serve` thật với hai yêu cầu và trả tên tool cùng instructions.
func serveToolNames(t *testing.T, root string, extra ...string) ([]string, string) {
	t.Helper()
	in := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n"
	out, code := run(t, root, in, append([]string{"mcp", "serve"}, extra...)...)
	if code != 0 {
		t.Fatalf("mcp serve: %d\n%s", code, out)
	}
	var names []string
	var instr string
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	for sc.Scan() {
		var m struct {
			ID     int `json:"id"`
			Result struct {
				Instructions string `json:"instructions"`
				Tools        []struct {
					Name string `json:"name"`
				} `json:"tools"`
			} `json:"result"`
		}
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue // dòng nhật ký của shim ra stderr
		}
		if m.ID == 1 {
			instr = m.Result.Instructions
		}
		for _, tl := range m.Result.Tools {
			names = append(names, tl.Name)
		}
	}
	return names, instr
}

// Chế độ đánh giá, BINT_MCP_NO_GATEWAY=1 và repo không có service khả dụng: `bint mcp serve` chỉ còn 6 tool của Bint và không
// chạm gateway (thư mục gateway không được tạo, không có daemon). Đối chứng: có service khả dụng thì shim khởi động daemon.
func TestMCPServeGatewayOptOuts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("dùng lệnh `true` làm upstream giả")
	}
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	local := filepath.Join(root, ".bint", "local.yml")
	if err := os.WriteFile(local, []byte("mcp:\n  services: {codegraph: {}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveMachineMCP(config.MachineMCP{Services: map[string]config.Registration{"codegraph": {Command: "true"}}}); err != nil {
		t.Fatal(err)
	}
	gwDir := filepath.Join(os.Getenv("BINT_STATE_DIR"), "gateway")
	noGateway := func(name string) {
		t.Helper()
		if _, err := os.Stat(gwDir); err == nil {
			t.Errorf("%s: không được tạo thư mục gateway %s", name, gwDir)
		}
	}

	// 1. BINT_MCP_NO_GATEWAY=1.
	t.Setenv("BINT_MCP_NO_GATEWAY", "1")
	if names, _ := serveToolNames(t, root); len(names) != 6 {
		t.Errorf("BINT_MCP_NO_GATEWAY: %v", names)
	}
	noGateway("BINT_MCP_NO_GATEWAY")
	t.Setenv("BINT_MCP_NO_GATEWAY", "")

	// 2. Chế độ đánh giá (--eval-config).
	cfg := filepath.Join(t.TempDir(), "ctx.json")
	items := t.TempDir()
	body, _ := json.Marshal(map[string]any{"items_dir": items, "root": root, "log": filepath.Join(t.TempDir(), "log.jsonl"), "max_tokens": 1000, "path_max_items": 3})
	if err := os.WriteFile(cfg, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if names, _ := serveToolNames(t, root, "--eval-config", cfg); len(names) != 6 {
		t.Errorf("--eval-config: %v", names)
	}
	noGateway("--eval-config")

	// 3. Repo không có service khả dụng: dự án không khai mcp.services.
	if err := os.WriteFile(local, []byte("harnesses: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if names, _ := serveToolNames(t, root); len(names) != 6 {
		t.Errorf("repo không khai service: %v", names)
	}
	noGateway("repo không khai service")
	// ... hoặc service khai nhưng máy chưa đăng ký.
	if err := os.WriteFile(local, []byte("mcp:\n  services: {codegraph: {}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveMachineMCP(config.MachineMCP{Services: map[string]config.Registration{}}); err != nil {
		t.Fatal(err)
	}
	if names, _ := serveToolNames(t, root); len(names) != 6 {
		t.Errorf("máy chưa đăng ký: %v", names)
	}
	noGateway("máy chưa đăng ký")

	// Đối chứng: service khả dụng → shim khởi động daemon; upstream giả không phải MCP nên service được báo lỗi trong
	// instructions, nhưng vẫn đủ 6 tool của Bint.
	if err := config.SaveMachineMCP(config.MachineMCP{Services: map[string]config.Registration{"codegraph": {Command: "true"}}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { run(t, root, "", "mcp", "gateway", "stop") })
	names, instr := serveToolNames(t, root)
	if len(names) != 6 {
		t.Errorf("đối chứng: %v", names)
	}
	if !strings.Contains(instr, "codegraph") {
		t.Errorf("đối chứng: instructions phải nêu codegraph lỗi: %q", instr)
	}
	if _, err := os.Stat(filepath.Join(gwDir, "gateway.json")); err != nil {
		t.Errorf("đối chứng: shim phải khởi động daemon: %v", err)
	}
}

// Mục MCP cấp người dùng (Antigravity) áp cho mọi workspace: ngoài repo Git hay repo chưa init Bint, serve không thoát
// lỗi mà phục vụ server rỗng kèm lời giải thích.
func TestMCPServeOutsideBintWorkspace(t *testing.T) {
	plain := t.TempDir() // không phải repo Git
	names, instr := serveToolNames(t, plain)
	if len(names) != 0 {
		t.Errorf("ngoài repo Bint không được có tool: %v", names)
	}
	if !strings.Contains(instr, "bint init") {
		t.Errorf("instructions phải giải thích và chỉ cách khởi tạo: %q", instr)
	}
}
