// Package frame vẽ khung viền mảnh góc vuông có tiêu đề nằm trên đường viền trên và nhãn (thường là
// các phím tắt) trên đường viền dưới — thư viện Lip Gloss không vẽ sẵn nên tự vẽ ở đây:
//
//	┌─────────────│Tri thức│─────────────┐
//	│ nội dung                           │
//	└─[Enter→ Mở] [a→ Duyệt] [q→ Thoát]──┘
//
// Độ rộng đo bằng text.Width (NFC + độ rộng ô) nên chuỗi tiếng Việt có dấu căn đúng cột.
package frame

import (
	"strings"

	"bint/internal/tui/components/footer"
	"bint/internal/tui/components/text"
	"bint/internal/tui/components/zone"
	"bint/internal/tui/theme"
)

// Align là vị trí của nhãn trên đường viền.
type Align int

const (
	Left Align = iota
	Center
	Right
)

// Box mô tả một khung.
type Box struct {
	// Title là tiêu đề thuần (không mã ANSI) trên viền trên; được bọc trong │…│.
	Title   string
	TitleAt Align
	// Top là nhãn đã tô màu đặt thêm ở viền trên, phía đối diện tiêu đề (rỗng = không có).
	Top string
	// Bottom là nhãn đã tô màu trên viền dưới (thường là chân phím).
	Bottom   string
	BottomAt Align
	// BottomHints là chân phím kiểu Plain tự chọn theo độ rộng khung (rút nhãn, rồi bỏ phím ưu tiên thấp) khi Bottom rỗng:
	// không bao giờ bị cắt giữa một phím, và mỗi phím bấm được bằng chuột.
	BottomHints []footer.Hint
	// Pane đặt tên khung cho chuột: mọi dòng của khung (kể cả viền) mang vùng "p:<Pane>", để bánh xe cuộn đúng khung dưới con trỏ.
	Pane string
	// Focused: viền dùng màu accent thay vì muted.
	Focused bool
}

// Inner trả kích thước vùng nội dung của khung rộng w, cao h: trừ viền và lề trong 1 cột.
func Inner(w, h int) (iw, ih int) { return max(w-4, 0), max(h-2, 0) }

// Render vẽ khung rộng w, cao h chứa body. Mọi dòng của kết quả rộng đúng w ô; body bị cắt/đệm cho
// vừa vùng nội dung. w < 4 hoặc h < 2: trả chuỗi rỗng.
func (b Box) Render(th theme.Theme, w, h int, body string) string {
	if w < 4 || h < 2 {
		return ""
	}
	role := theme.Muted
	if b.Focused {
		role = theme.Accent
	}
	bs := func(s string) string { return th.S(role, s) }

	bottom := b.Bottom
	if bottom == "" && len(b.BottomHints) > 0 {
		bottom = footer.Inline(th, b.BottomHints, max(w-4, 0))
	}
	var out []string
	out = append(out, b.edge(th, bs, w, "┌", "┐", b.titleLabel(th), b.TitleAt, b.Top))
	iw, ih := Inner(w, h)
	lines := text.Lines(body)
	for i := 0; i < ih; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		out = append(out, bs("│")+" "+text.Fit(line, iw)+" "+bs("│"))
	}
	out = append(out, b.edge(th, bs, w, "└", "┘", bottom, b.BottomAt, ""))
	if b.Pane != "" {
		for i := range out {
			out[i] = zone.Mark("p:"+b.Pane, out[i])
		}
	}
	return strings.Join(out, "\n")
}

// titleLabel dựng │Tiêu đề│ (viền do khung tô, chữ đậm; khung focus ở chế độ đơn sắc đảo màu).
func (b Box) titleLabel(th theme.Theme) string {
	if b.Title == "" {
		return ""
	}
	if b.Focused && !th.Colored() {
		// NO_COLOR: màu viền không còn phân biệt khung đang focus; tiêu đề đậm + đảo màu thay cho nó (kiểu chữ, không phải màu).
		return "\x00" + th.Selected(text.NFC(b.Title)) + "\x00"
	}
	return "\x00" + th.Bold(text.NFC(b.Title)) + "\x00"
}

// edge dựng một đường viền ngang (trên hoặc dưới). label đặt theo align; other (nếu có) đặt ở phía
// đối diện. Dấu \x00 trong label đánh dấu vị trí cần vẽ │ (xem titleLabel).
func (b Box) edge(th theme.Theme, bs func(string) string, w int, l, r, label string, align Align, other string) string {
	inner := w - 2
	const margin = 1
	avail := max(inner-2*margin, 0)
	seg := func(s string) (string, int) {
		if s == "" {
			return "", 0
		}
		bar := strings.Contains(s, "\x00")
		s = strings.ReplaceAll(s, "\x00", "")
		extra := 0
		if bar {
			extra = 2
		}
		s = text.Truncate(s, max(avail-extra, 0))
		wd := text.Width(s) + extra
		if bar {
			s = bs("│") + s + bs("│")
		}
		return s, wd
	}
	ls, lw := seg(label)
	os, ow := seg(other)
	if os != "" && lw+ow+1 > avail { // thiếu chỗ cho nhãn phụ: rút gọn (có …) thay vì bỏ mất; không còn chỗ tối thiểu mới bỏ
		if room := avail - lw - 1; room >= 8 {
			os = text.Truncate(os, room)
			ow = text.Width(os)
		} else {
			os, ow = "", 0
		}
	}
	dash := func(n int) string { return bs(strings.Repeat("─", max(n, 0))) }

	// Vị trí (cột bắt đầu trong vùng inner) của nhãn chính.
	start := 0
	switch align {
	case Center:
		start = (inner - lw) / 2
	case Right:
		start = inner - lw - margin
	default:
		start = margin
	}
	start = max(min(start, inner-lw), 0)
	if os != "" && align != Right { // tiêu đề căn giữa/trái đẩy sang trái đủ chỗ cho nhãn phụ thay vì làm dòng tràn
		start = max(min(start, inner-margin-ow-1-lw), margin)
	}

	var sb strings.Builder
	sb.WriteString(bs(l))
	cur := 0
	put := func(s string, wd, at int) {
		if at > cur {
			sb.WriteString(dash(at - cur))
			cur = at
		}
		sb.WriteString(s)
		cur += wd
	}
	if os != "" {
		oat := inner - ow - margin
		if align == Right { // nhãn phụ ở phía trái khi nhãn chính ở phải
			put(os, ow, margin)
			put(ls, lw, start)
		} else {
			put(ls, lw, start)
			put(os, ow, max(oat, cur+1))
		}
	} else {
		put(ls, lw, start)
	}
	if cur < inner {
		sb.WriteString(dash(inner - cur))
	}
	sb.WriteString(bs(r))
	return sb.String()
}
