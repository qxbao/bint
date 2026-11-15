package confirm

import "testing"

func TestPasteOnlyOnInput(t *testing.T) {
	m := Model{Input: true}
	if !m.Paste("lý do\ndài") || m.In.Text != "lý do dài" {
		t.Fatalf("Text = %q", m.In.Text)
	}
	n := Model{}
	if n.Paste("x") {
		t.Fatal("hộp không có ô nhập không nhận chữ dán")
	}
	c := Model{Input: true, Checks: []string{"a"}, On: []bool{false}}
	if c.Paste("x") {
		t.Fatal("tiêu điểm ở ô chọn không nhận chữ dán")
	}
}
