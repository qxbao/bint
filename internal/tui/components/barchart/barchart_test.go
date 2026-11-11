package barchart

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
)

var rows = []Row{
	{Label: "none", Value: 0.5, Lo: 0.25, Hi: 0.75, Valid: true, Note: "n=10"},
	{Label: "bint", Value: 1, Lo: 0.7, Hi: 1, Valid: true, Note: "n=10"},
	{Label: "Nhánh tối giản có dấu", Value: 0, Lo: 0, Hi: 0.3, Valid: true},
	{Label: "minimal", Value: 0.3333, Valid: false},
}

// Không dòng nào rộng quá width ở mọi độ rộng từ 12 tới 140, kể cả nhãn có dấu, nhãn dài và locale giả dài hơn.
func TestRenderNeverExceedsWidth(t *testing.T) {
	long := append([]Row{}, rows...)
	long = append(long, Row{Label: "⟦Ñhánh ầ ẳ ẵ — rất dài rất dài rất dài rất dài⟧", Value: 0.5, Lo: 0.1, Hi: 0.9, Valid: true, Note: "⟦n=10 — chú thích dài⟧"})
	for _, mode := range []theme.Mode{theme.Dark, theme.Mono} {
		th := theme.New(mode)
		for w := 12; w <= 140; w++ {
			lines := Render(th, long, w)
			if len(lines) != len(long) {
				t.Fatalf("w=%d: %d dòng", w, len(lines))
			}
			for _, l := range lines {
				if got := text.Width(ansi.Strip(l)); got > w {
					t.Fatalf("w=%d mode=%v: dòng rộng %d: %q", w, mode, got, ansi.Strip(l))
				}
			}
		}
	}
}

// Nhãn thẳng cột (đo theo độ rộng hiển thị) nên thanh bắt đầu cùng một cột ở mọi hàng.
func TestBarsAlignWithAccentedLabels(t *testing.T) {
	th := theme.New(theme.Mono)
	lines := Render(th, rows, 80)
	col := -1
	for _, l := range lines {
		s := ansi.Strip(l)
		i := strings.IndexAny(s, "█░[┃")
		if i < 0 {
			t.Fatalf("không thấy thanh: %q", s)
		}
		c := text.Width(s[:i])
		if col >= 0 && c != col {
			t.Fatalf("thanh lệch cột: %d ≠ %d\n%s", c, col, strings.Join(lines, "\n"))
		}
		col = c
	}
}

// Biên 0% và 100%, khoảng tin cậy chạm biên, khoảng không hợp lệ không vẽ mốc và không in khoảng.
func TestBoundsAndInvalidInterval(t *testing.T) {
	th := theme.New(theme.Mono)
	got := Render(th, rows, 80)
	for i, want := range []string{"50%", "100%", "0%", "33%"} {
		if !strings.Contains(ansi.Strip(got[i]), want) {
			t.Errorf("hàng %d thiếu %q: %q", i, want, ansi.Strip(got[i]))
		}
	}
	if !strings.Contains(ansi.Strip(got[1]), "[ 70–100]") {
		t.Errorf("khoảng tin cậy hàng 100%%: %q", ansi.Strip(got[1]))
	}
	if s := ansi.Strip(got[3]); strings.ContainsAny(s, "[]┃") || strings.Contains(s, "–") {
		t.Errorf("khoảng không hợp lệ không được vẽ: %q", s)
	}
	// Hàng 100%: phần đạt là toàn bộ thanh (trừ ô mốc), không có khối rỗng.
	if s := ansi.Strip(got[1]); strings.Contains(s, "░") {
		t.Errorf("100%% không có khối rỗng: %q", s)
	}
	// Hàng 0%: không có khối đặc.
	if s := ansi.Strip(got[2]); strings.Contains(s, "█") {
		t.Errorf("0%% không có khối đặc: %q", s)
	}
}

// Giá trị ngoài [0,1] và NaN được kẹp, không làm hỏng độ rộng.
func TestClampsOutOfRange(t *testing.T) {
	th := theme.New(theme.Mono)
	nan := 0.0
	nan = nan / nan
	lines := Render(th, []Row{{Label: "a", Value: 2, Lo: -1, Hi: 3, Valid: true}, {Label: "b", Value: nan}}, 50)
	if !strings.Contains(ansi.Strip(lines[0]), "100%") || !strings.Contains(ansi.Strip(lines[1]), "0%") {
		t.Fatalf("%q", lines)
	}
	if Render(th, nil, 50) != nil || Render(th, rows, 0) != nil {
		t.Fatal("không có hàng hoặc không có chỗ thì không vẽ")
	}
}

// Có màu: phần đạt dùng accent, mốc dùng key; đơn sắc không có mã màu.
func TestColorRoles(t *testing.T) {
	dark := strings.Join(Render(theme.New(theme.Dark), rows[:1], 60), "")
	if !strings.Contains(dark, "\x1b[") {
		t.Fatal("chế độ tối phải có mã màu")
	}
	mono := strings.Join(Render(theme.New(theme.Mono), rows[:1], 60), "")
	if strings.Contains(mono, "\x1b[3") || strings.Contains(mono, "\x1b[9") {
		t.Fatalf("đơn sắc không được có mã màu: %q", mono)
	}
}
