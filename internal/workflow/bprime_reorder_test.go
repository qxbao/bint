package workflow

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"bint/internal/config"
	"bint/internal/harness/hookcore"
	"bint/internal/task"
	"bint/internal/testutil"
)

// Kiểm tra mục tiêu của tác vụ 01M7VM1C4Z5VY3QVWKR5YM9TMM (luồng B′, giai đoạn 4: đảo thứ tự spec → test → mã): xem requirement.md mục "Kiểm tra mục tiêu".
// Ma trận loại (process B′, session B′, classic) × nơi (N1 thứ tự bước, N2 thăm dò độc lập, N3 gộp + đóng băng sau lõi xanh, N4 lượt mở rộng,
// N5 review, N6 lời văn ghi chú). Mỗi ô sai báo riêng bằng t.Errorf("[loại × nơi] …").

const (
	roProbeRel  = "probe/probe.sh"                                        // có dòng `Loại đầu vào:`
	roHelperRel = "probe/helper.sh"                                       // tệp phụ không có dòng nhãn
	roProbeBody = "# Loại đầu vào: có nhãn, khoá PEM\ntest -f done.txt\n" // gọi helper
)

// roWriteProbe ghi tệp thăm dò + tệp phụ vào thư mục chạy của agent b-goal giả.
func roWriteProbe(e *Env) error {
	dir := filepath.Join(e.Root, "probe")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(e.Root, filepath.FromSlash(roProbeRel)), []byte(roProbeBody), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(e.Root, filepath.FromSlash(roHelperRel)), []byte("# helper của thăm dò, không có dòng nhãn\n"), 0o644)
}

func roStepNames(res *Result) string {
	var names []string
	for _, s := range res.Steps {
		names = append(names, s.Step)
	}
	return strings.Join(names, ",")
}

// roPassedVerifs đếm lần lõi được kiểm xanh: kiểm chứng rộng xanh, hoặc kiểm hẹp cổng lõi xanh (cổng theo pha: lõi chỉ qua cổng hẹp, kể cả khi
// bgraph đòi cả bộ thì cổng hẹp chạy verify.command).
func roPassedVerifs(t *task.Task) int {
	n := 0
	for _, v := range t.State.Verifications {
		if v.Passed {
			n++
		}
	}
	for _, c := range t.State.Checks {
		if c.Passed && c.Skipped == "" && strings.HasPrefix(c.Reason, checkGateReason) {
			n++
		}
	}
	return n
}

func roWorktrees(t *testing.T, root string) int {
	t.Helper()
	out := testutil.Git(t, root, "worktree", "list", "--porcelain")
	n := 0
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "worktree ") {
			n++
		}
	}
	return n
}

func TestBPrimeReorderGoal(t *testing.T) {
	noEarlyStop(t) // các ca này đo nhánh có lượt mở rộng; dừng sớm có test riêng (TestBPrimeEarlyStopSkipsExtension)
	ctx := context.Background()

	// Loại 1: driver process B′ — lõi chạy ngay sau b-req, thăm dò chạy song song trong worktree tạm, gộp + đóng băng sau lõi xanh, một lượt mở rộng.
	t.Run("process B′", func(t *testing.T) {
		const L = "process"
		started := make(chan struct{})
		var once sync.Once
		var mu sync.Mutex
		var goalDir string
		var featN int
		var coreSawFrozen, extFrozen, extSawProbe bool
		var extPassed int
		goal := func(_ context.Context, e *Env, _ string) error {
			mu.Lock()
			goalDir = e.Root
			mu.Unlock()
			err := roWriteProbe(e)
			once.Do(func() { close(started) })
			return err
		}
		feat := func(_ context.Context, h *harness, _ *Env, id string) error {
			mu.Lock()
			featN++
			n := featN
			mu.Unlock()
			tk := loadTask(t, h, id)
			if n == 1 {
				select {
				case <-started: // b-feat lõi đang chạy mà thăm dò đã bắt đầu: song song
				case <-time.After(60 * time.Second):
					t.Errorf("[%s × N2] thăm dò không bắt đầu khi b-feat lõi còn chạy", L)
				}
				coreSawFrozen = tk.State.GoalTest != nil
			} else {
				extFrozen = tk.State.GoalTest != nil
				extPassed = roPassedVerifs(tk)
				_, err := os.Stat(filepath.Join(h.root, filepath.FromSlash(roProbeRel)))
				extSawProbe = err == nil
			}
			h.write("app.txt", "v2\n")
			h.write("done.txt", "ok\n")
			return nil
		}
		h := bpHarness(t, bpOpts{variant: config.VariantBPrime, goal: goal, feat: feat})
		r := h.runner()
		res, err := r.Run(ctx, RunOptions{Workflow: StepDo, Request: vagueGoalReq})
		if err != nil {
			t.Fatal(err)
		}
		tk := loadTask(t, h, res.TaskID)
		if res.Status != task.StatusDone {
			t.Fatalf("[%s] trạng thái = %s (%s), muốn done", L, res.Status, res.Reason)
		}
		// N1: b-feat lõi ngay sau b-req (0 bước agent chen giữa); b-goal ghi nhận sau lõi; một lượt mở rộng; một review.
		if got := roStepNames(res); got != "b-req,b-feat,b-goal,b-feat,b-review" {
			t.Errorf("[%s × N1] thứ tự bước = %s, muốn b-req,b-feat,b-goal,b-feat,b-review", L, got)
		}
		if h.calls[0] != StepReq || (h.calls[1] != StepFeat && h.calls[1] != StepGoal) {
			t.Errorf("[%s × N1] sau b-req phải là b-feat lõi (hoặc thăm dò song song): %v", L, h.calls)
		}
		// N2: thăm dò ở worktree tạm khác cây tác vụ, rào đọc cả thư mục tác vụ không ngoại lệ, worktree tạm đã dọn.
		gp, env, _ := bpReq(t, h, StepGoal, 0)
		mu.Lock()
		gd := goalDir
		mu.Unlock()
		if gd == "" || samePath(gd, h.root) {
			t.Errorf("[%s × N2] Dir của thăm dò = %q, phải khác cây tác vụ %q", L, gd, h.root)
		}
		if _, err := os.Stat(gd); err == nil {
			t.Errorf("[%s × N2] worktree tạm %s còn sót sau khi xong", L, gd)
		}
		deny := bpDenyList(env)
		if !slices.Contains(deny, tk.Dir) || !slices.Contains(deny, tk.ArtifactPath("requirement")) {
			t.Errorf("[%s × N2] rào đọc của thăm dò thiếu thư mục tác vụ: %v", L, deny)
		}
		for _, d := range deny {
			if strings.HasPrefix(d, "!") {
				t.Errorf("[%s × N2] rào đọc không được có ngoại lệ: %q", L, d)
			}
		}
		if !strings.Contains(gp, bprimeGoalMarker) || strings.Contains(gp, tk.ArtifactPath("plan")) {
			t.Errorf("[%s × N2] đề bài thăm dò sai dạng", L)
		}
		// Thăm dò khác thông tin với người viết mã: không thấy tiêu chí b-req thêm vào phiếu (đợt 7 thấy nguyên 5 tiêu chí).
		if strings.Contains(gp, "app.txt ghi v2") {
			t.Errorf("[%s × N2] đề bài thăm dò chứa tiêu chí do b-req viết", L)
		}
		if !tk.State.RequestCriteriaSet || len(tk.State.RequestCriteria) != 0 {
			t.Errorf("[%s × N2] phiếu phải ghi tiêu chí lúc tạo (rỗng): %+v", L, tk.State.RequestCriteria)
		}
		if n := roWorktrees(t, h.root); n != 1 {
			t.Errorf("[%s × N2] git worktree list còn %d mục, muốn 1", L, n)
		}
		// N3: đóng băng sau lõi xanh, gồm tệp phụ.
		if coreSawFrozen {
			t.Errorf("[%s × N3] thước đo đã đóng băng trước khi lõi xong", L)
		}
		if !extFrozen || extPassed < 1 || !extSawProbe {
			t.Errorf("[%s × N3] đầu lượt mở rộng: đóng băng=%v kiểm chứng xanh=%d đã gộp=%v; muốn đã đóng băng, ≥ 1 kiểm chứng xanh của lõi, tệp đã gộp", L, extFrozen, extPassed, extSawProbe)
		}
		gt := tk.State.GoalTest
		if gt == nil || !slices.Contains(gt.Paths(), roProbeRel) || !slices.Contains(gt.Paths(), roHelperRel) {
			t.Errorf("[%s × N3] GoalTest = %+v, muốn cả %s và %s", L, gt, roProbeRel, roHelperRel)
		}
		for _, p := range []string{roProbeRel, roHelperRel} {
			if !slices.Contains(tk.Ticket.Scope.Deny, p) {
				t.Errorf("[%s × N3] scope.deny thiếu %s: %v", L, p, tk.Ticket.Scope.Deny)
			}
		}
		// N4: đúng một lượt mở rộng (b-feat lần 2), có khối thước đo.
		if got := bpCount(h.calls, StepFeat); got != 2 {
			t.Errorf("[%s × N4] b-feat chạy %d lần, muốn 2 (lõi + 1 mở rộng)", L, got)
		}
		core, _, _ := bpReq(t, h, StepFeat, 0)
		ext, _, _ := bpReq(t, h, StepFeat, 1)
		if !strings.Contains(core, "LÕI trước") || strings.Contains(core, roProbeRel) {
			t.Errorf("[%s × N4] đề bài lõi phải là khối lõi và không lộ thước đo", L)
		}
		for _, want := range []string{"lượt MỞ RỘNG", roProbeRel, roHelperRel, "--drop-goal"} {
			if !strings.Contains(ext, want) {
				t.Errorf("[%s × N4] đề bài mở rộng thiếu %q", L, want)
			}
		}
		if tk.State.BPrimePhase != bprimePhaseDone {
			t.Errorf("[%s × N4] pha = %q, muốn done", L, tk.State.BPrimePhase)
		}
		// N5: review ngữ cảnh sạch.
		rp, renv, _ := bpReq(t, h, StepReview, 0)
		if !strings.Contains(rp, bprimeReviewMarker) || strings.Contains(rp, "Bề mặt bị ảnh hưởng` của `plan.md`") || strings.Contains(rp, "nội\ndung `plan.md`") ||
			strings.Contains(rp, tk.ArtifactPath("plan")) {
			t.Errorf("[%s × N5] đề bài review phải dùng thân B′ và không chỉ tới plan.md", L)
		}
		if rd := bpDenyList(renv); !slices.Contains(rd, tk.ArtifactPath("plan")) || !slices.Contains(rd, tk.ArtifactPath("tasks")) {
			t.Errorf("[%s × N5] rào đọc review thiếu plan/tasks: %v", L, rd)
		}
		// N5: review soát độ phủ của thước đo, thấy đúng dòng `Loại đầu vào:` của tệp thăm dò đã đóng băng.
		if !strings.Contains(rp, "SOÁT ĐỘ PHỦ CỦA THƯỚC ĐO") || !strings.Contains(rp, roProbeRel+":") || !strings.Contains(rp, probeMarker) {
			t.Errorf("[%s × N5] đề bài review thiếu khối soát độ phủ của thước đo:\n%s", L, rp)
		}
		// N6: lời văn.
		notes := strings.Join(res.Notes, "\n")
		if strings.Contains(notes, "đường nhanh") || strings.Contains(notes, "từ requirement.md") || !strings.Contains(notes, "lõi xanh") {
			t.Errorf("[%s × N6] ghi chú: %s", L, notes)
		}
	})

	// Loại 1a: tác vụ chạy trong worktree riêng — đề bài thăm dò phải nêu worktree TẠM, không nêu worktree của tác vụ.
	t.Run("process B′ isolation worktree", func(t *testing.T) {
		const L = "process-worktree"
		var mu sync.Mutex
		var goalDir string
		h := bpHarness(t, bpOpts{variant: config.VariantBPrime,
			cfg: func(h *harness) { h.cfg.Workflow.Isolation = config.IsolationWorktree },
			feat: func(_ context.Context, _ *harness, e *Env, _ string) error { // b-feat ghi vào worktree của tác vụ (e.Root), không phải cây gốc
				for _, f := range [][2]string{{"app.txt", "v2\n"}, {"done.txt", "ok\n"}} {
					if err := os.WriteFile(filepath.Join(e.Root, f[0]), []byte(f[1]), 0o644); err != nil {
						return err
					}
				}
				return nil
			},
			goal: func(_ context.Context, e *Env, _ string) error {
				mu.Lock()
				goalDir = e.Root
				mu.Unlock()
				return roWriteProbe(e)
			}})
		res, err := h.runner().Run(ctx, RunOptions{Workflow: StepDo, Request: vagueGoalReq})
		if err != nil {
			t.Fatal(err)
		}
		tk := loadTask(t, h, res.TaskID)
		if tk.State.Origin == "" || tk.State.Worktree == "" {
			t.Fatalf("[%s] tác vụ phải chạy trong worktree riêng: %+v", L, tk.State)
		}
		gp, _, _ := bpReq(t, h, StepGoal, 0)
		mu.Lock()
		gd := goalDir
		mu.Unlock()
		if gd == "" || samePath(gd, tk.State.Worktree) {
			t.Fatalf("[%s × N2] Dir của thăm dò = %q phải khác worktree tác vụ %q", L, gd, tk.State.Worktree)
		}
		if strings.Contains(gp, tk.State.Worktree) || strings.Contains(gp, "worktree riêng của tác vụ") || !strings.Contains(gp, gd) {
			t.Errorf("[%s × N2] đề bài thăm dò phải nêu worktree tạm %s và không nêu worktree tác vụ %s:\n%s", L, gd, tk.State.Worktree, gp)
		}
		if got := tk.State.GoalTest; got == nil || !slices.Contains(got.Paths(), roProbeRel) {
			t.Errorf("[%s × N3] thước đo phải được gộp và đóng băng: %+v; trạng thái %s %s; ghi chú %v; bước %v", L, got, res.Status, res.Reason, res.Notes, h.calls)
		}
	})

	// Loại 1b: lời văn của vòng sửa B′ (fastFix/fastFixNote) không nhắc "đường nhanh".
	t.Run("process B′ vòng sửa", func(t *testing.T) {
		h := bpHarness(t, bpOpts{variant: config.VariantBPrime, goal: func(_ context.Context, e *Env, _ string) error { return roWriteProbe(e) },
			review: func(h *harness) func(context.Context, string, *Env, string) error { return h.reviewCounts(1, 0) }})
		res, err := h.runner().Run(ctx, RunOptions{Workflow: StepDo, Request: vagueGoalReq})
		if err != nil {
			t.Fatal(err)
		}
		tk := loadTask(t, h, res.TaskID)
		var hist []string
		for _, tr := range tk.State.History {
			hist = append(hist, tr.Reason)
		}
		all := strings.Join(hist, "\n") + "\n" + strings.Join(res.Notes, "\n") + "\n" + fastFixNote(tk)
		if strings.Contains(all, "đường nhanh") || strings.Contains(all, "từ requirement.md") {
			t.Errorf("[process × N6] lời văn vòng sửa B′ còn nhắc đường nhanh/requirement.md:\n%s", all)
		}
		if !strings.Contains(all, "luồng B′") {
			t.Errorf("[process × N6] lời văn vòng sửa phải nêu luồng B′:\n%s", all)
		}
	})

	// Loại 2: driver session B′ — tuần tự lõi → b-goal → mở rộng → review; rào đọc đi qua bản ghi session-step.
	t.Run("session B′", func(t *testing.T) {
		const L = "session"
		var h *harness
		var featN int
		var goalDeny, reviewDeny []string
		var extFrozen bool
		readRec := func(id string) hookcore.SessionStepRecord {
			ps := loadTask(t, h, id).State.PendingStep
			if ps == nil {
				t.Errorf("[%s × N2] không có bước đang chạy", L)
				return hookcore.SessionStepRecord{}
			}
			rec, ok := hookcore.ReadSessionStep(filepath.Dir(h.tasks.Dir), ps.RunID)
			if !ok {
				t.Errorf("[%s × N2] không có bản ghi session-step của lượt %s", L, ps.RunID)
			}
			return rec
		}
		h = bpHarness(t, bpOpts{variant: config.VariantBPrime,
			goal: func(_ context.Context, e *Env, id string) error {
				goalDeny = readRec(id).DenyRead
				return roWriteProbe(e)
			},
			feat: func(_ context.Context, h *harness, _ *Env, id string) error {
				featN++
				if featN == 2 {
					extFrozen = loadTask(t, h, id).State.GoalTest != nil
				}
				h.write("app.txt", "v2\n")
				h.write("done.txt", "ok\n")
				return nil
			},
			review: func(h *harness) func(context.Context, string, *Env, string) error {
				return func(ctx context.Context, step string, e *Env, id string) error {
					if step == StepReview {
						reviewDeny = readRec(id).DenyRead
					}
					return h.happy(ctx, step, e, id)
				}
			}})
		r := h.runner()
		tk := h.newSession(r, NewTaskOptions{Request: vagueGoalReq})
		id := tk.State.ID
		steps, last := h.drive(r, id, false)
		if last.Action != ActionDone {
			t.Fatalf("[%s] dừng ở %+v (%s)", L, last, last.Reason)
		}
		if got := strings.Join(steps, ","); got != "b-req,b-feat,b-goal,b-feat,b-review" {
			t.Errorf("[%s × N1] thứ tự bước = %s, muốn b-req,b-feat,b-goal,b-feat,b-review", L, got)
		}
		tk = loadTask(t, h, id)
		for _, want := range []string{tk.Dir, tk.ArtifactPath("requirement"), tk.ArtifactPath("plan"), tk.ArtifactPath("tasks")} {
			if !slices.Contains(goalDeny, want) {
				t.Errorf("[%s × N2] bản ghi session-step của b-goal thiếu rào đọc %q: %v", L, want, goalDeny)
			}
		}
		gt := tk.State.GoalTest
		if gt == nil || !slices.Contains(gt.Paths(), roProbeRel) || !slices.Contains(gt.Paths(), roHelperRel) {
			t.Errorf("[%s × N3] GoalTest = %+v, muốn cả %s và %s", L, gt, roProbeRel, roHelperRel)
		}
		if !extFrozen {
			t.Errorf("[%s × N3] thước đo phải đóng băng trước lượt mở rộng", L)
		}
		if featN != 2 || !strings.Contains(h.prompts[StepFeat], "lượt MỞ RỘNG") {
			t.Errorf("[%s × N4] b-feat chạy %d lần (muốn 2) hoặc đề bài cuối không phải mở rộng", L, featN)
		}
		if !slices.Contains(reviewDeny, tk.ArtifactPath("plan")) || !slices.Contains(reviewDeny, tk.ArtifactPath("tasks")) {
			t.Errorf("[%s × N5] bản ghi session-step của review thiếu rào đọc plan/tasks: %v", L, reviewDeny)
		}
		if rp := h.prompts[StepReview]; !strings.Contains(rp, bprimeReviewMarker) || strings.Contains(rp, "Bề mặt bị ảnh hưởng` của `plan.md`") {
			t.Errorf("[%s × N5] đề bài review thiếu thân B′ hoặc còn chỉ dẫn plan.md", L)
		}
		if strings.Contains(strings.Join(last.Notes, "\n"), "đường nhanh") {
			t.Errorf("[%s × N6] ghi chú nhắc đường nhanh: %v", L, last.Notes)
		}
	})

	// Loại 3: classic (đối chứng) — thứ tự và đề bài cũ.
	t.Run("classic", func(t *testing.T) {
		const L = "classic"
		h := bpHarness(t, bpOpts{variant: config.VariantClassic, goal: func(_ context.Context, e *Env, _ string) error { return roWriteProbe(e) }})
		res, err := h.runner().Run(ctx, RunOptions{Workflow: StepDo, Request: vagueGoalReq})
		if err != nil {
			t.Fatal(err)
		}
		tk := loadTask(t, h, res.TaskID)
		if got := strings.Join(h.calls, ","); got != "b-req,b-goal,b-feat,b-review" {
			t.Errorf("[%s × N1] thứ tự bước = %s, muốn b-req,b-goal,b-feat,b-review", L, got)
		}
		if _, _, _ = bpReq(t, h, StepGoal, 0); strings.Contains(h.prompts[StepGoal], bprimeGoalMarker) {
			t.Errorf("[%s × N2] b-goal classic phải là dạng cũ", L)
		}
		if tk.State.BPrimePhase != "" {
			t.Errorf("[%s × N3] classic không được có pha B′: %q", L, tk.State.BPrimePhase)
		}
		fp := h.prompts[StepFeat]
		if strings.Contains(fp, "LÕI trước") || strings.Contains(fp, "lượt MỞ RỘNG") {
			t.Errorf("[%s × N4] đề bài b-feat classic không được có khối lõi/mở rộng", L)
		}
		if bpCount(h.calls, StepFeat) != 1 {
			t.Errorf("[%s × N4] b-feat classic chạy %d lần, muốn 1", L, bpCount(h.calls, StepFeat))
		}
		if rp := h.prompts[StepReview]; strings.Contains(rp, bprimeReviewMarker) || !strings.Contains(rp, tk.ArtifactPath("plan")) {
			t.Errorf("[%s × N5] review classic phải thấy kế hoạch và dùng thân skill gốc", L)
		}
		if n := strings.Join(res.Notes, "\n"); strings.Contains(n, "luồng B′") {
			t.Errorf("[%s × N6] ghi chú classic nhắc luồng B′: %s", L, n)
		}
	})
}

// --variant của lúc lập phiếu thắng workflow.variant (AC-4): chọn luồng cho riêng một phiếu để dựng nhánh B/B′ khi đo.
func TestTaskNewVariantFlag(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		name, cfg, flag, want string
		wantErr               bool
	}{
		{"cờ bprime thắng cấu hình classic", config.VariantClassic, config.VariantBPrime, task.VariantBPrime, false},
		{"cờ classic thắng cấu hình bprime", config.VariantBPrime, config.VariantClassic, "", false},
		{"không cờ theo cấu hình", config.VariantBPrime, "", task.VariantBPrime, false},
		{"giá trị lạ bị từ chối", config.VariantClassic, "nhanh", "", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			h.cfg.Workflow.Variant = c.cfg
			tk, _, err := h.runner().NewTask(ctx, NewTaskOptions{Request: vagueGoalReq, Workflow: StepDo, Plan: ptr(true), Variant: c.flag})
			if c.wantErr {
				if err == nil || !strings.Contains(err.Error(), "--variant") {
					t.Fatalf("muốn lỗi nêu --variant, có %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := ""
			if cl := tk.State.Classification; cl != nil {
				got = cl.Variant
			}
			if got != c.want {
				t.Errorf("classification.variant = %q, muốn %q", got, c.want)
			}
		})
	}
}
