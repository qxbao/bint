package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"bint/internal/eval"
	"bint/internal/task"
)

// Kiểm tra mục tiêu độc lập và đóng băng (b-goal, goalfreeze.go): kiểm tra mục tiêu `TestGoalFreezeBlackBox` của phiếu 01M7NA28NAXVSE14HNHACRZG97.

const (
	goalSpecMark = "GOAL-SPEC-7731" // chỉ có ở requirement.md (mục Kiểm tra mục tiêu)
	planOnlyMark = "PLAN-ONLY-4410" // chỉ có ở plan.md và tasks.md
	frozenRel    = "goal/check.sh"
	frozenBody   = "# FROZEN-1\ntest -f done.txt\n"
)

// goalBoxDocs là bộ artifact NHỎ hợp lệ; withGoal=false bỏ mục Kiểm tra mục tiêu (phiếu cũ).
func goalBoxDocs(withGoal bool) map[string]string {
	const crit = "app.txt ghi v2"
	req := "# Yêu cầu\n## Mục tiêu và phạm vi\nĐổi app.txt\n## Tiêu chí chấp nhận\n- [ ] AC-1: " + crit + "\n## Ngoài phạm vi\nKhông có\n"
	if withGoal {
		req += "## Kiểm tra mục tiêu\n`TestAppV2` quét app.txt (" + goalSpecMark + "), đạt khi ghi v2\n"
	}
	req += "## Câu hỏi mở\n"
	return map[string]string{
		"requirement": req,
		"plan":        "# Kế hoạch\nSửa app.txt " + planOnlyMark + "\n## Cỡ kế hoạch\nKết luận: nhỏ\n- `app.txt`\n- `done.txt`\n## Bề mặt bị ảnh hưởng\n- app.txt — AC-1\n- done.txt\n",
		"tasks":       "- [ ] TSK-1 [AC-1]: sửa app.txt " + planOnlyMark + " — xong khi: " + crit + "\n",
	}
}

// goalBox dựng harness: b-req ghi goalBoxDocs; b-goal ghi goal/check.sh (chạy bằng `sh {test_files}` qua affected_command, đỏ tới khi done.txt có);
// b-feat do feat quyết định; review mặc định của h.happy.
func goalBox(t *testing.T, withGoal bool, feat func(ctx context.Context, h *harness, e *Env, id string) error) *harness {
	t.Helper()
	h := newHarness(t)
	h.cfg.Workflow.Verify.AffectedCommand = "sh {test_files}"
	h.goal = func(_ context.Context, e *Env, _ string) error {
		p := filepath.Join(e.Root, filepath.FromSlash(frozenRel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		return os.WriteFile(p, []byte(frozenBody), 0o644)
	}
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		switch step {
		case StepReq:
			if _, _, err := e.SetTicket(id, TicketEdit{ClearCriteria: true, Criteria: []string{"app.txt ghi v2"}}); err != nil {
				return err
			}
			for n, body := range goalBoxDocs(withGoal) {
				if _, _, err := e.WriteArtifact(id, n, body); err != nil {
					return err
				}
			}
			return nil
		case StepFeat:
			if feat != nil {
				return feat(ctx, h, e, id)
			}
		}
		return h.happy(ctx, step, e, id)
	}
	return h
}

func goalSHA(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// recordRoles bọc factory agent của r để ghi role của từng bước (StepSpec.Role).
func recordRoles(r *Runner) map[string]string {
	roles := map[string]string{}
	orig := r.Agent
	r.Agent = func(spec StepSpec, maxCost *float64, maxTurns *int) (eval.Agent, error) {
		roles[spec.ID] = spec.Role
		return orig(spec, maxCost, maxTurns)
	}
	return roles
}

func TestGoalFreezeBlackBox(t *testing.T) {
	ctx := context.Background()

	t.Run("b-goal độc lập, đóng băng, cổng riêng, b-feat không viết lại", func(t *testing.T) {
		h := goalBox(t, true, nil)
		r := h.runner()
		roles := recordRoles(r)
		res, err := r.Run(ctx, RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(true)})
		if err != nil || res.Status != task.StatusDone {
			t.Fatalf("%+v %v calls=%v", res, err, h.calls)
		}
		if got := strings.Join(h.calls, ","); got != "b-req,b-goal,b-feat,b-review" {
			t.Fatalf("bước đã chạy: %s", got)
		}
		if roles[StepGoal] != "planner" {
			t.Errorf("b-goal phải theo role planner, được %q", roles[StepGoal])
		}
		tk := loadTask(t, h, res.TaskID)
		// Đề bài b-goal: có mục tiêu, không có plan/tasks.
		gp := h.prompts[StepGoal]
		if !strings.Contains(gp, goalSpecMark) {
			t.Errorf("đề bài b-goal thiếu requirement.md (%s):\n%s", goalSpecMark, gp)
		}
		for _, bad := range []string{planOnlyMark, tk.ArtifactPath("plan"), tk.ArtifactPath("tasks"), "TSK-1"} {
			if strings.Contains(gp, bad) {
				t.Errorf("đề bài b-goal không được chứa %q:\n%s", bad, gp)
			}
		}
		// Đóng băng: tệp + sha256 + scope.deny.
		gt := tk.State.GoalTest
		if gt == nil || !slices.Equal(gt.Paths(), []string{frozenRel}) || gt.Files[0].SHA256 != goalSHA(frozenBody) {
			t.Fatalf("GoalTest = %+v", gt)
		}
		if !slices.Contains(tk.Ticket.Scope.Deny, frozenRel) {
			t.Errorf("scope.deny thiếu %s: %v", frozenRel, tk.Ticket.Scope.Deny)
		}
		// Cổng riêng: mốc đỏ trước b-feat, xanh sau b-feat; log và kết quả riêng, không lẫn vào verify.
		if len(tk.State.GoalChecks) != 2 || tk.State.GoalChecks[0].Passed || !tk.State.GoalChecks[1].Passed || tk.State.GoalChecks[0].Reason != goalReasonBaseline {
			t.Fatalf("GoalChecks = %+v", tk.State.GoalChecks)
		}
		for _, n := range []string{"goal-1.log", "goal-2.log"} {
			if _, err := os.Stat(tk.LogPath(n)); err != nil {
				t.Errorf("thiếu %s: %v", n, err)
			}
		}
		if got := tk.State.GoalChecks[1].Command; got != "sh "+frozenRel {
			t.Errorf("cổng mục tiêu phải dùng affected_command trên tệp đóng băng, chạy %q", got)
		}
		if v := tk.State.LastVerification(); v == nil || !v.Passed || strings.Contains(v.Log, "goal-") {
			t.Errorf("kiểm chứng riêng của phiếu: %+v", v)
		}
		// b-feat không còn bị bảo viết test mục tiêu.
		fp := h.prompts[StepFeat]
		if !strings.Contains(fp, "ĐÃ ĐƯỢC VIẾT độc lập") || !strings.Contains(fp, frozenRel) || strings.Contains(fp, "viết thành test tự động") {
			t.Errorf("đề bài b-feat:\n%s", fp)
		}
		// Tệp đóng băng còn nguyên không bị hỏi duyệt dù khớp scope.deny.
		if len(tk.State.PendingPaths) != 0 {
			t.Errorf("tệp đóng băng nguyên vẹn không được chờ duyệt: %v", tk.State.PendingPaths)
		}
	})

	t.Run("b-goal phiên mới, b-feat vẫn nối tiếp phiên b-req", func(t *testing.T) {
		procSetup(t)
		h := goalBox(t, true, nil)
		h.claudeLike(nil)
		res, err := handoffRunner(h).Run(ctx, RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(true)})
		if err != nil || res.Status != task.StatusDone || strings.Join(h.calls, ",") != "b-req,b-goal,b-feat,b-review" {
			t.Fatalf("%+v %v calls=%v", res, err, h.calls)
		}
		var plan, goal, feat eval.AgentRequest
		for i, c := range h.calls {
			switch c {
			case StepReq:
				plan = h.reqs[i]
			case StepGoal:
				goal = h.reqs[i]
			case StepFeat:
				feat = h.reqs[i]
			}
		}
		if plan.SessionID == "" || goal.ResumeSessionID != "" || goal.SessionID == "" || goal.SessionID == plan.SessionID {
			t.Errorf("b-goal phải là phiên mới, không nối tiếp b-req: plan=%q goal=%+v", plan.SessionID, goal)
		}
		if feat.ResumeSessionID != plan.SessionID {
			t.Errorf("b-feat phải nối tiếp phiên b-req %q: %+v", plan.SessionID, feat)
		}
		if !strings.Contains(feat.Prompt, "ĐÃ ĐƯỢC VIẾT độc lập") || strings.Contains(feat.Prompt, "viết thành test tự động") {
			t.Errorf("lời nhắn nối tiếp phải nêu test đóng băng:\n%s", feat.Prompt)
		}
	})

	// Sửa hoặc xoá tệp đóng băng: blocked trước kiểm chứng và review, approve không bỏ qua; khôi phục tệp rồi resume thì đi tiếp.
	for _, tc := range []struct {
		name   string
		tamper func(h *harness)
		want   string
	}{
		{"b-feat sửa tệp đóng băng", func(h *harness) { h.write(frozenRel, "# FROZEN-1\ntrue\n") }, "nội dung đã sửa"},
		{"b-feat xoá tệp đóng băng", func(h *harness) { os.Remove(filepath.Join(h.root, filepath.FromSlash(frozenRel))) }, "bị xoá hoặc đổi tên"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := goalBox(t, true, func(ctx context.Context, h *harness, e *Env, id string) error {
				if err := h.happy(ctx, StepFeat, e, id); err != nil {
					return err
				}
				tc.tamper(h)
				return nil
			})
			r := h.runner()
			res, err := r.Run(ctx, RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(true)})
			if err != nil || res.Status != task.StatusBlocked || !strings.Contains(res.Reason, frozenRel) || !strings.Contains(res.Reason, tc.want) {
				t.Fatalf("phải blocked nêu %s (%s): %+v %v", frozenRel, tc.want, res, err)
			}
			if got := strings.Join(h.calls, ","); got != "b-req,b-goal,b-feat" {
				t.Fatalf("không được kiểm chứng/review khi tệp đóng băng bị sửa: %s", got)
			}
			tk := loadTask(t, h, res.TaskID)
			if len(tk.State.Verifications) != 0 || len(tk.State.GoalChecks) != 1 {
				t.Errorf("chặn phải đứng trước kiểm chứng và cổng mục tiêu: verify=%d goal=%d", len(tk.State.Verifications), len(tk.State.GoalChecks))
			}
			// Duyệt đường dẫn không bỏ qua cổng.
			r.Approve(res.TaskID) // duyệt mọi thay đổi đang chờ (đọc diff hiện tại, gồm tệp đóng băng nếu bị sửa)
			res2, err := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true})
			if err != nil || res2.Status != task.StatusBlocked || !strings.Contains(res2.Reason, frozenRel) {
				t.Fatalf("approve không được bỏ qua cổng đóng băng: %+v %v", res2, err)
			}
			if got := strings.Join(h.calls, ","); got != "b-req,b-goal,b-feat" {
				t.Fatalf("resume không gọi lại agent: %s", got)
			}
			// Khôi phục tệp: đi tiếp tới done.
			h.write(frozenRel, frozenBody)
			res3, err := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true})
			if err != nil || res3.Status != task.StatusDone {
				t.Fatalf("sau khi khôi phục: %+v %v calls=%v", res3, err, h.calls)
			}
			if tk = loadTask(t, h, res.TaskID); len(tk.State.GoalChecks) != 2 || !tk.State.GoalChecks[1].Passed {
				t.Errorf("GoalChecks = %+v", tk.State.GoalChecks)
			}
		})
	}

	t.Run("cổng mục tiêu đỏ: review sạch vẫn mở vòng sửa, không done khi còn đỏ", func(t *testing.T) {
		feats := 0
		h := goalBox(t, true, func(ctx context.Context, h *harness, e *Env, id string) error {
			feats++
			if feats == 1 {
				h.write("app.txt", "v2\n") // done.txt chưa có: cổng mục tiêu đỏ (và kiểm chứng đỏ)
				return nil
			}
			return h.happy(ctx, StepFeat, e, id)
		})
		res, err := h.runner().Run(ctx, RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(true)})
		if err != nil || res.Status != task.StatusDone {
			t.Fatalf("%+v %v calls=%v", res, err, h.calls)
		}
		tk := loadTask(t, h, res.TaskID)
		if last := tk.State.LastGoalCheck(); last == nil || !last.Passed || len(tk.State.GoalChecks) < 3 {
			t.Fatalf("lần cổng mục tiêu cuối phải qua sau vòng sửa: %+v", tk.State.GoalChecks)
		}
		if !strings.Contains(h.prompts[StepFeat], "ĐÃ ĐƯỢC VIẾT độc lập") {
			t.Errorf("đề bài vòng sửa:\n%s", h.prompts[StepFeat])
		}
	})

	t.Run("đường nhanh không chạy b-goal, đề bài như cũ", func(t *testing.T) {
		h := newHarness(t)
		h.behave = h.reviewCounts(0)
		res, err := h.runner().Run(ctx, RunOptions{Workflow: StepDo, Request: fastReq, Fast: true})
		if err != nil || res.Status != task.StatusDone || strings.Join(h.calls, ",") != "b-feat,b-review" {
			t.Fatalf("%+v %v calls=%v", res, err, h.calls)
		}
		tk := loadTask(t, h, res.TaskID)
		if tk.State.GoalTest != nil || len(tk.State.GoalChecks) != 0 {
			t.Errorf("đường nhanh không có kiểm tra mục tiêu đóng băng: %+v %+v", tk.State.GoalTest, tk.State.GoalChecks)
		}
		if !strings.Contains(h.prompts[StepFeat], "Kiểm tra mục tiêu: TRƯỚC khi sửa") {
			t.Errorf("đề bài đường nhanh phải giữ khối cũ:\n%s", h.prompts[StepFeat])
		}
	})

	t.Run("phiếu không có mục Kiểm tra mục tiêu giữ hành vi cũ", func(t *testing.T) {
		h := goalBox(t, false, nil)
		r := h.runner()
		res, err := r.Run(ctx, RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(true)})
		if err != nil || res.Status != task.StatusBlocked || !strings.HasPrefix(res.Reason, "kế hoạch lớn:") {
			t.Fatalf("thiếu mục là kế hoạch lớn: %+v %v", res, err)
		}
		res, err = r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true})
		if err != nil || res.Status != task.StatusDone || strings.Join(h.calls, ",") != "b-req,b-feat,b-review" {
			t.Fatalf("%+v %v calls=%v", res, err, h.calls)
		}
		if tk := loadTask(t, h, res.TaskID); tk.State.GoalTest != nil || len(tk.State.GoalChecks) != 0 {
			t.Errorf("không có b-goal: %+v", tk.State)
		}
	})

	t.Run("driver session", func(t *testing.T) {
		h := goalBox(t, true, nil)
		r := h.runner()
		tk := h.newSession(r, NewTaskOptions{Plan: ptr(true)})
		steps, last := h.drive(r, tk.State.ID, false)
		if strings.Join(steps, ",") != "b-req,b-goal,b-feat,b-review" || last.Action != ActionDone {
			t.Fatalf("bước %v, dừng %+v", steps, last)
		}
		got := loadTask(t, h, tk.State.ID)
		if got.State.GoalTest == nil || len(got.State.GoalChecks) != 2 || !got.State.GoalChecks[1].Passed {
			t.Fatalf("session: %+v %+v", got.State.GoalTest, got.State.GoalChecks)
		}
		if p := h.prompts[StepGoal]; !strings.Contains(p, goalSpecMark) || strings.Contains(p, planOnlyMark) {
			t.Errorf("đề bài b-goal của subagent:\n%s", p)
		}
	})
}

// sessionGoal chạy bước b-goal của driver session (Next → subagent → Complete) và kiểm nó là bước kế tiếp.
func sessionGoal(t *testing.T, h *harness, r *Runner, id string, next NextOptions) {
	t.Helper()
	ctx := context.Background()
	act, err := r.Next(ctx, next)
	if err != nil || act.Action != ActionAgent || act.Step != StepGoal {
		t.Fatalf("b-goal phải là bước kế tiếp: %+v %v", act, err)
	}
	h.subagent(act)
	if c, err := r.Complete(ctx, CompleteOptions{TaskRef: id, RunID: act.RunID, AgentID: "G-" + act.RunID}); err != nil || c.Action != "" {
		t.Fatalf("hoàn tất b-goal: %+v %v", c, err)
	}
}
