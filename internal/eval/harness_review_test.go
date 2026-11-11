package eval

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/config"
	"bint/internal/ctxengine"
	"bint/internal/harness/claude"
	"bint/internal/harness/hookcore"
	"bint/internal/hcomp"
)

const reviewStrategy = "id: strategy\nscope: global\nstatus: verified\norigin: human\nversion: 1\ncontext:\n  max_tokens: 1234\n  path_max_items: 2\n"

// repoWithStrategy: repository có strategy.yml verified; env.Cfg nạp bằng config.Load như lệnh thật.
func repoWithStrategy(t *testing.T) (Env, *Case) {
	t.Helper()
	t.Setenv("BINT_CONFIG_HOME", t.TempDir())
	r := newKRepo(t)
	writeFile(t, r.root, ".bint/config.yml", config.Render(config.InitParams{ProjectName: "p"}))
	writeFile(t, r.root, ".bint/harness/strategy.yml", reviewStrategy)
	cfg, _, err := config.Load(r.root, true)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Context.MaxTokens != 1234 {
		t.Fatalf("strategy verified phải được trộn vào cfg: %+v", cfg.Context)
	}
	cfg.Eval.MinimalSource = "knowledge"
	env := knowledgeEnv(t, r.root)
	env.Cfg = cfg
	env.BintExe = "/usr/bin/bint"
	return env, &Case{ID: "c1", Source: Source{Base: r.base, Commit: r.sol}, SourceFiles: []string{"lib/a.go"}}
}

func evalCfgOf(t *testing.T, s ArmSetup) ctxengine.EvalConfig {
	t.Helper()
	ec, err := ctxengine.LoadEvalConfig(s.EvalConfig)
	if err != nil {
		t.Fatal(err)
	}
	return ec
}

// [.1] strategy verified của repository không được rò sang bint@harness=off: nhánh đó dùng
// context.* gốc; harness=on (mặc định) mới áp strategy.
func TestStrategyDoesNotLeakIntoHarnessOff(t *testing.T) {
	ctx := context.Background()
	env, c := repoWithStrategy(t)
	def := config.Default().Context
	off, err := prepareArm(ctx, env, "bint@harness=off", LeakStrict, c, nil, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if ec := evalCfgOf(t, off); ec.MaxTokens != def.MaxTokens || ec.PathMaxItems != def.PathMaxItems {
		t.Fatalf("harness=off phải dùng context.* gốc (%d/%d): %+v", def.MaxTokens, def.PathMaxItems, ec)
	}
	on, err := prepareArm(ctx, env, "bint@harness=on", LeakStrict, c, nil, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if ec := evalCfgOf(t, on); ec.MaxTokens != 1234 || ec.PathMaxItems != 2 {
		t.Fatalf("harness=on phải áp strategy: %+v", ec)
	}
}

// [.1] Đề xuất deprecate strategy: base (còn strategy) khác candidate (không còn) dù repo đã
// trộn strategy vào cấu hình.
func TestDeprecateStrategyBaseDiffersFromCandidate(t *testing.T) {
	ctx := context.Background()
	env, c := repoWithStrategy(t)
	strat := hcomp.LoadDir(filepath.Join(env.BintRoot, ".bint", "harness")).Select(hcomp.ModeOn).Strategy()
	if strat == nil {
		t.Fatal("repo phải có strategy verified")
	}
	env.Variant = &Variant{Delivery: ArmBint, Label: "p1", Harness: func(_ context.Context, arm string, _ *Case, _ *LeakInput) (hcomp.Set, error) {
		set := (*hcomp.Library)(nil).Select(hcomp.ModeOff)
		if arm == ArmBase {
			set.Components = append(set.Components, *strat)
		}
		return set, nil
	}}
	bs, err := prepareArm(ctx, env, ArmBase, LeakStrict, c, nil, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cs, err := prepareArm(ctx, env, ArmCandidate, LeakStrict, c, nil, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	be, ce := evalCfgOf(t, bs), evalCfgOf(t, cs)
	if be.MaxTokens != 1234 || ce.MaxTokens != config.Default().Context.MaxTokens {
		t.Fatalf("base phải có strategy, candidate (đã bỏ) phải về gốc: base=%d candidate=%d", be.MaxTokens, ce.MaxTokens)
	}
}

// [.2] path=off + guardrail: hook Edit chỉ chạy guardrail, không chèn tri thức theo đường dẫn.
func TestEvalHookPathOffWithGuardrailDoesNotInjectPathKnowledge(t *testing.T) {
	ctx := context.Background()
	env, c := harnessRepo(t)
	dir, logDir := t.TempDir(), t.TempDir()
	s, err := prepareArm(ctx, env, "bint@path=off", LeakStrict, c, nil, dir, logDir)
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(readStr(t, s.Settings)), &settings); err != nil {
		t.Fatal(err)
	}
	editHook := false
	for _, g := range settings.Hooks["PreToolUse"] {
		editHook = editHook || strings.Contains(g.Matcher, "Edit")
	}
	if !editHook {
		t.Fatalf("path=off + guardrail vẫn cần hook Edit: %v", settings.Hooks)
	}
	if !evalCfgOf(t, s).NoPathHook {
		t.Fatal("EvalConfig phải mang cờ NoPathHook")
	}
	t.Setenv(ctxengine.EvalEnv, s.EvalConfig)
	res, err := hookcoreHandleEdit(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Output, "chữ ký") {
		t.Fatalf("path=off không được chèn tri thức theo đường dẫn: %q", res.Output)
	}
	lines, _ := ctxengine.ReadLog(s.ContextLog)
	for _, l := range lines {
		if l.Channel == ctxengine.ChannelHookPath {
			t.Fatalf("không được ghi kênh hook_path: %+v", l)
		}
	}
	// Đối chứng: nhánh bint đầy đủ vẫn chèn.
	s2, err := prepareArm(ctx, env, ArmBint, LeakStrict, c, nil, dir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(ctxengine.EvalEnv, s2.EvalConfig)
	if res, err := hookcoreHandleEdit(ctx, dir); err != nil || !strings.Contains(res.Output, "chữ ký") {
		t.Fatalf("đối chứng: nhánh bint phải chèn tri thức theo đường dẫn: %q %v", res.Output, err)
	}
}

// [.8] Kiểm rò rỉ phải thấy cả Title và Usage (được render vào AGENTS.md).
func TestHarnessLeakCheckCoversTitleAndUsage(t *testing.T) {
	ctx := context.Background()
	env, c := harnessRepo(t)
	leak := &LeakInput{Commit: "sol", AddedLines: []string{"func C() { return secretSolutionCall(42) }"},
		NewIdents: map[string]bool{"secretSolutionCall": true}, TestIdents: map[string]bool{}}
	writeFile(t, env.BintRoot, ".bint/harness/guidance/leak-title.md", "---\n"+hmeta("leak-title", "verified")+"title: \"func C() { return secretSolutionCall(42) }\"\n---\nNội dung sạch.\n")
	if _, err := prepareArm(ctx, env, ArmBint, LeakStrict, c, leak, t.TempDir(), t.TempDir()); err == nil {
		t.Fatal("Title chứa mã lời giải phải làm nhánh không khả dụng")
	}
	writeFile(t, env.BintRoot, ".bint/harness/guidance/leak-title.md", "---\n"+hmeta("leak-title", "deprecated")+"---\nx\n")
	writeFile(t, env.BintRoot, ".bint/harness/scripts/run-it.yml", hmeta("run-it", "verified")+"description: Chạy test\nusage: \"func C() { return secretSolutionCall(42) }\"\n")
	if _, err := prepareArm(ctx, env, ArmBint, LeakStrict, c, leak, t.TempDir(), t.TempDir()); err == nil {
		t.Fatal("Usage chứa mã lời giải phải làm nhánh không khả dụng")
	}
}

// hookcoreHandleEdit chạy hook PreToolUse(Edit lib/a.go) của lượt eval đang đặt trong BINT_EVAL_CONTEXT.
func hookcoreHandleEdit(ctx context.Context, dir string) (hookcore.Result, error) {
	return hookcore.Handle(ctx, hookcore.Input{SessionID: "S1", Cwd: dir, Event: hookcore.EvPreTool, ToolName: "Edit",
		ToolInput: map[string]any{"file_path": filepath.Join(dir, "lib", "a.go")}}, claude.Dialect, hookcore.Env{})
}
