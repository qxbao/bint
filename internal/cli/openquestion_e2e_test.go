package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// questionAgentScript giống agentScript nhưng b-req để lại một câu hỏi mở.
var questionAgentScript = strings.Replace(agentScript, `## Câu hỏi mở\n'`, `## Câu hỏi mở\n- [ ] Q-1: giữ v1 không?\n'`, 1)

func TestOpenQuestionEndToEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tác nhân giả viết bằng sh")
	}
	if questionAgentScript == agentScript {
		t.Fatal("không chèn được câu hỏi mở vào tác nhân giả")
	}
	root := newLintRepo(t)
	os.WriteFile(filepath.Join(root, ".bint", "local.yml"), []byte("eval:\n  test_command: \"test -f done.txt\"\n"), 0o644)
	script := filepath.Join(t.TempDir(), "agent.sh")
	os.WriteFile(script, []byte(questionAgentScript), 0o755)
	t.Setenv("BINT", bintBin)
	t.Setenv("MARK", filepath.Join(t.TempDir(), "mark"))
	agent := "sh " + script
	const req = "Thêm tính năng v2 cho app.txt"

	// b-do: dừng blocked trước b-feat vì còn Q-1.
	out, code := run(t, root, "", "run", "b-do", "--agent-command", agent, "--plan", "--criteria", "app.txt chứa v2", req)
	if code != 1 || !strings.Contains(out, "blocked") || !strings.Contains(out, "Q-1") || strings.Contains(out, "b-feat  ") {
		t.Fatalf("phải blocked vì câu hỏi mở: %d\n%s", code, out)
	}
	id := newestTaskID(t, root)
	if out, code := run(t, root, "", "task", "lint", id); code != 1 || !strings.Contains(out, "Q-1") {
		t.Fatalf("lint phải báo câu hỏi mở: %d\n%s", code, out)
	}
	// Chưa trả lời: resume vẫn bị chặn, không gọi b-feat.
	if out, code := run(t, root, "", "run", "resume", id, "--agent-command", agent); code != 1 || strings.Contains(out, "b-feat  ") {
		t.Fatalf("resume khi chưa trả lời: %d\n%s", code, out)
	}
	// Ghi câu trả lời → resume vào thẳng b-feat, b-req không chạy lại.
	answered := "# Yêu cầu\n## Tiêu chí chấp nhận\n- [ ] AC-1: app.txt chứa v2\n## Câu hỏi mở\n- [x] Q-1: giữ v1 không? — Trả lời: không\n"
	if out, code := run(t, root, answered, "task", "write", id, "requirement", "-"); code != 0 {
		t.Fatalf("write: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "task", "lint", id); code != 0 {
		t.Fatalf("lint sau khi trả lời: %d\n%s", code, out)
	}
	out, code = run(t, root, "", "run", "resume", id, "--agent-command", agent)
	if code != 0 || !strings.Contains(out, ": done") || strings.Contains(out, "b-req  ") || !strings.Contains(out, "b-feat") {
		t.Fatalf("resume sau trả lời: %d\n%s", code, out)
	}
}

func TestOpenQuestionStandaloneReqExitsNonZeroAndStaysPlanned(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tác nhân giả viết bằng sh")
	}
	root := newLintRepo(t)
	script := filepath.Join(t.TempDir(), "agent.sh")
	os.WriteFile(script, []byte(questionAgentScript), 0o755)
	t.Setenv("BINT", bintBin)
	t.Setenv("MARK", filepath.Join(t.TempDir(), "mark"))
	out, code := run(t, root, "", "run", "b-req", "--agent-command", "sh "+script, "--criteria", "app.txt chứa v2", "Thêm tính năng v2 cho app.txt")
	if code != 1 || !strings.Contains(out, ": planned") || !strings.Contains(out, "Q-1") {
		t.Fatalf("b-req riêng gặp Q mở: %d\n%s", code, out)
	}
	id := newestTaskID(t, root)
	if out, _ := run(t, root, "", "task", "show", id, "--json"); !strings.Contains(out, `"status": "planned"`) {
		t.Fatalf("phải ở planned, không blocked:\n%s", out)
	}
}

func newestTaskID(t *testing.T, root string) string {
	t.Helper()
	out, _ := run(t, root, "", "task", "list", "--all", "--json")
	var rows []struct{ ID string }
	if err := json.Unmarshal(jsonArray(out), &rows); err != nil || len(rows) == 0 {
		t.Fatalf("task list: %v\n%s", err, out)
	}
	return rows[0].ID
}
