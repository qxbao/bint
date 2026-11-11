package evolve

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"bint/internal/eval"
	"bint/internal/knowledge"
	"bint/internal/runlog"
	"bint/internal/task"
)

// Failure là một đơn vị thất bại để rút bài học (kế hoạch E1): một lượt đánh giá trượt
// trên tập VALIDATION, hoặc một lượt thật có nhãn failure. Tập test không bao giờ xuất
// hiện ở đây: LoadFailures lọc theo split của lượt lẫn split hiện tại của tác vụ.
type Failure struct {
	// Source: "eval" (lượt đánh giá) | "run" (lượt thật).
	Source string `json:"source"`
	// ID: attempt_id (ea_…) hoặc run_id — là bằng chứng (evidence) của bài học.
	ID string `json:"id"`
	// Unit: đơn vị phân biệt để đếm ngưỡng: tác vụ đánh giá, hoặc tác vụ/lượt thật. Nhiều
	// lượt của cùng một Unit chỉ tính là một đơn vị (mục 9.6 của tài liệu P8 — E1).
	Unit string `json:"unit"`
	Case string `json:"case,omitempty"` // tác vụ đánh giá (eval)
	// Split: tập của tác vụ tại thời điểm nạp; chỉ "validation" mới được vào bài học.
	Split  string `json:"split,omitempty"`
	Batch  string `json:"batch,omitempty"`
	Arm    string `json:"arm,omitempty"`
	Status string `json:"status"`
	// Area: vùng mã của lượt (thư mục tệp kiểm thử trượt, hoặc khu vực chính của tệp đã sửa).
	// Nhóm theo Area sinh bài học riêng.
	Area string `json:"area"`
	// Allowed: mẫu phạm vi của lượt; tệp sửa ngoài các mẫu này là dấu hiệu "sửa ngoài vùng".
	Allowed []string `json:"allowed,omitempty"`
	// allowDeny: mẫu cấm của phiếu (scope.deny); tệp khớp mẫu này cũng là "sửa ngoài vùng".
	allowDeny []string
	// Signals: dấu hiệu hành vi (xem Sig*). Nhóm theo dấu hiệu sinh bài học chung.
	Signals []string `json:"signals"`
	// Measured: lượt có vết công cụ (log stream-json). Log cũ chỉ ghi "không đo được".
	Measured  bool     `json:"measured"`
	Modified  []string `json:"modified,omitempty"`
	Read      []string `json:"read,omitempty"`
	Retrieved bool     `json:"retrieved"` // tri thức đã được chèn trong lượt
	Cited     bool     `json:"cited"`     // agent đã trích dẫn tri thức đó
	// Các trường nội bộ, dùng để dựng bản tóm tắt (không ghi ra JSON gọn).
	Prompt    string   `json:"-"`
	Trace     Trace    `json:"-"`
	TestFails []string `json:"test_fails,omitempty"`
	TestTail  []string `json:"-"`
	LogDir    string   `json:"-"`
	StartedAt string   `json:"started_at,omitempty"`
	Error     string   `json:"error,omitempty"`
}

// Dấu hiệu hành vi của một lượt thất bại (E1).
const (
	SigNoTest      = "no_test"      // không chạy lệnh test trước khi kết thúc
	SigEditOutside = "edit_outside" // sửa tệp ngoài vùng của tác vụ/phiếu
	SigRepeatCmd   = "repeat_cmd"   // lặp cùng một lệnh (≥ 3 lần)
	SigTimeout     = "timeout"      // hết thời gian (hoặc hết lượt)
	SigIgnoredCtx  = "ignored_ctx"  // tri thức đã chèn nhưng agent không trích dẫn
)

// signalLabels: mô tả tiếng Việt của từng dấu hiệu, dùng trong bản tóm tắt và nhóm.
var signalLabels = map[string]string{
	SigNoTest:      "không chạy test trước khi kết thúc",
	SigEditOutside: "sửa tệp ngoài vùng",
	SigRepeatCmd:   "lặp cùng một lệnh",
	SigTimeout:     "hết thời gian hoặc hết lượt",
	SigIgnoredCtx:  "bỏ qua tri thức đã chèn",
}

// repeatCmdMin: số lần lặp tối thiểu của một lệnh để tính là dấu hiệu lặp.
const repeatCmdMin = 3

// Trace là vết công cụ của một lượt, đọc từ log stream-json của Claude Code.
type Trace struct {
	// Available: log có vết công cụ (dòng assistant/system). false = không đo được.
	Available bool
	// Commands: lệnh Bash theo thứ tự xuất hiện.
	Commands []string
	// Reads: tệp đọc (công cụ Read).
	Reads []string
	// Writes: tệp sửa/ghi (Edit, Write, MultiEdit, NotebookEdit).
	Writes []string
}

// testCmdRe khớp lệnh chạy kiểm thử (dấu hiệu "có chạy test").
var testCmdRe = regexp.MustCompile(`(?i)\b(test|tests|pytest|jest|vitest|rspec)\b`)

// ParseTrace đọc log stream-json. prefix là tiền tố tuyệt đối của thư mục làm việc (bỏ
// khỏi đường dẫn để còn đường dẫn tương đối); rỗng = giữ nguyên.
func ParseTrace(data []byte, prefix string) Trace {
	var t Trace
	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var ev struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Type  string         `json:"type"`
					Name  string         `json:"name"`
					Input map[string]any `json:"input"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &ev) != nil {
			continue
		}
		if ev.Type == "assistant" || ev.Type == "system" {
			t.Available = true
		}
		if ev.Type != "assistant" {
			continue
		}
		for _, c := range ev.Message.Content {
			if c.Type != "tool_use" {
				continue
			}
			switch c.Name {
			case "Bash":
				if cmd, ok := c.Input["command"].(string); ok && strings.TrimSpace(cmd) != "" {
					t.Commands = append(t.Commands, strings.TrimSpace(cmd))
				}
			case "Read":
				if p, ok := c.Input["file_path"].(string); ok {
					t.Reads = append(t.Reads, relTo(p, prefix))
				}
			case "Edit", "Write", "MultiEdit", "NotebookEdit":
				p, _ := c.Input["file_path"].(string)
				if p == "" {
					p, _ = c.Input["notebook_path"].(string)
				}
				if p != "" {
					t.Writes = append(t.Writes, relTo(p, prefix))
				}
			}
		}
	}
	return t
}

// relTo bỏ tiền tố thư mục làm việc khỏi đường dẫn tuyệt đối.
func relTo(p, prefix string) string {
	if prefix != "" && strings.HasPrefix(p, prefix) {
		return strings.TrimPrefix(p, prefix)
	}
	return p
}

// readOptional đọc tệp nếu có; ok=false khi không có hoặc không đọc được.
func readOptional(path string) ([]byte, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return b, true
}

// testFailureLines trích dòng báo test trượt (Go, pytest, Jest, TAP) và đuôi log.
func testFailureLines(log []byte) (fails, tail []string) {
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(string(log), "\r\n", "\n"), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, strings.TrimRight(l, " \t"))
		}
	}
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "--- FAIL:") || strings.HasPrefix(t, "FAIL\t") || strings.Contains(t, "FAILED") ||
			strings.HasPrefix(t, "✕") || strings.HasPrefix(t, "not ok") || strings.HasPrefix(t, "panic:") {
			fails = append(fails, clipRunes(t, 200))
		}
	}
	if len(fails) > 6 {
		fails = fails[:6]
	}
	start := len(lines) - 8
	if start < 0 {
		start = 0
	}
	for _, l := range lines[start:] {
		tail = append(tail, clipRunes(l, 200))
	}
	return fails, tail
}

// LoadFailures nạp các đơn vị thất bại: lượt đánh giá failed/timeout của tập validation
// (cả case_split của lượt lẫn split hiện tại của tác vụ phải là validation), và lượt thật
// có nhãn failure trong h. runtime (có thể rỗng) để đọc phạm vi và mục tiêu của phiếu.
// Chỉ đọc; không gọi mô hình.
func LoadFailures(ctx context.Context, db *sql.DB, cases []*eval.Case, h *History, runtime string) ([]Failure, error) {
	var out []Failure
	byID := map[string]*eval.Case{}
	for _, c := range cases {
		byID[c.ID] = c
	}
	if db != nil {
		attempts, err := eval.Attempts(ctx, db, `status IN ('failed','timeout') AND case_split = ?`, eval.SplitValidation)
		if err != nil {
			return nil, err
		}
		ctxUse, err := contextUsageOfFailures(ctx, db)
		if err != nil {
			return nil, err
		}
		for _, a := range attempts {
			c := byID[a.CaseID]
			if c == nil || c.Split != eval.SplitValidation {
				continue // tập test (hoặc tác vụ đã bị gỡ): không bao giờ là đầu vào
			}
			out = append(out, evalFailure(a, c, ctxUse[a.AttemptID]))
		}
	}
	if h != nil {
		tasks := task.NewStore(runtime)
		for _, r := range h.Runs {
			if r.Label != runlog.LabelFailure {
				continue
			}
			out = append(out, runFailure(r, tasks, runtime != ""))
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// contextUsageOfFailures trả về (đã chèn, đã trích dẫn) theo lượt đánh giá thất bại.
func contextUsageOfFailures(ctx context.Context, db *sql.DB) (map[string][2]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT u.attempt_id, MAX(u.retrieved), MAX(u.cited)
		FROM eval_context_usage u JOIN evaluation_runs r ON r.attempt_id = u.attempt_id
		WHERE r.status IN ('failed','timeout') AND r.case_split = ? GROUP BY u.attempt_id`, eval.SplitValidation)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][2]bool{}
	for rows.Next() {
		var id string
		var ret, cited int
		if err := rows.Scan(&id, &ret, &cited); err != nil {
			return nil, err
		}
		out[id] = [2]bool{ret > 0, cited > 0}
	}
	return out, rows.Err()
}

// evalFailure dựng đơn vị thất bại từ một lượt đánh giá.
func evalFailure(a eval.AttemptRow, c *eval.Case, ctxUse [2]bool) Failure {
	f := Failure{Source: "eval", ID: a.AttemptID, Unit: c.ID, Case: c.ID, Split: c.Split, Batch: a.BatchID, Arm: a.Arm,
		Status: a.Status, Prompt: c.Prompt, LogDir: a.LogDir, StartedAt: a.StartedAt, Error: a.Error,
		Retrieved: ctxUse[0], Cited: ctxUse[1]}
	// Vùng mã: thư mục tệp kiểm thử (nơi test trượt), không có thì thư mục tệp nguồn.
	files := append(append([]string{}, c.TestFiles...), c.SourceFiles...)
	if len(c.TestFiles) > 0 {
		f.Area = ModuleOf(c.TestFiles[0])
	} else if len(c.SourceFiles) > 0 {
		f.Area = ModuleOf(c.SourceFiles[0])
	}
	seen := map[string]bool{}
	for _, p := range files {
		if m := ModuleOf(p); m == "." {
			f.Allowed = nil // tệp ở gốc: không có phạm vi thư mục để so
			break
		} else if !seen[m] {
			seen[m] = true
			f.Allowed = append(f.Allowed, m+"/**")
		}
	}
	// Vết và tệp: log do runAttempt ghi. Thư mục làm việc là runs/<batch>/<key> cùng khoá.
	prefix := ""
	if a.LogDir != "" {
		prefix = strings.Replace(filepath.ToSlash(a.LogDir), "/logs/", "/runs/", 1) + "/"
	}
	if b, ok := readOptional(filepath.Join(a.LogDir, "agent.log")); ok {
		f.Trace = ParseTrace(b, prefix)
		f.Measured = f.Trace.Available
	}
	if b, ok := readOptional(filepath.Join(a.LogDir, eval.FilesListName)); ok {
		for _, l := range strings.Split(string(b), "\n") {
			if strings.TrimSpace(l) != "" {
				f.Modified = append(f.Modified, strings.TrimSpace(l))
			}
		}
	}
	if b, ok := readOptional(filepath.Join(a.LogDir, "test.log")); ok {
		f.TestFails, f.TestTail = testFailureLines(b)
	}
	f.Signals = classifySignals(&f)
	return f
}

// runFailure dựng đơn vị thất bại từ lượt thật. Phạm vi lấy từ phiếu nếu đọc được.
func runFailure(r *Run, tasks task.Store, haveRuntime bool) Failure {
	unit := r.TaskID
	if unit == "" {
		unit = r.ID
	}
	f := Failure{Source: "run", ID: r.ID, Unit: unit, Status: "failed", Area: r.Module,
		Modified: append([]string{}, r.Modified...), Read: append([]string{}, r.Read...),
		Retrieved: len(r.Retrieved) > 0, Cited: len(r.Cited) > 0, StartedAt: r.StartedAt}
	if haveRuntime && r.TaskID != "" {
		if t, err := tasks.Load(r.TaskID); err == nil {
			f.Prompt = t.Ticket.Goal
			if len(t.Ticket.Scope.Allow) > 0 {
				f.Allowed = append([]string{}, t.Ticket.Scope.Allow...)
			}
			f.allowDeny = append([]string{}, t.Ticket.Scope.Deny...)
		}
	}
	f.Signals = classifySignals(&f)
	return f
}

// classifySignals tính dấu hiệu hành vi xác định từ dữ liệu của lượt (không gọi mô hình).
func classifySignals(f *Failure) []string {
	var sigs []string
	add := func(s string) { sigs = append(sigs, s) }
	if f.Measured && !anyMatch(f.Trace.Commands, testCmdRe) {
		add(SigNoTest)
	}
	touched := append(append([]string{}, f.Modified...), f.Trace.Writes...)
	if (len(f.Allowed) > 0 && anyOutside(touched, f.Allowed)) || (len(f.allowDeny) > 0 && anyInside(touched, f.allowDeny)) {
		add(SigEditOutside)
	}
	if maxRepeat(f.Trace.Commands) >= repeatCmdMin {
		add(SigRepeatCmd)
	}
	if f.Source == "eval" && f.Status == "timeout" {
		add(SigTimeout)
	}
	if f.Retrieved && !f.Cited {
		add(SigIgnoredCtx)
	}
	return sigs
}

// anyMatch: có chuỗi nào khớp biểu thức.
func anyMatch(xs []string, re *regexp.Regexp) bool {
	for _, x := range xs {
		if re.MatchString(x) {
			return true
		}
	}
	return false
}

// anyOutside: có tệp nào không khớp mẫu phạm vi nào.
func anyOutside(files, allowed []string) bool {
	sc := knowledge.Scope{Paths: allowed}
	for _, f := range files {
		if !sc.MatchScope(f) {
			return true
		}
	}
	return false
}

// anyInside: có tệp nào khớp mẫu cấm.
func anyInside(files, denied []string) bool {
	sc := knowledge.Scope{Paths: denied}
	for _, f := range files {
		if sc.MatchScope(f) {
			return true
		}
	}
	return false
}

// maxRepeat: số lần xuất hiện nhiều nhất của một lệnh (chuẩn hoá khoảng trắng).
func maxRepeat(cmds []string) int {
	counts := map[string]int{}
	best := 0
	for _, c := range cmds {
		k := strings.Join(strings.Fields(c), " ")
		counts[k]++
		if counts[k] > best {
			best = counts[k]
		}
	}
	return best
}

// clipRunes cắt chuỗi theo số ký tự (rune), thêm "…" khi bị cắt.
func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
