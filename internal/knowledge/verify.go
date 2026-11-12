package knowledge

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"bint/internal/pathglob"
)

// Mức của một phát hiện.
const (
	LevelError = "error"
	LevelWarn  = "warn"
)

// Mã phát hiện.
const (
	CodeSchema      = "schema"
	CodeEvidence    = "evidence"    // tệp/vùng dòng của bằng chứng không tồn tại
	CodeFingerprint = "fingerprint" // bằng chứng đã thay đổi kể từ khi ghi
	CodeSymbol      = "symbol"      // ký hiệu được tham chiếu không có trong mã
	CodeScope       = "scope"       // mẫu phạm vi không khớp tệp nào
	CodeExample     = "example"     // ví dụ mã không khớp mã thật
	CodePathRef     = "path-ref"    // đường dẫn nhắc tới trong nội dung không tồn tại
	CodeDuplicate   = "duplicate"   // trùng với mục khác
	CodeThirdParty  = "third-party" // đầu vào bên thứ ba nhưng chưa gắn cờ
	CodeOverview    = "overview"    // tổng quan/cấu trúc thư mục: không ưu tiên (mục 4.2)
	CodeLength      = "length"      // quá dài cho tri thức chèn vào ngữ cảnh
	CodeUnreviewed  = "unreviewed"  // mục trạng thái verified nhưng thiếu dấu vết duyệt
	CodeRelocated   = "relocated"   // vùng dòng của bằng chứng dịch chuyển, nội dung không đổi
	CodeMoved       = "moved"       // khối bằng chứng đã chuyển sang tệp khác, nội dung không đổi
)

// Relocation là một bằng chứng có vùng dòng cần cập nhật (nội dung không đổi).
//
// From/To là vùng dòng cũ/mới. FromPath luôn là tệp lúc kiểm; ToPath chỉ có khi khối đã chuyển
// sang tệp khác (CodeMoved). ApplyRelocations chỉ áp khi cả hai còn đúng như lúc kiểm.
type Relocation struct {
	Index    int    `json:"index"`
	From     string `json:"from"`
	To       string `json:"to"`
	FromPath string `json:"from_path,omitempty"`
	ToPath   string `json:"to_path,omitempty"`
}

// Finding là một phát hiện của kiểm tra xác định được.
type Finding struct {
	Level string `json:"level"`
	Code  string `json:"code"`
	// Detail là câu tiếng Việt trung tính (không có lệnh bint) cho JSON/MCP/lý do trạng thái.
	Detail string `json:"detail"`
	// Evidence có mặt với phát hiện về bằng chứng: tham số có cấu trúc để CLI/TUI tự dựng câu.
	Evidence *EvidenceIssue `json:"evidence,omitempty"`
}

// Result là kết quả kiểm tra một mục.
type Result struct {
	ID        string       `json:"id"`
	Findings  []Finding    `json:"findings"`
	Relocated []Relocation `json:"relocated,omitempty"`
}

// ApplyRelocations cập nhật vị trí (vùng dòng, và đường dẫn nếu khối đã chuyển tệp) của các
// bằng chứng chỉ bị dịch chuyển. Chỉ áp khi đường dẫn và vùng dòng hiện tại còn đúng như lúc
// kiểm; không đụng fingerprint, ký hiệu, commit. Trả về true nếu áp được ít nhất một bản ghi.
func (r Result) ApplyRelocations(it *Item) bool {
	applied := false
	for _, rl := range r.Relocated {
		if rl.Index < 0 || rl.Index >= len(it.Evidence) {
			continue
		}
		e := &it.Evidence[rl.Index]
		if e.Lines != rl.From || (rl.FromPath != "" && e.Path != rl.FromPath) {
			continue
		}
		if rl.ToPath != "" {
			e.Path = rl.ToPath
		}
		e.Lines = rl.To
		applied = true
	}
	return applied
}

// OK cho biết không có lỗi (cảnh báo không chặn).
func (r Result) OK() bool {
	for _, f := range r.Findings {
		if f.Level == LevelError {
			return false
		}
	}
	return true
}

// OnlyErrors cho biết có lỗi và mọi lỗi đều thuộc các mã đã cho.
func (r Result) OnlyErrors(codes ...string) bool {
	any := false
	for _, f := range r.Findings {
		if f.Level != LevelError {
			continue
		}
		any = true
		ok := false
		for _, c := range codes {
			ok = ok || f.Code == c
		}
		if !ok {
			return false
		}
	}
	return any
}

// Has cho biết có phát hiện với mã đã cho.
func (r Result) Has(code string) bool {
	for _, f := range r.Findings {
		if f.Code == code {
			return true
		}
	}
	return false
}

// ThirdPartyPatterns nhận diện đầu vào là nội dung bên thứ ba (mục 15.5).
var ThirdPartyPatterns = []string{
	"vendor/**", "**/vendor/**", "node_modules/**", "**/node_modules/**", "third_party/**", "**/third_party/**",
	"third-party/**", "external/**", "**/site-packages/**", ".venv/**", "Pods/**",
}

// IsThirdPartyInput cho biết một đầu vào (đường dẫn hoặc URL) là nội dung bên thứ ba.
func IsThirdPartyInput(in string) bool {
	l := strings.ToLower(strings.TrimSpace(in))
	if strings.Contains(l, "://") || strings.HasPrefix(l, "issue:") || strings.HasPrefix(l, "pr:") {
		return true
	}
	return pathglob.MatchAny(ThirdPartyPatterns, cleanRel(in))
}

// Limits cho kiểm tra độ dài.
const (
	MaxTitleRunes = 120
	MaxBodyRunes  = 1200
)

var (
	fenceRe      = regexp.MustCompile("(?ms)^```([^\\n]*)\\n(.*?)^```")
	inlineCodeRe = regexp.MustCompile("`([^`\\n]+)`")
	overviewRe   = regexp.MustCompile(`(?i)\b(tổng quan|kiến trúc tổng|cấu trúc thư mục|cây thư mục|overview|architecture overview|directory (structure|layout)|project structure)\b`)
	pathLikeRe   = regexp.MustCompile(`^[A-Za-z0-9_.\-]+(/[A-Za-z0-9_.\-*]+)+/?$|^[A-Za-z0-9_\-]+\.(go|ts|tsx|js|jsx|py|rs|java|kt|rb|php|cs|yml|yaml|json|toml|sql|proto|md|sh)$`)
	shellLangs   = map[string]bool{"sh": true, "bash": true, "shell": true, "console": true, "zsh": true, "powershell": true,
		"ps1": true, "cmd": true, "text": true, "txt": true, "plaintext": true, "diff": true}
)

// Verifier chạy kiểm tra xác định được cho các mục trên một nguồn mã.
type Verifier struct {
	Src Source
	// All là toàn bộ mục (để phát hiện trùng lặp).
	All []*Item
	// FindMoved bật tìm khối bằng chứng ở tệp khác khi không tìm lại được ở tệp cũ (có giới hạn
	// chi phí, xem moved.go). Mặc định tắt để đường nóng (eval, evolve, ctxengine) không đổi.
	FindMoved bool
}

// Verify kiểm tra một mục: schema, bằng chứng, fingerprint, ký hiệu, phạm vi, ví dụ
// mã, đường dẫn được nhắc tới, trùng lặp, cờ bên thứ ba. Không gọi mô hình.
func (v Verifier) Verify(it *Item) Result {
	r := Result{ID: it.ID}
	add := func(level, code, format string, a ...any) {
		r.Findings = append(r.Findings, Finding{Level: level, Code: code, Detail: fmt.Sprintf(format, a...)})
	}
	for _, p := range it.Validate() {
		add(LevelError, CodeSchema, "%s", p)
	}

	// Bằng chứng: tồn tại, vùng dòng hợp lệ, fingerprint khớp (theo mức đã lưu, có
	// định vị lại khi vùng dòng chỉ bị dịch chuyển hoặc khối đã chuyển tệp), ký hiệu có mặt.
	evidenceText := map[string][]byte{}
	addIssue := func(level, code string, iss EvidenceIssue, detail string) {
		r.Findings = append(r.Findings, Finding{Level: level, Code: code, Detail: detail, Evidence: &iss})
	}
	for i, e := range it.Evidence {
		base := EvidenceIssue{Index: i, Path: e.Path, Lines: e.Lines, Symbol: e.Symbol}
		// moved báo khối đã chuyển/không chọn được; false nếu không tìm chuyển tệp hoặc không thấy.
		// code là mã của phát hiện gốc, dùng khi kết quả là ambiguous hoặc không thấy.
		moved := func(code string, iss EvidenceIssue, detail string) (Evidence, []byte, bool) {
			if !v.FindMoved || e.Fingerprint == "" {
				addIssue(LevelError, code, iss, detail)
				return e, nil, false
			}
			mv := findMoved(v.Src, e, e.Commit, it.Origin.Commit)
			iss.Truncated = mv.Truncated
			switch mv.Kind {
			case IssueMoved:
				iss.Kind, iss.NewPath, iss.NewLines, iss.FileLines = IssueMoved, mv.NewPath, mv.NewLines, 0
				addIssue(LevelError, CodeMoved, iss, iss.Describe())
				r.Relocated = append(r.Relocated, Relocation{Index: i, From: e.Lines, To: mv.NewLines, FromPath: e.Path, ToPath: mv.NewPath})
				ne := e
				ne.Path = mv.NewPath
				if mv.NewLines != "" {
					ne.Lines = mv.NewLines
				}
				return ne, mv.Content, true
			case IssueAmbiguous:
				iss.Kind, iss.Candidates = IssueAmbiguous, mv.Candidates
				addIssue(LevelError, code, iss, iss.Describe())
			default:
				addIssue(LevelError, code, iss, detail)
			}
			return e, nil, false
		}
		b, ok := v.Src.Read(e.Path)
		if !ok {
			iss := base
			iss.Kind = IssueMissingFile
			ne, nb, found := moved(CodeEvidence, iss, fmt.Sprintf("bằng chứng %s: không có tệp trong %s", e.Path, v.Src.Name()))
			if !found {
				continue
			}
			e, b = ne, nb
			evidenceText[e.Path] = b
		} else {
			evidenceText[e.Path] = b
			if e.Fingerprint == "" {
				if _, err := Fingerprint(e, b); err != nil {
					iss := base
					iss.Kind = IssueBadRange
					var ie *EvidenceIssueError
					if errors.As(err, &ie) {
						iss = withIndex(ie.Issue, i)
					}
					addIssue(LevelError, CodeEvidence, iss, fmt.Sprintf("bằng chứng %s: %v", e, err))
					continue
				}
				iss := base
				iss.Kind = IssueNoFingerprint
				addIssue(LevelWarn, CodeFingerprint, iss, fmt.Sprintf("bằng chứng %s chưa có fingerprint (không phát hiện được khi lỗi thời)", e))
			} else {
				switch c := CheckEvidence(e, b); c.Status {
				case EvChanged:
					iss := base
					iss.Kind, iss.FileLines = c.Kind, c.FileLines
					detail := fmt.Sprintf("bằng chứng %s đã thay đổi kể từ khi ghi: %s", e, c.Detail)
					// Chỉ tìm chuyển tệp khi khối không còn ở tệp cũ (vùng vượt quá tệp, ký hiệu mất). Nội
					// dung đổi tại chỗ (changed) vẫn là changed/stale: một bản sao ở tệp khác không thay được
					// khối đã sửa, và không được chặn cách sửa refresh.
					if c.Kind != IssueOutOfRange && c.Kind != IssueSymbolGone {
						addIssue(LevelError, CodeFingerprint, iss, detail)
						break
					}
					if ne, nb, found := moved(CodeFingerprint, iss, detail); found {
						e, b = ne, nb
						evidenceText[e.Path] = b
					}
				case EvRelocated:
					iss := base
					iss.Kind, iss.NewLines = IssueRelocated, c.Lines
					addIssue(LevelWarn, CodeRelocated, iss, fmt.Sprintf("bằng chứng %s: %s", e, c.Detail))
					r.Relocated = append(r.Relocated, Relocation{Index: i, From: e.Lines, To: c.Lines, FromPath: e.Path})
					e.Lines = c.Lines
				}
			}
		}
		if e.Symbol != "" && !containsSymbol(regionOf(e, b), e.Symbol) {
			iss := EvidenceIssue{Index: i, Kind: IssueSymbolGone, Path: e.Path, Lines: e.Lines, Symbol: e.Symbol}
			addIssue(LevelError, CodeSymbol, iss, fmt.Sprintf("ký hiệu %q không có trong %s", e.Symbol, e))
		}
	}

	// Phạm vi: mỗi mẫu phải khớp ít nhất một tệp.
	var scoped []string
	for _, p := range it.Scope.Paths {
		if p == "**" || p == "**/*" {
			continue
		}
		matched := matchFiles(v.Src, p, 200)
		if len(matched) == 0 {
			add(LevelError, CodeScope, "phạm vi %q không khớp tệp nào trong %s", p, v.Src.Name())
		}
		scoped = append(scoped, matched...)
	}
	// Tệp dùng để tìm ký hiệu và ví dụ mã: bằng chứng trước, rồi tệp trong phạm vi.
	corpus := func(yield func(string, []byte) bool) {
		for p, b := range evidenceText {
			if !yield(p, b) {
				return
			}
		}
		n := 0
		for _, p := range scoped {
			if _, done := evidenceText[p]; done {
				continue
			}
			if n++; n > 200 {
				return
			}
			if b, ok := v.Src.Read(p); ok && !yield(p, b) {
				return
			}
		}
	}
	for _, sym := range it.Scope.Symbols {
		found := false
		for _, b := range corpus {
			if containsSymbol(b, sym) {
				found = true
				break
			}
		}
		if !found {
			add(LevelError, CodeSymbol, "ký hiệu phạm vi %q không có trong bằng chứng hay tệp thuộc phạm vi", sym)
		}
	}

	// Ví dụ mã trong nội dung phải khớp mã thật.
	for _, m := range fenceRe.FindAllStringSubmatch(it.Body, -1) {
		info, code := strings.TrimSpace(m[1]), m[2]
		lang, attrs := parseFenceInfo(info)
		if shellLangs[strings.ToLower(lang)] && attrs["path"] == "" {
			continue // lệnh shell, không phải trích mã nguồn
		}
		sig := significantLines(code)
		if len(sig) == 0 {
			continue
		}
		if p := attrs["path"]; p != "" {
			b, ok := v.Src.Read(cleanRel(p))
			if !ok {
				add(LevelError, CodeExample, "ví dụ mã tham chiếu %s nhưng tệp không tồn tại", p)
				continue
			}
			if miss := missingLines(sig, [][]byte{b}); len(miss) > 0 {
				add(LevelError, CodeExample, "ví dụ mã (path=%s) có %d/%d dòng không có trong tệp, ví dụ: %q", p, len(miss), len(sig), miss[0])
			}
			continue
		}
		var texts [][]byte
		for _, b := range corpus {
			texts = append(texts, b)
		}
		if len(texts) == 0 {
			add(LevelWarn, CodeExample, "ví dụ mã không đối chiếu được (không có bằng chứng); thêm path=<tệp> vào dòng mở ``` nếu trích từ mã thật")
			continue
		}
		if miss := missingLines(sig, texts); len(miss) > 0 {
			level := LevelWarn
			if len(miss)*2 > len(sig) {
				level = LevelError
			}
			add(level, CodeExample, "ví dụ mã có %d/%d dòng không thấy trong bằng chứng/phạm vi, ví dụ: %q (ví dụ minh hoạ thì đánh dấu ```text)", len(miss), len(sig), miss[0])
		}
	}

	// Đường dẫn nhắc tới trong nội dung (dạng `a/b.go`) phải tồn tại.
	bodyNoFence := fenceRe.ReplaceAllString(it.Body, "")
	for _, m := range inlineCodeRe.FindAllStringSubmatch(it.Title+"\n"+bodyNoFence, -1) {
		tok := strings.TrimSpace(m[1])
		if !pathLikeRe.MatchString(tok) || strings.Contains(tok, "://") {
			continue
		}
		rel := cleanRel(tok)
		if pathglob.HasMeta(rel) {
			if len(matchFiles(v.Src, rel, 1)) == 0 {
				add(LevelWarn, CodePathRef, "mẫu %q trong nội dung không khớp tệp nào", tok)
			}
			continue
		}
		if !HasFileOrDir(v.Src, rel) {
			add(LevelWarn, CodePathRef, "đường dẫn %q trong nội dung không tồn tại", tok)
		}
	}

	// Trùng lặp với mục khác còn hiệu lực.
	nt, nb := normText(it.Title), normText(it.Body)
	for _, o := range v.All {
		if o.ID == it.ID || !o.Active() {
			continue
		}
		if (nt != "" && normText(o.Title) == nt) || (nb != "" && normText(o.Body) == nb) {
			level := LevelWarn
			if it.Status == StatusCandidate {
				level = LevelError
			}
			add(level, CodeDuplicate, "trùng với %s (%s)", o.ID, o.Title)
		}
	}

	// Đầu vào bên thứ ba phải được gắn cờ.
	if !it.Origin.ThirdParty {
		for _, in := range it.Origin.Inputs {
			if IsThirdPartyInput(in) {
				add(LevelError, CodeThirdParty, "đầu vào %q là nội dung bên thứ ba; đặt origin.third_party: true", in)
				break
			}
		}
		for _, e := range it.Evidence {
			if IsThirdPartyInput(e.Path) {
				add(LevelError, CodeThirdParty, "bằng chứng %q là nội dung bên thứ ba; đặt origin.third_party: true", e.Path)
				break
			}
		}
	}

	if (it.Type == TypeFact || it.Type == TypeDecision) && overviewRe.MatchString(it.Title+"\n"+it.Body) {
		add(LevelWarn, CodeOverview, "có vẻ là tổng quan kiến trúc/cấu trúc thư mục — agent tự khám phá được, không nên ghi làm tri thức (mục 4.2)")
	}
	if n := len([]rune(it.Title)); n > MaxTitleRunes {
		add(LevelWarn, CodeLength, "tiêu đề dài %d ký tự (nên <= %d)", n, MaxTitleRunes)
	}
	if n := len([]rune(it.Body)); n > MaxBodyRunes {
		add(LevelWarn, CodeLength, "nội dung dài %d ký tự (nên <= %d); tri thức chèn vào ngữ cảnh cần ngắn", n, MaxBodyRunes)
	}
	if it.Status == StatusVerified && it.Origin.Kind != OriginHuman && it.ReviewedBy == "" && it.Type == TypeRule {
		add(LevelError, CodeUnreviewed, "quy tắc verified nhưng không có người duyệt")
	}
	return r
}

func matchFiles(src Source, pattern string, limit int) []string {
	pattern = strings.TrimSuffix(cleanRel(pattern), "/")
	var out []string
	if !pathglob.HasMeta(pattern) {
		if HasFileOrDir(src, pattern) {
			prefix := pattern + "/"
			for _, f := range src.Files() {
				if f == pattern || strings.HasPrefix(f, prefix) {
					out = append(out, f)
					if len(out) >= limit {
						break
					}
				}
			}
		}
		return out
	}
	pt := pathglob.Compile(pattern)
	for _, f := range src.Files() {
		if pt.Match(f) {
			out = append(out, f)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

// MatchScope cho biết tệp rel thuộc phạm vi đường dẫn của mục.
func (s Scope) MatchScope(rel string) bool {
	if s.Global() {
		return true
	}
	for _, p := range s.Paths {
		p = strings.TrimSuffix(cleanRel(p), "/")
		if pathglob.HasMeta(p) {
			if pathglob.Compile(p).Match(rel) {
				return true
			}
		} else if rel == p || strings.HasPrefix(rel, p+"/") {
			return true
		}
	}
	return false
}

func regionOf(e Evidence, b []byte) []byte {
	from, to, ok, err := e.LineRange()
	if !ok || err != nil {
		return b
	}
	lines := normalizeLines(b)
	if to > len(lines) {
		return b
	}
	return []byte(strings.Join(lines[from-1:to], "\n"))
}

// containsSymbol: ký hiệu có mặt trong văn bản. Ký hiệu định danh đầy đủ ("Lớp.phương_thức",
// "pkg::fn", "Type#method") không xuất hiện nguyên văn trong mã — định nghĩa chỉ ghi tên cuối
// (def __post_init__) — nên khớp theo tên cuối; vị trí đúng trong lớp do symbols.Locate và
// fingerprint bảo đảm.
func containsSymbol(b []byte, sym string) bool {
	if containsWord(b, sym) {
		return true
	}
	sym = strings.TrimSpace(sym)
	if i := strings.LastIndexAny(sym, ".:#"); i >= 0 && i+1 < len(sym) {
		return containsWord(b, sym[i+1:])
	}
	return false
}

func containsWord(b []byte, word string) bool {
	word = strings.TrimSpace(word)
	if word == "" {
		return true
	}
	re, err := regexp.Compile(`(^|[^\pL\pN_])` + regexp.QuoteMeta(word) + `($|[^\pL\pN_])`)
	if err != nil {
		return strings.Contains(string(b), word)
	}
	return re.Match(b)
}

func parseFenceInfo(info string) (string, map[string]string) {
	attrs := map[string]string{}
	f := strings.Fields(info)
	lang := ""
	for i, tok := range f {
		if k, v, ok := strings.Cut(tok, "="); ok {
			attrs[strings.ToLower(k)] = strings.Trim(v, `"'`)
		} else if i == 0 {
			lang = tok
		}
	}
	return lang, attrs
}

var wsRe = regexp.MustCompile(`\s+`)

func normWS(s string) string { return strings.TrimSpace(wsRe.ReplaceAllString(s, " ")) }

// significantLines trả về các dòng mã đủ đặc trưng để đối chiếu (bỏ dòng ngắn,
// dấu ngoặc, dấu ba chấm thay cho mã lược bỏ).
func significantLines(code string) []string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(code, "\r\n", "\n"), "\n") {
		n := normWS(l)
		if len(n) < 8 || strings.Contains(n, "...") || strings.Contains(n, "…") {
			continue
		}
		if strings.Trim(n, "{}()[];, ") == "" {
			continue
		}
		out = append(out, n)
	}
	return out
}

func missingLines(sig []string, texts [][]byte) []string {
	var norm []string
	for _, t := range texts {
		norm = append(norm, normWS(string(t)))
	}
	var miss []string
	for _, s := range sig {
		found := false
		for _, t := range norm {
			if strings.Contains(t, s) {
				found = true
				break
			}
		}
		if !found {
			miss = append(miss, s)
		}
	}
	return miss
}

func normText(s string) string { return strings.ToLower(normWS(s)) }
