package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"bint/internal/testutil"
)

// featScript: b-feat giả sửa app.txt, thêm done.txt và ghi thư mục làm việc vào $MARK.
const featScript = `echo v2 > app.txt
echo ok > done.txt
if [ -n "$MARK" ]; then pwd > "$MARK"; fi
echo '{"type":"result","total_cost_usd":0.01,"num_turns":1,"result":"xong"}'
`

func initIsolationRepo(t *testing.T) string {
	t.Helper()
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"app.txt": "v1\n", "migrations/001.sql": "--\n"})
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	os.WriteFile(filepath.Join(root, ".bint", "local.yml"), []byte("eval:\n  test_command: \"test -f done.txt\"\n"), 0o644)
	return root
}

func writeScript(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "agent.sh")
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return "sh " + filepath.ToSlash(p) // sh -c nuốt dấu gạch chéo ngược: đường dẫn Windows trần ra mã thoát 127
}

func taskIDs(t *testing.T, dir string) []string {
	t.Helper()
	out, code := run(t, dir, "", "task", "list", "--json")
	var ts []struct{ ID string }
	if code != 0 || json.Unmarshal([]byte(out), &ts) != nil {
		t.Fatalf("task list: %d\n%s", code, out)
	}
	var ids []string
	for _, x := range ts {
		ids = append(ids, x.ID)
	}
	return ids
}

// AC-5: `bint task diff` chỉ cho thấy thay đổi của riêng tác vụ: không lẫn thay đổi chưa commit có sẵn.
func TestTaskDiffCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tác nhân giả viết bằng sh")
	}
	root := initIsolationRepo(t)
	// Thay đổi chưa commit có sẵn từ trước khi lập phiếu (kể cả đường dẫn bảo vệ).
	os.WriteFile(filepath.Join(root, "migrations", "001.sql"), []byte("-- dở dang\n"), 0o644)
	os.WriteFile(filepath.Join(root, "scratch.txt"), []byte("nháp\n"), 0o644)

	out, code := run(t, root, "", "run", "b-feat", "--agent-command", writeScript(t, featScript), "Thêm tính năng v2 cho app.txt")
	if code != 0 || !strings.Contains(out, "in_review") {
		t.Fatalf("run b-feat: %d\n%s", code, out)
	}
	id := taskIDs(t, root)[0]

	out, code = run(t, root, "", "task", "diff", id, "--name-only")
	if code != 0 || out != "app.txt\ndone.txt\n" {
		t.Fatalf("--name-only: %d %q", code, out)
	}
	out, code = run(t, root, "", "task", "diff", id, "--stat")
	if code != 0 || !strings.Contains(out, "app.txt") || !strings.Contains(out, "done.txt") || !strings.Contains(out, "2 files changed") ||
		strings.Contains(out, "migrations") || strings.Contains(out, "scratch") {
		t.Fatalf("--stat: %d\n%s", code, out)
	}
	out, code = run(t, root, "", "task", "diff", id)
	if code != 0 || !strings.Contains(out, "diff --git a/app.txt b/app.txt") || !strings.Contains(out, "+v2") ||
		!strings.Contains(out, "new file mode") || !strings.Contains(out, "+ok") ||
		strings.Contains(out, "migrations") || strings.Contains(out, "scratch") || strings.Contains(out, "dở dang") {
		t.Fatalf("diff đầy đủ: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "task", "diff", id, "--stat", "--name-only"); code != 2 || !strings.Contains(out, "loại trừ nhau") {
		t.Fatalf("hai cờ: %d\n%s", code, out)
	}
	if _, code := run(t, root, "", "task", "diff"); code == 0 {
		t.Fatal("thiếu ID phải lỗi")
	}
	if out, code := run(t, root, "", "task", "diff", "khongco"); code == 0 {
		t.Fatalf("tác vụ không có phải lỗi: %s", out)
	}
	// Chạy ở thư mục con: vẫn đúng tác vụ.
	if out, code := run(t, filepath.Join(root, "migrations"), "", "task", "diff", id, "--name-only"); code != 0 || out != "app.txt\ndone.txt\n" {
		t.Fatalf("từ thư mục con: %d %q", code, out)
	}

	// NT-WF-BASELINE: tệp người khác đổi vào cây sau khi tác vụ chạy bị loại khỏi diff mặc định (ghi chú nêu tên);
	// --all hiện lại.
	os.WriteFile(filepath.Join(root, "other.txt"), []byte("của người khác\n"), 0o644)
	if out, code := run(t, root, "", "task", "diff", id, "--name-only"); code != 0 || !strings.HasSuffix(out, "\napp.txt\ndone.txt\n") ||
		!strings.HasPrefix(out, "! đã loại 1 tệp không thuộc tác vụ: other.txt") {
		t.Fatalf("diff mặc định phải loại other.txt và ghi chú: %d %q", code, out)
	}
	if out, code := run(t, root, "", "task", "diff", id, "--name-only", "--all"); code != 0 || out != "app.txt\ndone.txt\nother.txt\n" {
		t.Fatalf("--all: %d %q", code, out)
	}
	os.Remove(filepath.Join(root, "other.txt"))

	// Phiếu cũ (không BaseTree/Touched): so base với cây hiện tại trên toàn cây, kèm ghi chú.
	out, _ = run(t, root, "", "task", "show", id, "--json")
	var shown struct{ Dir string }
	if json.Unmarshal([]byte(out), &shown) != nil || shown.Dir == "" {
		t.Fatalf("task show: %s", out)
	}
	sp := filepath.Join(shown.Dir, "state.json")
	b, err := os.ReadFile(sp)
	if err != nil {
		t.Fatal(err)
	}
	var st map[string]any
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatal(err)
	}
	if st["base_tree"] == nil || st["touched"] == nil {
		t.Fatalf("state.json phải có base_tree và touched: %s", b)
	}
	delete(st, "base_tree")
	delete(st, "touched")
	b, _ = json.Marshal(st)
	if err := os.WriteFile(sp, b, 0o600); err != nil {
		t.Fatal(err)
	}
	out, code = run(t, root, "", "task", "diff", id, "--name-only")
	if code != 0 || !strings.Contains(out, "phiếu lập trước khi Bint theo dõi") || !strings.Contains(out, "migrations/001.sql") ||
		!strings.Contains(out, "scratch.txt") || !strings.Contains(out, "app.txt") {
		t.Fatalf("phiếu cũ: %d\n%s", code, out)
	}
}

// AC-4: `bint run resume` gõ ở thư mục khác chạy đúng State.Worktree; thư mục không còn thì failed với thông báo rõ.
func TestRunResumeFromOtherDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tác nhân giả viết bằng sh")
	}
	root := initIsolationRepo(t)
	newBlocked := func(wt string) string {
		out, code := run(t, wt, "", "task", "new", "--workflow", "b-feat", "Thêm tính năng v2 cho app.txt", "--json")
		var c struct {
			ID string `json:"id"`
		}
		if code != 0 || json.Unmarshal([]byte(out), &c) != nil || c.ID == "" {
			t.Fatalf("task new: %d\n%s", code, out)
		}
		if out, code := run(t, wt, "", "task", "status", c.ID, "blocked", "--reason", "thử resume"); code != 0 {
			t.Fatalf("task status: %d\n%s", code, out)
		}
		return c.ID
	}

	wt := filepath.Join(t.TempDir(), "wt")
	testutil.Git(t, root, "worktree", "add", "-q", "-b", "wt-branch", wt)
	id := newBlocked(wt)
	mark := filepath.Join(t.TempDir(), "mark")
	t.Setenv("MARK", mark)
	out, code := run(t, root, "", "run", "resume", id, "--agent-command", writeScript(t, featScript))
	if code != 0 || !strings.Contains(out, "chạy trong") || !strings.Contains(out, "in_review") {
		t.Fatalf("resume ở thư mục khác: %d\n%s", code, out)
	}
	got, _ := os.ReadFile(mark)
	want, _ := filepath.EvalSymlinks(wt)
	have, _ := filepath.EvalSymlinks(strings.TrimSpace(string(got)))
	if have != want {
		t.Fatalf("agent chạy ở %q, mong %q", have, want)
	}
	if _, err := os.Stat(filepath.Join(root, "done.txt")); err == nil {
		t.Fatal("thư mục đang đứng bị chạm")
	}
	if _, err := os.Stat(filepath.Join(wt, "done.txt")); err != nil {
		t.Fatalf("worktree của tác vụ không có kết quả: %v", err)
	}
	if out, code := run(t, root, "", "task", "diff", id, "--name-only"); code != 0 || out != "app.txt\ndone.txt\n" {
		t.Fatalf("task diff từ thư mục khác: %d %q", code, out)
	}

	// Thư mục worktree biến mất.
	wt2 := filepath.Join(t.TempDir(), "wt2")
	testutil.Git(t, root, "worktree", "add", "-q", "-b", "wt-branch2", wt2)
	id2 := newBlocked(wt2)
	testutil.Git(t, root, "worktree", "remove", "--force", wt2)
	out, code = run(t, root, "", "run", "resume", id2, "--agent-command", writeScript(t, featScript))
	if code != 1 || !strings.Contains(out, "failed") || !strings.Contains(out, "thư mục làm việc của tác vụ không còn") || !strings.Contains(out, wt2) {
		t.Fatalf("worktree mất: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "task", "diff", id2); code == 0 || !strings.Contains(out, "không còn") {
		t.Fatalf("task diff khi worktree mất: %d\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(root, "done.txt")); err == nil {
		t.Fatal("không được lùi về thư mục đang đứng")
	}
}
