package hookcore

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestWorktreeViolation(t *testing.T) {
	base := t.TempDir()
	wt := filepath.Join(base, "wt")
	root := filepath.Join(base, "root")
	other := filepath.Join(base, "wt2") // tên chung tiền tố: không nằm trong wt
	edit := func(p string) ToolInfo { return ToolInfo{Kind: ToolEdit, Paths: []string{p}} }
	sh := func(c string) ToolInfo { return ToolInfo{Kind: ToolShell, Command: c} }
	cases := []struct {
		name string
		cwd  string
		info ToolInfo
		bad  bool
		want string
	}{
		{"edit trong worktree", root, edit(filepath.Join(wt, "a.go")), false, ""},
		{"write ra cây gốc", root, edit(filepath.Join(root, "x.txt")), true, "x.txt"},
		{"write sang wt2", wt, edit(filepath.Join(other, "x.txt")), true, "nằm ngoài worktree"},
		{"đường dẫn tương đối theo cwd trong wt", wt, edit("a.go"), false, ""},
		{"đường dẫn tương đối theo cwd gốc", root, edit("a.go"), true, "a.go"},
		{"bash cd worktree", root, sh(CdPrefix(wt) + "go test ./..."), false, ""},
		{"bash cd thư mục con worktree", root, sh("cd '" + filepath.Join(wt, "sub") + "' && ls"), false, ""},
		{"bash cd ra ngoài", wt, sh("cd '" + root + "' && ls"), true, "cd"},
		{"bash cd wt2", wt, sh("cd '" + other + "' && ls"), true, "cd"},
		{"bash không cd, cwd trong worktree", wt, sh("go vet ./..."), false, ""},
		{"bash không cd, cwd ngoài worktree", root, sh("go vet ./..."), true, "không bắt đầu"},
		{"bash không cd, thiếu cwd", "", sh("ls"), true, "không bắt đầu"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			what, bad := WorktreeViolation(wt, c.cwd, c.info)
			if bad != c.bad || (c.bad && !strings.Contains(what, c.want)) || (!c.bad && what != "") {
				t.Fatalf("bad=%v what=%q, muốn bad=%v chứa %q", bad, what, c.bad, c.want)
			}
		})
	}
	// Công cụ không phải sửa tệp/shell: không phải vi phạm.
	if _, bad := WorktreeViolation(wt, root, ToolInfo{Kind: ToolRead, Paths: []string{filepath.Join(root, "x")}}); bad {
		t.Fatal("Read không bị kiểm")
	}
}

// Lý do deny thật của hook, sau khi Claude Code bọc "PreToolUse:<Tool> hook error: ", phải được IsHookDenial nhận ra: hai bên
// đổi cùng nhau. Đầu ra thật của lệnh có "hook error" ở giữa, hoặc lý do Bint trần không có tiền tố bọc, không phải bị chặn.
func TestIsHookDenial(t *testing.T) {
	v := worktreeDecision(worktreeCheck{AgentType: SubagentImplementer, Tool: "Write",
		Info: ToolInfo{Kind: ToolEdit, Paths: []string{"/ngoai/x"}}, Step: &SessionStepRecord{Isolated: true, Worktree: t.TempDir()}})
	if !v.Deny || !IsHookDenial("PreToolUse:Write hook error: "+v.Reason) {
		t.Fatalf("lý do deny của worktree không nhận ra: %+v", v)
	}
	for text, want := range map[string]bool{
		"PreToolUse:Write hook error: lý do khác":            true,
		"  \nPreToolUse:Bash hook error: Bint: Bash bị chặn": true,  // khoảng trắng đầu được bỏ
		"Bint: Bash bị chặn — lệnh":                          false, // không có tiền tố bọc
		"make: lỗi\nPreToolUse:Bash hook error: x":           false, // không ở đầu
		"Exit code 1\nthấy hook error ở giữa đầu ra":         false,
		"PreToolUse:Bash ok\nhook error ở dòng sau":          false, // không ở dòng đầu
		"Bash hook error: x":                                 false,
		"File created successfully at: /x/y.txt":             false,
		"Bint: ok":                                           false,
		"":                                                   false,
		strings.Repeat("a", 5000) + "hook error":             false,
	} {
		if got := IsHookDenial(text); got != want {
			t.Errorf("IsHookDenial(%.40q) = %v, muốn %v", text, got, want)
		}
	}
}

// IsPreExecDenial nhận các dạng Claude Code chặn trước khi chạy; Bash đã chạy ("Exit code N"), dạng lạ và tiền tố nằm giữa
// văn bản thì không.
func TestIsPreExecDenial(t *testing.T) {
	for text, want := range map[string]bool{
		"<tool_use_error>Blocked: sleep 100 followed by: cat x</tool_use_error>":           true,
		"<tool_use_error>InputValidationError: Bash failed</tool_use_error>":               true,
		"<tool_use_error>Error: No such tool available: Foo</tool_use_error>":              true,
		"  \n<tool_use_error>Blocked: x</tool_use_error>":                                  true, // khoảng trắng đầu được bỏ
		"Permission to use Bash has been denied.":                                          true,
		"Permission for this tool use was denied.":                                         true,
		"Permission for this action was denied by the Claude Code auto mode classifier: x": true,
		"Permission for this command was denied by a built-in Claude Code safety check":    true,
		"The user doesn't want to proceed with this tool use. The tool use was rejected":   true,
		"The user doesn't want to take this action right now. STOP what you are doing":     true,
		"The user doesn\u2019t want to proceed with this tool use.":                        true,
		"PreToolUse:Bash hook error: Bint: Bash bị chặn":                                   true,
		"Exit code 1\n<tool_use_error>trong đầu ra</tool_use_error>":                       false,
		"Exit code 1\nPermission for this tool use was denied":                             false,
		"Exit code 2\nThe user doesn't want to proceed":                                    false,
		"Exit code 1\n...":                        false,
		"log: <tool_use_error>x</tool_use_error>": false,
		"permission denied":                       false,
		"Something unknown went wrong":            false,
		"":                                        false,
	} {
		if got := IsPreExecDenial(text); got != want {
			t.Errorf("IsPreExecDenial(%.50q) = %v, muốn %v", text, got, want)
		}
	}
}
