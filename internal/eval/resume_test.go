package eval

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// helpFlags đọc các cờ (-c, --config, ...) khai báo ở đầu dòng của bản trợ giúp codex lưu trong testdata.
func helpFlags(t *testing.T, name string) map[string]bool {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "codex-0.167", name))
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?m)^ {2,6}(?:(-[a-zA-Z]), +)?(--[a-z][a-z0-9-]*)?`)
	out := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(string(b), -1) {
		for _, f := range m[1:] {
			if f != "" {
				out[f] = true
			}
		}
	}
	if !out["--json"] || !out["-c"] {
		t.Fatalf("%s: không đọc được cờ từ bản trợ giúp: %v", name, out)
	}
	return out
}

func claudeResumeFixture() *ClaudeAgent {
	a := &ClaudeAgent{Model: "sonnet"}
	a.Cfg.PermissionMode = "acceptEdits"
	a.Cfg.AllowedTools = []string{"Read", "Edit"}
	a.Cfg.SettingSources = "project"
	a.Cfg.ExtraArgs = []string{"--exclude-dynamic-system-prompt-sections"}
	budget := 2.5
	a.MaxCostUSD = &budget
	return a
}

// Bước nối tiếp của Claude chỉ khác bước gốc ở cặp --session-id <uuid> → --resume <id>: mọi cờ quyền/MCP/settings/model/
// ngân sách/ExtraArgs giữ nguyên và đúng thứ tự; không có --no-session-persistence và không có --fork-session.
func TestClaudeArgsResumeKeepsOriginalFlags(t *testing.T) {
	a := claudeResumeFixture()
	base := AgentRequest{SessionID: "sess-1", MCPConfig: "/m.json", Settings: "/s.json", MCPTools: []string{"cg_explore"}, HasSkills: true, Trace: true}
	orig := a.ArgsFor(base)
	cont := a.ArgsFor(AgentRequest{ResumeSessionID: "sess-1", MCPConfig: "/m.json", Settings: "/s.json", MCPTools: []string{"cg_explore"}, HasSkills: true, Trace: true})
	i := slices.Index(orig, "--session-id")
	j := slices.Index(cont, "--resume")
	if i < 0 || j != i || cont[j+1] != "sess-1" {
		t.Fatalf("--resume phải thế đúng chỗ --session-id:\ngốc  %v\ntiếp %v", orig, cont)
	}
	want := slices.Clone(orig)
	want[i] = "--resume"
	if !slices.Equal(want, cont) {
		t.Fatalf("cờ khác phải giữ nguyên:\ngốc  %v\ntiếp %v", orig, cont)
	}
	for _, bad := range []string{"--no-session-persistence", "--fork-session", "--session-id", "--continue"} {
		if slices.Contains(cont, bad) {
			t.Errorf("bước nối tiếp không được có %s: %v", bad, cont)
		}
	}
	for _, must := range []string{"--permission-mode", "--mcp-config", "--allowedTools", "--settings", "--setting-sources", "--model",
		"--max-budget-usd", "--exclude-dynamic-system-prompt-sections", "--strict-mcp-config"} {
		if !slices.Contains(cont, must) {
			t.Errorf("thiếu %s ở bước nối tiếp: %v", must, cont)
		}
	}
	// Lượt eval (không SessionID/ResumeSessionID) không đổi.
	if def := a.ArgsFor(AgentRequest{}); !slices.Contains(def, "--no-session-persistence") || slices.Contains(def, "--resume") {
		t.Fatalf("lượt eval phải giữ --no-session-persistence: %v", def)
	}
}

func codexResumeFixture(t *testing.T) (*CodexAgent, AgentRequest) {
	t.Helper()
	mcp := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(mcp, []byte(`{"mcpServers":{"bint":{"command":"/x/bint","args":["mcp","serve"]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return &CodexAgent{Model: "gpt-x", ExtraArgs: []string{"-c", "k=v"}}, AgentRequest{Dir: "/repo", WritableDirs: []string{"/state"},
		MCPConfig: mcp, ExclusiveMCP: false, MCPTools: []string{"cg_explore"}, Settings: "settings.json", ReadOnly: true}
}

// Bước nối tiếp của Codex: `exec <cờ của bước gốc> resume <id> -`; dãy -c (MCP độc quyền, tool, hook, ExtraArgs) trùng bước gốc;
// mọi cờ chỉ dùng cờ có trong bản trợ giúp codex-cli lưu ở testdata (cờ trước `resume` ∈ exec, sau `resume` ∈ exec resume).
func TestCodexArgsResumeKeepsOriginalConfig(t *testing.T) {
	a, req := codexResumeFixture(t)
	orig, err := a.ArgsFor(req)
	if err != nil {
		t.Fatal(err)
	}
	req.ResumeSessionID = "thread-9"
	cont, err := a.ArgsFor(req)
	if err != nil {
		t.Fatal(err)
	}
	r := slices.Index(cont, "resume")
	if r < 0 || cont[r+1] != "thread-9" || cont[len(cont)-1] != "-" || len(cont) != r+3 {
		t.Fatalf("dạng `… resume <id> -`: %v", cont)
	}
	if !slices.Equal(orig[:len(orig)-1], cont[:r]) {
		t.Fatalf("cờ của bước gốc phải giữ nguyên trước `resume`:\ngốc  %v\ntiếp %v", orig, cont)
	}
	cfg := func(args []string) []string {
		var out []string
		for i, x := range args {
			if x == "-c" {
				out = append(out, args[i+1])
			}
		}
		return out
	}
	if g, w := cfg(cont), cfg(orig); !slices.Equal(g, w) || len(g) < 4 {
		t.Fatalf("dãy -c phải trùng bước gốc (MCP, tool, hook, ExtraArgs): %v vs %v", g, w)
	}
	execFlags, resumeFlags := helpFlags(t, "exec-help.txt"), helpFlags(t, "exec-resume-help.txt")
	for i, x := range cont {
		if !strings.HasPrefix(x, "-") || x == "-" {
			continue
		}
		set := execFlags
		if i > r {
			set = resumeFlags
		}
		if !set[x] {
			t.Errorf("cờ %q (vị trí %d) không có trong bản trợ giúp codex %s", x, i, map[bool]string{false: "exec", true: "exec resume"}[i > r])
		}
	}
	// --sandbox/--cd/--add-dir chỉ có ở cấp exec: phải đứng trước `resume`.
	for _, f := range []string{"--sandbox", "--cd", "--add-dir"} {
		if i := slices.Index(cont, f); i < 0 || i > r {
			t.Errorf("%s phải có và đứng trước resume: %v", f, cont)
		}
		if resumeFlags[f] {
			t.Errorf("giả định sai: exec resume nhận %s (cập nhật ArgsFor)", f)
		}
	}
}

// MCP độc quyền: bảng nội tuyến tắt server khác vẫn đứng trước mọi -c mcp_servers.* ở bước nối tiếp.
func TestCodexArgsResumeExclusiveMCPOrder(t *testing.T) {
	dir := t.TempDir()
	mcp := filepath.Join(dir, "mcp.json")
	os.WriteFile(mcp, []byte(`{"mcpServers":{"bint":{"command":"/x/bint","args":["mcp","serve"]}}}`), 0o644)
	t.Setenv("CODEX_HOME", filepath.Join(dir, "home"))
	os.MkdirAll(filepath.Join(dir, "home"), 0o755)
	os.WriteFile(filepath.Join(dir, "home", "config.toml"), []byte("[mcp_servers.other]\ncommand = \"x\"\n"), 0o644)
	a := &CodexAgent{}
	req := AgentRequest{Dir: dir, MCPConfig: mcp, ExclusiveMCP: true}
	orig, err := a.ArgsFor(req)
	if err != nil {
		t.Skipf("không dựng được MCP độc quyền trong môi trường kiểm thử: %v", err)
	}
	req.ResumeSessionID = "t1"
	cont, err := a.ArgsFor(req)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(orig[:len(orig)-1], cont[:len(cont)-3]) {
		t.Fatalf("khác bước gốc:\n%v\n%v", orig, cont)
	}
}

func TestResumeMissingRegexes(t *testing.T) {
	for _, tc := range []struct {
		re   *regexp.Regexp
		msg  string
		want bool
	}{
		{claudeResumeMissingRe, "No conversation found with session ID: 11111111-1111-4111-8111-111111111111", true},
		{claudeResumeMissingRe, "no conversation found with session id x", true},
		{claudeResumeMissingRe, "API Error: 529 overloaded", false},
		{claudeResumeMissingRe, "đã đọc 3 conversation và tìm thấy session", false},
		{codexResumeMissingRe, "Error: thread/resume: thread/resume failed: no rollout found for thread id 1111 (code -32600)", true},
		{codexResumeMissingRe, "No saved session found with ID abc", true},
		{codexResumeMissingRe, "unexpected status 503 Service Unavailable", false},
		{codexResumeMissingRe, "thread/resume failed: permission denied", false},
	} {
		if got := tc.re.MatchString(tc.msg); got != tc.want {
			t.Errorf("%q: got %v, muốn %v", tc.msg, got, tc.want)
		}
	}
}

// Run bằng tiến trình giả (chính binary kiểm thử, không sh): harness báo mất phiên khi nối tiếp ⇒ ResumeMissing, không Infra;
// cùng thông báo ở lượt không nối tiếp, hoặc thông báo khác, thì không.
func TestRunResumeMissing(t *testing.T) {
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	claude := func(req AgentRequest) AgentResult {
		return (&ClaudeAgent{Binary: bin}).Run(context.Background(), req)
	}
	codex := func(req AgentRequest) AgentResult { return (&CodexAgent{Binary: bin}).Run(context.Background(), req) }
	mk := func(msg, resume string) AgentRequest {
		return AgentRequest{Dir: t.TempDir(), Prompt: "x", Timeout: 30 * time.Second, ResumeSessionID: resume,
			Env: []string{rawFixtureEnv + "=" + msg}}
	}
	cmsg := "No conversation found with session ID: s1"
	xmsg := "Error: thread/resume: thread/resume failed: no rollout found for thread id s1 (code -32600)"
	if r := claude(mk(cmsg, "s1")); !r.ResumeMissing || r.Infra != "" || r.SessionID != "s1" {
		t.Errorf("claude nối tiếp mất phiên: %+v", r)
	}
	if r := claude(mk(cmsg, "")); r.ResumeMissing {
		t.Errorf("lượt không nối tiếp không được ResumeMissing: %+v", r)
	}
	if r := claude(mk("lỗi lạ khác", "s1")); r.ResumeMissing {
		t.Errorf("thông báo khác không phải mất phiên: %+v", r)
	}
	if r := codex(mk(xmsg, "s1")); !r.ResumeMissing || r.Infra != "" || r.QuotaHit {
		t.Errorf("codex nối tiếp mất phiên: %+v", r)
	}
	if r := codex(mk(xmsg, "")); r.ResumeMissing {
		t.Errorf("codex lượt thường không được ResumeMissing: %+v", r)
	}
	if r := codex(mk("unexpected status 503 Service Unavailable", "s1")); r.ResumeMissing {
		t.Errorf("503 không phải mất phiên: %+v", r)
	}
}
