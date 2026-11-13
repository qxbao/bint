package hookcore

import "strings"

// Hậu kiểm cô lập worktree ở `bint run complete` (NT-ISO-LEAK): hook là lớp chặn duy nhất và fail-open, nên workflow quét lại
// transcript của subagent bằng CHÍNH quy tắc của hook (worktreeDecision) để hai lớp không lệch nhau.

// WorktreeViolation cho biết một lời gọi công cụ (Edit-type hoặc Bash) của bint-implementer có vi phạm cô lập worktree không:
// đường dẫn ngoài worktree, `cd` ra ngoài, hoặc không có `cd <worktree> &&` mà cwd nằm ngoài worktree. what mô tả vi phạm.
// cwd là thư mục hiện tại của lời gọi (transcript ghi theo từng dòng).
func WorktreeViolation(worktree, cwd string, info ToolInfo) (what string, bad bool) {
	v := worktreeDecision(worktreeCheck{
		AgentType: SubagentImplementer,
		Info:      info,
		Cwd:       cwd,
		Step:      &SessionStepRecord{Isolated: true, Worktree: worktree},
	})
	return v.What, v.Deny
}

// Claude Code bọc deny của PreToolUse thành "PreToolUse:<Tool> hook error: <lý do>" ở ĐẦU nội dung tool_result (dòng đầu).
// Chỉ nhận dạng này: đầu ra thật của lệnh (Bash thoát khác 0) có thể chứa "hook error" ở giữa mà lệnh vẫn đã chạy. Lý do của
// chính Bint bắt đầu "Bint: <Tool> bị chặn" (worktreeDecision, readonlyDecision) nằm sau tiền tố đó; đặt cạnh lý do deny để
// cùng đổi khi lý do đổi.
const (
	hookDenialPrefix = "PreToolUse:"
	hookDenialMarker = "hook error"
)

// IsHookDenial cho biết nội dung tool_result là lời gọi bị hook chặn (không chạy): sau khi bỏ khoảng trắng đầu, bắt đầu bằng
// "PreToolUse:" và dòng đầu có "hook error".
func IsHookDenial(text string) bool {
	text = strings.TrimLeft(text, " \t\r\n")
	if !strings.HasPrefix(text, hookDenialPrefix) {
		return false
	}
	first, _, _ := strings.Cut(text, "\n")
	return strings.Contains(first, hookDenialMarker)
}

// Tiền tố nội dung tool_result của lời gọi bị Claude Code chặn TRƯỚC khi chạy (không thực thi), khảo sát từ transcript thật:
// Bash đã chạy mà lỗi luôn bắt đầu "Exit code N", nên mọi dạng dưới đây đều là chưa chạy. Dạng lạ không nằm trong danh sách này
// vẫn tính là đã chạy (hậu kiểm là lưới đỡ của hook fail-open); không suy "chưa chạy" từ việc thiếu "Exit code".
var preExecDenialPrefixes = []string{
	"<tool_use_error>",               // Claude Code từ chối: InputValidationError, "Blocked: sleep ...", "No such tool ..."
	"Permission to use ",             // "Permission to use <tool> has been denied"
	"Permission for this ",           // "... tool use was denied", "... action/command was denied by ..."
	"The user doesn't want to ",      // người dùng từ chối ("... proceed with this tool use", "... take this action right now")
	"The user doesn\u2019t want to ", // cùng câu với dấu nháy cong
}

// IsPreExecDenial cho biết nội dung tool_result của một lời gọi lỗi (is_error) là lời gọi bị chặn trước khi chạy: hook của Bint
// (IsHookDenial) hoặc Claude Code tự từ chối (thiếu quyền, người dùng từ chối, đầu vào không hợp lệ). Chỉ nhận tiền tố ở ĐẦU
// nội dung sau khi bỏ khoảng trắng đầu: đầu ra thật của lệnh có thể nhắc các chuỗi này ở giữa. Người gọi chỉ áp khi is_error.
func IsPreExecDenial(text string) bool {
	if IsHookDenial(text) {
		return true
	}
	text = strings.TrimLeft(text, " \t\r\n")
	for _, p := range preExecDenialPrefixes {
		if strings.HasPrefix(text, p) {
			return true
		}
	}
	return false
}
