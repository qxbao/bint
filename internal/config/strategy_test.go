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

func TestStrategyBrokenVerifiedIsIgnoredWithProblem(t *testing.T) {
	cases := map[string]string{
		"khoá lạ trong context": "status: verified\nid: strategy\nscope: global\ncontext:\n  retrieval:\n    lexical: false\n",
		"khoá lạ ở gốc":         "status: verified\nid: strategy\nscope: global\nbogus: 1\ncontext:\n  max_tokens: 5\n",
		"khoá lạ trong lessons": "status: verified\nid: strategy\nscope: global\ncontext:\n  lessons:\n    k: 1\n",
		"scope riêng":           "status: verified\nid: strategy\nscope: domain:src/**\ncontext:\n  max_tokens: 5\n",
		"giá trị không hợp lệ":  "status: verified\nid: strategy\nscope: global\ncontext:\n  max_tokens: 0\n",
		"cú pháp hỏng":          "status: verified\nid: [strategy\n",
	}
	def := Default()
	for name, body := range cases {
		root := strategyRepo(t, body)
		cfg, src, err := Load(root, true)
		if err != nil {
			t.Errorf("%s: strategy hỏng không được làm hỏng Load: %v", name, err)
			continue
		}
		if src.Strategy != "" || src.StrategyProblem == "" || !strings.Contains(src.StrategyProblem, "strategy.yml") {
			t.Errorf("%s: phải bỏ qua và nêu lý do: %+v", name, src)
		}
		if cfg.Context.MaxTokens != def.Context.MaxTokens {
			t.Errorf("%s: tệp hỏng không được áp một phần: %+v", name, cfg.Context)
		}
	}
}

func TestStrategyBrokenNotVerifiedDoesNotFailLoad(t *testing.T) {
	// Candidate/deprecated hỏng (do người hoặc agent ghi) cũng không được làm hỏng mọi lệnh bint.
	for _, body := range []string{
		"status: candidate\nbogus: 1\ncontext:\n  nope: 2\n",
		"status: candidate\nid: [strategy\n",
		"status: deprecated\nscope: domain:x\ncontext:\n  max_tokens: 0\n",
		"\tkhông phải yaml: [\n",
	} {
		root := strategyRepo(t, body)
		_, src, err := Load(root, true)
		if err != nil || src.Strategy != "" {
			t.Errorf("%q: err=%v src=%+v", body, err, src)
		}
	}
}
