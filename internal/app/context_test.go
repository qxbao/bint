package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/harness/claude"
	"bint/internal/knowledge"
	"bint/internal/mcpserver"
	"bint/internal/runlog"
	"bint/internal/testutil"
)

// Nghiệm thu P3: hook chèn quy tắc theo đường dẫn (không lặp trong phiên), công cụ
// MCP được quy về lượt chạy qua PostToolUse, và thống kê truy xuất/trích dẫn.
func TestPathHookMCPAndStats(t *testing.T) {
	testutil.Isolate(t)
	for _, k := range agentEnvVars {
		t.Setenv(k, "")
	}
	ctx := context.Background()
	root := testutil.NewRepo(t, map[string]string{"internal/db/db.go": "package db\n\nfunc Open() {}\n", "README.md": "x\n"})
	mustInit(t, InitOptions{Dir: root, Hooks: yes(), MCP: yes()})
	if !claude.MCPInstalled(claude.MCPConfigPath(root)) {
		t.Fatal(".mcp.json thiếu mục bint")
	}
	installed, _ := claude.HooksInstalled(filepath.Join(root, ".claude", "settings.local.json"))
	if !installed {
		t.Fatal("hook (gồm PreToolUse) chưa được cài")
	}
	k, err := OpenKnowledge(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	res, err := AddKnowledge(ctx, k, AddOptions{Type: "rule", Title: "Không mở DB trong handler", Body: "Dùng pool chung.",
		Paths: []string{"internal/db/**"}, Evidence: []string{"internal/db/db.go:3#Open"}, Origin: knowledge.OriginHuman})
	if err != nil || !res.Saved {
		t.Fatalf("%+v %v", res.Check, err)
	}
	id := res.Item.ID
	if !strings.HasPrefix(res.Item.Evidence[0].Fingerprint, "symbol:") {
		t.Fatalf("fingerprint theo ký hiệu: %+v", res.Item.Evidence[0])
	}

	pre := func(tool string) claude.HookResult {
		return runHook(t, hookInput("S1", "PreToolUse", root, map[string]any{"tool_name": tool, "tool_use_id": "tu_" + tool + time.Now().Format("150405.000000"),
			"tool_input": map[string]any{"file_path": filepath.Join(root, "internal", "db", "db.go")}}))
	}
	r := pre("Read")
	var out struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(r.Output), &out); err != nil || out.HookSpecificOutput.HookEventName != "PreToolUse" ||
		!strings.Contains(out.HookSpecificOutput.AdditionalContext, "[k:"+id+"]") {
		t.Fatalf("chèn theo đường dẫn: %+v\n%s", r, r.Output)
	}
	if r2 := pre("Edit"); r2.Output != "" {
		t.Fatalf("không lặp lại trong phiên: %s", r2.Output)
	}
	if r3 := runHook(t, hookInput("S1", "PreToolUse", root, map[string]any{"tool_name": "Read",
		"tool_input": map[string]any{"file_path": filepath.Join(root, "README.md")}})); r3.Output != "" {
		t.Fatal("tệp ngoài phạm vi không được chèn")
	}
	// PostToolUse cho công cụ MCP của Bint.
	runHook(t, hookInput("S1", "PostToolUse", root, map[string]any{"tool_name": "mcp__bint__cite_knowledge", "tool_use_id": "c1",
		"tool_input": map[string]any{"ids": []string{id}, "note": "dùng pool"}, "tool_response": []any{map[string]any{"type": "text", "text": "Đã ghi nhận: [k:" + id + "]"}}}))
	runHook(t, hookInput("S1", "PostToolUse", root, map[string]any{"tool_name": "mcp__bint__context_for_task", "tool_use_id": "c2",
		"tool_input": map[string]any{"task": "sửa db"}, "tool_response": "...[k:" + id + "]..."}))

	c, err := OpenContext(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.K.Ingest(ctx, c.Store, time.Now()); err != nil {
		t.Fatal(err)
	}
	st, err := KnowledgeStats(ctx, c)
	if err != nil || len(st) != 1 || st[0].Retrieved != 2 || st[0].Cited != 1 || st[0].Runs != 1 {
		t.Fatalf("stats: %+v %v", st, err)
	}
	runs, _ := runlog.ListRuns(ctx, c.Store.DB, 5)
	if len(runs) != 1 {
		t.Fatalf("runs: %+v", runs)
	}
	// P6: mỗi lần Read ghi file.read (kể cả khi không có tri thức để chèn) cho phát hiện
	// khám phá lặp lại; Edit không ghi.
	arts, _ := runlog.RunArtifacts(ctx, c.Store.DB, runs[0].RunID)
	reads := map[string]int{}
	for _, a := range arts {
		if a.Kind == "file.read" {
			reads[a.Path] = a.Count
		}
	}
	if len(reads) != 2 || reads["internal/db/db.go"] != 1 || reads["README.md"] != 1 {
		t.Fatalf("file.read: %+v", arts)
	}

	// Backend MCP của repository: truy xuất, tìm kiếm, trích dẫn, quan sát.
	b := &RepoBackend{Dir: root}
	defer b.Close()
	p, err := b.ForTask(ctx, "mở kết nối DB trong handler", nil)
	if err != nil || len(p.Entries) == 0 || p.Entries[0].ID != id {
		t.Fatalf("%+v %v", p, err)
	}
	if p, _ := b.ForPaths(ctx, []string{filepath.Join(root, "internal/db/db.go")}); len(p.Entries) != 1 {
		t.Fatalf("paths: %+v", p)
	}
	if p, _ := b.ForPaths(ctx, []string{"internal/db/db.go"}); len(p.Entries) != 0 {
		t.Fatal("MCP không lặp lại mục trong phiên")
	}
	if ok, err := b.Cite(ctx, []string{id[len(id)-6:]}, ""); err != nil || len(ok) != 1 || ok[0] != id {
		t.Fatalf("cite: %v %v", ok, err)
	}
	t.Setenv("CLAUDECODE", "1")
	newID, err := b.Observe(ctx, mcpserver.Observation{Kind: "gotcha", Type: "warning", Title: "Open không kiểm tra lỗi",
		Body: "Open bỏ qua lỗi kết nối.", Paths: []string{"internal/db/**"}, Evidence: []string{"internal/db/db.go:3"}})
	if err != nil || newID == "" {
		t.Fatalf("observe: %q %v", newID, err)
	}
	it, _ := knowledge.ReadFile(k.Store.PathFor(newID))
	if it.Status != knowledge.StatusCandidate || it.Origin.Kind != knowledge.OriginAgent {
		t.Fatalf("mục do agent ghi phải là candidate: %+v", it)
	}
	// Mục mới xuất hiện ngay ở lần truy xuất sau (chỉ mục tự đồng bộ).
	if hits, err := b.Search(ctx, "kiểm tra lỗi kết nối", 5); err != nil || len(hits) == 0 || hits[0].ID != newID {
		t.Fatalf("search sau khi thêm: %+v %v", hits, err)
	}

	// Gỡ MCP.
	mustInit(t, InitOptions{Dir: root, Hooks: yes(), MCP: no()})
	if claude.MCPInstalled(claude.MCPConfigPath(root)) {
		t.Fatal("chưa gỡ MCP")
	}
	os.Remove(claude.MCPConfigPath(root))
}
