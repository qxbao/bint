package app

import (
	"os"
	"strings"
	"testing"

	"bint/internal/config"
	"bint/internal/tui/actions"
)

// 6a.1 (01M75YH2JGR9AD685VH886WE33.5): người khác thêm khoá sau khi mở, người dùng sửa khoá khác; Sửa lại gộp ba chiều
// nên sau khi xác nhận tệp có CẢ HAI thay đổi (không hoàn tác thay đổi của người khác), và diff chỉ còn phần của người dùng.
func TestSystemConfigEditRebaseKeepsOthersChange(t *testing.T) {
	m, root, execs := sysReal(t)
	proj := config.ProjectPath(root)
	orig, _ := os.ReadFile(proj)
	press(t, m, "e", "ctrl+s")
	st := (*execs)[0].Opts.(actions.ConfigEditState)
	other := string(orig) + "# khoá của người khác\n"
	if err := os.WriteFile(proj, []byte(other), 0o644); err != nil {
		t.Fatal(err)
	}
	mine := strings.Replace(string(orig), "language: auto", "language: en", 1)
	editWith(t, m, (*execs)[0], mine)
	press(t, m, "enter") // xác nhận ghi → bị từ chối vì tệp gốc đổi
	hasAll(t, plain(m.Render()), "đã bị đổi", "Sửa lại")
	press(t, m, "enter") // Sửa lại: gộp rồi mở trình soạn thảo
	if len(*execs) != 2 {
		t.Fatalf("sửa lại phải mở trình soạn thảo: %d", len(*execs))
	}
	temp, _ := os.ReadFile(st.Edit.TempPath)
	want := strings.Replace(other, "language: auto", "language: en", 1)
	if string(temp) != want {
		t.Fatalf("bản tạm phải mang cả hai thay đổi:\n%s", temp)
	}
	// Người dùng thoát trình soạn thảo mà không sửa thêm: diff theo tệp gốc MỚI chỉ còn thay đổi của người dùng.
	_, cmd := m.Update(actions.ResultMsg{ID: "system.open_file", Opts: (*execs)[1].Opts})
	drive(t, m, cmd)
	out := plain(m.Render())
	hasAll(t, out, "Ghi cấu hình?", "+  language: en")
	if strings.Contains(out, "-# khoá của người khác") {
		t.Fatalf("diff không được xoá dòng của người khác:\n%s", out)
	}
	press(t, m, "enter")
	if b, _ := os.ReadFile(proj); string(b) != want {
		t.Fatalf("tệp phải có cả hai thay đổi:\n%s", b)
	}
	if _, err := os.Stat(st.Edit.TempPath); err == nil {
		t.Fatal("ghi thành công thì bản tạm phải bị xoá")
	}
}

// Xung đột: bản tạm mang dấu, trình soạn thảo mở lại; để nguyên dấu thì kiểm từ chối; gộp tay xong thì ghi được.
func TestSystemConfigEditRebaseConflictMarkers(t *testing.T) {
	m, root, execs := sysReal(t)
	proj := config.ProjectPath(root)
	orig, _ := os.ReadFile(proj)
	press(t, m, "e", "ctrl+s")
	st := (*execs)[0].Opts.(actions.ConfigEditState)
	other := strings.Replace(string(orig), "language: auto", "language: vi", 1)
	os.WriteFile(proj, []byte(other), 0o644)
	editWith(t, m, (*execs)[0], strings.Replace(string(orig), "language: auto", "language: en", 1))
	press(t, m, "enter", "enter") // ghi bị từ chối → Sửa lại
	if len(*execs) != 2 {
		t.Fatalf("phải mở lại trình soạn thảo: %d", len(*execs))
	}
	temp, _ := os.ReadFile(st.Edit.TempPath)
	if !strings.Contains(string(temp), "<<<<<<< ") || !strings.Contains(string(temp), ">>>>>>> ") {
		t.Fatalf("bản tạm phải mang dấu xung đột:\n%s", temp)
	}
	hasAll(t, plain(m.Render()), "xung đột")
	_, cmd := m.Update(actions.ResultMsg{ID: "system.open_file", Opts: (*execs)[1].Opts}) // thoát mà chưa gỡ dấu
	drive(t, m, cmd)
	hasAll(t, plain(m.Render()), "Cấu hình không hợp lệ", "xung đột")
	if b, _ := os.ReadFile(proj); string(b) != other {
		t.Fatal("tệp gốc không được đổi")
	}
	// Sửa lại lần nữa, gộp tay (giữ en) rồi ghi.
	press(t, m, "enter")
	resolved := strings.Replace(other, "language: vi", "language: en", 1)
	os.WriteFile(st.Edit.TempPath, []byte(resolved), 0o644)
	_, cmd = m.Update(actions.ResultMsg{ID: "system.open_file", Opts: (*execs)[2].Opts})
	drive(t, m, cmd)
	hasAll(t, plain(m.Render()), "Ghi cấu hình?")
	press(t, m, "enter")
	if b, _ := os.ReadFile(proj); string(b) != resolved {
		t.Fatalf("ghi sau khi gộp tay:\n%s", b)
	}
}

// rebaseErrorSetup dựng ca gộp lỗi: người dùng sửa khoá khác, tệp gốc đổi rồi thành thư mục (đặt lại mốc không đọc được);
// trả về mô hình đang ở hộp Sửa lại/Bỏ mở lại kèm lỗi.
func rebaseErrorSetup(t *testing.T) (m *Model, execs *[]actions.ExecMsg, st actions.ConfigEditState, proj, mine, other string) {
	t.Helper()
	m, root, execs := sysReal(t)
	proj = config.ProjectPath(root)
	orig, _ := os.ReadFile(proj)
	press(t, m, "e", "ctrl+s")
	st = (*execs)[0].Opts.(actions.ConfigEditState)
	other = string(orig) + "# khoá của người khác\n"
	os.WriteFile(proj, []byte(other), 0o644)
	mine = strings.Replace(string(orig), "language: auto", "language: en", 1)
	editWith(t, m, (*execs)[0], mine)
	press(t, m, "enter") // bị từ chối vì tệp gốc đổi
	os.Remove(proj)
	if err := os.Mkdir(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	press(t, m, "enter") // Sửa lại → gộp lỗi
	if len(*execs) != 1 {
		t.Fatalf("gộp lỗi thì không mở trình soạn thảo: %d", len(*execs))
	}
	hasAll(t, plain(m.Render()), "Sửa lại", "Bỏ")
	if b, err := os.ReadFile(st.Edit.TempPath); err != nil || string(b) != mine {
		t.Fatalf("bản tạm phải còn nguyên sau lỗi gộp: %v %q", err, b)
	}
	return m, execs, st, proj, mine, other
}

// 01M75YH2JGR9AD685VH886WE33.3: gộp lỗi (không đọc được tệp gốc) thì bản tạm còn nguyên, hộp Sửa lại/Bỏ mở lại kèm lỗi;
// thử lại được khi hết lỗi.
func TestSystemConfigEditRebaseErrorKeepsTempAndRetries(t *testing.T) {
	m, execs, st, proj, mine, other := rebaseErrorSetup(t)
	os.Remove(proj)
	os.WriteFile(proj, []byte(other), 0o644)
	press(t, m, "enter") // Sửa lại: hết lỗi, gộp được
	if len(*execs) != 2 {
		t.Fatalf("thử lại phải mở trình soạn thảo: %d", len(*execs))
	}
	if b, err := os.ReadFile(st.Edit.TempPath); err != nil || !strings.Contains(string(b), "language: en") || !strings.Contains(string(b), "khoá của người khác") {
		t.Fatalf("bản tạm sau khi gộp: %v\n%s", err, b)
	}
	_ = mine
}

// Review 6a .2: nhấn Bỏ ở hộp Sửa lại/Bỏ ngay sau lỗi gộp thì mới xoá bản tạm (trước đó nó còn nguyên).
func TestSystemConfigEditRebaseErrorDiscardDeletesTemp(t *testing.T) {
	m, execs, st, _, _, _ := rebaseErrorSetup(t)
	press(t, m, "esc") // Bỏ
	if _, err := os.Stat(st.Edit.TempPath); err == nil {
		t.Fatal("Bỏ phải xoá bản tạm")
	}
	if len(*execs) != 1 {
		t.Fatalf("Bỏ không được mở lại trình soạn thảo: %d", len(*execs))
	}
	if strings.Contains(plain(m.Render()), "Sửa lại") {
		t.Fatalf("hộp Sửa lại/Bỏ phải đóng sau Bỏ:\n%s", plain(m.Render()))
	}
}
