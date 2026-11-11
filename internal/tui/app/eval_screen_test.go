package app

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	bintapp "bint/internal/app"
	"bint/internal/eval"
	"bint/internal/tui/actions"
	"bint/internal/tui/components/text"
	"bint/internal/tui/jobs"
	elscreen "bint/internal/tui/screens/eval"
	"bint/internal/tui/theme"
)

// goEval đưa mô hình sang màn Đánh giá (thẻ Ứng viên).
func goEval(t *testing.T, m *Model) { t.Helper(); press(t, m, "5") }

func TestEvalCandidatesTabListAndDetail(t *testing.T) {
	m, _ := newCapModel(t, Options{})
	goEval(t, m)
	hasAll(t, plain(m.Render()), "Ứng viên (2)", "c0001aaaa", "c0002bbbb", "chờ duyệt", "Chi tiết · c0001aaaa", "Thêm cột email vào bảng người dùng",
		"Đề bài", "Nguồn commit", "Kiểm chứng fail→pass", "đạt (fail→pass)", "không rò rỉ", "Lệnh chấm", "go test ./...", "internal/db/db_test.go")
	press(t, m, "j")
	hasAll(t, plain(m.Render()), "Chi tiết · c0002bbbb", "chưa kiểm chứng (bấm v)", "lộ lời giải", "đề bài chứa mã của lời giải")
	// A: gồm ứng viên đã loại.
	press(t, m, "A")
	if !m.el.ShowAll() {
		t.Fatal("A phải bật chế độ gồm đã loại")
	}
	hasAll(t, plain(m.Render()), "Ứng viên (3)", "gồm đã loại", "c0005eeee")
	press(t, m, "j")
	hasAll(t, plain(m.Render()), "Lý do loại", "kiểm thử không thất bại ở commit cha", "không đạt fail→pass")
}

func TestEvalFilterAndSplitFilter(t *testing.T) {
	m, _ := newCapModel(t, Options{})
	goEval(t, m)
	press(t, m, "/")
	typeText(t, m, "khoá")
	press(t, m, "enter")
	if c := m.el.Cand(); c == nil || c.ID != "c0002bbbb" {
		t.Fatalf("lọc khoá: %+v", c)
	}
	hasAll(t, plain(m.Render()), "Ứng viên (1)", "/khoá")
	press(t, m, "esc")
	hasAll(t, plain(m.Render()), "Ứng viên (2)")
	// Thẻ Tác vụ: t đổi tập con.
	press(t, m, ".")
	hasAll(t, plain(m.Render()), "Tác vụ đã duyệt (2)", "mọi tập", "c0003cccc", "c0004dddd")
	press(t, m, "t")
	hasAll(t, plain(m.Render()), "Tác vụ đã duyệt (1)", "tập build", "c0003cccc")
	press(t, m, "t")
	hasAll(t, plain(m.Render()), "Tác vụ đã duyệt (1)", "tập validation", "c0004dddd")
}

func TestEvalNarrowSinglePane(t *testing.T) {
	m := newModel(t, Options{Mode: theme.Mono}, sampleData(), 80, 24)
	goEval(t, m)
	out := plain(m.Render())
	if !strings.Contains(out, "Ứng viên (2)") || strings.Contains(out, "Nguồn commit") {
		t.Fatalf("hẹp: chỉ danh sách:\n%s", out)
	}
	press(t, m, "enter")
	if out := plain(m.Render()); !strings.Contains(out, "Nguồn commit") || strings.Contains(out, "Ứng viên (2)") {
		t.Fatalf("Enter: chỉ chi tiết:\n%s", out)
	}
	press(t, m, "tab")
	hasAll(t, plain(m.Render()), "Ứng viên (2)")
	wide := newModel(t, Options{Mode: theme.Mono}, sampleData(), 104, 30) // vùng nội dung rộng 100 (trừ khung ngoài)
	goEval(t, wide)
	hasAll(t, plain(wide.Render()), "Ứng viên (2)", "Nguồn commit") // từ 100 cột: hai khung
}

// Thẻ Đợt: danh sách và báo cáo có biểu đồ cột tỷ lệ đạt kèm CI95, bảng nhánh, cặp, tác vụ, chẩn đoán; đổi mốc.
func TestEvalBatchReportAndBaseline(t *testing.T) {
	m, _ := newCapModel(t, Options{})
	goEval(t, m)
	press(t, m, ".", ".")
	hasAll(t, plain(m.Render()), "Đợt chạy (2)", "đã huỷ", "hoàn thành", "đợt chưa có lượt nào")
	press(t, m, "j")
	out := plain(m.Render())
	hasAll(t, out, "Báo cáo · ", "Tỷ lệ đạt theo nhánh", "none", "bint", "50%", "100%", "[ 25– 75]", "██", "Các nhánh", "none: đạt 50% [25, 75]", "Mốc so sánh: none",
		"Harness command", "economy")
	press(t, m, "tab", "G")
	hasAll(t, plain(m.Render()), "So sánh theo cặp", "bint − none: 2 tác vụ", "+50.0 điểm %", "[+10.0,", "+90.0]", "Theo tác vụ", "c0003cccc", "Độ tin cậy của phép đo", "economy")
	// Đổi mốc: b mở form (eval report), điền nhánh làm mốc.
	press(t, m, "tab", "b")
	hasAll(t, plain(m.Render()), "Báo cáo", "Nhánh làm mốc", "bint eval report")
	press(t, m, "tab")
	typeText(t, m, "bint")
	press(t, m, "ctrl+s")
	if m.el.Baseline() != "bint" {
		t.Fatalf("mốc = %q", m.el.Baseline())
	}
	hasAll(t, plain(m.Render()), "Mốc so sánh: bint", "none − bint")
}

func TestEvalApproveNormalDialog(t *testing.T) {
	m, c := newCapModel(t, Options{})
	goEval(t, m)
	press(t, m, "a")
	open, danger, checks := m.el.Dialog()
	if !open || danger || len(checks) != 0 {
		t.Fatalf("ứng viên hợp lệ: hộp thường không ô chọn: %v %v %v", open, danger, checks)
	}
	hasAll(t, plain(m.Render()), "Duyệt ứng viên c0001aaaa?", "đã qua kiểm chứng fail→pass", "đề bài không lộ lời giải", "Tập")
	press(t, m, "enter")
	if o := c.last(t, "eval.approve").(bintapp.EvalApproveOptions); len(o.IDs) != 1 || o.IDs[0] != "c0001aaaa" || o.Force || o.Unverified || o.Split != "" {
		t.Fatalf("%+v", o)
	}
	// Chọn tập đích.
	press(t, m, "a")
	typeText(t, m, "test")
	press(t, m, "enter")
	if o := c.last(t, "eval.approve").(bintapp.EvalApproveOptions); o.Split != "test" {
		t.Fatalf("%+v", o)
	}
	// Esc: không gửi gì.
	n := c.count("eval.approve")
	press(t, m, "a", "esc")
	if c.count("eval.approve") != n {
		t.Fatal("Esc không được duyệt")
	}
}

// Ứng viên bị chặn: hộp đỏ có ô --force (loại/lộ lời giải) và --unverified, mặc định không chọn; cờ chỉ đặt khi người chọn.
func TestEvalApproveBlockedRedDialogFlags(t *testing.T) {
	m, c := newCapModel(t, Options{})
	goEval(t, m)
	press(t, m, "j", "a") // c0002bbbb: lộ lời giải + chưa kiểm chứng
	open, danger, checks := m.el.Dialog()
	if !open || !danger || len(checks) != 2 || checks[0] || checks[1] {
		t.Fatalf("hộp đỏ hai ô chưa chọn: %v %v %v", open, danger, checks)
	}
	hasAll(t, plain(m.Render()), "chưa đủ điều kiện duyệt", "lộ lời giải", "(--force)", "(--unverified)", "chưa kiểm chứng fail→pass")
	press(t, m, "enter")
	if o := c.last(t, "eval.approve").(bintapp.EvalApproveOptions); o.Force || o.Unverified {
		t.Fatalf("không chọn thì không cờ: %+v", o)
	}
	press(t, m, "a", "x", "tab", "x", "enter")
	if o := c.last(t, "eval.approve").(bintapp.EvalApproveOptions); !o.Force || !o.Unverified {
		t.Fatalf("chọn đủ: %+v", o)
	}
	press(t, m, "a", "tab", "x", "enter")
	if o := c.last(t, "eval.approve").(bintapp.EvalApproveOptions); o.Force || !o.Unverified {
		t.Fatalf("chỉ chọn ô --unverified: %+v", o)
	}
	// Ứng viên đã loại: ô --force nêu lý do loại.
	press(t, m, "A", "j", "j", "a")
	if _, danger, checks := m.el.Dialog(); !danger || len(checks) != 2 {
		t.Fatalf("đã loại: %v %v", danger, checks)
	}
	hasAll(t, plain(m.Render()), "đã bị loại (kiểm chứng: kiểm thử không thất bại ở commit cha)")
}

// Kết quả từng ID của EvalApproveOutcome hiện rõ: đã duyệt, đã có từ trước, bị từ chối kèm lý do nguyên văn.
func TestEvalApproveOutcomesShown(t *testing.T) {
	m, c := newCapModel(t, Options{})
	goEval(t, m)
	c.values["eval.approve"] = []bintapp.EvalApproveOutcome{
		{ID: "c0001aaaa", Approved: true, Split: "build", Note: " (rò rỉ: warn)"},
		{ID: "c0009zzzz", Already: true},
		{ID: "c0002bbbb", Problem: "c0002bbbb: chưa qua kiểm chứng fail→pass (`bint eval verify c0002bbbb`) — hoặc dùng --unverified"},
	}
	press(t, m, "a", "enter")
	hasAll(t, plain(m.Render()), "Kết quả duyệt", "✓ đã duyệt c0001aaaa vào tập build (rò rỉ: warn)", "c0009zzzz đã được duyệt từ trước", "✗ c0002bbbb: chưa qua kiểm chứng")
}

func TestEvalRejectWithReason(t *testing.T) {
	m, c := newCapModel(t, Options{})
	goEval(t, m)
	press(t, m, "x")
	hasAll(t, plain(m.Render()), "Loại ứng viên c0001aaaa?", "Lý do", "người duyệt loại")
	press(t, m, "enter")
	if o := c.last(t, "eval.reject").(bintapp.EvalRejectOptions); len(o.IDs) != 1 || o.IDs[0] != "c0001aaaa" || o.Reason != "người duyệt loại" {
		t.Fatalf("lý do trống = mặc định của CLI: %+v", o)
	}
	press(t, m, "x")
	typeText(t, m, "trùng")
	press(t, m, "enter")
	if o := c.last(t, "eval.reject").(bintapp.EvalRejectOptions); o.Reason != "trùng" {
		t.Fatalf("%+v", o)
	}
	// Ở thẻ khác: không có con trỏ ứng viên, chuyển về thẻ Ứng viên và nhắc.
	press(t, m, ".", "x")
	if m.el.Tab() != 0 {
		t.Fatalf("phải về thẻ Ứng viên: %d", m.el.Tab())
	}
	hasAll(t, plain(m.Render()), "Chọn một ứng viên")
}

func TestEvalCleanAsksFirst(t *testing.T) {
	m, c := newCapModel(t, Options{})
	goEval(t, m)
	press(t, m, "C")
	hasAll(t, plain(m.Render()), "Dọn kho đệm commit gốc?")
	press(t, m, "esc")
	if c.count("eval.clean") != 0 {
		t.Fatal("Esc không được xoá")
	}
	c.values["eval.clean"] = bintapp.EvalCleanResult{Removed: 2, Freed: 3 << 20}
	press(t, m, "C", "enter")
	if c.count("eval.clean") != 1 {
		t.Fatal("Enter phải xoá")
	}
	hasAll(t, plain(m.Render()), "Đã xoá 2 kho đệm, giải phóng 3.0 MiB")
}

func TestEvalEstimateFormAndResult(t *testing.T) {
	m, c := newCapModel(t, Options{})
	goEval(t, m)
	cost := 1.5
	c.values["eval.estimate"] = &bintapp.EvalEstimateResult{Estimate: eval.Estimate{Runs: 12, TotalCost: &cost, CostSource: "mặc định", NeededCases: 30, AvailableCases: 2, Effect: 0.1},
		Arms: []string{"none", "bint"}, Repeats: 3}
	press(t, m, "E")
	hasAll(t, plain(m.Render()), "Ước tính", "Nhánh", "Số lần lặp", "bint eval estimate")
	press(t, m, "ctrl+s")
	if _, ok := c.last(t, "eval.estimate").(bintapp.EvalEstimateOptions); !ok {
		t.Fatal("phải gửi EvalEstimateOptions")
	}
	hasAll(t, plain(m.Render()), "Ước tính chạy đợt", "12 lượt (nhánh none,bint, 3 lần lặp)", "$1.50", "cần 30 tác vụ", "hiện có 2")
}

// Nhấn r ở thẻ Tác vụ điền sẵn tác vụ dưới con trỏ; hộp xác nhận kèm ước tính nằm ở việc nền (xem test job).
func TestEvalRunFormPrefilledAndAdvanced(t *testing.T) {
	m, _ := newCapModel(t, Options{})
	goEval(t, m)
	press(t, m, ".", "j", "r")
	hasAll(t, plain(m.Render()), "Chạy so sánh các nhánh", "Tác vụ cụ thể", "c0004dddd", "Nhánh", "Ngân sách ($)", "Nâng cao", "bint eval run")
	press(t, m, "ctrl+o")
	hasAll(t, plain(m.Render()), "Lệnh agent tuỳ chỉnh", "Hạt giống", "Chạy song song")
	press(t, m, "esc")
}

// jobOf trả việc Đánh giá đang chạy.
func jobOf(t *testing.T, m *Model) *jobs.Job {
	t.Helper()
	j := m.jobs.OfKind(jobs.KindEval)
	if j == nil {
		t.Fatal("không có việc Đánh giá đang chạy")
	}
	return j
}

// Việc chạy đợt nền: thẻ Log hiện lưới ô dựng từ thông điệp có kiểu; Esc huỷ cho đợt cancelled và hiện lệnh --resume.
func TestEvalRunJobGridCancelAndResumeLine(t *testing.T) {
	m, c := newCapModel(t, Options{}, "eval.run")
	d := newDrv(t, m)
	d.press("5", ".", "r", "ctrl+s")
	if !d.until(func() bool {
		return m.jobs.Len() == 1 && strings.Contains(plain(m.Render()), "dòng log đầu của eval.run")
	}) {
		t.Fatalf("thẻ Log phải hiện log trực tiếp:\n%s", plain(m.Render()))
	}
	if m.el.Tab() != 3 {
		t.Fatalf("phải chuyển sang thẻ Log: %d", m.el.Tab())
	}
	if o := c.last(t, "eval.run").(bintapp.EvalRunOptions); len(o.CaseIDs) != 1 || o.CaseIDs[0] != "c0003cccc" || o.Yes {
		t.Fatalf("TUI không bao giờ đặt Yes: %+v", o)
	}
	hasAll(t, plain(m.Render()), "Đang chạy: eval run")
	j := jobOf(t, m)
	m.Update(jobs.DataMsg{Job: j.ID, Value: bintapp.EvalRunPlan{CaseIDs: []string{"c0003cccc", "c0004dddd"}, Arms: []string{"none", "bint"}, Repeats: 2}})
	m.Update(jobs.DataMsg{Job: j.ID, Value: actions.EvalAttempt{Row: eval.AttemptRow{BatchID: "eb_x", CaseID: "c0003cccc", Arm: "none", Repeat: 1, Status: "passed"}, N: 1, Total: 8}})
	m.Update(jobs.DataMsg{Job: j.ID, Value: actions.EvalAttempt{Row: eval.AttemptRow{BatchID: "eb_x", CaseID: "c0003cccc", Arm: "bint", Repeat: 2, Status: "failed"}, N: 2, Total: 8}})
	hasAll(t, plain(m.Render()), "Tiến độ theo ô", "2/8 lượt đã xong", "c0003cccc none ✓· bint ·✗", "c0004dddd none ·· bint ··")
	// Esc ở thẻ Log: huỷ an toàn.
	c.values["eval.run"] = &bintapp.EvalRunResult{RunResult: eval.RunResult{BatchID: "eb_x", Status: "cancelled", Total: 8, Done: 2}}
	d.press("esc")
	if !d.until(func() bool { return m.jobs.Len() == 0 }) {
		t.Fatal("huỷ phải kết thúc việc")
	}
	hasAll(t, plain(m.Render()), "Kết quả chạy đợt", "Đợt eb_x: đã huỷ, 2/8 lượt", "Chạy tiếp bằng: bint eval run --resume eb_x")
	d.press("esc") // đóng hộp
	hasAll(t, plain(m.Render()), "Chạy tiếp", "bint eval run --resume eb_x")
}

// Đợt xong: toast và thẻ Đợt mở báo cáo của đợt vừa chạy.
func TestEvalRunCompletedOpensReport(t *testing.T) {
	m, c := newCapModel(t, Options{}, "eval.run")
	d := newDrv(t, m)
	done := sampleEval().Batches[1]
	c.values["eval.run"] = &bintapp.EvalRunResult{RunResult: eval.RunResult{BatchID: done.BatchID, Status: "completed", Total: 8, Done: 8}}
	d.press("5", ".", "r", "ctrl+s")
	if !d.until(func() bool { return m.jobs.Len() == 1 }) {
		t.Fatal("việc phải chạy")
	}
	close(c.release["eval.run"])
	if !d.until(func() bool { return m.jobs.Len() == 0 && m.el.Tab() == 2 }) {
		t.Fatalf("thẻ Đợt phải mở:\n%s", plain(m.Render()))
	}
	d.settle(nil)
	hasAll(t, plain(m.Render()), "Đợt eb_01EVALDONE0000000000000001 hoàn thành", "Tỷ lệ đạt theo nhánh", "50%", "100%")
	if b := m.el.CursorBatch(); b == nil || b.BatchID != done.BatchID {
		t.Fatalf("con trỏ phải ở đợt vừa chạy: %+v", b)
	}
}

// Mỗi lúc một việc Đánh giá nặng: việc thứ hai nhận ErrEvalBusy, việc đang chạy không đổi.
func TestEvalOnlyOneHeavyJob(t *testing.T) {
	m, c := newCapModel(t, Options{}, "eval.run", "eval.bootstrap")
	d := newDrv(t, m)
	d.press("5", ".", "r", "ctrl+s")
	if !d.until(func() bool { return m.jobs.Len() == 1 }) {
		t.Fatal("việc phải chạy")
	}
	_, cmd := m.Update(actions.RequestMsg{ID: "eval.bootstrap", Opts: bintapp.EvalBootstrapOptions{}})
	msg := cmd()
	r, ok := msg.(actions.ResultMsg)
	if !ok || !errorsIs(r.Err, actions.ErrEvalBusy) {
		t.Fatalf("việc thứ hai phải bị từ chối: %#v", msg)
	}
	m.Update(msg)
	hasAll(t, plain(m.Render()), "Đang có một việc Đánh giá chạy nền")
	if m.jobs.Len() != 1 || c.count("eval.bootstrap") != 0 {
		t.Fatalf("việc đang chạy không đổi: %d %d", m.jobs.Len(), c.count("eval.bootstrap"))
	}
	// B khi đang bận: nhắc và chuyển sang thẻ Log.
	d.press("5", "B")
	if m.el.Tab() != 3 {
		t.Fatalf("B khi bận phải chuyển sang Log: %d", m.el.Tab())
	}
	close(c.release["eval.run"])
	d.until(func() bool { return m.jobs.Len() == 0 })
}

func errorsIs(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// Chạy tiếp từ thẻ Đợt: chỉ đợt chưa xong; gửi --resume với đúng đợt.
func TestEvalResumeFromBatchesTab(t *testing.T) {
	m, c := newCapModel(t, Options{}, "eval.resume")
	d := newDrv(t, m)
	d.press("5", ".", ".", "j", "R") // đợt hoàn thành: không có gì để chạy tiếp
	hasAll(t, plain(m.Render()), "đã hoàn thành")
	if c.count("eval.resume") != 0 {
		t.Fatal("đợt đã xong không chạy tiếp")
	}
	d.press("k", "R") // đợt đã huỷ: form chạy tiếp (ngân sách, lệnh agent), Ctrl+S gửi
	hasAll(t, plain(m.Render()), "Chạy tiếp đợt chưa xong", "bint eval run --resume eb_01EVALSTOP0000000000000002")
	d.press("ctrl+s")
	if !d.until(func() bool { return c.count("eval.resume") == 1 }) {
		t.Fatal("phải gửi eval.resume")
	}
	if o := c.last(t, "eval.resume").(bintapp.EvalRunOptions); o.Resume != "eb_01EVALSTOP0000000000000002" || o.Yes {
		t.Fatalf("%+v", o)
	}
	if m.el.Tab() != 3 {
		t.Fatalf("phải chuyển sang thẻ Log: %d", m.el.Tab())
	}
	close(c.release["eval.resume"])
	d.until(func() bool { return m.jobs.Len() == 0 })
}

// Hộp thư: Enter trên dòng ứng viên đánh giá mở màn Đánh giá, thẻ Ứng viên, chọn đúng ứng viên.
func TestInboxEnterOnEvalCandidateOpensEvalScreenAtThatCase(t *testing.T) {
	loader := func(context.Context, string, elscreen.Query) (elscreen.Data, error) {
		d := sampleEval()
		d.Pending[0].ID, d.Pending[1].ID = "01EVAL1", "01EVAL2"
		return d, nil
	}
	m := newModel(t, Options{Mode: theme.Mono, EvalLoader: loader}, fullPending(), 140, 40)
	press(t, m, "j", "j", "j", "j", "j", "j", "j", "enter") // 0,1 lô; 2 còn lại; 3 thành phần; 4 tác vụ; 5 đề xuất; 6,7 ứng viên
	if m.Tab() != 4 || m.el.Tab() != 0 {
		t.Fatalf("tab = %d, thẻ = %d, muốn 4 và 0", m.Tab(), m.el.Tab())
	}
	if c := m.el.Cand(); c == nil || c.ID != "01EVAL2" {
		t.Fatalf("con trỏ phải ở ứng viên của dòng hộp thư: %+v", c)
	}
	hasAll(t, plain(m.Render()), "Chi tiết · 01EVAL2")
	// Từ lại hộp thư, dòng thứ nhất.
	press(t, m, "1", "k", "enter")
	if c := m.el.Cand(); c == nil || c.ID != "01EVAL1" || m.Tab() != 4 {
		t.Fatalf("ứng viên thứ nhất: %+v tab=%d", c, m.Tab())
	}
}

// Ở chế độ chỉ đọc, mọi hành động eval có Writes bị từ chối qua phím tắt, bảng lệnh và yêu cầu trực tiếp (Do và Job).
func TestEvalReadOnlyDeniesEveryWritingAction(t *testing.T) {
	var writes, doWrites, jobWrites []string
	for _, a := range actions.Default().All() {
		if a.Screen != actions.Eval || !a.Writes {
			continue
		}
		writes = append(writes, a.ID)
		if a.Job != nil {
			jobWrites = append(jobWrites, a.ID)
		} else if a.Do != nil {
			doWrites = append(doWrites, a.ID)
		}
	}
	want := "eval.approve eval.bootstrap eval.clean eval.mine eval.open_file eval.reject eval.resume eval.run eval.spec eval.verify"
	if got := strings.Join(sortedCopy(writes), " "); got != want {
		t.Fatalf("hành động ghi của eval: %s", got)
	}
	if len(doWrites) == 0 || len(jobWrites) == 0 {
		t.Fatalf("phải có cả hành động Do (%v) và Job (%v) ghi dữ liệu", doWrites, jobWrites)
	}
	reg := actions.Default()
	m := newModel(t, Options{Mode: theme.Mono, ReadOnly: true, Registry: reg}, sampleData(), 140, 40)
	goEval(t, m)
	// Yêu cầu trực tiếp: DeniedMsg, không việc nào bắt đầu.
	for _, id := range writes {
		a, _ := reg.ByID(id)
		_, cmd := m.Update(actions.RequestMsg{ID: id, Opts: a.Options})
		if cmd != nil {
			if msg := cmd(); msg != nil {
				m.Update(msg)
			}
		}
		if m.jobs.Len() != 0 {
			t.Fatalf("%s: không được bắt đầu việc ở chế độ chỉ đọc", id)
		}
		if a.Check(actions.Mode{ReadOnly: true}) != actions.Hidden {
			t.Fatalf("%s phải bị ẩn ở chế độ chỉ đọc", id)
		}
	}
	hasAll(t, plain(m.Render()), "chỉ đọc")
	// Phím tắt: không mở form hay hộp nào.
	for _, k := range []string{"m", "v", "V", "p", "a", "x", "e", "r", "R", "B", "C"} {
		press(t, m, k)
		if m.el.Capturing() {
			t.Fatalf("phím %s không được mở form/hộp ở chế độ chỉ đọc", k)
		}
	}
	// Bảng lệnh: không liệt kê hành động ghi của eval.
	for _, it := range m.paletteItems() {
		for _, id := range writes {
			if it.ID == id {
				t.Fatalf("bảng lệnh không được liệt kê %s ở chế độ chỉ đọc", id)
			}
		}
	}
	// Hành động đọc vẫn dùng được: ước tính, đổi mốc, báo cáo.
	press(t, m, "E")
	hasAll(t, plain(m.Render()), "bint eval estimate")
}

func sortedCopy(s []string) []string {
	out := append([]string(nil), s...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// Không dòng nào vượt chiều rộng ở các trạng thái chính, 80×24 và 140×40, vi/en/xx.
func TestEvalViewsNeverExceedWidth(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {140, 40}} {
		for _, lang := range []string{"vi", "en", "xx"} {
			for name, keys := range map[string][]string{
				"cands": {"5"}, "detail": {"5", "enter"}, "cases": {"5", "."}, "batches": {"5", ".", "."}, "report": {"5", ".", ".", "j", "enter"},
				"approve": {"5", "a"}, "approve_red": {"5", "j", "a"}, "run_form": {"5", "r"}, "estimate_form": {"5", "E"}, "filter": {"5", "/"},
			} {
				m, _ := newCapModel(t, Options{Lang: lang})
				m.SetSize(sz[0], sz[1])
				press(t, m, keys...)
				for i, l := range strings.Split(m.Render(), "\n") {
					if w := text.Width(ansi.Strip(l)); w > sz[0] {
						t.Fatalf("%dx%d %s %s dòng %d rộng %d: %q", sz[0], sz[1], lang, name, i, w, ansi.Strip(l))
					}
				}
			}
		}
	}
	_ = fmt.Sprint
}

// evalGridModel dựng thẻ Log của một việc chạy đợt đang chạy: lưới ô nhận kế hoạch và hai lượt đã xong, kèm log (kiểm thử
// và ảnh chụp). Việc nền thật không chạy: bảng việc có một việc KindEval với log dựng sẵn.
func evalGridModel(t *testing.T, m *Model) {
	t.Helper()
	cancel := func() {}
	j := m.jobs.Add(jobs.Job{Action: "eval.run", Kind: jobs.KindEval, Label: "eval run", Cancel: cancel})
	j.Log.Add("Ước tính: 8 lượt, khoảng $1.20", "Kế hoạch: 2 tác vụ × nhánh none,bint × 2 lần lặp (0 lượt đã xong từ trước)")
	press(t, m, "5", "r")
	press(t, m, "esc")
	m.el.ShowLog()
	m.Update(jobs.DataMsg{Job: j.ID, Value: bintapp.EvalRunPlan{CaseIDs: []string{"c0003cccc", "c0004dddd"}, Arms: []string{"none", "bint"}, Repeats: 2}})
	for n, a := range []eval.AttemptRow{{CaseID: "c0003cccc", Arm: "none", Repeat: 1, Status: "passed"}, {CaseID: "c0003cccc", Arm: "bint", Repeat: 1, Status: "failed"},
		{CaseID: "c0004dddd", Arm: "none", Repeat: 2, Status: "timeout"}} {
		a.BatchID = "eb_x"
		m.Update(jobs.DataMsg{Job: j.ID, Value: actions.EvalAttempt{Row: a, N: n + 1, Total: 8}})
		j.Log.Add(fmt.Sprintf("Lượt %d/8: %s · %s · lần %d → %s", n+1, a.CaseID, a.Arm, a.Repeat, a.Status))
	}
}
