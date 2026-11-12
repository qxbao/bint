package mcpgw

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"bint/internal/config"
	"bint/internal/mcpcat"
)

// Upstream nhận đúng env tối thiểu + env mặc định của danh mục + biến credential đọc từ secrets.env lúc khởi động.
func TestUpstreamEnv(t *testing.T) {
	h := newHarness(t, nil)
	t.Setenv("LEAK_ME", "x")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "aws-secret")
	t.Setenv("JIRA_MCP_PASSWORD", "tu-daemon") // biến credential trùng tên trong env của daemon không được dùng
	writeSecrets(t, h, "# chú thích", "JIRA_MCP_URL=http://jira.example", "JIRA_MCP_USERNAME=bot", "JIRA_MCP_PASSWORD=pw-1", "KHAC=khong-khai-bao")
	h.register("jira")
	c := h.open("jira", nil)
	if m, _ := c.call(1, "jira_issue_get", nil); len(m.Error) > 0 {
		t.Fatal(string(m.Error))
	}
	env := startEnv(t, h, "jira", 0)
	svc, _ := mcpcat.Lookup("jira")
	allowed := append(slices.Clone(BaseEnv), svc.CredentialEnv...)
	for k := range env {
		if !slices.Contains(allowed, k) {
			t.Errorf("biến %s lọt sang upstream (cho phép: %v)", k, allowed)
		}
	}
	if env["JIRA_MCP_PASSWORD"] != "pw-1" || env["JIRA_MCP_USERNAME"] != "bot" || env["JIRA_MCP_URL"] != "http://jira.example" {
		t.Errorf("credential từ secrets.env: %v", env)
	}
	if _, ok := env["PATH"]; !ok {
		t.Error("thiếu PATH trong env tối thiểu")
	}

	// Đổi secrets.env: upstream khởi động sau nhận giá trị mới.
	writeSecrets(t, h, "JIRA_MCP_URL=http://jira.example", "JIRA_MCP_USERNAME=bot", "JIRA_MCP_PASSWORD=pw-2")
	h.d.Restart("jira")
	if m, _ := c.call(2, "jira_issue_get", nil); len(m.Error) > 0 {
		t.Fatal(string(m.Error))
	}
	if env2 := startEnv(t, h, "jira", 1); env2["JIRA_MCP_PASSWORD"] != "pw-2" {
		t.Errorf("sau khi đổi secrets.env: %q, muốn pw-2", env2["JIRA_MCP_PASSWORD"])
	}
}

func TestUpstreamEnvDefaultsAndMissing(t *testing.T) {
	h := newHarness(t, nil)
	h.register("codegraph")
	repo := evalDir(t, newRepo(t, false))
	c := h.open("codegraph", worktreeHdr(repo))
	c.call(1, "codegraph_explore", nil)
	env := startEnv(t, h, "codegraph", 0)
	if env["CODEGRAPH_TELEMETRY"] != "0" || len(strings.Split(env["CODEGRAPH_MCP_TOOLS"], ",")) != 8 {
		t.Errorf("env mặc định của danh mục: %v", env)
	}

	// Thiếu biến credential: không khởi động, thông báo chỉ nêu tên biến.
	h.register("jira")
	writeSecrets(t, h, "JIRA_MCP_URL=http://j", "JIRA_MCP_USERNAME=u")
	_, msg := h.tryOpen("jira", nil)
	if !strings.Contains(msg, "JIRA_MCP_PASSWORD") || strings.Contains(msg, "http://j") {
		t.Errorf("thiếu credential: %s", msg)
	}
	if n := len(h.starts("jira")); n != 0 {
		t.Errorf("thiếu credential mà vẫn khởi động upstream (%d)", n)
	}
}

// Giá trị credential không lọt vào status hay thông báo lỗi dù upstream in nó ra stderr.
func TestSecretsScrubbedFromStatus(t *testing.T) {
	h := newHarness(t, nil)
	writeSecrets(t, h, "JIRA_MCP_URL=http://j", "JIRA_MCP_USERNAME=u", "JIRA_MCP_PASSWORD=pw-rat-bi-mat")
	h.register("jira", "diemsg=1")
	_, msg := h.tryOpen("jira", nil)
	if strings.Contains(msg, "pw-rat-bi-mat") {
		t.Errorf("giá trị bí mật trong phản hồi: %s", msg)
	}
	b, _ := json.Marshal(h.d.Status())
	if strings.Contains(string(b), "pw-rat-bi-mat") {
		t.Errorf("giá trị bí mật trong status: %s", b)
	}
	if !strings.Contains(string(b), "boom") || !strings.Contains(string(b), "***") {
		t.Errorf("status phải ghi lỗi đã che (stderr có 'boom ***'): %s", b)
	}
	if n := h.d.Status().Upstreams[0].Restarts; n != 0 {
		t.Errorf("restarts=%d", n)
	}
}

func startEnv(t *testing.T, h *harness, svc string, idx int) map[string]string {
	t.Helper()
	var s []map[string]any
	eventually(t, "upstream ghi nhật ký khởi động", 5*time.Second, func() bool { s = h.starts(svc); return len(s) > idx })
	raw, _ := json.Marshal(s[idx]["env"])
	var env map[string]string
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	return env
}

var _ = config.MachineMCP{}
