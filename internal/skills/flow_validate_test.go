package skills

import (
	"os"
	"testing"

	"bint/internal/flow"
)

// tdd là flow tuỳ biến mẫu của các test GĐ5: lập kế hoạch, làm, review. Skill của bước lấy từ skill gốc.
func tddFlow(name string) flow.Def {
	f := false // verify: false ở bước implement đầu (không phải cuối) hợp lệ
	return flow.Def{Schema: flow.Schema, Name: name, Description: "Viết test trước rồi mã",
		Steps: []flow.Step{
			{ID: "plan", Kind: flow.KindPlan, Skill: "b-req"},
			{ID: "scaffold", Kind: flow.KindImplement, Skill: "b-feat", Verify: &f},
			{ID: "impl", Kind: flow.KindImplement, Skill: "b-feat"},
			{ID: "rev", Kind: flow.KindReview, Skill: "b-review", Fix: "impl"},
		}}
}

func TestValidateFlowTable(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(root string)
		flow    string
		wantErr string // rỗng = hợp lệ
	}{
		{name: "hợp lệ", flow: "tdd-flow"},
		{name: "trùng skill gốc", flow: "b-do", wantErr: "b-"},
		{name: "trùng skill tuỳ biến hợp lệ", flow: "mine", wantErr: "trùng tên một skill",
			setup: func(root string) { writeCustom(t, root, "mine", "SKILL.md", goodSkill("mine")) }},
		{name: "trùng skill tuỳ biến lỗi lint", flow: "broken", wantErr: "skill tuỳ biến hoặc skill tiến hoá",
			setup: func(root string) { writeCustom(t, root, "broken", "SKILL.md", "không có frontmatter\n") }},
		{name: "trùng skill tiến hoá", flow: "core-retry", wantErr: "skill tuỳ biến hoặc skill tiến hoá",
			setup: func(root string) {
				if err := newEvolved("core-retry", goodDesc, "1. Gọi Reset.").Save(root); err != nil {
					t.Fatal(err)
				}
			}},
		{name: "tên dành riêng", flow: "overlays", wantErr: "dành riêng",
			setup: func(root string) { os.MkdirAll(root+"/.bint/skills/overlays", 0o755) }},
		{name: "tên sai cú pháp chỉ có lỗi cú pháp", flow: "Ab", wantErr: "không hợp lệ"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			if c.setup != nil {
				c.setup(root)
			}
			errs := ValidateFlow(root, tddFlow(c.flow), c.flow)
			if c.wantErr == "" {
				if len(errs) != 0 {
					t.Fatalf("muốn hợp lệ: %v", errs)
				}
				return
			}
			if !hasErr(errs, c.wantErr) {
				t.Fatalf("muốn lỗi chứa %q: %v", c.wantErr, errs)
			}
		})
	}
}

func TestUsableInFlow(t *testing.T) {
	root := t.TempDir()
	writeCustom(t, root, "good", "SKILL.md", goodSkill("good"))
	writeCustom(t, root, "bad", "SKILL.md", "hỏng\n")
	if err := newEvolved("core-retry", goodDesc, "1. Gọi Reset.").Save(root); err != nil {
		t.Fatal(err)
	}
	ok := UsableInFlow(root)
	for name, want := range map[string]bool{"b-feat": true, "good": true, "bad": false, "core-retry": false, "nope": false} {
		if ok(name) != want {
			t.Errorf("UsableInFlow(%q) = %v, muốn %v", name, !want, want)
		}
	}
}
