package app

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"bint/internal/i18n"
	"bint/internal/tui/components/footer"
	"bint/internal/tui/ui"
)

// allScenarios gộp mọi kịch bản ảnh chụp (màn hình chính và Hệ thống) ở cỡ w×h.
func allScenarios(t *testing.T, lang string, w, h int) map[string]func() *Model {
	out := mainScreens(t, lang, w, h)
	for k, v := range systemScreens(t, lang, w, h) {
		out[k] = v
	}
	return out
}

var dottedRE = regexp.MustCompile(`[a-z][a-z0-9_]*(?:\.[a-z0-9_]+)+`)

// rawKeys trả các chuỗi trong out chính là một khoá i18n còn nguyên (chưa dịch).
func rawKeys(cat *i18n.Catalog, out string) []string {
	var bad []string
	for _, tok := range dottedRE.FindAllString(out, -1) {
		if cat.Has(tok) {
			bad = append(bad, tok)
		}
	}
	return bad
}

// Chế độ hẹp (60×20) và 80×24, vi/en/xx, mọi kịch bản ảnh chụp: mọi dòng rộng đúng W, đủ H dòng, tên thẻ phụ đang chọn hiện đủ, không
// còn khoá i18n thô; chân phím ngoài luôn có ?, q, : và mọi phím ưu tiên ≤ 2 của ngữ cảnh; chân phím trong khung không bị cắt giữa một phím.
func TestNarrowScan(t *testing.T) {
	for _, sz := range [][2]int{{60, 20}, {80, 24}} {
		w, h := sz[0], sz[1]
		for _, lang := range []string{"vi", "en", "xx"} {
			cat := i18n.MustLoad(lang)
			for name, build := range allScenarios(t, lang, w, h) {
				m := build()
				out := plain(m.Render())
				ls := strings.Split(out, "\n")
				tag := fmt.Sprintf("%s %dx%d %s", name, w, h, lang)
				if len(ls) != h {
					t.Errorf("%s: %d dòng, muốn %d", tag, len(ls), h)
				}
				for i, l := range ls {
					if got := ansi.StringWidth(l); got != w {
						t.Errorf("%s: dòng %d rộng %d, muốn %d: %q", tag, i, got, w, l)
					}
				}
				if bad := rawKeys(cat, out); len(bad) > 0 {
					t.Errorf("%s: còn khoá i18n thô %v", tag, bad)
				}
				if st, ok := m.screens[m.Screen()].(ui.SubTabber); ok && m.pal == nil && !m.showHelp {
					if sub := st.SubTab(); sub != "" && !strings.Contains(out, sub) {
						t.Errorf("%s: tên thẻ phụ đang chọn %q không hiện đủ:\n%s", tag, sub, out)
					}
				}
				for i, l := range ls {
					if strings.Contains(l, "…─") || strings.Contains(l, "…┘") {
						t.Errorf("%s: dòng %d (chân phím/viền) bị cắt giữa chừng: %q", tag, i, l)
					}
				}
				checkOuterFooter(t, m, ls, tag)
			}
		}
	}
}

// checkOuterFooter: chân phím khung ngoài (dòng cuối) có ?, q, : và mọi phím Priority ≤ 2 của ngữ cảnh khi màn hình không bị hộp chiếm.
func checkOuterFooter(t *testing.T, m *Model, ls []string, tag string) {
	t.Helper()
	hints := m.Hints()
	pinned := false
	for _, h := range hints {
		pinned = pinned || h.Key == "?"
	}
	if !pinned || m.pal != nil || m.showHelp {
		return
	}
	last := ls[len(ls)-1]
	has := func(key string) bool {
		return strings.Contains(last, "["+key+"→") || strings.Contains(last, "["+key+"]")
	}
	for _, key := range []string{"?", "q", ":"} {
		if !has(key) {
			t.Errorf("%s: chân phím ngoài thiếu %q: %q", tag, key, last)
		}
	}
	for _, h := range hints {
		if (h.Pinned || h.Priority <= footer.MustPriority) && !has(h.Key) {
			t.Errorf("%s: chân phím ngoài thiếu phím ưu tiên %q (%s): %q", tag, h.Key, h.Label, last)
		}
	}
}
