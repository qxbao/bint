package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

// goalRunOnly (goalfreeze.go): chỉ thu hẹp lệnh `go test` khi chắc chắn; mọi ca còn lại giữ lệnh cũ (chạy rộng hơn còn hơn bỏ sót test).
func TestGoalRunOnly(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const hdr = "package x\n\nimport \"testing\"\n\n"
	write("x/a_test.go", hdr+"func TestB(t *testing.T) {}\nfunc TestA(t *testing.T) {}\nfunc Testlower(t *testing.T) {}\nfunc TestNoArg() {}\nfunc (s S) TestMethod(t *testing.T) {}\n")
	write("x/b_test.go", hdr+"func TestA(t *testing.T) {}\nfunc TestC(t *testing.T) {}\n")
	write("x/fuzz_test.go", hdr+"func TestF(t *testing.T) {}\nfunc FuzzF(f *testing.F) {}\n")
	write("x/example_test.go", hdr+"func TestE(t *testing.T) {}\nfunc ExampleE() {}\n")
	write("x/none_test.go", hdr+"func helper(t *testing.T) {}\n")
	write("x/broken_test.go", "package x\nfunc TestBroken(t *testing.T) {\n")

	const base = "go test -timeout 30m ./x"
	for _, tc := range []struct {
		name  string
		cmd   string
		files []string
		want  string
	}{
		{"một tệp, tên sắp xếp, bỏ hàm không phải Test", base, []string{"x/a_test.go"}, "go test -run '^(TestA|TestB)$' -timeout 30m ./x"},
		{"nhiều tệp, bỏ trùng", base, []string{"x/a_test.go", "x/b_test.go"}, "go test -run '^(TestA|TestB|TestC)$' -timeout 30m ./x"},
		{"lệnh chỉ có go test", "go test", []string{"x/b_test.go"}, "go test -run '^(TestA|TestC)$'"},
		{"đã có -run", "go test -run TestOnly " + "./x", []string{"x/a_test.go"}, "go test -run TestOnly ./x"},
		{"đã có -test.run", "go test -test.run=Only ./x", []string{"x/a_test.go"}, "go test -test.run=Only ./x"},
		{"không phải go test", "sh x/check.sh", []string{"x/a_test.go"}, "sh x/check.sh"},
		{"tiền tố giống go test", "go testing ./x", []string{"x/a_test.go"}, "go testing ./x"},
		{"tệp không phải _test.go", base, []string{"x/a_test.go", "goal/check.sh"}, base},
		{"có Fuzz", base, []string{"x/fuzz_test.go"}, base},
		{"có Example", base, []string{"x/example_test.go"}, base},
		{"không có hàm Test", base, []string{"x/none_test.go"}, base},
		{"không phân tích được", base, []string{"x/broken_test.go"}, base},
		{"tệp không đọc được", base, []string{"x/missing_test.go"}, base},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := goalRunOnly(tc.cmd, root, tc.files); got != tc.want {
				t.Errorf("goalRunOnly(%q) = %q, muốn %q", tc.cmd, got, tc.want)
			}
		})
	}
	if got := goalRunOnly(base, "", []string{"x/a_test.go"}); got != base {
		t.Errorf("root rỗng phải giữ lệnh cũ: %q", got)
	}
}
