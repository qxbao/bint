package i18n

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Mọi khoá có ở mọi locale, cùng tham số, cùng dạng (chuỗi thường hay số nhiều).
func TestCatalogsAreInSync(t *testing.T) {
	vi, en := MustLoad(Vietnamese), MustLoad(English)
	if len(vi.Keys()) == 0 {
		t.Fatal("danh mục rỗng")
	}
	for _, k := range vi.Keys() {
		if !en.Has(k) {
			t.Errorf("khoá %q có ở vi nhưng thiếu ở en", k)
		}
	}
	for _, k := range en.Keys() {
		if !vi.Has(k) {
			t.Errorf("khoá %q có ở en nhưng thiếu ở vi", k)
		}
	}
	for _, k := range vi.Keys() {
		if !en.Has(k) {
			continue
		}
		if a, b := vi.Params(k), en.Params(k); !slices.Equal(a, b) {
			t.Errorf("khoá %q: tham số vi %v ≠ en %v", k, a, b)
		}
		if vi.IsPlural(k) != en.IsPlural(k) {
			t.Errorf("khoá %q: dạng số nhiều khác nhau giữa vi và en", k)
		}
		if vi.T(k) == "" || en.T(k) == "" {
			t.Errorf("khoá %q: chuỗi rỗng", k)
		}
	}
}

func TestPluralNeedsCount(t *testing.T) {
	// Khoá số nhiều dùng {count} ở mọi dạng nó nhắc tới tham số để người dùng thấy con số.
	for _, lang := range Langs {
		c := MustLoad(lang)
		for _, k := range c.Keys() {
			if c.IsPlural(k) && !slices.Contains(c.Params(k), "count") {
				t.Errorf("%s: khoá số nhiều %q phải dùng {count}", lang, k)
			}
		}
	}
}

func TestLookupAndPlural(t *testing.T) {
	c := &Catalog{lang: "t", msgs: map[string]entry{
		"hello": {text: "Chào {name}, bạn có {n} tin"},
		"items": {plural: true, one: "{count} mục", other: "{count} mục (nhiều)"},
		"only":  {plural: true, other: "{count} cái"},
	}}
	if got := c.T("hello", A{"name": "Ba", "n": 3}); got != "Chào Ba, bạn có 3 tin" {
		t.Fatalf("%q", got)
	}
	if got := c.T("hello"); got != "Chào {name}, bạn có {n} tin" {
		t.Fatalf("tham số thiếu giữ nguyên: %q", got)
	}
	if got := c.N("items", 1); got != "1 mục" {
		t.Fatalf("%q", got)
	}
	if got := c.N("items", 5); got != "5 mục (nhiều)" {
		t.Fatalf("%q", got)
	}
	if got := c.N("only", 1); got != "1 cái" {
		t.Fatalf("thiếu one thì dùng other: %q", got)
	}
	if got := c.T("không.có"); got != Missing("không.có") || got != "!không.có!" {
		t.Fatalf("%q", got)
	}
	if got := c.N("không.có", 2); got != "!không.có!" {
		t.Fatalf("%q", got)
	}
}

// Chuỗi trả ra luôn là NFC, kể cả khi tham số truyền vào ở dạng tổ hợp (dấu rời).
func TestOutputIsNFC(t *testing.T) {
	c := &Catalog{lang: "t", msgs: map[string]entry{"k": {text: "Tên: {x}"}}}
	decomposed := norm.NFD.String("Việt Nam")
	if decomposed == "Việt Nam" {
		t.Fatal("fixture phải ở dạng tổ hợp")
	}
	got := c.T("k", A{"x": decomposed})
	if !norm.NFC.IsNormalString(got) || got != "Tên: Việt Nam" {
		t.Fatalf("%q", got)
	}
}

func TestPseudoLocaleStretches(t *testing.T) {
	en, xx := MustLoad(English), MustLoad(Pseudo)
	if xx.Lang() != Pseudo {
		t.Fatal(xx.Lang())
	}
	if !slices.Equal(en.Keys(), xx.Keys()) {
		t.Fatal("locale giả phải có cùng khoá với en")
	}
	checked := 0
	for _, k := range en.Keys() {
		if !slices.Equal(en.Params(k), xx.Params(k)) {
			t.Errorf("%q: tham số bị đổi ở locale giả", k)
		}
		if len(en.Params(k)) > 0 || en.IsPlural(k) {
			continue
		}
		e, x := en.T(k), xx.T(k)
		if !strings.HasPrefix(x, "⟦") || !strings.HasSuffix(x, "⟧") {
			t.Errorf("%q: thiếu dấu mở/đóng: %q", k, x)
		}
		if float64(utf8.RuneCountInString(x)) < 1.4*float64(utf8.RuneCountInString(e)) {
			t.Errorf("%q: %q chưa dài hơn ~40%% so với %q", k, x, e)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("không có khoá nào để kiểm")
	}
}

func TestLoadUnknownLanguage(t *testing.T) {
	if _, err := Load("fr"); err == nil {
		t.Fatal("ngôn ngữ lạ phải báo lỗi")
	}
}

func TestResolvePrecedence(t *testing.T) {
	cases := []struct {
		name           string
		flag, env, cfg string
		sys            []string
		want           string
	}{
		{"không gì cả → vi", "", "", "", nil, "vi"},
		{"cờ thắng tất cả", "en", "vi", "vi", []string{"vi_VN.UTF-8"}, "en"},
		{"BINT_LANG thắng cấu hình", "", "en", "vi", nil, "en"},
		{"cấu hình thắng LANG", "", "", "en", []string{"vi_VN.UTF-8"}, "en"},
		{"auto bỏ qua cấu hình, dùng LANG", "", "", "auto", []string{"en_US.UTF-8"}, "en"},
		{"LC_ALL trước LANG (thứ tự đưa vào)", "", "", "", []string{"", "en_GB", "vi_VN"}, "en"},
		{"LANG không hỗ trợ → vi", "", "", "", []string{"fr_FR.UTF-8"}, "vi"},
		{"giá trị lạ ở bậc cao bị bỏ qua", "fr", "", "en", nil, "en"},
		{"chuẩn hoá hoa/thường và dấu gạch", "EN-us", "", "", nil, "en"},
		{"locale giả qua cờ", "xx", "", "", nil, "xx"},
		{"locale giả qua BINT_LANG", "", "xx", "", nil, "xx"},
		{"locale giả không nhận từ cấu hình", "", "", "xx", nil, "vi"},
		{"locale giả không nhận từ LANG", "", "", "", []string{"xx"}, "vi"},
	}
	for _, tc := range cases {
		if got := Resolve(tc.flag, tc.env, tc.cfg, tc.sys...); got != tc.want {
			t.Errorf("%s: Resolve = %q, muốn %q", tc.name, got, tc.want)
		}
	}
}
