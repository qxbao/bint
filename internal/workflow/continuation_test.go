package workflow

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/harness/claude"
	"bint/internal/harness/hookcore"
	"bint/internal/task"
)

func TestContinuationDecision(t *testing.T) {
	last := func(mod func(l *task.StepStop)) *task.StepStop {
		l := &task.StepStop{Step: StepFeat, RunID: "r1", StopClass: StopExternal, SessionID: "A1", Fingerprint: "fp", Continuable: true, Via: ViaSubagent}
		if mod != nil {
			mod(l)
		}
		return l
	}
	for _, tc := range []struct {
		name string
		f    ContinueFacts
		want bool
		why  string // chuỗi con của Why khi không nối tiếp
	}{
		{"nối tiếp external", ContinueFacts{Last: last(nil), Step: StepFeat, Fingerprint: "fp"}, true, ""},
		{"fresh", ContinueFacts{Last: last(nil), Step: StepFeat, Fingerprint: "fp", Fresh: true}, false, "--fresh"},
		{"không có LastStep", ContinueFacts{Step: StepFeat, Fingerprint: "fp"}, false, ""},
		{"Via rỗng (phiếu cũ/driver process)", ContinueFacts{Last: last(func(l *task.StepStop) { l.Via = "" }), Step: StepFeat, Fingerprint: "fp"}, false, "subagent"},
		{"không continuable", ContinueFacts{Last: last(func(l *task.StepStop) { l.Continuable = false }), Step: StepFeat, Fingerprint: "fp"}, false, "không nối tiếp được"},
		{"khác bước", ContinueFacts{Last: last(nil), Step: StepReview, Fingerprint: "fp"}, false, "khác bước"},
		{"fingerprint đổi", ContinueFacts{Last: last(nil), Step: StepFeat, Fingerprint: "fp2"}, false, "đã đổi"},
		{"không chụp được fingerprint", ContinueFacts{Last: last(nil), Step: StepFeat}, false, "fingerprint"},
		{"external lần 3 còn được", ContinueFacts{Last: last(func(l *task.StepStop) { l.Continuations = 2 }), Step: StepFeat, Fingerprint: "fp"}, true, ""},
		{"external quá 3 lần", ContinueFacts{Last: last(func(l *task.StepStop) { l.Continuations = 3 }), Step: StepFeat, Fingerprint: "fp"}, false, "tối đa 3"},
		{"timeout lần đầu", ContinueFacts{Last: last(func(l *task.StepStop) { l.StopClass = StopTimeout }), Step: StepFeat, Fingerprint: "fp"}, true, ""},
		{"timeout đã nối tiếp một lần", ContinueFacts{Last: last(func(l *task.StepStop) { l.StopClass, l.Continuations = StopTimeout, 1 }), Step: StepFeat, Fingerprint: "fp"}, false, "quá giờ"},
		{"awaiting_human của plan, bộ đếm cao vẫn nối tiếp", ContinueFacts{Last: last(func(l *task.StepStop) {
			l.Step, l.StopClass, l.Continuations = StepReq, StopAwaitingHuman, 9
		}), Step: StepReq, LastIsPlan: true, Fingerprint: "fp"}, true, ""},
		{"awaiting_human không phải plan", ContinueFacts{Last: last(func(l *task.StepStop) { l.StopClass = StopAwaitingHuman }), Step: StepFeat, Fingerprint: "fp"}, false, "chỉ bước plan"},
		{"intrinsic", ContinueFacts{Last: last(func(l *task.StepStop) { l.StopClass, l.Continuable = StopIntrinsic, false }), Step: StepFeat, Fingerprint: "fp"}, false, "intrinsic"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := continuationDecision(tc.f)
			if d.Continue != tc.want {
				t.Fatalf("Continue = %v, muốn %v (%+v)", d.Continue, tc.want, d)
			}
			if !tc.want && !strings.Contains(d.Why, tc.why) {
				t.Fatalf("Why = %q, muốn chứa %q", d.Why, tc.why)
			}
			if tc.want && d.Step != tc.f.Last.Step {
				t.Fatalf("Step = %q", d.Step)
			}
		})
	}
}

// failFeatSession mở b-feat, báo thất bại bằng lý do hạ tầng với agent A; trả hành động đã mở.
func failFeatSession(t *testing.T, h *harness, r *Runner, id, agent string) *SessionAction {
	t.Helper()
	ctx := context.Background()
	a, err := r.Next(ctx, NextOptions{TaskRef: id, Resume: false})
	if err != nil || a.Action != ActionAgent {
		t.Fatalf("next: %+v %v", a, err)
	}
	return completeFailed(t, r, id, a, agent)
}

func completeFailed(t *testing.T, r *Runner, id string, a *SessionAction, agent string) *SessionAction {
	t.Helper()
	c, err := r.Complete(context.Background(), CompleteOptions{TaskRef: id, RunID: a.RunID, AgentID: agent, Failed: "API Error: 529 overloaded"})
	if err != nil || c.Action != ActionFailed {
		t.Fatalf("complete: %+v %v", c, err)
	}
	return c
}

func sessionStepFile(h *harness, runID string) string {
	return hookcore.SessionStepPath(filepath.Dir(h.tasks.Dir), runID)
}

func TestSessionCompleteFailedInfraIsExternalContinuable(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	failFeatSession(t, h, r, tk.State.ID, "A1")
	ls := loadTask(t, h, tk.State.ID).State.LastStep
	if ls == nil || ls.StopClass != StopExternal || !ls.Continuable || ls.Via != ViaSubagent || ls.SessionID != "A1" || ls.Step != StepFeat {
		t.Fatalf("LastStep: %+v", ls)
	}
	// Lý do thường vẫn là intrinsic.
	tk2 := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	a, _ := r.Next(context.Background(), NextOptions{TaskRef: tk2.State.ID})
	if _, err := r.Complete(context.Background(), CompleteOptions{TaskRef: tk2.State.ID, RunID: a.RunID, AgentID: "A2", Failed: "test đỏ"}); err != nil {
		t.Fatal(err)
	}
	if ls := loadTask(t, h, tk2.State.ID).State.LastStep; ls == nil || ls.StopClass != StopIntrinsic || ls.Continuable {
		t.Fatalf("intrinsic: %+v", ls)
	}
}

func TestSessionResumeContinuesSameAgent(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	id := tk.State.ID
	first := failFeatSession(t, h, r, id, "A1")
	_ = first
	ctx := context.Background()
	a, err := r.Next(ctx, NextOptions{TaskRef: id, Resume: true})
	if err != nil || a.Action != ActionAgent || a.Step != StepFeat {
		t.Fatalf("resume: %+v %v", a, err)
	}
	if a.ContinueAgentID != "A1" || !strings.Contains(a.Prompt, "Tiếp tục bước "+StepFeat) || !strings.Contains(a.Prompt, a.RunID) {
		t.Fatalf("nối tiếp: %q %q", a.ContinueAgentID, a.Prompt)
	}
	if strings.Contains(a.Prompt, "<bint-task") || !hasNote(a.Notes, "SendMessage") {
		t.Fatalf("lời nhắn phải ngắn, notes hướng dẫn SendMessage: %v\n%s", a.Notes, a.Prompt)
	}
	ps := loadTask(t, h, id).State.PendingStep
	if ps == nil || ps.ContinueAgentID != "A1" || ps.FullPrompt == "" || ps.Continuations != 1 || ps.OriginRunID != first.Result.RunID {
		t.Fatalf("PendingStep: %+v", ps)
	}
	// Gọi lại next trả đúng bước nối tiếp đó.
	b, err := r.Next(ctx, NextOptions{TaskRef: id})
	if err != nil || b.RunID != a.RunID || b.ContinueAgentID != "A1" {
		t.Fatalf("gọi lại: %+v %v", b, err)
	}
	// Alias guardrail cho lượt gốc tồn tại cùng tệp của lượt mới; gỡ cả hai khi complete.
	if _, err := os.Stat(sessionStepFile(h, a.RunID)); err != nil {
		t.Fatalf("thiếu tệp session-steps của lượt mới: %v", err)
	}
	if _, err := os.Stat(sessionStepFile(h, first.Result.RunID)); err != nil {
		t.Fatalf("thiếu alias của lượt gốc: %v", err)
	}
	h.subagent(a)
	if _, err := r.Complete(ctx, CompleteOptions{TaskRef: id, RunID: a.RunID, AgentID: "A1"}); err != nil {
		t.Fatal(err)
	}
	for _, run := range []string{a.RunID, first.Result.RunID} {
		if _, err := os.Stat(sessionStepFile(h, run)); !os.IsNotExist(err) {
			t.Fatalf("tệp session-steps %s còn sau complete: %v", run, err)
		}
	}
}

func TestSessionContinuationLimitThree(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	id := tk.State.ID
	failFeatSession(t, h, r, id, "A1")
	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		a, err := r.Next(ctx, NextOptions{TaskRef: id, Resume: true})
		if err != nil || a.ContinueAgentID != "A1" {
			t.Fatalf("lần nối tiếp %d: %+v %v", i, a, err)
		}
		completeFailed(t, r, id, a, "A1")
		if ls := loadTask(t, h, id).State.LastStep; ls == nil || ls.Continuations != i || !ls.Continuable {
			t.Fatalf("sau lần %d: %+v", i, ls)
		}
	}
	a, err := r.Next(ctx, NextOptions{TaskRef: id, Resume: true})
	if err != nil || a.Action != ActionAgent || a.ContinueAgentID != "" || !strings.Contains(a.Prompt, "<bint-task") || !hasNote(a.Notes, "tối đa 3") {
		t.Fatalf("lần 4 phải là đề bài đầy đủ: %+v %v", a, err)
	}
}

func TestSessionContinuationFingerprintChanged(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	id := tk.State.ID
	failFeatSession(t, h, r, id, "A1")
	h.write("other.txt", "đổi cây\n")
	a, err := r.Next(context.Background(), NextOptions{TaskRef: id, Resume: true})
	if err != nil || a.ContinueAgentID != "" || !strings.Contains(a.Prompt, "<bint-task") || !hasNote(a.Notes, "đã đổi") {
		t.Fatalf("cây đổi: %+v %v", a, err)
	}
}

func TestSessionFreshSwapsPendingContinuation(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	id := tk.State.ID
	first := failFeatSession(t, h, r, id, "A1")
	ctx := context.Background()
	a, _ := r.Next(ctx, NextOptions{TaskRef: id, Resume: true})
	if a.ContinueAgentID != "A1" {
		t.Fatalf("%+v", a)
	}
	f, err := r.Next(ctx, NextOptions{TaskRef: id, Fresh: true})
	if err != nil || f.RunID != a.RunID || f.ContinueAgentID != "" || !strings.Contains(f.Prompt, "<bint-task") || !hasNote(f.Notes, "--fresh") {
		t.Fatalf("--fresh: %+v %v", f, err)
	}
	ps := loadTask(t, h, id).State.PendingStep
	if ps == nil || ps.ContinueAgentID != "" || ps.FullPrompt != "" || ps.UsageBase != nil || ps.OriginRunID != "" {
		t.Fatalf("PendingStep: %+v", ps)
	}
	if _, err := os.Stat(sessionStepFile(h, first.Result.RunID)); !os.IsNotExist(err) {
		t.Fatalf("alias phải gỡ: %v", err)
	}
	// Gọi lại không --fresh vẫn trả đề bài đầy đủ.
	g, _ := r.Next(ctx, NextOptions{TaskRef: id})
	if g.ContinueAgentID != "" || g.Prompt != f.Prompt {
		t.Fatalf("%+v", g)
	}
}

func TestSessionFreshWithoutPendingUsesFullPrompt(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	id := tk.State.ID
	failFeatSession(t, h, r, id, "A1")
	a, err := r.Next(context.Background(), NextOptions{TaskRef: id, Resume: true, Fresh: true})
	if err != nil || a.Action != ActionAgent || a.ContinueAgentID != "" || !strings.Contains(a.Prompt, "<bint-task") {
		t.Fatalf("%+v %v", a, err)
	}
}

func TestSessionLegacyLastStepWithoutViaNotContinued(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	id := tk.State.ID
	failFeatSession(t, h, r, id, "A1")
	if _, err := r.Tasks.Update(id, r.now(), func(t *task.Task) error { t.State.LastStep.Via = ""; return nil }); err != nil {
		t.Fatal(err)
	}
	a, _ := r.Next(context.Background(), NextOptions{TaskRef: id, Resume: true})
	if a.ContinueAgentID != "" || !strings.Contains(a.Prompt, "<bint-task") {
		t.Fatalf("%+v", a)
	}
}

// --- vòng câu hỏi nhiều lượt của b-req ---

const openQ2 = "- [ ] Q-2: dùng khoá nào?\n"
const answeredQ2 = "- [x] Q-2: dùng khoá nào? — Trả lời: khoá id\n"

func writeReq(t *testing.T, r *Runner, id, extra string) {
	t.Helper()
	if _, _, err := r.WriteArtifact(id, "requirement", validDocs("app.txt ghi v2", extra)["requirement"]); err != nil {
		t.Fatal(err)
	}
}

func TestSessionRequirementQuestionLoopReturnsToPlanner(t *testing.T) {
	h := newHarness(t)
	h.behave = h.reqWithQuestion(openQ)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{})
	id := tk.State.ID
	ctx := context.Background()

	a, _ := r.Next(ctx, NextOptions{TaskRef: id})
	if a.Step != StepReq {
		t.Fatalf("%+v", a)
	}
	h.subagent(a)
	c, err := r.Complete(ctx, CompleteOptions{TaskRef: id, RunID: a.RunID, AgentID: "P1"})
	if err != nil || c.Action != ActionBlocked || len(c.Questions) != 1 {
		t.Fatalf("complete: %+v %v", c, err)
	}
	ls := loadTask(t, h, id).State.LastStep
	if ls == nil || ls.StopClass != StopAwaitingHuman || ls.Via != ViaSubagent || ls.SessionID != "P1" || !ls.Continuable {
		t.Fatalf("LastStep: %+v", ls)
	}
	// Chưa trả lời: resume giữ hành vi cũ (chặn lại, không mở planner).
	if b, _ := r.Next(ctx, NextOptions{TaskRef: id, Resume: true}); b.Action != ActionBlocked {
		t.Fatalf("chưa trả lời: %+v", b)
	}

	// Vòng 1: trả lời Q-1 → nối tiếp planner cũ với nguyên văn dòng Q-1.
	writeReq(t, r, id, answeredQ)
	b, err := r.Next(ctx, NextOptions{TaskRef: id, Resume: true})
	if err != nil || b.Action != ActionAgent || b.Step != StepReq || b.ContinueAgentID != "P1" || b.Subagent != SubagentPlanner {
		t.Fatalf("nối tiếp planner: %+v %v", b, err)
	}
	if !strings.Contains(b.Prompt, strings.TrimSpace(answeredQ)) {
		t.Fatalf("prompt thiếu dòng Q-1 nguyên văn:\n%s", b.Prompt)
	}
	// Planner thêm Q-2 → complete chặn lại.
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		_, _, err := e.WriteArtifact(id, "requirement", validDocs("app.txt ghi v2", answeredQ+openQ2)["requirement"])
		return err
	}
	h.subagent(b)
	c, err = r.Complete(ctx, CompleteOptions{TaskRef: id, RunID: b.RunID, AgentID: "P1"})
	if err != nil || c.Action != ActionBlocked || len(c.Questions) != 1 || !strings.Contains(c.Questions[0], "Q-2") {
		t.Fatalf("vòng 2 phải chặn: %+v %v", c, err)
	}

	// Vòng 2: trả lời Q-2 → nối tiếp lần nữa, prompt có cả hai dòng.
	writeReq(t, r, id, answeredQ+answeredQ2)
	d, err := r.Next(ctx, NextOptions{TaskRef: id, Resume: true})
	if err != nil || d.ContinueAgentID != "P1" || d.Step != StepReq ||
		!strings.Contains(d.Prompt, strings.TrimSpace(answeredQ)) || !strings.Contains(d.Prompt, strings.TrimSpace(answeredQ2)) {
		t.Fatalf("vòng 2 nối tiếp: %+v %v", d, err)
	}
	// Planner không thêm câu hỏi: hết vòng, bước kế là b-feat với đề bài chứa các dòng Q đã trả lời.
	h.behave = h.happy
	h.subagent(d)
	writeReq(t, r, id, answeredQ+answeredQ2)
	if c, err = r.Complete(ctx, CompleteOptions{TaskRef: id, RunID: d.RunID, AgentID: "P1"}); err != nil || c.Action != "" {
		t.Fatalf("hết câu hỏi: %+v %v", c, err)
	}
	f, err := r.Next(ctx, NextOptions{TaskRef: id})
	if err != nil || f.Step != StepFeat || f.ContinueAgentID != "" ||
		!strings.Contains(f.Prompt, strings.TrimSpace(answeredQ)) || !strings.Contains(f.Prompt, strings.TrimSpace(answeredQ2)) {
		t.Fatalf("b-feat: %+v %v", f, err)
	}
	if got := countCalls(h, StepReq); got != 3 {
		t.Fatalf("b-req chạy %d lần, muốn 3 (gốc + 2 nối tiếp)", got)
	}
}

// Không nối tiếp được (không handle / --fresh / cây đổi): resume vào thẳng b-feat như trước, không chạy lại b-req.
func TestSessionAnsweredQuestionNoContinuationGoesToFeat(t *testing.T) {
	cases := map[string]struct {
		agent  string
		fresh  bool
		mutate func(h *harness)
	}{
		"không có handle": {agent: ""},
		"--fresh":         {agent: "P1", fresh: true},
		"cây đổi":         {agent: "P1", mutate: func(h *harness) { h.write("other.txt", "đổi\n") }},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.behave = h.reqWithQuestion(openQ)
			r := h.runner()
			tk := h.newSession(r, NewTaskOptions{})
			id := tk.State.ID
			ctx := context.Background()
			a, _ := r.Next(ctx, NextOptions{TaskRef: id})
			h.subagent(a)
			if cc, err := r.Complete(ctx, CompleteOptions{TaskRef: id, RunID: a.RunID, AgentID: c.agent}); err != nil || cc.Action != ActionBlocked {
				t.Fatalf("%+v %v", cc, err)
			}
			writeReq(t, r, id, answeredQ)
			if c.mutate != nil {
				c.mutate(h)
			}
			b, err := r.Next(ctx, NextOptions{TaskRef: id, Resume: true, Fresh: c.fresh})
			if err != nil || b.Action != ActionAgent || b.Step != StepFeat || b.ContinueAgentID != "" {
				t.Fatalf("phải vào thẳng b-feat: %+v %v", b, err)
			}
			if !strings.Contains(b.Prompt, strings.TrimSpace(answeredQ)) {
				t.Fatalf("đề bài b-feat thiếu dòng Q đã trả lời:\n%s", b.Prompt)
			}
		})
	}
}

func TestFeatPromptWithoutAnswersUnchanged(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	a, _ := r.Next(context.Background(), NextOptions{TaskRef: tk.State.ID})
	if strings.Contains(a.Prompt, "Câu hỏi đã trả lời") {
		t.Fatalf("không có câu trả lời thì đề bài không đổi:\n%s", a.Prompt)
	}
}

// --- chi phí của bước nối tiếp ---

func writeTranscript(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSessionContinuationCostCountsOnlyNewUsage(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	id := tk.State.ID
	ctx := context.Background()
	tr := filepath.Join(t.TempDir(), "agent-A1.jsonl")
	m1 := `{"type":"assistant","message":{"id":"m1","model":"claude-sonnet-5-5","usage":{"input_tokens":1000,"output_tokens":500}}}`
	m2 := `{"type":"assistant","message":{"id":"m2","model":"claude-sonnet-5-5","usage":{"input_tokens":2000,"output_tokens":1500}}}`
	rec := func(path string) {
		if err := hookcore.WriteSubagentRecord(filepath.Dir(h.tasks.Dir), hookcore.SubagentRecord{AgentID: "A1", AgentType: "bint-implementer", TranscriptPath: path}); err != nil {
			t.Fatal(err)
		}
	}
	writeTranscript(t, tr, m1)
	rec(tr)
	a, _ := r.Next(ctx, NextOptions{TaskRef: id})
	c1 := completeFailed(t, r, id, a, "A1")
	cost1 := *c1.Result.CostUSD
	if cost1 <= 0 {
		t.Fatalf("lượt đầu phải có chi phí: %v", cost1)
	}
	// Nối tiếp cùng transcript, dài thêm m2: chỉ tính m2.
	b, _ := r.Next(ctx, NextOptions{TaskRef: id, Resume: true})
	if b.ContinueAgentID != "A1" {
		t.Fatalf("%+v", b)
	}
	writeTranscript(t, tr, m1, m2)
	c2 := completeFailed(t, r, id, b, "A1")
	if c2.Result.CostUSD == nil {
		t.Fatal("thiếu chi phí")
	}
	m2only := claude.Usage{Turns: 1, Models: []claude.ModelUsage{{Model: "claude-sonnet-5-5", Input: 2000, Output: 1500}}}
	wantDelta := *m2only.Cost(nil)
	if math.Abs(*c2.Result.CostUSD-wantDelta) > 1e-9 {
		t.Fatalf("chi phí nối tiếp = %v, muốn chỉ phần mới %v (tính trùng nếu = %v)", *c2.Result.CostUSD, wantDelta, wantDelta+cost1)
	}
	if got := loadTask(t, h, id).State.SessionSpend; got == nil || math.Abs(got.Cost-wantDelta) > 1e-9 || got.Turns != 1 { // lượt cha mới sau mỗi lần dừng
		t.Fatalf("SessionSpend: %+v", got)
	}
	// Transcript khác tệp: không trừ mốc cũ (là agent mới).
	d, _ := r.Next(ctx, NextOptions{TaskRef: id, Resume: true})
	tr2 := filepath.Join(t.TempDir(), "agent-A1-b.jsonl")
	writeTranscript(t, tr2, m2)
	rec(tr2)
	c3 := completeFailed(t, r, id, d, "A1")
	if math.Abs(*c3.Result.CostUSD-wantDelta) > 1e-9 {
		t.Fatalf("khác transcript phải tính đủ: %v muốn %v", *c3.Result.CostUSD, wantDelta)
	}
}

// Transcript bị cắt ngắn dưới mốc: sàn 0, không ra giá trị âm.
func TestUsageSinceFloorsAtZero(t *testing.T) {
	u := &claude.Usage{Turns: 1, Models: []claude.ModelUsage{{Model: "m", Input: 10, Output: 5}}}
	base := &task.UsageMark{Turns: 4, Models: []task.UsageModel{{Model: "m", Input: 100, Output: 50}}}
	d := usageSince(u, base)
	if d.Turns != 0 || len(d.Models) != 0 {
		t.Fatalf("%+v", d)
	}
}

// Phiếu cũ không có trường mới vẫn nạp được; trường mới round-trip.
func TestLegacyPendingStepJSON(t *testing.T) {
	var ps task.PendingStep
	if err := json.Unmarshal([]byte(`{"run_id":"r","step":"b-feat","mode":"exclusive","prompt":"p","started_at":"x","lease_until":"y"}`), &ps); err != nil {
		t.Fatal(err)
	}
	if ps.ContinueAgentID != "" || ps.UsageBase != nil {
		t.Fatalf("%+v", ps)
	}
}

// Alias guardrail của lượt gốc bị gỡ cùng tệp của bước khi bước nối tiếp bị bỏ (abort) hoặc tác vụ bị huỷ.
func TestSessionContinuationAliasRemovedOnAbortAndCancel(t *testing.T) {
	for name, drop := range map[string]func(r *Runner, id string) error{
		"abort": func(r *Runner, id string) error { _, err := r.Abort(context.Background(), id, "bỏ"); return err },
		"cancel": func(r *Runner, id string) error {
			_, _, err := r.SetStatus(context.Background(), id, task.StatusCancelled, TransitionOptions{Reason: "bỏ"})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			r := h.runner()
			tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
			id := tk.State.ID
			first := failFeatSession(t, h, r, id, "A1")
			a, _ := r.Next(context.Background(), NextOptions{TaskRef: id, Resume: true})
			if a.ContinueAgentID != "A1" {
				t.Fatalf("%+v", a)
			}
			for _, run := range []string{a.RunID, first.Result.RunID} {
				if _, err := os.Stat(sessionStepFile(h, run)); err != nil {
					t.Fatalf("thiếu tệp %s trước khi bỏ: %v", run, err)
				}
			}
			if err := drop(r, id); err != nil {
				t.Fatal(err)
			}
			for _, run := range []string{a.RunID, first.Result.RunID} {
				if _, err := os.Stat(sessionStepFile(h, run)); !os.IsNotExist(err) {
					t.Fatalf("tệp %s còn sau %s: %v", run, name, err)
				}
			}
		})
	}
}
