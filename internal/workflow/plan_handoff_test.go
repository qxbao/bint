package workflow

import (
	"context"
	"os"
	"strings"
	"testing"

	"bint/internal/ctxengine"
	"bint/internal/eval"
	"bint/internal/task"
)

// Nối tiếp phiên b-req → b-feat ở driver process (GĐ1 nhóm 1, mục 2): b-feat đầu tiên nối tiếp phiên của b-req khi cùng harness, model,
// profile, cây không đổi và không --fresh; lời nhắn gồm skill b-feat + kế hoạch rút gọn, không task.yml, không khối tri thức.

// handoffRunner dựng Runner có Context giả (trả một mục tri thức) để kiểm đề bài đầy đủ có khối tri thức còn lời nhắn nối tiếp thì không.
func handoffRunner(h *harness) *Runner {
	r := h.runner()
	r.Context = func(context.Context, string, []string) (ctxengine.Pack, error) {
		return ctxengine.Pack{Mode: "task", Entries: []ctxengine.Entry{{ID: "k-test", Type: "rule", Title: "QUY-TAC-THU", Body: "nội dung", Origin: "agent"}}}, nil
	}
	return r
}

func planReqs(h *harness) (plan, feat eval.AgentRequest) {
	h.t.Helper()
	for i, c := range h.calls {
		switch c {
		case StepReq:
			plan = h.reqs[i]
		case StepFeat:
			if feat.Prompt == "" {
				feat = h.reqs[i]
			}
		}
	}
	if plan.Prompt == "" || feat.Prompt == "" {
		h.t.Fatalf("thiếu lần gọi b-req/b-feat: %v", h.calls)
	}
	return plan, feat
}

func runPlanned(t *testing.T, h *harness, mod func(*RunOptions)) *Result {
	t.Helper()
	o := RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(true)}
	if mod != nil {
		mod(&o)
	}
	res, err := handoffRunner(h).Run(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestPlanHandoffResumesPlannerSession(t *testing.T) {
	procSetup(t)
	h := newHarness(t)
	h.claudeLike(nil)
	res := runPlanned(t, h, nil)
	if res.Status != task.StatusDone || strings.Join(h.calls, ",") != "b-req,b-feat,b-review" {
		t.Fatalf("%+v calls=%v", res, h.calls)
	}
	plan, feat := planReqs(h)
	if plan.SessionID == "" || feat.ResumeSessionID != plan.SessionID || feat.SessionID != "" {
		t.Fatalf("b-feat phải nối tiếp phiên b-req %q: %+v", plan.SessionID, feat)
	}
	p := feat.Prompt
	for _, want := range []string{"b-req của tác vụ", "b-feat", "AC-1: app.txt ghi v2", "TSK-1 [AC-1]", "plan.md", "<bint-task id=", "BINT_TASK_ID"} {
		if !strings.Contains(p, want) {
			t.Errorf("lời nhắn nối tiếp thiếu %q:\n%s", want, p)
		}
	}
	for _, not := range []string{"Phiếu tác vụ (task.yml)", "```yaml", "<bint-context>", "Yêu cầu gốc của người dùng"} {
		if strings.Contains(p, not) {
			t.Errorf("lời nhắn nối tiếp không được chép lại %q:\n%s", not, p)
		}
	}
	if !strings.Contains(plan.Prompt, "Phiếu tác vụ (task.yml)") || !strings.Contains(plan.Prompt, "<bint-context>") {
		t.Fatalf("đề bài đầy đủ của b-req phải có phiếu và khối tri thức:\n%s", plan.Prompt)
	}
	if !strings.Contains(strings.Join(res.Notes, "\n"), "b-feat nối tiếp phiên") {
		t.Fatalf("notes phải nêu nối tiếp: %v", res.Notes)
	}
	// LastPlan đã được dùng: không còn trong phiếu.
	if loadTask(t, h, res.TaskID).State.LastPlan != nil {
		t.Fatal("LastPlan phải bị xoá khi b-feat bắt đầu")
	}
}

// Câu hỏi đã trả lời của b-req nằm trong kế hoạch rút gọn.
func TestPlanHandoffCarriesAnsweredQuestions(t *testing.T) {
	procSetup(t)
	h := newHarness(t)
	h.claudeLike(nil)
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepReq {
			if _, _, err := e.SetTicket(id, TicketEdit{ClearCriteria: true, Criteria: []string{"app.txt ghi v2"}}); err != nil {
				return err
			}
			docs := validDocs("app.txt ghi v2", "- [x] Q-1: giữ bao nhiêu ngày? — Trả lời: 30 ngày\n")
			for n, body := range docs {
				if _, _, err := e.WriteArtifact(id, n, body); err != nil {
					return err
				}
			}
			return nil
		}
		return h.happy(ctx, step, e, id)
	}
	res := runPlanned(t, h, nil)
	_, feat := planReqs(h)
	if res.Status != task.StatusDone || feat.ResumeSessionID == "" || !strings.Contains(feat.Prompt, "30 ngày") {
		t.Fatalf("câu hỏi đã trả lời phải vào lời nhắn nối tiếp: %+v\n%s", res.Status, feat.Prompt)
	}
}

// Usage của b-feat chỉ tính phần tăng thêm so với b-req (mốc = usage của b-req).
func TestPlanHandoffUsageBaseIsPlannerUsage(t *testing.T) {
	procSetup(t)
	h := newHarness(t)
	h.claudeLike(nil)
	res := runPlanned(t, h, func(o *RunOptions) { o.Plan = ptr(true) })
	if res.Status != task.StatusDone {
		t.Fatalf("%+v", res)
	}
	// Transcript giả: b-req ghi 1000 vào, b-feat (cùng phiên) ghi thêm 1000 vào: tổng phiên 2000, b-feat chỉ 1000.
	tk := loadTask(t, h, res.TaskID)
	if tk.State.LastImplement == nil || tk.State.LastImplement.Usage == nil || len(tk.State.LastImplement.Usage.Models) != 1 {
		t.Fatalf("LastImplement: %+v", tk.State.LastImplement)
	}
	if got := tk.State.LastImplement.Usage.Models[0].Input; got != 2000 {
		t.Fatalf("usage cộng dồn của phiên sau b-feat = %d, muốn 2000 (b-req 1000 + b-feat 1000)", got)
	}
}

// Các nhánh lùi: chạy đề bài đầy đủ (b-feat không resume) và ghi lý do vào notes.
func TestPlanHandoffFallbacks(t *testing.T) {
	cases := []struct {
		name  string
		setup func(h *harness)
		mod   func(*RunOptions)
		// onFeat chạy ngay khi b-feat bắt đầu (sau khi b-req đã xong, trước quyết định nối tiếp): mô phỏng cây đổi / mất phiên.
		onFeat func(h *harness)
		why    string
	}{
		{"--fresh", nil, func(o *RunOptions) { o.Fresh = true }, nil, "--fresh"},
		{"khác harness", func(h *harness) {
			h.agentNameFor = func(step string) string {
				if step == StepReq {
					return "claude"
				}
				return "codex"
			}
		}, nil, nil, "không nối tiếp chéo"},
		{"cây đổi sau b-req", nil, nil, func(h *harness) { h.write("app.txt", "người sửa chen vào\n") }, "cây làm việc đã đổi"},
		{"mất phiên sau b-req", nil, nil, func(h *harness) {
			if err := os.Remove(h.claudeTranscript(h.lastPlanSession())); err != nil {
				h.t.Fatal(err)
			}
		}, "transcript"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			procSetup(t)
			h := newHarness(t)
			h.claudeLike(nil)
			if tc.setup != nil {
				tc.setup(h)
			}
			r := handoffRunner(h)
			if tc.onFeat != nil {
				r.OnStep = func(ev StepEvent) {
					if ev.Step == StepFeat && !ev.Done {
						tc.onFeat(h)
					}
				}
			}
			o := RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(true)}
			if tc.mod != nil {
				tc.mod(&o)
			}
			res, err := r.Run(context.Background(), o)
			if err != nil {
				t.Fatal(err)
			}
			_, feat := planReqs(h)
			if feat.ResumeSessionID != "" || !strings.Contains(feat.Prompt, "Phiếu tác vụ (task.yml)") {
				t.Fatalf("phải chạy đề bài đầy đủ, không resume: resume=%q\n%s", feat.ResumeSessionID, feat.Prompt)
			}
			if !strings.Contains(strings.Join(res.Notes, "\n"), tc.why) {
				t.Fatalf("notes thiếu lý do %q: %v", tc.why, res.Notes)
			}
		})
	}
}

// lastPlanSession là phiên của lần gọi b-req gần nhất (harness giả claude).
func (h *harness) lastPlanSession() string {
	h.t.Helper()
	for i := len(h.calls) - 1; i >= 0; i-- {
		if h.calls[i] == StepReq {
			return h.reqs[i].SessionID
		}
	}
	h.t.Fatal("chưa có lần gọi b-req")
	return ""
}

// Model và harness giống nhau (cùng giá trị, khác rỗng) vẫn nối tiếp.
func TestPlanHandoffSameModelResumes(t *testing.T) {
	procSetup(t)
	h := newHarness(t)
	h.claudeLike(nil)
	h.models = map[string]string{StepReq: "model-a", StepFeat: "model-a"}
	runPlanned(t, h, nil)
	plan, feat := planReqs(h)
	if feat.ResumeSessionID == "" || feat.ResumeSessionID != plan.SessionID {
		t.Fatalf("cùng model phải nối tiếp: %+v", feat)
	}
}

// Đổi model giữa b-req và b-feat (cùng harness claudeLike): b-feat vẫn resume phiên b-req, agent dựng với model mới, notes ghi
// model cũ → mới và việc cache prompt dựng lại; cùng model thì không có dòng cache.
func TestPlanHandoffModelSwitchResumes(t *testing.T) {
	procSetup(t)
	h := newHarness(t)
	h.claudeLike(nil)
	h.models = map[string]string{StepReq: "model-a", StepFeat: "model-b"}
	// Bọc factory để đo model mà agent mỗi bước được dựng.
	r := handoffRunner(h)
	built := map[string]string{}
	inner := r.Agent
	r.Agent = func(spec StepSpec, maxCost *float64, maxTurns *int) (eval.Agent, error) {
		a, err := inner(spec, maxCost, maxTurns)
		if mn, ok := a.(eval.ModelNamer); ok {
			built[spec.ID] = mn.RequestedModel()
		}
		return a, err
	}
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(true)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != task.StatusDone || strings.Join(h.calls, ",") != "b-req,b-feat,b-review" {
		t.Fatalf("%+v calls=%v", res, h.calls)
	}
	plan, feat := planReqs(h)
	if plan.SessionID == "" || feat.ResumeSessionID != plan.SessionID {
		t.Fatalf("b-feat phải resume phiên b-req %q: %+v", plan.SessionID, feat)
	}
	if strings.Contains(feat.Prompt, "Phiếu tác vụ (task.yml)") {
		t.Fatalf("prompt phải là lời nhắn handoff:\n%s", feat.Prompt)
	}
	notes := strings.Join(res.Notes, "\n")
	if !strings.Contains(notes, "b-feat nối tiếp phiên") {
		t.Fatalf("notes thiếu dòng nối tiếp: %v", res.Notes)
	}
	cacheLine := ""
	for _, n := range res.Notes {
		if strings.Contains(n, "model-a") && strings.Contains(n, "model-b") && strings.Contains(n, "cache") {
			cacheLine = n
		}
	}
	if cacheLine == "" {
		t.Fatalf("notes thiếu dòng model-a → model-b kèm cache: %v", res.Notes)
	}
	if got := built[StepFeat]; got != "model-b" {
		t.Fatalf("agent b-feat dựng với model %q, muốn model-b", got)
	}
}

// Cùng model: không có dòng cache trong notes.
func TestPlanHandoffSameModelNoCacheNote(t *testing.T) {
	procSetup(t)
	h := newHarness(t)
	h.claudeLike(nil)
	h.models = map[string]string{StepReq: "model-a", StepFeat: "model-a"}
	res := runPlanned(t, h, nil)
	_, feat := planReqs(h)
	if feat.ResumeSessionID == "" {
		t.Fatalf("cùng model phải nối tiếp: %+v", feat)
	}
	if strings.Contains(strings.Join(res.Notes, "\n"), "cache") {
		t.Fatalf("cùng model không được có dòng cache: %v", res.Notes)
	}
}

// Bảng quyết định thuần cho lớp plan_handoff.
func TestContinuationDecisionPlanHandoff(t *testing.T) {
	last := func(mod func(*task.StepStop)) *task.StepStop {
		l := &task.StepStop{Step: StepFeat, StopClass: StopPlanHandoff, SessionID: "s", Fingerprint: "fp", Continuable: true, Via: "claude", Model: "m"}
		if mod != nil {
			mod(l)
		}
		return l
	}
	cases := []struct {
		name string
		f    ContinueFacts
		want bool
		why  string
	}{
		{"cùng harness/model/profile", ContinueFacts{Last: last(nil), Step: StepFeat, Via: "claude", Model: "m", Fingerprint: "fp"}, true, ""},
		{"khác model (claude)", ContinueFacts{Last: last(nil), Step: StepFeat, Via: "claude", Model: "n", Fingerprint: "fp"}, true, ""},
		{"khác model (codex)", ContinueFacts{Last: last(func(l *task.StepStop) { l.Via = "codex" }), Step: StepFeat, Via: "codex", Model: "n", Fingerprint: "fp"}, true, ""},
		{"model rỗng → có model", ContinueFacts{Last: last(func(l *task.StepStop) { l.Model = "" }), Step: StepFeat, Via: "claude", Model: "n", Fingerprint: "fp"}, true, ""},
		{"model có → rỗng", ContinueFacts{Last: last(nil), Step: StepFeat, Via: "claude", Model: "", Fingerprint: "fp"}, true, ""},
		{"khác model, subagent (chưa kiểm) vẫn chặn", ContinueFacts{Last: last(func(l *task.StepStop) { l.Via = ViaSubagent }), Step: StepFeat, Via: ViaSubagent, Model: "n", Fingerprint: "fp"}, false, "model m"},
		{"khác model + khác profile", ContinueFacts{Last: last(func(l *task.StepStop) { l.Profile = "claude/a" }), Step: StepFeat, Via: "claude", Model: "n", Profile: "claude/b", Fingerprint: "fp"}, false, "claude/a"},
		{"khác model + khác harness", ContinueFacts{Last: last(nil), Step: StepFeat, Via: "codex", Model: "n", Fingerprint: "fp"}, false, "không nối tiếp chéo"},
		{"khác model + cây đổi", ContinueFacts{Last: last(nil), Step: StepFeat, Via: "claude", Model: "n", Fingerprint: "fp2"}, false, "đã đổi"},
		{"khác model + --fresh", ContinueFacts{Last: last(nil), Step: StepFeat, Via: "claude", Model: "n", Fingerprint: "fp", Fresh: true}, false, "--fresh"},
		{"khác harness", ContinueFacts{Last: last(nil), Step: StepFeat, Via: "codex", Model: "m", Fingerprint: "fp"}, false, "không nối tiếp chéo"},
		{"khác profile", ContinueFacts{Last: last(func(l *task.StepStop) { l.Profile = "claude/a" }), Step: StepFeat, Via: "claude", Model: "m", Profile: "claude/b", Fingerprint: "fp"}, false, "claude/a"},
		{"cây đổi", ContinueFacts{Last: last(nil), Step: StepFeat, Via: "claude", Model: "m", Fingerprint: "fp2"}, false, "đã đổi"},
		{"không có fingerprint", ContinueFacts{Last: last(func(l *task.StepStop) { l.Fingerprint = "" }), Step: StepFeat, Via: "claude", Model: "m", Fingerprint: "fp"}, false, "fingerprint"},
		{"--fresh", ContinueFacts{Last: last(nil), Step: StepFeat, Via: "claude", Model: "m", Fingerprint: "fp", Fresh: true}, false, "--fresh"},
		{"không có phiên", ContinueFacts{Last: last(func(l *task.StepStop) { l.Continuable = false }), Step: StepFeat, Via: "claude", Model: "m", Fingerprint: "fp"}, false, "không nối tiếp được"},
		{"bước sắp mở không phải implement của handoff", ContinueFacts{Last: last(nil), Step: StepReview, Via: "claude", Model: "m", Fingerprint: "fp"}, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := continuationDecision(tc.f)
			if d.Continue != tc.want || !strings.Contains(d.Why, tc.why) {
				t.Fatalf("%+v, muốn continue=%v why chứa %q", d, tc.want, tc.why)
			}
		})
	}
}
