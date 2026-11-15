package eval

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
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

func TestCodexExclusiveMCPProjectLayers(t *testing.T) {
	repo := testutil.NewRepo(t, map[string]string{"README.md": "fixture\n"})
	repo, _ = filepath.EvalSymlinks(repo)
	child, deep := filepath.Join(repo, "child"), filepath.Join(repo, "child", "deep")
	for path, name := range map[string]string{repo: "root_only", child: "child_only", deep: "deep_only"} {
		writeMCPTestFile(t, filepath.Join(path, ".codex", "config.toml"), "[mcp_servers."+name+"]\ncommand='fixture'\n")
	}
	home := t.TempDir()
	for _, tc := range []struct {
		name, config string
		want         []string
	}{
		{"root trusted", trustedProject(repo), []string{"child_only", "deep_only", "root_only"}},
		{"child trusted only", trustedProject(child), []string{"child_only"}},
		{"deep trusted only", trustedProject(deep), []string{"deep_only"}},
		{"child explicitly untrusted", trustedProject(repo) + "\n[projects." + tomlString(child) + "]\ntrust_level='untrusted'\n", []string{"deep_only", "root_only"}},
		{"unrelated ancestor trusted", trustedProject(filepath.Dir(repo)), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeMCPTestFile(t, filepath.Join(home, "config.toml"), tc.config)
			got, err := codexOtherMCPServers(AgentRequest{Dir: deep, Env: []string{"CODEX_HOME=" + home}})
			if err != nil || !slices.Equal(got, tc.want) {
				t.Fatalf("project servers = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

// A .git file can point at a separate Git directory or a submodule's metadata;
// neither common directory has to be named .git.
func newCodexGitFileRepo(t *testing.T, kind string) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo := testutil.NewRepo(t, nil)
	switch kind {
	case "separate-git-dir":
		testutil.Git(t, repo, "init", "-q", "--separate-git-dir", filepath.Join(t.TempDir(), "gitstore"))
	case "submodule":
		super := testutil.NewRepo(t, nil)
		testutil.Git(t, super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", repo, "sub")
		repo = filepath.Join(super, "sub")
	default:
		t.Fatalf("unknown Git fixture %q", kind)
	}
	root, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestCodexExclusiveMCPGitFileProjectLayers(t *testing.T) {
	for _, kind := range []string{"separate-git-dir", "submodule", "linked-separate-git-dir", "linked-submodule"} {
		t.Run(kind, func(t *testing.T) {
			repo := newCodexGitFileRepo(t, strings.TrimPrefix(kind, "linked-"))
			if strings.HasPrefix(kind, "linked-") {
				wt := filepath.Join(t.TempDir(), "linked")
				testutil.Git(t, repo, "worktree", "add", "-q", "-b", "task", wt)
				var err error
				repo, err = filepath.EvalSymlinks(wt)
				if err != nil {
					t.Fatal(err)
				}
			}
			child := filepath.Join(repo, "child")
			for path, name := range map[string]string{repo: "root_only", child: "child_only", filepath.Dir(repo): "outside_only"} {
				writeMCPTestFile(t, filepath.Join(path, ".codex", "config.toml"), "[mcp_servers."+name+"]\ncommand='fixture'\n")
			}
			home := t.TempDir()
			for _, tc := range []struct {
				name, config string
				want         []string
			}{
				{"root trusted", trustedProject(repo), []string{"child_only", "root_only"}},
				{"child explicitly untrusted", trustedProject(repo) + "\n[projects." + tomlString(child) + "]\ntrust_level='untrusted'\n", []string{"root_only"}},
				{"outside ancestor trusted", trustedProject(filepath.Dir(repo)), nil},
			} {
				t.Run(tc.name, func(t *testing.T) {
					writeMCPTestFile(t, filepath.Join(home, "config.toml"), tc.config)
					req := AgentRequest{Dir: child, Env: []string{"CODEX_HOME=" + home}}
					got, err := codexOtherMCPServers(req)
					if err != nil || !slices.Equal(got, tc.want) {
						t.Fatalf("project servers = %v, %v; want %v", got, err, tc.want)
					}
				})
			}
		})
	}
}

type codexTrustCase struct {
	name, config string
	want         []string
}

func codexMarkerFixture(t *testing.T) (string, []codexTrustCase) {
	t.Helper()
	repo := testutil.NewRepo(t, map[string]string{"README.md": "fixture\n"})
	repo, _ = filepath.EvalSymlinks(repo)
	outer := filepath.Dir(repo)
	child, deep := filepath.Join(repo, "child"), filepath.Join(repo, "child", "deep")
	for path, name := range map[string]string{outer: "outer_only", repo: "root_only", child: "child_only", deep: "deep_only"} {
		writeMCPTestFile(t, filepath.Join(path, ".codex", "config.toml"), "[mcp_servers."+name+"]\ncommand='fixture'\n")
	}
	writeMCPTestFile(t, filepath.Join(outer, "CUSTOM"), "")
	writeMCPTestFile(t, filepath.Join(child, "INNER"), "")
	absoluteMarker := filepath.Join(t.TempDir(), "BOUNDARY")
	writeMCPTestFile(t, absoluteMarker, "")
	missingAbsolute := filepath.Join(t.TempDir(), "MISSING")
	// A relative decoy distinguishes Rust's path join from filepath.Join
	// on Unix, without depending on whether the filesystem root has a .git.
	if filepath.VolumeName(missingAbsolute) == "" {
		writeMCPTestFile(t, filepath.Join(repo, missingAbsolute), "")
	}
	deny := func(dir string) string { return "[projects." + tomlString(dir) + "]\ntrust_level='untrusted'\n" }
	all := []string{"child_only", "deep_only", "outer_only", "root_only"}
	return deep, []codexTrustCase{
		{"absolute missing", "project_root_markers=[" + tomlString(missingAbsolute) + "]\n" + trustedProject(repo), []string{"deep_only"}},
		{"absolute existing", "project_root_markers=[" + tomlString(absoluteMarker) + "]\n" + trustedProject(repo), []string{"deep_only"}},
		{"absolute existing before relative", "project_root_markers=[" + tomlString(absoluteMarker) + ",'.git']\n" + trustedProject(repo), []string{"deep_only"}},
		{"absolute missing before relative", "project_root_markers=[" + tomlString(missingAbsolute) + ",'.git']\n" + trustedProject(repo), []string{"child_only", "deep_only", "root_only"}},
		{"disabled markers", "project_root_markers=[]\n" + trustedProject(repo), []string{"deep_only"}},
		{"missing marker", "project_root_markers=['MISSING']\n" + trustedProject(repo), []string{"deep_only"}},
		{"custom root trusted", "project_root_markers=['CUSTOM']\n" + trustedProject(outer), all},
		{"custom root git fallback", "project_root_markers=['CUSTOM']\n" + trustedProject(repo), all},
		{"custom root denial wins", "project_root_markers=['CUSTOM']\n" + deny(outer) + trustedProject(repo), []string{"root_only"}},
		{"exact git denial wins", "project_root_markers=['CUSTOM']\n" + trustedProject(outer) + deny(repo), []string{"child_only", "deep_only", "outer_only"}},
		{"inner root", "project_root_markers=['INNER']\n" + trustedProject(child), []string{"child_only", "deep_only"}},
		{"inner root git fallback", "project_root_markers=['INNER']\n" + trustedProject(repo), []string{"child_only", "deep_only"}},
		{"nearest marker", "project_root_markers=['CUSTOM','.git']\n" + trustedProject(repo), []string{"child_only", "deep_only", "root_only"}},
	}
}

func codexLinkedTrustFixture(t *testing.T) (string, []codexTrustCase) {
	t.Helper()
	repo := testutil.NewRepo(t, nil)
	repo, _ = filepath.EvalSymlinks(repo)
	wt := filepath.Join(t.TempDir(), "linked")
	testutil.Git(t, repo, "worktree", "add", "-q", "-b", "task", wt)
	wt, _ = filepath.EvalSymlinks(wt)
	child, deep := filepath.Join(wt, "child"), filepath.Join(wt, "child", "deep")
	for path, name := range map[string]string{wt: "root_only", child: "child_only", deep: "deep_only"} {
		writeMCPTestFile(t, filepath.Join(path, ".codex", "config.toml"), "[mcp_servers."+name+"]\ncommand='fixture'\n")
	}
	deny := func(dir string) string { return "[projects." + tomlString(dir) + "]\ntrust_level='untrusted'\n" }
	all := []string{"child_only", "deep_only", "root_only"}
	return deep, []codexTrustCase{
		{"worktree trusted", trustedProject(wt), all},
		{"worktree denied", trustedProject(repo) + deny(wt), nil},
		{"worktree overrides main denial", deny(repo) + trustedProject(wt), all},
		{"exact child denial", trustedProject(wt) + deny(child), []string{"deep_only", "root_only"}},
		{"exact child trust overrides root denial", deny(wt) + trustedProject(child), []string{"child_only"}},
		{"no markers use main", "project_root_markers=[]\n" + trustedProject(repo) + deny(wt), []string{"deep_only"}},
		{"no markers ignore worktree root", "project_root_markers=[]\n" + trustedProject(wt), nil},
	}
}

func TestCodexExclusiveMCPRootTrust(t *testing.T) {
	for _, fixture := range []struct {
		name string
		make func(*testing.T) (string, []codexTrustCase)
	}{{"markers", codexMarkerFixture}, {"linked", codexLinkedTrustFixture}} {
		t.Run(fixture.name, func(t *testing.T) {
			dir, cases := fixture.make(t)
			home := t.TempDir()
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					writeMCPTestFile(t, filepath.Join(home, "config.toml"), tc.config)
					got, err := codexOtherMCPServers(AgentRequest{Dir: dir, Env: []string{"CODEX_HOME=" + home}})
					if err != nil || !slices.Equal(got, tc.want) {
						t.Fatalf("project servers = %v, %v; want %v", got, err, tc.want)
					}
				})
			}
		})
	}
}

func TestCodexExclusiveMCPIgnoresParentGitEnvironment(t *testing.T) {
	repo := testutil.NewRepo(t, nil)
	repo, _ = filepath.EvalSymlinks(repo)
	other := testutil.NewRepo(t, nil)
	wt := filepath.Join(t.TempDir(), "linked")
	testutil.Git(t, repo, "worktree", "add", "-q", "-b", "task", wt)
	child := filepath.Join(wt, "child")
	for dir, name := range map[string]string{wt: "root_only", child: "child_only"} {
		writeMCPTestFile(t, filepath.Join(dir, ".codex", "config.toml"), "[mcp_servers."+name+"]\ncommand='fixture'\n")
	}
	home := t.TempDir()
	writeMCPTestFile(t, filepath.Join(home, "config.toml"), trustedProject(repo))
	for _, tc := range []struct{ key, value string }{
		{"GIT_DIR", filepath.Join(other, ".git")},
		{"GIT_WORK_TREE", other},
		{"GIT_COMMON_DIR", filepath.Join(other, ".git")},
		{"GIT_INDEX_FILE", filepath.Join(other, ".git", "index")},
	} {
		t.Setenv(tc.key, tc.value)
	}
	got, err := codexOtherMCPServers(AgentRequest{Dir: child, Env: []string{"CODEX_HOME=" + home}})
	if err != nil || !slices.Equal(got, []string{"child_only", "root_only"}) {
		t.Fatalf("parent Git environment changed child config resolution: %v, %v", got, err)
	}
	if os.Getenv("BINT_CODEX_CONFIG_LIVE") == "1" {
		req := AgentRequest{Dir: child, Env: []string{"CODEX_HOME=" + home}}
		off, err := codexDisableMCPArgs(req)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		args := append([]string{"--no-daemon"}, off...)
		args = append(args, "-c", `mcp_servers.bint.command="fixture-bint"`, "mcp", "list", "--json")
		cmd := exec.CommandContext(ctx, "codex", args...)
		cmd.Dir, cmd.Env = child, cleanEnv(req.Env...)
		raw, err := cmd.Output()
		if err != nil {
			t.Fatalf("Codex mcp list: %v", err)
		}
		var servers []struct {
			Name    string
			Enabled bool
		}
		if err := json.Unmarshal(raw, &servers); err != nil {
			t.Fatal(err)
		}
		var enabled []string
		for _, s := range servers {
			if s.Enabled {
				enabled = append(enabled, s.Name)
			}
		}
		if !slices.Equal(enabled, []string{"bint"}) {
			t.Fatalf("parent Git environment left extra servers enabled: %v", enabled)
		}
	}
}
