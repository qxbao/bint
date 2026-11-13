package mcpcat

import (
	"slices"
	"strings"
	"testing"
)

func TestCatalogServices(t *testing.T) {
	want := []string{"codegraph", "specmatic", "deepwiki", "context7", "jira", "chrome-devtools"}
	if got := IDs(); !slices.Equal(got, want) {
		t.Fatalf("IDs = %v, muốn %v", got, want)
	}
	if _, ok := Lookup("bint"); ok {
		t.Fatal("bint không nằm trong danh mục")
	}
	prefixes := map[string]string{}
	for _, s := range All() {
		if s.Prefix == "" || s.Share == "" || s.Transport == "" || len(s.Groups) == 0 {
			t.Errorf("%s: thiếu tiền tố/chia sẻ/kiểu/nhóm: %+v", s.ID, s)
		}
		if other, dup := prefixes[s.Prefix]; dup {
			t.Errorf("tiền tố %q trùng giữa %s và %s", s.Prefix, other, s.ID)
		}
		prefixes[s.Prefix] = s.ID
		if s.Transport == HTTP && (s.URL == "" || s.Share != ShareRemote) {
			t.Errorf("%s: http cần URL và chia sẻ remote", s.ID)
		}
		if s.Transport == Stdio && (len(s.Run) == 0 || s.Install == "") {
			t.Errorf("%s: stdio cần lệnh chạy và lệnh cài gợi ý", s.ID)
		}
		// Lệnh cài gợi ý phải ghim đúng phiên bản của danh mục (D1): ==<v> (uv/pip) hoặc @<v> (npm).
		if s.Install != "" && (s.Version == "" || !(strings.HasSuffix(s.Install, "=="+s.Version) || strings.HasSuffix(s.Install, "@"+s.Version))) {
			t.Errorf("%s: lệnh cài %q không ghim phiên bản %q", s.ID, s.Install, s.Version)
		}
	}
}

func TestAgentToolName(t *testing.T) {
	cases := []struct{ svc, up, want string }{
		{"codegraph", "codegraph_explore", "cg_explore"},
		{"codegraph", "codegraph_status", "cg_status"},
		{"jira", "jira_issue_get", "jira_issue_get"},
		{"specmatic", "backward_compatibility_check", "spec_backward_compatibility_check"},
		{"deepwiki", "read_wiki_contents", "wiki_read_wiki_contents"},
		{"context7", "get-library-docs", "ctx7_get-library-docs"},
	}
	for _, c := range cases {
		s, _ := Lookup(c.svc)
		if got := AgentToolName(s, c.up); got != c.want {
			t.Errorf("AgentToolName(%s, %s) = %q, muốn %q", c.svc, c.up, got, c.want)
		}
	}
}

func TestCatalogInvariants(t *testing.T) {
	for _, s := range All() {
		seen := map[string]string{}
		agentNames := map[string]string{}
		for _, g := range s.Groups {
			if g.Name == "" || len(g.Tools) == 0 {
				t.Errorf("%s: nhóm rỗng %+v", s.ID, g)
			}
			for _, tool := range g.Tools {
				if prev, dup := seen[tool]; dup {
					t.Errorf("%s: tool %s thuộc cả nhóm %s và %s", s.ID, tool, prev, g.Name)
				}
				seen[tool] = g.Name
				name := AgentToolName(s, tool)
				if n := len(AgentPrefix + name); n > MaxToolNameLen {
					t.Errorf("%s: %s dài %d ký tự (> %d)", s.ID, AgentPrefix+name, n, MaxToolNameLen)
				}
				if prev, dup := agentNames[name]; dup {
					t.Errorf("%s: tên agent %s trùng giữa %s và %s", s.ID, name, prev, tool)
				}
				agentNames[name] = tool
			}
		}
		for _, list := range [][]string{s.ReadOnly, s.WritesWorktree} {
			for _, tool := range list {
				if _, ok := seen[tool]; !ok {
					t.Errorf("%s: tool %s (chỉ đọc/ghi cây) không thuộc nhóm nào", s.ID, tool)
				}
			}
		}
		for _, tool := range s.ReadOnly {
			if slices.Contains(s.WritesWorktree, tool) {
				t.Errorf("%s: %s vừa chỉ đọc vừa ghi cây làm việc", s.ID, tool)
			}
		}
		for tool := range s.DefaultArgs {
			if _, ok := seen[tool]; !ok {
				t.Errorf("%s: DefaultArgs trỏ tool không có: %s", s.ID, tool)
			}
		}
	}
}

func TestCatalogSpecifics(t *testing.T) {
	cg, _ := Lookup("codegraph")
	if cg.Share != ShareWorktree || cg.Cwd != CwdWorktree || cg.DefaultEnv["CODEGRAPH_TELEMETRY"] != "0" {
		t.Errorf("codegraph: %+v", cg)
	}
	if n := len(strings.Split(cg.DefaultEnv["CODEGRAPH_MCP_TOOLS"], ",")); n != 8 {
		t.Errorf("CODEGRAPH_MCP_TOOLS có %d tool, muốn 8", n)
	}
	if len(cg.Tools()) != 8 || len(cg.ReadOnly) != 8 {
		t.Errorf("codegraph: mọi tool chỉ đọc: %v / %v", cg.Tools(), cg.ReadOnly)
	}
	sp, _ := Lookup("specmatic")
	if g, _ := sp.Group("mock"); !g.Exclusive {
		t.Error("specmatic.mock phải độc quyền")
	}
	if g, _ := sp.Group("contract"); g.MaxConcurrent != 1 {
		t.Errorf("specmatic.contract chạy tối đa một lời gọi đồng thời mỗi phân vùng, có %d", g.MaxConcurrent)
	}
	for _, g := range sp.Groups {
		if g.Name != "contract" && g.MaxConcurrent != 0 {
			t.Errorf("specmatic.%s không khai báo giới hạn đồng thời, có %d", g.Name, g.MaxConcurrent)
		}
	}
	if sp.Cwd != CwdState || sp.DefaultArgs["backward_compatibility_check"]["repoDir"] != WorktreePlaceholder {
		t.Errorf("specmatic: %+v", sp)
	}
	if len(sp.Options) == 0 || sp.Options["specs"] == "" {
		t.Error("specmatic cần tuỳ chọn specs")
	}
	if sp.Install != "npm i -g specmatic-mcp@1.2.0" {
		t.Errorf("specmatic.Install = %q", sp.Install)
	}
	dw, _ := Lookup("deepwiki")
	if g, ok := dw.Group("read"); !ok || !slices.Contains(g.Tools, "ask_wiki_question") {
		t.Errorf("deepwiki.read thiếu ask_wiki_question: %+v", g)
	}
	if j, _ := Lookup("jira"); len(j.CredentialEnv) == 0 || j.Share != ShareMachine {
		t.Errorf("jira: %+v", j)
	}
	if c, _ := Lookup("chrome-devtools"); c.Share != ShareSession {
		t.Errorf("chrome-devtools: %+v", c)
	}
}

func TestExpand(t *testing.T) {
	refs, err := Expand("codegraph.explore")
	if err != nil || len(refs) != 1 || refs[0].Agent != "cg_explore" || refs[0].Upstream != "codegraph_explore" {
		t.Fatalf("codegraph.explore = %+v, %v", refs, err)
	}
	all, err := Expand("jira.*")
	if err != nil {
		t.Fatal(err)
	}
	j, _ := Lookup("jira")
	if len(all) != len(j.Tools()) {
		t.Errorf("jira.* = %d tool, muốn %d", len(all), len(j.Tools()))
	}
	for _, bad := range []string{"", "codegraph", "codegraph.", ".explore", "codegraph.nope", "nope.read", "bint.read", "a.b.c", "codegraph.explore.x"} {
		if _, err := Expand(bad); err == nil {
			t.Errorf("Expand(%q) phải lỗi", bad)
		}
	}
}
