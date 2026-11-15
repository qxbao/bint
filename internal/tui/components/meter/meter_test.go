package meter

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
)

var modes = []theme.Mode{theme.Dark, theme.ANSI16, theme.Mono}

func TestBarAndStackedWidthExact(t *testing.T) {
	segs := []Seg{{N: 7, Role: theme.OK}, {N: 1, Role: theme.Err}, {N: 2, Role: theme.Warn}, {N: 0, Role: theme.Key}}
	for _, m := range modes {
		th := theme.New(m)
		for _, w := range []int{1, 3, 8, 20, 40, 80} {
			for _, r := range []float64{0, 0.001, 0.5, 1, 2, -1} {
				if got := text.Width(ansi.Strip(Bar(th, r, w, theme.OK))); got != w {
					t.Fatalf("Bar w=%d r=%v: %d", w, r, got)
				}
			}
			if got := text.Width(ansi.Strip(Stacked(th, segs, w))); got != w {
				t.Fatalf("Stacked w=%d: %d", w, got)
			}
			if got := text.Width(ansi.Strip(Stacked(th, nil, w))); got != w {
				t.Fatalf("Stacked rỗng w=%d: %d", w, got)
			}
		}
	}
	if Bar(theme.New(theme.Dark), 0.5, 0, theme.OK) != "" || Stacked(theme.New(theme.Dark), segs, -1) != "" {
		t.Fatal("width <= 0 phải rỗng")
	}
}

func TestMonoHasNoANSIAndDistinguishes(t *testing.T) {
	th := theme.New(theme.Mono)
	segs := []Seg{{N: 5, Role: theme.OK}, {N: 3, Role: theme.Err}, {N: 2, Role: theme.Warn}}
	out := []string{Bar(th, 0.4, 20, theme.OK), Stacked(th, segs, 20), Badge(th, theme.Err, "lỗi")}
	for _, s := range out {
		if strings.Contains(s, "\x1b") {
			t.Fatalf("mono có ANSI: %q", s)
		}
	}
	st := out[1]
	for _, g := range Glyphs(segs) {
		if !strings.Contains(st, g) {
			t.Fatalf("thiếu ký hiệu %q trong %q", g, st)
		}
	}
	if out[2] != "[lỗi]" || BadgeWidth("lỗi") != text.Width(out[2]) {
		t.Fatalf("badge mono: %q", out[2])
	}
	col := Badge(theme.New(theme.Dark), theme.OK, "ok")
	if text.Width(ansi.Strip(col)) != BadgeWidth("ok") || !strings.Contains(col, "\x1b") {
		t.Fatalf("badge màu: %q", col)
	}
}

func TestStackedMinimumOneCell(t *testing.T) {
	th := theme.New(theme.Mono)
	s := Stacked(th, []Seg{{N: 1000, Glyph: "A"}, {N: 1, Glyph: "B"}}, 8)
	if !strings.Contains(s, "B") || strings.Count(s, "A")+strings.Count(s, "B") != 8 {
		t.Fatalf("%q", s)
	}
}

func TestMoney(t *testing.T) {
	v := 0.42
	z := 0.0
	cases := []struct {
		v       *float64
		partial bool
		want    string
	}{{nil, false, "?"}, {nil, true, "?"}, {&v, false, "$0.4200"}, {&v, true, "≥$0.4200"}, {&z, false, "$0.0000"}}
	for _, c := range cases {
		if got := Money(c.v, c.partial); got != c.want {
			t.Fatalf("Money = %q, muốn %q", got, c.want)
		}
	}
}
