package eval

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"bint/internal/config"
)

// Kiểm thử TSK-1 (vết lượt đánh giá): stream-json kèm --verbose khi Trace, và files.txt liệt kê
// đúng các tệp agent đã sửa, không có tệp kiểm thử ẩn (được ghi trước khi áp chúng).

func TestArgsForTraceUsesStreamJSON(t *testing.T) {
	a := &ClaudeAgent{Cfg: config.EvalAgent{PermissionMode: "acceptEdits", AllowedTools: []string{"Read", "Edit"}}}
	plain := a.ArgsFor(AgentRequest{})
	if !slices.Contains(plain, "json") || slices.Contains(plain, "--verbose") {
		t.Fatalf("không Trace phải giữ json: %v", plain)
	}
	traced := a.ArgsFor(AgentRequest{Trace: true})
	if !slices.Contains(traced, "stream-json") || !slices.Contains(traced, "--verbose") {
		t.Fatalf("Trace phải dùng stream-json --verbose: %v", traced)
	}
	if allowed := traced[slices.Index(traced, "--allowedTools")+1]; strings.Contains(allowed, "Skill") {
		t.Fatalf("Trace không được cấp công cụ Skill: %s", allowed)
	}
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestWriteChangedFilesListsAgentEdits(t *testing.T) {
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	write := func(rel, body string) {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "package a\n")
	write("b.go", "package b\n")
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-qm", "base")
	prep := gitIn(t, dir, "rev-parse", "HEAD")

	// Agent sửa a.go, thêm sub/c.go; b.go không đổi.
	write("a.go", "package a\n\nvar X = 1\n")
	write("sub/c.go", "package sub\n")
	logDir := t.TempDir()
	if err := writeChangedFiles(context.Background(), dir, prep, logDir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(logDir, FilesListName))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(b), "a.go\nsub/c.go\n"; got != want {
		t.Fatalf("files.txt = %q, muốn %q", got, want)
	}
}

// Agent không công cụ (reflector): cờ tường minh --tools "" và không cờ --allowedTools/--mcp-config,
// dù cấu hình eval.agent có allowlist hay yêu cầu có MCP.
func TestArgsForNoToolsLocksTools(t *testing.T) {
	a := &ClaudeAgent{Cfg: config.EvalAgent{PermissionMode: "dontAsk", AllowedTools: []string{"Read", "Edit"}}, NoTools: true}
	args := a.ArgsFor(AgentRequest{MCPConfig: "/x/mcp.json", HasSkills: true})
	i := slices.Index(args, "--tools")
	if i < 0 || i+1 >= len(args) || args[i+1] != "" {
		t.Fatalf("phải có --tools \"\" (tắt hết công cụ): %q", args)
	}
	for _, bad := range []string{"--allowedTools", "--mcp-config"} {
		if slices.Contains(args, bad) {
			t.Fatalf("không được cấp công cụ qua %s: %q", bad, args)
		}
	}
	// Không NoTools: không có --tools.
	if plain := (&ClaudeAgent{Cfg: config.EvalAgent{PermissionMode: "dontAsk"}}).ArgsFor(AgentRequest{}); slices.Contains(plain, "--tools") {
		t.Fatalf("agent thường không được gắn --tools: %q", plain)
	}
}
