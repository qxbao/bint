package cli_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func taskCount(t *testing.T, root string) int {
	t.Helper()
	out, _ := run(t, root, "", "task", "list", "--all", "--json")
	var rows []struct{ ID string }
	if err := json.Unmarshal(jsonArray(out), &rows); err != nil {
		t.Fatalf("task list: %v\n%s", err, out)
	}
	return len(rows)
}

func jsonArray(out string) []byte {
	i, j := strings.Index(out, "["), strings.LastIndex(out, "]")
	if i < 0 || j < i {
		return []byte(out)
	}
	return []byte(out[i : j+1])
}

func TestDepthFlagValidation(t *testing.T) {
	root := newLintRepo(t)
	cases := map[string][]string{
		"no-plan + deep (run)":    {"run", "b-do", "--no-plan", "--depth", "deep", "Đổi app.txt"},
		"b-feat + deep":           {"run", "b-feat", "--depth", "deep", "Đổi app.txt"},
		"b-review + standard":     {"run", "b-review", "--depth", "standard", "--task", "X"},
		"ngoài enum (run)":        {"run", "b-do", "--depth", "shallow", "Đổi app.txt"},
		"no-plan + deep (new)":    {"task", "new", "--no-plan", "--depth", "deep", "Đổi app.txt"},
		"workflow b-feat (new)":   {"task", "new", "--workflow", "b-feat", "--depth", "deep", "Đổi app.txt"},
		"ngoài enum (new)":        {"task", "new", "--depth", "x", "Đổi app.txt"},
		"workflow b-review (new)": {"task", "new", "--workflow", "b-review", "--depth", "deep", "Đổi app.txt"},
	}
	for name, args := range cases {
		if out, code := run(t, root, "", args...); code != 2 {
			t.Errorf("%s: mã %d, muốn 2 (usage)\n%s", name, code, out)
		}
	}
	if n := taskCount(t, root); n != 0 {
		t.Fatalf("lỗi cờ phải xảy ra trước khi tạo task, có %d task", n)
	}
	// --depth auto không đổi nghĩa --no-plan/--plan: không phải lỗi tổ hợp.
	if out, code := run(t, root, "", "task", "new", "--no-plan", "--depth", "auto", "Đổi app.txt"); code != 0 {
		t.Fatalf("--depth auto + --no-plan: %d\n%s", code, out)
	}
	// resume không nhận --depth.
	if _, code := run(t, root, "", "run", "resume", "X", "--depth", "deep"); code != 2 {
		t.Fatalf("resume không có --depth: mã %d", code)
	}
}

func TestTaskNewAndShowDepth(t *testing.T) {
	root := newLintRepo(t)
	out, code := run(t, root, "", "task", "new", "--json", "--depth", "deep", "Đổi app.txt")
	var created struct {
		ID             string
		Classification struct {
			Plan        bool
			Depth       string
			DepthReason string `json:"depth_reason"`
		}
		Assessment struct{ Risk string }
	}
	if code != 0 || json.Unmarshal([]byte(out), &created) != nil || !created.Classification.Plan ||
		created.Classification.Depth != "deep" || created.Classification.DepthReason != "user_override" || created.Assessment.Risk != "low" {
		t.Fatalf("task new: %d\n%s", code, out)
	}
	out, _ = run(t, root, "", "task", "show", created.ID, "--json")
	var shown struct {
		Classification struct {
			Depth       string
			DepthReason string `json:"depth_reason"`
		}
	}
	if json.Unmarshal([]byte(out), &shown) != nil || shown.Classification.Depth != "deep" || shown.Classification.DepthReason != "user_override" {
		t.Fatalf("task show --json thiếu classification.depth:\n%s", out)
	}
	if out, _ := run(t, root, "", "task", "show", created.ID); !strings.Contains(out, "Phân tích:") || !strings.Contains(out, "deep") {
		t.Fatalf("task show:\n%s", out)
	}
	// Tác vụ dài: auto chọn deep/complex_request, Risk vẫn low.
	long := "Ý 1\nÝ 2\nÝ 3\nÝ 4\nÝ 5\nÝ 6"
	out, _ = run(t, root, "", "task", "new", "--json", long)
	created.Classification.Depth = ""
	if json.Unmarshal([]byte(out), &created) != nil || created.Classification.Depth != "deep" ||
		created.Classification.DepthReason != "complex_request" || created.Assessment.Risk != "low" {
		t.Fatalf("auto dài:\n%s", out)
	}
}

func TestDepthOverrideOnPlannedTaskRejected(t *testing.T) {
	root := newLintRepo(t)
	id := newTaskID(t, root, "--no-plan", "Đổi app.txt")
	if out, code := run(t, root, "", "task", "status", id, "planned"); code != 0 {
		t.Fatalf("planned: %d\n%s", code, out)
	}
	out, code := run(t, root, "", "run", "b-do", "--task", id, "--depth", "deep", "--agent-command", "true")
	if code == 0 || !strings.Contains(out, "không đổi được độ sâu") || !strings.Contains(out, "b-req --task") {
		t.Fatalf("phải lỗi có hướng dẫn: %d\n%s", code, out)
	}
}

// depthAgentScript chép đề bài từng bước ra $PDIR rồi hành xử như agentScript.
const depthAgentScript = `S="$(grep -o 'step="[a-z-]*"' "$BINT_EVAL_PROMPT_FILE" | head -1 | cut -d'"' -f2)"
cp "$BINT_EVAL_PROMPT_FILE" "$PDIR/$S.txt"
` + agentScript

func TestDepthReachesReqPromptOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tác nhân giả viết bằng sh")
	}
	root := newLintRepo(t)
	os.WriteFile(filepath.Join(root, ".bint", "local.yml"), []byte("eval:\n  test_command: \"test -f done.txt\"\n"), 0o644)
	script := filepath.Join(t.TempDir(), "agent.sh")
	pdir := t.TempDir()
	os.WriteFile(script, []byte(depthAgentScript), 0o755)
	t.Setenv("BINT", bintBin)
	t.Setenv("MARK", filepath.Join(t.TempDir(), "mark"))
	t.Setenv("PDIR", pdir)
	out, code := run(t, root, "", "run", "b-do", "--agent-command", "sh "+script, "--depth", "deep",
		"--criteria", "app.txt chứa v2", "Thêm tính năng v2 cho app.txt")
	if code != 0 || !strings.Contains(out, ": done") {
		t.Fatalf("run b-do: %d\n%s", code, out)
	}
	req, _ := os.ReadFile(filepath.Join(pdir, "b-req.txt"))
	if !strings.Contains(string(req), "Phân tích: deep") || !strings.Contains(string(req), "Lý do: user_override") {
		t.Fatalf("prompt b-req:\n%s", req)
	}
	feat, _ := os.ReadFile(filepath.Join(pdir, "b-feat.txt"))
	if len(feat) == 0 || strings.Contains(string(feat), "Phân tích:") {
		t.Fatalf("prompt b-feat không có bước phân tích mới:\n%.300s", feat)
	}
}

// task show: lệnh setup worktree và trạng thái xác nhận (văn bản và --json); task approve của người xác nhận lệnh.
func TestTaskShowSetupCommand(t *testing.T) {
	asHuman(t)
	root := newLintRepo(t)
	if err := os.WriteFile(filepath.Join(root, ".bint", "local.yml"), []byte("workflow:\n  worktree:\n    setup_command: make deps\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := run(t, root, "", "task", "new", "--json", "--worktree", "Đổi app.txt")
	var created struct{ ID string }
	if code != 0 || json.Unmarshal([]byte(out), &created) != nil {
		t.Fatalf("task new: %d\n%s", code, out)
	}
	show := func() (struct {
		Command   string `json:"setup_command"`
		Confirmed bool   `json:"setup_confirmed"`
		Required  bool   `json:"setup_required"`
	}, string) {
		t.Helper()
		out, _ := run(t, root, "", "task", "show", created.ID, "--json")
		var s struct {
			Command   string `json:"setup_command"`
			Confirmed bool   `json:"setup_confirmed"`
			Required  bool   `json:"setup_required"`
		}
		if err := json.Unmarshal([]byte(out), &s); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		text, _ := run(t, root, "", "task", "show", created.ID)
		return s, text
	}
	s, text := show()
	sum := sha256.Sum256([]byte("make deps"))
	sha := hex.EncodeToString(sum[:])[:12]
	if s.Command != "make deps" || s.Confirmed || !s.Required || !strings.Contains(text, "Setup worktree: make deps  [chưa xác nhận: bint task approve "+created.ID+" --setup-sha "+sha+"]") {
		t.Fatalf("trước xác nhận: %+v\n%s", s, text)
	}
	// Không có --setup-sha: in lệnh và băm, không xác nhận.
	if out, code := run(t, root, "", "task", "approve", created.ID); code != 0 || !strings.Contains(out, "CHƯA được xác nhận: make deps") || !strings.Contains(out, "--setup-sha "+sha) {
		t.Fatalf("approve không băm: %d\n%s", code, out)
	}
	if s, _ := show(); s.Confirmed {
		t.Fatalf("thiếu --setup-sha mà đã xác nhận: %+v", s)
	}
	// Băm sai: lỗi, không xác nhận.
	if out, code := run(t, root, "", "task", "approve", created.ID, "--setup-sha", "0123456789ab"); code == 0 || !strings.Contains(out, "đã đổi") {
		t.Fatalf("approve băm sai: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "task", "approve", created.ID, "--setup-sha", sha); code != 0 || !strings.Contains(out, "Xác nhận lệnh setup worktree: make deps") {
		t.Fatalf("approve: %d\n%s", code, out)
	}
	s, text = show()
	if !s.Confirmed || !strings.Contains(text, "Setup worktree: make deps  [đã xác nhận]") {
		t.Fatalf("sau xác nhận: %+v\n%s", s, text)
	}
	// Lệnh đổi ⇒ cần xác nhận lại.
	if err := os.WriteFile(filepath.Join(root, ".bint", "local.yml"), []byte("workflow:\n  worktree:\n    setup_command: make other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if s, _ := show(); s.Command != "make other" || s.Confirmed {
		t.Fatalf("lệnh đổi phải chờ xác nhận lại: %+v", s)
	}
	// Agent không xác nhận được.
	t.Setenv("CLAUDECODE", "1")
	if out, code := run(t, root, "", "task", "approve", created.ID); code == 0 || !strings.Contains(out, "chỉ dành cho người") {
		t.Fatalf("agent approve: %d\n%s", code, out)
	}
}
