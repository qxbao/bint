package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"bint/internal/config"
	"bint/internal/mcpcat"
	"bint/internal/mcpcat/probe/probetest"
	"bint/internal/testutil"
)

func TestMain(m *testing.M) {
	probetest.MaybeRun() // MCP/codegraph giả: chạy lại chính tệp test (xem probetest)
	os.Exit(m.Run())
}

const secretValue = "hunter2-S3CRET"

var fakeEnvNames = []string{probetest.EnvMode, probetest.EnvTools, probetest.EnvDump}

// mcpFx là một repository đã init, cấu hình user cô lập và người dùng (không phải agent).
type mcpFx struct {
	root string
	dump string
}

func newMCPFx(t *testing.T, projectMCP string) *mcpFx {
	t.Helper()
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	mustInit(t, InitOptions{Dir: root, Hooks: no()})
	if projectMCP != "" {
		if err := os.WriteFile(config.LocalPath(root), []byte("mcp:\n"+projectMCP), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &mcpFx{root: root, dump: filepath.Join(t.TempDir(), "dump.jsonl")}
}

// secrets ghi secrets.env (0600) với cấu hình của bản giả và một credential không được lộ.
func (f *mcpFx) secrets(t *testing.T, mode, tools string) {
	t.Helper()
	body := fmt.Sprintf("%s=%s\n%s=%s\n%s=%s\nJIRA_MCP_PASSWORD=%s\nJIRA_MCP_URL=https://jira.example\nJIRA_MCP_USERNAME=bob\n",
		probetest.EnvMode, mode, probetest.EnvTools, tools, probetest.EnvDump, f.dump, secretValue)
	p := filepath.Join(config.MachineDir(), "secrets.env")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func fakeExe(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return exe
}

// register đăng ký service giả: lệnh là chính tệp test, cấu hình bản giả đi qua secrets.env.
func (f *mcpFx) register(t *testing.T, id string, extraEnv ...string) MCPAddResult {
	t.Helper()
	res, err := MCPAdd(context.Background(), f.root, MCPAddOptions{Service: id, Command: fakeExe(t), EnvNames: append(slices.Clone(fakeEnvNames), extraEnv...)})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// codegraphOnPath đặt một bản sao của tệp test tên `codegraph` vào đầu PATH: bản giả đóng vai CLI codegraph (init/sync).
func codegraphOnPath(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	name := "codegraph"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	data, err := os.ReadFile(fakeExe(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func codegraphTools() string { s, _ := mcpcat.Lookup("codegraph"); return strings.Join(s.Tools(), ",") }

func TestMCPAgentForbidden(t *testing.T) {
	f := newMCPFx(t, "  services: {codegraph: {}}\n")
	asAgent(t)
	ctx := context.Background()
	if _, err := MCPAdd(ctx, f.root, MCPAddOptions{Service: "codegraph"}); !errors.Is(err, ErrAgentForbidden) {
		t.Errorf("add: %v", err)
	}
	if _, err := MCPRemove(ctx, f.root, MCPRemoveOptions{Service: "codegraph"}); !errors.Is(err, ErrAgentForbidden) {
		t.Errorf("remove: %v", err)
	}
	if _, err := MCPIndex(ctx, f.root, MCPIndexOptions{Service: "codegraph"}, nil); !errors.Is(err, ErrAgentForbidden) {
		t.Errorf("index: %v", err)
	}
	if _, err := os.Stat(config.MCPMachinePath()); err == nil {
		t.Error("add bị chặn không được ghi mcp.yml")
	}
	// Agent được list, check, grants.
	if _, err := MCPList(ctx, f.root, MCPListOptions{}); err != nil {
		t.Errorf("list: %v", err)
	}
	if _, err := MCPCheck(ctx, f.root, MCPCheckOptions{}); err != nil {
		t.Errorf("check: %v", err)
	}
	if _, err := MCPGrants(ctx, f.root, MCPGrantsOptions{}); err != nil {
		t.Errorf("grants: %v", err)
	}
}

func TestMCPAddValidate(t *testing.T) {
	cases := []struct {
		name string
		o    MCPAddOptions
		want string
	}{
		{"thiếu service", MCPAddOptions{}, "cần tên service"},
		{"service ngoài danh mục", MCPAddOptions{Service: "gitnexus"}, "không có trong danh mục"},
		{"service bint", MCPAddOptions{Service: "bint"}, "không có trong danh mục"},
		{"url và command", MCPAddOptions{Service: "deepwiki", URL: "https://x", Command: "c"}, "loại trừ"},
		{"url cho stdio", MCPAddOptions{Service: "codegraph", URL: "https://x"}, "chỉ dùng cho service http"},
		{"command cho http", MCPAddOptions{Service: "deepwiki", Command: "c"}, "chỉ dùng cho service stdio"},
		{"url không phải http", MCPAddOptions{Service: "deepwiki", URL: "ftp://x"}, "không phải địa chỉ http"},
		{"arg không command", MCPAddOptions{Service: "codegraph", Args: []string{"x"}}, "--arg cần đi cùng"},
		{"tên biến có giá trị", MCPAddOptions{Service: "jira", EnvNames: []string{"A=" + secretValue}}, "chỉ nhận TÊN biến"},
	}
	for _, c := range cases {
		err := c.o.Validate()
		if err == nil || !strings.Contains(err.Error(), c.want) || !IsUsage(err) {
			t.Errorf("%s: %v, muốn lỗi cú pháp chứa %q", c.name, err, c.want)
		} else if strings.Contains(err.Error(), secretValue) {
			t.Errorf("%s: lỗi lộ giá trị", c.name)
		}
	}
	if err := (MCPAddOptions{Service: "codegraph"}).Validate(); err != nil {
		t.Errorf("hợp lệ: %v", err)
	}
}

func TestMCPAddInstallHintAndWarnings(t *testing.T) {
	f := newMCPFx(t, "")
	ctx := context.Background()
	t.Setenv("PATH", t.TempDir()) // không có binary nào
	res, err := MCPAdd(ctx, f.root, MCPAddOptions{Service: "codegraph"})
	if err != nil {
		t.Fatal(err)
	}
	cg, _ := mcpcat.Lookup("codegraph")
	if res.InstallHint != cg.Install || res.Check.Status != MCPStatusFail || !strings.Contains(res.Check.Detail, cg.Install) {
		t.Fatalf("thiếu binary: %+v", res)
	}
	if res.Registration.Command != "codegraph" || !slices.Equal(res.Registration.Args, []string{"serve", "--mcp"}) || res.Registration.Version != "1.6.2" {
		t.Fatalf("mặc định từ danh mục: %+v", res.Registration)
	}
	// Đã ghi vào mcp.yml dù check lỗi.
	m, err := config.LoadMachineMCP()
	if err != nil || m.Services["codegraph"].Command != "codegraph" {
		t.Fatalf("mcp.yml: %+v %v", m, err)
	}
	// Cảnh báo npx @latest / uvx / uv tool run; npx có ghim phiên bản thì không cảnh báo.
	warn := func(cmd string, args ...string) int {
		r, err := MCPAdd(ctx, f.root, MCPAddOptions{Service: "codegraph", Command: cmd, Args: args})
		if err != nil {
			t.Fatal(err)
		}
		return len(r.Warnings)
	}
	if warn("npx", "-y", "@colbymchenry/codegraph@latest", "serve") != 1 || warn("uvx", "x") != 1 || warn("uv", "tool", "run", "x") != 1 {
		t.Error("npx @latest, uvx, uv tool run phải có cảnh báo")
	}
	if warn("npx", "-y", "@colbymchenry/codegraph@1.6.2") != 0 || warn("codegraph") != 0 {
		t.Error("lệnh ghim phiên bản không được cảnh báo")
	}
	// Phát hiện .5: npx không có @<phiên bản> thực chất là latest, cũng phải cảnh báo.
	if warn("npx", "-y", "chrome-devtools-mcp") != 1 || warn("npx", "-y", "@colbymchenry/codegraph", "serve") != 1 ||
		warn("npx", "--package", "pkg", "x") != 1 || warn("npx", "--package=pkg@2.0.0", "x") != 0 || warn("npx", "-y", "pkg@") != 1 {
		t.Error("npx không ghim phiên bản phải có cảnh báo; có ghim thì không")
	}
	// Đăng ký lại là cập nhật.
	if r, _ := MCPAdd(ctx, f.root, MCPAddOptions{Service: "codegraph"}); !r.Replaced {
		t.Error("đăng ký lại phải báo Replaced")
	}
}

// Phát hiện .4: service http chưa dùng credential, nên --env-name bị từ chối và check không đòi biến.
func TestMCPHTTPServiceHasNoCredential(t *testing.T) {
	f := newMCPFx(t, "")
	ctx := context.Background()
	err := MCPAddOptions{Service: "context7", EnvNames: []string{"CONTEXT7_API_KEY"}}.Validate()
	if err == nil || !IsUsage(err) || !strings.Contains(err.Error(), "--env-name không dùng được cho service http") {
		t.Fatalf("--env-name cho service http: %v", err)
	}
	if _, err := MCPAdd(ctx, f.root, MCPAddOptions{Service: "context7", EnvNames: []string{"CONTEXT7_API_KEY"}}); !IsUsage(err) {
		t.Fatalf("add: %v", err)
	}
	if _, statErr := os.Stat(config.MCPMachinePath()); statErr == nil {
		t.Error("add bị từ chối không được ghi mcp.yml")
	}
	// Đăng ký cũ (sửa tay) có env_names cho service http: check vẫn chạy, không fail vì biến thiếu.
	srv := httptest.NewServer(rpcHandler(false, nil))
	defer srv.Close()
	m, _ := config.LoadMachineMCP()
	m.Services = map[string]config.Registration{"context7": {URL: srv.URL, EnvNames: []string{"CONTEXT7_API_KEY"}}}
	if err := config.SaveMachineMCP(m); err != nil {
		t.Fatal(err)
	}
	res, err := MCPCheck(ctx, f.root, MCPCheckOptions{Service: "context7"})
	if err != nil || len(res) != 1 || len(res[0].MissingEnv) != 0 || strings.Contains(res[0].Detail, "credential") {
		t.Fatalf("check http không được đòi credential: %+v %v", res, err)
	}
}

func TestMCPAddThenCheckOK(t *testing.T) {
	f := newMCPFx(t, "")
	t.Setenv("BINT_TEST_LEAK", "leaked-from-shell")
	f.secrets(t, "ok", codegraphTools())
	res := f.register(t, "codegraph")
	if res.Check.Status != MCPStatusOK || len(res.Check.Tools) != 8 || len(res.Check.Missing) != 0 || len(res.Check.Extra) != 0 {
		t.Fatalf("check sau add: %+v", res.Check)
	}
	dumps, err := probetest.ReadDumps(f.dump)
	if err != nil || len(dumps) != 1 {
		t.Fatalf("dump: %v %v", dumps, err)
	}
	d := dumps[0]
	wantCwd, _ := filepath.EvalSymlinks(f.root)
	gotCwd, _ := filepath.EvalSymlinks(d.Cwd)
	if gotCwd != wantCwd {
		t.Errorf("cwd = %s, muốn gốc worktree %s", gotCwd, wantCwd)
	}
	if d.Env["CODEGRAPH_TELEMETRY"] != "0" || len(strings.Split(d.Env["CODEGRAPH_MCP_TOOLS"], ",")) != 8 || d.Env[probetest.EnvMode] != "ok" || d.Env["PATH"] == "" {
		t.Errorf("env mặc định/credential: %v", d.Env)
	}
	if _, leaked := d.Env["BINT_TEST_LEAK"]; leaked {
		t.Error("env của shell không được kế thừa")
	}
	if _, leaked := d.Env["JIRA_MCP_PASSWORD"]; leaked {
		t.Error("chỉ biến khai báo trong EnvNames được truyền")
	}
	b, _ := json.Marshal(res)
	if strings.Contains(string(b), secretValue) {
		t.Error("kết quả lộ credential")
	}
	// mcp.yml không chứa giá trị.
	raw, _ := os.ReadFile(config.MCPMachinePath())
	if strings.Contains(string(raw), secretValue) || strings.Contains(string(raw), f.dump) {
		t.Errorf("mcp.yml chứa giá trị credential:\n%s", raw)
	}
}

func TestMCPRemove(t *testing.T) {
	f := newMCPFx(t, "  services: {codegraph: {}}\n")
	ctx := context.Background()
	if _, err := MCPRemove(ctx, f.root, MCPRemoveOptions{Service: "codegraph"}); err == nil || !strings.Contains(err.Error(), "chưa đăng ký") {
		t.Fatalf("remove chưa đăng ký: %v", err)
	}
	if _, err := MCPRemove(ctx, f.root, MCPRemoveOptions{Service: "bint"}); !IsUsage(err) {
		t.Fatalf("remove ngoài danh mục: %v", err)
	}
	f.secrets(t, "ok", codegraphTools())
	f.register(t, "codegraph")
	res, err := MCPRemove(ctx, f.root, MCPRemoveOptions{Service: "codegraph"})
	if err != nil || !res.ProjectUses {
		t.Fatalf("remove: %+v %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(config.MachineDir(), "secrets.env")); err != nil {
		t.Error("remove không được đụng secrets.env")
	}
	list, _ := MCPList(ctx, f.root, MCPListOptions{})
	for _, s := range list {
		if s.ID == "codegraph" && (s.Registered || !s.ProjectUses || !s.Required) {
			t.Errorf("sau remove: %+v", s)
		}
	}
}

func TestMCPListOutsideRepo(t *testing.T) {
	testutil.Isolate(t)
	list, err := MCPList(context.Background(), t.TempDir(), MCPListOptions{})
	if err != nil || len(list) != len(mcpcat.All()) {
		t.Fatalf("%v %v", list, err)
	}
	for _, s := range list {
		if s.ProjectUses || s.Registered {
			t.Errorf("%+v", s)
		}
	}
}

func TestMCPCheckMissingEnv(t *testing.T) {
	f := newMCPFx(t, "")
	// secrets.env chỉ có biến của bản giả, thiếu JIRA_*.
	body := fmt.Sprintf("%s=ok\n%s=x\n%s=%s\n", probetest.EnvMode, probetest.EnvTools, probetest.EnvDump, f.dump)
	os.MkdirAll(config.MachineDir(), 0o700)
	os.WriteFile(filepath.Join(config.MachineDir(), "secrets.env"), []byte(body), 0o600)
	res := f.register(t, "jira")
	c := res.Check
	if c.Status != MCPStatusFail || !slices.Equal(c.MissingEnv, []string{"JIRA_MCP_PASSWORD", "JIRA_MCP_URL", "JIRA_MCP_USERNAME"}) {
		t.Fatalf("check: %+v", c)
	}
	if _, err := os.Stat(f.dump); err == nil {
		t.Error("thiếu credential thì không được khởi động service")
	}
}

func TestMCPCheckToolsComparison(t *testing.T) {
	f := newMCPFx(t, "")
	f.secrets(t, "ok", "codegraph_explore,codegraph_status,codegraph_zzz")
	res := f.register(t, "codegraph")
	c := res.Check
	if c.Status != MCPStatusDegraded || len(c.Missing) != 6 || !slices.Equal(c.Extra, []string{"codegraph_zzz"}) || !strings.Contains(c.Detail, "codegraph_node") {
		t.Fatalf("check: %+v", c)
	}
}

func TestMCPCheckCodegraphDegraded(t *testing.T) {
	for _, mode := range []string{"status_error", "status_rpcerror"} {
		t.Run(mode, func(t *testing.T) {
			f := newMCPFx(t, "")
			f.secrets(t, mode, codegraphTools())
			c := f.register(t, "codegraph").Check
			if c.Status != MCPStatusDegraded || !strings.Contains(c.Detail, "codegraph daemon") || !strings.Contains(c.Detail, "daemon.pid") {
				t.Fatalf("check: %+v", c)
			}
			if len(c.Missing) != 0 {
				t.Errorf("đủ tool mà báo thiếu: %+v", c)
			}
		})
	}
}

// Phát hiện .2: chỉ lỗi báo tool bị tắt mới đổ cho daemon env hẹp; lỗi khác báo nguyên văn, không khuyên dừng daemon.
func TestMCPCheckCodegraphOtherErrors(t *testing.T) {
	for _, mode := range []string{"status_error_other", "status_rpcerror_other"} {
		for _, indexed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/indexed=%v", mode, indexed), func(t *testing.T) {
				f := newMCPFx(t, "")
				f.secrets(t, mode, codegraphTools())
				if indexed {
					if err := os.MkdirAll(filepath.Join(f.root, ".codegraph"), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(f.root, ".codegraph", "codegraph.db"), []byte("x"), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				c := f.register(t, "codegraph").Check
				if c.Status != MCPStatusDegraded || strings.Contains(c.Detail, "codegraph daemon") || strings.Contains(c.Detail, "daemon.pid") {
					t.Fatalf("lỗi không phải tool bị tắt không được đổ cho daemon: %+v", c)
				}
				want := "database not initialized"
				if mode == "status_rpcerror_other" {
					want = "internal error: timeout"
				}
				if !strings.Contains(c.Detail, want) {
					t.Errorf("phải giữ nguyên văn lỗi %q: %q", want, c.Detail)
				}
				if hasIndexHint := strings.Contains(c.Detail, "bint mcp index codegraph"); hasIndexHint == indexed {
					t.Errorf("gợi ý index khi chưa có chỉ mục (indexed=%v): %q", indexed, c.Detail)
				}
			})
		}
	}
}

func TestMCPCheckTimeout(t *testing.T) {
	old := mcpCheckTimeout
	mcpCheckTimeout = 600 * time.Millisecond
	defer func() { mcpCheckTimeout = old }()
	f := newMCPFx(t, "")
	f.secrets(t, "hang", "x")
	start := time.Now()
	c := f.register(t, "codegraph").Check
	if c.Status != MCPStatusFail || !strings.Contains(c.Detail, "initialize") {
		t.Fatalf("check: %+v", c)
	}
	if time.Since(start) > 15*time.Second {
		t.Fatal("check treo quá lâu")
	}
}

func TestMCPCheckNeverLeaksSecret(t *testing.T) {
	f := newMCPFx(t, "")
	f.secrets(t, "die", "x") // tiến trình chết và in credential ra stderr
	res := f.register(t, "jira")
	b, _ := json.Marshal(res)
	out := string(b)
	if strings.Contains(out, secretValue) {
		t.Fatalf("kết quả lộ credential: %s", out)
	}
	c := res.Check
	if c.Status != MCPStatusFail || !strings.Contains(c.Detail, "boom") || !strings.Contains(c.Detail, "***") {
		t.Fatalf("stderr phải còn dấu vết nhưng đã che: %+v", c)
	}
}

func TestMCPCheckSecretsPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("không kiểm quyền trên Windows")
	}
	f := newMCPFx(t, "")
	f.secrets(t, "ok", codegraphTools())
	p := filepath.Join(config.MachineDir(), "secrets.env")
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	res := f.register(t, "codegraph")
	if len(res.Check.Warnings) == 0 || !strings.Contains(res.Check.Warnings[0], "secrets.env") {
		t.Fatalf("phải cảnh báo quyền: %+v", res.Check)
	}
}

// rpcHandler là MCP server http giả; sse chọn dạng phản hồi.
func rpcHandler(sse bool, tools []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m struct {
			ID     *int64 `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&m)
		if m.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result string
		switch m.Method {
		case "initialize":
			result = `{"protocolVersion":"2025-06-18","serverInfo":{"name":"w","version":"1"}}`
		case "tools/list":
			var ts []string
			for _, t := range tools {
				ts = append(ts, fmt.Sprintf(`{"name":%q}`, t))
			}
			result = `{"tools":[` + strings.Join(ts, ",") + `]}`
		default:
			result = `{}`
		}
		body := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":%s}`, *m.ID, result)
		if sse {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", body)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	})
}

func TestMCPCheckHTTP(t *testing.T) {
	dw, _ := mcpcat.Lookup("deepwiki")
	for _, sse := range []bool{false, true} {
		t.Run(fmt.Sprintf("sse=%v", sse), func(t *testing.T) {
			f := newMCPFx(t, "")
			srv := httptest.NewServer(rpcHandler(sse, dw.Tools()[:2]))
			defer srv.Close()
			res, err := MCPAdd(context.Background(), f.root, MCPAddOptions{Service: "deepwiki", URL: srv.URL})
			if err != nil {
				t.Fatal(err)
			}
			c := res.Check
			if c.Status != MCPStatusDegraded || len(c.Tools) != 2 || !slices.Equal(c.Missing, dw.Tools()[2:]) {
				t.Fatalf("check: %+v", c)
			}
		})
	}
	// Server từ chối: fail kèm mã HTTP.
	f := newMCPFx(t, "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "no", 500) }))
	defer srv.Close()
	res, _ := MCPAdd(context.Background(), f.root, MCPAddOptions{Service: "deepwiki", URL: srv.URL})
	if res.Check.Status != MCPStatusFail || !strings.Contains(res.Check.Detail, "500") {
		t.Fatalf("check: %+v", res.Check)
	}
}

func TestMCPCheckUnregistered(t *testing.T) {
	f := newMCPFx(t, "")
	if _, err := MCPCheck(context.Background(), f.root, MCPCheckOptions{Service: "codegraph"}); err == nil || !strings.Contains(err.Error(), "chưa đăng ký") {
		t.Fatalf("check service chưa đăng ký: %v", err)
	}
	if r, err := MCPCheck(context.Background(), f.root, MCPCheckOptions{}); err != nil || len(r) != 0 {
		t.Fatalf("không có service nào: %v %v", r, err)
	}
}

func TestMCPCheckStateCwd(t *testing.T) {
	f := newMCPFx(t, "")
	f.secrets(t, "ok", "backward_compatibility_check,run_contract_test,run_resiliency_test,manage_mock_server")
	c := f.register(t, "specmatic").Check
	if c.Status != MCPStatusOK {
		t.Fatalf("check: %+v", c)
	}
	dumps, _ := probetest.ReadDumps(f.dump)
	if len(dumps) != 1 || !strings.Contains(filepath.ToSlash(dumps[0].Cwd), "/mcp/specmatic/") {
		t.Fatalf("cwd của specmatic phải nằm trong thư mục trạng thái: %+v", dumps)
	}
}

func TestMCPIndex(t *testing.T) {
	f := newMCPFx(t, "  services: {codegraph: {}}\n")
	ctx := context.Background()
	if _, err := MCPIndex(ctx, f.root, MCPIndexOptions{Service: "jira"}, nil); err == nil || !IsUsage(err) {
		t.Fatalf("index service khác codegraph: %v", err)
	}
	if _, err := MCPIndex(ctx, f.root, MCPIndexOptions{Service: "codegraph"}, nil); err == nil || !strings.Contains(err.Error(), "chưa đăng ký") {
		t.Fatalf("index chưa đăng ký: %v", err)
	}
	f.secrets(t, "ok", codegraphTools())
	f.register(t, "codegraph")
	codegraphOnPath(t)
	var out bytes.Buffer
	res, err := MCPIndex(ctx, f.root, MCPIndexOptions{Service: "codegraph"}, &out)
	if err != nil || res.Action != "init" || !res.ExcludeAdded {
		t.Fatalf("lần 1: %+v %v", res, err)
	}
	if !strings.Contains(out.String(), "fake codegraph init -y") {
		t.Errorf("đầu ra của codegraph phải chuyển ra writer: %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(f.root, ".codegraph", "codegraph.db")); err != nil {
		t.Fatal(err)
	}
	res, err = MCPIndex(ctx, f.root, MCPIndexOptions{Service: "codegraph"}, &out)
	if err != nil || res.Action != "sync" || res.ExcludeAdded {
		t.Fatalf("lần 2: %+v %v", res, err)
	}
	dumps, _ := probetest.ReadDumps(f.dump)
	// dump 0 và 1 là các lần check của add; các lần index đứng sau.
	var idx [][]string
	for _, d := range dumps {
		if len(d.Args) > 0 && (d.Args[0] == "init" || d.Args[0] == "sync") {
			idx = append(idx, d.Args)
			wantCwd, _ := filepath.EvalSymlinks(f.root)
			if got, _ := filepath.EvalSymlinks(d.Cwd); got != wantCwd {
				t.Errorf("cwd index = %s, muốn %s", got, wantCwd)
			}
		}
	}
	if len(idx) != 2 || !slices.Equal(idx[0], []string{"init", "-y"}) || !slices.Equal(idx[1], []string{"sync"}) {
		t.Fatalf("lệnh đã chạy: %v", idx)
	}
	ex, _ := os.ReadFile(res.ExcludeFile)
	if n := strings.Count(string(ex), ".codegraph/"); n != 1 {
		t.Fatalf("info/exclude có %d dòng .codegraph/:\n%s", n, ex)
	}
	// .codegraph/ bị bỏ qua nên cây làm việc vẫn sạch.
	if st := testutil.Git(t, f.root, "status", "--porcelain", "--", ".codegraph"); strings.TrimSpace(st) != "" {
		t.Errorf("git status thấy .codegraph: %q", st)
	}
}

// Phát hiện .1: lệnh đã đăng ký là lệnh bọc (npx/node/uvx) thì index không được chạy `<lệnh> init -y`.
func TestMCPIndexIgnoresWrapperLauncher(t *testing.T) {
	f := newMCPFx(t, "")
	f.secrets(t, "ok", codegraphTools())
	ctx := context.Background()
	// Lệnh bọc `npx` là bản giả ghi dump: nếu index chạy nó thì dump có args init/sync.
	dir := t.TempDir()
	npx := "npx"
	if runtime.GOOS == "windows" {
		npx += ".exe"
	}
	data, err := os.ReadFile(fakeExe(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, npx), data, 0o755); err != nil {
		t.Fatal(err)
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("không có git")
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+filepath.Dir(gitPath)) // không có binary codegraph
	if _, err := MCPAdd(ctx, f.root, MCPAddOptions{Service: "codegraph", Command: "npx", Args: []string{"-y", "@colbymchenry/codegraph@1.6.2", "serve", "--mcp"},
		EnvNames: slices.Clone(fakeEnvNames)}); err != nil {
		t.Fatal(err)
	}
	before, _ := probetest.ReadDumps(f.dump)
	_, err = MCPIndex(ctx, f.root, MCPIndexOptions{Service: "codegraph"}, nil)
	if err == nil || !strings.Contains(err.Error(), "không phải binary codegraph") || !strings.Contains(err.Error(), "npx") {
		t.Fatalf("index với lệnh bọc npx phải từ chối kèm hướng dẫn: %v", err)
	}
	after, _ := probetest.ReadDumps(f.dump)
	if len(after) != len(before) {
		t.Fatalf("index đã chạy lệnh bọc: %+v", after[len(before):])
	}
	if _, err := os.Stat(filepath.Join(f.root, ".codegraph")); err == nil {
		t.Error("không được tạo chỉ mục")
	}
	// Có binary codegraph trên PATH thì index chạy binary đó, không chạy npx đã đăng ký.
	codegraphOnPath(t)
	res, err := MCPIndex(ctx, f.root, MCPIndexOptions{Service: "codegraph"}, nil)
	if err != nil || res.Action != "init" {
		t.Fatalf("index với codegraph trên PATH: %+v %v", res, err)
	}
	after, _ = probetest.ReadDumps(f.dump)
	for _, d := range after[len(before):] {
		if len(d.Args) == 0 || (d.Args[0] != "init" && d.Args[0] != "sync") {
			t.Errorf("chỉ init/sync của codegraph được chạy: %v", d.Args)
		}
	}
}

func TestMCPIndexLinkedWorktree(t *testing.T) {
	f := newMCPFx(t, "")
	f.secrets(t, "ok", codegraphTools())
	f.register(t, "codegraph")
	codegraphOnPath(t)
	wt := filepath.Join(t.TempDir(), "wt")
	testutil.Git(t, f.root, "worktree", "add", "-b", "feature", wt)
	res, err := MCPIndex(context.Background(), wt, MCPIndexOptions{Service: "codegraph"}, nil)
	if err != nil || res.Action != "init" {
		t.Fatalf("%+v %v", res, err)
	}
	want, _ := filepath.EvalSymlinks(filepath.Join(f.root, ".git", "info", "exclude"))
	if got, _ := filepath.EvalSymlinks(res.ExcludeFile); got != want {
		t.Errorf("worktree liên kết phải ghi vào exclude của git common dir: %s, muốn %s", got, want)
	}
	if _, err := os.Stat(filepath.Join(wt, ".codegraph", "codegraph.db")); err != nil {
		t.Errorf("chỉ mục phải nằm ở worktree hiện tại: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.root, ".codegraph")); err == nil {
		t.Error("không được tạo chỉ mục ở worktree gốc")
	}
}

func TestEnsureGitExcludeVariants(t *testing.T) {
	root := testutil.NewRepo(t, nil)
	ctx := context.Background()
	ex := filepath.Join(root, ".git", "info", "exclude")
	os.MkdirAll(filepath.Dir(ex), 0o755)
	os.WriteFile(ex, []byte("# c\n/.codegraph/\n"), 0o644)
	if _, added, err := ensureGitExclude(ctx, root, ".codegraph/"); err != nil || added {
		t.Fatalf("dòng tương đương đã có: added=%v err=%v", added, err)
	}
	os.WriteFile(ex, []byte("*.log"), 0o644) // không có xuống dòng cuối
	if _, added, err := ensureGitExclude(ctx, root, ".codegraph/"); err != nil || !added {
		t.Fatalf("added=%v err=%v", added, err)
	}
	if b, _ := os.ReadFile(ex); string(b) != "*.log\n.codegraph/\n" {
		t.Fatalf("nội dung: %q", b)
	}
	os.Remove(ex)
	if _, added, err := ensureGitExclude(ctx, root, ".codegraph/"); err != nil || !added {
		t.Fatalf("tệp chưa có: added=%v err=%v", added, err)
	}
}

func TestMCPGrants(t *testing.T) {
	f := newMCPFx(t, `  services: {codegraph: {}, jira: {required: false}}
  grants:
    planner: [codegraph.explore, codegraph.*, deepwiki.read]
    reviewer: [jira.read]
`)
	ctx := context.Background()
	m := config.MachineMCP{Services: map[string]config.Registration{"codegraph": {Command: "codegraph", Args: []string{"serve", "--mcp"}}}}
	if err := config.SaveMachineMCP(m); err != nil {
		t.Fatal(err)
	}
	roles, err := MCPGrants(ctx, f.root, MCPGrantsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]MCPRoleGrants{}
	var names []string
	for _, r := range roles {
		by[r.Role] = r
		names = append(names, r.Role)
	}
	if !slices.Equal(names, []string{"planner", "reviewer", "interactive"}) {
		t.Fatalf("roles: %v", names)
	}
	pl := by["planner"]
	var agent []string
	for _, tl := range pl.Tools {
		agent = append(agent, tl.Agent)
	}
	if agent[0] != "cg_explore" || len(pl.Tools) != 8+3 {
		t.Fatalf("planner (đã bỏ trùng cg_explore): %v", agent)
	}
	if !slices.Contains(agent, "wiki_read_wiki_structure") || !slices.Contains(agent, "cg_status") {
		t.Errorf("planner: %v", agent)
	}
	joined := strings.Join(pl.Warnings, "\n")
	if !strings.Contains(joined, "deepwiki được cấp nhưng máy này chưa đăng ký") || !strings.Contains(joined, "deepwiki được cấp nhưng dự án không khai báo") {
		t.Errorf("planner cảnh báo: %v", pl.Warnings)
	}
	if strings.Contains(joined, "codegraph") {
		t.Errorf("codegraph đã đăng ký và khai báo, không được cảnh báo: %v", pl.Warnings)
	}
	rv := by["reviewer"]
	if len(rv.Tools) == 0 || len(rv.Warnings) != 1 || !strings.Contains(rv.Warnings[0], "jira") || rv.Implicit {
		t.Errorf("reviewer: %+v", rv)
	}
	// interactive không khai báo (D4): chỉ service dự án khai báo VÀ máy đã đăng ký → codegraph, không có jira.
	in := by["interactive"]
	if !in.Implicit || len(in.Tools) != 8 || len(in.Warnings) != 0 || in.Tools[0].Service != "codegraph" {
		t.Errorf("interactive: %+v", in)
	}
	// --role.
	one, err := MCPGrants(ctx, f.root, MCPGrantsOptions{Role: "reviewer"})
	if err != nil || len(one) != 1 || one[0].Role != "reviewer" {
		t.Fatalf("--role: %+v %v", one, err)
	}
	if one, err := MCPGrants(ctx, f.root, MCPGrantsOptions{Role: "interactive"}); err != nil || len(one) != 1 {
		t.Fatalf("--role interactive: %+v %v", one, err)
	}
	if _, err := MCPGrants(ctx, f.root, MCPGrantsOptions{Role: "nobody"}); !IsUsage(err) {
		t.Fatalf("role lạ: %v", err)
	}
}

func TestMCPGrantsExplicitInteractiveAndNotInitialized(t *testing.T) {
	f := newMCPFx(t, "  services: {codegraph: {}}\n  grants: {interactive: [codegraph.explore]}\n")
	roles, err := MCPGrants(context.Background(), f.root, MCPGrantsOptions{})
	if err != nil || len(roles) != 1 || roles[0].Implicit || len(roles[0].Tools) != 1 {
		t.Fatalf("%+v %v", roles, err)
	}
	if _, err := MCPGrants(context.Background(), t.TempDir(), MCPGrantsOptions{}); err == nil {
		t.Fatal("ngoài repository phải lỗi")
	}
}

func checkOf(rep *DoctorReport, name string) (Check, bool) {
	for _, c := range rep.Checks {
		if c.Name == name {
			return c, true
		}
	}
	return Check{}, false
}

func TestDoctorMCP(t *testing.T) {
	f := newMCPFx(t, "  services: {codegraph: {}, specmatic: {required: false}}\n")
	ctx := context.Background()

	rep := Doctor(ctx, f.root)
	if c, ok := checkOf(rep, "MCP codegraph"); !ok || c.Level != LevelWarn || !strings.Contains(c.Detail, "chưa đăng ký") {
		t.Errorf("codegraph chưa đăng ký: %+v", c)
	}
	if c, ok := checkOf(rep, "MCP specmatic"); !ok || c.Level != LevelInfo {
		t.Errorf("specmatic tuỳ chọn: %+v", c)
	}
	if c, ok := checkOf(rep, "MCP codegraph chỉ mục"); !ok || c.Level != LevelWarn || !strings.Contains(c.Detail, "bint mcp index codegraph") {
		t.Errorf("thiếu chỉ mục: %+v", c)
	}
	if rep.Failed() {
		t.Error("các cảnh báo MCP không được làm doctor fail")
	}

	// Đã đăng ký + có chỉ mục: hết cảnh báo; doctor không khởi động tiến trình MCP.
	f.secrets(t, "ok", codegraphTools())
	m := config.MachineMCP{Services: map[string]config.Registration{"codegraph": {Command: fakeExe(t), EnvNames: fakeEnvNames}}}
	if err := config.SaveMachineMCP(m); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(f.root, ".codegraph"), 0o755)
	os.WriteFile(filepath.Join(f.root, ".codegraph", "codegraph.db"), []byte("x"), 0o644)
	rep = Doctor(ctx, f.root)
	if c, _ := checkOf(rep, "MCP codegraph"); c.Level != LevelOK {
		t.Errorf("đã đăng ký: %+v", c)
	}
	if _, ok := checkOf(rep, "MCP codegraph chỉ mục"); ok {
		t.Error("đã có chỉ mục mà vẫn báo")
	}
	if _, err := os.Stat(f.dump); err == nil {
		t.Error("doctor không được khởi động tiến trình MCP")
	}

	// secrets.env quyền lỏng (chỉ Unix).
	if runtime.GOOS != "windows" {
		if _, ok := checkOf(rep, "secrets.env"); ok {
			t.Error("0600 không được báo")
		}
		p := filepath.Join(config.MachineDir(), "secrets.env")
		os.Chmod(p, 0o644)
		if c, ok := checkOf(Doctor(ctx, f.root), "secrets.env"); !ok || c.Level != LevelWarn || !strings.Contains(c.Detail, "chmod 600") {
			t.Errorf("quyền lỏng: %+v", c)
		}
	}

	// mcp.yml hỏng.
	os.WriteFile(config.MCPMachinePath(), []byte("version: 1\nlạ: 1\n"), 0o600)
	if c, ok := checkOf(Doctor(ctx, f.root), "mcp.yml"); !ok || c.Level != LevelWarn {
		t.Errorf("mcp.yml hỏng: %+v", c)
	}
}

func TestDoctorMCPQuietWithoutConfig(t *testing.T) {
	f := newMCPFx(t, "")
	for _, c := range Doctor(context.Background(), f.root).Checks {
		if strings.HasPrefix(c.Name, "MCP ") && strings.Contains(c.Detail, "mcp.services") {
			t.Errorf("dự án không khai báo gì mà vẫn báo: %+v", c)
		}
	}
}

// Role chỉ đọc (reviewer, planner): tool ghi vào cây làm việc không được cấp dù grants có ghi, và có cảnh báo.
func TestMCPGrantsReadOnlyRoleDropsWriteTools(t *testing.T) {
	f := newMCPFx(t, `  services: {specmatic: {}}
  grants:
    reviewer: [specmatic.*]
    planner: [specmatic.*]
    implementer: [specmatic.*]
`)
	m := config.MachineMCP{Services: map[string]config.Registration{"specmatic": {Command: "specmatic-mcp"}}}
	if err := config.SaveMachineMCP(m); err != nil {
		t.Fatal(err)
	}
	roles, err := MCPGrants(context.Background(), f.root, MCPGrantsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]MCPRoleGrants{}
	for _, r := range roles {
		by[r.Role] = r
	}
	names := func(r MCPRoleGrants) []string {
		var out []string
		for _, tl := range r.Tools {
			out = append(out, tl.Agent)
		}
		return out
	}
	for _, role := range []string{"reviewer", "planner"} {
		r := by[role]
		if !slices.Equal(names(r), []string{"spec_backward_compatibility_check"}) {
			t.Errorf("%s: %v", role, names(r))
		}
		w := strings.Join(r.Warnings, "\n")
		if !strings.Contains(w, "spec_manage_mock_server") || !strings.Contains(w, "chỉ đọc") {
			t.Errorf("%s: cảnh báo thiếu spec_manage_mock_server: %v", role, r.Warnings)
		}
	}
	impl := by["implementer"]
	if len(impl.Tools) != 4 || len(impl.Warnings) != 0 {
		t.Errorf("implementer (bước ghi) phải đủ tool và không cảnh báo: %v %v", names(impl), impl.Warnings)
	}
	one, err := MCPGrants(context.Background(), f.root, MCPGrantsOptions{Role: "reviewer"})
	if err != nil || len(one) != 1 || len(one[0].Tools) != 1 {
		t.Fatalf("--role reviewer: %+v %v", one, err)
	}
}
