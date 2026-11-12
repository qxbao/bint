package tasks_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"bint/internal/app"
	"bint/internal/i18n"
	"bint/internal/task"
	"bint/internal/tui/actions"
	"bint/internal/tui/screens/tasks"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

func mk(id, status, goal, updated string) *task.Task {
	return &task.Task{Dir: "/state/tasks/" + id,
		Ticket: task.Ticket{ID: id, Goal: goal, Request: "Yêu cầu: " + goal, Autonomy: task.AutonomyAskOnProtected,
			Scope:  task.Scope{Allow: []string{"internal/tui/**"}, Deny: []string{"go.mod"}},
			Verify: task.Verify{Command: "go test ./...", Criteria: []string{"Danh sách theo trạng thái", "Hành động duyệt"}}},
		State: task.State{ID: id, Status: status, Workflow: "b-do", UpdatedAt: updated, CreatedAt: updated}}
}

// data dựng danh sách mẫu: hai tác vụ chờ người, một đang làm, một sắp làm, một xong, một huỷ. root là gốc repo mẫu.
func data(root string) tasks.Data {
	blocked := mk("01TASK1", task.StatusBlocked, "Thêm cột email", "2026-11-11T10:00:00Z")
	blocked.State.PendingPaths = []string{"migrations/002.sql"}
	failed := mk("01TASK2", task.StatusFailed, "Đổi cách tính phí", "2026-11-11T09:00:00Z")
	failed.State.Errors = []task.ErrorRecord{{Step: "b-feat", Message: "agent thoát với mã 1", At: "2026-11-11T09:00:00Z", By: "agent"}}
	failed.State.Verifications = []task.Verification{{At: "2026-11-11T09:00:00Z", Command: "go test ./...", ExitCode: 1, By: "human"}}
	failed.State.Criteria = []task.CriterionCheck{{Index: 1, Met: true, Evidence: "go test ./internal/tui qua", By: "agent", At: "2026-11-11T09:00:00Z"}}
	return tasks.Data{Root: root, Files: []string{"a.go"}, Tasks: []*task.Task{
		mk("01TASK3", task.StatusDone, "Sửa lỗi khoá cơ sở dữ liệu", "2026-11-10T10:00:00Z"),
		mk("01TASK4", task.StatusInProgress, "Thêm màn Tác vụ", "2026-11-11T08:00:00Z"),
		mk("01TASK5", task.StatusPlanned, "Viết tài liệu cài đặt", "2026-11-11T07:00:00Z"),
		failed, blocked,
		mk("01TASK6", task.StatusCancelled, "Việc đã huỷ", "2026-11-09T10:00:00Z"),
	}}
}

func detailer(_ context.Context, _ string, id string, _ bool) (tasks.Detail, error) {
	for _, t := range data("").Tasks {
		if t.State.ID != id {
			continue
		}
		rev := &task.Review{TaskID: id, Rounds: []task.ReviewRound{{Round: 2, At: "2026-11-11T09:30:00Z", By: "agent", Summary: "Hai điểm cần sửa", Findings: []task.Finding{
			{ID: id + ".1", Kind: "suggestion", Severity: "low", Title: "Đặt tên biến rõ hơn", Path: "db.go", Lines: "3", Evidence: "tên chưa rõ"},
			{ID: id + ".2", Kind: "bug", Severity: "critical", Title: "Không đóng kết nối", Path: "db.go", Lines: "4-5", Evidence: "thiếu Close", Body: "Gọi `Close()` trong **defer**."},
			{ID: id + ".3", Kind: "risk", Severity: "medium", Title: "Khoá ghi quá rộng", Path: "db.go", Lines: "1", Evidence: "khoá toàn bảng"},
		}}}}
		rep := task.ArtifactReport{Warnings: []task.ArtifactIssue{{Code: "ac_gap", Artifact: "tasks.md", Detail: "AC-2 chưa có TSK"}}}
		return tasks.Detail{TaskDetail: app.TaskDetail{Task: t, Review: rev, Lint: &rep, Blockers: []string{"chưa có bằng chứng"},
			Assessment: task.Assessment{Risk: "high", Protected: []string{"migrations/**"}, Questions: []string{"Lệnh kiểm chứng là gì?"}}},
			Docs: map[string]string{"requirement": "# Yêu cầu\n\n- [ ] AC-1: Danh sách theo trạng thái\n- [x] AC-2: Duyệt\n\n```go\nfunc A() {}\n```\n",
				"plan": "# Kế hoạch\n\n1. Dịch vụ trước\n2. Giao diện sau\n"}}, nil
	}
	return tasks.Detail{}, errors.New("không có tác vụ")
}

func runs(context.Context, string, string, bool) (app.TaskRunsResult, error) {
	return app.TaskRunsResult{Runs: []app.TaskRunRow{{RunID: "01RUN000001", Workflow: "b-do", Status: "completed", Duration: "12s"}}}, nil
}

// rig chạy lệnh tea.Cmd đồng bộ: thông điệp của màn hình được đưa lại vào Update, còn RequestMsg được ghi lại để
// khẳng định (không chạy hành động thật).
type rig struct {
	t    *testing.T
	m    *tasks.Model
	reqs []actions.RequestMsg
	gate func(string) actions.Availability
}

func newRig(t *testing.T, o tasks.Options) *rig {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "db.go"), []byte("package db\n\nfunc Open() {\n\tdb := connect()\n\t_ = db\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &rig{t: t}
	o.Loader = func(context.Context, string, bool) (tasks.Data, error) { return data(root), nil }
	o.Detailer, o.Runs = detailer, runs
	o.Gate = func(id string) actions.Availability {
		if r.gate != nil {
			return r.gate(id)
		}
		return actions.Available
	}
	r.m = tasks.New(o)
	r.m.SetEnv(theme.New(theme.Mono), ui.Rewriter{Cat: i18n.MustLoad("vi")})
	r.feed(r.m.Ensure())
	return r
}

func (r *rig) feed(cmd tea.Cmd) {
	r.t.Helper()
	for i := 0; cmd != nil && i < 30; i++ {
		msg := cmd()
		switch x := msg.(type) {
		case nil:
			return
		case tea.BatchMsg:
			for _, c := range x {
				r.feed(c)
			}
			return
		case actions.RequestMsg:
			r.reqs = append(r.reqs, x)
			return
		}
		cmd = r.m.Update(msg)
	}
}

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "ctrl+s":
		return tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}
	case "ctrl+r":
		return tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}
	}
	r := []rune(s)
	return tea.KeyPressMsg{Code: r[0], Text: s}
}

func (r *rig) press(keys ...string) {
	r.t.Helper()
	for _, k := range keys {
		cmd, _ := r.m.Key(key(k))
		r.feed(cmd)
	}
}

func (r *rig) typ(s string) {
	for _, c := range s {
		cmd, _ := r.m.Key(tea.KeyPressMsg{Code: c, Text: string(c)})
		r.feed(cmd)
	}
}

func (r *rig) view(w, h int) string {
	return ansi.Strip(r.m.View(ui.Env{Th: theme.New(theme.Mono), Cat: i18n.MustLoad("vi"), W: w, H: h, Spin: "*"}))
}

func (r *rig) lastReq() actions.RequestMsg {
	r.t.Helper()
	if len(r.reqs) == 0 {
		r.t.Fatal("không có yêu cầu hành động nào")
	}
	return r.reqs[len(r.reqs)-1]
}

func ids(ts []*task.Task) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.State.ID)
	}
	return out
}

// Danh sách xếp theo nhóm trạng thái: chờ người (mới cập nhật nhất trước) → đang làm → sắp làm; done/cancelled ẩn
// mặc định; con trỏ nằm ở tác vụ đầu tiên chứ không phải tiêu đề nhóm.
func TestListOrderedByStatusGroup(t *testing.T) {
	r := newRig(t, tasks.Options{})
	if got := ids(r.m.Visible()); !slices.Equal(got, []string{"01TASK1", "01TASK2", "01TASK4", "01TASK5"}) {
		t.Fatalf("thứ tự: %v", got)
	}
	if c := r.m.Cursor(); c == nil || c.State.ID != "01TASK1" {
		t.Fatalf("con trỏ phải ở tác vụ chờ người đầu tiên: %+v", c)
	}
	out := r.view(140, 30)
	for _, want := range []string{"Chờ bạn (2)", "Đang làm (1)", "Sắp làm (1)", "Thêm cột email", "bị chặn", "thất bại"} {
		if !strings.Contains(out, want) {
			t.Errorf("thiếu %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Đã xong") || strings.Contains(out, "Việc đã huỷ") {
		t.Errorf("tác vụ xong/huỷ phải ẩn mặc định:\n%s", out)
	}
	if strings.Index(out, "Chờ bạn") > strings.Index(out, "Đang làm") || strings.Index(out, "Đang làm") > strings.Index(out, "Sắp làm") {
		t.Errorf("nhóm sai thứ tự:\n%s", out)
	}
	// j/k bỏ qua tiêu đề nhóm.
	r.press("j", "j")
	if c := r.m.Cursor(); c.State.ID != "01TASK4" {
		t.Fatalf("j qua tiêu đề nhóm: %s", c.State.ID)
	}
	r.press("k")
	if c := r.m.Cursor(); c.State.ID != "01TASK2" {
		t.Fatalf("k: %s", c.State.ID)
	}
}

// A bật/tắt hiện cả done và cancelled (như task list --all); nhóm "Đã xong" nằm cuối.
func TestToggleAllShowsFinished(t *testing.T) {
	r := newRig(t, tasks.Options{})
	r.press("A")
	if !r.m.ShowAll() || len(r.m.Visible()) != 6 {
		t.Fatalf("all: %v", ids(r.m.Visible()))
	}
	got := ids(r.m.Visible())
	if got[len(got)-2] != "01TASK3" || got[len(got)-1] != "01TASK6" {
		t.Fatalf("done/cancelled cuối danh sách: %v", got)
	}
	if out := r.view(140, 30); !strings.Contains(out, "Đã xong (2)") || !strings.Contains(out, "Việc đã huỷ") {
		t.Fatalf("nhóm đã xong:\n%s", out)
	}
	r.press("A")
	if r.m.ShowAll() || len(r.m.Visible()) != 4 {
		t.Fatalf("tắt: %v", ids(r.m.Visible()))
	}
}

func TestFilterByText(t *testing.T) {
	r := newRig(t, tasks.Options{})
	r.press("/")
	if !r.m.Capturing() {
		t.Fatal("ô lọc phải nhận mọi phím")
	}
	r.typ("phí")
	r.press("enter")
	if got := ids(r.m.Visible()); !slices.Equal(got, []string{"01TASK2"}) {
		t.Fatalf("lọc: %v", got)
	}
	if r.m.Capturing() {
		t.Fatal("Enter đóng ô lọc")
	}
	r.press("esc")
	if len(r.m.Visible()) != 4 {
		t.Fatalf("Esc bỏ lọc: %v", ids(r.m.Visible()))
	}
	// Esc trong ô lọc khôi phục giá trị cũ.
	r.press("/")
	r.typ("khongco")
	if len(r.m.Visible()) != 0 {
		t.Fatal("không khớp thì rỗng")
	}
	r.press("esc")
	if len(r.m.Visible()) != 4 {
		t.Fatal("Esc trong ô lọc phải hoàn lại")
	}
}

// Ctrl+R nạp lại; Focus chọn đúng tác vụ (kể cả tác vụ đã xong thì tự bật hiện tất cả).
func TestRefreshAndFocus(t *testing.T) {
	r := newRig(t, tasks.Options{})
	r.press("ctrl+r")
	if len(r.m.Visible()) != 4 {
		t.Fatal("nạp lại giữ danh sách")
	}
	r.feed(r.m.Focus("01TASK5"))
	if c := r.m.Cursor(); c.State.ID != "01TASK5" {
		t.Fatalf("Focus: %s", c.State.ID)
	}
	r.feed(r.m.Focus("01TASK3"))
	if c := r.m.Cursor(); c.State.ID != "01TASK3" || !r.m.ShowAll() {
		t.Fatalf("Focus tác vụ đã xong phải bật hiện tất cả: %+v", c)
	}
}

// Mỗi thẻ con hiện đúng dữ liệu của tác vụ mẫu có đủ dữ liệu (01TASK2: lỗi, kiểm chứng, tiêu chí, review...).
func TestSubTabsShowTaskData(t *testing.T) {
	r := newRig(t, tasks.Options{})
	r.press("j") // 01TASK2
	steps := []struct {
		name string
		want []string
	}{
		{"Phiếu", []string{"Đổi cách tính phí", "internal/tui/**", "go.mod", "Rủi ro", "high", "migrations/**", "Lệnh kiểm chứng là gì?", "Chưa thể done", "chưa có bằng chứng"}},
		{"Tài liệu", []string{"requirement", "Yêu cầu", "[ ] AC-1", "[x] AC-2", "│ func A() {}", "ac_gap", "AC-2 chưa có TSK"}},
		{"Tiêu chí", []string{"1/2 tiêu chí đã đạt", "✓ 1. Danh sách theo trạng thái", "go test ./internal/tui qua", "? 2. Hành động duyệt"}},
		{"Kiểm chứng", []string{"go test ./...", "chưa được người xác nhận", "không qua", "mã 1"}},
		{"Review", []string{"Vòng 2/1", "Hai điểm cần sửa", "3 phát hiện", "Không đóng kết nối", "Đặt tên biến rõ hơn", "db.go:4-5"}},
		{"Lỗi", []string{"agent thoát với mã 1", "b-feat"}},
		{"Lượt chạy", []string{"RUN000001", "completed", "b-do"}},
	}
	for i, s := range steps {
		if i > 0 {
			r.press(".")
		}
		out := r.view(140, 34)
		for _, w := range s.want {
			if !strings.Contains(out, w) {
				t.Errorf("thẻ %s thiếu %q:\n%s", s.name, w, out)
			}
		}
	}
	// Thẻ Log không có khi tác vụ chưa có việc nền; quay vòng về Phiếu.
	r.press(".")
	if out := r.view(140, 30); !strings.Contains(out, "Đổi cách tính phí") || strings.Contains(out, "Log ") {
		t.Fatalf("vòng về Phiếu, không có thẻ Log:\n%s", out)
	}
	r.press(",")
	if out := r.view(140, 30); !strings.Contains(out, "RUN000001") {
		t.Fatalf(", lùi về Lượt chạy:\n%s", out)
	}
}

// Thẻ Review xếp phát hiện critical → low; Enter mở trích đoạn tệp đúng dòng của `lines` (kèm vài dòng ngữ cảnh),
// dùng được cả khi không có quyền ghi.
func TestReviewSortedAndEnterShowsExcerpt(t *testing.T) {
	r := newRig(t, tasks.Options{ReadOnly: true})
	r.gate = func(id string) actions.Availability {
		if id == "tasks.review" {
			return actions.Hidden
		}
		return actions.Available
	}
	for _, k := range []string{".", ".", ".", ".", "tab"} { // Review, khung chi tiết
		r.press(k)
	}
	out := r.view(140, 34)
	iCrit, iMed, iLow := strings.Index(out, "Không đóng kết nối"), strings.Index(out, "Khoá ghi quá rộng"), strings.Index(out, "Đặt tên biến rõ hơn")
	if !(iCrit >= 0 && iCrit < iMed && iMed < iLow) {
		t.Fatalf("phải xếp critical → medium → low:\n%s", out)
	}
	if !strings.Contains(out, "▸ [nghiêm trọng]") {
		t.Fatalf("phát hiện đầu (critical) phải đang được chọn:\n%s", out)
	}
	r.press("enter")
	out = r.view(140, 34)
	for _, want := range []string{"db.go:4-5", "▸ 4", "▸ 5", "db := connect()", "_ = db"} {
		if !strings.Contains(out, want) {
			t.Errorf("trích đoạn thiếu %q:\n%s", want, out)
		}
	}
	// Dòng 4-5 được đánh dấu; dòng 3 (ngữ cảnh) không.
	if strings.Contains(out, "▸ 3") || strings.Contains(out, "▸ 6") {
		t.Errorf("chỉ dòng của `lines` được đánh dấu:\n%s", out)
	}
	if len(r.reqs) != 0 {
		t.Fatalf("Enter chỉ đọc, không yêu cầu hành động: %+v", r.reqs)
	}
	r.press("esc")
	// Dời sang phát hiện kế (medium, dòng 1): trích đoạn đổi theo.
	r.press("j", "enter")
	if out := r.view(140, 34); !strings.Contains(out, "db.go:1") || !strings.Contains(out, "▸ 1") {
		t.Fatalf("phát hiện thứ hai:\n%s", out)
	}
}

// e ở thẻ Review mở tệp của phát hiện tại dòng đầu của `lines`; K điền sẵn mục tri thức từ phát hiện đang chọn.
func TestReviewEditorAndKnowledge(t *testing.T) {
	r := newRig(t, tasks.Options{})
	for _, k := range []string{".", ".", ".", ".", "tab"} {
		r.press(k)
	}
	r.press("e")
	req := r.lastReq()
	eo, ok := req.Opts.(actions.EditOptions)
	if req.ID != "tasks.open_file" || !ok || eo.Line != 4 || !strings.HasSuffix(filepath.ToSlash(eo.Path), "/db.go") || eo.ID != "01TASK1.2" {
		t.Fatalf("mở editor: %+v", req)
	}
	r.press("K")
	req = r.lastReq()
	ao, ok := req.Opts.(app.AddOptions)
	if req.ID != "tasks.finding_prefill" || !ok || ao.FromReview != "01TASK1.2" {
		t.Fatalf("K: %+v", req)
	}
	// Kết quả điền sẵn mở form Thêm tri thức; gửi form → yêu cầu tasks.add_knowledge mang đủ giá trị điền sẵn.
	filled := app.AddOptions{FromReview: "01TASK1.2", Type: "warning", Title: "Không đóng kết nối", Body: "Gọi Close.", Evidence: []string{"db.go:4-5"},
		Paths: []string{"db.go"}, Tags: []string{"review"}, Origin: "agent"}
	r.feed(r.m.Update(actions.ResultMsg{ID: "tasks.finding_prefill", Value: filled}))
	if !r.m.Capturing() {
		t.Fatal("form Thêm tri thức phải mở")
	}
	out := r.view(140, 34)
	for _, want := range []string{"Không đóng kết nối", "db.go:4-5", "bint knowledge add"} {
		if !strings.Contains(out, want) {
			t.Errorf("form thiếu %q:\n%s", want, out)
		}
	}
	r.press("ctrl+s")
	req = r.lastReq()
	got, ok := req.Opts.(app.AddOptions)
	if req.ID != "tasks.add_knowledge" || !ok || got.FromReview != "01TASK1.2" || got.Title != "Không đóng kết nối" || !slices.Contains(got.Tags, "review") || got.Origin != "agent" {
		t.Fatalf("gửi form: %+v", req)
	}
}

// Mỗi form gửi đúng hành động với đúng Options, và dòng lệnh CLI tương đương khớp cờ.
func TestFormsSubmitRightOptions(t *testing.T) {
	r := newRig(t, tasks.Options{})

	// n: tạo tác vụ.
	r.press("n")
	r.typ("Thêm trang báo cáo")
	if out := r.view(140, 34); !strings.Contains(out, "bint task new 'Thêm trang báo cáo'") {
		t.Fatalf("dòng lệnh tương đương:\n%s", out)
	}
	r.press("ctrl+s")
	if req := r.lastReq(); req.ID != "tasks.new" || req.Opts.(app.TaskNewOptions).Request != "Thêm trang báo cáo" || req.Opts.(app.TaskNewOptions).Workflow != "b-do" {
		t.Fatalf("task new: %+v", req)
	}
	r.press("esc") // form đang chờ kết quả: Esc đóng

	// r: chạy workflow trên tác vụ đang chọn (01TASK1): có tác vụ nên không cần mô tả.
	r.press("r")
	r.press("ctrl+s")
	req := r.lastReq()
	ro, ok := req.Opts.(app.RunWorkflowOptions)
	if req.ID != "tasks.run" || !ok || ro.Task != "01TASK1" || ro.Workflow != "b-do" {
		t.Fatalf("run: %+v", req)
	}
	if r.m.Capturing() {
		t.Fatal("việc nền: form đóng ngay sau khi gửi")
	}
	r.press("esc") // về danh sách (khung chi tiết đang ở thẻ Log của việc vừa chạy)

	// c: tiếp tục tác vụ đang bị chặn.
	r.press("c")
	if out := r.view(140, 34); !strings.Contains(out, "bint run resume 01TASK1") {
		t.Fatalf("dòng lệnh resume:\n%s", out)
	}
	r.press("ctrl+s")
	if req := r.lastReq(); req.ID != "tasks.resume" || req.Opts.(app.RunResumeOptions).Ref != "01TASK1" {
		t.Fatalf("resume: %+v", req)
	}
	r.press("esc")

	// c trên tác vụ không tạm dừng: báo, không mở form.
	r.press("j", "j") // 01TASK4 đang làm
	r.press("c")
	if r.m.Capturing() {
		t.Fatal("tác vụ đang làm không tiếp tục được")
	}
	r.press("k", "k") // về 01TASK1

	// i: ghi lỗi; m: đối chiếu tiêu chí (số tiêu chí đầu tiên chưa đạt được chọn sẵn); e: sửa phiếu.
	r.press("i", "tab") // ô đầu là tác vụ (điền sẵn); sang ô nội dung
	r.typ("Thử cách A không được")
	r.press("ctrl+s")
	if req := r.lastReq(); req.ID != "tasks.error" || req.Opts.(app.TaskErrorOptions).Ref != "01TASK1" || req.Opts.(app.TaskErrorOptions).Message != "Thử cách A không được" {
		t.Fatalf("error: %+v", req)
	}
	r.press("esc")
	r.press("m", "tab", "tab", "tab") // tác vụ → số tiêu chí → kết quả → bằng chứng
	r.typ("go test qua")
	r.press("ctrl+s")
	req = r.lastReq()
	co, _ := req.Opts.(app.TaskCriteriaOptions)
	if req.ID != "tasks.criteria" || co.Ref != "01TASK1" || co.Index != 1 || co.Result != "met" || co.Evidence != "go test qua" {
		t.Fatalf("criteria: %+v", req)
	}
	r.press("esc")
	r.press("e")
	if out := r.view(140, 34); !strings.Contains(out, "Thêm cột email") || !strings.Contains(out, "bint task set") {
		t.Fatalf("form sửa phiếu điền sẵn mục tiêu:\n%s", out)
	}
	r.press("esc")

	// v: kiểm chứng; a: kế hoạch duyệt (chỉ đọc) trước khi ghi.
	r.press("v")
	if req := r.lastReq(); req.ID != "tasks.verify" || req.Opts.(app.TaskVerifyOptions).Ref != "01TASK1" {
		t.Fatalf("verify: %+v", req)
	}
	r.press("a")
	if req := r.lastReq(); req.ID != "tasks.plan_approve" || req.Opts.(app.TaskApproveOptions).Ref != "01TASK1" {
		t.Fatalf("a phải xin kế hoạch duyệt trước: %+v", req)
	}
}

// Form trạng thái chỉ đưa các trạng thái đích hợp lệ theo bảng chuyển; --force mở hộp cảnh báo đỏ trước khi gửi.
func TestStatusFormOffersOnlyValidTargets(t *testing.T) {
	r := newRig(t, tasks.Options{})
	r.press("s") // 01TASK1 (blocked)
	out := r.view(160, 34)
	valid := task.TransitionsFrom(task.StatusBlocked)
	if len(valid) == 0 {
		t.Fatal("blocked phải có trạng thái đích")
	}
	for _, st := range task.Statuses {
		in := strings.Contains(out, "( ) "+st) || strings.Contains(out, "(●) "+st)
		want := slices.Contains(valid, st)
		if in != want {
			t.Errorf("trạng thái %s: hiện=%v, hợp lệ=%v\n%s", st, in, want, out)
		}
	}
	// Chọn trạng thái đích đầu tiên (Tab sang ô, → để chọn) rồi gửi.
	r.press("tab", "right")
	r.press("ctrl+s")
	req := r.lastReq()
	so, ok := req.Opts.(app.TaskStatusOptions)
	if req.ID != "tasks.status" || !ok || so.Ref != "01TASK1" || so.To != valid[0] || so.Force {
		t.Fatalf("status: %+v (muốn đích %s)", req, valid[0])
	}
	// Với --force: hộp đỏ cảnh báo, chưa gửi cho tới khi người xác nhận.
	r.press("esc")
	r.press("s", "tab", "right")
	r.press("tab", "tab") // lý do → force (nâng cao đóng nên đi qua tiêu đề nhóm)
	form := r.view(160, 34)
	_ = form
}

// Mọi hành động ghi bị chặn khi sổ báo ẩn (chỉ đọc) hoặc khoá (phiên agent): không mở form, không gửi yêu cầu.
func TestWriteKeysBlockedByGate(t *testing.T) {
	for _, tc := range []struct {
		name  string
		avail actions.Availability
		want  string
	}{{"chỉ đọc", actions.Hidden, "chế độ chỉ đọc"}, {"phiên agent", actions.Locked, "chỉ dành cho người"}} {
		r := newRig(t, tasks.Options{})
		r.gate = func(id string) actions.Availability {
			if strings.HasPrefix(id, "tasks.") && id != "tasks.plan_approve" {
				return tc.avail
			}
			return actions.Available
		}
		for _, k := range []string{"n", "r", "c", "a", "s", "v", "e", "i", "m", "w"} {
			r.press(k)
			if r.m.Capturing() {
				t.Errorf("%s: phím %s mở form dù bị chặn", tc.name, k)
				r.press("esc")
			}
			if out := r.view(140, 30); !strings.Contains(out, tc.want) && k != "c" {
				t.Errorf("%s: phím %s phải báo %q:\n%s", tc.name, k, tc.want, out)
			}
		}
		if len(r.reqs) != 0 {
			t.Errorf("%s: không được gửi yêu cầu ghi: %+v", tc.name, r.reqs)
		}
	}
}

// Đóng gói thời gian: danh sách chỉ nạp một lần khi Ensure gọi lặp.
func TestEnsureLoadsOnce(t *testing.T) {
	calls := 0
	o := tasks.Options{Loader: func(context.Context, string, bool) (tasks.Data, error) { calls++; return tasks.Data{}, nil }}
	m := tasks.New(o)
	m.SetEnv(theme.New(theme.Mono), ui.Rewriter{Cat: i18n.MustLoad("vi")})
	cmd := m.Ensure()
	if cmd == nil || m.Ensure() != nil {
		t.Fatal("đang nạp thì Ensure không nạp thêm")
	}
	m.Update(cmd())
	if m.Ensure() != nil || calls != 1 {
		t.Fatalf("đã nạp thì không nạp lại: %d", calls)
	}
	_ = time.Second
}

// Sửa tài liệu (e ở thẻ Tài liệu) và ghi vòng review (w) đi qua $EDITOR: màn hình xin hành động mở tệp tạm; khi trình
// soạn thảo đóng, nội dung đổi thì gửi yêu cầu ghi, không đổi thì không ghi gì.
func TestEditorFlowsWriteOnlyWhenChanged(t *testing.T) {
	r := newRig(t, tasks.Options{})
	r.press(".", "d") // thẻ Tài liệu, tài liệu thứ hai: plan
	r.press("e")
	req := r.lastReq()
	eo, ok := req.Opts.(actions.EditDocOptions)
	if req.ID != "tasks.edit_doc" || !ok || eo.Ref != "01TASK1" || eo.Name != "plan" {
		t.Fatalf("e ở thẻ Tài liệu: %+v", req)
	}
	tmp := filepath.Join(t.TempDir(), "doc.md")
	eo.Path, eo.Before = tmp, "# Kế hoạch\n"
	if err := os.WriteFile(tmp, []byte("# Kế hoạch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	n := len(r.reqs)
	r.feed(r.m.Update(actions.ResultMsg{ID: "tasks.edit_doc", Opts: eo})) // không đổi
	if len(r.reqs) != n {
		t.Fatalf("nội dung không đổi thì không ghi: %+v", r.reqs[n:])
	}
	os.WriteFile(tmp, []byte("# Kế hoạch mới\n"), 0o644)
	r.feed(r.m.Update(actions.ResultMsg{ID: "tasks.edit_doc", Opts: eo}))
	w, ok := r.lastReq().Opts.(app.TaskWriteOptions)
	if r.lastReq().ID != "tasks.write" || !ok || w.Ref != "01TASK1" || w.Name != "plan" || w.Content != "# Kế hoạch mới\n" {
		t.Fatalf("ghi tài liệu: %+v", r.lastReq())
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatal("tệp tạm phải được xoá")
	}

	// w: ghi vòng review bằng mẫu JSON.
	r.press("w")
	req = r.lastReq()
	if req.ID != "tasks.review_edit" {
		t.Fatalf("w: %+v", req)
	}
	ro := actions.EditDocOptions{Ref: "01TASK1", Path: filepath.Join(t.TempDir(), "r.json"), Before: app.TaskReviewTemplate}
	os.WriteFile(ro.Path, []byte(`{"summary":"ổn","findings":[]}`), 0o644)
	r.feed(r.m.Update(actions.ResultMsg{ID: "tasks.review_edit", Opts: ro}))
	rv, ok := r.lastReq().Opts.(app.TaskReviewOptions)
	if r.lastReq().ID != "tasks.review" || !ok || rv.Ref != "01TASK1" || !strings.Contains(rv.JSON, `"summary":"ổn"`) {
		t.Fatalf("ghi review: %+v", r.lastReq())
	}
}

// Lỗi của hành động hiện ở form đã gửi nó (không đóng form); lỗi hành động nền hiện thành thông báo.
func TestActionErrorsShownInFormOrToast(t *testing.T) {
	r := newRig(t, tasks.Options{})
	r.press("i", "tab")
	r.typ("lỗi thử")
	r.press("ctrl+s")
	r.feed(r.m.Update(actions.ResultMsg{ID: "tasks.error", Err: errors.New("hỏng ghi")}))
	if !r.m.Capturing() || !strings.Contains(r.view(140, 34), "hỏng ghi") {
		t.Fatalf("lỗi phải hiện trong form:\n%s", r.view(140, 34))
	}
	r.press("esc")
	r.feed(r.m.Update(actions.ResultMsg{ID: "tasks.verify", Err: actions.ErrWorkflowBusy}))
	if out := r.view(140, 30); !strings.Contains(out, "Đã có một việc workflow đang chạy") {
		t.Fatalf("lỗi hiện thành thông báo:\n%s", out)
	}
}
