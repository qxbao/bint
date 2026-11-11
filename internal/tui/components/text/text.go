// Package text gom các hàm đo và cắt chuỗi hiển thị dùng chung cho mọi component: chuẩn hoá NFC
// trước khi đo (chữ tiếng Việt có dấu dựng sẵn hay tổ hợp đều cùng độ rộng), đo bằng độ rộng ô
// terminal, cắt và đệm theo ô chứ không theo byte hay rune.
package text

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/text/unicode/norm"
)

// NFC chuẩn hoá chuỗi về dạng dựng sẵn.
func NFC(s string) string { return norm.NFC.String(s) }

// Width là độ rộng hiển thị (số ô) của s, bỏ qua mã ANSI.
func Width(s string) int { return lipgloss.Width(NFC(s)) }

// Truncate cắt s còn tối đa w ô; phần bị cắt thay bằng "…" (nếu w >= 1).
func Truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = NFC(s)
	if lipgloss.Width(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

// Pad đệm khoảng trắng bên phải cho đủ w ô (không cắt).
func Pad(s string, w int) string {
	if n := w - Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// Fit cắt rồi đệm cho s chiếm đúng w ô.
func Fit(s string, w int) string { return Pad(Truncate(s, w), w) }

// Center đặt s vào giữa w ô (cắt nếu dài hơn).
func Center(s string, w int) string {
	s = Truncate(s, w)
	n := w - Width(s)
	if n <= 0 {
		return s
	}
	left := n / 2
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", n-left)
}

// Lines tách s theo dòng (không để lại dòng trống cuối do "\n" kết thúc).
func Lines(s string) []string {
	s = strings.TrimRight(NFC(s), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// MaxWidth là độ rộng lớn nhất của các dòng.
func MaxWidth(lines []string) int {
	m := 0
	for _, l := range lines {
		m = max(m, Width(l))
	}
	return m
}

// Wrap ngắt dòng s theo từ để mỗi dòng rộng tối đa w ô.
func Wrap(s string, w int) []string {
	if w <= 0 {
		return nil
	}
	var out []string
	for _, para := range strings.Split(NFC(s), "\n") {
		w2 := ansi.Wordwrap(para, w, "")
		w2 = ansi.Hardwrap(w2, w, true)
		out = append(out, strings.Split(w2, "\n")...)
	}
	return out
}

// Overlay đặt các dòng box lên các dòng base tại cột x, dòng y (không đổi kích thước base). Hoạt
// động theo ô và giữ mã ANSI của phần base còn lại.
func Overlay(base, box []string, x, y int) []string {
	out := append([]string(nil), base...)
	for i, bl := range box {
		row := y + i
		if row < 0 || row >= len(out) {
			continue
		}
		line := out[row]
		bw := Width(bl)
		total := max(Width(line), x+bw)
		line = Pad(line, total)
		out[row] = ansi.Cut(line, 0, x) + bl + ansi.Cut(line, x+bw, total)
	}
	return out
}
