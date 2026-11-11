package tabs

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
)

func items() []Item {
	return []Item{{Label: "Tổng quan", Short: "TQ"}, {Label: "Tri thức", Short: "TT", Badge: 3}, {Label: "Tác vụ", Short: "TV"},
		{Label: "Lượt chạy", Short: "LC"}, {Label: "Đánh giá", Short: "ĐG"}, {Label: "Tiến hoá", Short: "TH"}, {Label: "Hệ thống", Short: "HT"}}
}

func TestRenderFullAndBadge(t *testing.T) {
	out := ansi.Strip(Render(theme.New(theme.Mono), 120, items(), 1, false))
	if !strings.Contains(out, "Tri thức (3)") || !strings.Contains(out, "Tổng quan │ Tri thức") {
		t.Fatalf("%q", out)
	}
}

// Dòng tab không bao giờ vượt chiều rộng; tự rút gọn khi hẹp (không cần compact tường minh).
func TestRenderNeverOverflows(t *testing.T) {
	for _, mode := range theme.Modes {
		for w := 10; w <= 130; w += 7 {
			for _, compact := range []bool{false, true} {
				out := Render(theme.New(mode), w, items(), 3, compact)
				if text.Width(out) > w {
					t.Fatalf("mode=%s w=%d compact=%v: rộng %d", mode, w, compact, text.Width(out))
				}
			}
		}
	}
	narrow := ansi.Strip(Render(theme.New(theme.Mono), 60, items(), 3, false))
	if !strings.Contains(narrow, "Lượt chạy") || !strings.Contains(narrow, "TT (3)") {
		t.Fatalf("hẹp phải tự rút gọn tab chưa chọn: %q", narrow)
	}
}
