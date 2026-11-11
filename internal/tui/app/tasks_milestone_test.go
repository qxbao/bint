package app

import (
	"strings"
	"testing"

	"bint/internal/task"
)

// Mốc xong của giai đoạn 2 (docs/tui-plan.md mục 9): từ TUI tạo tác vụ, chạy b-do, duyệt, xem review đến done, với
// agent giả (b-req ghi ba tài liệu hợp lệ, b-feat sửa app.txt, b-review ghi vòng không có phát hiện chặn).
//
// Kịch bản: tạo tác vụ bằng form (kèm lệnh kiểm chứng luôn trả 0) → chạy b-do → blocked vì lệnh kiểm chứng chưa được
// người xác nhận → `a` (hộp duyệt hiện đúng lệnh) → tiếp tục → thẻ Review hiện vòng của b-review → done.
func TestMilestoneTaskToDone(t *testing.T) {
	f := newFixture(t, Options{})
	f.press("3")
	f.pump(func() bool { return f.m.tk.Loaded }, nil)

	// 1. Tạo tác vụ bằng form: mô tả → lệnh kiểm chứng → (criteria, allow, workflow) → bật "Luôn qua b-req".
	f.press("n")
	for _, c := range "Đổi app.txt thành v2" {
		f.press(string(c))
	}
	f.press("tab")
	for _, c := range "git --version" {
		f.press(string(c))
	}
	f.press("tab", "tab", "tab", "tab", " ") // criteria, allow, workflow, plan → bật
	if out := plain(f.m.Render()); !strings.Contains(out, "bint task new") || !strings.Contains(out, "--verify-command") || !strings.Contains(out, "--plan") {
		t.Fatalf("dòng lệnh tương đương phải khớp cờ:\n%s", out)
	}
	f.press("ctrl+s")
	f.pump(func() bool { return f.m.tk.Cursor() != nil && !f.m.tk.Capturing() }, nil)
	cur := f.m.tk.Cursor()
	if cur == nil || cur.State.Status != task.StatusCreated || cur.Ticket.Verify.Command != "git --version" {
		t.Fatalf("tác vụ vừa tạo phải được chọn: %+v", cur)
	}
	id := cur.State.ID

	// 2. Chạy b-do (form chạy điền sẵn tác vụ).
	f.startRun()
	f.waitJobsDone()
	tk := f.task(id)
	if tk.State.Status != task.StatusBlocked || !strings.Contains(tk.State.Reason, "xác nhận") {
		t.Fatalf("phải blocked vì lệnh kiểm chứng chưa được xác nhận: %s — %s", tk.State.Status, tk.State.Reason)
	}
	if strings.Join(f.calls, ",") != "b-req,b-feat,b-review" {
		t.Fatalf("các bước đã chạy: %v", f.calls)
	}
	if tk.State.VerifyApproval != nil {
		t.Fatal("chưa duyệt thì chưa có VerifyApproval")
	}
	if !strings.Contains(f.m.toast, "cần bạn") {
		t.Fatalf("thông báo khi xong phải nói tác vụ cần người: %q", f.m.toast)
	}
	// Tác vụ chờ người nằm đầu danh sách ở nhóm "Chờ bạn".
	f.press("esc")
	f.pump(func() bool { c := f.m.tk.Cursor(); return c != nil && c.State.Status == task.StatusBlocked }, nil)
	if out := plain(f.m.Render()); !strings.Contains(out, "Chờ bạn (1)") {
		t.Fatalf("nhóm Chờ bạn:\n%s", out)
	}

	// 3. Duyệt: hộp xác nhận dựng từ kế hoạch chỉ đọc, nêu đúng lệnh kiểm chứng; chưa ghi gì tới khi đồng ý.
	f.press("a")
	f.pump(func() bool { return f.m.tk.Capturing() }, nil)
	out := plain(f.m.Render())
	for _, want := range []string{"Duyệt tác vụ", "git --version", "Sẽ xác nhận lệnh kiểm chứng"} {
		if !strings.Contains(out, want) {
			t.Errorf("hộp duyệt thiếu %q:\n%s", want, out)
		}
	}
	if f.task(id).State.VerifyApproval != nil {
		t.Fatal("xem kế hoạch duyệt không được ghi gì")
	}
	f.press("enter")
	f.pump(func() bool { return f.task(id).State.VerifyApproval != nil }, nil)
	if va := f.task(id).State.VerifyApproval; va.Command != "git --version" || !strings.Contains(va.By, "Rev") {
		t.Fatalf("VerifyApproval: %+v", va)
	}

	// 4. Chạy kiểm chứng (v, việc nền): lệnh đã được người xác nhận nên được phép chạy và qua trên đúng trạng thái mã.
	f.press("v")
	f.waitJobsDone()
	if v := f.task(id).State.LastVerification(); v == nil || !v.Passed || !v.Confirmed {
		t.Fatalf("kiểm chứng đã xác nhận phải qua: %+v", v)
	}
	if !strings.Contains(f.m.toast, "Kiểm chứng") && !strings.Contains(f.m.toast, "verify") {
		t.Fatalf("thông báo khi xong việc kiểm chứng: %q", f.m.toast)
	}

	// 5. Tiếp tục (c): b-review chạy lại một vòng rồi tác vụ tới done nhờ bằng chứng.
	f.press("esc", "c")
	if out := plain(f.m.Render()); !strings.Contains(out, "bint run resume "+id) {
		t.Fatalf("form tiếp tục phải hiện dòng lệnh tương đương:\n%s", out)
	}
	f.press("ctrl+s")
	f.waitJobsDone()
	tk = f.task(id)
	if tk.State.Status != task.StatusDone {
		t.Fatalf("phải done sau khi duyệt và tiếp tục: %s — %s", tk.State.Status, tk.State.Reason)
	}
	if strings.Join(f.calls, ",") != "b-req,b-feat,b-review,b-review" {
		t.Fatalf("tiếp tục không được chạy lại b-feat, chỉ review lại: %v", f.calls)
	}

	// 6. Xem review: thẻ Review hiện vòng của b-review kèm phát hiện của nó.
	f.press("A") // hiện cả xong/huỷ
	f.pump(func() bool {
		c := f.m.tk.Cursor()
		return c != nil && c.State.ID == id && c.State.Status == task.StatusDone
	}, f.m.tk.Focus(id))
	f.press("esc", ".", ".", ".", ".")
	f.pump(func() bool { return strings.Contains(plain(f.m.Render()), "Vòng 2/2") }, nil)
	out = plain(f.m.Render())
	for _, want := range []string{"Thay đổi nhỏ, đúng yêu cầu", "Đặt tên rõ hơn", "app.txt:1", "Chi tiết · "} {
		if !strings.Contains(out, want) {
			t.Errorf("thẻ Review thiếu %q:\n%s", want, out)
		}
	}
	if !strings.Contains(plain(f.m.Render()), "Đã xong (1)") {
		t.Errorf("nhóm Đã xong:\n%s", plain(f.m.Render()))
	}
	if tk.State.ReviewRounds != 2 {
		t.Fatalf("số vòng review: %d", tk.State.ReviewRounds)
	}
}
