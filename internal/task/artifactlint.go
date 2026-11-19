package task

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"bint/internal/pathglob"
)

// PlanningContractV1 là phiên bản hợp đồng artifact của b-req (AC/TSK/Q có cấu trúc).
// State.PlanningContractVersion = 0 là task cũ: không bị chặn tự động.
const PlanningContractV1 = 1

// PlanningContractV2 thêm luật đo hiện trạng/bề mặt/giả định/tệp trong phạm vi (baseline_missing, ac_without_measure,
// surface_missing, surface_unmapped, assumptions_missing, plan_file_out_of_scope): lỗi ở v2, cảnh báo ở v1.
const PlanningContractV2 = 2

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
	lintHeadingRe  = regexp.MustCompile(`^ {0,3}(#{1,2})\s+(.+?)\s*$`)
	lintDeepHeadRe = regexp.MustCompile(`^ {0,3}#{3,}\s+(.+?)\s*$`)
	lintCheckboxRe = regexp.MustCompile(`^\s*[-*+]\s+\[([ xX])\]\s+(.*\S)\s*$`)
	lintACRe       = regexp.MustCompile(`^AC-(\d+)\s*:\s*(.*)$`)
	lintTSKRe      = regexp.MustCompile(`^TSK-(\d+)\s*\[([^\]]*)\]\s*:\s*(.*)$`)
	lintQRe        = regexp.MustCompile(`^Q-(\d+)\s*:\s*(.*)$`)
	lintDoneRe     = regexp.MustCompile(`(?i)^(.*?)\s*[—–-]+\s*xong khi\s*:\s*(.*)$`)
	lintAnswerRe   = regexp.MustCompile(`(?i)\s[—–-]+\s*trả lời\s*:\s*(.*)$`)
	lintRefRe      = regexp.MustCompile(`^AC-(\d+)$`)
	lintPlaceholdR = regexp.MustCompile(`(?i)<[^<>\n]*>|…|\b(?:todo|tbd)\b`)
)

// stripCodeSpans xoá mọi code span Markdown (n dấu backtick mở tới đúng n dấu backtick đóng) khỏi s.
// Span không có dấu đóng khớp được giữ nguyên (RE2 không có backreference nên quét tay).
func stripCodeSpans(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '`' {
			b.WriteByte(s[i])
			i++
			continue
		}
		n := 0
		for i+n < len(s) && s[i+n] == '`' {
			n++
		}
		closeAt := -1
		for j := i + n; j < len(s); {
			if s[j] != '`' {
				j++
				continue
			}
			m := 0
			for j+m < len(s) && s[j+m] == '`' {
				m++
			}
			if m == n {
				closeAt = j + m
				break
			}
			j += m
		}
		if closeAt < 0 {
			b.WriteString(s[i : i+n]) // không có dấu đóng: giữ nguyên, quét tiếp sau dãy backtick
			i += n
			continue
		}
		b.WriteByte(' ')
		i = closeAt
	}
	return b.String()
}

func isIdentRune(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

// dottedSegment: đoạn s[start:end] (một dạng <…>) là một thành phần của định danh có dấu chấm, như
// limits.workflows.<flow> hoặc harnesses.<h>.model: ngay trước là `ident.` (ident cũng có thể là một đoạn <…> khác liền
// kề, như a.<x>.<y>). Đoạn đứng đầu (không có `ident.` ngay trước) không được miễn dù theo sau là dấu chấm: `<tên>.md`
// hay `<việc>.Tiếp` là placeholder chưa điền, cũng như `<việc>.` cuối câu.
func dottedSegment(s string, start, end int) bool {
	if start >= 2 && s[start-1] == '.' {
		if r, _ := utf8.DecodeLastRuneInString(s[:start-1]); isIdentRune(r) || r == '>' {
			return true
		}
	}
	return false
}

// hasPlaceholder: nội dung dòng AC/TSK còn placeholder (<…> đứng riêng, ký tự …, TODO, TBD). Bỏ qua
// nội dung trong code span và đoạn <…> là thành phần của định danh có dấu chấm.
func hasPlaceholder(content string) bool {
	s := stripCodeSpans(content)
	for _, loc := range lintPlaceholdR.FindAllStringIndex(s, -1) {
		if s[loc[0]] == '<' && dottedSegment(s, loc[0], loc[1]) {
			continue
		}
		return true
	}
	return false
}

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

// sectionKind chuẩn hóa tên heading: secAC/secQ cho hai mục có ý nghĩa, còn lại giữ tên đã hạ chữ thường.
// Chấp nhận hậu tố mô tả ("Câu hỏi mở (cần trả lời)") và dạng số nhiều ("Các câu hỏi mở").
func sectionKind(name string) string {
	n := strings.ToLower(normSpace(strings.TrimRight(name, "# \t")))
	switch {
	case strings.HasPrefix(n, secAC):
		return secAC
	case strings.HasPrefix(n, secQ), strings.HasPrefix(n, "các "+secQ):
		return secQ
	}
	return n
}

// scanLines gọi fn cho từng dòng ngoài code fence cùng tên section (heading cấp 1–2 gần nhất).
// Fence đóng bằng đúng loại ký hiệu đã mở (``` hoặc ~~~). Trả về true nếu hết văn bản mà fence vẫn mở:
// mọi dòng sau đó bị bỏ qua nên nơi gọi phải báo lỗi thay vì lặng lẽ coi như không có câu hỏi.
func scanLines(text string, fn func(section string, line string)) (unclosedFence bool) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	return walkLines(lines, func(_ int, section, line string) { fn(section, line) }, nil)
}

// walkLines là lõi của scanLines: gọi fn(chỉ số dòng, section, dòng) cho từng dòng ngoài code fence và heading.
// heading (nếu có) được gọi cho mỗi heading cấp 1–2. Dùng chung với đồng bộ tiêu chí để hai nơi hiểu cùng một cấu trúc tệp.
func walkLines(lines []string, fn func(i int, section string, line string), heading func(i int, kind string)) (unclosedFence bool) {
	section, marker := "", ""
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if marker != "" {
			if strings.HasPrefix(t, marker) {
				marker = ""
			}
			continue
		}
		if strings.HasPrefix(t, "```") {
			marker = "```"
			continue
		}
		if strings.HasPrefix(t, "~~~") {
			marker = "~~~"
			continue
		}
		if m := lintHeadingRe.FindStringSubmatch(line); m != nil {
			section = sectionKind(m[2])
			if heading != nil {
				heading(i, section)
			}
			continue
		}
		fn(i, section, line)
	}
	return marker != ""
}

const (
	secAC    = "tiêu chí chấp nhận"
	secQ     = "câu hỏi mở"
	secGoal  = "kiểm tra mục tiêu"
	secPhase = "đề xuất tách giai đoạn"
	// secSize/secSurface: mục của plan.md mà cổng cỡ kế hoạch của b-do đọc (PlanSizeDecl, PlanSurfaces).
	secSize    = "cỡ kế hoạch"
	secSurface = "bề mặt bị ảnh hưởng"
	// secStrategy: mục `## Chiến lược thực thi` của plan.md (Chia lát, b-goal) mà Runner và lint đọc (PlanStrategy).
	secStrategy = "chiến lược thực thi"
)

// Lựa chọn của một dòng trong `## Chiến lược thực thi`.
const (
	StrategyYes     = "có"
	StrategyNo      = "không"
	StrategyMissing = "" // thiếu mục, thiếu dòng, sai giá trị hoặc thiếu lý do
)

var (
	strategySliceRe = regexp.MustCompile(`(?i)^\s*(?:[-*+]\s+)?Chia lát\s*:\s*(có|không)\s*(?:[—–-]+\s*(\S.*))?$`)
	strategyGoalRe  = regexp.MustCompile(`(?i)^\s*(?:[-*+]\s+)?Kiểm tra mục tiêu độc lập\s*\(b-goal\)\s*:\s*(có|không)\s*(?:[—–-]+\s*(\S.*))?$`)
)

// PlanStrategy đọc mục `## Chiến lược thực thi` của plan.md (ngoài code fence): lựa chọn "Chia lát: có|không" và
// "Kiểm tra mục tiêu độc lập (b-goal): có|không" (mỗi dòng cần lý do sau dấu gạch ngang). Dòng thiếu/sai/không có lý do trả StrategyMissing.
// Dòng đầu hợp lệ của mỗi loại là kết quả.
func PlanStrategy(plan string) (slice, goal string) {
	scanLines(plan, func(section, line string) {
		if section != secStrategy {
			return
		}
		for _, d := range []struct {
			re  *regexp.Regexp
			out *string
		}{{strategySliceRe, &slice}, {strategyGoalRe, &goal}} {
			if m := d.re.FindStringSubmatch(line); m != nil && strings.TrimSpace(m[2]) != "" && *d.out == StrategyMissing {
				*d.out = strings.ToLower(m[1])
			}
		}
	})
	return slice, goal
}

// LintPlanningText kiểm ba artifact (key requirement|plan|tasks; thiếu key = thiếu tệp)
// so với phiếu. Không I/O. strict=false hạ mọi lỗi định dạng thành cảnh báo nhưng vẫn
// liệt kê câu hỏi mở.
// Không áp luật v2 (version 0); dùng LintPlanningTextV khi biết phiên bản hợp đồng.
func LintPlanningText(ticket Ticket, docs map[string]string, strict bool) ArtifactReport {
	return lintPlanning(ticket, docs, nil, strict, 0, nil)
}

// LintPlanningTextFrozen như LintPlanningTextV nhưng nhận danh sách tệp kiểm tra mục tiêu đã đóng băng (State.GoalTest.Paths()):
// plan.md được nêu các tệp này trong `## Cỡ kế hoạch` dù chúng nằm ngoài scope.allow hoặc trong scope.deny.
func LintPlanningTextFrozen(ticket Ticket, docs map[string]string, strict bool, version int, frozen []string) ArtifactReport {
	return lintPlanning(ticket, docs, nil, strict, version, frozen)
}

// LintPlanningTextV như LintPlanningText nhưng theo phiên bản hợp đồng của tác vụ.
func LintPlanningTextV(ticket Ticket, docs map[string]string, strict bool, version int) ArtifactReport {
	return lintPlanning(ticket, docs, nil, strict, version, nil)
}

func lintPlanning(ticket Ticket, docs map[string]string, unreadable map[string]error, strict bool, version int, frozen []string) ArtifactReport {
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
	// Luật hợp đồng v2 (hiện trạng đo được, bề mặt, giả định, tệp trong phạm vi): lỗi ở v2, chỉ cảnh báo ở v1; v0 bỏ qua.
	if version >= PlanningContractV1 {
		v2 := &reportBuilder{}
		if usable["requirement"] {
			if lintBaseline(v2, docs["requirement"], acs) {
				b.r.Warnings = append(b.r.Warnings, ArtifactIssue{Code: "baseline_exempt", Artifact: artReq,
					Detail: "`## Hiện trạng` dùng lối thoát `Không đo được: <lý do>` thay cho số đo: chỉ dùng khi thật sự không đo được; người duyệt sẽ xem lý do"})
			}
		}
		if usable["plan"] {
			lintPlanV2(v2, ticket, docs["plan"], acs, frozen)
		}
		if version >= PlanningContractV2 && strict {
			b.r.Errors = append(b.r.Errors, v2.r.Errors...)
		} else {
			b.r.Warnings = append(b.r.Warnings, v2.r.Errors...)
		}
	}
	// Luôn là cảnh báo (kể cả strict): phiếu cũ và việc nhỏ không bị chặn, nhưng planner bỏ mục thì thấy ngay (GĐ2 nhóm 2).
	if usable["requirement"] && GoalCheck(docs["requirement"]) == "" {
		b.r.Warnings = append(b.r.Warnings, ArtifactIssue{Code: "goal_check_missing", Artifact: artReq,
			Detail: "requirement.md thiếu mục `## Kiểm tra mục tiêu` (hoặc mục rỗng): mô tả một kiểm tra chạy được suy từ mục tiêu người dùng và từng nhóm của `## Bề mặt bị ảnh hưởng`, kèm một AC cho kiểm tra đó"})
	}
	return b.r
}

// GoalCheck trả thân mục `## Kiểm tra mục tiêu` của requirement.md (đã cắt khoảng trắng hai đầu; ngoài code fence, gồm cả heading
// `###` con); rỗng khi thiếu mục hoặc mục không có nội dung. Cùng bộ duyệt dòng/fence với lint.
func GoalCheck(requirement string) string {
	lines := strings.Split(strings.ReplaceAll(requirement, "\r\n", "\n"), "\n")
	var out []string
	walkLines(lines, func(i int, section, _ string) {
		if section == secGoal {
			out = append(out, strings.TrimRight(lines[i], " \t\r"))
		}
	}, nil)
	return strings.TrimSpace(strings.Join(out, "\n"))
}

var planPhaseRe = regexp.MustCompile(`^\s*[-*+]\s+(?:\[[ xX]\]\s+)?G-(\d+)\s*:\s*(.*\S)\s*$`)

// PlanPhases trả các giai đoạn của plan.md: dòng `- G-N: …` (cho phép `[ ]`/`[x]` trước G-N) dưới `## Đề xuất tách giai đoạn`, ngoài
// code fence, sắp theo N tăng dần; mỗi phần tử có dạng "G-N: …". Không có mục hoặc không có dòng G-N thì nil (plan cũ).
func PlanPhases(plan string) []string {
	type ph struct {
		n    int
		text string
	}
	var phases []ph
	scanLines(plan, func(section, line string) {
		if section != secPhase {
			return
		}
		m := planPhaseRe.FindStringSubmatch(line)
		if m == nil {
			return
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return
		}
		phases = append(phases, ph{n, fmt.Sprintf("G-%d: %s", n, m[2])})
	})
	sort.SliceStable(phases, func(i, j int) bool { return phases[i].n < phases[j].n })
	out := make([]string, len(phases))
	for i, p := range phases {
		out[i] = p.text
	}
	return out
}

// Kết luận cỡ kế hoạch ghi trong `## Cỡ kế hoạch` của plan.md (PlanSizeDecl).
const (
	PlanSizeSmall = "nhỏ"
	PlanSizeLarge = "lớn"
)

var (
	planVerdictRe  = regexp.MustCompile(`(?i)^\s*(?:[-*+]\s+)?\**\s*kết luận\s*\**\s*:\s*\**\s*(nhỏ|lớn)(?:$|[^\p{L}\p{N}_])`)
	planFileLineRe = regexp.MustCompile(`^\s*[-*+]\s+(?:\[[ xX]\]\s+)?(.*)$`)
	planFileRe     = regexp.MustCompile("`([^`\\s]+)`")
)

// PlanSizeDecl đọc mục `## Cỡ kế hoạch` của plan.md (ngoài code fence): kết luận ("nhỏ" | "lớn" | "" khi thiếu mục hay không có dòng
// `Kết luận: nhỏ|lớn`) và danh sách tệp sẽ sửa (mỗi dòng bullet tính MỌI đường dẫn trong backtick, một dòng nhiều tệp cũng đếm đủ; bỏ trùng, giữ thứ tự).
// Có cả "nhỏ" lẫn "lớn" thì là "lớn": cổng cỡ kế hoạch chỉ được làm kế hoạch lớn hơn, không bao giờ nhỏ đi.
func PlanSizeDecl(plan string) (verdict string, files []string) {
	seen := map[string]bool{}
	scanLines(plan, func(section, line string) {
		if section != secSize {
			return
		}
		if m := planVerdictRe.FindStringSubmatch(line); m != nil {
			v := strings.ToLower(m[1])
			if verdict == "" || v == PlanSizeLarge {
				verdict = v
			}
			return
		}
		if lm := planFileLineRe.FindStringSubmatch(line); lm != nil {
			for _, m := range planFileRe.FindAllStringSubmatch(lm[1], -1) {
				if !seen[m[1]] {
					seen[m[1]] = true
					files = append(files, m[1])
				}
			}
		}
	})
	return verdict, files
}

// PlanSurfaces đếm số nhóm của `## Bề mặt bị ảnh hưởng` trong plan.md: dòng bullet ở cấp ngoài cùng (ngoài code fence); bullet thụt
// vào là chi tiết của nhóm trước, không tính. 0 khi thiếu mục.
func PlanSurfaces(plan string) int {
	n := 0
	scanLines(plan, func(section, line string) {
		if section != secSurface {
			return
		}
		if strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "  ") {
			return
		}
		if t := strings.TrimSpace(line); len(t) > 1 && strings.ContainsRune("-*+", rune(t[0])) && (t[1] == ' ' || t[1] == '\t') {
			n++
		}
	})
	return n
}

// PlanSlice là một nhóm của `## Bề mặt bị ảnh hưởng` có nêu tiêu chí: một lát mỏng của b-do chạy kế hoạch lớn.
type PlanSlice struct {
	// Text là nguyên văn dòng nhóm (không dấu bullet, bỏ khoảng trắng hai đầu).
	Text string
	// ACs là các số N của AC-N nêu trong dòng, theo thứ tự xuất hiện, bỏ trùng.
	ACs []int
}

var planACRe = regexp.MustCompile(`\bAC-(\d+)\b`)

// PlanSlices trả các nhóm của `## Bề mặt bị ảnh hưởng` có nêu ít nhất một AC-N, theo thứ tự dòng (cùng luật nhóm với PlanSurfaces: bullet
// cấp ngoài cùng, ngoài code fence). Nhóm không nêu AC (dòng "Ngoài phạm vi", ghi chú) không phải lát. Không có mục thì nil.
func PlanSlices(plan string) []PlanSlice {
	var out []PlanSlice
	scanLines(plan, func(section, line string) {
		if section != secSurface || strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "  ") {
			return
		}
		t := strings.TrimSpace(line)
		if len(t) < 2 || !strings.ContainsRune("-*+", rune(t[0])) || (t[1] != ' ' && t[1] != '\t') {
			return
		}
		text := strings.TrimSpace(t[1:])
		var acs []int
		for _, m := range planACRe.FindAllStringSubmatch(text, -1) {
			n, err := strconv.Atoi(m[1])
			if err == nil && !slices.Contains(acs, n) {
				acs = append(acs, n)
			}
		}
		if len(acs) > 0 {
			out = append(out, PlanSlice{Text: text, ACs: acs})
		}
	})
	return out
}

func lintRequirement(b *reportBuilder, text string) []acItem {
	var acs []acItem
	seenAC, seenQ := map[int]bool{}, map[int]bool{}
	unclosed := scanLines(text, func(section, line string) {
		if m := lintDeepHeadRe.FindStringSubmatch(line); m != nil {
			if k := sectionKind(m[1]); k == secAC || k == secQ {
				b.err("misplaced_section", artReq, "heading %q phải là `## ` (cấp 2); heading `###` trở xuống nằm trong mục trước nên Bint không đọc được mục này", strings.TrimSpace(line))
			}
			return
		}
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
			if hasPlaceholder(content) {
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
				b.err("malformed_question", artReq, "dòng %q sai khuôn; dùng `- [ ] Q-1: <câu hỏi>` (hoặc `- [x] Q-1: <câu hỏi> — Trả lời: <nội dung>`); dòng sai khuôn không được tính nên không chặn được b-feat", content)
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
	if unclosed {
		b.err("unclosed_code_fence", artReq, "có code fence (``` hoặc ~~~) không đóng: mọi dòng phía sau bị bỏ qua, gồm cả tiêu chí và câu hỏi mở")
	}
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
	unclosed := scanLines(text, func(_ string, line string) {
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
		if hasPlaceholder(content) {
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
	if unclosed {
		b.err("unclosed_code_fence", artTasks, "có code fence (``` hoặc ~~~) không đóng: mọi dòng phía sau bị bỏ qua")
	}
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
	return lintPlanning(t.Ticket, docs, bad, strict, t.State.PlanningContractVersion, t.State.GoalTest.Paths())
}

const (
	secBaseline    = "hiện trạng"
	secAssumptions = "giả định thay người dùng"
)

var (
	lintUnmeasurableRe = regexp.MustCompile(`(?i)^\s*(?:[-*+]\s+)?\**\s*không đo được\s*\**\s*:\s*(.*\S)`)
	lintOutOfScopeRe   = regexp.MustCompile(`(?i)ngoài phạm vi`)
)

// hasMeasure: dòng có số đo thật: chữ số nằm ngoài code span (`TestGoal2`, `internal/v2/x.go` không tính).
func hasMeasure(line string) bool {
	return strings.ContainsAny(stripCodeSpans(line), "0123456789")
}

// lintBaseline: requirement.md phải có `## Hiện trạng` chứa số đo (chữ số ngoài code span) hoặc dòng `Không đo được: <lý do>`,
// và ít nhất một AC có số đo. Trả true khi mục chỉ đạt nhờ lối thoát `Không đo được:` (nơi gọi phát cảnh báo baseline_exempt).
func lintBaseline(b *reportBuilder, req string, acs []acItem) (exempt bool) {
	measured, unmeasurable, has := false, false, false
	scanLines(req, func(section, line string) {
		if section != secBaseline {
			return
		}
		if strings.TrimSpace(line) != "" {
			has = true
		}
		switch {
		case lintUnmeasurableRe.MatchString(line):
			unmeasurable = true
		case hasMeasure(line):
			measured = true
		}
	})
	if !measured && !unmeasurable {
		why := "thiếu mục `## Hiện trạng`"
		if has {
			why = "mục `## Hiện trạng` không có số đo nào (số tệp, byte, dung lượng...; tên trong dấu backtick không tính)"
		}
		b.err("baseline_missing", artReq, "%s; đọc baseline.md của tác vụ, ghi số đo hiện trạng, hoặc một dòng `Không đo được: <lý do>`", why)
	}
	exempt = !measured && unmeasurable
	if len(acs) == 0 {
		return exempt
	}
	for _, a := range acs {
		if hasMeasure(a.Text) {
			return exempt
		}
	}
	b.err("ac_without_measure", artReq, "không dòng AC nào có số đo mục tiêu người dùng; ít nhất một AC phải nêu một con số kiểm được")
	return exempt
}

// lintPlanV2: bề mặt ảnh hưởng ánh xạ vào AC, có giả định, và tệp nêu trong `## Cỡ kế hoạch` nằm trong phạm vi phiếu.
func lintPlanV2(b *reportBuilder, ticket Ticket, plan string, acs []acItem, frozen []string) {
	known := map[int]bool{}
	for _, a := range acs {
		known[a.N] = true
	}
	groups := 0
	assumptions := false
	scanLines(plan, func(section, line string) {
		switch section {
		case secAssumptions:
			if strings.TrimSpace(line) != "" {
				assumptions = true
			}
		case secSurface:
			if strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "  ") {
				return
			}
			t := strings.TrimSpace(line)
			if len(t) < 2 || !strings.ContainsRune("-*+", rune(t[0])) || (t[1] != ' ' && t[1] != '\t') {
				return
			}
			groups++
			text := strings.TrimSpace(t[1:])
			refs := planACRe.FindAllStringSubmatch(text, -1)
			bad := len(refs) == 0 && !lintOutOfScopeRe.MatchString(text)
			for _, m := range refs {
				if n, err := strconv.Atoi(m[1]); err != nil || !known[n] {
					bad = true
				}
			}
			if bad {
				b.err("surface_unmapped", artPlan, "nhóm bề mặt %q phải nêu AC-N có thật trong requirement.md hoặc ghi `ngoài phạm vi`", text)
			}
		}
	})
	if groups == 0 {
		b.err("surface_missing", artPlan, "thiếu mục `## Bề mặt bị ảnh hưởng` (hoặc không có nhóm nào); liệt kê mọi nơi mục tiêu chạm tới")
	}
	if !assumptions {
		b.err("assumptions_missing", artPlan, "thiếu mục `## Giả định thay người dùng` (hoặc mục rỗng); ghi các giả định, hoặc `Không có`")
	}
	if sl, gl := PlanStrategy(plan); sl == StrategyMissing || gl == StrategyMissing {
		b.err("strategy_missing", artPlan, "thiếu mục `## Chiến lược thực thi` hoặc thiếu/sai một dòng `Chia lát: có|không — <lý do>` và "+
			"`Kiểm tra mục tiêu độc lập (b-goal): có|không — <lý do>` (mỗi dòng cần giá trị có|không và lý do)")
	}
	_, files := PlanSizeDecl(plan)
	for _, f := range files {
		rel := strings.TrimPrefix(strings.ReplaceAll(f, "\\", "/"), "./")
		if slices.ContainsFunc(frozen, func(p string) bool { return strings.TrimPrefix(strings.ReplaceAll(p, "\\", "/"), "./") == rel }) {
			continue // kiểm tra mục tiêu do b-goal viết và đóng băng: nằm ngoài scope.allow là bình thường
		}
		denied := pathglob.MatchAny(ticket.Scope.Deny, rel)
		if denied || (len(ticket.Scope.Allow) > 0 && !pathglob.MatchAny(ticket.Scope.Allow, rel)) {
			b.err("plan_file_out_of_scope", artPlan, "tệp %q trong `## Cỡ kế hoạch` nằm ngoài scope.allow hoặc khớp scope.deny của phiếu", f)
		}
	}
}

// AnsweredQuestions trả nguyên văn (đã cắt khoảng trắng hai đầu) các dòng câu hỏi đã trả lời của requirement.md:
// `- [x] Q-N: … — Trả lời: …` dưới mục câu hỏi mở, ngoài code fence, có nội dung trả lời. Dùng chung bộ duyệt dòng/fence với lint
// nên hai nơi hiểu cùng một cấu trúc tệp. Câu chưa trả lời (chưa [x] hoặc [x] không có nội dung) bị bỏ.
func AnsweredQuestions(requirement string) []string {
	var out []string
	scanLines(requirement, func(section, line string) {
		if section != secQ {
			return
		}
		cb := lintCheckboxRe.FindStringSubmatch(line)
		if cb == nil || cb[1] == " " {
			return
		}
		if lintQRe.FindStringSubmatch(cb[2]) == nil {
			return
		}
		am := lintAnswerRe.FindStringSubmatch(" " + cb[2])
		if am == nil || strings.TrimSpace(am[1]) == "" {
			return
		}
		out = append(out, strings.TrimSpace(line))
	})
	return out
}
