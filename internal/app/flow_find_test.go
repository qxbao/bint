package app

import (
	"context"
	"strings"
	"testing"

	"bint/internal/flow"
)

// Phát hiện review GĐ2 .3: findFlow dùng chung đường tra với flow.Get/Find; không có flow thì lỗi cách dùng.
func TestFindFlowSharesLookupWithFlowGet(t *testing.T) {
	root := t.TempDir()
	writeFlowFile(t, root, "tdd.yml", tddFlow)
	writeFlowFile(t, root, "tdd.yaml", tddFlow)
	writeFlowFile(t, root, "hong.yml", "schema: [oops\n")

	d, file, path, err := findFlow(root, "b-do")
	if err != nil || !d.IsBuiltin() || file != "" || path != "" {
		t.Fatalf("findFlow(b-do) = %+v %q %q %v", d, file, path, err)
	}
	// Cả tdd.yml và tdd.yaml cùng có: luôn chọn bản .yml, giống flow.Get.
	d, file, path, err = findFlow(root, "tdd")
	want, gerr := flow.Get(root, "tdd")
	if err != nil || gerr != nil || file != "tdd" || path != ".bint/flows/tdd.yml" || d.Name != want.Name || flow.Hash(d) != flow.Hash(want) {
		t.Fatalf("findFlow(tdd) = %+v %q %q %v (flow.Get: %v)", d, file, path, err, gerr)
	}
	if _, _, _, err = findFlow(root, "khong-co"); !IsUsage(err) || !strings.Contains(err.Error(), `không có flow "khong-co"`) ||
		!strings.Contains(err.Error(), "bint flow list") {
		t.Fatalf("flow không có: muốn lỗi cách dùng nhắc `bint flow list`, được %v", err)
	}
	if _, _, _, err = findFlow(root, "hong"); !IsUsage(err) || !strings.Contains(err.Error(), ".bint/flows/hong.yml") {
		t.Fatalf("tệp hỏng: muốn lỗi cách dùng nêu đường dẫn, được %v", err)
	}
}

func TestFlowServicesWithYmlAndYamlSameName(t *testing.T) {
	root := sysRepo(t, InitOptions{})
	ctx := context.Background()
	writeFlowFile(t, root, "tdd.yaml", tddFlow)
	writeFlowFile(t, root, "tdd.yml", tddFlow)

	res, err := ShowFlow(ctx, root, FlowShowOptions{Name: "tdd"})
	if err != nil || res.Source != FlowCustom || res.Path != ".bint/flows/tdd.yml" {
		t.Fatalf("ShowFlow(tdd): %v %+v", err, res)
	}
	lint, err := LintFlows(ctx, root, FlowLintOptions{Name: "tdd"})
	if err != nil || len(lint) != 2 || lint[0].Path != ".bint/flows/tdd.yml" || lint[1].Path != ".bint/flows/tdd.yaml" {
		t.Fatalf("LintFlows(tdd): %v %+v", err, lint)
	}
	if len(lint[1].Errors) != 1 || !strings.Contains(lint[1].Errors[0], "trùng") {
		t.Fatalf("tdd.yaml phải báo trùng: %+v", lint[1])
	}
	rows, err := ListFlows(ctx, root, FlowListOptions{})
	if err != nil || len(rows) != 6 || rows[4].Path != ".bint/flows/tdd.yml" || rows[5].Path != ".bint/flows/tdd.yaml" || rows[5].OK {
		t.Fatalf("ListFlows: %v %+v", err, rows)
	}
}
