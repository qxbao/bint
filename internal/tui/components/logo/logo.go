// Package logo vẽ logo chữ điểm ảnh "bint." bằng ký tự khối (không dùng figlet): bản lớn (5 dòng, mỗi
// điểm ảnh 2 cột × 1 dòng) khi đủ chỗ, bản nhỏ (3 dòng, nửa khối ▀ ▄ █), hoặc chỉ chữ "bint." đậm.
package logo

import (
	"strings"

	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
)

// Kích thước tối thiểu của vùng nội dung để dùng bản logo điểm ảnh; nhỏ hơn thì chỉ hiện chữ "bint." đậm.
const MinW, MinH = 100, 30

// Phông 5 dòng; X = điểm sáng. Chữ cái cách nhau một cột trống.
var glyphs = map[rune][5]string{
	'b': {"X..", "X..", "XX.", "X.X", "XX."},
	'i': {"X", ".", "X", "X", "X"},
	'n': {"...", "...", "XX.", "X.X", "X.X"},
	't': {".X.", "XXX", ".X.", ".X.", ".XX"},
	'.': {".", ".", ".", ".", "X"},
}

const word = "bint."

// bitmap ghép các chữ thành lưới điểm ảnh 5 dòng.
func bitmap() [5][]bool {
	var rows [5][]bool
	for ci, r := range word {
		g := glyphs[r]
		for y := 0; y < 5; y++ {
			if ci > 0 {
				rows[y] = append(rows[y], false)
			}
			for _, c := range g[y] {
				rows[y] = append(rows[y], c == 'X')
			}
		}
	}
	return rows
}

// Fits cho biết vùng w×h đủ chỗ cho logo điểm ảnh.
func Fits(w, h int) bool { return w >= MinW && h >= MinH }

// Render trả các dòng của logo: bản điểm ảnh 5 dòng (mỗi điểm ảnh 2 cột × 1 dòng, ký tự █) khi pixel
// = true, ngược lại chỉ chữ "bint." đậm. Chữ "bint" màu accent, dấu chấm màu key; mọi dòng cùng độ rộng.
func Render(th theme.Theme, pixel bool) []string {
	if !pixel {
		return []string{th.Bold(th.S(theme.Accent, "bint") + th.S(theme.Key, "."))}
	}
	bm := bitmap()
	width := len(bm[0])
	dotStart := width - 1 // cột của dấu chấm
	var out []string
	for y := 0; y < 5; y++ {
		var sb strings.Builder
		for x := 0; x < width; x++ {
			switch {
			case !bm[y][x]:
				sb.WriteString("  ")
			case x >= dotStart:
				sb.WriteString(th.S(theme.Key, "██"))
			default:
				sb.WriteString(th.S(theme.Accent, "██"))
			}
		}
		out = append(out, sb.String())
	}
	return out
}

// Width là độ rộng của logo.
func Width(th theme.Theme, pixel bool) int { return text.MaxWidth(Render(th, pixel)) }
