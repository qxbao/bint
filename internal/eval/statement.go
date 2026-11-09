package eval

import (
	"regexp"
	"strings"
)

var (
	trailerRe = regexp.MustCompile(`(?i)^(co-authored-by|signed-off-by|reviewed-by|acked-by|tested-by|reported-by|suggested-by|helped-by|cc|change-id|fixes|closes|resolves|refs|see-also|link|bug|issue|pr-url|reviewed-on|generated-with)\s*:`)
	fenceRe   = regexp.MustCompile("^\\s*(```|~~~)")
	diffRe    = regexp.MustCompile(`^(diff --git |index [0-9a-f]+\.\.|@@ |\+\+\+ |--- a/)`)
	hexRe     = regexp.MustCompile(`\b[0-9a-f]{7,40}\b`)
	blankRuns = regexp.MustCompile(`\n{3,}`)
	botLineRe = regexp.MustCompile(`(?i)^\s*(🤖\s*)?generated with \[`)
)

// DraftPrompt viết lại thông điệp commit thành đề bài: bỏ trailer, khối mã,
// đoạn diff và tham chiếu tới commit lời giải. Đây chỉ là bản nháp tự động;
// CheckLeak và người duyệt quyết định đề bài có dùng được hay không.
func DraftPrompt(message, solutionCommit string) string {
	lines := strings.Split(strings.ReplaceAll(message, "\r\n", "\n"), "\n")
	var out []string
	inFence := false
	for _, ln := range lines {
		if fenceRe.MatchString(ln) {
			inFence = !inFence
			continue
		}
		if inFence || trailerRe.MatchString(strings.TrimSpace(ln)) || diffRe.MatchString(ln) || botLineRe.MatchString(ln) {
			continue
		}
		ln = hexRe.ReplaceAllStringFunc(ln, func(h string) string {
			if solutionCommit != "" && strings.HasPrefix(solutionCommit, h) {
				return ""
			}
			return h
		})
		out = append(out, strings.TrimRight(ln, " \t"))
	}
	s := strings.TrimSpace(strings.Join(out, "\n"))
	return blankRuns.ReplaceAllString(s, "\n\n")
}

// promptVersion được ghi vào mỗi đợt chạy; tăng khi đổi khung đề bài vì nó ảnh
// hưởng trực tiếp tới kết quả đo.
const promptVersion = 1

// AgentPrompt bọc đề bài của tác vụ thành lời nhắc gửi cho agent. Khung này cố
// định cho mọi nhánh để phép so sánh chỉ khác nhau ở ngữ cảnh.
func AgentPrompt(task string) string {
	var b strings.Builder
	b.WriteString("You are working in a software repository (the current directory). Complete the task below by editing the code.\n")
	b.WriteString("Work autonomously: do not ask questions, make reasonable assumptions, and stop when the change is complete.\n")
	b.WriteString("The change will be checked by the project's automated tests, including tests you cannot see.\n\n")
	b.WriteString("<task>\n")
	b.WriteString(strings.TrimSpace(task))
	b.WriteString("\n</task>\n")
	return b.String()
}
