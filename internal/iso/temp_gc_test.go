package iso

import "testing"

func TestParseStatStartTicks(t *testing.T) {
	// Trường 22 = starttime; tên lệnh chứa khoảng trắng và ngoặc không được làm lệch cột.
	stat := "123 (co dex) (x)) S 1 123 123 0 -1 4194560 100 0 0 0 5 3 0 0 20 0 7 0 84292861 1000 100 18446744073709551615"
	if v, ok := parseStatStartTicks(stat); !ok || v != 84292861 {
		t.Fatalf("v=%d ok=%v", v, ok)
	}
	for _, bad := range []string{"", "123 (x) S 1", "no paren"} {
		if _, ok := parseStatStartTicks(bad); ok {
			t.Errorf("%q phải không parse được", bad)
		}
	}
}
