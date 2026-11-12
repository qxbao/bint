package theme

import "testing"

func TestNoColorStripsOnlyColors(t *testing.T) {
	mono, dark := New(Mono), New(Dark)
	cases := []struct{ in, want string }{
		{"\x1b[31mđỏ\x1b[0m", "đỏ\x1b[0m"},
		{"\x1b[1;31mđậm đỏ\x1b[22;39m", "\x1b[1mđậm đỏ\x1b[22;39m"},
		{"\x1b[38;5;196mx\x1b[0m", "x\x1b[0m"},
		{"\x1b[38;2;10;20;30;1mx\x1b[0m", "\x1b[1mx\x1b[0m"},
		{"\x1b[48;2;1;2;3m\x1b[7mx\x1b[27m", "\x1b[7mx\x1b[27m"},
		{"\x1b[92mx\x1b[107m", "x"},
		{"\x1b[1;31mdiff \x1b[m plain \x1b[32m+add\x1b[0m", "\x1b[1mdiff \x1b[m plain +add\x1b[0m"}, // ESC[m (đặt lại) phải được giữ
		{"không có mã", "không có mã"},
	}
	for _, c := range cases {
		if got := mono.NoColor(c.in); got != c.want {
			t.Errorf("Mono NoColor(%q) = %q, muốn %q", c.in, got, c.want)
		}
		if got := dark.NoColor(c.in); got != c.in {
			t.Errorf("Dark NoColor(%q) phải giữ nguyên: %q", c.in, got)
		}
	}
}
