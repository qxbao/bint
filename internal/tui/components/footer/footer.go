// Package footer chọn và vẽ các phím tắt hiện ở viền dưới của khung theo độ ưu tiên: luôn 4–6 phím
// quan trọng nhất của ngữ cảnh hiện tại. Thiếu chỗ thì bỏ phím ưu tiên thấp trước, rồi rút nhãn;
// các phím ghim (thường là ? và q) luôn được giữ.
package footer

import (
	"strings"

	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
)

// MaxHints là số phím tối đa hiện cùng lúc.
const MaxHints = 6

// Hint là một phím tắt.
type Hint struct {
	Key   string // "Enter", "a", "?"
	Label string // nhãn đã dịch
	// Priority: số nhỏ = quan trọng hơn (giữ lại trước).
	Priority int
	// Pinned: luôn giữ (kể cả phải rút nhãn).
	Pinned bool
}

func (h Hint) width(label string) int {
	if label == "" {
		return text.Width("[" + h.Key + "]")
	}
	return text.Width("[" + h.Key + "→ " + label + "]")
}

// Fit chọn các phím vừa maxW ô (kể cả khoảng trắng giữa các phím), giữ thứ tự ban đầu. Kết quả có
// thể có Label đã rút ngắn hoặc rỗng (chỉ hiện phím).
func Fit(hints []Hint, maxW int) []Hint {
	if len(hints) == 0 {
		return nil
	}
	// Thứ tự giữ lại: ghim trước, rồi theo ưu tiên, cùng ưu tiên thì theo thứ tự xuất hiện.
	order := make([]int, len(hints))
	for i := range order {
		order[i] = i
	}
	less := func(a, b int) bool {
		ha, hb := hints[a], hints[b]
		if ha.Pinned != hb.Pinned {
			return ha.Pinned
		}
		if ha.Priority != hb.Priority {
			return ha.Priority < hb.Priority
		}
		return a < b
	}
	for i := 1; i < len(order); i++ { // sắp ổn định (chèn)
		for j := i; j > 0 && less(order[j], order[j-1]); j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
	pinned := 0
	for _, h := range hints {
		if h.Pinned {
			pinned++
		}
	}
	limit := min(len(order), max(MaxHints, pinned))
	total := func(idx []int, labels func(Hint) string) int {
		w := 0
		for _, i := range idx {
			w += hints[i].width(labels(hints[i]))
		}
		return w + max(len(idx)-1, 0)
	}
	keep := func(n int) []Hint {
		idx := append([]int(nil), order[:n]...)
		sortInts(idx)
		out := make([]Hint, len(idx))
		for i, k := range idx {
			out[i] = hints[k]
		}
		return out
	}
	full := func(h Hint) string { return h.Label }
	for n := limit; n >= max(pinned, 1); n-- {
		idx := order[:n]
		if total(idx, full) <= maxW {
			return keep(n)
		}
	}
	// Chỉ còn các phím ghim mà vẫn tràn: rút nhãn dần, cuối cùng chỉ hiện phím.
	n := max(pinned, 1)
	idx := order[:n]
	for l := 8; l >= 0; l-- {
		short := func(h Hint) string {
			if l == 0 {
				return ""
			}
			return text.Truncate(h.Label, l)
		}
		if total(idx, short) <= maxW || l == 0 {
			out := keep(n)
			for i := range out {
				out[i].Label = short(out[i])
			}
			return out
		}
	}
	return keep(n)
}

func sortInts(a []int) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

// Render vẽ các phím đã chọn: [phím→ nhãn] cách nhau một khoảng trắng; phím màu key, nhãn màu muted.
func Render(th theme.Theme, hints []Hint) string {
	parts := make([]string, len(hints))
	for i, h := range hints {
		var sb strings.Builder
		sb.WriteString(th.S(theme.Muted, "["))
		sb.WriteString(th.S(theme.Key, text.NFC(h.Key)))
		if h.Label != "" {
			sb.WriteString(th.S(theme.Muted, "→ "+text.NFC(h.Label)))
		}
		sb.WriteString(th.S(theme.Muted, "]"))
		parts[i] = sb.String()
	}
	return strings.Join(parts, " ")
}

// Line chọn rồi vẽ: các phím vừa maxW ô.
func Line(th theme.Theme, hints []Hint, maxW int) string {
	return Render(th, Fit(hints, maxW))
}
