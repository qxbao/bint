package frame

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/text/unicode/norm"

	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
)

func plain(s string) string { return ansi.Strip(s) }

// Mọi dòng của khung rộng đúng w ô, với mọi chế độ màu, tiêu đề có dấu (dựng sẵn và tổ hợp) và nhãn dài.
func TestRenderEveryLineHasExactWidth(t *testing.T) {
	titles := []string{"Tri thức", norm.NFD.String("Tri thức"), "Đánh giá · Tiến hoá", strings.Repeat("Rất dài ", 20), ""}
	for _, mode := range theme.Modes {
		th := theme.New(mode)
		for _, title := range titles {
			for _, w := range []int{4, 5, 10, 24, 60, 80, 140} {
				for _, at := range []Align{Left, Center, Right} {
					b := Box{Title: title, TitleAt: at, Top: th.S(theme.Muted, "phụ"), Bottom: th.S(theme.Key, "[Enter→ Mở] [a→ Duyệt]"), BottomAt: at}
					out := b.Render(th, w, 5, "dòng một\nTiếng Việt có dấu: ắ ề ộ\n"+strings.Repeat("x", 300))
					lines := strings.Split(out, "\n")
					if len(lines) != 5 {
						t.Fatalf("w=%d: %d dòng, muốn 5", w, len(lines))
					}
					for i, l := range lines {
						if got := text.Width(l); got != w {
							t.Fatalf("mode=%s title=%q w=%d dòng %d rộng %d: %q", mode, title, w, i, got, plain(l))
						}
					}
				}
			}
		}
	}
}

func TestRenderTitleOnTopBorderAndKeysOnBottom(t *testing.T) {
	th := theme.New(theme.Mono)
	out := plain(Box{Title: "Tri thức", TitleAt: Center, Bottom: "[q→ Thoát]", BottomAt: Left}.Render(th, 40, 4, "nội dung"))
	lines := strings.Split(out, "\n")
	if !strings.HasPrefix(lines[0], "┌") || !strings.HasSuffix(lines[0], "┐") || !strings.Contains(lines[0], "│Tri thức│") {
		t.Fatalf("viền trên: %q", lines[0])
	}
	if !strings.HasPrefix(lines[3], "└─[q→ Thoát]") || !strings.HasSuffix(lines[3], "┘") {
		t.Fatalf("viền dưới: %q", lines[3])
	}
	if !strings.HasPrefix(lines[1], "│ nội dung") {
		t.Fatalf("thân: %q", lines[1])
	}
	// Căn giữa: số gạch hai bên chênh nhau không quá 1.
	l := strings.Index(lines[0], "│Tri")
	r := len([]rune(lines[0])) - 1 - (len([]rune(lines[0][:l])) + len([]rune("│Tri thức│")))
	if d := len([]rune(lines[0][:l])) - 1 - r; d < -1 || d > 1 {
		t.Fatalf("tiêu đề lệch tâm: %q", lines[0])
	}
}

func TestRenderTooSmall(t *testing.T) {
	th := theme.New(theme.Dark)
	if (Box{}).Render(th, 3, 5, "x") != "" || (Box{}).Render(th, 10, 1, "x") != "" {
		t.Fatal("khung quá nhỏ phải rỗng")
	}
}

func TestTopLabelOnOppositeSide(t *testing.T) {
	th := theme.New(theme.Mono)
	out := plain(Box{Title: "bint", TitleAt: Center, Top: "phụ"}.Render(th, 40, 3, ""))
	first := strings.Split(out, "\n")[0]
	if !strings.Contains(first, "│bint│") || !strings.Contains(first, "phụ") {
		t.Fatalf("%q", first)
	}
	if strings.Index(first, "phụ") < strings.Index(first, "bint") {
		t.Fatalf("nhãn phụ phải nằm bên phải tiêu đề: %q", first)
	}
}
