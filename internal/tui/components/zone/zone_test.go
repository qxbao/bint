package zone

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"bint/internal/tui/components/text"
)

func withActive(t *testing.T) {
	t.Helper()
	Begin()
	t.Cleanup(End)
}

func TestMarkIsIdentityWhenInactive(t *testing.T) {
	if got := Mark("a", "xyz"); got != "xyz" {
		t.Fatalf("%q", got)
	}
}

func TestScanStripsMarkersAndFindsZones(t *testing.T) {
	withActive(t)
	line := "│ " + Mark("tab:0", "Tổng quan") + " │ " + Mark("tab:1", "Tri thức") + " │"
	clean, zs := Scan(line + "\nkhông dấu\n" + Mark("row:l:3", "hàng ba"))
	if strings.Contains(clean, "\x1b") {
		t.Fatalf("còn dấu: %q", clean)
	}
	if len(zs) != 3 {
		t.Fatalf("%+v", zs)
	}
	if zs[0].ID != "tab:0" || zs[0].X0 != 2 || zs[0].X1 != 2+text.Width("Tổng quan") || zs[0].Y != 0 {
		t.Fatalf("%+v", zs[0])
	}
	if zs[1].X0 != zs[0].X1+3 || zs[2].Y != 2 || zs[2].X0 != 0 {
		t.Fatalf("%+v", zs)
	}
	if z, ok := At(zs, 3, 0, nil); !ok || z.ID != "tab:0" {
		t.Fatalf("%+v", z)
	}
	if _, ok := At(zs, 0, 0, nil); ok {
		t.Fatal("ô ngoài vùng")
	}
}

// Dấu sống sót qua text.Fit/Truncate/Overlay và độ rộng ô không đổi; vùng lồng nhau trả vùng hẹp nhất.
func TestMarkSurvivesTextOps(t *testing.T) {
	withActive(t)
	row := Mark("pane:detail", "  "+Mark("row:l:1", "hàng một")+"  ")
	fit := text.Fit(row, 20)
	if text.Width(fit) != 20 {
		t.Fatalf("độ rộng %d", text.Width(fit))
	}
	ov := text.Overlay([]string{fit}, []string{"[]"}, 5, 0)
	clean, zs := Scan(ov[0])
	if ansi.StringWidth(clean) != 20 {
		t.Fatalf("%q", clean)
	}
	if z, ok := At(zs, 2, 0, nil); !ok || z.ID != "row:l:1" {
		t.Fatalf("vùng hẹp nhất: %+v %+v", z, zs)
	}
	cut := text.Truncate(row, 6)
	if _, zs := Scan(cut); len(zs) == 0 {
		t.Fatal("cắt ngang vẫn phải còn vùng")
	}
}

func TestScanUnterminatedRunsToEndOfLine(t *testing.T) {
	_, zs := Scan("ab" + startPrefix + "x" + st + "cdef")
	if len(zs) != 1 || zs[0].X0 != 2 || zs[0].X1 != 6 {
		t.Fatalf("%+v", zs)
	}
	if _, zs := Scan("ab" + endMark + "cd"); len(zs) != 0 {
		t.Fatalf("dấu đóng thừa phải bị bỏ: %+v", zs)
	}
}
