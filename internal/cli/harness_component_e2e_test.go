package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/testutil"
)

func TestHarnessComponentEndToEnd(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n", "src/a.go": "package a\n"})
	testutil.Git(t, root, "config", "user.name", "Rev")
	testutil.Git(t, root, "config", "user.email", "rev@example.com")
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}

	// Bảng adapter cũ giữ nguyên.
	if out, code := run(t, root, "", "harness", "list", "--no-version"); code != 0 || !strings.Contains(out, "Guardrail quy trình") {
		t.Fatalf("harness list: %d\n%s", code, out)
	}

	out, code := run(t, root, "", "harness", "component", "list")
	if code != 0 || !strings.Contains(out, "Chưa có thành phần") {
		t.Fatalf("list rỗng: %d\n%s", code, out)
	}
	out, code = run(t, root, "", "harness", "component", "add", "--template", "test-before-stop")
	if code != 0 || !strings.Contains(out, "candidate") {
		t.Fatalf("add template: %d\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(root, ".bint", "harness", "guardrails", "test-before-stop.yml")); err != nil {
		t.Fatal(err)
	}
	// Thêm lần hai: đã có.
	if out, code = run(t, root, "", "harness", "component", "add", "--template", "test-before-stop"); code == 0 || !strings.Contains(out, "đã có") {
		t.Fatalf("add trùng phải lỗi: %d\n%s", code, out)
	}

	// Guidance do người soạn + script với .sh cùng tên.
	tmp := t.TempDir()
	gfile := filepath.Join(tmp, "tips.md")
	os.WriteFile(gfile, []byte("---\nid: db-tips\nscope: \"domain:src/**\"\nstatus: verified\norigin: human\nversion: 1\n---\nVùng src: chạy gofmt.\n"), 0o644)
	if out, code = run(t, root, "", "harness", "component", "add", "--kind", "guidance", "--file", gfile); code != 0 {
		t.Fatalf("add guidance: %d\n%s", code, out)
	}
	sfile := filepath.Join(tmp, "run-it.yml")
	os.WriteFile(sfile, []byte("id: run-it\nscope: global\nstatus: verified\norigin: human\nversion: 1\ndescription: Chạy test đúng chỗ\n"), 0o644)
	os.WriteFile(filepath.Join(tmp, "run-it.sh"), []byte("#!/bin/sh\necho hi\n"), 0o755)
	if out, code = run(t, root, "", "harness", "component", "add", "--kind", "script", "--file", sfile); code != 0 {
		t.Fatalf("add script: %d\n%s", code, out)
	}
	// Dù tệp ghi verified, thành phần vẫn vào ở candidate.
	out, code = run(t, root, "", "harness", "component", "list", "--json")
	var lst struct {
		Components []struct{ Kind, ID, Status, Origin, Risk string }
	}
	if code != 0 || json.Unmarshal([]byte(out), &lst) != nil || len(lst.Components) != 3 {
		t.Fatalf("list --json: %d\n%s", code, out)
	}
	for _, c := range lst.Components {
		if c.Status != "candidate" || c.Origin != "human" {
			t.Fatalf("add luôn candidate/human: %+v", c)
		}
		if c.Kind == "script" && c.Risk != "high" {
			t.Fatalf("script luôn rủi ro cao: %+v", c)
		}
	}

	// render: candidate chưa có hiệu lực ở chế độ on.
	out, _ = run(t, root, "", "harness", "render", "--paths", "src/a.go")
	if strings.Contains(out, "test-before-stop [") || strings.Contains(out, "Vùng src") {
		t.Fatalf("candidate không được xuất hiện ở render on:\n%s", out)
	}

	// Agent không duyệt được.
	asAgent(t)
	out, code = run(t, root, "", "harness", "component", "approve", "test-before-stop")
	if code == 0 || !strings.Contains(out, "chỉ dành cho người") {
		t.Fatalf("agent bị chặn approve: %d\n%s", code, out)
	}
	if out, code = run(t, root, "", "harness", "component", "deprecate", "test-before-stop"); code == 0 || !strings.Contains(out, "chỉ dành cho người") {
		t.Fatalf("agent bị chặn deprecate: %d\n%s", code, out)
	}
	// Agent thêm được, nhưng origin=agent.
	gfile2 := filepath.Join(tmp, "agent.md")
	os.WriteFile(gfile2, []byte("---\nid: from-agent\nscope: global\nstatus: verified\norigin: human\nversion: 9\n---\nGhi bởi agent.\n"), 0o644)
	if out, code = run(t, root, "", "harness", "component", "add", "--kind", "guidance", "--file", gfile2); code != 0 {
		t.Fatalf("agent add: %d\n%s", code, out)
	}
	out, _ = run(t, root, "", "harness", "component", "show", "from-agent")
	if !strings.Contains(out, "candidate") || !strings.Contains(out, "nguồn agent") || !strings.Contains(out, "phiên bản 9") {
		t.Fatalf("agent add → candidate/agent:\n%s", out)
	}

	// Người duyệt.
	asHuman(t)
	for _, id := range []string{"test-before-stop", "db-tips", "run-it"} {
		if out, code = run(t, root, "", "harness", "component", "approve", id); code != 0 || !strings.Contains(out, "verified") {
			t.Fatalf("approve %s: %d\n%s", id, code, out)
		}
	}
	if out, code = run(t, root, "", "harness", "component", "approve", "db-tips"); code == 0 {
		t.Fatalf("duyệt lần hai phải lỗi:\n%s", out)
	}
	out, _ = run(t, root, "", "harness", "component", "show", "test-before-stop")
	if !strings.Contains(out, "verified") || !strings.Contains(out, "Rev <rev@example.com>") {
		t.Fatalf("show sau approve:\n%s", out)
	}

	// render theo harness: guardrail stop chạy ở Claude, không chạy ở Codex.
	out, _ = run(t, root, "", "harness", "render", "--paths", "src/a.go")
	for _, want := range []string{"Script hỗ trợ (Bint)", "run-it.sh", "db-tips (domain:src/**) — khớp: src/a.go", "Vùng src: chạy gofmt.",
		"test-before-stop [global] on=stop khai báo=remind → remind"} {
		if !strings.Contains(out, want) {
			t.Fatalf("render claude thiếu %q:\n%s", want, out)
		}
	}
	out, _ = run(t, root, "", "harness", "render", "--harness", "codex")
	if !strings.Contains(out, "không thực thi (harness không hỗ trợ)") {
		t.Fatalf("render codex: stop không thực thi:\n%s", out)
	}
	if out, code = run(t, root, "", "harness", "render", "--harness", "nope"); code == 0 {
		t.Fatalf("harness lạ phải lỗi:\n%s", out)
	}

	// deprecate.
	if out, code = run(t, root, "", "harness", "component", "deprecate", "run-it"); code != 0 || !strings.Contains(out, "deprecated") {
		t.Fatalf("deprecate: %d\n%s", code, out)
	}
	out, _ = run(t, root, "", "harness", "render")
	if strings.Contains(out, "run-it.sh") {
		t.Fatalf("deprecated không còn trong render:\n%s", out)
	}
}
