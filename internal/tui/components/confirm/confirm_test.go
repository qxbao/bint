package confirm

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
)

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	}
	r := []rune(s)
	return tea.KeyPressMsg{Code: r[0], Text: s}
}

func TestConfirmYesNo(t *testing.T) {
	m := Model{Title: "Duyệt?", Body: []string{"a"}, YesLabel: "Đồng ý", NoLabel: "Huỷ"}
	if r := m.Update(key("x")); r != Pending {
		t.Fatal("phím lạ phải bị bỏ qua")
	}
	if m.Update(key("enter")) != Yes || m.Update(key("y")) != Yes {
		t.Fatal("Enter/y = đồng ý")
	}
	if m.Update(key("esc")) != No || m.Update(key("n")) != No {
		t.Fatal("Esc/n = từ chối")
	}
}

func TestConfirmRequiredReason(t *testing.T) {
	m := Model{Title: "Từ chối", Input: true, Label: "Lý do", Required: true, RequiredMsg: "Cần nhập lý do.", YesLabel: "OK", NoLabel: "Huỷ"}
	if m.Update(key("enter")) != Pending {
		t.Fatal("thiếu lý do thì Enter không xác nhận")
	}
	if out := strings.Join(m.View(theme.New(theme.Mono), 60, 12), "\n"); !strings.Contains(out, "Cần nhập lý do.") {
		t.Fatalf("thiếu thông báo bắt buộc:\n%s", out)
	}
	for _, c := range "sai rồi" {
		m.Update(key(string(c)))
	}
	if m.Value() != "sai rồi" {
		t.Fatalf("%q", m.Value())
	}
	if m.Update(key("y")) != Pending { // trong ô nhập, y là chữ chứ không phải đồng ý
		t.Fatal("y vào ô nhập")
	}
	if m.Update(key("enter")) != Yes || m.Value() != "sai rồiy" {
		t.Fatalf("%q", m.Value())
	}
}

func TestConfirmViewFitsAndScrolls(t *testing.T) {
	var body []string
	for i := 0; i < 40; i++ {
		body = append(body, "mục số "+strings.Repeat("Ắ", 5)+" rất dài "+strings.Repeat("ề", 80))
	}
	m := Model{Title: "Duyệt 40 mục?", Body: body, YesLabel: "Đồng ý", NoLabel: "Huỷ", Danger: true}
	for _, w := range []int{40, 84} {
		lines := m.View(theme.New(theme.Mono), w, 14)
		if len(lines) > 14 {
			t.Fatalf("cao %d > 14", len(lines))
		}
		for _, l := range lines {
			if text.Width(l) != w {
				t.Fatalf("dòng rộng %d, muốn %d: %q", text.Width(l), w, l)
			}
		}
	}
	m.View(theme.New(theme.Mono), 60, 14)
	m.Update(key("j"))
	m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if m.Scroll.Top == 0 {
		t.Fatal("phải cuộn được nội dung dài")
	}
}

// Ô chọn (hộp cảnh báo): mặc định không chọn, Space/x đảo ô đang trỏ, Tab dời ô và ô nhập, y/n không xác nhận.
func TestConfirmChecks(t *testing.T) {
	m := Model{Title: "Áp dụng", Danger: true, Checks: []string{"Bỏ qua kiểm chứng", "Chấp nhận rủi ro"}, Input: true, Label: "Ghi chú",
		YesLabel: "Áp dụng", NoLabel: "Huỷ"}
	if m.Checked(0) || m.Checked(1) {
		t.Fatal("mặc định không chọn")
	}
	if m.Update(key("y")) != Pending || m.Update(key("n")) != Pending {
		t.Fatal("có ô chọn thì y/n không xác nhận hay từ chối")
	}
	m.Update(key("x"))
	if !m.Checked(0) || m.Checked(1) {
		t.Fatalf("x đảo ô đầu: %v", m.On)
	}
	m.Update(key("tab"))
	m.Update(key(" "))
	if !m.Checked(1) {
		t.Fatalf("Space đảo ô thứ hai: %v", m.On)
	}
	m.Update(key("x")) // đảo lại ô hai
	if m.Checked(1) {
		t.Fatal("x đảo lại")
	}
	m.Update(key("tab")) // tới ô nhập: chữ vào ô, không đảo ô chọn
	m.Update(key("x"))
	if m.Value() != "x" || m.Checked(0) != true {
		t.Fatalf("ô nhập nhận chữ: %q %v", m.Value(), m.On)
	}
	out := strings.Join(m.View(theme.New(theme.Mono), 60, 20), "\n")
	if !strings.Contains(out, "[x] Bỏ qua kiểm chứng") || !strings.Contains(out, "[ ] Chấp nhận rủi ro") {
		t.Fatalf("view:\n%s", out)
	}
	if m.Update(key("enter")) != Yes || m.Update(key("esc")) != No {
		t.Fatal("Enter/Esc")
	}
}
