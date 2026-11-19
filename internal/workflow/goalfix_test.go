package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"bint/internal/eval"
	"bint/internal/task"
)

// Kiểm tra mục tiêu `TestGoalFixRound` của phiếu 01M7PD9T2ASG8XXMEM7SJ2G6C1 (sửa phát hiện review GĐ1 của b-goal): hộp đen trên harness
// agent giả của gói (goalBox/newHarness). Mỗi phát hiện có hành vi quan sát được là một subtest:
//   - #7 resume sau duyệt phạm vi tệp b-goal viết không gọi lại agent b-goal (cả `bint run` lẫn driver session);
//   - #4 người bỏ qua được cổng mục tiêu đỏ bằng land --force --reason, agent thì không;
//   - #3 cổng mục tiêu `go test` chỉ chạy đúng các hàm Test của tệp đóng băng;
//   - #8 chỉ bước b-goal nhận BINT_DENY_READ (đường dẫn tuyệt đối plan.md, tasks.md). Phần hook: TestGoalDenyReadHook (hookcore).

// goalCalls đếm số lần agent b-goal thật sự chạy và ghi nội dung khác nhau mỗi lần (lần chạy lại làm đổi sha256).
type goalCalls struct{ n int }

func (g *goalCalls) writer(rel string, bodies ...string) func(ctx context.Context, e *Env, id string) error {
	return func(_ context.Context, e *Env, _ string) error {
		g.n++
		body := bodies[len(bodies)-1]
		if g.n <= len(bodies) {
			body = bodies[g.n-1]
		}
		return writeAt(e.Root, rel, body)
	}
}

func countStep(calls []string, step string) int {
	n := 0
	for _, c := range calls {
		if c == step {
			n++
		}
	}
	return n
}

// runFlagRe bắt đối số của cờ -run trong lệnh go test (dạng `-run X`, `-run=X`, có hoặc không nháy).
var runFlagRe = regexp.MustCompile(`(?:^|\s)-{1,2}run(?:=|\s+)('[^']*'|"[^"]*"|\S+)`)

func runPattern(cmd string) (string, bool) {
	m := runFlagRe.FindStringSubmatch(cmd)
	if m == nil {
		return "", false
	}
	return strings.Trim(m[1], `'"`), true
}

func TestGoalFixRound(t *testing.T) {
	ctx := context.Background()

	// #7 (bint run): b-goal ghi tệp vào đường dẫn bảo vệ → blocked chờ duyệt → người approve → resume đi tiếp mà không gọi lại b-goal.
	t.Run("#7 resume sau duyệt phạm vi không chạy lại b-goal (bint run)", func(t *testing.T) {
		h := goalBox(t, true, nil)
		h.cfg.Limits.ProtectedPaths = append(h.cfg.Limits.ProtectedPaths, "goal/**")
		g := &goalCalls{}
		h.goal = g.writer(frozenRel, frozenBody, "# FROZEN-2 (b-goal bị gọi lại)\ntest -f done.txt\n")
		r := h.runner()
		res, err := r.Run(ctx, RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(true)})
		if err != nil || res.Status != task.StatusBlocked {
			t.Fatalf("tệp b-goal chạm đường dẫn bảo vệ phải dừng chờ duyệt: %+v %v calls=%v", res, err, h.calls)
		}
		if tk := loadTask(t, h, res.TaskID); !slices.Contains(tk.State.PendingPaths, frozenRel) {
			t.Errorf("PendingPaths phải nêu %s: %v (lý do %q)", frozenRel, tk.State.PendingPaths, res.Reason)
		}
		if _, _, err := r.Approve(res.TaskID); err != nil {
			t.Fatalf("người duyệt: %v", err)
		}
		res2, err := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true})
		if err != nil || res2.Status != task.StatusDone {
			t.Errorf("resume sau duyệt phải tới done: %+v %v", res2, err)
		}
		if got := strings.Join(h.calls, ","); got != "b-req,b-goal,b-feat,b-review" || g.n != 1 {
			t.Errorf("chuỗi bước phải có đúng một b-goal: %s (agent b-goal chạy %d lần)", got, g.n)
		}
		tk := loadTask(t, h, res.TaskID)
		gt := tk.State.GoalTest
		if gt == nil || !slices.Equal(gt.Paths(), []string{frozenRel}) || gt.Files[0].SHA256 != goalSHA(frozenBody) {
			t.Errorf("GoalTest phải ghi sha256 của nội dung đã duyệt %s: %+v", goalSHA(frozenBody), gt)
		}
		// D3: đóng băng qua stepGoalCheck (resume) vẫn phải ghi RunID của lượt b-goal, không để rỗng.
		if gt != nil && gt.RunID == "" {
			t.Errorf("GoalTest.RunID rỗng khi đóng băng sau resume: %+v", gt)
		}
		if tk.State.GoalCheckRunID != "" {
			t.Errorf("GoalCheckRunID phải được xoá sau khi đóng băng: %q", tk.State.GoalCheckRunID)
		}
	})

	// #7 (driver session `bint run next`): cùng hành vi.
	t.Run("#7 resume sau duyệt phạm vi không chạy lại b-goal (driver session)", func(t *testing.T) {
		h := goalBox(t, true, nil)
		h.cfg.Limits.ProtectedPaths = append(h.cfg.Limits.ProtectedPaths, "goal/**")
		g := &goalCalls{}
		h.goal = g.writer(frozenRel, frozenBody, "# FROZEN-2 (b-goal bị gọi lại)\ntest -f done.txt\n")
		r := h.runner()
		id := h.newSession(r, NewTaskOptions{Plan: ptr(true)}).State.ID
		steps, last := h.drive(r, id, false)
		if strings.Join(steps, ",") != "b-req,b-goal" || last.Action == ActionDone {
			t.Fatalf("session phải dừng chờ duyệt sau b-goal: bước %v, dừng %+v", steps, last)
		}
		if tk := loadTask(t, h, id); !slices.Contains(tk.State.PendingPaths, frozenRel) {
			t.Errorf("PendingPaths phải nêu %s: %v", frozenRel, tk.State.PendingPaths)
		}
		if _, _, err := r.Approve(id); err != nil {
			t.Fatalf("người duyệt: %v", err)
		}
		steps2, last2 := h.drive(r, id, true)
		if got := strings.Join(append(steps, steps2...), ","); got != "b-req,b-goal,b-feat,b-review" || g.n != 1 || last2.Action != ActionDone {
			t.Errorf("chuỗi bước phải có đúng một b-goal và tới done: %s (agent b-goal chạy %d lần), dừng %+v", got, g.n, last2)
		}
		tk := loadTask(t, h, id)
		gt := tk.State.GoalTest
		if gt == nil || !slices.Equal(gt.Paths(), []string{frozenRel}) || gt.Files[0].SHA256 != goalSHA(frozenBody) {
			t.Errorf("GoalTest phải ghi sha256 của nội dung đã duyệt %s: %+v", goalSHA(frozenBody), gt)
		}
	})

	// #4: tác vụ worktree, test đóng băng sai (luôn đỏ), b-feat làm đúng mã. Người land --force --reason được; agent bị từ chối.
	t.Run("#4 người bỏ qua cổng mục tiêu đỏ bằng land --force", func(t *testing.T) {
		if !task.BlockGoalFailed.Forceable() {
			t.Errorf("BlockGoalFailed phải thuộc Forceable")
		}
		if task.BlockGoalMissing.Forceable() {
			t.Errorf("BlockGoalMissing phải vẫn là điều chặn cứng")
		}
		const wrongGoal = "# test sai: luôn đỏ\nexit 1\n"
		h := goalBox(t, true, func(_ context.Context, _ *harness, e *Env, _ string) error {
			if err := writeAt(e.Root, "app.txt", "v2\n"); err != nil {
				return err
			}
			return writeAt(e.Root, "done.txt", "ok\n")
		}).wtHarness()
		h.goal = func(_ context.Context, e *Env, _ string) error { return writeAt(e.Root, frozenRel, wrongGoal) }
		r := h.runner()
		res, err := r.Run(ctx, RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(true), Isolation: task.IsolationWorktree})
		if err != nil || res.Status != task.StatusBlocked {
			t.Fatalf("cổng mục tiêu đỏ mãi phải dừng blocked: %+v %v calls=%v", res, err, h.calls)
		}
		id := res.TaskID
		tk := loadTask(t, h, id)
		if tk.State.Isolation() != task.IsolationWorktree || tk.State.Landed != nil {
			t.Fatalf("tác vụ worktree chưa được đưa về: isolation=%s landed=%+v", tk.State.Isolation(), tk.State.Landed)
		}
		if g := tk.State.LastGoalCheck(); g == nil || g.Passed {
			t.Fatalf("lần cổng mục tiêu cuối phải đỏ: %+v", g)
		}
		if v := tk.State.LastVerification(); v == nil || !v.Passed {
			t.Fatalf("kiểm chứng của phiếu phải xanh (b-feat làm đúng): %+v", v)
		}

		// Agent: --force bị từ chối, không land.
		agent := &Env{Root: h.root, Cfg: h.cfg, Tasks: h.tasks, Spool: h.spool, Actor: "agent"}
		if _, err := agent.Land(ctx, id, LandOptions{Force: true, Reason: "agent tự ép"}); !errors.Is(err, ErrAgent) {
			t.Errorf("agent land --force phải lỗi ErrAgent: %v", err)
		}
		if tk := loadTask(t, h, id); tk.State.Landed != nil {
			t.Fatalf("agent không được land: %+v", tk.State.Landed)
		}

		// Người: land --force --reason thành công, lý do và điều chặn cổng mục tiêu ghi lại.
		const reason = "test đóng băng sai: mâu thuẫn requirement.md"
		lr, err := r.Land(ctx, id, LandOptions{Force: true, Reason: reason})
		if err != nil {
			t.Fatalf("người land --force --reason phải đưa về được: %v", err)
		}
		if lr == nil || !slices.Contains(lr.Files, "app.txt") {
			t.Errorf("land phải đưa app.txt về: %+v", lr)
		}
		if got := h.readFile(h.root, "app.txt"); got != "v2\n" {
			t.Errorf("cây gốc phải nhận app.txt = v2, được %q", got)
		}
		l := loadTask(t, h, id).State.Landed
		if l == nil || !l.Forced {
			t.Fatalf("Landed phải ghi Forced: %+v", l)
		}
		all := l.ForceReason + "\n" + strings.Join(l.Bypassed, "\n")
		if !strings.Contains(all, reason) {
			t.Errorf("lý do ép phải được ghi: ForceReason=%q Bypassed=%v", l.ForceReason, l.Bypassed)
		}
		if !slices.ContainsFunc(l.Bypassed, func(s string) bool { return strings.Contains(s, "mục tiêu") }) {
			t.Errorf("Bypassed phải nêu điều chặn cổng mục tiêu: %v", l.Bypassed)
		}

		// docs/workflows.md nêu lối thoát này.
		doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "workflows.md"))
		if err != nil {
			t.Fatalf("đọc docs/workflows.md: %v", err)
		}
		found := false
		for para := range strings.SplitSeq(strings.ReplaceAll(string(doc), "\r\n", "\n"), "\n\n") {
			low := strings.ToLower(para)
			if strings.Contains(low, "mục tiêu") && strings.Contains(low, "land") && strings.Contains(low, "--force") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("docs/workflows.md phải có đoạn nêu cổng mục tiêu đỏ bỏ qua được bằng `bint task land --force --reason`")
		}
	})

	// #3: cổng mục tiêu chỉ chạy test mục tiêu.
	const goTestBody = "package x\n\nimport \"testing\"\n\n" +
		"func TestAlpha(t *testing.T) {}\n\n" +
		"func TestBeta(t *testing.T) { helperGamma(t) }\n\n" +
		"func helperGamma(t *testing.T) { t.Helper() }\n"
	for _, tc := range []struct {
		name, affected, rel, body string
		// wantRun: lệnh phải có -run gồm đúng TestAlpha|TestBeta; không thì lệnh phải đúng lệnh cũ (mở rộng affected_command).
		wantRun bool
	}{
		{"#3 go test + tệp _test.go thêm -run đúng hàm Test", "go test -timeout 30m {test_dirs}", "x/goal_test.go", goTestBody, true},
		{"#3 tệp không phải Go giữ lệnh cũ", "go test -timeout 30m {test_dirs}", frozenRel, frozenBody, false},
		{"#3 lệnh đã có -run giữ lệnh cũ", "go test -run TestOnly -timeout 30m {test_dirs}", "x/goal_test.go", goTestBody, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := goalBox(t, true, nil)
			h.cfg.Workflow.Verify.AffectedCommand = tc.affected
			h.goal = func(_ context.Context, e *Env, _ string) error { return writeAt(e.Root, tc.rel, tc.body) }
			res, err := h.runner().Run(ctx, RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(true)})
			if err != nil || res.TaskID == "" {
				t.Fatalf("run: %+v %v", res, err)
			}
			tk := loadTask(t, h, res.TaskID)
			if gt := tk.State.GoalTest; gt == nil || !slices.Equal(gt.Paths(), []string{tc.rel}) {
				t.Fatalf("tệp đóng băng phải là %s: %+v", tc.rel, gt)
			}
			if len(tk.State.GoalChecks) == 0 {
				t.Fatalf("cổng mục tiêu chưa chạy lần nào (calls=%v, %+v)", h.calls, res)
			}
			for i, g := range tk.State.GoalChecks {
				cmd := g.Command
				if !tc.wantRun {
					if want := eval.ExpandTestCommand(tc.affected, []string{tc.rel}); cmd != want {
						t.Errorf("GoalChecks[%d]: lệnh phải giữ như cũ %q, được %q", i, want, cmd)
					}
					continue
				}
				if !strings.HasPrefix(strings.TrimSpace(cmd), "go test") || !strings.Contains(cmd, "./x") {
					t.Errorf("GoalChecks[%d]: lệnh phải vẫn là go test trên ./x: %q", i, cmd)
				}
				pat, ok := runPattern(cmd)
				if !ok {
					t.Errorf("GoalChecks[%d]: lệnh thiếu -run: %q", i, cmd)
					continue
				}
				re, err := regexp.Compile(pat)
				if err != nil {
					t.Errorf("GoalChecks[%d]: -run %q không phải regexp hợp lệ: %v", i, pat, err)
					continue
				}
				for _, name := range []string{"TestAlpha", "TestBeta"} {
					if !re.MatchString(name) {
						t.Errorf("GoalChecks[%d]: -run %q phải khớp %s (lệnh %q)", i, pat, name, cmd)
					}
				}
				for _, name := range []string{"helperGamma", "TestAlphaBeta", "TestGamma", "XTestAlpha", "TestOther"} {
					if re.MatchString(name) {
						t.Errorf("GoalChecks[%d]: -run %q không được khớp %s (lệnh %q)", i, pat, name, cmd)
					}
				}
			}
		})
	}

	// #8: chỉ request của bước b-goal do Runner khởi chạy có BINT_DENY_READ = đường dẫn tuyệt đối plan.md và tasks.md.
	t.Run("#8 BINT_DENY_READ chỉ ở bước b-goal", func(t *testing.T) {
		h := goalBox(t, true, nil)
		res, err := h.runner().Run(ctx, RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(true)})
		if err != nil || res.Status != task.StatusDone || strings.Join(h.calls, ",") != "b-req,b-goal,b-feat,b-review" {
			t.Fatalf("%+v %v calls=%v", res, err, h.calls)
		}
		tk := loadTask(t, h, res.TaskID)
		want := []string{filepath.Clean(tk.ArtifactPath("plan")), filepath.Clean(tk.ArtifactPath("tasks")), filepath.Clean(tk.Dir), "!" + filepath.Clean(tk.ArtifactPath("requirement"))}
		for _, p := range want {
			if !filepath.IsAbs(strings.TrimPrefix(p, "!")) {
				t.Fatalf("đường dẫn artifact phải tuyệt đối: %s", p)
			}
		}
		slices.Sort(want)
		if len(h.reqs) != len(h.calls) {
			t.Fatalf("reqs/calls lệch: %d/%d", len(h.reqs), len(h.calls))
		}
		for i, step := range h.calls {
			val, has := "", false
			for _, kv := range h.reqs[i].Env {
				if k, v, _ := strings.Cut(kv, "="); k == "BINT_DENY_READ" {
					val, has = v, true
				}
			}
			if step != StepGoal {
				if has && strings.TrimSpace(val) != "" {
					t.Errorf("bước %s không được có BINT_DENY_READ: %q", step, val)
				}
				continue
			}
			var got []string
			for l := range strings.SplitSeq(strings.ReplaceAll(val, "\r\n", "\n"), "\n") {
				if l = strings.TrimSpace(l); l != "" {
					got = append(got, filepath.Clean(l))
				}
			}
			slices.Sort(got)
			if !slices.Equal(got, want) {
				t.Errorf("BINT_DENY_READ của b-goal phải gồm đúng %v (mỗi dòng một đường), được %q", want, val)
			}
		}
	})
}
