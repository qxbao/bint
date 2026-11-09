// Package ctxengine là Context Engine (mục 2.3, 8.2): nhận mô tả tác vụ hoặc
// đường dẫn tệp, chọn tri thức liên quan, kiểm tra hiệu lực bằng chứng và dựng gói
// ngữ cảnh ngắn theo ngân sách. Chỉ đọc: không bao giờ đổi trạng thái tri thức.
package ctxengine

import (
	"context"
	"database/sql"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"bint/internal/kindex"
	"bint/internal/knowledge"
	"bint/internal/pathglob"
)

// Nhãn trình bày của một mục trong gói ngữ cảnh.
const (
	LabelRule      = "rule"      // quy tắc bắt buộc: rule đã duyệt, bằng chứng còn khớp
	LabelWarning   = "warning"   // điểm dễ sai đã duyệt
	LabelVerified  = "verified"  // pattern, fact, decision, lesson đã xác thực
	LabelReference = "reference" // chưa duyệt hoặc có thể lỗi thời: chỉ tham khảo
)

var labelTier = map[string]int{LabelRule: 0, LabelWarning: 1, LabelVerified: 2, LabelReference: 3}

// Engine giữ tri thức và chỉ mục của một repository.
type Engine struct {
	// Items là mọi mục đọc từ tệp (nguồn chuẩn).
	Items []*knowledge.Item
	// Src là trạng thái mã nguồn dùng để kiểm tra bằng chứng (thường là cây làm việc).
	Src knowledge.Source
	// DB chứa chỉ mục FTS5 (knowledge_fts); cần cho truy xuất theo tác vụ.
	DB *sql.DB
	// MaxTokens là ngân sách gói ngữ cảnh (context.max_tokens).
	MaxTokens int
	// PathMaxItems giới hạn số mục cho truy xuất theo đường dẫn.
	PathMaxItems int
	// ExcludeEvalCase loại mục rút ra từ tác vụ đánh giá này (mục 12.3).
	ExcludeEvalCase string
	// IncludeCandidates cho phép mục chưa duyệt (gắn nhãn tham khảo). Mặc định bật
	// cho truy xuất theo tác vụ; tắt cho chèn theo đường dẫn trừ khi đặt.
	PathIncludeCandidates bool

	byID map[string]*knowledge.Item
}

// Entry là một mục trong gói ngữ cảnh.
type Entry struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"`
	Status   string   `json:"status"`
	Label    string   `json:"label"`
	Title    string   `json:"title"`
	Body     string   `json:"body"`
	Scope    []string `json:"scope,omitempty"`
	Origin   string   `json:"origin"`
	Reviewed string   `json:"reviewed_by,omitempty"`
	Evidence []string `json:"evidence,omitempty"`
	// Reasons: vì sao mục được chọn (path:<tệp>, fts, domain:<x>).
	Reasons []string `json:"reasons"`
	// Notes: cảnh báo đi kèm (chưa duyệt, bằng chứng đã đổi...).
	Notes []string `json:"notes,omitempty"`
	Score float64  `json:"score"`
	item  *knowledge.Item
}

// Omission là mục khớp nhưng không được đưa vào và lý do.
type Omission struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// Pack là gói ngữ cảnh.
type Pack struct {
	Mode      string     `json:"mode"` // task | paths
	Query     string     `json:"query,omitempty"`
	Paths     []string   `json:"paths,omitempty"`
	WorkKind  string     `json:"work_kind,omitempty"`
	Domains   []string   `json:"domains,omitempty"`
	Entries   []Entry    `json:"entries"`
	Omitted   []Omission `json:"omitted,omitempty"`
	Tokens    int        `json:"tokens_estimate"`
	Budget    int        `json:"budget_tokens"`
	Truncated bool       `json:"truncated"`
}

// IDs trả về ID các mục trong gói.
func (p Pack) IDs() []string {
	out := make([]string, len(p.Entries))
	for i, e := range p.Entries {
		out[i] = e.ID
	}
	return out
}

func (e *Engine) index() {
	if e.byID == nil {
		e.byID = map[string]*knowledge.Item{}
		for _, it := range e.Items {
			e.byID[it.ID] = it
		}
	}
}

// Get trả về mục theo ID (đầy đủ, tiền tố hoặc hậu tố).
func (e *Engine) Get(ref string) (*knowledge.Item, error) { return knowledge.Find(e.Items, ref) }

// eligible áp các quy tắc loại trừ chung: mục đã loại bỏ, mục gắn cờ bên thứ ba
// chưa duyệt (mục 8.1/15.5), mục rút ra từ chính tác vụ đánh giá.
func (e *Engine) eligible(it *knowledge.Item) (bool, string) {
	switch {
	case it.Status == knowledge.StatusDeprecated:
		return false, "deprecated"
	case it.Origin.ThirdParty && it.Status != knowledge.StatusVerified:
		return false, "đầu vào bên thứ ba, chưa duyệt"
	case e.ExcludeEvalCase != "" && it.Origin.EvalCase == e.ExcludeEvalCase:
		return false, "rút ra từ chính tác vụ đánh giá"
	}
	return true, ""
}

// classify gán nhãn và ghi chú theo trạng thái và độ tươi của bằng chứng.
func (e *Engine) classify(it *knowledge.Item) (string, []string) {
	var notes []string
	if it.Status == knowledge.StatusCandidate {
		return LabelReference, []string{"chưa được duyệt — kiểm tra lại trước khi dựa vào"}
	}
	if it.Status == knowledge.StatusStale {
		n := "lỗi thời: bằng chứng đã đổi, chờ xác thực lại"
		if it.StatusReason != "" {
			n += " (" + it.StatusReason + ")"
		}
		return LabelReference, []string{n}
	}
	// verified: kiểm tra bằng chứng ngay lúc truy xuất (mục 8.2 bước 5).
	if e.Src != nil {
		for _, ev := range it.Evidence {
			b, ok := e.Src.Read(ev.Path)
			if !ok {
				return LabelReference, []string{fmt.Sprintf("bằng chứng %s không còn — có thể lỗi thời", ev.Path)}
			}
			if c := knowledge.CheckEvidence(ev, b); c.Status == knowledge.EvChanged {
				return LabelReference, []string{fmt.Sprintf("bằng chứng %s đã đổi kể từ khi xác thực — có thể lỗi thời", ev.String())}
			}
		}
	}
	switch {
	case it.Origin.ThirdParty:
		notes = append(notes, "có đầu vào bên thứ ba: không phải quy tắc bắt buộc")
		return LabelVerified, notes
	case it.Type == knowledge.TypeRule:
		return LabelRule, notes
	case it.Type == knowledge.TypeWarning:
		return LabelWarning, notes
	}
	return LabelVerified, notes
}

func (e *Engine) entry(it *knowledge.Item, reasons []string, score float64) Entry {
	label, notes := e.classify(it)
	en := Entry{ID: it.ID, Type: it.Type, Status: it.Status, Label: label, Title: it.Title, Body: it.Body,
		Scope: it.Scope.Paths, Origin: it.Origin.Kind, Reviewed: it.ReviewedBy, Reasons: reasons, Notes: notes,
		Score: score, item: it}
	for _, ev := range it.Evidence {
		en.Evidence = append(en.Evidence, ev.String())
	}
	return en
}

// NormalizePath chuẩn hoá đường dẫn tương đối repository (dấu '/', bỏ "./").
func NormalizePath(p string) string {
	p = strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
	if p == "" {
		return ""
	}
	p = path.Clean(p)
	return strings.TrimPrefix(p, "./")
}

// specificity: phạm vi càng cụ thể (tiền tố cố định dài) càng được ưu tiên.
func specificity(scope knowledge.Scope, rel string) int {
	best := 0
	for _, p := range scope.Paths {
		p = NormalizePath(p)
		match := false
		if pathglob.HasMeta(p) {
			match = pathglob.Compile(p).Match(rel)
		} else {
			match = rel == p || strings.HasPrefix(rel, strings.TrimSuffix(p, "/")+"/")
		}
		if !match {
			continue
		}
		fixed := p
		if i := strings.IndexAny(p, "*?["); i >= 0 {
			fixed = p[:i]
		}
		if n := len(fixed); n > best {
			best = n
		}
	}
	return best
}

// ForPaths trả về quy tắc và cảnh báo có phạm vi khớp các đường dẫn (mục 8.2):
// chỉ mục có phạm vi đường dẫn cụ thể (mục toàn cục đã nằm trong AGENTS.md), ưu
// tiên rule và warning, bỏ mục trong exclude (đã chèn trong cùng phiên).
func (e *Engine) ForPaths(paths []string, exclude map[string]bool) Pack {
	e.index()
	limit := e.PathMaxItems
	if limit <= 0 {
		limit = 5
	}
	p := Pack{Mode: "paths", Budget: e.budget()}
	for _, x := range paths {
		if n := NormalizePath(x); n != "" && knowledge.ValidRelPath(n) == nil {
			p.Paths = append(p.Paths, n)
		}
	}
	var cands []Entry
	for _, it := range e.Items {
		if ok, _ := e.eligible(it); !ok || it.Scope.Global() {
			continue
		}
		if it.Status == knowledge.StatusCandidate && !e.PathIncludeCandidates {
			continue
		}
		var reasons []string
		spec := 0
		for _, rel := range p.Paths {
			if it.Scope.MatchScope(rel) {
				reasons = append(reasons, "path:"+rel)
				spec = max(spec, specificity(it.Scope, rel))
			}
		}
		if len(reasons) == 0 {
			continue
		}
		if exclude[it.ID] {
			p.Omitted = append(p.Omitted, Omission{it.ID, "đã chèn trước đó trong phiên"})
			continue
		}
		cands = append(cands, e.entry(it, reasons, float64(spec)))
	}
	sortEntries(cands)
	// Đường dẫn: chỉ rule/warning (kể cả bản tham khảo của chúng) và mục đã xác thực.
	p.Entries, p.Omitted = e.pack(cands, limit, p.Omitted)
	p.Tokens = estimateTokens(p.Render())
	return p
}

// work kinds: phân loại thô loại công việc theo từ khoá (mục 8.2 bước 2). Chỉ để
// trình bày và thống kê; không dùng để lọc.
var workKinds = []struct {
	kind string
	re   *regexp.Regexp
}{
	{"bugfix", regexp.MustCompile(`(?i)\b(fix|bug|lỗi|sửa lỗi|crash|regression|hỏng|panic|exception)\b`)},
	{"test", regexp.MustCompile(`(?i)\b(test|tests|kiểm thử|spec|coverage)\b`)},
	{"refactor", regexp.MustCompile(`(?i)\b(refactor|tái cấu trúc|rename|đổi tên|cleanup|dọn)\b`)},
	{"docs", regexp.MustCompile(`(?i)\b(docs?|tài liệu|readme)\b`)},
	{"feature", regexp.MustCompile(`(?i)\b(add|implement|support|feature|thêm|hỗ trợ|tính năng)\b`)},
}

var pathHintRe = regexp.MustCompile(`[A-Za-z0-9_.\-]+(/[A-Za-z0-9_.\-]+)+|[A-Za-z0-9_\-]+\.[A-Za-z0-9]{1,5}\b`)

// ForTask dựng gói ngữ cảnh theo mô tả tác vụ (mục 8.2), có thể kèm đường dẫn gợi ý.
func (e *Engine) ForTask(ctx context.Context, desc string, hintPaths []string) (Pack, error) {
	e.index()
	desc = strings.Join(strings.Fields(desc), " ")
	p := Pack{Mode: "task", Query: desc, Budget: e.budget()}
	for _, wk := range workKinds {
		if wk.re.MatchString(desc) {
			p.WorkKind = wk.kind
			break
		}
	}
	terms := map[string]bool{}
	for _, t := range kindex.Terms(desc) {
		terms[t] = true
	}
	// Khu vực mã nguồn có khả năng liên quan (bước 3): đường dẫn nêu trong mô tả,
	// tên tệp khớp tệp có thật, và đường dẫn do người gọi chỉ định.
	areas := map[string]bool{}
	for _, h := range hintPaths {
		if n := NormalizePath(h); n != "" {
			areas[n] = true
		}
	}
	for _, m := range pathHintRe.FindAllString(desc, -1) {
		n := NormalizePath(strings.TrimRight(m, "."))
		if n == "" || knowledge.ValidRelPath(n) != nil {
			continue
		}
		if e.Src != nil && knowledge.HasFileOrDir(e.Src, n) {
			areas[n] = true
		} else if e.Src != nil && !strings.Contains(n, "/") {
			for _, f := range e.Src.Files() {
				if path.Base(f) == n {
					areas[f] = true
				}
			}
		}
	}
	for a := range areas {
		p.Paths = append(p.Paths, a)
	}
	sort.Strings(p.Paths)

	scores := map[string]float64{}
	reasons := map[string][]string{}
	add := func(id string, s float64, why string) {
		scores[id] += s
		if !containsStr(reasons[id], why) {
			reasons[id] = append(reasons[id], why)
		}
	}
	// Bước 4: từ khoá (FTS5/BM25) — chỉ giữ kết quả đủ gần kết quả tốt nhất.
	if e.DB != nil {
		hits, err := kindex.Search(ctx, e.DB, desc, kindex.SearchOptions{Limit: 60})
		if err != nil {
			return p, err
		}
		top := 0.0
		for _, h := range hits {
			top = max(top, h.Score)
		}
		for _, h := range hits {
			if top > 0 && h.Score >= 0.3*top {
				add(h.ID, 1+h.Score/top, "fts")
			}
		}
	}
	domainSeen := map[string]bool{}
	for _, it := range e.Items {
		// theo đường dẫn
		for _, a := range p.Paths {
			if !it.Scope.Global() && it.Scope.MatchScope(a) {
				add(it.ID, 2, "path:"+a)
				break
			}
		}
		// theo miền nghiệp vụ, thẻ, ký hiệu được nêu tên
		for _, d := range it.Scope.Domains {
			if terms[strings.ToLower(d)] {
				add(it.ID, 1, "domain:"+d)
				if !domainSeen[d] {
					domainSeen[d] = true
					p.Domains = append(p.Domains, d)
				}
			}
		}
		for _, tg := range it.Scope.Tags {
			if terms[strings.ToLower(tg)] {
				add(it.ID, 0.5, "tag:"+tg)
			}
		}
		for _, s := range it.Scope.Symbols {
			if strings.Contains(desc, s) {
				add(it.ID, 1.5, "symbol:"+s)
			}
		}
	}
	sort.Strings(p.Domains)

	var cands []Entry
	seenTitle := map[string]string{}
	ids := make([]string, 0, len(scores))
	for id := range scores {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if scores[ids[i]] != scores[ids[j]] {
			return scores[ids[i]] > scores[ids[j]]
		}
		return ids[i] < ids[j]
	})
	for _, id := range ids {
		it := e.byID[id]
		if it == nil {
			continue // chỉ mục cũ hơn tệp
		}
		if ok, why := e.eligible(it); !ok {
			if why != "deprecated" {
				p.Omitted = append(p.Omitted, Omission{id, why})
			}
			continue
		}
		// Bước 6: loại trùng lặp (cùng tiêu đề sau chuẩn hoá).
		key := strings.ToLower(strings.Join(strings.Fields(it.Title), " "))
		if other, dup := seenTitle[key]; dup {
			p.Omitted = append(p.Omitted, Omission{id, "trùng với " + other})
			continue
		}
		seenTitle[key] = id
		cands = append(cands, e.entry(it, reasons[id], scores[id]))
	}
	sortEntries(cands)
	p.Entries, p.Omitted = e.pack(cands, 0, p.Omitted)
	p.Tokens = estimateTokens(p.Render())
	return p, nil
}

// sortEntries: quy tắc bắt buộc trước (bước 7), rồi cảnh báo, rồi theo điểm.
func sortEntries(es []Entry) {
	sort.SliceStable(es, func(i, j int) bool {
		ti, tj := labelTier[es[i].Label], labelTier[es[j].Label]
		if ti != tj {
			return ti < tj
		}
		if es[i].Score != es[j].Score {
			return es[i].Score > es[j].Score
		}
		return es[i].ID < es[j].ID
	})
}

func (e *Engine) budget() int {
	if e.MaxTokens <= 0 {
		return 6000
	}
	return e.MaxTokens
}

// pack chọn mục theo ngân sách token (bước 8). limit > 0 giới hạn số mục.
func (e *Engine) pack(cands []Entry, limit int, omitted []Omission) ([]Entry, []Omission) {
	budget := e.budget()
	used := estimateTokens(header)
	var out []Entry
	for _, c := range cands {
		if limit > 0 && len(out) >= limit {
			omitted = append(omitted, Omission{c.ID, fmt.Sprintf("vượt giới hạn %d mục", limit)})
			continue
		}
		cost := estimateTokens(renderEntry(c))
		if used+cost > budget {
			omitted = append(omitted, Omission{c.ID, "vượt ngân sách ngữ cảnh"})
			continue
		}
		used += cost
		out = append(out, c)
	}
	return out, omitted
}

// estimateTokens ước lượng thô số token: ~3 ký tự mỗi token (tiếng Việt có dấu tốn
// token hơn tiếng Anh). Chỉ để áp ngân sách, không dùng làm số liệu đo.
func estimateTokens(s string) int { return (len([]rune(s)) + 2) / 3 }

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
