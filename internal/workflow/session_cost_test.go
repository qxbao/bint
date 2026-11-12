package workflow

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/config"
	adapters "bint/internal/harness"
	"bint/internal/harness/claude"
	"bint/internal/harness/hookcore"
)

func fixedUsage(model string, out int64, turns int) func(string) (*claude.Usage, string) {
	return func(string) (*claude.Usage, string) {
		return &claude.Usage{Turns: turns, Models: []claude.ModelUsage{{Model: model, Output: out}}}, ""
	}
}

func hasNote(notes []string, sub string) bool {
	for _, n := range notes {
		if strings.Contains(n, sub) {
			return true
		}
	}
	return false
}

func errorsOf(t *testing.T, h *harness, id string) []string {
	t.Helper()
	var out []string
	for _, e := range loadTask(t, h, id).State.Errors {
		out = append(out, e.Step+": "+e.Message)
	}
	return out
}

// Đường mặc định: hook ghi runtime/subagents/<agent_id>.json + transcript có dòng lặp cùng message.id ⇒ complete đọc,
// khử trùng lặp, quy USD theo bảng giá dựng sẵn.
func TestSessionCompleteReadsSubagentTranscript(t *testing.T) {
	h := newHarness(t)
	h.behave = featWrites(h, nil)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	ctx := context.Background()
	a, err := r.Next(ctx, NextOptions{TaskRef: tk.State.ID})
	if err != nil || a.Action != ActionAgent {
		t.Fatal(a, err)
	}
	h.subagent(a)
	tr := filepath.Join(t.TempDir(), "agent-A1.jsonl")
	lines := []string{
		`{"type":"assistant","message":{"id":"m1","model":"claude-sonnet-5-5","usage":{"input_tokens":1000,"output_tokens":10}}}`,
		`{"type":"assistant","message":{"id":"m1","model":"claude-sonnet-5-5","usage":{"input_tokens":1000,"output_tokens":500}}}`,
		`{"type":"assistant","message":{"id":"m2","model":"claude-sonnet-5-5","usage":{"input_tokens":2000,"output_tokens":1500,"cache_read_input_tokens":10000}}}`,
	}
	if err := os.WriteFile(tr, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := hookcore.WriteSubagentRecord(filepath.Dir(h.tasks.Dir), hookcore.SubagentRecord{AgentID: "A1", AgentType: "bint-implementer", TranscriptPath: tr}); err != nil {
		t.Fatal(err)
	}
	c, err := h.runner().Complete(ctx, CompleteOptions{TaskRef: tk.State.ID, RunID: a.RunID, AgentID: "A1"})
	if err != nil || c.Result == nil {
		t.Fatalf("complete: %+v %v", c, err)
	}
	want := (3000*2.0 + 2000*10.0 + 10000*0.10) / 1e6 // Sonnet 5.5: 2 / 10 / đọc cache 0,10 USD mỗi triệu token
	if c.Result.CostUSD == nil || math.Abs(*c.Result.CostUSD-want) > 1e-9 {
		t.Fatalf("cost_usd = %v, muốn %v", c.Result.CostUSD, want)
	}
	if c.Result.Model != "claude-sonnet-5-5" {
		t.Fatalf("model %q", c.Result.Model)
	}
	if got := loadTask(t, h, tk.State.ID).State.SessionSpend; got == nil || math.Abs(got.Cost-want) > 1e-9 || got.Turns != 2 || got.CostUnknown {
		t.Fatalf("SessionSpend: %+v", got)
	}
}

// Thiếu transcript / thiếu SubagentStop / thiếu --agent-id ⇒ chi phí unknown (nil, không phải 0) kèm ghi chú; bước vẫn hợp lệ.
func TestSessionCompleteCostUnknown(t *testing.T) {
	cases := map[string]struct {
		agentID string
		setup   func(h *harness)
		note    string
	}{
		"không có tệp hook": {"A2", nil, "chưa có SubagentStop"},
		"transcript không còn": {"A3", func(h *harness) {
			hookcore.WriteSubagentRecord(filepath.Dir(h.tasks.Dir), hookcore.SubagentRecord{AgentID: "A3", TranscriptPath: filepath.Join(t.TempDir(), "mat.jsonl")})
		}, "không mở được transcript"},
		"thiếu --agent-id": {"", nil, "thiếu --agent-id"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.behave = featWrites(h, nil)
			r := h.runner()
			tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
			a, err := r.Next(context.Background(), NextOptions{TaskRef: tk.State.ID})
			if err != nil || a.Action != ActionAgent {
				t.Fatal(a, err)
			}
			h.subagent(a)
			if c.setup != nil {
				c.setup(h)
			}
			res, err := h.runner().Complete(context.Background(), CompleteOptions{TaskRef: tk.State.ID, RunID: a.RunID, AgentID: c.agentID})
			if err != nil || res.Result == nil || res.Result.Status != "completed" {
				t.Fatalf("lượt vẫn hợp lệ: %+v %v", res, err)
			}
			if res.Result.CostUSD != nil {
				t.Fatalf("chi phí phải unknown: %v", *res.Result.CostUSD)
			}
			if !hasNote(res.Notes, c.note) {
				t.Fatalf("thiếu ghi chú %q: %v", c.note, res.Notes)
			}
			if sp := loadTask(t, h, tk.State.ID).State.SessionSpend; sp == nil || !sp.CostUnknown {
				t.Fatalf("SessionSpend phải đánh dấu unknown: %+v", sp)
			}
		})
	}
}

func TestSessionCompleteModelWithoutPriceIsUnknownUntilConfigured(t *testing.T) {
	h := newHarness(t)
	h.behave = featWrites(h, nil)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	a, _ := r.Next(context.Background(), NextOptions{TaskRef: tk.State.ID})
	h.subagent(a)
	r2 := h.runner()
	r2.SubagentUsage = fixedUsage("claude-lạ-9", 1_000_000, 3)
	res, err := r2.Complete(context.Background(), CompleteOptions{TaskRef: tk.State.ID, RunID: a.RunID, AgentID: "A"})
	if err != nil || res.Result.CostUSD != nil || !hasNote(res.Notes, "không có giá") {
		t.Fatalf("model không giá ⇒ unknown: %+v %v", res, err)
	}
	// Cấu hình có giá thì tính được (lượt mới: abort rồi chạy lại bước).
	h.cfg.Harnesses.Claude.Pricing = map[string]config.ModelPrice{"claude-lạ-9": {InputUSDPerMTok: 1, OutputUSDPerMTok: 4}}
	if _, err := h.runner().Abort(context.Background(), tk.State.ID, "thử lại"); err != nil {
		t.Fatal(err)
	}
	a2, err := h.runner().Next(context.Background(), NextOptions{TaskRef: tk.State.ID, Resume: true})
	if err != nil || a2.Action != ActionAgent {
		t.Fatal(a2, err)
	}
	h.subagent(a2)
	r3 := h.runner()
	r3.SubagentUsage = fixedUsage("claude-lạ-9", 1_000_000, 3)
	res, err = r3.Complete(context.Background(), CompleteOptions{TaskRef: tk.State.ID, RunID: a2.RunID, AgentID: "B"})
	if err != nil || res.Result.CostUSD == nil || math.Abs(*res.Result.CostUSD-4) > 1e-9 {
		t.Fatalf("giá cấu hình: %+v %v", res, err)
	}
}

// Vượt max_cost_usd của bước ⇒ ghi lỗi (task.error) và ghi chú, bước không failed.
func TestSessionCompleteStepOverMaxCostRecordsError(t *testing.T) {
	h := newHarness(t)
	h.behave = featWrites(h, nil)
	limit := 1.0
	h.cfg.Limits.Workflows = map[string]config.PerRunLimits{StepFeat: {MaxCostUSD: &limit}}
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	a, err := r.Next(context.Background(), NextOptions{TaskRef: tk.State.ID})
	if err != nil || a.Action != ActionAgent {
		t.Fatal(a, err)
	}
	h.subagent(a)
	r2 := h.runner()
	r2.SubagentUsage = fixedUsage("claude-sonnet-5-5", 500_000, 4) // 5 USD
	res, err := r2.Complete(context.Background(), CompleteOptions{TaskRef: tk.State.ID, RunID: a.RunID, AgentID: "A"})
	if err != nil || res.Result == nil || res.Result.Status != "completed" {
		t.Fatalf("bước không được failed: %+v %v", res, err)
	}
	errs := errorsOf(t, h, tk.State.ID)
	if len(errs) != 1 || !strings.Contains(errs[0], "vượt limits.workflows.b-feat.max_cost_usd") {
		t.Fatalf("task.error: %v", errs)
	}
	// Trong hạn mức ⇒ không ghi lỗi.
	h2 := newHarness(t)
	h2.behave = featWrites(h2, nil)
	h2.cfg.Limits.Workflows = map[string]config.PerRunLimits{StepFeat: {MaxCostUSD: &limit}}
	r = h2.runner()
	tk = h2.newSession(r, NewTaskOptions{Plan: ptr(false)})
	a, _ = r.Next(context.Background(), NextOptions{TaskRef: tk.State.ID})
	h2.subagent(a)
	r2 = h2.runner()
	r2.SubagentUsage = fixedUsage("claude-sonnet-5-5", 10_000, 1)
	if _, err := r2.Complete(context.Background(), CompleteOptions{TaskRef: tk.State.ID, RunID: a.RunID, AgentID: "A"}); err != nil {
		t.Fatal(err)
	}
	if errs := errorsOf(t, h2, tk.State.ID); len(errs) != 0 {
		t.Fatalf("không được ghi lỗi: %v", errs)
	}
}

// Ngân sách b-do cộng dồn qua nhiều lệnh (mỗi lệnh một tiến trình/Runner mới): tổng vượt ⇒ next trả blocked.
func TestSessionBudgetAccumulatesAcrossCommands(t *testing.T) {
	h := newHarness(t)
	h.behave = featWrites(h, nil)
	budget := 6.0
	h.cfg.Limits.Workflows = map[string]config.PerRunLimits{StepDo: {MaxCostUSD: &budget}}
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	id := tk.State.ID
	ctx := context.Background()
	a, err := h.runner().Next(ctx, NextOptions{TaskRef: id})
	if err != nil || a.Action != ActionAgent || a.Step != StepFeat {
		t.Fatal(a, err)
	}
	h.subagent(a)
	rc := h.runner()
	rc.SubagentUsage = fixedUsage("claude-sonnet-5-5", 400_000, 5) // 4 USD (còn 2 trong ngân sách 6)
	if _, err := rc.Complete(ctx, CompleteOptions{TaskRef: id, RunID: a.RunID, AgentID: "A1"}); err != nil {
		t.Fatal(err)
	}
	if sp := loadTask(t, h, id).State.SessionSpend; sp == nil || math.Abs(sp.Cost-4) > 1e-9 {
		t.Fatalf("SessionSpend sau bước 1: %+v", sp)
	}
	// Bước kế (review) mở được vì còn ngân sách; giới hạn chi phí của nó là phần còn lại.
	a2, err := h.runner().Next(ctx, NextOptions{TaskRef: id})
	if err != nil || a2.Action != ActionAgent || a2.Step != StepReview {
		t.Fatalf("bước 2: %+v %v", a2, err)
	}
	h.subagent(a2)
	rc = h.runner()
	rc.SubagentUsage = fixedUsage("claude-sonnet-5-5", 300_000, 3) // thêm 3 USD ⇒ tổng 7 > 6
	if _, err := rc.Complete(ctx, CompleteOptions{TaskRef: id, RunID: a2.RunID, AgentID: "A2"}); err != nil {
		t.Fatal(err)
	}
	got := loadTask(t, h, id)
	if sp := got.State.SessionSpend; sp == nil || math.Abs(sp.Cost-7) > 1e-9 || sp.Turns != 8 {
		t.Fatalf("SessionSpend sau bước 2: %+v", sp)
	}
	// Tiến trình mới: ngân sách đã hết ⇒ không mở bước nào nữa.
	a3, err := h.runner().Next(ctx, NextOptions{TaskRef: id})
	if err != nil {
		t.Fatal(err)
	}
	if a3.Action == ActionAgent {
		t.Fatalf("hết ngân sách b-do mà next vẫn trả bước: %+v", a3)
	}
}

func TestSessionBudgetBlocksNextWhenSpent(t *testing.T) {
	h := newHarness(t)
	h.behave = featWrites(h, nil)
	budget := 3.0
	h.cfg.Limits.Workflows = map[string]config.PerRunLimits{StepDo: {MaxCostUSD: &budget}}
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	id := tk.State.ID
	ctx := context.Background()
	a, _ := h.runner().Next(ctx, NextOptions{TaskRef: id})
	h.subagent(a)
	rc := h.runner()
	rc.SubagentUsage = fixedUsage("claude-sonnet-5-5", 400_000, 5) // 4 > 3
	if _, err := rc.Complete(ctx, CompleteOptions{TaskRef: id, RunID: a.RunID, AgentID: "A1"}); err != nil {
		t.Fatal(err)
	}
	a2, err := h.runner().Next(ctx, NextOptions{TaskRef: id})
	if err != nil || a2.Action != ActionBlocked || !strings.Contains(a2.Reason, "max_cost_usd") {
		t.Fatalf("next phải blocked vì ngân sách: %+v %v", a2, err)
	}
}

// next trả tên subagent và model khớp định nghĩa do `bint init` sinh (harness.SubagentFiles) và cấu hình của role; tên của
// guardrail chỉ đọc trong hookcore khớp tên subagent của workflow.
func TestSessionNextSubagentMatchesDefinitions(t *testing.T) {
	if hookcore.SubagentPlanner != SubagentPlanner || hookcore.SubagentReviewer != SubagentReviewer {
		t.Fatalf("hookcore lệch workflow: %q/%q", hookcore.SubagentPlanner, hookcore.SubagentReviewer)
	}
	h := newHarness(t)
	h.behave = featWrites(h, nil)
	h.cfg.Harnesses.Claude.Models.Implementer = "haiku"
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	a, err := r.Next(context.Background(), NextOptions{TaskRef: tk.State.ID})
	if err != nil || a.Action != ActionAgent {
		t.Fatal(a, err)
	}
	if a.Subagent != SubagentFor(StepFeat) || a.Model != "haiku" {
		t.Fatalf("subagent/model: %q %q", a.Subagent, a.Model)
	}
	found := false
	for _, f := range adapters.SubagentFiles(h.cfg) {
		if f.Name == a.Subagent {
			found = true
			if !strings.Contains(string(f.Content), "model: haiku") {
				t.Fatalf("định nghĩa lệch model của next:\n%s", f.Content)
			}
		}
	}
	if !found {
		t.Fatalf("không có định nghĩa subagent %q", a.Subagent)
	}
}
