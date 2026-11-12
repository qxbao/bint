package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
