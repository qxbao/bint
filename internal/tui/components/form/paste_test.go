package form

import "testing"

type pasteOpts struct {
	Name  string `flag:"name"`
	Force bool   `flag:"force"`
}

func TestPasteIntoStringFieldOnly(t *testing.T) {
	m := New("t.paste", "t paste", &pasteOpts{})
	if !m.Paste("esc") {
		t.Fatal("ô String phải nhận chữ dán")
	}
	if got := m.inputs[0].Text; got != "esc" {
		t.Fatalf("Text = %q", got)
	}
	m.FieldErr[0] = "required"
	m.Paste("x")
	if _, bad := m.FieldErr[0]; bad {
		t.Fatal("dán phải xoá lỗi của ô")
	}
	m.Focus = 1 // ô Bool
	if m.Paste("zzz") {
		t.Fatal("ô Bool không nhận chữ dán")
	}
}
