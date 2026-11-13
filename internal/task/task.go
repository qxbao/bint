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
	"crypto/sha256"
	"encoding/hex"
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
	Request string `yaml:"request,omitempty"`
	Verify  Verify `yaml:"verify"`
	Scope   Scope  `yaml:"scope"`
	// References là tài liệu đầu vào (kế hoạch, đặc tả) mà mô tả trỏ tới: agent đọc để làm theo,
	// không phải nơi được sửa nên không nằm trong scope.allow.
	References  []string `yaml:"references,omitempty"`
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
	// Depth là độ sâu phân tích của b-req (DepthStandard | DepthDeep); chỉ có nghĩa khi Plan=true và
	// được chọn một lần lúc tạo tác vụ. Rỗng (state cũ, hoặc Plan=false) được hiểu là standard.
	Depth string `json:"depth,omitempty"`
	// DepthReason: protected_paths | complex_request | default | user_override.
	DepthReason string `json:"depth_reason,omitempty"`
	// Isolation (IsolationShared | IsolationWorktree) được chọn một lần lúc lập phiếu; rỗng (phiếu cũ) = shared.
	Isolation string `json:"isolation,omitempty"`
	// IsolationReason: concurrent_task | user | config | default.
	IsolationReason string `json:"isolation_reason,omitempty"`
	// IsolationTasks là ID các tác vụ khiến chế độ auto chọn worktree (IsolationReason = concurrent_task).
	IsolationTasks []string `json:"isolation_tasks,omitempty"`
	// Driver (DriverProcess | DriverSession) được chọn một lần lúc lập phiếu; rỗng (phiếu cũ) = process.
	Driver string `json:"driver,omitempty"`
	// DriverReason: user | config | default (cùng nghĩa với IsolationReason).
	DriverReason string `json:"driver_reason,omitempty"`
}

// Driver của tác vụ (docs/isolation-subagent-plan.md mục 6): ai chạy các bước agent.
const (
	DriverProcess = "process" // `bint run` gọi harness cho từng bước
	DriverSession = "session" // phiên orchestrator chạy bước bằng subagent (bint run next/complete)
)

// Lý do chọn driver (Classification.DriverReason).
const (
	DriverReasonUser    = "user"    // cờ --driver
	DriverReasonConfig  = "config"  // workflow.driver: session
	DriverReasonDefault = "default" // mặc định process
)

// EffectiveDriver trả về driver đang có hiệu lực (process khi chưa lưu: phiếu cũ).
func (c Classification) EffectiveDriver() string {
	if c.Driver == DriverSession {
		return DriverSession
	}
	return DriverProcess
}

// Chế độ cô lập của tác vụ (docs/isolation-subagent-plan.md mục 5.1).
const (
	IsolationShared   = "shared"   // sửa thẳng trên cây đã lập phiếu
	IsolationWorktree = "worktree" // chạy trong worktree riêng, đưa về cây gốc bằng `bint task land`
)

// Lý do chọn chế độ cô lập (Classification.IsolationReason).
const (
	IsolationReasonConcurrent = "concurrent_task" // auto: cây đang có tác vụ khác chưa kết thúc
	IsolationReasonUser       = "user"            // cờ --worktree / --shared
	IsolationReasonConfig     = "config"          // workflow.isolation: shared | worktree
	IsolationReasonDefault    = "default"         // auto, không có tác vụ nào khác
)

// EffectiveIsolation trả về chế độ cô lập đang có hiệu lực (shared khi chưa lưu: phiếu cũ).
func (c Classification) EffectiveIsolation() string {
	if c.Isolation == IsolationWorktree {
		return IsolationWorktree
	}
	return IsolationShared
}

// EffectiveDepth trả về độ sâu đang có hiệu lực (standard khi chưa lưu).
func (c Classification) EffectiveDepth() string {
	if c.Depth == DepthDeep {
		return DepthDeep
	}
	return DepthStandard
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
	// Tree là mã cây ảnh chụp cây làm việc sau lệnh kiểm chứng ("" nếu không chụp được); dùng để
	// biết tệp nào đổi kể từ lần kiểm chứng.
	Tree  string `json:"tree,omitempty"`
	By    string `json:"by"`
	RunID string `json:"run_id,omitempty"`
}

// Approval là một lần người duyệt.
type Approval struct {
	Command string `json:"command,omitempty"`
	// SHA256 là băm (hex) của Command đã trim: lệnh setup worktree so theo băm để lệnh đổi phải xác nhận lại.
	SHA256 string   `json:"sha256,omitempty"`
	Paths  []string `json:"paths,omitempty"`
	By     string   `json:"by"`
	At     string   `json:"at"`
}

// CommandSHA256 là băm sha256 (hex) của lệnh đã trim khoảng trắng đầu/cuối.
func CommandSHA256(cmd string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(cmd)))
	return hex.EncodeToString(sum[:])
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
	Schema   int    `json:"schema"`
	ID       string `json:"id"`
	Status   string `json:"status"`
	Workflow string `json:"workflow"`
	// FlowStep là id bước của flow đang chạy hoặc kế tiếp (Runner ghi khi mở bước và khi chuyển bước; rỗng = chưa theo
	// dõi, Runner suy từ trạng thái như flow nhúng). FlowSHA là băm định nghĩa flow lúc lập phiếu (flow.Hash), chỉ với
	// flow tuỳ biến: khác băm hiện tại nghĩa là flow đã đổi giữa chừng và tác vụ bị chặn.
	FlowStep string `json:"flow_step,omitempty"`
	FlowSHA  string `json:"flow_sha,omitempty"`
	// AgentChoices ghi lựa chọn harness/model hiệu lực theo role để resume không đổi agent khi cấu hình đổi.
	AgentChoices map[string]AgentChoice `json:"agent_choices,omitempty"`
	CreatedAt    string                 `json:"created_at"`
	UpdatedAt    string                 `json:"updated_at"`
	// Base là commit gốc lúc tạo tác vụ (phạm vi diff mặc định của review).
	Base     string `json:"base,omitempty"`
	Branch   string `json:"branch,omitempty"`
	Worktree string `json:"worktree,omitempty"`
	// BaseTree là mã cây (git tree) của ảnh chụp cây làm việc ngay trước lần b-feat đầu tiên, gồm cả
	// thay đổi chưa commit có sẵn lúc đó; giữ sống bằng refs/bint/tasks/<id>/base-tree (xoá khi tác vụ kết thúc). Rỗng = phiếu cũ
	// (hoặc không chụp được): tập tệp của tác vụ tính theo toàn bộ thay đổi so với Base.
	BaseTree string `json:"base_tree,omitempty"`
	// Touched là hợp các tệp đổi giữa ảnh chụp trước/sau của mọi lượt b-feat (đã sắp xếp, bỏ trùng);
	// chỉ có nghĩa khi TracksTouched().
	Touched []string `json:"touched,omitempty"`
	// Origin là cây (worktree) đã lập phiếu khi tác vụ chạy trong worktree riêng: Worktree là worktree của
	// tác vụ, Origin là nơi thay đổi được đưa về (`bint task land`). Rỗng = tác vụ chạy thẳng trên cây của nó
	// (shared) hoặc chưa tạo worktree.
	Origin string `json:"origin,omitempty"`
	// Landed ghi lần đưa về cây gốc (nil = chưa đưa về).
	Landed *Landing `json:"landed,omitempty"`
	// AwaitingLand: tác vụ đã qua cổng done ở worktree và chỉ còn đưa về (xung đột/lỗi land); tiếp tục thì
	// vào thẳng finalize, không chạy lại b-review.
	AwaitingLand bool `json:"awaiting_land,omitempty"`
	// Reason giải thích trạng thái hiện tại (ví dụ vì sao blocked).
	Reason string `json:"reason,omitempty"`
	// ResumeFrom là trạng thái hoạt động trước khi blocked/interrupted/failed.
	ResumeFrom     string          `json:"resume_from,omitempty"`
	Classification *Classification `json:"classification,omitempty"`
	// PlanningContractVersion: 0 = task cũ (không bị chặn tự động), 1 = artifact b-req theo hợp đồng v1.
	PlanningContractVersion int              `json:"planning_contract_version,omitempty"`
	History                 []Transition     `json:"history"`
	Criteria                []CriterionCheck `json:"criteria,omitempty"`
	Verifications           []Verification   `json:"verifications,omitempty"`
	VerifyApproval          *Approval        `json:"verify_approval,omitempty"`
	ProtectedApproval       *Approval        `json:"protected_approval,omitempty"`
	// SetupApproval là lần người xác nhận workflow.worktree.setup_command cho tác vụ worktree này
	// (bint task approve); so theo băm SHA256 của lệnh, lệnh đổi thì phải xác nhận lại.
	SetupApproval *Approval `json:"setup_approval,omitempty"`
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
	// ReviewDecided là số vòng review (ReviewRound.Round) đã được áp quyết định (finalize / vòng sửa / chờ người).
	// Vòng review mới hơn mà chưa áp là review chạy riêng bằng tiến trình (`bint run b-review --task`): driver
	// session áp nó ở `bint run next` thay vì review lặp.
	ReviewDecided int `json:"review_decided,omitempty"`
	// PendingStep là bước đang chạy bằng subagent (driver session): `bint run next` đã mở, `complete`/`abort` chưa
	// đóng. nil = không có bước dở.
	PendingStep *PendingStep `json:"pending_step,omitempty"`
	// SessionRun là lượt cha của driver session (mở ở `next` đầu tiên, đóng khi tác vụ dừng); các lượt con của bước
	// là con của nó, như lượt `bint run` của driver process.
	SessionRun string `json:"session_run,omitempty"`
	// AgentRuns ánh xạ agent_id của subagent (do `complete --agent-id` ghi) sang lượt chạy của bước, để nạp
	// spool gắn sự kiện của subagent vào đúng lượt.
	AgentRuns []AgentRun `json:"agent_runs,omitempty"`
	// SessionSpend cộng dồn chi phí/số lượt của các bước subagent trong lượt cha SessionRun, để ngân sách
	// limits.workflows.b-do (chi phí, lượt) được kiểm qua nhiều lệnh `bint run complete`/`next` (mỗi lệnh là một tiến trình).
	SessionSpend *SessionSpend `json:"session_spend,omitempty"`
}

// SessionSpend là phần ngân sách b-do đã dùng trong một lượt cha của driver session.
type SessionSpend struct {
	RunID string  `json:"run_id"` // lượt cha; lượt cha mới (sau resume) bắt đầu từ 0
	Cost  float64 `json:"cost_usd"`
	// CostUnknown: có bước chạy xong mà không biết chi phí (không có transcript hoặc model không có giá).
	CostUnknown bool  `json:"cost_unknown,omitempty"`
	Turns       int64 `json:"turns"`
}

// PendingStep là nửa giữa của một bước chạy bằng subagent: mọi thứ `complete` cần để làm nửa sau của bước
// (kiểm chỉ đọc, ghi kết quả) mà không phải dựng lại từ tiến trình `next` đã thoát.
type PendingStep struct {
	RunID       string `json:"run_id"` // lượt con của bước
	ParentRunID string `json:"parent_run_id,omitempty"`
	Step        string `json:"step"`
	// Kind là kind của bước trong flow lúc mở (plan | implement | review): nguồn của chế độ chỉ đọc và subagent khi
	// `next` được gọi lại hoặc `complete` đóng bước, không phụ thuộc flow còn nguyên hay không. Rỗng ở bước mở bởi bản cũ.
	Kind string `json:"kind,omitempty"`
	// Mode: chế độ khoá của bước ("shared" | "exclusive").
	Mode string `json:"mode"`
	// Snapshot: cây làm việc trước bước chỉ đọc; FeatBefore: cây trước b-feat (cho Touched); FirstFeat: lần b-feat đầu.
	Snapshot   string `json:"snapshot,omitempty"`
	FeatBefore string `json:"feat_before,omitempty"`
	FirstFeat  bool   `json:"first_feat,omitempty"`
	Harness    string `json:"harness,omitempty"`
	Model      string `json:"model,omitempty"`
	Subagent   string `json:"subagent,omitempty"`
	// Prompt là đề bài đã dựng lúc `next` (gọi lại `next` trả đúng nó, không dựng lại).
	Prompt   string `json:"prompt"`
	MaxTurns *int   `json:"max_turns,omitempty"`
	// StartedAt, LeaseUntil: RFC 3339 UTC. Quá LeaseUntil là bước hết hạn (hợp đồng thuê khoá cũng hết).
	StartedAt  string `json:"started_at"`
	LeaseUntil string `json:"lease_until"`
}

// AgentRun ghép agent_id của một subagent với lượt chạy của bước nó đã làm.
type AgentRun struct {
	AgentID string `json:"agent_id"`
	RunID   string `json:"run_id"`
}

// PendingActive cho biết có bước dở (subagent) còn hạn tại thời điểm now.
func (s *State) PendingActive(now time.Time) bool {
	if s.PendingStep == nil {
		return false
	}
	until, err := time.Parse(time.RFC3339, s.PendingStep.LeaseUntil)
	return err != nil || now.Before(until) // hạn không đọc được: coi như còn hạn (không mở khoá nhầm)
}

// Landing là kết quả đưa thay đổi của tác vụ worktree về cây gốc.
type Landing struct {
	At string `json:"at"`
	By string `json:"by"`
	// Files là tệp đã đưa về (tương đối với gốc repo); Conflicts là tệp còn dấu xung đột/chưa trộn được (--3way).
	Files     []string `json:"files"`
	Conflicts []string `json:"conflicts,omitempty"`
	// Unmerged ⊂ Conflicts: tệp không trộn được (cây gốc giữ nguyên, bản của tác vụ chỉ còn ở worktree).
	Unmerged []string `json:"unmerged,omitempty"`
	ThreeWay bool     `json:"three_way,omitempty"`
	// Fingerprint và Tree là trạng thái worktree lúc đưa về (worktree có thể đã gỡ): cổng done, diff và kiểm tra
	// phạm vi của tác vụ đã đưa về dùng bản lưu này thay vì đọc worktree.
	Fingerprint string `json:"fingerprint,omitempty"`
	Tree        string `json:"tree,omitempty"`
	// Kept: worktree được giữ lại (--keep).
	Kept bool `json:"kept,omitempty"`
	// Note ghi chú dọn dẹp lỗi (worktree không gỡ được...).
	Note string `json:"note,omitempty"`
}

// Isolation trả về chế độ cô lập của tác vụ (shared khi chưa ghi).
func (s *State) Isolation() string {
	if s.Classification == nil {
		return IsolationShared
	}
	return s.Classification.EffectiveIsolation()
}

// InWorktree cho biết tác vụ đã có worktree riêng (đã tạo, chưa đưa về).
func (s *State) InWorktree() bool { return s.Origin != "" && s.Landed == nil }

// NeedsLand cho biết tác vụ chạy trong worktree riêng mà chưa đưa về cây gốc: chưa được done.
func (s *State) NeedsLand() bool { return s.Origin != "" && s.Landed == nil }

// TracksTouched cho biết tác vụ theo dõi tập tệp riêng của mình (BaseTree/Touched). Khi false
// (phiếu cũ), phạm vi và diff của tác vụ tính theo toàn bộ thay đổi so với Base.
func (s *State) TracksTouched() bool { return s.BaseTree != "" }

// EverInProgress cho biết tác vụ đã từng vào in_progress (đã có hoặc đang có b-feat, kể cả làm tay).
func (s *State) EverInProgress() bool {
	for _, h := range s.History {
		if h.To == StatusInProgress {
			return true
		}
	}
	return false
}

// AgentChoice là harness/model đã chọn cho một role của workflow.
type AgentChoice struct {
	Harness string `json:"harness"`
	Model   string `json:"model,omitempty"`
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
