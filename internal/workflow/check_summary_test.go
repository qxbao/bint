package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Tóm tắt đỏ của `bint task check`: nêu test đỏ kèm vài dòng lỗi, tách test hết giờ, trần cỡ; log lạ thì lấy phần cuối.
func TestRedSummary(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	got := redSummary(write("go.log", "$ sh -c \"go test ./a\"\n--- FAIL: TestA (0.00s)\n    a_test.go:9: muốn 2, có 1\n    a_test.go:10: thêm\nFAIL\nFAIL\texample.com/m/a\t0.1s\n"+
		"panic: test timed out after 10m0s\n\trunning tests:\n\t\tTestSlow (10m0s)\nFAIL\texample.com/m/b\t600.0s\n"))
	for _, want := range []string{"ĐỎ example.com/m/a TestA", "a_test.go:9: muốn 2, có 1", "hết giờ", "example.com/m/b TestSlow"} {
		if !strings.Contains(got, want) {
			t.Errorf("thiếu %q trong:\n%s", want, got)
		}
	}
	if strings.Contains(got, "ĐỎ example.com/m/b") {
		t.Errorf("test hết giờ bị tính là đỏ:\n%s", got)
	}
	var big strings.Builder
	for i := range 400 {
		big.WriteString("--- FAIL: TestMany" + strings.Repeat("x", i%7) + string(rune('A'+i%26)) + " (0.00s)\n    lỗi dài dòng ở đây\n")
	}
	big.WriteString("FAIL\tp\t1s\n")
	if s := redSummary(write("big.log", big.String())); len(s) > redSummaryMax+40 || !strings.Contains(s, "cắt bớt") {
		t.Errorf("không cắt theo trần: %d byte", len(s))
	}
	if s := redSummary(write("lạ.log", "Ran 3 tests\nFAILED (failures=1)\n")); !strings.Contains(s, "FAILED (failures=1)") {
		t.Errorf("log lạ phải lấy phần cuối: %q", s)
	}
	if redSummary(filepath.Join(dir, "không-có.log")) != "" {
		t.Error("không có log mà vẫn có tóm tắt")
	}
}
