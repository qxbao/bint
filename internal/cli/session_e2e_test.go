package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// E2E của G3a (docs/isolation-subagent-plan.md mục 6): driver session bằng tiến trình bint thật. "Subagent giả" là chính
// test: ghi tệp ở thư mục của hành động và gọi `bint task review`, rồi `bint run complete`.

type sessAction struct {
	Action     string   `json:"action"`
	TaskID     string   `json:"task_id"`
	Status     string   `json:"task_status"`
	Step       string   `json:"step"`
	RunID      string   `json:"run_id"`
	Subagent   string   `json:"subagent"`
	Prompt     string   `json:"prompt"`
	Worktree   string   `json:"worktree"`
	ReadOnly   bool     `json:"readonly"`
	LeaseUntil string   `json:"lease_until"`
	Command    string   `json:"command"`
	Reason     string   `json:"reason"`
	Notes      []string `json:"notes"`
	Result     *struct {
		Step   string `json:"step"`
		Status string `json:"status"`
		Error  string `json:"error"`
	} `json:"result"`
}

// sess chạy `bint run <sub> …` và giải mã JSON hành động; trả cả mã thoát.
func sess(t *testing.T, dir string, args ...string) (sessAction, int, string) {
	t.Helper()
	out, code := run(t, dir, "", append([]string{"run"}, args...)...)
	var a sessAction
	// Stdout và stderr gộp một luồng: chỉ giải mã giá trị JSON đầu tiên (mã thoát 1 còn dòng "lỗi: …" phía sau).
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&a); err != nil && code != 2 {
		t.Fatalf("run %v: không phải JSON: %v\n%s", args, err, out)
	}
	return a, code, out
}

const reviewOK = `{"summary":"ổn","findings":[]}`

func TestSessionDriverEndToEnd(t *testing.T) {
	root := initWorktreeRepo(t)
	asAgent(t) // phiên orchestrator: CLAUDECODE/BINT_AGENT, không BINT_RUN_ID
	c := newTask(t, root, "--driver", "session", "Thêm tính năng A cho app.txt")
	if out, code := run(t, root, "", "task", "show", c.ID, "--json"); code != 0 || !strings.Contains(out, `"driver": "session"`) {
		t.Fatalf("task show phải có driver session: %d\n%s", code, out)
	}

	a, code, out := sess(t, root, "next", c.ID, "--json")
	if code != 0 || a.Action != "agent" || a.Step != "b-feat" || a.Subagent != "bint-implementer" || a.RunID == "" ||
		a.ReadOnly || a.LeaseUntil == "" || !strings.Contains(a.Prompt, c.ID) {
		t.Fatalf("next 1: %d\n%s", code, out)
	}
	if !exists(filepath.Join(root, ".git")) || a.Worktree == "" {
		t.Fatalf("worktree của hành động: %q", a.Worktree)
	}
	// Gọi lại: cùng bước.
	if b, code, _ := sess(t, root, "next", c.ID, "--json"); code != 0 || b.RunID != a.RunID || b.Prompt != a.Prompt {
		t.Fatalf("next 2 phải trả đúng bước: %+v", b)
	}
	// complete thiếu --run ⇒ lỗi dùng sai (mã 2); sai --run ⇒ bị từ chối (mã 1), không đổi gì.
	if out, code := run(t, root, "", "run", "complete", c.ID); code != 2 {
		t.Fatalf("thiếu --run phải mã 2: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "run", "complete", c.ID, "--run", "01HXXXXXXXXXXXXXXXXXXXXXXX", "--json"); code != 1 || !strings.Contains(out, "không khớp") {
		t.Fatalf("sai --run phải bị từ chối: %d\n%s", code, out)
	}

	// Subagent giả làm b-feat.
	if err := os.WriteFile(filepath.Join(a.Worktree, "app.txt"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	touch(t, filepath.Join(a.Worktree, "a.done"))
	d, code, out := sess(t, root, "complete", c.ID, "--run", a.RunID, "--agent-id", "agent-feat", "--json")
	if code != 0 || d.Action != "" || d.Result == nil || d.Result.Step != "b-feat" || d.Result.Status != "completed" {
		t.Fatalf("complete b-feat: %d\n%s", code, out)
	}
	if s := showTask(t, root, c.ID); s.State.Status != "in_review" {
		t.Fatalf("sau b-feat: %+v", s.State)
	}
	// complete lần hai cho cùng lượt: bước đã đóng.
	if out, code := run(t, root, "", "run", "complete", c.ID, "--run", a.RunID, "--json"); code != 1 || !strings.Contains(out, "không khớp") {
		t.Fatalf("complete lặp phải bị từ chối: %d\n%s", code, out)
	}

	// b-review (chỉ đọc): subagent ghi review qua bint task review.
	r, code, out := sess(t, root, "next", c.ID, "--json")
	if code != 0 || r.Action != "agent" || r.Step != "b-review" || !r.ReadOnly || r.Subagent != "bint-reviewer" {
		t.Fatalf("next review: %d\n%s", code, out)
	}
	if o, code := run(t, root, reviewOK, "task", "review", c.ID, "-"); code != 0 {
		t.Fatalf("task review: %d\n%s", code, o)
	}
	f, code, out := sess(t, root, "complete", c.ID, "--run", r.RunID, "--agent-id", "agent-review", "--json")
	if code != 0 || f.Action != "done" || f.Status != "done" || f.Result == nil || f.Result.Step != "b-review" {
		t.Fatalf("complete review: %d\n%s", code, out)
	}
	if s := showTask(t, root, c.ID); s.State.Status != "done" {
		t.Fatalf("cuối: %+v", s.State)
	}
	if z, code, _ := sess(t, root, "next", c.ID, "--json"); code != 0 || z.Action != "done" {
		t.Fatalf("next sau done: %+v", z)
	}
}

// Hợp đồng thuê giữa hai tiến trình bint: next của tác vụ khác thấy hợp đồng còn hạn ⇒ blocked (mã 1); abort gỡ hợp đồng.
func TestSessionLeaseAndAbortEndToEnd(t *testing.T) {
	root := initWorktreeRepo(t)
	asAgent(t)
	a := newTask(t, root, "--driver", "session", "--shared", "Thêm tính năng A cho app.txt")
	b := newTask(t, root, "--driver", "session", "--shared", "Thêm tính năng B cho app.txt")
	na, code, out := sess(t, root, "next", a.ID, "--json")
	if code != 0 || na.Action != "agent" {
		t.Fatalf("next A: %d\n%s", code, out)
	}
	nb, code, out := sess(t, root, "next", b.ID, "--no-wait", "--json")
	if code != 1 || nb.Action != "blocked" || !strings.Contains(nb.Reason, a.ID) || !strings.Contains(nb.Reason, "subagent") {
		t.Fatalf("next B phải blocked vì hợp đồng của A: %d\n%s", code, out)
	}
	// run b-feat kiểu tiến trình trên A bị từ chối khi bước còn dở.
	if out, code := run(t, root, "", "run", "b-feat", "--task", a.ID, "--json"); code == 0 || !strings.Contains(out, "bint run complete") {
		t.Fatalf("driver process trên tác vụ có bước dở: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "run", "abort", a.ID, "--reason", "orchestrator chết"); code != 0 {
		t.Fatalf("abort: %d\n%s", code, out)
	}
	if s := showTask(t, root, a.ID); s.State.Status != "interrupted" {
		t.Fatalf("sau abort: %+v", s.State)
	}
	// Khoá đã nhả: B (đang blocked vì khoá, như driver process) tiếp tục bằng --resume rồi lấy được bước.
	if nb, code, out = sess(t, root, "next", b.ID, "--no-wait", "--json"); code != 1 || nb.Action != "blocked" {
		t.Fatalf("next B không --resume vẫn dừng: %d\n%s", code, out)
	}
	if nb, code, out = sess(t, root, "next", b.ID, "--no-wait", "--resume", "--json"); code != 0 || nb.Action != "agent" || nb.Step != "b-feat" {
		t.Fatalf("next B sau abort: %d\n%s", code, out)
	}
	// run status thấy bước dở của B.
	if out, code := run(t, root, "", "run", "status", b.ID); code != 0 || !strings.Contains(out, "đang chạy bằng subagent") {
		t.Fatalf("run status: %d\n%s", code, out)
	}
}

// Role gán harness khác Claude ⇒ next trả action process với lệnh bint run của đúng bước.
func TestSessionProcessActionEndToEnd(t *testing.T) {
	root := initWorktreeRepo(t)
	asAgent(t)
	local := filepath.Join(root, ".bint", "local.yml")
	prev, _ := os.ReadFile(local)
	if err := os.WriteFile(local, append(prev, []byte("harnesses:\n  enabled: [claude, codex]\nroles:\n  implementer: codex\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	c := newTask(t, root, "--driver", "session", "Thêm tính năng A cho app.txt")
	a, code, out := sess(t, root, "next", c.ID, "--json")
	if code != 0 || a.Action != "process" || a.Command != "bint run b-feat --task "+c.ID+" --json" {
		t.Fatalf("next: %d\n%s", code, out)
	}
}
