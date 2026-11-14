package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/testutil"
)

func TestTaskDepsE2E(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n", "a.go": "package a\n"})
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	newJSON := func(args ...string) map[string]any {
		t.Helper()
		out := exec2(t, root, append([]string{"task", "new", "--json"}, args...)...)
		var m map[string]any
		if err := json.Unmarshal([]byte(out), &m); err != nil {
			t.Fatalf("json: %v\n%s", err, out)
		}
		return m
	}
	listIDs := func() []string {
		t.Helper()
		var rows []struct{ ID string }
		if err := json.Unmarshal([]byte(exec2(t, root, "task", "list", "--all", "--json")), &rows); err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, r := range rows {
			ids = append(ids, r.ID)
		}
		return ids
	}
	readyIDs := func(args ...string) []string {
		t.Helper()
		var rows []struct{ ID string }
		if err := json.Unmarshal([]byte(exec2(t, root, append([]string{"task", "ready", "--json"}, args...)...)), &rows); err != nil {
			t.Fatal(err)
		}
		ids := []string{}
		for _, r := range rows {
			ids = append(ids, r.ID)
		}
		return ids
	}
	eq := func(got, want []string) bool { return strings.Join(got, ",") == strings.Join(want, ",") }

	// Phiếu không quan hệ: không có khoá mới.
	plain := newJSON("việc thường")
	for _, k := range []string{"parent", "depends_on", "inherited", "from_findings"} {
		if _, has := plain[k]; has {
			t.Fatalf("task new thường không được có %s: %v", k, plain)
		}
	}
	plainID := plain["id"].(string)
	if s := exec2(t, root, "task", "show", plainID, "--json"); strings.Contains(s, `"children"`) || strings.Contains(s, "depends_on") || strings.Contains(s, "parent") {
		t.Fatalf("show --json phiếu không quan hệ có khoá quan hệ:\n%s", s)
	}
	if s := exec2(t, root, "task", "show", plainID); strings.Contains(s, "Quan hệ") {
		t.Fatalf("show phiếu không quan hệ có khối quan hệ:\n%s", s)
	}

	// Phiếu cha có phạm vi bảo vệ, tài liệu tham chiếu và ràng buộc.
	parent := newJSON("kế hoạch cha", "--allow", "migrations/**", "--constraint", "không đụng cấu hình", "--deny", "secret/**")
	pid := parent["id"].(string)
	before := listIDs()

	// ID sai / tiền tố mơ hồ / không tồn tại: mã 2, không tạo thư mục.
	for _, args := range [][]string{
		{"task", "new", "con", "--parent", "NOPE"},
		{"task", "new", "con", "--depends-on", "NOPE"},
		{"task", "new", "con", "--depends-on", pid + "," + "ZZZZ"},
		{"task", "new", "con", "--depends-on", "0"}, // tiền tố mơ hồ (mọi ULID cùng bắt đầu bằng 0)
	} {
		if o, c := run(t, root, "", args...); c != 2 {
			t.Fatalf("%v: mã %d, muốn 2\n%s", args, c, o)
		}
	}
	if got := listIDs(); len(got) != len(before) {
		t.Fatalf("lỗi đầu vào đã tạo thư mục tác vụ: %v -> %v", before, got)
	}

	// Kế thừa: phiếu con không đặt gì kế thừa đủ, Assess tính trên phiếu sau kế thừa; nhận tiền tố ID.
	c1 := newJSON("con một", "--parent", pid[:20], "--depends-on", plainID[:20])
	c1id := c1["id"].(string)
	if c1["parent"] != pid {
		t.Fatalf("parent phải là ID đầy đủ: %v", c1["parent"])
	}
	if d, _ := c1["depends_on"].([]any); len(d) != 1 || d[0] != plainID {
		t.Fatalf("depends_on: %v", c1["depends_on"])
	}
	inh, _ := c1["inherited"].([]any)
	if len(inh) != 3 { // scope.allow, scope.deny, constraints (cha không có references)
		t.Fatalf("inherited: %v", inh)
	}
	if a := c1["assessment"].(map[string]any); a["risk"] != "high" {
		t.Fatalf("rủi ro phải tính trên phiếu sau kế thừa: %v", a)
	}
	// Phiếu con tự đặt phạm vi / --clear-scope thì không bị ghi đè.
	own := newJSON("con hai", "--parent", pid, "--allow", "a.go")
	if s, _ := json.Marshal(own["ticket"]); strings.Contains(string(s), "migrations") {
		t.Fatalf("phạm vi tự đặt bị ghi đè: %s", s)
	}
	cleared := newJSON("con ba", "--parent", pid, "--clear-scope")
	if s, _ := json.Marshal(cleared["ticket"]); strings.Contains(string(s), "migrations") {
		t.Fatalf("--clear-scope bị ghi đè: %s", s)
	}
	if a := cleared["assessment"].(map[string]any); a["risk"] == "high" {
		t.Fatalf("--clear-scope không được kế thừa phạm vi bảo vệ: %v", a)
	}

	// Ready: c1 phụ thuộc plainID (chưa done) nên vắng.
	ready := readyIDs()
	if !eq(ready, sortedIDs(plainID, pid, own["id"].(string), cleared["id"].(string))) {
		t.Fatalf("ready: %v", ready)
	}
	if got := readyIDs("--parent", pid); !eq(got, sortedIDs(own["id"].(string), cleared["id"].(string))) {
		t.Fatalf("ready --parent: %v", got)
	}
	exec2(t, root, "task", "status", plainID, "in_progress", "--reason", "thử")
	exec2(t, root, "task", "status", plainID, "done", "--force", "--reason", "thử")
	if got := readyIDs(); !strings.Contains(strings.Join(got, ","), c1id) || strings.Contains(strings.Join(got, ","), plainID) {
		t.Fatalf("sau khi phụ thuộc done c1 phải sẵn sàng, phiếu done bị loại: %v", got)
	}
	// Phụ thuộc chưa done hoặc cancelled không mở khoá.
	dep2 := newJSON("phụ thuộc huỷ")["id"].(string)
	c2 := newJSON("con bị chặn", "--depends-on", dep2)["id"].(string)
	if got := strings.Join(readyIDs(), ","); strings.Contains(got, c2) {
		t.Fatalf("dep chưa done: c2 phải vắng: %s", got)
	}
	exec2(t, root, "task", "cancel", dep2)
	if got := strings.Join(readyIDs(), ","); strings.Contains(got, c2) || strings.Contains(got, dep2) {
		t.Fatalf("dep cancelled không mở khoá, phiếu cancelled bị loại: %s", got)
	}

	// show: phiếu cha liệt kê con; phiếu con liệt kê cha và phụ thuộc kèm trạng thái.
	var sh struct{ Children []struct{ ID string } }
	if err := json.Unmarshal([]byte(exec2(t, root, "task", "show", pid, "--json")), &sh); err != nil || len(sh.Children) != 3 {
		t.Fatalf("children: %v %v", sh, err)
	}
	txt := exec2(t, root, "task", "show", c1id)
	for _, w := range []string{"Phiếu cha: " + pid, "Phụ thuộc: " + plainID + " [done]", "Kế thừa từ phiếu cha"} {
		if !strings.Contains(txt, w) {
			t.Fatalf("show thiếu %q:\n%s", w, txt)
		}
	}
	if txt := exec2(t, root, "task", "show", pid); !strings.Contains(txt, "Phiếu con: "+c1id) {
		t.Fatalf("show cha thiếu phiếu con:\n%s", txt)
	}
}

func sortedIDs(ids ...string) []string {
	out := append([]string{}, ids...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func TestTaskDepsCycleE2E(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	mk := func(goal string) string {
		var m struct{ ID string }
		if err := json.Unmarshal([]byte(exec2(t, root, "task", "new", "--json", goal)), &m); err != nil {
			t.Fatal(err)
		}
		return m.ID
	}
	a, b, c := mk("a"), mk("b"), mk("c")
	state := func(id string) []byte {
		t.Helper()
		var m struct{ Dir string }
		if err := json.Unmarshal([]byte(exec2(t, root, "task", "show", id, "--json")), &m); err != nil {
			t.Fatal(err)
		}
		out, err := os.ReadFile(filepath.Join(m.Dir, "state.json"))
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	exec2(t, root, "task", "set", a, "--depends-on", b)
	exec2(t, root, "task", "set", b, "--depends-on", c)
	exec2(t, root, "task", "set", a, "--depends-on", b) // lặp lại: không trùng
	before := [][]byte{state(a), state(b), state(c)}
	for _, args := range [][]string{
		{"task", "set", b, "--depends-on", a},      // 2 nút
		{"task", "set", c, "--depends-on", a},      // 3 nút
		{"task", "set", c, "--depends-on", c},      // tự trỏ
		{"task", "set", c, "--depends-on", "NOPE"}, // không tồn tại
	} {
		o, code := run(t, root, "", args...)
		if code != 2 {
			t.Fatalf("%v: mã %d, muốn 2\n%s", args, code, o)
		}
	}
	if o, _ := run(t, root, "", "task", "set", c, "--depends-on", a); !strings.Contains(o, a+" → "+b+" → "+c+" → "+a) && !strings.Contains(o, c+" → "+a+" → "+b+" → "+c) {
		t.Fatalf("thông báo phải nêu đường chu trình:\n%s", o)
	}
	for i, id := range []string{a, b, c} {
		if string(state(id)) != string(before[i]) {
			t.Fatalf("state.json của %s đổi sau khi bị từ chối", id)
		}
	}
	if s := exec2(t, root, "task", "show", a, "--json"); strings.Count(s, b) < 1 || strings.Count(s, `"depends_on"`) != 1 {
		t.Fatalf("depends_on:\n%s", s)
	}
	exec2(t, root, "task", "set", a, "--clear-depends-on")
	if s := exec2(t, root, "task", "show", a, "--json"); strings.Contains(s, "depends_on") {
		t.Fatalf("--clear-depends-on:\n%s", s)
	}

	// Kèm cờ sửa phiếu mà chu trình bị từ chối: không có gì được ghi (cả mục tiêu, tiêu chí lẫn state.json).
	before = [][]byte{state(a), state(b), state(c)}
	showBefore := exec2(t, root, "task", "show", c, "--json")
	exec2(t, root, "task", "set", a, "--depends-on", b)
	before[0] = state(a)
	if o, code := run(t, root, "", "task", "set", c, "--depends-on", a, "--goal", "mục tiêu mới", "--criteria", "tiêu chí mới"); code != 2 {
		t.Fatalf("chu trình kèm --goal: mã %d, muốn 2\n%s", code, o)
	}
	if s := exec2(t, root, "task", "show", c, "--json"); s != showBefore {
		t.Fatalf("phiếu đổi dù bị từ chối:\n%s\n---\n%s", showBefore, s)
	}
	for i, id := range []string{a, b, c} {
		if string(state(id)) != string(before[i]) {
			t.Fatalf("state.json của %s đổi sau khi bị từ chối", id)
		}
	}
	// Hợp lệ: ghi cả hai.
	exec2(t, root, "task", "set", c, "--depends-on", plainOf(t, root), "--goal", "mục tiêu mới")
	if s := exec2(t, root, "task", "show", c, "--json"); !strings.Contains(s, "mục tiêu mới") || !strings.Contains(s, `"depends_on"`) {
		t.Fatalf("set kèm cờ sửa phiếu:\n%s", s)
	}

	// Phiếu đã kết thúc: --depends-on bị từ chối như task set thường, state.json không đổi.
	exec2(t, root, "task", "status", b, "cancelled", "--reason", "thử")
	bState := state(b)
	if o, code := run(t, root, "", "task", "set", b, "--depends-on", c); code == 0 {
		t.Fatalf("phiếu cancelled phải từ chối --depends-on\n%s", o)
	}
	if o, code := run(t, root, "", "task", "set", b, "--clear-depends-on"); code == 0 {
		t.Fatalf("phiếu cancelled phải từ chối --clear-depends-on\n%s", o)
	}
	if string(state(b)) != string(bState) {
		t.Fatal("state.json của phiếu cancelled đổi")
	}
}

// plainOf tạo một phiếu mới không quan hệ và trả ID.
func plainOf(t *testing.T, root string) string {
	t.Helper()
	var m struct{ ID string }
	if err := json.Unmarshal([]byte(exec2(t, root, "task", "new", "--json", "phụ thuộc hợp lệ")), &m); err != nil {
		t.Fatal(err)
	}
	return m.ID
}
