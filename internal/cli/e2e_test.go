package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"bint/internal/testutil"
)

var bintBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "bint-e2e-")
	if err != nil {
		panic(err)
	}
	bintBin = filepath.Join(dir, "bint")
	if runtime.GOOS == "windows" {
		bintBin += ".exe"
	}
	out, err := exec.Command("go", "build", "-o", bintBin, "bint/cmd/bint").CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "build: %v\n%s", err, out)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func run(t *testing.T, dir, stdin string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(bintBin, args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdin)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return out.String(), code
}

func TestEndToEnd(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})

	if out, code := run(t, root, "", "status"); code != 4 {
		t.Fatalf("status before init: code %d\n%s", code, out)
	}
	if out, code := run(t, t.TempDir(), "", "init", "--no-hooks"); code != 4 {
		t.Fatalf("init outside git: code %d\n%s", code, out)
	}
	if _, code := run(t, root, "", "init", "--bogus"); code != 2 {
		t.Fatalf("bad flag: code %d", code)
	}

	out, code := run(t, root, "", "init", "--harness", "claude", "--hooks")
	if code != 0 {
		t.Fatalf("init: code %d\n%s", code, out)
	}
	if !strings.Contains(out, "hook claude") || !strings.Contains(out, "go test ./...") {
		t.Fatalf("init output:\n%s", out)
	}
	// Hook trỏ tới chính tệp thực thi đã build (không có trong PATH).
	settings, _ := os.ReadFile(filepath.Join(root, ".claude", "settings.local.json"))
	if !strings.Contains(string(settings), filepath.ToSlash(bintBin)) {
		t.Fatalf("hook command:\n%s", settings)
	}

	if out, code := run(t, root, "", "init", "--hooks"); code != 0 {
		t.Fatalf("re-init: code %d\n%s", code, out)
	}

	// Mô phỏng Claude Code gọi hook qua stdin.
	hook := func(m map[string]any) {
		m["cwd"] = root
		m["session_id"] = "sess-e2e"
		b, _ := json.Marshal(m)
		if out, code := run(t, root, string(b), "hook", "claude"); code != 0 || out != "" {
			t.Fatalf("hook must be silent and exit 0: code %d out %q", code, out)
		}
	}
	hook(map[string]any{"hook_event_name": "SessionStart", "source": "startup", "model": "claude-e2e"})
	hook(map[string]any{"hook_event_name": "PostToolUse", "tool_name": "Write", "tool_use_id": "t1",
		"tool_input": map[string]any{"file_path": filepath.Join(root, "main.go")}})
	hook(map[string]any{"hook_event_name": "PostToolUse", "tool_name": "Bash", "tool_use_id": "t2",
		"tool_input": map[string]any{"command": "go test ./..."}})
	hook(map[string]any{"hook_event_name": "SessionEnd", "reason": "other"})
	// Đầu vào hỏng: vẫn thoát 0 và không in gì ra stdout của harness.
	if _, code := run(t, root, "{garbage", "hook", "claude"); code != 0 {
		t.Fatal("hook must never fail")
	}

	out, code = run(t, root, "", "runs", "list", "--json")
	if code != 0 {
		t.Fatalf("runs list: %d\n%s", code, out)
	}
	var runs []map[string]any
	if err := json.Unmarshal([]byte(out), &runs); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if len(runs) != 1 || runs[0]["status"] != "completed" || runs[0]["model"] != "claude-e2e" ||
		runs[0]["files_modified"] != float64(1) || runs[0]["tests_ok"] != float64(1) {
		t.Fatalf("runs = %v", runs)
	}
	// Không đo được thì là null, không phải 0.
	if v, ok := runs[0]["harness_session_id"]; !ok || v != "sess-e2e" {
		t.Fatalf("session id = %v", v)
	}

	id := runs[0]["run_id"].(string)
	out, code = run(t, root, "", "runs", "show", id[:10])
	if code != 0 || !strings.Contains(out, "main.go") || !strings.Contains(out, "test.completed") {
		t.Fatalf("runs show: %d\n%s", code, out)
	}

	out, code = run(t, root, "", "doctor")
	if code != 0 {
		t.Fatalf("doctor: %d\n%s", code, out)
	}
	if !strings.Contains(out, "lỗi hook") {
		t.Fatalf("doctor should report the garbage hook input:\n%s", out)
	}

	if out, code := run(t, root, "", "status"); code != 0 || !strings.Contains(out, "completed=1") {
		t.Fatalf("status: %d\n%s", code, out)
	}

	out, code = run(t, root, "", "bootstrap")
	if code != 3 || !strings.Contains(out, "P2") {
		t.Fatalf("planned command: %d\n%s", code, out)
	}
}
