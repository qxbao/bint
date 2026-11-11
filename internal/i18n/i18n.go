// Package i18n là danh mục chuỗi giao diện (vi, en) của Bint. Chuỗi nằm trong tệp YAML nhúng
// (locales/<lang>.yaml), tra theo khoá có chấm (knowledge.approve.confirm), tham số có tên
// ({count}) và số nhiều dạng one/other.
//
// Gói tự viết gọn thay vì kéo thư viện lớn; đổi sang golang.org/x/text/message nếu sau này cần
// quy tắc số nhiều phức tạp. Mọi chuỗi trả ra đã chuẩn hoá NFC (đo độ rộng cột ổn định).
package i18n

import (
	"embed"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
	"gopkg.in/yaml.v3"
)

//go:embed locales/*.yaml
var localeFS embed.FS

// Mã ngôn ngữ.
const (
	Vietnamese = "vi"
	English    = "en"
	// Pseudo là locale giả sinh từ tiếng Anh bằng mã (không có tệp): chuỗi dài hơn ~40% và có dấu
	// mở/đóng, để bắt lỗi chữ tràn khung và chuỗi viết thẳng trong mã.
	Pseudo = "xx"
	// Default là ngôn ngữ khi không có chỉ định nào.
	Default = Vietnamese
)

// Langs là các ngôn ngữ thật được hỗ trợ (không gồm Pseudo).
var Langs = []string{Vietnamese, English}

// Supported cho biết lang là ngôn ngữ thật được hỗ trợ.
func Supported(lang string) bool {
	for _, l := range Langs {
		if l == lang {
			return true
		}
	}
	return false
}

// A là tham số có tên của một chuỗi: i18n.A{"count": 3}.
type A map[string]any

// entry là một chuỗi: text (không số nhiều) hoặc one/other.
type entry struct {
	text, one, other string
	plural           bool
}

// Catalog là danh mục chuỗi của một ngôn ngữ.
type Catalog struct {
	lang string
	msgs map[string]entry
}

// Lang trả mã ngôn ngữ của danh mục.
func (c *Catalog) Lang() string { return c.lang }

var paramRE = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Load nạp danh mục của lang ("vi", "en" hoặc Pseudo).
func Load(lang string) (*Catalog, error) {
	if lang == Pseudo {
		en, err := load(English)
		if err != nil {
			return nil, err
		}
		return en.pseudo(), nil
	}
	return load(lang)
}

// MustLoad như Load nhưng panic khi lỗi (tệp nhúng sai là lỗi lập trình, test bắt).
func MustLoad(lang string) *Catalog {
	c, err := Load(lang)
	if err != nil {
		panic(err)
	}
	return c
}

func load(lang string) (*Catalog, error) {
	b, err := localeFS.ReadFile("locales/" + lang + ".yaml")
	if err != nil {
		return nil, fmt.Errorf("i18n: không có ngôn ngữ %q", lang)
	}
	var raw map[string]any
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("i18n: %s.yaml: %w", lang, err)
	}
	c := &Catalog{lang: lang, msgs: make(map[string]entry, len(raw))}
	for k, v := range raw {
		switch t := v.(type) {
		case string:
			c.msgs[k] = entry{text: norm.NFC.String(t)}
		case map[string]any:
			e := entry{plural: true}
			for pk, pv := range t {
				s, ok := pv.(string)
				if !ok {
					return nil, fmt.Errorf("i18n: %s.yaml: %s.%s phải là chuỗi", lang, k, pk)
				}
				switch pk {
				case "one":
					e.one = norm.NFC.String(s)
				case "other":
					e.other = norm.NFC.String(s)
				default:
					return nil, fmt.Errorf("i18n: %s.yaml: %s: dạng số nhiều %q không hỗ trợ (one | other)", lang, k, pk)
				}
			}
			if e.other == "" {
				return nil, fmt.Errorf("i18n: %s.yaml: %s thiếu dạng other", lang, k)
			}
			c.msgs[k] = e
		default:
			return nil, fmt.Errorf("i18n: %s.yaml: %s phải là chuỗi hoặc {one, other}", lang, k)
		}
	}
	return c, nil
}

// pseudo dựng locale giả từ c: mỗi chuỗi dài thêm ~40% và bọc trong ⟦ ⟧; tham số {x} giữ nguyên.
func (c *Catalog) pseudo() *Catalog {
	p := &Catalog{lang: Pseudo, msgs: make(map[string]entry, len(c.msgs))}
	stretch := func(s string) string {
		if s == "" {
			return s
		}
		lit := paramRE.ReplaceAllString(s, "")
		pad := int(math.Ceil(0.4 * float64(utf8.RuneCountInString(lit))))
		return "⟦" + s + strings.Repeat("~", pad) + "⟧"
	}
	for k, e := range c.msgs {
		e.text, e.one, e.other = stretch(e.text), stretch(e.one), stretch(e.other)
		p.msgs[k] = e
	}
	return p
}

// Has cho biết khoá có trong danh mục.
func (c *Catalog) Has(key string) bool { _, ok := c.msgs[key]; return ok }

// Keys trả mọi khoá, sắp tăng dần.
func (c *Catalog) Keys() []string {
	out := make([]string, 0, len(c.msgs))
	for k := range c.msgs {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// IsPlural cho biết khoá là chuỗi có dạng số nhiều.
func (c *Catalog) IsPlural(key string) bool { return c.msgs[key].plural }

// Params trả tên tham số của khoá (hợp của mọi dạng), sắp tăng dần; nil nếu không có khoá.
func (c *Catalog) Params(key string) []string {
	e, ok := c.msgs[key]
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, s := range []string{e.text, e.one, e.other} {
		for _, m := range paramRE.FindAllStringSubmatch(s, -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				out = append(out, m[1])
			}
		}
	}
	sort.Strings(out)
	return out
}

// Missing là dạng hiển thị của khoá không có trong danh mục.
func Missing(key string) string { return "!" + key + "!" }

// T trả chuỗi của khoá với tham số; khoá thiếu trả Missing(key) (test bắt). Khoá có dạng số nhiều
// dùng N; gọi T với khoá đó trả dạng other.
func (c *Catalog) T(key string, args ...A) string {
	e, ok := c.msgs[key]
	if !ok {
		return Missing(key)
	}
	s := e.text
	if e.plural {
		s = e.other
	}
	return expand(s, args)
}

// N trả dạng số nhiều của khoá theo count (1 → one nếu có, ngược lại other); count tự thành tham
// số {count}.
func (c *Catalog) N(key string, count int, args ...A) string {
	e, ok := c.msgs[key]
	if !ok {
		return Missing(key)
	}
	s := e.text
	if e.plural {
		s = e.other
		if count == 1 && e.one != "" {
			s = e.one
		}
	}
	merged := A{"count": count}
	for _, a := range args {
		for k, v := range a {
			merged[k] = v
		}
	}
	return expand(s, []A{merged})
}

func expand(s string, args []A) string {
	if len(args) == 0 || !strings.Contains(s, "{") {
		return norm.NFC.String(s)
	}
	out := paramRE.ReplaceAllStringFunc(s, func(m string) string {
		name := m[1 : len(m)-1]
		for _, a := range args {
			if v, ok := a[name]; ok {
				return fmt.Sprint(v)
			}
		}
		return m
	})
	return norm.NFC.String(out)
}

// Resolve chọn ngôn ngữ giao diện theo thứ tự: cờ --lang > biến BINT_LANG > khoá cấu hình
// ui.language > LC_ALL/LANG (sys, theo thứ tự đưa vào) > vi. Giá trị không hỗ trợ (hoặc "auto",
// rỗng) ở một bậc thì bỏ qua và xét bậc kế. Pseudo chỉ được chọn qua cờ hoặc BINT_LANG.
func Resolve(flag, env, cfg string, sys ...string) string {
	l, _ := ResolveFrom(flag, env, cfg, sys...)
	return l
}

// Nguồn của ngôn ngữ do ResolveFrom chọn.
const (
	FromFlag    = 0  // cờ --lang
	FromEnv     = 1  // biến BINT_LANG
	FromConfig  = 2  // khoá ui.language
	FromDefault = -1 // không bậc nào hợp lệ: ngôn ngữ mặc định
	// Từ 3 trở đi là các biến hệ thống theo thứ tự đưa vào (LC_ALL, LC_MESSAGES, LANG).
)

// ResolveFrom như Resolve nhưng trả thêm bậc đã quyết định (FromFlag, FromEnv, FromConfig, 3+ cho biến hệ thống, FromDefault).
func ResolveFrom(flag, env, cfg string, sys ...string) (lang string, from int) {
	for i, v := range append([]string{flag, env, cfg}, sys...) {
		l := normalizeLang(v)
		if Supported(l) || (l == Pseudo && i < 2) {
			return l, i
		}
	}
	return Default, FromDefault
}

// normalizeLang đưa "vi_VN.UTF-8", "en-US", "EN" về "vi", "en".
func normalizeLang(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if i := strings.IndexAny(v, "_.-@:"); i >= 0 {
		v = v[:i]
	}
	return v
}
