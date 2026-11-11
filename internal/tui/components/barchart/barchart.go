// Package barchart vẽ biểu đồ cột ngang cho tỷ lệ đạt kèm khoảng tin cậy 95%: mỗi hàng một nhánh, phần đạt tô
// màu accent, phần còn lại màu muted, hai đầu khoảng tin cậy đánh dấu bằng ký tự một cột đè lên cột (màu key), tỷ
// lệ phần trăm và khoảng tin cậy ở bên phải. Chỉ dùng ba màu ngoài fg; hình dạng (khối đặc, khối rỗng, ngoặc) vẫn
// đọc được khi đơn sắc. Độ rộng đo bằng text.Width sau NFC nên chịu được chuỗi có dấu và locale giả `xx`.
package barchart

import (
	"fmt"
	"math"
	"strings"

	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
)

// Row là một cột. Value, Lo, Hi là tỷ lệ trong [0, 1]; Valid = false (khoảng tin cậy không tính được, vd. thiếu lượt)
// thì không vẽ mốc và không in khoảng.
type Row struct {
	Label  string
	Value  float64
	Lo, Hi float64
	Valid  bool
	// Note là chú thích muted đặt sau số (vd. "n=10"); chỉ hiện khi đủ chỗ.
	Note string
}

const (
	fillGlyph  = "█"
	emptyGlyph = "░"
	minBar     = 4
)

func clamp01(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return math.Max(0, math.Min(v, 1))
}

func pct(v float64) string { return fmt.Sprintf("%.0f%%", clamp01(v)*100) }

// Render trả mỗi hàng một dòng, không dòng nào rộng quá width ô. Nhãn được căn thẳng cột theo nhãn dài nhất (cắt khi
// quá nửa chiều rộng chia ba), cột số căn phải.
func Render(th theme.Theme, rows []Row, width int) []string {
	if len(rows) == 0 || width <= 0 {
		return nil
	}
	labelW := 0
	for _, r := range rows {
		labelW = max(labelW, text.Width(r.Label))
	}
	labelW = max(min(labelW, max(width/3, 4)), 1)

	// Cột số: "100%" và, nếu có chỗ, " [ 34–100]".
	showCI := width >= 44
	valW := 4
	if showCI {
		valW = 4 + 1 + 9 // "100% [ 34–100]"
	}
	showNote := width >= 64
	noteW := 0
	if showNote {
		for _, r := range rows {
			noteW = max(noteW, text.Width(r.Note))
		}
		if noteW > 0 {
			noteW++
		}
	}
	barW := width - labelW - 1 - valW - 1 - noteW
	if barW < minBar { // quá hẹp: bỏ khoảng tin cậy và chú thích trước, rồi mới thu nhỏ nhãn
		showCI, showNote, noteW, valW = false, false, 0, 4
		barW = width - labelW - 1 - valW - 1
		if barW < minBar {
			labelW = max(width-minBar-valW-2, 1)
			barW = max(width-labelW-1-valW-1, 0)
		}
	}

	out := make([]string, 0, len(rows))
	for _, r := range rows {
		label := text.Pad(text.Truncate(r.Label, labelW), labelW)
		val := pct(r.Value)
		if showCI && r.Valid {
			val += fmt.Sprintf(" [%3.0f–%3.0f]", clamp01(r.Lo)*100, clamp01(r.Hi)*100)
		}
		val = padLeft(val, valW)
		line := th.S(theme.Accent, label) + " " + bar(th, r, barW) + " " + val
		if showNote && r.Note != "" {
			line += " " + th.S(theme.Muted, r.Note)
		}
		out = append(out, text.Fit(line, width))
	}
	return out
}

func padLeft(s string, w int) string {
	if n := w - text.Width(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}
	return s
}

// bar vẽ thanh n ô: khối đặc cho phần đạt, khối rỗng cho phần còn lại, mốc khoảng tin cậy đè lên.
func bar(th theme.Theme, r Row, n int) string {
	if n <= 0 {
		return ""
	}
	filled := int(math.Round(clamp01(r.Value) * float64(n)))
	cells := make([]string, n)
	for i := range cells {
		if i < filled {
			cells[i] = th.S(theme.Accent, fillGlyph)
		} else {
			cells[i] = th.S(theme.Muted, emptyGlyph)
		}
	}
	if r.Valid && n >= 3 {
		lo := min(int(math.Floor(clamp01(r.Lo)*float64(n))), n-1)
		hi := max(min(int(math.Ceil(clamp01(r.Hi)*float64(n)))-1, n-1), lo)
		if lo == hi {
			cells[lo] = th.S(theme.Key, "┃")
		} else {
			cells[lo], cells[hi] = th.S(theme.Key, "["), th.S(theme.Key, "]")
		}
	}
	return strings.Join(cells, "")
}
