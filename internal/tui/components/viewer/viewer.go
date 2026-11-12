// Package viewer là trình xem văn bản cuộn được (nội dung chi tiết, trích đoạn dòng, diff tô màu).
package viewer

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
)

// Model giữ các dòng đã dựng sẵn và vị trí cuộn.
type Model struct {
	Lines []string
	Top   int
	// H là chiều cao vùng nhìn lần vẽ gần nhất.
	H int
}

// SetLines thay nội dung; giữ vị trí cuộn nếu còn hợp lệ. Ký tự tab được đổi thành 4 khoảng trắng: tab
// làm lệch độ rộng đo theo ô (khung viền, cột) và mỗi terminal hiểu một kiểu.
func (m *Model) SetLines(l []string) {
	m.Lines = l
	copied := false
	for i, s := range l {
		if strings.Contains(s, "\t") {
			if !copied {
				m.Lines, copied = append([]string(nil), l...), true
			}
			m.Lines[i] = strings.ReplaceAll(s, "\t", "    ")
		}
	}
	m.clamp()
}

// SetWrapped như SetLines nhưng ngắt dòng dài hơn w ô thay vì để bị cắt khi vẽ: dòng tiếp thụt theo dòng gốc cộng 2 ô (giữ nguyên mã màu).
func (m *Model) SetWrapped(l []string, w int) {
	out := make([]string, 0, len(l))
	for _, s := range l {
		s = strings.ReplaceAll(s, "\t", "    ")
		if w <= 0 || text.Width(s) <= w {
			out = append(out, s)
			continue
		}
		plain := ansi.Strip(s)
		hang := strings.Repeat(" ", len(plain)-len(strings.TrimLeft(plain, " "))+2)
		out = append(out, text.WrapHang(s, w, "", hang)...)
	}
	m.SetLines(out)
}

// SetText đặt nội dung từ chuỗi nhiều dòng (không ngắt dòng; người gọi tự bọc theo độ rộng).
func (m *Model) SetText(s string) { m.SetLines(text.Lines(s)) }

// Reset đưa về đầu nội dung.
func (m *Model) Reset() { m.Top = 0 }

func (m *Model) clamp() { m.Top = max(0, min(m.Top, max(len(m.Lines)-max(m.H, 1), 0))) }

// Scroll cuộn d dòng.
func (m *Model) Scroll(d int) { m.Top += d; m.clamp() }

// Page cuộn d trang.
func (m *Model) Page(d int) { m.Scroll(d * max(m.H-1, 1)) }

// Home và End cuộn về đầu/cuối.
func (m *Model) Home() { m.Top = 0 }
func (m *Model) End()  { m.Top = max(len(m.Lines)-max(m.H, 1), 0) }

// Overflow cho biết nội dung dài hơn vùng nhìn.
func (m Model) Overflow() bool { return len(m.Lines) > max(m.H, 1) }

// Render vẽ vùng nhìn rộng w, cao h (đệm cho đủ h dòng, mỗi dòng đúng w ô).
func (m *Model) Render(w, h int) string {
	m.H = max(h, 1)
	m.clamp()
	out := make([]string, 0, h)
	for i := m.Top; i < len(m.Lines) && len(out) < h; i++ {
		out = append(out, text.Fit(m.Lines[i], w))
	}
	for len(out) < h {
		out = append(out, strings.Repeat(" ", w))
	}
	return strings.Join(out, "\n")
}

// Position trả "từ-đến/tổng" của vùng đang hiện (rỗng nếu không cuộn).
func (m Model) Position() string {
	if !m.Overflow() {
		return ""
	}
	to := min(m.Top+m.H, len(m.Lines))
	return itoa(m.Top+1) + "-" + itoa(to) + "/" + itoa(len(m.Lines))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// ColorDiff tô màu một bản diff thống nhất: dòng thêm màu ok, xoá màu err, đầu khối (@@) màu accent,
// tiêu đề tệp đậm. Mã thoát không đổi độ rộng nên căn lề giữ nguyên.
func ColorDiff(th theme.Theme, diff string) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimRight(diff, "\n"), "\n") {
		switch {
		case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"):
			out = append(out, th.Bold(l))
		case strings.HasPrefix(l, "@@"):
			out = append(out, th.S(theme.Accent, l))
		case strings.HasPrefix(l, "+"):
			out = append(out, th.S(theme.OK, l))
		case strings.HasPrefix(l, "-"):
			out = append(out, th.S(theme.Err, l))
		default:
			out = append(out, l)
		}
	}
	return out
}
