package workflow

import "testing"

// Báo đỏ sớm của kiểm nền: gọi đúng một lần ở dòng đỏ đầu tiên, kể cả khi dòng bị cắt giữa hai lần ghi.
func TestFirstFailWriter(t *testing.T) {
	n := 0
	f := &firstFail{on: func() { n++ }}
	for _, chunk := range []string{"ok  \tp/a\t0.1s\n=== RUN TestX\n", "--- FA", "IL: TestX (0.00s)\n", "FAIL\tp/b\t0.2s\n"} {
		f.Write([]byte(chunk))
	}
	if n != 1 {
		t.Errorf("on gọi %d lần, muốn 1", n)
	}
	g := &firstFail{on: func() { n++ }}
	g.Write([]byte("ok  \tp/a\t0.1s\nPASS\n"))
	if n != 1 {
		t.Errorf("log xanh mà vẫn báo đỏ")
	}
}
