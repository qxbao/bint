package hookcore_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"bint/internal/config"
	"bint/internal/gitrepo"
	"bint/internal/harness/claude"
	"bint/internal/state"
	"bint/internal/testutil"
)

// Kiểm tra mục tiêu (phần hook của #8) của phiếu 01M7PD9T2ASG8XXMEM7SJ2G6C1: khi tiến trình agent có BINT_DENY_READ (Runner chỉ đặt
// cho bước b-goal: đường dẫn tuyệt đối plan.md và tasks.md, mỗi dòng một đường), hook PreToolUse trả deny cho công cụ đọc trỏ tới các
// tệp đó và lệnh shell nhắc tới chúng; requirement.md và tệp khác vẫn đọc được. Không có biến thì không chặn. Phần Env: TestGoalFixRound.
func TestGoalDenyReadHook(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := testutil.NewRepo(t, map[string]string{"app.txt": "v1\n"})
	// Repository đã khởi tạo Bint (như sau `bint init`): hook xử lý đầy đủ thay vì bỏ qua.
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
	if !paths.Initialized() {
		t.Fatal("repository test phải ở trạng thái đã khởi tạo Bint")
	}
	// Artifact của phiếu nằm ngoài repository, trong thư mục trạng thái (như runtime/tasks/<id>/).
	dir := filepath.Join(paths.Runtime, "tasks", "01M7TESTGOALDENYREAD000000")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	plan, tasks, req := filepath.Join(dir, "plan.md"), filepath.Join(dir, "tasks.md"), filepath.Join(dir, "requirement.md")
	for p, body := range map[string]string{plan: "# Kế hoạch\nPLAN-ONLY\n", tasks: "- [ ] TSK-1: PLAN-ONLY\n", req: "# Yêu cầu\n"} {
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	n := 0
	decide := func(t *testing.T, tool string, in map[string]any) (decision, raw string) {
		t.Helper()
		n++
		payload, _ := json.Marshal(map[string]any{"session_id": "s-goal", "hook_event_name": "PreToolUse", "cwd": root,
			"tool_name": tool, "tool_input": in, "tool_use_id": "tu-" + strconv.Itoa(n)})
		res, err := claude.HandleHook(ctx, strings.NewReader(string(payload)), claude.HookEnv{BintVersion: "test"})
		if err != nil {
			t.Errorf("%s: hook không được trả lỗi: %v", tool, err)
		}
		if res.Output == "" {
			return "", ""
		}
		var m map[string]map[string]any
		if err := json.Unmarshal([]byte(res.Output), &m); err != nil {
			t.Fatalf("%s: đầu ra hook không phải JSON: %q", tool, res.Output)
		}
		d, _ := m["hookSpecificOutput"]["permissionDecision"].(string)
		return d, res.Output
	}

	cases := []struct {
		name string
		tool string
		in   map[string]any
		deny bool
	}{
		{"Read plan.md", "Read", map[string]any{"file_path": plan}, true},
		{"Read tasks.md", "Read", map[string]any{"file_path": tasks}, true},
		{"Grep vào tasks.md", "Grep", map[string]any{"pattern": "TSK", "path": tasks}, true},
		{"Bash cat plan.md", "Bash", map[string]any{"command": "cat " + plan}, true},
		{"Read requirement.md", "Read", map[string]any{"file_path": req}, false},
		{"Bash cat requirement.md", "Bash", map[string]any{"command": "cat " + req}, false},
		{"Read tệp trong repository", "Read", map[string]any{"file_path": filepath.Join(root, "app.txt")}, false},
	}

	t.Run("có BINT_DENY_READ (bước b-goal)", func(t *testing.T) {
		t.Setenv("BINT_DENY_READ", plan+"\n"+tasks)
		for _, c := range cases {
			d, raw := decide(t, c.tool, c.in)
			if got := d == "deny"; got != c.deny {
				t.Errorf("%s: deny=%v, muốn %v (đầu ra %q)", c.name, got, c.deny, raw)
			}
		}
	})

	t.Run("không có BINT_DENY_READ (bước khác)", func(t *testing.T) {
		t.Setenv("BINT_DENY_READ", "")
		for _, c := range cases {
			if d, raw := decide(t, c.tool, c.in); d == "deny" {
				t.Errorf("%s: không có BINT_DENY_READ thì không được deny (đầu ra %q)", c.name, raw)
			}
		}
	})
}
