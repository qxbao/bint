// Package meter gồm các thành phần trực quan nhỏ thay cho chuỗi chữ dài: thanh tỷ lệ (Bar), thanh xếp chồng theo
// nhóm (Stacked), huy hiệu trạng thái (Badge) và định dạng tiền (Money). Mọi kết quả có độ rộng hiển thị đúng bằng
// (hoặc không quá) độ rộng được cấp. Ở chế độ Mono không phát mã ANSI nào: thanh phân biệt bằng hình khối, đoạn bằng
// ký hiệu riêng, huy hiệu bằng ngoặc vuông.
package meter

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
)

const (
	fillGlyph  = "█"
	emptyGlyph = "░"
)

// defaultGlyphs: ký hiệu mặc định theo thứ tự đoạn, khác nhau để đơn sắc vẫn phân biệt được.
var defaultGlyphs = []string{"█", "▓", "▒", "░", "▚", "▞"}

func paint(th theme.Theme, r theme.Role, s string) string {
	if !th.Colored() {
		return s
	}
	return th.S(r, s)
}

// Bar vẽ thanh rộng đúng width ô: phần ratio (kẹp trong [0, 1]) bằng khối đặc tô màu role, phần còn lại khối rỗng muted.
func Bar(th theme.Theme, ratio float64, width int, role theme.Role) string {
	if width <= 0 {
		return ""
	}
	if math.IsNaN(ratio) {
		ratio = 0
	}
	ratio = math.Max(0, math.Min(ratio, 1))
	n := int(math.Round(ratio * float64(width)))
	if ratio > 0 && n == 0 {
		n = 1
	}
	return paint(th, role, strings.Repeat(fillGlyph, n)) + paint(th, theme.Muted, strings.Repeat(emptyGlyph, width-n))
}

// Seg là một đoạn của thanh xếp chồng. Glyph rỗng nhận ký hiệu mặc định theo vị trí đoạn.
type Seg struct {
	N     int
	Role  theme.Role
	Glyph string
}

// Glyphs trả ký hiệu đã dùng cho từng đoạn (để dựng chú giải khớp thanh).
func Glyphs(segs []Seg) []string {
	out := make([]string, len(segs))
	for i, s := range segs {
		out[i] = s.Glyph
		if out[i] == "" {
			out[i] = defaultGlyphs[i%len(defaultGlyphs)]
		}
	}
	return out
}

// Stacked vẽ thanh rộng đúng width ô, chia theo N của các đoạn (phần dư lớn nhất nhận ô còn lại; đoạn N>0 tối thiểu
// một ô khi đủ chỗ). Tổng N bằng 0 hoặc width<=0 trả thanh rỗng muted (hoặc chuỗi rỗng khi width<=0).
func Stacked(th theme.Theme, segs []Seg, width int) string {
	if width <= 0 {
		return ""
	}
	total := 0
	for _, s := range segs {
		if s.N > 0 {
			total += s.N
		}
	}
	if total == 0 {
		return paint(th, theme.Muted, strings.Repeat(emptyGlyph, width))
	}
	cells := make([]int, len(segs))
	type rem struct {
		i int
		f float64
	}
	var rems []rem
	used := 0
	for i, s := range segs {
		if s.N <= 0 {
			continue
		}
		exact := float64(s.N) * float64(width) / float64(total)
		cells[i] = int(exact)
		used += cells[i]
		rems = append(rems, rem{i, exact - float64(cells[i])})
	}
	sort.SliceStable(rems, func(x, y int) bool { return rems[x].f > rems[y].f })
	for k := 0; used < width && len(rems) > 0; k++ { // phần dư lớn nhất trước
		cells[rems[k%len(rems)].i]++
		used++
	}
	for i := range segs { // đoạn có số liệu mà 0 ô: lấy một ô từ đoạn rộng nhất
		if segs[i].N > 0 && cells[i] == 0 {
			big := -1
			for j := range cells {
				if cells[j] > 1 && (big < 0 || cells[j] > cells[big]) {
					big = j
				}
			}
			if big >= 0 {
				cells[big]--
				cells[i]++
			}
		}
	}
	glyphs := Glyphs(segs)
	var b strings.Builder
	for i, n := range cells {
		if n > 0 {
			b.WriteString(paint(th, segs[i].Role, strings.Repeat(glyphs[i], n)))
		}
	}
	return b.String()
}

// Badge là huy hiệu trạng thái: có màu thì "● nhãn" tô màu role; đơn sắc thì "[nhãn]".
func Badge(th theme.Theme, role theme.Role, label string) string {
	if !th.Colored() {
		return "[" + label + "]"
	}
	return th.S(role, "● "+label)
}

// BadgeWidth là độ rộng hiển thị của Badge(label) ở mọi chế độ (chế độ màu "● " và đơn sắc "[]" cùng rộng label+2).
func BadgeWidth(label string) int { return text.Width(label) + 2 }

// Money định dạng chi phí USD: "?" khi không biết (nil), "$0.4200", và "≥$0.4200" khi chỉ là tổng một phần.
func Money(v *float64, partial bool) string {
	if v == nil {
		return "?"
	}
	s := fmt.Sprintf("$%.4f", *v)
	if partial {
		return "≥" + s
	}
	return s
}

// sparkRunes là tám mức của sparkline (thấp → cao).
var sparkRunes = []rune("▁▂▃▄▅▆▇█")

// Spark vẽ sparkline một dòng: mỗi giá trị một ô, chiều cao tỉ lệ với giá trị lớn nhất; giá trị 0 vẽ mức thấp nhất màu muted.
// hot(i) đúng thì ô i tô màu hotRole (vd ngày có lượt hỏng), còn lại tô role.
func Spark(th theme.Theme, vals []int, role, hotRole theme.Role, hot func(i int) bool) string {
	top := 0
	for _, v := range vals {
		top = max(top, v)
	}
	var b strings.Builder
	for i, v := range vals {
		if v <= 0 || top == 0 {
			b.WriteString(th.S(theme.Muted, string(sparkRunes[0])))
			continue
		}
		lvl := max(1, min(len(sparkRunes)-1, (v*(len(sparkRunes)-1)+top-1)/top))
		r := role
		if hot != nil && hot(i) {
			r = hotRole
		}
		b.WriteString(paint(th, r, string(sparkRunes[lvl])))
	}
	return b.String()
}
