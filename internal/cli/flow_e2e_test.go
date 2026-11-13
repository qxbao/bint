package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/testutil"
)

const e2eTDD = `# flow mẫu của kế hoạch
schema: 1
name: tdd
description: "Dùng khi muốn viết kiểm thử đỏ trước khi sửa mã"
steps:
  - {id: plan,   kind: plan,      skill: b-req, optional: true}
  - {id: red,    kind: implement, skill: write-failing-test, role: test-writer, verify: false}
  - {id: green,  kind: implement, skill: b-feat}
  - {id: review, kind: review,    skill: b-review, fix: green}
`

func writeFlowE2E(t *testing.T, root, name, content string) {
	t.Helper()
	p := filepath.Join(root, ".bint", "flows", name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Flow tuỳ biến từ đầu tới cuối: list, show, lint (thoát 0 với flow mẫu kèm skill tuỳ biến, thoát 1 khi lỗi).
func TestFlowCommandsEndToEnd(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}

	// Chưa có flow tuỳ biến: list có bốn flow nhúng, lint không tên thoát 0.
	out, code := run(t, root, "", "flow", "list")
	if code != 0 {
		t.Fatalf("flow list: %d\n%s", code, out)
	}
	for _, n := range []string{"b-do", "b-req", "b-feat", "b-review"} {
		if !strings.Contains(out, n) {
			t.Errorf("flow list thiếu %s:\n%s", n, out)
		}
	}
	if out, code = run(t, root, "", "flow", "lint"); code != 0 || !strings.Contains(out, "Chưa có flow tuỳ biến") {
		t.Fatalf("flow lint không flow: %d\n%s", code, out)
	}
	if out, code = run(t, root, "", "flow", "lint", "b-do"); code != 0 || !strings.Contains(out, "b-do (nhúng)") {
		t.Fatalf("flow lint b-do: %d\n%s", code, out)
	}

	// tdd chưa có skill write-failing-test → thoát 1 và nêu skill.
	writeFlowE2E(t, root, "tdd.yml", e2eTDD)
	if out, code = run(t, root, "", "flow", "lint", "tdd"); code != 1 || !strings.Contains(out, "write-failing-test") {
		t.Fatalf("flow lint tdd thiếu skill: %d\n%s", code, out)
	}

	// Tạo skill tuỳ biến bằng bint skill new → flow mẫu thoát 0.
	if out, code = run(t, root, "", "skill", "new", "write-failing-test"); code != 0 {
		t.Fatalf("skill new: %d\n%s", code, out)
	}
	if out, code = run(t, root, "", "flow", "lint", "tdd"); code != 0 || !strings.Contains(out, "✓ .bint/flows/tdd.yml") {
		t.Fatalf("flow lint tdd: %d\n%s", code, out)
	}
	if out, code = run(t, root, "", "flow", "lint"); code != 0 {
		t.Fatalf("flow lint (tất cả): %d\n%s", code, out)
	}

	// list: bốn flow nhúng + tdd, ở dạng JSON.
	out, _ = run(t, root, "", "flow", "list", "--json")
	var rows []struct {
		Name, Source, Path string
		Steps              int
		OK                 bool
	}
	if json.Unmarshal([]byte(out), &rows) != nil || len(rows) != 5 {
		t.Fatalf("flow list --json: %s", out)
	}
	if last := rows[4]; last.Name != "tdd" || last.Source != "custom" || last.Steps != 4 || !last.OK || last.Path != ".bint/flows/tdd.yml" {
		t.Fatalf("dòng tdd: %+v", last)
	}
	if out, _ = run(t, root, "", "flow", "list"); !strings.Contains(out, "tdd") || !strings.Contains(out, "Dùng khi muốn viết kiểm thử đỏ") {
		t.Fatalf("flow list thiếu tdd:\n%s", out)
	}

	// show: b-do in ba bước, tdd in bốn bước đã điền role mặc định.
	out, code = run(t, root, "", "flow", "show", "b-do")
	if code != 0 {
		t.Fatalf("flow show b-do: %d\n%s", code, out)
	}
	for _, want := range []string{"flow nhúng", "id: b-req", "id: b-feat", "id: b-review", "fix: b-feat", "optional: true"} {
		if !strings.Contains(out, want) {
			t.Errorf("flow show b-do thiếu %q:\n%s", want, out)
		}
	}
	out, code = run(t, root, "", "flow", "show", "tdd")
	if code != 0 {
		t.Fatalf("flow show tdd: %d\n%s", code, out)
	}
	for _, want := range []string{".bint/flows/tdd.yml", "id: plan", "id: red", "id: green", "id: review", "role: test-writer", "role: reviewer", "verify: false", "sha256:"} {
		if !strings.Contains(out, want) {
			t.Errorf("flow show tdd thiếu %q:\n%s", want, out)
		}
	}
	out, _ = run(t, root, "", "flow", "show", "tdd", "--json")
	var shown struct {
		Name, Hash string
		Definition struct{ Steps []struct{ ID, Role string } }
	}
	if json.Unmarshal([]byte(out), &shown) != nil || shown.Name != "tdd" || len(shown.Definition.Steps) != 4 || shown.Definition.Steps[0].Role != "planner" {
		t.Fatalf("flow show --json: %s", out)
	}
	if _, code = run(t, root, "", "flow", "show", "khong-co"); code == 0 {
		t.Fatal("flow show tên lạ phải lỗi")
	}

	// Các lỗi của AC-5 qua CLI đều thoát 1.
	for name, c := range map[string]struct{ yaml, want string }{
		"khoalalo":   {strings.Replace(e2eTDD, "schema: 1", "schema: 1\nowner: x", 1), "owner"},
		"kindsai":    {strings.Replace(e2eTDD, "kind: plan,", "kind: build,", 1), "kind"},
		"saithutu":   {"schema: 1\nname: saithutu\nsteps:\n  - {id: a, kind: review, skill: b-review}\n  - {id: b, kind: implement, skill: b-feat}\n", "plan → implement → review"},
		"thieuimpl":  {"schema: 1\nname: thieuimpl\nsteps:\n  - {id: a, kind: plan, skill: b-req}\n", "thiếu bước implement"},
		"fixsai":     {strings.Replace(e2eTDD, "fix: green", "fix: plan", 1), "fix"},
		"b-do":       {strings.Replace(e2eTDD, "name: tdd", "name: b-do", 1), "trùng workflow gốc"},
		"khongskill": {strings.Replace(e2eTDD, "write-failing-test", "khong-co-skill", 1), "khong-co-skill"},
	} {
		writeFlowE2E(t, root, name+".yml", strings.Replace(c.yaml, "name: tdd", "name: "+name, 1))
		out, code = run(t, root, "", "flow", "lint", name)
		if code != 1 || !strings.Contains(out, c.want) {
			t.Errorf("flow %s: thoát %d, muốn 1 kèm %q:\n%s", name, code, c.want, out)
		}
	}
	// Lint không tên: một flow hỏng làm cả lệnh thoát 1 nhưng vẫn in flow tốt.
	out, code = run(t, root, "", "flow", "lint")
	if code != 1 || !strings.Contains(out, "✓ .bint/flows/tdd.yml") {
		t.Fatalf("flow lint tất cả: %d\n%s", code, out)
	}
}
