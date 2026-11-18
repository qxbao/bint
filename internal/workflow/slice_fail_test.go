package workflow

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"bint/internal/eval"
	"bint/internal/gitrepo"
	"bint/internal/task"
)

// Agent lỗi (hết quota/chi phí, hạ tầng) GIỮA một lát: lát dở không phải "bị bỏ" mà là "chưa làm": cây về điểm xanh, tác vụ blocked, resume chạy lại
// đúng lát đó (không nhảy sang lát kế). Khác với lát quá trần thời gian hoặc không đạt số đo (đã bỏ, chạy lát kế): TestSliceRatchet.
func TestSliceAgentFailureKeepsSlicePending(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.write("old.txt", slOldBody)
	h.write("verify.sh", slVerifyScript)
	h.commitAll(h.root, "test cũ và lệnh kiểm chứng")
	vc := "sh verify.sh"
	h.cfg.Eval.TestCommand = &vc
	h.cfg.Workflow.Verify.AffectedCommand = "sh {test_files}"
	h.cfg.Workflow.TimeoutMinutes = 45
	h.goal = func(_ context.Context, e *Env, _ string) error { return slWrite(e.Root, slGoalFile, slGoalScript) }

	latRe := regexp.MustCompile(`LẦN NÀY CHỈ LÀM lát (\d+)/3`)
	attempts := map[int]int{}
	cur := 0
	var id string
	h.behave = func(ctx context.Context, step string, e *Env, tid string) error {
		switch step {
		case StepReq:
			id = tid
			if _, _, err := e.SetTicket(tid, TicketEdit{ClearCriteria: true, Criteria: []string{"c1.txt ghi ok-c1", "c2.txt ghi ok-c2", "c3.txt ghi ok-c3"}}); err != nil {
				return err
			}
			for n, body := range slDocs(slOpts{}) {
				if _, _, err := e.WriteArtifact(tid, n, body); err != nil {
					return err
				}
			}
		case StepReview:
			_, _, err := e.SubmitReview(tid, []byte(`{"summary":"ổn","findings":[]}`))
			return err
		case StepFeat:
			m := latRe.FindStringSubmatch(h.slFeatPrompt())
			if m == nil {
				for _, c := range []string{"c1", "c2", "c3"} {
					if err := slWrite(e.Root, c+".txt", "ok-"+c+"\n"); err != nil {
						return err
					}
				}
				return nil
			}
			cur, _ = strconv.Atoi(m[1])
			attempts[cur]++
			switch {
			case cur == 1:
				return slWrite(e.Root, "c1.txt", "ok-c1\n")
			case cur == 2 && attempts[2] == 1: // lần đầu: ghi dở rồi agent chết
				for rel, body := range map[string]string{"c2.txt": "ok-c2\n", "l2-extra.txt": "phụ của L2\n"} {
					if err := slWrite(e.Root, rel, body); err != nil {
						return err
					}
				}
			case cur == 2:
				return slWrite(e.Root, "c2.txt", "ok-c2\n")
			default:
				return slWrite(e.Root, "c3.txt", "ok-c3\n")
			}
		}
		return nil
	}
	h.mutateReq = func(_ eval.AgentRequest, step string, res *eval.AgentResult) {
		if step == StepFeat && cur == 2 && attempts[2] == 1 {
			res.Error, res.ExitCode = "hết quota", 1
		}
	}

	r := h.runner()
	res, err := r.Run(ctx, RunOptions{Workflow: StepDo, Request: slReq, Plan: ptr(true)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status == task.StatusBlocked && strings.HasPrefix(res.Reason, "kế hoạch lớn") {
		if res, err = r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: res.TaskID, Resume: true}); err != nil {
			t.Fatal(err)
		}
	}
	if res.Status != task.StatusBlocked || !strings.Contains(res.Reason, "lát 2/3") || !strings.Contains(res.Reason, "chạy lại lát này") {
		t.Fatalf("agent chết giữa lát 2: tác vụ phải blocked, nêu lát 2 sẽ chạy lại: %s %q", res.Status, res.Reason)
	}
	checkSliceList(t, "Result.Slices sau khi agent lỗi giữa lát 2", resultSlices(res), []string{"accepted", "", ""})
	if got := h.read("c2.txt"); got != "<không có>" {
		t.Errorf("c2.txt do lát dở tạo phải bị xoá khi về điểm xanh, còn %q", got)
	}
	if got := h.read("l2-extra.txt"); got != "<không có>" {
		t.Errorf("l2-extra.txt do lát dở tạo phải bị xoá, còn %q", got)
	}
	if got := h.read("c1.txt"); got != "ok-c1\n" {
		t.Errorf("c1.txt (lát 1 đã chốt) phải còn nguyên: %q", got)
	}
	if out, err := gitrepo.Run(ctx, h.root, "show", slSliceRef(id, "slice-2-discarded")+":l2-extra.txt"); err != nil || !strings.Contains(out, "phụ của L2") {
		t.Errorf("phần dở của lát 2 phải ghim ở slice-2-discarded để lấy lại: %q %v", out, err)
	}

	// Resume: chạy lại đúng lát 2 (không nhảy sang lát 3, không chạy lại lát 1).
	res, err = h.runner().Run(ctx, RunOptions{Workflow: StepDo, TaskRef: id, Resume: true})
	if err != nil {
		t.Fatal(err)
	}
	if attempts[1] != 1 || attempts[2] != 2 || attempts[3] != 1 {
		t.Errorf("số lần chạy mỗi lát = %v, muốn lát 1 một lần, lát 2 hai lần (lỗi rồi chạy lại), lát 3 một lần", attempts)
	}
	items, _ := stateSlices(loadTask(t, h, id))
	checkSliceList(t, "State.Slices sau resume", items, []string{"accepted", "accepted", "accepted"})
	if res.Status != task.StatusDone {
		t.Errorf("sau resume tác vụ phải done: %s %q", res.Status, res.Reason)
	}
}
