// Package split vẽ hai pane cạnh nhau với tỉ lệ chỉnh được (phần trăm độ rộng pane trái), phóng to một pane và kéo đường chia
// bằng chuột. Mỗi màn hai pane giữ một State; mô hình gốc xử phím (<, >, z, =) và chuột qua State, màn chỉ gọi Render.
package split

import (
	"strings"
)

// Giới hạn và bước của tỉ lệ (phần trăm độ rộng pane trái).
const (
	MinPct  = 20
	MaxPct  = 80
	StepPct = 5
	// WideW: hẹp hơn chiều rộng này thì chỉ hiện một pane (pane đang chọn); Tab/Enter đổi pane như trước.
	WideW = 100
	// MinPane là độ rộng tối thiểu của mỗi pane khi hiện cạnh nhau.
	MinPane = 30
)

// State là trạng thái chia của một màn: tỉ lệ hiện tại, mặc định của màn, và pane đang phóng to.
type State struct {
	Def  int  // tỉ lệ mặc định của màn (phần trăm)
	Pct  int  // tỉ lệ hiện tại; 0 = Def
	Zoom bool // phóng to pane đang chọn (không lưu)

	lastW, lastLeft int // độ rộng vùng và pane trái của lần vẽ gần nhất (cho chuột); lastLeft = 0 khi không chia
}

// New dựng State với tỉ lệ mặc định def.
func New(def int) State { return State{Def: clamp(def)} }

func clamp(p int) int { return max(MinPct, min(MaxPct, p)) }

// Percent là tỉ lệ đang áp dụng.
func (s *State) Percent() int {
	if s.Pct == 0 {
		return clamp(s.Def)
	}
	return clamp(s.Pct)
}

// Set đặt tỉ lệ (kẹp vào [MinPct, MaxPct]); trả true nếu tỉ lệ đổi.
func (s *State) Set(p int) bool {
	old := s.Percent()
	s.Pct = clamp(p)
	return s.Percent() != old
}

// Grow / Shrink đổi tỉ lệ một bước; Reset về mặc định; ToggleZoom bật/tắt phóng to. Trả true nếu bố cục đổi.
func (s *State) Grow() bool   { return s.Set(s.Percent() + StepPct) }
func (s *State) Shrink() bool { return s.Set(s.Percent() - StepPct) }
func (s *State) Reset() bool {
	changed := s.Percent() != clamp(s.Def) || s.Zoom
	s.Pct, s.Zoom = 0, false
	return changed
}
func (s *State) ToggleZoom() { s.Zoom = !s.Zoom }

// Custom cho biết tỉ lệ khác mặc định (cần lưu).
func (s *State) Custom() bool { return s.Percent() != clamp(s.Def) }

// LeftWidth là độ rộng pane trái khi vùng rộng w ô hiện hai pane.
func (s *State) LeftWidth(w int) int {
	lw := w * s.Percent() / 100
	return max(MinPane, min(lw, w-MinPane))
}

// Side cho biết vùng rộng w hiện hai pane cạnh nhau.
func Side(w int) bool { return w >= WideW }

// Divider trả cột của đường chia (viền phải pane trái) ở lần vẽ gần nhất; ok = false khi không chia.
func (s *State) Divider() (col int, ok bool) {
	if s.lastLeft == 0 {
		return 0, false
	}
	return s.lastLeft - 1, true
}

// DragTo đặt đường chia tại cột x (x là cột của viền phải pane trái); trả true nếu tỉ lệ đổi. Không chia thì không làm gì.
func (s *State) DragTo(x int) bool {
	if s.lastLeft == 0 || s.lastW <= 0 {
		return false
	}
	lw := max(MinPane, min(x+1, s.lastW-MinPane))
	// Tỉ lệ nguyên gần nhất cho pane trái rộng lw ô (tỉ lệ là số nguyên nên có thể lệch tối đa ~1 ô).
	p, best := MinPct, 1<<30
	for q := MinPct; q <= MaxPct; q++ {
		if d := abs(s.lastW*q/100 - lw); d < best {
			p, best = q, d
		}
	}
	return s.Set(p)
}

// Render vẽ vùng rộng w: rightFocused cho biết pane phải đang chọn (dùng khi phóng to và khi hẹp chỉ hiện một pane). left/right
// vẽ pane với độ rộng cho trước (cùng số dòng).
func (s *State) Render(w int, rightFocused bool, left, right func(w int) string) string {
	s.lastW, s.lastLeft = w, 0
	if !Side(w) || s.Zoom {
		if rightFocused {
			return right(w)
		}
		return left(w)
	}
	lw := s.LeftWidth(w)
	s.lastLeft = lw
	l, r := strings.Split(left(lw), "\n"), strings.Split(right(w-lw), "\n")
	n := max(len(l), len(r))
	out := make([]string, n)
	for i := range n {
		var a, b string
		if i < len(l) {
			a = l[i]
		}
		if i < len(r) {
			b = r[i]
		}
		out[i] = a + b
	}
	return strings.Join(out, "\n")
}

// Single cho biết lần vẽ gần nhất chỉ hiện một pane (hẹp hoặc phóng to).
func (s *State) Single() bool { return s.lastLeft == 0 }

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
