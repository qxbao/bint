package hookcore_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/config"
	"bint/internal/gitrepo"
	"bint/internal/harness/claude"
	"bint/internal/state"
	"bint/internal/testutil"
)

// goalDenyRepo dựng repository đã khởi tạo Bint và trả (root, thư mục tác vụ giả nằm ngoài repository).
func goalDenyRepo(t *testing.T) (root, dir string) {
	t.Helper()
	testutil.Isolate(t)
	ctx := context.Background()
	root = testutil.NewRepo(t, map[string]string{"app.txt": "v1\n"})
	repo, err := gitrepo.Discover(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := state.Resolve(repo, config.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.Ensure(repo); err != nil {
		t.Fatal(err)
	}
	dir = filepath.Join(paths.Runtime, "tasks", "01M7TESTGOALDENYDIR0000000")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return root, dir
}

func goalDenyDecide(t *testing.T, root, tool string, in map[string]any) (denied bool, err error) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"session_id": "s-goal", "hook_event_name": "PreToolUse", "cwd": root,
		"tool_name": tool, "tool_input": in, "tool_use_id": "tu-" + tool})
	res, err := claude.HandleHook(context.Background(), strings.NewReader(string(payload)), claude.HookEnv{BintVersion: "test"})
	return strings.Contains(res.Output, `"deny"`), err
}

// D2: đề bài b-goal in đường dẫn requirement.md trong thư mục tác vụ; rào phải chặn cả thư mục đó (Grep/Glob/ls/đường dẫn tuyệt đối
// của tệp khác như review.md) trừ requirement.md ("!" là ngoại lệ).
func TestGoalDenyReadBlocksTaskDir(t *testing.T) {
	root, dir := goalDenyRepo(t)
	req, plan, tasks := filepath.Join(dir, "requirement.md"), filepath.Join(dir, "plan.md"), filepath.Join(dir, "tasks.md")
	t.Setenv("BINT_DENY_READ", strings.Join([]string{plan, tasks, dir, "!" + req}, "\n"))
	for _, c := range []struct {
		name string
		tool string
		in   map[string]any
		deny bool
	}{
		{"Read requirement.md", "Read", map[string]any{"file_path": req}, false},
		{"Bash cat requirement.md", "Bash", map[string]any{"command": "cat " + req}, false},
		{"Read plan.md", "Read", map[string]any{"file_path": plan}, true},
		{"Grep trên thư mục tác vụ", "Grep", map[string]any{"pattern": "TSK", "path": dir}, true},
		{"Glob trong thư mục tác vụ", "Glob", map[string]any{"pattern": "*.md", "path": dir}, true},
		{"Bash ls thư mục tác vụ", "Bash", map[string]any{"command": "ls " + dir}, true},
		{"Read review.md cùng thư mục", "Read", map[string]any{"file_path": filepath.Join(dir, "review.md")}, true},
		{"Bash cat plan.md cạnh requirement.md", "Bash", map[string]any{"command": "cat " + req + " " + plan}, true},
		{"Read tệp trong repository", "Read", map[string]any{"file_path": filepath.Join(root, "app.txt")}, false},
	} {
		got, err := goalDenyDecide(t, root, c.tool, c.in)
		if err != nil {
			t.Errorf("%s: hook không được trả lỗi: %v", c.name, err)
		}
		if got != c.deny {
			t.Errorf("%s: deny=%v, muốn %v", c.name, got, c.deny)
		}
	}
}

// D4: cấu hình hỏng làm hook trả lỗi trước khi tới rào đọc; rào chỉ cần biến môi trường nên vẫn phải deny lời gọi nhắc tới đường dẫn
// bị cấm (fail closed hẹp: lệnh khác không bị ảnh hưởng).
func TestGoalDenyReadFailsClosedOnBadConfig(t *testing.T) {
	root, dir := goalDenyRepo(t)
	cfg := filepath.Join(root, ".bint", "config.yml")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte("khoa_la: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := filepath.Join(dir, "plan.md")
	t.Setenv("BINT_DENY_READ", strings.Join([]string{plan, dir, "!" + filepath.Join(dir, "requirement.md")}, "\n"))
	denied, err := goalDenyDecide(t, root, "Read", map[string]any{"file_path": plan})
	if err == nil {
		t.Fatal("cấu hình hỏng phải trả lỗi")
	}
	if !denied {
		t.Errorf("Read plan.md phải bị deny dù cấu hình hỏng")
	}
	if denied, _ := goalDenyDecide(t, root, "Read", map[string]any{"file_path": filepath.Join(root, "app.txt")}); denied {
		t.Errorf("lời gọi không nhắc đường dẫn cấm không được bị deny")
	}
}
