package eval

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func allowedTools(args []string) []string {
	for i, a := range args {
		if a == "--allowedTools" && i+1 < len(args) {
			return strings.Split(args[i+1], ",")
		}
	}
	return nil
}

// AgentRequest.MCPTools: Claude thêm mcp__bint__<tên> vào --allowedTools (sau 6 tool của Bint), không trùng; không có thì đối số như cũ.
func TestClaudeArgsForMCPTools(t *testing.T) {
	a := &ClaudeAgent{}
	a.Cfg.PermissionMode = "acceptEdits"
	a.Cfg.AllowedTools = []string{"Read"}
	base := a.ArgsFor(AgentRequest{MCPConfig: "/m.json"})
	same := a.ArgsFor(AgentRequest{MCPConfig: "/m.json", MCPTools: nil})
	if !slices.Equal(base, same) {
		t.Fatalf("MCPTools rỗng phải không đổi đối số:\n%v\n%v", base, same)
	}
	got := allowedTools(a.ArgsFor(AgentRequest{MCPConfig: "/m.json", MCPTools: []string{"cg_explore", "spec_backward_compatibility_check", "cg_explore"}}))
	for _, want := range append(slices.Clone(BintMCPTools), "mcp__bint__cg_explore", "mcp__bint__spec_backward_compatibility_check") {
		if !slices.Contains(got, want) {
			t.Errorf("--allowedTools thiếu %s: %v", want, got)
		}
	}
	if n := strings.Count(strings.Join(got, ","), "mcp__bint__cg_explore"); n != 1 {
		t.Errorf("mcp__bint__cg_explore xuất hiện %d lần", n)
	}
	// Lượt đánh giá (không MCPConfig): không thêm gì dù có MCPTools.
	if got := allowedTools(a.ArgsFor(AgentRequest{MCPTools: []string{"cg_explore"}})); slices.Contains(got, "mcp__bint__cg_explore") {
		t.Errorf("không MCPConfig thì không cấp tool MCP: %v", got)
	}
}

// Codex: approval_mode approve cho từng tool được cấp; không có thì như cũ; tên lạ bị từ chối.
func TestCodexArgsForMCPTools(t *testing.T) {
	mcp := filepath.Join(t.TempDir(), "mcp.json")
	os.WriteFile(mcp, []byte(`{"mcpServers":{"bint":{"command":"/x/bint","args":["mcp","serve","--run-id","R","--task-id","T"]}}}`), 0o644)
	c := &CodexAgent{}
	base, err := c.ArgsFor(AgentRequest{Dir: "/r", MCPConfig: mcp})
	if err != nil {
		t.Fatal(err)
	}
	if s := strings.Join(base, " "); strings.Contains(s, "approval_mode") && strings.Count(s, "approval_mode") != 1 {
		t.Errorf("không MCPTools: chỉ cite_knowledge được approve: %s", s)
	}
	args, err := c.ArgsFor(AgentRequest{Dir: "/r", MCPConfig: mcp, MCPTools: []string{"cg_explore", "ctx7_get-library-docs"}})
	if err != nil {
		t.Fatal(err)
	}
	s := strings.Join(args, " ")
	for _, want := range []string{`mcp_servers.bint.args=["mcp", "serve", "--run-id", "R", "--task-id", "T"]`,
		`mcp_servers.bint.tools.cg_explore.approval_mode="approve"`, `mcp_servers.bint.tools.ctx7_get-library-docs.approval_mode="approve"`,
		`mcp_servers.bint.tools.cite_knowledge.approval_mode="approve"`} {
		if !strings.Contains(s, want) {
			t.Errorf("codex thiếu %q: %s", want, s)
		}
	}
	if _, err := c.ArgsFor(AgentRequest{MCPConfig: mcp, MCPTools: []string{`a.b`}}); err == nil {
		t.Error("tên tool có dấu chấm phải bị từ chối")
	}
}
