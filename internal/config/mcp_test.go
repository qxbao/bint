package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"bint/internal/testutil"
)

func mcpCfg(t *testing.T, yml string) error {
	t.Helper()
	c := Default()
	if err := Decode(strings.NewReader(yml), &c); err != nil {
		return err
	}
	return c.Validate()
}

func TestValidateMCP(t *testing.T) {
	ok := []string{
		"mcp:\n  services: {codegraph: {}, deepwiki: {}}\n  grants: {planner: [codegraph.explore, deepwiki.read]}\n",
		"mcp:\n  services:\n    specmatic: {required: false, options: {specs: \"api/**\"}}\n  grants: {interactive: [specmatic.*]}\n",
		// service được cấp mà dự án không khai báo: chỉ doctor/grants cảnh báo, Validate không lỗi.
		"mcp:\n  grants: {planner: [jira.read]}\n",
		"mcp:\n  services: {codegraph: }\n",
	}
	for _, y := range ok {
		if err := mcpCfg(t, y); err != nil {
			t.Errorf("phải hợp lệ:\n%s\n=> %v", y, err)
		}
	}
	bad := []struct{ name, yml, want string }{
		{"service lạ", "mcp:\n  services: {gitnexus: {}}\n", `service "gitnexus" không có trong danh mục`},
		{"service bint", "mcp:\n  services: {bint: {}}\n", `không có trong danh mục`},
		{"option lạ", "mcp:\n  services: {codegraph: {options: {x: y}}}\n", `tuỳ chọn "x"`},
		{"option của service khác", "mcp:\n  services: {codegraph: {options: {specs: a}}}\n", `tuỳ chọn "specs"`},
		{"role rỗng", "mcp:\n  grants: {\"\": [codegraph.explore]}\n", "tên role"},
		{"role có khoảng trắng", "mcp:\n  grants: {\"a b\": [codegraph.explore]}\n", "tên role"},
		{"thiếu nhóm", "mcp:\n  grants: {r: [codegraph]}\n", "cần dạng <service>.<nhóm>"},
		{"nhóm rỗng", "mcp:\n  grants: {r: [\"codegraph.\"]}\n", "cần dạng <service>.<nhóm>"},
		{"nhiều dấu chấm", "mcp:\n  grants: {r: [codegraph.explore.x]}\n", "cần dạng <service>.<nhóm>"},
		{"nhóm lạ", "mcp:\n  grants: {r: [codegraph.nope]}\n", `nhóm "nope"`},
		{"service lạ trong grants", "mcp:\n  grants: {r: [nope.read]}\n", `service "nope"`},
	}
	for _, c := range bad {
		err := mcpCfg(t, c.yml)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: lỗi = %v, muốn chứa %q", c.name, err, c.want)
		}
	}
}

func TestMCPDefaultsEmpty(t *testing.T) {
	d := Default().MCP
	if d.Services == nil || d.Grants == nil || len(d.Services) != 0 || len(d.Grants) != 0 {
		t.Fatalf("mặc định phải là hai ánh xạ rỗng không nil: %+v", d)
	}
	if !(MCPService{}).IsRequired() {
		t.Fatal("required mặc định true")
	}
	f := false
	if (MCPService{Required: &f}).IsRequired() {
		t.Fatal("required:false")
	}
}

func TestUpgradeAddsMCPBlock(t *testing.T) {
	old := "version: 1\nproject:\n  name: x\n  root: auto\nui:\n  language: vi\n"
	res, err := Upgrade([]byte(old))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.Content), "mcp:") {
		t.Fatalf("Upgrade phải thêm khối mcp:\n%s", res.Content)
	}
	var found bool
	for _, a := range res.Added {
		found = found || strings.HasPrefix(a, "mcp")
	}
	if !found {
		t.Fatalf("Added không có khoá mcp: %v", res.Added)
	}
	if err := mcpCfg(t, string(res.Content)); err != nil {
		t.Fatal(err)
	}
}

func TestMachinePaths(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BINT_CONFIG_HOME", dir)
	if MachineDir() != dir || MCPMachinePath() != filepath.Join(dir, "mcp.yml") {
		t.Fatalf("đường dẫn: %q %q", MachineDir(), MCPMachinePath())
	}
	if got := SecretsPath(MachineMCP{}); got != filepath.Join(dir, "secrets.env") {
		t.Fatalf("SecretsPath = %q", got)
	}
	if got := SecretsPath(MachineMCP{SecretsFile: "/x/s.env"}); got != "/x/s.env" {
		t.Fatalf("SecretsPath tuỳ chỉnh = %q", got)
	}
}

func TestMachineMCPRoundTrip(t *testing.T) {
	testutil.Isolate(t)
	dir := t.TempDir()
	t.Setenv("BINT_CONFIG_HOME", filepath.Join(dir, "cfg"))
	m, err := LoadMachineMCP()
	if err != nil || len(m.Services) != 0 {
		t.Fatalf("chưa có tệp phải rỗng: %+v %v", m, err)
	}
	m.Services["codegraph"] = Registration{Command: "codegraph", Args: []string{"serve", "--mcp"}, Version: "1.6.2"}
	m.Services["deepwiki"] = Registration{URL: "https://mcp.deepwiki.com/mcp"}
	m.Services["jira"] = Registration{Command: "atlassian-jira-mcp", EnvNames: []string{"JIRA_MCP_URL", "JIRA_MCP_PASSWORD"}, IdleTimeout: "10m", MaxRestarts: 3}
	if err := SaveMachineMCP(m); err != nil {
		t.Fatal(err)
	}
	got, err := LoadMachineMCP()
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != MachineMCPVersion || len(got.Services) != 3 ||
		got.Services["jira"].EnvNames[1] != "JIRA_MCP_PASSWORD" || got.Services["codegraph"].Args[1] != "--mcp" ||
		got.Services["deepwiki"].URL == "" || got.Services["jira"].MaxRestarts != 3 {
		t.Fatalf("round-trip: %+v", got)
	}
	p := MCPMachinePath()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("mcp.yml quyền %v, muốn 0600", fi.Mode().Perm())
		}
		if di, _ := os.Stat(filepath.Dir(p)); di.Mode().Perm() != 0o700 {
			t.Errorf("thư mục quyền %v, muốn 0700", di.Mode().Perm())
		}
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(p), ".mcp-*")); len(left) != 0 {
		t.Errorf("còn tệp tạm: %v", left)
	}
}

func TestMachineMCPRejects(t *testing.T) {
	cases := map[string]string{
		"khoá lạ":           "version: 1\nservices: {}\nfoo: bar\n",
		"khoá lạ trong mục": "version: 1\nservices:\n  codegraph: {command: codegraph, password: x}\n",
		"service ngoài":     "version: 1\nservices:\n  gitnexus: {command: x}\n",
		"thiếu command":     "version: 1\nservices:\n  codegraph: {}\n",
		"http thiếu url":    "version: 1\nservices:\n  deepwiki: {}\n",
		"stdio có url":      "version: 1\nservices:\n  codegraph: {command: c, url: http://x}\n",
		"env có giá trị":    "version: 1\nservices:\n  jira: {command: c, env: [\"JIRA_MCP_PASSWORD=hunter2\"]}\n",
		"idle sai":          "version: 1\nservices:\n  jira: {command: c, idle_timeout: abc}\n",
		"phiên bản schema":  "version: 9\nservices: {}\n",
	}
	for name, y := range cases {
		_, err := DecodeMachineMCP(strings.NewReader(y))
		if err == nil {
			t.Errorf("%s: phải bị từ chối", name)
		} else if strings.Contains(err.Error(), "hunter2") {
			t.Errorf("%s: lỗi lộ giá trị: %v", name, err)
		}
	}
	dir := t.TempDir()
	t.Setenv("BINT_CONFIG_HOME", dir)
	if err := os.WriteFile(filepath.Join(dir, "mcp.yml"), []byte("version: 1\nzzz: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadMachineMCP(); err == nil || !strings.Contains(err.Error(), "mcp.yml") {
		t.Fatalf("LoadMachineMCP khoá lạ: %v", err)
	}
}

func TestReadSecrets(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "secrets.env")
	body := "\ufeff# chú thích\n\nJIRA_MCP_URL=https://x.example\nexport JIRA_MCP_USERNAME = \"bob\"\nJIRA_MCP_PASSWORD='p=ss w0rd'\nEMPTY=\nA_B1=a#b\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadSecrets(p)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"JIRA_MCP_URL": "https://x.example", "JIRA_MCP_USERNAME": "bob", "JIRA_MCP_PASSWORD": "p=ss w0rd", "EMPTY": "", "A_B1": "a#b"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, muốn %q", k, got[k], v)
		}
	}
	if m, err := ReadSecrets(filepath.Join(dir, "none.env")); err != nil || len(m) != 0 {
		t.Fatalf("thiếu tệp: %v %v", m, err)
	}
	bad := filepath.Join(dir, "bad.env")
	os.WriteFile(bad, []byte("khong-co-dau-bang-mat-khau-hunter2\n"), 0o600)
	if _, err := ReadSecrets(bad); err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("dòng sai phải lỗi và không lộ nội dung: %v", err)
	}
}

func TestSecretsPermProblem(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("không kiểm ACL trên Windows")
	}
	p := filepath.Join(t.TempDir(), "secrets.env")
	os.WriteFile(p, []byte("A=1\n"), 0o600)
	if err := os.Chmod(p, 0o600); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(p)
	if s := SecretsPermProblem(fi); s != "" {
		t.Fatalf("0600 phải ổn: %q", s)
	}
	os.Chmod(p, 0o644)
	fi, _ = os.Stat(p)
	if s := SecretsPermProblem(fi); s == "" {
		t.Fatal("0644 phải bị báo")
	}
}
