package cli_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"bint/internal/testutil"
)

func gitCommit(t *testing.T, root, msg string, files map[string]string) string {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	testutil.Git(t, root, "add", "-A")
	testutil.Git(t, root, "commit", "-q", "-m", msg)
	return testutil.Git(t, root, "rev-parse", "HEAD")
}

func TestEvalEndToEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("sh"); err != nil {
			t.Skip("cần sh")
		}
	}
	testutil.Isolate(t)
	calc := "add() { echo $(($1 + $2)); }\n"
	root := testutil.NewRepo(t, map[string]string{
		"lib/calc.sh":       calc,
		"tests/run.sh":      "set -e\nfor f in tests/test_*.sh; do sh \"$f\"; done\n",
		"tests/test_add.sh": ". lib/calc.sh\n[ \"$(add 2 3)\" = 5 ]\n",
	})
	good := gitCommit(t, root, "Add a multiplication helper to the calc library\n\nCallers need products, not only sums.",
		map[string]string{"lib/calc.sh": calc + "mul() { echo $(($1 * $2)); }\n", "tests/test_mul.sh": ". lib/calc.sh\n[ \"$(mul 4 5)\" = 20 ]\n"})
	bad := gitCommit(t, root, "Add a regression test for add with small numbers",
		map[string]string{"lib/calc.sh": "# sums\n" + calc + "mul() { echo $(($1 * $2)); }\n", "tests/test_add2.sh": ". lib/calc.sh\n[ \"$(add 1 1)\" = 2 ]\n"})
	goodID, badID := "c"+good[:12], "c"+bad[:12]

	testutil.Git(t, root, "config", "user.name", "t")
	testutil.Git(t, root, "config", "user.email", "t@example.com")
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	// Ghi cấu hình máy cá nhân: lệnh kiểm thử đã xác nhận, cho phép lời giải 1 dòng.
	local := "eval:\n  test_command: \"sh tests/run.sh\"\n  mine:\n    min_source_lines: 1\n"
	if err := os.WriteFile(filepath.Join(root, ".bint", "local.yml"), []byte(local), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := run(t, root, "", "eval", "mine", "--verify", "--verify-runs", "1")
	if code != 0 || !strings.Contains(out, "2 ứng viên mới") {
		t.Fatalf("mine: %d\n%s", code, out)
	}
	if !strings.Contains(out, goodID) || !strings.Contains(out, "verified") || !strings.Contains(out, "failed") {
		t.Fatalf("mine output:\n%s", out)
	}

	if out, code := run(t, root, "", "eval", "approve", badID); code != 1 || !strings.Contains(out, "đã bị loại") {
		t.Fatalf("approve rejected candidate: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "eval", "approve", goodID[:6], "--split", "validation"); code != 0 {
		t.Fatalf("approve: %d\n%s", code, out)
	}
	caseFile := filepath.Join(root, ".bint", "evals", "cases", goodID+".yml")
	data, err := os.ReadFile(caseFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "original_message") || !strings.Contains(string(data), "split: validation") ||
		!strings.Contains(string(data), "reviewed_by: t <t@example.com>") {
		t.Fatalf("case file:\n%s", data)
	}

	if out, code := run(t, root, "", "eval", "show", goodID); code != 0 || !strings.Contains(out, "Callers need products") {
		t.Fatalf("show: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "eval", "list"); code != 0 || !strings.Contains(out, "Tác vụ đã duyệt (1)") {
		t.Fatalf("list: %d\n%s", code, out)
	}

	// Claude Code cần ngân sách; lỗi trước khi gọi tới harness.
	if out, code := run(t, root, "", "eval", "run", "--arms", "none", "--yes"); code != 2 || !strings.Contains(out, "ngân sách") {
		t.Fatalf("run without budget: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "eval", "run", "--arms", "bogus", "--yes", "--budget", "1"); code != 2 || !strings.Contains(out, "không hợp lệ") {
		t.Fatalf("invalid arm: %d\n%s", code, out)
	}

	agentDir := t.TempDir()
	agent := filepath.Join(agentDir, "agent.sh")
	os.WriteFile(agent, []byte("echo 'mul() { echo $(($1 * $2)); }' >> lib/calc.sh\necho '{\"type\":\"result\",\"num_turns\":2,\"total_cost_usd\":0.01}'\n"), 0o644)
	out, code = run(t, root, "", "eval", "run", "--arms", "none", "--repeats", "2", "--yes", "--seed", "3",
		"--agent-command", "sh "+filepath.ToSlash(agent))
	if code != 0 || !strings.Contains(out, "completed, 2/2") {
		t.Fatalf("run: %d\n%s", code, out)
	}

	out, code = run(t, root, "", "eval", "report", "--json")
	if code != 0 {
		t.Fatalf("report: %d\n%s", code, out)
	}
	var rep struct {
		Batch struct {
			Status string
			Seed   uint64
		} `json:"batch"`
		Arms []struct {
			Arm      string `json:"arm"`
			Graded   int    `json:"graded"`
			Passed   int    `json:"passed"`
			PassRate struct {
				Mean *float64 `json:"mean"`
			} `json:"pass_rate"`
		} `json:"arms"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("report json: %v\n%s", err, out)
	}
	if rep.Batch.Status != "completed" || rep.Batch.Seed != 3 || len(rep.Arms) != 1 || rep.Arms[0].Passed != 2 ||
		rep.Arms[0].PassRate.Mean == nil || *rep.Arms[0].PassRate.Mean != 1 {
		t.Fatalf("report: %+v", rep)
	}
	if out, code := run(t, root, "", "eval", "report"); code != 0 || !strings.Contains(out, "Độ tin cậy của phép đo") {
		t.Fatalf("report text: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "eval", "estimate", "--arms", "none", "--repeats", "3", "--harness", "command"); code != 0 ||
		!strings.Contains(out, "trung bình 2 lượt trước") {
		t.Fatalf("estimate: %d\n%s", code, out)
	}
}
