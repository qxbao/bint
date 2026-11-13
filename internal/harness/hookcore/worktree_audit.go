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

// hookDenialMarkers: dấu hiệu trong nội dung tool_result khi lời gọi bị hook chặn. Claude Code bọc deny của PreToolUse thành
// "PreToolUse:<Tool> hook error: <lý do>"; lý do của chính Bint bắt đầu "Bint: <Tool> bị chặn" (worktreeDecision, readonlyDecision).
// Đặt cạnh lý do deny để cùng đổi khi lý do đổi.
const (
	hookDenialMarker  = "hook error"
	bintDenialPrefix  = "Bint:"
	bintDenialInfix   = "bị chặn"
	maxDenialTextScan = 4096
)

// IsHookDenial cho biết nội dung tool_result là lời gọi bị hook chặn (không chạy).
func IsHookDenial(text string) bool {
	if len(text) > maxDenialTextScan {
		text = text[:maxDenialTextScan]
	}
	if strings.Contains(text, hookDenialMarker) {
		return true
	}
	return strings.Contains(text, bintDenialPrefix) && strings.Contains(text, bintDenialInfix)
}
