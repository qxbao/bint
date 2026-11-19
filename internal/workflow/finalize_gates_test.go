package workflow

import (
	"context"
	"strings"
	"testing"
	"time"

	"bint/internal/affected"
	"bint/internal/task"
)

// A1: blockFinalize chỉ nhớ dấu vân tay (v == nil) khi lý do chặn duy nhất là lệnh kiểm chứng chưa xác nhận.
func TestBlockFinalizeFPOnlyWhenUnconfirmedVerifyIsTheOnlyReason(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	id := h.finishFeat()
	h.cfg.Eval.TestCommand = nil // lệnh chưa được xác nhận
	r := h.runner()
	if _, err := r.Tasks.Update(id, time.Now(), func(tk *task.Task) error { tk.State.Verifications = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	block := func() string {
		rs := &runState{t: h.load(id), res: &Result{Steps: []StepResult{}}, ran: map[string]int{}}
		r.blockFinalize(ctx, rs, "thử")
		return h.load(id).State.FinalizeFP
	}
	if fp := block(); fp == "" {
		t.Fatal("chỉ thiếu xác nhận lệnh: phải nhớ dấu vân tay")
	}
	setCriteriaRaw(t, r, id, "app.txt ghi v2")
	if _, err := r.CheckCriterion(id, 1, false, "chưa xong"); err != nil {
		t.Fatal(err)
	}
	if fp := block(); fp != "" {
		t.Fatalf("còn tiêu chí chưa đạt: phải xoá dấu vân tay, có %q", fp)
	}
}

// A2: kiểm chứng đầy đủ ở finalize quá giờ là lỗi môi trường: chặn chờ người, không mở vòng sửa b-feat.
func TestFinalizeFullVerifyTimeoutBlocksWithoutFixRound(t *testing.T) {
	old := verifyTimeoutUnit
	verifyTimeoutUnit = time.Second
	t.Cleanup(func() { verifyTimeoutUnit = old })
	h := goHarness(t)
	h.cfg.Workflow.MaxReviewRounds = 3
	h.cfg.Workflow.VerifyTimeoutMinutes = 1
	full := "sleep 30"
	h.cfg.Eval.TestCommand = &full
	feats := 0
	h.behaveFeat(func() {
		feats++
		h.write("a/a.go", "package a\n\nfunc A() int { return 2 }\n")
		h.write("done.txt", "ok\n")
	})
	id := h.runFeat("Thêm tính năng cho gói a", "").TaskID
	res := h.resumeDo(id)
	if res.Status != task.StatusBlocked || feats != 1 {
		t.Fatalf("muốn blocked không vòng sửa: status=%s feats=%d reason=%q", res.Status, feats, res.Reason)
	}
	if !strings.Contains(res.Reason, "quá giờ") || !strings.Contains(res.Reason, "verify_timeout_minutes") || !strings.Contains(res.Reason, "--force") {
		t.Fatalf("lý do thiếu gợi ý: %q", res.Reason)
	}
	if h.load(id).State.FinalizeFP == "" {
		t.Fatal("quá giờ: resume không đổi cây phải chạy lại kiểm chứng, không review lại")
	}
}

// A3: quyết định dùng lại lần affected.
func TestAffectedCoversCommand(t *testing.T) {
	exp := func(m map[string][]string) func(string) ([]string, error) {
		return func(p string) ([]string, error) { return m[p], nil }
	}
	const cmd = "go test -count=1 ./a/... ./b"
	v := func(ran string, dirs ...string) *task.Verification {
		return &task.Verification{Command: cmd, Passed: true, Scope: "graph", Ran: ran,
			Selection: &affected.Selection{Scope: affected.TierGraph, TestDirs: dirs}}
	}
	ex := exp(map[string][]string{"./a/...": {"a", "a/x"}, "./b": {"b"}, "./a": {"a"}})
	notPassed := v("go test -count=1 ./a", "./a")
	notPassed.Passed = false
	none := v("", "./a")
	none.Scope = "none"
	other := v("go test -run X ./a", "./a")
	other.Command = "go test -run X ./a"
	cases := []struct {
		name string
		v    *task.Verification
		cmd  string
		want bool
	}{
		{"bao trùm", v("go test -count=1 ./a ./a/x ./b ./c", "./a", "./a/x", "./b", "./c"), cmd, true},
		{"thiếu gói", v("go test -count=1 ./a ./b", "./a", "./b"), cmd, false},
		{"khác cờ", v("go test ./a ./a/x ./b", "./a", "./a/x", "./b"), cmd, false},
		{"cờ -run", other, other.Command, false},
		{"không qua", notPassed, cmd, false},
		{"scope none", none, cmd, false},
	}
	for _, c := range cases {
		if got := affectedCoversCommand(c.v, c.cmd, ex); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	bad := func(string) ([]string, error) { return nil, context.Canceled }
	if affectedCoversCommand(v("go test -count=1 ./a ./b", "./a", "./b"), cmd, bad) {
		t.Error("go list lỗi phải bảo thủ: không bao trùm")
	}
	if affectedCoversCommand(v("echo ./a", "./a"), "make test", ex) {
		t.Error("lệnh không phải go test thuần phải chạy lại")
	}
}

// A4: số đo đỏ của lát bị bỏ không vào vòng sửa.
func TestDiscardVoidsRedChecks(t *testing.T) {
	tk := &task.Task{}
	tk.State.Verifications = []task.Verification{{Passed: false, RunID: "r1"}}
	tk.State.GoalChecks = []task.Verification{{Passed: false, RunID: "r1"}}
	if redVerification(tk) == nil {
		t.Fatal("chưa bỏ lát: phải thấy đỏ")
	}
	voidRedChecks(&tk.State)
	if v := redVerification(tk); v != nil {
		t.Fatalf("sau discard phải bỏ qua: %+v", v)
	}
	tk.State.Verifications = append(tk.State.Verifications, task.Verification{Passed: false, RunID: "r2"})
	if redVerification(tk) == nil {
		t.Fatal("lần đỏ mới sau discard phải tính")
	}
}
