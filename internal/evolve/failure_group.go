package evolve

import (
	"fmt"
	"sort"

	"bint/internal/knowledge"
)

// Nhóm thất bại (E1, E4): theo vùng mã sinh bài học RIÊNG; theo dấu hiệu hành vi (qua
// nhiều vùng) sinh bài học CHUNG. Một lượt có thể thuộc một vùng và nhiều dấu hiệu.

// failureAreaPrefix và failureSignalPrefix là tiền tố khoá cơ hội failure_lesson.
const (
	failureAreaPrefix   = "lesson:area:"
	failureSignalPrefix = "lesson:signal:"
)

// failureLessonOpps gom các đơn vị thất bại thành cơ hội failure_lesson. Một nhóm cần ít nhất
// min đơn vị phân biệt (tác vụ đánh giá hoặc tác vụ/lượt thật; nhiều lượt của cùng một tác
// vụ chỉ tính một) và ít nhất e lượt chưa dùng cho đề xuất trước cùng nhóm. used/pending
// là trạng thái đề xuất đã có (như Scan).
func failureLessonOpps(fails []Failure, min, e int, used map[string]map[string]bool, pending map[string]string) []Opportunity {
	type bucket struct {
		kind, name string
		fails      []Failure
	}
	buckets := map[string]*bucket{}
	put := func(key, kind, name string, f Failure) {
		b := buckets[key]
		if b == nil {
			b = &bucket{kind: kind, name: name}
			buckets[key] = b
		}
		b.fails = append(b.fails, f)
	}
	for _, f := range fails {
		// Tệp ở gốc repository (vùng ".") không có phạm vi thư mục nên không sinh bài học riêng.
		if f.Area != "" && f.Area != "." {
			put(failureAreaPrefix+f.Area, "vùng", f.Area, f)
		}
		for _, s := range f.Signals {
			put(failureSignalPrefix+s, "dấu hiệu", s, f)
		}
	}
	keys := sortedKeys(buckets)
	var out []Opportunity
	for _, key := range keys {
		b := buckets[key]
		ids := map[string]bool{}
		units := map[string]bool{}
		for _, f := range b.fails {
			ids[f.ID] = true
			units[f.Unit] = true
		}
		supporting := sortedKeys(ids)
		newEv := 0
		for _, id := range supporting {
			if !used[key][id] {
				newEv++
			}
		}
		o := Opportunity{Key: key, Kind: KindFailureLesson, Action: ActionCreate, Generator: GenAgent,
			Group: b.name, SupportingRuns: supporting, NewEvidence: newEv}
		o.Summary = fmt.Sprintf("%d tác vụ thất bại (%d lượt) cùng %s %s", len(units), len(supporting), b.kind, b.name)
		switch {
		case len(units) < min && len(supporting) >= min:
			o.Blocked = fmt.Sprintf("%d lượt thất bại nhưng chỉ %d tác vụ: các lượt của cùng một tác vụ không tính là nhiều đơn vị (cần ≥ %d)",
				len(supporting), len(units), min)
		case len(units) < min:
			o.Blocked = fmt.Sprintf("chỉ %d tác vụ thất bại (cần ≥ %d, evolution.trigger.min_similar_runs)", len(units), min)
		case newEv < e:
			o.Blocked = fmt.Sprintf("chỉ %d lượt mới so với đề xuất trước cùng nhóm (cần ≥ %d, evolution.trigger.min_new_evidence)", newEv, e)
		default:
			o.Ready = true
		}
		if p, ok := pending[key]; ok {
			o.Ready, o.Blocked = false, "đã có đề xuất "+p+" chờ xử lý"
		}
		out = append(out, o)
	}
	return out
}

// failuresByID chọn các đơn vị thất bại có ID trong ids, theo thứ tự ID.
func failuresByID(fails []Failure, ids []string) []Failure {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var out []Failure
	for _, f := range fails {
		if want[f.ID] {
			out = append(out, f)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// lessonAreaFailures: đơn vị thất bại nằm trong phạm vi glob (dùng cho kiểm tra "riêng").
func lessonAreaFailures(fails []Failure, glob string) []Failure {
	var out []Failure
	for _, f := range fails {
		if f.Area != "" && f.Area != "." && scopeMatches(glob, f.Area+"/_") {
			out = append(out, f)
		}
	}
	return out
}

// areasOf: các vùng mã khác nhau của một tập đơn vị thất bại.
func areasOf(fails []Failure) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range fails {
		if f.Area != "" && !seen[f.Area] {
			seen[f.Area] = true
			out = append(out, f.Area)
		}
	}
	sort.Strings(out)
	return out
}

// scopeMatches: đường dẫn rel khớp mẫu glob (cùng ngữ nghĩa phạm vi tri thức).
func scopeMatches(glob, rel string) bool {
	return knowledge.Scope{Paths: []string{glob}}.MatchScope(rel)
}
