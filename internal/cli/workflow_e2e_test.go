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

// agentScript là tác nhân giả: đọc bước từ đề bài và gọi CLI bint như một agent thật.
const agentScript = `set -e
P="$(cat "$BINT_EVAL_PROMPT_FILE")"
case "$P" in
*'step="b-req"'*)
  printf '# Yêu cầu\n## Tiêu chí chấp nhận\n- [ ] AC-1: app.txt chứa v2\n## Câu hỏi mở\n' | "$BINT" task write "$BINT_TASK_ID" requirement -
  printf '# Kế hoạch\nSửa app.txt\n' | "$BINT" task write "$BINT_TASK_ID" plan -
  printf -- '- [ ] TSK-1 [AC-1]: sửa app.txt — xong khi: chứa v2\n' | "$BINT" task write "$BINT_TASK_ID" tasks -
  ;;
*'step="b-feat"'*)
  echo v2 > app.txt
  echo ok > done.txt
  if "$BINT" task approve "$BINT_TASK_ID" >/dev/null 2>&1; then echo approved > "$MARK"; else echo refused > "$MARK"; fi
  "$BINT" task criteria "$BINT_TASK_ID" 1 met --evidence "app.txt chứa v2"
  ;;
*'step="b-review"'*)
  "$BINT" task review "$BINT_TASK_ID" - <<'EOF'
{"summary":"Thay đổi nhỏ, đúng yêu cầu","findings":[{"kind":"risk","severity":"medium","title":"Tệp done.txt là cờ tạm","path":"done.txt","lines":"1","evidence":"done.txt chỉ dùng để kiểm chứng","body":"Không commit tệp cờ tạm vào repository."}]}
EOF
  ;;
esac
echo '{"type":"result","total_cost_usd":0.01,"num_turns":2,"result":"xong"}'
`

func TestWorkflowEndToEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tác nhân giả viết bằng sh")
	}
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"app.txt": "v1\n", "migrations/001.sql": "--\n"})
	testutil.Git(t, root, "config", "user.name", "Rev")
	testutil.Git(t, root, "config", "user.email", "rev@example.com")
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 || !strings.Contains(out, "skill b-do") {
		t.Fatalf("init: %d\n%s", code, out)
	}
	os.WriteFile(filepath.Join(root, ".bint", "local.yml"), []byte("eval:\n  test_command: \"test -f done.txt\"\n"), 0o644)

	// Lớp phủ: tạo mẫu, bật một khối, đồng bộ.
	if out, code := run(t, root, "", "skill", "overlay", "b-review"); code != 0 {
		t.Fatalf("overlay: %s", out)
	}
	ov := filepath.Join(root, ".bint", "skills", "overlays", "b-review.md")
	tpl, _ := os.ReadFile(ov)
	if !strings.Contains(string(tpl), "    <!-- bint:overlay review-checklist append -->") {
		t.Fatalf("mẫu lớp phủ:\n%s", tpl)
	}
	os.WriteFile(ov, append(tpl, []byte("\n<!-- bint:overlay review-checklist -->\n- Kiểm tra phân quyền theo tenant.\n<!-- bint:overlay-end -->\n")...), 0o644)
	if out, code := run(t, root, "", "sync"); code != 0 || !strings.Contains(out, "~ skill b-review") {
		t.Fatalf("sync: %d\n%s", code, out)
	}
	pub, _ := os.ReadFile(filepath.Join(root, ".claude", "skills", "b-review", "SKILL.md"))
	if !strings.Contains(string(pub), "Kiểm tra phân quyền theo tenant.") {
		t.Fatal("lớp phủ chưa vào skill đã xuất bản")
	}
	if out, _ := run(t, root, "", "skill", "list"); !strings.Contains(out, "b-review") || !strings.Contains(out, "1 khối") {
		t.Fatalf("skill list:\n%s", out)
	}
	// Sửa tay skill đã xuất bản → xung đột.
	os.WriteFile(filepath.Join(root, ".claude", "skills", "b-do", "SKILL.md"), []byte("sửa tay\n"), 0o644)
	if out, code := run(t, root, "", "sync"); code != 1 || !strings.Contains(out, "✗ skill b-do") {
		t.Fatalf("sync xung đột: %d\n%s", code, out)
	}
	run(t, root, "", "sync", "--force")

	// b-do qua tác nhân giả.
	script := filepath.Join(t.TempDir(), "agent.sh")
	mark := filepath.Join(t.TempDir(), "mark")
	os.WriteFile(script, []byte(agentScript), 0o755)
	t.Setenv("BINT", bintBin)
	t.Setenv("MARK", mark)
	out, code := run(t, root, "", "run", "b-do", "--agent-command", "sh "+script, "--plan",
		"--criteria", "app.txt chứa v2", "Thêm tính năng v2 cho app.txt")
	if code != 0 || !strings.Contains(out, ": done") {
		t.Fatalf("run b-do: %d\n%s", code, out)
	}
	if m, _ := os.ReadFile(mark); strings.TrimSpace(string(m)) != "refused" {
		t.Fatalf("agent không được tự duyệt: %q", m)
	}
	out, _ = run(t, root, "", "task", "list", "--all", "--json")
	var tasks []struct{ ID, Status string }
	if json.Unmarshal([]byte(out), &tasks) != nil || len(tasks) != 1 || tasks[0].Status != "done" {
		t.Fatalf("task list: %s", out)
	}
	id := tasks[0].ID
	out, code = run(t, root, "", "run", "status", id)
	for _, want := range []string{"b-do", "b-req", "b-feat", "b-review", "passed"} {
		if code != 0 || !strings.Contains(out, want) {
			t.Fatalf("run status thiếu %q:\n%s", want, out)
		}
	}
	out, _ = run(t, root, "", "task", "show", id)
	for _, want := range []string{"✓ requirement", "✓ 1. app.txt chứa v2", "Review vòng 1", id + ".1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("task show thiếu %q:\n%s", want, out)
		}
	}
	if out, _ := run(t, root, "", "runs", "list"); !strings.Contains(out, "success") {
		t.Fatalf("runs list phải có nhãn success (kiểm chứng đã xác nhận qua):\n%s", out)
	}

	// Phát hiện review → tri thức: agent → candidate; người → verified.
	t.Setenv("BINT_AGENT", "1")
	out, code = run(t, root, "", "knowledge", "add", "--from-review", id[:12]+".1", "--json")
	var added struct {
		Saved      bool
		Status, ID string
	}
	if code != 0 || json.Unmarshal([]byte(out), &added) != nil || !added.Saved || added.Status != "candidate" {
		t.Fatalf("from-review (agent): %d\n%s", code, out)
	}
	run(t, root, "", "knowledge", "rm", added.ID)
	asHuman(t)
	out, code = run(t, root, "", "knowledge", "add", "--from-review", id+".1", "--type", "lesson", "--json")
	if code != 0 || json.Unmarshal([]byte(out), &added) != nil || added.Status != "verified" {
		t.Fatalf("from-review (người): %d\n%s", code, out)
	}
	if out, _ := run(t, root, "", "knowledge", "show", added.ID); !strings.Contains(out, "Tệp done.txt là cờ tạm") || !strings.Contains(out, "done.txt:1") {
		t.Fatalf("mục tri thức:\n%s", out)
	}
	if _, code := run(t, root, "", "knowledge", "add", "--from-review", id+".9"); code == 0 {
		t.Fatal("phát hiện không tồn tại phải lỗi")
	}
}

func TestWorkflowProtectedPathsAndTaskCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tác nhân giả viết bằng sh")
	}
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"app.txt": "v1\n", "migrations/001.sql": "--\n"})
	run(t, root, "", "init", "--no-hooks")

	// Tác vụ rủi ro cao thiếu cách kiểm chứng: không có terminal → blocked kèm câu hỏi.
	script := filepath.Join(t.TempDir(), "agent.sh")
	os.WriteFile(script, []byte("echo 'ALTER;' > migrations/002.sql\necho ok > done.txt\n"), 0o755)
	out, code := run(t, root, "", "run", "b-do", "--no-plan", "--no-review", "--agent-command", "sh "+script, "Thêm bảng mới trong migrations/")
	if code != 1 || !strings.Contains(out, "blocked") || !strings.Contains(out, "kiểm chứng thay đổi bằng lệnh nào") {
		t.Fatalf("phải hỏi lệnh kiểm chứng: %d\n%s", code, out)
	}
	out, _ = run(t, root, "", "task", "list", "--json")
	var tasks []struct{ ID string }
	json.Unmarshal([]byte(out), &tasks)
	id := tasks[0].ID

	// Agent đề xuất lệnh kiểm chứng → chưa được chạy cho tới khi người xác nhận.
	t.Setenv("BINT_AGENT", "1")
	if out, code := run(t, root, "", "task", "set", id, "--verify-command", "test -f done.txt"); code != 0 || !strings.Contains(out, "chưa xác nhận") {
		t.Fatalf("set: %d\n%s", code, out)
	}
	if out, _ := run(t, root, "", "task", "verify", id); !strings.Contains(out, "Bỏ qua") {
		t.Fatalf("lệnh chưa xác nhận không được chạy:\n%s", out)
	}
	if _, code := run(t, root, "", "task", "set", id, "--autonomy", "auto"); code == 0 {
		t.Fatal("agent không được đặt autonomy auto")
	}
	if out, code := run(t, root, "", "task", "approve", id); code == 0 || !strings.Contains(out, "chỉ dành cho người") {
		t.Fatalf("agent approve: %d\n%s", code, out)
	}
	asHuman(t)
	if out, code := run(t, root, "", "task", "approve", id); code != 0 || !strings.Contains(out, "Xác nhận lệnh kiểm chứng") {
		t.Fatalf("approve: %d\n%s", code, out)
	}

	// Tiếp tục: b-feat sửa migrations/ → chờ người duyệt (không có terminal).
	out, code = run(t, root, "", "run", "resume", id, "--no-review", "--agent-command", "sh "+script)
	if code != 1 || !strings.Contains(out, "migrations/002.sql") {
		t.Fatalf("phải chờ duyệt đường dẫn bảo vệ: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "run", "resume", id, "--agent-command", "sh "+script); code == 0 || !strings.Contains(out, "chờ người duyệt") {
		t.Fatalf("resume khi còn chờ duyệt phải bị từ chối: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "task", "status", id, "done"); code == 0 || !strings.Contains(out, "chưa được duyệt") {
		t.Fatalf("done phải bị chặn: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "task", "approve", id); code != 0 || !strings.Contains(out, "Duyệt thay đổi: migrations/002.sql") {
		t.Fatalf("approve paths: %d\n%s", code, out)
	}
	out, code = run(t, root, "", "run", "resume", id, "--no-review", "--agent-command", "sh "+script)
	if code != 0 || !strings.Contains(out, ": done") || strings.Contains(out, "b-feat  ") {
		t.Fatalf("resume sau khi duyệt (không chạy lại b-feat): %d\n%s", code, out)
	}
	if out, _ := run(t, root, "", "task", "transitions"); !strings.Contains(out, "interrupted") {
		t.Fatalf("transitions:\n%s", out)
	}

	// Hook: sửa đường dẫn bảo vệ trong phiên → yêu cầu harness hỏi người dùng.
	hookIn := `{"session_id":"S1","cwd":"` + root + `","hook_event_name":"PreToolUse","tool_name":"Edit","tool_input":{"file_path":"migrations/001.sql"},"tool_use_id":"u1"}`
	if out, _ := run(t, root, hookIn, "hook", "claude"); !strings.Contains(out, `"permissionDecision":"ask"`) {
		t.Fatalf("hook phải hỏi:\n%s", out)
	}
	t.Setenv("BINT_APPROVED_PATHS", "migrations/001.sql")
	if out, _ := run(t, root, hookIn, "hook", "claude"); strings.Contains(out, "permissionDecision") {
		t.Fatalf("đường dẫn đã duyệt không hỏi lại:\n%s", out)
	}
	readIn := strings.Replace(hookIn, `"Edit"`, `"Read"`, 1)
	t.Setenv("BINT_APPROVED_PATHS", "")
	if out, _ := run(t, root, readIn, "hook", "claude"); strings.Contains(out, "permissionDecision") {
		t.Fatalf("đọc tệp không cần hỏi:\n%s", out)
	}
}

// R3: task chưa qua Runner/checkScope: done bị chặn vì đường dẫn bảo vệ -> approve phải
// tính lại diff và duyệt được -> done thành công (không --force, không gán tay ProtectedApproval).
func TestTaskApproveRecomputesDiffWithoutRunner(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"app.txt": "v1\n", "migrations/001.sql": "--\n"})
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	out, code := run(t, root, "", "task", "new", "--criteria", "migration đã sửa", "--no-plan", "Sửa migrations/001.sql")
	if code != 0 {
		t.Fatalf("task new: %d\n%s", code, out)
	}
	out, _ = run(t, root, "", "task", "list", "--json")
	var tasks []struct{ ID string }
	if json.Unmarshal([]byte(out), &tasks) != nil || len(tasks) != 1 {
		t.Fatalf("task list: %s", out)
	}
	id := tasks[0].ID
	if out, code := run(t, root, "", "task", "status", id, "in_progress"); code != 0 {
		t.Fatalf("in_progress: %d\n%s", code, out)
	}
	os.WriteFile(filepath.Join(root, "migrations", "001.sql"), []byte("ALTER;\n"), 0o644)
	if out, code := run(t, root, "", "task", "criteria", id, "1", "met", "--evidence", "đã sửa"); code != 0 {
		t.Fatalf("criteria: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "task", "status", id, "done"); code == 0 || !strings.Contains(out, "chưa được duyệt") {
		t.Fatalf("done phải bị chặn: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "task", "approve", id); code != 0 || !strings.Contains(out, "Duyệt thay đổi: migrations/001.sql") {
		t.Fatalf("approve phải duyệt migrations/001.sql: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "task", "status", id, "done"); code != 0 {
		t.Fatalf("done sau approve: %d\n%s", code, out)
	}
}
