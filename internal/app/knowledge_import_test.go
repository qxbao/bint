package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/knowledge"
	"bint/internal/knowledge/importer"
	"bint/internal/testutil"
)

// importRepo dựng repository đã init, đóng vai người, thư mục home giả (Isolate) và không biến cấu hình harness.
func importRepo(t *testing.T) (*KnowledgeWS, string) {
	t.Helper()
	testutil.Isolate(t)
	asHuman(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	root := testutil.NewRepo(t, map[string]string{
		"README.md":           "# demo\n",
		"internal/db/db.go":   "package db\n\nfunc OpenStore(path string) error {\n\treturn nil\n}\n",
		"internal/db/lock.go": "package db\n\nfunc AcquireLock() {}\n",
	})
	mustInit(t, InitOptions{Dir: root})
	k, err := OpenKnowledge(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	return k, root
}

func writeFile(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// claudeMemDir là thư mục bộ nhớ Claude giả của repository (đúng quy tắc slug) dưới home giả.
func claudeMemDir(t *testing.T, k *KnowledgeWS) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(home, ".claude", "projects", importer.SlugOf(k.Repo.Toplevel), "memory")
}

func seedClaudeMemory(t *testing.T, k *KnowledgeWS) {
	dir := claudeMemDir(t, k)
	writeFile(t, filepath.Join(dir, "feedback_db.md"), "---\nname: db\ndescription: Mở SQLite bằng OpenStore\ntype: feedback\n---\n\nLuôn mở SQLite bằng `OpenStore`, đừng tự gọi sql.Open: thiếu busy_timeout.\n")
	writeFile(t, filepath.Join(dir, "feedback_none.md"), "---\nname: none\ndescription: Quy ước chung\ntype: feedback\n---\n\nLuôn viết commit message ngắn gọn và rõ ràng.\n")
	writeFile(t, filepath.Join(dir, "user_me.md"), "---\nname: me\ndescription: vai trò của tôi\ntype: user\n---\n\nTôi dùng `OpenStore` hằng ngày.\n")
	writeFile(t, filepath.Join(dir, "project_wip.md"), "---\nname: wip\ndescription: Tiến độ B3\ntype: project\n---\n\nĐang làm `OpenStore` ở commit 755b544.\n")
	writeFile(t, filepath.Join(dir, "ref_derived.md"), "---\nname: d\ndescription: Khoá\ntype: reference\n---\n\nXem `AcquireLock` [k:01M746PTBYPSXDT0WJZS38KPFD] về khoá.\n")
	writeFile(t, filepath.Join(dir, "MEMORY.md"), "- [db](feedback_db.md) — OpenStore\n")
}

func outcomes(p *ImportPlan) map[string]string {
	m := map[string]string{}
	for _, i := range p.Ideas {
		if i.Outcome == ImportCreate {
			m[i.Ref] = ImportCreate
		} else {
			m[i.Ref] = i.Reason
		}
	}
	return m
}

func TestImportClaudeMemory(t *testing.T) {
	k, _ := importRepo(t)
	seedClaudeMemory(t, k)
	ctx := context.Background()

	plan, err := PlanImport(ctx, k, ImportOptions{From: "claude-memory"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"claude-memory:feedback_db.md":   ImportCreate,
		"claude-memory:feedback_none.md": importer.SkipNoEvidence,
		"claude-memory:user_me.md":       importer.SkipPrivate,
		"claude-memory:project_wip.md":   importer.SkipWorkState,
		"claude-memory:ref_derived.md":   importer.SkipBintDerived,
		"claude-memory:MEMORY.md":        importer.SkipIndexFile,
	}
	got := outcomes(plan)
	for ref, w := range want {
		if got[ref] != w {
			t.Errorf("%s: kết cục %q, muốn %q", ref, got[ref], w)
		}
	}
	if plan.Batch != "nguon:import-claude-memory" || plan.Files != 6 {
		t.Errorf("plan = %+v", plan)
	}
	for _, i := range plan.Ideas {
		if i.Outcome == ImportSkip && i.Detail == "" {
			t.Errorf("%s: bỏ qua mà không có lý do", i.Ref)
		}
	}
	if items, _, _ := k.Items(); len(items) != 0 {
		t.Fatalf("xem trước không được ghi mục: %d", len(items))
	}

	res, err := ImportKnowledge(ctx, k, ImportOptions{From: "claude-memory", Expect: plan.Hash})
	if err != nil || !res.Written || len(res.IDs) != 1 {
		t.Fatalf("ImportKnowledge = %+v, %v", res, err)
	}
	items, _, _ := k.Items()
	if len(items) != 1 {
		t.Fatalf("mục = %d", len(items))
	}
	it := items[0]
	if it.Status != knowledge.StatusCandidate || it.Origin.Kind != knowledge.OriginImported || it.Origin.Source != knowledge.ImportClaudeMemory {
		t.Errorf("origin/status sai: %+v", it)
	}
	if it.Type != knowledge.TypeWarning || it.Confidence != "low" {
		t.Errorf("loại/độ tin cậy: %s %s", it.Type, it.Confidence)
	}
	if b := it.Scope.Batch(); b != "nguon:import-claude-memory" {
		t.Errorf("nhãn lô = %q (thẻ %v)", b, it.Scope.Tags)
	}
	if len(it.Evidence) == 0 || it.Evidence[0].Fingerprint == "" || it.Evidence[0].Path != "internal/db/db.go" || it.Evidence[0].Symbol != "OpenStore" {
		t.Errorf("bằng chứng: %+v", it.Evidence)
	}
	if len(it.Scope.Paths) != 1 || it.Scope.Paths[0] != "internal/db/db.go" {
		t.Errorf("phạm vi nên theo bằng chứng, không toàn repo: %v", it.Scope.Paths)
	}
	// Riêng tư: tham chiếu nguồn không chứa đường dẫn home; mục không chứa nội dung của ý loại user.
	home, _ := os.UserHomeDir()
	raw, _ := os.ReadFile(it.Path)
	if strings.Contains(string(raw), filepath.ToSlash(home)) || strings.Contains(string(raw), home) || strings.Contains(string(raw), "hằng ngày") {
		t.Errorf("tệp mục rò đường dẫn home hoặc nội dung riêng tư:\n%s", raw)
	}
	if len(it.Origin.Inputs) != 1 || it.Origin.Inputs[0] != "claude-memory:feedback_db.md" {
		t.Errorf("inputs = %v", it.Origin.Inputs)
	}

	// Chạy lần hai: không mục mới, ý đã nhập báo already_imported.
	res2, err := ImportKnowledge(ctx, k, ImportOptions{From: "claude-memory"})
	if err != nil || res2.Written || len(res2.IDs) != 0 {
		t.Fatalf("lần hai: %+v, %v", res2, err)
	}
	if got := outcomes(&res2.Plan)["claude-memory:feedback_db.md"]; got != importer.SkipAlreadyImported {
		t.Errorf("lần hai: %q", got)
	}
	if items, _, _ := k.Items(); len(items) != 1 {
		t.Fatalf("lần hai tạo thêm mục: %d", len(items))
	}

	// Mục imported bị từ chối (deprecated) vẫn không được nhập lại.
	if _, err := RejectKnowledge(ctx, k, RejectOptions{Refs: []string{it.ID}, Reason: "không đúng"}); err != nil {
		t.Fatal(err)
	}
	res3, err := ImportKnowledge(ctx, k, ImportOptions{From: "claude-memory"})
	if err != nil || res3.Written {
		t.Fatalf("sau từ chối: %+v, %v", res3, err)
	}
	if got := outcomes(&res3.Plan)["claude-memory:feedback_db.md"]; got != importer.SkipAlreadyImported {
		t.Errorf("sau từ chối: %q", got)
	}
}

func TestImportDuplicateOfExistingItem(t *testing.T) {
	k, _ := importRepo(t)
	ctx := context.Background()
	if res, err := AddKnowledge(ctx, k, AddOptions{Type: knowledge.TypeWarning, Title: "Mở SQLite bằng OpenStore", Body: "Dùng OpenStore để mở.",
		Evidence: []string{"internal/db/db.go:3-5"}}); err != nil || !res.Saved {
		t.Fatalf("%v %+v", err, res.Check.Findings)
	}
	seedClaudeMemory(t, k)
	plan, err := PlanImport(ctx, k, ImportOptions{From: "claude-memory"})
	if err != nil {
		t.Fatal(err)
	}
	if got := outcomes(plan)["claude-memory:feedback_db.md"]; got != importer.SkipDuplicate {
		t.Fatalf("trùng mục đã có: %q", got)
	}
}

func TestImportCodexMemory(t *testing.T) {
	k, _ := importRepo(t)
	home, _ := os.UserHomeDir()
	writeFile(t, filepath.Join(home, ".codex", "memories", "notes", "db.md"), "# Ghi chú\n\n- Dùng `AcquireLock` trước khi sửa khoá liên tiến trình.\n- Không có gì liên quan tới mã ở dòng này cả.\n")
	res, err := ImportKnowledge(context.Background(), k, ImportOptions{From: "codex-memory", Type: knowledge.TypeRule})
	if err != nil || len(res.IDs) != 1 {
		t.Fatalf("%+v, %v", res, err)
	}
	if got := outcomes(&res.Plan); got["codex-memory:notes/db.md#1"] != ImportCreate || got["codex-memory:notes/db.md#2"] != importer.SkipNoEvidence {
		t.Errorf("kết cục: %v", got)
	}
	items, _, _ := k.Items()
	if len(items) != 1 || items[0].Origin.Source != knowledge.ImportCodexMemory || items[0].Type != knowledge.TypeRule ||
		items[0].Scope.Batch() != "nguon:import-codex-memory" {
		t.Fatalf("mục: %+v", items)
	}
	// CODEX_HOME được tôn trọng.
	alt := t.TempDir()
	t.Setenv("CODEX_HOME", alt)
	if _, err := PlanImport(context.Background(), k, ImportOptions{From: "codex-memory"}); !errors.Is(err, importer.ErrNoSource) {
		t.Fatalf("CODEX_HOME rỗng phải ErrNoSource: %v", err)
	}
}

func TestImportCursorRules(t *testing.T) {
	k, _ := importRepo(t)
	writeFile(t, filepath.Join(k.Root, ".cursor", "rules", "db.mdc"), "---\ndescription: Quy ước DB\nglobs: internal/db/**\nalwaysApply: false\n---\n- Mọi truy cập SQLite phải đi qua `OpenStore` của gói db.\n")
	res, err := ImportKnowledge(context.Background(), k, ImportOptions{From: "cursor-rules"})
	if err != nil || len(res.IDs) != 1 {
		t.Fatalf("%+v, %v", res, err)
	}
	items, _, _ := k.Items()
	it := items[0]
	if it.Type != knowledge.TypeRule || it.Origin.Source != knowledge.ImportCursorRules || it.Title != "Quy ước DB" ||
		len(it.Scope.Paths) != 1 || it.Scope.Paths[0] != "internal/db/**" || it.Scope.Batch() != "nguon:import-cursor-rules" {
		t.Fatalf("mục: %+v", it)
	}
}

func TestImportFile(t *testing.T) {
	k, _ := importRepo(t)
	notes := filepath.Join(t.TempDir(), "notes.md")
	writeFile(t, notes, "# Ghi chú\n\nĐoạn văn về `OpenStore` và cách mở SQLite đúng.\n")
	// --from nhận cả đường dẫn tệp trần.
	res, err := ImportKnowledge(context.Background(), k, ImportOptions{From: notes})
	if err != nil || len(res.IDs) != 1 {
		t.Fatalf("%+v, %v", res, err)
	}
	items, _, _ := k.Items()
	if items[0].Origin.Source != knowledge.ImportFile || items[0].Scope.Batch() != "nguon:import-file" || items[0].Origin.Inputs[0] != "file:notes.md" {
		t.Fatalf("mục: %+v", items[0])
	}
	if _, err := PlanImport(context.Background(), k, ImportOptions{From: "https://x.io/a.md"}); !IsUsage(err) {
		t.Fatalf("URL phải là lỗi dùng sai: %v", err)
	}
	if _, err := PlanImport(context.Background(), k, ImportOptions{From: "file"}); !IsUsage(err) {
		t.Fatalf("file thiếu --path: %v", err)
	}
	if _, err := PlanImport(context.Background(), k, ImportOptions{From: notes, Type: "bad"}); !IsUsage(err) {
		t.Fatalf("--type sai: %v", err)
	}
}

func TestImportAgentForbidden(t *testing.T) {
	k, _ := importRepo(t)
	seedClaudeMemory(t, k)
	t.Setenv("CLAUDECODE", "1") // đóng vai agent sau khi dựng
	before, _, _ := k.Items()
	for _, dry := range []bool{false, true} {
		_, err := ImportKnowledge(context.Background(), k, ImportOptions{From: "claude-memory", DryRun: dry})
		if !errors.Is(err, ErrAgentForbidden) {
			t.Fatalf("dry=%v: phải ErrAgentForbidden, nhận %v", dry, err)
		}
	}
	if _, err := PlanImport(context.Background(), k, ImportOptions{From: "claude-memory"}); !errors.Is(err, ErrAgentForbidden) {
		t.Fatalf("PlanImport: %v", err)
	}
	if after, _, _ := k.Items(); len(after) != len(before) {
		t.Fatal("agent không được ghi mục")
	}
	if entries, _ := os.ReadDir(filepath.Join(k.Root, ".bint", "knowledge", "items")); len(entries) != 0 {
		t.Fatalf("có tệp trong items: %v", entries)
	}
}

func TestImportDryRunAndExpectMismatch(t *testing.T) {
	k, _ := importRepo(t)
	seedClaudeMemory(t, k)
	ctx := context.Background()
	res, err := ImportKnowledge(ctx, k, ImportOptions{From: "claude-memory", DryRun: true})
	if err != nil || res.Written || res.Plan.Count(ImportCreate) != 1 {
		t.Fatalf("dry-run: %+v, %v", res, err)
	}
	if items, _, _ := k.Items(); len(items) != 0 {
		t.Fatal("dry-run ghi mục")
	}
	hash := res.Plan.Hash
	// Nguồn đổi giữa xem trước và ghi: không ghi gì.
	writeFile(t, filepath.Join(claudeMemDir(t, k), "feedback_lock.md"), "---\nname: l\ndescription: Khoá\ntype: feedback\n---\n\nDùng `AcquireLock` cho khoá liên tiến trình.\n")
	res, err = ImportKnowledge(ctx, k, ImportOptions{From: "claude-memory", Expect: hash})
	if !errors.Is(err, ErrImportPlanChanged) || res.Written {
		t.Fatalf("Expect lệch phải ErrImportPlanChanged: %+v, %v", res, err)
	}
	if items, _, _ := k.Items(); len(items) != 0 {
		t.Fatal("kế hoạch lệch vẫn ghi mục")
	}
	// Dựng lại plan rồi ghi với băm mới thì qua.
	plan, _ := PlanImport(ctx, k, ImportOptions{From: "claude-memory"})
	res, err = ImportKnowledge(ctx, k, ImportOptions{From: "claude-memory", Expect: plan.Hash})
	if err != nil || len(res.IDs) != 2 {
		t.Fatalf("%+v, %v", res, err)
	}
}

func TestDetectImportSources(t *testing.T) {
	k, _ := importRepo(t)
	ctx := context.Background()
	if got := DetectImportSources(ctx, k.Root); len(got) != 0 {
		t.Fatalf("chưa có nguồn nào: %+v", got)
	}
	seedClaudeMemory(t, k)
	writeFile(t, filepath.Join(k.Root, ".cursorrules"), "- Một quy tắc đủ dài về `OpenStore`.\n")
	got := DetectImportSources(ctx, k.Root)
	if len(got) != 2 || got[0].Source != knowledge.ImportClaudeMemory || got[0].Files != 5 || got[1].Source != knowledge.ImportCursorRules {
		t.Fatalf("nguồn: %+v", got) // MEMORY.md không tính
	}
}

// Onboarding: báo cáo init nhắc `bint knowledge import` khi có nguồn nhập, và không nhắc khi không có. Init chỉ liệt kê,
// không nhập gì (không mục nào được tạo).
func TestInitNextStepMentionsImportOnlyWithSources(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	root := testutil.NewRepo(t, map[string]string{"README.md": "# demo\n", "internal/db/db.go": "package db\n\nfunc OpenStore() {}\n"})

	has := func(rep *InitReport) string {
		for _, s := range rep.NextSteps {
			if strings.Contains(s, "bint knowledge import") {
				return s
			}
		}
		return ""
	}
	if s := has(mustInit(t, InitOptions{Dir: root})); s != "" {
		t.Fatalf("không có nguồn thì không được nhắc nhập: %q", s)
	}

	// Có bộ nhớ Claude của repository: lần init kế tiếp nhắc, kèm nguồn và số tệp.
	k, err := OpenKnowledge(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	seedClaudeMemory(t, k)
	step := has(mustInit(t, InitOptions{Dir: root}))
	if !strings.Contains(step, "claude-memory: 5 tệp") || !strings.Contains(step, "--from claude-memory --dry-run") {
		t.Fatalf("bước tiếp theo: %q", step)
	}
	home, _ := os.UserHomeDir()
	if strings.Contains(step, home) {
		t.Fatalf("bước tiếp theo không được chứa đường dẫn home: %q", step)
	}
	if items, _, _ := k.Items(); len(items) != 0 {
		t.Fatalf("init không được tự nhập: %d mục", len(items))
	}
}

// Moving a source block must not make its former position suppress a new idea.
func TestImportReorderedIdeas(t *testing.T) {
	k, _ := importRepo(t)
	source := filepath.Join(t.TempDir(), "notes.md")
	old := "- Always open SQLite using `OpenStore` to preserve its timeout.\n- This unrelated note has no repository evidence at all.\n"
	writeFile(t, source, old)
	opts := ImportOptions{From: source}
	first, err := ImportKnowledge(context.Background(), k, opts)
	if err != nil || len(first.IDs) != 1 {
		t.Fatalf("first import: %+v, %v", first, err)
	}
	writeFile(t, source, "- Always call `AcquireLock` before changing interprocess locks.\n"+old)
	plan, err := PlanImport(context.Background(), k, opts)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Count(ImportCreate) != 1 || plan.Ideas[0].Outcome != ImportCreate || plan.Ideas[1].Reason != importer.SkipAlreadyImported {
		t.Fatalf("new idea must survive reused Ref and old content stay deduplicated: %+v", plan.Ideas)
	}
}

func TestImportSourceErrorsHideHome(t *testing.T) {
	k, _ := importRepo(t)
	home, _ := os.UserHomeDir()
	missing := filepath.Join(home, "private-notes", "missing.md")
	_, err := PlanImport(context.Background(), k, ImportOptions{From: "file", Path: missing})
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing source: %v", err)
	}
	if strings.Contains(err.Error(), home) {
		t.Fatalf("source error leaks home: %v", err)
	}
}

func TestDetectImportSourcesIgnoresGlobalCodexMemory(t *testing.T) {
	k, _ := importRepo(t)
	home, _ := os.UserHomeDir()
	writeFile(t, filepath.Join(home, ".codex", "memories", "other-project.md"), "Rules for an unrelated project.\n")
	if got := DetectImportSources(context.Background(), k.Root); len(got) != 0 {
		t.Fatalf("global Codex memory does not establish a source for this repository: %+v", got)
	}
}

func TestImportUnclosedPrivateMemoryIsNotImported(t *testing.T) {
	k, _ := importRepo(t)
	source := filepath.Join(t.TempDir(), "private.md")
	writeFile(t, source, "---\ntype: user\ndescription: Private preference\nPrivate review fixture marker: I prefer `OpenStore` for all database operations.\n")
	got, err := ImportKnowledge(context.Background(), k, ImportOptions{From: "claude-memory", Path: source})
	if err != nil {
		t.Fatal(err)
	}
	items, _, err := k.Items()
	if err != nil {
		t.Fatal(err)
	}
	if got.Written || len(items) != 0 {
		t.Fatalf("private malformed source persisted: result=%+v items=%+v", got, items)
	}
}

func TestImportPrivateFrontmatterWhitespaceDelimiter(t *testing.T) {
	k, _ := importRepo(t)
	source := filepath.Join(t.TempDir(), "private.md")
	writeFile(t, source, "--- \ntype: user\ndescription: Private preference\n---\nPrivate review fixture marker: I prefer `OpenStore` for all database operations.\n")
	got, err := ImportKnowledge(context.Background(), k, ImportOptions{From: "claude-memory", Path: source})
	if err != nil {
		t.Fatal(err)
	}
	items, _, err := k.Items()
	if err != nil {
		t.Fatal(err)
	}
	if got.Written || len(items) != 0 {
		t.Fatalf("private source persisted with whitespace delimiter: result=%+v items=%+v", got, items)
	}
}
