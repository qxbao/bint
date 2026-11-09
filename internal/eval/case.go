// Package eval là khung đánh giá của Bint (P1, mục 12 của kế hoạch): khai thác
// tác vụ từ lịch sử Git, dựng môi trường cô lập tại commit gốc, chạy nhiều nhánh
// × nhiều lần lặp cho một harness và báo cáo so sánh theo cặp kèm khoảng tin cậy.
//
// Nguồn chuẩn của tác vụ đã duyệt là tệp YAML trong .bint/evals/cases/ (có thể
// commit). Ứng viên chưa duyệt chứa thông điệp commit gốc nên chỉ nằm trong thư
// mục trạng thái cục bộ. Kết quả chạy nằm trong SQLite.
package eval

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"bint/internal/fsafe"
)

// Trạng thái của ứng viên.
const (
	StatusCandidate = "candidate"
	StatusApproved  = "approved"
	StatusRejected  = "rejected"
)

// Các tập con của tập đánh giá (mục 12.2).
const (
	SplitBuild      = "build"
	SplitValidation = "validation"
	SplitTest       = "test"
)

// Splits là danh sách tập con hợp lệ.
var Splits = []string{SplitBuild, SplitValidation, SplitTest}

// CaseSchemaVersion là phiên bản định dạng tệp tác vụ.
const CaseSchemaVersion = 1

// Case là một tác vụ đánh giá được khai thác từ một commit.
type Case struct {
	Schema int    `yaml:"schema"`
	ID     string `yaml:"id"`
	Status string `yaml:"status"`
	Split  string `yaml:"split,omitempty"`

	Source Source `yaml:"source"`
	// Prompt là đề bài đưa cho agent; đã được viết lại để không lộ lời giải.
	Prompt string `yaml:"prompt"`
	// OriginalMessage chỉ có trong ứng viên (thư mục trạng thái) để người duyệt đối chiếu.
	OriginalMessage string `yaml:"original_message,omitempty"`

	// TestFiles là tệp kiểm thử của commit, được áp vào sau khi agent làm xong để chấm.
	TestFiles   []string `yaml:"test_files"`
	SourceFiles []string `yaml:"source_files"`
	AuxFiles    []string `yaml:"aux_files,omitempty"`
	Stats       Stats    `yaml:"stats"`
	// TestCommand ghi đè eval.test_command cho riêng tác vụ này. Hỗ trợ
	// {test_files} và {test_dirs}.
	TestCommand string `yaml:"test_command,omitempty"`

	Leak         LeakReport    `yaml:"leak_check"`
	Verification *Verification `yaml:"verification,omitempty"`

	MinedAt      string `yaml:"mined_at"`
	ReviewedBy   string `yaml:"reviewed_by,omitempty"`
	ReviewedAt   string `yaml:"reviewed_at,omitempty"`
	RejectReason string `yaml:"reject_reason,omitempty"`
}

// Source là commit gốc của tác vụ.
type Source struct {
	Commit  string `yaml:"commit"`
	Base    string `yaml:"base"` // commit cha: trạng thái agent bắt đầu
	Author  string `yaml:"author"`
	Date    string `yaml:"date"`
	Subject string `yaml:"subject"`
}

// Stats là quy mô thay đổi của commit.
type Stats struct {
	Files       int `yaml:"files"`
	SourceLines int `yaml:"source_lines"`
	TestLines   int `yaml:"test_lines"`
}

// Verification là kết quả kiểm chứng "kiểm thử mới thất bại trên commit cha và
// qua trên commit đó".
type Verification struct {
	Status      string `yaml:"status"` // verified | failed | error
	FailBefore  bool   `yaml:"fail_before"`
	PassAfter   bool   `yaml:"pass_after"`
	AfterRuns   int    `yaml:"after_runs"`
	Detail      string `yaml:"detail,omitempty"`
	TestCommand string `yaml:"test_command"`
	VerifiedAt  string `yaml:"verified_at"`
}

// Verified cho biết tác vụ đã qua kiểm chứng fail→pass.
func (c *Case) Verified() bool { return c.Verification != nil && c.Verification.Status == "verified" }

// Fingerprint băm các trường quyết định nội dung phép đo; báo cáo dùng nó để
// phát hiện tác vụ bị sửa giữa các lần chạy.
func (c *Case) Fingerprint() string {
	h := sha256.New()
	for _, s := range []string{c.Source.Commit, c.Source.Base, c.Prompt, c.TestCommand, strings.Join(c.TestFiles, "\n")} {
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// CaseID tính ID ổn định từ commit nên khai thác lại không tạo trùng.
func CaseID(commit string) string {
	if len(commit) > 12 {
		commit = commit[:12]
	}
	return "c" + commit
}

// AssignSplit chia tác vụ vào tập con một cách tất định theo ID (50/25/25).
func AssignSplit(id string) string {
	sum := sha256.Sum256([]byte("bint-split:" + id))
	switch v := int(sum[0]) % 4; {
	case v < 2:
		return SplitBuild
	case v == 2:
		return SplitValidation
	default:
		return SplitTest
	}
}

// Store quản lý tệp ứng viên và tác vụ đã duyệt.
type Store struct {
	CandidatesDir string // <state>/eval/candidates
	CasesDir      string // <repo>/.bint/evals/cases
}

// NewStore dựng Store từ thư mục trạng thái và thư mục .bint của repository.
func NewStore(stateRoot, bintDir string) Store {
	return Store{
		CandidatesDir: filepath.Join(stateRoot, "eval", "candidates"),
		CasesDir:      filepath.Join(bintDir, "evals", "cases"),
	}
}

func readCase(path string) (*Case, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Case
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if c.ID == "" || c.Source.Commit == "" || c.Source.Base == "" {
		return nil, fmt.Errorf("%s: thiếu id hoặc source.commit/base", path)
	}
	if want := strings.TrimSuffix(filepath.Base(path), ".yml"); want != c.ID {
		return nil, fmt.Errorf("%s: id %q không khớp tên tệp", path, c.ID)
	}
	return &c, nil
}

func writeCase(path string, c *Case, perm os.FileMode, header string) error {
	var buf bytes.Buffer
	buf.WriteString(header)
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(c); err != nil {
		return err
	}
	enc.Close()
	return fsafe.WriteAtomic(path, buf.Bytes(), perm)
}

func listDir(dir string) ([]*Case, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*Case
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		c, err := readCase(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source.Date != out[j].Source.Date {
			return out[i].Source.Date > out[j].Source.Date
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// Candidates trả về mọi ứng viên (kể cả đã duyệt/loại, để khai thác lại không tạo trùng).
func (s Store) Candidates() ([]*Case, error) { return listDir(s.CandidatesDir) }

// Cases trả về các tác vụ đã duyệt.
func (s Store) Cases() ([]*Case, error) { return listDir(s.CasesDir) }

// CandidatePath là đường dẫn tệp ứng viên.
func (s Store) CandidatePath(id string) string { return filepath.Join(s.CandidatesDir, id+".yml") }

// CasePath là đường dẫn tệp tác vụ đã duyệt.
func (s Store) CasePath(id string) string { return filepath.Join(s.CasesDir, id+".yml") }

// SaveCandidate ghi ứng viên (quyền 0600: chứa thông điệp commit gốc).
func (s Store) SaveCandidate(c *Case) error {
	return writeCase(s.CandidatePath(c.ID), c, 0o600,
		"# Ứng viên tác vụ đánh giá của Bint. Sửa `prompt` nếu cần rồi chạy `bint eval approve "+c.ID+"`.\n")
}

// SaveCase ghi tác vụ đã duyệt vào repository.
func (s Store) SaveCase(c *Case) error {
	return writeCase(s.CasePath(c.ID), c, 0o644,
		"# Tác vụ đánh giá đã duyệt (bint eval). Sửa prompt hoặc test_command sẽ đổi fingerprint của tác vụ.\n")
}

// Get tìm tác vụ theo ID hoặc tiền tố ID, ưu tiên tác vụ đã duyệt.
func (s Store) Get(idOrPrefix string) (*Case, bool, error) {
	cases, err := s.Cases()
	if err != nil {
		return nil, false, err
	}
	if c, err := pick(cases, idOrPrefix); c != nil || err != nil {
		return c, true, err
	}
	cands, err := s.Candidates()
	if err != nil {
		return nil, false, err
	}
	c, err := pick(cands, idOrPrefix)
	if c == nil && err == nil {
		err = fmt.Errorf("không tìm thấy tác vụ %q", idOrPrefix)
	}
	return c, false, err
}

func pick(cs []*Case, idOrPrefix string) (*Case, error) {
	var found []*Case
	for _, c := range cs {
		if c.ID == idOrPrefix {
			return c, nil
		}
		if strings.HasPrefix(c.ID, idOrPrefix) || strings.HasPrefix(c.Source.Commit, idOrPrefix) {
			found = append(found, c)
		}
	}
	switch len(found) {
	case 0:
		return nil, nil
	case 1:
		return found[0], nil
	}
	return nil, fmt.Errorf("tiền tố %q khớp nhiều tác vụ", idOrPrefix)
}

func nowString(t time.Time) string { return t.UTC().Format(time.RFC3339) }
