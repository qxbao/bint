// Package tabs vẽ thanh tab một dòng: tab đang chọn in đậm, tab khác màu accent, số trong ngoặc là
// số việc đang chờ người (màu key; không dùng chữ số mũ vì độ rộng không ổn định giữa các font).
package tabs

import (
	"fmt"
	"strconv"
	"strings"

	"bint/internal/tui/components/text"
	"bint/internal/tui/components/zone"
	"bint/internal/tui/theme"
)

// Item là một tab.
type Item struct {
	Label string // nhãn đầy đủ
	Short string // nhãn rút gọn khi màn hình hẹp
	Badge int    // số việc chờ người; 0 = không hiện
}

// ZoneID là id vùng bấm của tab i ("tab:<i>").
func ZoneID(i int) string { return "tab:" + strconv.Itoa(i) }

// ParseZone trả chỉ số tab của id vùng "tab:<i>".
func ParseZone(id string) (int, bool) {
	rest, ok := strings.CutPrefix(id, "tab:")
	if !ok {
		return 0, false
	}
	i, err := strconv.Atoi(rest)
	return i, err == nil && i >= 0
}

// seps là các dấu ngăn tab từ rộng tới hẹp: " │ " bình thường; hẹp hơn chỉ khi tám tab vẫn chưa vừa ở chế độ rút gọn (màn 80 cột, locale
// có chuỗi dài, tab đang chọn mang nhãn đầy đủ) để tab cuối không bị cắt mất.
var seps = []string{" │ ", " │", "│"}

// Render vẽ thanh tab trong tối đa width ô. compact: tab chưa chọn dùng nhãn rút gọn, chỉ tab đang
// chọn giữ nhãn đầy đủ. Nếu vẫn dài hơn width thì tự chuyển compact, rồi thu hẹp dấu ngăn, rồi cắt cuối dòng.
func Render(th theme.Theme, width int, items []Item, selected int, compact bool) string {
	build := func(compact bool, sepText string) string {
		sep := th.S(theme.Muted, sepText)
		parts := make([]string, len(items))
		for i, it := range items {
			label := it.Label
			if compact && i != selected && it.Short != "" {
				label = it.Short
			}
			label = text.NFC(label)
			var s string
			if i == selected {
				s = th.Selected(label)
			} else {
				s = th.S(theme.Accent, label)
			}
			if it.Badge > 0 {
				s += th.S(theme.Key, fmt.Sprintf(" (%d)", it.Badge))
			}
			parts[i] = zone.Mark(ZoneID(i), s)
		}
		return strings.Join(parts, sep)
	}
	line := build(compact, seps[0])
	if text.Width(line) > width && !compact {
		compact = true
		line = build(true, seps[0])
	}
	for _, sp := range seps[1:] {
		if text.Width(line) <= width {
			break
		}
		line = build(compact, sp)
	}
	return text.Truncate(line, width)
}

// Width là độ rộng nhỏ nhất mà Render cần để không cắt tab nào (compact: tab chưa chọn dùng nhãn rút gọn, dấu ngăn hẹp nhất).
func Width(items []Item, selected int, compact bool) int {
	n := 0
	for i, it := range items {
		label := it.Label
		if compact && i != selected && it.Short != "" {
			label = it.Short
		}
		n += text.Width(text.NFC(label))
		if it.Badge > 0 {
			n += text.Width(fmt.Sprintf(" (%d)", it.Badge))
		}
	}
	if len(items) > 1 {
		n += (len(items) - 1) * text.Width(seps[len(seps)-1])
	}
	return n
}
