package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDefaultValid(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRenderRoundTrip(t *testing.T) {
	out := Render(InitParams{
		ProjectName:          `demo "x": y`,
		Harnesses:            []string{"claude", "codex"},
		DefaultHarness:       "claude",
		SuggestedTestCommand: "go test ./...",
	})
	cfg := Default()
	if err := Decode(strings.NewReader(out), &cfg); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.Project.Name != `demo "x": y` {
		t.Fatalf("name = %q", cfg.Project.Name)
	}
	if !reflect.DeepEqual(cfg.Harnesses.Enabled, []string{"claude", "codex"}) {
		t.Fatalf("harnesses = %v", cfg.Harnesses.Enabled)
	}
	if cfg.Eval.TestCommand != nil {
		t.Fatal("test_command must stay null until confirmed")
	}
	if cfg.Eval.SuggestedTestCommand == nil || *cfg.Eval.SuggestedTestCommand != "go test ./..." {
		t.Fatal("suggested test command lost")
	}
	if cfg.Eval.BudgetUSD != nil {
		t.Fatal("budget must be null")
	}
	if cfg.Eval.PriorRunUSD != 0.5 {
		t.Fatalf("prior_run_usd mặc định = %v, mong đợi 0.5", cfg.Eval.PriorRunUSD)
	}
}

func TestRenderMatchesDefaults(t *testing.T) {
	cfg := Config{}
	if err := Decode(strings.NewReader(Render(InitParams{ProjectName: "p"})), &cfg); err != nil {
		t.Fatal(err)
	}
	want := Default()
	want.Project.Name = "p"
	// workflow.variant là khoá bật tay: mẫu chỉ ghi chú thích (binary cũ không biết khoá vẫn nạp được config), nên giải mã ra rỗng;
	// giá trị mặc định classic đến từ Default() khi hợp nhất lớp (xem TestWorkflowVariant).
	want.Workflow.Variant = ""
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("rendered config differs from Default():\n got %+v\nwant %+v", cfg, want)
	}
}

func TestValidateErrors(t *testing.T) {
	c := Default()
	c.Version = 2
	c.Harnesses.Enabled = []string{"vim"}
	c.Knowledge.RequireHumanApprovalFor = []string{"fact"}
	c.State.Dir = "relative/path"
	err := c.Validate()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want ValidationError, got %v", err)
	}
	joined := err.Error()
	for _, s := range []string{"version", "vim", `"rule"`, "state.dir"} {
		if !strings.Contains(joined, s) {
			t.Errorf("missing problem about %s in:\n%s", s, joined)
		}
	}
}

func TestUnknownFieldRejected(t *testing.T) {
	cfg := Default()
	err := Decode(strings.NewReader("version: 1\ncontxt:\n  max_tokens: 1\n"), &cfg)
	if err == nil {
		t.Fatal("typo key should be rejected")
	}
}

func TestLoadLayers(t *testing.T) {
	t.Setenv("BINT_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	if _, _, err := Load(root, true); !errors.Is(err, ErrNoProjectConfig) {
		t.Fatalf("want ErrNoProjectConfig, got %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, DirName), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(os.Getenv("BINT_CONFIG_HOME"), FileName), "context:\n  max_tokens: 1000\n")
	write(ProjectPath(root), Render(InitParams{ProjectName: "p"}))
	abs, _ := filepath.Abs(t.TempDir())
	write(LocalPath(root), "state:\n  dir: "+quote(abs)+"\n")

	cfg, src, err := Load(root, true)
	if err != nil {
		t.Fatal(err)
	}
	// Dự án ghi đè toàn cục (dự án đặt 6000), cục bộ ghi đè state.dir.
	if cfg.Context.MaxTokens != 6000 {
		t.Fatalf("max_tokens = %d", cfg.Context.MaxTokens)
	}
	if cfg.State.Dir != abs {
		t.Fatalf("state.dir = %q", cfg.State.Dir)
	}
	if src.Global == "" || src.Project == "" || src.Local == "" {
		t.Fatalf("sources = %+v", src)
	}
}

// ui.language: mặc định auto, chỉ nhận auto | vi | en.
func TestUILanguage(t *testing.T) {
	if got := Default().UI.Language; got != "auto" {
		t.Fatalf("mặc định = %q", got)
	}
	for _, ok := range UILanguages {
		c := Default()
		c.UI.Language = ok
		if err := c.Validate(); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"fr", "", "VI"} {
		c := Default()
		c.UI.Language = bad
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), "ui.language") {
			t.Errorf("%q phải bị từ chối: %v", bad, err)
		}
	}
	cfg := Default()
	if err := Decode(strings.NewReader("version: 1\nui:\n  language: en\n"), &cfg); err != nil || cfg.UI.Language != "en" {
		t.Fatalf("nạp ui.language: %v %+v", err, cfg.UI)
	}
}

func TestWorkflowIsolationDefaultsAndValidate(t *testing.T) {
	c := Default()
	if c.Workflow.Isolation != IsolationAuto || c.Workflow.Worktree.Copy == nil || len(c.Workflow.Worktree.Copy) != 0 ||
		c.Workflow.Worktree.SetupCommand != "" {
		t.Fatalf("mặc định cô lập sai: %+v", c.Workflow)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mode := range IsolationModes {
		c := Default()
		c.Workflow.Isolation = mode
		if err := c.Validate(); err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
	}
	bad := []struct {
		name string
		edit func(*Config)
		want string
	}{
		{"chế độ lạ", func(c *Config) { c.Workflow.Isolation = "separate" }, "workflow.isolation"},
		{"chế độ rỗng", func(c *Config) { c.Workflow.Isolation = "" }, "workflow.isolation"},
		{"glob tuyệt đối", func(c *Config) { c.Workflow.Worktree.Copy = []string{"/etc/passwd"} }, "workflow.worktree.copy[0]"},
		{"glob .. đầu", func(c *Config) { c.Workflow.Worktree.Copy = []string{".env", "../x"} }, "workflow.worktree.copy[1]"},
		{"glob .. giữa", func(c *Config) { c.Workflow.Worktree.Copy = []string{"a/../../b"} }, "workflow.worktree.copy[0]"},
		{"glob .. kiểu Windows", func(c *Config) { c.Workflow.Worktree.Copy = []string{`..\x`} }, "workflow.worktree.copy[0]"},
		{"glob rỗng", func(c *Config) { c.Workflow.Worktree.Copy = []string{" "} }, "workflow.worktree.copy[0]"},
	}
	for _, tc := range bad {
		c := Default()
		tc.edit(&c)
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: muốn lỗi chứa %q, được %v", tc.name, tc.want, err)
		}
	}
	c = Default()
	c.Workflow.Worktree.Copy = []string{".env", "config/*.local.json", "a..b/c"}
	c.Workflow.Worktree.SetupCommand = "npm ci"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowDriverDefaultsAndValidate(t *testing.T) {
	c := Default()
	if c.Workflow.Driver != DriverProcess {
		t.Fatalf("mặc định workflow.driver = %q, muốn %q", c.Workflow.Driver, DriverProcess)
	}
	for _, mode := range DriverModes {
		c := Default()
		c.Workflow.Driver = mode
		if err := c.Validate(); err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
	}
	for _, v := range []string{"", "subagent", "Session"} {
		c := Default()
		c.Workflow.Driver = v
		if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "workflow.driver") {
			t.Errorf("driver %q: muốn lỗi workflow.driver, được %v", v, err)
		}
	}
	// Tệp cấu hình cũ không có khoá driver vẫn nạp được và nhận mặc định.
	cfg := Default()
	if err := Decode(strings.NewReader("workflow:\n  isolation: shared\n"), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Workflow.Driver != DriverProcess {
		t.Fatalf("thiếu khoá driver phải về mặc định, được %q", cfg.Workflow.Driver)
	}
}

func TestClaudePricingDefaultsAndValidate(t *testing.T) {
	c := Default()
	if c.Harnesses.Claude.Pricing == nil || len(c.Harnesses.Claude.Pricing) != 0 {
		t.Fatalf("mặc định phải là bảng rỗng: %#v", c.Harnesses.Claude.Pricing)
	}
	c.Harnesses.Claude.Pricing = map[string]ModelPrice{"claude-x-1": {InputUSDPerMTok: 2, OutputUSDPerMTok: 10}}
	if err := c.Validate(); err != nil {
		t.Fatalf("giá hợp lệ bị từ chối: %v", err)
	}
	for name, mut := range map[string]func(*Config){
		"giá âm": func(c *Config) {
			c.Harnesses.Claude.Pricing["claude-x-1"] = ModelPrice{InputUSDPerMTok: -1, OutputUSDPerMTok: 10}
		},
		"cache âm": func(c *Config) {
			c.Harnesses.Claude.Pricing["claude-x-1"] = ModelPrice{InputUSDPerMTok: 1, OutputUSDPerMTok: 2, CacheReadUSDPerMTok: -0.1}
		},
		"khoá rỗng": func(c *Config) { c.Harnesses.Claude.Pricing[""] = ModelPrice{InputUSDPerMTok: 1, OutputUSDPerMTok: 2} },
		"khoá có cách": func(c *Config) {
			c.Harnesses.Claude.Pricing["claude x"] = ModelPrice{InputUSDPerMTok: 1, OutputUSDPerMTok: 2}
		},
		"thiếu giá": func(c *Config) { c.Harnesses.Claude.Pricing["claude-x-1"] = ModelPrice{} },
	} {
		cc := Default()
		cc.Harnesses.Claude.Pricing = map[string]ModelPrice{"claude-x-1": {InputUSDPerMTok: 2, OutputUSDPerMTok: 10}}
		mut(&cc)
		if err := cc.Validate(); err == nil || !strings.Contains(err.Error(), "pricing") {
			t.Errorf("%s: muốn lỗi nhắc pricing, được %v", name, err)
		}
	}
	// Khoá nạp từ YAML.
	var cfg Config
	y := strings.Replace(Render(InitParams{ProjectName: "p"}), "    pricing: {}", "    pricing:\n      claude-y-2: { input_usd_per_mtok: 1, output_usd_per_mtok: 5 }", 1)
	if err := Decode(strings.NewReader(y), &cfg); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Harnesses.Claude.Pricing["claude-y-2"]; got.InputUSDPerMTok != 1 || got.OutputUSDPerMTok != 5 {
		t.Fatalf("pricing nạp từ YAML: %+v", cfg.Harnesses.Claude.Pricing)
	}
}

func TestTasksLedgerKey(t *testing.T) {
	c := Default()
	if c.Tasks.Ledger != LedgerGit {
		t.Fatalf("mặc định tasks.ledger = %q, muốn git", c.Tasks.Ledger)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.Tasks.Ledger = LedgerLocal
	if err := c.Validate(); err != nil {
		t.Fatalf("local hợp lệ: %v", err)
	}
	c.Tasks.Ledger = "svn"
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "tasks.ledger") {
		t.Fatalf("giá trị lạ phải bị từ chối: %v", err)
	}
}

func TestWorkflowVerifyScope(t *testing.T) {
	c := Default()
	if c.Workflow.Verify.Scope != VerifyScopeFull || c.Workflow.Verify.AffectedCommand != "" {
		t.Fatalf("mặc định verify = %+v", c.Workflow.Verify)
	}
	ok := []WorkflowVerify{
		{Scope: "full"},
		{Scope: "full", AffectedCommand: "go test {test_dirs}"},
		{Scope: "affected", AffectedCommand: "go test -count=1 {test_dirs}"},
		{Scope: "affected", AffectedCommand: "pytest {test_files}"},
	}
	for _, v := range ok {
		c := Default()
		c.Workflow.Verify = v
		if err := c.Validate(); err != nil {
			t.Errorf("%+v: %v", v, err)
		}
	}
	bad := []struct {
		v    WorkflowVerify
		want string
	}{
		{WorkflowVerify{Scope: "partial"}, "workflow.verify.scope"},
		{WorkflowVerify{Scope: ""}, "workflow.verify.scope"},
		{WorkflowVerify{Scope: "affected"}, "workflow.verify.affected_command"},
		{WorkflowVerify{Scope: "affected", AffectedCommand: "  "}, "workflow.verify.affected_command"},
		{WorkflowVerify{Scope: "affected", AffectedCommand: "go test ./..."}, "workflow.verify.affected_command"},
	}
	for _, tc := range bad {
		c := Default()
		c.Workflow.Verify = tc.v
		if err := c.Validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%+v: muốn lỗi chứa %q, được %v", tc.v, tc.want, err)
		}
	}
}

func TestValidateHarnessProfile(t *testing.T) {
	ok := Default()
	ok.Harnesses.Claude.Profile = "work"
	ok.Harnesses.Codex.Profile = "a.b_c-1"
	if err := ok.Validate(); err != nil {
		t.Fatalf("tên hợp lệ bị từ chối: %v", err)
	}
	if ok.Harnesses.ProfileFor("claude") != "work" || ok.Harnesses.ProfileFor("codex") != "a.b_c-1" || ok.Harnesses.ProfileFor("x") != "" {
		t.Fatalf("ProfileFor sai: %+v", ok.Harnesses)
	}
	for _, tc := range []struct {
		name, field, val, want string
	}{
		{"claude khoảng trắng", "claude", "a b", "harnesses.claude.profile"},
		{"codex chấm", "codex", ".", "harnesses.codex.profile"},
		{"codex chấm đôi", "codex", "..", "harnesses.codex.profile"},
		{"codex quá dài", "codex", strings.Repeat("a", 65), "harnesses.codex.profile"},
		{"codex dấu gạch chéo", "codex", "a/b", "harnesses.codex.profile"},
		{"antigravity", "antigravity", "work", "harnesses.antigravity.profile"},
	} {
		c := Default()
		switch tc.field {
		case "claude":
			c.Harnesses.Claude.Profile = tc.val
		case "codex":
			c.Harnesses.Codex.Profile = tc.val
		default:
			c.Harnesses.Antigravity.Profile = tc.val
		}
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: want lỗi %q, got %v", tc.name, tc.want, err)
		}
	}
}

// Đường nhanh chỉ chạy khi người dùng chọn (--fast): mặc định không tự chọn; ngưỡng kế hoạch nhỏ có mặc định và bị Validate chặn khi âm.
func TestWorkflowFastPathAndSmallPlanDefaults(t *testing.T) {
	d := Default()
	if d.Workflow.FastPath || d.Workflow.SmallPlanMaxFiles != 5 {
		t.Fatalf("mặc định fast_path=%v small_plan_max_files=%d, muốn false và 5", d.Workflow.FastPath, d.Workflow.SmallPlanMaxFiles)
	}
	cfg := Config{}
	if err := Decode(strings.NewReader(Render(InitParams{ProjectName: "p"})), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Workflow.FastPath || cfg.Workflow.SmallPlanMaxFiles != 5 {
		t.Fatalf("mẫu Render: fast_path=%v small_plan_max_files=%d", cfg.Workflow.FastPath, cfg.Workflow.SmallPlanMaxFiles)
	}
	d.Workflow.SmallPlanMaxFiles = 0
	if err := d.Validate(); err != nil {
		t.Fatalf("0 hợp lệ (không kế hoạch nào nhỏ): %v", err)
	}
	d.Workflow.SmallPlanMaxFiles = -1
	if err := d.Validate(); err == nil || !strings.Contains(err.Error(), "workflow.small_plan_max_files") {
		t.Fatalf("số âm phải bị từ chối: %v", err)
	}
}

// workflow.variant: mặc định classic, có trong mẫu Render, chỉ nhận classic|bprime.
func TestWorkflowVariant(t *testing.T) {
	d := Default()
	if d.Workflow.Variant != VariantClassic {
		t.Fatalf("mặc định variant = %q, muốn classic", d.Workflow.Variant)
	}
	// Khoá bật tay nằm trong mẫu dưới dạng chú thích (binary cũ không biết khoá vẫn nạp được .bint/config.yml); mặc định đến từ Default().
	tpl := Render(InitParams{ProjectName: "p"})
	if !strings.Contains(tpl, "# variant: classic") {
		t.Fatalf("mẫu Render thiếu dòng chú thích variant")
	}
	cfg := Config{}
	if err := Decode(strings.NewReader(tpl), &cfg); err != nil || cfg.Workflow.Variant != "" {
		t.Fatalf("mẫu Render không được đặt variant: %v %q", err, cfg.Workflow.Variant)
	}
	d.Workflow.Variant = VariantBPrime
	if err := d.Validate(); err != nil {
		t.Fatalf("bprime hợp lệ: %v", err)
	}
	for _, bad := range []string{"", "B'", "b-prime"} {
		d.Workflow.Variant = bad
		if err := d.Validate(); err == nil || !strings.Contains(err.Error(), "workflow.variant") {
			t.Fatalf("variant %q phải bị từ chối: %v", bad, err)
		}
	}
}

// Lát mỏng của b-do (slice.go): trần thời gian mỗi lát mặc định 30 phút, có trong mẫu Render, 0 = tắt, âm bị Validate chặn.
func TestWorkflowSliceTimeoutMinutes(t *testing.T) {
	d := Default()
	if d.Workflow.SliceTimeoutMinutes != 30 {
		t.Fatalf("mặc định slice_timeout_minutes = %d, muốn 30", d.Workflow.SliceTimeoutMinutes)
	}
	cfg := Config{}
	if err := Decode(strings.NewReader(Render(InitParams{ProjectName: "p"})), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Workflow.SliceTimeoutMinutes != 30 {
		t.Fatalf("mẫu Render: slice_timeout_minutes = %d, muốn 30", cfg.Workflow.SliceTimeoutMinutes)
	}
	d.Workflow.SliceTimeoutMinutes = 0
	if err := d.Validate(); err != nil {
		t.Fatalf("0 hợp lệ (tắt lát mỏng): %v", err)
	}
	d.Workflow.SliceTimeoutMinutes = -1
	if err := d.Validate(); err == nil || !strings.Contains(err.Error(), "workflow.slice_timeout_minutes") {
		t.Fatalf("số âm phải bị từ chối: %v", err)
	}
}
