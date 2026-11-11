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

	// Nguồn gốc (mục 12.3): chỉ mục tạo tại/trước commit gốc mới được giữ.
	baseTime := strings.TrimSpace(testutil.Git(t, root, "show", "-s", "--format=%cI", base))
	bt, _ := time.Parse(time.RFC3339, baseTime)
	origin := func(it *knowledge.Item, commit string, created time.Time) {
		it.Origin.Commit, it.CreatedAt = commit, knowledge.Now(created)
		if err := knowledge.NewStore(root).Save(it); err != nil {
			t.Fatal(err)
		}
	}
	origin(stable, base, bt)
	origin(changed, base, bt)
	origin(futureCommit, base, bt)
	origin(lesson, base, bt)
	origin(newFile, base, bt)
	// Bằng chứng cũ (hàm A tại gốc) nhưng kết luận sinh sau lời giải.
	lateOrigin := saveItem(t, root, knowledge.TypeRule, "Kết luận mới, bằng chứng cũ", knowledge.Evidence{Path: "lib/a.go", Lines: "3"})
	origin(lateOrigin, after, bt.Add(time.Hour))
	// Cùng origin.commit hợp lệ nhưng created_at sau commit gốc.
	lateTime := saveItem(t, root, knowledge.TypeRule, "Tạo sau commit gốc", knowledge.Evidence{Path: "lib/a.go", Lines: "3"})
	origin(lateTime, base, bt.Add(time.Hour))
	// Không có origin.commit: không chứng minh được thời điểm → loại.
	noOrigin := saveItem(t, root, knowledge.TypeRule, "Không rõ nguồn gốc", knowledge.Evidence{Path: "lib/a.go", Lines: "3"})
	origin(noOrigin, "", bt)

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
	for _, it := range []*knowledge.Item{changed, futureCommit, lesson, newFile, lateOrigin, lateTime, noOrigin} {
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

// EvidenceHoldsAt: bài học từ thất bại thuần (không có bằng chứng tệp) được miễn; có bằng chứng
// tệp (vd. lesson thường được gộp vào bài học) thì vẫn bị đối chiếu fingerprint tại commit gốc.
func TestEvidenceHoldsAtLessonExemptionOnlyWithoutEvidence(t *testing.T) {
	ctx := context.Background()
	root := testutil.NewRepo(t, map[string]string{"lib/a.go": "package lib\n\nfunc A() {}\n"})
	it := saveItem(t, root, knowledge.TypeLesson, "Lesson có bằng chứng tệp", knowledge.Evidence{Path: "lib/a.go", Lines: "3"})
	base := commitAll(t, root, "base")
	c := &Case{ID: "c1", Source: Source{Base: base}}
	failure := knowledge.Origin{Kind: knowledge.OriginAgent, Provenance: knowledge.ProvenanceFailureLesson, Runs: []string{"ea_1"}}

	// Thuần failure_lesson: không bằng chứng → miễn.
	pure := &knowledge.Item{Type: knowledge.TypeLesson, Origin: failure}
	if !EvidenceHoldsAt(ctx, root, c, pure) {
		t.Fatal("bài học từ thất bại không có bằng chứng tệp phải được miễn")
	}
	// Có bằng chứng và còn khớp → đạt.
	merged := *it
	merged.Origin = failure
	if !EvidenceHoldsAt(ctx, root, c, &merged) {
		t.Fatal("bằng chứng còn khớp phải đạt")
	}
	// Có bằng chứng nhưng lệch fingerprint → không được miễn dù provenance failure_lesson.
	writeFile(t, root, "lib/a.go", "package lib\n\nfunc A() { println() }\n")
	c2 := &Case{ID: "c2", Source: Source{Base: commitAll(t, root, "đổi a")}}
	merged.Evidence = []knowledge.Evidence{{Path: "lib/a.go", Lines: "3", Fingerprint: it.Evidence[0].Fingerprint}}
	if EvidenceHoldsAt(ctx, root, c2, &merged) {
		t.Fatal("bằng chứng tệp lệch fingerprint không được miễn chỉ vì provenance failure_lesson")
	}
	// Không bằng chứng và không phải failure_lesson → loại như trước.
	if EvidenceHoldsAt(ctx, root, c, &knowledge.Item{Type: knowledge.TypeLesson}) {
		t.Fatal("mục không bằng chứng, không phải failure_lesson phải bị loại")
	}
}
