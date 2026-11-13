package cli_test

import (
	"encoding/json"
	"strings"
	"testing"

	"bint/internal/testutil"
)

const findingsReview = `{"summary":"r","findings":[
 {"kind":"bug","severity":"high","title":"lỗi một","path":"b.go","evidence":"e1"},
 {"kind":"risk","severity":"low","title":"lỗi hai","path":"a.go","evidence":"e2"},
 {"kind":"suggestion","severity":"low","title":"không path","evidence":"e3"}]}`

func TestFindingsOutcomesE2E(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n", "a.go": "package a\n", "b.go": "package b\n"})
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	newTask := func(args ...string) (string, int) {
		return run(t, root, "", append([]string{"task", "new"}, args...)...)
	}
	out, code := newTask("việc gốc", "--json")
	if code != 0 {
		t.Fatalf("task new: %d\n%s", code, out)
	}
	var created map[string]any
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if _, has := created["from_findings"]; has {
		t.Fatalf("task new thường không được có from_findings: %v", created)
	}
	id := created["id"].(string)
	if o, c := run(t, root, `{"summary":"x","findings":[{"kind":"bug","severity":"low","title":"t","evidence":"e","outcome":"fixed"}]}`, "task", "review", id, "-"); c == 0 {
		t.Fatalf("outcome trong review phải bị từ chối:\n%s", o)
	}
	if o, c := run(t, root, findingsReview, "task", "review", id, "-"); c != 0 {
		t.Fatalf("task review: %d\n%s", c, o)
	}
	f1, f2, f3 := id+".1", id+".2", id+".3"

	list := func(args ...string) []map[string]any {
		t.Helper()
		cmd := exec2(t, root, append([]string{"findings", "list", "--json"}, args...)...)
		var res struct{ Findings []map[string]any }
		if err := json.Unmarshal([]byte(cmd), &res); err != nil {
			t.Fatalf("json: %v\n%s", err, cmd)
		}
		return res.Findings
	}
	if got := list("--open"); len(got) != 3 {
		t.Fatalf("mọi phát hiện mặc định open: %v", got)
	}

	// Lỗi cách dùng: không ghi gì.
	for _, args := range [][]string{
		{"findings", "set", "bogus", f1},
		{"findings", "set", "fixed", f1},
		{"findings", "set", "fixed", f1, "--commit", "deadbeefdeadbeef"},
		{"findings", "set", "rejected", f1},
		{"findings", "set", "deferred", f1},
		{"findings", "set", "rejected", f1, f1 + "9", "--reason", "x"},
		{"findings", "set", "rejected", "--reason", "x"},
	} {
		if o, c := run(t, root, "", args...); c != 2 {
			t.Fatalf("%v: mã %d, muốn 2\n%s", args, c, o)
		}
	}
	if got := list("--open"); len(got) != 3 {
		t.Fatalf("lỗi đầu vào không được ghi gì: %v", got)
	}

	head := testutil.Git(t, root, "rev-parse", "HEAD")
	if o, c := run(t, root, "", "findings", "set", "fixed", f1, "--commit", head[:8]); c != 0 || !strings.Contains(o, head) {
		t.Fatalf("set fixed: %d\n%s", c, o)
	}
	if o, c := run(t, root, "", "findings", "set", "deferred", f3, "--reason", "để sau"); c != 0 {
		t.Fatalf("set deferred: %d\n%s", c, o)
	}
	if got := list("--open"); len(got) != 1 || got[0]["id"] != f2 {
		t.Fatalf("--open: %v", got)
	}
	all := list()
	if len(all) != 3 || all[0]["outcome"] != "fixed" || all[0]["commit"] != head {
		t.Fatalf("list: %v", all)
	}

	// Vòng review mới không làm mất kết cục; tác vụ done/cancelled vẫn được liệt kê.
	if o, c := run(t, root, findingsReview, "task", "review", id, "-"); c != 0 {
		t.Fatalf("review vòng 2: %d\n%s", c, o)
	}
	if got := list("--task", id); len(got) != 6 || got[0]["outcome"] != "fixed" {
		t.Fatalf("sau vòng 2: %v", got)
	}
	if o, c := run(t, root, "", "task", "cancel", id); c != 0 {
		t.Fatalf("cancel: %d\n%s", c, o)
	}
	if got := list("--open"); len(got) != 4 {
		t.Fatalf("tác vụ cancelled vẫn liệt kê: %v", got)
	}

	// task new --from-findings.
	if o, c := newTask("--from-findings", f1); c != 2 || !strings.Contains(o, "fixed") {
		t.Fatalf("phát hiện fixed phải bị từ chối: %d\n%s", c, o)
	}
	if o, c := newTask("--from-findings", id+".99"); c != 2 {
		t.Fatalf("ID lạ phải bị từ chối: %d\n%s", c, o)
	}
	if o, c := run(t, root, "", "findings", "set", "rejected", id+".6", "--reason", "không đúng"); c != 0 {
		t.Fatalf("set rejected: %d\n%s", c, o)
	}
	if o, c := newTask("--from-findings", id+".6"); c != 2 {
		t.Fatalf("phát hiện rejected phải bị từ chối: %d\n%s", c, o)
	}
	out, code = newTask("--from-findings", id+".4,"+id+".5,"+f2, "--allow", "docs/**", "--json")
	if code != 0 {
		t.Fatalf("from-findings: %d\n%s", code, out)
	}
	var fixTask struct {
		Ticket struct {
			Goal  string
			Scope struct{ Allow []string }
		}
		FromFindings []string `json:"from_findings"`
	}
	if err := json.Unmarshal([]byte(out), &fixTask); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	want := []string{"a.go", "b.go", "docs/**"}
	if strings.Join(fixTask.Ticket.Scope.Allow, ",") != strings.Join(want, ",") {
		t.Fatalf("scope.allow = %v, muốn %v", fixTask.Ticket.Scope.Allow, want)
	}
	if len(fixTask.FromFindings) != 3 {
		t.Fatalf("from_findings: %v", fixTask.FromFindings)
	}
	if o, c := newTask("--from-findings", f3); c != 0 || !strings.Contains(o, "Từ phát hiện: "+f3) {
		t.Fatalf("deferred được phép: %d\n%s", c, o)
	}
}

func exec2(t *testing.T, root string, args ...string) string {
	t.Helper()
	o, c := run(t, root, "", args...)
	if c != 0 {
		t.Fatalf("%v: mã %d\n%s", args, c, o)
	}
	return o
}
