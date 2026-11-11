// Package text gom các hàm đo và cắt chuỗi hiển thị dùng chung cho mọi component: chuẩn hoá NFC
// trước khi đo (chữ tiếng Việt có dấu dựng sẵn hay tổ hợp đều cùng độ rộng), đo bằng độ rộng ô
// terminal, cắt và đệm theo ô chứ không theo byte hay rune.
package text

import (
	"os"
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

// WrapHang ngắt dòng s theo từ để mỗi dòng (kể cả tiền tố) rộng tối đa w ô, dòng đầu có tiền tố
// first, các dòng sau có tiền tố rest (thụt treo: dùng rest rộng bằng first để dòng gói tiếp thẳng
// hàng với chữ). Đoạn mã trong cặp dấu huyền (`...`) là một từ nguyên khối: không ngắt ở khoảng trắng
// hay dấu gạch nối bên trong. Chỉ ngắt tại khoảng trắng; từ dài hơn chỗ trống thì cắt cứng.
func WrapHang(s string, w int, first, rest string) []string {
	var out []string
	prefix := first
	for _, para := range strings.Split(NFC(s), "\n") {
		line := ""
		flush := func() {
			out = append(out, prefix+line)
			prefix, line = rest, ""
		}
		for _, word := range codeWords(para) {
			room := max(w-Width(prefix), 1)
			for Width(word) > room { // từ dài hơn cả dòng: cắt cứng
				if line != "" {
					flush()
					room = max(w-Width(prefix), 1)
					continue
				}
				line, word = cutHead(word, room)
				flush()
				room = max(w-Width(prefix), 1)
			}
			switch {
			case word == "":
			case line == "":
				line = word
			case Width(line)+1+Width(word) <= room:
				line += " " + word
			default:
				flush()
				line = word
			}
		}
		if line != "" || para == "" {
			flush()
		}
	}
	return out
}

// cutHead tách phần đầu của word rộng tối đa room ô (luôn lấy ít nhất một cụm ký tự hiển thị, kể cả
// khi nó rộng hơn room) và trả phần còn lại. Cắt theo số byte đã tiêu thụ nên head+rest == word kể cả
// khi word có mã ANSI (ansi.Truncate chép mã thoát nằm sau điểm cắt, nên head không còn là tiền tố của
// word và TrimPrefix không bỏ được gì). Mã thoát đứng trước điểm cắt nằm ở head, sau điểm cắt ở rest.
func cutHead(word string, room int) (head, rest string) {
	var state byte
	used, cells := 0, 0
	for used < len(word) {
		_, width, n, ns := ansi.DecodeSequence(word[used:], state, nil)
		if n <= 0 {
			break
		}
		if width > 0 && cells > 0 && cells+width > room {
			break
		}
		state = ns
		used += n
		cells += width
		if cells >= room {
			break
		}
	}
	if used == 0 { // không tiến được (không nên xảy ra): trả nguyên word để vòng gọi chắc chắn kết thúc
		return word, ""
	}
	return word[:used], word[used:]
}

// codeWords tách s theo khoảng trắng, giữ nguyên khối đoạn mã trong dấu huyền (kể cả dấu câu dính liền
// sau dấu huyền đóng). Dấu huyền không cân thì coi phần còn lại như chữ thường.
func codeWords(s string) []string {
	var words []string
	var cur strings.Builder
	inCode := false
	for _, r := range s {
		switch {
		case r == '`':
			inCode = !inCode
			cur.WriteRune(r)
		case r == ' ' && !inCode:
			if cur.Len() > 0 {
				words = append(words, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if inCode { // dấu huyền không đóng: tách lại như chữ thường
		rest := cur.String()
		cur.Reset()
		words = append(words, strings.Fields(rest)...)
	} else if cur.Len() > 0 {
		words = append(words, cur.String())
	}
	return words
}

// homeDir trả thư mục home đã bỏ dấu phân cách cuối; rỗng nếu không xác định được (hoặc home là "/").
func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return strings.TrimRight(home, `/\`)
}

// Tilde rút gọn thư mục home ở đầu đường dẫn thành "~" (chỉ khi home là một thành phần trọn vẹn của
// đường dẫn: /home/a không khớp /home/ab). Mọi đường dẫn hiển thị trong TUI đều đi qua đây.
func Tilde(p string) string {
	home := homeDir()
	if home == "" || !strings.HasPrefix(p, home) {
		return p
	}
	rest := p[len(home):]
	if rest == "" || rest[0] == '/' || rest[0] == '\\' {
		return "~" + rest
	}
	return p
}

// TildeIn rút gọn mọi đường dẫn bắt đầu bằng thư mục home nằm trong một chuỗi tự do (ví dụ chi tiết
// một bước: "/home/a/.local/state/x (schema v6)") thành "~...". Chỉ thay khi home đứng ở đầu một
// "từ" (đầu chuỗi hoặc sau khoảng trắng, dấu ngoặc, nháy, '=', ':', ',') và là thành phần trọn vẹn.
func TildeIn(s string) string {
	home := homeDir()
	if home == "" {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], home) && atWordStart(s, i) {
			end := i + len(home)
			if end == len(s) || s[end] == '/' || s[end] == '\\' || !isPathRune(s[end]) {
				b.WriteByte('~')
				i = end
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func atWordStart(s string, i int) bool {
	if i == 0 {
		return true
	}
	return strings.IndexByte(" \t([{\"'`=:,;", s[i-1]) >= 0
}

// isPathRune cho biết byte có thể nằm trong tên thành phần đường dẫn (để /home/a không khớp /home/ab).
func isPathRune(c byte) bool {
	return c >= 0x80 || c == '_' || c == '-' || c == '.' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// Path rút gọn đường dẫn để hiển thị trong w ô: thay home bằng "~", và nếu vẫn dài thì bỏ phần đầu,
// thay bằng "…" (giữ phần cuối, cố gắng bắt đầu ở ranh giới thư mục).
func Path(p string, w int) string {
	p = NFC(Tilde(p))
	if w <= 0 {
		return ""
	}
	if Width(p) <= w {
		return p
	}
	if w == 1 {
		return "…"
	}
	tail := tailFit(p, w-1)
	if i := strings.IndexAny(tail, `/\`); i > 0 && i < len(tail)-1 {
		tail = tail[i:]
	}
	return "…" + tail
}

// TruncateHead cắt s còn tối đa w ô bằng cách bỏ phần ĐẦU, thay bằng "…" (giữ đuôi). Dùng cho chuỗi mà
// đuôi mới là phần quan trọng (đường dẫn, hậu tố "(schema v6)").
func TruncateHead(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = NFC(s)
	if Width(s) <= w {
		return s
	}
	return "…" + tailFit(s, w-1)
}

// tailFit trả hậu tố dài nhất của s rộng tối đa w ô.
func tailFit(s string, w int) string {
	runes := []rune(s)
	start, width := len(runes), 0
	for start > 0 {
		rw := Width(string(runes[start-1]))
		if width+rw > w {
			break
		}
		width += rw
		start--
	}
	return string(runes[start:])
}
