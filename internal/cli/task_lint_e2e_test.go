package cli_test

import (
	"encoding/json"
	"strings"
	"testing"

	"bint/internal/testutil"
)

type lintJSON struct {
	TaskID          string                            `json:"task_id"`
	ContractVersion int                               `json:"contract_version"`
	Errors          []struct{ Code, Artifact string } `json:"errors"`
	Warnings        []struct{ Code, Artifact string } `json:"warnings"`
	OpenQuestions   []struct{ Code, Detail string }   `json:"open_questions"`
}

// jsonPart cắt phần JSON khỏi đầu ra trộn stdout+stderr (lỗi thoát in thêm một dòng).
func jsonPart(out string) []byte {
	i, j := strings.Index(out, "{"), strings.LastIndex(out, "}")
	if i < 0 || j < i {
		return []byte(out)
	}
	return []byte(out[i : j+1])
}

func newLintRepo(t *testing.T) string {
	t.Helper()
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"app.txt": "v1\n"})
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	return root
}

func newTaskID(t *testing.T, root string, args ...string) string {
	t.Helper()
	out, code := run(t, root, "", append([]string{"task", "new", "--json"}, args...)...)
	var r struct{ ID string }
	if code != 0 || json.Unmarshal(jsonPart(out), &r) != nil || r.ID == "" {
		t.Fatalf("task new: %d\n%s", code, out)
	}
	return r.ID
}

func writeDoc(t *testing.T, root, id, name, body string) {
	t.Helper()
	if out, code := run(t, root, body, "task", "write", id, name, "-"); code != 0 {
		t.Fatalf("task write %s: %d\n%s", name, code, out)
	}
}

func TestTaskLint(t *testing.T) {
	root := newLintRepo(t)
	id := newTaskID(t, root, "--plan", "--criteria", "app.txt chứa v2", "Thêm tính năng v2 cho app.txt")

	// v1 thiếu artifact: mã 1, JSON có mã lỗi ổn định và mảng rỗng (không null).
	out, code := run(t, root, "", "task", "lint", id, "--json")
	var r lintJSON
	if code != 1 || json.Unmarshal(jsonPart(out), &r) != nil || r.ContractVersion != 1 || len(r.Errors) != 3 || r.Errors[0].Code != "artifact_missing" {
		t.Fatalf("lint v1 lỗi: %d\n%s", code, out)
	}
	if !strings.Contains(out, `"warnings": []`) || !strings.Contains(out, `"open_questions": []`) {
		t.Fatalf("JSON phải chứa mảng rỗng:\n%s", out)
	}

	writeDoc(t, root, id, "requirement", "# Yêu cầu\n## Tiêu chí chấp nhận\n- [ ] AC-1: app.txt chứa v2\n## Câu hỏi mở\n- [ ] Q-1: giữ v1 không?\n")
	writeDoc(t, root, id, "plan", "# Kế hoạch\nSửa app.txt\n")
	writeDoc(t, root, id, "tasks", "- [ ] TSK-1 [AC-1]: sửa app.txt — xong khi: chứa v2\n")
	// Câu hỏi mở: không phải lỗi cấu trúc nhưng vẫn thoát 1.
	out, code = run(t, root, "", "task", "lint", id, "--json")
	r = lintJSON{}
	if code != 1 || json.Unmarshal(jsonPart(out), &r) != nil || len(r.Errors) != 0 || len(r.OpenQuestions) != 1 {
		t.Fatalf("câu hỏi mở: %d\n%s", code, out)
	}
	writeDoc(t, root, id, "requirement", "# Yêu cầu\n## Tiêu chí chấp nhận\n- [ ] AC-1: app.txt chứa v2\n## Câu hỏi mở\n- [x] Q-1: giữ v1 không? — Trả lời: không\n")
	if out, code := run(t, root, "", "task", "lint", id); code != 0 || !strings.Contains(out, "✓") {
		t.Fatalf("hợp lệ: %d\n%s", code, out)
	}
}

func TestTaskLintLegacyIsAdvisoryUnlessStrict(t *testing.T) {
	root := newLintRepo(t)
	id := newTaskID(t, root, "--no-plan", "Đổi app.txt")
	out, code := run(t, root, "", "task", "lint", id, "--json")
	var r lintJSON
	if code != 0 || json.Unmarshal(jsonPart(out), &r) != nil || r.ContractVersion != 0 || len(r.Errors) != 0 || len(r.Warnings) != 3 {
		t.Fatalf("v0 mặc định chỉ cảnh báo: %d\n%s", code, out)
	}
	out, code = run(t, root, "", "task", "lint", id, "--strict", "--json")
	r = lintJSON{}
	if code != 1 || json.Unmarshal(jsonPart(out), &r) != nil || len(r.Errors) != 3 {
		t.Fatalf("--strict trên v0: %d\n%s", code, out)
	}
	// Không ghi state.
	show, _ := run(t, root, "", "task", "show", id, "--json")
	if strings.Contains(show, "planning_contract_version") {
		t.Fatalf("lint không được đổi state:\n%s", show)
	}
}

// bint task set --criteria/--clear-criteria trên phiếu đã có artifact b-req tự đồng bộ AC/TSK:
// lint --strict qua ngay, không cần task write.
func TestTaskSetSyncsCriteriaThenLintPasses(t *testing.T) {
	root := newLintRepo(t)
	id := newTaskID(t, root, "--plan", "--criteria", "app.txt chứa v2", "Thêm tính năng v2 cho app.txt")
	writeDoc(t, root, id, "requirement", "# Yêu cầu\n## Tiêu chí chấp nhận\n- [ ] AC-1: app.txt chứa v2\n## Câu hỏi mở\n")
	writeDoc(t, root, id, "plan", "# Kế hoạch\nSửa app.txt\n")
	writeDoc(t, root, id, "tasks", "- [ ] TSK-1 [AC-1]: sửa app.txt — xong khi: chứa v2\n")
	if out, code := run(t, root, "", "task", "lint", id, "--strict"); code != 0 {
		t.Fatalf("lint ban đầu: %d\n%s", code, out)
	}

	out, code := run(t, root, "", "task", "set", id, "--criteria", "app.txt có chú thích đầu tệp")
	if code != 0 || !strings.Contains(out, "đồng bộ tiêu chí") {
		t.Fatalf("task set phải in ghi chú đồng bộ: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "task", "lint", id, "--strict"); code != 0 {
		t.Fatalf("lint sau --criteria: %d\n%s", code, out)
	}

	out, code = run(t, root, "", "task", "set", id, "--clear-criteria", "--criteria", "một", "--criteria", "hai", "--criteria", "ba")
	if code != 0 {
		t.Fatalf("task set --clear-criteria: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "task", "lint", id, "--strict"); code != 0 {
		t.Fatalf("lint sau --clear-criteria: %d\n%s", code, out)
	}
}
