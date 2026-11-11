package eval

import (
	"bint/internal/harness/claude"
	"bint/internal/harness/hookcore"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/ctxengine"
	"bint/internal/knowledge"
	"bint/internal/testutil"
	"bint/internal/ulid"
)

func TestBintAndIdealArms(t *testing.T) {
	ctx := context.Background()
	root := testutil.NewRepo(t, map[string]string{
		"lib/a.go": "package lib\n\nfunc A() {}\n", "web/b.ts": "export const b = 1;\n",
	})
	scoped := saveItem(t, root, knowledge.TypeWarning, "A phải giữ chữ ký", knowledge.Evidence{Path: "lib/a.go", Lines: "3"})
	scoped.Scope.Paths = []string{"lib/**"}
	knowledge.NewStore(root).Save(scoped)
	other := saveItem(t, root, knowledge.TypeWarning, "Web dùng ESM", knowledge.Evidence{Path: "web/b.ts", Lines: "1"})
	other.Scope.Paths = []string{"web/**"}
	knowledge.NewStore(root).Save(other)
	base := commitAll(t, root, "knowledge at base")
	writeFile(t, root, "lib/a.go", "package lib\n\nfunc A() {}\n\nfunc C() {}\n")
	sol := commitAll(t, root, "solution")

	env := knowledgeEnv(t, root)
	env.BintExe = "/usr/bin/bint"
	c := &Case{ID: "c1", Source: Source{Base: base, Commit: sol}, SourceFiles: []string{"lib/a.go"}}

	// ideal: tập đúng = mục có phạm vi chồng lên lib/a.go, đặt sẵn trong AGENTS.md.
	dir, logDir := t.TempDir(), t.TempDir()
	s, err := prepareArm(ctx, env, ArmIdeal, LeakStrict, c, nil, dir, logDir)
	if err != nil {
		t.Fatal(err)
	}
	agents, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if len(s.Ideal) != 1 || s.Ideal[0] != scoped.ID || !strings.Contains(string(agents), "liên quan tới phần mã") {
		t.Fatalf("ideal: %+v\n%s", s, agents)
	}
	if s.MCPConfig != "" {
		t.Fatal("ideal không dùng Context Engine")
	}

	// bint: AGENTS.md như minimal + hướng dẫn công cụ; cấu hình MCP, hook, bản sao tri thức.
	dir, logDir = t.TempDir(), t.TempDir()
	s, err = prepareArm(ctx, env, ArmBint, LeakStrict, c, nil, dir, logDir)
	if err != nil {
		t.Fatal(err)
	}
	agents, _ = os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if !strings.Contains(string(agents), "context_for_task") || s.EvalConfig == "" || s.Settings == "" {
		t.Fatalf("bint: %+v\n%s", s, agents)
	}
	var mcp struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	b, _ := os.ReadFile(s.MCPConfig)
	if json.Unmarshal(b, &mcp) != nil || mcp.MCPServers["bint"].Command != "/usr/bin/bint" {
		t.Fatalf("mcp config: %s", b)
	}
	ec, err := ctxengine.LoadEvalConfig(s.EvalConfig)
	if err != nil || ec.Root != dir || ec.ExcludeEvalCase != "c1" {
		t.Fatalf("%+v %v", ec, err)
	}
	e, closeFn, err := ec.Engine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer closeFn()
	p := e.ForPaths([]string{"lib/a.go"}, nil)
	if len(p.Entries) != 1 || p.Entries[0].ID != scoped.ID {
		t.Fatalf("engine trong thư mục đánh giá: %+v", p)
	}
	// Hook UserPromptSubmit chèn gói ngữ cảnh theo đề bài, không chờ agent gọi MCP.
	settings, _ := os.ReadFile(s.Settings)
	if !strings.Contains(string(settings), "UserPromptSubmit") {
		t.Fatalf("settings thiếu hook UserPromptSubmit: %s", settings)
	}
	t.Setenv(ctxengine.EvalEnv, s.EvalConfig)
	hr, err := hookcore.Handle(ctx, hookcore.Input{SessionID: "S", Cwd: dir, Event: hookcore.EvPrompt,
		Prompt: "Sửa lỗi trong lib/a.go"}, claude.Dialect, hookcore.Env{})
	if err != nil || !strings.Contains(hr.Output, `"UserPromptSubmit"`) || !strings.Contains(hr.Output, scoped.ID) {
		t.Fatalf("chèn theo đề bài: %+v %v", hr, err)
	}
	// Biến thể chiến lược ngữ cảnh: tắt hook theo đề bài và MCP.
	vdir, vlog := t.TempDir(), t.TempDir()
	vs, err := prepareArm(ctx, env, "bint@task=off+mcp=off", LeakStrict, c, nil, vdir, vlog)
	if err != nil {
		t.Fatal(err)
	}
	vset, _ := os.ReadFile(vs.Settings)
	vagents, _ := os.ReadFile(filepath.Join(vdir, "AGENTS.md"))
	if vs.MCPConfig != "" || strings.Contains(string(vset), "UserPromptSubmit") || !strings.Contains(string(vset), "PreToolUse") || strings.Contains(string(vagents), "context_for_task") {
		t.Fatalf("biến thể: %+v\n%s\n%s", vs, vset, vagents)
	}
	logb, _ := os.ReadFile(s.ContextLog)
	if !strings.Contains(string(logb), ctxengine.ChannelHookTask) {
		t.Fatalf("nhật ký thiếu kênh hook:task: %s", logb)
	}
	// Agent giả lập: hook trả mục đúng, agent trích dẫn; mục khác bị truy xuất thừa.
	ctxengine.AppendLog(s.ContextLog, ctxengine.PackLog(ctxengine.ChannelHookPath, p))
	ctxengine.AppendLog(s.ContextLog, ctxengine.LogLine{Channel: ctxengine.ChannelMCPTask, IDs: []string{other.ID}})
	ctxengine.AppendLog(s.ContextLog, ctxengine.LogLine{Channel: "cite", IDs: []string{scoped.ID}})

	_, st, db := testEnv(t, root)
	_ = st
	row := AttemptRow{AttemptID: "ea_1", BatchID: "eb_1", CaseID: "c1", CaseFingerprint: "x", Arm: ArmBint, Repeat: 1,
		Harness: "command", StartedAt: "2026-01-01T00:00:00.000Z"}
	if err := createBatch(ctx, db, Batch{BatchID: "eb_1", Status: "running", Harness: "command", LeakageMode: LeakStrict,
		Arms: []string{ArmBint}, Repeats: 1, CaseIDs: []string{"c1"}, StartedAt: "2026-01-01T00:00:00.000Z"}); err != nil {
		t.Fatal(err)
	}
	if err := startAttempt(ctx, db, row); err != nil {
		t.Fatal(err)
	}
	if err := recordContextUsage(ctx, db, "ea_1", s); err != nil {
		t.Fatal(err)
	}
	m, err := batchContext(ctx, db, "eb_1")
	if err != nil {
		t.Fatal(err)
	}
	a := m["ea_1"]
	if a.Ideal != 1 || a.IdealRetrieved != 1 || a.IdealCited != 1 || a.RetrievedNotIdeal != 1 || a.Retrieved != 2 {
		t.Fatalf("%+v", a)
	}

	// bint/ideal cần minimal_source: knowledge.
	env.Cfg.Eval.MinimalSource = "file"
	if _, err := prepareArm(ctx, env, ArmBint, LeakStrict, c, nil, t.TempDir(), t.TempDir()); err == nil {
		t.Fatal("phải từ chối khi minimal_source: file")
	}
}

func TestSplitArm(t *testing.T) {
	base, v, err := SplitArm("bint@task=off+mcp=off+tokens=3000+items=3")
	if err != nil || base != ArmBint || !v.NoTaskHook || v.NoPathHook || !v.NoMCP || v.MaxTokens != 3000 || v.PathMaxItems != 3 {
		t.Fatalf("%s %+v %v", base, v, err)
	}
	for _, bad := range []string{"minimal@task=off", "bint@task=maybe", "bint@foo=1", "bint@tokens=0"} {
		if _, _, err := SplitArm(bad); err == nil {
			t.Errorf("%q phải lỗi", bad)
		}
	}
	if ArmNotImplemented("bint@path=off") != nil || ArmNotImplemented("bint@x=1") == nil {
		t.Fatal("ArmNotImplemented với biến thể")
	}
}

// Nhánh ideal giữ bài học từ thất bại có phạm vi khớp tệp nguồn: mục được tính là ideal phải
// thật sự được chở vào AGENTS.md (qua phần Pack.Lessons, ngân sách riêng).
func TestIdealArmKeepsLessons(t *testing.T) {
	ctx := context.Background()
	root := testutil.NewRepo(t, map[string]string{"lib/a.go": "package lib\n\nfunc A() {}\n"})
	lesson := &knowledge.Item{Schema: knowledge.SchemaVersion, ID: ulid.New(), Type: knowledge.TypeLesson,
		Status: knowledge.StatusVerified, Title: "Khi sửa lib nhớ gọi Reset", Body: "Khi sửa lib nhớ gọi Reset.", ReviewedBy: "r",
		Version: 1, CreatedAt: knowledge.Now(time.Now()), Scope: knowledge.Scope{Paths: []string{"lib/**"}},
		Origin: knowledge.Origin{Kind: knowledge.OriginAgent, Provenance: knowledge.ProvenanceFailureLesson, Runs: []string{"ea_1"}}}
	if err := knowledge.NewStore(root).Save(lesson); err != nil {
		t.Fatal(err)
	}
	base := commitAll(t, root, "knowledge at base")
	writeFile(t, root, "lib/a.go", "package lib\n\nfunc A() {}\n\nfunc C() {}\n")
	sol := commitAll(t, root, "solution")

	env := knowledgeEnv(t, root)
	c := &Case{ID: "c1", Source: Source{Base: base, Commit: sol}, SourceFiles: []string{"lib/a.go"}}
	dir := t.TempDir()
	s, err := prepareArm(ctx, env, ArmIdeal, LeakStrict, c, nil, dir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	agents, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if len(s.Ideal) != 1 || s.Ideal[0] != lesson.ID || !strings.Contains(string(agents), "Khi sửa lib nhớ gọi Reset") {
		t.Fatalf("bài học ideal phải nằm trong AGENTS.md: %+v\n%s", s, agents)
	}
}
