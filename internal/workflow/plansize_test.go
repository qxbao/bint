package workflow

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"bint/internal/task"
)

// Cổng cỡ kế hoạch và đường nhanh tuỳ chọn (--fast): đường nhanh chỉ chạy khi người dùng chọn; kế hoạch nhỏ thật sự chạy thẳng từ b-req
// sang b-feat; kế hoạch lớn (kể cả có giai đoạn hoãn) luôn dừng chờ người. Mọi biến thể giữ kiểm chứng và review.

const vagueGoalReq = "làm cho nó gọn gàng hơn"

// sizeVariant là một biến thể plan.md/requirement.md do planner giả ghi. want rỗng = kế hoạch nhỏ; ngược lại là chuỗi con bắt buộc có
// trong lý do "kế hoạch lớn:".
type sizeVariant struct {
	name string
	plan func(plan string) string
	req  func(req string) string
	cfg  func(h *harness)
	want []string
}

const (
	goalSection = "## Kiểm tra mục tiêu\n`TestAppV2` quét app.txt, đạt khi ghi v2\n"
	sizeSection = "## Cỡ kế hoạch\nKết luận: nhỏ\n- `app.txt`\n- `done.txt`\n"
)

func files(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "- `f%d.go`\n", i)
	}
	return b.String()
}

func sizeVariants() []sizeVariant {
	return []sizeVariant{
		{name: "a nhỏ hợp lệ"},
		{name: "b có giai đoạn G-1/G-2", want: []string{"giai đoạn"},
			plan: func(p string) string {
				return p + "## Đề xuất tách giai đoạn\n- G-1: làm phần một\n- G-2: làm phần hai\n"
			}},
		{name: "b' một dòng G-1", want: []string{"giai đoạn"},
			plan: func(p string) string { return p + "## Đề xuất tách giai đoạn\n- G-1: làm hết\n" }},
		{name: "c sáu tệp", want: []string{"6 tệp", "ngưỡng"},
			plan: func(p string) string { return strings.Replace(p, "- `app.txt`\n- `done.txt`\n", files(6), 1) }},
		{name: "c' sáu nhóm bề mặt", want: []string{"6 nhóm bề mặt", "ngưỡng"},
			plan: func(p string) string {
				return strings.Replace(p, "- app.txt — AC-1\n- done.txt\n", "- a\n- b\n- c\n- d\n- e\n- f\n", 1)
			}},
		{name: "d thiếu Kiểm tra mục tiêu", want: []string{"Kiểm tra mục tiêu"},
			req: func(r string) string { return strings.Replace(r, goalSection, "", 1) }},
		{name: "d' Kiểm tra mục tiêu không nêu tên test", want: []string{"Kiểm tra mục tiêu", "tên test"},
			req: func(r string) string { return strings.Replace(r, "`TestAppV2` quét", "Quét", 1) }},
		{name: "e chạm migrations/**", want: []string{"bảo vệ", "migrations/**"},
			plan: func(p string) string { return strings.Replace(p, "- `done.txt`\n", "- `migrations/002.sql`\n", 1) }},
		{name: "f thiếu mục Cỡ kế hoạch", want: []string{"thiếu mục"},
			plan: func(p string) string { return strings.Replace(p, sizeSection, "", 1) }},
		{name: "f' kết luận lớn", want: []string{"kết luận kế hoạch lớn"},
			plan: func(p string) string { return strings.Replace(p, "Kết luận: nhỏ", "Kết luận: lớn", 1) }},
		{name: "f'' không liệt kê tệp", want: []string{"không liệt kê tệp"},
			plan: func(p string) string { return strings.Replace(p, "- `app.txt`\n- `done.txt`\n", "", 1) }},
		{name: "c2 sáu tệp trên một dòng", want: []string{"6 tệp", "ngưỡng"},
			plan: func(p string) string {
				return strings.Replace(p, "- `app.txt`\n- `done.txt`\n", "- `a.go`, `b.go`, `c.go`, `d.go`, `e.go`, `f.go`\n", 1)
			}},
		{name: "c3 glob không đếm được", want: []string{"glob"},
			plan: func(p string) string { return strings.Replace(p, "- `done.txt`\n", "- `internal/workflow/**`\n", 1) }},
		{name: "i thiếu mục Bề mặt bị ảnh hưởng", want: []string{"Bề mặt bị ảnh hưởng"},
			plan: func(p string) string {
				return strings.Replace(p, "## Bề mặt bị ảnh hưởng\n- app.txt — AC-1\n- done.txt\n", "", 1)
			}},
		{name: "g ngưỡng 0", want: []string{"ngưỡng"}, cfg: func(h *harness) { h.cfg.Workflow.SmallPlanMaxFiles = 0 }},
		{name: "h ngưỡng 2 đủ cho hai tệp, hai nhóm"},
	}
}

// planner trả hành vi harness: b-req ghi biến thể v, các bước khác như harness mặc định (review qua happy).
func (h *harness) variantPlanner(v sizeVariant) func(context.Context, string, *Env, string) error {
	return func(ctx context.Context, step string, e *Env, id string) error {
		if step != StepReq {
			return h.happy(ctx, step, e, id)
		}
		const crit = "app.txt ghi v2"
		if _, _, err := e.SetTicket(id, TicketEdit{ClearCriteria: true, Criteria: []string{crit}}); err != nil {
			return err
		}
		for n, body := range validDocs(crit, "") {
			switch {
			case n == "plan" && v.plan != nil:
				body = v.plan(body)
			case n == "requirement" && v.req != nil:
				body = v.req(body)
			}
			if _, _, err := e.WriteArtifact(id, n, body); err != nil {
				return err
			}
		}
		return nil
	}
}

func (h *harness) historyHasBlockedBetween(id, from, to string) bool {
	h.t.Helper()
	hist := loadTask(h.t, h, id).State.History
	seenFrom := false
	for _, tr := range hist {
		if tr.To == from {
			seenFrom = true
		}
		if seenFrom && tr.To == task.StatusBlocked {
			return true
		}
		if seenFrom && tr.To == to {
			return false
		}
	}
	return false
}

func count(xs []string, s string) int {
	n := 0
	for _, x := range xs {
		if x == s {
			n++
		}
	}
	return n
}

func newSizeHarness(t *testing.T, v sizeVariant) *harness {
	h := newHarness(t)
	h.cfg.Limits.ProtectedPaths = []string{"migrations/**"}
	if v.name == "h ngưỡng 2 đủ cho hai tệp, hai nhóm" {
		h.cfg.Workflow.SmallPlanMaxFiles = 2
	}
	if v.cfg != nil {
		v.cfg(h)
	}
	h.behave = h.variantPlanner(v)
	return h
}

// checkLargeReason kiểm lý do dừng của kế hoạch lớn.
func checkLargeReason(t *testing.T, name, reason string, want []string) {
	t.Helper()
	if !strings.HasPrefix(reason, "kế hoạch lớn:") {
		t.Errorf("%s: lý do phải mở đầu \"kế hoạch lớn:\": %q", name, reason)
	}
	for _, w := range want {
		if !strings.Contains(reason, w) {
			t.Errorf("%s: lý do thiếu %q: %s", name, w, reason)
		}
	}
}

// Kiểm tra mục tiêu của tác vụ 01M7MHWPT8E8NR27XC30SV6S94: xem requirement.md mục "Kiểm tra mục tiêu".
func TestPlanSizeAndFastGoal(t *testing.T) {
	ctx := context.Background()

	// (1) Không cờ, yêu cầu ngắn mơ hồ, cấu hình mặc định: không đường nhanh, b-req chạy.
	t.Run("1 không cờ đi b-req", func(t *testing.T) {
		h := newSizeHarness(t, sizeVariant{})
		res, err := h.runner().Run(ctx, RunOptions{Workflow: StepDo, Request: vagueGoalReq})
		if err != nil {
			t.Fatal(err)
		}
		c := loadTask(t, h, res.TaskID).State.Classification
		if c == nil || c.Fast || !c.Plan || len(h.calls) == 0 || h.calls[0] != StepReq {
			t.Fatalf("(1) Fast=false và b-req phải chạy: cls=%+v calls=%v", c, h.calls)
		}
	})

	// (2) Cờ --fast: đường nhanh, không b-req, một b-feat, một review.
	t.Run("2 --fast", func(t *testing.T) {
		h := newSizeHarness(t, sizeVariant{})
		res, err := h.runner().Run(ctx, RunOptions{Workflow: StepDo, Request: vagueGoalReq, Fast: true})
		if err != nil {
			t.Fatal(err)
		}
		c := loadTask(t, h, res.TaskID).State.Classification
		if c == nil || !c.Fast || c.Plan || strings.Join(h.calls, ",") != "b-feat,b-review" || res.Status != task.StatusDone {
			t.Fatalf("(2) Fast=true, không b-req, một b-feat, một review, done: cls=%+v calls=%v status=%s (%s)", c, h.calls, res.Status, res.Reason)
		}
		if n := len(loadTask(t, h, res.TaskID).State.Verifications); n < 1 {
			t.Fatalf("(2) phải có kiểm chứng: %d", n)
		}
	})

	// (3) Mọi biến thể bằng driver process.
	for _, v := range sizeVariants() {
		t.Run("3 process "+v.name, func(t *testing.T) {
			h := newSizeHarness(t, v)
			r := h.runner()
			res, err := r.Run(ctx, RunOptions{Workflow: StepDo, Request: vagueGoalReq})
			if err != nil {
				t.Fatal(err)
			}
			if len(v.want) == 0 {
				checkSmall(t, h, v.name, res.TaskID, res.Status, res.Reason, res.Notes, h.calls)
				return
			}
			if res.Status != task.StatusBlocked || strings.Join(h.calls, ",") != StepReq {
				t.Fatalf("%s: phải blocked sau b-req, b-feat chưa chạy: %s %v (%s)", v.name, res.Status, h.calls, res.Reason)
			}
			checkLargeReason(t, v.name, res.Reason, v.want)
			if !h.historyHasBlockedBetween(res.TaskID, task.StatusPlanned, task.StatusInProgress) {
				t.Errorf("%s: lịch sử phải có blocked giữa planned và in_progress", v.name)
			}
			res2, err := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true})
			if err != nil {
				t.Fatal(err)
			}
			want := "b-req,b-goal,b-feat,b-review" // có mục Kiểm tra mục tiêu thì b-goal chạy giữa b-req và b-feat
			if strings.Contains(v.name, "thiếu Kiểm tra mục tiêu") {
				want = "b-req,b-feat,b-review" // phiếu không có mục này giữ hành vi cũ
			}
			if res2.Status != task.StatusDone || strings.Join(h.calls, ",") != want || count(h.calls, StepReq) != 1 {
				t.Fatalf("%s: resume phải chạy b-feat và b-review, không chạy lại b-req: %s calls=%v (%s)", v.name, res2.Status, h.calls, res2.Reason)
			}
		})
	}

	// (4) Driver session: (a) nhỏ và (c) sáu tệp cho cùng kết quả.
	for _, v := range sizeVariants() {
		if v.name != "a nhỏ hợp lệ" && v.name != "c sáu tệp" && v.name != "b có giai đoạn G-1/G-2" {
			continue
		}
		t.Run("4 session "+v.name, func(t *testing.T) {
			h := newSizeHarness(t, v)
			r := h.runner()
			tk := h.newSession(r, NewTaskOptions{Request: vagueGoalReq})
			if c := tk.State.Classification; c.Fast || !c.Plan {
				t.Fatalf("session không cờ phải đi b-req: %+v", c)
			}
			steps, last := h.drive(r, tk.State.ID, false)
			if len(v.want) == 0 {
				checkSmall(t, h, v.name, tk.State.ID, last.Status, last.Reason, nil, steps)
				if last.Action != ActionDone {
					t.Fatalf("%s: phải done: %+v", v.name, last)
				}
				return
			}
			if last.Action != ActionBlocked || strings.Join(steps, ",") != StepReq {
				t.Fatalf("%s: phải blocked sau b-req: steps=%v last=%+v", v.name, steps, last)
			}
			checkLargeReason(t, v.name, last.Reason, v.want)
			if !strings.Contains(last.Reason, "bint run next "+tk.State.ID+" --resume") {
				t.Errorf("%s: lý do phải nêu lệnh resume của driver session: %s", v.name, last.Reason)
			}
			if !h.historyHasBlockedBetween(tk.State.ID, task.StatusPlanned, task.StatusInProgress) {
				t.Errorf("%s: lịch sử phải có blocked giữa planned và in_progress", v.name)
			}
			steps2, last2 := h.drive(r, tk.State.ID, true)
			if strings.Join(steps2, ",") != "b-goal,b-feat,b-review" || last2.Action != ActionDone || count(h.calls, StepReq) != 1 {
				t.Fatalf("%s: resume phải chạy b-feat,b-review rồi done, không chạy lại b-req: %v %+v calls=%v", v.name, steps2, last2, h.calls)
			}
		})
	}
}

// checkSmall kiểm kế hoạch nhỏ: không blocked giữa b-req và b-feat, kiểm chứng và review vẫn chạy, tác vụ done.
func checkSmall(t *testing.T, h *harness, name, id, status, reason string, notes, calls []string) {
	t.Helper()
	if status != task.StatusDone || strings.Join(calls, ",") != "b-req,b-goal,b-feat,b-review" {
		t.Fatalf("%s: kế hoạch nhỏ phải chạy b-req,b-goal,b-feat,b-review rồi done: %s calls=%v (%s)", name, status, calls, reason)
	}
	if h.historyHasBlockedBetween(id, task.StatusPlanned, task.StatusInProgress) {
		t.Errorf("%s: kế hoạch nhỏ không được blocked giữa b-req và b-feat", name)
	}
	st := loadTask(t, h, id).State
	if len(st.Verifications) < 1 || count(calls, StepReview) != 1 {
		t.Errorf("%s: kiểm chứng (%d) và review (%d) vẫn phải chạy", name, len(st.Verifications), count(calls, StepReview))
	}
	if notes != nil && !strings.Contains(strings.Join(notes, "\n"), "cỡ kế hoạch: nhỏ") { // session: notes của complete không nằm ở hành động cuối
		t.Errorf("%s: notes phải ghi \"cỡ kế hoạch: nhỏ\": %v", name, notes)
	}
}

// Mặc định workflow.fast_path tắt: yêu cầu ngắn mơ hồ lập phiếu không cờ không còn Classification.Fast mà đi b-req.
func TestFastPathOffByDefault(t *testing.T) {
	h := newHarness(t)
	if h.cfg.Workflow.FastPath {
		t.Fatal("config.Default() phải tắt workflow.fast_path")
	}
	tk, _, err := h.runner().NewTask(context.Background(), NewTaskOptions{Request: fastReq, Workflow: StepDo})
	if err != nil {
		t.Fatal(err)
	}
	c := tk.State.Classification
	if c.Fast || !c.Plan || c.Reason != task.ReasonVagueShort {
		t.Fatalf("không cờ phải đi b-req, không Fast: %+v", c)
	}
	if tk.State.PlanningContractVersion != task.PlanningContractV1 {
		t.Fatalf("đi b-req thì tác vụ theo hợp đồng v1: %d", tk.State.PlanningContractVersion)
	}
	// Bật tay workflow.fast_path: Bint lại tự chọn từ câu chữ (opt-in giữ hành vi cũ).
	h.cfg.Workflow.FastPath = true
	tk2, _, err := h.runner().NewTask(context.Background(), NewTaskOptions{Request: fastReq, Workflow: StepDo})
	if err != nil || !tk2.State.Classification.Fast {
		t.Fatalf("fast_path: true phải tự chọn đường nhanh: %v %+v", err, tk2.State.Classification)
	}
}

func TestFastFlag(t *testing.T) {
	ctx := context.Background()
	newH := func(t *testing.T) *harness {
		h := newHarness(t)
		h.cfg.Limits.ProtectedPaths = []string{"migrations/**"}
		h.behave = h.reviewCounts(0)
		return h
	}
	t.Run("task new --fast", func(t *testing.T) {
		h := newH(t)
		tk, _, err := h.runner().NewTask(ctx, NewTaskOptions{Request: fastReq, Workflow: StepDo, Fast: true})
		if err != nil {
			t.Fatal(err)
		}
		c := tk.State.Classification
		if !c.Fast || c.Plan || c.Depth != "" || !strings.Contains(c.Reason, "chỉ định đường nhanh") || tk.State.PlanningContractVersion != 0 {
			t.Fatalf("%+v v=%d", c, tk.State.PlanningContractVersion)
		}
	})
	t.Run("--fast với yêu cầu rõ ràng (bỏ b-req nhưng không phải đường nhanh)", func(t *testing.T) {
		h := newH(t)
		tk, _, err := h.runner().NewTask(ctx, NewTaskOptions{Request: "Sửa lỗi nil trong parseTicket", Workflow: StepDo, Fast: true})
		if err != nil || !tk.State.Classification.Fast {
			t.Fatalf("--fast thắng Classify: %v %+v", err, tk.State.Classification)
		}
	})
	t.Run("run b-do --fast", func(t *testing.T) {
		h := newH(t)
		res, err := h.runner().Run(ctx, RunOptions{Workflow: StepDo, Request: fastReq, Fast: true})
		if err != nil || res.Status != task.StatusDone || strings.Join(h.calls, ",") != "b-feat,b-review" {
			t.Fatalf("%+v %v calls=%v", res, err, h.calls)
		}
	})
	t.Run("run b-do --fast --task trên phiếu created", func(t *testing.T) {
		h := newH(t)
		r := h.runner()
		tk, _, err := r.NewTask(ctx, NewTaskOptions{Request: fastReq, Workflow: StepDo})
		if err != nil || tk.State.Classification.Fast {
			t.Fatalf("lập không cờ: %v %+v", err, tk.State.Classification)
		}
		res, err := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: tk.State.ID, Fast: true})
		if err != nil || res.Status != task.StatusDone || strings.Join(h.calls, ",") != "b-feat,b-review" {
			t.Fatalf("%+v %v calls=%v", res, err, h.calls)
		}
		got := loadTask(t, h, tk.State.ID)
		if !got.State.Classification.Fast || got.State.PlanningContractVersion != 0 {
			t.Fatalf("%+v v=%d", got.State.Classification, got.State.PlanningContractVersion)
		}
	})
	t.Run("--fast trên phiếu đã qua b-req bị từ chối", func(t *testing.T) {
		h := newH(t)
		r := h.runner()
		res, err := r.Run(ctx, RunOptions{Workflow: StepReq, Request: fastReq})
		if err != nil || res.Status != task.StatusPlanned {
			t.Fatalf("%+v %v", res, err)
		}
		if _, err := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Fast: true}); err == nil || !strings.Contains(err.Error(), "--fast") {
			t.Fatalf("phải từ chối: %v", err)
		}
	})
	t.Run("xung đột cờ và workflow", func(t *testing.T) {
		h := newH(t)
		r := h.runner()
		for _, tc := range []struct {
			name string
			o    NewTaskOptions
		}{
			{"--plan", NewTaskOptions{Request: fastReq, Workflow: StepDo, Fast: true, Plan: ptr(true)}},
			{"--no-plan", NewTaskOptions{Request: fastReq, Workflow: StepDo, Fast: true, Plan: ptr(false)}},
			{"--depth", NewTaskOptions{Request: fastReq, Workflow: StepDo, Fast: true, Depth: "deep"}},
			{"b-feat", NewTaskOptions{Request: fastReq, Workflow: StepFeat, Fast: true}},
			{"b-req", NewTaskOptions{Request: fastReq, Workflow: StepReq, Fast: true}},
		} {
			if _, _, err := r.NewTask(ctx, tc.o); err == nil || !strings.Contains(err.Error(), "--fast") {
				t.Errorf("%s: phải báo lỗi --fast: %v", tc.name, err)
			}
		}
		tk, _, err := r.NewTask(ctx, NewTaskOptions{Request: fastReq, Workflow: StepDo})
		if err != nil {
			t.Fatal(err)
		}
		for name, o := range map[string]RunOptions{
			"run --plan":     {Workflow: StepDo, TaskRef: tk.State.ID, Fast: true, Plan: ptr(true)},
			"run --no-plan":  {Workflow: StepDo, TaskRef: tk.State.ID, Fast: true, Plan: ptr(false)},
			"run --depth":    {Workflow: StepDo, TaskRef: tk.State.ID, Fast: true, Depth: "standard"},
			"run b-feat":     {Workflow: StepFeat, TaskRef: tk.State.ID, Fast: true},
			"run mới b-feat": {Workflow: StepFeat, Request: fastReq, Fast: true},
		} {
			if _, err := r.Run(ctx, o); err == nil || !strings.Contains(err.Error(), "--fast") {
				t.Errorf("%s: phải báo lỗi --fast: %v", name, err)
			}
		}
		if len(h.calls) != 0 {
			t.Fatalf("lỗi cờ không được chạy agent: %v", h.calls)
		}
	})
	t.Run("phạm vi chạm đường dẫn bảo vệ giữ b-req", func(t *testing.T) {
		h := newH(t)
		r := h.runner()
		tk, _, err := r.NewTask(ctx, NewTaskOptions{Request: fastReq, Workflow: StepDo, Fast: true, Edit: TicketEdit{Allow: []string{"migrations/**"}}})
		if err != nil {
			t.Fatal(err)
		}
		c := tk.State.Classification
		if c.Fast || !c.Plan || !strings.Contains(c.Reason, "migrations/**") || !strings.Contains(c.Reason, "đường nhanh") || c.Depth == "" {
			t.Fatalf("phải giữ b-req kèm lý do: %+v", c)
		}
		// --task + --allow chạm đường dẫn bảo vệ trên phiếu created cũng giữ b-req.
		tk2, _, err := r.NewTask(ctx, NewTaskOptions{Request: fastReq, Workflow: StepDo})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: tk2.State.ID, Fast: true, Edit: TicketEdit{Allow: []string{"migrations/**"}}}); err != nil {
			t.Fatal(err)
		}
		if got := loadTask(t, h, tk2.State.ID).State.Classification; got.Fast || !got.Plan {
			t.Fatalf("--task --fast --allow bảo vệ: %+v", got)
		}
		if len(h.calls) == 0 || h.calls[0] != StepReq {
			t.Fatalf("phải chạy b-req: %v", h.calls)
		}
	})
	t.Run("session --fast", func(t *testing.T) {
		h := newH(t)
		r := h.runner()
		tk := h.newSession(r, NewTaskOptions{Request: fastReq, Fast: true})
		if !tk.State.Classification.Fast {
			t.Fatalf("%+v", tk.State.Classification)
		}
		steps, last := h.drive(r, tk.State.ID, false)
		if strings.Join(steps, ",") != "b-feat,b-review" || last.Action != ActionDone {
			t.Fatalf("%v %+v", steps, last)
		}
	})
}

// Đường nhanh tự chọn (workflow.fast_path: true) vẫn theo bảng điều kiện cũ.
func TestFastPathAutoSelectionTable(t *testing.T) {
	cases := []struct {
		name string
		o    NewTaskOptions
		fast bool
	}{
		{"yêu cầu mơ hồ", NewTaskOptions{Request: fastReq}, true},
		{"--plan", NewTaskOptions{Request: fastReq, Plan: ptr(true)}, false},
		{"--no-plan", NewTaskOptions{Request: fastReq, Plan: ptr(false)}, false},
		{"--depth deep", NewTaskOptions{Request: fastReq, Depth: "deep"}, false},
		{"rủi ro high", NewTaskOptions{Request: fastReq, Edit: TicketEdit{Allow: []string{"migrations/**"}}}, false},
		{"yêu cầu rõ", NewTaskOptions{Request: "Sửa lỗi nil trong parseTicket"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.cfg.Workflow.FastPath = true
			h.cfg.Limits.ProtectedPaths = []string{"migrations/**"}
			tc.o.Workflow = StepDo
			tk, _, err := h.runner().NewTask(context.Background(), tc.o)
			if err != nil {
				t.Fatal(err)
			}
			if got := tk.State.Classification.Fast; got != tc.fast {
				t.Fatalf("Fast=%v, muốn %v: %+v", got, tc.fast, tk.State.Classification)
			}
		})
	}
}

// Có Confirm tương tác: kế hoạch lớn hỏi người; đồng ý thì chạy b-feat luôn, từ chối thì blocked như không có Confirm.
func TestPlanSizeConfirm(t *testing.T) {
	ctx := context.Background()
	large := sizeVariant{name: "c sáu tệp", want: []string{"6 tệp"},
		plan: func(p string) string { return strings.Replace(p, "- `app.txt`\n- `done.txt`\n", files(6), 1) }}
	for _, yes := range []bool{true, false} {
		t.Run(fmt.Sprintf("trả lời %v", yes), func(t *testing.T) {
			h := newSizeHarness(t, large)
			r := h.runner()
			var asked []Question
			r.Confirm = func(q Question) bool { asked = append(asked, q); return yes }
			res, err := r.Run(ctx, RunOptions{Workflow: StepDo, Request: vagueGoalReq})
			if err != nil {
				t.Fatal(err)
			}
			if len(asked) != 1 || asked[0].Key != QConfirmPlanSize || !strings.Contains(asked[0].Text, "6 tệp") {
				t.Fatalf("phải hỏi đúng một lần: %+v", asked)
			}
			if yes {
				if res.Status != task.StatusDone || strings.Join(h.calls, ",") != "b-req,b-goal,b-feat,b-review" ||
					!strings.Contains(strings.Join(res.Notes, "\n"), "người đã đồng ý") {
					t.Fatalf("đồng ý thì đi tiếp: %+v calls=%v", res, h.calls)
				}
				return
			}
			if res.Status != task.StatusBlocked || strings.Join(h.calls, ",") != "b-req" {
				t.Fatalf("từ chối thì blocked: %+v calls=%v", res, h.calls)
			}
			checkLargeReason(t, "từ chối", res.Reason, large.want)
		})
	}
}

// Kế hoạch nhỏ nhưng b-req là phiên riêng: b-feat sau cổng vẫn nối tiếp phiên b-req khi điều kiện nối tiếp đạt, và không dừng chờ giữa hai bước
// (đã kiểm ở TestPlanSizeAndFastGoal); ở đây khoá việc cổng không chạm flow khác b-do gốc: b-req chạy riêng vẫn dừng ở planned.
func TestPlanSizeGateOnlyForBDo(t *testing.T) {
	h := newHarness(t)
	large := sizeVariant{plan: func(p string) string { return strings.Replace(p, "Kết luận: nhỏ", "Kết luận: lớn", 1) }}
	h.behave = h.variantPlanner(large)
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepReq, Request: "Đổi app.txt"})
	if err != nil || res.Status != task.StatusPlanned {
		t.Fatalf("b-req riêng dừng ở planned: %+v %v", res, err)
	}
	if slices.Contains(res.Notes, "cỡ kế hoạch: nhỏ") {
		t.Fatalf("b-req riêng không qua cổng: %v", res.Notes)
	}
}

// Kế hoạch lớn đi qua một lần dừng KHÁC đặt trước cổng (lint b-req, câu hỏi mở): sau khi người/agent sửa rồi resume, cổng vẫn phải dừng
// chờ người trước b-feat (không được vào thẳng b-feat vì tác vụ đã `planned`); resume lần sau mới chạy b-feat. Không chạy lại b-req.
func TestPlanSizeGateNotSkippedByEarlierStops(t *testing.T) {
	ctx := context.Background()
	largePlan := func(p string) string {
		return p + "## Đề xuất tách giai đoạn\n- G-1: phần một\n- G-2: phần hai\n"
	}
	cases := []struct {
		name string
		v    sizeVariant
		// stop là chuỗi con của lý do lần dừng đầu; fix sửa tài liệu rồi để resume.
		stop string
		fix  func(t *testing.T, h *harness, r *Runner, id string)
	}{
		{"lint: thiếu tasks.md", sizeVariant{plan: largePlan}, "artifact b-req chưa đạt hợp đồng",
			func(t *testing.T, h *harness, r *Runner, id string) {
				if _, _, err := r.WriteArtifact(id, "tasks", validDocs("app.txt ghi v2", "")["tasks"]); err != nil {
					t.Fatal(err)
				}
			}},
		{"câu hỏi mở", sizeVariant{plan: largePlan, req: func(r string) string { return r + "- [ ] Q-1: dùng bảng nào?\n" }}, "câu hỏi mở",
			func(t *testing.T, h *harness, r *Runner, id string) {
				req := strings.Replace(validDocs("app.txt ghi v2", "")["requirement"], "## Câu hỏi mở\n", "## Câu hỏi mở\n- [x] Q-1: dùng bảng nào? — Trả lời: bảng cũ\n", 1)
				if _, _, err := r.WriteArtifact(id, "requirement", req); err != nil {
					t.Fatal(err)
				}
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newSizeHarness(t, tc.v)
			base := h.behave
			h.behave = func(ctx context.Context, step string, e *Env, id string) error {
				if step == StepReq && tc.name == "lint: thiếu tasks.md" {
					if err := base(ctx, step, e, id); err != nil {
						return err
					}
					return removeArtifact(h, id, "tasks")
				}
				return base(ctx, step, e, id)
			}
			r := h.runner()
			res, err := r.Run(ctx, RunOptions{Workflow: StepDo, Request: vagueGoalReq})
			if err != nil || res.Status != task.StatusBlocked || !strings.Contains(res.Reason, tc.stop) {
				t.Fatalf("lần dừng đầu: %+v %v (%s)", res.Status, err, res.Reason)
			}
			tc.fix(t, h, r, res.TaskID)
			res2, err := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true})
			if err != nil {
				t.Fatal(err)
			}
			if res2.Status != task.StatusBlocked || count(h.calls, StepFeat) != 0 {
				t.Fatalf("kế hoạch lớn phải dừng ở cổng trước b-feat: %s calls=%v (%s)", res2.Status, h.calls, res2.Reason)
			}
			checkLargeReason(t, tc.name, res2.Reason, []string{"giai đoạn"})
			res3, err := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true})
			if err != nil || res3.Status != task.StatusDone || count(h.calls, StepReq) != 1 || count(h.calls, StepFeat) != 1 {
				t.Fatalf("resume lần hai phải chạy b-feat, không chạy lại b-req: %+v %v calls=%v", res3.Status, err, h.calls)
			}
		})
	}
}

func removeArtifact(h *harness, id, name string) error {
	tk, err := h.tasks.Load(id)
	if err != nil {
		return err
	}
	return os.Remove(tk.ArtifactPath(name))
}

// Quyết định của người dùng (giả định ghi ở docs/workflows.md): workflow.fast_path tắt thì Classify không tự chọn đường nhanh; yêu cầu MƠ HỒ
// bắt buộc đi b-req, còn việc nhỏ và yêu cầu ngắn nêu rõ chỗ sửa vẫn được bỏ b-req như heuristic Classify (không phải đường nhanh).
func TestClassifyAssumptionWithFastPathOff(t *testing.T) {
	cases := []struct {
		name, req string
		wantPlan  bool
	}{
		{"việc nhỏ (chính tả)", "Sửa chính tả trong README", false},
		{"ngắn, rõ (nêu lỗi cụ thể)", "Sửa lỗi nil trong parseTicket", false},
		{"ngắn, mơ hồ", fastReq, true},
		{"mô tả dài", "Thêm tính năng v2 cho app.txt\nvà nhiều thứ nữa\nvà thêm nữa", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			tk, _, err := h.runner().NewTask(context.Background(), NewTaskOptions{Request: tc.req, Workflow: StepDo})
			if err != nil {
				t.Fatal(err)
			}
			c := tk.State.Classification
			if c.Plan != tc.wantPlan || c.Fast {
				t.Fatalf("fast_path tắt, không cờ: Plan=%v muốn %v, Fast=%v (%s)", c.Plan, tc.wantPlan, c.Fast, c.Reason)
			}
			// Cờ tường minh --fast vẫn chọn được đường nhanh.
			tk2, _, err := h.runner().NewTask(context.Background(), NewTaskOptions{Request: tc.req, Workflow: StepDo, Fast: true})
			if err != nil || !tk2.State.Classification.Fast {
				t.Fatalf("--fast phải chọn đường nhanh: %v %+v", err, tk2.State.Classification)
			}
		})
	}
}
