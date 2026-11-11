package cli_test

import (
	"strings"
	"testing"

	"bint/internal/testutil"
)

// Ràng buộc cờ của `task`/`run` đã chuyển xuống Validate() của dịch vụ vẫn cho mã thoát 2 (lỗi dùng sai).
func TestTaskAndRunUsageErrorsExitCode2(t *testing.T) {
	root := newLintRepo(t)
	id := newTaskID(t, root, "--no-plan", "Đổi app.txt")
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"task new --plan --no-plan", []string{"task", "new", "--plan", "--no-plan", "Đổi app.txt"}, "loại trừ nhau"},
		{"task new thiếu mô tả", []string{"task", "new"}, "thiếu mô tả"},
		{"task new --depth với --no-plan", []string{"task", "new", "--depth", "deep", "--no-plan", "Đổi app.txt"}, "--no-plan"},
		{"task new --depth sai", []string{"task", "new", "--depth", "sâu", "Đổi app.txt"}, "--depth"},
		{"task criteria số sai", []string{"task", "criteria", id, "x", "met", "-e", "ok"}, "số tiêu chí"},
		{"task criteria kết quả sai", []string{"task", "criteria", id, "1", "maybe", "-e", "ok"}, "met hoặc unmet"},
		{"run thiếu workflow", []string{"run"}, "thiếu workflow"},
		{"run --plan --no-plan", []string{"run", "b-do", "--plan", "--no-plan", "x"}, "loại trừ nhau"},
		{"run --agent-command với --harness", []string{"run", "b-do", "--agent-command", "true", "--harness", "claude", "--task", id}, "--agent-command"},
		{"run --agent-command với --role", []string{"run", "b-do", "--agent-command", "true", "--role", "implementer=codex", "--task", id}, "--agent-command"},
		{"run --harness lạ", []string{"run", "b-do", "--harness", "bogus", "--task", id}, "harness không hỗ trợ"},
		{"run --role sai cú pháp", []string{"run", "b-do", "--role", "implementer", "--task", id}, "role=harness"},
		{"run --depth với b-feat", []string{"run", "b-feat", "--depth", "deep", "--task", id}, "b-do hoặc b-req"},
		{"run resume --agent-command với --harness", []string{"run", "resume", id, "--agent-command", "true", "--harness", "claude"}, "--agent-command"},
	}
	for _, c := range cases {
		out, code := run(t, root, "", c.args...)
		if code != 2 || !strings.Contains(out, c.want) {
			t.Errorf("%s: mã %d, muốn 2 và %q\n%s", c.name, code, c.want, out)
		}
	}
	// Ca ngược: lệnh hợp lệ không bị coi là sai cách dùng.
	if out, code := run(t, root, "", "task", "criteria", id, "1", "met", "-e", "đã kiểm"); code == 2 {
		t.Errorf("task criteria hợp lệ không được mã 2: %d\n%s", code, out)
	}
}

// Thao tác chỉ dành cho người trong phiên agent: lỗi bọc ErrAgentForbidden (mã thoát 3) từ tầng dịch vụ.
func TestTaskHumanOnlyRefusedInAgentSessionFromService(t *testing.T) {
	root := newLintRepo(t)
	id := newTaskID(t, root, "--no-plan", "Đổi app.txt")
	t.Setenv("CLAUDECODE", "1")
	for _, args := range [][]string{
		{"task", "approve", id},
		{"task", "status", id, "done", "--force"},
		{"task", "set", id, "--autonomy", "auto"},
		{"task", "new", "--autonomy", "auto", "Đổi app.txt"},
	} {
		out, code := run(t, root, "", args...)
		if code == 0 || !strings.Contains(out, "chỉ dành cho người") {
			t.Errorf("%v: mã %d\n%s", args, code, out)
		}
	}
}

// Lỗi dùng sai (mã 2) thắng lỗi "chưa khởi tạo" (mã 4) như trước khi chuyển xuống dịch vụ: Options được kiểm tra
// trước khi mở workspace.
func TestUsageErrorBeatsNotInitialized(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"app.txt": "v1\n"}) // chưa init
	for _, args := range [][]string{
		{"run", "b-do", "--plan", "--no-plan", "x"},
		{"run", "b-do", "--agent-command", "true", "--harness", "claude", "x"},
		{"run", "resume", "abc", "--agent-command", "true", "--role", "implementer=codex"},
		{"task", "new", "--plan", "--no-plan", "x"},
		{"task", "new"},
	} {
		if out, code := run(t, root, "", args...); code != 2 {
			t.Errorf("%v: mã %d, muốn 2\n%s", args, code, out)
		}
	}
	if _, code := run(t, root, "", "task", "new", "x"); code != 4 {
		t.Errorf("lệnh hợp lệ trên repo chưa init phải mã 4: %d", code)
	}
}
