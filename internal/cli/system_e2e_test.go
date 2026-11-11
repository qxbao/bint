package cli_test

import (
	"fmt"
	"strings"
	"testing"

	"bint/internal/mcpserver"
	"bint/internal/skills"
	"bint/internal/testutil"
)

// Đầu ra `bint mcp tools` giống từng byte với cách in cũ (mcpserver.New + RegisterTools(nil), "%-20s %s\n").
func TestMCPToolsOutputByteIdentical(t *testing.T) {
	srv := mcpserver.New("bint", "x", "")
	mcpserver.RegisterTools(srv, nil)
	var want strings.Builder
	for _, tl := range srv.Tools() {
		fmt.Fprintf(&want, "%-20s %s\n", tl.Name, tl.Description)
	}
	out, code := run(t, t.TempDir(), "", "mcp", "tools")
	if code != 0 || out != want.String() {
		t.Fatalf("mã %d\n--- got\n%s--- want\n%s", code, out, want.String())
	}
}

// Lỗi dùng sai của nhóm lệnh đã tách dịch vụ (Validate) là mã thoát 2, kể cả khi repository chưa init.
func TestSystemCommandsUsageErrorsExitTwo(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	cases := [][]string{
		{"harness", "component", "add"},                                          // thiếu --template/--file
		{"harness", "component", "add", "--file", "x.md"},                        // --file cần --kind
		{"harness", "component", "add", "--kind", "nhảm", "--file", "x.md"},      // loại sai
		{"harness", "component", "add", "--template", "t", "--kind", "guidance"}, // --template loại trừ --kind
		{"harness", "component", "list", "--kind", "nhảm"},                       // loại sai
		{"harness", "component", "list", "--status", "nhảm"},                     // trạng thái sai
		{"harness", "component", "show", "--kind", "nhảm", "id"},                 // loại sai
		{"harness", "render", "--mode", "nhảm"},                                  // chế độ sai
		{"harness", "render", "--harness", "nhảm"},                               // harness sai
		{"harness", "show", "nhảm"},                                              // harness lạ
	}
	for _, args := range cases {
		if out, code := run(t, root, "", args...); code != 2 {
			t.Errorf("bint %s: mã %d, muốn 2\n%s", strings.Join(args, " "), code, out)
		}
	}
}

// `skill check` không đạt → mã thoát 1; đạt → 0; skill lạ → mã 1 (lỗi thường, không phải dùng sai).
func TestSkillCheckExitCodes(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n", "internal/core/x.go": "package core\n"})
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	mk := func(name, glob, desc string) {
		ev := &skills.Evolved{
			Meta: skills.EvolvedMeta{Schema: skills.EvolvedSchema, ID: "sk_" + name, Name: name, Status: skills.EvActive, Version: 1,
				Triggers: skills.Triggers{Paths: []string{glob}},
				Versions: []skills.VersionEntry{{N: 1, SHA256: "x", At: "2026-11-11T00:00:00.000Z"}}},
			Content: skills.BuildContent(name, desc, "1. Mở internal/core/x.go."),
		}
		if err := ev.Save(root); err != nil {
			t.Fatal(err)
		}
	}
	mk("khop-tep", "internal/core/**", "Dùng khi sửa Retry trong internal/core: gọi Reset trước khi thử lại.")
	mk("khong-khop", "khong/co/**", "Chuẩn hoá tên biến thể HTTP header ở lớp gateway ngoài cùng.")

	if out, code := run(t, root, "", "skill", "check", "khop-tep"); code != 0 || !strings.Contains(out, "khop-tep") {
		t.Fatalf("skill khớp tệp phải đạt: %d\n%s", code, out)
	}
	out, code := run(t, root, "", "skill", "check", "khong-khop")
	if code != 1 || !strings.Contains(out, "điều kiện kích hoạt không đạt") {
		t.Fatalf("skill có mẫu không khớp tệp nào phải mã 1: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "skill", "check", "khong-co-skill-nay"); code != 1 {
		t.Fatalf("skill lạ: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "skill", "check"); code == 0 {
		t.Fatalf("thiếu đối số phải lỗi: %d\n%s", code, out)
	}
}
