package eval

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"bint/internal/testutil"
)

func writeMCPTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// disabledArgs lấy các giá trị -c tắt server MCP (không tính các -c mcp_servers.bint.*), theo thứ tự xuất hiện.
func disabledArgs(args []string) []string {
	var out []string
	for i, a := range args {
		if a == "-c" && i+1 < len(args) && strings.HasPrefix(args[i+1], "mcp_servers") && !strings.HasPrefix(args[i+1], "mcp_servers.bint.") {
			out = append(out, args[i+1])
		}
	}
	return out
}

func trustedProject(dir string) string {
	return "[projects." + tomlString(dir) + "]\ntrust_level = \"trusted\"\n"
}

func TestCodexExclusiveMCP(t *testing.T) {
	mcp := filepath.Join(t.TempDir(), "mcp.json")
	writeMCPTestFile(t, mcp, `{"mcpServers":{"bint":{"command":"/x/bint","args":["mcp","serve"]}}}`)
	cases := []struct {
		name          string
		user, project string // nội dung config.toml; "%DIR%" là thư mục dự án; rỗng = không có tệp
		trust         bool   // thêm projects.<dir>.trust_level = "trusted" vào config người dùng
		want          []string
	}{
		{name: "không có tệp"},
		{name: "có tệp nhưng không có mcp_servers", user: "model = \"gpt-5\"\n", project: "[features]\nx = true\n", trust: true},
		{name: "tên có dấu gạch là khoá trần", user: "[mcp_servers.jira-run]\ncommand=\"x\"\n",
			want: []string{`mcp_servers.jira-run.enabled=false`}},
		{name: "tên có dấu chấm là khoá trích dẫn TOML", user: "[mcp_servers.\"a.b\"]\ncommand=\"x\"\n",
			want: []string{`mcp_servers={"a.b"={enabled=false}}`}},
		{name: "nhiều tên trích dẫn gộp một bảng, khoá trần đứng sau", user: "[mcp_servers.\"a.b\"]\ncommand=\"x\"\n[mcp_servers.\"c d\"]\ncommand=\"x\"\n[mcp_servers.plain]\ncommand=\"x\"\n",
			want: []string{`mcp_servers={"a.b"={enabled=false},"c d"={enabled=false}}`, `mcp_servers.plain.enabled=false`}},
		{name: "trùng ở hai tệp chỉ một lần", user: "[mcp_servers.dup]\ncommand=\"x\"\n", project: "[mcp_servers.dup]\ncommand=\"y\"\n[mcp_servers.only-proj]\ncommand=\"z\"\n", trust: true,
			want: []string{`mcp_servers.dup.enabled=false`, `mcp_servers.only-proj.enabled=false`}},
		{name: "server chỉ có ở dự án chưa tin cậy không bị tắt (Codex bỏ qua tệp đó; tắt tên không tồn tại làm Codex lỗi)",
			user: "[mcp_servers.dup]\ncommand=\"x\"\n", project: "[mcp_servers.dup]\ncommand=\"y\"\n[mcp_servers.only-proj]\ncommand=\"z\"\n",
			want: []string{`mcp_servers.dup.enabled=false`}},
		{name: "bint không bị tắt", user: "[mcp_servers.bint]\ncommand=\"x\"\n[mcp_servers.other]\ncommand=\"y\"\n",
			want: []string{`mcp_servers.other.enabled=false`}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home, dir := t.TempDir(), t.TempDir()
			user := c.user
			if c.trust {
				user += "\n" + trustedProject(dir)
			}
			if user != "" {
				writeMCPTestFile(t, filepath.Join(home, "config.toml"), user)
			}
			if c.project != "" {
				writeMCPTestFile(t, filepath.Join(dir, ".codex", "config.toml"), c.project)
			}
			agent := &CodexAgent{ExtraArgs: []string{"-c", "k=v"}}
			args, err := agent.ArgsFor(AgentRequest{Dir: dir, MCPConfig: mcp, ExclusiveMCP: true, Env: []string{"CODEX_HOME=" + home}})
			if err != nil {
				t.Fatal(err)
			}
			got := disabledArgs(args)
			if !slices.Equal(got, c.want) {
				t.Fatalf("tắt %v, muốn %v (args: %v)", got, c.want, args)
			}
			s := strings.Join(args, " ")
			if strings.Contains(s, "--ignore-user-config") || !strings.Contains(s, `mcp_servers.bint.command="/x/bint"`) {
				t.Fatalf("đối số: %s", s)
			}
			// Bảng nội tuyến thay thế mọi mcp_servers.* đặt trước nó: cờ tắt phải đứng trước cờ của bint;
			// cả hai đứng trước extra_args để người dùng ghi đè được.
			if len(c.want) > 0 {
				first := slices.Index(args, c.want[0])
				if bint := slices.Index(args, `mcp_servers.bint.command="/x/bint"`); first > bint {
					t.Fatalf("cờ tắt phải đứng trước mcp_servers.bint.*: %v", args)
				}
				if slices.Index(args, "k=v") < slices.Index(args, c.want[len(c.want)-1]) {
					t.Fatalf("cờ tắt phải đứng trước extra_args: %v", args)
				}
			}
		})
	}
}

func TestCodexExclusiveMCPEnvPrecedenceAndErrors(t *testing.T) {
	mcp := filepath.Join(t.TempDir(), "mcp.json")
	writeMCPTestFile(t, mcp, `{"mcpServers":{"bint":{"command":"/x/bint","args":["mcp","serve"]}}}`)
	procHome, envHome := t.TempDir(), t.TempDir()
	writeMCPTestFile(t, filepath.Join(procHome, "config.toml"), "[mcp_servers.from-proc]\ncommand=\"x\"\n")
	writeMCPTestFile(t, filepath.Join(envHome, "config.toml"), "[mcp_servers.from-env]\ncommand=\"x\"\n")
	t.Setenv("CODEX_HOME", procHome)
	args, err := (&CodexAgent{}).ArgsFor(AgentRequest{Dir: t.TempDir(), MCPConfig: mcp, ExclusiveMCP: true})
	if err != nil || !slices.Equal(disabledArgs(args), []string{"mcp_servers.from-proc.enabled=false"}) {
		t.Fatalf("CODEX_HOME của tiến trình: %v %v", args, err)
	}
	args, err = (&CodexAgent{}).ArgsFor(AgentRequest{Dir: t.TempDir(), MCPConfig: mcp, ExclusiveMCP: true, Env: []string{"CODEX_HOME=" + envHome}})
	if err != nil || !slices.Equal(disabledArgs(args), []string{"mcp_servers.from-env.enabled=false"}) {
		t.Fatalf("req.Env phải thắng: %v %v", args, err)
	}
	// Không đặt CODEX_HOME: mặc định ~/.codex.
	t.Setenv("CODEX_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	writeMCPTestFile(t, filepath.Join(home, ".codex", "config.toml"), "[mcp_servers.default-home]\ncommand=\"x\"\n")
	args, err = (&CodexAgent{}).ArgsFor(AgentRequest{Dir: t.TempDir(), MCPConfig: mcp, ExclusiveMCP: true})
	if err != nil || !slices.Equal(disabledArgs(args), []string{"mcp_servers.default-home.enabled=false"}) {
		t.Fatalf("mặc định ~/.codex: %v %v", args, err)
	}
	// TOML hỏng: lỗi nêu đường dẫn.
	writeMCPTestFile(t, filepath.Join(envHome, "config.toml"), "[mcp_servers\n")
	if _, err = (&CodexAgent{}).ArgsFor(AgentRequest{Dir: t.TempDir(), MCPConfig: mcp, ExclusiveMCP: true, Env: []string{"CODEX_HOME=" + envHome}}); err == nil ||
		!strings.Contains(err.Error(), filepath.Join(envHome, "config.toml")) {
		t.Fatalf("muốn lỗi nêu đường dẫn, được %v", err)
	}
}

// Lượt eval (Arm khác rỗng, ExclusiveMCP không đặt) giữ nguyên đối số, kể cả khi config người dùng có server khác.
func TestCodexEvalArgsUnchangedByUserMCP(t *testing.T) {
	mcp := filepath.Join(t.TempDir(), "mcp.json")
	writeMCPTestFile(t, mcp, `{"mcpServers":{"bint":{"command":"/x/bint","args":["mcp","serve","--eval-config","/l/c.json"]}}}`)
	home := t.TempDir()
	writeMCPTestFile(t, filepath.Join(home, "config.toml"), "[mcp_servers.jira]\ncommand=\"x\"\n")
	t.Setenv("CODEX_HOME", home)
	req := AgentRequest{Dir: t.TempDir(), Arm: "bint", MCPConfig: mcp, Env: []string{"CODEX_HOME=" + home}}
	args, err := (&CodexAgent{}).ArgsFor(req)
	if err != nil {
		t.Fatal(err)
	}
	if d := disabledArgs(args); len(d) != 0 {
		t.Fatalf("lượt eval không được thêm cờ tắt: %v", d)
	}
	// Cùng yêu cầu khi bỏ config người dùng cho đúng đối số cũ.
	t.Setenv("CODEX_HOME", t.TempDir())
	req.Env = nil
	want, _ := (&CodexAgent{}).ArgsFor(req)
	if !slices.Equal(args, want) {
		t.Fatalf("đối số eval đổi theo config người dùng:\n%v\n%v", args, want)
	}
}

// Bước Codex chạy trong worktree liên kết của repo đã tin cậy (cây gốc có trong projects.*): server MCP của
// .codex/config.toml dự án vẫn bị tắt; worktree của repo chưa tin cậy thì không.
func TestCodexExclusiveMCPWorktreeInheritsTrust(t *testing.T) {
	mcp := filepath.Join(t.TempDir(), "mcp.json")
	writeMCPTestFile(t, mcp, `{"mcpServers":{"bint":{"command":"/x/bint","args":["mcp","serve"]}}}`)
	repo := testutil.NewRepo(t, map[string]string{
		"README.md":          "x\n",
		".codex/config.toml": "[mcp_servers.proj-only]\ncommand=\"x\"\n",
	})
	if r, err := filepath.EvalSymlinks(repo); err == nil {
		repo = r
	}
	wt := filepath.Join(t.TempDir(), "wt")
	testutil.Git(t, repo, "worktree", "add", "-q", "-b", "task", wt)
	if r, err := filepath.EvalSymlinks(wt); err == nil {
		wt = r
	}
	home := t.TempDir()
	run := func(userCfg string) []string {
		t.Helper()
		writeMCPTestFile(t, filepath.Join(home, "config.toml"), userCfg)
		args, err := (&CodexAgent{}).ArgsFor(AgentRequest{Dir: wt, MCPConfig: mcp, ExclusiveMCP: true, Env: []string{"CODEX_HOME=" + home}})
		if err != nil {
			t.Fatal(err)
		}
		return disabledArgs(args)
	}
	if got := run(trustedProject(repo)); !slices.Equal(got, []string{"mcp_servers.proj-only.enabled=false"}) {
		t.Fatalf("worktree của repo tin cậy phải tắt server dự án: %v", got)
	}
	if got := run(""); len(got) != 0 {
		t.Fatalf("repo chưa tin cậy: không tắt server dự án, được %v", got)
	}
	// Tin cậy một thư mục không liên quan thì không kế thừa.
	if got := run(trustedProject(t.TempDir())); len(got) != 0 {
		t.Fatalf("tin cậy thư mục khác: không tắt, được %v", got)
	}
}

// TOML hỏng trong .codex/config.toml của dự án CHƯA tin cậy (Codex không đọc nó) không làm hỏng bước; dự án đã tin cậy thì báo lỗi.
func TestCodexExclusiveMCPBrokenProjectTOML(t *testing.T) {
	mcp := filepath.Join(t.TempDir(), "mcp.json")
	writeMCPTestFile(t, mcp, `{"mcpServers":{"bint":{"command":"/x/bint","args":["mcp","serve"]}}}`)
	dir := t.TempDir()
	writeMCPTestFile(t, filepath.Join(dir, ".codex", "config.toml"), "[mcp_servers\n")
	home := t.TempDir()
	writeMCPTestFile(t, filepath.Join(home, "config.toml"), "[mcp_servers.global]\ncommand=\"x\"\n")
	req := AgentRequest{Dir: dir, MCPConfig: mcp, ExclusiveMCP: true, Env: []string{"CODEX_HOME=" + home}}
	args, err := (&CodexAgent{}).ArgsFor(req)
	if err != nil || !slices.Equal(disabledArgs(args), []string{"mcp_servers.global.enabled=false"}) {
		t.Fatalf("dự án chưa tin cậy: muốn bỏ qua TOML hỏng, được %v %v", args, err)
	}
	writeMCPTestFile(t, filepath.Join(home, "config.toml"), "[mcp_servers.global]\ncommand=\"x\"\n\n"+trustedProject(dir))
	if _, err = (&CodexAgent{}).ArgsFor(req); err == nil {
		t.Fatal("dự án đã tin cậy mà TOML hỏng: muốn lỗi")
	}
}
