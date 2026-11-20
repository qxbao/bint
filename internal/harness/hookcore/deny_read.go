package hookcore

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Rào đọc của bước b-goal (goalfreeze.go) và của review/thăm dò luồng B′: b-goal chỉ được viết kiểm tra mục tiêu từ requirement.md (B′: từ yêu cầu gốc), review B′ không
// đọc plan.md/tasks.md. Runner đặt EnvDenyRead (đường dẫn tuyệt đối, mỗi dòng một đường) cho các bước đó khi chạy bằng tiến trình, hoặc ghi DenyRead vào
// bản ghi session-step khi chạy bằng subagent (driver session); hook PreToolUse trả deny cho lời gọi công
// cụ có giá trị nào nhắc tới các đường dẫn đó (đọc tệp, Grep/Glob, lệnh shell). Đây là rào chống đọc nhầm, không phải ranh giới bảo mật: lệnh
// cố tình né (ghép chuỗi, biến, glob) không bị bắt. Hook không trả lỗi; thiếu biến thì không chặn gì.

// EnvDenyRead: tệp mà tiến trình agent hiện tại không được đọc (mỗi dòng một đường dẫn tuyệt đối).
const EnvDenyRead = "BINT_DENY_READ"

// GuardrailDenyRead là id của guardrail dựng sẵn trong sự kiện guardrail.triggered.
const GuardrailDenyRead = "bint-deny-read"

// denyReadPaths đọc EnvDenyRead thành danh sách đường dẫn cấm và danh sách ngoại lệ đã chuẩn hoá để so khớp (dấu /, không phân biệt
// hoa thường). Dòng bắt đầu bằng "!" là ngoại lệ: phần nhắc tới đường dẫn đó bị bỏ khỏi chuỗi trước khi quét (vd thư mục tác vụ bị cấm
// nhưng requirement.md được đọc). Dòng cấm có thể là thư mục: mọi đường dẫn bên trong cũng bị chặn (trừ ngoại lệ).
func denyReadPaths(env string) (deny, allow []string) {
	for l := range strings.SplitSeq(strings.ReplaceAll(env, "\r\n", "\n"), "\n") {
		l = strings.TrimSpace(l)
		switch {
		case l == "":
		case strings.HasPrefix(l, "!"):
			if l = strings.TrimSpace(l[1:]); l != "" {
				allow = append(allow, normDenyPath(l))
			}
		default:
			deny = append(deny, normDenyPath(l))
		}
	}
	return deny, allow
}

func normDenyPath(p string) string {
	return strings.ToLower(filepath.ToSlash(filepath.Clean(p)))
}

// denyReadDecision cho biết lời gọi công cụ (tool_input) có nhắc tới đường dẫn bị cấm đọc không; trả đường dẫn đầu tiên khớp.
func denyReadDecision(env string, toolInput map[string]any) (hit string, deny bool) {
	paths, allow := denyReadPaths(env)
	if len(paths) == 0 || len(toolInput) == 0 {
		return "", false
	}
	var scan func(v any) bool
	scan = func(v any) bool {
		switch x := v.(type) {
		case string:
			s := strings.ToLower(strings.ReplaceAll(x, `\`, "/"))
			for _, a := range allow {
				s = strings.ReplaceAll(s, a, "")
			}
			if i := slices.IndexFunc(paths, func(p string) bool { return strings.Contains(s, p) }); i >= 0 {
				hit = paths[i]
				return true
			}
		case map[string]any:
			for _, e := range x {
				if scan(e) {
					return true
				}
			}
		case []any:
			for _, e := range x {
				if scan(e) {
					return true
				}
			}
		}
		return false
	}
	if scan(toolInput) {
		return hit, true
	}
	return "", false
}

// denyReadEnv trả giá trị rào đọc của lời gọi hiện tại: biến môi trường (driver process) hoặc, khi không có, DenyRead trong bản ghi session-step của bước subagent
// (tra bằng khối <bint-task … run=…> ở đầu prompt của subagent). Không có thì "" (không chặn gì).
func denyReadEnv(d Dialect, hi Input, runtimeDir string) string {
	if env := os.Getenv(EnvDenyRead); env != "" {
		return env
	}
	if d.SubagentPrompt == nil || hi.AgentType == "" || !HasSessionSteps(runtimeDir) {
		return ""
	}
	prompt, ok := d.SubagentPrompt(hi)
	if !ok {
		return ""
	}
	tid, _, run, ok := PromptTaskHeader(prompt)
	if !ok {
		return ""
	}
	rec, ok := ReadSessionStep(runtimeDir, run)
	if !ok || rec.TaskID != tid || len(rec.DenyRead) == 0 {
		return ""
	}
	return strings.Join(rec.DenyRead, "\n")
}

// denyReadReason là lý do deny hiển thị cho agent.
func denyReadReason(hit string) string {
	return "Bint: bước b-goal chỉ được viết kiểm tra mục tiêu từ requirement.md; không đọc " + filepath.Base(hit) +
		" (plan.md/tasks.md bị chặn để giữ tính độc lập của kiểm tra mục tiêu)."
}
