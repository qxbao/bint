// Package theme định nghĩa bảng màu theo VAI TRÒ (không theo màu) cho TUI của Bint: fg, muted,
// accent, key, ok, warn, err. Có bộ tối/sáng truecolor, bộ 16 màu và chế độ đơn sắc (NO_COLOR:
// chỉ còn đậm, gạch chân, đảo màu). Nền luôn là màu mặc định của terminal.
//
// Quy tắc dùng (tiết chế): mỗi màn hình tối đa ba màu ngoài fg (thường accent, key, muted); màu
// trạng thái (ok/warn/err) chỉ tô ký hiệu trạng thái, và ký hiệu luôn đi kèm hình dạng để không
// phụ thuộc vào khả năng phân biệt màu.
package theme

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Role là vai trò màu.
type Role int

const (
	Fg     Role = iota // nội dung
	Muted              // viền, chú thích, phân cách
	Accent             // nhãn, tab chưa chọn, liên kết
	Key                // phím tắt, điểm nhấn hiếm
	OK                 // trạng thái tốt
	Warn               // cảnh báo
	Err                // lỗi
	numRoles
)

// Mode là chế độ màu.
type Mode int

const (
	Dark   Mode = iota // nền tối, truecolor
	Light              // nền sáng, truecolor
	ANSI16             // terminal chỉ có 16 màu
	Mono               // NO_COLOR hoặc terminal không màu: chỉ đậm, gạch chân, đảo màu
)

func (m Mode) String() string {
	switch m {
	case Dark:
		return "dark"
	case Light:
		return "light"
	case ANSI16:
		return "ansi16"
	}
	return "mono"
}

// ParseMode đọc tên chế độ ("dark", "light", "ansi16", "mono"); ok = false nếu lạ.
func ParseMode(s string) (Mode, bool) {
	for _, m := range []Mode{Dark, Light, ANSI16, Mono} {
		if m.String() == s {
			return m, true
		}
	}
	return Dark, false
}

// Modes là mọi chế độ, theo thứ tự hiển thị trong trang trưng bày.
var Modes = []Mode{Dark, Light, ANSI16, Mono}

type spec struct {
	fg                         color.Color // nil = màu mặc định của terminal
	bold, faint, underline, rv bool
}

func hex(s string) color.Color { return lipgloss.Color(s) }
func ansi(n int) color.Color   { return lipgloss.ANSIColor(uint8(n)) }

// palette trả đặc tả từng vai trò của một chế độ.
func palette(m Mode) [numRoles]spec {
	switch m {
	case Dark:
		return [numRoles]spec{
			Fg: {fg: hex("#E6E6E6")}, Muted: {fg: hex("#7A7A7A")}, Accent: {fg: hex("#5FB3A1")}, Key: {fg: hex("#E5A55A")},
			OK: {fg: hex("#8FB573")}, Warn: {fg: hex("#E5C07B")}, Err: {fg: hex("#E06C75")},
		}
	case Light:
		return [numRoles]spec{
			Fg: {fg: hex("#1F1F1F")}, Muted: {fg: hex("#6B6B6B")}, Accent: {fg: hex("#1E7F6D")}, Key: {fg: hex("#B36A00")},
			OK: {fg: hex("#3F7D2A")}, Warn: {fg: hex("#9A6B00")}, Err: {fg: hex("#B3261E")},
		}
	case ANSI16:
		// Fg dùng màu mặc định của terminal (nền sáng hay tối đều đọc được).
		return [numRoles]spec{
			Fg: {}, Muted: {fg: ansi(8)}, Accent: {fg: ansi(6)}, Key: {fg: ansi(3)},
			OK: {fg: ansi(2)}, Warn: {fg: ansi(3)}, Err: {fg: ansi(1)},
		}
	}
	// Mono: không màu. Phân biệt bằng đậm/đảo màu/gạch chân.
	return [numRoles]spec{
		Fg: {}, Muted: {}, Accent: {}, Key: {bold: true}, OK: {}, Warn: {bold: true}, Err: {rv: true},
	}
}

// Theme là bộ kiểu đã dựng cho một chế độ.
type Theme struct {
	Mode  Mode
	roles [numRoles]lipgloss.Style
	bold  lipgloss.Style
	under lipgloss.Style
	rev   lipgloss.Style
}

// New dựng Theme cho chế độ m.
func New(m Mode) Theme {
	t := Theme{Mode: m}
	for r, sp := range palette(m) {
		st := lipgloss.NewStyle()
		if sp.fg != nil {
			st = st.Foreground(sp.fg)
		}
		t.roles[r] = st.Bold(sp.bold).Faint(sp.faint).Underline(sp.underline).Reverse(sp.rv)
	}
	t.bold = lipgloss.NewStyle().Bold(true)
	t.under = lipgloss.NewStyle().Underline(true)
	t.rev = lipgloss.NewStyle().Reverse(true)
	return t
}

// Detect chọn chế độ màu: NO_COLOR (bất kỳ giá trị khác rỗng) hoặc terminal không có màu → Mono;
// terminal chỉ có 16 màu → ANSI16; ngược lại theo nền terminal (darkBackground).
func Detect(noColor, basic16, darkBackground bool) Mode {
	switch {
	case noColor:
		return Mono
	case basic16:
		return ANSI16
	case darkBackground:
		return Dark
	}
	return Light
}

// S tô chuỗi s theo vai trò r. Chuỗi rỗng trả về rỗng.
func (t Theme) S(r Role, s string) string {
	if s == "" {
		return s
	}
	return t.roles[r].Render(s)
}

// Bold in đậm.
func (t Theme) Bold(s string) string {
	if s == "" {
		return s
	}
	return t.bold.Render(s)
}

// Underline gạch chân.
func (t Theme) Underline(s string) string {
	if s == "" {
		return s
	}
	return t.under.Render(s)
}

// Selected đánh dấu mục đang chọn: đậm; ở chế độ đơn sắc thêm đảo màu để vẫn thấy rõ.
func (t Theme) Selected(s string) string {
	if s == "" {
		return s
	}
	if t.Mode == Mono {
		return t.rev.Render(t.bold.Render(s))
	}
	return t.bold.Render(s)
}

// Colored cho biết chế độ có màu (không phải Mono).
func (t Theme) Colored() bool { return t.Mode != Mono }
