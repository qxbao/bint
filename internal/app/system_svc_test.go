package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
