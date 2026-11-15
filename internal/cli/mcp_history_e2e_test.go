package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"bint/internal/testutil"
)

// context.history.enabled quyết định `bint mcp serve` có thêm history_for_paths và run_get; chế độ đánh giá
// (--eval-config) không bao giờ có hai công cụ này, kể cả khi cấu hình dự án bật.
func TestMCPServeHistoryTools(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	t.Setenv("BINT_MCP_NO_GATEWAY", "1")
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	hasHistory := func(names []string) bool {
		return slices.Contains(names, "history_for_paths") || slices.Contains(names, "run_get")
	}

	if names, _ := serveToolNames(t, root); len(names) != 6 || hasHistory(names) {
		t.Fatalf("mặc định (tắt): %v", names)
	}

	local := filepath.Join(root, ".bint", "local.yml")
	if err := os.WriteFile(local, []byte("context:\n  history:\n    enabled: true\n    max_runs: 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	names, _ := serveToolNames(t, root)
	if len(names) != 8 || !slices.Contains(names, "history_for_paths") || !slices.Contains(names, "run_get") {
		t.Fatalf("bật: %v", names)
	}

	cfg := filepath.Join(t.TempDir(), "ctx.json")
	body, _ := json.Marshal(map[string]any{"items_dir": t.TempDir(), "root": root, "log": filepath.Join(t.TempDir(), "log.jsonl"),
		"max_tokens": 1000, "path_max_items": 3})
	if err := os.WriteFile(cfg, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if names, _ := serveToolNames(t, root, "--eval-config", cfg); len(names) != 6 || hasHistory(names) {
		t.Fatalf("--eval-config: %v", names)
	}

	if err := os.WriteFile(local, []byte("context:\n  history:\n    enabled: true\n    max_runs: 51\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, code := run(t, root, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`+"\n", "mcp", "serve"); code == 0 {
		t.Fatalf("max_runs 51 phải làm cấu hình lỗi:\n%s", out)
	}
}

// `bint runs history` và `bint runs get` là kênh CLI chỉ đọc tương đương hai công cụ MCP: dùng được kể cả khi
// context.history.enabled tắt, và báo chưa có dữ liệu thay vì lỗi khi chưa có lượt chạy.
func TestRunsHistoryAndGetCLI(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	out, code := run(t, root, "", "runs", "history", "main.go")
	if code != 0 || !strings.Contains(out, "<bint-history") && !strings.Contains(out, "Chưa có dữ liệu lượt chạy") {
		t.Fatalf("runs history: %d\n%s", code, out)
	}
	if out, code = run(t, root, "", "runs", "history", "main.go", "--json", "--include-reads", "-n", "3"); code != 0 || !strings.Contains(out, `"runs"`) {
		t.Fatalf("runs history --json: %d\n%s", code, out)
	}
	if out, code = run(t, root, "", "runs", "get", "khongco"); code == 0 && !strings.Contains(out, "Chưa có dữ liệu lượt chạy") {
		t.Fatalf("runs get id không tồn tại phải lỗi hoặc báo chưa có dữ liệu:\n%s", out)
	}
}
