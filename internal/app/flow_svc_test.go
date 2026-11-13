package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"bint/internal/config"
	"bint/internal/flow"
)

const tddFlow = `schema: 1
name: tdd
description: "Dùng khi muốn viết kiểm thử đỏ trước khi sửa mã"
steps:
  - {id: plan,   kind: plan,      skill: b-req, optional: true}
  - {id: red,    kind: implement, skill: write-failing-test, role: test-writer, verify: false}
  - {id: green,  kind: implement, skill: b-feat}
  - {id: review, kind: review,    skill: b-review, fix: green}
`

func writeFlowFile(t *testing.T, root, name, content string) {
	t.Helper()
	p := filepath.Join(root, ".bint", "flows", name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Tên flow nhúng phải khớp danh sách workflow gốc của config và id bước của b-do khớp bước Runner đang dùng:
// flow không import config nên chỉ test này giữ hai nơi khỏi lệch nhau.
func TestBuiltinFlowsMatchConfigWorkflows(t *testing.T) {
	if !slices.Equal(flow.BuiltinNames(), config.WorkflowNames) {
		t.Fatalf("flow.BuiltinNames() = %v, config.WorkflowNames = %v", flow.BuiltinNames(), config.WorkflowNames)
	}
	do, _ := flow.GetBuiltin("b-do")
	var ids []string
	for _, s := range do.Steps {
		ids = append(ids, s.ID)
	}
	if strings.Join(ids, ",") != "b-req,b-feat,b-review" {
		t.Fatalf("bước của b-do: %v", ids)
	}
}

func TestFlowServices(t *testing.T) {
	root := sysRepo(t, InitOptions{})
	ctx := context.Background()

	// Chưa có flow tuỳ biến: list chỉ có bốn flow nhúng, đều qua lint; lint không tên rỗng.
	rows, err := ListFlows(ctx, root, FlowListOptions{})
	if err != nil || len(rows) != 4 {
		t.Fatalf("%v %+v", err, rows)
	}
	for i, r := range rows {
		if r.Source != FlowBuiltin || !r.OK || r.Name != flow.BuiltinNames()[i] || r.Problems == nil {
			t.Errorf("dòng nhúng %d: %+v", i, r)
		}
	}
	if rows[0].Steps != 3 || rows[1].Steps != 1 {
		t.Errorf("số bước: %+v", rows)
	}
	if res, err := LintFlows(ctx, root, FlowLintOptions{}); err != nil || len(res) != 0 {
		t.Fatalf("không flow tuỳ biến: %v %+v", err, res)
	}
	// Lint theo tên nhúng kiểm flow nhúng.
	if res, err := LintFlows(ctx, root, FlowLintOptions{Name: "b-do"}); err != nil || len(res) != 1 || !res[0].OK() || res[0].Source != FlowBuiltin {
		t.Fatalf("%v %+v", err, res)
	}

	// Flow tdd tham chiếu skill tuỳ biến chưa có → bị từ chối.
	writeFlowFile(t, root, "tdd.yml", tddFlow)
	res, err := LintFlows(ctx, root, FlowLintOptions{Name: "tdd"})
	if err != nil || len(res) != 1 || res[0].OK() || !strings.Contains(strings.Join(res[0].Errors, "\n"), "write-failing-test") {
		t.Fatalf("muốn lỗi skill không tồn tại: %v %+v", err, res)
	}

	// Skill tuỳ biến có nhưng lỗi lint → vẫn bị từ chối (skill lỗi không được xuất bản).
	sk, err := NewSkill(ctx, root, SkillNewOptions{Name: "write-failing-test"})
	if err != nil {
		t.Fatal(err)
	}
	good := read(t, sk.Abs)
	if err := os.WriteFile(sk.Abs, []byte("---\nname: write-failing-test\ndescription: x\n---\n\n[l](nope.md)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if res, _ = LintFlows(ctx, root, FlowLintOptions{Name: "tdd"}); res[0].OK() {
		t.Fatalf("skill tuỳ biến lỗi lint mà flow vẫn qua: %+v", res)
	}
	// Sửa skill → flow qua.
	if err := os.WriteFile(sk.Abs, []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	if res, _ = LintFlows(ctx, root, FlowLintOptions{Name: "tdd"}); !res[0].OK() || res[0].Source != FlowCustom || res[0].Path != ".bint/flows/tdd.yml" {
		t.Fatalf("flow tdd mẫu phải qua: %+v", res)
	}

	// Thêm flow hỏng: list vẫn có tdd, lint không tên trả cả hai, lỗi của tệp này không chặn tệp kia.
	writeFlowFile(t, root, "la.yml", "schema: 1\nname: la\nnhieu: 1\nsteps: []\n")
	writeFlowFile(t, root, "b-do.yml", strings.Replace(tddFlow, "name: tdd", "name: b-do", 1))
	rows, _ = ListFlows(ctx, root, FlowListOptions{})
	got := map[string]FlowRow{}
	for _, r := range rows {
		if r.Source == FlowCustom {
			got[r.Name] = r
		}
	}
	if len(rows) != 7 || !got["tdd"].OK || got["tdd"].Steps != 4 || got["la"].OK || got["b-do"].OK {
		t.Fatalf("list: %+v", rows)
	}
	all, err := LintFlows(ctx, root, FlowLintOptions{})
	if err != nil || len(all) != 3 || all[0].Name != "b-do" || all[1].Name != "la" || all[2].Name != "tdd" {
		t.Fatalf("%v %+v", err, all)
	}
	if all[0].OK() || all[1].OK() || !all[2].OK() {
		t.Fatalf("kết quả lint: %+v", all)
	}
	// Lint theo tên nhúng: flow nhúng (qua) rồi tệp b-do.yml trùng tên (lỗi), không che nhau.
	if r, _ := LintFlows(ctx, root, FlowLintOptions{Name: "b-do"}); len(r) != 2 || !r[0].OK() || r[0].Source != FlowBuiltin || r[1].OK() || r[1].Source != FlowCustom {
		t.Fatalf("%+v", r)
	}
	if _, err := LintFlows(ctx, root, FlowLintOptions{Name: "khong-co"}); err == nil || !IsUsage(err) {
		t.Fatalf("muốn lỗi cách dùng, được %v", err)
	}

	// Flow tên trùng skill tiến hoá/tuỳ biến lỗi.
	writeFlowFile(t, root, "write-failing-test.yml", strings.Replace(tddFlow, "name: tdd", "name: write-failing-test", 1))
	if r, _ := LintFlows(ctx, root, FlowLintOptions{Name: "write-failing-test"}); r[0].OK() || !strings.Contains(strings.Join(r[0].Errors, "\n"), "trùng tên một skill") {
		t.Fatalf("%+v", r)
	}
	// Skill tuỳ biến lỗi lint cùng tên flow: Validate không thấy (skill không "tồn tại") nên dịch vụ phải bắt.
	if err := os.WriteFile(sk.Abs, []byte("không có frontmatter"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r, _ := LintFlows(ctx, root, FlowLintOptions{Name: "write-failing-test"}); r[0].OK() || !strings.Contains(strings.Join(r[0].Errors, "\n"), "trùng tên một skill") {
		t.Fatalf("skill tuỳ biến lỗi cùng tên flow: %+v", r)
	}
}

func TestShowFlow(t *testing.T) {
	root := sysRepo(t, InitOptions{})
	ctx := context.Background()

	do, err := ShowFlow(ctx, root, FlowShowOptions{Name: "b-do"})
	if err != nil || do.Source != FlowBuiltin || do.Path != "" || len(do.Errors) != 0 || do.Errors == nil {
		t.Fatalf("%v %+v", err, do)
	}
	for _, want := range []string{"name: b-do", "id: b-req", "id: b-feat", "id: b-review", "fix: b-feat", "role: planner"} {
		if !strings.Contains(do.YAML, want) {
			t.Errorf("YAML của b-do thiếu %q:\n%s", want, do.YAML)
		}
	}
	if !strings.HasPrefix(do.Hash, "sha256:") || len(do.Def.Steps) != 3 {
		t.Errorf("%+v", do)
	}

	writeFlowFile(t, root, "tdd.yml", tddFlow)
	tdd, err := ShowFlow(ctx, root, FlowShowOptions{Name: "tdd"})
	if err != nil || tdd.Source != FlowCustom || tdd.Path != ".bint/flows/tdd.yml" {
		t.Fatalf("%v %+v", err, tdd)
	}
	if len(tdd.Errors) == 0 { // skill write-failing-test chưa có: vẫn in nhưng báo lỗi
		t.Errorf("muốn lỗi lint kèm theo: %+v", tdd)
	}
	if !strings.Contains(tdd.YAML, "role: test-writer") || !strings.Contains(tdd.YAML, "role: reviewer") {
		t.Errorf("YAML chuẩn hoá:\n%s", tdd.YAML)
	}

	writeFlowFile(t, root, "hong.yml", "schema: [oops\n")
	for _, name := range []string{"hong", "khong-co", ""} {
		if _, err := ShowFlow(ctx, root, FlowShowOptions{Name: name}); err == nil || !IsUsage(err) {
			t.Errorf("ShowFlow(%q): muốn lỗi cách dùng, được %v", name, err)
		}
	}
}

func TestFlowServicesNeedInit(t *testing.T) {
	dir := t.TempDir()
	if _, err := ListFlows(context.Background(), dir, FlowListOptions{}); err == nil {
		t.Fatal("thư mục chưa init phải lỗi")
	}
}
