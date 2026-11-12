package footer

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"bint/internal/tui/components/text"
	"bint/internal/tui/components/zone"
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

// Thiếu chỗ thì rút nhãn TRƯỚC khi bỏ phím: còn đủ phím khi chỉ-còn-phím vừa; thứ tự hiển thị giữ như đầu vào.
func TestFitShortensLabelsBeforeDroppingKeys(t *testing.T) {
	wide := Fit(sample(), 200)
	// 6 phím: 2 ghim + Enter(1), a(2) bắt buộc + 2 phím ưu tiên cao nhất còn lại (/=3, x=5), theo thứ tự đầu vào.
	if keys(wide) != "Enter a x / ? q" {
		t.Fatalf("rộng: %s", keys(wide))
	}
	if wide[0].Label != "Mở" {
		t.Fatalf("đủ chỗ thì giữ nhãn đầy đủ: %+v", wide)
	}
	for w := 27; w < 70; w++ { // chỉ-còn-phím cần 27 ô cho 6 phím
		g := Fit(sample(), w)
		if keys(g) != keys(wide) {
			t.Fatalf("w=%d: còn đủ chỗ cho 6 phím rút nhãn mà vẫn bỏ phím: %s", w, keys(g))
		}
		if widthOf(g) > w {
			t.Fatalf("w=%d tràn %d", w, widthOf(g))
		}
	}
	if g := Fit(sample(), 44); g[0].Label == "Mở" && g[1].Label == "Duyệt" && g[2].Label == "Chọn" {
		t.Fatalf("hẹp phải rút nhãn: %+v", g)
	}
}

// Khi chỉ-còn-phím vẫn tràn thì bỏ phím ưu tiên thấp (số lớn) trước, và không bao giờ giữ x mà bỏ a.
func TestFitDropsLowestPriorityOnlyWhenKeysOnlyOverflow(t *testing.T) {
	for w := 12; w < 120; w++ {
		g := keys(Fit(sample(), w))
		if strings.Contains(g, "x") && !strings.Contains(g, "a") {
			t.Fatalf("w=%d: giữ x mà bỏ a: %s", w, g)
		}
	}
	if g := keys(Fit(sample(), 20)); g != "Enter a ? q" {
		t.Fatalf("hẹp: %s", g)
	}
}

// Phím ghim và phím Priority ≤ MustPriority được giữ đến cùng, kể cả khi vượt MaxHints.
func TestFitKeepsMustHintsBeyondMax(t *testing.T) {
	var hs []Hint
	for _, k := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		hs = append(hs, Hint{Key: k, Label: "Nhãn " + k, Priority: 2})
	}
	hs = append(hs, Hint{Key: "?", Label: "Trợ giúp", Pinned: true}, Hint{Key: "z", Label: "Thấp", Priority: 9})
	g := Fit(hs, 60)
	if len(g) != 9 || strings.Contains(keys(g), "z") {
		t.Fatalf("giữ 8 phím bắt buộc + ghim, bỏ phím thấp: %s", keys(g))
	}
	if widthOf(g) > 60 {
		t.Fatalf("tràn %d", widthOf(g))
	}
}

// Rất hẹp: chỉ còn các phím ghim, nhãn bị rút rồi bỏ; chỉ hiện phím.
func TestFitShortensPinnedLabels(t *testing.T) {
	got := Fit(sample(), 9)
	if keys(got) != "? q" {
		t.Fatalf("%s", keys(got))
	}
	tiny := Fit(sample(), 7)
	if keys(tiny) != "? q" || tiny[0].Label != "" || tiny[1].Label != "" {
		t.Fatalf("%+v", tiny)
	}
}

// Kiểu Plain: đệm một ô hai đầu, hai ô giữa các phím; không bao giờ tràn và không cắt giữa một phím.
func TestInlineNeverOverflowsOrCutsKey(t *testing.T) {
	th := theme.New(theme.Mono)
	for w := 6; w < 100; w++ {
		s := ansi.Strip(Inline(th, sample(), w))
		if text.Width(s) > w {
			t.Fatalf("w=%d tràn: %q", w, s)
		}
		for _, h := range sample() {
			if strings.Contains(s, h.Key+" ") && h.Label != "" && !strings.Contains(s, h.Key+" "+h.Label) && !strings.Contains(s, h.Key+" ") {
				t.Fatalf("w=%d cắt giữa phím: %q", w, s)
			}
		}
	}
	if s := ansi.Strip(Inline(th, []Hint{{Key: "d", Label: "Tài liệu"}, {Key: "e", Label: "Sửa"}}, 80)); s != " d Tài liệu  e Sửa " {
		t.Fatalf("%q", s)
	}
}

func TestKeyMsgAndZones(t *testing.T) {
	for in, want := range map[string]string{"Enter": "enter", "Esc": "esc", "Ctrl+R": "ctrl+r", "ctrl+r": "ctrl+r", "a": "a", "A": "A", "?": "?", "Tab": "tab", "Del": "delete", "Space": "space"} {
		m, ok := KeyMsg(in)
		if !ok || m.String() != want {
			t.Errorf("%q → %q ok=%v, muốn %q", in, m.String(), ok, want)
		}
	}
	for _, bad := range []string{"j/k", ", .", "+/-", "", "Ctrl+Shift+X"} {
		if _, ok := KeyMsg(bad); ok {
			t.Errorf("%q không được nhận", bad)
		}
	}
	zone.Begin()
	defer zone.End()
	out := Render(theme.New(theme.Mono), []Hint{{Key: "a", Label: "Duyệt"}, {Key: "j/k", Label: "Chọn"}})
	clean, zs := zone.Scan(out)
	if len(zs) != 1 || zs[0].ID != "key:a" || ansi.Strip(clean) != "[a→ Duyệt] [j/k→ Chọn]" || zs[0].X0 != 0 || zs[0].X1 != text.Width("[a→ Duyệt]") {
		t.Fatalf("%q %+v", clean, zs)
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
