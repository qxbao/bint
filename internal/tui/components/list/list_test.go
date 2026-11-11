package list

import (
	"fmt"
	"strings"
	"testing"

	"bint/internal/i18n"
	"bint/internal/tui/components/text"
)

// Danh sách 5 000 hàng chỉ gọi hàm vẽ cho các hàng trong vùng nhìn, kể cả khi cuộn.
func TestVirtualListDrawsOnlyVisibleRows(t *testing.T) {
	var m Model
	m.SetN(5000)
	drawn := 0
	row := func(i int, cursor bool) string { drawn++; return fmt.Sprintf("hàng %d", i) }
	const h = 18
	out := m.Render(40, h, row)
	if drawn != h || len(strings.Split(out, "\n")) != h {
		t.Fatalf("vẽ %d hàng, muốn %d", drawn, h)
	}
	for _, step := range []func(){func() { m.Move(7) }, func() { m.Page(1) }, m.End, m.Home, func() { m.MoveTo(2500) }} {
		step()
		drawn = 0
		m.Render(40, h, row)
		if drawn > h {
			t.Fatalf("vẽ %d hàng > chiều cao %d", drawn, h)
		}
		from, to := m.Visible(h)
		if m.Cursor < from || m.Cursor >= to {
			t.Fatalf("con trỏ %d ngoài vùng nhìn [%d,%d)", m.Cursor, from, to)
		}
	}
	m.End()
	if m.Cursor != 4999 || m.Top != 5000-h {
		t.Fatalf("End: cursor=%d top=%d", m.Cursor, m.Top)
	}
}

func TestListCursorClampsAndKeepsAfterShrink(t *testing.T) {
	var m Model
	m.SetN(10)
	m.Visible(4)
	m.Move(100)
	if m.Cursor != 9 {
		t.Fatalf("%d", m.Cursor)
	}
	m.Move(-100)
	if m.Cursor != 0 {
		t.Fatalf("%d", m.Cursor)
	}
	m.MoveTo(8)
	m.SetN(3)
	if m.Cursor != 2 || m.Top > 2 {
		t.Fatalf("sau khi co lại: cursor=%d top=%d", m.Cursor, m.Top)
	}
	m.SetN(0)
	if m.Cursor != 0 {
		t.Fatalf("rỗng: %d", m.Cursor)
	}
	if out := m.Render(10, 3, func(int, bool) string { t.Fatal("không có hàng để vẽ"); return "" }); strings.Count(out, "\n") != 2 {
		t.Fatalf("phải đệm đủ 3 dòng: %q", out)
	}
}

func TestSelection(t *testing.T) {
	s := Selection{}
	s.Toggle("b")
	s.Toggle("a")
	s.Toggle("b")
	if s.Count() != 1 || !s.Has("a") || s.Has("b") {
		t.Fatalf("%v", s)
	}
	s["c"] = true
	if got := s.IDs([]string{"c", "x", "a"}); strings.Join(got, ",") != "c,a" {
		t.Fatalf("%v", got)
	}
	s.Clear()
	if s.Count() != 0 {
		t.Fatal("Clear")
	}
}

// Cột thẳng hàng với chuỗi có dấu và locale giả: mọi hàng rộng đúng tổng độ rộng các cột.
func TestColumnsAlignWithAccentsAndPseudoLocale(t *testing.T) {
	widths := Widths(60, 1, []int{2, 8, 0, 12})
	if sum := widths[0] + widths[1] + widths[2] + widths[3] + 3; sum != 60 {
		t.Fatalf("tổng %d, muốn 60: %v", sum, widths)
	}
	pseudo := i18n.MustLoad(i18n.Pseudo)
	cells := [][]string{
		{"●", "rule", "Endpoint phải có phiên bản /v1 cho mọi tuyến", "api/**"},
		{" ", "lesson", "Ắ ề ộ ữ ợ — chữ có dấu kéo dài hơn khung rất nhiều lần để thử cắt", "internal/db/**"},
		{" ", "fact", pseudo.T("overview.inbox.empty"), "*"},
	}
	for _, c := range cells {
		line := Row(widths, 1, c...)
		if w := text.Width(line); w != 60 {
			t.Errorf("hàng %q rộng %d, muốn 60", line, w)
		}
	}
	// Cột thứ hai bắt đầu cùng một ô ở mọi hàng.
	var starts []int
	for _, c := range cells {
		line := Row(widths, 1, c...)
		starts = append(starts, text.Width(line[:strings.Index(line, c[1])]))
	}
	for _, s := range starts {
		if s != starts[0] {
			t.Fatalf("cột lệch: %v", starts)
		}
	}
}
