package workflow

// Golden hành vi Runner (GĐ3 của docs/custom-flow-plan.md): chốt cách Runner chọn bước kế tiếp, chế độ chỉ đọc, role,
// subagent, đề bài và chuyển trạng thái của bốn workflow gốc để refactor sang "theo kind của flow" không đổi hành vi.
//
// testdata/flow_golden.json được SINH từ mã cũ (trước refactor) bằng BINT_UPDATE_GOLDEN=1 và không được sửa tay;
// chạy không có biến này thì test so khớp nguyên văn với tệp.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"bint/internal/config"
	adapters "bint/internal/harness"
	"bint/internal/task"
)

const goldenFlowPath = "testdata/flow_golden.json"

var (
	reGoldenULID = regexp.MustCompile(`\b[0-9A-HJKMNP-TV-Z]{26}\b`)
	// AllowedTools chứa đường dẫn binary đang chạy (binary kiểm thử có tên thay đổi theo lần chạy).
	reGoldenTestBin = regexp.MustCompile(`Bash\([^)"]*\.test:\*\)`)
	reGoldenTime    = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})?`)
)

// goldenRunState dựng runState cho bảng nextStep. Đây là chỗ duy nhất trong golden biết cách khởi tạo runState.
func goldenRunState(opts RunOptions, tk *task.Task, ran int) *runState {
	rs := &runState{opts: opts, flow: builtinFlow(opts.Workflow), t: tk, res: &Result{Steps: []StepResult{}}, ran: map[string]int{}}
	if ran > 0 {
		rs.ran[opts.Workflow] = ran
	}
	return rs
}

func (h *harness) norm(s string) string {
	s = strings.ReplaceAll(s, filepath.Dir(h.spool), "<STATE>")
	s = strings.ReplaceAll(s, h.root, "<ROOT>")
	s = reGoldenULID.ReplaceAllString(s, "<ULID>")
	return reGoldenTime.ReplaceAllString(s, "<TIME>")
}

func (h *harness) promptSum(p string) string {
	n := h.norm(p)
	sum := sha256.Sum256([]byte(n))
	return fmt.Sprintf("%d:%s", len(n), hex.EncodeToString(sum[:8]))
}

// ---- bảng nextStep

type nextStepRow struct {
	Workflow string `json:"workflow"`
	Status   string `json:"status"`
	Plan     string `json:"plan"` // nil | plan | noplan
	Check    bool   `json:"awaiting_check"`
	Land     bool   `json:"awaiting_land"`
	NoReview bool   `json:"no_review"`
	Ran      int    `json:"ran"`
	Next     string `json:"next"`
	After    string `json:"status_after"`
}

func goldenNextStepTable(t *testing.T) []nextStepRow {
	t.Helper()
	var rows []nextStepRow
	h := newHarness(t)
	r := h.runner()
	for _, wf := range []string{StepDo, StepReq, StepFeat, StepReview} {
		for _, st := range []string{task.StatusCreated, task.StatusPlanned, task.StatusInProgress, task.StatusInReview} {
			for _, plan := range []string{"nil", "plan", "noplan"} {
				for _, mask := range []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15} {
					check, land, noReview, ran := mask&1 != 0, mask&2 != 0, mask&4 != 0, mask>>3
					var tk *task.Task
					// finalize chỉ được gọi từ nextStep của b-do ở in_review: cần phiếu thật.
					if wf == StepDo && st == task.StatusInReview && (land || noReview) {
						created, _, err := r.NewTask(context.Background(), NewTaskOptions{Workflow: wf, Request: "Đổi app.txt", Plan: ptr(false)})
						if err != nil {
							t.Fatal(err)
						}
						tk = created
					} else {
						tk = &task.Task{State: task.State{ID: "golden"}}
					}
					tk.State.Status, tk.State.AwaitingCheck, tk.State.AwaitingLand = st, check, land
					switch plan {
					case "plan":
						tk.State.Classification = &task.Classification{Plan: true}
					case "noplan":
						tk.State.Classification = &task.Classification{Plan: false}
					default:
						tk.State.Classification = nil
					}
					if tk.State.ID != "golden" {
						nt, err := h.tasks.Update(tk.State.ID, r.now(), func(x *task.Task) error {
							x.State.Status, x.State.AwaitingCheck, x.State.AwaitingLand = st, check, land
							x.State.Classification = tk.State.Classification
							return nil
						})
						if err != nil {
							t.Fatal(err)
						}
						tk = nt
					}
					rs := goldenRunState(RunOptions{Workflow: wf, NoReview: noReview}, tk, ran)
					next := r.nextStep(rs)
					rows = append(rows, nextStepRow{Workflow: wf, Status: st, Plan: plan, Check: check, Land: land, NoReview: noReview,
						Ran: ran, Next: next, After: rs.t.State.Status})
				}
			}
		}
	}
	return rows
}

// ---- thuộc tính theo bước

type stepPropRow struct {
	Step        string   `json:"step"`
	ReadOnly    bool     `json:"readonly"`
	Role        string   `json:"role"`
	Subagent    string   `json:"subagent"`
	AgentArgsRO string   `json:"agent_args"`
	ForStep     string   `json:"for_step"`
	ForStepAll  string   `json:"for_step_all_claude"`
	ReadOnlyOf  []string `json:"readonly_role_of_step_role,omitempty"`
}

func goldenStepProps() ([]stepPropRow, map[string]bool) {
	cfg := config.Default()
	forStep := func(sel adapters.Selection, id string) string {
		h, src, err := adapters.ForStep(cfg, sel, id)
		if err != nil {
			return "err:" + err.Error()
		}
		return h + "/" + src
	}
	var rows []stepPropRow
	for _, id := range []string{"b-req", "b-feat", "b-review", "b-do", stepCheck, "verify", "", "bogus"} {
		args, _ := json.Marshal(AgentArgs(cfg, id, nil))
		args = reGoldenTestBin.ReplaceAll(args, []byte("Bash(<test-binary>:*)"))
		rows = append(rows, stepPropRow{Step: id, ReadOnly: IsReadOnly(id), Role: adapters.StepRole(id), Subagent: SubagentFor(id),
			AgentArgsRO: string(args), ForStep: forStep(adapters.Selection{}, id), ForStepAll: forStep(adapters.Selection{All: "claude"}, id)})
	}
	ro := map[string]bool{}
	for _, role := range []string{"planner", "implementer", "reviewer", "", "bogus"} {
		ro[role] = ReadOnlyRole(role)
	}
	return rows, ro
}

// ---- vết Runner.Run

type resTrace struct {
	Status         string   `json:"status"`
	Reason         string   `json:"reason,omitempty"`
	Steps          []string `json:"steps"`
	Notes          []string `json:"notes,omitempty"`
	Questions      []string `json:"questions,omitempty"`
	PlanningIssues int      `json:"planning_issues,omitempty"`
	Err            string   `json:"err,omitempty"`
}

type runTrace struct {
	Calls    []string   `json:"calls"`
	ReadOnly []bool     `json:"readonly"`
	Prompts  []string   `json:"prompts"`
	Results  []resTrace `json:"results"`
	History  []string   `json:"history"`
	Final    string     `json:"final_status"`
}

func (h *harness) result(res *Result, err error) resTrace {
	rt := resTrace{Steps: []string{}}
	if err != nil {
		rt.Err = h.norm(err.Error())
	}
	if res == nil {
		return rt
	}
	rt.Status, rt.Reason = res.Status, h.norm(res.Reason)
	for _, s := range res.Steps {
		rt.Steps = append(rt.Steps, s.Step+":"+s.Status)
	}
	for _, n := range res.Notes {
		rt.Notes = append(rt.Notes, h.norm(n))
	}
	for _, q := range res.Questions {
		rt.Questions = append(rt.Questions, h.norm(q))
	}
	rt.PlanningIssues = len(res.PlanningIssues)
	return rt
}

func (h *harness) trace(t *testing.T, id string, results []resTrace) runTrace {
	t.Helper()
	tr := runTrace{Calls: append([]string{}, h.calls...), ReadOnly: []bool{}, Prompts: []string{}, Results: results, History: []string{}}
	for _, rq := range h.reqs {
		tr.ReadOnly = append(tr.ReadOnly, rq.ReadOnly)
		tr.Prompts = append(tr.Prompts, h.promptSum(rq.Prompt))
	}
	if id != "" {
		tk := loadTask(t, h, id)
		tr.Final = tk.State.Status
		for _, tx := range tk.State.History {
			tr.History = append(tr.History, tx.From+">"+tx.To)
		}
	}
	return tr
}

const goldenRequest = "Thêm tính năng phiên bản v2 cho app.txt"

// blockingReview: vòng đầu có phát hiện nghiêm trọng, các vòng sau sạch (n = số vòng có phát hiện).
func (h *harness) blockingReview(n int) func(context.Context, string, *Env, string) error {
	rounds := 0
	return func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepReview {
			rounds++
			body := `{"summary":"ổn","findings":[]}`
			if rounds <= n {
				body = `{"summary":"lỗi","findings":[{"kind":"bug","severity":"high","title":"sai","path":"app.txt","lines":"1","evidence":"v2 sai"}]}`
			}
			_, _, err := e.SubmitReview(id, []byte(body))
			return err
		}
		return h.happy(ctx, step, e, id)
	}
}

func (h *harness) planSkipping(skip map[string]bool) func(context.Context, string, *Env, string) error {
	return func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepReq {
			return h.writePlan(e, id, skip)
		}
		return h.happy(ctx, step, e, id)
	}
}

func goldenRunScenarios(t *testing.T) map[string]runTrace {
	t.Helper()
	ctx := context.Background()
	out := map[string]runTrace{}
	scenario := func(name string, setup func(h *harness), body func(t *testing.T, h *harness, r *Runner) (string, []resTrace)) {
		t.Run("run/"+name, func(t *testing.T) {
			h := newHarness(t)
			if setup != nil {
				setup(h)
			}
			id, results := body(t, h, h.runner())
			out[name] = h.trace(t, id, results)
		})
	}
	run := func(h *harness, r *Runner, o RunOptions) (string, resTrace) {
		res, err := r.Run(ctx, o)
		id := ""
		if res != nil {
			id = res.TaskID
		}
		return id, h.result(res, err)
	}
	single := func(o RunOptions) func(t *testing.T, h *harness, r *Runner) (string, []resTrace) {
		return func(_ *testing.T, h *harness, r *Runner) (string, []resTrace) {
			id, rt := run(h, r, o)
			return id, []resTrace{rt}
		}
	}

	scenario("do_full", nil, single(RunOptions{Workflow: StepDo, Request: goldenRequest}))
	scenario("do_noplan", nil, single(RunOptions{Workflow: StepDo, Request: goldenRequest, Plan: ptr(false)}))
	scenario("do_noreview", nil, single(RunOptions{Workflow: StepDo, Request: goldenRequest, Plan: ptr(false), NoReview: true}))
	scenario("do_review_fix", func(h *harness) { h.behave = h.blockingReview(1) },
		single(RunOptions{Workflow: StepDo, Request: goldenRequest, Plan: ptr(false)}))
	scenario("do_review_exhaust", func(h *harness) { h.cfg.Workflow.MaxReviewRounds = 1; h.behave = h.blockingReview(9) },
		single(RunOptions{Workflow: StepDo, Request: goldenRequest, Plan: ptr(false)}))
	scenario("do_plan_missing_tasks", func(h *harness) { h.behave = h.planSkipping(map[string]bool{"tasks": true}) },
		single(RunOptions{Workflow: StepDo, Request: goldenRequest}))
	scenario("do_plan_writes_nothing", func(h *harness) {
		h.behave = h.planSkipping(map[string]bool{"requirement": true, "plan": true, "tasks": true})
	},
		single(RunOptions{Workflow: StepDo, Request: goldenRequest}))
	scenario("do_open_question", func(h *harness) { h.behave = h.reqWithQuestion(openQ) },
		single(RunOptions{Workflow: StepDo, Request: goldenRequest}))
	scenario("do_depth_deep", nil, single(RunOptions{Workflow: StepDo, Request: goldenRequest, Depth: task.DepthDeep}))
	scenario("do_feat_fails", func(h *harness) {
		h.behave = func(ctx context.Context, step string, e *Env, id string) error {
			if step == StepFeat {
				return fmt.Errorf("agent hỏng")
			}
			return h.happy(ctx, step, e, id)
		}
	}, single(RunOptions{Workflow: StepDo, Request: goldenRequest, Plan: ptr(false)}))
	scenario("do_protected_then_resume", func(h *harness) {
		base := h.happy
		h.behave = func(ctx context.Context, step string, e *Env, id string) error {
			if step == StepFeat {
				h.write("migrations/002.sql", "ALTER TABLE x;\n")
			}
			return base(ctx, step, e, id)
		}
	}, func(t *testing.T, h *harness, r *Runner) (string, []resTrace) {
		id, rt := run(h, r, RunOptions{Workflow: StepDo, Request: goldenRequest, Plan: ptr(false)})
		if _, _, err := r.Approve(id); err != nil {
			t.Fatal(err)
		}
		_, rt2 := run(h, r, RunOptions{Workflow: StepDo, TaskRef: id, Resume: true})
		return id, []resTrace{rt, rt2}
	})

	scenario("req_only", nil, single(RunOptions{Workflow: StepReq, Request: goldenRequest}))
	scenario("req_missing_tasks", func(h *harness) { h.behave = h.planSkipping(map[string]bool{"tasks": true}) },
		single(RunOptions{Workflow: StepReq, Request: goldenRequest}))
	scenario("req_open_question", func(h *harness) { h.behave = h.reqWithQuestion(openQ) },
		single(RunOptions{Workflow: StepReq, Request: goldenRequest}))
	scenario("req_then_do", nil, func(t *testing.T, h *harness, r *Runner) (string, []resTrace) {
		id, rt := run(h, r, RunOptions{Workflow: StepReq, Request: goldenRequest})
		_, rt2 := run(h, r, RunOptions{Workflow: StepDo, TaskRef: id})
		return id, []resTrace{rt, rt2}
	})
	scenario("req_then_feat_then_review", nil, func(t *testing.T, h *harness, r *Runner) (string, []resTrace) {
		id, rt := run(h, r, RunOptions{Workflow: StepReq, Request: goldenRequest})
		_, rt2 := run(h, r, RunOptions{Workflow: StepFeat, TaskRef: id})
		_, rt3 := run(h, r, RunOptions{Workflow: StepReview, TaskRef: id})
		return id, []resTrace{rt, rt2, rt3}
	})
	scenario("req_bad_plan_then_feat", func(h *harness) { h.behave = h.planSkipping(map[string]bool{"tasks": true}) },
		func(t *testing.T, h *harness, r *Runner) (string, []resTrace) {
			id, rt := run(h, r, RunOptions{Workflow: StepReq, Request: goldenRequest})
			_, rt2 := run(h, r, RunOptions{Workflow: StepFeat, TaskRef: id})
			return id, []resTrace{rt, rt2}
		})
	scenario("req_question_then_feat", func(h *harness) { h.behave = h.reqWithQuestion(openQ) },
		func(t *testing.T, h *harness, r *Runner) (string, []resTrace) {
			id, rt := run(h, r, RunOptions{Workflow: StepReq, Request: goldenRequest})
			_, rt2 := run(h, r, RunOptions{Workflow: StepFeat, TaskRef: id})
			return id, []resTrace{rt, rt2}
		})
	scenario("req_rerun_on_planned", nil, func(t *testing.T, h *harness, r *Runner) (string, []resTrace) {
		id, rt := run(h, r, RunOptions{Workflow: StepReq, Request: goldenRequest})
		_, rt2 := run(h, r, RunOptions{Workflow: StepReq, TaskRef: id})
		return id, []resTrace{rt, rt2}
	})

	scenario("feat_only", nil, single(RunOptions{Workflow: StepFeat, Request: goldenRequest}))
	scenario("feat_protected_then_resume", func(h *harness) {
		base := h.happy
		h.behave = func(ctx context.Context, step string, e *Env, id string) error {
			if step == StepFeat {
				h.write("migrations/002.sql", "ALTER TABLE x;\n")
			}
			return base(ctx, step, e, id)
		}
	}, func(t *testing.T, h *harness, r *Runner) (string, []resTrace) {
		id, rt := run(h, r, RunOptions{Workflow: StepFeat, Request: goldenRequest})
		if _, _, err := r.Approve(id); err != nil {
			t.Fatal(err)
		}
		_, rt2 := run(h, r, RunOptions{Workflow: StepFeat, TaskRef: id, Resume: true})
		return id, []resTrace{rt, rt2}
	})
	scenario("feat_fails", func(h *harness) {
		h.behave = func(context.Context, string, *Env, string) error { return fmt.Errorf("agent hỏng") }
	}, single(RunOptions{Workflow: StepFeat, Request: goldenRequest}))

	scenario("review_only", nil, func(t *testing.T, h *harness, r *Runner) (string, []resTrace) {
		id, rt := run(h, r, RunOptions{Workflow: StepFeat, Request: goldenRequest})
		_, rt2 := run(h, r, RunOptions{Workflow: StepReview, TaskRef: id})
		return id, []resTrace{rt, rt2}
	})
	scenario("review_only_blocking", func(h *harness) { h.behave = h.blockingReview(9) }, func(t *testing.T, h *harness, r *Runner) (string, []resTrace) {
		id, rt := run(h, r, RunOptions{Workflow: StepFeat, Request: goldenRequest})
		_, rt2 := run(h, r, RunOptions{Workflow: StepReview, TaskRef: id})
		return id, []resTrace{rt, rt2}
	})
	scenario("review_modifies_tree", func(h *harness) {
		h.behave = func(ctx context.Context, step string, e *Env, id string) error {
			if step == StepReview {
				h.write("app.txt", "sửa lén\n")
			}
			return h.happy(ctx, step, e, id)
		}
	}, single(RunOptions{Workflow: StepDo, Request: goldenRequest, Plan: ptr(false)}))
	scenario("req_modifies_tree", func(h *harness) {
		h.behave = func(ctx context.Context, step string, e *Env, id string) error {
			if step == StepReq {
				h.write("app.txt", "sửa lén\n")
			}
			return h.happy(ctx, step, e, id)
		}
	}, single(RunOptions{Workflow: StepReq, Request: goldenRequest}))
	return out
}

// ---- vết driver session

type actTrace struct {
	Call     string   `json:"call"`
	Action   string   `json:"action"`
	Status   string   `json:"task_status"`
	Step     string   `json:"step,omitempty"`
	Subagent string   `json:"subagent,omitempty"`
	ReadOnly bool     `json:"readonly,omitempty"`
	Prompt   string   `json:"prompt,omitempty"`
	Command  string   `json:"command,omitempty"`
	Reason   string   `json:"reason,omitempty"`
	Notes    []string `json:"notes,omitempty"`
	Result   string   `json:"result,omitempty"`
}

type sessTrace struct {
	Actions []actTrace `json:"actions"`
	History []string   `json:"history"`
	Final   string     `json:"final_status"`
}

func (h *harness) act(call string, a *SessionAction) actTrace {
	at := actTrace{Call: call, Action: a.Action, Status: a.Status, Step: a.Step, Subagent: a.Subagent, ReadOnly: a.ReadOnly,
		Command: h.norm(a.Command), Reason: h.norm(a.Reason)}
	if a.Prompt != "" {
		at.Prompt = h.promptSum(a.Prompt)
	}
	for _, n := range a.Notes {
		at.Notes = append(at.Notes, h.norm(n))
	}
	if a.Result != nil {
		at.Result = a.Result.Step + ":" + a.Result.Status
	}
	return at
}

// goldenDrive lặp next → subagent → complete như h.drive nhưng ghi lại từng hành động; dừng ở hành động không phải agent.
func goldenDrive(t *testing.T, h *harness, r *Runner, id string, resume bool, tr *sessTrace) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < 20; i++ {
		a, err := r.Next(ctx, NextOptions{TaskRef: id, Resume: resume && i == 0})
		if err != nil {
			t.Fatal(err)
		}
		tr.Actions = append(tr.Actions, h.act("next", a))
		if a.Action != ActionAgent {
			return
		}
		h.subagent(a)
		c, err := r.Complete(ctx, CompleteOptions{TaskRef: id, RunID: a.RunID, AgentID: "agent-" + a.Step})
		if err != nil {
			t.Fatal(err)
		}
		tr.Actions = append(tr.Actions, h.act("complete", c))
		if c.Action != "" {
			return
		}
	}
	t.Fatal("vòng session không dừng")
}

func goldenSessionScenarios(t *testing.T) map[string]sessTrace {
	t.Helper()
	ctx := context.Background()
	out := map[string]sessTrace{}
	scenario := func(name string, setup func(h *harness), opts NewTaskOptions, body func(t *testing.T, h *harness, r *Runner, id string, tr *sessTrace)) {
		t.Run("session/"+name, func(t *testing.T) {
			h := newHarness(t)
			if setup != nil {
				setup(h)
			}
			r := h.runner()
			tk := h.newSession(r, opts)
			var tr sessTrace
			body(t, h, r, tk.State.ID, &tr)
			got := loadTask(t, h, tk.State.ID)
			tr.Final = got.State.Status
			tr.History = []string{}
			for _, tx := range got.State.History {
				tr.History = append(tr.History, tx.From+">"+tx.To)
			}
			out[name] = tr
		})
	}
	plain := func(t *testing.T, h *harness, r *Runner, id string, tr *sessTrace) {
		goldenDrive(t, h, r, id, false, tr)
	}

	scenario("full", nil, NewTaskOptions{}, plain)
	scenario("noplan", nil, NewTaskOptions{Plan: ptr(false)}, plain)
	scenario("review_fix", func(h *harness) { h.behave = h.blockingReview(1) }, NewTaskOptions{Plan: ptr(false)}, plain)
	scenario("review_exhaust", func(h *harness) { h.cfg.Workflow.MaxReviewRounds = 1; h.behave = h.blockingReview(9) },
		NewTaskOptions{Plan: ptr(false)}, plain)
	scenario("plan_missing_tasks", func(h *harness) { h.behave = h.planSkipping(map[string]bool{"tasks": true}) }, NewTaskOptions{}, plain)
	scenario("open_question", func(h *harness) { h.behave = h.reqWithQuestion(openQ) }, NewTaskOptions{}, plain)
	scenario("review_modifies_tree", func(h *harness) {
		h.behave = func(ctx context.Context, step string, e *Env, id string) error {
			if step == StepReview {
				h.write("app.txt", "sửa lén\n")
			}
			return h.happy(ctx, step, e, id)
		}
	}, NewTaskOptions{Plan: ptr(false)}, plain)
	scenario("protected_then_resume", func(h *harness) {
		base := h.happy
		h.behave = func(ctx context.Context, step string, e *Env, id string) error {
			if step == StepFeat {
				h.write("migrations/002.sql", "ALTER TABLE x;\n")
			}
			return base(ctx, step, e, id)
		}
	}, NewTaskOptions{Plan: ptr(false)}, func(t *testing.T, h *harness, r *Runner, id string, tr *sessTrace) {
		goldenDrive(t, h, r, id, false, tr)
		if _, _, err := r.Approve(id); err != nil {
			t.Fatal(err)
		}
		goldenDrive(t, h, r, id, true, tr)
	})
	scenario("process_other_harness", func(h *harness) {
		h.cfg.Harnesses.Enabled = append(h.cfg.Harnesses.Enabled, "codex")
		h.cfg.Roles.Implementer = "codex"
		h.cfg.Roles.Reviewer = "codex"
		h.behave = featWrites(h, nil)
	}, NewTaskOptions{Plan: ptr(false)}, func(t *testing.T, h *harness, r *Runner, id string, tr *sessTrace) {
		step := func(call string, a *SessionAction, err error) {
			if err != nil {
				t.Fatal(err)
			}
			tr.Actions = append(tr.Actions, h.act(call, a))
		}
		a, err := r.Next(ctx, NextOptions{TaskRef: id})
		step("next", a, err)
		res, err := h.runner().Run(ctx, RunOptions{Workflow: StepFeat, TaskRef: id})
		tr.Actions = append(tr.Actions, actTrace{Call: "run b-feat", Action: res.Status})
		if err != nil {
			t.Fatal(err)
		}
		a, err = r.Next(ctx, NextOptions{TaskRef: id})
		step("next", a, err)
		res, err = h.runner().Run(ctx, RunOptions{Workflow: StepReview, TaskRef: id})
		tr.Actions = append(tr.Actions, actTrace{Call: "run b-review", Action: res.Status})
		if err != nil {
			t.Fatal(err)
		}
		a, err = r.Next(ctx, NextOptions{TaskRef: id})
		step("next", a, err)
	})
	scenario("process_review_blocking", func(h *harness) {
		h.cfg.Harnesses.Enabled = append(h.cfg.Harnesses.Enabled, "codex")
		h.cfg.Roles.Reviewer = "codex"
		h.behave = func(ctx context.Context, step string, e *Env, id string) error {
			if step == StepReview {
				_, _, err := e.SubmitReview(id, []byte(`{"summary":"lỗi","findings":[{"kind":"bug","severity":"high","title":"sai","path":"app.txt","lines":"1","evidence":"v2 sai"}]}`))
				return err
			}
			return featWrites(h, nil)(ctx, step, e, id)
		}
	}, NewTaskOptions{Plan: ptr(false)}, func(t *testing.T, h *harness, r *Runner, id string, tr *sessTrace) {
		goldenDrive(t, h, r, id, false, tr)
		if _, err := h.runner().Run(ctx, RunOptions{Workflow: StepReview, TaskRef: id}); err != nil {
			t.Fatal(err)
		}
		a, err := r.Next(ctx, NextOptions{TaskRef: id})
		if err != nil {
			t.Fatal(err)
		}
		tr.Actions = append(tr.Actions, h.act("next", a))
	})
	scenario("abort_mid_feat", nil, NewTaskOptions{Plan: ptr(false)}, func(t *testing.T, h *harness, r *Runner, id string, tr *sessTrace) {
		a, err := r.Next(ctx, NextOptions{TaskRef: id})
		if err != nil {
			t.Fatal(err)
		}
		tr.Actions = append(tr.Actions, h.act("next", a))
		h.write("app.txt", "dở dang\n")
		ab, err := r.Abort(ctx, id, "thử")
		if err != nil {
			t.Fatal(err)
		}
		tr.Actions = append(tr.Actions, h.act("abort", ab))
	})
	return out
}

// ---- tập hợp và so khớp

type flowGolden struct {
	NextStep     []nextStepRow        `json:"next_step"`
	StepProps    []stepPropRow        `json:"step_props"`
	ReadOnlyRole map[string]bool      `json:"readonly_role"`
	Runs         map[string]runTrace  `json:"runs"`
	Sessions     map[string]sessTrace `json:"sessions"`
}

func TestFlowGoldenRunnerBehaviour(t *testing.T) {
	props, ro := goldenStepProps()
	g := flowGolden{NextStep: goldenNextStepTable(t), StepProps: props, ReadOnlyRole: ro,
		Runs: goldenRunScenarios(t), Sessions: goldenSessionScenarios(t)}
	got, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if os.Getenv("BINT_UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenFlowPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("đã ghi %s (%d byte)", goldenFlowPath, len(got))
		return
	}
	want, err := os.ReadFile(goldenFlowPath)
	if err != nil {
		t.Fatalf("thiếu golden (sinh bằng BINT_UPDATE_GOLDEN=1 trên mã cũ): %v", err)
	}
	if string(want) == string(got) {
		return
	}
	var w flowGolden
	if err := json.Unmarshal(want, &w); err != nil {
		t.Fatal(err)
	}
	diffGolden(t, "next_step", w.NextStep, g.NextStep)
	diffGolden(t, "step_props", w.StepProps, g.StepProps)
	diffGolden(t, "readonly_role", w.ReadOnlyRole, g.ReadOnlyRole)
	for k, v := range w.Runs {
		diffGolden(t, "runs/"+k, v, g.Runs[k])
	}
	for k, v := range w.Sessions {
		diffGolden(t, "sessions/"+k, v, g.Sessions[k])
	}
	for k := range g.Runs {
		if _, ok := w.Runs[k]; !ok {
			t.Errorf("runs/%s: không có trong golden", k)
		}
	}
	for k := range g.Sessions {
		if _, ok := w.Sessions[k]; !ok {
			t.Errorf("sessions/%s: không có trong golden", k)
		}
	}
	t.Fatal("golden không khớp")
}

func diffGolden(t *testing.T, name string, want, got any) {
	t.Helper()
	w, _ := json.MarshalIndent(want, "", " ")
	g, _ := json.MarshalIndent(got, "", " ")
	if string(w) == string(g) {
		return
	}
	// In dòng khác đầu tiên của từng phần để chỉ ra chỗ lệch mà không đổ cả bảng.
	wl, gl := strings.Split(string(w), "\n"), strings.Split(string(g), "\n")
	for i := 0; i < len(wl) || i < len(gl); i++ {
		var a, b string
		if i < len(wl) {
			a = wl[i]
		}
		if i < len(gl) {
			b = gl[i]
		}
		if a != b {
			lo := max(i-6, 0)
			t.Errorf("%s lệch ở dòng %d:\n--- golden\n%s\n--- thực tế\n%s", name, i+1,
				strings.Join(wl[lo:min(i+3, len(wl))], "\n"), strings.Join(gl[lo:min(i+3, len(gl))], "\n"))
			return
		}
	}
}
