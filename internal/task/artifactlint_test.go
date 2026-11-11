package task

import (
	"strings"
	"testing"
)

const goodReq = "# Yêu cầu\n## Mục tiêu và phạm vi\nĐổi app.txt\n## Tiêu chí chấp nhận\n- [ ] AC-1: app.txt chứa v2\n## Ngoài phạm vi\nKhông có\n## Câu hỏi mở\n"
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
