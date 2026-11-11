package eval

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Mẫu đầu ra thật của `codex exec --json` (codex-cli 0.167), rút gọn.
const codexSample = `{"type":"thread.started","thread_id":"01a1c35f-6ec6-78e1-9ff0-14c28dcf01a5"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"item_0","type":"error","message":"Codex is ignoring 6 unrecognized configuration settings."}}
{"type":"item.completed","item":{"id":"item_5","type":"agent_message","text":"I'll add c.txt."}}
{"type":"item.started","item":{"id":"item_9","type":"file_change","changes":[{"path":"/r/c.txt","kind":"add"}],"status":"in_progress"}}
{"type":"item.completed","item":{"id":"item_11","type":"agent_message","text":"Created c.txt."}}
{"type":"turn.completed","usage":{"input_tokens":62713,"cached_input_tokens":29568,"cache_write_input_tokens":0,"output_tokens":509,"reasoning_output_tokens":195}}
`

func TestRealCodexJSONL(t *testing.T) {
	b, err := os.ReadFile("testdata/codex-0.167/citation-approved.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, []byte("{\"type\":\"future.event\",\"extra\":true}\n{\"type\":\"item.completed\"}\n")...)
	var r AgentResult
	parseCodexJSONL(b, &r)
	if r.Error != "" || r.InputTokens == nil || *r.InputTokens != 60266 || r.OutputTokens == nil || *r.OutputTokens != 247 || !strings.Contains(r.Text, "citation succeeded") {
		t.Fatalf("JSONL thật: %+v", r)
	}
	if r.CostUSD != nil || r.NumTurns != nil {
		t.Fatal("không được mô phỏng cost/turn từ token hoặc turn.started")
	}
}

// Mẫu đầu ra thật của `agy --output-format json` (agy 1.5.3).
const agySample = `{"conversation_id":"e15c9026","status":"SUCCESS","response":"Done.\n","duration_seconds":3.8,"num_turns":1,"usage":{"input_tokens":10046,"output_tokens":114,"thinking_tokens":0,"cache_read_tokens":5608,"total_tokens":10160}}`
const agyQuota = `{"conversation_id":"4a1b","status":"ERROR","response":"","error":"Individual quota reached.","duration_seconds":0,"num_turns":1,"usage":{"input_tokens":0,"output_tokens":0},"finished_reason":"error"}`

func TestParseCodexAndAgy(t *testing.T) {
	var r AgentResult
	parseCodexJSONL([]byte(codexSample), &r)
	if r.Text != "Created c.txt." || r.InputTokens == nil || *r.InputTokens != 62713 || *r.OutputTokens != 509 || r.Error != "" || r.CostUSD != nil {
		t.Fatalf("codex: %+v", r)
	}
	r = AgentResult{}
	parseCodexJSONL([]byte(`{"type":"turn.failed","error":{"message":"rate limited"}}`), &r)
	if !strings.Contains(r.Error, "rate limited") {
		t.Fatalf("codex lỗi: %+v", r)
	}
	r = AgentResult{}
	parseAgyJSON([]byte(agySample), &r)
	if r.Text != "Done.\n" || *r.NumTurns != 1 || *r.InputTokens != 10046 || *r.OutputTokens != 114 || r.Error != "" {
		t.Fatalf("agy: %+v", r)
	}
	r = AgentResult{}
	parseAgyJSON([]byte("log line\n"+agyQuota), &r)
	if !strings.Contains(r.Error, "quota") {
		t.Fatalf("agy lỗi: %+v", r)
	}
}

func TestCodexAndAgyArgs(t *testing.T) {
	mcp := filepath.Join(t.TempDir(), "mcp.json")
	os.WriteFile(mcp, []byte(`{"mcpServers":{"bint":{"command":"/x/bint","args":["mcp","serve","--eval-config","/l/c.json"]}}}`), 0o644)
	c := &CodexAgent{Model: "gpt-x", ExtraArgs: []string{"-c", "k=v"}}
	args, err := c.ArgsFor(AgentRequest{Dir: "/repo", WritableDirs: []string{"/state"}, MCPConfig: mcp, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	s := strings.Join(args, " ")
	for _, want := range []string{"exec --json", "--sandbox workspace-write", "--cd /repo", "--add-dir /state", "--model gpt-x",
		`mcp_servers.bint.command="/x/bint"`, `mcp_servers.bint.args=["mcp", "serve", "--eval-config", "/l/c.json"]`, "-c k=v"} {
		if !strings.Contains(s, want) {
			t.Errorf("codex thiếu %q: %s", want, s)
		}
	}
	if args[len(args)-1] != "-" || strings.Contains(s, "dangerously") {
		t.Fatalf("codex: %s", s)
	}
	a := &AgyAgent{Model: "gemini-x"}
	args, err = a.ArgsFor(AgentRequest{Prompt: "-p lạ", ReadOnly: true, WritableDirs: []string{"/state"}, Timeout: 90 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	s = strings.Join(args, " ")
	for _, want := range []string{"--output-format json", "--mode plan", "--add-dir /state", "--model gemini-x", "--print-timeout 90s"} {
		if !strings.Contains(s, want) {
			t.Errorf("agy thiếu %q: %s", want, s)
		}
	}
	if args[len(args)-1] != "--print=-p lạ" || strings.Contains(s, "dangerously") {
		t.Fatalf("agy: %s", s)
	}
	args, _ = a.ArgsFor(AgentRequest{Prompt: "x"})
	if !strings.Contains(strings.Join(args, " "), "--mode accept-edits") {
		t.Fatal("bước ghi phải accept-edits")
	}
	if _, err := a.ArgsFor(AgentRequest{Prompt: strings.Repeat("x", MaxPromptBytes+1)}); err == nil {
		t.Fatal("đề bài quá dài phải lỗi")
	}
	if a.ArmUnavailable("bint") == "" || a.ArmUnavailable("minimal") != "" || c.ArmUnavailable("bint") != "" {
		t.Fatal("ArmUnavailable")
	}
}

func TestCodexEvalCitationApproval(t *testing.T) {
	mcp := filepath.Join(t.TempDir(), "mcp.json")
	os.WriteFile(mcp, []byte(`{"mcpServers":{"bint":{"command":"bint","args":["mcp","serve"]}}}`), 0o644)
	args, err := (&CodexAgent{}).ArgsFor(AgentRequest{MCPConfig: mcp})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(args, " "), `mcp_servers.bint.tools.cite_knowledge.approval_mode="approve"`) {
		t.Fatalf("eval không cấu hình quyền citation: %v", args)
	}
	plain, _ := (&CodexAgent{}).ArgsFor(AgentRequest{})
	if strings.Contains(strings.Join(plain, " "), "approval_mode") {
		t.Fatal("không cấu hình MCP khi không được yêu cầu")
	}
}

func TestCodexEvalPathHook(t *testing.T) {
	// F2 đã bắt được tool_name=apply_patch, tool_input.command trên Codex thật.
	mcp := filepath.Join(t.TempDir(), "mcp.json")
	os.WriteFile(mcp, []byte(`{"mcpServers":{"bint":{"command":"/path with spaces/bint","args":["mcp","serve"]}}}`), 0o644)
	a := &CodexAgent{}
	args, err := a.ArgsFor(AgentRequest{MCPConfig: mcp, Settings: "settings.json"})
	if err != nil {
		t.Fatal(err)
	}
	s := strings.Join(args, " ")
	if !strings.Contains(s, `hooks.PreToolUse=[{matcher="apply_patch"`) || !strings.Contains(s, `hook codex`) {
		t.Fatalf("nhánh bint thiếu path hook Codex: %v", args)
	}
	if strings.Contains(s, "hook claude") || strings.Contains(s, "bypass-hook-trust") {
		t.Fatal("không mượn hook Claude hoặc tự bỏ duyệt trust")
	}
	plain, _ := a.ArgsFor(AgentRequest{MCPConfig: mcp})
	if strings.Contains(strings.Join(plain, " "), "hooks.PreToolUse") {
		t.Fatal("không thêm hook khi nhánh không yêu cầu")
	}
}

func TestCodexEvalHookQuotesExecutableLiterally(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("kiểm chứng POSIX shell")
	}
	for _, name := range []string{"literal$(printf expanded)", "single'and\"double", `back\slash$HOME`} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			bin := filepath.Join(root, name, "bint")
			os.MkdirAll(filepath.Dir(bin), 0o755)
			os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s %s' \"$1\" \"$2\"\n"), 0o755)
			mcp := filepath.Join(root, "mcp.json")
			b, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"bint": map[string]any{"command": bin}}})
			os.WriteFile(mcp, b, 0o644)
			args, err := (&CodexAgent{}).ArgsFor(AgentRequest{MCPConfig: mcp, Settings: "settings.json"})
			if err != nil {
				t.Fatal(err)
			}
			var command string
			for _, arg := range args {
				if !strings.HasPrefix(arg, "hooks.PreToolUse=") {
					continue
				}
				i := strings.Index(arg, "command=")
				if i < 0 {
					t.Fatal("thiếu command")
				}
				if err := json.NewDecoder(strings.NewReader(arg[i+len("command="):])).Decode(&command); err != nil {
					t.Fatal(err)
				}
			}
			out, err := exec.Command("sh", "-c", command).CombinedOutput()
			if err != nil || string(out) != "hook codex" {
				t.Fatalf("đường dẫn bị shell diễn giải: %s %v", out, err)
			}
		})
	}
}

// Chạy chương trình giả thay cho codex/agy: kiểm tra đề bài tới đúng kênh (stdin /
// đối số) và kết quả được đọc lại.
func TestCodexAndAgyRunWithFakeBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("kịch bản sh")
	}
	dir := t.TempDir()
	codexBin := filepath.Join(dir, "codex")
	os.WriteFile(codexBin, []byte(`#!/bin/sh
p=$(cat)
printf '%s\n' '{"type":"thread.started","thread_id":"t1"}'
printf '{"type":"item.completed","item":{"type":"agent_message","text":"got:%s"}}\n' "$p"
printf '%s\n' '{"type":"turn.completed","usage":{"input_tokens":5,"output_tokens":2}}'
`), 0o755)
	r := (&CodexAgent{Binary: codexBin}).Run(context.Background(), AgentRequest{Dir: dir, Prompt: "hello", Timeout: 10 * time.Second,
		LogPath: filepath.Join(dir, "log", "codex.log")})
	if r.Error != "" || r.Text != "got:hello" || *r.InputTokens != 5 {
		t.Fatalf("codex run: %+v", r)
	}
	agyBin := filepath.Join(dir, "agy")
	os.WriteFile(agyBin, []byte(`#!/bin/sh
for a in "$@"; do case "$a" in --print=*) p="${a#--print=}";; esac; done
printf '{"status":"SUCCESS","response":"got:%s","num_turns":2,"usage":{"input_tokens":7,"output_tokens":1}}\n' "$p"
`), 0o755)
	r = (&AgyAgent{Binary: agyBin}).Run(context.Background(), AgentRequest{Dir: dir, Prompt: "hi", Timeout: 10 * time.Second})
	if r.Error != "" || r.Text != "got:hi" || *r.NumTurns != 2 {
		t.Fatalf("agy run: %+v", r)
	}
	// Chương trình không tồn tại: lỗi rõ ràng, không treo.
	r = (&CodexAgent{Binary: filepath.Join(dir, "missing")}).Run(context.Background(), AgentRequest{Dir: dir, Prompt: "x", Timeout: 5 * time.Second})
	if r.Error == "" || r.ExitCode == 0 {
		t.Fatalf("missing: %+v", r)
	}
	// Thoát lỗi: thông điệp gồm phần cuối đầu ra.
	bad := filepath.Join(dir, "bad")
	os.WriteFile(bad, []byte("#!/bin/sh\necho 'not logged in' >&2\nexit 3\n"), 0o755)
	r = (&AgyAgent{Binary: bad}).Run(context.Background(), AgentRequest{Dir: dir, Prompt: "x", Timeout: 5 * time.Second})
	if !strings.Contains(r.Error, "not logged in") || r.ExitCode != 3 {
		t.Fatalf("bad: %+v", r)
	}
}
