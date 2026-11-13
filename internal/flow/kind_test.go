package flow

import (
	"errors"
	"strings"
	"testing"
)

func TestKindHelpers(t *testing.T) {
	do, _ := GetBuiltin("b-do")
	req, _ := GetBuiltin("b-req")
	feat, _ := GetBuiltin("b-feat")
	rev, _ := GetBuiltin("b-review")
	tests := []struct {
		d                                   Def
		multi, plan, impl, review, planOnly bool
		optional                            bool
	}{
		{do, true, true, true, true, false, true},
		{req, false, true, false, false, true, false},
		{feat, false, false, true, false, false, false},
		{rev, false, false, false, true, false, false},
		{Def{}, false, false, false, false, false, false},
	}
	for _, c := range tests {
		if c.d.Multi() != c.multi || c.d.HasKind(KindPlan) != c.plan || c.d.HasKind(KindImplement) != c.impl ||
			c.d.HasKind(KindReview) != c.review || c.d.OnlyKind(KindPlan) != c.planOnly || c.d.PlanOptional() != c.optional {
			t.Errorf("%q: multi=%v plan=%v impl=%v review=%v planOnly=%v optional=%v", c.d.Name, c.d.Multi(), c.d.HasKind(KindPlan),
				c.d.HasKind(KindImplement), c.d.HasKind(KindReview), c.d.OnlyKind(KindPlan), c.d.PlanOptional())
		}
	}
	if s, ok := do.FirstOfKind(KindImplement); !ok || s.ID != "b-feat" {
		t.Errorf("FirstOfKind(implement) của b-do = %+v, %v", s, ok)
	}
	if _, ok := req.FirstOfKind(KindReview); ok {
		t.Error("b-req không có bước review")
	}
	tdd := mustDecode(t, tddYAML)
	if s, _ := tdd.FirstOfKind(KindImplement); s.ID != "red" {
		t.Errorf("bước implement đầu của tdd = %q, muốn red", s.ID)
	}
}

func TestBuiltinStep(t *testing.T) {
	for id, want := range map[string]struct {
		kind Kind
		role string
	}{
		"b-req":    {KindPlan, "planner"},
		"b-feat":   {KindImplement, "implementer"},
		"b-review": {KindReview, "reviewer"},
	} {
		s, ok := BuiltinStep(id)
		if !ok || s.Kind != want.kind || s.EffectiveRole() != want.role || s.Skill != id {
			t.Errorf("BuiltinStep(%q) = %+v, %v", id, s, ok)
		}
	}
	for _, id := range []string{"", "b-do", "post-feat", "verify", "plan", "bogus"} {
		if _, ok := BuiltinStep(id); ok {
			t.Errorf("BuiltinStep(%q) phải không có", id)
		}
	}
}

// Phát hiện review GĐ2 .1: skillExists=nil nghĩa là mọi skill tồn tại, nhưng không được suy ra "tên flow trùng tên skill".
func TestValidateNilSkillExistsAcceptsCustomFlow(t *testing.T) {
	d := mustDecode(t, tddYAML)
	if errs := Validate(d, "tdd", nil); len(errs) != 0 {
		t.Fatalf("Validate(tdd, nil) phải hợp lệ, được %v", errs)
	}
	// Có skillExists thật mà tên flow trùng một skill thì vẫn bị báo.
	errs := Validate(d, "tdd", skillSet("b-req", "b-feat", "b-review", "write-failing-test", "tdd"))
	if len(errs) != 1 || !strings.Contains(errs[0], "trùng tên một skill") {
		t.Fatalf("tên trùng skill phải bị báo, được %v", errs)
	}
}

// Phát hiện review GĐ2 .2: tdd.yml và tdd.yaml cùng có.
func TestLoadYmlAndYamlSameName(t *testing.T) {
	root := t.TempDir()
	writeFlow(t, root, "tdd.yaml", tddYAML)
	writeFlow(t, root, "tdd.yml", tddYAML)
	writeFlow(t, root, "abc.yml", strings.Replace(tddYAML, "name: tdd", "name: abc", 1))

	for range 3 { // thứ tự xác định, không phụ thuộc thứ tự thư mục
		files, err := Load(root)
		if err != nil {
			t.Fatal(err)
		}
		var paths []string
		for _, f := range files {
			paths = append(paths, f.Path)
		}
		if got := strings.Join(paths, ","); got != ".bint/flows/abc.yml,.bint/flows/tdd.yml,.bint/flows/tdd.yaml" {
			t.Fatalf("thứ tự Load: %s", got)
		}
		if files[1].Err != nil {
			t.Errorf("tdd.yml: %v", files[1].Err)
		}
		if e := files[2].Err; e == nil || !strings.Contains(e.Error(), "trùng") || !strings.Contains(e.Error(), "tdd.yml") {
			t.Errorf("tdd.yaml phải báo trùng với tdd.yml, được %v", e)
		}
	}
	f, err := Find(root, "tdd")
	if err != nil || f.Err != nil || f.Path != ".bint/flows/tdd.yml" || f.Def.Name != "tdd" {
		t.Fatalf("Find(tdd) = %+v, %v; muốn bản .yml", f, err)
	}
	if d, err := Get(root, "tdd"); err != nil || d.Name != "tdd" {
		t.Fatalf("Get(tdd) = %+v, %v; muốn bản .yml", d, err)
	}
}

func TestFind(t *testing.T) {
	root := t.TempDir()
	writeFlow(t, root, "tdd.yml", tddYAML)
	writeFlow(t, root, "hong.yml", "schema: [oops\n")
	writeFlow(t, root, "cu.yaml", tddYAML)

	if f, err := Find(root, "b-do"); err != nil || !f.Def.IsBuiltin() || f.Path != "" || f.Name != "b-do" {
		t.Errorf("Find(b-do) = %+v, %v", f, err)
	}
	if f, err := Find(root, "tdd"); err != nil || f.Err != nil || f.Path != ".bint/flows/tdd.yml" {
		t.Errorf("Find(tdd) = %+v, %v", f, err)
	}
	// Tệp hỏng: Find trả tệp kèm Err riêng (không phải lỗi tra cứu), Get biến nó thành lỗi.
	if f, err := Find(root, "hong"); err != nil || f.Err == nil || f.Path != ".bint/flows/hong.yml" {
		t.Errorf("Find(hong) = %+v, %v", f, err)
	}
	if f, err := Find(root, "cu"); err != nil || f.Err == nil || !strings.Contains(f.Err.Error(), ".yml") {
		t.Errorf("Find(cu) = %+v, %v", f, err)
	}
	for _, n := range []string{"khong-co", "", "../x", `a\b`, ".an"} {
		if _, err := Find(root, n); !errors.Is(err, ErrNotFound) {
			t.Errorf("Find(%q): muốn ErrNotFound, được %v", n, err)
		}
	}
}
