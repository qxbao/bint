package cli_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/testutil"
)

type gwStatusJSON struct {
	Running bool   `json:"running"`
	Dir     string `json:"dir"`
	Status  *struct {
		PID       int `json:"pid"`
		Port      int `json:"port"`
		Sessions  int `json:"sessions"`
		Upstreams []struct {
			Service string `json:"service"`
		} `json:"upstreams"`
	} `json:"status"`
}

// bint mcp gateway start|status|restart|stop qua binary thật: daemon tách rời, status có PID/cổng, chưa chạy thì báo không chạy.
func TestMCPGatewayEndToEnd(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	t.Cleanup(func() { run(t, root, "", "mcp", "gateway", "stop") })

	out, code := run(t, root, "", "mcp", "gateway", "status")
	if code != 0 || !strings.Contains(out, "không chạy") {
		t.Fatalf("status khi chưa chạy: %d\n%s", code, out)
	}
	out, code = run(t, root, "", "mcp", "gateway", "status", "--json")
	var st gwStatusJSON
	if code != 0 || json.Unmarshal([]byte(out), &st) != nil || st.Running || st.Status != nil {
		t.Fatalf("status --json khi chưa chạy: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "mcp", "gateway", "stop"); code != 0 || !strings.Contains(out, "không chạy") {
		t.Fatalf("stop khi chưa chạy: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "mcp", "gateway", "restart", "codegraph"); code == 0 || !strings.Contains(out, "không chạy") {
		t.Fatalf("restart khi chưa có daemon: %d\n%s", code, out)
	}

	out, code = run(t, root, "", "mcp", "gateway", "start")
	if code != 0 || !strings.Contains(out, "PID") || !strings.Contains(out, "cổng") {
		t.Fatalf("start: %d\n%s", code, out)
	}
	out, code = run(t, root, "", "mcp", "gateway", "status", "--json")
	if code != 0 || json.Unmarshal([]byte(out), &st) != nil || !st.Running || st.Status == nil || st.Status.PID == 0 || st.Status.Port == 0 {
		t.Fatalf("status --json khi chạy: %d\n%s", code, out)
	}
	pid := st.Status.PID
	if out, code := run(t, root, "", "mcp", "gateway", "status"); code != 0 || !strings.Contains(out, "Gateway chạy") ||
		!strings.Contains(out, "khởi động lười") {
		t.Fatalf("status: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "mcp", "gateway", "start"); code != 0 || !strings.Contains(out, "đã chạy từ trước") || !strings.Contains(out, "PID") {
		t.Fatalf("start lần hai: %d\n%s", code, out)
	}

	// restart: service lạ là lỗi cú pháp; service hợp lệ chưa có upstream thì dừng 0.
	if out, code := run(t, root, "", "mcp", "gateway", "restart", "khong-co"); code != 2 {
		t.Fatalf("restart service lạ: %d\n%s", code, out)
	}
	out, code = run(t, root, "", "mcp", "gateway", "restart", "codegraph", "--json")
	var rr struct {
		Service string `json:"service"`
		Stopped int    `json:"stopped"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &rr) != nil || rr.Service != "codegraph" || rr.Stopped != 0 {
		t.Fatalf("restart: %d\n%s", code, out)
	}

	out, code = run(t, root, "", "mcp", "gateway", "stop")
	if code != 0 || !strings.Contains(out, "Đã tắt") {
		t.Fatalf("stop: %d\n%s", code, out)
	}
	out, _ = run(t, root, "", "mcp", "gateway", "status")
	if !strings.Contains(out, "không chạy") {
		t.Fatalf("status sau stop:\n%s", out)
	}
	_ = pid
}

// stop và restart chỉ dành cho người; status chạy được trong phiên agent.
func TestMCPGatewayAgentForbidden(t *testing.T) {
	testutil.Isolate(t)
	asAgent(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	for _, args := range [][]string{{"stop"}, {"restart", "codegraph"}} {
		out, code := run(t, root, "", append([]string{"mcp", "gateway"}, args...)...)
		if code == 0 || !strings.Contains(out, "chỉ dành cho người dùng") {
			t.Errorf("mcp gateway %s trong phiên agent: %d\n%s", args[0], code, out)
		}
	}
	if out, code := run(t, root, "", "mcp", "gateway", "status"); code != 0 || !strings.Contains(out, "không chạy") {
		t.Errorf("status trong phiên agent: %d\n%s", code, out)
	}
}

// bint mcp gateway log <service>: chưa có gì → ghi chú; có gateway.log → chỉ dòng của service; --lines; --json; chạy được trong phiên agent.
func TestMCPGatewayLog(t *testing.T) {
	testutil.Isolate(t)
	asAgent(t) // log chỉ đọc nên agent chạy được
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})

	out, code := run(t, root, "", "mcp", "gateway", "log", "codegraph")
	if code != 0 || !strings.Contains(out, "gateway không chạy") || !strings.Contains(out, "không có dòng nào") {
		t.Fatalf("khi chưa có gì: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "mcp", "gateway", "log"); code != 2 {
		t.Fatalf("thiếu service: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "mcp", "gateway", "log", "khong-co"); code != 2 {
		t.Fatalf("service lạ: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "mcp", "gateway", "log", "codegraph", "--lines", "-3"); code != 2 {
		t.Fatalf("--lines âm: %d\n%s", code, out)
	}

	dir := filepath.Join(os.Getenv("BINT_STATE_DIR"), "gateway")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	var lines []string
	for i := 0; i < 4; i++ {
		lines = append(lines, fmt.Sprintf("2026-11-13T04:00:0%dZ codegraph (p): lỗi số %d", i, i), fmt.Sprintf("2026-11-13T04:00:0%dZ jira (p): của jira", i))
	}
	if err := os.WriteFile(filepath.Join(dir, "gateway.log"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, code = run(t, root, "", "mcp", "gateway", "log", "codegraph", "--lines", "2")
	if code != 0 || strings.Contains(out, "jira") || strings.Contains(out, "lỗi số 1") || !strings.Contains(out, "lỗi số 2") || !strings.Contains(out, "lỗi số 3") {
		t.Fatalf("lọc và giới hạn: %d\n%s", code, out)
	}
	out, code = run(t, root, "", "mcp", "gateway", "log", "codegraph", "--json")
	var res struct {
		Service   string   `json:"service"`
		Upstreams []any    `json:"upstreams"`
		LogFile   string   `json:"log_file"`
		Lines     []string `json:"lines"`
		Note      string   `json:"note"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &res) != nil || res.Service != "codegraph" || len(res.Lines) != 4 ||
		res.Upstreams == nil || !strings.HasSuffix(res.LogFile, "gateway.log") || !strings.Contains(res.Note, "không chạy") {
		t.Fatalf("--json: %d\n%s", code, out)
	}
}
