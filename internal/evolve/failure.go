package evolve

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

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
	// Truncated: vết thiếu (có dòng quá dài có thể chứa lệnh bị bỏ, hoặc log vượt trần
	// maxTraceBytes). Vết thiếu
	// không đủ để suy ra điều gì từ SỰ VẮNG MẶT của lệnh (vd. no_test).
	Truncated bool
}

// testCmdRe khớp lệnh chạy kiểm thử (dấu hiệu "có chạy test").
var testCmdRe = regexp.MustCompile(`(?i)\b(test|tests|pytest|jest|vitest|rspec)\b`)

// ParseTrace đọc log stream-json. prefix là tiền tố tuyệt đối của thư mục làm việc (bỏ
// khỏi đường dẫn để còn đường dẫn tương đối); rỗng = giữ nguyên.
func ParseTrace(data []byte, prefix string) Trace {
	return ParseTraceReader(bytes.NewReader(data), prefix)
}

// Giới hạn đọc vết: mỗi dòng (một sự kiện, tool_result có thể rất lớn) tối đa maxTraceLine byte,
// dòng dài hơn bị bỏ qua (Trace.Truncated khi dòng đó có thể chứa lệnh, xem
// skippedMayHoldToolUse); cả log tối đa maxTraceBytes byte, vượt thì Trace.Truncated.
var (
	maxTraceLine  = 4 << 20
	maxTraceBytes = int64(256 << 20)
)

// ParseTraceReader đọc log stream-json theo luồng từng dòng: bộ nhớ bị chặn bởi maxTraceLine mà
// không mất phần cuối của log (lệnh test thường nằm ở cuối).
func ParseTraceReader(r io.Reader, prefix string) Trace {
	var t Trace
	lr := &io.LimitedReader{R: r, N: maxTraceBytes + 1}
	br := bufio.NewReaderSize(lr, 64<<10)
	for {
		line, skipped, head, err := readLineMax(br, maxTraceLine)
		if skipped && skippedMayHoldToolUse(head) {
			t.Truncated = true
		}
		if len(line) > 0 {
			parseTraceLine(&t, line, prefix)
		}
		if err != nil {
			break
		}
	}
	if lr.N <= 0 {
		t.Truncated = true // log lớn hơn maxTraceBytes: phần sau bị bỏ
	}
	return t
}

// skippedHeadLen: số byte đầu của dòng bị bỏ được giữ lại để nhận ra loại sự kiện.
const skippedHeadLen = 256

// eventTypeRe lấy giá trị "type" đứng đầu một sự kiện stream-json.
var eventTypeRe = regexp.MustCompile(`^\s*\{\s*"type"\s*:\s*"([^"]*)"`)

// skippedMayHoldToolUse: dòng quá dài bị bỏ có thể chứa tool_use (sự kiện assistant) không. Dòng
// khổng lồ thường là user/tool_result (đọc tệp lớn, đầu ra dài) và không chứa lệnh nào, nên khi
// đầu dòng cho thấy rõ loại sự kiện khác thì vết vẫn đủ; không nhận ra được thì coi là có thể.
func skippedMayHoldToolUse(head []byte) bool {
	m := eventTypeRe.FindSubmatch(head)
	return m == nil || string(m[1]) == "assistant"
}

// readLineMax đọc một dòng (bỏ '\n'). Dòng dài hơn max byte bị bỏ hết và trả skipped=true
// (line rỗng) cùng head là tối đa skippedHeadLen byte đầu của dòng, mà vẫn đọc tới cuối dòng để
// lần gọi sau bắt đầu ở dòng kế.
func readLineMax(br *bufio.Reader, max int) (line []byte, skipped bool, head []byte, err error) {
	var buf []byte
	for {
		chunk, e := br.ReadSlice('\n')
		if !skipped {
			if len(buf)+len(chunk) > max {
				h := append(buf, chunk...)
				head = append([]byte(nil), h[:min(len(h), skippedHeadLen)]...)
				skipped, buf = true, nil
			} else {
				buf = append(buf, chunk...)
			}
		}
		if e == bufio.ErrBufferFull {
			continue
		}
		if skipped {
			return nil, true, head, e
		}
		return bytes.TrimSpace(buf), false, nil, e
	}
}

// parseTraceLine gộp một dòng log (đã bỏ khoảng trắng hai đầu) vào vết.
func parseTraceLine(t *Trace, line []byte, prefix string) {
	if len(line) == 0 || line[0] != '{' {
		return
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
		return
	}
	if ev.Type == "assistant" || ev.Type == "system" {
		t.Available = true
	}
	if ev.Type != "assistant" {
		return
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

// relTo bỏ tiền tố thư mục làm việc khỏi đường dẫn tuyệt đối. Chuẩn hoá cả hai phía bằng
// filepath.ToSlash trước khi so (trên Windows file_path của harness dùng dấu gạch ngược).
func relTo(p, prefix string) string {
	p = filepath.ToSlash(p)
	if prefix = filepath.ToSlash(prefix); prefix != "" {
		if !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
		if strings.HasPrefix(p, prefix) {
			return strings.TrimPrefix(p, prefix)
		}
	}
	return p
}

// workDirOf dựng thư mục làm việc của lượt eval từ thư mục log: log nằm ở
// <WorkRoot>/logs/<batch>/<key> còn thư mục làm việc là <WorkRoot>/runs/<batch>/<key> (xem
// eval.runAttempt). Dựng theo cấu trúc, không thay chuỗi "/logs/" (WorkRoot có thể chứa nó).
func workDirOf(logDir string) string {
	if logDir == "" {
		return ""
	}
	logDir = filepath.Clean(logDir)
	key := filepath.Base(logDir)
	batchDir := filepath.Dir(logDir)
	logsDir := filepath.Dir(batchDir)
	if filepath.Base(logsDir) != "logs" {
		return "" // cấu trúc lạ: không đoán, giữ đường dẫn nguyên
	}
	return filepath.Join(filepath.Dir(logsDir), "runs", filepath.Base(batchDir), key)
}

// readOptional đọc tệp nếu có; ok=false khi không có hoặc không đọc được.
func readOptional(path string) ([]byte, bool) {
	b, err := os.ReadFile(path)
	return b, err == nil
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
// since (không phải zero) là đầu mút cửa sổ lịch sử: chỉ lượt eval bắt đầu từ since mới được nạp
// (cùng cửa sổ với lượt thật trong h), để mỗi lần quét không đọc log của cả lịch sử.
// Lượt timeout nhưng test đạt (test_passed=1) đã giải đúng nên không phải thất bại. lessons là
// tập ID bài học từ thất bại (LessonIDs): không tính vào "đã chèn tri thức" của dấu hiệu
// ignored_ctx, vì bài học chung được chèn ở mọi đề bài và hiếm khi được trích dẫn. Chỉ đọc;
// không gọi mô hình.
func LoadFailures(ctx context.Context, db *sql.DB, cases []*eval.Case, h *History, runtime string, since time.Time, lessons map[string]bool) ([]Failure, error) {
	var out []Failure
	byID := map[string]*eval.Case{}
	for _, c := range cases {
		byID[c.ID] = c
	}
	if db != nil {
		where, args := failedAttemptsWhere("", since)
		attempts, err := eval.Attempts(ctx, db, where, args...)
		if err != nil {
			return nil, err
		}
		ctxUse, err := contextUsageOfFailures(ctx, db, since, lessons)
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
			out = append(out, runFailure(r, tasks, runtime != "", lessons))
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

// failedAttemptsWhere dựng điều kiện chọn lượt eval thất bại của tập validation: failed, hoặc
// timeout mà test không đạt; từ since trở đi nếu có. alias là tiền tố bảng (vd "r.").
func failedAttemptsWhere(alias string, since time.Time) (string, []any) {
	where := fmt.Sprintf(`%[1]sstatus IN ('failed','timeout') AND NOT (%[1]sstatus = 'timeout' AND COALESCE(%[1]stest_passed, 0) = 1) AND %[1]scase_split = ?`, alias)
	args := []any{eval.SplitValidation}
	if !since.IsZero() {
		where += fmt.Sprintf(" AND %sstarted_at >= ?", alias)
		args = append(args, knowledge.Now(since))
	}
	return where, args
}

// contextUsageOfFailures trả về (đã chèn, đã trích dẫn) theo lượt đánh giá thất bại, không tính
// các mục trong lessons.
func contextUsageOfFailures(ctx context.Context, db *sql.DB, since time.Time, lessons map[string]bool) (map[string][2]bool, error) {
	where, args := failedAttemptsWhere("r.", since)
	rows, err := db.QueryContext(ctx, `SELECT u.attempt_id, u.item_id, u.retrieved, u.cited
		FROM eval_context_usage u JOIN evaluation_runs r ON r.attempt_id = u.attempt_id
		WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][2]bool{}
	for rows.Next() {
		var id, item string
		var ret, cited int
		if err := rows.Scan(&id, &item, &ret, &cited); err != nil {
			return nil, err
		}
		if lessons[item] {
			continue
		}
		u := out[id]
		out[id] = [2]bool{u[0] || ret > 0, u[1] || cited > 0}
	}
	return out, rows.Err()
}

// LessonIDs trả về tập ID của bài học từ thất bại (LessonFromRuns) trong items.
func LessonIDs(items []*knowledge.Item) map[string]bool {
	out := map[string]bool{}
	for _, it := range items {
		if it.LessonFromRuns() {
			out[it.ID] = true
		}
	}
	return out
}

// countNonLesson đếm ID không thuộc lessons.
func countNonLesson(ids []string, lessons map[string]bool) int {
	n := 0
	for _, id := range ids {
		if !lessons[id] {
			n++
		}
	}
	return n
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
	prefix := workDirOf(a.LogDir)
	if lf, err := os.Open(filepath.Join(a.LogDir, "agent.log")); err == nil {
		f.Trace = ParseTraceReader(lf, prefix)
		lf.Close()
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

// runFailure dựng đơn vị thất bại từ lượt thật. Phạm vi lấy từ phiếu nếu đọc được. Bài học
// (lessons) không tính vào đã chèn/đã trích dẫn (xem LoadFailures).
func runFailure(r *Run, tasks task.Store, haveRuntime bool, lessons map[string]bool) Failure {
	unit := r.TaskID
	if unit == "" {
		unit = r.ID
	}
	f := Failure{Source: "run", ID: r.ID, Unit: unit, Status: "failed", Area: r.Module,
		Modified: append([]string{}, r.Modified...), Read: append([]string{}, r.Read...),
		Retrieved: countNonLesson(r.Retrieved, lessons) > 0, Cited: countNonLesson(r.Cited, lessons) > 0, StartedAt: r.StartedAt}
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
	if f.Measured && !f.Trace.Truncated && !anyMatch(f.Trace.Commands, testCmdRe) {
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
