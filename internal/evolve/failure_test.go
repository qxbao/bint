package evolve

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"bint/internal/eval"
	"bint/internal/knowledge"
)

// Kiểm thử E1–E3 của kế hoạch E, không tốn token: không gọi mô hình, không chạy eval thật.

func readSample(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "failure", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// sampleFailure dựng lượt thất bại từ log mẫu đúng như LoadFailures làm (không cần DB).
func sampleFailure(t *testing.T, id, unit, area string) Failure {
	t.Helper()
	tr := ParseTrace(readSample(t, "sample/agent.log"), "/RUN/")
	fails, tail := testFailureLines(readSample(t, "sample/test.log"))
	f := Failure{Source: "eval", ID: id, Unit: unit, Case: unit, Split: eval.SplitValidation, Arm: "bint",
		Status: "failed", Area: area, Prompt: "Sửa lỗi Retry để không panic.", Trace: tr, Measured: tr.Available,
		Modified: []string{"internal/api/h.go", "internal/core/x.go"}, TestFails: fails, TestTail: tail,
		Allowed: []string{"internal/core/**"}}
	f.Signals = classifySignals(&f)
	return f
}

func TestParseTraceSample(t *testing.T) {
	tr := ParseTrace(readSample(t, "sample/agent.log"), "/RUN/")
	if !tr.Available {
		t.Fatal("log stream-json phải có vết")
	}
	if want := []string{"internal/core/x.go"}; !reflect.DeepEqual(tr.Reads, want) {
		t.Fatalf("Reads = %v, muốn %v", tr.Reads, want)
	}
	if want := []string{"internal/core/x.go", "internal/api/h.go"}; !reflect.DeepEqual(tr.Writes, want) {
		t.Fatalf("Writes = %v, muốn %v (tiền tố thư mục làm việc phải bị bỏ)", tr.Writes, want)
	}
	if len(tr.Commands) != 3 || tr.Commands[0] != "go build ./..." {
		t.Fatalf("Commands = %v", tr.Commands)
	}
	if legacy := ParseTrace(readSample(t, "legacy/agent.log"), ""); legacy.Available {
		t.Fatal("log kiểu cũ (một đối tượng JSON, không có tool_use) phải ghi là không đo được")
	}
}

func TestClassifySignals(t *testing.T) {
	f := sampleFailure(t, "ea_1", "t1", "internal/core")
	want := []string{SigNoTest, SigEditOutside, SigRepeatCmd}
	if !reflect.DeepEqual(f.Signals, want) {
		t.Fatalf("Signals = %v, muốn %v", f.Signals, want)
	}
	// Đã chạy test: không còn dấu hiệu không chạy test.
	f.Trace.Commands = append(f.Trace.Commands, "go test ./internal/core/...")
	f.Signals = classifySignals(&f)
	if containsStr(f.Signals, SigNoTest) {
		t.Fatalf("đã chạy test mà vẫn báo: %v", f.Signals)
	}
	// Lượt đánh giá không có vết: không kết luận được "không chạy test".
	legacy := Failure{Source: "eval", Measured: false, Status: "failed"}
	if sigs := classifySignals(&legacy); containsStr(sigs, SigNoTest) {
		t.Fatalf("log cũ không có vết mà vẫn báo không chạy test: %v", sigs)
	}
}

func TestBuildDigestSections(t *testing.T) {
	f := sampleFailure(t, "ea_1", "t1", "internal/core")
	d := BuildDigest(f)
	for _, want := range []string{
		"Lượt ea_1 (eval, nhánh bint, trạng thái failed, tác vụ t1, vùng internal/core)",
		"Đề bài: Sửa lỗi Retry để không panic.",
		"Dấu hiệu: không chạy test trước khi kết thúc; sửa tệp ngoài vùng; lặp cùng một lệnh",
		"Lệnh đã chạy (3, gộp lệnh lặp):",
		"- go build ./... ×3",
		"Tệp đọc: internal/core/x.go",
		"Tệp sửa: internal/api/h.go, internal/core/x.go",
		"--- FAIL: TestRetry (0.00s)",
		"nguồn: tập validation",
	} {
		if !strings.Contains(d.Text, want) {
			t.Errorf("bản tóm tắt thiếu %q:\n%s", want, d.Text)
		}
	}
	if d.Tokens > DigestTokens || d.Tokens <= 0 {
		t.Fatalf("token = %d, trần %d", d.Tokens, DigestTokens)
	}
	if again := BuildDigest(f); again.Text != d.Text {
		t.Fatal("cùng đầu vào phải cho cùng bản tóm tắt")
	}
}

func TestBuildDigestLegacyLogAndLongLog(t *testing.T) {
	// Log kiểu cũ: nói rõ là không đo được, không đoán.
	legacy := Failure{Source: "eval", ID: "ea_old", Unit: "t9", Status: "failed", Prompt: "đề bài"}
	d := BuildDigest(legacy)
	if !strings.Contains(d.Text, "Lệnh đã chạy: không đo được (log không có vết công cụ)") {
		t.Fatalf("log cũ phải ghi không đo được:\n%s", d.Text)
	}
	// Log dài: vẫn trong ngân sách, dù có hàng nghìn lệnh và đuôi test dài.
	long := Failure{Source: "eval", ID: "ea_long", Unit: "t2", Status: "failed", Measured: true, Prompt: strings.Repeat("đề bài dài ", 500)}
	for i := 0; i < 3000; i++ {
		long.Trace.Commands = append(long.Trace.Commands, fmt.Sprintf("grep -rn symbol_%d internal/ | head -5 # mục %d", i, i))
		long.Modified = append(long.Modified, fmt.Sprintf("internal/mod%d/file%d.go", i%40, i))
	}
	for i := 0; i < 500; i++ {
		long.TestTail = append(long.TestTail, strings.Repeat("dòng lỗi rất dài ", 20))
	}
	long.TestFails = []string{"--- FAIL: TestA", "--- FAIL: TestB"}
	d = BuildDigest(long)
	if d.Tokens > DigestTokens {
		t.Fatalf("log dài: %d token, vượt trần %d", d.Tokens, DigestTokens)
	}
	if again := BuildDigest(long); again.Text != d.Text {
		t.Fatal("log dài: hai lần dựng phải giống hệt")
	}
}

func TestBuildDigestNeverContainsTestSetFile(t *testing.T) {
	// Lượt đánh giá: chỉ đề bài, không có diff lời giải. Bản tóm tắt chỉ dùng trường đã nạp.
	f := sampleFailure(t, "ea_1", "t1", "internal/core")
	f.Prompt = "Đề bài công khai."
	if d := BuildDigest(f); strings.Contains(d.Text, "lời giải") || strings.Contains(d.Text, "diff") {
		t.Fatalf("bản tóm tắt không được nhắc tới diff lời giải:\n%s", d.Text)
	}
}

func TestFailureLessonGrouping(t *testing.T) {
	mk := func(id, unit, area string, sigs ...string) Failure {
		return Failure{ID: id, Unit: unit, Area: area, Signals: sigs, Source: "eval", Split: eval.SplitValidation}
	}
	// 3 tác vụ cùng vùng → sẵn sàng; và nhóm theo dấu hiệu gom qua nhiều vùng.
	fails := []Failure{
		mk("ea_1", "t1", "internal/core", SigNoTest),
		mk("ea_2", "t2", "internal/core", SigNoTest),
		mk("ea_3", "t3", "internal/core", SigRepeatCmd),
		mk("ea_4", "t4", "internal/api", SigNoTest),
	}
	ops := map[string]Opportunity{}
	for _, o := range failureLessonOpps(fails, 3, 2, nil, nil) {
		ops[o.Key] = o
	}
	core := ops[failureAreaPrefix+"internal/core"]
	if !core.Ready || core.Kind != KindFailureLesson || core.Generator != GenAgent {
		t.Fatalf("vùng internal/core có 3 tác vụ phải sẵn sàng: %+v", core)
	}
	if got := ops[failureSignalPrefix+SigNoTest]; !got.Ready || len(got.SupportingRuns) != 3 {
		t.Fatalf("dấu hiệu no_test gom qua 2 vùng (3 lượt) phải sẵn sàng: %+v", got)
	}
	if got := ops[failureAreaPrefix+"internal/api"]; got.Ready || !strings.Contains(got.Blocked, "chỉ 1 tác vụ") {
		t.Fatalf("2 vùng có 1 tác vụ phải bị chặn: %+v", got)
	}

	// 3 lượt của cùng một tác vụ: không đủ 3 đơn vị phân biệt, phải bị chặn kèm lý do.
	same := []Failure{mk("ea_a", "t1", "internal/core"), mk("ea_b", "t1", "internal/core"), mk("ea_c", "t1", "internal/core")}
	for _, o := range failureLessonOpps(same, 3, 2, nil, nil) {
		if o.Key == failureAreaPrefix+"internal/core" {
			if o.Ready || !strings.Contains(o.Blocked, "cùng một tác vụ") {
				t.Fatalf("3 lượt cùng tác vụ phải bị chặn: %+v", o)
			}
		}
	}

	// Đề xuất đang chờ thì không mở thêm; bằng chứng đã dùng hết thì thiếu bằng chứng mới.
	pending := map[string]string{failureAreaPrefix + "internal/core": "ep_pending"}
	for _, o := range failureLessonOpps(fails, 3, 2, nil, pending) {
		if o.Key == failureAreaPrefix+"internal/core" && (o.Ready || !strings.Contains(o.Blocked, "ep_pending")) {
			t.Fatalf("có đề xuất chờ xử lý phải bị chặn: %+v", o)
		}
	}
	used := map[string]map[string]bool{failureAreaPrefix + "internal/core": {"ea_1": true, "ea_2": true, "ea_3": true}}
	for _, o := range failureLessonOpps(fails, 3, 2, used, nil) {
		if o.Key == failureAreaPrefix+"internal/core" && o.Ready {
			t.Fatalf("mọi lượt đã dùng cho đề xuất trước thì không sẵn sàng: %+v", o)
		}
	}
}

func TestParseLessonsKeepsOnePerKind(t *testing.T) {
	text := "Phân tích xong.\n```json\n" + `{"lessons":[
		{"scope":"global","trigger":"a","mistake":"b","instead":"c","evidence":["ea_1"]},
		{"scope":"global","trigger":"chung thứ hai","mistake":"b","instead":"c"},
		{"scope":"domain:internal/core/**","trigger":"d","mistake":"e","instead":"f","evidence":["ea_2"]},
		{"scope":"domain:internal/api/**","trigger":"riêng thứ hai","mistake":"e","instead":"f"},
		{"scope":"all","trigger":"sai khuôn","mistake":"e","instead":"f"}]}` + "\n```"
	got, ok := parseLessons(text)
	if !ok || len(got) != 2 {
		t.Fatalf("parseLessons = %v, %v; muốn 2 ứng viên (một chung, một riêng)", got, ok)
	}
	if got[0].Trigger != "a" || got[0].Scope != "global" || got[1].Scope != "domain:internal/core/**" {
		t.Fatalf("giữ sai ứng viên: %+v", got)
	}
	if _, ok := parseLessons(`không có khối JSON`); ok {
		t.Fatal("thiếu khối lessons phải báo không đọc được")
	}
	if none, ok := parseLessons(`{"lessons":[]}`); !ok || len(none) != 0 {
		t.Fatalf("{\"lessons\":[]} phải đọc được và rỗng: %v %v", none, ok)
	}
}

func lessonCand(scope string) lessonCandidate {
	return lessonCandidate{Scope: scope, Trigger: "Khi sửa mã trong vùng có test",
		Mistake: "Agent kết thúc mà không chạy test", Instead: "Chạy test của vùng đã sửa trước khi báo xong",
		Evidence: []string{"ea_1", "ea_2"}}
}

func TestCheckLessonTable(t *testing.T) {
	core1 := sampleFailure(t, "ea_1", "t1", "internal/core")
	api2 := sampleFailure(t, "ea_2", "t2", "internal/api")
	core3 := sampleFailure(t, "ea_3", "t3", "internal/core")
	core1.Split, api2.Split, core3.Split = eval.SplitValidation, eval.SplitValidation, eval.SplitValidation
	good := []Failure{core1, api2}

	codes := func(v LessonVerdict) []string {
		var out []string
		for _, f := range v.Findings {
			out = append(out, f.Code)
		}
		return out
	}
	cases := []struct {
		name string
		in   lessonInput
		want []string // mã vi phạm mong đợi (rỗng = đạt)
	}{
		{"chung hợp lệ đạt", lessonInput{Cand: lessonCand("global"), Evidence: good}, nil},
		{"chung chỉ một vùng bị loại", lessonInput{Cand: lessonCand("global"), Evidence: []Failure{core1, core3}}, []string{LessonGeneral}},
		{"riêng một lượt bị loại", lessonInput{Cand: lessonCand("domain:internal/api/**"), Evidence: []Failure{api2}}, []string{LessonSpecific}},
		{"riêng đủ hai lượt đạt", lessonInput{Cand: lessonCand("domain:internal/core/**"), Evidence: []Failure{core1, core3}}, nil},
		{"thiếu instead bị loại", lessonInput{Cand: func() lessonCandidate { c := lessonCand("global"); c.Instead = ""; return c }(), Evidence: good}, []string{LessonForm}},
		{"quá 80 từ bị loại", lessonInput{Cand: func() lessonCandidate {
			c := lessonCand("global")
			c.Mistake = strings.Repeat("từ ", 90)
			return c
		}(), Evidence: good}, []string{LessonForm}},
		{"scope sai bị loại", lessonInput{Cand: lessonCand("all"), Evidence: good}, []string{LessonScope}},
		{"không có bằng chứng bị loại", lessonInput{Cand: lessonCand("global")}, []string{LessonOrigin, LessonGeneral}},
		{"lượt tập test bị loại", lessonInput{Cand: lessonCand("global"),
			Evidence: []Failure{core1, func() Failure { f := api2; f.Split = eval.SplitTest; return f }()}}, []string{LessonOrigin}},
		{"rò rỉ định danh lời giải bị loại", lessonInput{Cand: func() lessonCandidate {
			c := lessonCand("global")
			c.Instead = "Gọi computeQuotaBudget trước khi báo xong"
			return c
		}(), Evidence: good, Leaks: []eval.LeakInput{{NewIdents: map[string]bool{"computeQuotaBudget": true}}}}, []string{LessonLeak}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := CheckLesson(c.in)
			if got := codes(v); !slices.Equal(got, c.want) {
				t.Fatalf("vi phạm = %v (%s), muốn %v", got, describeFindings(v.Findings), c.want)
			}
		})
	}

	// Trùng: không phải vi phạm, mà gộp vào mục đã có.
	existing := &knowledge.Item{ID: "01LESSON", Type: knowledge.TypeLesson, Status: knowledge.StatusVerified,
		Title: "Khi sửa mã trong vùng có test", Body: "x", Scope: knowledge.Scope{}}
	v := CheckLesson(lessonInput{Cand: lessonCand("global"), Evidence: good, Existing: []*knowledge.Item{existing}})
	if !v.OK() || v.Duplicate != "01LESSON" {
		t.Fatalf("trùng phải gộp (không vi phạm, Duplicate=01LESSON): %+v", v)
	}
}

func TestEvidenceForFallsBackToScope(t *testing.T) {
	core := sampleFailure(t, "ea_1", "t1", "internal/core")
	api := sampleFailure(t, "ea_2", "t2", "internal/api")
	fails := []Failure{core, api}
	// Reflector nêu ID không thuộc nhóm → không dùng; bài học riêng rơi về các lượt trong vùng.
	c := lessonCandidate{Scope: "domain:internal/core/**", Evidence: []string{"ea_999"}}
	if got := evidenceFor(c, fails); len(got) != 1 || got[0].ID != "ea_1" {
		t.Fatalf("evidenceFor = %v", got)
	}
	g := lessonCandidate{Scope: "global", Evidence: []string{"ea_2"}}
	if got := evidenceFor(g, fails); len(got) != 1 || got[0].ID != "ea_2" {
		t.Fatalf("evidenceFor(global, nêu ea_2) = %v", got)
	}
}

func TestSelectCasesLessonRules(t *testing.T) {
	lesson := &Proposal{Changes: []Change{{Op: ActionCreate, Draft: &Draft{Type: knowledge.TypeLesson,
		Lesson: &LessonMeta{Provenance: knowledge.ProvenanceFailureLesson}}}}, EvalCases: []string{"c1"}}
	c1 := &eval.Case{ID: "c1", Split: eval.SplitValidation}
	ctx := context.Background()

	// Tác vụ nguồn của bài học không được chấm lại trên chính nó.
	imp, err := SelectCases(ctx, eval.Env{}, "economy", lesson, nil, []*eval.Case{c1})
	if err != nil || len(imp) != 1 || imp[0].Affected || !strings.Contains(imp[0].Reason, "nguồn của bài học") {
		t.Fatalf("SelectCases bỏ tác vụ nguồn: %+v, %v", imp, err)
	}
	// Delivery minimal không chở được bài học: không đo được, ghi rõ lý do.
	lesson.EvalCases = nil
	env := eval.Env{Variant: &eval.Variant{Delivery: "minimal"}}
	imp, err = SelectCases(ctx, env, "economy", lesson, nil, []*eval.Case{c1})
	if err != nil || len(imp) != 1 || !strings.Contains(imp[0].Reason, "delivery minimal") {
		t.Fatalf("SelectCases với minimal: %+v, %v", imp, err)
	}
}
