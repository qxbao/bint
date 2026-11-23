package cli_test

import (
	"strings"
	"testing"

	"bint/internal/testutil"
)

// audit/verdict/verdicts qua CLI: agent lưu được nhận định nhưng không áp được; người áp được.
func TestKnowledgeAuditVerdictCLI(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n", "a.go": "package a\n\nfunc A() {}\n"})
	testutil.Git(t, root, "config", "user.name", "Rev")
	testutil.Git(t, root, "config", "user.email", "rev@example.com")
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	out, code := run(t, root, "", "knowledge", "add", "--type", "rule", "--title", "Quy tắc A", "--paths", "a.go",
		"--evidence", "a.go:3", "Hàm A phải rỗng.")
	if code != 0 {
		t.Fatalf("add: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "knowledge", "audit"); code != 0 || !strings.Contains(out, "Không có mục nào cần rà soát") {
		t.Fatalf("audit: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "knowledge", "verdict", "ABCD", "--verdict", "bậy", "--reason", "x"); code == 0 || !strings.Contains(out, "fix-evidence") {
		t.Fatalf("verdict sai phải lỗi: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "knowledge", "verdicts"); code != 0 || !strings.Contains(out, "Chưa có nhận định") {
		t.Fatalf("verdicts: %d\n%s", code, out)
	}
	t.Setenv("CLAUDECODE", "1")
	if out, code := run(t, root, "", "knowledge", "verdicts", "--apply"); code == 0 {
		t.Fatalf("agent áp nhận định phải bị chặn\n%s", out)
	}
}
