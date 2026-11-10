// Package task quản lý tác vụ của các workflow gốc (mục 6): phiếu tác vụ (mục
// 6.7), trạng thái và bảng chuyển trạng thái, tài liệu đầu ra (requirement, plan,
// tasks), kết quả kiểm chứng, đối chiếu tiêu chí và phát hiện review.
//
// Mỗi tác vụ là một thư mục trong thư mục trạng thái cục bộ (ngoài repository):
//
//	<state>/runtime/tasks/<task-id>/
//	├── task.yml        phiếu tác vụ (người hoặc agent sửa được)
//	├── state.json      trạng thái do Bint quản lý (không sửa tay)
//	├── requirement.md  plan.md  tasks.md
//	├── review.json     review.md
//	└── logs/
//
// Ghi state.json luôn đi qua khoá thư mục để tiến trình điều phối (`bint run`) và
// lệnh `bint task` do agent gọi không ghi đè lẫn nhau.
package task

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"bint/internal/fsafe"
	"bint/internal/ulid"
)

// Tên tệp trong thư mục tác vụ.
const (
	TicketFile = "task.yml"
	StateFile  = "state.json"
	ReviewFile = "review.json"
	ReviewMD   = "review.md"
	LogsDir    = "logs"
)

// SchemaVersion của state.json.
const SchemaVersion = 1

// Artifacts là các tài liệu đầu ra của b-req và tên tệp tương ứng.
var Artifacts = map[string]string{"requirement": "requirement.md", "plan": "plan.md", "tasks": "tasks.md"}

// ArtifactNames trả về tên tài liệu theo thứ tự cố định.
func ArtifactNames() []string { return []string{"requirement", "plan", "tasks"} }

// Mức tự chủ (mục 6.7).
const (
	AutonomyAskAlways      = "ask_always"
	AutonomyAskOnProtected = "ask_on_protected"
	AutonomyAuto           = "auto"
)

// Autonomies là các giá trị hợp lệ của autonomy.
var Autonomies = []string{AutonomyAskAlways, AutonomyAskOnProtected, AutonomyAuto}

// Ticket là phiếu tác vụ (task.yml).
type Ticket struct {
	ID string `yaml:"id"`
	// Goal là mục tiêu một dòng.
	Goal string `yaml:"goal"`
	// Request là mô tả tự do gốc của người dùng.
	Request     string   `yaml:"request,omitempty"`
	Verify      Verify   `yaml:"verify"`
	Scope       Scope    `yaml:"scope"`
	Constraints []string `yaml:"constraints"`
	Autonomy    string   `yaml:"autonomy"`
}

// Verify là cách kiểm chứng tác vụ.
type Verify struct {
	Command  string   `yaml:"command"`
	Criteria []string `yaml:"criteria"`
}

// Scope là phạm vi được sửa.
type Scope struct {
	Allow []string `yaml:"allow"`
	Deny  []string `yaml:"deny"`
}

// Validate kiểm tra phiếu.
func (t Ticket) Validate() error {
	var errs []string
	if strings.TrimSpace(t.Goal) == "" {
		errs = append(errs, "goal: không được rỗng")
	}
	if !slices.Contains(Autonomies, t.Autonomy) {
		errs = append(errs, fmt.Sprintf("autonomy: phải là %s", strings.Join(Autonomies, " | ")))
	}
	for _, p := range append(append([]string{}, t.Scope.Allow...), t.Scope.Deny...) {
		if p == "" || filepath.IsAbs(p) || strings.HasPrefix(p, "../") || p == ".." {
			errs = append(errs, fmt.Sprintf("scope: mẫu %q phải là đường dẫn tương đối trong repository", p))
		}
	}
	for i, c := range t.Verify.Criteria {
		if strings.TrimSpace(c) == "" {
			errs = append(errs, fmt.Sprintf("verify.criteria[%d]: rỗng", i+1))
		}
	}
	if len(errs) > 0 {
		return errors.New("phiếu tác vụ không hợp lệ: " + strings.Join(errs, "; "))
	}
	return nil
}

func encodeTicket(t Ticket) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("# Phiếu tác vụ Bint (mục 6.7). Sửa được bằng tay hoặc `bint task set`.\n")
	buf.WriteString("# verify.command chỉ được Bint chạy sau khi người xác nhận (`bint task approve`),\n")
	buf.WriteString("# trừ khi trùng eval.test_command đã xác nhận trong cấu hình.\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if t.Constraints == nil {
		t.Constraints = []string{}
	}
	if t.Verify.Criteria == nil {
		t.Verify.Criteria = []string{}
	}
	if t.Scope.Allow == nil {
		t.Scope.Allow = []string{}
	}
	if t.Scope.Deny == nil {
		t.Scope.Deny = []string{}
	}
	if err := enc.Encode(t); err != nil {
		return nil, err
	}
	enc.Close()
	return buf.Bytes(), nil
}

func decodeTicket(data []byte) (Ticket, error) {
	var t Ticket
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&t); err != nil {
		return t, err
	}
	return t, nil
}

// Transition là một lần chuyển trạng thái.
type Transition struct {
	From   string `json:"from"`
	To     string `json:"to"`
	At     string `json:"at"`
	By     string `json:"by"`
	Reason string `json:"reason,omitempty"`
}

// Classification là quyết định của bước Classify.
type Classification struct {
	Plan   bool   `json:"plan"`
	Reason string `json:"reason"`
}

// CriterionCheck là kết quả đối chiếu một tiêu chí chấp nhận (agent tự đánh giá).
type CriterionCheck struct {
	Index    int    `json:"index"` // từ 1
	Met      bool   `json:"met"`
	Evidence string `json:"evidence"`
	By       string `json:"by"`
	At       string `json:"at"`
}

// Verification là một lần chạy lệnh kiểm chứng.
type Verification struct {
	At        string `json:"at"`
	Command   string `json:"command"`
	Confirmed bool   `json:"confirmed"`
	ExitCode  int    `json:"exit_code"`
	Passed    bool   `json:"passed"`
	TimedOut  bool   `json:"timed_out,omitempty"`
	Log       string `json:"log,omitempty"`
	// Worktree là fingerprint trạng thái mã lúc kiểm chứng (HEAD + thay đổi chưa commit).
	Worktree string `json:"worktree,omitempty"`
	By       string `json:"by"`
	RunID    string `json:"run_id,omitempty"`
}

// Approval là một lần người duyệt.
type Approval struct {
	Command string   `json:"command,omitempty"`
	Paths   []string `json:"paths,omitempty"`
	By      string   `json:"by"`
	At      string   `json:"at"`
}

// ErrorRecord là một lỗi hoặc lần thử thất bại được ghi nhận.
type ErrorRecord struct {
	Step    string `json:"step,omitempty"`
	Message string `json:"message"`
	At      string `json:"at"`
	By      string `json:"by"`
	RunID   string `json:"run_id,omitempty"`
}

// Runner là tiến trình `bint run` đang điều phối tác vụ.
type Runner struct {
	PID     int    `json:"pid"`
	Host    string `json:"host"`
	RunID   string `json:"run_id"`
	Started string `json:"started"`
}

// State là trạng thái tác vụ (state.json), chỉ Bint ghi.
type State struct {
	Schema    int    `json:"schema"`
	ID        string `json:"id"`
	Status    string `json:"status"`
	Workflow  string `json:"workflow"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	// Base là commit gốc lúc tạo tác vụ (phạm vi diff mặc định của review).
	Base     string `json:"base,omitempty"`
	Branch   string `json:"branch,omitempty"`
	Worktree string `json:"worktree,omitempty"`
	// Reason giải thích trạng thái hiện tại (ví dụ vì sao blocked).
	Reason string `json:"reason,omitempty"`
	// ResumeFrom là trạng thái hoạt động trước khi blocked/interrupted/failed.
	ResumeFrom        string           `json:"resume_from,omitempty"`
	Classification    *Classification  `json:"classification,omitempty"`
	History           []Transition     `json:"history"`
	Criteria          []CriterionCheck `json:"criteria,omitempty"`
	Verifications     []Verification   `json:"verifications,omitempty"`
	VerifyApproval    *Approval        `json:"verify_approval,omitempty"`
	ProtectedApproval *Approval        `json:"protected_approval,omitempty"`
	// PendingPaths là tệp đã sửa chạm đường dẫn bảo vệ/deny đang chờ người duyệt.
	PendingPaths []string      `json:"pending_paths,omitempty"`
	Errors       []ErrorRecord `json:"errors,omitempty"`
	Runs         []string      `json:"runs,omitempty"`
	Runner       *Runner       `json:"runner,omitempty"`
	ReviewRounds int           `json:"review_rounds"`
	// AwaitingCheck: b-feat đã xong nhưng chưa qua kiểm tra phạm vi/kiểm chứng (ví dụ
	// đang chờ người duyệt); tiếp tục thì không chạy lại b-feat.
	AwaitingCheck bool              `json:"awaiting_check,omitempty"`
	NextFinding   int               `json:"next_finding"`
	Artifacts     map[string]string `json:"artifacts,omitempty"` // tên → thời điểm ghi
}

// LastVerification trả về lần kiểm chứng gần nhất (nil nếu chưa có).
func (s *State) LastVerification() *Verification {
	if len(s.Verifications) == 0 {
		return nil
	}
	return &s.Verifications[len(s.Verifications)-1]
}

// LinkRun ghi nhận một lượt chạy gắn với tác vụ.
func (s *State) LinkRun(runID string) {
	if runID != "" && !slices.Contains(s.Runs, runID) {
		s.Runs = append(s.Runs, runID)
	}
}

// LastRun trả về lượt chạy gắn gần nhất.
func (s *State) LastRun() string {
	if len(s.Runs) == 0 {
		return ""
	}
	return s.Runs[len(s.Runs)-1]
}

// CriterionStatus trả về lần đối chiếu mới nhất của tiêu chí i (từ 1).
func (s *State) CriterionStatus(i int) *CriterionCheck {
	for j := len(s.Criteria) - 1; j >= 0; j-- {
		if s.Criteria[j].Index == i {
			return &s.Criteria[j]
		}
	}
	return nil
}

// Task là một tác vụ đã nạp.
type Task struct {
	Dir    string
	Ticket Ticket
	State  State
}

// ArtifactPath trả về đường dẫn tài liệu của tác vụ.
func (t *Task) ArtifactPath(name string) string { return filepath.Join(t.Dir, Artifacts[name]) }

// LogPath trả về đường dẫn tệp log trong thư mục tác vụ.
func (t *Task) LogPath(name string) string { return filepath.Join(t.Dir, LogsDir, name) }

// Store là thư mục chứa các tác vụ.
type Store struct{ Dir string }

// NewStore trả về kho tác vụ trong thư mục runtime.
func NewStore(runtimeDir string) Store { return Store{Dir: filepath.Join(runtimeDir, "tasks")} }

// Now định dạng thời điểm lưu trong state.json.
func Now(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// Create tạo tác vụ mới. ticket.ID được cấp nếu trống.
func (s Store) Create(t Ticket, st State, now time.Time) (*Task, error) {
	if t.ID == "" {
		t.ID = ulid.At(now)
	}
	if t.Autonomy == "" {
		t.Autonomy = AutonomyAskOnProtected
	}
	if err := t.Validate(); err != nil {
		return nil, err
	}
	dir := filepath.Join(s.Dir, t.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	st.Schema, st.ID = SchemaVersion, t.ID
	if st.Status == "" {
		st.Status = StatusCreated
	}
	st.CreatedAt, st.UpdatedAt = Now(now), Now(now)
	st.History = append(st.History, Transition{From: "", To: st.Status, At: Now(now), By: "bint", Reason: "tạo tác vụ"})
	task := &Task{Dir: dir, Ticket: t, State: st}
	data, err := encodeTicket(t)
	if err != nil {
		return nil, err
	}
	if err := fsafe.WriteAtomic(filepath.Join(dir, TicketFile), data, 0o600); err != nil {
		return nil, err
	}
	return task, writeState(task)
}

func writeState(t *Task) error {
	data, err := json.MarshalIndent(t.State, "", "  ")
	if err != nil {
		return err
	}
	return fsafe.WriteAtomic(filepath.Join(t.Dir, StateFile), append(data, '\n'), 0o600)
}

func loadDir(dir string) (*Task, error) {
	t := &Task{Dir: dir}
	data, err := os.ReadFile(filepath.Join(dir, StateFile))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &t.State); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(dir, StateFile), err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, TicketFile))
	if err != nil {
		return nil, err
	}
	if t.Ticket, err = decodeTicket(raw); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(dir, TicketFile), err)
	}
	if t.Ticket.ID != t.State.ID {
		return nil, fmt.Errorf("%s: id %q không khớp thư mục %q", TicketFile, t.Ticket.ID, t.State.ID)
	}
	return t, nil
}

// ErrNotFound: không có tác vụ khớp.
var ErrNotFound = errors.New("không tìm thấy tác vụ")

// Resolve tìm ID tác vụ theo ID đầy đủ hoặc tiền tố (không phân biệt hoa thường).
func (s Store) Resolve(ref string) (string, error) {
	ref = strings.ToUpper(strings.TrimSpace(ref))
	if ref == "" {
		return "", fmt.Errorf("%w: thiếu task ID", ErrNotFound)
	}
	entries, err := os.ReadDir(s.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("%w: %s", ErrNotFound, ref)
	}
	if err != nil {
		return "", err
	}
	var hits []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), ref) {
			if e.Name() == ref {
				return ref, nil
			}
			hits = append(hits, e.Name())
		}
	}
	switch len(hits) {
	case 0:
		return "", fmt.Errorf("%w: %s", ErrNotFound, ref)
	case 1:
		return hits[0], nil
	}
	return "", fmt.Errorf("tiền tố %s khớp %d tác vụ", ref, len(hits))
}

// Load nạp tác vụ theo ID hoặc tiền tố.
func (s Store) Load(ref string) (*Task, error) {
	id, err := s.Resolve(ref)
	if err != nil {
		return nil, err
	}
	return loadDir(filepath.Join(s.Dir, id))
}

// List nạp mọi tác vụ, mới nhất trước. Thư mục hỏng bị bỏ qua và trả về riêng.
func (s Store) List() ([]*Task, []error) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, []error{err}
	}
	var out []*Task
	var bad []error
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		t, err := loadDir(filepath.Join(s.Dir, e.Name()))
		if err != nil {
			bad = append(bad, err)
			continue
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].State.ID > out[j].State.ID })
	return out, bad
}

// Update nạp lại tác vụ dưới khoá, gọi fn rồi ghi state.json (và task.yml nếu fn
// đổi phiếu). fn trả lỗi thì không ghi gì.
func (s Store) Update(ref string, now time.Time, fn func(*Task) error) (*Task, error) {
	id, err := s.Resolve(ref)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(s.Dir, id)
	unlock, err := lockDir(dir)
	if err != nil {
		return nil, err
	}
	defer unlock()
	t, err := loadDir(dir)
	if err != nil {
		return nil, err
	}
	before, _ := encodeTicket(t.Ticket)
	if err := fn(t); err != nil {
		return t, err
	}
	if err := t.Ticket.Validate(); err != nil {
		return t, err
	}
	after, err := encodeTicket(t.Ticket)
	if err != nil {
		return t, err
	}
	if !bytes.Equal(before, after) {
		if err := fsafe.WriteAtomic(filepath.Join(dir, TicketFile), after, 0o600); err != nil {
			return t, err
		}
	}
	t.State.UpdatedAt = Now(now)
	return t, writeState(t)
}

// lockDir lấy khoá thư mục bằng mkdir (nguyên tử trên mọi hệ điều hành). Khoá cũ
// hơn 2 phút được coi là của tiến trình đã chết.
func lockDir(dir string) (func(), error) {
	p := filepath.Join(dir, ".lock")
	deadline := time.Now().Add(15 * time.Second)
	for {
		err := os.Mkdir(p, 0o700)
		if err == nil {
			return func() { os.Remove(p) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if fi, serr := os.Stat(p); serr == nil && time.Since(fi.ModTime()) > 2*time.Minute {
			os.Remove(p)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("tác vụ đang bị khoá bởi tiến trình khác (%s)", p)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// WriteArtifact ghi một tài liệu của tác vụ (requirement, plan, tasks).
func (t *Task) WriteArtifact(name, content string) (string, error) {
	file, ok := Artifacts[name]
	if !ok {
		return "", fmt.Errorf("tài liệu %q không hợp lệ (requirement | plan | tasks)", name)
	}
	if strings.TrimSpace(content) == "" {
		return "", fmt.Errorf("nội dung %s rỗng", name)
	}
	p := filepath.Join(t.Dir, file)
	return p, fsafe.WriteAtomic(p, []byte(strings.TrimRight(content, "\n")+"\n"), 0o600)
}
