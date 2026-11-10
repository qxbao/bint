package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/harness/hookcore"
)

func TestSessionEndTimeoutMatchesRealCodex(t *testing.T) {
	// Codex 0.167 thực tế cảnh báo và ép timeout SessionEnd xuống 3s.
	raw, err := os.ReadFile("testdata/codex-0.167/session-end-timeout.json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "clamping SessionEnd hook timeout to 3s") {
		t.Fatal("fixture không có cảnh báo thật")
	}
	path := filepath.Join(t.TempDir(), "hooks.json")
	if _, err := InstallHooks(path, "bint", t.TempDir(), time.Now()); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Timeout int `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	for _, g := range f.Hooks["SessionEnd"] {
		for _, h := range g.Hooks {
			if h.Timeout < 1 || h.Timeout > 3 {
				t.Fatalf("SessionEnd timeout=%d: Codex chỉ cho phép tối đa 3s", h.Timeout)
			}
		}
	}
}

func TestRealCodexHookPayloads(t *testing.T) {
	raw, err := os.ReadFile("testdata/codex-0.167/hooks.json")
	if err != nil {
		t.Fatal(err)
	}
	var records []struct {
		Stdin json.RawMessage `json:"stdin"`
	}
	if err := json.Unmarshal(raw, &records); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, record := range records {
		in, err := hookcore.NormalizeClaudeStyle(record.Stdin)
		if err != nil {
			t.Fatal(err)
		}
		if in.SessionID == "" || in.Cwd != "/repo" || in.Event == "" {
			t.Fatalf("payload mất trường: %+v", in)
		}
		seen[in.RawEvent] = true
		if in.ToolName == "" {
			continue
		}
		info := Classify(in.ToolName, in.ToolInput)
		switch in.ToolName {
		case "Bash":
			if info.Kind != hookcore.ToolShell || info.Command != "cat hookcheck.txt" {
				t.Fatalf("shell: %+v", info)
			}
		case "apply_patch":
			if info.Kind != hookcore.ToolEdit || len(info.Paths) == 0 {
				t.Fatalf("patch: %+v", info)
			}
			for _, path := range info.Paths {
				seen[filepath.Base(path)] = true
			}
		case "mcp__bint__context_for_task", "mcp__bint__context_for_paths", "mcp__bint__cite_knowledge":
			if info.Kind != hookcore.ToolMCP || info.MCPTool != strings.TrimPrefix(in.ToolName, "mcp__bint__") {
				t.Fatalf("MCP: %+v", info)
			}
		default:
			t.Fatalf("fixture thêm tool chưa kiểm tra: %s", in.ToolName)
		}
	}
	for _, key := range []string{"SessionStart", "SessionEnd", "PreToolUse", "PostToolUse", "hookcheck.txt", "probe.txt", "moved.txt", "extra.txt", "001.sql"} {
		if !seen[key] {
			t.Errorf("fixture chưa bao phủ %s", key)
		}
	}
}

func TestInstalledMCPCanCiteWithApprovalNever(t *testing.T) {
	// Lỗi thật được lưu cạnh fixture của executor, không dùng agent giả để kết luận.
	b, err := os.ReadFile("../../eval/testdata/codex-0.167/citation-denied.jsonl")
	if err != nil || !strings.Contains(string(b), "MCP tool call requires approval, but approval policy is never") {
		t.Fatalf("fixture citation: %s, %v", b, err)
	}
	block := MCPBlock("bint")
	if !strings.Contains(block, "[mcp_servers.bint.tools.cite_knowledge]\napproval_mode = \"approve\"") {
		t.Fatalf("citation vẫn cần duyệt dưới policy never:\n%s", block)
	}
	if strings.Contains(block, "default_tools_approval_mode") || strings.Contains(block, "report_observation") {
		t.Fatal("không được tự duyệt mọi tool hoặc tool tạo tri thức")
	}
}

func TestCitationApprovalConfigured(t *testing.T) {
	for _, tc := range []struct {
		name, config string
		want         bool
	}{
		{"installed", MCPBlock("bint"), true},
		{"missing", "[mcp_servers.bint]\ncommand = 'bint'\n", false},
		{"prompt", "[mcp_servers.bint.tools.cite_knowledge]\napproval_mode = 'prompt'\n", false},
		{"other tool", "[mcp_servers.bint.tools.report_observation]\napproval_mode = 'approve'\n", false},
		{"comment", "# [mcp_servers.bint.tools.cite_knowledge]\n# approval_mode = 'approve'\n", false},
		{"quoted", "[mcp_servers.\"bint\".tools.\"cite_knowledge\"] # local\napproval_mode = 'approve' # explicit\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			os.MkdirAll(filepath.Join(root, ".codex"), 0o755)
			os.WriteFile(ConfigPath(root), []byte(tc.config), 0o644)
			if got := CitationApprovalConfigured(root); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
