package app

import (
	"context"

	bintapp "bint/internal/app"
	"bint/internal/config"
	"bint/internal/mcpgw"
	mcscreen "bint/internal/tui/screens/mcp"
)

// sampleMCPData là dữ liệu mẫu xác định cho màn MCP (không đọc mcp.yml/secrets.env hay gateway thật của máy chạy kiểm thử): sáu mục danh mục,
// codegraph đang chạy ở hai phân vùng, specmatic chết (LastError), jira đã đăng ký mà thiếu biến credential, context7 và chrome-devtools chưa đăng ký,
// deepwiki đã đăng ký (trạng thái degraded chỉ có sau khi check trong phiên: xem mcpWithCheck).
func sampleMCPData() mcscreen.Data {
	reg := func(cmd string, args ...string) *config.Registration {
		return &config.Registration{Command: cmd, Args: args}
	}
	return mcscreen.Data{
		SecretsFile: "/home/u/.config/bint/secrets.env", Root: "/work/demo",
		Services: []bintapp.MCPServiceStatus{
			{ID: "codegraph", Prefix: "cg", Transport: "stdio", Share: "worktree", Groups: []string{"explore", "query"}, Install: "npm i -g @colbymchenry/codegraph@1.6.2",
				Registered: true, Registration: reg("codegraph", "serve", "--mcp"), ProjectUses: true, Required: true},
			{ID: "specmatic", Prefix: "spec", Transport: "stdio", Share: "session", Groups: []string{"compat", "contract", "mock"}, Install: "npm i -g specmatic-mcp",
				Registered: true, Registration: reg("specmatic-mcp"), ProjectUses: true},
			{ID: "deepwiki", Prefix: "wiki", Transport: "http", Share: "remote", Groups: []string{"read"}, Registered: true,
				Registration: &config.Registration{URL: "https://mcp.deepwiki.com/mcp"}, ProjectUses: true},
			{ID: "context7", Prefix: "ctx7", Transport: "http", Share: "remote", Groups: []string{"read"}},
			{ID: "jira", Prefix: "jira", Transport: "stdio", Share: "machine", Groups: []string{"read", "write"}, Install: "npm i -g jira-mcp@3.4.0",
				Registered: true, Registration: reg("jira-mcp"), MissingEnv: []string{"JIRA_MCP_PASSWORD"}, ProjectUses: true},
			{ID: "chrome-devtools", Prefix: "chrome", Transport: "stdio", Share: "session", Groups: []string{"read", "act"}, MissingEnv: []string{}},
		},
		Gateway: &mcpgw.Status{PID: 4242, Port: 40123, Sessions: 3, Upstreams: []mcpgw.UpstreamStatus{
			{Service: "codegraph", Partition: "/work/other", Share: "worktree", Running: true, Generation: 1, PID: 4300, RSSKB: 120 * 1024, Sessions: 1},
			{Service: "codegraph", Partition: "/work/demo", Share: "worktree", Running: true, Generation: 2, PID: 4301, RSSKB: 184320, PSSKB: 90000, Sessions: 2, Restarts: 1,
				LastCall: &mcpgw.LastCall{Tool: "codegraph_explore", Outcome: "ok", DurationMS: 120}},
			{Service: "specmatic", Partition: "phiên 01HX0000", Share: "session", Generation: 1, Restarts: 3, Limited: true,
				LastError: "exit status 1 [stderr: boom ***]"},
		}},
	}
}

func sampleMCPGrants() mcscreen.GrantsData {
	g := func(svc, grp string) bintapp.MCPGrantTool {
		return bintapp.MCPGrantTool{Service: svc, Group: grp, Upstream: svc + "_" + grp, Agent: "mcp__bint__" + svc + "_" + grp}
	}
	return mcscreen.GrantsData{Roles: []bintapp.MCPRoleGrants{
		{Role: "implementer", Tools: []bintapp.MCPGrantTool{g("codegraph", "explore"), g("codegraph", "query"), g("codegraph", "query"), g("specmatic", "mock"), g("jira", "read")}},
		{Role: "reviewer", Tools: []bintapp.MCPGrantTool{g("codegraph", "explore"), g("specmatic", "compat")},
			Warnings: []string{"service specmatic được cấp nhưng máy này chưa đăng ký (bint mcp add specmatic)", "role reviewer chạy ở bước chỉ đọc: tool ghi vào cây làm việc không được cấp dù mcp.grants có ghi (mcp__bint__jira_write)"}},
		{Role: "interactive", Implicit: true, Tools: []bintapp.MCPGrantTool{g("codegraph", "explore"), g("codegraph", "query"), g("deepwiki", "read"), g("jira", "read"), g("jira", "write")}},
	}}
}

// sampleMCPLoaders là các hàm nạp mẫu của màn MCP.
func sampleMCPLoaders() mcscreen.Loaders {
	return mcscreen.Loaders{
		Services: func(context.Context, string) (mcscreen.Data, error) { return sampleMCPData(), nil },
		Grants:   func(context.Context, string) (mcscreen.GrantsData, error) { return sampleMCPGrants(), nil },
		Tools: func(context.Context, string, string) ([]bintapp.MCPTool, error) {
			return []bintapp.MCPTool{{Name: "context_for_task", Description: "Gói ngữ cảnh cho một tác vụ"}, {Name: "knowledge_search", Description: "Tìm tri thức theo từ khoá"},
				{Name: "cite_knowledge", Description: "Báo mục tri thức đã áp dụng"}}, nil
		},
	}
}

// withSampleMCP điền các hàm nạp còn nil bằng dữ liệu mẫu (như withSampleSystem): test cần dữ liệu riêng thì đặt Options.MCPLoad.
func withSampleMCP(l mcscreen.Loaders) mcscreen.Loaders {
	s := sampleMCPLoaders()
	if l.Services == nil {
		l.Services = s.Services
	}
	if l.Grants == nil {
		l.Grants = s.Grants
	}
	if l.Tools == nil {
		l.Tools = s.Tools
	}
	return l
}
