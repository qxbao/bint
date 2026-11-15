package cli_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// Lập phiếu b-debug bằng CLI: nhận --depth deep, --plan, --no-plan; phiếu mang workflow b-debug.
func TestTaskNewDebugWorkflow(t *testing.T) {
	root := newLintRepo(t)
	type created struct {
		ID             string
		Classification struct {
			Plan  bool
			Depth string
		}
	}
	for name, c := range map[string]struct {
		args      []string
		plan      bool
		wantDepth string
	}{
		"deep":    {[]string{"--depth", "deep"}, true, "deep"},
		"plan":    {[]string{"--plan"}, true, ""},
		"no-plan": {[]string{"--no-plan"}, false, ""},
	} {
		args := append([]string{"task", "new", "--json", "--workflow", "b-debug"}, c.args...)
		out, code := run(t, root, "", append(args, "Sửa lỗi app.txt ghi sai")...)
		var got created
		if code != 0 || json.Unmarshal([]byte(out), &got) != nil || got.Classification.Plan != c.plan {
			t.Fatalf("%s: task new: %d\n%s", name, code, out)
		}
		if c.wantDepth != "" && got.Classification.Depth != c.wantDepth {
			t.Fatalf("%s: depth = %q\n%s", name, got.Classification.Depth, out)
		}
		out, code = run(t, root, "", "task", "show", got.ID, "--json")
		if code != 0 || !strings.Contains(out, `"b-debug"`) {
			t.Fatalf("%s: task show: %d\n%s", name, code, out)
		}
	}
	// Workflow không có bước plan vẫn từ chối --depth; b-debug có bước plan nên không bị từ chối ở trên.
	if _, code := run(t, root, "", "task", "new", "--workflow", "b-feat", "--depth", "deep", "Đổi app.txt"); code != 2 {
		t.Fatalf("b-feat --depth: mã %d, muốn 2", code)
	}
}

// Phiếu b-debug chỉ chạy bằng flow của nó: b-do/b-feat/b-review --task bị từ chối (trước khi gọi agent); --step của b-debug được
// nhận (lỗi là bước kế tiếp sai, không phải lỗi dùng cờ).
func TestRunDebugTaskGuards(t *testing.T) {
	root := newLintRepo(t)
	out, code := run(t, root, "", "task", "new", "--json", "--workflow", "b-debug", "--no-plan", "Sửa lỗi app.txt ghi sai")
	var created struct{ ID string }
	if code != 0 || json.Unmarshal([]byte(out), &created) != nil || created.ID == "" {
		t.Fatalf("task new: %d\n%s", code, out)
	}
	for _, wf := range []string{"b-do", "b-feat", "b-review"} {
		out, code := run(t, root, "", "run", wf, "--task", created.ID)
		if code == 0 || !strings.Contains(out, "thuộc flow b-debug") {
			t.Errorf("run %s --task trên phiếu b-debug: mã %d\n%s", wf, code, out)
		}
	}
	out, code = run(t, root, "", "run", "b-debug", "--task", created.ID, "--step", "b-fix")
	if code == 0 || code == 2 || !strings.Contains(out, "bước kế tiếp") {
		t.Errorf("run b-debug --step b-fix (bước kế tiếp là b-repro): mã %d\n%s", code, out)
	}
	if _, code := run(t, root, "", "run", "b-do", "--task", created.ID, "--step", "b-repro"); code != 2 {
		t.Errorf("--step với workflow gốc b-do: mã %d, muốn 2 (usage)", code)
	}
}
