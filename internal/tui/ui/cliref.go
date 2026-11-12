package ui

import (
	"regexp"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"bint/internal/i18n"
	"bint/internal/tui/actions"
)

var defaultReg = sync.OnceValue(actions.Default)

// Rewriter viết lại các đoạn nhắc lệnh CLI ("bint knowledge verify --apply") trong thông điệp của tầng dịch vụ thành tham chiếu
// TUI tương ứng (màn, thẻ phụ, phím hoặc mục bảng lệnh, ô form của cờ). Mỗi mô hình gốc giữ một Rewriter của riêng nó (danh mục chuỗi,
// sổ hành động, chế độ chỉ đọc) và truyền cho mọi màn hình: không có trạng thái toàn cục của gói, nên hai mô hình trong cùng tiến trình
// không ảnh hưởng nhau.
type Rewriter struct {
	Cat *i18n.Catalog
	// Reg là sổ hành động để tra phím; nil = sổ mặc định (tiện cho test đơn vị; mô hình gốc luôn truyền sổ của nó).
	Reg *actions.Registry
	// ReadOnly: TUI ở chế độ chỉ đọc; tham chiếu tới hành động ghi nói rõ là không dùng được.
	ReadOnly bool
}

func (r Rewriter) reg() *actions.Registry {
	if r.Reg != nil {
		return r.Reg
	}
	return defaultReg()
}

// Rewrite viết lại thông điệp theo ngôn ngữ của r.Cat. Chỉ dùng cho thông điệp (lỗi, lý do, gợi ý, kết quả); nội dung tệp, diff,
// Markdown và log của agent không đi qua đây. Đường lệnh không có hành động trong sổ ("bint hook", "bint mcp serve") và chữ "bint"
// không kèm lệnh được giữ nguyên; cụm cờ rời của tầng dịch vụ ("hoặc dùng --unverified") được thay bằng cụm TUI tương ứng.
func (r Rewriter) Rewrite(s string) string {
	s = r.rewritePhrases(s)
	if !strings.Contains(s, "bint") {
		return s
	}
	var out strings.Builder
	i := 0
	for from := 0; ; {
		j := strings.Index(s[from:], "bint ")
		if j < 0 {
			break
		}
		at := from + j
		from = at + 5
		if at > 0 && (isWordByte(s[at-1]) || s[at-1] == '.' || s[at-1] == '/') {
			continue
		}
		c, ok := parseCLI(s, at)
		if !ok || at < i {
			continue
		}
		prefix := s[i:at]
		prefix = strings.TrimSuffix(prefix, "`")
		lead, verb := "", false
		for _, v := range cliVerbs {
			n := len(v.word)
			if len(prefix) > n && prefix[len(prefix)-1] == ' ' && strings.EqualFold(prefix[len(prefix)-1-n:len(prefix)-1], v.word) &&
				(len(prefix) == n+1 || !isLetterBefore(prefix, len(prefix)-1-n)) {
				prefix, lead, verb = prefix[:len(prefix)-1-n], v.lead, v.verb
				break
			}
		}
		out.WriteString(prefix)
		out.WriteString(lead)
		out.WriteString(c.render(r, verb))
		i, from = c.end, c.end
	}
	out.WriteString(s[i:])
	return out.String()
}

func isWordByte(b byte) bool {
	return b == '_' || b == '-' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= 0x80
}

func isLetterBefore(s string, i int) bool {
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// cliCmd là một lệnh CLI đã đọc từ thông điệp.
type cliCmd struct {
	end   int      // vị trí ngay sau lệnh (đã gồm dấu huyền đóng)
	paths []string // đường lệnh ("knowledge reject", "knowledge deprecate" cho "reject/deprecate")
	flags []string
}

var (
	wordRE = regexp.MustCompile(`^[a-z][a-z-]*`)
	flagRE = regexp.MustCompile("--[a-z][a-z-]*")
	// Mã có tiền tố ("ep_01M7…", "eb_123"): phần sau gạch dưới phải có ít nhất một chữ số (ULID, số thứ tự), để từ thường có gạch dưới
	// ("task_id", "key_name") đứng sau lệnh không bị nuốt vào tham chiếu.
	posRE = regexp.MustCompile(`^(<[^>]*>|%[sdqv]|-|\d+|[0-9A-Z]{20,}|b-[a-z]+|[a-z]{1,4}_[0-9A-Za-z]*[0-9][0-9A-Za-z]*)$`)
)

// rewritePhrases thay các cụm dùng "bint run" như danh từ (chỉ tiến trình điều phối) và các cụm cờ rời bằng cụm không nhắc lệnh.
// Cụm cờ rời (khoá cliref.flag.*) không thay phần nằm trong một lệnh bint mà parseCLI đọc được ("`bint evolve process --budget <USD>`"):
// lệnh đầy đủ do bộ đọc lệnh xử lý để còn chỉ tới ô form của cờ. Nhóm bắt thứ nhất của mẫu (nếu có) được giữ sau cụm thay.
func (r Rewriter) rewritePhrases(s string) string {
	if !strings.Contains(s, "bint") && !strings.Contains(s, "--") {
		return s
	}
	for _, p := range phraseRE {
		locs := p.re.FindAllStringSubmatchIndex(s, -1)
		if len(locs) == 0 {
			continue
		}
		var spans [][2]int
		if strings.HasPrefix(p.key, "cliref.flag.") {
			spans = cliSpans(s)
		}
		var b strings.Builder
		last := 0
		for _, m := range locs {
			if overlaps(spans, m[0], m[1]) {
				continue
			}
			b.WriteString(s[last:m[0]])
			b.WriteString(r.Cat.T(p.key))
			if len(m) >= 4 && m[2] >= 0 {
				b.WriteString(s[m[2]:m[3]])
			}
			last = m[1]
		}
		b.WriteString(s[last:])
		s = b.String()
	}
	return s
}

// cliSpans trả các đoạn [đầu, cuối) của s mà Rewrite đọc được thành lệnh bint (cùng điều kiện nhận lệnh với vòng lặp của Rewrite).
func cliSpans(s string) [][2]int {
	var spans [][2]int
	end := 0
	for from := 0; from < len(s); {
		j := strings.Index(s[from:], "bint ")
		if j < 0 {
			break
		}
		at := from + j
		from = at + 5
		if at > 0 && (isWordByte(s[at-1]) || s[at-1] == '.' || s[at-1] == '/') {
			continue
		}
		c, ok := parseCLI(s, at)
		if !ok || at < end {
			continue
		}
		spans = append(spans, [2]int{at, c.end})
		end, from = c.end, c.end
	}
	return spans
}

func overlaps(spans [][2]int, a, b int) bool {
	for _, sp := range spans {
		if a < sp[1] && sp[0] < b {
			return true
		}
	}
	return false
}

// parseCLI đọc lệnh bint bắt đầu ở s[at:] ("bint " đã khớp): đường lệnh dài nhất có hành động trong sổ, rồi các đối số và cờ.
func parseCLI(s string, at int) (cliCmd, bool) {
	p := at + 5
	var words []string
	var ends []int
	for q := p; len(words) < 3; {
		w := wordRE.FindString(s[q:])
		if w == "" {
			break
		}
		words = append(words, w)
		q += len(w)
		ends = append(ends, q)
		if q+1 < len(s) && s[q] == ' ' {
			q++
			continue
		}
		break
	}
	var c cliCmd
	e := -1
	for n := len(words); n >= 1; n-- {
		if path := strings.Join(words[:n], " "); actions.HasCLIPath(path) {
			c.paths, e = []string{path}, ends[n-1]
			break
		}
	}
	if e < 0 {
		return c, false
	}
	// "knowledge reject/deprecate": các lệnh con anh em cách nhau bằng "/".
	if first := strings.Fields(c.paths[0]); len(first) >= 2 {
		for e < len(s) && s[e] == '/' {
			w := wordRE.FindString(s[e+1:])
			if w == "" {
				break
			}
			if alt := strings.Join(first[:len(first)-1], " ") + " " + w; actions.HasCLIPath(alt) {
				c.paths = append(c.paths, alt)
			}
			e += 1 + len(w)
		}
	}
	back := at > 0 && s[at-1] == '`'
	for e < len(s) && s[e] == ' ' {
		tok, te := cliToken(s, e+1)
		if tok == "" {
			break
		}
		if strings.HasPrefix(tok, "--") {
			name, _, hasEq := strings.Cut(tok[2:], "=")
			c.flags = append(c.flags, name)
			e = te
			if !hasEq && actions.FlagTakesValue(name) && e < len(s) && s[e] == ' ' {
				if v, ve := cliToken(s, e+1); v != "" && !strings.HasPrefix(v, "--") {
					e = ve
				}
			}
			continue
		}
		if !posRE.MatchString(tok) && tok[0] != '\'' && tok[0] != '"' {
			break
		}
		e = te
	}
	if back {
		if k := strings.IndexByte(s[e:], '`'); k >= 0 {
			for _, f := range flagRE.FindAllString(s[e:e+k], -1) {
				c.flags = append(c.flags, f[2:])
			}
			e += k + 1
		}
	}
	// "`bint knowledge reject` / `deprecate`": lệnh con anh em viết tắt trong dấu huyền riêng sau dấu "/".
	if back && len(c.paths) == 1 {
		if first := strings.Fields(c.paths[0]); len(first) >= 2 {
			for strings.HasPrefix(s[e:], " / `") {
				w := wordRE.FindString(s[e+4:])
				if w == "" || e+4+len(w) >= len(s) || s[e+4+len(w)] != '`' {
					break
				}
				alt := strings.Join(first[:len(first)-1], " ") + " " + w
				if !actions.HasCLIPath(alt) {
					break
				}
				c.paths = append(c.paths, alt)
				e += 4 + len(w) + 1
			}
		}
	}
	c.end = e
	return c, true
}

// cliToken đọc một token từ s[q:] (chuỗi có nháy được đọc trọn); trả token rỗng nếu gặp dấu kết thúc.
func cliToken(s string, q int) (string, int) {
	if q >= len(s) {
		return "", q
	}
	if c := s[q]; c == '\'' || c == '"' {
		if k := strings.IndexByte(s[q+1:], c); k >= 0 {
			return s[q : q+k+2], q + k + 2
		}
	}
	e := q
	for e < len(s) && !strings.ContainsRune(" )(,;`", rune(s[e])) {
		e++
	}
	if e > q && s[e-1] == '.' && (e == len(s) || s[e] == ' ') {
		e--
	}
	return s[q:e], e
}

// render dựng tham chiếu TUI của lệnh; verb: dạng động từ ("nhấn s ở …") thay vì danh từ ("phím s ở …").
func (c cliCmd) render(rw Rewriter, verb bool) string {
	var parts []string
	for _, p := range c.paths {
		t, ok := actions.LookupCLI(rw.reg(), p, c.flags)
		if !ok {
			continue
		}
		parts = append(parts, refText(rw, t, verb))
	}
	return strings.Join(parts, " / ")
}

// ActionRef dựng tham chiếu TUI (phím, thẻ phụ, ô form) tới hành động id trong sổ, không qua chuỗi lệnh CLI. fields là các cờ cần nhập
// ở ô form ("apply" cho knowledge.verify). Hành động không có trong sổ thì trả về tên hành động để lỗi không im lặng. Ghi chú chỉ đọc tự
// thêm như refText.
func (r Rewriter) ActionRef(id string, fields ...string) string {
	t, ok := actions.TargetFor(r.reg(), id, fields...)
	if !ok {
		return id
	}
	return refText(r, t, false)
}

func refText(rw Rewriter, t actions.CLITarget, verb bool) string {
	cat := rw.Cat
	suffix := ".noun"
	if verb {
		suffix = ".verb"
	}
	ref := func(kind string, a i18n.A) string { return cat.T("cliref."+kind+suffix, a) }
	a := t.Action
	screen := a.Screen
	if rest, ok := strings.CutPrefix(a.ID, "nav."); ok {
		screen = actions.Screen(rest)
	}
	where := cat.T("tab." + string(screen))
	if screen == actions.Init {
		where = cat.T("init.group")
	}
	if t.TabKey != "" {
		where += " › " + cat.T(t.TabKey)
	}
	var s string
	switch {
	case t.TabOnly:
		s = ref("tab", i18n.A{"where": where})
	case a.Key != "":
		s = ref("key", i18n.A{"key": keyName(a.Key), "where": where})
	default:
		s = ref("palette", i18n.A{"title": cat.T(a.TitleKey)})
	}
	var fields []string
	for _, f := range t.Fields {
		if k := "form." + a.FormNS() + "." + f; cat.Has(k) {
			fields = append(fields, cat.T(k))
		}
	}
	if len(fields) > 0 {
		s += cat.T("cliref.fields", i18n.A{"fields": strings.Join(fields, ", ")})
	}
	// Chỉ đọc: hành động ghi, hoặc lệnh mang cờ mà phần chạy theo cờ đó là hành động ghi (knowledge verify --apply), không dùng được.
	if rw.ReadOnly && (a.Writes || writesByFlag(rw.reg(), t)) {
		s += cat.T("cliref.readonly")
	}
	return s
}

// writesByFlag: lệnh mang cờ làm việc chuyển sang một hành động ghi khác (CLITarget.WriteID) và hành động đó có Writes.
func writesByFlag(reg *actions.Registry, t actions.CLITarget) bool {
	if t.WriteID == "" {
		return false
	}
	w, ok := reg.ByID(t.WriteID)
	return ok && w.Writes
}

// keyName viết hoa chữ đầu của tên phím nhiều ký tự ("enter" → "Enter").
func keyName(k string) string {
	if len(k) > 1 && k[0] >= 'a' && k[0] <= 'z' && !strings.Contains(k, "+") {
		return strings.ToUpper(k[:1]) + k[1:]
	}
	return k
}
