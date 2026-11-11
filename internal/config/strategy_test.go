package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func strategyRepo(t *testing.T, strategy string) string {
	t.Helper()
	t.Setenv("BINT_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, DirName, "harness"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ProjectPath(root), []byte(Render(InitParams{ProjectName: "p"})), 0o644); err != nil {
		t.Fatal(err)
	}
	if strategy != "" {
		if err := os.WriteFile(StrategyPath(root), []byte(strategy), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const strategyBody = "id: strategy\nscope: global\norigin: human\nversion: 1\ncontext:\n  max_tokens: 1234\n  lessons:\n    global_k: 1\n  interfaces:\n    mcp: false\n"

func TestStrategyVerifiedOverridesContext(t *testing.T) {
	root := strategyRepo(t, "status: verified\n"+strategyBody)
	cfg, src, err := Load(root, true)
	if err != nil {
		t.Fatal(err)
	}
	def := Default()
	if cfg.Context.MaxTokens != 1234 || cfg.Context.Lessons.GlobalK != 1 || cfg.Context.Interfaces.MCP {
		t.Fatalf("strategy verified phải ghi đè: %+v", cfg.Context)
	}
	// Khoá không khai báo giữ nguyên.
	if cfg.Context.PathMaxItems != def.Context.PathMaxItems || cfg.Context.Lessons.MaxTokens != def.Context.Lessons.MaxTokens ||
		cfg.Context.Interfaces.PathHooks != def.Context.Interfaces.PathHooks {
		t.Fatalf("khoá không khai báo bị đổi: %+v", cfg.Context)
	}
	if src.Strategy != StrategyPath(root) {
		t.Fatalf("Sources.Strategy = %q", src.Strategy)
	}
}

func TestStrategyNotVerifiedIgnored(t *testing.T) {
	for _, st := range []string{"candidate", "deprecated", ""} {
		root := strategyRepo(t, "status: "+st+"\n"+strategyBody)
		cfg, src, err := Load(root, true)
		if err != nil {
			t.Fatalf("%s: %v", st, err)
		}
		if cfg.Context.MaxTokens == 1234 || src.Strategy != "" {
			t.Fatalf("strategy %q không được có hiệu lực: %+v", st, cfg.Context)
		}
	}
}

func TestStrategyLocalStillWins(t *testing.T) {
	root := strategyRepo(t, "status: verified\n"+strategyBody)
	if err := os.WriteFile(LocalPath(root), []byte("context:\n  max_tokens: 777\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(root, true)
	if err != nil || cfg.Context.MaxTokens != 777 || cfg.Context.Lessons.GlobalK != 1 {
		t.Fatalf("local ghi đè strategy: %+v %v", cfg.Context, err)
	}
}

func TestStrategyRejectsUnknownKeysAndScopes(t *testing.T) {
	cases := map[string]string{
		"khoá lạ trong context": "status: verified\nid: strategy\nscope: global\ncontext:\n  retrieval:\n    lexical: false\n",
		"khoá lạ ở gốc":         "status: verified\nid: strategy\nscope: global\nbogus: 1\ncontext:\n  max_tokens: 5\n",
		"khoá lạ trong lessons": "status: verified\nid: strategy\nscope: global\ncontext:\n  lessons:\n    k: 1\n",
		"scope riêng":           "status: verified\nid: strategy\nscope: domain:src/**\ncontext:\n  max_tokens: 5\n",
		"giá trị không hợp lệ":  "status: verified\nid: strategy\nscope: global\ncontext:\n  max_tokens: 0\n",
	}
	for name, body := range cases {
		root := strategyRepo(t, body)
		if _, _, err := Load(root, true); err == nil {
			t.Errorf("%s: phải báo lỗi", name)
		} else if !strings.Contains(err.Error(), "strategy.yml") && !strings.Contains(err.Error(), "max_tokens") {
			t.Errorf("%s: lỗi không nêu nguồn: %v", name, err)
		}
	}
}
