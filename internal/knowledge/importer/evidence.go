package importer

import (
	"bytes"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"

	"bint/internal/knowledge"
	"bint/internal/symbols"
)

// MaxEvidence là số bằng chứng tối đa của một ý.
const MaxEvidence = 3

// maxDefHits: ký hiệu có nhiều định nghĩa hơn mức này (tên chung như New, Run) bị coi là mơ hồ và bỏ qua.
const maxDefHits = 2

// maxScanBytes giới hạn tệp được quét tìm định nghĩa.
const maxScanBytes = 512 << 10

// Finder tìm bằng chứng cho một ý bằng cách đối chiếu mã nội tuyến và đường dẫn trong văn bản với tệp của repository.
// Xác định, không gọi mô hình. Không an toàn cho dùng song song.
type Finder struct {
	files  map[string]bool
	order  []string
	bases  map[string][]string
	read   func(rel string) ([]byte, bool)
	cache  map[string][]byte
	defHit map[string][]defLoc
}

type defLoc struct {
	path   string
	region symbols.Region
	line   int
}

// NewFinder dựng bộ tìm từ danh sách tệp (đường dẫn tương đối dạng '/') và hàm đọc tệp.
func NewFinder(files []string, read func(rel string) ([]byte, bool)) *Finder {
	f := &Finder{files: map[string]bool{}, bases: map[string][]string{}, read: read,
		cache: map[string][]byte{}, defHit: map[string][]defLoc{}}
	for _, p := range files {
		if strings.HasPrefix(p, ".bint/") || knowledge.IsThirdPartyInput(p) {
			continue
		}
		f.files[p] = true
		f.order = append(f.order, p)
		f.bases[path.Base(p)] = append(f.bases[path.Base(p)], p)
	}
	return f
}

var (
	inlineCodeRe = regexp.MustCompile("`([^`\n]+)`")
	identRe      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	lineSuffixRe = regexp.MustCompile(`:(\d+)(?:-(\d+))?$`)
)

// Find trả tối đa MaxEvidence bằng chứng (chưa có fingerprint): định nghĩa ký hiệu trước, rồi tệp được nhắc tới.
func (f *Finder) Find(text string) []knowledge.Evidence {
	var idents, pathToks []string
	add := func(dst *[]string, s string) {
		for _, x := range *dst {
			if x == s {
				return
			}
		}
		*dst = append(*dst, s)
	}
	for _, m := range inlineCodeRe.FindAllStringSubmatch(text, -1) {
		span := strings.TrimSpace(m[1])
		if !strings.ContainsAny(span, " \t") {
			if id := identOf(span); id != "" {
				add(&idents, id)
			}
		}
		for _, tok := range strings.Fields(span) {
			add(&pathToks, trimTok(tok))
		}
	}
	for _, tok := range strings.Fields(text) {
		if strings.ContainsAny(tok, "/.") {
			add(&pathToks, trimTok(tok))
		}
	}

	var out []knowledge.Evidence
	inFile := map[string]bool{}
	for _, id := range idents {
		if len(out) >= MaxEvidence {
			break
		}
		hits := f.definitions(id)
		if len(hits) == 0 || len(hits) > maxDefHits {
			continue
		}
		for _, h := range hits {
			if len(out) >= MaxEvidence {
				break
			}
			e := knowledge.Evidence{Path: h.path, Symbol: id}
			if h.region.From > 0 && !(h.region.Container && h.region.To-h.region.From > 60) {
				e.Lines = fmt.Sprintf("%d-%d", h.region.From, h.region.To)
			} else {
				e.Lines = strconv.Itoa(h.line)
			}
			out = append(out, e)
			inFile[h.path] = true
		}
	}
	for _, tok := range pathToks {
		if len(out) >= MaxEvidence {
			break
		}
		rel, lines, ok := f.resolve(tok)
		if !ok || (inFile[rel] && lines == "") {
			continue
		}
		e := knowledge.Evidence{Path: rel, Lines: lines}
		dup := false
		for _, x := range out {
			if x.Path == e.Path && x.Lines == e.Lines {
				dup = true
			}
		}
		if !dup {
			out = append(out, e)
			inFile[rel] = true
		}
	}
	return out
}

// identOf lấy tên ký hiệu từ một đoạn mã nội tuyến một từ: "Foo", "foo()", "pkg.Foo", "Type.Method".
// Tên ngắn hoặc toàn chữ thường không có dấu hiệu là ký hiệu (từ thường như "server") bị bỏ.
func identOf(span string) string {
	called := strings.HasSuffix(span, "()")
	span = strings.TrimSuffix(span, "()")
	if i := strings.LastIndex(span, "."); i >= 0 {
		span = span[i+1:]
	}
	if !identRe.MatchString(span) || len(span) < 4 {
		return ""
	}
	if called || strings.ContainsAny(span, "_") || strings.ToLower(span) != span {
		return span
	}
	return ""
}

func trimTok(t string) string {
	t = strings.Trim(t, "\"'()[]{}<>,;*!?")
	return strings.TrimRight(t, ".:")
}

// resolve đổi một token thành tệp của repository (kèm vùng dòng nếu có hậu tố :a-b hợp lệ).
func (f *Finder) resolve(tok string) (rel, lines string, ok bool) {
	tok = strings.TrimPrefix(strings.ReplaceAll(tok, `\`, "/"), "./")
	if m := lineSuffixRe.FindStringSubmatch(tok); m != nil {
		cand := strings.TrimSuffix(tok, m[0])
		if r, found := f.lookup(cand); found {
			from, _ := strconv.Atoi(m[1])
			to := from
			if m[2] != "" {
				to, _ = strconv.Atoi(m[2])
			}
			n := f.lineCount(r)
			if from >= 1 && to >= from && to <= n {
				l := m[1]
				if m[2] != "" {
					l += "-" + m[2]
				}
				return r, l, true
			}
			return r, "", true
		}
	}
	if r, found := f.lookup(tok); found {
		return r, "", true
	}
	return "", "", false
}

// lookup tìm tệp theo đường dẫn đầy đủ, đuôi đường dẫn hoặc tên tệp, chỉ khi kết quả duy nhất. Token không có dấu '.'
// hay '/' (từ thường) không bao giờ khớp.
func (f *Finder) lookup(tok string) (string, bool) {
	if tok == "" || !strings.ContainsAny(tok, "./") || strings.HasSuffix(tok, "/") {
		return "", false
	}
	if f.files[tok] {
		return tok, true
	}
	var hits []string
	if !strings.Contains(tok, "/") {
		hits = f.bases[tok]
	} else {
		for _, p := range f.order {
			if strings.HasSuffix(p, "/"+tok) {
				hits = append(hits, p)
			}
		}
	}
	if len(hits) == 1 {
		return hits[0], true
	}
	return "", false
}

func (f *Finder) content(rel string) []byte {
	if b, ok := f.cache[rel]; ok {
		return b
	}
	b, ok := f.read(rel)
	if !ok || len(b) > maxScanBytes {
		b = nil
	}
	f.cache[rel] = b
	return b
}

func (f *Finder) lineCount(rel string) int {
	return bytes.Count(f.content(rel), []byte("\n")) + 1
}

var defTmpl = `(?m)^[ \t]*(?:export[ \t]+(?:default[ \t]+)?|pub(?:\([a-z]+\))?[ \t]+|async[ \t]+|static[ \t]+|abstract[ \t]+)*` +
	`(?:func[ \t]+(?:\([^)]*\)[ \t]*)?|type[ \t]+|class[ \t]+|interface[ \t]+|struct[ \t]+|enum[ \t]+|trait[ \t]+|def[ \t]+|` +
	`function[ \t]+|fn[ \t]+|const[ \t]+|var[ \t]+|let[ \t]+|object[ \t]+)%s\b`

// definitions tìm nơi định nghĩa ký hiệu id trong các tệp mã (loại tệp kiểm thử nếu còn nơi khác).
func (f *Finder) definitions(id string) []defLoc {
	if h, ok := f.defHit[id]; ok {
		return h
	}
	re := regexp.MustCompile(fmt.Sprintf(defTmpl, regexp.QuoteMeta(id)))
	var hits, tests []defLoc
	for _, p := range f.order {
		if !symbols.Supported(p) {
			continue
		}
		b := f.content(p)
		if len(b) == 0 || !bytes.Contains(b, []byte(id)) {
			continue
		}
		loc := re.FindIndex(b)
		if loc == nil {
			continue
		}
		line := bytes.Count(b[:loc[0]], []byte("\n")) + 1
		d := defLoc{path: p, line: line}
		if r, ok := symbols.Locate(p, b, id); ok && r.From <= line && line <= r.To {
			d.region = r
		}
		if isTestFile(p) {
			tests = append(tests, d)
		} else {
			hits = append(hits, d)
		}
	}
	if len(hits) == 0 {
		hits = tests
	}
	f.defHit[id] = hits
	return hits
}

func isTestFile(p string) bool {
	b := path.Base(p)
	return strings.HasSuffix(b, "_test.go") || strings.Contains(b, ".test.") || strings.Contains(b, ".spec.") ||
		strings.HasPrefix(b, "test_") || strings.HasSuffix(b, "_test.py")
}
