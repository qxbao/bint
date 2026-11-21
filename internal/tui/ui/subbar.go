package ui

import (
	"strings"

	"bint/internal/i18n"
	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
)

// SubBar vẽ thanh thẻ phụ một dòng rộng tối đa width ô: dạng đầy đủ " Tên " cách nhau "·"; không vừa thì chỉ thẻ đang chọn có đệm, các
// thẻ khác cách nhau một dấu cách; vẫn không vừa thì chỉ hiện cửa sổ các thẻ quanh thẻ đang chọn kèm "‹ ›" báo còn thẻ ẩn. Tên thẻ
// đang chọn luôn hiện đủ (không bao giờ bị cắt). names[i] ứng với id ids[i] (vùng bấm "t:<id>"); selected là chỉ số trong names.
func SubBar(th theme.Theme, names []string, ids []int, selected, width int) string {
	if len(names) == 0 {
		return ""
	}
	mark := func(i int, s string) string { return MarkSub(ids[i], s) }
	build := func(full bool) string {
		parts := make([]string, len(names))
		for i, n := range names {
			switch {
			case i == selected:
				parts[i] = mark(i, th.Selected(" "+n+" "))
			case full:
				parts[i] = mark(i, th.S(theme.Muted, " "+n+" "))
			default:
				parts[i] = mark(i, th.S(theme.Muted, n))
			}
		}
		if full {
			return " " + strings.Join(parts, th.S(theme.Muted, "·"))
		}
		return " " + strings.Join(parts, " ")
	}
	if bar := build(true); text.Width(bar) <= width {
		return bar
	}
	if bar := build(false); text.Width(bar) <= width {
		return bar
	}
	// Cửa sổ quanh thẻ đang chọn, mở rộng dần sang hai bên tới khi hết chỗ.
	span := func(lo, hi int) int {
		n := 2
		for i := lo; i <= hi; i++ {
			n += text.Width(names[i]) + 1
			if i == selected {
				n += 2
			}
		}
		return n + 2
	}
	lo, hi := selected, selected
	for {
		grew := false
		if hi+1 < len(names) && span(lo, hi+1) <= width {
			hi++
			grew = true
		}
		if lo > 0 && span(lo-1, hi) <= width {
			lo--
			grew = true
		}
		if !grew {
			break
		}
	}
	var parts []string
	for i := lo; i <= hi; i++ {
		if i == selected {
			parts = append(parts, mark(i, th.Selected(" "+names[i]+" ")))
		} else {
			parts = append(parts, mark(i, th.S(theme.Muted, names[i])))
		}
	}
	left, right := " ", ""
	if lo > 0 {
		left = mark(lo-1, th.S(theme.Muted, "‹"))
	}
	if hi < len(names)-1 {
		right = " " + mark(hi+1, th.S(theme.Muted, "›"))
	}
	return left + " " + strings.Join(parts, " ") + right
}

// FilterPrompt dựng ô lọc nhanh đang nhập ("/ chữ▏ · 12/57 khớp · Enter giữ · Esc huỷ"); shown/total < 0 thì không ghi số khớp.
func FilterPrompt(th theme.Theme, rw Rewriter, query string, shown, total int) string {
	s := th.S(theme.Key, "/ ") + query + th.S(theme.Key, "▏")
	if shown >= 0 && total >= 0 {
		s += th.S(theme.Muted, "  "+rw.Cat.T("ui.filter.count", i18n.A{"shown": shown, "total": total}))
	}
	return s + th.S(theme.Muted, "  "+rw.Cat.T("ui.filter.keys"))
}

// WithFilter đặt ô lọc vào bên phải dòng thanh thẻ phụ bar (rộng w ô); thiếu chỗ thì ô lọc thay cho thanh thẻ phụ.
func WithFilter(bar, prompt string, w int) string {
	if text.Width(bar)+2+text.Width(prompt) <= w {
		return bar + strings.Repeat(" ", w-text.Width(bar)-text.Width(prompt)) + prompt
	}
	return " " + text.Truncate(prompt, max(w-1, 1))
}
