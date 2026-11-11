// Package tabs vẽ thanh tab một dòng: tab đang chọn in đậm, tab khác màu accent, số trong ngoặc là
// số việc đang chờ người (màu key; không dùng chữ số mũ vì độ rộng không ổn định giữa các font).
package tabs

import (
	"fmt"
	"strings"

	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
)

// Item là một tab.
type Item struct {
	Label string // nhãn đầy đủ
	Short string // nhãn rút gọn khi màn hình hẹp
	Badge int    // số việc chờ người; 0 = không hiện
}

// Render vẽ thanh tab trong tối đa width ô. compact: tab chưa chọn dùng nhãn rút gọn, chỉ tab đang
// chọn giữ nhãn đầy đủ. Nếu vẫn dài hơn width thì tự chuyển compact và cắt cuối dòng.
func Render(th theme.Theme, width int, items []Item, selected int, compact bool) string {
	sep := th.S(theme.Muted, " │ ")
	build := func(compact bool) string {
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
			parts[i] = s
		}
		return strings.Join(parts, sep)
	}
	line := build(compact)
	if text.Width(line) > width && !compact {
		line = build(true)
	}
	return text.Truncate(line, width)
}
