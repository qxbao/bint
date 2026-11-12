package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/task"
	"bint/internal/testutil"
	"bint/internal/workflow"
)

// gcNow dọn mọi đối tượng không còn ref (như git gc sau khi hết hạn reflog): cây mà Bint không còn giữ bị xoá thật.
func gcNow(t *testing.T, root string) {
	t.Helper()
	testutil.Git(t, root, "reflog", "expire", "--expire=now", "--all")
	testutil.Git(t, root, "gc", "--prune=now", "-q")
}

// wantPruned kiểm lỗi của `task diff`: bọc ErrTaskTreePruned, nêu ID, trạng thái, mã cây thiếu, nguyên nhân git gc,
// và không lộ lỗi thô của git.
func wantPruned(t *testing.T, err error, id, status string) string {
	t.Helper()
	if !errors.Is(err, ErrTaskTreePruned) {
		t.Fatalf("muốn ErrTaskTreePruned, được %v", err)
	}
	msg := err.Error()
	for _, want := range []string{id, status, "git gc", "kết thúc"} {
		if !strings.Contains(msg, want) {
			t.Errorf("thông báo thiếu %q: %s", want, msg)
		}
	}
	for _, bad := range []string{"fatal:", "bad object", "Not a valid object"} {
		if strings.Contains(msg, bad) {
			t.Errorf("thông báo lộ lỗi thô của git (%q): %s", bad, msg)
		}
	}
	return msg
}

// Tác vụ shared đã done: BaseTree (cây gồm tệp chưa commit lúc đầu) bị git gc dọn ⇒ lỗi dễ hiểu, không phải lỗi thô.
// Trước khi gc, diff vẫn ra bình thường.
func TestDiffTaskSharedPrunedBaseTree(t *testing.T) {
	ws, root, id := taskRepo(t)
	ctx := context.Background()
	if _, _, err := ApproveTask(ctx, ws, TaskApproveOptions{Ref: id}); err != nil {
		t.Fatal(err)
	}
	// Tệp chưa commit có sẵn: BaseTree là một cây riêng, không trùng cây của commit nào.
	if err := os.WriteFile(filepath.Join(root, "pre.txt"), []byte("co san\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if res := runFeat(t, ws, id); res.Status != task.StatusInReview {
		t.Fatalf("b-feat: %+v", res)
	}
	tk, err := ws.Tasks.Load(id)
	if err != nil || tk.State.BaseTree == "" {
		t.Fatalf("thiếu BaseTree: %+v %v", tk, err)
	}
	if d, err := DiffTask(ctx, ws, TaskDiffOptions{Ref: id, NameOnly: true}); err != nil || strings.TrimSpace(d.Text) != "a.go" {
		t.Fatalf("diff khi đang chạy: %+v %v", d, err)
	}
	if _, _, err := ws.SetStatus(ctx, id, task.StatusDone, workflow.TransitionOptions{Reason: "xong", Force: true}); err != nil {
		t.Fatal(err)
	}
	if out := testutil.Git(t, root, "for-each-ref", "refs/bint/tasks/"+id+"/"); strings.TrimSpace(out) != "" {
		t.Fatalf("còn ref sau done: %s", out)
	}
	// Chưa gc: cây còn (đối tượng chưa bị dọn) nên diff vẫn ra.
	if _, err := DiffTask(ctx, ws, TaskDiffOptions{Ref: id, NameOnly: true}); err != nil {
		t.Fatalf("diff trước gc: %v", err)
	}
	gcNow(t, root)
	_, err = DiffTask(ctx, ws, TaskDiffOptions{Ref: id, NameOnly: true})
	msg := wantPruned(t, err, id, "done")
	if !strings.Contains(msg, tk.State.BaseTree[:10]) {
		t.Errorf("thiếu mã cây %s: %s", tk.State.BaseTree[:10], msg)
	}
}

// Tác vụ worktree đã land rồi done: Landed.Tree (cây gồm thay đổi chưa commit ở cây gốc) bị git gc dọn.
func TestDiffTaskWorktreePrunedLandedTree(t *testing.T) {
	ws, root, id := taskRepo(t)
	ctx := context.Background()
	if _, err := ws.Tasks.Update(id, time.Now(), func(tk *task.Task) error {
		tk.State.Classification.Isolation, tk.State.Classification.IsolationReason = task.IsolationWorktree, task.IsolationReasonUser
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ApproveTask(ctx, ws, TaskApproveOptions{Ref: id}); err != nil {
		t.Fatal(err)
	}
	if res := runFeat(t, ws, id); res.Status != task.StatusInReview {
		t.Fatalf("b-feat: %+v", res)
	}
	if _, err := LandTask(ctx, ws, TaskLandOptions{Ref: id}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ws.SetStatus(ctx, id, task.StatusDone, workflow.TransitionOptions{Reason: "xong", Force: true}); err != nil {
		t.Fatal(err)
	}
	if out := testutil.Git(t, root, "for-each-ref", "refs/bint/tasks/"+id+"/"); strings.TrimSpace(out) != "" {
		t.Fatalf("còn ref sau land+done: %s", out)
	}
	tk, err := ws.Tasks.Load(id)
	if err != nil || tk.State.Landed == nil || tk.State.Landed.Tree == "" {
		t.Fatalf("thiếu Landed.Tree: %+v %v", tk, err)
	}
	if _, err := DiffTask(ctx, ws, TaskDiffOptions{Ref: id, NameOnly: true}); err != nil {
		t.Fatalf("diff trước gc: %v", err)
	}
	gcNow(t, root)
	_, err = DiffTask(ctx, ws, TaskDiffOptions{Ref: id, NameOnly: true})
	msg := wantPruned(t, err, id, "done")
	// Mốc đầu (BaseTree gồm tệp chưa theo dõi của kho thử) và Landed.Tree đều đã bị dọn; thông báo nêu mốc thiếu đầu
	// tiên và danh sách tệp. Landed.Tree một mình cũng bị phát hiện.
	if !strings.Contains(msg, "a.go") || !strings.Contains(msg, shortRev(tk.State.BaseTree)) && !strings.Contains(msg, shortRev(tk.State.Landed.Tree)) {
		t.Errorf("thiếu mã cây thiếu hoặc danh sách tệp: %s", msg)
	}
	err = checkTreesPresent(ctx, root, tk, "", tk.State.Landed.Tree)
	if !errors.Is(err, ErrTaskTreePruned) || !strings.Contains(err.Error(), shortRev(tk.State.Landed.Tree)) {
		t.Errorf("Landed.Tree thiếu phải được phát hiện: %v", err)
	}
}

// Lỗi không phải "cây bị dọn" (không chạy được git) không bị gói thành ErrTaskTreePruned.
func TestCheckTreesPresentIgnoresEnvironmentErrors(t *testing.T) {
	tk := &task.Task{}
	if err := checkTreesPresent(context.Background(), filepath.Join(t.TempDir(), "khong-co"), tk, "deadbeef"); err != nil {
		t.Fatalf("thư mục không phải repository: %v", err)
	}
}

// Lệnh setup: chỉ người xác nhận được (agent bị ErrAgentForbidden và không ghi SetupApproval); kế hoạch duyệt,
// chi tiết tác vụ phản ánh trạng thái; đổi lệnh thì chờ xác nhận lại.
func TestApproveSetupCommandHumanOnlyAndShow(t *testing.T) {
	ws, _, id := taskRepo(t)
	ctx := context.Background()
	if _, err := ws.Tasks.Update(id, time.Now(), func(tk *task.Task) error {
		tk.State.Classification.Isolation, tk.State.Classification.IsolationReason = task.IsolationWorktree, task.IsolationReasonUser
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ws.Cfg.Workflow.Worktree.SetupCommand = "make deps"
	p, err := PlanTaskApprove(ctx, ws, TaskApproveOptions{Ref: id})
	if err != nil || p.SetupCommand != "make deps" || p.Empty() {
		t.Fatalf("kế hoạch: %+v %v", p, err)
	}
	if d, err := ShowTask(ctx, ws, TaskShowOptions{Ref: id}); err != nil || !d.Setup.Required || d.Setup.Confirmed || d.Setup.Command != "make deps" {
		t.Fatalf("show chưa xác nhận: %+v %v", d.Setup, err)
	}

	asAgent(t)
	if _, _, err := ApproveTask(ctx, ws, TaskApproveOptions{Ref: id}); !errors.Is(err, ErrAgentForbidden) {
		t.Fatalf("agent approve: %v", err)
	}
	if tk, _ := ws.Tasks.Load(id); tk.State.SetupApproval != nil {
		t.Fatalf("agent không được ghi SetupApproval: %+v", tk.State.SetupApproval)
	}

	asHuman(t)
	// Kế hoạch (người xem) mang băm của lệnh; thiếu băm thì approve không xác nhận lệnh setup.
	plan, err := PlanTaskApprove(ctx, ws, TaskApproveOptions{Ref: id})
	if err != nil || plan.SetupCommand != "make deps" || plan.SetupSHA != task.CommandSHA256("make deps") {
		t.Fatalf("kế hoạch: %+v %v", plan, err)
	}
	if _, a, err := ApproveTask(ctx, ws, TaskApproveOptions{Ref: id}); err != nil || a.SetupCommand != "" || a.SetupWaiting != "make deps" {
		t.Fatalf("approve không băm: %+v %v", a, err)
	}
	_, a, err := ApproveTask(ctx, ws, TaskApproveOptions{Ref: id, SetupSHA: plan.SetupSHA})
	if err != nil || a.SetupCommand != "make deps" {
		t.Fatalf("người approve: %+v %v", a, err)
	}
	tk, _ := ws.Tasks.Load(id)
	if sa := tk.State.SetupApproval; sa == nil || sa.SHA256 != task.CommandSHA256("make deps") || sa.By == "" {
		t.Fatalf("SetupApproval: %+v", sa)
	}
	if d, _ := ShowTask(ctx, ws, TaskShowOptions{Ref: id}); !d.Setup.Confirmed || d.Setup.Pending() {
		t.Fatalf("show sau xác nhận: %+v", d.Setup)
	}
	if p, _ := PlanTaskApprove(ctx, ws, TaskApproveOptions{Ref: id}); p.SetupCommand != "" {
		t.Fatalf("đã xác nhận thì kế hoạch không còn lệnh setup: %+v", p)
	}
	// Đổi lệnh: băm khác ⇒ chờ xác nhận lại.
	ws.Cfg.Workflow.Worktree.SetupCommand = "make deps && make gen"
	if d, _ := ShowTask(ctx, ws, TaskShowOptions{Ref: id}); d.Setup.Confirmed || !d.Setup.Pending() {
		t.Fatalf("lệnh đổi phải chờ xác nhận lại: %+v", d.Setup)
	}
}

// bint doctor nêu lệnh setup worktree (kèm băm) và liệt kê tác vụ worktree còn chờ người xác nhận.
func TestDoctorReportsSetupCommand(t *testing.T) {
	_, root, id := taskRepo(t)
	if err := os.WriteFile(filepath.Join(root, ".bint", "local.yml"), []byte("workflow:\n  worktree:\n    setup_command: make deps\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := OpenTasks(context.Background(), root, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Tasks.Update(id, time.Now(), func(tk *task.Task) error {
		tk.State.Classification.Isolation, tk.State.Classification.IsolationReason = task.IsolationWorktree, task.IsolationReasonUser
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	find := func(name string) *Check {
		rep := Doctor(context.Background(), root)
		for i := range rep.Checks {
			if rep.Checks[i].Name == name {
				return &rep.Checks[i]
			}
		}
		return nil
	}
	c := find("lệnh setup worktree")
	if c == nil || !strings.Contains(c.Detail, "make deps") || !strings.Contains(c.Detail, task.CommandSHA256("make deps")[:12]) {
		t.Fatalf("thiếu mục lệnh setup: %+v", c)
	}
	if w := find("lệnh setup chờ xác nhận"); w == nil || w.Level != LevelWarn || !strings.Contains(w.Detail, id) {
		t.Fatalf("thiếu cảnh báo chờ xác nhận: %+v", w)
	}
	asHuman(t)
	ws2, err := OpenTasks(context.Background(), root, "test") // nạp lại để thấy lớp local.yml
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ApproveTask(context.Background(), ws2, TaskApproveOptions{Ref: id, SetupSHA: task.CommandSHA256("make deps")}); err != nil {
		t.Fatal(err)
	}
	if w := find("lệnh setup chờ xác nhận"); w != nil {
		t.Fatalf("đã xác nhận thì không còn cảnh báo: %+v", w)
	}
}

// Review .2 (01M785XQ…): security.require_approval_for_commands tắt thì doctor không nói "cần người xác nhận" mà cảnh báo
// lệnh chạy không cần xác nhận.
func TestDoctorSetupCommandApprovalOff(t *testing.T) {
	_, root, _ := taskRepo(t)
	if err := os.WriteFile(filepath.Join(root, ".bint", "local.yml"), []byte("workflow:\n  worktree:\n    setup_command: make deps\nsecurity:\n  require_approval_for_commands: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range Doctor(context.Background(), root).Checks {
		if c.Name == "lệnh setup worktree" {
			if c.Level != LevelWarn || strings.Contains(c.Detail, "cần người xác nhận cho từng") || !strings.Contains(c.Detail, "KHÔNG cần người xác nhận") {
				t.Fatalf("doctor khi tắt xác nhận: %+v", c)
			}
			return
		}
	}
	t.Fatal("thiếu mục lệnh setup worktree")
}
