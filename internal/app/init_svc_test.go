package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"bint/internal/config"
	"bint/internal/skills"
	"bint/internal/testutil"
)

func TestInitFlagOptionsValidate(t *testing.T) {
	ok := InitDefaults(config.Default())
	cases := []struct {
		name string
		mut  func(*InitFlagOptions)
		want string // rỗng = hợp lệ
	}{
		{"mặc định", func(*InitFlagOptions) {}, ""},
		{"--hooks", func(o *InitFlagOptions) { o.Hooks = true }, ""},
		{"--hooks + --no-hooks", func(o *InitFlagOptions) { o.Hooks, o.NoHooks = true, true }, "--hooks và --no-hooks"},
		{"--mcp", func(o *InitFlagOptions) { o.MCP = true }, ""},
		{"--mcp + --no-mcp", func(o *InitFlagOptions) { o.MCP, o.NoMCP = true, true }, "--mcp và --no-mcp"},
		{"--hooks-scope user", func(o *InitFlagOptions) { o.HooksScope = "user" }, ""},
		{"--hooks-scope sai", func(o *InitFlagOptions) { o.HooksScope = "global" }, "--hooks-scope"},
		{"--git local", func(o *InitFlagOptions) { o.GitMode = "local" }, ""},
		{"--git sai", func(o *InitFlagOptions) { o.GitMode = "nham" }, "--git"},
		{"--harness claude,codex", func(o *InitFlagOptions) { o.Harnesses = []string{"claude", "codex"} }, ""},
		{"--harness lạ", func(o *InitFlagOptions) { o.Harnesses = []string{"claude", "vim"} }, "harness không hỗ trợ"},
	}
	for _, c := range cases {
		o := ok
		c.mut(&o)
		err := o.Validate()
		if c.want == "" {
			if err != nil {
				t.Errorf("%s: %v", c.name, err)
			}
			continue
		}
		if err == nil || !IsUsage(err) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: muốn UsageError chứa %q, nhận %v", c.name, c.want, err)
		}
	}
}

func TestInitFlagOptionsToInit(t *testing.T) {
	get := func(o InitFlagOptions) InitOptions {
		t.Helper()
		got, err := o.ToInit("/r", "bint", "v1")
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if g := get(InitFlagOptions{}); g.Hooks != nil || g.MCP != nil || g.HooksScope != "local" || g.Dir != "/r" || g.Version != "v1" {
		t.Fatalf("rỗng: %+v", g)
	}
	if g := get(InitFlagOptions{Hooks: true, NoMCP: true, HooksScope: "user", GitMode: "local", Update: true, Force: true, NoSkills: true, AgentsMD: true}); g.Hooks == nil || !*g.Hooks ||
		g.MCP == nil || *g.MCP || g.HooksScope != "user" || g.GitMode != "local" || !g.Update || !g.Force || !g.NoSkills || !g.AgentsMD {
		t.Fatalf("đầy đủ: %+v", g)
	}
	if g := get(InitFlagOptions{NoHooks: true, MCP: true}); g.Hooks == nil || *g.Hooks || g.MCP == nil || !*g.MCP {
		t.Fatalf("no-hooks/mcp: %+v", g)
	}
	if _, err := (InitFlagOptions{HooksScope: "x"}).ToInit("/r", "bint", "v"); !IsUsage(err) {
		t.Fatalf("scope sai: %v", err)
	}
}

// snapshot chụp tên và băm của mọi tệp dưới các thư mục (bỏ .git), để chứng minh một thao tác không ghi gì.
func snapshot(t *testing.T, dirs ...string) []string {
	t.Helper()
	var out []string
	for _, d := range dirs {
		filepath.WalkDir(d, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if e.IsDir() {
				if e.Name() == ".git" {
					return filepath.SkipDir
				}
				out = append(out, "d "+p)
				return nil
			}
			b, _ := os.ReadFile(p)
			out = append(out, fmt.Sprintf("f %s %x", p, sha256.Sum256(b)))
			return nil
		})
	}
	return out
}

func TestInitStateAndPlanAreReadOnly(t *testing.T) {
	stateDir := testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	ctx := context.Background()
	everything := InitFlagOptions{Harnesses: []string{"claude", "codex"}, Hooks: true, MCP: true, AgentsMD: true, GitMode: "local", Update: true}

	// Chưa init.
	before := snapshot(t, root, stateDir)
	st, err := ReadInitState(ctx, root)
	if err != nil || !st.NotInit || st.Root == "" {
		t.Fatalf("chưa init: %+v %v", st, err)
	}
	pl, err := PlanInit(ctx, root, everything)
	if err != nil || !pl.FirstInit {
		t.Fatalf("plan chưa init: %+v %v", pl, err)
	}
	want := map[string]PlanChange{".bint/config.yml": PlanCreate, ".mcp.json": PlanCreate, ".git/info/exclude": PlanUpdate, "AGENTS.md": PlanCreate,
		".claude/settings.local.json": PlanCreate, ".codex/hooks.json": PlanCreate}
	for path, ch := range want {
		found := false
		for _, it := range pl.Items {
			if it.Path == path && it.Change == ch {
				found = true
			}
		}
		if !found {
			t.Errorf("plan thiếu %s (%s): %+v", path, ch, pl.Items)
		}
	}
	if !slices.Equal(before, snapshot(t, root, stateDir)) {
		t.Fatal("ReadInitState/PlanInit ghi tệp trên repo chưa init")
	}

	// Đã init với hook + MCP + git local.
	mustInit(t, InitOptions{Dir: root, Harnesses: []string{"claude"}, Hooks: yes(), MCP: yes(), GitMode: GitLocal, HooksScope: "project"})
	before = snapshot(t, root, stateDir)
	st, err = ReadInitState(ctx, root)
	if err != nil || st.NotInit {
		t.Fatalf("đã init: %+v %v", st, err)
	}
	if !slices.Equal(st.Enabled, []string{"claude"}) || !st.HooksOn() || !st.MCPOn() || st.GitMode != GitLocal || st.HooksScope() != "project" || st.AgentsBlock || st.ConfigOutdated {
		t.Fatalf("trạng thái: %+v", st)
	}
	d := st.FormDefaults()
	if !d.Hooks || !d.MCP || d.GitMode != GitLocal || d.HooksScope != "project" || d.AgentsMD || !slices.Equal(d.Harnesses, []string{"claude"}) {
		t.Fatalf("FormDefaults: %+v", d)
	}
	pl, err = PlanInit(ctx, root, InitFlagOptions{NoHooks: true, NoMCP: true, HooksScope: "project", GitMode: GitTrack, AgentsMD: true})
	if err != nil || pl.FirstInit {
		t.Fatalf("plan: %+v %v", pl, err)
	}
	for path, ch := range map[string]PlanChange{".claude/settings.json": PlanRemove, ".mcp.json": PlanRemove, ".git/info/exclude": PlanRemove, ".gitignore": PlanUpdate, "AGENTS.md": PlanCreate} {
		found := false
		for _, it := range pl.Items {
			if it.Path == path && it.Change == ch {
				found = true
			}
		}
		if !found {
			t.Errorf("plan thiếu %s (%s): %+v", path, ch, pl.Items)
		}
	}
	if pl.Changes() == 0 {
		t.Error("Changes() = 0")
	}
	if !slices.Equal(before, snapshot(t, root, stateDir)) {
		t.Fatal("ReadInitState/PlanInit ghi tệp trên repo đã init")
	}
	if _, err := PlanInit(ctx, root, InitFlagOptions{Hooks: true, NoHooks: true}); !IsUsage(err) {
		t.Fatalf("plan phải kiểm Validate: %v", err)
	}
}

// oldTemplate biến cấu hình vừa tạo thành kiểu cũ (bỏ khối models của claude).
func oldTemplate(t *testing.T, root string) string {
	t.Helper()
	p := config.ProjectPath(root)
	cur := read(t, p)
	i, j := strings.Index(cur, "  # models:"), strings.Index(cur, "  codex:")
	if i < 0 || j < i {
		t.Fatalf("mẫu không có khối models của claude:\n%s", cur)
	}
	old := cur[:i] + cur[j:]
	if err := os.WriteFile(p, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	return old
}

func TestDoctorAndStateReportOldConfigTemplate(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	ctx := context.Background()
	mustInit(t, InitOptions{Dir: root, Harnesses: []string{"claude"}, Hooks: no(), NoSkills: true})
	has := func() *Check {
		for _, c := range Doctor(ctx, root).Checks {
			if c.Name == CheckConfigTemplate {
				return &c
			}
		}
		return nil
	}
	if c := has(); c != nil {
		t.Fatalf("mẫu hiện tại không được có dòng mẫu cũ: %+v", c)
	}
	if st, _ := ReadInitState(ctx, root); st.ConfigOutdated {
		t.Fatal("ConfigOutdated với mẫu hiện tại")
	}
	old := oldTemplate(t, root)
	c := has()
	if c == nil || c.Level != LevelWarn || !strings.Contains(c.Detail, "bint init --update") {
		t.Fatalf("doctor: %+v", c)
	}
	st, _ := ReadInitState(ctx, root)
	if !st.ConfigOutdated || len(st.ConfigAdded) == 0 {
		t.Fatalf("state: %+v", st)
	}

	// Kế hoạch nâng cấp khớp config.Upgrade; chưa ghi gì.
	pl, err := PlanConfigUpgrade(ctx, root)
	if err != nil || !pl.Changed {
		t.Fatalf("plan: %+v %v", pl, err)
	}
	up, _ := config.Upgrade([]byte(old))
	if string(pl.Content) != string(up.Content) || !slices.Equal(pl.Added, up.Added) || !strings.Contains(pl.Diff, "+") {
		t.Fatalf("plan lệch config.Upgrade:\n%s", pl.Diff)
	}
	if read(t, config.ProjectPath(root)) != old {
		t.Fatal("PlanConfigUpgrade ghi tệp")
	}

	// Áp dụng: tệp bằng nội dung đã nâng, có bản sao lưu đúng nội dung cũ.
	rep, err := UpgradeConfig(ctx, root, ConfigUpgradeOptions{BintPath: "bint", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if read(t, config.ProjectPath(root)) != string(up.Content) {
		t.Fatal("tệp sau khi nâng không bằng nội dung config.Upgrade")
	}
	bak := ConfigBackupPath(rep)
	if b, err := os.ReadFile(bak); bak == "" || err != nil || string(b) != old {
		t.Fatalf("bản sao lưu %q: %v", bak, err)
	}
	if rep.ConfigBackup != bak { // 5b.4: trường có cấu trúc do upgradeConfig điền
		t.Fatalf("InitReport.ConfigBackup=%q, muốn %q", rep.ConfigBackup, bak)
	}
	if has() != nil {
		t.Fatal("sau khi nâng doctor không còn báo mẫu cũ")
	}
	if pl, _ := PlanConfigUpgrade(ctx, root); pl.Changed {
		t.Fatal("nâng lần hai không còn gì để đổi")
	}
}

func TestPlanConfigUpgradeRefusesLinkedWorktree(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	mustInit(t, InitOptions{Dir: root, Harnesses: []string{"claude"}, Hooks: no(), NoSkills: true})
	oldTemplate(t, root)
	testutil.Git(t, root, "add", "-A")
	testutil.Git(t, root, "-c", "user.name=t", "-c", "user.email=t@e.x", "commit", "-q", "-m", "cfg")
	wt := filepath.Join(t.TempDir(), "wt")
	testutil.Git(t, root, "worktree", "add", "-q", wt, "-b", "side")
	before := read(t, config.ProjectPath(wt))
	_, err := PlanConfigUpgrade(context.Background(), wt)
	var le LinkedWorktreeError
	if !errors.As(err, &le) || !strings.Contains(err.Error(), le.Main) || le.Main == "" {
		t.Fatalf("muốn LinkedWorktreeError kèm đường dẫn chính: %v", err)
	}
	if _, err := UpgradeConfig(context.Background(), wt, ConfigUpgradeOptions{BintPath: "bint"}); !errors.As(err, &le) {
		t.Fatalf("UpgradeConfig ở worktree phụ: %v", err)
	}
	if read(t, config.ProjectPath(wt)) != before {
		t.Fatal("worktree phụ bị đổi tệp")
	}
}

func TestVersionInfoLockMismatch(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	ctx := context.Background()
	if _, err := VersionInfo(ctx, root, "1.0"); !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("chưa init: %v", err)
	}
	mustInit(t, InitOptions{Dir: root, Harnesses: []string{"claude"}, Hooks: no(), Version: "1.0"})
	vs, err := VersionInfo(ctx, root, "1.0")
	if err != nil || vs.LockVersion != "1.0" || vs.LockMismatch || vs.ConfigOutdated || vs.Binary != "1.0" {
		t.Fatalf("cùng phiên bản: %+v %v", vs, err)
	}
	vs, _ = VersionInfo(ctx, root, "2.0")
	if !vs.LockMismatch || vs.LockVersion != "1.0" {
		t.Fatalf("khác phiên bản: %+v", vs)
	}
	lock := filepath.Join(root, filepath.FromSlash(skills.LockRel))
	os.Remove(lock)
	if vs, _ := VersionInfo(ctx, root, "2.0"); vs.LockMismatch || vs.LockVersion != "" {
		t.Fatalf("không có lock: %+v", vs)
	}
	oldTemplate(t, root)
	if vs, _ := VersionInfo(ctx, root, "2.0"); !vs.ConfigOutdated {
		t.Fatal("ConfigOutdated")
	}
}

func TestUpdateOptionsValidateAndDefaults(t *testing.T) {
	t.Setenv(SourceEnv, "")
	if err := (UpdateOptions{}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (UpdateOptions{Source: fakeSource(t, "bint"), Pull: true}).Validate(); err != nil {
		t.Fatal(err)
	}
	err := UpdateOptions{Source: fakeSource(t, "khac")}.Validate()
	if !IsUsage(err) || !errors.Is(err, ErrNoSource) {
		t.Fatalf("nguồn sai phải là UsageError(ErrNoSource): %v", err)
	}
	good := fakeSource(t, "bint")
	t.Setenv(SourceEnv, good)
	if d := UpdateDefaults(); d.Source != good || d.Pull {
		t.Fatalf("Defaults: %+v", d)
	}
}

// 5b.4: ConfigBackupPath đọc trường có cấu trúc, không phân tích câu chữ của Detail (đổi câu chữ không làm mất bản sao lưu).
func TestConfigBackupPathUsesStructuredField(t *testing.T) {
	rep := &InitReport{ConfigBackup: "/x/backups/config.yml", Steps: []Step{{Name: ".bint/config.yml", Status: StepUpdated, Detail: "old backup: elsewhere"}}}
	if got := ConfigBackupPath(rep); got != "/x/backups/config.yml" {
		t.Fatalf("%q", got)
	}
	if got := ConfigBackupPath(&InitReport{Steps: []Step{{Name: ".bint/config.yml", Status: StepUpdated, Detail: "thêm: x (bản cũ: /y)"}}}); got != "" {
		t.Fatalf("không được cắt chuỗi Detail: %q", got)
	}
	if ConfigBackupPath(nil) != "" {
		t.Fatal("nil")
	}
}
