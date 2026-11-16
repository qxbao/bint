package workflow

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/eval"
	"bint/internal/harness/claude"
	"bint/internal/spool"
	"bint/internal/task"
)

// Nối tiếp phiên của bước driver process (docs/step-continuation-plan.md giai đoạn 3): agent giả đóng vai Claude/Codex, ghi tệp
// phiên (transcript/rollout) vào CLAUDE_CONFIG_DIR/CODEX_HOME tạm và nhận AgentRequest.ResumeSessionID. Không gọi harness thật.

// procSetup trỏ thư mục phiên của Claude/Codex vào thư mục tạm; trả (claudeDir, codexDir).
func procSetup(t *testing.T) (string, string) {
	t.Helper()
	cl, cx := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cl)
	t.Setenv("CODEX_HOME", cx)
	return cl, cx
}

func (h *harness) claudeTranscript(id string) string {
	return filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "projects", claude.ProjectSlug(h.root), id+".jsonl")
}

// claudeAppend mô phỏng Claude ghi thêm một lượt vào transcript của phiên (usage cộng dồn theo phiên).
func (h *harness) claudeAppend(id, msgID string, in, out int) {
	h.t.Helper()
	p := h.claudeTranscript(id)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		h.t.Fatal(err)
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		h.t.Fatal(err)
	}
	defer f.Close()
	fmt.Fprintf(f, `{"type":"assistant","message":{"id":%q,"model":"claude-sonnet-5-5","usage":{"input_tokens":%d,"output_tokens":%d}}}`+"\n", msgID, in, out)
}

// claudeLike làm agent giả giống Claude: mỗi lần gọi ghi một lượt vào transcript của phiên (UUID Bint cấp hoặc phiên nối tiếp), đặt
// SessionID của kết quả, và báo chi phí tổng cộng khổng lồ (cả phiên) để chứng minh Bint không dùng nó cho bước nối tiếp.
func (h *harness) claudeLike(outcome func(req eval.AgentRequest, step string, n int, res *eval.AgentResult)) {
	h.agentName = "claude"
	huge := 999.0
	h.mutateReq = func(req eval.AgentRequest, step string, res *eval.AgentResult) {
		id := req.SessionID
		if req.ResumeSessionID != "" {
			id = req.ResumeSessionID
		}
		n := countCalls(h, step)
		h.claudeAppend(id, fmt.Sprintf("m-%s-%d", step, n), 1000*n, 100*n)
		res.SessionID, res.CostUSD = id, &huge
		if outcome != nil {
			outcome(req, step, n, res)
		}
	}
}

func externalFail(res *eval.AgentResult) {
	res.ExitCode, res.Error, res.Infra, res.QuotaHit = 1, "429 Too Many Requests", "hết hạn mức sử dụng", true
}

func lastFeatReq(h *harness) eval.AgentRequest {
	for i := len(h.calls) - 1; i >= 0; i-- {
		if h.calls[i] == StepFeat {
			return h.reqs[i]
		}
	}
	h.t.Fatal("không có lần gọi b-feat")
	return eval.AgentRequest{}
}

func envOf(req eval.AgentRequest, key string) string {
	for _, kv := range req.Env {
		if k, v, _ := strings.Cut(kv, "="); k == key {
			return v
		}
	}
	return ""
}

func resumeDo(t *testing.T, h *harness, id string, mod func(*RunOptions)) *Result {
	t.Helper()
	o := RunOptions{Workflow: StepDo, TaskRef: id, Resume: true}
	if mod != nil {
		mod(&o)
	}
	res, err := h.runner().Run(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func startDo(t *testing.T, h *harness) (*Result, string) {
	t.Helper()
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: "Thêm tính năng phiên bản v2 cho app.txt", Plan: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	return res, res.TaskID
}

func TestProcessContinuationExternalResumesSameSession(t *testing.T) {
	procSetup(t)
	h := newHarness(t)
	h.claudeLike(func(req eval.AgentRequest, step string, n int, res *eval.AgentResult) {
		if step == StepFeat && n == 1 {
			externalFail(res)
		}
	})
	res, id := startDo(t, h)
	tk := loadTask(t, h, id)
	ls := tk.State.LastStep
	first := lastFeatReq(h)
	if res.Status != task.StatusFailed || ls == nil || ls.Via != "claude" || ls.StopClass != StopExternal || !ls.Continuable || ls.SessionID != first.SessionID ||
		ls.Usage == nil || ls.Usage.Transcript == "" || len(ls.Usage.Models) != 1 {
		t.Fatalf("LastStep sau lần dừng đầu: %+v (%s)", ls, res.Status)
	}
	firstRun := ls.RunID

	res2 := resumeDo(t, h, id, nil)
	second := lastFeatReq(h)
	if second.ResumeSessionID != first.SessionID || second.SessionID != "" {
		t.Fatalf("phải nối tiếp phiên %q: %+v", first.SessionID, second)
	}
	if !strings.Contains(second.Prompt, "Tiếp tục bước "+StepFeat) || strings.Contains(second.Prompt, "<bint-task") || !strings.Contains(second.Prompt, "nguyên nhân ngoại cảnh") {
		t.Fatalf("lời nhắn nối tiếp:\n%s", second.Prompt)
	}
	child := envOf(second, EnvRunID)
	if child == "" || child == firstRun || !strings.Contains(second.Prompt, child) || !strings.Contains(second.Prompt, firstRun) {
		t.Fatalf("BINT_RUN_ID phải là lượt con mới (%q, lượt cũ %q)", child, firstRun)
	}
	if res2.Status != task.StatusDone {
		t.Fatalf("sau nối tiếp b-feat thành công phải đi tiếp tới done: %s %s", res2.Status, res2.Reason)
	}
	if !hasNote(res2.Notes, "nối tiếp phiên") {
		t.Fatalf("notes: %v", res2.Notes)
	}
	// Chi phí: chỉ phần transcript vượt mốc lúc dừng (lượt n=2), không phải 999 USD harness báo.
	var featStep *StepResult
	for i := range res2.Steps {
		if res2.Steps[i].Step == StepFeat {
			featStep = &res2.Steps[i]
		}
	}
	delta := claude.Usage{Turns: 1, Models: []claude.ModelUsage{{Model: "claude-sonnet-5-5", Input: 2000, Output: 200}}}
	if featStep == nil || featStep.CostUSD == nil || math.Abs(*featStep.CostUSD-*delta.Cost(nil)) > 1e-9 {
		t.Fatalf("chi phí bước nối tiếp phải là phần mới %v: %+v", *delta.Cost(nil), featStep)
	}
}

func TestProcessContinuationTimeoutOnlyOnce(t *testing.T) {
	procSetup(t)
	h := newHarness(t)
	h.claudeLike(func(req eval.AgentRequest, step string, n int, res *eval.AgentResult) {
		if step == StepFeat && n <= 2 {
			res.TimedOut, res.ExitCode = true, -1
		}
	})
	_, id := startDo(t, h)
	if ls := loadTask(t, h, id).State.LastStep; ls == nil || ls.StopClass != StopTimeout || !ls.Continuable {
		t.Fatalf("LastStep: %+v", ls)
	}
	first := lastFeatReq(h).SessionID
	resumeDo(t, h, id, nil) // nối tiếp lần 1, lại quá giờ
	if r := lastFeatReq(h); r.ResumeSessionID != first {
		t.Fatalf("lần 2 phải nối tiếp: %+v", r)
	}
	if ls := loadTask(t, h, id).State.LastStep; ls == nil || ls.StopClass != StopTimeout || ls.Continuations != 1 {
		t.Fatalf("sau nối tiếp quá giờ: %+v", ls)
	}
	res := resumeDo(t, h, id, nil) // quá giờ lần nữa: chạy mới
	r3 := lastFeatReq(h)
	if r3.ResumeSessionID != "" || r3.SessionID == "" || r3.SessionID == first || !strings.Contains(r3.Prompt, "<bint-task") {
		t.Fatalf("lần 3 phải chạy mới với đề bài đầy đủ: %+v", r3)
	}
	if !hasNote(res.Notes, "quá giờ đã được nối tiếp") {
		t.Fatalf("notes: %v", res.Notes)
	}
}

func TestProcessContinuationExternalCapThree(t *testing.T) {
	procSetup(t)
	h := newHarness(t)
	h.claudeLike(func(req eval.AgentRequest, step string, n int, res *eval.AgentResult) {
		if step == StepFeat && n <= 4 {
			externalFail(res)
		}
	})
	_, id := startDo(t, h)
	first := lastFeatReq(h).SessionID
	for i := 1; i <= 3; i++ {
		resumeDo(t, h, id, nil)
		if r := lastFeatReq(h); r.ResumeSessionID != first {
			t.Fatalf("lần nối tiếp %d: %+v", i, r)
		}
		if ls := loadTask(t, h, id).State.LastStep; ls == nil || ls.Continuations != i || ls.SessionID != first {
			t.Fatalf("sau lần %d: %+v", i, ls)
		}
	}
	res := resumeDo(t, h, id, nil)
	r := lastFeatReq(h)
	if r.ResumeSessionID != "" || !strings.Contains(r.Prompt, "<bint-task") || !hasNote(res.Notes, "tối đa 3") {
		t.Fatalf("lần 4 phải chạy mới: %+v / %v", r, res.Notes)
	}
}

func TestProcessContinuationNotContinued(t *testing.T) {
	for _, tc := range []struct {
		name string
		prep func(t *testing.T, h *harness, id string)
		opt  func(*RunOptions)
		note string
	}{
		{"cây đổi", func(t *testing.T, h *harness, id string) { h.write("other.txt", "đổi cây\n") }, nil, "đã đổi"},
		{"--fresh", nil, func(o *RunOptions) { o.Fresh = true }, "--fresh"},
		{"khác harness", func(t *testing.T, h *harness, id string) { h.agentName = "codex" }, nil, "không nối tiếp chéo"},
		{"LastStep không có Via", func(t *testing.T, h *harness, id string) {
			if _, err := h.tasks.Update(id, time.Now(), func(tk *task.Task) error { tk.State.LastStep.Via = ""; return nil }); err != nil {
				t.Fatal(err)
			}
		}, nil, "không ghi cách chạy"},
		{"harness không hỗ trợ nối tiếp", func(t *testing.T, h *harness, id string) {
			if _, err := h.tasks.Update(id, time.Now(), func(tk *task.Task) error { tk.State.LastStep.Via = "antigravity"; return nil }); err != nil {
				t.Fatal(err)
			}
			h.agentName = "antigravity"
		}, nil, "không hỗ trợ nối tiếp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			procSetup(t)
			h := newHarness(t)
			h.claudeLike(func(req eval.AgentRequest, step string, n int, res *eval.AgentResult) {
				if step == StepFeat && n == 1 {
					externalFail(res)
				}
			})
			_, id := startDo(t, h)
			if tc.prep != nil {
				tc.prep(t, h, id)
			}
			res := resumeDo(t, h, id, tc.opt)
			r := lastFeatReq(h)
			if r.ResumeSessionID != "" || !strings.Contains(r.Prompt, "<bint-task") || !hasNote(res.Notes, tc.note) {
				t.Fatalf("phải chạy mới (note %q): req=%+v notes=%v", tc.note, r, res.Notes)
			}
		})
	}
}

// Claude chết trước khi ghi phiên (phát hiện .5 của GĐ1): không có transcript lúc dừng ⇒ continuable=false, lần sau chạy mới kèm note.
func TestProcessSessionNeverWrittenNotContinuable(t *testing.T) {
	procSetup(t)
	h := newHarness(t)
	h.agentName = "claude"
	h.mutateReq = func(req eval.AgentRequest, step string, res *eval.AgentResult) {
		if step == StepFeat && countCalls(h, StepFeat) == 1 {
			res.ExitCode, res.Error, res.NoResult = 1, "chết trước khi ghi phiên", true // không ghi transcript
		}
	}
	res, id := startDo(t, h)
	ls := loadTask(t, h, id).State.LastStep
	if ls == nil || ls.StopClass != StopExternal || ls.Continuable || ls.SessionID == "" || ls.Via != "claude" {
		t.Fatalf("LastStep: %+v", ls)
	}
	if !hasNote(res.Notes, "chưa được Claude ghi") {
		t.Fatalf("notes: %v", res.Notes)
	}
	res2 := resumeDo(t, h, id, nil)
	if r := lastFeatReq(h); r.ResumeSessionID != "" || !strings.Contains(r.Prompt, "<bint-task") || !hasNote(res2.Notes, "không nối tiếp được") {
		t.Fatalf("phải chạy mới: %+v / %v", r, res2.Notes)
	}
}

// Phiên biến mất giữa lúc dừng và lúc nối tiếp (tệp bị xoá): chạy mới ngay trong bước kèm note, không gọi harness resume.
func TestProcessSessionVanishedBeforeResumeRunsFresh(t *testing.T) {
	procSetup(t)
	h := newHarness(t)
	h.claudeLike(func(req eval.AgentRequest, step string, n int, res *eval.AgentResult) {
		if step == StepFeat && n == 1 {
			externalFail(res)
		}
	})
	_, id := startDo(t, h)
	if err := os.Remove(h.claudeTranscript(lastFeatReq(h).SessionID)); err != nil {
		t.Fatal(err)
	}
	res := resumeDo(t, h, id, nil)
	if r := lastFeatReq(h); r.ResumeSessionID != "" || !strings.Contains(r.Prompt, "<bint-task") || !hasNote(res.Notes, "không còn") {
		t.Fatalf("phải chạy mới: %+v / %v", r, res.Notes)
	}
}

// Harness báo không tìm thấy phiên dù tệp còn: gọi lại một lần với đề bài đầy đủ và UUID mới trong cùng bước.
func TestProcessResumeMissingFallsBackToFullPrompt(t *testing.T) {
	procSetup(t)
	h := newHarness(t)
	h.claudeLike(func(req eval.AgentRequest, step string, n int, res *eval.AgentResult) {
		switch {
		case step == StepFeat && n == 1:
			externalFail(res)
		case step == StepFeat && req.ResumeSessionID != "":
			res.ExitCode, res.Error, res.NoResult, res.ResumeMissing, res.CostUSD, res.SessionID = 1, "No conversation found", true, true, nil, ""
		}
	})
	_, id := startDo(t, h)
	first := lastFeatReq(h).SessionID
	callsBefore := countCalls(h, StepFeat)
	res := resumeDo(t, h, id, nil)
	if got := countCalls(h, StepFeat) - callsBefore; got != 2 {
		t.Fatalf("phải gọi agent 2 lần (resume hỏng rồi chạy mới): %d", got)
	}
	var feats []eval.AgentRequest
	for i, c := range h.calls {
		if c == StepFeat {
			feats = append(feats, h.reqs[i])
		}
	}
	r1, r2 := feats[len(feats)-2], feats[len(feats)-1]
	if r1.ResumeSessionID != first || r2.ResumeSessionID != "" || r2.SessionID == "" || r2.SessionID == first || !strings.Contains(r2.Prompt, "<bint-task") {
		t.Fatalf("lần 2 phải là đề bài đầy đủ với UUID mới:\n%+v\n%+v", r1, r2)
	}
	if !hasNote(res.Notes, "không tìm thấy phiên") || res.Status != task.StatusDone {
		t.Fatalf("%s %v", res.Status, res.Notes)
	}
	// Bước chạy mới: bộ đếm nối tiếp không kế thừa.
	for _, s := range res.Steps {
		if s.Step == StepFeat && s.StopClass != "" {
			t.Fatalf("bước feat hoàn thành bình thường: %+v", s)
		}
	}
}

func TestProcessContinuationClaudeCostFromFixtureTranscript(t *testing.T) {
	procSetup(t)
	const dir = "../harness/claude/testdata/claude-2.1.322/"
	before, err := os.ReadFile(dir + "transcript-before-resume.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(dir + "transcript-after-resume.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t)
	h.agentName = "claude"
	var sid string
	h.mutateReq = func(req eval.AgentRequest, step string, res *eval.AgentResult) {
		if step != StepFeat {
			return
		}
		huge := 500.0
		res.CostUSD = &huge
		if req.ResumeSessionID == "" {
			sid = req.SessionID
			os.MkdirAll(filepath.Dir(h.claudeTranscript(sid)), 0o755)
			os.WriteFile(h.claudeTranscript(sid), before, 0o644)
			res.SessionID = sid
			externalFail(res)
			return
		}
		os.WriteFile(h.claudeTranscript(sid), after, 0o644) // Claude ghi tiếp vào cùng tệp
		res.SessionID = sid
	}
	_, id := startDo(t, h)
	res := resumeDo(t, h, id, nil)
	var got *StepResult
	for i := range res.Steps {
		if res.Steps[i].Step == StepFeat {
			got = &res.Steps[i]
		}
	}
	// Phần mới của fixture = hai lượt cuối (msg_3, msg_4); model claude-sonnet-5-5.
	want := claude.Usage{Turns: 2, Models: []claude.ModelUsage{{Model: "claude-sonnet-5-5", Input: 650, Output: 1700, CacheWrite: 2000, CacheRead: 14400}}}
	if got == nil || got.CostUSD == nil || math.Abs(*got.CostUSD-*want.Cost(nil)) > 1e-9 {
		t.Fatalf("chi phí = %+v, muốn phần mới %v", got, *want.Cost(nil))
	}
}

func TestProcessContinuationClaudeCostUnknownWithoutPricing(t *testing.T) {
	procSetup(t)
	h := newHarness(t)
	h.claudeLike(func(req eval.AgentRequest, step string, n int, res *eval.AgentResult) {
		if step == StepFeat && n == 1 {
			externalFail(res)
		}
		if step == StepFeat && n == 2 {
			// Model lạ không có giá: unknown (nil) kèm note, không đoán.
			p := h.claudeTranscript(req.ResumeSessionID)
			f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
			fmt.Fprintln(f, `{"type":"assistant","message":{"id":"m-x","model":"model-la","usage":{"input_tokens":5,"output_tokens":5}}}`)
			f.Close()
		}
	})
	_, id := startDo(t, h)
	res := resumeDo(t, h, id, nil)
	for _, s := range res.Steps {
		if s.Step == StepFeat && s.CostUSD != nil {
			t.Fatalf("model không có giá phải unknown: %+v", s)
		}
	}
	if !hasNote(res.Notes, "không có giá") {
		t.Fatalf("notes: %v", res.Notes)
	}
}

// writeRollout ghi rollout Codex của thread với bộ đếm token cộng dồn (cùng cấu trúc fixture testdata của harness/codex).
func writeRollout(t *testing.T, thread string, in, cached, out int) {
	t.Helper()
	dir := filepath.Join(os.Getenv("CODEX_HOME"), "sessions", "2026", "11", "16")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := fmt.Sprintf(`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":%d,"cached_input_tokens":%d,"output_tokens":%d}}}}`+"\n", in, cached, out)
	if err := os.WriteFile(filepath.Join(dir, "rollout-2026-11-16T09-00-00-"+thread+".jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestProcessContinuationCodexTokensDelta(t *testing.T) {
	procSetup(t)
	h := newHarness(t)
	h.agentName = "codex"
	const thread = "01a17179-5784-7152-8335-f0af387ce18a"
	h.mutateReq = func(req eval.AgentRequest, step string, res *eval.AgentResult) {
		if step != StepFeat {
			return
		}
		res.SessionID = thread
		bogus := int64(777777) // usage JSONL của lần resume có thể cộng dồn: không được dùng
		res.InputTokens, res.OutputTokens = &bogus, &bogus
		if req.ResumeSessionID == "" {
			writeRollout(t, thread, 55000, 20000, 900)
			externalFail(res)
			return
		}
		if req.ResumeSessionID != thread || req.SessionID != "" {
			t.Errorf("phải nối tiếp thread: %+v", req)
		}
		writeRollout(t, thread, 61000, 41000, 1100)
	}
	_, id := startDo(t, h)
	if ls := loadTask(t, h, id).State.LastStep; ls == nil || ls.Via != "codex" || !ls.Continuable || ls.SessionID != thread || ls.Usage == nil || ls.Usage.Models[0].Input != 55000 {
		t.Fatalf("LastStep: %+v", ls)
	}
	res := resumeDo(t, h, id, nil)
	var got *StepResult
	for i := range res.Steps {
		if res.Steps[i].Step == StepFeat {
			got = &res.Steps[i]
		}
	}
	if got == nil || got.CostUSD != nil {
		t.Fatalf("Codex không báo USD: %+v", got)
	}
	// Token của bước nối tiếp = bộ đếm mới − mốc (6000 vào, 200 ra), không phải 777777.
	ev := h.lastFinished(t, got.RunID)
	if ev["input_tokens"] != float64(6000) || ev["output_tokens"] != float64(200) {
		t.Fatalf("token bước nối tiếp: %v", ev)
	}
}

func TestProcessContinuationCodexNoRollout(t *testing.T) {
	procSetup(t)
	h := newHarness(t)
	h.agentName = "codex"
	h.mutateReq = func(req eval.AgentRequest, step string, res *eval.AgentResult) {
		if step == StepFeat && countCalls(h, StepFeat) == 1 {
			res.SessionID = "thread-x"
			externalFail(res) // không có rollout trên đĩa
		}
	}
	_, id := startDo(t, h)
	if ls := loadTask(t, h, id).State.LastStep; ls == nil || ls.Continuable || ls.SessionID != "thread-x" {
		t.Fatalf("thiếu rollout ⇒ không nối tiếp được: %+v", ls)
	}
}

// lastFinished trả payload workflow.finished của lượt con runID (đọc từ spool).
func (h *harness) lastFinished(t *testing.T, runID string) map[string]any {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(h.spool, "*.jsonl"))
	for _, f := range files {
		evs, err := spool.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, ev := range evs {
			if ev.RunID == runID && ev.Type == spool.WorkflowFinished {
				return ev.Payload
			}
		}
	}
	t.Fatalf("không thấy workflow.finished của %s", runID)
	return nil
}

func lastReq(h *harness, step string) eval.AgentRequest {
	for i := len(h.calls) - 1; i >= 0; i-- {
		if h.calls[i] == step {
			return h.reqs[i]
		}
	}
	h.t.Fatalf("không có lần gọi %s", step)
	return eval.AgentRequest{}
}

// planLoop dựng agent giả cho vòng câu hỏi: b-req ghi requirement với `docs` hiện tại (đổi giữa các vòng), bước khác như happy.
func planLoop(h *harness, docs *string) {
	h.behave = func(c context.Context, step string, e *Env, id string) error {
		if step != StepReq {
			return h.happy(c, step, e, id)
		}
		if _, _, err := e.SetTicket(id, TicketEdit{ClearCriteria: true, Criteria: []string{"app.txt ghi v2"}}); err != nil {
			return err
		}
		for n, body := range validDocs("app.txt ghi v2", *docs) {
			if _, _, err := e.WriteArtifact(id, n, body); err != nil {
				return err
			}
		}
		return nil
	}
}

// bint run resume: câu hỏi Q-1 chặn → trả lời → resume nối tiếp planner cũ (nguyên văn dòng đã trả lời) → planner thêm Q-2 thì chặn
// lại → trả lời → resume nối tiếp lần nữa → hết câu hỏi thì b-do đi tiếp b-feat trong cùng lượt.
func TestProcessRequirementQuestionLoopResume(t *testing.T) {
	procSetup(t)
	h := newHarness(t)
	h.claudeLike(nil)
	docs := openQ
	planLoop(h, &docs)
	r := h.runner()
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, Request: "Thêm tính năng phiên bản v2 cho app.txt"})
	if err != nil {
		t.Fatal(err)
	}
	id := res.TaskID
	if res.Status != task.StatusBlocked || strings.Join(h.calls, ",") != "b-req" {
		t.Fatalf("phải blocked ở Q-1: %s %v", res.Status, h.calls)
	}
	ls := loadTask(t, h, id).State.LastStep
	if ls == nil || ls.StopClass != StopAwaitingHuman || ls.Via != "claude" || !ls.Continuable || ls.Step != StepReq {
		t.Fatalf("LastStep: %+v", ls)
	}
	planner := ls.SessionID

	// Chưa trả lời: chặn lại như cũ, không gọi planner.
	if res1 := resumeDo(t, h, id, nil); res1.Status != task.StatusBlocked || countCalls(h, StepReq) != 1 {
		t.Fatalf("chưa trả lời: %s %v", res1.Status, h.calls)
	}

	// Vòng 1: trả lời Q-1 → nối tiếp planner; planner thêm Q-2 → dừng chờ lần nữa.
	writeReq(t, r, id, answeredQ)
	docs = answeredQ + openQ2
	res2 := resumeDo(t, h, id, nil)
	q := lastReq(h, StepReq)
	if q.ResumeSessionID != planner || !strings.Contains(q.Prompt, strings.TrimSpace(answeredQ)) || strings.Contains(q.Prompt, "<bint-task") {
		t.Fatalf("vòng 1 phải nối tiếp planner với dòng Q-1: %+v", q)
	}
	if res2.Status != task.StatusBlocked || len(res2.Questions) != 1 || !strings.Contains(res2.Questions[0], "Q-2") || countCalls(h, StepFeat) != 0 {
		t.Fatalf("planner thêm Q-2 phải chặn lại: %s %v %v", res2.Status, res2.Questions, h.calls)
	}
	if ls := loadTask(t, h, id).State.LastStep; ls == nil || ls.StopClass != StopAwaitingHuman || ls.SessionID != planner || !ls.Continuable {
		t.Fatalf("LastStep sau vòng 1: %+v", ls)
	}

	// Vòng 2: trả lời Q-2 → nối tiếp tiếp; hết câu hỏi → b-feat cùng lượt.
	writeReq(t, r, id, answeredQ+answeredQ2)
	docs = answeredQ + answeredQ2
	res3 := resumeDo(t, h, id, nil)
	q = lastReq(h, StepReq)
	if q.ResumeSessionID != planner || !strings.Contains(q.Prompt, strings.TrimSpace(answeredQ)) || !strings.Contains(q.Prompt, strings.TrimSpace(answeredQ2)) {
		t.Fatalf("vòng 2: %+v", q)
	}
	if res3.Status != task.StatusDone || countCalls(h, StepReq) != 3 || countCalls(h, StepFeat) != 1 {
		t.Fatalf("hết câu hỏi phải đi tiếp b-feat: %s %v", res3.Status, h.calls)
	}
	f := lastReq(h, StepFeat)
	if f.ResumeSessionID != "" || !strings.Contains(f.Prompt, strings.TrimSpace(answeredQ)) || !strings.Contains(f.Prompt, strings.TrimSpace(answeredQ2)) {
		t.Fatalf("b-feat chạy mới với các dòng Q đã trả lời: %+v", f)
	}
}

// Câu hỏi Q-1 ở b-req chạy riêng (tác vụ planned, không blocked): b-req --task và b-do --task đều nối tiếp planner cũ.
func TestProcessRequirementQuestionStandaloneThenTask(t *testing.T) {
	for _, wf := range []string{StepDo, StepReq} {
		t.Run(wf, func(t *testing.T) {
			procSetup(t)
			h := newHarness(t)
			h.claudeLike(nil)
			docs := openQ
			planLoop(h, &docs)
			r := h.runner()
			res, err := r.Run(context.Background(), RunOptions{Workflow: StepReq, Request: "Thêm tính năng phiên bản v2 cho app.txt"})
			if err != nil {
				t.Fatal(err)
			}
			id := res.TaskID
			tk := loadTask(t, h, id)
			ls := tk.State.LastStep
			if tk.State.Status != task.StatusPlanned || ls == nil || ls.StopClass != StopAwaitingHuman || !ls.Continuable || ls.Via != "claude" {
				t.Fatalf("b-req riêng phải giữ planned và ghi awaiting_human: %s %+v", tk.State.Status, ls)
			}
			planner := ls.SessionID
			writeReq(t, r, id, answeredQ)
			docs = answeredQ
			out, err := r.Run(context.Background(), RunOptions{Workflow: wf, TaskRef: id})
			if err != nil {
				t.Fatal(err)
			}
			q := lastReq(h, StepReq)
			if q.ResumeSessionID != planner || !strings.Contains(q.Prompt, strings.TrimSpace(answeredQ)) || countCalls(h, StepReq) != 2 {
				t.Fatalf("%s --task phải nối tiếp planner: %+v %v", wf, q, h.calls)
			}
			if wf == StepDo && (out.Status != task.StatusDone || countCalls(h, StepFeat) != 1) {
				t.Fatalf("b-do đi tiếp b-feat trong cùng lượt: %s %v", out.Status, h.calls)
			}
			if wf == StepReq && countCalls(h, StepFeat) != 0 {
				t.Fatalf("b-req riêng dừng sau bước plan: %v", h.calls)
			}
		})
	}
}

// Không nối tiếp được thì giữ hành vi cũ: --fresh, cây đổi, hoặc còn câu hỏi mở không chạy planner; b-do vào thẳng b-feat.
func TestProcessRequirementQuestionNotContinuedKeepsOldBehavior(t *testing.T) {
	for _, tc := range []struct {
		name string
		prep func(h *harness)
		opt  func(*RunOptions)
		note string
	}{
		{"--fresh", nil, func(o *RunOptions) { o.Fresh = true }, "--fresh"},
		{"cây đổi", func(h *harness) { h.write("other.txt", "đổi cây\n") }, nil, "đã đổi"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			procSetup(t)
			h := newHarness(t)
			h.claudeLike(nil)
			docs := openQ
			planLoop(h, &docs)
			r := h.runner()
			res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, Request: "Thêm tính năng phiên bản v2 cho app.txt"})
			if err != nil {
				t.Fatal(err)
			}
			writeReq(t, r, res.TaskID, answeredQ)
			if tc.prep != nil {
				tc.prep(h)
			}
			out := resumeDo(t, h, res.TaskID, tc.opt)
			if countCalls(h, StepReq) != 1 || countCalls(h, StepFeat) != 1 || !hasNote(out.Notes, tc.note) || !hasNote(out.Notes, "không nối tiếp planner cũ") {
				t.Fatalf("phải vào thẳng b-feat, không chạy lại b-req: %s %v %v", out.Status, h.calls, out.Notes)
			}
		})
	}
}
