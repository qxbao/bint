package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/harness/hookcore"
)

const wsPrompt = "thân skill\n\n<bint-task id=\"T1\" step=\"b-feat\" run=\"R1\">\n"

func writeSubTranscript(t *testing.T, first string, rest ...string) (hookcore.Input, string) {
	t.Helper()
	dir := t.TempDir()
	parent := filepath.Join(dir, "proj", "sess1.jsonl")
	sub := SubagentTranscriptPath(parent, "sess1", "a1")
	if err := os.MkdirAll(filepath.Dir(sub), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sub, []byte(strings.Join(append([]string{first}, rest...), "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return hookcore.Input{SessionID: "sess1", AgentID: "a1", TranscriptPath: parent}, sub
}

func userLine(content any, extra map[string]any) string {
	m := map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": content}}
	for k, v := range extra {
		m[k] = v
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func TestSubagentTranscriptPathLayout(t *testing.T) {
	got := SubagentTranscriptPath("/h/.claude/projects/p/sess1.jsonl", "sess1", "a1")
	if want := filepath.FromSlash("/h/.claude/projects/p/sess1/subagents/agent-a1.jsonl"); got != want {
		t.Fatalf("%q, muốn %q", got, want)
	}
	for _, c := range [][3]string{{"", "s", "a"}, {"/p/t.jsonl", "../s", "a"}, {"/p/t.jsonl", "s", "../a"}, {"/p/t.jsonl", "s", ""}, {"/p/t.jsonl", "", "a"}, {"/p/t.jsonl", "s", "a/b"}} {
		if p := SubagentTranscriptPath(c[0], c[1], c[2]); p != "" {
			t.Errorf("%v phải bị từ chối, được %q", c, p)
		}
	}
}

func TestSubagentPrompt(t *testing.T) {
	hi, _ := writeSubTranscript(t, userLine(wsPrompt, map[string]any{"agentId": "a1"}), userLine("dòng sau", nil))
	if p, ok := SubagentPrompt(hi); !ok || p != wsPrompt {
		t.Fatalf("content chuỗi: %q %v", p, ok)
	}
	hi, _ = writeSubTranscript(t, userLine([]any{map[string]any{"type": "text", "text": wsPrompt}, map[string]any{"type": "image"}}, nil))
	if p, ok := SubagentPrompt(hi); !ok || !strings.Contains(p, `run="R1"`) {
		t.Fatalf("content mảng: %q %v", p, ok)
	}
}

func TestSubagentPromptUnreadable(t *testing.T) {
	// thiếu tệp
	if _, ok := SubagentPrompt(hookcore.Input{SessionID: "s", AgentID: "a", TranscriptPath: filepath.Join(t.TempDir(), "t.jsonl")}); ok {
		t.Error("thiếu tệp ⇒ false")
	}
	// thiếu transcript_path / agent_id
	if _, ok := SubagentPrompt(hookcore.Input{SessionID: "s"}); ok {
		t.Error("thiếu dữ liệu ⇒ false")
	}
	// JSON hỏng
	hi, _ := writeSubTranscript(t, "{không phải json")
	if _, ok := SubagentPrompt(hi); ok {
		t.Error("JSON hỏng ⇒ false")
	}
	// agentId lệch
	hi, _ = writeSubTranscript(t, userLine(wsPrompt, map[string]any{"agentId": "khac"}))
	if _, ok := SubagentPrompt(hi); ok {
		t.Error("agentId lệch ⇒ false")
	}
	// không phải message user
	hi, _ = writeSubTranscript(t, `{"type":"assistant","message":{"role":"assistant","content":"x"}}`)
	if _, ok := SubagentPrompt(hi); ok {
		t.Error("message assistant ⇒ false")
	}
	// session_id có ../
	hi, _ = writeSubTranscript(t, userLine(wsPrompt, nil))
	hi.SessionID = "../sess1"
	if _, ok := SubagentPrompt(hi); ok {
		t.Error("session_id có ../ ⇒ false")
	}
	// dòng quá trần
	big := userLine(strings.Repeat("x", maxSubagentPromptLine+10), nil)
	hi, _ = writeSubTranscript(t, big)
	if _, ok := SubagentPrompt(hi); ok {
		t.Error("dòng quá trần ⇒ false")
	}
}

// Dialect Claude gắn SubagentPrompt (guardrail bint-worktree cần nó).
func TestDialectHasSubagentPrompt(t *testing.T) {
	if Dialect.SubagentPrompt == nil {
		t.Fatal("claude.Dialect phải khai SubagentPrompt")
	}
}
