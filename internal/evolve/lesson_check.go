package evolve

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"bint/internal/eval"
	"bint/internal/knowledge"
)

// Mã vi phạm của ứng viên bài học (kiểm tra xác định E3). Không gọi mô hình.
const (
	LessonLeak     = "leak"     // chứa định danh/mã của lời giải của tác vụ làm bằng chứng
	LessonGeneral  = "general"  // bài học chung: bằng chứng chưa đủ ≥ 2 vùng mã
	LessonSpecific = "specific" // bài học riêng: chưa đủ ≥ 2 lượt thất bại trong vùng
	LessonForm     = "form"     // thiếu trigger/mistake/instead, hoặc quá lượng từ
	LessonOrigin   = "origin"   // nguồn gốc: không có lượt, hoặc lượt không thuộc validation
	LessonScope    = "scope"    // phạm vi không phải global hoặc domain:<glob> hợp lệ
)

// lessonMaxWords là tổng số từ tối đa của trigger + mistake + instead (~80 từ).
const lessonMaxWords = 80

// lessonDupJaccard: ngưỡng trùng theo tình huống (cùng ngưỡng với trùng tri thức).
const lessonDupJaccard = 0.6

// LessonFinding là một vi phạm của ứng viên bài học.
type LessonFinding struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

// lessonInput là ứng viên cùng bối cảnh để kiểm tra xác định.
type lessonInput struct {
	Cand lessonCandidate
	// Evidence: lượt thất bại làm bằng chứng (đã chọn trong nhóm).
	Evidence []Failure
	// Leaks: đầu vào kiểm rò rỉ của các tác vụ đánh giá làm bằng chứng (diff lời giải).
	Leaks []eval.LeakInput
	// Existing: tri thức hiện có (tìm mục bài học trùng).
	Existing []*knowledge.Item
}

// LessonVerdict là kết quả kiểm tra: Findings rỗng = đạt. Duplicate khác rỗng = trùng với
// mục bài học sẵn có, nên đề xuất gộp vào mục đó thay vì tạo mới (E3.3).
type LessonVerdict struct {
	Findings  []LessonFinding `json:"findings,omitempty"`
	Duplicate string          `json:"duplicate,omitempty"`
}

// OK cho biết ứng viên không vi phạm điều nào.
func (v LessonVerdict) OK() bool { return len(v.Findings) == 0 }

// CheckLesson kiểm tra xác định một ứng viên bài học theo E3: rò rỉ, tổng quát/riêng,
// trùng, hình thức, nguồn gốc. Rò rỉ được kiểm trước và fail đóng: không kiểm được thì
// nơi gọi phải loại ứng viên, không bỏ qua.
func CheckLesson(in lessonInput) LessonVerdict {
	var v LessonVerdict
	add := func(code, format string, a ...any) {
		v.Findings = append(v.Findings, LessonFinding{Code: code, Detail: fmt.Sprintf(format, a...)})
	}
	c := in.Cand
	trigger := strings.TrimSpace(c.Trigger)
	mistake := strings.TrimSpace(c.Mistake)
	instead := strings.TrimSpace(c.Instead)

	// Hình thức: đủ ba trường và không quá lượng từ.
	if trigger == "" || mistake == "" || instead == "" {
		add(LessonForm, "thiếu trigger, mistake hoặc instead")
	}
	if words := len(strings.Fields(trigger + " " + mistake + " " + instead)); words > lessonMaxWords {
		add(LessonForm, "%d từ, quá %d", words, lessonMaxWords)
	}

	// Phạm vi: global, hoặc domain:<glob> với glob hợp lệ.
	global, glob, ok := parseLessonScope(c.Scope)
	switch {
	case !ok:
		add(LessonScope, "scope %q phải là global hoặc domain:<glob>", c.Scope)
	case !global:
		if err := knowledge.ValidRelPath(strings.TrimSuffix(glob, "/")); err != nil {
			add(LessonScope, "domain %q: %v", glob, err)
		} else if _, err := path.Match(glob, ""); err != nil {
			add(LessonScope, "domain %q không phải mẫu glob hợp lệ", glob)
		} else if why := globOutsideAreas(glob, areasOf(in.Evidence)); why != "" {
			add(LessonScope, "domain %q %s", glob, why)
		}
	}

	// Nguồn gốc: có lượt làm bằng chứng, và mọi lượt eval thuộc tập validation (tập test
	// không bao giờ là đầu vào của reflector).
	if len(in.Evidence) == 0 {
		add(LessonOrigin, "không có lượt bằng chứng")
	}
	for _, f := range in.Evidence {
		if f.Source == "eval" && f.Split != eval.SplitValidation {
			add(LessonOrigin, "lượt %s thuộc tập %q, không phải validation", f.ID, f.Split)
		}
	}

	// Tổng quát (chung: ≥ 2 vùng mã) hoặc riêng (≥ 2 lượt thất bại trong đúng vùng).
	switch {
	case global:
		if n := len(areasOf(in.Evidence)); n < 2 {
			add(LessonGeneral, "bằng chứng từ %d vùng mã (cần ≥ 2 để là bài học chung)", n)
		}
	case ok:
		if n := len(lessonAreaFailures(in.Evidence, glob)); n < 2 {
			add(LessonSpecific, "%d lượt thất bại trong phạm vi %s (cần ≥ 2 để là bài học riêng)", n, glob)
		}
	}

	// Rò rỉ: nội dung không được chứa định danh/mã của lời giải (cùng cơ chế CheckLeak).
	if msg := textLeaks(trigger+"\n"+mistake+"\n"+instead, in.Leaks); msg != "" {
		add(LessonLeak, "%s", msg)
	}

	// Trùng: chỉ xét khi chưa có vi phạm. Trùng → gộp vào mục đã có.
	if v.OK() {
		scope := knowledge.Scope{}
		if ok && !global {
			scope.Paths = []string{glob}
		}
		for _, it := range in.Existing {
			if !it.LessonFromRuns() || !it.Active() || !scopesOverlap(scope, it.Scope) {
				continue
			}
			if jaccard(tokens(trigger), tokens(it.Title)) >= lessonDupJaccard {
				v.Duplicate = it.ID
				break
			}
		}
	}
	return v
}

// textLeaks kiểm một đoạn văn bản (bài học, thành phần quy trình) với diff lời giải của các tác vụ
// làm bằng chứng; trả về mô tả vi phạm đầu tiên ("" = không rò rỉ). Lõi dùng chung của CheckLesson
// và kiểm tra đề xuất quy trình (H3): chứa dòng mã của lời giải (CheckLeak), định danh của phần
// thêm mới (CheckLeak chỉ cảnh báo định danh mới; với văn bản do agent soạn thì đó là đáp án), hoặc
// chuỗi literal dài của phần thêm mới.
func textLeaks(text string, leaks []eval.LeakInput) string {
	for _, leak := range leaks {
		if r := eval.CheckLeak(text, leak); r.Status == "fail" {
			detail := "định danh của lời giải"
			if len(r.Findings) > 0 {
				detail = r.Findings[0].Detail
			}
			return fmt.Sprintf("nội dung chứa mã của lời giải (%s)", detail)
		}
		if id, ok := identInText(text, solutionTerms(leak)); ok {
			return fmt.Sprintf("nhắc tới định danh của lời giải %q", id)
		}
		if lit, ok := literalInText(text, eval.SolutionLiterals(leak)); ok {
			return fmt.Sprintf("nhắc tới chuỗi literal của lời giải %q", lit)
		}
	}
	return ""
}

// globOutsideAreas trả về lý do khi glob của bài học riêng không nằm trong vùng mã của nhóm
// bằng chứng ("" = hợp lệ). Glob khớp toàn repo (không có tiền tố thư mục cố định, như **, *,
// *.go) bị từ chối: nó lách yêu cầu "≥ 2 vùng" của bài học chung và giới hạn top-k. Tiền tố cố
// định phải bằng hoặc nằm dưới một vùng của bằng chứng (không rộng hơn vùng).
func globOutsideAreas(glob string, areas []string) string {
	prefix := globDirPrefix(glob)
	if prefix == "." || prefix == "" {
		return "khớp toàn repo; phải nằm trong vùng của nhóm thất bại"
	}
	for _, a := range areas {
		if a != "." && (prefix == a || strings.HasPrefix(prefix, a+"/")) {
			return ""
		}
	}
	return fmt.Sprintf("rộng hơn hoặc ngoài vùng của nhóm thất bại (%s)", strings.Join(areas, ", "))
}

// globDirPrefix trả về tiền tố thư mục cố định của glob, cắt ở ranh giới thư mục: ký tự đại diện
// đứng giữa một tên (vd internal/evolve*/**) khớp cả thư mục anh em (internal/evolve2, …), nên
// tiền tố lùi về thư mục cha (internal). Glob không có ký tự đại diện giữ nguyên.
func globDirPrefix(glob string) string {
	p := glob
	if i := strings.IndexAny(p, "*?["); i >= 0 {
		p = p[:i]
		if !strings.HasSuffix(p, "/") {
			if j := strings.LastIndex(p, "/"); j >= 0 {
				p = p[:j+1]
			} else {
				p = ""
			}
		}
	}
	p = strings.TrimSuffix(p, "/")
	if p == "" {
		return "."
	}
	return p
}

// solutionTerms gộp định danh dạng mã của phần thêm mới với MỌI định danh mới (kể cả tên một
// từ như Reset, Flush) trừ tên có trong kiểm thử: với bài học, tên hàm một từ cũng là đáp án.
func solutionTerms(leak eval.LeakInput) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range append(eval.SolutionIdentifiers(leak), eval.SolutionNewIdentifiers(leak)...) {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// literalInText trả về chuỗi literal đầu tiên xuất hiện trong text (không phân biệt hoa thường,
// khoảng trắng đã chuẩn hoá).
func literalInText(text string, lits []string) (string, bool) {
	norm := strings.ToLower(strings.Join(strings.Fields(text), " "))
	for _, l := range lits {
		if strings.Contains(norm, strings.ToLower(l)) {
			return l, true
		}
	}
	return "", false
}

// identInText trả về định danh đầu tiên xuất hiện như một từ trong text.
func identInText(text string, ids []string) (string, bool) {
	for _, id := range ids {
		if regexp.MustCompile(`(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(id) + `($|[^A-Za-z0-9_])`).MatchString(text) {
			return id, true
		}
	}
	return "", false
}

// parseLessonScope đọc scope của ứng viên: "global" hoặc "domain:<glob>".
func parseLessonScope(s string) (global bool, glob string, ok bool) {
	s = strings.TrimSpace(s)
	if s == "global" {
		return true, "", true
	}
	if g, found := strings.CutPrefix(s, "domain:"); found {
		g = strings.TrimSpace(g)
		return false, g, g != ""
	}
	return false, "", false
}

// describeFindings ghép các vi phạm thành một dòng (cho thông báo và danh sách bị loại).
func describeFindings(fs []LessonFinding) string {
	var parts []string
	for _, f := range fs {
		parts = append(parts, f.Code+": "+f.Detail)
	}
	return strings.Join(parts, "; ")
}
