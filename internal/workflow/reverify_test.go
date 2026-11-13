package workflow

import (
	"context"
	"strings"
	"testing"
	"time"

	"bint/internal/task"
)

// NT-WF-BASELINE: người/tác vụ khác commit vào cùng cây giữa lúc tác vụ chạy làm vân tay mã đổi sau lần kiểm chứng
// cuối. Finalize chỉ tự chạy lại lệnh kiểm chứng (đúng một lần) khi mọi tệp đổi đều ngoài Touched; mọi lý do chặn khác
// vẫn dừng chờ người như cũ.

// finishFeat chạy b-feat tới in_review (kiểm chứng đã chạy một lần sau bước implement) rồi trả ID tác vụ.
func (h *harness) finishFeat() string {
	h.t.Helper()
	res := h.runFeat("Thêm tính năng phiên bản v2 cho app.txt", "")
	if res.Status != task.StatusInReview {
		h.t.Fatalf("b-feat: %+v", res)
	}
	if n := len(h.load(res.TaskID).State.Verifications); n != 1 {
		h.t.Fatalf("sau b-feat cần đúng 1 lần kiểm chứng, có %d", n)
	}
	return res.TaskID
}

// resumeDo tiếp tục tác vụ ở driver process: b-review rồi finalize.
func (h *harness) resumeDo(id string) *Result {
	h.t.Helper()
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: id, Resume: true})
	if err != nil {
		h.t.Fatal(err)
	}
	return res
}

func TestFinalizeReverifiesWhenOnlyOutsideFilesChanged(t *testing.T) {
	h := newHarness(t)
	id := h.finishFeat()
	h.write("docs/other.md", "commit của người khác\n") // ngoài Touched, không phải đường dẫn bảo vệ
	res := h.resumeDo(id)
	if res.Status != task.StatusDone {
		t.Fatalf("phải tự kiểm chứng lại rồi done: %+v", res)
	}
	got := h.load(id)
	if len(got.State.Verifications) != 2 {
		t.Fatalf("cần đúng 2 lần kiểm chứng (1 sau b-feat, 1 tự chạy lại), có %d", len(got.State.Verifications))
	}
	v := got.State.LastVerification()
	if !v.Passed || !v.Confirmed || v.Reason != "mã khác đổi sau lần kiểm chứng: docs/other.md" {
		t.Fatalf("lần kiểm chứng lại: %+v", v)
	}
	if got.State.Verifications[0].Reason != "" {
		t.Fatalf("lần kiểm chứng đầu không có lý do: %+v", got.State.Verifications[0])
	}
}

func TestFinalizeDoesNotReverifyWhenTouchedFileChanged(t *testing.T) {
	h := newHarness(t)
	id := h.finishFeat()
	h.write("app.txt", "sửa tiếp sau kiểm chứng\n") // thuộc Touched
	h.write("docs/other.md", "của người khác\n")
	res := h.resumeDo(id)
	if res.Status != task.StatusBlocked || !strings.Contains(res.Reason, "chưa đủ bằng chứng hoàn thành") ||
		!strings.Contains(res.Reason, "bint task verify") {
		t.Fatalf("phải chặn như cũ: %+v", res)
	}
	if n := len(h.load(id).State.Verifications); n != 1 {
		t.Fatalf("không được tự kiểm chứng lại khi tệp Touched đổi: %d lần", n)
	}
}

func TestFinalizeDoesNotReverifyUnconfirmedCommand(t *testing.T) {
	h := newHarness(t)
	id := h.finishFeat()
	h.cfg.Eval.TestCommand = nil // lệnh không còn được người xác nhận
	h.write("docs/other.md", "của người khác\n")
	res := h.resumeDo(id)
	if res.Status != task.StatusBlocked || !strings.Contains(res.Reason, "chưa được người xác nhận") {
		t.Fatalf("phải chặn vì lệnh chưa xác nhận: %+v", res)
	}
	if n := len(h.load(id).State.Verifications); n != 1 {
		t.Fatalf("không được chạy lệnh chưa xác nhận: %d lần", n)
	}
}

func TestFinalizeDoesNotReverifyWhenOtherBlockerExists(t *testing.T) {
	h := newHarness(t)
	id := h.finishFeat()
	// Tệp ngoài Touched nhưng là đường dẫn bảo vệ: chưa được duyệt nên done vẫn bị chặn dù kiểm chứng có chạy lại.
	h.write("migrations/002.sql", "ALTER TABLE x;\n")
	res := h.resumeDo(id)
	if res.Status != task.StatusBlocked || !strings.Contains(res.Reason, "chưa được duyệt") {
		t.Fatalf("phải chặn vì đường dẫn bảo vệ: %+v", res)
	}
	if n := len(h.load(id).State.Verifications); n != 1 {
		t.Fatalf("lý do chặn khác thì không tự kiểm chứng lại: %d lần", n)
	}
}

func TestFinalizeReverifyFailureBlocksWithResult(t *testing.T) {
	h := newHarness(t)
	cmd := "test -f done.txt && ! test -f poison.txt"
	h.cfg.Eval.TestCommand = &cmd
	id := h.finishFeat()
	h.write("poison.txt", "làm lệnh kiểm chứng hỏng\n")
	res := h.resumeDo(id)
	if res.Status != task.StatusBlocked || !strings.Contains(res.Reason, "tự kiểm chứng lại") ||
		!strings.Contains(res.Reason, "mã khác đổi sau lần kiểm chứng: poison.txt") || !strings.Contains(res.Reason, "mã thoát 1") ||
		!strings.Contains(res.Reason, "bint run resume "+id) {
		t.Fatalf("phải chặn kèm kết quả: %+v", res)
	}
	got := h.load(id)
	if len(got.State.Verifications) != 2 {
		t.Fatalf("đúng một lần chạy lại: %d lần", len(got.State.Verifications))
	}
	if v := got.State.LastVerification(); v.Passed || v.ExitCode != 1 {
		t.Fatalf("%+v", v)
	}
}

// Driver session đi chung finalize: tự kiểm chứng lại; lời nhắc tiếp tục là bint run next --resume.
func TestSessionFinalizeReverifiesAndResumeHint(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{})
	id := tk.State.ID
	// b-req và b-feat (kiểm chứng chạy ở Complete của b-feat), rồi người khác đổi tệp ngoài Touched trước b-review.
	for _, want := range []string{StepReq, StepFeat} {
		act, err := r.Next(context.Background(), NextOptions{TaskRef: id})
		if err != nil || act.Step != want {
			t.Fatalf("next: %+v %v", act, err)
		}
		h.subagent(act)
		if _, err := r.Complete(context.Background(), CompleteOptions{TaskRef: id, RunID: act.RunID, AgentID: "a-" + want}); err != nil {
			t.Fatal(err)
		}
	}
	h.write("docs/other.md", "của người khác\n")
	steps, last := h.drive(r, id, false)
	if last.Action != ActionDone || len(steps) != 1 || steps[0] != StepReview {
		t.Fatalf("bước %v, dừng %+v", steps, last)
	}
	got := loadTask(t, h, id)
	if len(got.State.Verifications) != 2 || !strings.Contains(got.State.LastVerification().Reason, "docs/other.md") {
		t.Fatalf("kiểm chứng: %+v", got.State.Verifications)
	}
}

func TestResumeHintFollowsDriver(t *testing.T) {
	// Finalize bị chặn vì tệp Touched đổi sau kiểm chứng: lý do nêu lệnh tiếp tục đúng driver.
	t.Run("session", func(t *testing.T) {
		h := newHarness(t)
		r := h.runner()
		tk := h.newSession(r, NewTaskOptions{})
		id := tk.State.ID
		for _, want := range []string{StepReq, StepFeat} {
			act, err := r.Next(context.Background(), NextOptions{TaskRef: id})
			if err != nil || act.Step != want {
				t.Fatalf("next: %+v %v", act, err)
			}
			h.subagent(act)
			if _, err := r.Complete(context.Background(), CompleteOptions{TaskRef: id, RunID: act.RunID, AgentID: "a-" + want}); err != nil {
				t.Fatal(err)
			}
		}
		h.write("app.txt", "sửa tiếp sau kiểm chứng\n")
		_, last := h.drive(r, id, false)
		reason := last.Reason
		if last.Result != nil {
			reason += " " + last.Result.Error
		}
		if last.Action != ActionBlocked || !strings.Contains(reason, "chưa đủ bằng chứng hoàn thành") ||
			!strings.Contains(reason, "`bint run next "+id+" --resume`") || strings.Contains(reason, "bint run resume") {
			t.Fatalf("session: %+v result=%+v", last, last.Result)
		}
	})
	t.Run("process", func(t *testing.T) {
		h := newHarness(t)
		id := h.finishFeat()
		h.write("app.txt", "sửa tiếp sau kiểm chứng\n")
		res := h.resumeDo(id)
		if res.Status != task.StatusBlocked || !strings.Contains(res.Reason, "`bint run resume "+id+"`") ||
			strings.Contains(res.Reason, "bint run next") {
			t.Fatalf("process: %+v", res)
		}
	})
}

func TestStaleVerifyOnlyConditions(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	id := h.finishFeat()
	e := h.runner().Env
	if _, ok := e.staleVerifyOnly(ctx, h.load(id)); ok {
		t.Fatal("mã chưa đổi: không có gì để chạy lại")
	}
	h.write("docs/other.md", "của người khác\n")
	files, ok := e.staleVerifyOnly(ctx, h.load(id))
	if !ok || len(files) != 1 || files[0] != "docs/other.md" {
		t.Fatalf("tệp ngoài Touched đổi: %v %v", files, ok)
	}
	in, err := e.DoneInputDetailed(ctx, h.load(id))
	if err != nil || len(in.TouchedSinceVerify) != 0 || len(in.OutsideSinceVerify) != 1 {
		t.Fatalf("DoneInputDetailed: %+v %v", in, err)
	}
	// Không có ảnh chụp lúc kiểm chứng: không biết tệp nào đổi nên không chạy lại.
	if _, err := h.tasks.Update(id, time.Now(), func(t *task.Task) error {
		t.State.Verifications[0].Tree = ""
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.staleVerifyOnly(ctx, h.load(id)); ok {
		t.Fatal("không có Tree: không được tự chạy lại")
	}
	// Có thêm tệp Touched đổi: TouchedSinceVerify liệt kê đúng và không chạy lại.
	h2 := newHarness(t)
	id2 := h2.finishFeat()
	h2.write("docs/other.md", "x\n")
	h2.write("app.txt", "sửa tiếp\n")
	e2 := h2.runner().Env
	in2, err := e2.DoneInputDetailed(ctx, h2.load(id2))
	if err != nil || len(in2.TouchedSinceVerify) != 1 || in2.TouchedSinceVerify[0] != "app.txt" ||
		len(in2.OutsideSinceVerify) != 1 || in2.OutsideSinceVerify[0] != "docs/other.md" {
		t.Fatalf("DoneInputDetailed: %+v %v", in2, err)
	}
	if _, ok := e2.staleVerifyOnly(ctx, h2.load(id2)); ok {
		t.Fatal("có tệp Touched đổi: không được tự chạy lại")
	}
}
