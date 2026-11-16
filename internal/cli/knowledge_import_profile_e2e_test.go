package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"bint/internal/config"
	"bint/internal/iso"
	"bint/internal/knowledge/importer"
	"bint/internal/testutil"
)

// refsOf chạy `knowledge import --dry-run --json` và trả tập Ref các ý (đã sắp xếp).
func importRefs(t *testing.T, root string, args ...string) []string {
	t.Helper()
	out, code := run(t, root, "", append([]string{"knowledge", "import", "--dry-run", "--json"}, args...)...)
	var res importJSON
	if code != 0 || json.Unmarshal([]byte(out), &res) != nil {
		t.Fatalf("import %v: %d\n%s", args, code, out)
	}
	var refs []string
	for _, i := range res.Plan.Ideas {
		refs = append(refs, i.Ref)
	}
	sort.Strings(refs)
	return refs
}

func writeMemory(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// `knowledge import --profile`: đọc bộ nhớ trong HOME giả của profile (codex/work hoặc chỉ work); không cờ thì profile của phiên khi
// cùng tool, ngược lại gốc thật; --profile với nguồn khác hoặc tool không khớp là lỗi cách dùng.
func TestKnowledgeImportProfile(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	root := testutil.NewRepo(t, map[string]string{
		"go.mod":            "module demo\n",
		"internal/db/db.go": "package db\n\nfunc OpenStore() {}\n",
	})
	testutil.Git(t, root, "config", "user.name", "Rev")
	testutil.Git(t, root, "config", "user.email", "rev@example.com")
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	realHome, _ := os.UserHomeDir()

	st, err := iso.OpenStore(config.Default().Iso, os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	pw, err := st.Create(iso.Codex, "work")
	if err != nil {
		t.Fatal(err)
	}
	pc, err := st.Create(iso.Claude, "work")
	if err != nil {
		t.Fatal(err)
	}
	const body = "Luôn mở SQLite bằng `OpenStore` trong internal/db/db.go, đừng tự gọi sql.Open.\n"
	writeMemory(t, filepath.Join(realHome, ".codex", "memories"), "real.md", body)
	writeMemory(t, filepath.Join(pw.ConfigDir(), "memories"), "inprofile.md", body+"(profile codex)\n")
	top := strings.TrimSpace(testutil.Git(t, root, "rev-parse", "--show-toplevel"))
	slug := filepath.Join("projects", importer.SlugOf(filepath.FromSlash(top)), "memory")
	writeMemory(t, filepath.Join(pc.ConfigDir(), slug), "claude_in_profile.md", "---\nname: x\ndescription: d\ntype: feedback\n---\n\n"+body)

	real := []string{"codex-memory:real.md"}
	prof := []string{"codex-memory:inprofile.md"}
	eq := func(got, want []string, what string) {
		t.Helper()
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("%s: %v, muốn %v", what, got, want)
		}
	}
	eq(importRefs(t, root, "--from", "codex-memory"), real, "không cờ, ngoài profile: gốc thật")
	eq(importRefs(t, root, "--from", "codex-memory", "--profile", "work"), prof, "--profile work")
	eq(importRefs(t, root, "--from", "codex-memory", "--profile", "codex/work"), prof, "--profile codex/work")
	eq(importRefs(t, root, "--from", "claude-memory", "--profile", "claude/work"), []string{"claude-memory:claude_in_profile.md"}, "claude-memory --profile")
	eq(importRefs(t, root, "--from", "claude-memory", "--profile", "work"), []string{"claude-memory:claude_in_profile.md"}, "claude-memory --profile tên trần")

	// Phiên ở profile cùng tool: không cờ thì đọc profile của phiên (HOME/CODEX_HOME của tiến trình).
	t.Setenv("HOME", pw.Home)
	t.Setenv("USERPROFILE", pw.Home)
	t.Setenv("CODEX_HOME", pw.ConfigDir())
	t.Setenv("BINT_ISO_PROFILE", "codex/work")
	t.Setenv("BINT_ISO_REAL_HOME", realHome)
	t.Setenv("BINT_ISO_REAL_USERPROFILE", realHome)
	eq(importRefs(t, root, "--from", "codex-memory"), prof, "phiên codex/work, không cờ")
	// Có cờ vẫn theo cờ; phiên codex mà nhập claude-memory không cờ: gốc thật (chưa có bộ nhớ Claude thật).
	eq(importRefs(t, root, "--from", "codex-memory", "--profile", "codex/work"), prof, "phiên codex/work, có cờ")

	// Phiên ở profile khác tool: nguồn codex-memory đọc gốc thật.
	t.Setenv("HOME", pc.Home)
	t.Setenv("USERPROFILE", pc.Home)
	t.Setenv("CODEX_HOME", "")
	t.Setenv("CLAUDE_CONFIG_DIR", pc.ConfigDir())
	t.Setenv("BINT_ISO_PROFILE", "claude/work")
	eq(importRefs(t, root, "--from", "codex-memory"), real, "phiên claude/work, nguồn codex: gốc thật")
	t.Setenv("BINT_ISO_PROFILE", "")
	t.Setenv("HOME", realHome)
	t.Setenv("USERPROFILE", realHome)
	t.Setenv("CLAUDE_CONFIG_DIR", "")

	// Lỗi cách dùng (mã 2): nguồn không có profile, tool không khớp, tên sai; profile chưa tạo là lỗi nêu bint iso create.
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--from", "cursor-rules", "--profile", "work"}, "--profile chỉ dùng với"},
		{[]string{"--from", "codex-memory", "--profile", "claude/work"}, "là profile của claude"},
		{[]string{"--from", "codex-memory", "--profile", "a/b/c"}, "--profile"},
		{[]string{"--from", "codex-memory", "--profile", ".."}, "--profile"},
		{[]string{"--from", "codex-memory", "--profile", "ghost"}, "bint iso create codex --name ghost"},
	} {
		out, code := run(t, root, "", append([]string{"knowledge", "import", "--dry-run"}, tc.args...)...)
		if code == 0 || !strings.Contains(out, tc.want) {
			t.Errorf("%v: code=%d, muốn lỗi chứa %q\n%s", tc.args, code, tc.want, out)
		}
	}
}
