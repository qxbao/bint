package task

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"bint/internal/fsafe"
)

// Loại và mức độ phát hiện review (mục 6.3).
var (
	FindingKinds      = []string{"bug", "risk", "suggestion"}
	FindingSeverities = []string{"critical", "high", "medium", "low"}
)

// Finding là một phát hiện của b-review.
type Finding struct {
	// ID dạng <task-id>.<n>, duy nhất toàn repository.
	ID       string `json:"id" yaml:"id"`
	Kind     string `json:"kind" yaml:"kind"`
	Severity string `json:"severity" yaml:"severity"`
	Title    string `json:"title" yaml:"title"`
	Path     string `json:"path,omitempty" yaml:"path"`
	Lines    string `json:"lines,omitempty" yaml:"lines"`
	Evidence string `json:"evidence" yaml:"evidence"`
	Body     string `json:"body,omitempty" yaml:"body"`
	// Criterion là tiêu chí chấp nhận liên quan (từ 1), nếu có.
	Criterion int `json:"criterion,omitempty" yaml:"criterion"`
}

// Blocking: phát hiện nghiêm trọng (lỗi hoặc rủi ro mức critical/high) — b-do quay
// lại b-feat, và là tín hiệu kết quả tiêu cực mức trung bình (mục 9.6).
func (f Finding) Blocking() bool {
	return f.Kind != "suggestion" && (f.Severity == "critical" || f.Severity == "high")
}

// ReviewRound là một vòng review.
type ReviewRound struct {
	Round    int       `json:"round"`
	At       string    `json:"at"`
	By       string    `json:"by"`
	RunID    string    `json:"run_id,omitempty"`
	Base     string    `json:"base,omitempty"`
	Summary  string    `json:"summary"`
	Findings []Finding `json:"findings"`
}

// Blocking trả về các phát hiện nghiêm trọng của vòng.
func (r ReviewRound) Blocking() []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Blocking() {
			out = append(out, f)
		}
	}
	return out
}

// Review là toàn bộ các vòng review của tác vụ (review.json).
type Review struct {
	TaskID string        `json:"task_id"`
	Rounds []ReviewRound `json:"rounds"`
}

// Last trả về vòng gần nhất (nil nếu chưa có).
func (r *Review) Last() *ReviewRound {
	if r == nil || len(r.Rounds) == 0 {
		return nil
	}
	return &r.Rounds[len(r.Rounds)-1]
}

// Find tìm phát hiện theo ID.
func (r *Review) Find(id string) (*Finding, *ReviewRound) {
	for i := range r.Rounds {
		for j := range r.Rounds[i].Findings {
			if strings.EqualFold(r.Rounds[i].Findings[j].ID, id) {
				return &r.Rounds[i].Findings[j], &r.Rounds[i]
			}
		}
	}
	return nil, nil
}

// LoadReview đọc review.json của tác vụ (rỗng nếu chưa có).
func (t *Task) LoadReview() (*Review, error) {
	r := &Review{TaskID: t.State.ID}
	data, err := os.ReadFile(filepath.Join(t.Dir, ReviewFile))
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, r); err != nil {
		return nil, fmt.Errorf("%s: %w", ReviewFile, err)
	}
	return r, nil
}

// Submission là dữ liệu b-review gửi lên (`bint task review <id> -`).
type Submission struct {
	Summary  string    `json:"summary" yaml:"summary"`
	Findings []Finding `json:"findings" yaml:"findings"`
}

// decodeSubmission đọc JSON (bắt đầu bằng `{`) hoặc YAML block. Claude Code chặn lệnh
// shell chứa `{"` ("Contains brace with quote character"), nên heredoc JSON không dùng
// được ở đó; YAML là tập cha của JSON và không cần ngoặc nhọn. JSON giữ nguyên hành vi.
func decodeSubmission(data []byte) (Submission, error) {
	var s Submission
	if t := bytes.TrimSpace(data); len(t) > 0 && t[0] != '{' {
		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true)
		return s, dec.Decode(&s)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return s, dec.Decode(&s)
}

var linesRe = regexp.MustCompile(`^[1-9][0-9]*(-[1-9][0-9]*)?$`)

// ParseSubmission đọc và kiểm tra dữ liệu review. root dùng để kiểm tra đường dẫn
// và vùng dòng có thật (tệp bị xoá trong diff vẫn được chấp nhận nếu không nêu dòng).
func ParseSubmission(data []byte, root string) (Submission, []string) {
	s, err := decodeSubmission(data)
	if err != nil {
		return s, []string{"JSON/YAML không hợp lệ: " + err.Error() +
			` (cần {"summary": "...", "findings": [{"kind","severity","title","path","lines","evidence","body"}]}, hoặc cùng cấu trúc dạng YAML block)`}
	}
	var errs []string
	if strings.TrimSpace(s.Summary) == "" {
		errs = append(errs, "summary: không được rỗng")
	}
	for i := range s.Findings {
		f := &s.Findings[i]
		at := fmt.Sprintf("findings[%d]", i)
		f.ID = ""
		f.Kind, f.Severity = strings.ToLower(strings.TrimSpace(f.Kind)), strings.ToLower(strings.TrimSpace(f.Severity))
		// Tiêu đề một dòng: tiêu đề được chèn vào yêu cầu của phiếu sửa (--from-findings) dưới dạng dòng danh sách.
		f.Title, f.Evidence = strings.Join(strings.Fields(f.Title), " "), strings.TrimSpace(f.Evidence)
		f.Path = strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(f.Path)), "./")
		if !slices.Contains(FindingKinds, f.Kind) {
			errs = append(errs, fmt.Sprintf("%s.kind: phải là %s", at, strings.Join(FindingKinds, " | ")))
		}
		if !slices.Contains(FindingSeverities, f.Severity) {
			errs = append(errs, fmt.Sprintf("%s.severity: phải là %s", at, strings.Join(FindingSeverities, " | ")))
		}
		if f.Title == "" {
			errs = append(errs, at+".title: không được rỗng")
		}
		if f.Evidence == "" {
			errs = append(errs, at+".evidence: bắt buộc (lập luận hoặc đầu ra kiểm chứng được)")
		}
		if f.Lines != "" && !linesRe.MatchString(f.Lines) {
			errs = append(errs, at+".lines: dạng a hoặc a-b")
		}
		if f.Path == "" {
			if f.Lines != "" {
				errs = append(errs, at+".lines: cần path")
			}
			continue
		}
		if filepath.IsAbs(f.Path) || (len(f.Path) > 1 && f.Path[1] == ':') || slices.Contains(strings.Split(f.Path, "/"), "..") {
			errs = append(errs, at+".path: phải là đường dẫn tương đối trong repository")
			continue
		}
		// path thành scope.allow của phiếu sửa (--from-findings): ký tự glob sẽ mở rộng phạm vi (ví dụ "**").
		if strings.ContainsAny(f.Path, "*?[]{}") {
			errs = append(errs, at+".path: không được chứa ký tự glob (*, ?, [, {)")
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f.Path)))
		if err != nil {
			if f.Lines != "" {
				errs = append(errs, fmt.Sprintf("%s.path: %s không đọc được (tệp đã xoá thì bỏ lines)", at, f.Path))
			}
			continue
		}
		if f.Lines != "" {
			n := bytes.Count(data, []byte("\n"))
			if len(data) > 0 && data[len(data)-1] != '\n' {
				n++
			}
			a, b, _ := strings.Cut(f.Lines, "-")
			ai, _ := strconv.Atoi(a)
			bi := ai
			if b != "" {
				bi, _ = strconv.Atoi(b)
			}
			if bi < ai || bi > n {
				errs = append(errs, fmt.Sprintf("%s.lines: %s ngoài phạm vi %s (%d dòng)", at, f.Lines, f.Path, n))
			}
		}
	}
	return s, errs
}

// AddRound gán ID cho phát hiện, ghi vòng review mới vào review.json và review.md.
// Gọi trong Store.Update (state.NextFinding/ReviewRounds được cập nhật).
func (t *Task) AddRound(sub Submission, by, runID, at string) (ReviewRound, error) {
	rev, err := t.LoadReview()
	if err != nil {
		return ReviewRound{}, err
	}
	t.State.ReviewRounds++
	r := ReviewRound{Round: t.State.ReviewRounds, At: at, By: by, RunID: runID, Base: t.State.Base,
		Summary: strings.TrimSpace(sub.Summary), Findings: sub.Findings}
	if r.Findings == nil {
		r.Findings = []Finding{}
	}
	for i := range r.Findings {
		t.State.NextFinding++
		r.Findings[i].ID = fmt.Sprintf("%s.%d", t.State.ID, t.State.NextFinding)
	}
	rev.Rounds = append(rev.Rounds, r)
	data, err := json.MarshalIndent(rev, "", "  ")
	if err != nil {
		return r, err
	}
	if err := fsafe.WriteAtomic(filepath.Join(t.Dir, ReviewFile), append(data, '\n'), 0o600); err != nil {
		return r, err
	}
	return r, fsafe.WriteAtomic(filepath.Join(t.Dir, ReviewMD), []byte(RenderReview(rev)), 0o600)
}

// RenderReview dựng báo cáo Markdown.
func RenderReview(rev *Review) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Review tác vụ %s\n", rev.TaskID)
	for _, r := range rev.Rounds {
		fmt.Fprintf(&b, "\n## Vòng %d — %s\n\n%s\n", r.Round, r.At, r.Summary)
		if len(r.Findings) == 0 {
			b.WriteString("\nKhông có phát hiện. (Không phải bằng chứng tuyệt đối rằng thay đổi an toàn.)\n")
			continue
		}
		for _, f := range r.Findings {
			loc := f.Path
			if f.Lines != "" {
				loc += ":" + f.Lines
			}
			fmt.Fprintf(&b, "\n### [%s] %s/%s — %s\n\n", f.ID, f.Kind, f.Severity, f.Title)
			if loc != "" {
				fmt.Fprintf(&b, "- Vị trí: `%s`\n", loc)
			}
			fmt.Fprintf(&b, "- Bằng chứng: %s\n", f.Evidence)
			if f.Criterion > 0 {
				fmt.Fprintf(&b, "- Tiêu chí: %d\n", f.Criterion)
			}
			if f.Body != "" {
				fmt.Fprintf(&b, "\n%s\n", strings.TrimSpace(f.Body))
			}
		}
	}
	return b.String()
}

// SplitFindingID tách "<task-id>.<n>" thành tiền tố task và ID đầy đủ.
func SplitFindingID(id string) (string, bool) {
	taskRef, n, ok := strings.Cut(strings.TrimSpace(id), ".")
	if !ok || taskRef == "" {
		return "", false
	}
	if _, err := strconv.Atoi(n); err != nil {
		return "", false
	}
	return taskRef, true
}
