package cli_test

import (
	"strings"
	"testing"

	"bint/internal/testutil"
)

// Ràng buộc cờ của `init` và `update` đã chuyển xuống Validate() của dịch vụ: lỗi dùng sai cho mã thoát 2
// (trước giai đoạn 5b, --git và --harness sai trả mã 1).
func TestInitAndUpdateUsageErrorsExitCode2(t *testing.T) {
	testutil.Isolate(t)
	t.Setenv("BINT_SOURCE_DIR", "")
	root := testutil.NewRepo(t, map[string]string{"app.txt": "v1\n"})
	notSource := t.TempDir()
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"init --hooks --no-hooks", []string{"init", "--hooks", "--no-hooks"}, "--hooks và --no-hooks loại trừ nhau"},
		{"init --mcp --no-mcp", []string{"init", "--mcp", "--no-mcp"}, "--mcp và --no-mcp loại trừ nhau"},
		{"init --hooks-scope sai", []string{"init", "--hooks-scope", "global"}, "--hooks-scope"},
		{"init --git sai", []string{"init", "--git", "nham"}, "--git"},
		{"init --harness lạ", []string{"init", "--harness", "vim"}, "harness không hỗ trợ"},
		{"update --source không phải mã nguồn", []string{"update", "--source", notSource}, "không phải mã nguồn Bint"},
	}
	for _, c := range cases {
		out, code := run(t, root, "", c.args...)
		if code != 2 || !strings.Contains(out, c.want) {
			t.Errorf("%s: mã %d, muốn 2 và %q\n%s", c.name, code, c.want, out)
		}
	}
	// Ca ngược: cờ hợp lệ không bị coi là sai cách dùng.
	if out, code := run(t, root, "", "init", "--no-hooks", "--git", "track", "--hooks-scope", "project"); code != 0 {
		t.Errorf("init hợp lệ: mã %d\n%s", code, out)
	}
}
