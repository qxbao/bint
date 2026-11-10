package eval

import (
	"context"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// Hợp đồng ẩn ngoài chữ ký định danh: chuỗi so khớp nguyên văn, cờ dòng lệnh,
// khoá cấu hình/dữ liệu và thuộc tính module bị test thay thế. Mọi thứ ở đây chỉ
// lộ TÊN (cờ, khoá), không bao giờ lộ giá trị hay nội dung chuỗi so khớp.

// Loại gợi ý giao diện (InterfaceHint.Kind). Rỗng nghĩa là HintSignature, để các
// tác vụ đã lưu trước khi có trường Kind vẫn đọc và ghi lại y nguyên.
const (
	HintSignature = "signature"
	HintFlag      = "flag"
	HintConfigKey = "config_key"
	HintDataKey   = "data_key"
)

// Với chuỗi ngắn, "không có tại commit gốc" xét trong các tệp lời giải đụng tới (existsInFiles),
// không phải cả repository: chuỗi như "THIẾU" thường đã có đâu đó trong tài liệu hay mã khác
// mà vẫn là hợp đồng mới của chính tệp được sửa.

// maxNewTextProbes giới hạn số lần git grep cho chuỗi ngắn "đặc thù" mỗi tác vụ.
const maxNewTextProbes = 40

var (
	keyShapeRe  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)
	identOnlyRe = regexp.MustCompile(`^[A-Za-z_]\w*$`)
	pathLikeRe  = regexp.MustCompile(`^[\w./\\-]+(?:/[\w./\\-]*|\.\w{1,5})$`)
	fixtureIDRe = regexp.MustCompile(`^[A-Za-z]+(?:-[A-Za-z]+)*-\d+$`)
	flagRe      = regexp.MustCompile(`^--?[A-Za-z][\w-]*`)
	// setattr(x, "name", …) / patch.object(x, "name"): nhóm 1 là mô-đun, nhóm 2 là tên.
	patchObjRes = []*regexp.Regexp{
		regexp.MustCompile(`setattr\(\s*([\w.]+)\s*,\s*["']([A-Za-z_]\w*)["']`),
		regexp.MustCompile(`patch\.object\(\s*([\w.]+)\s*,\s*["']([A-Za-z_]\w*)["']`),
	}
	// setattr("a.b.name", …) / patch("a.b.name"): đường dẫn chấm.
	patchPathRes = []*regexp.Regexp{
		regexp.MustCompile(`setattr\(\s*["']([\w.]+)["']`),
		regexp.MustCompile(`patch(?:\.\w+)?\(\s*["']([\w.]+)["']`),
	}
	configExts = map[string]bool{".yml": true, ".yaml": true, ".json": true, ".toml": true}
)

// existsInFiles: name đã có trong các tệp files tại commit rev (khác existsAt, chỉ xét
// tệp lời giải đụng tới). Cờ và khoá thường đã có ở nơi khác trong repository (tệp
// khác, tài liệu) nhưng lời giải mới đưa vào đúng tệp này.
func existsInFiles(ctx context.Context, repoDir, rev, name string, files []string) bool {
	args := append([]string{"grep", "-q", "-w", "-F", "-e", name, rev, "--"}, files...)
	_, err := git(ctx, repoDir, args...)
	return err == nil
}

// fileLines là các dòng thêm mới của một tệp lời giải.
type fileLines struct {
	path  string
	lines []string
}

// exactTexts: chuỗi trong phần kiểm thử được thêm, cũng nằm trong mã lời giải
// được thêm, nhưng đề bài không nêu — agent phải đoán đúng từng chữ. Gồm:
//   - chuỗi có khoảng trắng từ minExactText ký tự;
//   - chuỗi ngắn "đặc thù" (>= 3 ký tự; chữ hoa liên tiếp, ký hiệu hoặc không phải
//     ASCII) mà commit gốc không có. Cụm có khoảng trắng bị tách trong f-string
//     hay chuỗi ghép cũng tính khi mọi từ của nó có trong lời giải.
//
// skip là tên đã đưa vào khối giao diện (cờ, khoá) nên không cần cảnh báo nữa.
func exactTexts(ctx context.Context, repoDir, base string, srcFiles []string, in LeakInput, prompt string, skip map[string]bool) []string {
	src := strings.Join(in.AddedLines, "\n")
	np := normWS(prompt)
	var out []string
	probes := 0
	for _, s := range testLiterals(in.TestAdded) {
		if skip[s] || flagName(s) != "" || strings.Contains(np, normWS(s)) {
			continue
		}
		spaced := strings.ContainsAny(s, " \t")
		if spaced && len([]rune(s)) >= minExactText && strings.Contains(src, s) {
			out = append(out, s)
			continue
		}
		if !distinctive(s) || notSpecific(s) || fragment(s) {
			continue
		}
		if !strings.Contains(src, s) && !(spaced && partsIn(in.AddedLines, s)) {
			continue
		}
		if probes >= maxNewTextProbes {
			continue
		}
		probes++
		if !existsInFiles(ctx, repoDir, base, s, srcFiles) {
			out = append(out, s)
		}
	}
	return out
}

// testLiterals: các chuỗi literal không rỗng, đã khử trùng, theo thứ tự trong dòng test thêm mới.
func testLiterals(added []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range stringLitRe.FindAllStringSubmatch(strings.Join(added, "\n"), -1) {
		s := m[1] + m[2]
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// distinctive: chuỗi đủ đặc thù để agent không đoán được — dài >= 3 và có chữ hoa
// liên tiếp (>= 2, kể cả có dấu), ký hiệu ngoài chữ/số/gạch dưới, hoặc ký tự không phải ASCII.
func distinctive(s string) bool {
	if len([]rune(s)) < 3 {
		return false
	}
	run := 0
	for _, r := range s {
		if unicode.IsUpper(r) {
			if run++; run >= 2 {
				return true
			}
		} else {
			run = 0
		}
		if r > 127 || (!unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && !unicode.IsSpace(r)) {
			return true
		}
	}
	return false
}

// notSpecific: đường dẫn/tên tệp, chuỗi chỉ gồm định danh (interface_gap lo), chuỗi hex,
// mã dữ liệu mẫu của test (AC-2, API-QC-0005), mẩu JSON/dòng lệnh có cờ (cờ do flagHints lo).
func notSpecific(s string) bool {
	if identOnlyRe.MatchString(s) || hexLikeRe.MatchString(s) || fixtureIDRe.MatchString(s) {
		return true
	}
	if strings.ContainsAny(s, "{}") || strings.Contains(s, " -") {
		return true
	}
	return !strings.ContainsAny(s, " \t") && (pathLikeRe.MatchString(s) || strings.HasPrefix(s, "test_") || strings.Contains(s, "tests/"))
}

// fragment: chuỗi trông như mảnh vỡ của đoạn văn/chú thích (khoảng trắng đầu/cuối, mở đầu
// bằng dấu câu đóng) do bộ tách literal cắt nhầm quanh dấu nháy trong chú thích.
func fragment(s string) bool {
	if s != strings.TrimSpace(s) {
		return true
	}
	return strings.ContainsRune(",.;:)]}`", []rune(s)[0])
}

// partsIn: cụm ngắn (<= 4 từ) bị f-string/chuỗi ghép tách rời: mọi từ (>= 3 ký tự)
// của nó cùng nằm trên một dòng lời giải thêm mới. Đoạn văn dài không tính.
func partsIn(lines []string, s string) bool {
	words := strings.Fields(s)
	if len(words) > 4 {
		return false
	}
	var parts []string
	for _, w := range words {
		if w = strings.Trim(w, ".,:;!?()[]{}\"'`"); len([]rune(w)) >= 3 {
			parts = append(parts, w)
		}
	}
	if len(parts) == 0 {
		return false
	}
	for _, ln := range lines {
		ok := true
		for _, w := range parts {
			if !strings.Contains(ln, w) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func isWordByte(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

// wordIn: s xuất hiện trong text như một từ nguyên.
func wordIn(text, s string) bool {
	if s == "" {
		return false
	}
	for from := 0; from < len(text); {
		k := strings.Index(text[from:], s)
		if k < 0 {
			return false
		}
		a, b := from+k, from+k+len(s)
		if (a == 0 || !isWordByte(text[a-1]) || !isWordByte(s[0])) && (b == len(text) || !isWordByte(text[b]) || !isWordByte(s[len(s)-1])) {
			return true
		}
		from = a + 1
	}
	return false
}

// flagName trả về tên cờ nếu literal có dạng "--from", "--from elicited", "--k=v", "-x"; "" nếu không.
func flagName(lit string) string {
	m := flagRe.FindString(lit)
	if m != "" && (len(m) == len(lit) || lit[len(m)] == ' ' || lit[len(m)] == '=') {
		return m
	}
	return ""
}

var importNameRe = func(name string) *regexp.Regexp {
	q := regexp.QuoteMeta(name)
	return regexp.MustCompile(`^\s*(?:from\s+\S+\s+)?import\b.*\b` + q + `\b|^\s*` + q + `\s*(?::[^=]*)?=[^=]`)
}

// importedAttr: lời giải thêm name vào một mô-đun bằng dòng import hoặc phép gán cấp
// mô-đun (test thay thế thuộc tính đó). Tên đã có ở nơi khác nên không thể xét bằng
// existsAt, nhưng mô-đun phải có thuộc tính này thì test mới patch được.
func importedAttr(t patchTarget, src []fileLines) (InterfaceHint, bool) {
	name := t.Name
	re := importNameRe(name)
	for _, f := range src {
		if langOf(f.path) != "py" || t.Mod == "" || strings.TrimLeft(strings.TrimSuffix(path.Base(f.path), path.Ext(f.path)), "_") != t.Mod {
			continue
		}
		for _, ln := range f.lines {
			if re.MatchString(ln) {
				return InterfaceHint{Path: f.path, Name: name, Signature: name + " (module attribute)"}, true
			}
		}
	}
	return InterfaceHint{}, false
}

// patchTarget là thuộc tính bị monkeypatch/mock; Mod là tên mô-đun (đoạn cuối của biểu
// thức/đường dẫn chứa nó, bỏ gạch dưới đầu) nếu suy ra được.
type patchTarget struct{ Name, Mod string }

func lastSeg(s string) string {
	if k := strings.LastIndex(s, "."); k >= 0 {
		return s[k+1:]
	}
	return s
}

// patchTargets: thuộc tính bị monkeypatch/mock trong dòng test thêm mới.
func patchTargets(added []string) []patchTarget {
	seen := map[string]bool{}
	var out []patchTarget
	add := func(name, mod string) {
		mod = strings.TrimLeft(mod, "_")
		if len(name) >= 3 && !seen[name+"@"+mod] {
			seen[name+"@"+mod] = true
			out = append(out, patchTarget{name, mod})
		}
	}
	for _, ln := range added {
		for _, re := range patchObjRes {
			for _, m := range re.FindAllStringSubmatch(ln, -1) {
				add(m[2], lastSeg(m[1]))
			}
		}
		for _, re := range patchPathRes {
			for _, m := range re.FindAllStringSubmatch(ln, -1) {
				parts := strings.Split(m[1], ".")
				if len(parts) >= 2 {
					add(parts[len(parts)-1], parts[len(parts)-2])
				} else {
					add(parts[0], "")
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name+"@"+out[i].Mod < out[j].Name+"@"+out[j].Mod })
	return out
}

// flagHints: cờ dòng lệnh mà test kiểm, lời giải đưa vào, đề bài chưa nêu.
func flagHints(ctx context.Context, repoDir, base string, srcFiles []string, in LeakInput, prompt string) []InterfaceHint {
	src := strings.Join(in.AddedLines, "\n")
	np := normWS(prompt)
	seen := map[string]bool{}
	var out []InterfaceHint
	for _, lit := range testLiterals(in.TestAdded) {
		fl := flagName(lit)
		if fl == "" || seen[fl] || !strings.Contains(src, fl) || wordIn(np, fl) || existsInFiles(ctx, repoDir, base, fl, srcFiles) {
			continue
		}
		seen[fl] = true
		out = append(out, InterfaceHint{Kind: HintFlag, Name: fl, Signature: "CLI flag: " + fl})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// keyHints: literal của test đóng vai khoá do lời giải đưa vào (xem keyHint). named là
// tên đã có hint (không lặp lại).
func keyHints(ctx context.Context, repoDir, base string, srcFiles []string, in LeakInput, prompt string, src []fileLines, named map[string]bool) []InterfaceHint {
	np := normWS(prompt)
	var out []InterfaceHint
	for _, lit := range testLiterals(in.TestAdded) {
		if !keyShapeRe.MatchString(lit) || len(lit) < 3 || named[lit] || flagName(lit) != "" {
			continue
		}
		if h, ok := keyHint(ctx, repoDir, base, srcFiles, lit, np, src); ok {
			named[lit] = true
			out = append(out, h)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// keyHint: khoá cấu hình nếu key xuất hiện như khoá ở dòng thêm mới của tệp
// .yml/.yaml/.json/.toml (`key:`, `"key":`, `key =`); khoá dữ liệu nếu xuất hiện như
// khoá dict/chỉ số trong mã thêm mới (`["key"]`, `"key":`, `.get("key")`). Không có
// tại commit gốc và đề bài chưa nêu.
func keyHint(ctx context.Context, repoDir, base string, srcFiles []string, key, normPrompt string, src []fileLines) (InterfaceHint, bool) {
	q := regexp.QuoteMeta(key)
	cfgRes := []*regexp.Regexp{
		regexp.MustCompile(`^\s*(?:-\s+)?["']?` + q + `["']?\s*[:=]`),
		regexp.MustCompile(`["']` + q + `["']\s*:`),
		regexp.MustCompile(`^\s*\[+` + q + `\]+\s*$`),
	}
	codeRes := []*regexp.Regexp{
		regexp.MustCompile(`\[\s*["']` + q + `["']\s*\]`),
		regexp.MustCompile(`["']` + q + `["']\s*:`),
		regexp.MustCompile(`\.get\(\s*["']` + q + `["']`),
	}
	var hint *InterfaceHint
scan:
	for _, f := range src {
		cfg := configExts[strings.ToLower(path.Ext(f.path))]
		res := codeRes
		if cfg {
			res = cfgRes
		}
		for _, ln := range f.lines {
			for _, re := range res {
				if !re.MatchString(ln) {
					continue
				}
				if cfg {
					hint = &InterfaceHint{Kind: HintConfigKey, Path: f.path, Name: key}
					break scan
				}
				if hint == nil {
					hint = &InterfaceHint{Kind: HintDataKey, Path: f.path, Name: key}
				}
			}
		}
	}
	if hint == nil || wordIn(normPrompt, key) || existsInFiles(ctx, repoDir, base, key, srcFiles) {
		return InterfaceHint{}, false
	}
	return *hint, true
}
