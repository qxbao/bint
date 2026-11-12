package app

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"bint/internal/hcomp"
	"bint/internal/knowledge"
	"bint/internal/skills"
	"bint/internal/testutil"
	"bint/internal/workflow"
)

// Kiểm thử liệt kê (docs/isolation-subagent-plan.md 4.5/4.6): mọi hàm ghi tệp repo của tầng dịch vụ phải đi qua
// hàm ghi chung repoWriter (repowrite.go), để bước chỉ đọc của `bint run` nhận ra thay đổi do Bint ghi thay người.

// directWriteAllowed liệt kê các hàm CÒN gọi thẳng os.WriteFile/Remove/... hoặc fsafe.WriteAtomic/SyncBlock/
// RemoveBlock ngoài repowrite.go, kèm lý do vì sao đường dẫn đó không nằm trong cây làm việc của repo. Thiếu hay
// thừa một mục đều làm kiểm thử đỏ.
var directWriteAllowed = map[string]string{
	"bootstrap.go:Bootstrap":          "generated/inspect.json trong thư mục trạng thái",
	"config_svc.go:PrepareConfigEdit": "bản tạm soạn cấu hình trong thư mục tạm của hệ điều hành",
	"config_svc.go:RebaseConfigEdit":  "ghi lại bản tạm soạn cấu hình (thư mục tạm), không phải tệp lớp cấu hình",
	"doctor.go:checkWritable":         "tệp thăm dò quyền ghi, tạo rồi xoá ngay",
	"evolvesvc/cycle.go:Cycle":        "evolve/cycles/<id>.json trong thư mục trạng thái",
	"gitvis.go:saveGitMode":           "git-mode trong thư mục trạng thái",
	"mcp_svc.go:ensureGitExclude":     "git info/exclude nằm trong .git (không phải tệp của cây làm việc); chỉ thêm một dòng .codegraph/",
	"hook.go:RunHook":                 "hook-errors.log trong thư mục trạng thái; hook không dùng repoWriter (TestHookDoesNotUseRepoWriter)",
	"skills_svc.go:PlanSync":          "xem trước sync trên bản sao tạm, không chạm repo",
	"skills_svc.go:copyTree":          "sao cây vào thư mục tạm của PlanSync",
	"update.go:UpdateSelf":            "thay binary bint đang chạy (ngoài repo)",
}

// Các lời gọi ghi trực tiếp bị theo dõi (gói.hàm).
var directWriteCalls = map[string]bool{
	"os.WriteFile": true, "os.Create": true, "os.OpenFile": true, "os.Remove": true, "os.RemoveAll": true,
	"os.Rename": true, "os.Truncate": true, "os.CreateTemp": true, "os.MkdirTemp": true,
	"fsafe.WriteAtomic": true, "fsafe.SyncBlock": true, "fsafe.RemoveBlock": true,
}

// Lời gọi sang gói thấp hơn tự ghi tệp repo: chỉ repowrite.go được gọi (hàm bọc ghi nhận ngay sau).
var lowLevelCalls = map[string]bool{
	"hcomp.Save": true, "skills.Install": true,
}

type callSite struct {
	file, fn string
	call     string
}

// scanApp duyệt mọi tệp .go không phải test của internal/app và internal/app/evolvesvc.
func scanApp(t *testing.T) (sites []callSite, funcBodies map[string]map[string]bool) {
	t.Helper()
	funcBodies = map[string]map[string]bool{} // "tệp:hàm" → tên các hàm/phương thức được gọi trong thân
	fset := token.NewFileSet()
	for _, dir := range []string{".", "evolvesvc"} {
		ents, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range ents {
			n := e.Name()
			if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
				continue
			}
			rel := n
			if dir != "." {
				rel = dir + "/" + n
			}
			f, err := parser.ParseFile(fset, filepath.Join(dir, n), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				key := rel + ":" + fd.Name.Name
				names := map[string]bool{}
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					ce, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					switch fun := ce.Fun.(type) {
					case *ast.SelectorExpr:
						names[fun.Sel.Name] = true
						if id, ok := fun.X.(*ast.Ident); ok {
							q := id.Name + "." + fun.Sel.Name
							names[q] = true
							if directWriteCalls[q] || lowLevelCalls[q] || q == "evolve.Apply" || q == "evolve.Rollback" {
								sites = append(sites, callSite{rel, fd.Name.Name, q})
							}
						}
						// k.Store.Save / k.Store.Remove
						if inner, ok := fun.X.(*ast.SelectorExpr); ok && inner.Sel.Name == "Store" && (fun.Sel.Name == "Save" || fun.Sel.Name == "Remove") {
							sites = append(sites, callSite{rel, fd.Name.Name, "Store." + fun.Sel.Name})
						}
						// hi.InstallHooks / UninstallHooks / mi.InstallMCP: adapter tự ghi tệp repo.
						switch fun.Sel.Name {
						case "InstallHooks", "UninstallHooks", "InstallMCP":
							sites = append(sites, callSite{rel, fd.Name.Name, fun.Sel.Name})
						}
					case *ast.Ident:
						names[fun.Name] = true
					}
					return true
				})
				funcBodies[key] = names
			}
		}
	}
	return sites, funcBodies
}

func TestRepoWritesGoThroughRepoWriter(t *testing.T) {
	sites, bodies := scanApp(t)

	allowed := directWriteAllowed
	used := map[string]bool{}
	var bad []string
	for _, s := range sites {
		key := s.file + ":" + s.fn
		if s.file == "repowrite.go" {
			continue
		}
		switch s.call {
		case "evolve.Apply", "evolve.Rollback", "InstallHooks", "UninstallHooks", "InstallMCP":
			// Gói/adapter thấp hơn tự ghi: hàm gọi phải ghi nhận ngay trong cùng thân.
			if !bodies[key]["Record"] && !bodies[key]["record"] && !bodies[key]["recordFiles"] && !bodies[key]["recordHooks"] && !bodies[key]["RecordRepoWrites"] {
				bad = append(bad, fmt.Sprintf("%s gọi %s nhưng không ghi nhận (Record/record/recordFiles/recordHooks/RecordRepoWrites) trong cùng hàm", key, s.call))
			}
		case "Store.Save", "Store.Remove", "hcomp.Save", "skills.Install":
			if _, ok := allowed[key]; !ok {
				bad = append(bad, fmt.Sprintf("%s gọi %s thẳng: dùng repoWriter.SaveItem/RemoveItem/SaveComponent/InstallSkills", key, s.call))
			}
			used[key] = true
		default: // os.* / fsafe.*
			if _, ok := allowed[key]; !ok {
				bad = append(bad, fmt.Sprintf("%s gọi %s thẳng: đi qua repoWriter (hoặc thêm vào directWriteAllowed kèm lý do nếu không phải tệp repo)", key, s.call))
			}
			used[key] = true
		}
	}
	for k := range allowed {
		if !used[k] {
			bad = append(bad, fmt.Sprintf("directWriteAllowed có %q nhưng hàm đó không còn gọi ghi trực tiếp: xoá khỏi bảng", k))
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Fatalf("hàm ghi ngoài repoWriter:\n  %s", strings.Join(bad, "\n  "))
	}
}

// hook.go không được tham chiếu hàm ghi chung: hook không ghi tệp repo và không được ghi vào nhật ký.
func TestHookDoesNotUseRepoWriter(t *testing.T) {
	b, err := os.ReadFile("hook.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"repoWriter", "newRepoWriter", "AppendWrites", "writes.jsonl", "WritesLogPath"} {
		if strings.Contains(string(b), w) {
			t.Errorf("hook.go không được dùng %s", w)
		}
	}
}

// Bảng kiểm động: mỗi dịch vụ ghi chạy vai người giữa hai ảnh chụp cây làm việc; ReadonlyViolations phải miễn
// mọi tệp đổi (dòng nhật ký không phải agent, chuỗi prev_sha256 nối liền từ ảnh trước, băm khớp), trừ tệp đã
// liệt kê tường minh trong noPrev.
func TestEveryServiceWriteIsLogged(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	ctx := context.Background()
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n", "a.go": "package a\n\nfunc A() {}\n"})
	testutil.Git(t, root, "config", "user.name", "Rev")
	testutil.Git(t, root, "config", "user.email", "rev@example.com")
	mustInit(t, InitOptions{Dir: root, Hooks: no(), AgentsMD: true})
	k, err := OpenKnowledge(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	runtime := k.Paths.Runtime

	add := func(title string) string {
		res, err := AddKnowledge(ctx, k, AddOptions{Type: knowledge.TypeFact, Title: title, Body: "Nội dung " + title,
			Paths: []string{"a.go"}, Evidence: []string{"a.go:3"}, Origin: knowledge.OriginAgent, ThirdParty: true})
		if err != nil || !res.Saved {
			t.Fatalf("%s: %v %+v", title, err, res.Check.Findings)
		}
		return res.Item.ID
	}
	var approveID, rejectID, removeID string
	coreSkill := skills.Core()[0].Name

	type step struct {
		name  string
		setup func() // chạy TRƯỚC ảnh chụp trước (chuẩn bị trạng thái, không tính)
		act   func() error
	}
	// noPrev: theo tên bước, tệp (tương đối với gốc repo) mà đường ghi chưa biết trạng thái trước (Record:
	// prev_sha256 nil) nên ReadonlyViolations không miễn — hướng an toàn, ghi ở docs/workflows.md "Giới hạn đã biết".
	noPrev := map[string][]string{
		// InstallMCP chỉ trả đường dẫn sau khi ghi nên không băm trước được.
		"init --hooks --mcp (cài hook và .mcp.json)": {".codex/config.toml", ".mcp.json"},
	}
	steps := []step{
		{"config set ui.language (lớp project)", nil, func() error {
			_, err := SetUILanguage(ctx, root, UILanguageOptions{Language: "vi", Layer: "project"})
			return err
		}},
		{"knowledge add", nil, func() error { approveID = add("Một"); rejectID = add("Hai"); removeID = add("Ba"); return nil }},
		{"knowledge approve", nil, func() error {
			_, err := ApproveKnowledge(ctx, k, ApproveOptions{Refs: []string{approveID}})
			return err
		}},
		{"knowledge reject", nil, func() error {
			_, err := RejectKnowledge(ctx, k, RejectOptions{Refs: []string{rejectID}, Reason: "không còn đúng"})
			return err
		}},
		{"knowledge remove", nil, func() error { _, err := RemoveKnowledge(k, []string{removeID}); return err }},
		{"knowledge verify --apply (làm mới fingerprint)", func() {
			os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n\n// đổi\nfunc A() {}\n"), 0o644)
		}, func() error { _, err := VerifyKnowledge(ctx, k, []string{approveID}, true, time.Now()); return err }},
		{"skill overlay", nil, func() error { _, err := CreateOverlay(ctx, root, SkillOverlayOptions{Name: coreSkill}); return err }},
		{"sync (xuất bản lại skill đã xoá)", func() {
			os.Remove(filepath.Join(root, ".claude", "skills", coreSkill, "SKILL.md"))
		}, func() error { _, err := Sync(ctx, root, "test", SyncOptions{}); return err }},
		{"bootstrap publish (AGENTS.md)", func() { // đổi khối Bint để publish có việc làm
			os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# Tài liệu\n"), 0o644)
		}, func() error {
			_, err := PublishAgents(ctx, PublishOptions{Dir: root, Yes: true, Force: true})
			return err
		}},
		{"harness component add + approve", nil, func() error {
			w, err := OpenComponents(ctx, root)
			if err != nil {
				return err
			}
			c, err := AddComponent(ctx, w, AddComponentOptions{Template: hcomp.Templates()[0].ID})
			if err != nil {
				return err
			}
			_, err = ReviewComponent(ctx, w, ComponentReviewOptions{ID: c.ID, Kind: c.Kind}, hcomp.StatusVerified)
			return err
		}},
		{"init --update (nâng cấu hình)", func() {
			p := filepath.Join(root, ".bint", "config.yml")
			b, _ := os.ReadFile(p)
			var out []string
			for _, ln := range strings.Split(string(b), "\n") {
				if !strings.HasPrefix(strings.TrimSpace(ln), "verify_timeout_minutes:") {
					out = append(out, ln)
				}
			}
			os.WriteFile(p, []byte(strings.Join(out, "\n")), 0o644)
		}, func() error {
			_, err := Init(ctx, InitOptions{Dir: root, Hooks: no(), Update: true, BintPath: "bint"})
			return err
		}},
		{"init --hooks --mcp (cài hook và .mcp.json)", nil, func() error {
			yes := true
			_, err := Init(ctx, InitOptions{Dir: root, Hooks: &yes, MCP: &yes, BintPath: "bint"})
			return err
		}},
	}
	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			if s.setup != nil {
				s.setup()
			}
			before, err := workflow.WorktreeSnapshot(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			since := time.Now()
			if err := s.act(); err != nil {
				t.Fatal(err)
			}
			after, err := workflow.WorktreeSnapshot(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			changed, err := workflow.DiffPaths(ctx, root, before, after)
			if err != nil {
				t.Fatal(err)
			}
			if len(changed) == 0 {
				t.Fatal("dịch vụ không đổi tệp nào: bước kiểm thử vô nghĩa, chỉnh lại setup")
			}
			recs, err := workflow.ReadWrites(workflow.WritesLogPath(runtime), since)
			if err != nil {
				t.Fatal(err)
			}
			// Đối chiếu bằng chính ReadonlyViolations (chuỗi prev_sha256 nối liền từ ảnh trước): tệp nào không được
			// miễn thì phải nằm trong danh sách noPrev của bước (đường ghi chưa biết trạng thái trước).
			chk := workflow.ReadonlyViolations(ctx, root, before, after, since, workflow.WritesLogPath(runtime))
			var want []string
			for _, p := range changed {
				if slices.Contains(noPrev[s.name], p) {
					want = append(want, p)
				}
			}
			if len(noPrev[s.name]) != len(want) {
				t.Errorf("noPrev %v có tệp không đổi (đổi: %v)", noPrev[s.name], changed)
			}
			got := slices.Clone(chk.Violations)
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("vi phạm = %v, muốn đúng %v (chỉ đường ghi không biết trạng thái trước mới được lọt)", got, want)
			}
			if t.Failed() {
				names := make([]string, 0, len(recs))
				for _, r := range recs {
					names = append(names, strings.TrimPrefix(r.Path, workflow.NormalizeWritePath(root)+"/")+"("+r.Actor+")")
				}
				t.Logf("đổi: %v\nnhật ký: %v", slices.Clone(changed), names)
			}
		})
	}
}
