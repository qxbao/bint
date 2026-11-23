package cli_test

import (
	"strings"
	"testing"

	"bint/internal/testutil"
)

// Phát hiện review GĐ2 .2: .bint/flows có cả testfirst.yml và testfirst.yaml — tra theo tên chọn bản .yml, bản .yaml vẫn bị báo lỗi.
func TestFlowCommandsYmlAndYamlSameName(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "skill", "new", "write-failing-test"); code != 0 {
		t.Fatalf("skill new: %d\n%s", code, out)
	}
	writeFlowE2E(t, root, "testfirst.yaml", e2eTDD)
	writeFlowE2E(t, root, "testfirst.yml", e2eTDD)

	out, code := run(t, root, "", "flow", "show", "testfirst")
	if code != 0 || !strings.Contains(out, ".bint/flows/testfirst.yml") || strings.Contains(out, "testfirst.yaml") {
		t.Fatalf("flow show testfirst: %d\n%s", code, out)
	}
	out, code = run(t, root, "", "flow", "lint", "testfirst")
	if code != 1 || !strings.Contains(out, "✓ .bint/flows/testfirst.yml") || !strings.Contains(out, "testfirst.yaml") || !strings.Contains(out, "trùng") {
		t.Fatalf("flow lint testfirst: %d\n%s", code, out)
	}
	if strings.Index(out, "testfirst.yml") > strings.Index(out, "testfirst.yaml") {
		t.Fatalf("bản .yml phải đứng trước .yaml:\n%s", out)
	}
}
