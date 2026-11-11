package ctxengine

import (
	"context"
	"strings"
	"testing"
	"time"

	"bint/internal/knowledge"
)

// Kiểm thử E5 của kế hoạch E: top-k bài học chung, bài học riêng theo phạm vi, ngân sách riêng.

func lessonItem(id, title string, paths []string, status string) *knowledge.Item {
	return &knowledge.Item{ID: id, Type: knowledge.TypeLesson, Status: status, Title: title, Body: title + ".",
		Scope:  knowledge.Scope{Paths: paths},
		Origin: knowledge.Origin{Kind: knowledge.OriginAgent, Provenance: knowledge.ProvenanceFailureLesson, Runs: []string{"ea_1"}}}
}

func ids(es []Entry) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.ID)
	}
	return out
}

func TestTaskLessonsGlobalTopK(t *testing.T) {
	items := []*knowledge.Item{
		lessonItem("L01", "Bỏ qua chạy test", nil, knowledge.StatusVerified),
		lessonItem("L02", "Đọc tài liệu trước", nil, knowledge.StatusVerified),
		lessonItem("L03", "Chạy test của vùng đã sửa", nil, knowledge.StatusCandidate),
		lessonItem("L04", "Sửa lỗi nhỏ gọn", nil, knowledge.StatusVerified),
		lessonItem("L05", "Chạy test trước khi báo xong", nil, knowledge.StatusVerified),
	}
	e := &Engine{Items: items, MaxTokens: 6000, LessonGlobalK: 3, LessonMaxTokens: 600}
	p, err := e.ForTask(context.Background(), "sửa lỗi và chạy test", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Luôn chèn đúng top-k chung; xếp theo số từ khoá trùng với đề bài (có "test" trước).
	if got := ids(p.Lessons); len(got) != 3 || got[0] != "L05" && got[0] != "L03" {
		t.Fatalf("top-3 bài học chung = %v", got)
	}
	if len(p.Entries) != 0 {
		t.Fatalf("bài học không được đi vào phần tri thức thường: %v", ids(p.Entries))
	}
	if !strings.Contains(strings.Join(omittedReasons(p.Omitted), ";"), "ngoài top-k bài học chung") {
		t.Fatalf("bài học chung vượt top-k phải ghi lý do: %+v", p.Omitted)
	}
	if got := len(p.IDs()); got != 3 {
		t.Fatalf("IDs() phải gồm bài học đã chèn: %d", got)
	}
}

func omittedReasons(os []Omission) []string {
	var out []string
	for _, o := range os {
		out = append(out, o.Reason)
	}
	return out
}

func TestTaskLessonsScopedOnlyWhenMatched(t *testing.T) {
	items := []*knowledge.Item{
		lessonItem("L10", "Khi sửa api nhớ đăng ký route", []string{"internal/api/**"}, knowledge.StatusVerified),
		lessonItem("L11", "Khi sửa core nhớ gọi Reset", []string{"internal/core/**"}, knowledge.StatusVerified),
	}
	e := &Engine{Items: items, MaxTokens: 6000, LessonGlobalK: 3, LessonMaxTokens: 600}
	p, err := e.ForTask(context.Background(), "sửa handler", []string{"internal/api/h.go"})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(p.Lessons); len(got) != 1 || got[0] != "L10" {
		t.Fatalf("bài học riêng chỉ khi khớp: %v", got)
	}
	if !strings.HasPrefix(p.Lessons[0].Reasons[0], "path:") {
		t.Fatalf("lý do chọn phải là khớp đường dẫn: %v", p.Lessons[0].Reasons)
	}
}

func TestPathLessonsExcludeSessionAndNoGlobal(t *testing.T) {
	items := []*knowledge.Item{
		lessonItem("L20", "Khi sửa api nhớ đăng ký route", []string{"internal/api/**"}, knowledge.StatusVerified),
		lessonItem("L21", "Bài học chung không dành cho ForPaths", nil, knowledge.StatusVerified),
	}
	e := &Engine{Items: items, MaxTokens: 6000, PathMaxItems: 5, LessonMaxTokens: 600}
	p := e.ForPaths([]string{"internal/api/h.go"}, map[string]bool{"L20": true})
	if len(p.Lessons) != 0 {
		t.Fatalf("bài học đã chèn trong phiên phải bị bỏ: %v", ids(p.Lessons))
	}
	p = e.ForPaths([]string{"internal/api/h.go"}, nil)
	if got := ids(p.Lessons); len(got) != 1 || got[0] != "L20" {
		t.Fatalf("ForPaths chỉ chọn bài học riêng khớp: %v", got)
	}
}

func TestLessonBudgetDoesNotDropKnowledge(t *testing.T) {
	rule := &knowledge.Item{ID: "R1", Type: knowledge.TypeRule, Status: knowledge.StatusVerified, Title: "Quy tắc api",
		Body: "Luôn đăng ký route.", Scope: knowledge.Scope{Paths: []string{"internal/api/**"}},
		Origin: knowledge.Origin{Kind: knowledge.OriginHuman}}
	items := []*knowledge.Item{rule, lessonItem("L30", "Khi sửa api nhớ đăng ký route", []string{"internal/api/**"}, knowledge.StatusVerified)}
	// Ngân sách bài học quá nhỏ: bài học bị bỏ, nhưng tri thức vẫn được chèn (ngân sách tách riêng).
	e := &Engine{Items: items, MaxTokens: 6000, LessonMaxTokens: 5}
	p, err := e.ForTask(context.Background(), "sửa api", []string{"internal/api/h.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Lessons) != 0 || !strings.Contains(strings.Join(omittedReasons(p.Omitted), ";"), "vượt ngân sách bài học") {
		t.Fatalf("bài học phải bị bỏ do ngân sách riêng: lessons=%v omitted=%+v", ids(p.Lessons), p.Omitted)
	}
	if got := ids(p.Entries); len(got) != 1 || got[0] != "R1" {
		t.Fatalf("ngân sách bài học không được làm rơi tri thức: %v", got)
	}
	// Tắt phần bài học (0): không chèn bài học nào.
	off := &Engine{Items: items, MaxTokens: 6000}
	if p, _ := off.ForTask(context.Background(), "sửa api", []string{"internal/api/h.go"}); len(p.Lessons) != 0 {
		t.Fatalf("LessonMaxTokens = 0 phải tắt bài học: %v", ids(p.Lessons))
	}
}

func TestRenderLessonsWithoutKnowledge(t *testing.T) {
	items := []*knowledge.Item{lessonItem("L40", "Chạy test trước khi kết thúc", nil, knowledge.StatusVerified)}
	e := &Engine{Items: items, MaxTokens: 6000, LessonGlobalK: 3, LessonMaxTokens: 600}
	p, err := e.ForTask(context.Background(), "chạy test", nil)
	if err != nil {
		t.Fatal(err)
	}
	out := p.Render()
	if out == "" || !strings.Contains(out, lessonsTitle) || !strings.Contains(out, "[k:L40]") {
		t.Fatalf("gói chỉ có bài học phải được chèn kèm tiêu đề phần bài học:\n%s", out)
	}
}

// Bài học rút từ nhiều tác vụ không bao giờ được chèn khi chấm lại chính một trong số đó (mục 12.3).
func TestLessonFromSeveralEvalCasesIsExcludedForEach(t *testing.T) {
	l := lessonItem("L50", "Chạy test trước khi kết thúc", nil, knowledge.StatusVerified)
	l.Origin.EvalCases = []string{"c-val-1", "c-val-3"}
	items := []*knowledge.Item{l}
	e := &Engine{Items: items, MaxTokens: 6000, LessonGlobalK: 3, LessonMaxTokens: 600, ExcludeEvalCase: "c-val-3"}
	if p, _ := e.ForTask(context.Background(), "chạy test", nil); len(p.Lessons) != 0 {
		t.Fatalf("bài học rút từ c-val-3 không được chèn khi chấm c-val-3: %v", ids(p.Lessons))
	}
	e.ExcludeEvalCase = "c-val-2"
	if p, _ := e.ForTask(context.Background(), "chạy test", nil); len(p.Lessons) != 1 {
		t.Fatalf("tác vụ không phải nguồn thì bài học vẫn được chèn: %v", ids(p.Lessons))
	}
}

// Lesson do người viết (không phải failure_lesson) đi luồng tri thức thường: truy xuất theo
// từ khoá, không bị chuyển sang phần bài học, không phụ thuộc LessonMaxTokens.
func TestHumanLessonStaysInKnowledgeFlow(t *testing.T) {
	human := &knowledge.Item{ID: "H01", Type: knowledge.TypeLesson, Status: knowledge.StatusVerified,
		Title: "Chạy migration trước khi sửa schema", Body: "Chạy migration trước khi sửa schema.",
		Scope:  knowledge.Scope{Tags: []string{"migration"}},
		Origin: knowledge.Origin{Kind: knowledge.OriginHuman}}
	auto := lessonItem("L20", "Chạy test của vùng đã sửa", nil, knowledge.StatusVerified)
	e := &Engine{Items: []*knowledge.Item{human, auto}, MaxTokens: 6000, LessonGlobalK: 3, LessonMaxTokens: 0}
	p, err := e.ForTask(context.Background(), "sửa schema cần chạy migration", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(p.Entries); len(got) != 1 || got[0] != "H01" {
		t.Fatalf("lesson thường phải ở phần tri thức thường: entries=%v lessons=%v", got, ids(p.Lessons))
	}
	if len(p.Lessons) != 0 {
		t.Fatalf("LessonMaxTokens=0 chỉ tắt bài học từ thất bại: %v", ids(p.Lessons))
	}
}

// ContextEvent ghi cả ID bài học (nhãn "lesson") để context_usages đếm được lượt chèn.
func TestContextEventRecordsLessons(t *testing.T) {
	p := Pack{Mode: "task", Entries: []Entry{{ID: "K1", Label: LabelVerified}}, Lessons: []Entry{{ID: "L1", Label: LabelReference}}}
	ev := ContextEvent("claude", "s", "/w", ChannelHookTask, "k", p, time.Now())
	ids, _ := ev.Payload["ids"].([]any)
	labels, _ := ev.Payload["labels"].(map[string]any)
	if len(ids) != 2 || ids[1] != "L1" || labels["L1"] != LabelLesson || labels["K1"] != LabelVerified {
		t.Fatalf("payload = %+v", ev.Payload)
	}
}
