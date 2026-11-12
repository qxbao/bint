package theme

import (
	"regexp"
	"strconv"
	"strings"
)

var sgrRE = regexp.MustCompile("\x1b\\[([0-9;:]*)m")

// isColorParam cho biết mã SGR n đặt màu (30–38, 40–48, 90–97, 100–107); 39/49 (về màu mặc định) và các mã kiểu chữ không phải màu.
func isColorParam(n int) bool {
	return n >= 30 && n <= 38 || n >= 40 && n <= 48 || n >= 90 && n <= 97 || n >= 100 && n <= 107
}

// NoColor bỏ mọi mã màu SGR khỏi s khi chế độ là Mono (NO_COLOR); đậm, mờ, gạch chân, đảo màu và các mã đặt lại được giữ. Chế độ có màu trả
// nguyên s. Dùng cho nội dung ngoài (log của agent, tệp, diff) có thể mang màu riêng.
func (t Theme) NoColor(s string) string {
	if t.Colored() || !strings.Contains(s, "\x1b[") {
		return s
	}
	return sgrRE.ReplaceAllStringFunc(s, func(seq string) string {
		params := sgrRE.FindStringSubmatch(seq)[1]
		if params == "" { // ESC[m không tham số = đặt lại (lipgloss, git phát): phải giữ, nếu không đậm/đảo màu tràn
			return seq
		}
		var keep []string
		toks := strings.FieldsFunc(params, func(r rune) bool { return r == ';' || r == ':' })
		for i := 0; i < len(toks); i++ {
			n, err := strconv.Atoi(toks[i])
			if err != nil {
				continue
			}
			if n == 38 || n == 48 { // 38;5;n hoặc 38;2;r;g;b: bỏ cả các đối số
				if i+1 < len(toks) {
					if toks[i+1] == "5" {
						i += 2
					} else if toks[i+1] == "2" {
						i += 4
					}
				}
				continue
			}
			if !isColorParam(n) {
				keep = append(keep, toks[i])
			}
		}
		if len(keep) == 0 {
			return ""
		}
		return "\x1b[" + strings.Join(keep, ";") + "m"
	})
}
