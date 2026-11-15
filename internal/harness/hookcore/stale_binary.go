package hookcore

import (
	"bint/internal/skills"
)

// StaleBinaryReminder là dòng nhắc chèn ở SessionStart khi binary cũ hơn bản đã ghi core.lock.
const StaleBinaryReminder = "Bint: binary đang chạy cũ hơn skill đã xuất trong core.lock; chạy `bint update --pull` để cập nhật (đừng `bint sync` bằng binary này vì sẽ hạ cấp skill)."

// staleBinaryReminder so core.lock ở root với binary đang chạy. Chỉ đọc một tệp: không SQLite, không gọi mô hình, không
// trả lỗi; core.lock thiếu/hỏng, khớp, mới hơn hoặc không xác định thì không xuất gì ("").
func staleBinaryReminder(d Dialect, root string, env Env) string {
	lock, err := skills.ReadLock(root)
	if err != nil {
		return ""
	}
	bin := skills.CurrentBuild()
	if env.Build != nil {
		bin = env.Build()
	} else if env.BintVersion != "" {
		bin.Version = env.BintVersion
	}
	if skills.CompareBuild(lock, bin) != skills.DirBinOlder {
		return ""
	}
	return d.SessionStartOutput(StaleBinaryReminder)
}

// ClaudeStyleSessionStartOutput: hookSpecificOutput.additionalContext của SessionStart (Claude Code).
func ClaudeStyleSessionStartOutput(text string) string {
	return claudeStyleContext("SessionStart", text)
}
