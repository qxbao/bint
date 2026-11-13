package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Config cũ (không có role tuỳ biến) vẫn nạp được và Extra rỗng; role tuỳ biến dưới roles và models vào Extra.
func TestDecodeExtraRoles(t *testing.T) {
	cfg := Default()
	if err := Decode(strings.NewReader("version: 1\n"), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Roles.Extra != nil || cfg.Harnesses.Claude.Models.Extra != nil {
		t.Fatalf("config cũ không được có Extra: %+v", cfg.Roles)
	}
	cfg = Default()
	src := "roles:\n  test-writer: codex\n  planner: claude\nharnesses:\n  claude:\n    models:\n      test-writer: haiku\n      planner: opus\n"
	if err := Decode(strings.NewReader(src), &cfg); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Roles.For("test-writer"); got != "codex" {
		t.Errorf("Roles.For(test-writer) = %q", got)
	}
	if got := cfg.Roles.For("planner"); got != "claude" {
		t.Errorf("Roles.For(planner) = %q", got)
	}
	if got := cfg.Harnesses.Models("claude").For("test-writer"); got != "haiku" {
		t.Errorf("models.For(test-writer) = %q", got)
	}
	if got := cfg.Roles.For("khong-co"); got != "" {
		t.Errorf("role lạ phải rỗng, nhận %q", got)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("cấu hình hợp lệ bị từ chối: %v", err)
	}
	// Khoá cố định vẫn nằm trong trường, không lọt vào Extra.
	if _, ok := cfg.Roles.Extra["planner"]; ok || len(cfg.Roles.Extra) != 1 {
		t.Errorf("Extra = %v", cfg.Roles.Extra)
	}
}

func TestValidateExtraRoles(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Config)
		want string // "" = hợp lệ
	}{
		{"hợp lệ", func(c *Config) { c.Roles.Extra = map[string]string{"test-writer": "claude"} }, ""},
		{"role sai cú pháp", func(c *Config) { c.Roles.Extra = map[string]string{"Test_Writer": "claude"} }, "roles.Test_Writer"},
		{"role trùng trường cố định", func(c *Config) { c.Roles.Extra = map[string]string{"planner": "claude"} }, "trùng role cố định"},
		{"role evolver", func(c *Config) { c.Roles.Extra = map[string]string{"evolver": "claude"} }, "trùng role cố định"},
		{"role interactive", func(c *Config) { c.Roles.Extra = map[string]string{"interactive": "claude"} }, "dành riêng"},
		{"harness lạ", func(c *Config) { c.Roles.Extra = map[string]string{"test-writer": "vim"} }, "harness không hỗ trợ"},
		{"model có khoảng trắng", func(c *Config) {
			c.Harnesses.Claude.Models.Extra = map[string]string{"test-writer": "a b"}
		}, "khoảng trắng"},
		{"model role trùng trường cố định", func(c *Config) {
			c.Harnesses.Codex.Models.Extra = map[string]string{"reviewer": "x"}
		}, "harnesses.codex.models.reviewer"},
		{"model role sai cú pháp", func(c *Config) {
			c.Harnesses.Antigravity.Models.Extra = map[string]string{"A B": "x"}
		}, "harnesses.antigravity.models"},
		{"limits tên flow", func(c *Config) {
			c.Limits.Workflows = map[string]PerRunLimits{"tdd": {MaxTurns: ptr(5)}}
		}, ""},
		{"limits khoá sai cú pháp", func(c *Config) {
			c.Limits.Workflows = map[string]PerRunLimits{"Bad_Name": {MaxTurns: ptr(5)}}
		}, "limits.workflows"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := Default()
			c.edit(&cfg)
			err := cfg.Validate()
			switch {
			case c.want == "" && err != nil:
				t.Fatalf("muốn hợp lệ, nhận %v", err)
			case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
				t.Fatalf("muốn lỗi chứa %q, nhận %v", c.want, err)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

// Lớp sau có `roles:`/`models:` không được làm mất role tuỳ biến của lớp trước (yaml.v3 đặt lại map inline).
func TestMergeFileKeepsExtraRoles(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.yml"), filepath.Join(dir, "b.yml")
	if err := os.WriteFile(a, []byte("roles:\n  test-writer: codex\n  qa: claude\nharnesses:\n  claude:\n    models:\n      test-writer: haiku\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("roles:\n  planner: codex\n  qa: codex\nharnesses:\n  claude:\n    models:\n      planner: sonnet\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	for _, p := range []string{a, b} {
		if _, err := mergeFile(&cfg, p, nil); err != nil {
			t.Fatal(err)
		}
	}
	want := map[string]string{"test-writer": "codex", "qa": "codex"}
	if !reflect.DeepEqual(cfg.Roles.Extra, want) {
		t.Errorf("Roles.Extra = %v, muốn %v", cfg.Roles.Extra, want)
	}
	if cfg.Roles.Planner != "codex" {
		t.Errorf("planner = %q", cfg.Roles.Planner)
	}
	if got := cfg.Harnesses.Claude.Models.Extra["test-writer"]; got != "haiku" {
		t.Errorf("models.test-writer = %q (lớp sau không có khoá này, phải giữ lớp trước)", got)
	}
	if cfg.Harnesses.Claude.Models.Planner != "sonnet" {
		t.Errorf("models.planner = %q", cfg.Harnesses.Claude.Models.Planner)
	}
}
