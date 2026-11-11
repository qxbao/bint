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
