// Package palette là bảng lệnh (":" hoặc Ctrl+K): tìm mờ trên mọi hành động của mọi màn hình, kể cả
// hành động hiếm dùng — bảo đảm "không tính năng nào bị giấu". Khớp theo dãy con (subsequence) không
// phân biệt hoa/thường và dấu tiếng Việt, tính điểm theo ký tự liên tiếp và đầu từ; tìm được cả theo
// đường lệnh CLI và tên cờ (--budget).
package palette

import (
	"sort"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"bint/internal/tui/components/frame"
	"bint/internal/tui/components/text"
	"bint/internal/tui/components/zone"
	"bint/internal/tui/theme"
)

// Item là một dòng trong bảng lệnh.
type Item struct {
	ID    string
	Title string // đã dịch
	// Group là nhãn nhóm (tên màn hình đã dịch); cũng được tìm.
	Group string
	// Keywords là từ khoá phụ: đường lệnh CLI và tên cờ.
	Keywords string
	// Key là phím tắt hiển thị bên phải.
	Key string
	// Locked khác rỗng: hành động bị khoá, kèm lý do (hiện thay phím tắt, không chạy được).
	Locked string
}

// Match là một dòng đã khớp.
type Match struct {
	Item  Item
	Score int
}

// Model là trạng thái bảng lệnh.
type Model struct {
	items   []Item
	Query   string
	Cursor  int
	matches []Match
}

// New dựng bảng lệnh trên items.
func New(items []Item) Model {
	m := Model{items: items}
	m.refilter()
	return m
}

// Matches trả các dòng đang hiện theo điểm giảm dần.
func (m Model) Matches() []Match { return m.matches }

// Selected trả dòng đang chọn.
func (m Model) Selected() (Item, bool) {
	if m.Cursor < 0 || m.Cursor >= len(m.matches) {
		return Item{}, false
	}
	return m.matches[m.Cursor].Item, true
}

// Type thêm ký tự vào ô tìm.
func (m *Model) Type(s string) {
	m.Query += s
	m.refilter()
}

// Backspace xoá ký tự cuối.
func (m *Model) Backspace() {
	r := []rune(m.Query)
	if len(r) > 0 {
		m.Query = string(r[:len(r)-1])
		m.refilter()
	}
}

// Clear xoá ô tìm.
func (m *Model) Clear() {
	m.Query = ""
	m.refilter()
}

// Move dịch con trỏ (vòng quanh).
func (m *Model) Move(d int) {
	n := len(m.matches)
	if n == 0 {
		m.Cursor = 0
		return
	}
	m.Cursor = ((m.Cursor+d)%n + n) % n
}

func (m *Model) refilter() {
	m.matches = m.matches[:0:0]
	for i, it := range m.items {
		sc, ok := ScoreItem(m.Query, it)
		if ok {
			m.matches = append(m.matches, Match{Item: it, Score: sc - i/1000}) // ổn định theo thứ tự đầu vào
		}
	}
	sort.SliceStable(m.matches, func(i, j int) bool { return m.matches[i].Score > m.matches[j].Score })
	m.Cursor = 0
}

// fold chuẩn hoá để so khớp: chữ thường, bỏ dấu (NFD rồi bỏ dấu tổ hợp), đ → d.
func fold(s string) string {
	var sb strings.Builder
	for _, r := range norm.NFD.String(s) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case r == 'đ' || r == 'Đ':
			sb.WriteRune('d')
		default:
			sb.WriteRune(unicode.ToLower(r))
		}
	}
	return sb.String()
}

// Score chấm điểm khớp dãy con của query trên cand; ok = false nếu không khớp. Query rỗng khớp mọi thứ.
func Score(query, cand string) (int, bool) {
	q, c := []rune(fold(query)), []rune(fold(cand))
	q = trimSpaces(q)
	if len(q) == 0 {
		return 0, true
	}
	score, qi, prev := 0, 0, -2
	for ci := 0; ci < len(c) && qi < len(q); ci++ {
		if c[ci] != q[qi] {
			continue
		}
		pts := 10
		if ci == prev+1 {
			pts += 15 // liên tiếp
		}
		if ci == 0 || c[ci-1] == ' ' || c[ci-1] == '-' || c[ci-1] == '.' || c[ci-1] == '/' {
			pts += 12 // đầu từ
		}
		if prev >= 0 && ci > prev+1 {
			pts -= min(ci-prev-1, 5) // khoảng trống giữa hai ký tự khớp
		}
		score += pts
		prev = ci
		qi++
	}
	if qi < len(q) {
		return 0, false
	}
	score -= len(c) / 8 // chuỗi ngắn hơn được ưu tiên
	return score, true
}

func trimSpaces(r []rune) []rune {
	out := r[:0:0]
	for _, x := range r {
		if x != ' ' {
			out = append(out, x)
		}
	}
	return out
}

// ScoreItem chấm điểm item: tiêu đề tính nặng nhất, rồi nhóm, rồi từ khoá (đường lệnh, cờ).
func ScoreItem(query string, it Item) (int, bool) {
	if strings.TrimSpace(query) == "" {
		return 0, true
	}
	best, found := 0, false
	for _, f := range []struct {
		s string
		w int
	}{{it.Title, 40}, {it.Group + " " + it.Title, 20}, {it.Keywords, 0}} {
		if f.s == "" {
			continue
		}
		if sc, ok := Score(query, f.s); ok {
			if sc+f.w > best || !found {
				best = sc + f.w
			}
			found = true
		}
	}
	return best, found
}

// RowZone là id vùng bấm của dòng i trong danh sách đã khớp ("pal:<i>").
func RowZone(i int) string { return "pal:" + strconv.Itoa(i) }

// ParseRowZone trả chỉ số dòng của id vùng "pal:<i>".
func ParseRowZone(id string) (int, bool) {
	rest, ok := strings.CutPrefix(id, "pal:")
	if !ok {
		return 0, false
	}
	i, err := strconv.Atoi(rest)
	return i, err == nil && i >= 0
}

// SetCursor đặt con trỏ ở dòng i (nếu hợp lệ).
func (m *Model) SetCursor(i int) {
	if i >= 0 && i < len(m.matches) {
		m.Cursor = i
	}
}

// View vẽ bảng lệnh trong khung rộng w, cao h. title là tiêu đề khung; empty là chuỗi khi không khớp.
func (m Model) View(th theme.Theme, w, h int, title, empty string) string {
	iw, ih := frame.Inner(w, h)
	lines := []string{th.S(theme.Key, ":") + " " + text.NFC(m.Query) + th.S(theme.Muted, "▏"), ""}
	rows := max(ih-len(lines), 1)
	if len(m.matches) == 0 {
		lines = append(lines, th.S(theme.Muted, empty))
	}
	// Cửa sổ cuộn quanh con trỏ.
	start := 0
	if m.Cursor >= rows {
		start = m.Cursor - rows + 1
	}
	for i := start; i < len(m.matches) && i < start+rows; i++ {
		it := m.matches[i].Item
		selected := i == m.Cursor
		right := it.Key
		if it.Locked != "" {
			right = it.Locked
		}
		rightW := text.Width(right)
		group := ""
		if it.Group != "" {
			group = it.Group + " › "
		}
		// Nhãn bên trái: nhóm (muted) + tiêu đề; cắt theo chỗ còn lại sau phần phím bên phải.
		avail := max(iw-2-rightW-1, 1)
		full := text.Truncate(group+it.Title, avail)
		gap := strings.Repeat(" ", max(iw-2-text.Width(full)-rightW, 1))
		var left string
		if selected {
			left = th.Selected(full)
		} else if strings.HasPrefix(full, text.NFC(group)) && group != "" {
			left = th.S(theme.Muted, text.NFC(group)) + strings.TrimPrefix(full, text.NFC(group))
		} else {
			left = full
		}
		role := theme.Key
		if it.Locked != "" {
			role = theme.Warn
		}
		marker := "  "
		if selected {
			marker = th.S(theme.Accent, "▸ ")
		}
		lines = append(lines, zone.Mark(RowZone(i), marker+left+gap+th.S(role, right)))
	}
	return frame.Box{Title: title, TitleAt: frame.Left, Focused: true}.Render(th, w, h, strings.Join(lines, "\n"))
}
