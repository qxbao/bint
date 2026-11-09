package eval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/config"
	"bint/internal/knowledge"
	"bint/internal/testutil"
	"bint/internal/ulid"
)

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commitAll(t *testing.T, root, msg string) string {
	t.Helper()
	testutil.Git(t, root, "add", "-A")
	testutil.Git(t, root, "commit", "-q", "-m", msg)
	return testutil.Git(t, root, "rev-parse", "HEAD")
}

// saveItem ghi một mục verified có bằng chứng (fingerprint lấy từ cây làm việc).
func saveItem(t *testing.T, root, typ, title string, evidence ...knowledge.Evidence) *knowledge.Item {
	t.Helper()
	it := &knowledge.Item{
		Schema: knowledge.SchemaVersion, ID: ulid.New(), Type: typ, Status: knowledge.StatusVerified, Title: title,
		Body: title + ".", Origin: knowledge.Origin{Kind: knowledge.OriginAgent}, ReviewedBy: "r", Version: 1,
		CreatedAt: knowledge.Now(time.Now()), Evidence: evidence,
	}
	if err := knowledge.FillFingerprints(it, &knowledge.WorktreeSource{Root: root}); err != nil {
		t.Fatal(err)
	}
	if err := knowledge.NewStore(root).Save(it); err != nil {
		t.Fatal(err)
	}
	return it
}

func knowledgeEnv(t *testing.T, root string) Env {
	cfg := config.Default()
	cfg.Eval.MinimalSource = "knowledge"
	return Env{RepoDir: root, WorkRoot: filepath.Join(t.TempDir(), "eval-work"), BintRoot: root, Cfg: cfg}
}

// Kiểm thử toàn vẹn (mục 17): strict không để lọt mục tri thức tạo sau commit gốc.
func TestStrictUsesKnowledgeAtBase(t *testing.T) {
	ctx := context.Background()
	root := testutil.NewRepo(t, map[string]string{"lib/a.go": "package lib\n\nfunc A() {}\n"})
	early := saveItem(t, root, knowledge.TypeRule, "Quy tắc có từ trước", knowledge.Evidence{Path: "lib/a.go", Lines: "3"})
	base := commitAll(t, root, "knowledge at base")
	writeFile(t, root, "lib/a.go", "package lib\n\nfunc A() {}\n\nfunc B() {}\n")
	late := saveItem(t, root, knowledge.TypeRule, "Quy tắc viết sau khi có lời giải B", knowledge.Evidence{Path: "lib/a.go", Lines: "5"})
	commitAll(t, root, "solution + late knowledge")

	env := knowledgeEnv(t, root)
	c := &Case{ID: "c1", Source: Source{Base: base}}
	content, src, screening, err := minimalFromKnowledge(ctx, env, LeakStrict, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if screening || !strings.Contains(string(content), early.ID) || strings.Contains(string(content), late.ID) {
		t.Fatalf("strict phải dùng tri thức tại commit gốc (src %s):\n%s", src, content)
	}
	if strings.Contains(string(content), "bint knowledge") {
		t.Fatal("ngữ cảnh đánh giá không chứa chỉ dẫn CLI Bint")
	}

	// Ảnh chụp do bootstrap sinh được ưu tiên, kể cả mục candidate.
	snapItem := &knowledge.Item{Schema: 1, ID: ulid.New(), Type: knowledge.TypeRule, Status: knowledge.StatusCandidate,
		Title: "Từ ảnh chụp", Body: "x", Origin: knowledge.Origin{Kind: knowledge.OriginAgent},
		Evidence: []knowledge.Evidence{{Path: "lib/a.go"}}, Version: 1, CreatedAt: knowledge.Now(time.Now())}
	snapDir := SnapshotDir(env.WorkRoot, base)
	if err := (knowledge.Store{Dir: filepath.Join(snapDir, "items")}).Save(snapItem); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(snapDir, "meta.json"), []byte(`{"base":"`+base+`","items":1}`), 0o600)
	content, src, _, err = minimalFromKnowledge(ctx, env, LeakStrict, c, nil)
	if err != nil || !strings.Contains(string(content), snapItem.ID) || strings.Contains(string(content), early.ID) || !strings.HasPrefix(src, "bootstrap@") {
		t.Fatalf("ảnh chụp: %v %s\n%s", err, src, content)
	}

	// Commit gốc không có tri thức và không có ảnh chụp → nhánh không khả dụng.
	first := testutil.Git(t, root, "rev-list", "--max-parents=0", "HEAD")
	_, _, _, err = minimalFromKnowledge(ctx, env, LeakStrict, &Case{ID: "c0", Source: Source{Base: first}}, nil)
	if err == nil || !strings.Contains(err.Error(), "eval bootstrap") {
		t.Fatalf("thiếu tri thức tại commit gốc: %v", err)
	}
}

// economy: chỉ giữ mục có bằng chứng khớp tại commit gốc, loại bài học từ chính
// tác vụ và mục có commit bằng chứng sau commit gốc.
func TestEconomyFilter(t *testing.T) {
	ctx := context.Background()
	root := testutil.NewRepo(t, map[string]string{"lib/a.go": "package lib\n\nfunc A() {}\n", "lib/b.go": "package lib\n\nvar x = 1\n"})
	base := testutil.Git(t, root, "rev-parse", "HEAD")
	writeFile(t, root, "lib/b.go", "package lib\n\nvar x = 2\n")
	after := commitAll(t, root, "change b")

	stable := saveItem(t, root, knowledge.TypeRule, "Ổn định từ trước", knowledge.Evidence{Path: "lib/a.go", Lines: "3"})
	changed := saveItem(t, root, knowledge.TypeRule, "Dựa trên b đã đổi", knowledge.Evidence{Path: "lib/b.go", Lines: "3"})
	futureCommit := saveItem(t, root, knowledge.TypeWarning, "Bằng chứng là commit sau", knowledge.Evidence{Path: "lib/a.go", Lines: "3", Commit: after})
	lesson := saveItem(t, root, knowledge.TypeWarning, "Bài học từ chính tác vụ", knowledge.Evidence{Path: "lib/a.go"})
	lesson.Origin.EvalCase = "c1"
	knowledge.NewStore(root).Save(lesson)
	writeFile(t, root, "lib/new.go", "package lib\n")
	newFile := saveItem(t, root, knowledge.TypeRule, "Tệp chưa có tại gốc", knowledge.Evidence{Path: "lib/new.go"})

	env := knowledgeEnv(t, root)
	c := &Case{ID: "c1", Source: Source{Base: base}}
	content, _, screening, err := minimalFromKnowledge(ctx, env, LeakEconomy, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := string(content)
	if !screening || !strings.Contains(s, stable.ID) {
		t.Fatalf("economy:\n%s", s)
	}
	for _, it := range []*knowledge.Item{changed, futureCommit, lesson, newFile} {
		if strings.Contains(s, it.ID) {
			t.Errorf("%q không được lọt vào nhánh minimal", it.Title)
		}
	}
	// Không còn mục nào hợp lệ → không khả dụng.
	os.Remove(stable.Path)
	if _, _, _, err := minimalFromKnowledge(ctx, env, LeakEconomy, c, nil); err == nil {
		t.Fatal("không có mục hợp lệ phải báo nhánh không khả dụng")
	}
}

// Ảnh chụp bootstrap: agent (giả lập) gọi bint trong thư mục cô lập; chỉ mục agent
// candidate hợp lệ được giữ, agent thất bại không tạo ảnh chụp rỗng.
func TestBootstrapSnapshotFakeAgent(t *testing.T) {
	needSh(t)
	ctx := context.Background()
	root := testutil.NewRepo(t, map[string]string{"lib/a.go": "package lib\n", "AGENTS.md": "secret context\n"})
	base := testutil.Git(t, root, "rev-parse", "HEAD")
	env := knowledgeEnv(t, root)

	// Agent giả: kiểm tra môi trường cô lập, rồi ghi một tệp tri thức hợp lệ trực tiếp.
	id := ulid.New()
	item := "---\nschema: 1\nid: " + id + "\ntype: rule\nstatus: candidate\ntitle: Quy tắc giả\nscope: {}\nevidence:\n  - path: lib/a.go\norigin:\n  kind: agent\nversion: 1\ncreated_at: \"2026-01-01T00:00:00Z\"\n---\n\nNội dung.\n"
	script := filepath.Join(t.TempDir(), "agent.sh")
	os.WriteFile(script, []byte(`set -e
[ "$BINT_AGENT" = 1 ] || { echo "thiếu BINT_AGENT" >&2; exit 3; }
[ ! -e AGENTS.md ] || { echo "ngữ cảnh chưa bị gỡ" >&2; exit 4; }
[ -f .bint/config.yml ] || exit 5
mkdir -p .bint/knowledge/items
cat > .bint/knowledge/items/`+id+`.md <<'EOF'
`+item+`EOF
echo '{"type":"result","num_turns":3,"total_cost_usd":0.02}'
`), 0o644)
	meta, err := BootstrapSnapshot(ctx, env, base, BootstrapOptions{Agent: &CommandAgent{Command: "sh " + filepath.ToSlash(script)}})
	if err != nil {
		t.Fatalf("%v (%+v)", err, meta)
	}
	if meta.Items != 1 || meta.CostUSD == nil || *meta.CostUSD != 0.02 {
		t.Fatalf("meta: %+v", meta)
	}
	_, items, ok, err := ReadSnapshot(env.WorkRoot, base)
	if err != nil || !ok || len(items) != 1 || items[0].ID != id {
		t.Fatalf("ảnh chụp: %v %v %d", ok, err, len(items))
	}

	// Agent thất bại: không thay ảnh chụp cũ bằng ảnh chụp rỗng.
	fail := filepath.Join(t.TempDir(), "fail.sh")
	os.WriteFile(fail, []byte("exit 7\n"), 0o644)
	if _, err := BootstrapSnapshot(ctx, env, base, BootstrapOptions{Agent: &CommandAgent{Command: "sh " + filepath.ToSlash(fail)}}); err == nil {
		t.Fatal("agent thất bại phải báo lỗi")
	}
	if _, items, ok, _ := ReadSnapshot(env.WorkRoot, base); !ok || len(items) != 1 {
		t.Fatal("ảnh chụp cũ phải được giữ")
	}
}
