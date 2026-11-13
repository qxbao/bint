package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/testutil"
)

// Skill tuỳ biến từ đầu tới cuối: new → lint → sync → list, kèm tệp phụ, lỗi lint (thoát 1) và việc không ghi đè.
func TestCustomSkillEndToEnd(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}

	// Skill gốc b-add-skill có trong danh sách và được xuất bản.
	out, _ := run(t, root, "", "skill", "list", "--json")
	var rows []struct {
		Name, Kind string
		Files      int
	}
	if json.Unmarshal([]byte(out), &rows) != nil {
		t.Fatalf("skill list --json: %s", out)
	}
	hasAdd := false
	for _, r := range rows {
		hasAdd = hasAdd || (r.Name == "b-add-skill" && r.Kind == "core")
	}
	if !hasAdd {
		t.Fatalf("thiếu b-add-skill: %s", out)
	}
	for _, d := range []string{".claude/skills", ".agents/skills"} {
		if _, err := os.Stat(filepath.Join(root, d, "b-add-skill", "SKILL.md")); err != nil {
			t.Fatalf("b-add-skill chưa xuất bản ở %s: %v", d, err)
		}
	}

	// new → lint thoát 0 (mẫu qua lint) → sync xuất bản.
	if out, code := run(t, root, "", "skill", "new", "demo-skill"); code != 0 || !strings.Contains(out, "bint skill lint demo-skill") {
		t.Fatalf("skill new: %d\n%s", code, out)
	}
	src := filepath.Join(root, ".bint", "skills", "demo-skill", "SKILL.md")
	tpl, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if out, code := run(t, root, "", "skill", "lint", "demo-skill"); code != 0 || !strings.Contains(out, "✓") {
		t.Fatalf("lint mẫu: %d\n%s", code, out)
	}
	// Lần hai không ghi đè.
	os.WriteFile(src, append(tpl, []byte("\nThêm.\n")...), 0o644)
	if out, code := run(t, root, "", "skill", "new", "demo-skill"); code != 0 || !strings.Contains(out, "không ghi đè") {
		t.Fatalf("skill new lần hai: %d\n%s", code, out)
	}
	if b, _ := os.ReadFile(src); !strings.HasSuffix(string(b), "Thêm.\n") {
		t.Fatal("skill new đã ghi đè")
	}
	// Tên sai là lỗi cách dùng.
	for _, bad := range []string{"b-mine", "Demo", "overlays", "b-do"} {
		if out, code := run(t, root, "", "skill", "new", bad); code == 0 {
			t.Fatalf("skill new %s phải lỗi:\n%s", bad, out)
		}
	}
	if out, code := run(t, root, "", "skill", "lint", "b-do"); code != 2 || !strings.Contains(out, "skill gốc") {
		t.Fatalf("lint skill gốc phải là lỗi cách dùng: %d\n%s", code, out)
	}

	// Tệp phụ: xuất bản cùng; tests/ thì không.
	os.MkdirAll(filepath.Join(root, ".bint", "skills", "demo-skill", "references"), 0o755)
	os.MkdirAll(filepath.Join(root, ".bint", "skills", "demo-skill", "tests"), 0o755)
	os.WriteFile(filepath.Join(root, ".bint", "skills", "demo-skill", "references", "a.md"), []byte("tham chiếu\n"), 0o644)
	os.WriteFile(filepath.Join(root, ".bint", "skills", "demo-skill", "tests", "t.yml"), []byte("x\n"), 0o644)
	if out, code := run(t, root, "", "sync"); code != 0 {
		t.Fatalf("sync: %d\n%s", code, out)
	}
	for _, d := range []string{".claude/skills", ".agents/skills"} {
		for _, f := range []string{"SKILL.md", "references/a.md"} {
			if _, err := os.Stat(filepath.Join(root, d, "demo-skill", filepath.FromSlash(f))); err != nil {
				t.Fatalf("%s/demo-skill/%s: %v", d, f, err)
			}
		}
		if _, err := os.Stat(filepath.Join(root, d, "demo-skill", "tests")); err == nil {
			t.Fatalf("tests/ không được xuất bản ở %s", d)
		}
	}
	out, _ = run(t, root, "", "skill", "list", "--json")
	var found bool
	json.Unmarshal([]byte(out), &rows)
	for _, r := range rows {
		if r.Name == "demo-skill" && r.Kind == "custom" && r.Files == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("skill list thiếu dòng custom:\n%s", out)
	}
	if out, _ := run(t, root, "", "skill", "show", "demo-skill"); !strings.Contains(out, "name: demo-skill") {
		t.Fatalf("skill show:\n%s", out)
	}
	if b, _ := os.ReadFile(filepath.Join(root, ".bint", "skills", "core.lock")); !strings.Contains(string(b), "custom:") {
		t.Fatalf("core.lock thiếu mục custom:\n%s", b)
	}

	// Lỗi lint → thoát 1, sync báo xung đột và giữ bản đã xuất bản.
	os.WriteFile(src, []byte("---\nname: khac\ndescription: \"\"\nallowed-tools: Bash\n---\n\n[x](missing.md)\n"), 0o644)
	out, code := run(t, root, "", "skill", "lint", "demo-skill", "--json")
	if code != 1 {
		t.Fatalf("lint skill lỗi phải thoát 1, được %d\n%s", code, out)
	}
	for _, want := range []string{"khác tên thư mục", "description rỗng", "allowed-tools", "missing.md"} {
		if !strings.Contains(out, want) {
			t.Errorf("lint thiếu %q:\n%s", want, out)
		}
	}
	if out, code := run(t, root, "", "sync"); code != 1 || !strings.Contains(out, "demo-skill") {
		t.Fatalf("sync skill lỗi phải xung đột: %d\n%s", code, out)
	}
	if b, _ := os.ReadFile(filepath.Join(root, ".claude", "skills", "demo-skill", "SKILL.md")); !strings.Contains(string(b), "name: demo-skill") {
		t.Fatalf("phải giữ bản đã xuất bản:\n%s", b)
	}
	if out, _ := run(t, root, "", "doctor"); !strings.Contains(out, "skill demo-skill") {
		t.Fatalf("doctor phải nhắc skill lỗi:\n%s", out)
	}

	// Xoá skill → gỡ bản đã xuất bản.
	os.RemoveAll(filepath.Join(root, ".bint", "skills", "demo-skill"))
	if out, code := run(t, root, "", "sync"); code != 0 {
		t.Fatalf("sync sau khi xoá: %d\n%s", code, out)
	}
	for _, d := range []string{".claude/skills", ".agents/skills"} {
		if _, err := os.Stat(filepath.Join(root, d, "demo-skill")); err == nil {
			t.Fatalf("%s/demo-skill phải được gỡ", d)
		}
	}
}
