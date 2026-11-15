package input

import "testing"

func TestSanitize(t *testing.T) {
	cases := []struct {
		in    string
		multi bool
		want  string
	}{
		{"a\r\nb\rc\nd", false, "a b c d"},
		{"a\r\nb\rc\nd", true, "a\nb\nc\nd"},
		{"x\x1b[0my\x7fz\x00", false, "x[0myz"},
		{"tab\there", false, "tab here"},
		{"tab\there", true, "tab\there"},
		{"ngôn ngữ", false, "ngôn ngữ"},
	}
	for _, c := range cases {
		if got := Sanitize(c.in, c.multi); got != c.want {
			t.Errorf("Sanitize(%q, %v) = %q, muốn %q", c.in, c.multi, got, c.want)
		}
	}
}

func TestPasteInsertsAtCursor(t *testing.T) {
	m := New("héllo")
	m.Cur = 2
	if !m.Paste("ñ\nx") {
		t.Fatal("Paste trả false")
	}
	if m.Text != "héñ xllo" || m.Cur != 5 {
		t.Fatalf("Text=%q Cur=%d", m.Text, m.Cur)
	}
	ml := Model{Multiline: true}
	ml.Paste("a\nb")
	if ml.Text != "a\nb" {
		t.Fatalf("multiline: %q", ml.Text)
	}
	if ml.Paste("\x1b") {
		t.Fatal("chuỗi chỉ có ký tự điều khiển không được chèn")
	}
}
