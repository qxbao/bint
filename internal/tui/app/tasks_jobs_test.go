package app

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"bint/internal/task"
	"bint/internal/tui/actions"
	"bint/internal/workflow"
)

// startRun mở form chạy (r) trên tác vụ đang chọn rồi gửi (Ctrl+S): việc nền bắt đầu và khung chi tiết chuyển sang
// thẻ Log.
func (f *fixture) startRun() {
	f.t.Helper()
	f.press("r", "ctrl+s")
}

// protectedFeat: b-feat sửa một tệp chạm đường dẫn bảo vệ (migrations/**), nên Runner hỏi người duyệt.
func (f *fixture) protectedFeat() {
	base := f.happy
	f.behave = func(ctx context.Context, step string, e *workflow.Env, id, log string) error {
		if step == workflow.StepFeat {
			f.write("migrations/002.sql", "ALTER TABLE x;\n")
		}
		return base(ctx, step, e, id, log)
	}
}

// Confirm trả "không" thì không ghi duyệt nào: không có ProtectedApproval mới, tệp chờ duyệt vào PendingPaths và tác
// vụ blocked; trả "có" thì ghi duyệt và chạy tiếp tới done/blocked theo bằng chứng.
func TestConfirmNoWritesNoApproval(t *testing.T) {
	for _, yes := range []bool{false, true} {
		f := newFixture(t, Options{})
		f.protectedFeat()
		id := f.newTask("Đổi app.txt", func(o *bintappTaskNew) { o.NoPlan = true; o.VerifyCommand = "git --version" })
		f.selectTask(id)
		f.startRun()
		f.pump(func() bool { return f.m.prompt != nil }, nil)
		out := plain(f.m.Render())
		for _, want := range []string{"Duyệt thay đổi chạm vùng bảo vệ?", "migrations/002.sql", "Duyệt", "Không"} {
			if !strings.Contains(out, want) {
				t.Errorf("hộp thoại thiếu %q:\n%s", want, out)
			}
		}
		if f.m.prompt == nil || !f.m.prompt.dlg.Danger {
			t.Fatal("duyệt vùng bảo vệ phải là hộp đỏ")
		}
		if yes {
			f.press("enter")
		} else {
			f.press("esc")
		}
		f.waitJobsDone()
		tk := f.task(id)
		if yes {
			if tk.State.ProtectedApproval == nil || len(tk.State.PendingPaths) != 0 {
				t.Fatalf("trả có: phải ghi duyệt: %+v", tk.State)
			}
			continue
		}
		if tk.State.ProtectedApproval != nil || tk.State.VerifyApproval != nil || tk.State.Status != task.StatusBlocked ||
			len(tk.State.PendingPaths) != 1 || tk.State.PendingPaths[0] != "migrations/002.sql" {
			t.Fatalf("trả không: không được có duyệt mới: status=%s %+v", tk.State.Status, tk.State)
		}
	}
}

// Huỷ việc khi câu hỏi còn chờ: hàm hỏi trả "không" ngay, việc kết thúc trong hạn giờ và bảng việc nền rỗng (không
// treo goroutine); tác vụ không được duyệt gì. Chạy với `go test -race`.
func TestCancelWhilePromptPendingFinishesInTime(t *testing.T) {
	f := newFixture(t, Options{})
	f.protectedFeat()
	id := f.newTask("Đổi app.txt", func(o *bintappTaskNew) { o.NoPlan = true })
	f.selectTask(id)
	f.startRun()
	f.pump(func() bool { return f.m.prompt != nil }, nil)
	start := time.Now()
	_, cmd := f.m.Update(actions.CancelMsg{ID: "tasks.run"})
	if f.m.prompt != nil {
		t.Fatal("huỷ phải đóng câu hỏi đang chờ")
	}
	f.pump(func() bool { return f.m.jobs.Len() == 0 }, cmd)
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("việc kết thúc quá chậm sau khi huỷ: %v", d)
	}
	if tk := f.task(id); tk.State.ProtectedApproval != nil {
		t.Fatalf("huỷ không được duyệt gì: %+v", tk.State.ProtectedApproval)
	}
}

// Log trực tiếp: dòng agent ghi dần vào log của bước hiện trong View trước khi bước kết thúc, cùng dòng của
// orchestrator (Runner.Logf); thẻ Log tự mở khi bắt đầu việc.
func TestLiveLogVisibleBeforeStepEnds(t *testing.T) {
	f := newFixture(t, Options{})
	release := make(chan struct{})
	var wrote atomic.Int32
	f.behave = func(_ context.Context, step string, e *workflow.Env, id, log string) error {
		if step != workflow.StepFeat {
			return f.happy(context.Background(), step, e, id, log)
		}
		w := newLogWriter(t, log)
		w.line("dòng agent số một")
		wrote.Add(1)
		w.line("dòng agent số hai")
		wrote.Add(1)
		<-release
		w.line("dòng agent cuối")
		f.write("app.txt", "v2\n")
		return nil
	}
	id := f.newTask("Đổi app.txt", func(o *bintappTaskNew) { o.NoPlan = true })
	f.selectTask(id)
	f.startRun()
	f.pump(func() bool { return strings.Contains(plain(f.m.Render()), "dòng agent số hai") }, nil)
	out := plain(f.m.Render())
	if f.job() == nil || wrote.Load() != 2 {
		t.Fatalf("bước phải còn đang chạy khi đã thấy dòng mới: job=%v", f.job())
	}
	for _, want := range []string{"dòng agent số một", "Bước b-feat", "▶ b-feat", "Đang chạy"} {
		if !strings.Contains(out, want) {
			t.Errorf("View thiếu %q khi bước còn chạy:\n%s", want, out)
		}
	}
	if strings.Contains(out, "dòng agent cuối") {
		t.Fatal("dòng chưa ghi không được hiện")
	}
	close(release)
	f.pump(func() bool { return strings.Contains(plain(f.m.Render()), "dòng agent cuối") || f.m.jobs.Len() == 0 }, nil)
	f.waitJobsDone()
	// Xong: thông báo ở mọi màn hình, log vẫn xem lại được ở thẻ Log.
	if !strings.Contains(f.m.toast, "Đã xong") && !strings.Contains(f.m.toast, "cần bạn") {
		t.Fatalf("thiếu thông báo khi xong: %q", f.m.toast)
	}
	if out := plain(f.m.Render()); !strings.Contains(out, "Đã xong:") {
		t.Fatalf("log việc đã xong phải xem lại được:\n%s", out)
	}
}

// Esc ở thẻ Log huỷ việc an toàn: agent bị dừng, tác vụ chuyển interrupted và tiếp tục được (bint run resume).
func TestEscOnLogCancelsJobAndInterruptsTask(t *testing.T) {
	f := newFixture(t, Options{})
	started := make(chan struct{})
	f.behave = func(ctx context.Context, step string, e *workflow.Env, id, log string) error {
		if step == workflow.StepFeat {
			close(started)
			<-ctx.Done() // như tiến trình agent bị giết
			return nil
		}
		return f.happy(ctx, step, e, id, log)
	}
	id := f.newTask("Đổi app.txt", func(o *bintappTaskNew) { o.NoPlan = true })
	f.selectTask(id)
	f.startRun()
	f.pump(func() bool {
		select {
		case <-started:
			return true
		default:
			return false
		}
	}, nil)
	if !f.m.tk.WantsInterrupt() {
		t.Fatal("ở thẻ Log của việc đang chạy, Esc phải huỷ việc")
	}
	f.press("esc")
	f.waitJobsDone()
	tk := f.task(id)
	if tk.State.Status != task.StatusInterrupted {
		t.Fatalf("huỷ phải đưa tác vụ về interrupted: %s (%s)", tk.State.Status, tk.State.Reason)
	}
	if !strings.Contains(plain(f.m.Render()), "phím c ở Tác vụ") && !strings.Contains(f.m.toast, "interrupted") {
		t.Logf("toast: %q", f.m.toast)
	}
}

// Hộp thoại thoát khi còn việc workflow: liệt kê việc kèm lệnh tiếp tục; "Ở lại" không thoát; "Huỷ việc rồi thoát"
// huỷ context và chỉ tea.Quit sau khi việc báo xong.
func TestExitDialogWithRunningWorkflow(t *testing.T) {
	f := newFixture(t, Options{})
	started := make(chan struct{})
	f.behave = func(ctx context.Context, step string, e *workflow.Env, id, log string) error {
		if step == workflow.StepFeat {
			close(started)
			<-ctx.Done()
			return nil
		}
		return f.happy(ctx, step, e, id, log)
	}
	id := f.newTask("Đổi app.txt", func(o *bintappTaskNew) { o.NoPlan = true })
	f.selectTask(id)
	f.startRun()
	f.pump(func() bool {
		select {
		case <-started:
			return true
		default:
			return false
		}
	}, nil)
	f.press("1") // sang Tổng quan: thanh trên vẫn báo việc nền
	if out := plain(f.m.Render()); !strings.Contains(out, "1 việc nền") {
		t.Fatalf("thanh trên phải báo việc nền ở mọi màn hình:\n%s", out)
	}
	f.press("q")
	out := plain(f.m.Render())
	// .6/.11: hộp thoát hiện nguyên lệnh CLI tiếp tục kèm MÃ TÁC VỤ ĐẦY ĐỦ (người dùng đang rời TUI), không phải tham chiếu phím TUI.
	wantResume := "bint run resume " + id
	if js := f.m.jobs.All(); len(js) != 1 || js[0].Resume != wantResume {
		t.Fatalf("việc nền phải mang Resume %q, có %+v", wantResume, js)
	}
	if strings.Contains(out, "phím c ở Tác vụ") {
		t.Errorf("hộp thoát không được viết lại lệnh tiếp tục thành phím TUI:\n%s", out)
	}
	for _, want := range []string{"Còn việc đang chạy", "b-do · ", "Tiếp tục sau bằng: " + wantResume, "Huỷ việc rồi thoát", "Ở lại"} {
		if !strings.Contains(out, want) {
			t.Errorf("hộp thoại thoát thiếu %q:\n%s", want, out)
		}
	}
	f.press("esc") // Ở lại
	if f.m.quitting || f.m.exiting || f.m.jobs.Len() != 1 {
		t.Fatalf("Ở lại không được thoát hay huỷ việc: quit=%v exiting=%v jobs=%d", f.m.quitting, f.m.exiting, f.m.jobs.Len())
	}
	f.press("q", "enter")
	if !f.m.exiting {
		t.Fatal("phải ở trạng thái đang thoát")
	}
	if f.m.quitting {
		// Việc có thể đã kết thúc nhanh sau khi huỷ; chấp nhận nhưng việc phải đã rời bảng.
		if f.m.jobs.Len() != 0 {
			t.Fatal("chỉ được thoát sau khi việc báo xong hoặc hết hạn")
		}
	} else {
		f.pump(func() bool { return f.m.quitting }, nil)
		if f.m.jobs.Len() != 0 {
			t.Fatalf("phải đợi việc kết thúc trước khi thoát: %d", f.m.jobs.Len())
		}
	}
	if tk := f.task(id); tk.State.Status != task.StatusInterrupted {
		t.Fatalf("huỷ khi thoát phải để tác vụ interrupted: %s", tk.State.Status)
	}
}

// Chỉ một việc workflow mỗi lúc: yêu cầu thứ hai báo lỗi "đã có việc workflow đang chạy" và không bắt đầu thêm.
func TestOnlyOneWorkflowJobAtATime(t *testing.T) {
	f := newFixture(t, Options{})
	started, release := make(chan struct{}), make(chan struct{})
	f.behave = func(ctx context.Context, step string, e *workflow.Env, id, log string) error {
		if step == workflow.StepFeat {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
		return f.happy(ctx, step, e, id, log)
	}
	id := f.newTask("Đổi app.txt", func(o *bintappTaskNew) { o.NoPlan = true })
	f.selectTask(id)
	f.startRun()
	f.pump(func() bool {
		select {
		case <-started:
			return true
		default:
			return false
		}
	}, nil)
	_, cmd := f.m.Update(actions.RequestMsg{ID: "tasks.resume", Opts: bintappResume(id)})
	f.settle(cmd)
	if f.m.jobs.Len() != 1 {
		t.Fatalf("không được bắt đầu việc workflow thứ hai: %d", f.m.jobs.Len())
	}
	close(release)
	f.waitJobsDone()
}
