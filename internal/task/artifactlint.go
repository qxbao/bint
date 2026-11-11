package task

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// PlanningContractV1 là phiên bản hợp đồng artifact của b-req (AC/TSK/Q có cấu trúc).
// State.PlanningContractVersion = 0 là task cũ: không bị chặn tự động.
const PlanningContractV1 = 1

// ArtifactIssue là một phát hiện của bộ kiểm artifact. Code ổn định (dùng cho CLI/agent).
type ArtifactIssue struct {
	Code     string `json:"code"`
	Artifact string `json:"artifact"`
	Detail   string `json:"detail"`
}

func (i ArtifactIssue) String() string {
	return fmt.Sprintf("[%s] %s: %s", i.Code, i.Artifact, i.Detail)
}

// ArtifactReport là kết quả kiểm ba artifact của b-req.
type ArtifactReport struct {
	Errors        []ArtifactIssue `json:"errors"`
	Warnings      []ArtifactIssue `json:"warnings"`
	OpenQuestions []ArtifactIssue `json:"open_questions"`
}

// Blocking: có lỗi cấu trúc. Câu hỏi mở không làm Blocking() true; nơi gọi xét riêng.
func (r ArtifactReport) Blocking() bool { return len(r.Errors) > 0 }

var (
	lintHeadingRe  = regexp.MustCompile(`^(#{1,2})\s+(.+?)\s*$`)
	lintCheckboxRe = regexp.MustCompile(`^\s*-\s+\[([ xX])\]\s+(.*\S)\s*$`)
	lintACRe       = regexp.MustCompile(`^AC-(\d+)\s*:\s*(.*)$`)
	lintTSKRe      = regexp.MustCompile(`^TSK-(\d+)\s*\[([^\]]*)\]\s*:\s*(.*)$`)
	lintQRe        = regexp.MustCompile(`^Q-(\d+)\s*:\s*(.*)$`)
	lintDoneRe     = regexp.MustCompile(`(?i)^(.*?)\s*[—–-]+\s*xong khi\s*:\s*(.*)$`)
	lintAnswerRe   = regexp.MustCompile(`(?i)\s[—–-]+\s*trả lời\s*:\s*(.*)$`)
	lintRefRe      = regexp.MustCompile(`^AC-(\d+)$`)
	lintPlaceholdR = regexp.MustCompile(`(?i)<[^<>\n]*>|…|\b(?:todo|tbd)\b`)
)

const (
	artReq   = "requirement.md"
	artPlan  = "plan.md"
	artTasks = "tasks.md"
)

type acItem struct {
	N    int
	Text string
}

type tskItem struct {
	N    int
	Refs []int
}

type reportBuilder struct{ r ArtifactReport }

func (b *reportBuilder) err(code, artifact, format string, a ...any) {
	b.r.Errors = append(b.r.Errors, ArtifactIssue{Code: code, Artifact: artifact, Detail: fmt.Sprintf(format, a...)})
}

func (b *reportBuilder) question(code, detail string) {
	b.r.OpenQuestions = append(b.r.OpenQuestions, ArtifactIssue{Code: code, Artifact: artReq, Detail: detail})
}

func normSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

// scanLines gọi fn cho từng dòng ngoài code fence cùng tên section (heading cấp 1–2 gần nhất).
func scanLines(text string, fn func(section string, line string)) {
	section, fenced := "", false
	for line := range strings.SplitSeq(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		if m := lintHeadingRe.FindStringSubmatch(line); m != nil {
			section = strings.ToLower(normSpace(m[2]))
			continue
		}
		fn(section, line)
	}
}

const (
	secAC = "tiêu chí chấp nhận"
	secQ  = "câu hỏi mở"
)

// LintPlanningText kiểm ba artifact (key requirement|plan|tasks; thiếu key = thiếu tệp)
// so với phiếu. Không I/O. strict=false hạ mọi lỗi định dạng thành cảnh báo nhưng vẫn
// liệt kê câu hỏi mở.
func LintPlanningText(ticket Ticket, docs map[string]string, strict bool) ArtifactReport {
	return lintPlanning(ticket, docs, nil, strict)
}

func lintPlanning(ticket Ticket, docs map[string]string, unreadable map[string]error, strict bool) ArtifactReport {
	b := &reportBuilder{r: ArtifactReport{Errors: []ArtifactIssue{}, Warnings: []ArtifactIssue{}, OpenQuestions: []ArtifactIssue{}}}
	usable := map[string]bool{}
	for _, n := range ArtifactNames() {
		file := Artifacts[n]
		text, ok := docs[n]
		switch {
		case unreadable[n] != nil:
			b.err("artifact_unreadable", file, "không đọc được: %v", unreadable[n])
		case !ok:
			b.err("artifact_missing", file, "chưa có tệp; ghi bằng `bint task write <id> %s -`", n)
		case strings.TrimSpace(text) == "":
			b.err("artifact_empty", file, "tệp chỉ có khoảng trắng; ghi lại nội dung bằng `bint task write <id> %s -`", n)
		default:
			usable[n] = true
		}
	}

	var acs []acItem
	if usable["requirement"] {
		acs = lintRequirement(b, docs["requirement"])
	}
	var tsks []tskItem
	if usable["tasks"] {
		tsks = lintTasks(b, docs["tasks"])
	}

	// Đối chiếu AC ↔ phiếu và AC ↔ TSK (chỉ khi requirement dùng được).
	if usable["requirement"] {
		crit := ticket.Verify.Criteria
		if len(crit) == 0 {
			b.err("no_criteria", "task.yml", "phiếu chưa có tiêu chí (verify.criteria); chạy `bint task set <id> --clear-criteria --criteria \"…\"`")
		}
		if len(crit) > 0 && len(acs) > 0 && len(crit) != len(acs) {
			b.err("criteria_count_mismatch", artReq, "phiếu có %d tiêu chí nhưng requirement.md có %d dòng AC; hai danh sách phải khớp từng mục", len(crit), len(acs))
		}
		for _, a := range acs {
			if a.N >= 1 && a.N <= len(crit) && normSpace(a.Text) != normSpace(crit[a.N-1]) {
				b.err("criterion_mismatch", artReq, "AC-%d (%q) khác tiêu chí thứ %d trong task.yml (%q); sửa một trong hai cho khớp", a.N, normSpace(a.Text), a.N, normSpace(crit[a.N-1]))
			}
		}
	}
	if usable["requirement"] && usable["tasks"] {
		known := map[int]bool{}
		for _, a := range acs {
			known[a.N] = true
		}
		covered := map[int]bool{}
		for _, t := range tsks {
			for _, ref := range t.Refs {
				if !known[ref] {
					b.err("unknown_ac", artTasks, "TSK-%d tham chiếu AC-%d không có trong requirement.md", t.N, ref)
				}
				covered[ref] = true
			}
		}
		if len(acs) > 0 {
			ids := make([]int, 0, len(known))
			for n := range known {
				ids = append(ids, n)
			}
			sort.Ints(ids)
			for _, n := range ids {
				if !covered[n] {
					b.err("uncovered_ac", artTasks, "AC-%d chưa có TSK nào bao phủ; thêm `[AC-%d]` vào một TSK", n, n)
				}
			}
		}
	}

	if !strict {
		b.r.Warnings = append(b.r.Warnings, b.r.Errors...)
		b.r.Errors = []ArtifactIssue{}
	}
	return b.r
}

func lintRequirement(b *reportBuilder, text string) []acItem {
	var acs []acItem
	seenAC, seenQ := map[int]bool{}, map[int]bool{}
	scanLines(text, func(section, line string) {
		cb := lintCheckboxRe.FindStringSubmatch(line)
		if cb == nil {
			return
		}
		checked, content := cb[1] != " ", cb[2]
		switch section {
		case secAC:
			m := lintACRe.FindStringSubmatch(content)
			if m == nil {
				if strings.HasPrefix(content, "AC-") {
					b.err("malformed_ac", artReq, "dòng %q sai khuôn; dùng `- [ ] AC-1: <tiêu chí>`", content)
				}
				return
			}
			n, _ := strconv.Atoi(m[1])
			if n < 1 {
				b.err("invalid_id", artReq, "AC-%s: số thứ tự phải từ 1", m[1])
				return
			}
			if strings.TrimSpace(m[2]) == "" {
				b.err("malformed_ac", artReq, "AC-%d không có nội dung", n)
			}
			if lintPlaceholdR.MatchString(content) {
				b.err("placeholder_text", artReq, "AC-%d còn placeholder (<…>, …, TODO, TBD); viết tiêu chí cụ thể", n)
			}
			if seenAC[n] {
				b.err("duplicate_id", artReq, "AC-%d xuất hiện nhiều lần", n)
				return
			}
			seenAC[n] = true
			acs = append(acs, acItem{N: n, Text: m[2]})
		case secQ:
			m := lintQRe.FindStringSubmatch(content)
			if m == nil {
				return
			}
			n, _ := strconv.Atoi(m[1])
			if n < 1 {
				b.err("invalid_id", artReq, "Q-%s: số thứ tự phải từ 1", m[1])
				return
			}
			if seenQ[n] {
				b.err("duplicate_id", artReq, "Q-%d xuất hiện nhiều lần", n)
				return
			}
			seenQ[n] = true
			question := strings.TrimSpace(lintAnswerRe.ReplaceAllString(m[2], ""))
			if !checked {
				b.question("question_open", fmt.Sprintf("Q-%d: %s", n, question))
				return
			}
			am := lintAnswerRe.FindStringSubmatch(" " + m[2])
			if am == nil || strings.TrimSpace(am[1]) == "" {
				b.question("question_answer_empty", fmt.Sprintf("Q-%d: %s — đã đánh dấu [x] nhưng chưa có `— Trả lời: <nội dung>`", n, question))
			}
		}
	})
	if len(acs) == 0 {
		b.err("no_ac", artReq, "không có dòng AC nào dưới `## Tiêu chí chấp nhận`; dùng `- [ ] AC-1: <tiêu chí>`")
		return acs
	}
	seq := make([]int, 0, len(acs))
	for _, a := range acs {
		seq = append(seq, a.N)
	}
	sort.Ints(seq)
	for i, n := range seq {
		if n != i+1 {
			b.err("ac_not_sequential", artReq, "số thứ tự AC phải liên tục từ 1 (gặp AC-%d ở vị trí %d)", n, i+1)
			break
		}
	}
	return acs
}

func lintTasks(b *reportBuilder, text string) []tskItem {
	var tsks []tskItem
	seen := map[int]bool{}
	scanLines(text, func(_ string, line string) {
		cb := lintCheckboxRe.FindStringSubmatch(line)
		if cb == nil {
			return
		}
		content := cb[2]
		if !strings.HasPrefix(content, "TSK-") {
			b.r.Warnings = append(b.r.Warnings, ArtifactIssue{Code: "unstructured_task", Artifact: artTasks,
				Detail: fmt.Sprintf("dòng %q không có ID TSK-N nên không được tính; dùng `- [ ] TSK-1 [AC-1]: <việc> — xong khi: <điều kiện>`", content)})
			return
		}
		m := lintTSKRe.FindStringSubmatch(content)
		if m == nil {
			b.err("malformed_task", artTasks, "dòng %q sai khuôn; dùng `- [ ] TSK-1 [AC-1]: <việc> — xong khi: <điều kiện>`", content)
			return
		}
		n, _ := strconv.Atoi(m[1])
		if n < 1 {
			b.err("invalid_id", artTasks, "TSK-%s: số thứ tự phải từ 1", m[1])
			return
		}
		if lintPlaceholdR.MatchString(content) {
			b.err("placeholder_text", artTasks, "TSK-%d còn placeholder (<…>, …, TODO, TBD); viết việc cụ thể", n)
		}
		if seen[n] {
			b.err("duplicate_id", artTasks, "TSK-%d xuất hiện nhiều lần", n)
			return
		}
		seen[n] = true
		var refs []int
		for _, tok := range strings.FieldsFunc(m[2], func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
			rm := lintRefRe.FindStringSubmatch(tok)
			if rm == nil {
				b.err("malformed_task", artTasks, "TSK-%d: tham chiếu %q không phải dạng AC-N", n, tok)
				continue
			}
			ref, _ := strconv.Atoi(rm[1])
			refs = append(refs, ref)
		}
		if len(refs) == 0 {
			b.err("task_without_ac", artTasks, "TSK-%d phải tham chiếu ít nhất một AC, ví dụ `[AC-1]`", n)
		}
		dm := lintDoneRe.FindStringSubmatch(m[3])
		switch {
		case dm == nil:
			b.err("task_incomplete", artTasks, "TSK-%d thiếu `— xong khi: <điều kiện>`", n)
		case strings.TrimSpace(dm[1]) == "":
			b.err("task_incomplete", artTasks, "TSK-%d chưa mô tả việc cần làm", n)
		case strings.TrimSpace(dm[2]) == "":
			b.err("task_incomplete", artTasks, "TSK-%d có `xong khi:` nhưng chưa có điều kiện", n)
		}
		tsks = append(tsks, tskItem{N: n, Refs: refs})
	})
	if len(tsks) == 0 {
		b.err("no_tasks", artTasks, "không có dòng TSK nào; dùng `- [ ] TSK-1 [AC-1]: <việc> — xong khi: <điều kiện>`")
	}
	return tsks
}

// LintPlanningTask đọc ba artifact của tác vụ và kiểm như LintPlanningText.
func LintPlanningTask(t *Task, strict bool) ArtifactReport {
	docs := map[string]string{}
	bad := map[string]error{}
	for _, n := range ArtifactNames() {
		b, err := os.ReadFile(t.ArtifactPath(n))
		switch {
		case err == nil:
			docs[n] = string(b)
		case errors.Is(err, os.ErrNotExist):
		default:
			bad[n] = err
		}
	}
	return lintPlanning(t.Ticket, docs, bad, strict)
}
