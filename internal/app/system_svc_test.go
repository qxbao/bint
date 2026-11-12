package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"bint/internal/config"
	"bint/internal/fsafe"
	"bint/internal/harness"
	"bint/internal/hcomp"
	"bint/internal/testutil"
)

// sysRepo dựng repository đã init (claude, không hook) cho kiểm thử dịch vụ của màn Hệ thống.
func sysRepo(t *testing.T, opts InitOptions) string {
	t.Helper()
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	opts.Dir = root
	if opts.Harnesses == nil {
		opts.Harnesses = []string{"claude"}
	}
	if opts.Hooks == nil {
		opts.Hooks = no()
	}
	mustInit(t, opts)
	return root
}

func TestHarnessOptionsValidate(t *testing.T) {
	if err := (HarnessShowOptions{Name: "claude"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (HarnessShowOptions{Name: "nhảm"}).Validate(); !IsUsage(err) {
		t.Fatalf("harness lạ phải là UsageError: %v", err)
	}
	if err := HarnessListDefaults(config.Default()).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestListShowHarnessesNoVersion(t *testing.T) {
	root := sysRepo(t, InitOptions{})
	infos, err := ListHarnesses(context.Background(), root, HarnessListOptions{NoVersion: true})
	if err != nil || len(infos) != len(harness.All()) {
		t.Fatalf("%v %d", err, len(infos))
	}
	for _, in := range infos {
		if in.Version != "" {
			t.Fatalf("--no-version không được gọi chương trình harness: %s %q", in.Name, in.Version)
		}
	}
	in, err := ShowHarness(context.Background(), root, HarnessShowOptions{Name: "claude"})
	if err != nil || in.Name != "claude" || !in.Enabled {
		t.Fatalf("%v %+v", err, in)
	}
}

// HarnessRuntime và app.Doctor dùng chung một nguồn: dòng hook/MCP của doctor khớp trạng thái từng harness.
func TestHarnessRuntimeMatchesDoctor(t *testing.T) {
	cases := []struct {
		name  string
		hooks bool
		mcp   *bool
	}{
		{"không hook, không MCP", false, nil},
		{"có hook, có MCP", true, yes()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := no()
			if c.hooks {
				h = yes()
			}
			root := sysRepo(t, InitOptions{Hooks: h, MCP: c.mcp, HooksScope: "project"})
			ctx := context.Background()
			rts, err := HarnessRuntime(ctx, root)
			if err != nil || len(rts) != 1 || rts[0].Name != "claude" {
				t.Fatalf("%v %+v", err, rts)
			}
			rt := rts[0]
			rep := Doctor(ctx, root)
			byName := map[string]Check{}
			for _, ck := range rep.Checks {
				byName[ck.Name] = ck
			}
			hk, ok := byName["hook claude"]
			if !ok || rt.Hook == nil || hk.Level != rt.Hook.Level || hk.Detail != rt.Hook.Detail {
				t.Fatalf("hook: doctor=%+v runtime=%+v", hk, rt.Hook)
			}
			if c.hooks != (len(rt.Hook.Scopes) > 0) {
				t.Fatalf("phạm vi hook đã cài: %v (hooks=%v)", rt.Hook.Scopes, c.hooks)
			}
			mc, hasMCP := byName["MCP claude"]
			if !hasMCP || rt.MCP == nil {
				t.Fatalf("MCP: doctor=%+v runtime=%+v", mc, rt.MCP)
			}
			if rt.MCP.Registered != (mc.Level == LevelOK) {
				t.Fatalf("MCP đã đăng ký: runtime=%v doctor=%+v", rt.MCP.Registered, mc)
			}
			if c.mcp != nil && !rt.MCP.Registered {
				t.Fatal("init --mcp phải đăng ký MCP")
			}
		})
	}
}

func TestComponentOptionsValidate(t *testing.T) {
	cases := []struct {
		name string
		o    AddComponentOptions
		ok   bool
	}{
		{"mẫu", AddComponentOptions{Template: "test-before-stop"}, true},
		{"tệp + loại", AddComponentOptions{Kind: hcomp.KindGuidance, File: "a.md"}, true},
		{"không có gì", AddComponentOptions{}, false},
		{"tệp thiếu loại", AddComponentOptions{File: "a.md"}, false},
		{"loại sai", AddComponentOptions{Kind: "nhảm", File: "a.md"}, false},
		{"mẫu cùng tệp", AddComponentOptions{Template: "x", Kind: hcomp.KindGuidance, File: "a.md"}, false},
		{"mẫu cùng loại", AddComponentOptions{Template: "x", Kind: hcomp.KindGuidance}, false},
	}
	for _, c := range cases {
		err := c.o.Validate()
		if (err == nil) != c.ok || (err != nil && !IsUsage(err)) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	if err := (ComponentListOptions{Status: "nhảm"}).Validate(); !IsUsage(err) {
		t.Errorf("status lạ: %v", err)
	}
	if err := (ComponentListOptions{Kind: "nhảm"}).Validate(); !IsUsage(err) {
		t.Errorf("kind lạ: %v", err)
	}
	if err := (ComponentShowOptions{}).Validate(); !IsUsage(err) {
		t.Errorf("thiếu id: %v", err)
	}
	if err := (RenderOptions{Mode: "nhảm"}).Validate(); !IsUsage(err) {
		t.Errorf("mode lạ: %v", err)
	}
	if err := (RenderOptions{Harness: "nhảm"}).Validate(); !IsUsage(err) {
		t.Errorf("harness lạ: %v", err)
	}
	if err := RenderDefaults(config.Default()).Validate(); err != nil {
		t.Error(err)
	}
	if err := (RenderOptions{}).Validate(); err != nil {
		t.Errorf("RenderOptions rỗng dùng mặc định: %v", err)
	}
}

func TestComponentListShowAddReview(t *testing.T) {
	root := sysRepo(t, InitOptions{})
	ctx := context.Background()
	w, err := OpenComponents(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AddComponent(ctx, w, AddComponentOptions{Template: "test-before-stop"}); err != nil {
		t.Fatal(err)
	}
	lst, err := ListComponents(w, ComponentListOptions{})
	if err != nil || len(lst.Rows) != 1 || lst.Rows[0].Status != hcomp.StatusCandidate {
		t.Fatalf("%v %+v", err, lst)
	}
	if lst, _ = ListComponents(w, ComponentListOptions{Status: hcomp.StatusVerified}); len(lst.Rows) != 0 {
		t.Fatalf("lọc verified phải rỗng: %+v", lst.Rows)
	}
	d, err := ShowComponent(w, ComponentShowOptions{ID: "test-before-stop"})
	if err != nil || !strings.Contains(d.Content, "test-before-stop") || d.Row.ID != "test-before-stop" {
		t.Fatalf("%v %+v", err, d)
	}
	// Chỉ người duyệt được: phiên agent bị từ chối.
	asAgent(t)
	if _, err := ReviewComponent(ctx, w, ComponentReviewOptions{ID: "test-before-stop"}, hcomp.StatusVerified); !errors.Is(err, ErrAgentForbidden) {
		t.Fatalf("agent phải bị chặn: %v", err)
	}
	if _, err := ReviewComponent(ctx, w, ComponentReviewOptions{ID: "test-before-stop"}, hcomp.StatusDeprecated); !errors.Is(err, ErrAgentForbidden) {
		t.Fatalf("agent phải bị chặn loại bỏ: %v", err)
	}
	asHuman(t)
	c, err := ReviewComponent(ctx, w, ComponentReviewOptions{ID: "test-before-stop"}, hcomp.StatusVerified)
	if err != nil || c.Status != hcomp.StatusVerified {
		t.Fatalf("%v %+v", err, c)
	}
	out, err := RenderHarness(w, RenderOptions{Harness: "claude", Mode: hcomp.ModeOn})
	if err != nil || !strings.Contains(out, "test-before-stop") {
		t.Fatalf("render: %v\n%s", err, out)
	}
}

func TestListSkillsShowOverlaySync(t *testing.T) {
	root := sysRepo(t, InitOptions{})
	ctx := context.Background()
	k, err := OpenKnowledge(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	lst, err := ListSkills(k, SkillListOptions{})
	if err != nil || len(lst.Rows) == 0 || lst.Rows[0].Kind != "core" {
		t.Fatalf("%v %+v", err, lst)
	}
	name := lst.Rows[0].Name
	core, err := ShowSkill(ctx, root, SkillShowOptions{Name: name, Core: true})
	if err != nil || core.Composed || len(core.Content) == 0 {
		t.Fatalf("%v", err)
	}
	if _, err := ShowSkill(ctx, root, SkillShowOptions{Name: "khong-co"}); err == nil {
		t.Fatal("skill lạ phải lỗi")
	}

	// Tạo lớp phủ: lần đầu tạo, lần hai không ghi đè.
	r1, err := CreateOverlay(ctx, root, SkillOverlayOptions{Name: name})
	if err != nil || r1.Existed {
		t.Fatalf("%v %+v", err, r1)
	}
	if err := os.WriteFile(r1.Abs, []byte("# đã sửa tay\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r2, err := CreateOverlay(ctx, root, SkillOverlayOptions{Name: name})
	if err != nil || !r2.Existed {
		t.Fatalf("%v %+v", err, r2)
	}
	if b, _ := os.ReadFile(r1.Abs); string(b) != "# đã sửa tay\n" {
		t.Fatal("CreateOverlay ghi đè tệp có sẵn")
	}
	os.Remove(r1.Abs) // bỏ lớp phủ giả: các bước sau cần skill đã ghép hợp lệ

	// PlanSync không ghi gì; sau khi xoá một skill đã xuất bản, kế hoạch báo tạo lại và khớp Sync.
	target := filepath.Join(root, ".claude", "skills", name, "SKILL.md")
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, root)
	plan, err := PlanSync(ctx, root, "test", SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if after := snapshotTree(t, root); after != before {
		t.Fatalf("PlanSync không được đổi tệp nào:\n%s\n---\n%s", before, after)
	}
	var created *SyncFile
	for i, f := range plan.Files {
		if f.Result == fsafe.Created && strings.Contains(f.Path, name) {
			created = &plan.Files[i]
		}
	}
	if created == nil || plan.Changes < 1 || !strings.Contains(created.Diff, "+") {
		t.Fatalf("kế hoạch phải báo tạo lại %s: %+v", name, plan)
	}
	si, err := Sync(ctx, root, "test", SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("Sync phải tạo lại %s: %v", target, err)
	}
	if si.Conflicts != plan.Conflicts {
		t.Fatalf("xung đột khác kế hoạch: %d vs %d", si.Conflicts, plan.Conflicts)
	}
	got := map[string]fsafe.Outcome{}
	for _, o := range si.Outcomes {
		got[o.Path] = o.Result
	}
	for _, f := range plan.Files {
		if got[f.Path] != f.Result {
			t.Errorf("%s: kế hoạch %v nhưng Sync %v", f.Path, f.Result, got[f.Path])
		}
	}

	// Skill bị sửa tay → xung đột trong kế hoạch, không ghi đè; --force thì ghi.
	if err := os.WriteFile(target, []byte("sửa tay\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err = PlanSync(ctx, root, "test", SyncOptions{})
	if err != nil || plan.Conflicts == 0 {
		t.Fatalf("phải có xung đột: %v %+v", err, plan)
	}
	if b, _ := os.ReadFile(target); string(b) != "sửa tay\n" {
		t.Fatal("PlanSync không được ghi đè")
	}
	plan, err = PlanSync(ctx, root, "test", SyncOptions{Force: true})
	if err != nil || plan.Conflicts != 0 || plan.Changes == 0 {
		t.Fatalf("--force bỏ xung đột: %v %+v", err, plan)
	}
	if b, _ := os.ReadFile(target); string(b) != "sửa tay\n" {
		t.Fatal("PlanSync --force cũng không được ghi thật")
	}
}

// snapshotTree liệt kê tệp (không gồm .git) cùng băm nội dung để so sánh trước/sau.
func snapshotTree(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			if info != nil && info.IsDir() && info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		data, _ := os.ReadFile(p)
		rel, _ := filepath.Rel(root, p)
		b.WriteString(rel + " " + fsafe.Hash(string(data)) + "\n")
		return nil
	})
	return b.String()
}

func TestRebuildIndexCountsAndReportsBad(t *testing.T) {
	root := sysRepo(t, InitOptions{})
	ctx := context.Background()
	res, err := RebuildIndex(ctx, root, IndexRebuildOptions{})
	if err != nil || res.Total != 0 || len(res.Errors) != 0 {
		t.Fatalf("%v %+v", err, res)
	}
	items := filepath.Join(root, ".bint", "knowledge", "items")
	if err := os.MkdirAll(items, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(items, "hong.md"), []byte("không có frontmatter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = RebuildIndex(ctx, root, IndexRebuildOptions{})
	if err != nil || len(res.Errors) != 1 {
		t.Fatalf("tệp hỏng phải được báo: %v %+v", err, res)
	}
}

func TestMCPToolsListsTools(t *testing.T) {
	tl, err := MCPTools("test", MCPToolsOptions{})
	if err != nil || len(tl) < 4 {
		t.Fatalf("%v %+v", err, tl)
	}
	names := map[string]bool{}
	for _, x := range tl {
		names[x.Name] = true
	}
	for _, want := range []string{"context_for_task", "knowledge_search", "cite_knowledge"} {
		if !names[want] {
			t.Errorf("thiếu công cụ %s", want)
		}
	}
}

func TestConfigViewGroupsAndSources(t *testing.T) {
	root := sysRepo(t, InitOptions{})
	v, err := ConfigView(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, g := range v.Groups {
		keys = append(keys, g.Key)
		if strings.TrimSpace(g.YAML) == "" {
			t.Errorf("nhóm %s rỗng", g.Key)
		}
	}
	want := "project state harnesses roles context knowledge memory eval evolution workflow limits security ui"
	if strings.Join(keys, " ") != want {
		t.Fatalf("nhóm khoá = %v", keys)
	}
	if len(v.Sources) != 4 || !v.Sources[1].Present || v.Sources[1].Name != "project" || !v.Sources[1].Editable || v.Sources[3].Present {
		t.Fatalf("lớp nguồn: %+v", v.Sources)
	}
	if v.LinkedWorktree {
		t.Fatal("worktree chính")
	}
}

func TestConfigEditFlow(t *testing.T) {
	root := sysRepo(t, InitOptions{})
	ctx := context.Background()
	proj := config.ProjectPath(root)
	orig, _ := os.ReadFile(proj)

	e, err := PrepareConfigEdit(ctx, root, config.LayerProject)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(e.TempPath)
	if b, _ := os.ReadFile(e.TempPath); string(b) != string(orig) {
		t.Fatal("bản tạm phải giống tệp gốc")
	}
	// Không đổi.
	if c, err := CheckConfigEdit(ctx, root, e, orig); err != nil || c.Changed || !c.Valid {
		t.Fatalf("%v %+v", err, c)
	}
	// Nội dung không hợp lệ: lỗi hiện, tệp gốc giữ nguyên.
	bad := []byte(strings.Replace(string(orig), "language: auto", "language: fr", 1))
	c, err := CheckConfigEdit(ctx, root, e, bad)
	if err != nil || !c.Changed || c.Valid || !strings.Contains(c.Err, "language") {
		t.Fatalf("%v %+v", err, c)
	}
	if _, err := SaveConfigEdit(ctx, root, e, bad); err == nil {
		t.Fatal("Save phải từ chối nội dung không hợp lệ")
	}
	if b, _ := os.ReadFile(proj); string(b) != string(orig) {
		t.Fatal("tệp gốc không được đổi khi nội dung không hợp lệ")
	}
	// Hợp lệ: diff có thay đổi, ghi sau đó.
	good := []byte(strings.Replace(string(orig), "language: auto", "language: en", 1))
	c, err = CheckConfigEdit(ctx, root, e, good)
	if err != nil || !c.Valid || !strings.Contains(c.Diff, "+") {
		t.Fatalf("%v %+v", err, c)
	}
	// Tệp gốc bị đổi trong lúc sửa → từ chối.
	if err := os.WriteFile(proj, append(append([]byte{}, orig...), []byte("# người khác sửa\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveConfigEdit(ctx, root, e, good); !errors.Is(err, ErrConfigChanged) {
		t.Fatalf("phải từ chối khi tệp gốc đổi: %v", err)
	}
	if b, _ := os.ReadFile(proj); !strings.Contains(string(b), "người khác sửa") {
		t.Fatal("không được ghi đè thay đổi của người khác")
	}
	// Sao lại rồi ghi được; bản cũ được sao lưu.
	os.WriteFile(proj, orig, 0o644)
	res, err := SaveConfigEdit(ctx, root, e, good)
	if err != nil || res.Backup == "" {
		t.Fatalf("%v %+v", err, res)
	}
	if b, _ := os.ReadFile(proj); string(b) != string(good) {
		t.Fatal("tệp phải được ghi")
	}
}

func TestPrepareConfigEditLocalNewHasSeed(t *testing.T) {
	root := sysRepo(t, InitOptions{})
	ctx := context.Background()
	e, err := PrepareConfigEdit(ctx, root, config.LayerLocal)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(e.TempPath)
	if e.Existed || !strings.Contains(string(e.Seed), "local.yml") {
		t.Fatalf("%+v", e)
	}
	// Không sửa gì (giữ nguyên hạt giống) → không thay đổi.
	if c, _ := CheckConfigEdit(ctx, root, e, e.Seed); c.Changed {
		t.Fatal("hạt giống không đổi thì không phải thay đổi")
	}
	if _, err := PrepareConfigEdit(ctx, root, config.Layer("global")); !IsUsage(err) {
		t.Fatalf("lớp toàn cục: %v", err)
	}
	// Lớp cục bộ đè lên: nội dung sai ở local bị bắt bởi Validate trên cấu hình đã ghép.
	c, _ := CheckConfigEdit(ctx, root, e, []byte("ui:\n  language: de\n"))
	if c.Valid || c.Err == "" {
		t.Fatalf("%+v", c)
	}
}

func TestSetUILanguageService(t *testing.T) {
	root := sysRepo(t, InitOptions{})
	ctx := context.Background()
	proj := config.ProjectPath(root)
	origProj, _ := os.ReadFile(proj)

	if err := (UILanguageOptions{Language: "fr"}).Validate(); !IsUsage(err) {
		t.Fatalf("%v", err)
	}
	// Mặc định ghi vào local.yml (chưa có → tạo), không đụng config.yml.
	res, err := SetUILanguage(ctx, root, UILanguageDefaults(config.Default()))
	if err != nil || !res.Changed {
		t.Fatalf("%v %+v", err, res)
	}
	o := UILanguageOptions{Language: "en", Layer: "local"}
	res, err = SetUILanguage(ctx, root, o)
	if err != nil || !res.Changed || res.Path != config.LocalPath(root) {
		t.Fatalf("%v %+v", err, res)
	}
	b, _ := os.ReadFile(config.LocalPath(root))
	if !strings.Contains(string(b), "language: en") {
		t.Fatalf("local.yml: %q", b)
	}
	if now, _ := os.ReadFile(proj); string(now) != string(origProj) {
		t.Fatal("config.yml không được đổi khi ghi vào local")
	}
	// Ghi cùng giá trị lần nữa: không đổi.
	if res, err = SetUILanguage(ctx, root, o); err != nil || res.Changed {
		t.Fatalf("%v %+v", err, res)
	}
	// Ghi vào config.yml: chỉ đổi đúng dòng ui.language, giữ chú thích và các dòng khác.
	res, err = SetUILanguage(ctx, root, UILanguageOptions{Language: "vi", Layer: "project"})
	if err != nil || !res.Changed {
		t.Fatalf("%v %+v", err, res)
	}
	now, _ := os.ReadFile(proj)
	want := strings.Replace(string(origProj), "language: auto", "language: vi", 1)
	if string(now) != want {
		t.Fatalf("chỉ được đổi đúng một dòng:\n--- muốn\n%s\n--- có\n%s", want, now)
	}
	if res.Backup == "" {
		t.Fatal("config.yml cũ phải được sao lưu")
	}
}

func TestSetUILanguageInsertsMissingUIBlock(t *testing.T) {
	root := sysRepo(t, InitOptions{})
	ctx := context.Background()
	proj := config.ProjectPath(root)
	orig, _ := os.ReadFile(proj)
	i := strings.Index(string(orig), "\nui:\n")
	if i < 0 {
		t.Fatal("mẫu phải có khối ui")
	}
	noUI := string(orig[:i+1])
	if err := os.WriteFile(proj, []byte(noUI), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SetUILanguage(ctx, root, UILanguageOptions{Language: "vi", Layer: "project"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(proj)
	if !strings.HasPrefix(string(b), noUI) || !strings.Contains(string(b), "ui:\n  language: vi") {
		t.Fatalf("phải giữ nguyên phần cũ và chèn khối ui:\n%s", b[len(b)-200:])
	}
	if _, _, err := config.Load(root, true); err != nil {
		t.Fatalf("cấu hình sau khi chèn phải nạp được: %v", err)
	}
}

func TestConfigEditRefusedInLinkedWorktree(t *testing.T) {
	root := sysRepo(t, InitOptions{})
	ctx := context.Background()
	// Cấu hình đã commit để worktree phụ cũng đọc được .bint/config.yml của chính nó.
	testutil.Git(t, root, "add", "-A")
	testutil.Git(t, root, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "bint", "--allow-empty")
	wt := filepath.Join(t.TempDir(), "wt")
	testutil.Git(t, root, "worktree", "add", "-q", "-b", "phu", wt)

	if _, err := PrepareConfigEdit(ctx, wt, config.LayerProject); !errors.As(err, new(LinkedWorktreeError)) {
		t.Fatalf("phải từ chối ở worktree phụ: %v", err)
	}
	before := snapshotTree(t, wt)
	_, err := SetUILanguage(ctx, wt, UILanguageOptions{Language: "en", Layer: "local"})
	var lw LinkedWorktreeError
	if !errors.As(err, &lw) || lw.Main == "" {
		t.Fatalf("ghi ngôn ngữ ở worktree phụ phải bị từ chối: %v", err)
	}
	if snapshotTree(t, wt) != before {
		t.Fatal("worktree phụ không được đổi tệp nào")
	}
	if _, err := os.Stat(config.LocalPath(root)); err == nil {
		t.Fatal("không được tạo local.yml ở worktree chính qua worktree phụ")
	}
	v, err := ConfigView(ctx, wt)
	if err != nil || !v.LinkedWorktree {
		t.Fatalf("xem cấu hình ở worktree phụ vẫn được: %v %+v", err, v.LinkedWorktree)
	}
}

func TestSystemOptionsValidateAndDefaults(t *testing.T) {
	cfg := config.Default()
	for name, err := range map[string]error{
		"skill list":     SkillListDefaults(cfg).Validate(),
		"skill overlay":  (SkillOverlayOptions{Name: "b-do"}).Validate(),
		"skill diff":     (SkillDiffOptions{Name: "x", From: 1, To: 2}).Validate(),
		"skill versions": (SkillVersionsOptions{Name: "x"}).Validate(),
		"sync":           SyncDefaults(cfg).Validate(),
		"index rebuild":  IndexRebuildDefaults(cfg).Validate(),
		"mcp tools":      MCPToolsDefaults(cfg).Validate(),
		"harness list":   HarnessListDefaults(cfg).Validate(),
		"ui language":    UILanguageDefaults(cfg).Validate(),
	} {
		if err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, err := range map[string]error{
		"skill show trống":       (SkillShowOptions{}).Validate(),
		"skill overlay trống":    (SkillOverlayOptions{}).Validate(),
		"skill diff trống":       (SkillDiffOptions{}).Validate(),
		"skill diff from < 0":    (SkillDiffOptions{Name: "x", From: -1}).Validate(),
		"skill versions trống":   (SkillVersionsOptions{}).Validate(),
		"ui language lạ":         (UILanguageOptions{Language: "fr"}).Validate(),
		"ui language nơi ghi":    (UILanguageOptions{Language: "vi", Layer: "global"}).Validate(),
		"component review trống": (ComponentReviewOptions{}).Validate(),
	} {
		if !IsUsage(err) {
			t.Errorf("%s: muốn UsageError, có %v", name, err)
		}
	}
	if got := UILanguageDefaults(cfg); got.Layer != "local" || got.Language != cfg.UI.Language {
		t.Errorf("mặc định ngôn ngữ: %+v", got)
	}
	if got := RenderDefaults(cfg); got.Harness != "claude" || got.Mode != hcomp.ModeOn {
		t.Errorf("mặc định render: %+v", got)
	}
}

func TestDiffSkillAndVersionsOfEvolvedSkill(t *testing.T) {
	root := sysRepo(t, InitOptions{})
	ctx := context.Background()
	if _, err := DiffSkill(ctx, root, SkillDiffOptions{Name: "khong-co"}); err == nil {
		t.Fatal("skill tiến hoá lạ phải lỗi")
	}
	if _, err := SkillVersions(ctx, root, SkillVersionsOptions{Name: "khong-co"}); err == nil {
		t.Fatal("skill tiến hoá lạ phải lỗi")
	}
}

// 6a.1 (01M75YH2JGR9AD685VH886WE33.5): sau ErrConfigChanged, RebaseConfigEdit gộp ba chiều thay đổi của người dùng lên
// tệp gốc mới: không hoàn tác thay đổi của người khác; xung đột thì bản tạm mang dấu và Check từ chối; tệp gốc bị xoá thì
// giữ bản của người dùng làm tệp mới.
func TestRebaseConfigEditMergesThreeWay(t *testing.T) {
	cases := []struct {
		name      string
		other     func(orig string) (string, bool) // nội dung tệp gốc mới (bool=false: xoá tệp)
		wantMerge bool
		conflicts int
	}{
		{"không xung đột", func(o string) (string, bool) { return o + "# người khác thêm\n", true }, true, 0},
		{"xung đột", func(o string) (string, bool) { return strings.Replace(o, "language: auto", "language: vi", 1), true }, true, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := sysRepo(t, InitOptions{})
			ctx := context.Background()
			proj := config.ProjectPath(root)
			orig, _ := os.ReadFile(proj)
			e, err := PrepareConfigEdit(ctx, root, config.LayerProject)
			if err != nil {
				t.Fatal(err)
			}
			defer os.Remove(e.TempPath)
			mine := strings.Replace(string(orig), "language: auto", "language: en", 1)
			if mine == string(orig) {
				t.Fatal("mẫu cấu hình phải có language: auto")
			}
			os.WriteFile(e.TempPath, []byte(mine), 0o600)
			other, exists := tc.other(string(orig))
			if exists {
				os.WriteFile(proj, []byte(other), 0o644)
			} else {
				os.Remove(proj)
			}
			if _, err := SaveConfigEdit(ctx, root, e, []byte(mine)); !errors.Is(err, ErrConfigChanged) {
				t.Fatalf("phải từ chối: %v", err)
			}
			e2, rb, err := RebaseConfigEdit(ctx, root, e)
			if err != nil {
				t.Fatal(err)
			}
			if rb.Merged != tc.wantMerge || rb.Conflicts != tc.conflicts {
				t.Fatalf("kết quả gộp %+v", rb)
			}
			temp, _ := os.ReadFile(e2.TempPath)
			switch tc.name {
			case "không xung đột":
				want := strings.Replace(other, "language: auto", "language: en", 1)
				if string(temp) != want {
					t.Fatalf("bản tạm phải là bản đã gộp:\n%s", temp)
				}
				if string(e2.Seed) != other || string(e2.Original) != other || e2.BaseHash == e.BaseHash {
					t.Fatalf("mốc mới sai: %+v", e2)
				}
				c, err := CheckConfigEdit(ctx, root, e2, temp)
				if err != nil || !c.Changed || !c.Valid || strings.Contains(c.Diff, "-# người khác") || !strings.Contains(c.Diff, "+  language: en") {
					t.Fatalf("diff chỉ còn thay đổi của người dùng: %v %+v", err, c)
				}
				if _, err := SaveConfigEdit(ctx, root, e2, temp); err != nil {
					t.Fatalf("ghi sau gộp: %v", err)
				}
				if b, _ := os.ReadFile(proj); string(b) != want {
					t.Fatalf("tệp gốc phải có cả hai thay đổi:\n%s", b)
				}
			case "xung đột":
				if !strings.Contains(string(temp), ConflictMine) || !strings.Contains(string(temp), ConflictTheirs) {
					t.Fatalf("bản tạm phải mang dấu xung đột:\n%s", temp)
				}
				c, err := CheckConfigEdit(ctx, root, e2, temp)
				if err != nil || c.Valid || !strings.Contains(c.Err, "xung đột") {
					t.Fatalf("còn dấu xung đột phải bị từ chối: %v %+v", err, c)
				}
				resolved := strings.Replace(other, "language: vi", "language: en", 1)
				if c, _ := CheckConfigEdit(ctx, root, e2, []byte(resolved)); !c.Valid || !c.Changed {
					t.Fatalf("gộp tay xong phải hợp lệ: %+v", c)
				}
			}
		})
	}
}

// Tệp gốc bị xoá sau khi mở (lớp local): gộp giữ bản của người dùng làm tệp mới, ghi tạo lại tệp.
func TestRebaseConfigEditOriginalDeleted(t *testing.T) {
	root := sysRepo(t, InitOptions{})
	ctx := context.Background()
	local := config.LocalPath(root)
	os.WriteFile(local, []byte("ui:\n  language: vi\n"), 0o644)
	e, err := PrepareConfigEdit(ctx, root, config.LayerLocal)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(e.TempPath)
	mine := "ui:\n  language: en\n"
	os.WriteFile(e.TempPath, []byte(mine), 0o600)
	os.Remove(local)
	if _, err := SaveConfigEdit(ctx, root, e, []byte(mine)); !errors.Is(err, ErrConfigChanged) {
		t.Fatalf("phải từ chối: %v", err)
	}
	e2, rb, err := RebaseConfigEdit(ctx, root, e)
	if err != nil || rb.Conflicts != 0 {
		t.Fatalf("%v %+v", err, rb)
	}
	temp, _ := os.ReadFile(e2.TempPath)
	if string(temp) != mine || e2.Existed || e2.Original != nil {
		t.Fatalf("giữ bản của người dùng: existed=%v %q", e2.Existed, temp)
	}
	if c, err := CheckConfigEdit(ctx, root, e2, temp); err != nil || !c.Valid || !c.Changed {
		t.Fatalf("%v %+v", err, c)
	}
	if _, err := SaveConfigEdit(ctx, root, e2, temp); err != nil {
		t.Fatalf("ghi lại tệp đã bị xoá: %v", err)
	}
	if b, _ := os.ReadFile(local); string(b) != mine {
		t.Fatalf("tệp mới phải là bản của người dùng:\n%s", b)
	}
}

// Lỗi lúc gộp (không đọc được bản tạm) trả lỗi và không đụng tới bản tạm hay tệp gốc.
func TestRebaseConfigEditErrorKeepsTemp(t *testing.T) {
	root := sysRepo(t, InitOptions{})
	ctx := context.Background()
	e, err := PrepareConfigEdit(ctx, root, config.LayerProject)
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(e.TempPath)
	if _, _, err := RebaseConfigEdit(ctx, root, e); err == nil {
		t.Fatal("thiếu bản tạm phải lỗi")
	}
	os.WriteFile(e.TempPath, []byte("x: 1\n"), 0o600)
	defer os.Remove(e.TempPath)
	if _, _, err := RebaseConfigEdit(ctx, t.TempDir(), e); err == nil {
		t.Fatal("thư mục chưa init phải lỗi")
	}
	if b, _ := os.ReadFile(e.TempPath); string(b) != "x: 1\n" {
		t.Fatalf("bản tạm phải còn nguyên: %q", b)
	}
}

func TestMerge3(t *testing.T) {
	cases := []struct {
		name, base, ours, theirs, want string
		conflicts                      int
	}{
		{"cả hai thêm ở chỗ khác nhau", "a\nb\nc\n", "a\nB\nc\n", "a\nb\nc\nd\n", "a\nB\nc\nd\n", 0},
		{"giống nhau", "a\nb\n", "a\nX\n", "a\nX\n", "a\nX\n", 0},
		{"chỉ bên kia đổi", "a\nb\n", "a\nb\n", "a\nZ\n", "a\nZ\n", 0},
		{"không có xuống dòng cuối", "a\nb", "a\nB", "a\nb\nc", "a\n" + ConflictMine + "\nB\n" + ConflictSep + "\nb\nc\n" + ConflictTheirs + "\n", 1},
		{"rỗng", "", "x\n", "", "x\n", 0},
	}
	for _, tc := range cases {
		got, n := Merge3(tc.base, tc.ours, tc.theirs)
		if got != tc.want || n != tc.conflicts {
			t.Errorf("%s: got %q (%d xung đột), muốn %q (%d)", tc.name, got, n, tc.want, tc.conflicts)
		}
	}
	if !HasConflictMarkers([]byte("x\n"+ConflictMine+"\n"+ConflictSep+"\n"+ConflictTheirs+"\n")) || HasConflictMarkers([]byte("=======\n")) {
		t.Error("HasConflictMarkers sai")
	}
}

// 5a.2: local.yml có `language:` rỗng (kèm chú thích) vẫn ghi ra `language: vi` hợp lệ qua ValidateWith.
func TestSetUILanguageEmptyValue(t *testing.T) {
	root := sysRepo(t, InitOptions{})
	local := config.LocalPath(root)
	os.WriteFile(local, []byte("ui:\n  language: # chú thích\n"), 0o644)
	res, err := SetUILanguage(context.Background(), root, UILanguageOptions{Language: "vi", Layer: "local"})
	if err != nil || !res.Changed {
		t.Fatalf("%v %+v", err, res)
	}
	b, _ := os.ReadFile(local)
	if string(b) != "ui:\n  language: vi # chú thích\n" {
		t.Fatalf("%q", b)
	}
	if _, err := config.ValidateWith(root, config.LayerLocal, b); err != nil {
		t.Fatal(err)
	}
}

// 5b.3/5a.4: core.lock ghi phiên bản cũ, nội dung skill khớp → PlanSync vẫn coi core.lock là thay đổi (có diff);
// cùng phiên bản thì không; lock bị xoá cũng là thay đổi. Sync thật ghi lock mang phiên bản binary.
func TestPlanSyncCountsCoreLock(t *testing.T) {
	root := sysRepo(t, InitOptions{Version: "0.1.0"})
	ctx := context.Background()
	lock := filepath.Join(root, filepath.FromSlash(".bint/skills/core.lock"))
	plan, err := PlanSync(ctx, root, "0.1.0", SyncOptions{})
	if err != nil || plan.Changes != 0 || plan.LockChanged {
		t.Fatalf("cùng phiên bản: %v %+v", err, plan)
	}
	before, _ := os.ReadFile(lock)
	plan, err = PlanSync(ctx, root, "9.9.9", SyncOptions{})
	if err != nil || !plan.LockChanged || plan.Changes != 1 || !strings.Contains(plan.LockDiff, "9.9.9") {
		t.Fatalf("lock lệch phiên bản phải là thay đổi: %v %+v", err, plan)
	}
	if after, _ := os.ReadFile(lock); string(after) != string(before) {
		t.Fatal("PlanSync không được ghi core.lock")
	}
	// Thiếu tệp lock.
	os.Remove(lock)
	plan, err = PlanSync(ctx, root, "0.1.0", SyncOptions{})
	if err != nil || !plan.LockChanged || plan.Changes == 0 {
		t.Fatalf("thiếu lock: %v %+v", err, plan)
	}
	if _, err := Sync(ctx, root, "9.9.9", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(lock); !strings.Contains(string(b), "9.9.9") {
		t.Fatalf("lock phải mang phiên bản binary:\n%s", b)
	}
	if plan, _ := PlanSync(ctx, root, "9.9.9", SyncOptions{}); plan.Changes != 0 || plan.LockChanged {
		t.Fatalf("sau sync phải khớp: %+v", plan)
	}
}

// 5b.1: SetupSteps biến thay đổi của form Cài đặt repo thành các lần chạy init tuần tự nhắm đúng phạm vi hook.
func TestSetupSteps(t *testing.T) {
	st := func(claudeScopes, codexScopes []string) InitState {
		return InitState{Root: "/r", Enabled: []string{"claude", "codex"}, Harnesses: []HarnessInitState{
			{Name: "claude", Enabled: true, HasHooks: true, HookScopes: claudeScopes},
			{Name: "codex", Enabled: true, HasHooks: true, HookScopes: codexScopes},
		}}
	}
	type step = InitFlagOptions
	off := func(sc string) step { return step{NoHooks: true, HooksScope: sc} }
	on := func(sc string) step { return step{Hooks: true, HooksScope: sc} }
	cases := []struct {
		name  string
		s     InitState
		ch    InitFlagOptions
		scope string
		want  []step
	}{
		{"không đổi hook: giữ nguyên", st(nil, nil), InitFlagOptions{MCP: true}, "local", []step{{MCP: true}}},
		{"tắt, hook ở project", st([]string{"project"}, nil), InitFlagOptions{NoHooks: true}, "project", []step{off("project")}},
		{"tắt, hook ở local và project của claude", st([]string{"local", "project"}, nil), InitFlagOptions{NoHooks: true}, "local",
			[]step{off("local"), off("project")}},
		{"tắt, hook ở user", st([]string{"user"}, nil), InitFlagOptions{NoHooks: true}, "user", []step{off("user")}},
		{"tắt, Codex dùng chung tệp local=project: một bước", st(nil, []string{"local", "project"}), InitFlagOptions{NoHooks: true}, "local",
			[]step{off("local")}},
		{"tắt kèm cờ khác: cờ đi kèm bước đầu", st([]string{"local", "project"}, nil), InitFlagOptions{NoHooks: true, MCP: true}, "local",
			[]step{{NoHooks: true, HooksScope: "local", MCP: true}, off("project")}},
		{"đổi local → project: gỡ trước, cài sau", st([]string{"local"}, nil), InitFlagOptions{Hooks: true, HooksScope: "project"}, "project",
			[]step{off("local"), on("project")}},
		{"đổi project → user", st([]string{"project"}, nil), InitFlagOptions{Hooks: true, HooksScope: "user"}, "user",
			[]step{off("project"), on("user")}},
		{"đổi local → project với Codex dùng chung tệp: không gỡ tệp sắp cài", st([]string{"local"}, []string{"local", "project"}),
			InitFlagOptions{Hooks: true, HooksScope: "project"}, "project", []step{off("local"), on("project")}},
		{"Codex một mình đổi local → project: chỉ cài", st(nil, []string{"local", "project"}),
			InitFlagOptions{Hooks: true, HooksScope: "project"}, "project", []step{on("project")}},
		{"bật mới: phạm vi tường minh", st(nil, nil), InitFlagOptions{Hooks: true}, "local", []step{on("local")}},
		{"bật mới ở project", st(nil, nil), InitFlagOptions{Hooks: true, HooksScope: "project"}, "project", []step{on("project")}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := SetupSteps(c.s, c.ch, c.scope)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got  %+v\nwant %+v", got, c.want)
			}
		})
	}
}

// 5b.1: chạy thật các bước của SetupSteps (claude + codex, hook local → project): sau khi chạy mỗi harness chỉ còn hook ở project và
// hook Codex (tệp chung local=project) không bị mất.
func TestSetupStepsRunMovesHooksWithSharedCodexFile(t *testing.T) {
	root := sysRepo(t, InitOptions{Harnesses: []string{"claude", "codex"}, Hooks: boolPtrT(true)})
	ctx := context.Background()
	st, err := ReadInitState(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	form := st.FormDefaults()
	if !form.Hooks || form.HooksScope != "local" {
		t.Fatalf("điều kiện thử: %+v", form)
	}
	steps := SetupSteps(st, InitFlagOptions{Hooks: true, HooksScope: "project"}, "project")
	if len(steps) != 2 || !steps[0].NoHooks || !steps[1].Hooks {
		t.Fatalf("bước: %+v", steps)
	}
	for _, s := range steps {
		io, err := s.ToInit(root, "bint", "test")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Init(ctx, io); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := ReadInitState(ctx, root)
	for _, h := range after.Harnesses {
		if !h.Enabled || !h.HasHooks {
			continue
		}
		want := []string{"project"}
		if h.Name == "codex" { // tệp chung: local và project cùng trỏ .codex/hooks.json
			want = []string{"local", "project"}
		}
		if !reflect.DeepEqual(h.HookScopes, want) {
			t.Errorf("%s: HookScopes=%v, muốn %v", h.Name, h.HookScopes, want)
		}
	}
	if got := after.HooksScope(); got != "project" {
		t.Errorf("HooksScope=%q", got)
	}
}

func boolPtrT(b bool) *bool { return &b }

// 5a.5: SetUILanguage báo ui.language hiệu lực sau khi ghép lớp và lớp che giá trị vừa ghi.
func TestSetUILanguageReportsEffective(t *testing.T) {
	root := sysRepo(t, InitOptions{})
	ctx := context.Background()
	// Ghi vào local: hiệu lực đúng giá trị vừa ghi, không bị che.
	r, err := SetUILanguage(ctx, root, UILanguageOptions{Language: "vi", Layer: "local"})
	if err != nil || r.Effective != "vi" || r.ShadowedBy != "" {
		t.Fatalf("%v %+v", err, r)
	}
	// Ghi vào project khi local.yml đặt vi: hiệu lực vẫn là vi, bị lớp local che.
	r, err = SetUILanguage(ctx, root, UILanguageOptions{Language: "en", Layer: "project"})
	if err != nil || !r.Changed || r.Effective != "vi" || r.ShadowedBy != config.LayerLocal {
		t.Fatalf("project bị local che: %v %+v", err, r)
	}
	// Local trùng giá trị: không bị che.
	r, err = SetUILanguage(ctx, root, UILanguageOptions{Language: "vi", Layer: "project"})
	if err != nil || r.Effective != "vi" || r.ShadowedBy != "" {
		t.Fatalf("%v %+v", err, r)
	}
	// Không đổi gì (giá trị đã đúng) vẫn báo hiệu lực.
	r, err = SetUILanguage(ctx, root, UILanguageOptions{Language: "vi", Layer: "project"})
	if err != nil || r.Changed || r.Effective != "vi" {
		t.Fatalf("%v %+v", err, r)
	}
}
