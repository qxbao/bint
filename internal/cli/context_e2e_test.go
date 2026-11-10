package cli_test

import (
	"bufio"
	"encoding/json"
	"strings"
	"testing"

	"bint/internal/testutil"
)

func TestContextEngineEndToEnd(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{
		"internal/db/db.go": "package db\n\nfunc Open() error {\n\treturn nil\n}\n",
		"web/app.ts":        "export const x = 1;\n",
	})
	testutil.Git(t, root, "config", "user.name", "Rev")
	testutil.Git(t, root, "config", "user.email", "rev@example.com")
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	out, code := run(t, root, "", "knowledge", "add", "--type", "warning", "--paths", "internal/db/**",
		"--evidence", "internal/db/db.go:3-5#Open", "--json", "Migration cơ sở dữ liệu phải chạy qua make migrate, không sửa schema tay")
	var added struct {
		ID string `json:"id"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &added) != nil || added.ID == "" {
		t.Fatalf("add: %d\n%s", code, out)
	}

	// Theo đường dẫn: chỉ mục có phạm vi khớp.
	if out, code := run(t, root, "", "context", "--paths", "internal/db/db.go"); code != 0 || !strings.Contains(out, "[k:"+added.ID+"]") ||
		!strings.Contains(out, "Điểm dễ sai") {
		t.Fatalf("context --paths: %d\n%s", code, out)
	}
	if out, _ := run(t, root, "", "context", "--paths", "web/app.ts"); !strings.Contains(out, "Không có tri thức liên quan") {
		t.Fatalf("ngoài phạm vi: %s", out)
	}
	// Theo tác vụ (không dấu vẫn khớp).
	if out, code := run(t, root, "", "context", "them migration cho bang users"); code != 0 || !strings.Contains(out, added.ID) {
		t.Fatalf("context task: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "knowledge", "search", "schema"); code != 0 || !strings.Contains(out, added.ID) {
		t.Fatalf("search: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "index", "rebuild"); code != 0 || !strings.Contains(out, "1 mục") {
		t.Fatalf("index rebuild: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "context"); code != 2 {
		t.Fatalf("thiếu đối số: %d\n%s", code, out)
	}

	// MCP qua stdio.
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"context_for_paths","arguments":{"paths":["internal/db/db.go"]}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"knowledge_get","arguments":{"id":"` + added.ID[len(added.ID)-6:] + `"}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"cite_knowledge","arguments":{"ids":["[k:` + added.ID + `]"]}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"knowledge_get","arguments":{"id":"ZZZZZZ"}}}`,
		`{"jsonrpc":"2.0","id":7,"method":"nope"}`,
	}, "\n") + "\n"
	out, code = run(t, root, in, "mcp", "serve")
	if code != 0 {
		t.Fatalf("mcp serve: %d\n%s", code, out)
	}
	resp := map[int]map[string]any{}
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	for sc.Scan() {
		var m map[string]any
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			t.Fatalf("dòng không phải JSON-RPC: %s", sc.Text())
		}
		id, _ := m["id"].(float64)
		resp[int(id)] = m
	}
	if len(resp) != 7 {
		t.Fatalf("cần 7 phản hồi (thông báo không có phản hồi): %d\n%s", len(resp), out)
	}
	if r := resp[1]["result"].(map[string]any); r["protocolVersion"] != "2025-06-18" {
		t.Fatalf("initialize: %v", r)
	}
	if tools := resp[2]["result"].(map[string]any)["tools"].([]any); len(tools) != 6 {
		t.Fatalf("tools: %d", len(tools))
	}
	text := func(id int) (string, bool) {
		r := resp[id]["result"].(map[string]any)
		c := r["content"].([]any)[0].(map[string]any)["text"].(string)
		isErr, _ := r["isError"].(bool)
		return c, isErr
	}
	if s, e := text(3); e || !strings.Contains(s, "[k:"+added.ID+"]") {
		t.Fatalf("context_for_paths: %s", s)
	}
	if s, e := text(4); e || !strings.Contains(s, "make migrate") {
		t.Fatalf("knowledge_get: %s", s)
	}
	if s, e := text(5); e || !strings.Contains(s, added.ID) {
		t.Fatalf("cite: %s", s)
	}
	if _, e := text(6); !e {
		t.Fatal("ID không tồn tại phải trả isError")
	}
	if resp[7]["error"] == nil {
		t.Fatal("method lạ phải trả lỗi JSON-RPC")
	}

	// init --mcp ghi .mcp.json; runs rate từ chối trong phiên agent.
	if out, code := run(t, root, "", "init", "--no-hooks", "--mcp"); code != 0 && !strings.Contains(out, "MCP claude") {
		t.Fatalf("init --mcp: %d\n%s", code, out)
	}
	asAgent(t)
	if out, code := run(t, root, "", "runs", "rate", "x", "good"); code == 0 || !strings.Contains(out, "phiên agent") {
		t.Fatalf("rate trong phiên agent: %d\n%s", code, out)
	}
}
