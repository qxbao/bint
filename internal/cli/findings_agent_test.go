package cli_test

import (
	"encoding/json"
	"strings"
	"testing"

	"bint/internal/testutil"
)

// findings set là thao tác chỉ dành cho người: trong phiên agent bị từ chối và không ghi gì.
func TestFindingsSetForbiddenInAgentSession(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n", "a.go": "package a\n"})
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	out, code := run(t, root, "", "task", "new", "việc", "--json")
	if code != 0 {
		t.Fatalf("task new: %d\n%s", code, out)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if o, c := run(t, root, findingsReview, "task", "review", created.ID, "-"); c != 0 {
		t.Fatalf("task review: %d\n%s", c, o)
	}
	asAgent(t)
	o, c := run(t, root, "", "findings", "set", "rejected", created.ID+".1", "--reason", "x")
	if c == 0 || !strings.Contains(o, "chỉ dành cho người dùng") {
		t.Fatalf("agent phải bị từ chối: %d\n%s", c, o)
	}
	asHuman(t)
	got := exec2(t, root, "findings", "list", "--open", "--json")
	var res struct{ Findings []map[string]any }
	if err := json.Unmarshal([]byte(got), &res); err != nil || len(res.Findings) != 3 {
		t.Fatalf("không được ghi gì khi bị từ chối (%v):\n%s", err, got)
	}
}
