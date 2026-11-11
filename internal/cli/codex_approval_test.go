package cli_test

import (
	"encoding/json"
	"strings"
	"testing"

	"bint/internal/testutil"
)

func TestApprovePathBeforeHookAllowsCreation(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"a.go": "package a\n"})
	if out, code := run(t, root, "", "init", "--harness", "codex", "--no-hooks"); code != 0 {
		t.Fatalf("init: %s", out)
	}
	out, code := run(t, root, "", "task", "new", "Create a protected migration", "--json")
	var created struct {
		ID string `json:"id"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &created) != nil || created.ID == "" {
		t.Fatalf("new: %s", out)
	}
	// Hook đã deny trước khi file tồn tại, nên không có diff để Approve tự tìm.
	if out, code = run(t, root, "", "task", "approve", created.ID, "--path", "migrations/001.sql"); code != 0 || !strings.Contains(out, "migrations/001.sql") {
		t.Fatalf("duyệt trước đường dẫn: %d %s", code, out)
	}
	for _, path := range []string{"../outside", "migrations/**", "migrations/../.."} {
		if out, code := run(t, root, "", "task", "approve", created.ID, "--path", path); code == 0 {
			t.Fatalf("không được duyệt %q: %s", path, out)
		}
	}
	t.Setenv("BINT_AGENT", "1")
	if out, code := run(t, root, "", "task", "approve", created.ID, "--path", "migrations/002.sql"); code == 0 || !strings.Contains(out, "chỉ dành cho người") {
		t.Fatalf("agent tự duyệt: %s", out)
	}
}
