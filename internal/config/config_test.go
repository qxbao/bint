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
}

func TestRenderMatchesDefaults(t *testing.T) {
	cfg := Config{}
	if err := Decode(strings.NewReader(Render(InitParams{ProjectName: "p"})), &cfg); err != nil {
		t.Fatal(err)
	}
	want := Default()
	want.Project.Name = "p"
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
