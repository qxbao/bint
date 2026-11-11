package ctxengine

import (
	"sort"
	"strings"

	"bint/internal/kindex"
	"bint/internal/knowledge"
)

// Bài học từ thất bại (kế hoạch E5) được chèn trong phần riêng của gói ngữ cảnh, với ngân sách
// token riêng (LessonMaxTokens), để không chiếm chỗ của tri thức thường:
//   - ForTask: luôn chèn top-k bài học CHUNG (phạm vi toàn repo, xếp theo số từ khoá trùng với
//     đề bài; LessonGlobalK). Bài học RIÊNG chỉ được chèn khi phạm vi khớp đường dẫn, hoặc miền
//     và thẻ được nêu trong đề bài.
//   - ForPaths: chỉ bài học RIÊNG khớp đường dẫn, bỏ mục đã chèn trong phiên.
// Không mở SQLite, không gọi mô hình, không trả lỗi: chỉ đọc Items đã nạp.

// LabelLesson là nhãn ghi vào sự kiện context.retrieved cho mục của phần bài học.
const LabelLesson = "lesson"

// isLesson cho biết mục là bài học từ thất bại do Bint sinh (provenance failure_lesson, có
// lượt nguồn): chỉ loại này đi qua phần bài học. Mục type lesson do người viết hoặc sinh từ
// review đi luồng tri thức thường.
func isLesson(it *knowledge.Item) bool { return it.LessonFromRuns() }

// rankLessons sắp xếp bài học: điểm cao trước; cùng điểm thì đã xác thực trước ứng viên;
// rồi mục mới hơn (ID ULID tăng theo thời gian).
func rankLessons(es []Entry) {
	sort.SliceStable(es, func(i, j int) bool {
		if es[i].Score != es[j].Score {
			return es[i].Score > es[j].Score
		}
		if ti, tj := labelTier[es[i].Label], labelTier[es[j].Label]; ti != tj {
			return ti < tj
		}
		return es[i].ID > es[j].ID
	})
}

// taskLessons chọn bài học cho ForTask. terms là từ khoá của đề bài; paths là khu vực mã đã
// suy ra. Bài học chung vượt top-k bị bỏ với lý do, không im lặng.
func (e *Engine) taskLessons(terms map[string]bool, paths []string, omitted []Omission) ([]Entry, []Omission) {
	if e.LessonMaxTokens <= 0 {
		return nil, omitted
	}
	var global, scoped []Entry
	for _, it := range e.Items {
		if !isLesson(it) {
			continue
		}
		if ok, _ := e.eligible(it); !ok {
			continue
		}
		if it.Scope.Global() {
			hits := 0
			for _, t := range kindex.Terms(it.Title + " " + it.Body) {
				if terms[t] {
					hits++
				}
			}
			global = append(global, e.entry(it, []string{"lesson:global"}, float64(hits)))
			continue
		}
		var reasons []string
		for _, a := range paths {
			if it.Scope.MatchScope(a) {
				reasons = append(reasons, "path:"+a)
			}
		}
		for _, d := range it.Scope.Domains {
			if terms[strings.ToLower(d)] {
				reasons = append(reasons, "domain:"+d)
			}
		}
		for _, tg := range it.Scope.Tags {
			if terms[strings.ToLower(tg)] {
				reasons = append(reasons, "tag:"+tg)
			}
		}
		if len(reasons) > 0 {
			scoped = append(scoped, e.entry(it, reasons, float64(len(reasons))))
		}
	}
	rankLessons(global)
	rankLessons(scoped)
	k := max(e.LessonGlobalK, 0)
	var cands []Entry
	for i, g := range global {
		if i < k {
			cands = append(cands, g)
		} else {
			omitted = append(omitted, Omission{g.ID, "ngoài top-k bài học chung"})
		}
	}
	cands = append(cands, scoped...)
	return e.packLessons(cands, omitted)
}

// pathLessons chọn bài học RIÊNG khớp các đường dẫn (ForPaths); exclude là mục đã chèn trong
// phiên, bị bỏ kèm lý do.
func (e *Engine) pathLessons(paths []string, exclude map[string]bool, omitted []Omission) ([]Entry, []Omission) {
	if e.LessonMaxTokens <= 0 || len(paths) == 0 {
		return nil, omitted
	}
	var cands []Entry
	for _, it := range e.Items {
		if !isLesson(it) || it.Scope.Global() {
			continue
		}
		if ok, _ := e.eligible(it); !ok {
			continue
		}
		if it.Status == knowledge.StatusCandidate && !e.PathIncludeCandidates {
			continue
		}
		var reasons []string
		for _, rel := range paths {
			if it.Scope.MatchScope(rel) {
				reasons = append(reasons, "path:"+rel)
			}
		}
		if len(reasons) == 0 {
			continue
		}
		if exclude[it.ID] {
			omitted = append(omitted, Omission{it.ID, "đã chèn trước đó trong phiên"})
			continue
		}
		cands = append(cands, e.entry(it, reasons, float64(len(reasons))))
	}
	rankLessons(cands)
	return e.packLessons(cands, omitted)
}

// packLessons giữ bài học trong ngân sách riêng LessonMaxTokens; phần vượt bị bỏ với lý do.
func (e *Engine) packLessons(cands []Entry, omitted []Omission) ([]Entry, []Omission) {
	var out []Entry
	used := 0
	for _, c := range cands {
		cost := estimateTokens(renderEntry(c))
		if used+cost > e.LessonMaxTokens {
			omitted = append(omitted, Omission{c.ID, "vượt ngân sách bài học"})
			continue
		}
		used += cost
		out = append(out, c)
	}
	return out, omitted
}
