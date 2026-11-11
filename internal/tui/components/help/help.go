// Package help vẽ bảng trợ giúp đầy đủ (phím "?"): mọi hành động đang dùng được, nhóm theo màn hình,
// kèm lệnh CLI tương đương. Dữ liệu sinh từ sổ hành động nên không bao giờ lệch với thực tế.
package help

import (
	"strings"

	"bint/internal/tui/components/frame"
	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
)

// Row là một dòng trợ giúp.
type Row struct {
	Key   string
	Title string
	CLI   string // lệnh CLI tương đương, có thể rỗng
	// Locked khác rỗng: hành động bị khoá, kèm lý do.
	Locked string
}

// Group là một nhóm dòng dưới một tiêu đề.
type Group struct {
	Title string
	Rows  []Row
}

// Render vẽ bảng trợ giúp rộng w, cao h; nếu không đủ chỗ, cắt cuối và báo còn nữa (more đã dịch).
func Render(th theme.Theme, w, h int, title string, groups []Group, more string) string {
	iw, ih := frame.Inner(w, h)
	var lines []string
	kw := 0
	for _, g := range groups {
		for _, r := range g.Rows {
			kw = max(kw, text.Width(r.Key))
		}
	}
	for gi, g := range groups {
		if gi > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, th.S(theme.Accent, th.Bold(g.Title)))
		for _, r := range g.Rows {
			key := th.S(theme.Key, text.Pad(r.Key, kw))
			tail := ""
			switch {
			case r.Locked != "":
				tail = th.S(theme.Warn, " ("+r.Locked+")")
			case r.CLI != "":
				tail = th.S(theme.Muted, "  "+r.CLI)
			}
			body := text.Truncate(r.Title, max(iw-kw-3, 4))
			lines = append(lines, " "+key+"  "+body+text.Truncate(tail, max(iw-kw-4-text.Width(body), 0)))
		}
	}
	if len(lines) > ih {
		lines = append(lines[:max(ih-1, 0)], th.S(theme.Muted, more))
	}
	return frame.Box{Title: title, TitleAt: frame.Left, Focused: true}.Render(th, w, h, strings.Join(lines, "\n"))
}
