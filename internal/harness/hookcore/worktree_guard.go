package hookcore

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"bint/internal/fsafe"
	"bint/internal/state"
)

// Guardrail đường dẫn worktree cho subagent bint-implementer của driver session (docs/isolation-subagent-plan.md mục 7, 12.14).
//
// Công cụ Agent của Claude Code không nhận thư mục làm việc: subagent bắt đầu ở thư mục của phiên cha (cây gốc) dù tác vụ
// chạy trong worktree riêng. Hook chặn Edit/Write/MultiEdit/NotebookEdit có đường dẫn ngoài worktree và lệnh Bash không bắt
// đầu bằng `cd <worktree> &&` (cwd không nằm trong worktree).
//
// Ánh xạ agent_id ⇒ worktree không cần lệnh nào của orchestrator hay subagent: `bint run next` ghi tệp
// runtime/session-steps/<run_id>.json khi mở bước subagent; hook đọc DÒNG ĐẦU transcript của subagent (Dialect.SubagentPrompt,
// chứa nguyên prompt, trong đó có khối <bint-task id=… step=… run=…>) để biết run_id rồi tra tệp. Không ánh xạ được thì
// fail-closed có giới hạn (xem worktreeDecision): chỉ khi đang có bước worktree còn hạn.
//
// Hook chỉ đọc tệp (một dòng transcript có trần kích thước, tệp runtime nhỏ): không lỗi, không SQLite, không mô hình.

// SubagentImplementer là tên subagent thực thi do Bint cài. Phải khớp workflow.SubagentImplementer (kiểm thử so hai bên).
const SubagentImplementer = "bint-implementer"

// GuardrailWorktree là id của guardrail dựng sẵn trong sự kiện guardrail.triggered.
const GuardrailWorktree = "bint-worktree"

// Giá trị outcome của guardrail.triggered do guardrail này ghi.
const (
	OutcomeFired    = "fired"    // ánh xạ được sang bước, vi phạm bị chặn
	OutcomeUnmapped = "unmapped" // không ánh xạ được, bị chặn theo chính sách fail-closed có giới hạn
)

// SessionStepRecord là tệp `bint run next` ghi khi mở một bước subagent: runtime/session-steps/<run_id>.json.
// Hook dùng nó để biết worktree của bước (Isolated=false: tác vụ chạy ở cây gốc dùng chung, không có gì để chặn).
type SessionStepRecord struct {
	RunID    string `json:"run_id"`
	TaskID   string `json:"task_id"`
	Step     string `json:"step"`
	Subagent string `json:"subagent,omitempty"`
	// Worktree: thư mục làm việc của bước (tuyệt đối).
	Worktree string `json:"worktree"`
	// Isolated: tác vụ chạy trong worktree riêng (State.Origin != "").
	Isolated bool `json:"isolated"`
	// Until: hạn hợp đồng thuê của bước (RFC3339); quá hạn thì không còn tính là "đang chạy" khi không ánh xạ được.
	Until string `json:"until,omitempty"`
}

const (
	sessionStepsDir  = "session-steps"
	maxSessionSteps  = 64       // số tệp tối đa đọc khi liệt kê
	maxSessionStepSz = 64 << 10 // trần kích thước một tệp
)

// SessionStepPath là đường dẫn tệp của runID dưới thư mục runtime ("" nếu runID không hợp lệ: dùng làm tên tệp).
func SessionStepPath(runtimeDir, runID string) string {
	if !ValidAgentID(runID) {
		return ""
	}
	return filepath.Join(runtimeDir, sessionStepsDir, runID+".json")
}

// WriteSessionStep ghi nguyên tử. Lỗi được trả cho người gọi (workflow chỉ ghi chú, không làm hỏng bước).
func WriteSessionStep(runtimeDir string, rec SessionStepRecord) error {
	p := SessionStepPath(runtimeDir, rec.RunID)
	if p == "" {
		return os.ErrInvalid
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return fsafe.WriteAtomic(p, b, 0o600)
}

func readSessionStepFile(p string) (SessionStepRecord, bool) {
	st, err := os.Stat(p)
	if err != nil || !st.Mode().IsRegular() || st.Size() > maxSessionStepSz {
		return SessionStepRecord{}, false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return SessionStepRecord{}, false
	}
	var rec SessionStepRecord
	if json.Unmarshal(b, &rec) != nil || rec.RunID == "" || rec.Worktree == "" {
		return SessionStepRecord{}, false
	}
	return rec, true
}

// ReadSessionStep đọc tệp của runID; ok=false nếu không có hoặc hỏng.
func ReadSessionStep(runtimeDir, runID string) (SessionStepRecord, bool) {
	p := SessionStepPath(runtimeDir, runID)
	if p == "" {
		return SessionStepRecord{}, false
	}
	rec, ok := readSessionStepFile(p)
	if !ok || rec.RunID != runID {
		return SessionStepRecord{}, false
	}
	return rec, true
}

// RemoveSessionStep gỡ tệp của runID (không có tệp cũng không phải lỗi).
func RemoveSessionStep(runtimeDir, runID string) error {
	p := SessionStepPath(runtimeDir, runID)
	if p == "" {
		return os.ErrInvalid
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// LiveIsolatedSteps liệt kê bước isolated còn hạn tại now (chỉ dùng khi không ánh xạ được). Tệp hỏng hoặc Until không
// đọc được bị bỏ qua.
func LiveIsolatedSteps(runtimeDir string, now time.Time) []SessionStepRecord {
	ents, err := os.ReadDir(filepath.Join(runtimeDir, sessionStepsDir))
	if err != nil {
		return nil
	}
	var out []SessionStepRecord
	for i, e := range ents {
		if i >= maxSessionSteps {
			break
		}
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		rec, ok := readSessionStepFile(filepath.Join(runtimeDir, sessionStepsDir, e.Name()))
		if !ok || !rec.Isolated {
			continue
		}
		until, err := time.Parse(time.RFC3339Nano, rec.Until)
		if err != nil || !until.After(now) {
			continue
		}
		out = append(out, rec)
	}
	return out
}

// PruneSessionSteps xoá tệp quá hạn hơn grace (phiên chết giữa bước). Lỗi bị bỏ qua.
func PruneSessionSteps(runtimeDir string, now time.Time, grace time.Duration) {
	dir := filepath.Join(runtimeDir, sessionStepsDir)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for i, e := range ents {
		if i >= maxSessionSteps*4 {
			return
		}
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		rec, ok := readSessionStepFile(p)
		if !ok {
			if st, err := os.Stat(p); err == nil && now.Sub(st.ModTime()) > grace {
				os.Remove(p)
			}
			continue
		}
		if until, err := time.Parse(time.RFC3339Nano, rec.Until); err == nil && now.Sub(until) > grace {
			os.Remove(p)
		}
	}
}

// bintTaskHeaderRe khớp khối `<bint-task id="…" step="…" run="…">` của prompt (định dạng ở workflow/run.go). Lấy khớp ĐẦU
// TIÊN: thân skill đứng trước khối, phiếu và nội dung do người viết đứng sau, nên một khối giả trong đó không đè lên được.
var bintTaskHeaderRe = regexp.MustCompile(`<bint-task id="([^"\s]*)" step="([^"\s]*)" run="([^"\s]*)">`)

// PromptTaskHeader trích (task_id, step, run_id) từ khối bint-task đầu tiên của prompt.
func PromptTaskHeader(prompt string) (taskID, step, runID string, ok bool) {
	m := bintTaskHeaderRe.FindStringSubmatch(prompt)
	if m == nil || m[1] == "" || m[3] == "" {
		return "", "", "", false
	}
	return m[1], m[2], m[3], true
}

// WorktreeCheck là đầu vào của worktreeDecision.
type worktreeCheck struct {
	AgentType string
	Tool      string
	Info      ToolInfo
	Cwd       string
	// Step: bước ánh xạ được (nil = không ánh xạ được). Live: các bước isolated còn hạn (dùng khi Step == nil).
	Step *SessionStepRecord
	Live []SessionStepRecord
}

// worktreeVerdict là kết quả worktreeDecision.
type worktreeVerdict struct {
	Deny    bool
	Outcome string
	Reason  string
}

// cdTargetRe: tiền tố `cd MỘT_TOKEN &&` (cùng dạng cdPrefixRe, có nhóm bắt đích).
var cdTargetRe = regexp.MustCompile(`^\s*cd\s+("[^"$` + "`" + `\\]*"|'[^']*'|[^\s;&|<>$` + "`" + `()\\"']+)\s*&&\s*`)

// cdTarget trả thư mục của tiền tố `cd X &&` đầu lệnh ("" nếu không có). X tương đối thì tính theo cwd.
func cdTarget(cmd, cwd string) (string, bool) {
	m := cdTargetRe.FindStringSubmatch(cmd)
	if m == nil {
		return "", false
	}
	t := m[1]
	if len(t) >= 2 && (t[0] == '"' || t[0] == '\'') {
		t = t[1 : len(t)-1]
	}
	if t == "" {
		return "", false
	}
	return absAgainst(t, cwd), true
}

func absAgainst(p, cwd string) string {
	if !filepath.IsAbs(p) && cwd != "" {
		p = filepath.Join(cwd, p)
	}
	return p
}

// normDir chuẩn hoá một thư mục (giải symlink của tổ tiên và của chính nó, vì "x" không tồn tại nên nó dừng ở thư mục).
func normDir(dir string) string {
	return path.Dir(state.NormalizeWritePath(filepath.Join(dir, "x")))
}

// within: p (đã chuẩn hoá, dấu '/') nằm trong thư mục dir (đã chuẩn hoá) — so theo thành phần, `/wt2` không nằm trong `/wt`.
func within(dir, p string) bool {
	return p == dir || strings.HasPrefix(p, strings.TrimSuffix(dir, "/")+"/")
}

func pathInAny(p string, wts []string) bool {
	for _, w := range wts {
		if within(w, p) {
			return true
		}
	}
	return false
}

// worktreeDecision là hàm thuần của guardrail:
//
//   - agent_type khác bint-implementer (kể cả rỗng) hoặc công cụ không phải sửa tệp/shell ⇒ không can thiệp.
//   - Ánh xạ được: tác vụ shared ⇒ không can thiệp; isolated ⇒ Edit chỉ được trong worktree của bước; Bash chỉ được khi bắt
//     đầu bằng `cd <thư mục trong worktree> &&`, hoặc không có tiền tố cd mà cwd đã trong worktree.
//   - Không ánh xạ được: chỉ khi có ít nhất một bước isolated còn hạn mới áp (fail-closed có giới hạn): Edit chỉ được vào
//     MỘT worktree đang chạy, Bash như trên với worktree đang chạy bất kỳ; còn lại deny với outcome "unmapped". Không có bước
//     isolated nào ⇒ không can thiệp (người không dùng worktree session không bị ảnh hưởng).
func worktreeDecision(c worktreeCheck) worktreeVerdict {
	if c.AgentType != SubagentImplementer || (c.Info.Kind != ToolEdit && c.Info.Kind != ToolShell) {
		return worktreeVerdict{}
	}
	var wts []string
	outcome := OutcomeFired
	where := ""
	if c.Step != nil {
		if !c.Step.Isolated {
			return worktreeVerdict{}
		}
		wts = []string{normDir(c.Step.Worktree)}
		where = fmt.Sprintf("worktree của tác vụ %s là %s", c.Step.TaskID, c.Step.Worktree)
	} else {
		if len(c.Live) == 0 {
			return worktreeVerdict{}
		}
		outcome = OutcomeUnmapped
		var raw []string
		for _, s := range c.Live {
			wts = append(wts, normDir(s.Worktree))
			raw = append(raw, s.Worktree)
		}
		where = "không xác định được tác vụ của subagent này (không đọc được prompt đầu của nó hoặc không có bước tương ứng); " +
			"đang có bước chạy trên worktree: " + strings.Join(raw, ", ")
	}
	deny := func(what string) worktreeVerdict {
		return worktreeVerdict{Deny: true, Outcome: outcome, Reason: fmt.Sprintf(
			"Bint: %s bị chặn — %s. Bạn đang đứng ở cây gốc, không phải worktree. Chỉ làm việc trong worktree: dùng đường dẫn tuyệt đối "+
				"dưới đó cho Read/Edit/Write, và bắt đầu MỌI lệnh Bash bằng `cd <worktree> && …`.", c.Tool, what+" ("+where+")")}
	}
	switch c.Info.Kind {
	case ToolEdit:
		for _, p := range c.Info.Paths {
			abs := state.NormalizeWritePath(absAgainst(p, c.Cwd))
			if !pathInAny(abs, wts) {
				return deny("đường dẫn " + p + " nằm ngoài worktree")
			}
		}
	case ToolShell:
		if t, ok := cdTarget(c.Info.Command, c.Cwd); ok {
			if !pathInAny(normDir(t), wts) {
				return deny("lệnh `cd` ra ngoài worktree")
			}
			return worktreeVerdict{}
		}
		if c.Cwd == "" || !pathInAny(normDir(c.Cwd), wts) {
			return deny("lệnh không bắt đầu bằng `cd <worktree> &&` và thư mục hiện tại nằm ngoài worktree")
		}
	}
	return worktreeVerdict{}
}
