package workflow

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"bint/internal/state"
	"bint/internal/task"
)

// Các hồi quy từ benchmark Bint vs làm tay ngày 2026-11-16 (docs/evals/bench-bint-vs-plain-20261116, mục "Điểm vướng").

// Điểm vướng 1: lệnh kiểm chứng chạy với TMPDIR là thư mục riêng mới tạo trong thư mục trạng thái (không phải /tmp dùng
// chung với sandbox Codex, nơi /tmp/.git làm `go` báo lỗi VCS), và thư mục đó được dọn sau khi chạy.
func TestVerifyUsesPrivateTempDir(t *testing.T) {
	h := newHarness(t)
	out := filepath.Join(t.TempDir(), "tmpdir.txt")
	capture := `printf %s "$TMPDIR"`
	if runtime.GOOS == "windows" {
		// Git Bash chuyển TMPDIR sang dạng POSIX; đổi về đường dẫn Windows
		// trước khi so với filepath của tiến trình Go và kiểm thư mục đã dọn.
		capture = `cygpath -m "$TMPDIR"`
	}
	cmd := `test -f done.txt && test -d "$TMPDIR" && ` + capture + ` > '` + strings.ReplaceAll(filepath.ToSlash(out), "'", "'\\''") + `'`
	h.cfg.Eval.TestCommand = &cmd
	id := h.finishFeat() // b-feat → kiểm chứng của Runner
	ran := func() string {
		t.Helper()
		b, err := os.ReadFile(out)
		if err != nil {
			t.Fatalf("lệnh kiểm chứng không chạy hoặc thiếu TMPDIR: %v (%+v)", err, h.load(id).State.LastVerification())
		}
		os.Remove(out)
		return strings.TrimSpace(string(b))
	}
	base := state.TmpDirFor(h.tasks.Dir)
	check := func(who, dir string) {
		t.Helper()
		if dir == "" || dir == os.TempDir() || filepath.Dir(filepath.Clean(dir)) != base {
			t.Fatalf("%s: TMPDIR=%q, muốn thư mục con riêng của %s", who, dir, base)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("%s: thư mục tạm %s phải được dọn sau khi chạy (%v)", who, dir, err)
		}
	}
	first := ran()
	check("Runner", first)
	// bint task verify (Env.Verify): cùng cơ chế, mỗi lần một thư mục mới.
	if _, vr, err := h.runner().Verify(context.Background(), id); err != nil || !vr.Verification.Passed {
		t.Fatalf("verify: %+v %v", vr, err)
	}
	second := ran()
	check("bint task verify", second)
	if first == second {
		t.Fatalf("mỗi lần kiểm chứng cần thư mục tạm mới: %q", first)
	}
}

// Không tạo được thư mục tạm riêng thì lần kiểm chứng vẫn chạy (TMPDIR kế thừa), không hỏng.
func TestVerifyTempDirFailureDoesNotBreakVerify(t *testing.T) {
	h := newHarness(t)
	id := h.finishFeat()
	// Chiếm chỗ <state>/tmp bằng một tệp: MkdirAll lỗi.
	base := state.TmpDirFor(h.tasks.Dir)
	os.RemoveAll(base)
	if err := os.WriteFile(base, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, vr, err := h.runner().Verify(context.Background(), id); err != nil || !vr.Verification.Passed {
		t.Fatalf("lỗi tạo thư mục tạm không được làm hỏng kiểm chứng: %+v %v", vr, err)
	}
}

// Điểm vướng 2: b-feat ghi lại tasks.md làm mất `— xong khi: …` (dạng bản hỏng của B-mid: giữ TSK-ID/AC, đánh dấu [x]).
// Runner phục hồi các dòng TSK của bản trước bước, giữ đánh dấu, ghi chú; tác vụ không bị chặn vì hợp đồng.
func TestFeatRewritingTasksKeepsPlanningContract(t *testing.T) {
	h := newHarness(t)
	base := h.happy
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepFeat {
			if _, _, err := e.WriteArtifact(id, "tasks", "- [x] TSK-1 [AC-1]: sửa app.txt thành v2; test qua.\n"); err != nil {
				return err
			}
		}
		return base(ctx, step, e, id)
	}
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: "Thêm tính năng phiên bản v2 cho app.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != task.StatusDone {
		t.Fatalf("không được chặn vì hợp đồng artifact: %+v", res)
	}
	got := h.load(res.TaskID)
	b, _ := os.ReadFile(got.ArtifactPath("tasks"))
	if string(b) != "- [x] TSK-1 [AC-1]: sửa app.txt — xong khi: app.txt ghi v2\n" {
		t.Fatalf("tasks.md sau phục hồi: %q", b)
	}
	if rep := task.LintPlanningTask(got, true); rep.Blocking() {
		t.Fatalf("tasks.md phải đạt hợp đồng: %v", rep.Errors)
	}
	if !slices.ContainsFunc(res.Notes, func(n string) bool { return strings.Contains(n, "tasks.md không đạt hợp đồng v1") }) {
		t.Fatalf("thiếu ghi chú phục hồi: %v", res.Notes)
	}
}

// Agent thêm TSK mới khi ghi lại (không ghép được theo TSK-ID): phục hồi nguyên bản trước bước.
func TestFeatRewritingTasksRestoresWholeWhenIDsDiffer(t *testing.T) {
	h := newHarness(t)
	base := h.happy
	var before string
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepFeat {
			tk, _ := e.Tasks.Load(id)
			b, _ := os.ReadFile(tk.ArtifactPath("tasks"))
			before = string(b)
			if _, _, err := e.WriteArtifact(id, "tasks", "- [x] TSK-1 [AC-1]: sửa app.txt\n- [x] TSK-2 [AC-1]: thêm test\n"); err != nil {
				return err
			}
		}
		return base(ctx, step, e, id)
	}
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: "Thêm tính năng phiên bản v2 cho app.txt"})
	if err != nil || res.Status != task.StatusDone {
		t.Fatalf("%+v %v", res, err)
	}
	b, _ := os.ReadFile(h.load(res.TaskID).ArtifactPath("tasks"))
	if before == "" || string(b) != before {
		t.Fatalf("phải phục hồi nguyên bản trước bước: %q, muốn %q", b, before)
	}
}

// Điểm vướng 4 (B-mid): kiểm chứng đầy đủ ở finalize đỏ giả làm Runner mở vòng sửa (in_review → in_progress); vòng sửa
// bị dừng chờ người, người chạy lại `bint task verify` (xanh, cùng cây) rồi resume. Runner không được chạy lại b-feat rồi
// b-review mà vào thẳng finalize. Cây đổi sau lần xanh đó thì vòng sửa chạy như cũ.
func TestResumeSkipsFixWhenLatestFullVerifyPassed(t *testing.T) {
	for _, tc := range []string{"cây không đổi", "cây đổi sau lần xanh"} {
		t.Run(tc, func(t *testing.T) {
			ctx := context.Background()
			h := goHarness(t)
			h.cfg.Workflow.MaxReviewRounds = 2
			flag := filepath.ToSlash(filepath.Join(t.TempDir(), "outage"))
			full := "test -f done.txt && ! test -f " + flag // hỏng khi có cờ ngoài cây: mô phỏng đỏ giả do môi trường
			h.cfg.Eval.TestCommand = &full
			feats := 0
			h.behaveFeat(func() {
				feats++
				h.write("a/a.go", "package a\n\nfunc A() int { return 2 }\n")
				h.write("done.txt", "ok\n")
			})
			id := h.runFeat("Thêm tính năng cho gói a", "").TaskID
			if v := h.load(id).State.LastVerification(); v == nil || v.Full() {
				t.Fatalf("b-feat phải kiểm chứng affected: %+v", v)
			}
			if _, err := h.tasks.Update(id, time.Now(), func(tk *task.Task) error {
				tk.Ticket.Autonomy = task.AutonomyAskAlways // vòng sửa dừng chờ người ở đầu b-feat
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			setFlag(t, flag, true)
			r := h.runner()
			r.Confirm = func(q Question) bool { return q.Key != QConfirmFeat }
			res, err := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: id, Resume: true})
			if err != nil {
				t.Fatal(err)
			}
			got := h.load(id)
			if res.Status != task.StatusBlocked || got.State.ResumeFrom != task.StatusInProgress || feats != 1 || !inVerifyFix(got) {
				t.Fatalf("phải mở vòng sửa rồi dừng ở b-feat: %+v resume_from=%s feats=%d", res, got.State.ResumeFrom, feats)
			}
			// Người sửa môi trường và chạy lại kiểm chứng: qua, đầy đủ, cùng cây.
			setFlag(t, flag, false)
			if _, vr, err := h.runner().Verify(ctx, id); err != nil || !vr.Verification.Passed || !vr.Verification.Full() {
				t.Fatalf("verify của người: %+v %v", vr, err)
			}
			if tc == "cây đổi sau lần xanh" {
				h.write("docs/note.md", "người sửa thêm\n")
			}
			r = h.runner()
			r.Confirm = func(Question) bool { return true }
			nv := len(h.load(id).State.Verifications)
			res, err = r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: id, Resume: true})
			if err != nil {
				t.Fatal(err)
			}
			if tc == "cây đổi sau lần xanh" {
				if feats != 2 {
					t.Fatalf("cây đổi: vòng sửa phải chạy như cũ, feats=%d %+v", feats, res)
				}
				return
			}
			if res.Status != task.StatusDone || feats != 1 || len(res.Steps) != 0 {
				t.Fatalf("phải done mà không chạy b-feat/b-review: %+v steps=%v feats=%d", res, stepNames(res), feats)
			}
			got = h.load(id)
			if len(got.State.Verifications) != nv {
				t.Fatalf("không được kiểm chứng lại: %d → %d", nv, len(got.State.Verifications))
			}
			if !slices.ContainsFunc(got.State.History, func(tr task.Transition) bool {
				return tr.From == task.StatusInProgress && tr.To == task.StatusInReview && strings.HasPrefix(tr.Reason, "bỏ vòng sửa")
			}) {
				t.Fatalf("lịch sử phải ghi việc bỏ vòng sửa: %+v", got.State.History)
			}
		})
	}
}

// Resume sau khi finalize bị chặn vì kiểm chứng hỏng, nhưng người đã chạy lại kiểm chứng và qua đầy đủ trên cùng cây:
// không chạy lại lệnh kiểm chứng lần nữa.
func TestFinalizeResumeSkipsReverifyWhenLatestPassed(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	_, flag := h.flagCommand()
	id := h.finishFeat()
	setFlag(t, flag, true)
	h.runner().VerifyWith(ctx, id, "sự cố ngoài mã")
	if res := h.resumeDo(id); res.Status != task.StatusBlocked || h.load(id).State.FinalizeFP == "" {
		t.Fatalf("phải chặn ở finalize: %+v", res)
	}
	setFlag(t, flag, false)
	if _, vr, err := h.runner().Verify(ctx, id); err != nil || !vr.Verification.Passed {
		t.Fatalf("%+v %v", vr, err)
	}
	nv := len(h.load(id).State.Verifications)
	res := h.resumeDo(id)
	if res.Status != task.StatusDone || slices.Contains(stepNames(res), StepReview) {
		t.Fatalf("%+v steps=%v", res, stepNames(res))
	}
	if n := len(h.load(id).State.Verifications); n != nv {
		t.Fatalf("không được kiểm chứng lại: %d → %d", nv, n)
	}
}
