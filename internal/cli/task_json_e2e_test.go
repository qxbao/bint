package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"bint/internal/testutil"
)

// runSplit chạy bint với stdout và stderr tách riêng.
func runSplit(t *testing.T, dir, stdin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(bintBin, args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdin)
	var o, e bytes.Buffer
	cmd.Stdout, cmd.Stderr = &o, &e
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return o.String(), e.String(), code
}

// Mọi lệnh ghi của task có --json: stdout là đúng một JSON object có task_id; `task done` là bí danh của `status done`.
func TestTaskWriteCommandsJSONAndDoneAlias(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n", "a.go": "package a\n"})
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	var created map[string]any
	if err := json.Unmarshal([]byte(exec2(t, root, "task", "new", "--json", "--criteria", "hoạt động", "việc thử --json")), &created); err != nil {
		t.Fatal(err)
	}
	id := created["id"].(string)

	obj := func(stdin string, args ...string) map[string]any {
		t.Helper()
		out, _, code := runSplit(t, root, stdin, args...)
		if code != 0 {
			t.Fatalf("%v: mã %d\n%s", args, code, out)
		}
		dec := json.NewDecoder(strings.NewReader(out))
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			t.Fatalf("%v: không phải JSON: %v\n%s", args, err, out)
		}
		if dec.More() {
			t.Fatalf("%v: nhiều hơn một JSON object:\n%s", args, out)
		}
		if m["task_id"] != id {
			t.Fatalf("%v: task_id = %v, muốn %s\n%s", args, m["task_id"], id, out)
		}
		return m
	}
	obj("", "task", "set", id, "--goal", "mục tiêu mới", "--json")
	obj("# Yêu cầu\n", "task", "write", id, "requirement", "-", "--json")
	obj("", "task", "criteria", id, "1", "met", "--evidence", "đã thử", "--json")
	obj("", "task", "error", id, "thử thất bại", "--step", "b-feat", "--json")
	obj(`{"summary":"ổn","findings":[]}`, "task", "review", id, "-", "--json")
	if m := obj("", "task", "status", id, "in_progress", "--json"); m["status"] != "in_progress" {
		t.Fatalf("%v", m)
	}
	if m := obj("", "task", "status", id, "in_review", "--json"); m["status"] != "in_review" {
		t.Fatalf("%v", m)
	}
	m := obj("", "task", "done", id, "--json", "-m", "xong", "--force")
	if m["status"] != "done" {
		t.Fatalf("task done: %v", m)
	}

	// Lỗi vẫn ra stderr với mã thoát như cũ, stdout rỗng.
	out, errOut, code := runSplit(t, root, "", "task", "done", "01ZZZZZZZZZZZZZZZZZZZZZZZZ", "--json")
	if code == 0 || out != "" || !strings.Contains(errOut, "lỗi:") {
		t.Fatalf("mã %d stdout=%q stderr=%q", code, out, errOut)
	}
}
