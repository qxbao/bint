package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Skill b-do và mẫu flowskill phải mô tả nối tiếp phiên của GĐ2 (SendMessage khi có continue_agent_id, fallback --fresh, vòng
// hỏi–đáp b-req) bằng lệnh và cờ CÓ trong CLI.
func TestSkillsDescribeSessionContinuation(t *testing.T) {
	root := repoRootForTest(t)
	rootCmd := NewRoot(IO{In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}})
	for _, f := range []string{"internal/skills/core/b-do/SKILL.md", "internal/skills/flowskill/SKILL.md.tmpl"} {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f)))
		if err != nil {
			t.Fatal(err)
		}
		text := string(b)
		for _, want := range []string{"continue_agent_id", "SendMessage", "--fresh", "next <id> --resume"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s thiếu %q", f, want)
			}
		}
	}
	b, err := os.ReadFile(filepath.Join(root, "internal/skills/core/b-do/SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "Yêu cầu mơ hồ") {
		t.Error("b-do thiếu quy tắc định tuyến yêu cầu mơ hồ")
	}
	for _, cmd := range []string{
		"bint run next <id> --fresh --json",
		"bint run next <id> --resume --fresh --json",
		"bint run complete <id> --run <run_id> --agent-id <agentId> --json",
	} {
		if msg := checkDocCommand(rootCmd, cmd); msg != "" {
			t.Errorf("%q không khớp CLI: %s", cmd, msg)
		}
	}
}
