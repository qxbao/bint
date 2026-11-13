package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"bint/internal/config"
	"bint/internal/flow"
	adapters "bint/internal/harness"
	"bint/internal/task"
)

// Test của GĐ4 (docs/custom-flow-plan.md): Runner chạy flow tuỳ biến. Flow dùng skill gốc (b-req, b-feat, b-review) để
// không cần skill tuỳ biến; id bước chỉ gồm chữ thường và gạch ngang (stepRe của agent giả).

const tddFlow = `schema: 1
name: tdd
steps:
  - {id: plan,   kind: plan,      skill: b-req, optional: true}
  - {id: red,    kind: implement, skill: b-feat, role: test-writer, verify: false}
  - {id: green,  kind: implement, skill: b-feat}
  - {id: review, kind: review,    skill: b-review, fix: green}
`

func (h *harness) writeFlow(name, body string) {
	h.t.Helper()
	h.write(".bint/flows/"+name+".yml", body)
}

func tddDef(t *testing.T) flow.Def {
	t.Helper()
	d, err := flow.Decode([]byte(tddFlow))
	if err != nil {
		t.Fatal(err)
	}
	return flow.Normalize(d)
}

// tddBehave: red ghi kiểm thử đỏ, green ghi mã và dấu done, review phát hiện nghiêm trọng ở n vòng đầu.
func (h *harness) tddBehave(blockingRounds int) func(context.Context, string, *Env, string) error {
	rounds := 0
	return func(ctx context.Context, step string, e *Env, id string) error {
		switch step {
		case "plan":
			return h.writePlan(e, id, nil)
		case "red":
			h.write("red_test.txt", "đỏ\n")
		case "green":
			h.write("app.txt", "v2\n")
			h.write("done.txt", "ok\n")
		case "review":
			rounds++
			body := `{"summary":"ổn","findings":[]}`
			if rounds <= blockingRounds {
				body = `{"summary":"lỗi","findings":[{"kind":"bug","severity":"high","title":"sai","path":"app.txt","lines":"1","evidence":"v2 sai"}]}`
			}
			_, _, err := e.SubmitReview(id, []byte(body))
			return err
		}
		return nil
	}
}

func TestPickStepFourStepFlow(t *testing.T) {
	fd := tddDef(t)
	cases := []struct {
		name     string
		status   string
		flowStep string
		check    bool
		land     bool
		noPlan   *bool // Classification.Plan
		noReview bool
		want     string
		finalize bool
	}{
		{name: "created không phân loại ⇒ plan", status: task.StatusCreated, want: "plan"},
		{name: "created Plan=true ⇒ plan", status: task.StatusCreated, noPlan: ptr(true), want: "plan"},
		{name: "created Plan=false (plan optional) ⇒ red", status: task.StatusCreated, noPlan: ptr(false), want: "red"},
		{name: "planned ⇒ implement đầu", status: task.StatusPlanned, want: "red"},
		{name: "in_progress chưa có FlowStep ⇒ red", status: task.StatusInProgress, want: "red"},
		{name: "in_progress FlowStep red", status: task.StatusInProgress, flowStep: "red", want: "red"},
		{name: "in_progress FlowStep green", status: task.StatusInProgress, flowStep: "green", want: "green"},
		{name: "in_progress FlowStep là review ⇒ implement đầu", status: task.StatusInProgress, flowStep: "review", want: "red"},
		{name: "in_progress FlowStep lạ ⇒ implement đầu", status: task.StatusInProgress, flowStep: "khong-co", want: "red"},
		{name: "AwaitingCheck ⇒ stepCheck", status: task.StatusInProgress, flowStep: "red", check: true, want: stepCheck},
		{name: "AwaitingCheck nhưng không in_progress", status: task.StatusPlanned, check: true, want: "red"},
		{name: "in_review ⇒ review", status: task.StatusInReview, flowStep: "green", want: "review"},
		{name: "in_review FlowStep review", status: task.StatusInReview, flowStep: "review", want: "review"},
		{name: "in_review --no-review ⇒ finalize", status: task.StatusInReview, flowStep: "green", noReview: true, finalize: true},
		{name: "in_review chờ đưa về ⇒ finalize", status: task.StatusInReview, land: true, finalize: true},
		{name: "blocked ⇒ không có bước", status: task.StatusBlocked},
		{name: "done ⇒ không có bước", status: task.StatusDone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tk := &task.Task{State: task.State{Status: c.status, FlowStep: c.flowStep, AwaitingCheck: c.check, AwaitingLand: c.land}}
			if c.noPlan != nil {
				tk.State.Classification = &task.Classification{Plan: *c.noPlan}
			}
			rs := &runState{opts: RunOptions{Workflow: "tdd", NoReview: c.noReview}, flow: fd, t: tk, ran: map[string]int{}}
			step, fin := rs.pickStep()
			if step != c.want || fin != c.finalize {
				t.Fatalf("pickStep = (%q, %v), muốn (%q, %v)", step, fin, c.want, c.finalize)
			}
		})
	}
}

// Thứ tự plan, red, green, review, green, review rồi done: red không kiểm chứng, mỗi green kiểm chứng một lần, phạm vi
// kiểm sau mỗi bước implement; vòng sửa về đúng bước fix (green), không về red.
func TestRunCustomFlowOrderVerifyAndFix(t *testing.T) {
	h := newHarness(t)
	h.writeFlow("tdd", tddFlow)
	h.behave = h.tddBehave(1)
	r := h.runner()
	res, err := r.Run(context.Background(), RunOptions{Workflow: "tdd", Request: "Thêm tính năng phiên bản v2 cho app.txt", Plan: ptr(true)})
	if err != nil || res.Status != task.StatusDone {
		t.Fatalf("run tdd: %+v %v", res, err)
	}
	if got, want := strings.Join(h.calls, ","), "plan,red,green,review,green,review"; got != want {
		t.Fatalf("thứ tự bước = %s, muốn %s", got, want)
	}
	tk := loadTask(t, h, res.TaskID)
	if n := len(tk.State.Verifications); n != 2 {
		t.Fatalf("kiểm chứng %d lần, muốn 2 (sau mỗi green, không sau red)", n)
	}
	if tk.State.Workflow != "tdd" || tk.State.FlowSHA != flow.Hash(tddDef(t)) {
		t.Fatalf("phiếu phải mang flow và băm: %q %q", tk.State.Workflow, tk.State.FlowSHA)
	}
	// Chỉ đọc theo kind của bước trong flow: plan và review chỉ đọc, red/green ghi được.
	for i, step := range h.calls {
		wantRO := step == "plan" || step == "review"
		if h.reqs[i].ReadOnly != wantRO {
			t.Errorf("%s: ReadOnly = %v", step, h.reqs[i].ReadOnly)
		}
	}
	// Lý do chuyển trạng thái dựng từ id bước của flow.
	var reasons []string
	for _, e := range tk.State.History {
		reasons = append(reasons, e.Reason)
	}
	joined := strings.Join(reasons, "|")
	for _, want := range []string{"red bắt đầu", "green xong", "sửa theo review vòng 1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("lịch sử thiếu %q:\n%s", want, joined)
		}
	}
}

// Review .2 của GĐ3: vòng sửa về đúng bước Step.Fix, rồi chạy tiếp các bước sau nó. Fix = red quay về red rồi green.
func TestReviewFixReturnsToFixStepThenContinues(t *testing.T) {
	h := newHarness(t)
	h.writeFlow("tdd", strings.Replace(tddFlow, "fix: green", "fix: red", 1))
	h.behave = h.tddBehave(1)
	r := h.runner()
	res, err := r.Run(context.Background(), RunOptions{Workflow: "tdd", Request: "Thêm tính năng phiên bản v2 cho app.txt", Plan: ptr(false)})
	if err != nil || res.Status != task.StatusDone {
		t.Fatalf("run tdd: %+v %v", res, err)
	}
	if got, want := strings.Join(h.calls, ","), "red,green,review,red,green,review"; got != want {
		t.Fatalf("thứ tự bước = %s, muốn %s", got, want)
	}
}

// Review không khai báo fix ⇒ vòng sửa về bước implement cuối (flow nhiều bước vẫn áp quyết định).
func TestReviewWithoutFixReturnsToLastImplement(t *testing.T) {
	h := newHarness(t)
	h.writeFlow("tdd", strings.Replace(tddFlow, ", fix: green", "", 1))
	h.behave = h.tddBehave(1)
	r := h.runner()
	res, err := r.Run(context.Background(), RunOptions{Workflow: "tdd", Request: "Thêm tính năng phiên bản v2 cho app.txt", Plan: ptr(false)})
	if err != nil || res.Status != task.StatusDone {
		t.Fatalf("run tdd: %+v %v", res, err)
	}
	if got, want := strings.Join(h.calls, ","), "red,green,review,green,review"; got != want {
		t.Fatalf("thứ tự bước = %s, muốn %s", got, want)
	}
}

// Quá số vòng review ⇒ blocked, không lặp vô hạn.
func TestReviewRoundsExhaustedBlocks(t *testing.T) {
	h := newHarness(t)
	h.cfg.Workflow.MaxReviewRounds = 1
	h.writeFlow("tdd", tddFlow)
	h.behave = h.tddBehave(10)
	r := h.runner()
	res, err := r.Run(context.Background(), RunOptions{Workflow: "tdd", Request: "Thêm tính năng phiên bản v2 cho app.txt", Plan: ptr(false)})
	if err != nil || res.Status != task.StatusBlocked || !strings.Contains(res.Reason, "max_review_rounds") {
		t.Fatalf("run tdd: %+v %v", res, err)
	}
}

const twoReviewFlow = `schema: 1
name: duo
steps:
  - {id: impl,  kind: implement, skill: b-feat}
  - {id: rev-a, kind: review,    skill: b-review, fix: impl}
  - {id: rev-b, kind: review,    skill: b-review, fix: impl}
`

// GĐ4 .4: flow có nhiều bước review: max_review_rounds đếm vòng sửa (vòng có phát hiện nghiêm trọng), không đếm mọi lượt
// review của mọi bước. verdict(step, lần gọi thứ n của bước đó) cho biết lượt đó có phát hiện nghiêm trọng không.
func (h *harness) twoReviewBehave(verdict func(step string, n int) bool) func(context.Context, string, *Env, string) error {
	calls := map[string]int{}
	return func(ctx context.Context, step string, e *Env, id string) error {
		if step == "impl" {
			h.write("app.txt", "v2\n")
			h.write("done.txt", "ok\n")
			return nil
		}
		calls[step]++
		body := `{"summary":"ổn","findings":[]}`
		if verdict(step, calls[step]) {
			body = `{"summary":"lỗi","findings":[{"kind":"bug","severity":"high","title":"sai","path":"app.txt","lines":"1","evidence":"v2 sai"}]}`
		}
		_, _, err := e.SubmitReview(id, []byte(body))
		return err
	}
}

func TestMaxReviewRoundsCountsFixRoundsNotReviewSteps(t *testing.T) {
	// max_review_rounds = 2: rev-a vòng 1 nghiêm trọng (sửa 1), rev-a vòng 2 sạch, rev-b vòng 3 nghiêm trọng (sửa 2): số
	// vòng review đã tới 3 nhưng mới 2 vòng sửa nên vẫn được sửa tiếp, không blocked.
	h := newHarness(t)
	h.cfg.Workflow.MaxReviewRounds = 2
	h.writeFlow("duo", twoReviewFlow)
	h.behave = h.twoReviewBehave(func(step string, n int) bool { return (step == "rev-a" && n == 1) || (step == "rev-b" && n == 1) })
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: "duo", Request: "Thêm tính năng phiên bản v2 cho app.txt"})
	if err != nil || res.Status != task.StatusDone {
		t.Fatalf("run duo: %+v %v", res, err)
	}
	if got, want := strings.Join(h.calls, ","), "impl,rev-a,impl,rev-a,rev-b,impl,rev-a,rev-b"; got != want {
		t.Fatalf("thứ tự bước = %s, muốn %s", got, want)
	}
}

func TestMaxReviewRoundsBlocksWhenFixRoundsExceeded(t *testing.T) {
	// Cùng cấu hình nhưng rev-b luôn nghiêm trọng: vòng sửa thứ 3 (vòng review 5) vượt max_review_rounds = 2.
	h := newHarness(t)
	h.cfg.Workflow.MaxReviewRounds = 2
	h.writeFlow("duo", twoReviewFlow)
	h.behave = h.twoReviewBehave(func(step string, n int) bool { return (step == "rev-a" && n == 1) || step == "rev-b" })
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: "duo", Request: "Thêm tính năng phiên bản v2 cho app.txt"})
	if err != nil || res.Status != task.StatusBlocked || !strings.Contains(res.Reason, "max_review_rounds = 2") ||
		!strings.Contains(res.Reason, "sau 3 vòng") {
		t.Fatalf("run duo: %+v %v", res, err)
	}
	if got, want := strings.Join(h.calls, ","), "impl,rev-a,impl,rev-a,rev-b,impl,rev-a,rev-b"; got != want {
		t.Fatalf("thứ tự bước = %s, muốn %s", got, want)
	}
}

// Kiểm phạm vi sau MỖI bước implement: red sửa tệp thuộc scope.deny bị chặn chờ người duyệt ngay (blocked), green không
// chạy; kiểm chứng không chạy sau bước bị chặn.
func TestCustomFlowScopeCheckedAfterEachImplement(t *testing.T) {
	h := newHarness(t)
	h.writeFlow("tdd", tddFlow)
	inner := h.tddBehave(0)
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == "red" {
			h.write("outside/leak.txt", "x\n")
		}
		return inner(ctx, step, e, id)
	}
	r := h.runner()
	r.Confirm = nil
	res, err := r.Run(context.Background(), RunOptions{Workflow: "tdd", Request: "Thêm tính năng phiên bản v2 cho app.txt", Plan: ptr(false),
		Edit: TicketEdit{Deny: []string{"outside/**"}}})
	if err != nil || res.Status != task.StatusBlocked {
		t.Fatalf("run tdd: %+v %v", res, err)
	}
	if strings.Join(h.calls, ",") != "red" {
		t.Fatalf("green không được chạy sau red bị chặn: %v", h.calls)
	}
	if n := len(loadTask(t, h, res.TaskID).State.Verifications); n != 0 {
		t.Fatalf("không kiểm chứng sau red: %d lần", n)
	}
}

// Luật "luôn kiểm chứng trước review và trước khi done" nằm ở Runner: bước implement cuối có verify:false (lint bị vượt)
// vẫn được kiểm chứng.
func TestVerifyBeforeReviewEvenWhenLastImplementOptsOut(t *testing.T) {
	h := newHarness(t)
	rv := flow.Def{Schema: 1, Name: "tdd", Steps: []flow.Step{
		{ID: "red", Kind: flow.KindImplement, Skill: "b-feat", Verify: ptr(false)},
		{ID: "green", Kind: flow.KindImplement, Skill: "b-feat", Verify: ptr(false)},
		{ID: "review", Kind: flow.KindReview, Skill: "b-review"}}}
	rs := &runState{flow: rv}
	if next, ok := rs.stepAfter("green"); !ok || next.Kind != flow.KindReview {
		t.Fatalf("stepAfter(green) = %+v %v", next, ok)
	}
	// Kiểm qua Run: tệp flow vượt lint không chạy được (Validate chặn), nên kiểm trực tiếp afterStep.
	h.behave = h.tddBehave(0)
	r := h.runner()
	tk, _, err := r.NewTask(context.Background(), NewTaskOptions{Request: "Thêm tính năng phiên bản v2 cho app.txt", Workflow: StepFeat, Plan: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	h.write("done.txt", "ok\n")
	if _, err := h.tasks.Update(tk.State.ID, r.now(), func(t *task.Task) error {
		t.State.Status, t.State.AwaitingCheck, t.State.FlowStep = task.StatusInProgress, true, "green"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	loaded := loadTask(t, h, tk.State.ID)
	rs = &runState{opts: RunOptions{Workflow: "tdd"}, flow: rv, t: loaded, runID: "r1", res: &Result{Steps: []StepResult{}}, ran: map[string]int{}}
	if !r.afterStep(context.Background(), rs, "green") {
		t.Fatalf("afterStep dừng: %+v", rs.res)
	}
	if n := len(loadTask(t, h, tk.State.ID).State.Verifications); n != 1 {
		t.Fatalf("bước implement cuối bị bỏ kiểm chứng: %d lần", n)
	}
	// Bước giữa (red) có verify:false và bước sau là implement ⇒ không kiểm chứng.
	if _, err := h.tasks.Update(tk.State.ID, r.now(), func(t *task.Task) error {
		t.State.Status, t.State.AwaitingCheck, t.State.FlowStep = task.StatusInProgress, true, "red"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rs.t = loadTask(t, h, tk.State.ID)
	if !r.afterStep(context.Background(), rs, "red") {
		t.Fatalf("afterStep red dừng")
	}
	got := loadTask(t, h, tk.State.ID)
	if n := len(got.State.Verifications); n != 1 || got.State.FlowStep != "green" || got.State.Status != task.StatusInProgress {
		t.Fatalf("sau red: %d kiểm chứng, FlowStep=%q, %s", n, got.State.FlowStep, got.State.Status)
	}
}

// Review .1 của GĐ3: chế độ chỉ đọc lấy từ MỘT nguồn (kind của bước trong flow đang chạy). Flow tuỳ biến có bước implement
// mang id b-review: bước vẫn ghi được, khoá độc quyền, action session readonly=false, subagent bint-implementer.
func TestReadOnlyFollowsKindNotStepName(t *testing.T) {
	const solo = "schema: 1\nname: solo\nsteps:\n  - {id: b-review, kind: implement, skill: b-feat}\n"
	h := newHarness(t)
	h.writeFlow("solo", solo)
	h.behave = func(_ context.Context, step string, _ *Env, _ string) error {
		if step != "b-review" {
			t.Errorf("bước = %s", step)
		}
		h.write("app.txt", "v2\n")
		h.write("done.txt", "ok\n")
		return nil
	}
	r := h.runner()
	res, err := r.Run(context.Background(), RunOptions{Workflow: "solo", Request: "Thêm tính năng phiên bản v2 cho app.txt"})
	if err != nil || res.Status != task.StatusDone {
		t.Fatalf("run solo (bước implement id b-review ghi tệp): %+v %v", res, err)
	}
	if len(h.reqs) != 1 || h.reqs[0].ReadOnly {
		t.Fatalf("agent phải được chạy ở chế độ ghi: %+v", h.reqs)
	}
	if sp := (&runState{flow: flow.Normalize(mustFlow(t, solo))}).spec("b-review"); sp.ReadOnly || sp.Kind != flow.KindImplement {
		t.Fatalf("spec = %+v", sp)
	}

	// Driver session: cùng bước, action không readonly, subagent theo kind implement.
	h = newHarness(t)
	h.writeFlow("solo", solo)
	r = h.runner()
	tk := h.newSession(r, NewTaskOptions{Workflow: "solo", Plan: ptr(false)})
	act, err := r.Next(context.Background(), NextOptions{TaskRef: tk.State.ID})
	if err != nil {
		t.Fatal(err)
	}
	if act.Action != ActionAgent || act.Step != "b-review" || act.ReadOnly || act.Subagent != SubagentImplementer {
		t.Fatalf("action session: %+v", act)
	}
	// Bước dở được trả lại đúng (gọi lại `next`) và mang Kind.
	again, err := r.Next(context.Background(), NextOptions{TaskRef: tk.State.ID})
	if err != nil || again.ReadOnly || again.Subagent != SubagentImplementer {
		t.Fatalf("next lần hai: %+v %v", again, err)
	}
	if ps := loadTask(t, h, tk.State.ID).State.PendingStep; ps == nil || ps.Kind != string(flow.KindImplement) || ps.Mode != leaseModeName(LockExclusive) {
		t.Fatalf("PendingStep phải mang Kind implement và khoá độc quyền: %+v", ps)
	}
	// Ngược lại: bước review đặt id b-feat vẫn chỉ đọc.
	rv := flow.Normalize(mustFlow(t, "schema: 1\nname: odd\nsteps:\n  - {id: b-req, kind: implement, skill: b-feat}\n  - {id: b-feat, kind: review, skill: b-review}\n"))
	rs := &runState{flow: rv}
	if !rs.spec("b-feat").ReadOnly || rs.spec("b-req").ReadOnly {
		t.Fatalf("chỉ đọc phải theo kind: %+v %+v", rs.spec("b-feat"), rs.spec("b-req"))
	}
}

func mustFlow(t *testing.T, y string) flow.Def {
	t.Helper()
	d, err := flow.Decode([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// Flow đổi giữa chừng (FlowSHA khác) ⇒ Run --task và resume blocked kèm lý do nêu tên flow, không bước nào chạy; khôi phục
// flow thì resume chạy tiếp. Flow bị xoá cũng blocked.
func TestChangedFlowBlocksRunAndResume(t *testing.T) {
	h := newHarness(t)
	h.writeFlow("tdd", tddFlow)
	h.behave = h.tddBehave(0)
	r := h.runner()
	tk, _, err := r.NewTask(context.Background(), NewTaskOptions{Request: "Thêm tính năng phiên bản v2 cho app.txt", Workflow: "tdd", Plan: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	id := tk.State.ID
	if tk.State.FlowSHA == "" {
		t.Fatal("phiếu flow tuỳ biến phải có FlowSHA")
	}
	h.writeFlow("tdd", strings.Replace(tddFlow, "verify: false", "verify: true", 1))
	res, err := r.Run(context.Background(), RunOptions{Workflow: "tdd", TaskRef: id})
	if err != nil || res.Status != task.StatusBlocked || !strings.Contains(res.Reason, "flow tdd đã đổi từ lúc lập phiếu") ||
		!strings.Contains(res.Reason, ".bint/flows/tdd.yml") {
		t.Fatalf("flow đổi phải blocked: %+v %v", res, err)
	}
	if len(h.calls) != 0 {
		t.Fatalf("không bước nào được chạy: %v", h.calls)
	}
	res, err = r.Run(context.Background(), RunOptions{Workflow: "tdd", TaskRef: id, Resume: true})
	if err != nil || res.Status != task.StatusBlocked || !strings.Contains(res.Reason, "đã đổi") || len(h.calls) != 0 {
		t.Fatalf("resume khi flow còn lệch: %+v %v", res, err)
	}
	// Flow bị xoá.
	if err := os.Remove(filepath.Join(h.root, ".bint", "flows", "tdd.yml")); err != nil {
		t.Fatal(err)
	}
	res, err = r.Run(context.Background(), RunOptions{Workflow: "tdd", TaskRef: id, Resume: true})
	if err != nil || res.Status != task.StatusBlocked || !strings.Contains(res.Reason, "không còn dùng được") {
		t.Fatalf("flow bị xoá: %+v %v", res, err)
	}
	h.writeFlow("tdd", tddFlow)
	res, err = r.Run(context.Background(), RunOptions{Workflow: "tdd", TaskRef: id, Resume: true})
	if err != nil || res.Status != task.StatusDone {
		t.Fatalf("resume sau khi khôi phục: %+v %v", res, err)
	}
	if got, want := strings.Join(h.calls, ","), "red,green,review"; got != want {
		t.Fatalf("thứ tự = %s", got)
	}
}

// Driver session: flow đổi ⇒ next blocked (không mở bước); khôi phục rồi next --resume mở bước đầu.
func TestSessionNextBlocksWhenFlowChanged(t *testing.T) {
	h := newHarness(t)
	h.writeFlow("tdd", tddFlow)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Workflow: "tdd", Plan: ptr(false)})
	h.writeFlow("tdd", strings.Replace(tddFlow, "verify: false", "verify: true", 1))
	act, err := r.Next(context.Background(), NextOptions{TaskRef: tk.State.ID})
	if err != nil || act.Action != ActionBlocked || !strings.Contains(act.Reason, "flow tdd đã đổi từ lúc lập phiếu") || act.RunID != "" {
		t.Fatalf("next: %+v %v", act, err)
	}
	h.writeFlow("tdd", tddFlow)
	act, err = r.Next(context.Background(), NextOptions{TaskRef: tk.State.ID, Resume: true})
	if err != nil || act.Action != ActionAgent || act.Step != "red" {
		t.Fatalf("next --resume sau khi khôi phục: %+v %v", act, err)
	}
}

// Driver session trên flow 4 bước: step, subagent, readonly, model của từng bước; role test-writer có model riêng; vòng sửa
// về green; role gán harness khác Claude ⇒ action process kèm --step.
func TestSessionCustomFlowActions(t *testing.T) {
	h := newHarness(t)
	h.cfg.Harnesses.Claude.Models.Extra = map[string]string{"test-writer": "haiku"}
	h.writeFlow("tdd", tddFlow)
	h.behave = h.tddBehave(1)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Workflow: "tdd", Plan: ptr(true)})
	type row struct {
		step, subagent, model string
		ro                    bool
	}
	var got []row
	for i := 0; i < 20; i++ {
		act, err := r.Next(context.Background(), NextOptions{TaskRef: tk.State.ID})
		if err != nil {
			t.Fatal(err)
		}
		if act.Action != ActionAgent {
			if act.Action != ActionDone {
				t.Fatalf("dừng: %+v", act)
			}
			break
		}
		got = append(got, row{act.Step, act.Subagent, act.Model, act.ReadOnly})
		h.subagent(act)
		if _, err := r.Complete(context.Background(), CompleteOptions{TaskRef: tk.State.ID, RunID: act.RunID, AgentID: "a-" + act.Step}); err != nil {
			t.Fatal(err)
		}
	}
	want := []row{{"plan", "bint-planner", "opus", true}, {"red", "bint-implementer", "haiku", false},
		{"green", "bint-implementer", "sonnet", false}, {"review", "bint-reviewer", "opus", true},
		{"green", "bint-implementer", "sonnet", false}, {"review", "bint-reviewer", "opus", true}}
	if !slices.Equal(got, want) {
		t.Fatalf("action = %+v\nmuốn %+v", got, want)
	}
	if n := len(loadTask(t, h, tk.State.ID).State.Verifications); n != 2 {
		t.Fatalf("kiểm chứng %d lần, muốn 2", n)
	}

	// Role test-writer gán codex ⇒ process --step red.
	h = newHarness(t)
	h.cfg.Harnesses.Enabled = []string{"claude", "codex"}
	h.cfg.Roles.Extra = map[string]string{"test-writer": "codex"}
	h.writeFlow("tdd", tddFlow)
	r = h.runner()
	tk = h.newSession(r, NewTaskOptions{Workflow: "tdd", Plan: ptr(false)})
	act, err := r.Next(context.Background(), NextOptions{TaskRef: tk.State.ID})
	if err != nil {
		t.Fatal(err)
	}
	if act.Action != ActionProcess || act.Step != "red" || act.Command != "bint run tdd --task "+tk.State.ID+" --step red --json" ||
		!strings.Contains(strings.Join(act.Notes, " "), "test-writer") {
		t.Fatalf("action process: %+v", act)
	}
}

// --step: chỉ với --task và flow tuỳ biến, phải là bước kế tiếp; chạy đúng một bước rồi dừng.
func TestRunStepRequest(t *testing.T) {
	h := newHarness(t)
	h.writeFlow("tdd", tddFlow)
	h.behave = h.tddBehave(0)
	r := h.runner()
	if _, err := r.Run(context.Background(), RunOptions{Workflow: "tdd", Request: "x", Step: "red"}); err == nil || !strings.Contains(err.Error(), "--step") {
		t.Fatalf("--step không --task: %v", err)
	}
	tk, _, err := r.NewTask(context.Background(), NewTaskOptions{Request: "Thêm tính năng phiên bản v2 cho app.txt", Workflow: "tdd", Plan: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	id := tk.State.ID
	if _, err := r.Run(context.Background(), RunOptions{Workflow: StepFeat, TaskRef: id, Step: "red"}); err == nil || !strings.Contains(err.Error(), "--step") {
		t.Fatalf("--step với workflow gốc: %v", err)
	}
	if _, err := r.Run(context.Background(), RunOptions{Workflow: "tdd", TaskRef: id, Step: "green"}); err == nil || !strings.Contains(err.Error(), "bước kế tiếp") {
		t.Fatalf("--step không phải bước kế tiếp: %v", err)
	}
	if len(h.calls) != 0 || loadTask(t, h, id).State.Status != task.StatusCreated {
		t.Fatalf("--step sai không được đổi gì: %v %s", h.calls, loadTask(t, h, id).State.Status)
	}
	res, err := r.Run(context.Background(), RunOptions{Workflow: "tdd", TaskRef: id, Step: "red"})
	if err != nil || strings.Join(h.calls, ",") != "red" {
		t.Fatalf("--step red: %+v %v %v", res, err, h.calls)
	}
	got := loadTask(t, h, id)
	if got.State.Status != task.StatusInProgress || got.State.FlowStep != "green" {
		t.Fatalf("sau red: %s FlowStep=%q", got.State.Status, got.State.FlowStep)
	}
	if _, err := r.Run(context.Background(), RunOptions{Workflow: "tdd", TaskRef: id, Step: "green"}); err != nil {
		t.Fatal(err)
	}
	if got = loadTask(t, h, id); got.State.Status != task.StatusInReview {
		t.Fatalf("sau green: %s", got.State.Status)
	}
}

// Tên không phải workflow gốc và không có flow ⇒ lỗi; flow không lint sạch (skill không tồn tại) ⇒ lỗi nêu lý do.
func TestRunUnknownOrInvalidFlow(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	if _, err := r.Run(context.Background(), RunOptions{Workflow: "khong-co", Request: "x"}); !errors.Is(err, ErrUnknownWorkflow) {
		t.Fatalf("flow lạ: %v", err)
	}
	h.writeFlow("hong", "schema: 1\nname: hong\nsteps:\n  - {id: build, kind: implement, skill: skill-khong-ton-tai}\n")
	if _, err := r.Run(context.Background(), RunOptions{Workflow: "hong", Request: "x"}); err == nil || !strings.Contains(err.Error(), "skill-khong-ton-tai") {
		t.Fatalf("flow hỏng: %v", err)
	}
	if _, _, err := r.NewTask(context.Background(), NewTaskOptions{Request: "x", Workflow: "hong"}); err == nil {
		t.Fatal("NewTask với flow hỏng phải lỗi")
	}
}

// Flow trùng tên skill tiến hoá (hoặc skill tuỳ biến lỗi lint) bị Runner từ chối: cùng luật với `bint flow lint` và
// `bint sync`, vì flow được xuất bản thành skill cùng tên.
func TestResolveFlowRejectsNameClashingWithSkill(t *testing.T) {
	h := newHarness(t)
	h.write(".bint/skills/evolved/core-retry/SKILL.md", "---\nname: core-retry\ndescription: Dùng khi x\n---\nthân\n")
	h.write(".bint/skills/vo-hong/SKILL.md", "không có frontmatter\n")
	for _, name := range []string{"core-retry", "vo-hong"} {
		h.writeFlow(name, "schema: 1\nname: "+name+"\nsteps:\n  - {id: build, kind: implement, skill: b-feat}\n")
		_, err := h.runner().Run(context.Background(), RunOptions{Workflow: name, Request: "x"})
		if err == nil || !strings.Contains(err.Error(), "trùng tên một skill") {
			t.Fatalf("flow %s trùng skill phải bị từ chối: %v", name, err)
		}
	}
}

// Luật đổi workflow khi chạy tác vụ có sẵn.
func TestCheckFlowSwitch(t *testing.T) {
	mk := func(wf string) *task.Task { return &task.Task{State: task.State{ID: "t1", Workflow: wf}} }
	cases := []struct {
		taskWF, run string
		wantErr     bool
	}{
		{"b-do", "b-feat", false}, // như cũ
		{"b-req", "tdd", false},   // tác vụ mới chỉ lập kế hoạch được chạy bằng flow tuỳ biến
		{"tdd", "tdd", false},     // chính nó
		{"tdd", "b-feat", true},   // tác vụ flow tuỳ biến không chạy bằng workflow khác
		{"tdd", "khac", true},     // cũng không bằng flow khác
		{"b-do", "tdd", true},     // flow tuỳ biến không nhận tác vụ b-do
		{"", "tdd", false},        // phiếu rất cũ
	}
	for _, c := range cases {
		err := checkFlowSwitch(mk(c.taskWF), c.run)
		if (err != nil) != c.wantErr {
			t.Errorf("%s ⇒ %s: err = %v", c.taskWF, c.run, err)
		}
	}
	// b-req → flow tuỳ biến: Run ghi workflow và băm của flow.
	h := newHarness(t)
	h.writeFlow("tdd", tddFlow)
	h.behave = h.tddBehave(0)
	r := h.runner()
	tk, _, err := r.NewTask(context.Background(), NewTaskOptions{Request: "Thêm tính năng phiên bản v2 cho app.txt", Workflow: StepReq})
	if err != nil {
		t.Fatal(err)
	}
	if tk.State.FlowSHA != "" {
		t.Fatalf("workflow gốc không có FlowSHA: %q", tk.State.FlowSHA)
	}
	res, err := r.Run(context.Background(), RunOptions{Workflow: "tdd", TaskRef: tk.State.ID, Plan: ptr(false)})
	if err != nil || res.Status != task.StatusDone {
		t.Fatalf("run tdd trên tác vụ b-req: %+v %v", res, err)
	}
	got := loadTask(t, h, tk.State.ID)
	if got.State.Workflow != "tdd" || got.State.FlowSHA != flow.Hash(tddDef(t)) {
		t.Fatalf("workflow/băm sau khi đổi: %q %q", got.State.Workflow, got.State.FlowSHA)
	}
}

// Bước plan bắt buộc của flow tuỳ biến (không optional): phân loại không bỏ được nó.
func TestNewTaskCustomFlowRequiredPlan(t *testing.T) {
	h := newHarness(t)
	h.writeFlow("plan-first", "schema: 1\nname: plan-first\nsteps:\n  - {id: plan, kind: plan, skill: b-req}\n  - {id: build, kind: implement, skill: b-feat}\n")
	r := h.runner()
	if _, _, err := r.NewTask(context.Background(), NewTaskOptions{Request: "Đổi nhãn trong app.txt", Workflow: "plan-first", Plan: ptr(false)}); err == nil ||
		!strings.Contains(err.Error(), "bước plan bắt buộc") {
		t.Fatalf("--no-plan với plan bắt buộc: %v", err)
	}
	tk, _, err := r.NewTask(context.Background(), NewTaskOptions{Request: "Đổi nhãn trong app.txt", Workflow: "plan-first"})
	if err != nil {
		t.Fatal(err)
	}
	if c := tk.State.Classification; c == nil || !c.Plan || tk.State.PlanningContractVersion != task.PlanningContractV1 {
		t.Fatalf("phân loại: %+v v%d", c, tk.State.PlanningContractVersion)
	}
}

// Agent của bước lấy harness từ roles.<role>, model từ models.<role>; role tuỳ biến chưa cấu hình rơi về role gốc theo kind.
func TestAgentFactoryCustomRole(t *testing.T) {
	cfg := config.Default()
	cfg.Harnesses.Enabled = []string{"claude", "codex"}
	cfg.Harnesses.Codex.Binary = filepath.Join(t.TempDir(), "khong-co")
	spec := StepSpec{ID: "red", Kind: flow.KindImplement, Role: "test-writer", Base: "implementer"}

	// Chưa cấu hình test-writer: theo implementer (claude ⇒ chạy được; model của implementer).
	f := HarnessAgentsWithChoices(cfg, "", adapters.Selection{}, nil)
	a, err := f(spec, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := a.(interface{ RequestedModel() string }).RequestedModel(); got != "sonnet" {
		t.Errorf("model rơi về implementer = %q", got)
	}
	// models.test-writer ưu tiên hơn models.implementer.
	cfg.Harnesses.Claude.Models.Extra = map[string]string{"test-writer": "haiku"}
	a, err = HarnessAgentsWithChoices(cfg, "", adapters.Selection{}, nil)(spec, nil, nil)
	if err != nil || a.(interface{ RequestedModel() string }).RequestedModel() != "haiku" {
		t.Fatalf("models.test-writer: %v", err)
	}
	// roles.test-writer = codex ⇒ harness codex (binary không có ⇒ lỗi nêu adapter không khả dụng).
	cfg.Roles.Extra = map[string]string{"test-writer": "codex"}
	if _, err = HarnessAgentsWithChoices(cfg, "", adapters.Selection{}, nil)(spec, nil, nil); err == nil || !strings.Contains(err.Error(), "không khả dụng") {
		t.Fatalf("roles.test-writer=codex: %v", err)
	}
	// Chỉ roles.implementer = codex (test-writer không cấu hình) ⇒ test-writer đi theo implementer.
	cfg.Roles.Extra = nil
	cfg.Roles.Implementer = "codex"
	if _, err = HarnessAgentsWithChoices(cfg, "", adapters.Selection{}, nil)(spec, nil, nil); err == nil || !strings.Contains(err.Error(), "không khả dụng") {
		t.Fatalf("test-writer phải theo implementer=codex: %v", err)
	}
	// --role implementer=claude (chọn của người dùng cho role gốc) áp cho role tuỳ biến chưa cấu hình.
	if _, err = HarnessAgentsWithChoices(cfg, "", adapters.Selection{Roles: map[string]string{"implementer": "claude"}}, nil)(spec, nil, nil); err != nil {
		t.Fatalf("--role implementer=claude: %v", err)
	}
}

// Quyền MCP của bước driver process: mcp.grants.<role> nếu khai báo, không thì mcp.grants.<role gốc>.
func TestStepMCPGrantRole(t *testing.T) {
	cfg := config.Default()
	cfg.MCP.Grants = map[string][]string{"implementer": {"codegraph.explore"}}
	if got := grantRole(cfg, "test-writer", "implementer"); got != "implementer" {
		t.Errorf("role chưa khai báo quyền phải dùng quyền role gốc: %q", got)
	}
	cfg.MCP.Grants["test-writer"] = []string{"deepwiki.read"}
	if got := grantRole(cfg, "test-writer", "implementer"); got != "test-writer" {
		t.Errorf("role khai báo quyền phải dùng quyền riêng: %q", got)
	}
	if got := grantRole(cfg, "implementer", "implementer"); got != "implementer" {
		t.Errorf("role gốc: %q", got)
	}

	// Qua Run: RunBinding.Role của từng bước.
	h := newHarness(t)
	h.cfg.MCP.Grants = map[string][]string{"implementer": {"codegraph.explore"}, "test-writer": {"deepwiki.read"}}
	h.writeFlow("tdd", tddFlow)
	h.behave = h.tddBehave(0)
	r := h.runner()
	r.BintExe = "/opt/bint/bint"
	if res, err := r.Run(context.Background(), RunOptions{Workflow: "tdd", Request: "Thêm tính năng phiên bản v2 cho app.txt", Plan: ptr(false)}); err != nil || res.Status != task.StatusDone {
		t.Fatalf("run: %+v %v", res, err)
	}
	var roles []string
	for _, b := range h.bindings {
		var rb struct{ Step, Role string }
		if err := json.Unmarshal(b, &rb); err != nil {
			t.Fatal(err)
		}
		roles = append(roles, rb.Step+"="+rb.Role)
	}
	if want := "red=test-writer,green=implementer,review=reviewer"; strings.Join(roles, ",") != want {
		t.Fatalf("RunBinding.Role = %v, muốn %s", roles, want)
	}
}

// Flow đổi trong lúc subagent đang chạy: complete vẫn đóng bước (kết quả được ghi) nhưng không áp cổng sau bước theo định
// nghĩa mới, mà chặn tác vụ kèm lý do nêu flow.
func TestSessionCompleteWhenFlowChangedBlocks(t *testing.T) {
	h := newHarness(t)
	h.writeFlow("tdd", tddFlow)
	h.behave = h.tddBehave(0)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Workflow: "tdd", Plan: ptr(false)})
	act, err := r.Next(context.Background(), NextOptions{TaskRef: tk.State.ID})
	if err != nil || act.Step != "red" {
		t.Fatalf("next: %+v %v", act, err)
	}
	h.subagent(act)
	h.writeFlow("tdd", strings.Replace(tddFlow, "verify: false", "verify: true", 1))
	c, err := r.Complete(context.Background(), CompleteOptions{TaskRef: tk.State.ID, RunID: act.RunID, AgentID: "a-red"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Action != ActionBlocked || !strings.Contains(c.Reason, "flow tdd đã đổi từ lúc lập phiếu") || c.Result == nil || c.Result.Status != "completed" {
		t.Fatalf("complete: %+v", c)
	}
	got := loadTask(t, h, tk.State.ID)
	if got.State.PendingStep != nil || got.State.Status != task.StatusBlocked {
		t.Fatalf("bước phải được đóng và tác vụ blocked: pending=%v status=%s", got.State.PendingStep, got.State.Status)
	}
	// Abort vẫn chạy được khi flow đã đổi (không phụ thuộc định nghĩa flow).
	h.writeFlow("tdd", tddFlow)
	act, err = r.Next(context.Background(), NextOptions{TaskRef: tk.State.ID, Resume: true})
	if err != nil || act.Action != ActionAgent {
		t.Fatalf("next --resume: %+v %v", act, err)
	}
	h.writeFlow("tdd", strings.Replace(tddFlow, "verify: false", "verify: true", 1))
	if _, err := r.Abort(context.Background(), tk.State.ID, "thử"); err != nil {
		t.Fatalf("abort khi flow đã đổi: %v", err)
	}
	if ps := loadTask(t, h, tk.State.ID).State.PendingStep; ps != nil {
		t.Fatalf("abort phải gỡ bước dở: %+v", ps)
	}
}

const quickFlow = "schema: 1\nname: quick\nsteps:\n  - {id: green, kind: implement, skill: b-feat}\n"

func quickBehave(h *harness) func(context.Context, string, *Env, string) error {
	return func(_ context.Context, step string, _ *Env, _ string) error {
		h.write("app.txt", "v2\n")
		h.write("done.txt", "ok\n")
		return nil
	}
}

// Review GĐ4 .1: flow tuỳ biến chỉ có MỘT bước implement phải tới done (không dừng ở in_review), có kiểm chứng trước done,
// và chạy lại / resume không chạy lại bước implement.
func TestCustomSingleImplementFlowReachesDone(t *testing.T) {
	h := newHarness(t)
	h.writeFlow("quick", quickFlow)
	h.behave = quickBehave(h)
	r := h.runner()
	res, err := r.Run(context.Background(), RunOptions{Workflow: "quick", Request: "Thêm tính năng phiên bản v2 cho app.txt"})
	if err != nil || res.Status != task.StatusDone {
		t.Fatalf("run quick: %+v %v", res, err)
	}
	if strings.Join(h.calls, ",") != "green" {
		t.Fatalf("bước = %v", h.calls)
	}
	tk := loadTask(t, h, res.TaskID)
	if n := len(tk.State.Verifications); n != 1 {
		t.Fatalf("kiểm chứng %d lần, muốn 1 trước done", n)
	}
	// Chạy lại tác vụ đã done bị từ chối, implement không chạy lại.
	if _, err := r.Run(context.Background(), RunOptions{Workflow: "quick", TaskRef: res.TaskID}); err == nil {
		t.Fatal("tác vụ done không được chạy lại")
	}
	if len(h.calls) != 1 {
		t.Fatalf("implement chạy lại: %v", h.calls)
	}
}

// .1: chạy lại ở in_review (sau lần chạy trước dừng giữa chừng) không chạy lại implement; --no-review/không review ⇒ finalize.
func TestCustomSingleImplementResumeInReviewDoesNotRerun(t *testing.T) {
	h := newHarness(t)
	h.writeFlow("quick", quickFlow)
	h.behave = quickBehave(h)
	r := h.runner()
	tk, _, err := r.NewTask(context.Background(), NewTaskOptions{Request: "Thêm tính năng phiên bản v2 cho app.txt", Workflow: "quick", Plan: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	h.write("done.txt", "ok\n")
	if _, err := h.tasks.Update(tk.State.ID, r.now(), func(t *task.Task) error {
		t.State.Status, t.State.FlowStep = task.StatusInReview, "green"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Verify(context.Background(), tk.State.ID); err != nil {
		t.Fatal(err)
	}
	res, err := r.Run(context.Background(), RunOptions{Workflow: "quick", TaskRef: tk.State.ID})
	if err != nil || res.Status != task.StatusDone || len(h.calls) != 0 {
		t.Fatalf("resume in_review: %+v %v calls=%v", res, err, h.calls)
	}
}

// .1 qua driver session: flow một bước implement tới done, sau bước implement không còn action agent.
func TestSessionCustomSingleImplementReachesDone(t *testing.T) {
	h := newHarness(t)
	h.writeFlow("quick", quickFlow)
	h.behave = quickBehave(h)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Workflow: "quick", Plan: ptr(false)})
	var steps []string
	for i := 0; i < 6; i++ {
		act, err := r.Next(context.Background(), NextOptions{TaskRef: tk.State.ID})
		if err != nil {
			t.Fatal(err)
		}
		if act.Action != ActionAgent {
			if act.Action != ActionDone {
				t.Fatalf("dừng: %+v", act)
			}
			break
		}
		steps = append(steps, act.Step)
		h.subagent(act)
		if _, err := r.Complete(context.Background(), CompleteOptions{TaskRef: tk.State.ID, RunID: act.RunID, AgentID: "a-" + act.Step}); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Join(steps, ",") != "green" || loadTask(t, h, tk.State.ID).State.Status != task.StatusDone {
		t.Fatalf("steps=%v status=%s", steps, loadTask(t, h, tk.State.ID).State.Status)
	}
}

// Review GĐ4 .2: limits.workflows.<flow> áp cho flow tuỳ biến kể cả một bước (tên flow khác id bước), không nới cổng.
func TestCustomSingleStepFlowAppliesWorkflowLimits(t *testing.T) {
	h := newHarness(t)
	h.writeFlow("quick", quickFlow)
	h.behave = quickBehave(h)
	lim := 0.5
	h.cfg.Limits.Workflows = map[string]config.PerRunLimits{"quick": {MaxCostUSD: &lim}}
	c := 0.6
	h.cost = &c
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: "quick", Request: "Thêm tính năng phiên bản v2 cho app.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(h.limits) != 1 || h.limits[0] == nil || *h.limits[0] != 0.5 {
		t.Fatalf("giới hạn chi phí của bước phải là limits.workflows.quick (0.5): %v (%+v)", h.limits, res)
	}
}

// Review GĐ4 .3: tác vụ b-req chuyển sang flow tuỳ biến đang hỏng vẫn đổi Workflow (để resume đi theo flow), lý do nêu đúng
// tệp của flow đích, và băm được ghi ở lần đầu flow nạp được ⇒ sửa flow giữa chừng sau đó bị chặn.
func TestSwitchFromReqToBrokenFlowRecordsHashLater(t *testing.T) {
	h := newHarness(t)
	h.behave = h.tddBehave(0)
	r := h.runner()
	tk, _, err := r.NewTask(context.Background(), NewTaskOptions{Request: "Thêm tính năng phiên bản v2 cho app.txt", Workflow: StepReq})
	if err != nil {
		t.Fatal(err)
	}
	id := tk.State.ID
	h.writeFlow("tdd", "schema: 1\nname: tdd\nsteps:\n  - {id: red, kind: implement, skill: skill-khong-ton-tai}\n")
	res, err := r.Run(context.Background(), RunOptions{Workflow: "tdd", TaskRef: id})
	if err != nil || res.Status != task.StatusBlocked || !strings.Contains(res.Reason, ".bint/flows/tdd.yml") {
		t.Fatalf("flow đích hỏng: %+v %v", res, err)
	}
	if got := loadTask(t, h, id); got.State.Workflow != "tdd" || got.State.FlowSHA != "" {
		t.Fatalf("sau lần chuyển hỏng: %q %q", got.State.Workflow, got.State.FlowSHA)
	}
	// Sửa flow, resume đúng một bước (plan): chạy được và băm được ghi.
	h.writeFlow("tdd", tddFlow)
	if _, err := r.Run(context.Background(), RunOptions{Workflow: "tdd", TaskRef: id, Resume: true, Step: "plan"}); err != nil {
		t.Fatal(err)
	}
	if got := loadTask(t, h, id); got.State.FlowSHA != flow.Hash(tddDef(t)) {
		t.Fatalf("FlowSHA chưa được ghi ở lần đầu flow nạp được: %q", got.State.FlowSHA)
	}
	// Flow bị sửa giữa chừng sau khi đã có băm.
	h.writeFlow("tdd", strings.Replace(tddFlow, "verify: false", "verify: true", 1))
	res, err = r.Run(context.Background(), RunOptions{Workflow: "tdd", TaskRef: id, Resume: true})
	if err != nil || res.Status != task.StatusBlocked || !strings.Contains(res.Reason, "đã đổi từ lúc lập phiếu") {
		t.Fatalf("flow sửa giữa chừng phải bị chặn: %+v %v", res, err)
	}
}
