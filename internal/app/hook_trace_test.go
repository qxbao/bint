package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// BINT_HOOK_TRACE (NT-ISO-LEAK): cặp tệp .in/.out cho mỗi lần `bint hook` chạy; tắt thì không ghi gì; lỗi ghi vết không đổi
// đầu ra của hook.

func traceRun(t *testing.T, root, input string) (stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	RunHook(context.Background(), "claude", "", strings.NewReader(input), &out, &errb, "test")
	return out.String(), errb.String()
}

func traceFiles(t *testing.T, ws *Workspace) []string {
	t.Helper()
	ents, _ := os.ReadDir(filepath.Join(ws.Paths.Runtime, "hook-trace"))
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	return names
}

func traceInput(root string) string {
	return hookInput("sess-trace", "PostToolUse", root, map[string]any{"tool_name": "Read", "tool_input": map[string]any{"file_path": filepath.Join(root, "src/a.go")}, "tool_use_id": "tu-1"})
}

func TestHookTraceWritesPairWhenEnabled(t *testing.T) {
	root, ws := subagentRepo(t)
	t.Setenv(HookTraceEnv, "1")
	in := traceInput(root)
	traceRun(t, root, in)
	names := traceFiles(t, ws)
	if len(names) != 2 || !strings.HasSuffix(names[0], "-claude-PostToolUse.in") || !strings.HasSuffix(names[1], "-claude-PostToolUse.out") ||
		strings.TrimSuffix(names[0], ".in") != strings.TrimSuffix(names[1], ".out") {
		t.Fatalf("muốn một cặp .in/.out, có %v", names)
	}
	dir := filepath.Join(ws.Paths.Runtime, "hook-trace")
	if b, _ := os.ReadFile(filepath.Join(dir, names[0])); string(b) != in {
		t.Fatalf(".in không khớp stdin: %q", b)
	}
	var out struct {
		Output     string `json:"output"`
		DurationMS *int64 `json:"duration_ms"`
		Skipped    string `json:"skipped"`
	}
	b, _ := os.ReadFile(filepath.Join(dir, names[1]))
	if err := json.Unmarshal(b, &out); err != nil || out.DurationMS == nil || out.Skipped == "" {
		t.Fatalf(".out: %s %v", b, err)
	}
	if fi, err := os.Stat(filepath.Join(dir, names[0])); err != nil || (runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600) {
		t.Fatalf("quyền .in: %v %v", fi, err)
	}
}

func TestHookTraceOffWritesNothing(t *testing.T) {
	root, ws := subagentRepo(t)
	for _, v := range []string{"", "0", "true"} {
		t.Setenv(HookTraceEnv, v)
		traceRun(t, root, traceInput(root))
	}
	if _, err := os.Stat(filepath.Join(ws.Paths.Runtime, "hook-trace")); !os.IsNotExist(err) {
		t.Fatalf("tắt thì không được tạo hook-trace: %v", err)
	}
}

// Lỗi ghi vết (hook-trace là tệp thường; thư mục chỉ đọc) ⇒ stdout/stderr giống hệt khi tắt, không có hook-errors.log.
func TestHookTraceWriteFailureDoesNotChangeOutput(t *testing.T) {
	root, ws := subagentRepo(t)
	// Đầu ra tham chiếu: sự kiện có chèn ngữ cảnh/đầu ra khác rỗng cũng được, miễn so cùng đầu vào.
	input := hookInput("sess-trace", "PreToolUse", root, map[string]any{"tool_name": "Read", "tool_input": map[string]any{"file_path": filepath.Join(root, "src/a.go")}, "tool_use_id": "tu-2"})
	t.Setenv(HookTraceEnv, "0")
	wantOut, wantErr := traceRun(t, root, input)
	t.Setenv(HookTraceEnv, "1")
	tdir := filepath.Join(ws.Paths.Runtime, "hook-trace")
	if err := os.MkdirAll(ws.Paths.Runtime, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tdir, []byte("không phải thư mục"), 0o600); err != nil {
		t.Fatal(err)
	}
	gotOut, gotErr := traceRun(t, root, input)
	if gotOut != wantOut || gotErr != wantErr {
		t.Fatalf("tệp thường: đầu ra đổi\n%q / %q\nvs\n%q / %q", gotOut, gotErr, wantOut, wantErr)
	}
	os.Remove(tdir)
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("chmod chỉ-đọc không có tác dụng (Windows/root)")
	}
	if err := os.MkdirAll(tdir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(tdir, 0o700)
	gotOut, gotErr = traceRun(t, root, input)
	if gotOut != wantOut || gotErr != wantErr {
		t.Fatalf("thư mục chỉ đọc: đầu ra đổi %q / %q", gotOut, gotErr)
	}
	if _, err := os.Stat(filepath.Join(ws.Paths.Root, HookErrorLog)); !os.IsNotExist(err) {
		t.Fatalf("lỗi ghi vết không được vào hook-errors.log: %v", err)
	}
}

func TestHookTraceCapAndTruncation(t *testing.T) {
	root, ws := subagentRepo(t)
	t.Setenv(HookTraceEnv, "1")
	old := maxHookTraceFiles
	maxHookTraceFiles = 6
	defer func() { maxHookTraceFiles = old }()
	for i := 0; i < 8; i++ {
		traceRun(t, root, traceInput(root))
	}
	names := traceFiles(t, ws)
	if len(names) != 6 {
		t.Fatalf("muốn đúng 6 tệp (trần), có %d: %v", len(names), names)
	}
	// Tệp còn lại là các lần mới nhất: chạy thêm một lần, tệp cũ nhất phải biến mất.
	first := names[0]
	traceRun(t, root, traceInput(root))
	after := traceFiles(t, ws)
	if len(after) != 6 || after[0] == first {
		t.Fatalf("tệp cũ nhất phải bị xoá: %v -> %v", names, after)
	}
	// Đầu vào lớn bị cắt kèm dòng đánh dấu.
	maxHookTraceFiles = 500
	big := hookInput("sess-trace", "PostToolUse", root, map[string]any{"tool_name": "Read", "tool_use_id": "tu-big", "pad": strings.Repeat("x", maxHookTraceInput+10)})
	traceRun(t, root, big)
	var inFile string
	for _, n := range traceFiles(t, ws) {
		if fi, _ := os.Stat(filepath.Join(ws.Paths.Runtime, "hook-trace", n)); strings.HasSuffix(n, ".in") && fi.Size() > maxHookTraceInput {
			inFile = n
		}
	}
	if inFile == "" {
		t.Fatal("không thấy .in của đầu vào lớn")
	}
	b, _ := os.ReadFile(filepath.Join(ws.Paths.Runtime, "hook-trace", inFile))
	if int64(len(b)) > maxHookTraceInput+200 || !strings.Contains(string(b), "đã bị cắt") {
		t.Fatalf(".in dài %d, thiếu dấu cắt", len(b))
	}
}

func TestTraceLabel(t *testing.T) {
	for in, want := range map[string]string{"PostToolUse": "PostToolUse", "": "unknown", "a/../b": "ab", "../..": "unknown", "x y": "xy"} {
		if got := traceLabel(in); got != want {
			t.Errorf("traceLabel(%q) = %q, muốn %q", in, got, want)
		}
	}
}
