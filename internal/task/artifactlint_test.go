package task

import (
	"slices"
	"strings"
	"testing"
)

const goodReq = "# Yêu cầu\n## Mục tiêu và phạm vi\nĐổi app.txt\n## Tiêu chí chấp nhận\n- [ ] AC-1: app.txt chứa v2\n## Ngoài phạm vi\nKhông có\n## Kiểm tra mục tiêu\nTestApp: cài app.txt giả rồi quét, đạt khi chứa v2\n## Câu hỏi mở\n"
const goodTasks = "- [ ] TSK-1 [AC-1]: sửa app.txt — xong khi: app.txt chứa v2\n"

func goodTicket() Ticket {
	return Ticket{Goal: "g", Autonomy: AutonomyAskOnProtected, Verify: Verify{Criteria: []string{"app.txt chứa v2"}}}
}

func goodDocs() map[string]string {
	return map[string]string{"requirement": goodReq, "plan": "# Kế hoạch\nSửa app.txt\n", "tasks": goodTasks}
}

func codes(is []ArtifactIssue) string {
	var out []string
	for _, i := range is {
		out = append(out, i.Code)
	}
	return strings.Join(out, ",")
}

func hasCode(is []ArtifactIssue, code string) bool {
	for _, i := range is {
		if i.Code == code {
			return true
		}
	}
	return false
}

func TestLintPlanningValid(t *testing.T) {
	r := LintPlanningText(goodTicket(), goodDocs(), true)
	if r.Blocking() || len(r.Errors)+len(r.Warnings)+len(r.OpenQuestions) != 0 {
		t.Fatalf("bộ tài liệu hợp lệ: %+v", r)
	}
	if r.Errors == nil || r.Warnings == nil || r.OpenQuestions == nil {
		t.Fatal("slice phải khác nil để JSON ra mảng rỗng")
	}
}

func TestLintPlanningMissingAndEmpty(t *testing.T) {
	d := goodDocs()
	delete(d, "plan")
	d["tasks"] = "  \n\t\n"
	r := LintPlanningText(goodTicket(), d, true)
	if !hasCode(r.Errors, "artifact_missing") || !hasCode(r.Errors, "artifact_empty") {
		t.Fatalf("%s", codes(r.Errors))
	}
	for _, i := range r.Errors {
		if i.Code == "artifact_missing" && i.Artifact != "plan.md" {
			t.Fatalf("artifact: %+v", i)
		}
		if i.Code == "artifact_empty" && i.Artifact != "tasks.md" {
			t.Fatalf("artifact: %+v", i)
		}
	}
	// Chỉ khoảng trắng sau `task write` (sửa trực tiếp) cũng là rỗng.
	d = goodDocs()
	d["requirement"] = "\n  \n"
	if r := LintPlanningText(goodTicket(), d, true); !hasCode(r.Errors, "artifact_empty") {
		t.Fatalf("%s", codes(r.Errors))
	}
}

func TestLintPlanningDuplicateIDs(t *testing.T) {
	d := goodDocs()
	d["requirement"] = strings.Replace(goodReq, "- [ ] AC-1: app.txt chứa v2\n", "- [ ] AC-1: app.txt chứa v2\n- [ ] AC-1: app.txt chứa v2\n", 1)
	if r := LintPlanningText(goodTicket(), d, true); !hasCode(r.Errors, "duplicate_id") {
		t.Fatalf("AC trùng: %s", codes(r.Errors))
	}
	d = goodDocs()
	d["tasks"] = goodTasks + goodTasks
	if r := LintPlanningText(goodTicket(), d, true); !hasCode(r.Errors, "duplicate_id") {
		t.Fatalf("TSK trùng: %s", codes(r.Errors))
	}
}

func TestLintPlanningCriterionMismatch(t *testing.T) {
	tk := goodTicket()
	tk.Verify.Criteria = []string{"app.txt chứa v3"}
	r := LintPlanningText(tk, goodDocs(), true)
	if !hasCode(r.Errors, "criterion_mismatch") {
		t.Fatalf("%s", codes(r.Errors))
	}
	// Khoảng trắng thừa không làm lệch.
	tk.Verify.Criteria = []string{"  app.txt   chứa\tv2 "}
	if r := LintPlanningText(tk, goodDocs(), true); r.Blocking() {
		t.Fatalf("chuẩn hóa khoảng trắng: %+v", r.Errors)
	}
}

func TestLintPlanningACTaskReferences(t *testing.T) {
	d := goodDocs()
	d["tasks"] = "- [ ] TSK-1 [AC-1, AC-7]: sửa — xong khi: ok\n"
	if r := LintPlanningText(goodTicket(), d, true); !hasCode(r.Errors, "unknown_ac") {
		t.Fatalf("%s", codes(r.Errors))
	}
	tk := goodTicket()
	tk.Verify.Criteria = []string{"app.txt chứa v2", "b"}
	d = goodDocs()
	d["requirement"] = strings.Replace(goodReq, "app.txt chứa v2\n## Ngoài", "app.txt chứa v2\n- [ ] AC-2: b\n## Ngoài", 1)
	r := LintPlanningText(tk, d, true)
	if !hasCode(r.Errors, "uncovered_ac") || hasCode(r.Errors, "criterion_mismatch") {
		t.Fatalf("%s", codes(r.Errors))
	}
}

func TestLintPlanningCriteriaCountMismatch(t *testing.T) {
	tk := goodTicket()
	tk.Verify.Criteria = []string{"app.txt chứa v2", "thừa"}
	if r := LintPlanningText(tk, goodDocs(), true); !hasCode(r.Errors, "criteria_count_mismatch") {
		t.Fatalf("ticket thừa tiêu chí: %s", codes(r.Errors))
	}
	tk.Verify.Criteria = nil
	if r := LintPlanningText(tk, goodDocs(), true); !hasCode(r.Errors, "no_criteria") {
		t.Fatalf("ticket không có tiêu chí: %s", codes(r.Errors))
	}
}

func TestLintPlanningSequentialAndTaskFormat(t *testing.T) {
	d := goodDocs()
	d["requirement"] = strings.Replace(goodReq, "AC-1", "AC-2", 1)
	tk := goodTicket()
	d["tasks"] = "- [ ] TSK-1 [AC-2]: sửa — xong khi: ok\n"
	if r := LintPlanningText(tk, d, true); !hasCode(r.Errors, "ac_not_sequential") {
		t.Fatalf("%s", codes(r.Errors))
	}
	for name, line := range map[string]string{
		"thiếu xong khi": "- [ ] TSK-1 [AC-1]: sửa app.txt\n",
		"xong khi rỗng":  "- [ ] TSK-1 [AC-1]: sửa app.txt — xong khi: \n",
		"thiếu AC":       "- [ ] TSK-1 []: sửa — xong khi: ok\n",
		"không có []":    "- [ ] TSK-1: sửa — xong khi: ok\n",
	} {
		d := goodDocs()
		d["tasks"] = line
		r := LintPlanningText(goodTicket(), d, true)
		if !r.Blocking() {
			t.Fatalf("%s: phải lỗi", name)
		}
	}
	// Không có TSK nào (định dạng cũ).
	d = goodDocs()
	d["tasks"] = "- [ ] sửa app.txt — xong khi: ok\n"
	if r := LintPlanningText(goodTicket(), d, true); !hasCode(r.Errors, "no_tasks") {
		t.Fatalf("%s", codes(r.Errors))
	}
}

func TestLintPlanningPlaceholder(t *testing.T) {
	for _, ph := range []string{"<nội dung>", "cần làm …", "TODO xem lại", "tbd"} {
		d := goodDocs()
		d["tasks"] = "- [ ] TSK-1 [AC-1]: sửa " + ph + " — xong khi: ok\n"
		r := LintPlanningText(goodTicket(), d, true)
		if !hasCode(r.Errors, "placeholder_text") {
			t.Fatalf("%q: %s", ph, codes(r.Errors))
		}
	}
	d := goodDocs()
	d["requirement"] = strings.Replace(goodReq, "AC-1: app.txt chứa v2", "AC-1: <nội dung>", 1)
	tk := goodTicket()
	tk.Verify.Criteria = []string{"<nội dung>"}
	if r := LintPlanningText(tk, d, true); !hasCode(r.Errors, "placeholder_text") {
		t.Fatalf("AC placeholder: %s", codes(r.Errors))
	}
}

func TestLintPlanningSectionsAndFences(t *testing.T) {
	// `###` nằm trong section AC; AC trong code fence bị bỏ qua.
	d := goodDocs()
	d["requirement"] = "# Yêu cầu\n## Tiêu chí chấp nhận\n### Nhóm 1\n- [ ] AC-1: app.txt chứa v2\n```\n- [ ] AC-2: giả trong fence\n```\n## Câu hỏi mở\n"
	if r := LintPlanningText(goodTicket(), d, true); r.Blocking() {
		t.Fatalf("%+v", r.Errors)
	}
	// AC ngoài section không được tính.
	d["requirement"] = "# Yêu cầu\n- [ ] AC-1: app.txt chứa v2\n"
	if r := LintPlanningText(goodTicket(), d, true); !hasCode(r.Errors, "no_ac") {
		t.Fatalf("%s", codes(r.Errors))
	}
	// Heading cấp 2 khác kết thúc section AC.
	d["requirement"] = "## Tiêu chí chấp nhận\n- [ ] AC-1: app.txt chứa v2\n## Ngoài phạm vi\n- [ ] AC-2: không phải AC\n"
	if r := LintPlanningText(goodTicket(), d, true); r.Blocking() {
		t.Fatalf("%+v", r.Errors)
	}
}

func TestLintPlanningOpenQuestions(t *testing.T) {
	d := goodDocs()
	d["requirement"] = goodReq + "- [ ] Q-1: dùng bảng nào?\n- [x] Q-2: có migration không? — Trả lời: không\n- [x] Q-3: xóa dữ liệu cũ? — Trả lời:   \n- [x] Q-4: chưa có phần trả lời\n"
	r := LintPlanningText(goodTicket(), d, true)
	if r.Blocking() {
		t.Fatalf("câu hỏi mở không làm Blocking(): %+v", r.Errors)
	}
	if len(r.OpenQuestions) != 3 {
		t.Fatalf("Q-1, Q-3, Q-4 còn mở: %+v", r.OpenQuestions)
	}
	if !strings.Contains(r.OpenQuestions[0].Detail, "Q-1") {
		t.Fatalf("%+v", r.OpenQuestions[0])
	}
	// Q chỉ tính dưới `## Câu hỏi mở`.
	d["requirement"] = "## Tiêu chí chấp nhận\n- [ ] AC-1: app.txt chứa v2\n## Ghi chú\n- [ ] Q-1: không tính\n"
	if r := LintPlanningText(goodTicket(), d, true); len(r.OpenQuestions) != 0 {
		t.Fatalf("%+v", r.OpenQuestions)
	}
	// Đã trả lời hợp lệ thì không còn mở.
	d["requirement"] = goodReq + "- [x] Q-1: dùng bảng nào? — Trả lời: bảng users\n"
	if r := LintPlanningText(goodTicket(), d, true); len(r.OpenQuestions) != 0 {
		t.Fatalf("%+v", r.OpenQuestions)
	}
}

func TestLintPlanningNonStrictKeepsQuestions(t *testing.T) {
	d := goodDocs()
	delete(d, "plan")
	d["requirement"] = goodReq + "- [ ] Q-1: hỏi?\n"
	r := LintPlanningText(goodTicket(), d, false)
	if r.Blocking() || len(r.Errors) != 0 || !hasCode(r.Warnings, "artifact_missing") || len(r.OpenQuestions) != 1 {
		t.Fatalf("%+v", r)
	}
}

func TestLintPlanningTaskReadsFiles(t *testing.T) {
	dir := t.TempDir()
	tk := &Task{Dir: dir, Ticket: goodTicket()}
	r := LintPlanningTask(tk, true)
	if len(r.Errors) != 3 || !hasCode(r.Errors, "artifact_missing") {
		t.Fatalf("%s", codes(r.Errors))
	}
	for n, c := range goodDocs() {
		if _, err := tk.WriteArtifact(n, c); err != nil {
			t.Fatal(err)
		}
	}
	if r := LintPlanningTask(tk, true); r.Blocking() || len(r.OpenQuestions) != 0 {
		t.Fatalf("%+v", r)
	}
}

// Review: dòng câu hỏi sai khuôn không được bỏ qua lặng lẽ (fail-open) vì đó là cổng chặn b-feat.
func TestLintPlanningMalformedQuestionsNotFailOpen(t *testing.T) {
	for name, req := range map[string]string{
		"thiếu dấu hai chấm": goodReq + "- [ ] Q-1 giữ v1?\n",
		"in đậm":             goodReq + "- [ ] **Q-1**: giữ v1?\n",
		"không có ID":        goodReq + "- [ ] giữ v1 không?\n",
	} {
		d := goodDocs()
		d["requirement"] = req
		r := LintPlanningText(goodTicket(), d, true)
		if !hasCode(r.Errors, "malformed_question") {
			t.Errorf("%s: phải báo malformed_question, được errors=%s open=%d", name, codes(r.Errors), len(r.OpenQuestions))
		}
	}
	// Dấu * hợp lệ vẫn được tính khi đúng khuôn Q-N.
	d := goodDocs()
	d["requirement"] = goodReq + "* [ ] Q-1: giữ v1?\n"
	if r := LintPlanningText(goodTicket(), d, true); len(r.OpenQuestions) != 1 || r.Blocking() {
		t.Errorf("`* [ ] Q-1:` đúng khuôn phải là câu hỏi mở: %+v", r)
	}
}

func TestLintPlanningSectionNameVariants(t *testing.T) {
	for _, heading := range []string{"## Câu hỏi mở (cần trả lời)", "## Các câu hỏi mở", "## CÂU HỎI MỞ ##", "   ## Câu hỏi mở"} {
		d := goodDocs()
		d["requirement"] = strings.Replace(goodReq, "## Câu hỏi mở", heading, 1) + "- [ ] Q-1: giữ v1?\n"
		if r := LintPlanningText(goodTicket(), d, true); len(r.OpenQuestions) != 1 {
			t.Errorf("%q: câu hỏi mở không được nhận ra: %+v", heading, r)
		}
	}
	// `###` Câu hỏi mở nằm trong section khác: báo lỗi thay vì bỏ qua.
	d := goodDocs()
	d["requirement"] = strings.Replace(goodReq, "## Câu hỏi mở\n", "### Câu hỏi mở\n- [ ] Q-1: giữ v1?\n", 1)
	if r := LintPlanningText(goodTicket(), d, true); !hasCode(r.Errors, "misplaced_section") {
		t.Errorf("### Câu hỏi mở: %s", codes(r.Errors))
	}
}

func TestLintPlanningFenceHandling(t *testing.T) {
	// Fence không đóng nằm trước section Q: lỗi, không lặng lẽ nuốt câu hỏi.
	d := goodDocs()
	d["requirement"] = "## Tiêu chí chấp nhận\n- [ ] AC-1: app.txt chứa v2\n```\nví dụ\n## Câu hỏi mở\n- [ ] Q-1: giữ v1?\n"
	if r := LintPlanningText(goodTicket(), d, true); !hasCode(r.Errors, "unclosed_code_fence") {
		t.Errorf("%s", codes(r.Errors))
	}
	d = goodDocs()
	d["tasks"] = goodTasks + "```\n- [ ] TSK-9 [AC-1]: x — xong khi: y\n"
	if r := LintPlanningText(goodTicket(), d, true); !hasCode(r.Errors, "unclosed_code_fence") {
		t.Errorf("tasks: %s", codes(r.Errors))
	}
	// Dòng ~~~ trong khối ``` không đóng fence sớm.
	d = goodDocs()
	d["requirement"] = "## Tiêu chí chấp nhận\n- [ ] AC-1: app.txt chứa v2\n```\n~~~\n- [ ] AC-2: giả trong fence\n```\n"
	if r := LintPlanningText(goodTicket(), d, true); r.Blocking() {
		t.Errorf("AC trong fence ``` chứa ~~~ phải bị bỏ qua: %+v", r.Errors)
	}
}

func TestHasPlaceholderTable(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"định danh có ngoặc nhọn ở cuối", "đặt limits.workflows.<flow> cho từng luồng", false},
		{"định danh có ngoặc nhọn ở giữa", "đọc harnesses.<h>.model từ cấu hình", false},
		{"hai đoạn liền nhau", "khoá a.<x>.<y> hợp lệ", false},
		{"định danh a.<b>.c", "khoá a.<b>.c hợp lệ", false},
		{"định danh có đoạn ngoặc nhọn rồi .ident", "khoá a.<b>.go hợp lệ", false},
		{"tên tệp ngoặc nhọn rồi .go", "tệp <tên>.go được sinh", true},
		{"tên tệp ngoặc nhọn rồi .md", "ghi <tên tệp>.md vào docs", true},
		{"ngoặc nhọn rồi .Tiếp thiếu khoảng trắng", "làm <việc>.Tiếp theo", true},
		{"ngoặc nhọn đứng đầu rồi đoạn khác", "<a>.<b> còn trống", true},
		{"code span chứa placeholder", "dùng `<nội dung>` và `TODO` và `…` làm ví dụ", false},
		{"code span nhiều backtick", "ví dụ ``a ` <x> `` xong", false},
		{"ngoặc nhọn đứng riêng", "sửa <nội dung> ngay", true},
		{"ngoặc nhọn cuối câu có dấu chấm", "viết <việc>.", true},
		{"ngoặc nhọn rồi dấu chấm và khoảng trắng", "viết <việc>. Sau đó làm tiếp", true},
		{"ba chấm Unicode", "cần làm …", true},
		{"TODO đứng riêng", "TODO xem lại", true},
		{"tbd đứng riêng", "kết quả tbd", true},
		{"code span không đóng", "dùng `<nội dung> chưa đóng", true},
		{"backtick độ dài khác không đóng", "dùng ``<x>` chưa đóng", true},
		{"ngoài code span vẫn bị bắt", "`ok` rồi <việc>", true},
		{"không có placeholder", "app.txt chứa v2", false},
	}
	for _, c := range cases {
		if got := hasPlaceholder(c.in); got != c.want {
			t.Errorf("%s: hasPlaceholder(%q) = %v, muốn %v", c.name, c.in, got, c.want)
		}
	}
}

func TestLintPlaceholderDottedIdentInACAndTSK(t *testing.T) {
	d := goodDocs()
	d["requirement"] = strings.Replace(goodReq, "AC-1: app.txt chứa v2", "AC-1: limits.workflows.<flow> và `<x>` đọc được", 1)
	d["tasks"] = "- [ ] TSK-1 [AC-1]: đọc harnesses.<h>.model — xong khi: có `TODO` trong ví dụ\n"
	tk := goodTicket()
	tk.Verify.Criteria = []string{"limits.workflows.<flow> và `<x>` đọc được"}
	if r := LintPlanningText(tk, d, true); hasCode(r.Errors, "placeholder_text") {
		t.Fatalf("định danh có dấu chấm/code span không được bắt: %s", codes(r.Errors))
	}
}

func TestGoalCheck(t *testing.T) {
	cases := []struct {
		name, req, want string
	}{
		{"có mục", goodReq, "TestApp: cài app.txt giả rồi quét, đạt khi chứa v2"},
		{"thiếu mục", "# Yêu cầu\n## Tiêu chí chấp nhận\n- [ ] AC-1: a\n", ""},
		{"mục rỗng", "# Y\n## Kiểm tra mục tiêu\n  \n## Câu hỏi mở\n", ""},
		{"chỉ trong code fence", "# Y\n```\n## Kiểm tra mục tiêu\nx\n```\n", ""},
		{"heading ### con nằm trong mục", "# Y\n## Kiểm tra mục tiêu\n### Chuẩn bị\ncài dữ liệu giả\n## Câu hỏi mở\nq\n", "### Chuẩn bị\ncài dữ liệu giả"},
		{"hậu tố mô tả vẫn là mục khác", "# Y\n## Kiểm tra mục tiêu (T2)\nx\n", ""},
		{"CRLF", "# Y\r\n## Kiểm tra mục tiêu\r\nTestX\r\n", "TestX"},
	}
	for _, tc := range cases {
		if got := GoalCheck(tc.req); got != tc.want {
			t.Errorf("%s: GoalCheck = %q, muốn %q", tc.name, got, tc.want)
		}
	}
}

// goal_check_missing luôn là cảnh báo (không chặn), kể cả strict; có mục thì không cảnh báo.
func TestLintPlanningGoalCheckMissingIsWarning(t *testing.T) {
	d := goodDocs()
	d["requirement"] = strings.Replace(goodReq, "## Kiểm tra mục tiêu\nTestApp: cài app.txt giả rồi quét, đạt khi chứa v2\n", "", 1)
	for _, strict := range []bool{true, false} {
		r := LintPlanningText(goodTicket(), d, strict)
		if r.Blocking() || !hasCode(r.Warnings, "goal_check_missing") || hasCode(r.Errors, "goal_check_missing") {
			t.Fatalf("strict=%v: phải chỉ cảnh báo goal_check_missing: %+v", strict, r)
		}
	}
	d["requirement"] = strings.Replace(goodReq, "TestApp: cài app.txt giả rồi quét, đạt khi chứa v2", "   ", 1)
	if r := LintPlanningText(goodTicket(), d, true); !hasCode(r.Warnings, "goal_check_missing") {
		t.Fatalf("mục rỗng cũng phải cảnh báo: %+v", r)
	}
	if r := LintPlanningText(goodTicket(), goodDocs(), true); hasCode(r.Warnings, "goal_check_missing") {
		t.Fatalf("có mục thì không cảnh báo: %+v", r)
	}
	// Thiếu requirement.md: lỗi artifact_missing, không thêm cảnh báo goal_check_missing.
	d = goodDocs()
	delete(d, "requirement")
	if r := LintPlanningText(goodTicket(), d, true); hasCode(r.Warnings, "goal_check_missing") {
		t.Fatalf("không có requirement thì không cảnh báo: %+v", r)
	}
}

func TestPlanPhases(t *testing.T) {
	cases := []struct {
		name, plan string
		want       []string
	}{
		{"không có mục", "# K\n## Hướng\n- G-1: x\n", nil},
		{"mục không dòng G-N", "# K\n## Đề xuất tách giai đoạn\nKhông tách: một mục tiêu.\n", nil},
		{"một dòng", "# K\n## Đề xuất tách giai đoạn\n- G-1: làm hết — phạm vi A\n", []string{"G-1: làm hết — phạm vi A"}},
		{"ba dòng theo N, có [x]", "# K\n## Đề xuất tách giai đoạn\n- [x] G-1: a\n- G-3: c — phụ thuộc G-2\n* [ ] G-2: b\n", []string{"G-1: a", "G-2: b", "G-3: c — phụ thuộc G-2"}},
		{"trong code fence bị bỏ", "# K\n## Đề xuất tách giai đoạn\n```\n- G-1: a\n- G-2: b\n```\n", nil},
		{"ngoài mục không tính", "# K\n## Đề xuất tách giai đoạn\n- G-1: a\n## Rủi ro\n- G-2: b\n", []string{"G-1: a"}},
	}
	for _, tc := range cases {
		got := PlanPhases(tc.plan)
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("%s: PlanPhases = %q, muốn %q", tc.name, got, tc.want)
		}
	}
}

func TestPlanSizeDecl(t *testing.T) {
	cases := []struct {
		name, plan  string
		wantVerdict string
		wantFiles   []string
	}{
		{"không có mục", "# K\n## Hướng\nKết luận: nhỏ\n- `a.go`\n", "", nil},
		{"nhỏ hai tệp", "# K\n## Cỡ kế hoạch\nKết luận: nhỏ\n- `a.go` — sửa\n- `b/c.go`\n", "nhỏ", []string{"a.go", "b/c.go"}},
		{"lớn kèm lý do", "# K\n## Cỡ kế hoạch\nKết luận: lớn (hơn 5 tệp)\n- `a.go`\n", "lớn", []string{"a.go"}},
		{"in đậm, bullet, hoa thường", "# K\n## Cỡ kế hoạch\n- **Kết luận**: NHỎ\n* `x.go`\n", "nhỏ", []string{"x.go"}},
		{"từ dính liền không phải kết luận", "# K\n## Cỡ kế hoạch\nKết luận: nhỏxíu\n", "", nil},
		{"có cả nhỏ và lớn thì lớn", "# K\n## Cỡ kế hoạch\nKết luận: nhỏ\nKết luận: lớn\n", "lớn", nil},
		{"lớn trước nhỏ vẫn lớn", "# K\n## Cỡ kế hoạch\nKết luận: lớn\nKết luận: nhỏ\n", "lớn", nil},
		{"bỏ trùng và dòng không có backtick", "# K\n## Cỡ kế hoạch\nKết luận: nhỏ\n- `a.go`\n- `a.go`\n- tệp khác không backtick\n", "nhỏ", []string{"a.go"}},
		{"trong code fence bị bỏ", "# K\n## Cỡ kế hoạch\n```\nKết luận: nhỏ\n- `a.go`\n```\n", "", nil},
		{"CRLF", "# K\r\n## Cỡ kế hoạch\r\nKết luận: nhỏ\r\n- `a.go`\r\n", "nhỏ", []string{"a.go"}},
		{"ngoài mục không tính", "# K\n## Cỡ kế hoạch\nKết luận: nhỏ\n- `a.go`\n## Rủi ro\n- `z.go`\n", "nhỏ", []string{"a.go"}},
		{"nhiều tệp một dòng đếm đủ", "# K\n## Cỡ kế hoạch\nKết luận: nhỏ\n- `a.go`, `b.go`, `c.go` và `d.go`\n", "nhỏ", []string{"a.go", "b.go", "c.go", "d.go"}},
		{"glob được giữ nguyên để cổng xử lý", "# K\n## Cỡ kế hoạch\nKết luận: nhỏ\n- `internal/workflow/**`\n", "nhỏ", []string{"internal/workflow/**"}},
	}
	for _, tc := range cases {
		v, files := PlanSizeDecl(tc.plan)
		if v != tc.wantVerdict || strings.Join(files, "|") != strings.Join(tc.wantFiles, "|") {
			t.Errorf("%s: PlanSizeDecl = %q %q, muốn %q %q", tc.name, v, files, tc.wantVerdict, tc.wantFiles)
		}
	}
}

func TestPlanSurfaces(t *testing.T) {
	cases := []struct {
		name, plan string
		want       int
	}{
		{"không có mục", "# K\n## Hướng\n- a\n", 0},
		{"hai nhóm", "# K\n## Bề mặt bị ảnh hưởng\n- A — AC-1\n- B\n", 2},
		{"bullet thụt vào là chi tiết", "# K\n## Bề mặt bị ảnh hưởng\n- A\n  - chi tiết\n\t- chi tiết\n- B\n", 2},
		{"dòng văn xuôi không tính", "# K\n## Bề mặt bị ảnh hưởng\nGhi chú\n- A\n", 1},
		{"trong code fence bị bỏ", "# K\n## Bề mặt bị ảnh hưởng\n```\n- A\n- B\n```\n- C\n", 1},
		{"CRLF", "# K\r\n## Bề mặt bị ảnh hưởng\r\n- A\r\n- B\r\n", 2},
		{"ngoài mục không tính", "# K\n## Bề mặt bị ảnh hưởng\n- A\n## Rủi ro\n- B\n", 1},
	}
	for _, tc := range cases {
		if got := PlanSurfaces(tc.plan); got != tc.want {
			t.Errorf("%s: PlanSurfaces = %d, muốn %d", tc.name, got, tc.want)
		}
	}
}

func TestPlanSlices(t *testing.T) {
	cases := []struct {
		name, plan string
		texts      []string
		acs        [][]int
	}{
		{"không có mục", "# K\n## Hướng\n- A — AC-1\n", nil, nil},
		{"nhóm có AC theo thứ tự dòng", "# K\n## Bề mặt bị ảnh hưởng\n- A: `a.go` — AC-1\n- B: `b.go` — AC-2, AC-3\n",
			[]string{"A: `a.go` — AC-1", "B: `b.go` — AC-2, AC-3"}, [][]int{{1}, {2, 3}}},
		{"nhóm không AC (ngoài phạm vi) không phải lát", "# K\n## Bề mặt bị ảnh hưởng\n- A — AC-1\n- Ngoài phạm vi: docs\n- B — AC-2\n",
			[]string{"A — AC-1", "B — AC-2"}, [][]int{{1}, {2}}},
		{"bullet thụt vào là chi tiết", "# K\n## Bề mặt bị ảnh hưởng\n- A — AC-1\n  - chi tiết AC-9\n- B — AC-2\n",
			[]string{"A — AC-1", "B — AC-2"}, [][]int{{1}, {2}}},
		{"AC lặp chỉ tính một lần, AC-10 không lẫn AC-1", "# K\n## Bề mặt bị ảnh hưởng\n- A — AC-10, AC-1, AC-10\n",
			[]string{"A — AC-10, AC-1, AC-10"}, [][]int{{10, 1}}},
		{"trong code fence bị bỏ", "# K\n## Bề mặt bị ảnh hưởng\n```\n- A — AC-1\n```\n- B — AC-2\n", []string{"B — AC-2"}, [][]int{{2}}},
		{"CRLF", "# K\r\n## Bề mặt bị ảnh hưởng\r\n- A — AC-1\r\n- B — AC-2\r\n", []string{"A — AC-1", "B — AC-2"}, [][]int{{1}, {2}}},
		{"ngoài mục không tính", "# K\n## Bề mặt bị ảnh hưởng\n- A — AC-1\n## Rủi ro\n- B — AC-2\n", []string{"A — AC-1"}, [][]int{{1}}},
	}
	for _, tc := range cases {
		got := PlanSlices(tc.plan)
		if len(got) != len(tc.texts) {
			t.Errorf("%s: %d lát, muốn %d: %+v", tc.name, len(got), len(tc.texts), got)
			continue
		}
		for i, g := range got {
			if g.Text != tc.texts[i] || !slices.Equal(g.ACs, tc.acs[i]) {
				t.Errorf("%s: lát %d = %q %v, muốn %q %v", tc.name, i+1, g.Text, g.ACs, tc.texts[i], tc.acs[i])
			}
		}
	}
}
