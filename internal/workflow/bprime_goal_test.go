package workflow

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"bint/internal/config"
	"bint/internal/harness/hookcore"
	"bint/internal/task"
)

// Kiểm tra mục tiêu của tác vụ 01M7VM1BQK174XNBNSTWTCSF4H (luồng B′, giai đoạn 2): xem requirement.md mục "Kiểm tra mục tiêu".
// Mỗi ô (loại, nơi) báo riêng bằng t.Errorf với nhãn Lx×Ny.

const (
	bpProbeRel   = "goal/probe.sh"
	bpProbeBody  = "# Loại đầu vào: có nhãn, khoá PEM, URL userinfo\ntest -f done.txt\n"
	bpNoMarkBody = "test -f done.txt\n"
	bpMinutes    = time.Minute
)

// bpOpts dựng harness B′/classic: b-req ghi bộ tài liệu gọn (B′) hoặc đầy đủ (classic); b-goal do goal quyết định (nil = mặc định của harness).
type bpOpts struct {
	variant string
	reqGoal bool // requirement.md có mục Kiểm tra mục tiêu (dạng cũ)
	goal    func(context.Context, *Env, string) error
	feat    func(ctx context.Context, h *harness, e *Env, id string) error
	review  func(h *harness) func(context.Context, string, *Env, string) error
	cfg     func(h *harness)
}

func bpHarness(t *testing.T, o bpOpts) *harness {
	t.Helper()
	h := newHarness(t)
	h.cfg.Workflow.Variant = o.variant
	h.cfg.Workflow.Verify.AffectedCommand = "sh {test_files}"
	if o.cfg != nil {
		o.cfg(h)
	}
	h.goal = o.goal
	rev := h.happy
	if o.review != nil {
		rev = o.review(h)
	}
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		switch {
		case step == StepReq && o.variant == config.VariantBPrime:
			const crit = "app.txt ghi v2"
			if _, _, err := e.SetTicket(id, TicketEdit{ClearCriteria: true, Criteria: []string{crit}}); err != nil {
				return err
			}
			docs := leanDocs(crit)
			if o.reqGoal {
				docs["requirement"] += "## Kiểm tra mục tiêu\n`TestAppV2` quét app.txt (" + goalSpecMark + "), đạt khi ghi v2\n"
			}
			docs["plan"] += planOnlyMark + "\n"
			for n, body := range docs {
				if _, _, err := e.WriteArtifact(id, n, body); err != nil {
					return err
				}
			}
			return nil
		case step == StepFeat && o.feat != nil:
			return o.feat(ctx, h, e, id)
		}
		return rev(ctx, step, e, id)
	}
	return h
}

func bpWriteProbe(body string) func(context.Context, *Env, string) error {
	return func(_ context.Context, e *Env, _ string) error {
		p := filepath.Join(e.Root, filepath.FromSlash(bpProbeRel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		return os.WriteFile(p, []byte(body), 0o644)
	}
}

// bpReq trả yêu cầu giao cho lần gọi thứ nth (từ 0) của bước step.
func bpReq(t *testing.T, h *harness, step string, nth int) (prompt string, env map[string]string, timeout time.Duration) {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for i, c := range h.calls {
		if c != step {
			continue
		}
		if n == nth {
			env = map[string]string{}
			for _, kv := range h.reqs[i].Env {
				k, v, _ := strings.Cut(kv, "=")
				env[k] = v
			}
			return h.reqs[i].Prompt, env, h.reqs[i].Timeout
		}
		n++
	}
	t.Fatalf("không có lần gọi %s #%d: calls=%v", step, nth, h.calls)
	return "", nil, 0
}

func bpCount(calls []string, step string) int {
	n := 0
	for _, c := range calls {
		if c == step {
			n++
		}
	}
	return n
}

func bpDenyList(env map[string]string) []string {
	return strings.Split(env[hookcore.EnvDenyRead], "\n")
}

func TestBPrimeGoalReviewGoal(t *testing.T) {
	ctx := context.Background()
	run := func(t *testing.T, h *harness) (*Runner, *Result) {
		t.Helper()
		r := h.runner()
		res, err := r.Run(ctx, RunOptions{Workflow: StepDo, Request: vagueGoalReq})
		if err != nil {
			t.Fatal(err)
		}
		return r, res
	}
	asked := func() func(context.Context, *harness, *Env, string) error {
		n := 0
		return func(_ context.Context, h *harness, e *Env, id string) error {
			h.write("app.txt", "v2\n")
			h.write("done.txt", "ok\n")
			// Giai đoạn 4: b-feat đầu là lõi (chưa có thước đo đóng băng); tranh chấp thước đo đến ở lượt MỞ RỘNG (lần gọi thứ hai).
			if n++; n == 2 {
				_, err := e.RecordQuestion(id, StepFeat, "Thước đo goal/probe.sh có vẻ sai?")
				return err
			}
			return nil
		}
	}

	// L1×N1: requirement.md có mục Kiểm tra mục tiêu nhưng B′ vẫn là thăm dò hộp đen, rào đọc không chừa requirement.md.
	t.Run("L1 dạng cũ trong requirement.md", func(t *testing.T) {
		h := bpHarness(t, bpOpts{variant: config.VariantBPrime, reqGoal: true, goal: bpWriteProbe(bpProbeBody)})
		_, res := run(t, h)
		tk := loadTask(t, h, res.TaskID)
		gp, env, _ := bpReq(t, h, StepGoal, 0)
		if !strings.Contains(gp, bprimeGoalMarker) {
			t.Errorf("(L1×N1) đề bài b-goal thiếu khối thăm dò B′")
		}
		for _, bad := range []string{goalSpecMark, planOnlyMark, tk.ArtifactPath("requirement"), tk.ArtifactPath("plan"), tk.ArtifactPath("tasks")} {
			if strings.Contains(gp, bad) {
				t.Errorf("(L1×N1) đề bài b-goal B′ không được chứa %q", bad)
			}
		}
		if !strings.Contains(gp, vagueGoalReq) {
			t.Errorf("(L1×N1) nguồn của thăm dò phải là yêu cầu gốc %q", vagueGoalReq)
		}
		deny := bpDenyList(env)
		for _, want := range []string{tk.Dir, tk.ArtifactPath("plan"), tk.ArtifactPath("tasks"), tk.ArtifactPath("requirement")} {
			if !slices.Contains(deny, want) {
				t.Errorf("(L1×N1) rào đọc thiếu %q: %v", want, deny)
			}
		}
		for _, d := range deny {
			if strings.HasPrefix(d, "!") {
				t.Errorf("(L1×N1) rào đọc B′ không được có ngoại lệ: %q", d)
			}
		}
	})

	// L7×N1: goal_check off thì b-goal 0 lần.
	t.Run("L7 goal_check off", func(t *testing.T) {
		h := bpHarness(t, bpOpts{variant: config.VariantBPrime, goal: bpWriteProbe(bpProbeBody),
			cfg: func(h *harness) { h.cfg.Workflow.GoalCheck = config.GateOff }})
		_, res := run(t, h)
		if n := bpCount(h.calls, StepGoal); n != 0 {
			t.Errorf("(L7×N1) goal_check off: b-goal được gọi %d lần, muốn 0 (calls=%v)", n, h.calls)
		}
		if loadTask(t, h, res.TaskID).State.GoalTest != nil {
			t.Errorf("(L7×N2) goal_check off: không được có thước đo đóng băng")
		}
	})

	// L2×N2/N3/N4/N5: thăm dò có dòng `Loại đầu vào:` được đóng băng; b-feat có lối thoát; review một lượt, không thấy kế hoạch; vòng sửa có trần.
	t.Run("L2 thăm dò có dòng Loại đầu vào", func(t *testing.T) {
		h := bpHarness(t, bpOpts{variant: config.VariantBPrime, goal: bpWriteProbe(bpProbeBody),
			review: func(h *harness) func(context.Context, string, *Env, string) error { return h.reviewCounts(1, 0) }})
		_, res := run(t, h)
		tk := loadTask(t, h, res.TaskID)
		gt := tk.State.GoalTest
		if gt == nil || !slices.Equal(gt.Paths(), []string{bpProbeRel}) {
			t.Fatalf("(L2×N2) GoalTest = %+v", gt)
		}
		if !slices.Contains(tk.Ticket.Scope.Deny, bpProbeRel) {
			t.Errorf("(L2×N2) scope.deny thiếu %s: %v", bpProbeRel, tk.Ticket.Scope.Deny)
		}
		core, _, _ := bpReq(t, h, StepFeat, 0)
		if strings.Contains(core, bpProbeRel) || !strings.Contains(core, "LÕI trước") {
			t.Errorf("(L2×N3) đề bài b-feat lõi không được thấy thước đo và phải là khối lõi")
		}
		fp, _, _ := bpReq(t, h, StepFeat, 1) // lượt mở rộng
		for _, want := range []string{"Thước đo thăm dò hộp đen (luồng B′)", "--question", "--drop-goal", bpProbeRel, "lượt MỞ RỘNG"} {
			if !strings.Contains(fp, want) {
				t.Errorf("(L2×N3) đề bài b-feat thiếu %q", want)
			}
		}
		if strings.Contains(fp, "ĐÃ ĐƯỢC VIẾT độc lập (b-goal, từ requirement.md)") {
			t.Errorf("(L2×N3) b-feat B′ không được nhận khối đóng băng kiểu cũ")
		}
		// N4: review không thấy kế hoạch.
		rp, env, _ := bpReq(t, h, StepReview, 0)
		for _, bad := range []string{planOnlyMark, tk.ArtifactPath("plan"), tk.ArtifactPath("tasks"), "TSK-1"} {
			if strings.Contains(rp, bad) {
				t.Errorf("(L2×N4) đề bài b-review không được chứa %q", bad)
			}
		}
		deny := bpDenyList(env)
		if !slices.Contains(deny, tk.ArtifactPath("plan")) || !slices.Contains(deny, tk.ArtifactPath("tasks")) || slices.Contains(deny, tk.Dir) {
			t.Errorf("(L2×N4) rào đọc review phải cấm plan.md và tasks.md (không cấm cả thư mục): %v", deny)
		}
		// N5: lõi (b-goal chạy song song nên thứ tự b-feat/b-goal không xác định), mở rộng, một lượt review, một vòng sửa rồi finalize;
		// vòng sửa có trần min(timeout, slice).
		if got := strings.Join(h.calls, ","); got != "b-req,b-goal,b-feat,b-feat,b-review,b-feat" && got != "b-req,b-feat,b-goal,b-feat,b-review,b-feat" {
			t.Errorf("(L2×N5) bước đã chạy: %s", got)
		}
		if res.Status != task.StatusDone {
			t.Errorf("(L2×N5) trạng thái = %s (%s), muốn done", res.Status, res.Reason)
		}
		_, _, first := bpReq(t, h, StepFeat, 0)
		_, _, fix := bpReq(t, h, StepFeat, 2)
		if first != time.Duration(h.cfg.Workflow.TimeoutMinutes)*bpMinutes {
			t.Errorf("(L2×N5) b-feat đầu có trần %v, muốn timeout_minutes", first)
		}
		if want := time.Duration(min(h.cfg.Workflow.TimeoutMinutes, h.cfg.Workflow.SliceTimeoutMinutes)) * bpMinutes; fix != want {
			t.Errorf("(L2×N5) vòng sửa có trần %v, muốn %v", fix, want)
		}
	})

	// L3×N2, L4×N2: thăm dò thiếu dòng nhãn, hoặc b-goal không viết gì: 0 tệp đóng băng, không blocked, b-feat vẫn chạy, b-goal chạy đúng 1 lần.
	for _, c := range []struct {
		name string
		goal func(context.Context, *Env, string) error
	}{
		{"L3 thiếu dòng Loại đầu vào", bpWriteProbe(bpNoMarkBody)},
		{"L4 b-goal không viết tệp", func(context.Context, *Env, string) error { return nil }},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := bpHarness(t, bpOpts{variant: config.VariantBPrime, goal: c.goal})
			_, res := run(t, h)
			tk := loadTask(t, h, res.TaskID)
			if tk.State.GoalTest != nil {
				t.Errorf("(%s×N2) không được đóng băng: %+v", c.name[:2], tk.State.GoalTest)
			}
			if slices.Contains(tk.Ticket.Scope.Deny, bpProbeRel) {
				t.Errorf("(%s×N2) scope.deny không được thêm %s", c.name[:2], bpProbeRel)
			}
			if res.Status != task.StatusDone || !slices.Contains(h.calls, StepFeat) || bpCount(h.calls, StepGoal) != 1 {
				t.Errorf("(%s×N2) phải không blocked, b-feat chạy, b-goal 1 lần: %s (%s) calls=%v", c.name[:2], res.Status, res.Reason, h.calls)
			}
			if !strings.Contains(strings.Join(res.Notes, "\n"), "không có thước đo đóng băng") {
				t.Errorf("(%s×N2) thiếu ghi chú không đóng băng: %v", c.name[:2], res.Notes)
			}
		})
	}

	// L5×N2: tranh chấp rồi --drop-goal gỡ đóng băng (GoalTest và scope.deny).
	t.Run("L5 tranh chấp rồi drop-goal", func(t *testing.T) {
		h2 := bpHarness(t, bpOpts{variant: config.VariantBPrime, goal: bpWriteProbe(bpProbeBody), feat: asked()})
		r, res := run(t, h2)
		if res.Status != task.StatusBlocked || loadTask(t, h2, res.TaskID).State.GoalTest == nil {
			t.Fatalf("(L5×N2) tranh chấp phải dừng blocked với thước đo còn đóng băng: %s (%s)", res.Status, res.Reason)
		}
		res2, err := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true, DropGoal: true})
		if err != nil || res2.Status != task.StatusDone {
			t.Fatalf("(L5×N2) resume --drop-goal: %+v %v", res2, err)
		}
		tk := loadTask(t, h2, res.TaskID)
		if tk.State.GoalTest != nil || slices.Contains(tk.Ticket.Scope.Deny, bpProbeRel) {
			t.Errorf("(L5×N2) còn đóng băng: GoalTest=%+v deny=%v", tk.State.GoalTest, tk.Ticket.Scope.Deny)
		}
		if bpCount(h2.calls, StepGoal) != 1 {
			t.Errorf("(L5×N2) b-goal không được chạy lại: calls=%v", h2.calls)
		}
	})

	// L6×N2: trả lời rồi resume thường giữ nguyên thước đo.
	t.Run("L6 tranh chấp rồi resume thường", func(t *testing.T) {
		h := bpHarness(t, bpOpts{variant: config.VariantBPrime, goal: bpWriteProbe(bpProbeBody), feat: asked()})
		r, res := run(t, h)
		if res.Status != task.StatusBlocked {
			t.Fatalf("(L6×N2) tranh chấp phải blocked: %s (%s)", res.Status, res.Reason)
		}
		res2, err := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true, Answer: "thước đo đúng, tiếp tục"})
		if err != nil || res2.Status != task.StatusDone {
			t.Fatalf("(L6×N2) resume --answer: %+v %v", res2, err)
		}
		tk := loadTask(t, h, res.TaskID)
		if gt := tk.State.GoalTest; gt == nil || !slices.Equal(gt.Paths(), []string{bpProbeRel}) || !slices.Contains(tk.Ticket.Scope.Deny, bpProbeRel) {
			t.Errorf("(L6×N2) thước đo phải được giữ: %+v deny=%v", gt, tk.Ticket.Scope.Deny)
		}
	})

	// N6: classic giữ nguyên: b-goal dạng cũ (đề bài chép requirement.md, rào đọc chừa requirement.md), review thấy plan.md, tới 2 vòng sửa;
	// --drop-goal bị từ chối.
	t.Run("N6 classic", func(t *testing.T) {
		h := bpHarness(t, bpOpts{variant: config.VariantClassic,
			review: func(h *harness) func(context.Context, string, *Env, string) error { return h.reviewCounts(1, 0) }})
		_, res := run(t, h)
		tk := loadTask(t, h, res.TaskID)
		if c := tk.State.Classification; c != nil && c.Variant != "" {
			t.Errorf("(N6) classic không được có variant: %+v", c)
		}
		if got := strings.Join(h.calls, ","); got != "b-req,b-goal,b-feat,b-review,b-feat,b-review" {
			t.Errorf("(N6) classic phải b-goal dạng cũ và review lại sau vòng sửa: %s", got)
		}
		gp, env, _ := bpReq(t, h, StepGoal, 0)
		if strings.Contains(gp, bprimeGoalMarker) || !strings.Contains(gp, "requirement.md (nguyên văn") {
			t.Errorf("(N6×N1) đề bài b-goal classic phải là dạng cũ")
		}
		if !slices.Contains(bpDenyList(env), "!"+tk.ArtifactPath("requirement")) {
			t.Errorf("(N6×N1) rào đọc classic phải chừa requirement.md: %v", bpDenyList(env))
		}
		rp, renv, _ := bpReq(t, h, StepReview, 0)
		if !strings.Contains(rp, tk.ArtifactPath("plan")) {
			t.Errorf("(N6×N4) review classic phải thấy kế hoạch")
		}
		if _, ok := renv[hookcore.EnvDenyRead]; ok {
			t.Errorf("(N6×N4) review classic không có rào đọc")
		}
		if err := checkDropGoal(tk, true); err == nil {
			t.Errorf("(N6) --drop-goal trên tác vụ classic phải bị từ chối")
		}
	})
}
