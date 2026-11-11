package harness

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"bint/internal/config"
	"bint/internal/eval"
	"bint/internal/fsafe"
	"bint/internal/harness/claude"
	"bint/internal/skills"
)

// Hợp đồng adapter (mục 17): báo cáo khả năng đầy đủ và nhất quán.
func TestCapabilityMatrixComplete(t *testing.T) {
	for _, a := range All() {
		seen := map[string]bool{}
		for _, c := range a.Capabilities() {
			if seen[c.Key] {
				t.Errorf("%s: khả năng %s khai hai lần", a.Name(), c.Key)
			}
			seen[c.Key] = true
			if c.Support != Yes && c.Support != Partial && c.Support != No {
				t.Errorf("%s: %s mức %q", a.Name(), c.Key, c.Support)
			}
			if c.Detail == "" {
				t.Errorf("%s: %s thiếu chi tiết", a.Name(), c.Key)
			}
		}
		for _, k := range AllCapabilities {
			if !seen[k.Key] {
				t.Errorf("%s: thiếu khả năng %s", a.Name(), k.Key)
			}
		}
		// Khả năng khai "có" phải có cài đặt tương ứng — không khai thứ không làm được.
		_, hooks := a.(HookInstaller)
		if (CapabilityOf(a, CapHooks).Support != No) != hooks {
			t.Errorf("%s: khai hooks=%s nhưng HookInstaller=%v", a.Name(), CapabilityOf(a, CapHooks).Support, hooks)
		}
		_, mcp := a.(MCPInstaller)
		if (CapabilityOf(a, CapMCP).Support != No) != mcp {
			t.Errorf("%s: khai mcp=%s nhưng MCPInstaller=%v", a.Name(), CapabilityOf(a, CapMCP).Support, mcp)
		}
		_, exec := a.(Executor)
		if (CapabilityOf(a, CapExecute).Support != No) != exec {
			t.Errorf("%s: khai execute nhưng Executor=%v", a.Name(), exec)
		}
		// Báo chi phí / giới hạn lượt phải khớp agent thực.
		if ex, ok := a.(Executor); ok {
			ag := ex.NewAgent(AgentOptions{Config: config.Default()})
			if (CapabilityOf(a, CapCost).Support == Yes) != ag.ReportsCost() {
				t.Errorf("%s: cost khai %s, agent.ReportsCost=%v", a.Name(), CapabilityOf(a, CapCost).Support, ag.ReportsCost())
			}
			tl, ok := ag.(eval.TurnLimiter)
			enforces := !ok || tl.EnforcesTurns()
			if (CapabilityOf(a, CapTurnLimit).Support == Yes) != enforces {
				t.Errorf("%s: turn_limit khai %s, agent áp=%v", a.Name(), CapabilityOf(a, CapTurnLimit).Support, enforces)
			}
			if ag.Name() != a.Name() {
				t.Errorf("agent %q của adapter %q", ag.Name(), a.Name())
			}
		}
	}
	levels := map[string]int{}
	for _, a := range All() {
		levels[a.Name()] = Level(a.Capabilities())
	}
	if levels["claude"] != 3 || levels["codex"] != 3 || levels["antigravity"] != 3 {
		t.Fatalf("levels = %v", levels)
	}
	if Level([]Capability{{CapSkills, Yes, "x"}}) != 1 || Level(nil) != 0 {
		t.Fatal("Level")
	}
}

// Hợp đồng adapter: không tạo skill sai định dạng.
func TestSkillTargetsAndFormat(t *testing.T) {
	ts := SkillTargets([]string{"antigravity", "codex", "claude"})
	if len(ts) != 2 || ts[0].Dir != ".agents/skills" || ts[1].Dir != ".claude/skills" {
		t.Fatalf("targets = %+v", ts)
	}
	if got := HarnessesForTarget([]string{"claude", "codex", "antigravity"}, ".agents/skills"); strings.Join(got, ",") != "codex,antigravity" {
		t.Fatalf("for target = %v", got)
	}
	for _, s := range skills.Core() {
		for _, tg := range ts {
			out := skills.ForTarget(s, tg)
			c := string(out.Content)
			if !strings.HasPrefix(c, "---\n") {
				t.Fatalf("%s/%s: thiếu frontmatter", tg.Dir, s.Name)
			}
			parts := strings.SplitN(c, "---\n", 3)
			var fm map[string]any
			if err := yaml.Unmarshal([]byte(parts[1]), &fm); err != nil {
				t.Fatalf("%s/%s: frontmatter: %v", tg.Dir, s.Name, err)
			}
			if fm["name"] != s.Name || fm["description"] == nil || fm["description"] == "" {
				t.Fatalf("%s/%s: name/description: %v", tg.Dir, s.Name, fm)
			}
			if tg.Format == skills.FormatAgents && len(fm) != 2 {
				t.Fatalf("%s/%s: khoá thừa %v", tg.Dir, s.Name, fm)
			}
			if out.Body() != s.Body() {
				t.Fatalf("%s/%s: thân skill bị đổi", tg.Dir, s.Name)
			}
		}
	}
}

func TestForStepPriorityAndEnabled(t *testing.T) {
	cfg := config.Default()
	cfg.Harnesses.Enabled = []string{"claude", "codex"}
	cfg.Roles.Implementer = "codex"
	check := func(sel Selection, step, want string) {
		t.Helper()
		h, _, err := ForStep(cfg, sel, step)
		if err != nil || h != want {
			t.Fatalf("%s: %q %v, muốn %q", step, h, err, want)
		}
	}
	check(Selection{}, "b-feat", "codex")
	check(Selection{}, "b-req", "claude")
	check(Selection{All: "codex"}, "b-review", "codex")
	check(Selection{All: "codex", Roles: map[string]string{"reviewer": "claude"}}, "b-review", "claude")
	if _, _, err := ForStep(cfg, Selection{All: "antigravity"}, "b-req"); err == nil || !strings.Contains(err.Error(), "harnesses.enabled") {
		t.Fatalf("harness chưa bật phải bị từ chối: %v", err)
	}
	roles, err := ParseRoleOverrides([]string{"implementer=codex", "b-review=claude"})
	if err != nil || roles["implementer"] != "codex" || roles["reviewer"] != "claude" {
		t.Fatalf("%v %v", roles, err)
	}
	for _, bad := range []string{"x", "boss=claude", "planner=gemini"} {
		if _, err := ParseRoleOverrides([]string{bad}); err == nil {
			t.Errorf("%q phải lỗi", bad)
		}
	}
}

// Hành vi khi adapter không khả dụng (mục 17): lỗi rõ ràng, có hướng xử lý.
func TestNewAgentUnavailable(t *testing.T) {
	cfg := config.Default()
	cfg.Harnesses.Codex.Binary = filepath.Join(t.TempDir(), "khong-co-codex")
	_, err := NewAgent("codex", AgentOptions{Config: cfg, Step: "b-feat"})
	var ue *UnavailableError
	if !errors.As(err, &ue) || !strings.Contains(err.Error(), "harnesses.codex.binary") {
		t.Fatalf("err = %v", err)
	}
	// Chương trình có ở đường dẫn cấu hình → tạo được agent, đúng tham số.
	bin := filepath.Join(t.TempDir(), "codex")
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	m := "gpt-x"
	cfg.Harnesses.Codex = config.HarnessAgent{Binary: bin, Model: &m, ExtraArgs: []string{"--foo"}}
	ag, err := NewAgent("codex", AgentOptions{Config: cfg, Step: "b-feat"})
	if err != nil {
		t.Fatal(err)
	}
	ca := ag.(*eval.CodexAgent)
	if ca.Binary != bin || ca.Model != "gpt-x" || ca.ExtraArgs[0] != "--foo" {
		t.Fatalf("%+v", ca)
	}
	ag, _ = NewAgent("codex", AgentOptions{Config: cfg, Model: "cli-model"})
	if ag.(*eval.CodexAgent).Model != "cli-model" {
		t.Fatal("--model phải ưu tiên hơn cấu hình")
	}
	if _, err := NewAgent("gemini", AgentOptions{Config: cfg}); err == nil {
		t.Fatal("harness lạ phải lỗi")
	}
}

// Hợp đồng adapter: cài/gỡ hook không ghi đè dữ liệu ngoài phạm vi, chạy lại không
// đổi gì, gỡ xong không để lại gì của Bint.
func TestHookInstallersContract(t *testing.T) {
	now := time.Now()
	foreign := map[string]string{
		"claude":      `{"permissions":{"allow":["Bash(ls:*)"]},"hooks":{"PostToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"audit.sh"}]}]}}`,
		"codex":       `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"herdr.sh session"}]}]}}`,
		"antigravity": `{"lint":{"PostToolUse":[{"matcher":"run_command","hooks":[{"command":"./lint.sh"}]}]}}`,
	}
	for _, a := range All() {
		hi, ok := a.(HookInstaller)
		if !ok {
			continue
		}
		t.Run(a.Name(), func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", t.TempDir())
			t.Setenv("CODEX_HOME", "")
			p, err := hi.HooksPath(claude.ScopeLocal, root)
			if err != nil {
				t.Fatal(err)
			}
			if rel, _ := filepath.Rel(root, p); strings.HasPrefix(rel, "..") {
				t.Fatalf("phạm vi local ra ngoài repository: %s", p)
			}
			os.MkdirAll(filepath.Dir(p), 0o755)
			os.WriteFile(p, []byte(foreign[a.Name()]), 0o644)
			backups := filepath.Join(t.TempDir(), "b")

			if ok, _ := hi.HooksInstalled(p); ok {
				t.Fatal("chưa cài mà báo đã cài")
			}
			res, err := hi.InstallHooks(p, "/opt/bint/bint", backups, now)
			if err != nil || !res.Changed {
				t.Fatalf("install %+v %v", res, err)
			}
			b, _ := os.ReadFile(p)
			var v any
			if json.Unmarshal(b, &v) != nil {
				t.Fatalf("JSON hỏng:\n%s", b)
			}
			if ok, _ := hi.HooksInstalled(p); !ok {
				t.Fatal("cài rồi mà không nhận ra")
			}
			for _, keep := range []string{"audit.sh", "herdr.sh", "lint.sh", "Bash(ls:*)"} {
				if strings.Contains(foreign[a.Name()], keep) && !strings.Contains(string(b), keep) {
					t.Fatalf("mất %q:\n%s", keep, b)
				}
			}
			if !strings.Contains(string(b), "hook "+a.Name()) {
				t.Fatalf("thiếu lệnh hook:\n%s", b)
			}
			if res, _ := hi.InstallHooks(p, "/opt/bint/bint", backups, now); res.Changed {
				t.Fatal("cài lần hai phải không đổi")
			}
			// Đổi đường dẫn bint: cập nhật tại chỗ, không nhân đôi.
			hi.InstallHooks(p, "bint", backups, now)
			b, _ = os.ReadFile(p)
			if strings.Contains(string(b), "/opt/bint") || strings.Count(string(b), "hook "+a.Name()) == 0 {
				t.Fatalf("cập nhật đường dẫn:\n%s", b)
			}
			if _, err := hi.UninstallHooks(p, backups, now); err != nil {
				t.Fatal(err)
			}
			b, _ = os.ReadFile(p)
			if strings.Contains(string(b), "hook "+a.Name()) {
				t.Fatalf("gỡ còn sót:\n%s", b)
			}
			if !jsonEqual(string(b), foreign[a.Name()]) {
				t.Fatalf("gỡ xong khác bản gốc:\n%s\n---\n%s", b, foreign[a.Name()])
			}
			// Chỉ tệp hook và thư mục sao lưu được chạm tới.
			var touched []string
			filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() {
					touched = append(touched, path)
				}
				return nil
			})
			if len(touched) != 1 || touched[0] != p {
				t.Fatalf("tệp ngoài phạm vi: %v", touched)
			}
		})
	}
}

func jsonEqual(a, b string) bool {
	var x, y any
	if json.Unmarshal([]byte(a), &x) != nil || json.Unmarshal([]byte(b), &y) != nil {
		return false
	}
	xa, _ := json.Marshal(x)
	ya, _ := json.Marshal(y)
	return string(xa) == string(ya)
}

func TestCodexMCPBlock(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", t.TempDir())
	m, _ := fsafe.LoadManifest(filepath.Join(t.TempDir(), "m.json"))
	p := filepath.Join(root, ".codex", "config.toml")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte("model = \"o\"\n\n[mcp_servers.other]\ncommand = \"x\"\n"), 0o644)
	req := MCPRequest{Root: root, BintPath: "bint", BackupDir: t.TempDir(), Manifest: m, Install: true, Now: time.Now()}
	res, err := Codex{}.InstallMCP(req)
	if err != nil || !res.Changed || res.Conflict != "" || res.Note == "" {
		t.Fatalf("%+v %v", res, err)
	}
	if res, _ := (Codex{}).InstallMCP(req); res.Changed {
		t.Fatal("lần hai phải không đổi")
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "[mcp_servers.other]") || !strings.Contains(string(b), "args = [\"mcp\", \"serve\"]") {
		t.Fatalf("%s", b)
	}
	req.Install = false
	if res, _ := (Codex{}).InstallMCP(req); !res.Changed {
		t.Fatal("gỡ phải đổi")
	}
	if b, _ := os.ReadFile(p); string(b) != "model = \"o\"\n\n[mcp_servers.other]\ncommand = \"x\"\n" {
		t.Fatalf("gỡ: %q", b)
	}
	// Bảng do người dùng tự khai: không ghi đè, báo xung đột.
	os.WriteFile(p, []byte("[mcp_servers.bint]\ncommand = \"mine\"\n"), 0o644)
	req.Install = true
	if res, _ := (Codex{}).InstallMCP(req); res.Conflict == "" {
		t.Fatal("phải báo xung đột")
	}
	if b, _ := os.ReadFile(p); string(b) != "[mcp_servers.bint]\ncommand = \"mine\"\n" {
		t.Fatal("đã ghi đè cấu hình của người dùng")
	}
}

// P5-03: Describe, Available và NewAgent phải nhất quán về binary cấu hình.
func TestDescribeConfiguredBinaryUsable(t *testing.T) {
	cfg := config.Default()
	a, _ := Get("codex")
	dir := t.TempDir()
	cases := map[string]bool{filepath.Join(dir, "none"): false, dir: false}
	noexec := filepath.Join(dir, "noexec")
	os.WriteFile(noexec, []byte("x"), 0o644)
	cases[noexec] = runtime.GOOS == "windows"
	ok := filepath.Join(dir, "codex")
	os.WriteFile(ok, []byte("#!/bin/sh\n"), 0o755)
	cases[ok] = true
	for bin, want := range cases {
		cfg.Harnesses.Codex.Binary = bin
		if got := Describe(a, cfg, nil).Installed; got != want {
			t.Errorf("%s: Installed = %v, muốn %v", bin, got, want)
		}
		if got := Available("codex", cfg); got != want {
			t.Errorf("%s: Available = %v, muốn %v", bin, got, want)
		}
	}
}

// allowRuleCovers mô phỏng quy tắc tiền tố `Bash(<tiền tố>:*)` của Claude Code.
func allowRuleCovers(rules []string, command string) bool {
	for _, r := range rules {
		p, ok := strings.CutPrefix(r, "Bash(")
		if !ok {
			continue
		}
		p, ok = strings.CutSuffix(p, ":*)")
		if ok && (command == p || strings.HasPrefix(command, p+" ")) {
			return true
		}
	}
	return false
}

// Claude 2.1.322 thật từ chối `<đường dẫn tuyệt đối>/bint ...` khi allowlist chỉ có
// `Bash(bint:*)` (permission_denials trong fixture): bước chỉ đọc không gọi được Bint.
func TestClaudeReadonlyAllowsCurrentBintExecutable(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "claude-2.1.322", "permission-denied-absolute-bint.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fx struct {
		Denials []struct {
			Input struct {
				Command string `json:"command"`
			} `json:"tool_input"`
		} `json:"permission_denials"`
	}
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	readonly := ClaudeWorkflowArgs(cfg, true, nil).AllowedTools
	write := ClaudeWorkflowArgs(cfg, false, nil).AllowedTools
	checked := 0
	for _, d := range fx.Denials {
		cmd := strings.ReplaceAll(d.Input.Command, "<BINT>", exe)
		if strings.Contains(cmd, ";") {
			continue // lệnh ghép không thuộc phạm vi quy tắc tiền tố
		}
		checked++
		if !allowRuleCovers(readonly, cmd) {
			t.Errorf("bước chỉ đọc không được phép chạy %q; rule: %v", cmd, readonly)
		}
		if allowRuleCovers(write, cmd) {
			t.Errorf("b-feat không dùng rule Bash(bint:*): %v", write)
		}
	}
	if checked == 0 {
		t.Fatal("fixture không có lệnh trực tiếp để kiểm tra")
	}
	if !slices.Contains(readonly, "Bash(bint:*)") {
		t.Fatalf("mất rule mặc định Bash(bint:*): %v", readonly)
	}
	// Người dùng đã gỡ rule bint khỏi cấu hình thì không tự thêm lại.
	cfg.Workflow.Agent.ReadonlyTools = []string{"Read", "Glob"}
	for _, r := range ClaudeWorkflowArgs(cfg, true, nil).AllowedTools {
		if strings.Contains(r, "bint") {
			t.Fatalf("tự thêm rule bint dù cấu hình đã gỡ: %s", r)
		}
	}
	// Không sửa slice trong cấu hình gốc.
	if len(config.Default().Workflow.Agent.ReadonlyTools) != 8 {
		t.Fatal("cấu hình mặc định bị đổi")
	}
}
