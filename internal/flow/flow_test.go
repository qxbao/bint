package flow

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const tddYAML = `# flow mẫu của kế hoạch
schema: 1
name: testfirst
description: "Dùng khi muốn viết kiểm thử đỏ trước khi sửa mã"
steps:
  - {id: plan,   kind: plan,      skill: b-req, optional: true}
  - {id: red,    kind: implement, skill: write-failing-test, role: test-writer, verify: false}
  - {id: green,  kind: implement, skill: b-feat}
  - {id: review, kind: review,    skill: b-review, fix: green}
`

// skillSet dựng skillExists từ danh sách tên.
func skillSet(names ...string) func(string) bool {
	return func(n string) bool {
		for _, x := range names {
			if x == n {
				return true
			}
		}
		return false
	}
}

var testSkills = skillSet("b-req", "b-feat", "b-review", "b-do", "write-failing-test")

func mustDecode(t *testing.T, y string) Def {
	t.Helper()
	d, err := Decode([]byte(y))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	return d
}

func TestValidateTable(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		file string // "" = testfirst
		want string // chuỗi con của lỗi; "" = hợp lệ
	}{
		{name: "hợp lệ: flow testfirst mẫu", yaml: tddYAML},
		{name: "schema 2", want: "schema: 2", yaml: strings.Replace(tddYAML, "schema: 1", "schema: 2", 1)},
		{name: "kind sai", want: `kind "build" không hợp lệ`, yaml: strings.Replace(tddYAML, "kind: plan,", "kind: build,", 1)},
		{name: "implement trước plan", want: "plan → implement → review", yaml: `
schema: 1
name: testfirst
steps:
  - {id: green, kind: implement, skill: b-feat}
  - {id: plan, kind: plan, skill: b-req}
`},
		{name: "review trước implement", want: "plan → implement → review", yaml: `
schema: 1
name: testfirst
steps:
  - {id: review, kind: review, skill: b-review}
  - {id: green, kind: implement, skill: b-feat}
`},
		{name: "thiếu bước implement", want: "thiếu bước implement", yaml: `
schema: 1
name: testfirst
steps:
  - {id: plan, kind: plan, skill: b-req}
  - {id: review, kind: review, skill: b-review}
`},
		{name: "không có bước", want: "không có bước nào", yaml: "schema: 1\nname: testfirst\nsteps: []\n"},
		{name: "skill không tồn tại", want: `skill "khong-co" không tồn tại`, yaml: strings.Replace(tddYAML, "write-failing-test", "khong-co", 1)},
		{name: "skill rỗng", want: "skill rỗng", yaml: strings.Replace(tddYAML, "skill: b-feat}", "skill: ''}", 1)},
		{name: "fix trỏ bước review", want: "fix phải trỏ tới bước implement", yaml: `
schema: 1
name: testfirst
steps:
  - {id: green, kind: implement, skill: b-feat}
  - {id: r1, kind: review, skill: b-review}
  - {id: r2, kind: review, skill: b-review, fix: r1}
`},
		{name: "fix trỏ bước plan", want: "fix phải trỏ tới bước implement", yaml: strings.Replace(tddYAML, "fix: green", "fix: plan", 1)},
		{name: "fix trỏ bước không tồn tại", want: `fix trỏ tới bước "ma" không tồn tại`, yaml: strings.Replace(tddYAML, "fix: green", "fix: ma", 1)},
		{name: "fix ở bước không phải review", want: "fix chỉ dùng được ở bước review", yaml: strings.Replace(tddYAML, "skill: b-feat}", "skill: b-feat, fix: red}", 1)},
		{name: "tên trùng workflow gốc", want: "trùng workflow gốc", file: "b-do", yaml: strings.Replace(tddYAML, "name: testfirst", "name: b-do", 1)},
		{name: "tiền tố b-", want: "tiền tố b-", file: "b-testfirst", yaml: strings.Replace(tddYAML, "name: testfirst", "name: b-testfirst", 1)},
		{name: "tên sai quy tắc", want: "không hợp lệ (chữ thường", file: "TDD", yaml: strings.Replace(tddYAML, "name: testfirst", "name: TDD", 1)},
		{name: "tên khác tên tệp", want: "khác tên tệp", file: "khac", yaml: tddYAML},
		{name: "tên trùng skill", want: "trùng tên một skill", file: "write-failing-test", yaml: strings.Replace(tddYAML, "name: testfirst", "name: write-failing-test", 1)},
		{name: "id trùng", want: "id trùng với bước trước", yaml: strings.Replace(tddYAML, "id: green", "id: red", 1)},
		{name: "id rỗng", want: "id rỗng", yaml: strings.Replace(tddYAML, "id: green", "id: ''", 1)},
		{name: "id post-feat", want: "bước giả", yaml: strings.Replace(tddYAML, "id: green", "id: post-feat", 1)},
		{name: "id verify", want: "bước giả", yaml: strings.Replace(tddYAML, "id: green", "id: verify", 1)},
		{name: "hai bước plan", want: "chỉ được có một bước plan", yaml: strings.Replace(tddYAML, "  - {id: red,", "  - {id: plan2, kind: plan, skill: b-req}\n  - {id: red,", 1)},
		{name: "optional ở implement", want: "optional chỉ dùng được ở bước plan", yaml: strings.Replace(tddYAML, "skill: b-feat}", "skill: b-feat, optional: true}", 1)},
		{name: "verify false ở implement cuối", want: "implement cuối", yaml: strings.Replace(tddYAML, "skill: b-feat}", "skill: b-feat, verify: false}", 1)},
		{name: "role sai", want: `role "Test Writer" không hợp lệ`, yaml: strings.Replace(tddYAML, "role: test-writer", `role: "Test Writer"`, 1)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			file := tc.file
			if file == "" {
				file = "testfirst"
			}
			d := mustDecode(t, tc.yaml)
			errs := Validate(d, file, testSkills)
			if tc.want == "" {
				if len(errs) != 0 {
					t.Fatalf("muốn hợp lệ, được %v", errs)
				}
				return
			}
			found := false
			for _, e := range errs {
				if strings.Contains(e, tc.want) {
					found = true
				}
			}
			if !found {
				t.Fatalf("muốn lỗi chứa %q, được %v", tc.want, errs)
			}
		})
	}
}

func TestDecodeRejectsUnknownKeys(t *testing.T) {
	for name, y := range map[string]string{
		"khoá lạ ở gốc":    strings.Replace(tddYAML, "schema: 1", "schema: 1\nowner: ai-do", 1),
		"khoá lạ ở bước":   strings.Replace(tddYAML, "skill: b-feat}", "skill: b-feat, timeout: 5}", 1),
		"tài liệu thứ hai": tddYAML + "---\nschema: 1\nname: khac\n",
		"tệp rỗng":         "",
		"không phải map":   "- a\n- b\n",
	} {
		if _, err := Decode([]byte(y)); err == nil {
			t.Errorf("%s: muốn lỗi, được nil", name)
		}
	}
	if _, err := Decode([]byte(tddYAML)); err != nil {
		t.Fatalf("flow mẫu: %v", err)
	}
}

func TestBuiltinPassValidate(t *testing.T) {
	core := skillSet("b-req", "b-goal", "b-feat", "b-review", "b-do", "b-debug", "b-repro", "b-fix")
	got := Builtin()
	if len(got) != 5 {
		t.Fatalf("Builtin trả %d flow, muốn 5", len(got))
	}
	for i, d := range got {
		if d.Name != BuiltinNames()[i] || !d.IsBuiltin() {
			t.Errorf("flow %d: %q builtin=%v", i, d.Name, d.IsBuiltin())
		}
		if errs := Validate(d, "", core); len(errs) != 0 {
			t.Errorf("%s: %v", d.Name, errs)
		}
	}
	do := got[0]
	var ids []string
	for _, s := range do.Steps {
		ids = append(ids, s.ID)
	}
	if strings.Join(ids, ",") != "b-req,b-goal,b-feat,b-review" {
		t.Errorf("b-do có bước %v", ids)
	}
	if !do.Steps[0].Optional || !do.Steps[1].Optional || do.Steps[1].Kind != KindGoal || do.Steps[3].Fix != "b-feat" {
		t.Errorf("b-do: plan optional=%v, goal=%+v, fix=%q", do.Steps[0].Optional, do.Steps[1], do.Steps[3].Fix)
	}
}

func TestBuiltinDebugShape(t *testing.T) {
	d, ok := GetBuiltin("b-debug")
	if !ok || BuiltinNames()[4] != "b-debug" || Legacy("b-debug") || !IsBuiltin("b-debug") {
		t.Fatalf("b-debug: ok=%v names=%v legacy=%v", ok, BuiltinNames(), Legacy("b-debug"))
	}
	want := []struct {
		id    string
		kind  Kind
		skill string
		fix   string
		opt   bool
		exp   bool
	}{
		{"b-req", KindPlan, "b-req", "", true, false},
		{"b-repro", KindImplement, "b-repro", "", false, true},
		{"b-fix", KindImplement, "b-fix", "", false, false},
		{"b-review", KindReview, "b-review", "b-fix", false, false},
	}
	if len(d.Steps) != len(want) {
		t.Fatalf("b-debug có %d bước, muốn %d", len(d.Steps), len(want))
	}
	for i, w := range want {
		s := d.Steps[i]
		if s.ID != w.id || s.Kind != w.kind || s.Skill != w.skill || s.Fix != w.fix || s.Optional != w.opt || s.ExpectsFail() != w.exp {
			t.Errorf("bước %d = %+v, muốn %+v", i, s, w)
		}
	}
	for _, n := range []string{"b-do", "b-req", "b-feat", "b-review"} {
		if !Legacy(n) {
			t.Errorf("Legacy(%q) = false", n)
		}
	}
}

func TestValidateExpect(t *testing.T) {
	red := "verify: false}"
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{name: "expect fail ở implement không cuối", yaml: strings.Replace(tddYAML, red, "expect: fail}", 1)},
		{name: "giá trị lạ", want: `expect "pass" không hợp lệ`, yaml: strings.Replace(tddYAML, red, "expect: pass}", 1)},
		{name: "ở bước plan", want: "expect chỉ dùng được ở bước implement", yaml: strings.Replace(strings.Replace(tddYAML, red, "}", 1), "optional: true}", "optional: true, expect: fail}", 1)},
		{name: "ở bước review", want: "expect chỉ dùng được ở bước implement", yaml: strings.Replace(tddYAML, "fix: green}", "fix: green, expect: fail}", 1)},
		{name: "ở implement cuối", want: "expect: fail không được đặt ở bước implement cuối", yaml: strings.Replace(strings.Replace(tddYAML, red, "}", 1), "skill: b-feat}", "skill: b-feat, expect: fail}", 1)},
		{name: "cùng verify false", want: "không đi cùng verify: false", yaml: strings.Replace(tddYAML, red, "verify: false, expect: fail}", 1)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Bỏ ", }" thừa do thay thế rỗng.
			y := strings.ReplaceAll(tc.yaml, ", }", "}")
			d, err := Decode([]byte(y))
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			errs := Validate(d, "testfirst", testSkills)
			if tc.want == "" {
				if len(errs) != 0 {
					t.Fatalf("muốn hợp lệ, được %v", errs)
				}
				return
			}
			if !strings.Contains(strings.Join(errs, "; "), tc.want) {
				t.Fatalf("muốn lỗi chứa %q, được %v", tc.want, errs)
			}
		})
	}
}

func TestHashExpect(t *testing.T) {
	base := mustDecode(t, tddYAML)
	withExpect := mustDecode(t, strings.Replace(tddYAML, "verify: false}", "expect: fail}", 1))
	if Hash(base) == Hash(withExpect) {
		t.Error("đặt expect không đổi băm")
	}
}

// Ngoại lệ của flow nhúng chỉ dành cho flow nhúng: cùng nội dung nhưng đến từ tệp thì bị từ chối.
func TestBuiltinExemptionsDoNotLeakToCustom(t *testing.T) {
	core := skillSet("b-req", "b-feat", "b-review")
	d, _ := GetBuiltin("b-req")
	d.builtin = false
	if errs := Validate(d, "", core); len(errs) < 2 {
		t.Errorf("flow b-req không cờ builtin phải bị từ chối cả vì tên lẫn thiếu implement, được %v", errs)
	}
}

func TestBuiltinReturnsFreshCopy(t *testing.T) {
	a := Builtin()
	a[0].Steps[0].ID = "hỏng"
	a[0].Steps = append(a[0].Steps, Step{ID: "x"})
	b := Builtin()
	if b[0].Steps[0].ID != "b-req" || len(b[0].Steps) != 4 {
		t.Fatalf("Builtin chia sẻ trạng thái giữa các lần gọi: %+v", b[0].Steps)
	}
	if d, ok := GetBuiltin("b-feat"); !ok || d.Steps[0].Role != "implementer" {
		t.Fatalf("GetBuiltin(b-feat) = %+v, %v", d, ok)
	}
	if _, ok := GetBuiltin("testfirst"); ok {
		t.Fatal("testfirst không phải flow nhúng")
	}
}

func TestHash(t *testing.T) {
	base := mustDecode(t, tddYAML)
	h := Hash(base)
	if !strings.HasPrefix(h, "sha256:") || len(h) != len("sha256:")+64 {
		t.Fatalf("dạng băm: %q", h)
	}

	// Đổi chú thích, thứ tự khoá, mô tả, cách viết (flow/block), role mặc định viết tường minh: không đổi băm.
	same := `
name: testfirst
steps:
  - kind: plan
    id: plan
    optional: true
    skill: b-req
    role: planner
  - {skill: write-failing-test, id: red, kind: implement, role: test-writer, verify: false}  # đỏ
  - {id: green, kind: implement, skill: b-feat, verify: true}
  - {id: review, kind: review, skill: b-review, fix: green}
description: mô tả khác hẳn
schema: 1
`
	if got := Hash(mustDecode(t, same)); got != h {
		t.Errorf("đổi chú thích/thứ tự khoá làm đổi băm: %s != %s", got, h)
	}

	for name, y := range map[string]string{
		"đổi skill":   strings.Replace(tddYAML, "write-failing-test", "viet-test", 1),
		"đổi role":    strings.Replace(tddYAML, "test-writer", "tester", 1),
		"đổi verify":  strings.Replace(tddYAML, "verify: false", "verify: true", 1),
		"đổi fix":     strings.Replace(tddYAML, "fix: green", "fix: red", 1),
		"bỏ optional": strings.Replace(tddYAML, ", optional: true", "", 1),
		"đổi tên":     strings.Replace(tddYAML, "name: testfirst", "name: tdd2", 1),
	} {
		if Hash(mustDecode(t, y)) == h {
			t.Errorf("%s: băm không đổi", name)
		}
	}
	// Hoán vị hai bước implement.
	d := mustDecode(t, tddYAML)
	d.Steps[1], d.Steps[2] = d.Steps[2], d.Steps[1]
	if Hash(d) == h {
		t.Error("hoán vị bước không đổi băm")
	}
}

func TestNormalizeAndMarshal(t *testing.T) {
	d := mustDecode(t, tddYAML)
	n := Normalize(d)
	if n.Steps[0].Role != "planner" || n.Steps[2].Role != "implementer" || n.Steps[3].Role != "reviewer" || n.Steps[1].Role != "test-writer" {
		t.Errorf("role sau Normalize: %+v", n.Steps)
	}
	if d.Steps[0].Role != "" {
		t.Error("Normalize sửa bản gốc")
	}
	*n.Steps[1].Verify = true
	if d.Steps[1].Verify == nil || *d.Steps[1].Verify {
		t.Error("Normalize chia sẻ con trỏ Verify với bản gốc")
	}
	b, err := Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Decode(b)
	if err != nil {
		t.Fatalf("YAML xuất ra không giải mã lại được: %v\n%s", err, b)
	}
	if Hash(back) != Hash(d) {
		t.Errorf("vòng xuất/nạp đổi băm\n%s", b)
	}
	if !strings.Contains(string(b), "role: planner") {
		t.Errorf("YAML chuẩn hoá thiếu role mặc định:\n%s", b)
	}
}

func writeFlow(t *testing.T, root, name, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(Rel), name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoad(t *testing.T) {
	root := t.TempDir()
	if files, err := Load(root); err != nil || len(files) != 0 {
		t.Fatalf("thư mục không có: %v, %v", files, err)
	}
	writeFlow(t, root, "testfirst.yml", tddYAML)
	writeFlow(t, root, "hong.yml", "schema: [oops\n")
	writeFlow(t, root, "la.yml", "schema: 1\nname: la\nlalala: 1\nsteps: []\n")
	writeFlow(t, root, "cu.yaml", tddYAML)
	writeFlow(t, root, "README.md", "bỏ qua")
	writeFlow(t, root, ".an.yml", "bỏ qua")
	writeFlow(t, root, "lon.yml", "# "+strings.Repeat("x", MaxFileBytes)+"\n"+tddYAML)

	files, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]File{}
	var names []string
	for _, f := range files {
		byName[f.Name] = f
		names = append(names, f.Name)
	}
	if strings.Join(names, ",") != "cu,hong,la,lon,testfirst" {
		t.Fatalf("tệp nạp được: %v", names)
	}
	if byName["testfirst"].Err != nil || byName["testfirst"].Def.Name != "testfirst" || byName["testfirst"].Path != ".bint/flows/testfirst.yml" {
		t.Errorf("testfirst: %+v", byName["testfirst"])
	}
	for _, n := range []string{"hong", "la", "cu", "lon"} {
		if byName[n].Err == nil {
			t.Errorf("%s: muốn lỗi nạp riêng", n)
		}
	}
	if e := byName["cu"].Err; e == nil || !strings.Contains(e.Error(), ".yml") {
		t.Errorf("cu.yaml: lỗi %v phải nhắc đuôi .yml", e)
	}
}

func TestLoadRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	writeFlow(t, root, "that.yml", tddYAML)
	link := filepath.Join(root, filepath.FromSlash(Rel), "testfirst.yml")
	if err := os.Symlink(filepath.Join(root, filepath.FromSlash(Rel), "that.yml"), link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("không tạo được symlink: %v", err)
		}
		t.Fatal(err)
	}
	files, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.Name == "testfirst" && f.Err == nil {
			t.Error("symlink phải bị từ chối")
		}
		if f.Name == "that" && f.Err != nil {
			t.Errorf("tệp thường bị từ chối: %v", f.Err)
		}
	}
}

func TestGet(t *testing.T) {
	root := t.TempDir()
	writeFlow(t, root, "testfirst.yml", tddYAML)
	writeFlow(t, root, "hong.yml", "schema: [oops\n")
	// Flow nhúng được tra trước, kể cả khi có tệp trùng tên.
	writeFlow(t, root, "b-do.yml", strings.Replace(tddYAML, "name: testfirst", "name: b-do", 1))

	if d, err := Get(root, "b-do"); err != nil || !d.IsBuiltin() {
		t.Errorf("Get(b-do) = %+v, %v; muốn flow nhúng", d, err)
	}
	if d, err := Get(root, "testfirst"); err != nil || d.Name != "testfirst" || d.IsBuiltin() {
		t.Errorf("Get(testfirst) = %+v, %v", d, err)
	}
	if _, err := Get(root, "hong"); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("Get(hong): muốn lỗi nạp (không phải ErrNotFound), được %v", err)
	}
	for _, n := range []string{"khong-co", "", "../x", `a\b`, ".an"} {
		if _, err := Get(root, n); !errors.Is(err, ErrNotFound) {
			t.Errorf("Get(%q): muốn ErrNotFound, được %v", n, err)
		}
	}
}

func TestWarnings(t *testing.T) {
	d := mustDecode(t, strings.Replace(tddYAML, "fix: green", "fix: green, verify: true", 1))
	w := Warnings(d)
	if len(w) != 1 || !strings.Contains(w[0], "review") {
		t.Errorf("Warnings = %v", w)
	}
	if len(Warnings(mustDecode(t, tddYAML))) != 0 {
		t.Error("flow mẫu không được có cảnh báo")
	}
}

// Kind goal chỉ dành cho flow nhúng b-do: flow tuỳ biến dùng kind này bị từ chối.
func TestGoalKindOnlyForBuiltinDo(t *testing.T) {
	core := skillSet("b-req", "b-goal", "b-feat", "b-review", "b-do")
	custom := Def{Schema: Schema, Name: "testfirst-goal", Steps: []Step{
		{ID: "mine-goal", Kind: KindGoal, Skill: "b-goal"},
		{ID: "mine-feat", Kind: KindImplement, Skill: "b-feat"},
	}}
	errs := Validate(custom, "", core)
	found := false
	for _, e := range errs {
		if strings.Contains(e, "kind goal chỉ dành cho flow nhúng") {
			found = true
		}
	}
	if !found {
		t.Errorf("flow tuỳ biến dùng kind goal phải bị từ chối, được %v", errs)
	}
}
