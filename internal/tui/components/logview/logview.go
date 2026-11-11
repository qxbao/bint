// Package logview là khung xem log trực tiếp của việc nền: các dòng của orchestrator ở trên, đuôi log của bước
// agent đang chạy ở dưới. Tự cuộn tới dòng mới nhất trừ khi người dùng đã cuộn lên xem (Follow = false).
package logview

import (
	"strings"

	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
)

// Model là trạng thái cuộn của khung log.
type Model struct {
	// Follow: bám theo dòng mới nhất.
	Follow bool
	// Top là dòng đầu của vùng nhìn khi không Follow.
	Top int
}

// New dựng khung log đang bám theo dòng mới.
func New() Model { return Model{Follow: true} }

// Scroll cuộn d dòng; cuộn lên thì thôi bám theo, cuộn tới đáy thì bám theo lại.
func (m *Model) Scroll(d, total, h int) {
	maxTop := max(total-h, 0)
	cur := m.Top
	if m.Follow {
		cur = maxTop
	}
	cur = max(0, min(cur+d, maxTop))
	m.Top = cur
	m.Follow = cur >= maxTop
}

// Section là một đoạn log có nhãn (vd. "Điều phối", "Bước b-feat").
type Section struct {
	Title string
	Lines []string
}

// Render vẽ các đoạn trong vùng w × h: mỗi đoạn một dòng tiêu đề rồi các dòng log (cắt theo độ rộng). Đoạn
// cuối cùng được ưu tiên chỗ khi hết chỗ (dòng cũ của đoạn trên bị bỏ trước). Trả đúng h dòng.
func (m *Model) Render(th theme.Theme, secs []Section, w, h int) string {
	var all []string
	for _, s := range secs {
		if len(s.Lines) == 0 && s.Title == "" {
			continue
		}
		if s.Title != "" {
			all = append(all, th.S(theme.Accent, "── "+text.Truncate(s.Title, max(w-4, 4))+" "+strings.Repeat("─", max(w-text.Width(s.Title)-4, 0))))
		}
		for _, l := range s.Lines {
			all = append(all, text.Truncate(strings.ReplaceAll(l, "\t", "    "), w))
		}
	}
	maxTop := max(len(all)-h, 0)
	top := m.Top
	if m.Follow {
		top = maxTop
	}
	top = max(0, min(top, maxTop))
	out := make([]string, 0, h)
	for i := top; i < len(all) && len(out) < h; i++ {
		out = append(out, text.Fit(all[i], w))
	}
	for len(out) < h {
		out = append(out, strings.Repeat(" ", w))
	}
	return strings.Join(out, "\n")
}

// Total là số dòng đã dựng (cho Scroll); gọi cùng secs với Render.
func Total(secs []Section) int {
	n := 0
	for _, s := range secs {
		if len(s.Lines) == 0 && s.Title == "" {
			continue
		}
		if s.Title != "" {
			n++
		}
		n += len(s.Lines)
	}
	return n
}
