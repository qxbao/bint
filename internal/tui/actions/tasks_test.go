package actions_test

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"bint/internal/app"
	"bint/internal/config"
	"bint/internal/knowledge"
	"bint/internal/task"
	"bint/internal/tui/actions"
	"bint/internal/workflow"
)

// taskWithReview dựng repo đã init kèm một tác vụ có một vòng review (một phát hiện trỏ tới src/a.go:3).
func taskWithReview(t *testing.T) (env actions.Env, taskID, findingID string) {
	t.Helper()
	root, _ := seed(t)
	env = actions.Env{Dir: root, Version: "test"}
	r := do(t, "tasks.new", env, func() app.TaskNewOptions {
		o := app.TaskNewDefaults(config.Default())
		o.Request, o.NoPlan = "Sửa hàm A trong src/a.go", true
		return o
	}())
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	taskID = r.Value.(app.TaskCreated).Task.State.ID
	r = do(t, "tasks.review", env, app.TaskReviewOptions{Ref: taskID, JSON: `{"summary":"ổn","findings":[{"kind":"risk","severity":"medium",
		"title":"Hàm A rỗng","path":"src/a.go","lines":"3","evidence":"a.go:3 không có thân","body":"Thêm thân hàm A."}]}`})
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	return env, taskID, taskID + ".1"
}

// `K`: phát hiện review → điền sẵn (origin agent) → thêm mục tri thức: candidate, có nhãn `review`, bằng chứng đúng dòng;
// chờ người duyệt ở màn Tri thức. Người đổi Nguồn thành human thì mục verified ngay (như CLI).
func TestFindingToKnowledgeCreatesCandidateWithReviewTag(t *testing.T) {
	env, taskID, fid := taskWithReview(t)
	r := do(t, "tasks.finding_prefill", env, app.AddOptions{FromReview: fid})
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	filled := r.Value.(app.AddOptions)
	if filled.Origin != knowledge.OriginAgent || filled.Type != knowledge.TypeWarning || filled.Title != "Hàm A rỗng" ||
		len(filled.Evidence) != 1 || filled.Evidence[0] != "src/a.go:3" || !strings.Contains(strings.Join(filled.Tags, ","), "review") {
		t.Fatalf("điền sẵn: %+v", filled)
	}
	r = do(t, "tasks.add_knowledge", env, filled)
	ar, _ := r.Value.(app.AddResult)
	if r.Err != nil || !ar.Saved || ar.Item.Status != knowledge.StatusCandidate || ar.Origin != knowledge.OriginAgent ||
		!strings.Contains(strings.Join(ar.Item.Scope.Tags, ","), "review") || !strings.Contains(ar.Item.Body, fid) || !strings.Contains(ar.Item.Body, taskID) {
		t.Fatalf("thêm mục: %v %+v", r.Err, ar)
	}
	// Đổi Nguồn thành human trong form: verified ngay.
	filled.Origin, filled.Body, filled.Title = knowledge.OriginHuman, "Nội dung do người viết lại, khác mục trước.", "Hàm A rỗng (người)"
	ar, _ = do(t, "tasks.add_knowledge", env, filled).Value.(app.AddResult)
	if !ar.Saved || ar.Item.Status != knowledge.StatusVerified {
		t.Fatalf("origin human: %+v", ar)
	}
	// ID phát hiện sai: lỗi, không ghi.
	if r := do(t, "tasks.finding_prefill", env, app.AddOptions{FromReview: "khong-hop-le"}); r.Err == nil {
		t.Fatal("ID sai phải lỗi")
	}
}

// Sửa tài liệu và ghi vòng review qua $EDITOR: hành động trả ExecMsg với tệp tạm chứa nội dung hiện có (hoặc mẫu JSON);
// sau khi đóng trình soạn thảo màn hình đọc tệp tạm và gửi yêu cầu ghi.
func TestEditDocAndReviewTemplateGoThroughEditor(t *testing.T) {
	env, taskID, _ := taskWithReview(t)
	if r := do(t, "tasks.write", env, app.TaskWriteOptions{Ref: taskID, Name: "requirement", Content: "# Yêu cầu cũ\n"}); r.Err != nil {
		t.Fatal(r.Err)
	}
	var edited []string
	env.Editor = func(p string) *exec.Cmd { edited = append(edited, p); return exec.Command("true") }

	a, _ := actions.Default().ByID("tasks.edit_doc")
	msg := a.Do(context.Background(), env, actions.EditDocOptions{Ref: taskID, Name: "requirement"})
	em, ok := msg.(actions.ExecMsg)
	if !ok {
		t.Fatalf("phải trả ExecMsg: %#v", msg)
	}
	eo := em.Opts.(actions.EditDocOptions)
	b, _ := os.ReadFile(eo.Path)
	if string(b) != "# Yêu cầu cũ\n" || eo.Before != "# Yêu cầu cũ\n" || len(edited) != 1 || edited[0] != eo.Path {
		t.Fatalf("tệp tạm phải chứa nội dung hiện có: %q %+v", b, eo)
	}
	os.Remove(eo.Path)

	a, _ = actions.Default().ByID("tasks.review_edit")
	em, ok = a.Do(context.Background(), env, actions.EditDocOptions{Ref: taskID}).(actions.ExecMsg)
	if !ok {
		t.Fatal("review_edit phải trả ExecMsg")
	}
	eo = em.Opts.(actions.EditDocOptions)
	if b, _ := os.ReadFile(eo.Path); string(b) != app.TaskReviewTemplate {
		t.Fatalf("mẫu JSON: %q", b)
	}
	os.Remove(eo.Path)
}

// `a`: kế hoạch duyệt chỉ đọc không ghi gì; ApproveTask chỉ chạy được khi là người.
func TestPlanApproveActionIsReadOnly(t *testing.T) {
	env, taskID, _ := taskWithReview(t)
	if r := do(t, "tasks.set", env, app.TaskSetOptions{Ref: taskID, VerifyCommand: "git --version", VerifyCommandSet: true}); r.Err != nil {
		t.Fatal(r.Err)
	}
	r := do(t, "tasks.plan_approve", env, app.TaskApproveOptions{Ref: taskID})
	p, ok := r.Value.(app.ApprovePlan)
	if r.Err != nil || !ok || p.VerifyCommand != "git --version" {
		t.Fatalf("plan: %v %+v", r.Err, r.Value)
	}
	ws, _ := app.OpenTasks(context.Background(), env.Dir, "t")
	if tk, _ := ws.Tasks.Load(taskID); tk.State.VerifyApproval != nil {
		t.Fatal("kế hoạch duyệt không được ghi VerifyApproval")
	}
	if r := do(t, "tasks.approve", env, app.TaskApproveOptions{Ref: taskID}); r.Err != nil {
		t.Fatal(r.Err)
	}
	if tk, _ := ws.Tasks.Load(taskID); tk.State.VerifyApproval == nil || tk.State.Status != task.StatusCreated {
		t.Fatalf("duyệt không đổi trạng thái, chỉ xác nhận lệnh: %+v", tk.State)
	}
}

// `D`: diff của tác vụ là hành động chỉ đọc, chạy qua dịch vụ app.DiffTask. Tác vụ chưa có b-feat nào: kết quả rỗng
// kèm ghi chú (hoặc phiếu cũ nếu không theo dõi tệp).
func TestTaskDiffActionIsReadOnly(t *testing.T) {
	a, ok := actions.Default().ByID("tasks.diff")
	if !ok || a.Writes || a.HumanOnly || a.CLIPath != "task diff" || a.Do == nil {
		t.Fatalf("tasks.diff phải là hành động chỉ đọc: %+v", a)
	}
	env, taskID, _ := taskWithReview(t)
	r := do(t, "tasks.diff", env, app.TaskDiffOptions{Ref: taskID, NameOnly: true})
	dr, ok := r.Value.(app.TaskDiffResult)
	if r.Err != nil || !ok || dr.Task == nil || dr.Task.State.ID != taskID || !dr.Legacy {
		t.Fatalf("kết quả: %v %+v", r.Err, r.Value)
	}
	if r := do(t, "tasks.diff", env, app.TaskDiffOptions{Ref: taskID, Stat: true, NameOnly: true}); r.Err == nil {
		t.Fatal("hai cờ loại trừ nhau phải lỗi")
	}
}

// G2: `o` đưa về cây gốc và `x` huỷ là hành động ghi qua dịch vụ app (cờ --3way/--keep, --reason/--discard là ô form);
// thông điệp nhắc lệnh CLI tra được trong bảng ánh xạ.
func TestTaskLandAndCancelActions(t *testing.T) {
	reg := actions.Default()
	for id, want := range map[string]struct {
		path  string
		flags []string
	}{
		"tasks.land":   {"task land", []string{"3way", "keep"}},
		"tasks.cancel": {"task cancel", []string{"reason", "discard"}},
	} {
		a, ok := reg.ByID(id)
		if !ok || !a.Writes || a.Do == nil || a.CLIPath != want.path {
			t.Fatalf("%s: %+v", id, a)
		}
		have := map[string]bool{}
		for _, f := range a.Flags() {
			have[f] = true
		}
		for _, f := range want.flags {
			if !have[f] {
				t.Errorf("%s thiếu cờ %s (có %v)", id, f, a.Flags())
			}
		}
		if tg, ok := actions.LookupCLI(reg, want.path, want.flags[:1]); !ok || tg.Action.ID != id || len(tg.Fields) != 1 || tg.Fields[0] != want.flags[0] {
			t.Errorf("tra lệnh %q: %+v %v", want.path, tg, ok)
		}
	}
	for id, want := range map[string]string{"tasks.diff": "D", "tasks.land": "o", "tasks.cancel": "x"} {
		if a, _ := reg.ByID(id); a.Key != want {
			t.Errorf("%s: phím %q, mong %q", id, a.Key, want)
		}
	}

	env, taskID, _ := taskWithReview(t)
	// Tác vụ chạy thẳng trên cây của nó không có gì để đưa về.
	if r := do(t, "tasks.land", env, app.TaskLandOptions{Ref: taskID}); r.Err == nil || !strings.Contains(r.Err.Error(), "không có gì để đưa về") {
		t.Fatalf("land tác vụ shared: %v", r.Err)
	}
	// Huỷ tác vụ shared như thường.
	r := do(t, "tasks.cancel", env, app.TaskCancelOptions{Ref: taskID, Reason: "bỏ"})
	if tk, ok := r.Value.(*task.Task); r.Err != nil || !ok || tk.State.Status != task.StatusCancelled {
		t.Fatalf("cancel: %v %+v", r.Err, r.Value)
	}
}

// G3a: `run abort` là hành động ghi của TUI (không phím), chạy qua dịch vụ app; `run next`/`run complete` là giao thức của
// phiên điều phối nên nằm trong Excluded của bảng phủ (không có hành động).
func TestTaskAbortAction(t *testing.T) {
	reg := actions.Default()
	a, ok := reg.ByID("tasks.abort")
	if !ok || !a.Writes || a.Do == nil || a.CLIPath != "run abort" || a.Key != "" || a.Footer {
		t.Fatalf("tasks.abort: %+v", a)
	}
	if tg, ok := actions.LookupCLI(reg, "run abort", []string{"reason"}); !ok || tg.Action.ID != "tasks.abort" {
		t.Fatalf("tra lệnh: %+v %v", tg, ok)
	}
	for _, p := range []string{"run next", "run complete"} {
		if _, ok := actions.Data.Excluded[p]; !ok {
			t.Errorf("%q phải nằm trong Excluded", p)
		}
	}
	env, taskID, _ := taskWithReview(t)
	// Tác vụ không có bước dở: abort vẫn chạy (dừng tác vụ), có ghi chú.
	r := do(t, "tasks.abort", env, app.RunAbortOptions{Ref: taskID, Reason: "thử"})
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if sa, ok := r.Value.(*workflow.SessionAction); !ok || sa.TaskID != taskID || sa.Status != task.StatusInterrupted {
		t.Fatalf("abort: %+v", r.Value)
	}
}
