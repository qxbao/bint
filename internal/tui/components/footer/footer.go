// Package footer chọn và vẽ các phím tắt hiện ở viền dưới của khung theo độ ưu tiên: luôn 4–6 phím
// quan trọng nhất của ngữ cảnh hiện tại. Thiếu chỗ thì rút nhãn trước (nhãn ngắn dần, rồi chỉ còn phím)
// để KHÔNG bỏ phím nào; chỉ khi chỉ-còn-phím vẫn tràn mới bỏ phím ưu tiên thấp. Các phím ghim (thường là ? và q)
// và phím ưu tiên ≤ MustPriority không bị bỏ trước các phím khác. Mỗi phím bấm được bằng chuột (vùng "key:<phím>").
package footer

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"bint/internal/tui/components/text"
	"bint/internal/tui/components/zone"
	"bint/internal/tui/theme"
)

// MaxHints là số phím tối đa hiện cùng lúc (phím ghim và phím ưu tiên ≤ MustPriority không bị cắt bởi giới hạn này).
const MaxHints = 6

// MustPriority: phím có Priority ≤ giá trị này (và phím ghim) được giữ đến cùng khi thiếu chỗ.
const MustPriority = 2

// Hint là một phím tắt.
type Hint struct {
	Key   string // hiển thị: "Enter", "a", "?", "Ctrl+R"
	Label string // nhãn đã dịch
	// Priority: số nhỏ = quan trọng hơn (giữ lại trước).
	Priority int
	// Pinned: luôn giữ (kể cả phải rút nhãn).
	Pinned bool
	// Press là phím gửi đi khi bấm chuột (dạng tea.KeyPressMsg.String(): "enter", "ctrl+r", "a"); rỗng = suy ra từ Key
	// (phím ghép như "j/k" không bấm được).
	Press string
	// ActionID là hành động trong sổ mà phím này chạy (rỗng = phím giao diện thuần như j/k, Esc: luôn hiện). Chân phím trong khung tra sổ
	// qua ui.Env.Allowed để bỏ phím của hành động Writes ở chế độ chỉ đọc (màn hình không tự kiểm ReadOnly, mục 7.4).
	ActionID string
}

// Style là kiểu vẽ: Bracket "[a→ Duyệt] [q→ Thoát]" (chân phím khung ngoài), Plain " a Duyệt  q Thoát " (viền dưới khung con).
type Style int

const (
	Bracket Style = iota
	Plain
)

func (h Hint) must() bool { return h.Pinned || h.Priority <= MustPriority }

func (s Style) hintWidth(key, label string) int {
	if s == Plain {
		if label == "" {
			return text.Width(key)
		}
		return text.Width(key) + 1 + text.Width(label)
	}
	if label == "" {
		return text.Width("[" + key + "]")
	}
	return text.Width("[" + key + "→ " + label + "]")
}

// gap là khoảng cách giữa hai phím; pad là phần đệm hai đầu của cả dòng.
func (s Style) gap() int {
	if s == Plain {
		return 2
	}
	return 1
}
func (s Style) pad() int {
	if s == Plain {
		return 2
	}
	return 0
}

// labelCaps là các mức rút nhãn: nhãn đầy đủ (-1), rồi cắt còn từng ấy ô, cuối cùng chỉ còn phím (0).
var labelCaps = []int{-1, 10, 7, 5, 3, 0}

// capLabel rút nhãn l còn tối đa cap ô: ưu tiên bỏ các từ cuối (giữ nguyên từ, không dấu …); từ đầu đã dài hơn cap thì cắt kèm "…".
func capLabel(l string, cap int) string {
	switch {
	case cap < 0 || text.Width(l) <= cap:
		return l
	case cap == 0:
		return ""
	}
	words := strings.Fields(l)
	out := ""
	for _, w := range words {
		next := w
		if out != "" {
			next = out + " " + w
		}
		if text.Width(next) > cap {
			break
		}
		out = next
	}
	if out != "" {
		return out
	}
	return text.Truncate(l, cap)
}

// Fit chọn các phím vừa maxW ô (kể cả khoảng trắng giữa các phím), giữ thứ tự ban đầu, kiểu Bracket.
func Fit(hints []Hint, maxW int) []Hint { return FitStyle(hints, maxW, Bracket) }

// FitStyle chọn các phím vừa maxW ô theo kiểu st. Thứ tự giữ lại khi phải bỏ: không-bắt-buộc theo Priority giảm dần,
// rồi bắt buộc (ghim, Priority ≤ MustPriority) theo Priority giảm dần, ghim cuối cùng. Nhãn của mọi phím giữ lại được rút
// chung một mức (đầy đủ → ngắn dần → chỉ phím) trước khi bỏ bất kỳ phím nào.
func FitStyle(hints []Hint, maxW int, st Style) []Hint {
	if len(hints) == 0 {
		return nil
	}
	n := len(hints)
	// rank: số nhỏ = giữ lại trước.
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	rank := func(i int) [3]int {
		h := hints[i]
		g := 2
		switch {
		case h.Pinned:
			g = 0
		case h.must():
			g = 1
		}
		return [3]int{g, h.Priority, i}
	}
	slices.SortStableFunc(order, func(a, b int) int {
		ra, rb := rank(a), rank(b)
		for k := 0; k < 3; k++ {
			if ra[k] != rb[k] {
				return ra[k] - rb[k]
			}
		}
		return 0
	})
	musts := 0
	for _, h := range hints {
		if h.must() {
			musts++
		}
	}
	limit := min(n, max(MaxHints, musts))
	width := func(idx []int, cap int) int {
		w := st.pad()
		for _, i := range idx {
			w += st.hintWidth(hints[i].Key, capLabel(hints[i].Label, cap))
		}
		return w + st.gap()*max(len(idx)-1, 0)
	}
	build := func(idx []int, cap int) []Hint {
		idx = append([]int(nil), idx...)
		slices.Sort(idx)
		out := make([]Hint, len(idx))
		for k, i := range idx {
			out[k] = hints[i]
			out[k].Label = capLabel(hints[i].Label, cap)
		}
		return out
	}
	for k := limit; k >= 1; k-- {
		idx := order[:k]
		for _, c := range labelCaps {
			if width(idx, c) <= maxW {
				return build(idx, c)
			}
		}
	}
	return build(order[:1], 0)
}

// KeyMsg đổi tên phím (dạng tea.KeyPressMsg.String() hoặc tên hiển thị như "Enter", "Ctrl+R") thành sự kiện phím.
func KeyMsg(s string) (tea.KeyPressMsg, bool) {
	l := strings.ToLower(s)
	named := map[string]rune{"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab, "backspace": tea.KeyBackspace, "bksp": tea.KeyBackspace,
		"delete": tea.KeyDelete, "del": tea.KeyDelete, "up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
		"pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown, "pgdn": tea.KeyPgDown, "home": tea.KeyHome, "end": tea.KeyEnd, "space": tea.KeySpace}
	if c, ok := named[l]; ok {
		if c == tea.KeySpace {
			return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}, true
		}
		return tea.KeyPressMsg{Code: c}, true
	}
	switch {
	case strings.HasPrefix(l, "ctrl+") && len([]rune(l)) == 6:
		return tea.KeyPressMsg{Code: []rune(l)[5], Mod: tea.ModCtrl}, true
	case l == "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, true
	}
	if r := []rune(s); len(r) == 1 && r[0] > ' ' {
		return tea.KeyPressMsg{Code: r[0], Text: s}, true
	}
	return tea.KeyPressMsg{}, false
}

// press trả phím gửi đi khi bấm hint h ("" = không bấm được).
func (h Hint) press() string {
	p := h.Press
	if p == "" {
		p = h.Key
	}
	if msg, ok := KeyMsg(p); ok {
		return msg.String()
	}
	return ""
}

// ZoneID là id vùng bấm của một phím: "key:<phím>".
func ZoneID(press string) string { return "key:" + press }

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
		parts[i] = zone.Mark(ZoneID(h.press()), sb.String())
		if h.press() == "" {
			parts[i] = sb.String()
		}
	}
	return strings.Join(parts, " ")
}

// RenderPlain vẽ kiểu Plain: " a Duyệt  q Thoát " (đệm một khoảng trắng hai đầu); phím màu key, nhãn muted.
func RenderPlain(th theme.Theme, hints []Hint) string {
	if len(hints) == 0 {
		return ""
	}
	parts := make([]string, len(hints))
	for i, h := range hints {
		s := th.S(theme.Key, text.NFC(h.Key))
		if h.Label != "" {
			s += " " + th.S(theme.Muted, text.NFC(h.Label))
		}
		if p := h.press(); p != "" {
			s = zone.Mark(ZoneID(p), s)
		}
		parts[i] = s
	}
	return " " + strings.Join(parts, "  ") + " "
}

// Line chọn rồi vẽ (Bracket): các phím vừa maxW ô.
func Line(th theme.Theme, hints []Hint, maxW int) string {
	return Render(th, Fit(hints, maxW))
}

// Inline chọn rồi vẽ (Plain): các phím vừa maxW ô, kể cả hai khoảng trắng đệm.
func Inline(th theme.Theme, hints []Hint, maxW int) string {
	return RenderPlain(th, FitStyle(hints, maxW, Plain))
}
