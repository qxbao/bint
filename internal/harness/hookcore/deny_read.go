package hookcore

import (
	"path/filepath"
	"slices"
	"strings"
)

// Rào đọc của bước b-goal (goalfreeze.go): b-goal chỉ được viết kiểm tra mục tiêu từ requirement.md, không đọc plan.md/tasks.md. Runner đặt
// EnvDenyRead (đường dẫn tuyệt đối, mỗi dòng một đường) CHỈ cho bước kind goal chạy bằng tiến trình; hook PreToolUse trả deny cho lời gọi công
// cụ có giá trị nào nhắc tới các đường dẫn đó (đọc tệp, Grep/Glob, lệnh shell). Đây là rào chống đọc nhầm, không phải ranh giới bảo mật: lệnh
// cố tình né (ghép chuỗi, biến, glob) không bị bắt. Hook không trả lỗi; thiếu biến thì không chặn gì.

// EnvDenyRead: tệp mà tiến trình agent hiện tại không được đọc (mỗi dòng một đường dẫn tuyệt đối).
const EnvDenyRead = "BINT_DENY_READ"

// GuardrailDenyRead là id của guardrail dựng sẵn trong sự kiện guardrail.triggered.
const GuardrailDenyRead = "bint-deny-read"

// denyReadPaths đọc EnvDenyRead thành danh sách đường dẫn đã chuẩn hoá để so khớp (dấu /, không phân biệt hoa thường).
func denyReadPaths(env string) []string {
	var out []string
	for l := range strings.SplitSeq(strings.ReplaceAll(env, "\r\n", "\n"), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, normDenyPath(l))
		}
	}
	return out
}

func normDenyPath(p string) string {
	return strings.ToLower(filepath.ToSlash(filepath.Clean(p)))
}

// denyReadDecision cho biết lời gọi công cụ (tool_input) có nhắc tới đường dẫn bị cấm đọc không; trả đường dẫn đầu tiên khớp.
func denyReadDecision(env string, toolInput map[string]any) (hit string, deny bool) {
	paths := denyReadPaths(env)
	if len(paths) == 0 || len(toolInput) == 0 {
		return "", false
	}
	var scan func(v any) bool
	scan = func(v any) bool {
		switch x := v.(type) {
		case string:
			s := strings.ToLower(strings.ReplaceAll(x, `\`, "/"))
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

// denyReadReason là lý do deny hiển thị cho agent.
func denyReadReason(hit string) string {
	return "Bint: bước b-goal chỉ được viết kiểm tra mục tiêu từ requirement.md; không đọc " + filepath.Base(hit) +
		" (plan.md/tasks.md bị chặn để giữ tính độc lập của kiểm tra mục tiêu)."
}
