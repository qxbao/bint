package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"bint/internal/config"
	"bint/internal/gitrepo"
	"bint/internal/state"
	"bint/internal/task"
)

// Test của G2 (docs/isolation-subagent-plan.md mục 5): chọn chế độ, tạo worktree, đưa về, huỷ, mồ côi.

func (h *harness) wtHarness() *harness {
	h.cfg.Workflow.Isolation = config.IsolationWorktree
	return h
}

func (h *harness) readFile(dir, name string) string {
	h.t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		h.t.Fatal(err)
	}
	return string(b)
}

func writeAt(dir, name, body string) error {
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(body), 0o644)
}

// featWrites là hành vi b-feat chuẩn: sửa app.txt và tạo done.txt ở thư mục chạy của bước (e.Root).
func featWrites(h *harness, extra func(e *Env) error) func(context.Context, string, *Env, string) error {
	return func(ctx context.Context, step string, e *Env, id string) error {
		switch step {
		case StepFeat:
			if err := writeAt(e.Root, "app.txt", "v2\n"); err != nil {
				return err
			}
			if err := writeAt(e.Root, "done.txt", "ok\n"); err != nil {
				return err
			}
			if extra != nil {
				return extra(e)
			}
		case StepReview:
			_, _, err := e.SubmitReview(id, []byte(`{"summary":"ổn","findings":[]}`))
			return err
		}
		return nil
	}
}

func (h *harness) newDo(r *Runner, opts NewTaskOptions) *task.Task {
	h.t.Helper()
	if opts.Request == "" {
		opts.Request = "Thêm tính năng phiên bản v2 cho app.txt"
	}
	if opts.Workflow == "" {
		opts.Workflow = StepDo
	}
	opts.Plan = ptr(false)
	tk, _, err := r.NewTask(context.Background(), opts)
	if err != nil {
		h.t.Fatal(err)
	}
	return tk
}

func (h *harness) status(id string) string { return loadTask(h.t, h, id).State.Status }

func TestChooseIsolation(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	ctx := context.Background()
	mk := func(opts NewTaskOptions) *task.Task {
		tk, _, err := r.NewTask(ctx, opts)
		if err != nil {
			t.Fatal(err)
		}
		return tk
	}
	req := "Thêm tính năng phiên bản v2 cho app.txt"
	cls := func(tk *task.Task) task.Classification { return *tk.State.Classification }

	// Cấu hình shared: luôn shared dù có tác vụ khác.
	a := mk(NewTaskOptions{Request: req, Workflow: StepFeat})
	if c := cls(a); c.Isolation != task.IsolationShared || c.IsolationReason != task.IsolationReasonConfig {
		t.Fatalf("config shared: %+v", c)
	}
	// Cờ người dùng thắng cấu hình.
	b := mk(NewTaskOptions{Request: req, Workflow: StepFeat, Isolation: task.IsolationWorktree})
	if c := cls(b); c.Isolation != task.IsolationWorktree || c.IsolationReason != task.IsolationReasonUser {
		t.Fatalf("--worktree: %+v", c)
	}
	// Cờ sai.
	if _, _, err := r.NewTask(ctx, NewTaskOptions{Request: req, Isolation: "xyz"}); err == nil {
		t.Fatal("chế độ cô lập sai phải lỗi")
	}

	// auto: phiếu đầu shared/default; phiếu hai concurrent_task kèm ID phiếu đầu.
	h.cfg.Workflow.Isolation = config.IsolationAuto
	r = h.runner()
	h.git("worktree", "list") // repo hợp lệ
	// Xoá hai phiếu trên bằng cách huỷ để auto bắt đầu từ cây sạch.
	for _, id := range []string{a.State.ID, b.State.ID} {
		if _, _, err := r.SetStatus(ctx, id, task.StatusCancelled, TransitionOptions{Reason: "dọn"}); err != nil {
			t.Fatal(err)
		}
	}
	first := mk(NewTaskOptions{Request: req, Workflow: StepFeat})
	if c := cls(first); c.Isolation != task.IsolationShared || c.IsolationReason != task.IsolationReasonDefault || len(c.IsolationTasks) != 0 {
		t.Fatalf("auto, cây trống: %+v", c)
	}
	second := mk(NewTaskOptions{Request: req, Workflow: StepFeat})
	if c := cls(second); c.Isolation != task.IsolationWorktree || c.IsolationReason != task.IsolationReasonConcurrent ||
		!slices.Equal(c.IsolationTasks, []string{first.State.ID}) {
		t.Fatalf("auto, có tác vụ khác: %+v", c)
	}
	// Tác vụ worktree (chưa tạo worktree) không tính là tác vụ shared: phiếu ba vẫn chỉ nêu phiếu đầu.
	third := mk(NewTaskOptions{Request: req, Workflow: StepFeat})
	if c := cls(third); !slices.Equal(c.IsolationTasks, []string{first.State.ID}) {
		t.Fatalf("phiếu ba: %+v", c)
	}
	// --shared thắng auto.
	if c := cls(mk(NewTaskOptions{Request: req, Workflow: StepFeat, Isolation: task.IsolationShared})); c.IsolationReason != task.IsolationReasonUser {
		t.Fatalf("--shared: %+v", c)
	}
	// Phiếu cũ (Classification không ghi chế độ) coi là shared: tác vụ cũ đang dở vẫn kích hoạt concurrent_task.
	if _, err := h.tasks.Update(first.State.ID, time.Now(), func(t *task.Task) error {
		t.State.Classification.Isolation, t.State.Classification.IsolationReason = "", ""
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := loadTask(t, h, first.State.ID).State.Isolation(); got != task.IsolationShared {
		t.Fatalf("phiếu cũ phải là shared: %q", got)
	}
	// done/cancelled không tính.
	for _, id := range []string{first.State.ID, second.State.ID, third.State.ID} {
		if _, _, err := r.SetStatus(ctx, id, task.StatusCancelled, TransitionOptions{Reason: "dọn"}); err != nil {
			t.Fatal(err)
		}
	}
	// Còn các phiếu --shared vừa lập ở trên chưa kết thúc (shared) nên vẫn kích hoạt; huỷ nốt rồi kiểm lại.
	all, _ := h.tasks.List()
	for _, o := range all {
		if !task.Terminal(o.State.Status) {
			if _, _, err := r.SetStatus(ctx, o.State.ID, task.StatusCancelled, TransitionOptions{Reason: "dọn"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if c := cls(mk(NewTaskOptions{Request: req, Workflow: StepFeat})); c.Isolation != task.IsolationShared || c.IsolationReason != task.IsolationReasonDefault {
		t.Fatalf("sau khi dọn hết: %+v", c)
	}
}

// AC-1/3: tác vụ worktree chạy b-do tới done; thay đổi về cây gốc dưới dạng chưa commit, chỉ mục không đổi;
// worktree, nhánh, ref base, tệp khoá bị gỡ; mọi bước chạy trong worktree.
func TestWorktreeRunLandsAndCleansUp(t *testing.T) {
	h := newHarness(t).wtHarness()
	h.behave = featWrites(h, nil)
	r := h.runner()
	tk := h.newDo(r, NewTaskOptions{})
	id := tk.State.ID
	wt := r.WorktreePath(id)

	headBefore := gitrepo.HeadCommit(context.Background(), h.root)
	indexBefore := h.git("diff", "--cached", "--name-only")
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: id})
	if err != nil || res.Status != task.StatusDone {
		t.Fatalf("run: %+v %v", res, err)
	}
	if res.Isolation != task.IsolationWorktree || !res.Landed || res.Origin != h.root || !slices.Equal(res.LandedFiles, []string{"app.txt", "done.txt"}) {
		t.Fatalf("kết quả thiếu thông tin đưa về: %+v", res)
	}
	// Mọi bước chạy trong worktree, kể cả b-feat/b-review.
	for i, d := range h.dirs {
		if !samePath(d, wt) {
			t.Errorf("bước %s chạy ở %s, mong %s", h.calls[i], d, wt)
		}
	}
	if len(h.dirs) == 0 {
		t.Fatal("không có bước nào chạy")
	}
	// Cây gốc nhận thay đổi dưới dạng chưa commit.
	if h.readFile(h.root, "app.txt") != "v2\n" || h.readFile(h.root, "done.txt") != "ok\n" {
		t.Fatal("cây gốc không nhận thay đổi")
	}
	if got := h.git("status", "--porcelain"); !strings.Contains(got, "M app.txt") || !strings.Contains(got, "?? done.txt") {
		t.Fatalf("thay đổi phải là chưa commit/chưa stage:\n%s", got)
	}
	if h.git("diff", "--cached", "--name-only") != indexBefore {
		t.Fatal("chỉ mục bị đổi")
	}
	if gitrepo.HeadCommit(context.Background(), h.root) != headBefore {
		t.Fatal("HEAD bị đổi")
	}
	// Dọn dẹp.
	if _, err := os.Stat(wt); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("worktree còn: %v", err)
	}
	if out := h.git("branch", "--list", "bint/*"); strings.TrimSpace(out) != "" {
		t.Fatalf("nhánh bint còn: %q", out)
	}
	if out := h.git("for-each-ref", "refs/bint"); strings.TrimSpace(out) != "" {
		t.Fatalf("ref bint còn: %q", out)
	}
	if _, err := os.Stat(lockPath(h.tasks.Dir, wt)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("tệp khoá của worktree còn: %v", err)
	}
	if _, err := os.Stat(lockPath(h.tasks.Dir, wt) + ".d"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("thư mục chủ khoá của worktree còn: %v", err)
	}
	got := loadTask(t, h, id)
	if got.State.Landed == nil || got.State.Landed.By == "" || got.State.Landed.Fingerprint == "" || got.State.Landed.Tree == "" ||
		got.State.Origin != h.root || got.State.AwaitingLand {
		t.Fatalf("Landed: %+v %+v", got.State.Landed, got.State)
	}
	// Báo cáo ghi rõ kiểm chứng chạy trên cây cô lập.
	if !strings.Contains(strings.Join(res.Notes, "\n"), "cây cô lập") {
		t.Fatalf("thiếu ghi chú cây cô lập: %v", res.Notes)
	}
	// Đề bài của agent nêu thư mục làm việc.
	if !strings.Contains(h.prompts[StepFeat], "worktree riêng") || !strings.Contains(h.prompts[StepFeat], wt) {
		t.Fatalf("đề bài thiếu dòng thư mục làm việc:\n%s", h.prompts[StepFeat])
	}
	// Tác vụ đã đưa về: diff và cổng done dùng bản lưu (worktree đã gỡ).
	if pend, err := r.UnapprovedChanges(context.Background(), got); err != nil || len(pend) != 0 {
		t.Fatalf("UnapprovedChanges sau land: %v %v", pend, err)
	}
	if in, err := r.DoneInputFor(context.Background(), got); err != nil || in.Worktree != got.State.Landed.Fingerprint {
		t.Fatalf("DoneInputFor sau land: %+v %v", in, err)
	}
}

// Base có thay đổi chưa commit và tệp mới: agent trong worktree thấy đúng nội dung đang sửa dở; tệp bị gitignore
// khớp workflow.worktree.copy được chép; tệp cục bộ do Bint quản lý được chép; setup_command chạy trong worktree.
func TestWorktreeBaseHasDirtyStateAndCopies(t *testing.T) {
	h := newHarness(t).wtHarness()
	h.write(".gitignore", ".env\nsecrets/\nignored.txt\n")
	h.git("add", ".gitignore")
	h.git("commit", "-q", "-m", "gitignore")
	h.write("app.txt", "dang sua do\n") // tệp đã theo dõi, sửa dở chưa commit
	h.write("fresh.txt", "tep moi\n")   // tệp mới chưa theo dõi
	h.write(".env", "TOKEN=1\n")        // bị gitignore, khớp mẫu copy
	h.write("secrets/a.key", "k\n")     // bị gitignore, khớp mẫu copy (thư mục)
	h.write("ignored.txt", "khong\n")   // bị gitignore, KHÔNG khớp
	h.write(".bint/local.yml", "ui:\n") // tệp cục bộ do Bint quản lý
	h.write(".claude/settings.local.json", "{}\n")
	h.cfg.Workflow.Worktree.Copy = []string{".env", "secrets", "nonexistent/*.x"}
	h.cfg.Workflow.Worktree.SetupCommand = "echo setup > setup.out && test -f fresh.txt"
	var seen = map[string]string{}
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepFeat {
			for _, f := range []string{"app.txt", "fresh.txt", ".env", "secrets/a.key", "ignored.txt", ".bint/local.yml", ".claude/settings.local.json", "setup.out"} {
				b, err := os.ReadFile(filepath.Join(e.Root, f))
				if err != nil {
					seen[f] = "<không có>"
				} else {
					seen[f] = string(b)
				}
			}
			return featWrites(h, nil)(ctx, step, e, id)
		}
		return featWrites(h, nil)(ctx, step, e, id)
	}
	r := h.runner()
	tk := h.newDo(r, NewTaskOptions{})
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: tk.State.ID})
	if err != nil || res.Status != task.StatusDone {
		t.Fatalf("run: %+v %v", res, err)
	}
	want := map[string]string{"app.txt": "dang sua do\n", "fresh.txt": "tep moi\n", ".env": "TOKEN=1\n", "secrets/a.key": "k\n",
		"ignored.txt": "<không có>", ".bint/local.yml": "ui:\n", ".claude/settings.local.json": "{}\n", "setup.out": "setup\n"}
	for f, w := range want {
		if seen[f] != w {
			t.Errorf("%s trong worktree = %q, mong %q", f, seen[f], w)
		}
	}
	// Tệp setup_command sinh ra (setup.out chưa bị gitignore) không thuộc tác vụ: không đưa về cây gốc.
	if _, err := os.Stat(filepath.Join(h.root, "setup.out")); err == nil {
		t.Fatal("setup.out (do setup_command sinh) bị đưa về cây gốc")
	}
	// Tệp đang sửa dở ở cây gốc đã bị b-feat ghi đè bằng v2 (đúng: đó là thay đổi của tác vụ).
	if h.readFile(h.root, "app.txt") != "v2\n" {
		t.Fatalf("app.txt ở cây gốc: %q", h.readFile(h.root, "app.txt"))
	}
	if h.readFile(h.root, "fresh.txt") != "tep moi\n" {
		t.Fatal("tệp mới của người dùng ở cây gốc bị đổi")
	}
}

// setup_command lỗi ⇒ failed kèm đường dẫn nhật ký; phần đã tạo bị dọn; resume (sau khi sửa) tạo lại worktree.
func TestWorktreeSetupCommandFailure(t *testing.T) {
	h := newHarness(t).wtHarness()
	h.behave = featWrites(h, nil)
	h.cfg.Workflow.Worktree.SetupCommand = "echo cai-dat-hong; exit 3"
	r := h.runner()
	tk := h.newDo(r, NewTaskOptions{})
	id := tk.State.ID
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: id})
	if err != nil || res.Status != task.StatusFailed {
		t.Fatalf("setup lỗi phải failed: %+v %v", res, err)
	}
	logPath := tk.LogPath("worktree-setup.log")
	if !strings.Contains(res.Reason, "setup_command") || !strings.Contains(res.Reason, logPath) || !strings.Contains(res.Reason, "mã 3") {
		t.Fatalf("lý do thiếu nhật ký: %q", res.Reason)
	}
	if b, _ := os.ReadFile(logPath); !strings.Contains(string(b), "cai-dat-hong") {
		t.Fatalf("nhật ký setup: %q", b)
	}
	if len(h.calls) != 0 {
		t.Fatalf("không bước agent nào được chạy: %v", h.calls)
	}
	wt := r.WorktreePath(id)
	if _, err := os.Stat(wt); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("worktree dở phải bị dọn: %v", err)
	}
	if out := h.git("branch", "--list", "bint/*"); strings.TrimSpace(out) != "" {
		t.Fatalf("nhánh dở còn: %q", out)
	}
	if out := h.git("for-each-ref", "refs/bint"); strings.TrimSpace(out) != "" {
		t.Fatalf("ref dở còn: %q", out)
	}
	if st := loadTask(t, h, id).State; st.Origin != "" || st.Worktree != h.root {
		t.Fatalf("phiếu không được ghi worktree khi tạo lỗi: %+v", st)
	}
	// Sửa cấu hình rồi resume: tạo lại worktree và chạy tới done.
	h.cfg.Workflow.Worktree.SetupCommand = ""
	r = h.runner()
	res, err = r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: id, Resume: true})
	if err != nil || res.Status != task.StatusDone || !res.Landed {
		t.Fatalf("resume: %+v %v", res, err)
	}
}

// Xung đột: người sửa cùng dòng ở cây gốc trong lúc tác vụ chạy ⇒ blocked "xung đột khi đưa về", cây gốc không bị
// đụng; resume không chạy lại review; gỡ xung đột rồi resume ⇒ done.
func TestWorktreeLandConflictBlocksAndResumes(t *testing.T) {
	h := newHarness(t).wtHarness()
	h.behave = featWrites(h, func(e *Env) error {
		// Người dùng sửa đúng dòng đó ở cây gốc trong lúc b-feat chạy.
		return writeAt(h.root, "app.txt", "nguoi dung sua\n")
	})
	r := h.runner()
	tk := h.newDo(r, NewTaskOptions{})
	id := tk.State.ID
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: id})
	if err != nil || res.Status != task.StatusBlocked || !strings.HasPrefix(res.Reason, "xung đột khi đưa về: app.txt") {
		t.Fatalf("phải blocked vì xung đột: %+v %v", res, err)
	}
	if !strings.Contains(res.Reason, "--3way") && !strings.Contains(res.Reason, "land") {
		t.Fatalf("lý do thiếu hướng dẫn: %q", res.Reason)
	}
	// Cây gốc không bị đụng.
	if h.readFile(h.root, "app.txt") != "nguoi dung sua\n" {
		t.Fatal("cây gốc bị đụng khi xung đột")
	}
	if _, err := os.Stat(filepath.Join(h.root, "done.txt")); err == nil {
		t.Fatal("tệp không xung đột cũng không được áp (áp nguyên tử)")
	}
	st := loadTask(t, h, id).State
	if !st.AwaitingLand || st.Landed != nil || st.Status != task.StatusBlocked {
		t.Fatalf("trạng thái: %+v", st)
	}
	if _, err := os.Stat(r.WorktreePath(id)); err != nil {
		t.Fatalf("worktree phải còn khi chưa đưa về: %v", err)
	}
	reviews := countCalls(h, StepReview)
	// Người gỡ xung đột: đưa app.txt ở cây gốc về đúng base (v1) rồi resume.
	if err := writeAt(h.root, "app.txt", "v1\n"); err != nil {
		t.Fatal(err)
	}
	h.behave = func(context.Context, string, *Env, string) error {
		t.Error("resume không được chạy lại agent")
		return nil
	}
	res, err = h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: id, Resume: true})
	if err != nil || res.Status != task.StatusDone {
		t.Fatalf("resume phải vào thẳng finalize: %+v %v", res, err)
	}
	if countCalls(h, StepReview) != reviews {
		t.Fatal("resume chạy lại b-review")
	}
	if h.readFile(h.root, "app.txt") != "v2\n" {
		t.Fatalf("app.txt sau khi đưa về: %q", h.readFile(h.root, "app.txt"))
	}
}

// land --3way: chỉ người; để lại dấu xung đột trong tệp ở cây gốc, phần sạch vẫn áp, chỉ mục không đổi.
func TestLandThreeWayHumanOnlyLeavesMarkers(t *testing.T) {
	h := newHarness(t).wtHarness()
	h.write("app.txt", "a\nb\nc\n")
	h.git("add", "app.txt")
	h.git("commit", "-q", "-m", "ba dong")
	h.behave = featWrites(h, func(e *Env) error {
		if err := writeAt(e.Root, "app.txt", "a\nB-tac-vu\nc\n"); err != nil {
			return err
		}
		return writeAt(h.root, "app.txt", "a\nB-nguoi\nc\n")
	})
	r := h.runner()
	tk := h.newDo(r, NewTaskOptions{})
	id := tk.State.ID
	if res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: id}); err != nil || res.Status != task.StatusBlocked {
		t.Fatalf("phải blocked: %+v %v", res, err)
	}
	indexBefore := h.git("diff", "--cached", "--name-only")

	// Từ phiên agent: bị từ chối, cây gốc không đổi.
	agent := h.runner().Env
	agent.Human = false
	if _, err := agent.Land(context.Background(), id, LandOptions{ThreeWay: true}); !errors.Is(err, ErrAgent) {
		t.Fatalf("--3way của agent phải ErrAgent: %v", err)
	}
	if h.readFile(h.root, "app.txt") != "a\nB-nguoi\nc\n" {
		t.Fatal("cây gốc bị đụng")
	}
	// Land thường vẫn báo xung đột, kể cả với người.
	var ce *LandConflictError
	if _, err := h.runner().Env.Land(context.Background(), id, LandOptions{}); !errors.As(err, &ce) || !slices.Equal(ce.Files, []string{"app.txt"}) {
		t.Fatalf("land thường: %v", err)
	}
	// Người --3way: dấu xung đột, tệp sạch (done.txt) vẫn áp.
	res, err := h.runner().Env.Land(context.Background(), id, LandOptions{ThreeWay: true})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Conflicts, []string{"app.txt"}) || !slices.Contains(res.Files, "done.txt") {
		t.Fatalf("kết quả 3way: %+v", res)
	}
	got := h.readFile(h.root, "app.txt")
	for _, want := range []string{"<<<<<<<", "B-nguoi", "B-tac-vu", ">>>>>>>"} {
		if !strings.Contains(got, want) {
			t.Errorf("app.txt thiếu %q:\n%s", want, got)
		}
	}
	if h.readFile(h.root, "done.txt") != "ok\n" {
		t.Fatal("tệp sạch phải được áp")
	}
	if h.git("diff", "--cached", "--name-only") != indexBefore {
		t.Fatal("chỉ mục bị đổi bởi --3way")
	}
	l := loadTask(t, h, id).State.Landed
	if l == nil || !l.ThreeWay || !slices.Equal(l.Conflicts, []string{"app.txt"}) {
		t.Fatalf("Landed: %+v", l)
	}
	if _, err := os.Stat(r.WorktreePath(id)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("worktree phải gỡ sau khi đưa về")
	}
	// Đã đưa về nhưng app.txt còn dấu xung đột: resume vào thẳng finalize (không agent) mà KHÔNG được done (.3).
	h.behave = func(context.Context, string, *Env, string) error { t.Error("không được chạy agent"); return nil }
	res2, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: id, Resume: true})
	if err != nil || res2.Status != task.StatusBlocked || !strings.Contains(res2.Reason, "app.txt") ||
		!strings.Contains(res2.Reason, "xung đột") {
		t.Fatalf("resume khi còn dấu xung đột phải blocked: %+v %v", res2, err)
	}
	// Người gỡ dấu xung đột xong thì resume chuyển done.
	if err := writeAt(h.root, "app.txt", "a\nB-gop\nc\n"); err != nil {
		t.Fatal(err)
	}
	if res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: id, Resume: true}); err != nil || res.Status != task.StatusDone {
		t.Fatalf("resume sau khi gỡ dấu xung đột: %+v %v", res, err)
	}
}

// .1: done --force không được bỏ qua việc đưa về: thay đổi sẽ kẹt trong worktree (Land từ chối tác vụ đã done).
func TestDoneForceCannotSkipLand(t *testing.T) {
	h := newHarness(t).wtHarness()
	h.behave = featWrites(h, nil)
	r := h.runner()
	ctx := context.Background()
	tk := h.newDo(r, NewTaskOptions{})
	id := tk.State.ID
	if res, err := r.Run(ctx, RunOptions{Workflow: StepFeat, TaskRef: id}); err != nil || res.Status != task.StatusInReview {
		t.Fatalf("b-feat: %+v %v", res, err)
	}
	_, _, err := r.SetStatus(ctx, id, task.StatusDone, TransitionOptions{Force: true, Reason: "ép"})
	var nd *NotDoneError
	if !errors.As(err, &nd) || !strings.Contains(err.Error(), "đưa về") {
		t.Fatalf("done --force phải bị từ chối vì chưa đưa về: %v", err)
	}
	if h.status(id) != task.StatusInReview {
		t.Fatalf("trạng thái đổi: %s", h.status(id))
	}
	if _, err := os.Stat(r.WorktreePath(id)); err != nil {
		t.Fatalf("worktree phải còn: %v", err)
	}
	if h.readFile(r.WorktreePath(id), "app.txt") != "v2\n" {
		t.Fatal("thay đổi của tác vụ mất khỏi worktree")
	}
}

// .1: tác vụ done mà chưa đưa về (phiếu cũ do done --force): doctor không gợi ý xoá worktree.
func TestOrphanUnlandedDoneNotSuggestedForRemoval(t *testing.T) {
	h := newHarness(t).wtHarness()
	h.behave = featWrites(h, nil)
	r := h.runner()
	ctx := context.Background()
	tk := h.newDo(r, NewTaskOptions{})
	id := tk.State.ID
	if res, err := r.Run(ctx, RunOptions{Workflow: StepFeat, TaskRef: id}); err != nil || res.Status != task.StatusInReview {
		t.Fatalf("b-feat: %+v %v", res, err)
	}
	// Dựng trạng thái mà bản cũ có thể để lại.
	if _, err := h.tasks.Update(id, time.Now(), func(t *task.Task) error {
		t.State.Status = task.StatusDone
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	orph, err := r.OrphanWorktrees(ctx)
	if err != nil || len(orph) != 1 || !orph[0].Unlanded {
		t.Fatalf("phải báo Unlanded: %+v %v", orph, err)
	}
	cmds := strings.Join(r.CleanupCommands(orph[0]), "\n")
	if strings.Contains(cmds, "remove") || strings.Contains(cmds, "branch -D") {
		t.Fatalf("không được gợi ý xoá thay đổi chưa đưa về: %s", cmds)
	}
}

// .2: --3way có tệp không trộn được (người đã xoá tệp ở cây gốc): giữ worktree để không mất bản của tác vụ,
// và done chỉ được khi người xác nhận.
func TestLandThreeWayUnmergedKeepsWorktree(t *testing.T) {
	h := newHarness(t).wtHarness()
	h.behave = featWrites(h, func(e *Env) error {
		return os.Remove(filepath.Join(h.root, "app.txt")) // người xoá tệp ở cây gốc, tác vụ lại sửa nó
	})
	r := h.runner()
	tk := h.newDo(r, NewTaskOptions{})
	id := tk.State.ID
	if res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: id}); err != nil || res.Status != task.StatusBlocked {
		t.Fatalf("phải blocked: %+v %v", res, err)
	}
	res, err := h.runner().Env.Land(context.Background(), id, LandOptions{ThreeWay: true})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Unmerged, []string{"app.txt"}) || res.Removed {
		t.Fatalf("kết quả: %+v", res)
	}
	if h.readFile(r.WorktreePath(id), "app.txt") != "v2\n" {
		t.Fatal("bản của tác vụ phải còn ở worktree")
	}
	if out := h.git("branch", "--list", "bint/"+id); strings.TrimSpace(out) == "" {
		t.Fatal("nhánh bint/<id> bị xoá")
	}
	if len(res.Notes) == 0 || !strings.Contains(strings.Join(res.Notes, "\n"), "app.txt") {
		t.Fatalf("thiếu ghi chú đường dẫn lấy lại: %v", res.Notes)
	}
	l := loadTask(t, h, id).State.Landed
	if l == nil || !l.Kept || !slices.Equal(l.Unmerged, []string{"app.txt"}) {
		t.Fatalf("Landed: %+v", l)
	}
	// Doctor báo worktree giữ lại kèm lý do.
	orph, _ := r.OrphanWorktrees(context.Background())
	if len(orph) != 1 || !orph[0].Kept || !strings.Contains(orph[0].Reason, "app.txt") {
		t.Fatalf("orphan: %+v", orph)
	}
	// Resume bị chặn bởi tệp chưa trộn; người --force thì done.
	h.behave = func(context.Context, string, *Env, string) error { return nil }
	if res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: id, Resume: true}); err != nil || res.Status != task.StatusBlocked {
		t.Fatalf("resume phải blocked: %+v %v", res, err)
	}
	if _, _, err := h.runner().SetStatus(context.Background(), id, task.StatusDone, TransitionOptions{Force: true, Reason: "đã ghép tay"}); err != nil {
		t.Fatalf("người --force sau khi xử lý: %v", err)
	}
}

// --keep giữ worktree; doctor (OrphanWorktrees) báo nó; land lần hai bị từ chối.
func TestLandKeepKeepsWorktree(t *testing.T) {
	h := newHarness(t).wtHarness()
	h.behave = featWrites(h, nil)
	r := h.runner()
	tk := h.newDo(r, NewTaskOptions{})
	id := tk.State.ID
	if res, err := r.Run(context.Background(), RunOptions{Workflow: StepFeat, TaskRef: id}); err != nil || res.Status != task.StatusInReview {
		t.Fatalf("b-feat: %+v %v", res, err)
	}
	res, err := r.Land(context.Background(), id, LandOptions{Keep: true})
	if err != nil || res.Removed {
		t.Fatalf("land --keep: %+v %v", res, err)
	}
	wt := r.WorktreePath(id)
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("--keep phải giữ worktree: %v", err)
	}
	if out := h.git("branch", "--list", "bint/*"); !strings.Contains(out, "bint/"+id) {
		t.Fatalf("--keep phải giữ nhánh: %q", out)
	}
	if _, err := r.Land(context.Background(), id, LandOptions{}); err == nil || !strings.Contains(err.Error(), "đã đưa về") {
		t.Fatalf("land lần hai: %v", err)
	}
	// done dùng bản lưu dù worktree còn.
	if _, _, err := r.SetStatus(context.Background(), id, task.StatusDone, TransitionOptions{Reason: "ok"}); err != nil {
		t.Fatalf("done sau land --keep: %v", err)
	}
	orph, err := r.OrphanWorktrees(context.Background())
	if err != nil || len(orph) != 1 || !orph[0].Kept || orph[0].TaskID != id {
		t.Fatalf("doctor phải báo worktree giữ lại: %+v %v", orph, err)
	}
}

// Land chỉ khi điều kiện done đủ (trừ chính việc đưa về); agent không đưa về được tác vụ đang được bint run điều phối.
func TestLandRequiresDoneConditionsAndOwnership(t *testing.T) {
	h := newHarness(t).wtHarness()
	h.behave = featWrites(h, nil)
	r := h.runner()
	tk := h.newDo(r, NewTaskOptions{})
	id := tk.State.ID
	if _, err := r.Land(context.Background(), id, LandOptions{}); err == nil || !strings.Contains(err.Error(), "chưa có worktree") {
		t.Fatalf("chưa chạy lần nào: %v", err)
	}
	if res, err := r.Run(context.Background(), RunOptions{Workflow: StepFeat, TaskRef: id}); err != nil || res.Status != task.StatusInReview {
		t.Fatalf("b-feat: %+v %v", res, err)
	}
	// Sửa mã sau kiểm chứng: fingerprint lệch ⇒ chưa đủ bằng chứng.
	wt := r.WorktreePath(id)
	if err := writeAt(wt, "app.txt", "v3-sau-kiem-chung\n"); err != nil {
		t.Fatal(err)
	}
	var nd *NotDoneError
	if _, err := r.Land(context.Background(), id, LandOptions{}); !errors.As(err, &nd) {
		t.Fatalf("mã đã đổi sau kiểm chứng phải NotDoneError: %v", err)
	}
	if h.readFile(h.root, "app.txt") != "v1\n" {
		t.Fatal("cây gốc bị đụng khi chưa đủ điều kiện")
	}
	// done bị chặn khi chưa đưa về, và thông báo nêu land.
	if _, _, err := r.SetStatus(context.Background(), id, task.StatusDone, TransitionOptions{Reason: "x"}); !errors.As(err, &nd) ||
		!strings.Contains(strings.Join(nd.Blockers, " "), "chưa đưa thay đổi về cây gốc") {
		t.Fatalf("done khi chưa đưa về: %v", err)
	}
	// Đang được bint run khác điều phối: từ chối.
	if _, err := h.tasks.Update(id, time.Now(), func(t *task.Task) error {
		t.State.Runner = &task.Runner{PID: 1, Host: "x", RunID: "RUN1", Started: task.Now(time.Now())} // pid 1 luôn sống
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Land(context.Background(), id, LandOptions{}); err == nil || !strings.Contains(err.Error(), "điều phối") {
		t.Fatalf("tác vụ đang được điều phối: %v", err)
	}
}

// Huỷ: worktree còn thay đổi chưa đưa về ⇒ cần --discard (chỉ người); không còn gì thì huỷ được và gỡ sạch.
func TestCancelRemovesWorktreeAndNeedsDiscard(t *testing.T) {
	h := newHarness(t).wtHarness()
	h.behave = featWrites(h, nil)
	r := h.runner()
	ctx := context.Background()

	// Chưa có worktree: huỷ như thường.
	t0 := h.newDo(r, NewTaskOptions{})
	if _, _, err := r.SetStatus(ctx, t0.State.ID, task.StatusCancelled, TransitionOptions{}); err != nil {
		t.Fatalf("huỷ tác vụ chưa có worktree: %v", err)
	}

	// Có thay đổi chưa đưa về.
	tk := h.newDo(r, NewTaskOptions{})
	id := tk.State.ID
	if res, err := r.Run(ctx, RunOptions{Workflow: StepFeat, TaskRef: id}); err != nil || res.Status != task.StatusInReview {
		t.Fatalf("b-feat: %+v %v", res, err)
	}
	wt := r.WorktreePath(id)
	var dr *ErrDiscardRequired
	if _, _, err := r.SetStatus(ctx, id, task.StatusCancelled, TransitionOptions{}); !errors.As(err, &dr) ||
		!slices.Contains(dr.Files, "app.txt") || !strings.Contains(err.Error(), "--discard") {
		t.Fatalf("cancel không --discard: %v", err)
	}
	if _, err := os.Stat(wt); err != nil || h.status(id) != task.StatusInReview {
		t.Fatalf("worktree/trạng thái không được đổi khi bị từ chối: %v %s", err, h.status(id))
	}
	// Agent không --discard được.
	agent := h.runner().Env
	agent.Human = false
	if _, _, err := agent.SetStatus(ctx, id, task.StatusCancelled, TransitionOptions{Discard: true}); !errors.Is(err, ErrAgent) {
		t.Fatalf("agent --discard: %v", err)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatal("worktree bị gỡ bởi agent")
	}
	// Người --discard: gỡ worktree, nhánh, ref, tệp khoá; cây gốc không bị đụng.
	if _, _, err := r.SetStatus(ctx, id, task.StatusCancelled, TransitionOptions{Discard: true, Reason: "bỏ"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wt); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("worktree còn sau cancel: %v", err)
	}
	if out := h.git("branch", "--list", "bint/*"); strings.TrimSpace(out) != "" {
		t.Fatalf("nhánh còn: %q", out)
	}
	if out := h.git("for-each-ref", "refs/bint"); strings.TrimSpace(out) != "" {
		t.Fatalf("ref còn: %q", out)
	}
	if _, err := os.Stat(lockPath(h.tasks.Dir, wt)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("tệp khoá còn")
	}
	if h.readFile(h.root, "app.txt") != "v1\n" {
		t.Fatal("cây gốc bị đụng khi huỷ")
	}

	// Worktree sạch (b-req chỉ đọc) huỷ được không cần --discard.
	t2 := h.newDo(r, NewTaskOptions{})
	h.behave = func(ctx context.Context, step string, e *Env, id string) error { return h.writePlan(e, id, nil) }
	if res, err := r.Run(ctx, RunOptions{Workflow: StepReq, TaskRef: t2.State.ID}); err != nil || res.Status != task.StatusPlanned {
		t.Fatalf("b-req: %+v %v", res, err)
	}
	if _, err := os.Stat(r.WorktreePath(t2.State.ID)); err != nil {
		t.Fatalf("worktree phải được tạo ở b-req (mọi workflow): %v", err)
	}
	if _, _, err := r.SetStatus(ctx, t2.State.ID, task.StatusCancelled, TransitionOptions{}); err != nil {
		t.Fatalf("huỷ worktree sạch: %v", err)
	}
	if _, err := os.Stat(r.WorktreePath(t2.State.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("worktree sạch phải được gỡ")
	}
}

func TestOrphanWorktrees(t *testing.T) {
	h := newHarness(t).wtHarness()
	h.behave = featWrites(h, nil)
	r := h.runner()
	ctx := context.Background()
	live := h.newDo(r, NewTaskOptions{})
	if res, err := r.Run(ctx, RunOptions{Workflow: StepFeat, TaskRef: live.State.ID}); err != nil || res.Status != task.StatusInReview {
		t.Fatalf("%+v %v", res, err)
	}
	if o, err := r.OrphanWorktrees(ctx); err != nil || len(o) != 0 {
		t.Fatalf("worktree của tác vụ đang làm không phải mồ côi: %+v %v", o, err)
	}
	// Thư mục không ứng với tác vụ nào.
	stray := filepath.Join(r.worktreesDir(), "01NOSUCHTASK")
	if err := os.MkdirAll(stray, 0o755); err != nil {
		t.Fatal(err)
	}
	// Worktree git đã đăng ký rồi mất thư mục (prunable).
	gone := filepath.Join(r.worktreesDir(), "01GONETASK")
	h.git("worktree", "add", "-q", "-b", "bint/01GONETASK", gone)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	// Tác vụ đã cancelled mà thư mục còn.
	dead := h.newDo(r, NewTaskOptions{})
	deadDir := filepath.Join(r.worktreesDir(), dead.State.ID)
	if err := os.MkdirAll(deadDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.SetStatus(ctx, dead.State.ID, task.StatusCancelled, TransitionOptions{}); err != nil {
		t.Fatal(err)
	}
	orph, err := r.OrphanWorktrees(ctx)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]OrphanWorktree{}
	for _, o := range orph {
		by[filepath.Base(o.Path)] = o
	}
	if _, ok := by[live.State.ID]; ok {
		t.Fatal("worktree đang dùng bị báo mồ côi")
	}
	if o, ok := by["01NOSUCHTASK"]; !ok || o.TaskID != "01NOSUCHTASK" || o.Status != "" {
		t.Fatalf("thiếu thư mục lạ: %+v", by)
	}
	if o, ok := by["01GONETASK"]; !ok || !o.Prunable {
		t.Fatalf("thiếu mục prunable: %+v", by)
	}
	if o, ok := by[dead.State.ID]; !ok || o.Status != task.StatusCancelled {
		t.Fatalf("thiếu tác vụ cancelled: %+v", by)
	}
	cmds := strings.Join(r.CleanupCommands(by["01GONETASK"]), "\n")
	if !strings.Contains(cmds, "worktree prune") || !strings.Contains(cmds, "branch -D bint/01GONETASK") {
		t.Fatalf("lệnh dọn: %s", cmds)
	}
	if cmds := strings.Join(r.CleanupCommands(by["01NOSUCHTASK"]), "\n"); !strings.Contains(cmds, "worktree remove --force") {
		t.Fatalf("lệnh dọn: %s", cmds)
	}
}

// Ref base dùng chung tên với G1 (BaseTree): Base vẫn đọc được sau b-feat đầu và sau khi tác vụ kết thúc.
func TestBaseCommitSurvivesFirstFeatAndDropTouched(t *testing.T) {
	h := newHarness(t).wtHarness()
	h.behave = featWrites(h, nil)
	r := h.runner()
	tk := h.newDo(r, NewTaskOptions{})
	id := tk.State.ID
	ctx := context.Background()
	if res, err := r.Run(ctx, RunOptions{Workflow: StepFeat, TaskRef: id}); err != nil || res.Status != task.StatusInReview {
		t.Fatalf("%+v %v", res, err)
	}
	st := loadTask(t, h, id).State
	if st.BaseTree == "" || st.Base == "" {
		t.Fatalf("thiếu Base/BaseTree: %+v", st)
	}
	if _, err := gitrepo.Run(ctx, h.root, "cat-file", "-e", st.Base+"^{commit}"); err != nil {
		t.Fatalf("Base không còn đọc được sau b-feat đầu: %v", err)
	}
	// Ref base nay trỏ vào commit BaseTree có cha là Base.
	parent := strings.TrimSpace(h.git("rev-parse", baseRef(id)+"^"))
	if parent != st.Base {
		t.Fatalf("cha của ref base = %s, mong Base %s", parent, st.Base)
	}
	r2 := &Runner{Env: r.Env}
	r2.dropTouched(&runState{t: loadTask(t, h, id), res: &Result{}}, "thử")
	if _, err := gitrepo.Run(ctx, h.root, "cat-file", "-e", st.Base+"^{commit}"); err != nil {
		t.Fatalf("Base mất sau dropTouched: %v", err)
	}
	if out := h.git("branch", "--list", "bint/"+id); !strings.Contains(out, id) {
		t.Fatal("nhánh bint/<id> phải còn để giữ Base")
	}
	if _, err := r.Land(ctx, id, LandOptions{}); err != nil {
		t.Fatalf("land sau dropTouched (không BaseTree): %v", err)
	}
	if h.readFile(h.root, "app.txt") != "v2\n" {
		t.Fatal("land không áp thay đổi")
	}
}

// Tác vụ khác đã đưa về cây này không bị quy cho tác vụ shared đang chạy ở cây đó; sửa tay sau đó vẫn bị tính.
func TestLandedFilesNotAttributedToSharedTask(t *testing.T) {
	h := newHarness(t)
	h.cfg.Eval.TestCommand = ptr("true")
	ctx := context.Background()
	r := h.runner()
	shared := h.newDo(r, NewTaskOptions{Isolation: task.IsolationShared})
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepFeat && id == shared.State.ID {
			return writeAt(e.Root, "a.txt", "A\n")
		}
		if step == StepFeat {
			return writeAt(e.Root, "b.txt", "B\n")
		}
		return nil
	}
	if res, err := r.Run(ctx, RunOptions{Workflow: StepFeat, TaskRef: shared.State.ID}); err != nil || res.Status != task.StatusInReview {
		t.Fatalf("%+v %v", res, err)
	}
	wtTask := h.newDo(r, NewTaskOptions{Isolation: task.IsolationWorktree})
	if res, err := r.Run(ctx, RunOptions{Workflow: StepFeat, TaskRef: wtTask.State.ID}); err != nil || res.Status != task.StatusInReview {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := r.Land(ctx, wtTask.State.ID, LandOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.SetStatus(ctx, wtTask.State.ID, task.StatusDone, TransitionOptions{Reason: "ok"}); err != nil {
		t.Fatal(err)
	}
	a := loadTask(t, h, shared.State.ID)
	files, err := ScopeFiles(ctx, h.root, h.tasks, a, r.Cfg.Limits.ProtectedPaths)
	if err != nil || !slices.Equal(files, []string{"a.txt"}) {
		t.Fatalf("ScopeFiles(A) = %v %v; b.txt của tác vụ đã đưa về không được quy cho A", files, err)
	}
	// Sửa tay sau khi đưa về: blob khác ⇒ bị tính.
	if err := writeAt(h.root, "b.txt", "B-nguoi-sua\n"); err != nil {
		t.Fatal(err)
	}
	files, err = ScopeFiles(ctx, h.root, h.tasks, a, r.Cfg.Limits.ProtectedPaths)
	if err != nil || !slices.Equal(files, []string{"a.txt", "b.txt"}) {
		t.Fatalf("sau khi sửa tay: %v %v", files, err)
	}
}

func TestNormalizeIsolationFlag(t *testing.T) {
	for in, want := range map[string]string{"": "", "auto": "", " Worktree ": "worktree", "shared": "shared"} {
		if got, err := NormalizeIsolationFlag(in); err != nil || got != want {
			t.Errorf("%q → %q %v", in, got, err)
		}
	}
	if _, err := NormalizeIsolationFlag("x"); err == nil {
		t.Error("giá trị lạ phải lỗi")
	}
}

func TestCopyIntoWorktreeGlobs(t *testing.T) {
	origin, wt := t.TempDir(), t.TempDir()
	for name, body := range map[string]string{
		".env": "e", "config/a.local.json": "a", "config/b.json": "b", "config/deep/c.local.json": "c",
		"cache/x/y.bin": "y", "cache/z.bin": "z", "tracked.txt": "o", "dir/sub/f.txt": "f",
	} {
		if err := writeAt(origin, name, body); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeAt(wt, "tracked.txt", "da-co-trong-worktree"); err != nil {
		t.Fatal(err)
	}
	copied, _, err := copyIntoWorktree(origin, wt, []string{".env", "config/*.local.json", "cache/**", "tracked.txt", "dir"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".env", "cache/x/y.bin", "cache/z.bin", "config/a.local.json", "dir/sub/f.txt"}
	if !slices.Equal(copied, want) {
		t.Fatalf("đã chép %v, mong %v", copied, want)
	}
	if b, _ := os.ReadFile(filepath.Join(wt, "tracked.txt")); string(b) != "da-co-trong-worktree" {
		t.Fatal("tệp đã có ở worktree không được ghi đè")
	}
	if _, err := os.Stat(filepath.Join(wt, "config", "deep", "c.local.json")); err == nil {
		t.Fatal("mẫu config/*.local.json không được khớp tệp ở thư mục con")
	}
}

func TestConflictFilesParsing(t *testing.T) {
	stderr := "error: patch failed: a.txt:3\nerror: a.txt: patch does not apply\nerror: new.txt: already exists in working directory\nerror: lạ.txt: patch does not apply\n"
	got := conflictFiles(stderr, []string{"a.txt", "new.txt", "ok.txt"})
	if !slices.Equal(got, []string{"a.txt", "new.txt"}) {
		t.Fatalf("%v", got)
	}
	if got := conflictFiles("không rõ", []string{"x", "y"}); !slices.Equal(got, []string{"x", "y"}) {
		t.Fatalf("không phân tích được thì nghi mọi tệp: %v", got)
	}
}

// .7: một nguồn sự thật cho thư mục worktree: Paths.Worktrees và thư mục workflow suy từ Tasks.Dir trùng nhau.
func TestWorktreesDirMatchesPaths(t *testing.T) {
	p := state.At("repo", filepath.Join(t.TempDir(), "state"))
	e := &Env{Tasks: task.NewStore(p.Runtime)}
	if e.worktreesDir() != p.Worktrees {
		t.Fatalf("worktreesDir %q != Paths.Worktrees %q", e.worktreesDir(), p.Worktrees)
	}
}

// .4: gitApply ép LC_ALL=C để conflictFiles đọc được thông báo kể cả khi môi trường đặt ngôn ngữ khác.
func TestGitApplyForcesEnglishMessages(t *testing.T) {
	h := newHarness(t)
	t.Setenv("LC_ALL", "vi_VN.UTF-8")
	t.Setenv("LANGUAGE", "vi")
	patch := "diff --git a/app.txt b/app.txt\n--- a/app.txt\n+++ b/app.txt\n@@ -1 +1 @@\n-khong-co-dong-nay\n+x\n"
	err := gitApply(context.Background(), h.root, patch, "--check")
	if err == nil {
		t.Fatal("bản vá phải lỗi")
	}
	if got := conflictFiles(err.Error(), []string{"app.txt", "khac.txt"}); !slices.Equal(got, []string{"app.txt"}) {
		t.Fatalf("conflictFiles(%q) = %v", err, got)
	}
}
