// Package knowledge là mô hình tri thức của Bint (mục 8 của kế hoạch): mỗi mục là
// một tệp .bint/knowledge/items/<ulid>.md gồm phần đầu YAML và nội dung Markdown.
// Tệp trong Git là nguồn chuẩn duy nhất; mọi chỉ mục (P3) đều dẫn xuất từ đây.
package knowledge

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"bint/internal/ulid"
)

// SchemaVersion là phiên bản định dạng tệp tri thức.
const SchemaVersion = 1

// Loại tri thức (mục 8.1).
const (
	TypeRule     = "rule"
	TypeFact     = "fact"
	TypePattern  = "pattern"
	TypeDecision = "decision"
	TypeLesson   = "lesson"
	TypeWarning  = "warning"
)

// Types là danh sách loại hợp lệ, theo thứ tự ưu tiên khi trình bày.
var Types = []string{TypeRule, TypeWarning, TypePattern, TypeFact, TypeDecision, TypeLesson}

// Trạng thái hiệu lực (mục 8.1).
const (
	StatusCandidate  = "candidate"
	StatusVerified   = "verified"
	StatusStale      = "stale"
	StatusDeprecated = "deprecated"
)

// Statuses là các trạng thái hợp lệ.
var Statuses = []string{StatusCandidate, StatusVerified, StatusStale, StatusDeprecated}

// Nguồn gốc.
const (
	OriginHuman    = "human"
	OriginAgent    = "agent"
	OriginImported = "imported"
)

// Mức tin cậy.
var Confidences = []string{"high", "medium", "low"}

// Item là một đơn vị tri thức.
type Item struct {
	Schema     int        `yaml:"schema" json:"schema"`
	ID         string     `yaml:"id" json:"id"`
	Type       string     `yaml:"type" json:"type"`
	Status     string     `yaml:"status" json:"status"`
	Title      string     `yaml:"title" json:"title"`
	Scope      Scope      `yaml:"scope" json:"scope"`
	Evidence   []Evidence `yaml:"evidence" json:"evidence"`
	Origin     Origin     `yaml:"origin" json:"origin"`
	Author     string     `yaml:"author,omitempty" json:"author,omitempty"`
	ReviewedBy string     `yaml:"reviewed_by,omitempty" json:"reviewed_by,omitempty"`
	ReviewedAt string     `yaml:"reviewed_at,omitempty" json:"reviewed_at,omitempty"`
	Confidence string     `yaml:"confidence,omitempty" json:"confidence,omitempty"`
	Version    int        `yaml:"version" json:"version"`
	CreatedAt  string     `yaml:"created_at" json:"created_at"`
	UpdatedAt  string     `yaml:"updated_at,omitempty" json:"updated_at,omitempty"`
	VerifiedAt string     `yaml:"verified_at,omitempty" json:"verified_at,omitempty"`
	// StatusReason ghi lý do của lần chuyển trạng thái gần nhất (từ chối, lỗi thời...).
	StatusReason string `yaml:"status_reason,omitempty" json:"status_reason,omitempty"`

	// Body là nội dung Markdown sau phần đầu.
	Body string `yaml:"-" json:"-"`
	// Path là đường dẫn tệp (không ghi vào tệp).
	Path string `yaml:"-" json:"-"`
}

// Scope là phạm vi áp dụng. Paths rỗng = toàn repository.
type Scope struct {
	Paths   []string `yaml:"paths,omitempty" json:"paths,omitempty"`
	Symbols []string `yaml:"symbols,omitempty" json:"symbols,omitempty"`
	Domains []string `yaml:"domains,omitempty" json:"domains,omitempty"`
	Tags    []string `yaml:"tags,omitempty" json:"tags,omitempty"`
}

// Global cho biết mục áp dụng cho toàn repository.
func (s Scope) Global() bool {
	if len(s.Paths) == 0 {
		return true
	}
	for _, p := range s.Paths {
		if p == "**" || p == "**/*" || p == "*" {
			return true
		}
	}
	return false
}

// Evidence là một nguồn bằng chứng. Lines dạng "a-b" hoặc "a" (đánh số từ 1).
type Evidence struct {
	Path        string `yaml:"path" json:"path"`
	Lines       string `yaml:"lines,omitempty" json:"lines,omitempty"`
	Symbol      string `yaml:"symbol,omitempty" json:"symbol,omitempty"`
	Commit      string `yaml:"commit,omitempty" json:"commit,omitempty"`
	Fingerprint string `yaml:"fingerprint,omitempty" json:"fingerprint,omitempty"`
	Note        string `yaml:"note,omitempty" json:"note,omitempty"`
}

// Origin ghi nguồn gốc của mục (mục 15.5).
type Origin struct {
	Kind string `yaml:"kind" json:"kind"`
	// RunID / Session: lượt chạy hoặc phiên agent đã tạo mục này.
	RunID   string `yaml:"run_id,omitempty" json:"run_id,omitempty"`
	Session string `yaml:"session,omitempty" json:"session,omitempty"`
	// Commit là HEAD của repository lúc tạo.
	Commit string `yaml:"commit,omitempty" json:"commit,omitempty"`
	// Inputs là các đầu vào agent đã đọc khi tạo mục.
	Inputs []string `yaml:"inputs,omitempty" json:"inputs,omitempty"`
	// ThirdParty = true khi đầu vào có nội dung bên thứ ba (dependency, tài liệu
	// ngoài, issue). Mục bị gắn cờ không được chèn như quy tắc bắt buộc.
	ThirdParty bool `yaml:"third_party,omitempty" json:"third_party,omitempty"`
	// EvalCase: mục (thường là lesson) rút ra từ tác vụ đánh giá này; không bao giờ
	// được dùng khi chấm lại chính tác vụ đó (mục 12.3).
	EvalCase string `yaml:"eval_case,omitempty" json:"eval_case,omitempty"`
	// EvalCases: mọi tác vụ đánh giá mà mục rút ra từ đó (bài học gom nhiều tác vụ). Dùng
	// FromEvalCase để kiểm tra; EvalCase giữ cho mục cũ chỉ có một tác vụ.
	EvalCases []string `yaml:"eval_cases,omitempty" json:"eval_cases,omitempty"`
	// Provenance: cách mục được sinh ra khi không có bằng chứng tệp (hiện chỉ
	// ProvenanceFailureLesson: bài học rút từ các lượt thất bại, bằng chứng là Runs).
	Provenance string `yaml:"provenance,omitempty" json:"provenance,omitempty"`
	// Runs: lượt chạy làm bằng chứng cho mục có Provenance (các lượt thất bại trong nhóm).
	Runs []string `yaml:"runs,omitempty" json:"runs,omitempty"`
}

// ProvenanceFailureLesson là nguồn của bài học rút từ lượt thất bại (kế hoạch E).
const ProvenanceFailureLesson = "failure_lesson"

// FromEvalCase cho biết mục rút ra từ tác vụ đánh giá id (một trong EvalCase/EvalCases).
// Mục như vậy không bao giờ được dùng khi chấm lại chính tác vụ đó (mục 12.3).
func (o Origin) FromEvalCase(id string) bool {
	if id == "" {
		return false
	}
	return o.EvalCase == id || slices.Contains(o.EvalCases, id)
}

// LessonFromRuns cho biết mục là bài học sinh từ lượt thất bại: không cần bằng chứng tệp,
// bằng chứng là danh sách lượt (Runs) của chính nó.
func (it *Item) LessonFromRuns() bool {
	return it.Type == TypeLesson && it.Origin.Provenance == ProvenanceFailureLesson && len(it.Origin.Runs) > 0
}

// LineRange trả về vùng dòng [from, to] của bằng chứng; ok=false nếu không có.
func (e Evidence) LineRange() (from, to int, ok bool, err error) {
	if strings.TrimSpace(e.Lines) == "" {
		return 0, 0, false, nil
	}
	a, b, found := strings.Cut(strings.TrimSpace(e.Lines), "-")
	from, err = strconv.Atoi(strings.TrimSpace(a))
	if err != nil {
		return 0, 0, false, fmt.Errorf("lines %q không hợp lệ", e.Lines)
	}
	to = from
	if found {
		if to, err = strconv.Atoi(strings.TrimSpace(b)); err != nil {
			return 0, 0, false, fmt.Errorf("lines %q không hợp lệ", e.Lines)
		}
	}
	if from < 1 || to < from {
		return 0, 0, false, fmt.Errorf("lines %q không hợp lệ", e.Lines)
	}
	return from, to, true, nil
}

// String trình bày bằng chứng dạng path[:a-b][#symbol].
func (e Evidence) String() string {
	s := e.Path
	if e.Lines != "" {
		s += ":" + e.Lines
	}
	if e.Symbol != "" {
		s += "#" + e.Symbol
	}
	return s
}

// ParseEvidence đọc chuỗi path[:a-b][#symbol].
func ParseEvidence(spec string) (Evidence, error) {
	spec = strings.TrimSpace(spec)
	var e Evidence
	if i := strings.LastIndex(spec, "#"); i >= 0 {
		e.Symbol = strings.TrimSpace(spec[i+1:])
		spec = spec[:i]
	}
	// Dấu ':' cuối cùng theo sau là số → vùng dòng (tránh nhầm ổ đĩa Windows).
	if i := strings.LastIndex(spec, ":"); i > 0 {
		tail := spec[i+1:]
		if tail != "" && strings.Trim(tail, "0123456789-") == "" {
			e.Lines = tail
			spec = spec[:i]
		}
	}
	e.Path = cleanRel(spec)
	if e.Path == "" {
		return e, fmt.Errorf("bằng chứng %q thiếu đường dẫn", spec)
	}
	if _, _, _, err := e.LineRange(); err != nil {
		return e, err
	}
	return e, nil
}

func cleanRel(p string) string {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	if p == "" {
		return ""
	}
	p = path.Clean(p)
	return strings.TrimPrefix(p, "./")
}

// ValidRelPath kiểm tra đường dẫn tương đối nằm trong repository.
func ValidRelPath(p string) error {
	if p == "" {
		return errors.New("đường dẫn rỗng")
	}
	if strings.HasPrefix(p, "/") || (len(p) > 1 && p[1] == ':') {
		return fmt.Errorf("%q phải là đường dẫn tương đối trong repository", p)
	}
	if slices.Contains(strings.Split(strings.ReplaceAll(p, `\`, "/"), "/"), "..") {
		return fmt.Errorf("%q nằm ngoài repository", p)
	}
	return nil
}

// ValidRev cho biết rev dùng được làm tham số revision của git: không rỗng, không bắt đầu bằng "-" (git sẽ đọc thành
// tuỳ chọn, ví dụ --output=<tệp> ghi đè tệp) và không chứa khoảng trắng hay ký tự điều khiển. Trường commit của mục tri
// thức đến từ tệp do agent hoặc PR viết nên phải qua kiểm tra này trước khi đưa vào lệnh git.
func ValidRev(rev string) bool {
	if rev == "" || rev[0] == '-' {
		return false
	}
	for _, c := range rev {
		if c <= ' ' || c == 0x7f {
			return false
		}
	}
	return true
}

// Validate kiểm tra ràng buộc schema và trả về mọi vấn đề.
func (it *Item) Validate() []string {
	var errs []string
	add := func(format string, a ...any) { errs = append(errs, fmt.Sprintf(format, a...)) }
	if it.Schema != SchemaVersion {
		add("schema: cần %d, nhận %d", SchemaVersion, it.Schema)
	}
	if !ulid.Valid(it.ID) {
		add("id: %q không phải ULID", it.ID)
	}
	if !slices.Contains(Types, it.Type) {
		add("type: %q không hợp lệ (%s)", it.Type, strings.Join(Types, ", "))
	}
	if !slices.Contains(Statuses, it.Status) {
		add("status: %q không hợp lệ (%s)", it.Status, strings.Join(Statuses, ", "))
	}
	if strings.TrimSpace(it.Title) == "" {
		add("title: không được rỗng")
	}
	if strings.ContainsAny(it.Title, "\r\n") {
		// Tiêu đề in thành một dòng trong AGENTS.md và ngữ cảnh agent: xuống dòng giả được mục/tiêu đề mới.
		add("title: phải nằm trên một dòng")
	}
	if strings.TrimSpace(it.Body) == "" {
		add("nội dung: không được rỗng")
	}
	if !slices.Contains([]string{OriginHuman, OriginAgent, OriginImported}, it.Origin.Kind) {
		add("origin.kind: %q không hợp lệ (human, agent, imported)", it.Origin.Kind)
	}
	if it.Confidence != "" && !slices.Contains(Confidences, it.Confidence) {
		add("confidence: %q không hợp lệ (high, medium, low)", it.Confidence)
	}
	if it.Version < 1 {
		add("version: phải >= 1")
	}
	for _, p := range it.Scope.Paths {
		if err := ValidRelPath(p); err != nil {
			add("scope.paths: %v", err)
		}
	}
	for i, e := range it.Evidence {
		if err := ValidRelPath(e.Path); err != nil {
			add("evidence[%d].path: %v", i, err)
		}
		if _, _, _, err := e.LineRange(); err != nil {
			add("evidence[%d]: %v", i, err)
		}
		if e.Commit != "" && !ValidRev(e.Commit) {
			add("evidence[%d].commit: %q không phải revision hợp lệ", i, e.Commit)
		}
	}
	if it.Origin.Commit != "" && !ValidRev(it.Origin.Commit) {
		add("origin.commit: %q không phải revision hợp lệ", it.Origin.Commit)
	}
	if it.Origin.Provenance != "" && it.Origin.Provenance != ProvenanceFailureLesson {
		add("origin.provenance: %q không hợp lệ (%s)", it.Origin.Provenance, ProvenanceFailureLesson)
	}
	if it.Origin.Provenance != "" && !it.LessonFromRuns() {
		add("origin.provenance: chỉ dùng cho mục lesson có origin.runs")
	}
	// Bài học từ thất bại không có bằng chứng tệp: bằng chứng là các lượt (origin.runs).
	if it.Origin.Kind == OriginAgent && len(it.Evidence) == 0 && !it.LessonFromRuns() {
		add("evidence: mục do agent tạo phải có ít nhất một bằng chứng")
	}
	if it.Status == StatusVerified && it.Type == TypeRule && it.ReviewedBy == "" && it.Origin.Kind != OriginHuman {
		add("reviewed_by: quy tắc verified phải có người duyệt")
	}
	return errs
}

const fmDelim = "---"

// Parse đọc một tệp tri thức.
func Parse(data []byte) (*Item, error) {
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	s = strings.TrimPrefix(s, string(rune(0xFEFF)))
	if !strings.HasPrefix(s, fmDelim+"\n") {
		return nil, errors.New("thiếu phần đầu YAML (tệp phải bắt đầu bằng ---)")
	}
	rest := s[len(fmDelim)+1:]
	end := strings.Index(rest, "\n"+fmDelim+"\n")
	var head, body string
	switch {
	case end >= 0:
		head, body = rest[:end], rest[end+len(fmDelim)+2:]
	case strings.HasSuffix(rest, "\n"+fmDelim):
		head = strings.TrimSuffix(rest, "\n"+fmDelim)
	default:
		return nil, errors.New("phần đầu YAML không có dấu --- kết thúc")
	}
	var it Item
	dec := yaml.NewDecoder(strings.NewReader(head))
	dec.KnownFields(true)
	if err := dec.Decode(&it); err != nil {
		return nil, fmt.Errorf("phần đầu YAML: %w", err)
	}
	it.Body = strings.TrimSpace(body)
	return &it, nil
}

// Marshal ghi mục thành nội dung tệp.
func (it *Item) Marshal() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(fmDelim + "\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(it); err != nil {
		return nil, err
	}
	enc.Close()
	buf.WriteString(fmDelim + "\n\n")
	buf.WriteString(strings.TrimSpace(it.Body))
	buf.WriteString("\n")
	return buf.Bytes(), nil
}

// Summary trả về dòng tóm tắt ngắn.
func (it *Item) Summary() string {
	return fmt.Sprintf("%s [%s/%s] %s", it.ID, it.Type, it.Status, it.Title)
}

// Active cho biết mục còn được dùng (không bị loại bỏ).
func (it *Item) Active() bool { return it.Status != StatusDeprecated }
