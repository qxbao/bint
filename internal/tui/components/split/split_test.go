package split

import (
	"strings"
	"testing"
)

func pane(ch string) func(int) string {
	return func(w int) string { return strings.Repeat(ch, w) + "\n" + strings.Repeat(ch, w) }
}

func TestRatioClampAndReset(t *testing.T) {
	s := New(50)
	for range 20 {
		s.Grow()
	}
	if s.Percent() != MaxPct {
		t.Fatalf("Grow phải dừng ở %d, có %d", MaxPct, s.Percent())
	}
	for range 20 {
		s.Shrink()
	}
	if s.Percent() != MinPct {
		t.Fatalf("Shrink phải dừng ở %d, có %d", MinPct, s.Percent())
	}
	if !s.Reset() || s.Percent() != 50 || s.Custom() {
		t.Fatalf("Reset: %d custom=%v", s.Percent(), s.Custom())
	}
}

func TestRenderSideZoomNarrowAndDrag(t *testing.T) {
	s := New(50)
	out := s.Render(140, false, pane("L"), pane("R"))
	if l := strings.Split(out, "\n")[0]; strings.Count(l, "L") != 70 || strings.Count(l, "R") != 70 {
		t.Fatalf("chia 50%%: %q", l)
	}
	if col, ok := s.Divider(); !ok || col != 69 {
		t.Fatalf("Divider = %d %v", col, ok)
	}
	if !s.DragTo(79) || s.LeftWidth(140) < 79 || s.LeftWidth(140) > 81 {
		t.Fatalf("kéo tới cột 79: pane trái %d (pct %d)", s.LeftWidth(140), s.Percent())
	}
	s.ToggleZoom()
	if out := s.Render(140, true, pane("L"), pane("R")); strings.Contains(out, "L") || !s.Single() {
		t.Fatalf("phóng to pane phải: %q", out)
	}
	s.ToggleZoom()
	if out := s.Render(80, false, pane("L"), pane("R")); strings.Contains(out, "R") {
		t.Fatalf("hẹp chỉ hiện pane đang chọn: %q", out)
	}
}
