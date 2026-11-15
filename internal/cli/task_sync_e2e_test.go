package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/testutil"
)

// B1: phiếu được xuất ra .bint/tasks, tự nhập bản đã commit, và `bint task sync` báo cáo.
func TestTaskLedgerEndToEnd(t *testing.T) {
	root := newLintRepo(t)
	head := testutil.Git(t, root, "rev-parse", "HEAD")
	id := newTaskID(t, root, "Đổi app.txt")
	dir := filepath.Join(root, ".bint", "tasks", id)
	for _, f := range []string{"task.yml", "state.json"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatalf("task new phải xuất %s: %v", f, err)
		}
	}
	if testutil.Git(t, root, "rev-parse", "HEAD") != head {
		t.Fatal("Bint không được tự commit")
	}
	out, code := run(t, root, "", "task", "sync", "--json")
	var rep struct {
		Items []struct{ ID, Action string }
	}
	if code != 0 || json.Unmarshal([]byte(out), &rep) != nil || len(rep.Items) != 1 || rep.Items[0].Action != "unchanged" {
		t.Fatalf("sync khi đã đồng bộ: %d\n%s", code, out)
	}

	// Bản đã commit trong cây mới hơn (như sau git pull): lệnh đọc tự nhập, không cần sync.
	sp := filepath.Join(dir, "state.json")
	data, _ := os.ReadFile(sp)
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	m["status"], m["updated_at"] = "blocked", "2099-01-01T00:00:00Z"
	nd, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(sp, nd, 0o644); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, root, "add", "-f", "--", ".bint/tasks")
	testutil.Git(t, root, "commit", "-q", "-m", "phiếu")
	out, code = run(t, root, "", "task", "show", id, "--json")
	if code != 0 || !strings.Contains(out, `"blocked"`) {
		t.Fatalf("task show phải tự nhập bản mới: %d\n%s", code, out)
	}

	// Cả hai phía đổi: báo xung đột, thoát khác 0, giữ bản cục bộ.
	m["reason"], m["updated_at"] = "từ cây", "2099-06-01T00:00:00Z"
	nd, _ = json.MarshalIndent(m, "", "  ")
	_ = os.WriteFile(sp, nd, 0o644)
	if out, code = run(t, root, "", "task", "set", id, "--goal", "mục tiêu cục bộ"); code != 0 {
		t.Fatalf("task set: %d\n%s", code, out)
	}
	out, code = run(t, root, "", "task", "sync")
	if code != 1 || !strings.Contains(out, "conflict") {
		t.Fatalf("sync xung đột: mã %d, muốn 1\n%s", code, out)
	}

	// tasks.ledger: local không đọc hay ghi .bint/tasks.
	root2 := newLintRepo(t)
	cfg := filepath.Join(root2, ".bint", "config.yml")
	c, _ := os.ReadFile(cfg)
	if err := os.WriteFile(cfg, []byte(strings.Replace(string(c), "ledger: git", "ledger: local", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	newTaskID(t, root2, "Đổi app.txt")
	if _, err := os.Stat(filepath.Join(root2, ".bint", "tasks")); err == nil {
		t.Fatal("tasks.ledger: local không được ghi .bint/tasks")
	}
	if out, code = run(t, root2, "", "task", "sync"); code == 0 {
		t.Fatalf("sync với ledger local phải báo lỗi: %s", out)
	}
}
