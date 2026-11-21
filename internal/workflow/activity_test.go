package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/task"
)

// Lệnh kiểm chứng chạy với State.Activity đã ghi (TUI thấy ngay lệnh đang chạy và log), và Activity được xoá khi xong.
func TestVerifyRecordsActivityWhileRunning(t *testing.T) {
	h := newHarness(t)
	out := filepath.Join(t.TempDir(), "state-during.json")
	id := h.finishFeat()
	state := filepath.Join(h.load(id).Dir, "state.json")
	q := func(p string) string { return "'" + strings.ReplaceAll(filepath.ToSlash(p), "'", `'\''`) + "'" }
	cmd := "cp " + q(state) + " " + q(out)
	h.cfg.Eval.TestCommand = &cmd
	if _, err := h.tasks.Update(id, h.runner().now(), func(x *task.Task) error { x.Ticket.Verify.Command = cmd; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, vr, err := h.runner().Verify(context.Background(), id); err != nil || !vr.Verification.Passed {
		t.Fatalf("verify: %+v %v", vr, err)
	}
	during, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(during), `"activity"`) || !strings.Contains(string(during), `"verify"`) {
		t.Fatalf("trong lúc chạy, state.json phải có activity của lệnh kiểm chứng:\n%s", during)
	}
	if a := h.load(id).State.Activity; a != nil {
		t.Fatalf("xong kiểm chứng phải xoá activity: %+v", a)
	}
}
