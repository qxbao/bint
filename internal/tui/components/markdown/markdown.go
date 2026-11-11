// Package markdown là trình xem Markdown gọn cho terminal (docs/tui-plan.md mục 7): tiêu đề, danh sách (kể cả
// ô kiểm), khối mã, trích dẫn, đường kẻ, đoạn văn ngắt dòng theo độ rộng và chữ đậm/mã nội dòng. Không hỗ trợ
// bảng, ảnh, liên kết (ngoài phạm vi). Mọi dòng trả về rộng tối đa w ô, kể cả chữ có dấu.
package markdown

import (
	"regexp"
	"strings"

	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
)

var (
	headingRe  = regexp.MustCompile(`^ {0,3}(#{1,6})\s+(.*?)\s*#*\s*$`)
	bulletRe   = regexp.MustCompile(`^(\s*)([-*+])\s+(.*)$`)
	numberedRe = regexp.MustCompile(`^(\s*)(\d{1,3})[.)]\s+(.*)$`)
	checkRe    = regexp.MustCompile(`^\[([ xX])\]\s+(.*)$`)
	fenceRe    = regexp.MustCompile("^\\s{0,3}(```+|~~~+)\\s*(\\S*)")
	ruleRe     = regexp.MustCompile(`^ {0,3}([-*_])(\s*[-*_]){2,}\s*$`)
	quoteRe    = regexp.MustCompile(`^\s{0,3}>\s?(.*)$`)
	boldRe     = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)
	codeRe     = regexp.MustCompile("`([^`\n]+)`")
)

// Render đổi Markdown src thành các dòng rộng tối đa w ô (đã tô màu theo theme).
func Render(th theme.Theme, src string, w int) []string {
	if w < 8 {
		w = 8
	}
	src = strings.ReplaceAll(text.NFC(src), "\r\n", "\n")
	var out []string
	add := func(l ...string) { out = append(out, l...) }
	lines := strings.Split(strings.TrimRight(src, "\n"), "\n")
	inCode := false
	fence := ""
	para := []string{}
	flushPara := func() {
		if len(para) == 0 {
			return
		}
		for _, l := range text.Wrap(strings.Join(para, " "), w) {
			add(inline(th, l))
		}
		para = para[:0]
	}
	for _, raw := range lines {
		if m := fenceRe.FindStringSubmatch(raw); m != nil {
			if !inCode {
				flushPara()
				inCode, fence = true, m[1][:3]
				if m[2] != "" {
					add(th.S(theme.Muted, "┌ "+text.Truncate(m[2], w-2)))
				}
			} else if strings.HasPrefix(m[1], fence) {
				inCode = false
			}
			continue
		}
		if inCode {
			add(th.S(theme.Muted, "│ ") + text.Truncate(strings.ReplaceAll(raw, "\t", "    "), w-2))
			continue
		}
		trim := strings.TrimSpace(raw)
		switch {
		case trim == "":
			flushPara()
			if len(out) > 0 && out[len(out)-1] != "" {
				add("")
			}
		case headingRe.MatchString(raw):
			flushPara()
			m := headingRe.FindStringSubmatch(raw)
			lvl := len(m[1])
			if len(out) > 0 && out[len(out)-1] != "" {
				add("")
			}
			for _, l := range text.Wrap(m[2], max(w-lvl-1, 4)) {
				if lvl <= 2 {
					add(th.Bold(th.S(theme.Accent, l)))
				} else {
					add(th.Bold(l))
				}
			}
			if lvl <= 2 {
				add(th.S(theme.Muted, strings.Repeat("─", min(w, max(text.Width(m[2]), 3)))))
			}
		case ruleRe.MatchString(raw):
			flushPara()
			add(th.S(theme.Muted, strings.Repeat("─", w)))
		case quoteRe.MatchString(raw):
			flushPara()
			m := quoteRe.FindStringSubmatch(raw)
			for _, l := range text.WrapHang(m[1], w, th.S(theme.Muted, "▎ "), th.S(theme.Muted, "▎ ")) {
				add(inline(th, l))
			}
		case bulletRe.MatchString(raw) || numberedRe.MatchString(raw):
			flushPara()
			var ind, mark, body string
			if m := bulletRe.FindStringSubmatch(raw); m != nil {
				ind, mark, body = m[1], "•", m[3]
			} else {
				m = numberedRe.FindStringSubmatch(raw)
				ind, mark, body = m[1], m[2]+".", m[3]
			}
			if c := checkRe.FindStringSubmatch(body); c != nil {
				if strings.EqualFold(c[1], "x") {
					mark = th.S(theme.OK, "[x]")
				} else {
					mark = th.S(theme.Muted, "[ ]")
				}
				body = c[2]
			}
			pad := strings.Repeat(" ", min(len(strings.ReplaceAll(ind, "\t", "    ")), w/2))
			first := pad + mark + " "
			rest := pad + strings.Repeat(" ", text.Width(mark)+1)
			for _, l := range text.WrapHang(body, w, first, rest) {
				add(inline(th, l))
			}
		default:
			para = append(para, trim)
		}
	}
	flushPara()
	// Bỏ dòng trống thừa ở cuối.
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	for i, l := range out {
		out[i] = text.Truncate(l, w)
	}
	return out
}

// inline tô chữ đậm (**x**) và mã (`x`) trong một dòng đã ngắt. Dấu không khớp trong dòng giữ nguyên.
func inline(th theme.Theme, l string) string {
	l = boldRe.ReplaceAllStringFunc(l, func(s string) string { return th.Bold(s[2 : len(s)-2]) })
	return codeRe.ReplaceAllStringFunc(l, func(s string) string { return th.S(theme.Key, s[1:len(s)-1]) })
}
