// Package list là danh sách ảo: chỉ gọi hàm vẽ hàng cho các chỉ số nằm trong vùng nhìn, nên chịu được
// hàng nghìn mục. Giữ con trỏ và vị trí cuộn; tập hàng được chọn (chọn nhiều) khoá theo ID để sống sót
// khi danh sách được lọc lại. Căn cột theo độ rộng chuỗi có dấu (text.Width, NFC).
package list

import (
	"strconv"
	"strings"

	"bint/internal/tui/components/text"
	"bint/internal/tui/components/zone"
)

// Model là trạng thái cuộn của danh sách N hàng.
type Model struct {
	N      int
	Cursor int
	Top    int // chỉ số hàng đầu tiên của vùng nhìn
	// H là chiều cao vùng nhìn lần vẽ gần nhất (để cuộn theo trang và giữ con trỏ trong vùng nhìn).
	H int
}

// SetN đặt số hàng và kéo con trỏ/cuộn về khoảng hợp lệ.
func (m *Model) SetN(n int) {
	m.N = max(n, 0)
	m.Cursor = max(0, min(m.Cursor, m.N-1))
	m.clamp()
}

// Move dịch con trỏ d hàng (không vòng).
func (m *Model) Move(d int) {
	m.Cursor = max(0, min(m.Cursor+d, m.N-1))
	m.clamp()
}

// Page dịch con trỏ d trang (một trang = chiều cao vùng nhìn).
func (m *Model) Page(d int) { m.Move(d * max(m.H-1, 1)) }

// Home và End đưa con trỏ về hàng đầu/cuối.
func (m *Model) Home() { m.Cursor, m.Top = 0, 0 }
func (m *Model) End()  { m.Cursor = max(m.N-1, 0); m.clamp() }

// MoveTo đặt con trỏ ở hàng i.
func (m *Model) MoveTo(i int) { m.Cursor = max(0, min(i, m.N-1)); m.clamp() }

// clamp giữ con trỏ trong vùng nhìn [Top, Top+H).
func (m *Model) clamp() {
	h := max(m.H, 1)
	if m.Cursor < m.Top {
		m.Top = m.Cursor
	}
	if m.Cursor >= m.Top+h {
		m.Top = m.Cursor - h + 1
	}
	m.Top = max(0, min(m.Top, max(m.N-h, 0)))
}

// Visible trả khoảng chỉ số [from, to) của vùng nhìn cao h.
func (m *Model) Visible(h int) (from, to int) {
	m.H = max(h, 1)
	m.clamp()
	return m.Top, min(m.Top+m.H, m.N)
}

// Render vẽ vùng nhìn cao h: gọi row đúng một lần cho mỗi hàng đang hiện (không gọi cho hàng ngoài vùng
// nhìn), đệm cho đủ h dòng. Mỗi dòng do row trả được cắt/đệm cho vừa w ô.
func (m *Model) Render(w, h int, row func(i int, cursor bool) string) string {
	from, to := m.Visible(h)
	out := make([]string, 0, h)
	for i := from; i < to; i++ {
		out = append(out, zone.Mark(RowZone(i), text.Fit(row(i, i == m.Cursor), w)))
	}
	for len(out) < h {
		out = append(out, strings.Repeat(" ", w))
	}
	return strings.Join(out, "\n")
}

// RowZone là id vùng bấm của hàng i ("r:<i>"); màn hình đổi id này về chỉ số hàng khi nhận chuột.
func RowZone(i int) string { return "r:" + strconv.Itoa(i) }

// ParseRowZone trả chỉ số hàng của id vùng "r:<i>".
func ParseRowZone(id string) (int, bool) {
	rest, ok := strings.CutPrefix(id, "r:")
	if !ok {
		return 0, false
	}
	i, err := strconv.Atoi(rest)
	return i, err == nil && i >= 0
}

// Selection là tập ID được chọn.
type Selection map[string]bool

// Toggle đảo trạng thái chọn của id.
func (s Selection) Toggle(id string) {
	if s[id] {
		delete(s, id)
	} else {
		s[id] = true
	}
}

// Has cho biết id đang được chọn.
func (s Selection) Has(id string) bool { return s[id] }

// Count là số mục đang chọn.
func (s Selection) Count() int { return len(s) }

// Clear bỏ chọn tất cả.
func (s Selection) Clear() {
	for k := range s {
		delete(s, k)
	}
}

// IDs trả các ID đang chọn theo thứ tự của order (ID ngoài order bị bỏ).
func (s Selection) IDs(order []string) []string {
	var out []string
	for _, id := range order {
		if s[id] {
			out = append(out, id)
		}
	}
	return out
}

// Widths chia tổng độ rộng total cho các cột: cột có chiều rộng cố định > 0 giữ nguyên, cột có 0 chia
// đều phần còn lại (tối thiểu 4 ô); giữa các cột cách gap ô. Trả độ rộng từng cột.
func Widths(total, gap int, fixed []int) []int {
	out := make([]int, len(fixed))
	flex, used := 0, gap*max(len(fixed)-1, 0)
	for i, f := range fixed {
		if f > 0 {
			out[i] = f
			used += f
		} else {
			flex++
		}
	}
	if flex > 0 {
		each := max((total-used)/flex, 4)
		for i, f := range fixed {
			if f == 0 {
				out[i] = each
			}
		}
	}
	return out
}

// Row ghép các ô thành một dòng, mỗi ô căn trái trong độ rộng widths[i] (đo theo ô hiển thị), cách gap ô.
func Row(widths []int, gap int, cells ...string) string {
	sep := strings.Repeat(" ", gap)
	parts := make([]string, len(widths))
	for i, w := range widths {
		c := ""
		if i < len(cells) {
			c = cells[i]
		}
		parts[i] = text.Fit(c, w)
	}
	return strings.Join(parts, sep)
}
