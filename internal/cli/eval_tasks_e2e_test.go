package cli_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"bint/internal/app"
	"bint/internal/eval"
	"bint/internal/task"
	"bint/internal/testutil"
)

// TestEvalMineFromTasksEndToEnd: phiếu b-do đã done → ứng viên → verify fail→pass → approve với split theo ID.
func TestEvalMineFromTasksEndToEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("sh"); err != nil {
			t.Skip("cần sh")
		}
	}
	testutil.Isolate(t)
	asHuman(t)
	calc := "add() { echo $(($1 + $2)); }\n"
	root := testutil.NewRepo(t, map[string]string{
		"lib/calc.sh":       calc,
		"tests/run.sh":      "set -e\nfor f in tests/test_*.sh; do sh \"$f\"; done\n",
		"tests/test_add.sh": ". lib/calc.sh\n[ \"$(add 2 3)\" = 5 ]\n",
	})
	base := testutil.Git(t, root, "rev-parse", "HEAD")
	baseTree := testutil.Git(t, root, "rev-parse", "HEAD^{tree}")
	gitCommit(t, root, "solution", map[string]string{"lib/calc.sh": calc + "mul() { echo $(($1 * $2)); }\n", "tests/test_mul.sh": ". lib/calc.sh\n[ \"$(mul 4 5)\" = 20 ]\n"})
	after := testutil.Git(t, root, "rev-parse", "HEAD^{tree}")
	testutil.Git(t, root, "reset", "-q", "--hard", base)

	testutil.Git(t, root, "config", "user.name", "t")
	testutil.Git(t, root, "config", "user.email", "t@example.com")
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	local := "eval:\n  test_command: \"sh tests/run.sh\"\n  mine:\n    min_source_lines: 1\n"
	if err := os.WriteFile(filepath.Join(root, ".bint", "local.yml"), []byte(local), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := app.OpenWorkspace(context.Background(), root, true)
	if err != nil {
		t.Fatal(err)
	}
	store := task.NewStore(ws.Paths.Runtime)
	done := func(wf, status string) task.State {
		return task.State{Status: status, Workflow: wf, Base: base, BaseTree: baseTree,
			Landed:  &task.Landing{Tree: after, Files: []string{"lib/calc.sh", "tests/test_mul.sh"}},
			History: []task.Transition{{To: task.StatusDone, At: "2026-11-10T00:00:00Z", By: "bint"}}}
	}
	tk, err := store.Create(task.Ticket{Goal: "Add a multiplication helper", Request: "Add a mul function to the calc library so callers can compute products, not only sums.",
		Verify: task.Verify{Command: "sh tests/run.sh", Criteria: []string{"mul 4 5 prints 20"}}}, done("b-do", task.StatusDone), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// Phiếu khác workflow hoặc chưa done không tạo ứng viên.
	for _, st := range []task.State{done("b-feat", task.StatusDone), done("b-do", task.StatusInReview)} {
		if _, err := store.Create(task.Ticket{Goal: "Other task"}, st, time.Now().Add(time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	id := eval.TaskCaseID(tk.State.ID)

	out, code := run(t, root, "", "eval", "mine", "--from-tasks", "--verify", "--verify-runs", "1")
	if code != 0 || !strings.Contains(out, "Đã quét 1 phiếu, 1 ứng viên mới") || !strings.Contains(out, "verified") {
		t.Fatalf("mine --from-tasks: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "eval", "mine", "--from-tasks"); code != 0 || !strings.Contains(out, "0 ứng viên mới") || !strings.Contains(out, "đã có") {
		t.Fatalf("re-mine: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "eval", "approve", id); code != 0 {
		t.Fatalf("approve: %d\n%s", code, out)
	}
	data, err := os.ReadFile(filepath.Join(root, ".bint", "evals", "cases", id+".yml"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Contains(s, "original_message") || !strings.Contains(s, "split: "+eval.AssignSplit(id)) ||
		!strings.Contains(s, "task: "+tk.State.ID) || !strings.Contains(s, "mul 4 5 prints 20") {
		t.Fatalf("case file:\n%s", s)
	}
}
