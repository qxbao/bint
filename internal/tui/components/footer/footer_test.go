package footer

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
)

func sample() []Hint {
	return []Hint{
		{Key: "Enter", Label: "Mở", Priority: 1},
		{Key: "a", Label: "Duyệt", Priority: 2},
		{Key: "x", Label: "Chọn", Priority: 5},
		{Key: "/", Label: "Tìm", Priority: 3},
		{Key: "n", Label: "Thêm", Priority: 6},
		{Key: "r", Label: "Làm mới", Priority: 7},
		{Key: "e", Label: "Sửa", Priority: 8},
		{Key: "?", Label: "Trợ giúp", Priority: 0, Pinned: true},
		{Key: "q", Label: "Thoát", Priority: 0, Pinned: true},
	}
}

func keys(hs []Hint) string {
	var k []string
	for _, h := range hs {
		k = append(k, h.Key)
	}
	return strings.Join(k, " ")
}

func widthOf(hs []Hint) int {
	th := theme.New(theme.Mono)
	return text.Width(Render(th, hs))
}

// Không bao giờ quá MaxHints; luôn giữ ? và q; kết quả vừa chiều rộng.
func TestFitAcrossWidths(t *testing.T) {
	for w := 12; w <= 140; w += 4 {
		got := Fit(sample(), w)
		if len(got) > MaxHints {
			t.Fatalf("w=%d: %d phím > %d", w, len(got), MaxHints)
		}
		if !strings.Contains(keys(got), "?") || !strings.Contains(keys(got), "q") {
			t.Fatalf("w=%d mất phím ghim: %s", w, keys(got))
		}
		if w >= 16 && widthOf(got) > w {
			t.Fatalf("w=%d: tràn %d: %s", w, widthOf(got), keys(got))
		}
	}
}

// Thiếu chỗ thì bỏ phím ưu tiên thấp (số lớn) trước; thứ tự hiển thị giữ như đầu vào.
func TestFitDropsLowestPriorityFirstKeepsOrder(t *testing.T) {
	wide := Fit(sample(), 200)
	// 6 phím: 2 ghim + 4 ưu tiên cao nhất (Enter=1, a=2, /=3, x=5), theo thứ tự đầu vào.
	if keys(wide) != "Enter a x / ? q" {
		t.Fatalf("rộng: %s", keys(wide))
	}
	mid := Fit(sample(), 44)
	if !strings.Contains(keys(mid), "Enter") || strings.Contains(keys(mid), "x") && !strings.Contains(keys(mid), "a") {
		t.Fatalf("hẹp: %s", keys(mid))
	}
	if keys(mid) == keys(wide) {
		t.Fatalf("hẹp phải bỏ bớt phím: %s", keys(mid))
	}
	// x (ưu tiên 5) bỏ trước a (ưu tiên 2).
	for w := 30; w < 120; w++ {
		g := keys(Fit(sample(), w))
		if strings.Contains(g, "x") && !strings.Contains(g, "a") {
			t.Fatalf("w=%d: giữ x mà bỏ a: %s", w, g)
		}
	}
}

// Rất hẹp: chỉ còn các phím ghim, nhãn bị rút rồi bỏ; chỉ hiện phím.
func TestFitShortensPinnedLabels(t *testing.T) {
	got := Fit(sample(), 22)
	if keys(got) != "? q" {
		t.Fatalf("%s", keys(got))
	}
	if w := widthOf(got); w > 22 {
		t.Fatalf("tràn %d", w)
	}
	tiny := Fit(sample(), 7)
	if keys(tiny) != "? q" || tiny[0].Label != "" || tiny[1].Label != "" {
		t.Fatalf("%+v", tiny)
	}
}

func TestFitEmpty(t *testing.T) {
	if Fit(nil, 80) != nil {
		t.Fatal("rỗng")
	}
	if got := Fit([]Hint{{Key: "q", Label: "Thoát", Pinned: true}}, 80); len(got) != 1 || got[0].Label != "Thoát" {
		t.Fatalf("%+v", got)
	}
}

func TestRenderMonoHasBracketsAndArrows(t *testing.T) {
	s := ansi.Strip(Render(theme.New(theme.Mono), []Hint{{Key: "a", Label: "Duyệt"}, {Key: "q"}}))
	if !strings.Contains(s, "[a→ Duyệt]") || !strings.HasSuffix(s, "[q]") {
		t.Fatalf("%q", s)
	}
}
