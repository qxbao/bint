package harness

import (
	"strings"
	"testing"

	"bint/internal/config"
)

// ForRole: --role của chính role → --harness → roles.<role> → [role tuỳ biến chưa cấu hình: cách chọn của role gốc] →
// harnesses.default → claude.
func TestForRoleTable(t *testing.T) {
	cfg := config.Default()
	cfg.Harnesses.Enabled = []string{"claude", "codex", "antigravity"}
	cfg.Harnesses.Default = "claude"
	cfg.Roles.Implementer = "codex"
	cfg.Roles.Extra = map[string]string{"test-writer": "antigravity"}

	cases := []struct {
		name       string
		edit       func(*config.Config)
		sel        Selection
		role, base string
		want, src  string
	}{
		{"role tuỳ biến có cấu hình", nil, Selection{}, "test-writer", "implementer", "antigravity", "roles.test-writer"},
		{"role tuỳ biến chưa cấu hình ⇒ roles.<gốc>", func(c *config.Config) { c.Roles.Extra = nil }, Selection{}, "test-writer", "implementer", "codex", "roles.implementer"},
		{"role tuỳ biến, gốc không cấu hình ⇒ harnesses.default", func(c *config.Config) {
			c.Roles.Extra, c.Roles.Implementer, c.Harnesses.Default = nil, "", "codex"
		}, Selection{}, "test-writer", "implementer", "codex", "harnesses.default"},
		{"không gì cả ⇒ claude", func(c *config.Config) {
			c.Roles.Extra, c.Roles.Implementer, c.Harnesses.Default = nil, "", ""
		}, Selection{}, "test-writer", "implementer", "claude", "mặc định"},
		{"--harness thắng roles.<role>", nil, Selection{All: "claude"}, "test-writer", "implementer", "claude", "--harness"},
		{"--role <role> thắng --harness", nil, Selection{All: "claude", Roles: map[string]string{"test-writer": "codex"}}, "test-writer", "implementer", "codex", "--role test-writer"},
		{"--role <gốc> chỉ áp cho role tuỳ biến chưa cấu hình", nil, Selection{Roles: map[string]string{"implementer": "claude"}}, "test-writer", "implementer", "antigravity", "roles.test-writer"},
		{"--role <gốc> áp khi role tuỳ biến chưa cấu hình", func(c *config.Config) { c.Roles.Extra = nil }, Selection{Roles: map[string]string{"implementer": "claude"}}, "test-writer", "implementer", "claude", "--role implementer"},
		{"role gốc không đổi", nil, Selection{}, "implementer", "implementer", "codex", "roles.implementer"},
		{"role gốc: --role", nil, Selection{Roles: map[string]string{"implementer": "claude"}}, "implementer", "implementer", "claude", "--role implementer"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cf := cfg
			cf.Roles.Extra = map[string]string{"test-writer": "antigravity"}
			if c.edit != nil {
				c.edit(&cf)
			}
			h, src, err := ForRole(cf, c.sel, c.role, c.base)
			if err != nil || h != c.want || src != c.src {
				t.Fatalf("ForRole = (%q, %q, %v), muốn (%q, %q)", h, src, err, c.want, c.src)
			}
		})
	}

	// Harness chọn được vẫn phải nằm trong harnesses.enabled.
	cfg.Harnesses.Enabled = []string{"claude"}
	if _, _, err := ForRole(cfg, Selection{}, "test-writer", "implementer"); err == nil || !strings.Contains(err.Error(), "harnesses.enabled") {
		t.Fatalf("harness chưa bật: %v", err)
	}
	// ForStep của flow nhúng giống ForRole theo role của bước.
	cfg.Harnesses.Enabled = []string{"claude", "codex"}
	if h, src, err := ForStep(cfg, Selection{}, "b-feat"); err != nil || h != "codex" || src != "roles.implementer" {
		t.Fatalf("ForStep(b-feat) = %q %q %v", h, src, err)
	}
}

// RoleModelFor: --model → models.<role> → models.<role gốc> → "" (model chung của harness).
func TestRoleModelForTable(t *testing.T) {
	cfg := config.Default()
	cfg.Harnesses.Claude.Models.Extra = map[string]string{"test-writer": "haiku"}
	cfg.Harnesses.Codex.Models = config.RoleModels{Implementer: "gpt-impl"}
	cases := []struct {
		h, role, base, cli, want string
	}{
		{"claude", "test-writer", "implementer", "", "haiku"},              // role tuỳ biến có model
		{"claude", "test-writer", "implementer", "cli-model", "cli-model"}, // --model thắng
		{"codex", "test-writer", "implementer", "", "gpt-impl"},            // không có ⇒ model của role gốc
		{"claude", "other", "reviewer", "", "opus"},                        // role gốc reviewer mặc định
		{"antigravity", "x", "planner", "", config.DefaultRoleModels("antigravity").Planner},
		{"claude", "implementer", "implementer", "", "sonnet"}, // role gốc
		{"claude", "", "", "", ""},                             // bước giả
		{"claude", "bogus", "", "", ""},                        // không có role gốc
	}
	for _, c := range cases {
		if got := RoleModelFor(cfg, c.h, c.role, c.base, c.cli); got != c.want {
			t.Errorf("RoleModelFor(%s, %q, %q, %q) = %q, muốn %q", c.h, c.role, c.base, c.cli, got, c.want)
		}
	}
	if got := RoleModel(cfg, "claude", "implementer", ""); got != "sonnet" {
		t.Errorf("RoleModel = %q", got)
	}
}
