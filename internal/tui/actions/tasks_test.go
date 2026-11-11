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
