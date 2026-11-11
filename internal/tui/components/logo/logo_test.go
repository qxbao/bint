package logo

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
)

func TestPixelLogoShape(t *testing.T) {
	th := theme.New(theme.Mono)
	ls := Render(th, true)
	if len(ls) != 5 {
		t.Fatalf("%d dòng", len(ls))
	}
	w := text.Width(ls[0])
	for i, l := range ls {
		if text.Width(l) != w {
			t.Fatalf("dòng %d rộng %d ≠ %d", i, text.Width(l), w)
		}
		if strings.Trim(ansi.Strip(l), " █") != "" {
			t.Fatalf("chỉ được dùng █ và khoảng trắng: %q", ansi.Strip(l))
		}
	}
	if w != Width(th, true) {
		t.Fatal("Width")
	}
	// Hình dạng: dòng cuối của "b" và dấu chấm đều có điểm sáng; dòng trên cùng chỉ có thân "b" và chấm của "i".
	if !strings.Contains(ansi.Strip(ls[4]), "██") {
		t.Fatal("dòng cuối phải có nét")
	}
}

func TestTextLogoAndFits(t *testing.T) {
	ls := Render(theme.New(theme.Mono), false)
	if len(ls) != 1 || ansi.Strip(ls[0]) != "bint." {
		t.Fatalf("%q", ls)
	}
	if Fits(99, 30) || Fits(100, 29) || !Fits(100, 30) || !Fits(140, 40) {
		t.Fatal("ngưỡng 100×30")
	}
}
