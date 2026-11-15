package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/knowledge/importer"
	"bint/internal/testutil"
)

type importJSON struct {
	DryRun  bool     `json:"dry_run"`
	Written bool     `json:"written"`
	IDs     []string `json:"ids"`
	Plan    struct {
		Source string `json:"source"`
		Batch  string `json:"batch"`
		Hash   string `json:"hash"`
		Ideas  []struct {
			Ref      string   `json:"ref"`
			Outcome  string   `json:"outcome"`
			Reason   string   `json:"reason"`
			Evidence []string `json:"evidence"`
		} `json:"ideas"`
	} `json:"plan"`
}

func countItems(t *testing.T, root string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, ".bint", "knowledge", "items"))
	if err != nil {
		return 0
	}
	return len(entries)
}

func TestKnowledgeImportEndToEnd(t *testing.T) {
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

	// Bộ nhớ Claude giả đặt theo đúng quy tắc tên thư mục dự án, dưới home giả của Isolate.
	home, _ := os.UserHomeDir()
	top := strings.TrimSpace(testutil.Git(t, root, "rev-parse", "--show-toplevel"))
	mem := filepath.Join(home, ".claude", "projects", importer.SlugOf(filepath.FromSlash(top)), "memory")
	if err := os.MkdirAll(mem, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"feedback_db.md": "---\nname: db\ndescription: Mở SQLite bằng OpenStore\ntype: feedback\n---\n\nLuôn mở SQLite bằng `OpenStore`, đừng tự gọi sql.Open.\n",
		"user_me.md":     "---\nname: me\ndescription: vai trò\ntype: user\n---\n\nTôi là kỹ sư dữ liệu dùng `OpenStore`.\n",
	} {
		if err := os.WriteFile(filepath.Join(mem, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// --dry-run: liệt kê từng ý kèm kết cục dự kiến, không ghi tệp nào.
	out, code := run(t, root, "", "knowledge", "import", "--from", "claude-memory", "--dry-run")
	if code != 0 || !strings.Contains(out, "tạo      claude-memory:feedback_db.md") || !strings.Contains(out, "bỏ qua   claude-memory:user_me.md") ||
		!strings.Contains(out, "[private]") || !strings.Contains(out, "Chưa ghi gì (--dry-run)") {
		t.Fatalf("dry-run: %d\n%s", code, out)
	}
	if n := countItems(t, root); n != 0 {
		t.Fatalf("--dry-run không được ghi: %d tệp", n)
	}

	// --json xuất cùng dữ liệu.
	out, code = run(t, root, "", "knowledge", "import", "--from", "claude-memory", "--dry-run", "--json")
	var dry importJSON
	if code != 0 || json.Unmarshal([]byte(out), &dry) != nil || !dry.DryRun || dry.Written || len(dry.IDs) != 0 || len(dry.Plan.Ideas) != 2 || dry.Plan.Hash == "" {
		t.Fatalf("dry-run --json: %d\n%s", code, out)
	}
	got := map[string]string{}
	for _, i := range dry.Plan.Ideas {
		got[i.Ref] = i.Outcome + "/" + i.Reason
	}
	if got["claude-memory:feedback_db.md"] != "create/" || got["claude-memory:user_me.md"] != "skip/private" {
		t.Fatalf("kết cục: %v", got)
	}

	// Chạy thật: một mục candidate với nhãn lô; lần hai không tạo thêm.
	out, code = run(t, root, "", "knowledge", "import", "--from", "claude-memory", "--json")
	var res importJSON
	if code != 0 || json.Unmarshal([]byte(out), &res) != nil || !res.Written || len(res.IDs) != 1 || res.Plan.Batch != "nguon:import-claude-memory" {
		t.Fatalf("import: %d\n%s", code, out)
	}
	raw, _ := os.ReadFile(filepath.Join(root, ".bint", "knowledge", "items", res.IDs[0]+".md"))
	for _, want := range []string{"status: candidate", "kind: imported", "source: claude-memory", "nguon:import-claude-memory", "fingerprint:", "claude-memory:feedback_db.md"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("mục thiếu %q:\n%s", want, raw)
		}
	}
	if strings.Contains(string(raw), filepath.ToSlash(home)) || strings.Contains(string(raw), "kỹ sư dữ liệu") {
		t.Errorf("mục rò đường dẫn home hoặc nội dung bộ nhớ riêng tư:\n%s", raw)
	}
	out, code = run(t, root, "", "knowledge", "import", "--from", "claude-memory")
	if code != 0 || !strings.Contains(out, "Không có mục mới để ghi") || countItems(t, root) != 1 {
		t.Fatalf("lần hai: %d\n%s", code, out)
	}

	// Trong phiên agent cả chạy thật lẫn --dry-run bị từ chối và không ghi gì thêm.
	asAgent(t)
	for _, args := range [][]string{{"--from", "claude-memory"}, {"--from", "claude-memory", "--dry-run"}} {
		out, code = run(t, root, "", append([]string{"knowledge", "import"}, args...)...)
		if code == 0 || !strings.Contains(out, "chỉ dành cho người dùng") {
			t.Fatalf("agent %v phải bị từ chối: %d\n%s", args, code, out)
		}
	}
	if n := countItems(t, root); n != 1 {
		t.Fatalf("agent làm đổi số mục: %d", n)
	}
	asHuman(t)

	// Lỗi dùng sai: thiếu --from, URL, nguồn không có.
	for _, args := range [][]string{{}, {"--from", "https://x.io/a.md"}, {"--from", "cursor-rules"}} {
		out, code = run(t, root, "", append([]string{"knowledge", "import"}, args...)...)
		if code == 0 {
			t.Fatalf("%v phải lỗi:\n%s", args, out)
		}
	}
}
