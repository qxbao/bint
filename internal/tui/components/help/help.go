// Package help vẽ bảng trợ giúp (phím "?"): trước hết ngữ cảnh hiện tại (thẻ phụ đang xem, đoạn giới thiệu, các phím của thẻ), rồi mọi hành
// động đang dùng được nhóm theo màn hình kèm lệnh CLI tương đương. Dữ liệu sinh từ sổ hành động nên không bao giờ lệch với thực tế. Hộp cuộn
// được (phím, bánh xe) và hiện vị trí cuộn thay vì cắt.
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
	// Intro là đoạn giới thiệu ngắn (có thể rỗng) hiện ngay dưới tiêu đề, tự ngắt dòng.
	Intro string
	Rows  []Row
}

// Layout dựng mọi dòng của các nhóm cho vùng nội dung rộng iw (chưa cắt theo chiều cao).
func Layout(th theme.Theme, iw int, groups []Group) []string {
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
		if g.Intro != "" {
			for _, l := range text.Wrap(g.Intro, max(iw-2, 8)) {
				lines = append(lines, " "+th.S(theme.Muted, l))
			}
		}
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
	return lines
}

// Window trả số dòng nhìn thấy và vị trí đầu lớn nhất khi có tổng total dòng trong hộp cao h.
func Window(h, total int) (visible, maxTop int) {
	_, ih := frame.Inner(0, h)
	visible = max(ih, 1)
	return visible, max(total-visible, 0)
}

// Render vẽ bảng trợ giúp rộng w, cao h, cuộn tới dòng top (bị kẹp vào khoảng hợp lệ). Không đủ chỗ thì viền dưới hiện vị trí cuộn
// (pos(từ, đến, tổng), đã dịch) thay vì cắt im lặng. Trả hộp đã vẽ.
func Render(th theme.Theme, w, h int, title string, groups []Group, top int, pos func(from, to, total int) string) string {
	iw, _ := frame.Inner(w, h)
	lines := Layout(th, iw, groups)
	visible, maxTop := Window(h, len(lines))
	top = max(0, min(top, maxTop))
	box := frame.Box{Title: title, TitleAt: frame.Left, Focused: true}
	if maxTop > 0 && pos != nil {
		box.Bottom = th.S(theme.Muted, pos(top+1, min(top+visible, len(lines)), len(lines)))
		box.BottomAt = frame.Right
	}
	return box.Render(th, w, h, strings.Join(lines[top:min(top+visible, len(lines))], "\n"))
}
