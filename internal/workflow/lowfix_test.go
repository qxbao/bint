package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"bint/internal/task"
	"bint/internal/testutil"
)

// Phiếu 01M7FKMZDVQ3XFR0EC1H5P2AXM: low .3 của NT-WF-BASELINE và ba lỗi C1–C3 của driver session.

// commitAll commit mọi thay đổi ở dir (chỉ HEAD đổi nếu cây đã chứa chúng).
func (h *harness) commitAll(dir, msg string) {
	h.t.Helper()
	testutil.Git(h.t, dir, "add", "-A")
	testutil.Git(h.t, dir, "commit", "-q", "-m", msg)
}

// NT-WF-BASELINE.3: commit chỉ đổi HEAD mà cây giữ nguyên (ảnh chụp trùng Verification.Tree) không làm Finalize chặn
// và không chạy thêm lần kiểm chứng nào.
func TestFinalizeIgnoresHeadOnlyChange(t *testing.T) {
	h := newHarness(t)
	id := h.finishFeat()
	before := loadTask(t, h, id).State.LastVerification().Worktree
	h.commitAll(h.root, "commit chỉ đổi HEAD")
	in, err := h.runner().Env.DoneInputDetailed(context.Background(), loadTask(t, h, id))
	if err != nil || in.Worktree != before {
		t.Fatalf("DoneInputDetailed phải coi lần kiểm chứng còn mới: %+v %v", in, err)
	}
	res := h.resumeDo(id)
	if res.Status != task.StatusDone {
		t.Fatalf("commit chỉ đổi HEAD phải tới done: %+v", res)
	}
	if n := len(loadTask(t, h, id).State.Verifications); n != 1 {
		t.Fatalf("không được chạy thêm lần kiểm chứng: %d lần", n)
	}
}

// poisonCmd hỏng khi poison.txt tồn tại: mô phỏng nguyên nhân hỏng nằm trong mã.
const poisonCmd = "test -f done.txt && ! test -f poison.txt"

// C1 (driver process): kiểm chứng cuối đỏ, người sửa nguyên nhân bằng commit (cây đổi); resume chạy b-review rồi
// Finalize tự chạy lại kiểm chứng đúng một lần thay vì chặn bằng kết quả cũ.
func TestFinalizeReverifiesAfterFailedVerifyAndFix(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	cmd := poisonCmd
	h.cfg.Eval.TestCommand = &cmd
	id := h.finishFeat()
	h.write("poison.txt", "làm lệnh kiểm chứng hỏng\n")
	if _, vr, err := h.runner().VerifyWith(ctx, id, "sự cố"); err != nil || vr.Verification.Passed {
		t.Fatalf("cần một lần kiểm chứng hỏng: %+v %v", vr, err)
	}
	nv := len(loadTask(t, h, id).State.Verifications)
	// Người sửa nguyên nhân: bỏ poison.txt, thêm tệp sửa, commit (cherry-pick).
	if err := os.Remove(filepath.Join(h.root, "poison.txt")); err != nil {
		t.Fatal(err)
	}
	h.write("docs/fix.md", "sửa nguyên nhân\n")
	h.commitAll(h.root, "cherry-pick bản sửa")

	res := h.resumeDo(id)
	if res.Status != task.StatusDone || !slices.Contains(stepNames(res), StepReview) {
		t.Fatalf("phải review rồi done: %+v steps=%v", res, stepNames(res))
	}
	got := loadTask(t, h, id)
	v := got.State.LastVerification()
	if len(got.State.Verifications) != nv+1 || !v.Passed || !strings.Contains(v.Reason, "docs/fix.md") {
		t.Fatalf("cần đúng một lần kiểm chứng mới có Reason: %+v", got.State.Verifications)
	}
}

// C1: không chạy lại khi lệnh chưa được xác nhận, còn điều chặn khác, hoặc cây không đổi (chỉ HEAD).
func TestFinalizeDoesNotReverifyFailedVerifyWhenGuarded(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		prep func(h *harness)
		want string
	}{
		{"lệnh chưa xác nhận", func(h *harness) {
			os.Remove(filepath.Join(h.root, "poison.txt"))
			h.write("docs/fix.md", "sửa\n")
			h.cfg.Eval.TestCommand = nil
		}, "chưa được người xác nhận"},
		{"còn điều chặn khác", func(h *harness) {
			os.Remove(filepath.Join(h.root, "poison.txt"))
			h.write("docs/fix.md", "sửa\n")
			h.write("migrations/002.sql", "ALTER TABLE x;\n")
		}, "chưa được duyệt"},
		{"cây không đổi (chỉ HEAD)", func(h *harness) { h.commitAll(h.root, "chỉ đổi HEAD") }, "lần kiểm chứng cuối không qua"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			cmd := poisonCmd
			h.cfg.Eval.TestCommand = &cmd
			id := h.finishFeat()
			h.write("poison.txt", "x\n")
			if _, vr, err := h.runner().VerifyWith(ctx, id, ""); err != nil || vr.Verification.Passed {
				t.Fatalf("%+v %v", vr, err)
			}
			tc.prep(h)
			res := h.resumeDo(id)
			if res.Status != task.StatusBlocked || !strings.Contains(res.Reason, tc.want) {
				t.Fatalf("phải chặn (%s): %+v", tc.want, res)
			}
			if n := len(loadTask(t, h, id).State.Verifications); n != 2 {
				t.Fatalf("không được chạy lại kiểm chứng: %d lần", n)
			}
		})
	}
}

// C1 (driver session, tác vụ worktree): resume chạy b-review rồi done với đúng một lần kiểm chứng mới có Reason.
func TestSessionWorktreeFinalizeReverifiesAfterFix(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t).wtHarness()
	cmd := poisonCmd
	h.cfg.Eval.TestCommand = &cmd
	h.behave = featWrites(h, nil)
	r := h.runner()
	tk := h.newSession(r, NewTaskOptions{Plan: ptr(false)})
	id := tk.State.ID
	act, err := r.Next(ctx, NextOptions{TaskRef: id})
	if err != nil || act.Step != StepFeat {
		t.Fatalf("next: %+v %v", act, err)
	}
	h.subagent(act)
	if _, err := r.Complete(ctx, CompleteOptions{TaskRef: id, RunID: act.RunID, AgentID: "a-feat"}); err != nil {
		t.Fatal(err)
	}
	wt := loadTask(t, h, id).State.Worktree
	if err := writeAt(wt, "poison.txt", "làm lệnh kiểm chứng hỏng\n"); err != nil {
		t.Fatal(err)
	}
	if _, vr, err := h.runner().VerifyWith(ctx, id, "sự cố"); err != nil || vr.Verification.Passed {
		t.Fatalf("cần kiểm chứng hỏng: %+v %v", vr, err)
	}
	steps, last := h.drive(r, id, false)
	if last.Action != ActionBlocked || !slices.Equal(steps, []string{StepReview}) {
		t.Fatalf("lượt đầu phải review rồi chặn: %v %+v", steps, last)
	}
	nv := len(loadTask(t, h, id).State.Verifications)
	// Người sửa nguyên nhân bằng commit vào nhánh tác vụ (cây đổi).
	if err := os.Remove(filepath.Join(wt, "poison.txt")); err != nil {
		t.Fatal(err)
	}
	if err := writeAt(wt, "docs/fix.md", "sửa nguyên nhân\n"); err != nil {
		t.Fatal(err)
	}
	h.commitAll(wt, "cherry-pick bản sửa")

	steps, last = h.drive(r, id, true)
	if last.Action != ActionDone || !slices.Equal(steps, []string{StepReview}) {
		t.Fatalf("resume phải chạy b-review rồi done: %v %+v", steps, last)
	}
	got := loadTask(t, h, id)
	v := got.State.LastVerification()
	if len(got.State.Verifications) != nv+1 || !v.Passed || v.Reason == "" {
		t.Fatalf("đúng một lần kiểm chứng mới có Reason: %+v", got.State.Verifications)
	}
}

// landedConflicted dựng tác vụ worktree đã đưa về bằng land --3way (còn dấu xung đột ở app.txt, worktree đã gỡ), theo
// driver session (session=true) hoặc process.
func landedConflicted(t *testing.T, session bool) (*harness, string) {
	t.Helper()
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
	var id string
	if session {
		id = h.newSession(r, NewTaskOptions{Plan: ptr(false)}).State.ID
		if _, last := h.drive(r, id, false); last.Action != ActionBlocked {
			t.Fatalf("phải blocked vì xung đột: %+v", last)
		}
	} else {
		id = h.newDo(r, NewTaskOptions{}).State.ID
		if res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: id}); err != nil || res.Status != task.StatusBlocked {
			t.Fatalf("phải blocked: %+v %v", res, err)
		}
	}
	if _, err := h.runner().Env.Land(context.Background(), id, LandOptions{ThreeWay: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r.WorktreePath(id)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("worktree phải gỡ sau khi đưa về")
	}
	h.behave = func(context.Context, string, *Env, string) error { t.Error("không được chạy agent"); return nil }
	return h, id
}

// resumeLanded tiếp tục tác vụ đã đưa về theo driver tương ứng; trả trạng thái cuối và lý do.
func resumeLanded(t *testing.T, h *harness, id string, session bool) (status, reason string) {
	t.Helper()
	if session {
		act, err := h.runner().Next(context.Background(), NextOptions{TaskRef: id, Resume: true})
		if err != nil {
			t.Fatal(err)
		}
		return act.Status, act.Reason
	}
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: id, Resume: true})
	if err != nil {
		t.Fatal(err)
	}
	return res.Status, res.Reason
}

// C3: tác vụ đã đưa về mà thư mục worktree đã gỡ, hoặc còn sót nhưng không còn là gốc worktree Git, đi tiếp tới done qua
// cổng done (vẫn chặn khi còn dấu xung đột của land --3way), không chuyển failed.
func TestLandedTaskResumesToDoneWhenWorktreeGone(t *testing.T) {
	for _, session := range []bool{true, false} {
		for _, leftover := range []bool{false, true} {
			name := map[bool]string{true: "session", false: "process"}[session] + "/" + map[bool]string{true: "thư mục sót", false: "đã gỡ"}[leftover]
			t.Run(name, func(t *testing.T) {
				h, id := landedConflicted(t, session)
				wt := loadTask(t, h, id).State.Worktree
				if leftover {
					if err := writeAt(wt, "rac.txt", "sót lại sau khi gỡ dở\n"); err != nil {
						t.Fatal(err)
					}
				}
				// Còn dấu xung đột: vẫn chặn (không phải failed).
				if st, reason := resumeLanded(t, h, id, session); st != task.StatusBlocked || !strings.Contains(reason, "app.txt") {
					t.Fatalf("còn dấu xung đột phải blocked: %s %q", st, reason)
				}
				if err := writeAt(h.root, "app.txt", "a\nB-gop\nc\n"); err != nil {
					t.Fatal(err)
				}
				if st, reason := resumeLanded(t, h, id, session); st != task.StatusDone {
					t.Fatalf("đã đưa về phải tới done: %s %q", st, reason)
				}
			})
		}
	}
}

// C2: tệp mà cây gốc đã có đúng bản của tác vụ (đã cherry-pick từ cây gốc) không phải xung đột: bỏ qua khi áp, các tệp
// khác vẫn được áp, Landed.Files vẫn liệt kê, kết quả có ghi chú; tệp khác nội dung vẫn là xung đột và cây gốc không đổi.
func TestLandSkipsFilesIdenticalAtOrigin(t *testing.T) {
	setup := func(t *testing.T, originApp string) (*harness, string) {
		h := newHarness(t).wtHarness()
		h.behave = featWrites(h, func(e *Env) error {
			// Cây gốc nhận trong lúc b-feat chạy: bản y hệt (cherry-pick) hoặc khác.
			return writeAt(h.root, "app.txt", originApp)
		})
		r := h.runner()
		id := h.newDo(r, NewTaskOptions{}).State.ID
		if _, err := r.Run(context.Background(), RunOptions{Workflow: StepFeat, TaskRef: id}); err != nil {
			t.Fatal(err)
		}
		return h, id
	}
	t.Run("giống hệt", func(t *testing.T) {
		h, id := setup(t, "v2\n")
		res, err := h.runner().Env.Land(context.Background(), id, LandOptions{})
		if err != nil {
			t.Fatalf("không được báo xung đột: %v", err)
		}
		if !slices.Equal(res.Files, []string{"app.txt", "done.txt"}) {
			t.Fatalf("Landed.Files vẫn liệt kê tệp bỏ qua: %v", res.Files)
		}
		if !slices.ContainsFunc(res.Notes, func(n string) bool { return strings.Contains(n, "app.txt") && strings.Contains(n, "giống hệt") }) {
			t.Fatalf("thiếu ghi chú: %v", res.Notes)
		}
		if h.readFile(h.root, "done.txt") != "ok\n" || h.readFile(h.root, "app.txt") != "v2\n" {
			t.Fatal("tệp khác vẫn phải được áp")
		}
	})
	// Review .1 của phiếu 01M7FKMZDVQ3XFR0EC1H5P2AXM: cùng nội dung nhưng tác vụ đổi quyền thực thi thì không
	// phải "giống hệt" — bỏ qua sẽ làm mất chmod +x.
	t.Run("cùng nội dung khác quyền", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("quyền thực thi không có trên Windows (core.fileMode=false)")
		}
		h := newHarness(t).wtHarness()
		h.behave = featWrites(h, func(e *Env) error {
			if err := os.Chmod(filepath.Join(e.Root, "app.txt"), 0o755); err != nil {
				return err
			}
			return writeAt(h.root, "app.txt", "v2\n") // cây gốc: cùng nội dung, vẫn 0644
		})
		r := h.runner()
		id := h.newDo(r, NewTaskOptions{}).State.ID
		if _, err := r.Run(context.Background(), RunOptions{Workflow: StepFeat, TaskRef: id}); err != nil {
			t.Fatal(err)
		}
		var ce *LandConflictError
		if _, err := h.runner().Env.Land(context.Background(), id, LandOptions{}); !errors.As(err, &ce) || !slices.Equal(ce.Files, []string{"app.txt"}) {
			t.Fatalf("khác quyền phải là xung đột, không được bỏ qua: %v", err)
		}
	})
	t.Run("khác nội dung", func(t *testing.T) {
		h, id := setup(t, "nguoi dung sua\n")
		var ce *LandConflictError
		if _, err := h.runner().Env.Land(context.Background(), id, LandOptions{}); !errors.As(err, &ce) || !slices.Equal(ce.Files, []string{"app.txt"}) {
			t.Fatalf("phải LandConflictError: %v", err)
		}
		if h.readFile(h.root, "app.txt") != "nguoi dung sua\n" {
			t.Fatal("cây gốc bị đụng")
		}
		if _, err := os.Stat(filepath.Join(h.root, "done.txt")); err == nil {
			t.Fatal("áp nguyên tử: done.txt không được áp khi xung đột")
		}
	})
}

// C2: cả hai phía cùng xoá một tệp cũng không phải xung đột.
func TestLandSkipsFileDeletedOnBothSides(t *testing.T) {
	h := newHarness(t).wtHarness()
	h.write("old.txt", "cũ\n")
	h.git("add", "old.txt")
	h.git("commit", "-q", "-m", "thêm old.txt")
	h.behave = featWrites(h, func(e *Env) error {
		if err := os.Remove(filepath.Join(e.Root, "old.txt")); err != nil {
			return err
		}
		return os.Remove(filepath.Join(h.root, "old.txt")) // người dùng xoá cùng tệp ở cây gốc
	})
	r := h.runner()
	id := h.newDo(r, NewTaskOptions{}).State.ID
	if _, err := r.Run(context.Background(), RunOptions{Workflow: StepFeat, TaskRef: id}); err != nil {
		t.Fatal(err)
	}
	res, err := h.runner().Env.Land(context.Background(), id, LandOptions{})
	if err != nil {
		t.Fatalf("cả hai phía cùng xoá không phải xung đột: %v", err)
	}
	if !slices.Contains(res.Files, "old.txt") {
		t.Fatalf("Files: %v", res.Files)
	}
	if h.readFile(h.root, "done.txt") != "ok\n" {
		t.Fatal("tệp khác vẫn phải được áp")
	}
}
