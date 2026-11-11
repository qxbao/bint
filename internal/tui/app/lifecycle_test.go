package app

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	bintapp "bint/internal/app"
	"bint/internal/config"
	"bint/internal/harness/claude"
	"bint/internal/testutil"
	"bint/internal/tui/actions"
	"bint/internal/tui/screens/overview"
	syscreen "bint/internal/tui/screens/system"
	"bint/internal/tui/theme"
)

// Kiểm thử vòng đời giai đoạn 5b trên repository thật (dịch vụ thật) với hàm cập nhật/exec giả: Cài đặt repo, hộp nâng cấu hình,
// Cập nhật Bint và hộp Khởi động lại.

type lifeCalls struct {
	mu sync.Mutex
	n  map[string]int
	o  map[string]any
}

func (c *lifeCalls) count(id string) int { c.mu.Lock(); defer c.mu.Unlock(); return c.n[id] }
func (c *lifeCalls) last(id string) any  { c.mu.Lock(); defer c.mu.Unlock(); return c.o[id] }

// lifeFixture dựng repo thật đã init (hook tắt, không skill) và mô hình dùng dịch vụ thật; mọi Do của hành động được đếm.
func lifeFixture(t *testing.T, mut func(*Options)) (*fixture, string, *lifeCalls) {
	t.Helper()
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	testutil.Git(t, root, "config", "user.name", "t")
	testutil.Git(t, root, "config", "user.email", "t@example.com")
	if _, err := bintapp.Init(context.Background(), bintapp.InitOptions{Dir: root, BintPath: "bint", Harnesses: []string{"claude"}, Hooks: boolPtr(false), NoSkills: true, Version: "0.1.0"}); err != nil {
		t.Fatal(err)
	}
	c := &lifeCalls{n: map[string]int{}, o: map[string]any{}}
	var list []actions.Action
	for _, a := range actions.Default().All() {
		if orig := a.Do; orig != nil {
			id := a.ID
			a.Do = func(ctx context.Context, env actions.Env, o any) tea.Msg {
				c.mu.Lock()
				c.n[id]++
				c.o[id] = o
				c.mu.Unlock()
				return orig(ctx, env, o)
			}
		}
		list = append(list, a)
	}
	o := Options{Dir: root, Version: "0.1.0", BintPath: "bint", Lang: "vi", Mode: theme.Mono, Registry: actions.NewRegistry(list), SpinnerFPS: time.Millisecond,
		Loader: func(context.Context, string) (overview.Data, error) {
			return overview.Data{Status: &bintapp.StatusReport{Repo: root}}, nil
		}}
	if mut != nil {
		mut(&o)
	}
	f := &fixture{t: t, root: root, msgs: make(chan tea.Msg, 256)}
	f.m = New(context.Background(), o)
	f.m.SetSize(140, 40)
	f.pump(func() bool { return !f.m.ov.Loading }, f.m.Init())
	return f, root, c
}

func (f *fixture) waitFor(sub string) {
	f.t.Helper()
	f.pump(func() bool { return strings.Contains(f.screen(), sub) }, nil)
}

// sysTab mở màn Hệ thống ở thẻ con tab (chỉ số).
func (f *fixture) sysTab(tab int) {
	f.t.Helper()
	f.press("7")
	for i := 0; i < tab; i++ {
		f.press(".")
	}
}

// Hệ thống › Cài đặt repo: không đổi gì thì không chạy init; đổi MCP thì dòng CLI chỉ có cờ đã đổi, có hộp xem trước và sau khi
// xác nhận MCP đã đăng ký; harness không đổi được ở đây.
func TestSetupTabOnlySendsWhatChanged(t *testing.T) {
	f, root, c := lifeFixture(t, nil)
	f.sysTab(syscreen.TabSetup)
	f.waitFor("Mẫu cấu hình")
	out := f.screen()
	for _, want := range []string{"Cài đặt repo", "claude", "hook", "MCP", "Git", "AGENTS.md", "theo mẫu hiện tại"} {
		if !strings.Contains(out, want) {
			t.Errorf("thẻ Cài đặt repo thiếu %q:\n%s", want, out)
		}
	}
	// Không đổi gì: gửi form ngay, không có hành động init nào chạy.
	f.press("e")
	if !f.m.sy.Capturing() || !strings.Contains(f.screen(), "Đăng ký MCP") {
		t.Fatalf("phải mở form init điền sẵn:\n%s", f.screen())
	}
	f.press("ctrl+s")
	if out := f.screen(); !strings.Contains(out, "Không có gì thay đổi") || f.m.sy.Capturing() {
		t.Fatalf("không đổi gì phải báo và đóng form:\n%s", out)
	}
	if c.count("system.repo_plan") != 0 || c.count("system.repo_apply") != 0 || c.count("init.run") != 0 {
		t.Fatalf("không đổi gì thì không được chạy init: plan=%d apply=%d", c.count("system.repo_plan"), c.count("system.repo_apply"))
	}
	// Đổi harness: bị từ chối, form còn mở.
	f.press("e")
	f.typeText(", codex")
	f.press("ctrl+s")
	if out := f.screen(); !strings.Contains(out, "Harness đổi ở thẻ Cấu hình") || !f.m.sy.Capturing() || c.count("system.repo_plan") != 0 {
		t.Fatalf("đổi harness phải bị từ chối:\n%s", out)
	}
	f.press("esc")
	// Đổi MCP: tab tới ô "Đăng ký MCP" (ô thứ 6) rồi Space.
	f.press("e", "tab", "tab", "tab", "tab", "tab", " ", "ctrl+s")
	f.waitFor("Xem trước init")
	out = f.screen()
	for _, want := range []string{".mcp.json", "bint init --mcp", "Chạy init"} {
		if !strings.Contains(out, want) {
			t.Errorf("hộp xem trước thiếu %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "bint init --mcp --") || strings.Contains(out, "--hooks") || strings.Contains(out, "--harness") {
		t.Errorf("dòng CLI chỉ được chứa cờ đã đổi:\n%s", out)
	}
	if st, _ := bintapp.ReadInitState(context.Background(), root); st.MCPOn() {
		t.Fatal("xem trước chưa được ghi MCP")
	}
	o := c.last("system.repo_plan").(bintapp.InitFlagOptions)
	if !o.MCP || o.NoMCP || o.Hooks || o.NoHooks || o.GitMode != "" || o.AgentsMD || len(o.Harnesses) != 0 {
		t.Fatalf("app chỉ được nhận phần đã đổi: %+v", o)
	}
	f.press("enter")
	f.pump(func() bool {
		return c.count("system.repo_apply") == 1 && strings.Contains(f.screen(), "Kết quả init")
	}, nil)
	if got := c.last("system.repo_apply").(bintapp.InitFlagOptions); !got.MCP || got.Hooks || got.NoHooks || len(got.Harnesses) != 0 {
		t.Fatalf("apply nhận %+v", got)
	}
	if st, _ := bintapp.ReadInitState(context.Background(), root); !st.MCPOn() {
		t.Fatalf("MCP phải đã đăng ký sau khi xác nhận: %+v", st)
	}
	hasAll(t, f.screen(), ".mcp.json")
}

// Chỉ đọc: thẻ Cài đặt repo không mở form ghi.
func TestSetupTabReadOnly(t *testing.T) {
	f, _, c := lifeFixture(t, func(o *Options) { o.ReadOnly = true })
	f.sysTab(syscreen.TabSetup)
	f.waitFor("Mẫu cấu hình")
	f.press("e", "enter", "U")
	if f.m.sy.Capturing() || c.count("system.repo_plan") != 0 || c.count("system.config_upgrade_plan") != 0 {
		t.Fatalf("chỉ đọc không được mở form/chạy init:\n%s", f.screen())
	}
}

// oldConfig biến cấu hình thành mẫu cũ (bỏ khối models của claude), trả nội dung cũ.
func oldConfig(t *testing.T, root string) string {
	t.Helper()
	p := config.ProjectPath(root)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	cur := string(b)
	i, j := strings.Index(cur, "  # models:"), strings.Index(cur, "  codex:")
	if i < 0 || j < i {
		t.Fatalf("mẫu không có khối models của claude")
	}
	old := cur[:i] + cur[j:]
	if err := os.WriteFile(p, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	return old
}

// Hộp nâng cấu hình: diff khớp config.Upgrade; từ chối thì tệp không đổi; xác nhận thì tệp bằng nội dung đã nâng và hộp báo đường
// dẫn bản sao lưu (có thật).
func TestConfigUpgradeDialog(t *testing.T) {
	f, root, c := lifeFixture(t, nil)
	old := oldConfig(t, root)
	up, err := config.Upgrade([]byte(old))
	if err != nil || !up.Changed() {
		t.Fatalf("điều kiện thử: %v", err)
	}
	f.sysTab(syscreen.TabConfig)
	f.waitFor("Lớp nguồn")
	f.press("U")
	f.waitFor("Nâng mẫu cấu hình")
	out := f.screen()
	hasAll(t, out, ".bint/config.yml", "Thêm khoá:", "harnesses.claude", "sao lưu", "Nâng cấp", "Giữ nguyên")
	if !strings.Contains(out, "+") {
		t.Fatalf("hộp phải hiện diff:\n%s", out)
	}
	// Từ chối: tệp không đổi.
	f.press("esc")
	if b, _ := os.ReadFile(config.ProjectPath(root)); string(b) != old || c.count("system.config_upgrade") != 0 {
		t.Fatal("từ chối thì tệp phải giữ nguyên và không ghi")
	}
	// Đồng ý.
	f.press("U")
	f.waitFor("Nâng mẫu cấu hình")
	f.press("enter")
	f.waitFor("Đã nâng cấu hình")
	if b, _ := os.ReadFile(config.ProjectPath(root)); string(b) != string(up.Content) {
		t.Fatal("tệp phải bằng nội dung config.Upgrade")
	}
	rep, _ := c.last("system.config_upgrade").(*bintapp.InitReport)
	_ = rep
	out = f.screen()
	hasAll(t, out, "Bản sao lưu")
	var bak string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "backups") {
			bak = strings.Trim(strings.TrimSpace(strings.Trim(l, "│")), " ")
		}
	}
	if bak == "" {
		t.Fatalf("hộp phải nêu đường dẫn bản sao lưu:\n%s", out)
	}
	// Đường dẫn trong hộp đã rút gọn ~; kiểm bản sao lưu thật nằm trong thư mục trạng thái.
	found := false
	filepath.WalkDir(os.Getenv("BINT_STATE_DIR"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.Contains(p, "backups") {
			if b, err := os.ReadFile(p); err == nil && string(b) == old {
				found = true
			}
		}
		return nil
	})
	if !found {
		t.Fatalf("không thấy bản sao lưu có nội dung cũ trong %s", os.Getenv("BINT_STATE_DIR"))
	}
	f.press("enter")
	// Sau khi nâng không còn gì để nâng.
	f.press("U")
	f.waitFor("đã theo mẫu hiện tại")
}

// Worktree phụ: nâng cấu hình bị từ chối kèm đường dẫn worktree chính, không đổi tệp nào.
func TestConfigUpgradeRefusedInLinkedWorktree(t *testing.T) {
	f, root, c := lifeFixture(t, nil)
	oldConfig(t, root)
	testutil.Git(t, root, "add", "-A")
	testutil.Git(t, root, "commit", "-q", "-m", "cfg")
	wt := filepath.Join(t.TempDir(), "wt")
	testutil.Git(t, root, "worktree", "add", "-q", wt, "-b", "side")
	before, _ := os.ReadFile(config.ProjectPath(wt))
	mainBefore, _ := os.ReadFile(config.ProjectPath(root))
	f2 := &fixture{t: t, root: wt, msgs: make(chan tea.Msg, 256)}
	f2.m = New(context.Background(), Options{Dir: wt, Version: "0.1.0", Lang: "vi", Mode: theme.Mono, Registry: f.m.reg, SpinnerFPS: time.Millisecond,
		Loader: func(context.Context, string) (overview.Data, error) {
			return overview.Data{Status: &bintapp.StatusReport{Repo: wt}}, nil
		}})
	f2.m.SetSize(140, 40)
	f2.pump(func() bool { return !f2.m.ov.Loading }, f2.m.Init())
	f2.sysTab(syscreen.TabConfig)
	f2.waitFor("Lớp nguồn")
	f2.press("U")
	f2.waitFor("worktree chính")
	out := f2.screen()
	if !strings.Contains(out, "cd "+root) && !strings.Contains(out, filepath.Base(root)) {
		t.Fatalf("phải nêu đường dẫn worktree chính %s:\n%s", root, out)
	}
	if c.count("system.config_upgrade") != 0 {
		t.Fatal("không được ghi khi ở worktree phụ")
	}
	after, _ := os.ReadFile(config.ProjectPath(wt))
	mainAfter, _ := os.ReadFile(config.ProjectPath(root))
	if string(before) != string(after) || string(mainBefore) != string(mainAfter) {
		t.Fatal("không tệp nào được đổi")
	}
}

// Hộp thư: dòng doctor báo mẫu cấu hình cũ dẫn thẳng tới hộp nâng cấu hình (không phải thẻ Doctor).
func TestInboxConfigTemplateRowOpensUpgradeDialog(t *testing.T) {
	var root string
	f, r, _ := lifeFixture(t, func(o *Options) {
		o.Loader = func(context.Context, string) (overview.Data, error) {
			return overview.Data{Status: &bintapp.StatusReport{Repo: root}, Pending: &bintapp.PendingWorkReport{
				Doctor: []bintapp.Check{{Name: bintapp.CheckConfigTemplate, Level: bintapp.LevelWarn, Detail: "theo mẫu cũ, thiếu: harnesses.claude.models — chạy `bint init --update`"}}}}, nil
		}
	})
	root = r
	oldConfig(t, root)
	f.press("r") // nạp lại Tổng quan với dữ liệu mới
	f.pump(func() bool { return !f.m.ov.Loading }, nil)
	in := f.m.ov.Data.Inbox()
	if len(in) != 1 || !in[0].ConfigUpgrade {
		t.Fatalf("dòng mẫu cũ phải dẫn tới hộp nâng cấu hình: %+v", in)
	}
	f.press("enter")
	f.waitFor("Nâng mẫu cấu hình")
	if f.m.Screen() != actions.System {
		t.Fatalf("phải ở màn Hệ thống, nhận %q", f.m.Screen())
	}
}

// Hộp thư: core.lock khác phiên bản binary có mục version_skew; Enter mở Hệ thống › Skill.
func TestInboxVersionSkewOpensSkills(t *testing.T) {
	f, _, _ := lifeFixture(t, func(o *Options) {
		o.Version = "9.9.9"
		o.Loader = func(context.Context, string) (overview.Data, error) {
			return overview.Data{Status: &bintapp.StatusReport{Repo: "/w"}, LockVersion: "0.1.0", Pending: &bintapp.PendingWorkReport{}}, nil
		}
	})
	in := f.m.ov.Data.Inbox()
	if len(in) != 1 || in[0].Key != "overview.inbox.version_skew" || in[0].SystemTab != "skills" {
		t.Fatalf("hộp thư: %+v", in)
	}
	out := f.screen()
	hasAll(t, out, "core.lock khác phiên bản binary", "0.1.0 ≠ 9.9.9")
	f.press("enter")
	if f.m.Screen() != actions.System || f.m.sy.Tab() != syscreen.TabSkills {
		t.Fatalf("Enter phải mở Hệ thống › Skill: screen=%q tab=%d", f.m.Screen(), f.m.sy.Tab())
	}
}

// fakeUpdate là hàm cập nhật giả: ghi log từng dòng vào Out (như go install) rồi trả kết quả cố định, không build thật.
func fakeUpdate(res bintapp.UpdateResult, err error, seen *bintapp.UpdateOptions) func(context.Context, bintapp.UpdateOptions) (bintapp.UpdateResult, error) {
	return func(_ context.Context, o bintapp.UpdateOptions) (bintapp.UpdateResult, error) {
		if seen != nil {
			*seen = o
		}
		o.Out.Write([]byte("go: downloading example.com/x v1.0.0\nbuilding bint\n"))
		o.Out.Write([]byte("linking"))
		return res, err
	}
}

// Cập nhật Bint chạy nền: log từng dòng hiện trong khung, kết thúc hiện bản cũ → mới, tệp, cảnh báo Mismatch; hàm giả nhận đúng
// Options; Unix hỏi khởi động lại, Enter đặt RestartPath là tệp vừa cài.
func TestUpdateTabRunsInBackgroundAndOffersRestart(t *testing.T) {
	var seen bintapp.UpdateOptions
	src := t.TempDir()
	res := bintapp.UpdateResult{Source: src, Target: "/home/u/go/bin/bint", Current: "/usr/local/bin/bint", OldRev: "abc1234", NewRev: "def5678", Mismatch: true}
	f, _, _ := lifeFixture(t, func(o *Options) {
		o.CanRestart = true
		o.Update = fakeUpdate(res, nil, &seen)
	})
	f.sysTab(syscreen.TabUpdate)
	f.waitFor("Mã nguồn")
	hasAll(t, f.screen(), "Cập nhật Bint", "0.1.0")
	f.press("u")
	if !f.m.sy.Capturing() || !strings.Contains(f.screen(), "Thư mục mã nguồn") {
		t.Fatalf("phải mở form source/pull:\n%s", f.screen())
	}
	f.press("tab", " ") // --pull
	f.press("ctrl+s")
	f.pump(func() bool { return strings.Contains(f.screen(), "Khởi động lại ngay?") }, nil)
	if !seen.Pull || seen.Out == nil {
		t.Fatalf("hàm cập nhật phải nhận Pull và Out: %+v", seen)
	}
	out := f.screen()
	hasAll(t, out, "abc1234 → def5678", "/home/u/go/bin/bint", "Bản đang chạy là /usr/local/bin/bint", "Khởi động lại")
	// Để sau: nhắc vẫn ở thẻ, log giữ lại.
	f.press("esc")
	out = f.screen()
	hasAll(t, out, "Log go install", "go: downloading example.com/x v1.0.0", "building bint", "linking", "Chưa khởi động lại")
	if f.m.RestartPath() != "" {
		t.Fatal("chưa chọn khởi động lại")
	}
	// R mở lại: Enter → thoát Bubble Tea và ghi đường dẫn tệp vừa cài.
	f.press("R")
	if _, cmd := f.m.Update(actions.RequestMsg{ID: "system.restart", Opts: actions.RestartOptions{Path: res.Target}}); cmd == nil {
		t.Fatal("system.restart không còn việc nền phải thoát")
	}
	if f.m.RestartPath() != res.Target || !f.m.quitting {
		t.Fatalf("RestartPath = %q quitting=%v", f.m.RestartPath(), f.m.quitting)
	}
}

// Còn việc nền khi khởi động lại: hỏi "huỷ an toàn rồi khởi động lại / ở lại"; Ở lại không đặt RestartPath, Đồng ý thì có.
func TestRestartWithBackgroundJobAsks(t *testing.T) {
	f, _, _ := lifeFixture(t, func(o *Options) { o.CanRestart = true })
	startInitJob(f.m)
	if _, cmd := f.m.Update(actions.RequestMsg{ID: "system.restart", Opts: actions.RestartOptions{Path: "/x/bint"}}); cmd != nil || f.m.exitDlg == nil || f.m.quitting {
		t.Fatalf("phải mở hộp hỏi, chưa thoát: dlg=%v quit=%v", f.m.exitDlg != nil, f.m.quitting)
	}
	hasAll(t, f.screen(), "Huỷ việc rồi khởi động lại", "Ở lại")
	f.press("esc")
	if f.m.exitDlg != nil || f.m.RestartPath() != "" || f.m.quitting {
		t.Fatal("Ở lại không được đặt RestartPath hay thoát")
	}
	f.m.Update(actions.RequestMsg{ID: "system.restart", Opts: actions.RestartOptions{Path: "/x/bint"}})
	f.m.Update(keyMsg("enter"))
	if f.m.RestartPath() != "/x/bint" || !f.m.exiting {
		t.Fatalf("đồng ý phải chờ việc dừng rồi khởi động lại: path=%q exiting=%v", f.m.RestartPath(), f.m.exiting)
	}
	// Việc kết thúc → thoát.
	if _, cmd := f.m.Update(actions.ResultMsg{ID: "init.run", Value: &bintapp.InitReport{}}); cmd == nil || !f.m.quitting {
		t.Fatal("hết việc phải thoát")
	}
}

// Windows (CanRestart = false): không exec, chỉ hộp yêu cầu đóng và mở lại, kèm bản cũ đổi tên.
func TestUpdateWithoutRestartSupportAsksToReopen(t *testing.T) {
	res := bintapp.UpdateResult{Target: "C:\\bin\\bint.exe", Current: "C:\\bin\\bint.exe", OldRev: "a1", NewRev: "b2", OldBackup: "C:\\bin\\bint.exe.old"}
	f, _, _ := lifeFixture(t, func(o *Options) { o.CanRestart = false; o.Update = fakeUpdate(res, nil, nil) })
	f.sysTab(syscreen.TabUpdate)
	f.waitFor("Mã nguồn")
	f.press("u", "ctrl+s")
	f.pump(func() bool { return strings.Contains(f.screen(), "Mở lại bint") }, nil)
	out := f.screen()
	hasAll(t, out, "đóng bint rồi mở lại", "bint.exe.old", "a1 → b2")
	if strings.Contains(out, "Khởi động lại ngay?") {
		t.Fatal("Windows không được hỏi khởi động lại tự động")
	}
	f.press("enter")
	if _, cmd := f.m.Update(actions.RequestMsg{ID: "system.restart", Opts: actions.RestartOptions{Path: res.Target}}); cmd != nil || f.m.RestartPath() != "" {
		t.Fatal("system.restart bị bỏ qua khi nền tảng không hỗ trợ")
	}
}

// Cập nhật lỗi: log và lỗi hiện trong thẻ, không hỏi khởi động lại; Esc huỷ việc nền.
func TestUpdateFailureShowsLogAndError(t *testing.T) {
	f, _, _ := lifeFixture(t, func(o *Options) {
		o.CanRestart = true
		o.Update = fakeUpdate(bintapp.UpdateResult{}, errors.New("go install ./cmd/bint: exit status 1"), nil)
	})
	f.sysTab(syscreen.TabUpdate)
	f.waitFor("Mã nguồn")
	f.press("u", "ctrl+s")
	f.pump(func() bool { return strings.Contains(f.screen(), "Cập nhật thất bại") && f.m.jobs.Len() == 0 }, nil)
	out := f.screen()
	hasAll(t, out, "exit status 1", "building bint")
	if strings.Contains(out, "Khởi động lại ngay?") || f.m.sy.Capturing() {
		t.Fatalf("lỗi không được hỏi khởi động lại:\n%s", out)
	}
}

// Trong lúc cập nhật chạy nền: spinner và log go install hiện từng dòng ngay khi có; Esc huỷ việc (context bị huỷ).
func TestUpdateTabShowsLiveLogAndEscCancels(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	f, _, _ := lifeFixture(t, func(o *Options) {
		o.CanRestart = true
		o.Update = func(ctx context.Context, u bintapp.UpdateOptions) (bintapp.UpdateResult, error) {
			u.Out.Write([]byte("go: downloading example.com/y v2.0.0\n"))
			once.Do(func() { close(started) })
			<-ctx.Done()
			return bintapp.UpdateResult{}, ctx.Err()
		}
	})
	f.sysTab(syscreen.TabUpdate)
	f.waitFor("Mã nguồn")
	f.press("u", "ctrl+s")
	f.waitFor("go: downloading example.com/y v2.0.0")
	<-started
	out := f.screen()
	hasAll(t, out, "Đang build và cài Bint", "Esc Huỷ")
	if f.m.jobs.Running("system.update") == nil {
		t.Fatal("việc cập nhật phải nằm trong bảng việc nền")
	}
	// Trong lúc chạy, thoát (q) hỏi vì còn việc nền.
	f.press("q")
	if f.m.exitDlg == nil {
		t.Fatal("còn việc cập nhật: phải mở hộp thoát")
	}
	hasAll(t, f.screen(), "bint update")
	f.press("esc") // Ở lại
	// Esc huỷ việc nền.
	f.pump(func() bool { return f.m.jobs.Len() == 0 && !strings.Contains(f.screen(), "Đang build") }, func() tea.Msg {
		_, cmd := f.m.Update(keyMsg("esc"))
		if cmd == nil {
			return nil
		}
		return cmd()
	})
	out = f.screen()
	hasAll(t, out, "Cập nhật thất bại", "go: downloading example.com/y v2.0.0")
	if strings.Contains(out, "Khởi động lại ngay?") {
		t.Fatal("huỷ không được hỏi khởi động lại")
	}
}

// 5b.3/5a.4: core.lock ghi phiên bản cũ nhưng nội dung skill khớp → Enter mục hộp thư lệch phiên bản, s, xác nhận thì
// core.lock mang phiên bản binary và mục hộp thư biến mất sau khi nạp lại.
func TestInboxVersionSkewSyncClearsItem(t *testing.T) {
	f, root, calls := lifeFixture(t, func(o *Options) {
		if _, err := bintapp.Sync(context.Background(), o.Dir, "0.1.0", bintapp.SyncOptions{}); err != nil {
			t.Fatal(err)
		}
		o.Version = "9.9.9"
		o.Loader = overview.Load
	})
	lock := filepath.Join(root, ".bint", "skills", "core.lock")
	f.press("r")
	f.pump(func() bool { return !f.m.ov.Loading }, nil)
	skew := func() bool {
		for _, it := range f.m.ov.Data.Inbox() {
			if it.Key == "overview.inbox.version_skew" {
				return true
			}
		}
		return false
	}
	if !skew() {
		t.Fatalf("hộp thư phải có mục lệch phiên bản: %+v", f.m.ov.Data.Inbox())
	}
	f.press("enter", "s")
	f.waitFor("core.lock") // hộp xem trước phải nêu core.lock, không phải 'đã cập nhật'
	if calls.count("system.sync") != 0 {
		t.Fatal("chưa xác nhận thì chưa sync")
	}
	f.press("enter")
	f.pump(func() bool { return calls.count("system.sync") == 1 }, nil)
	if b, _ := os.ReadFile(lock); !strings.Contains(string(b), "9.9.9") {
		t.Fatalf("core.lock phải mang phiên bản binary:\n%s", b)
	}
	f.press("1", "r")
	f.pump(func() bool { return !f.m.ov.Loading }, nil)
	if skew() {
		t.Fatalf("mục lệch phiên bản phải biến mất: %+v", f.m.ov.Data.Inbox())
	}
}

// installClaudeHook cài hook Claude ở phạm vi sc bằng app.Init (như `bint init --hooks --hooks-scope sc`).
func installClaudeHook(t *testing.T, root string, sc claude.Scope) {
	t.Helper()
	if _, err := bintapp.Init(context.Background(), bintapp.InitOptions{Dir: root, BintPath: "bint", Harnesses: []string{"claude"},
		Hooks: boolPtr(true), HooksScope: sc, NoSkills: true, Version: "0.1.0"}); err != nil {
		t.Fatal(err)
	}
}

func claudeHookScopes(t *testing.T, root string) []string {
	t.Helper()
	st, err := bintapp.ReadInitState(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range st.Harnesses {
		if h.Name == "claude" {
			return h.HookScopes
		}
	}
	t.Fatal("không thấy harness claude")
	return nil
}

// 5b.1: hook đang cài ở phạm vi project, bỏ chọn hook → hook bị gỡ khỏi settings.json (không chỉ settings.local.json); hộp xem
// trước nêu dòng CLI nhắm đúng phạm vi.
func TestSetupTabHookScopeOff(t *testing.T) {
	f, root, c := lifeFixture(t, nil)
	installClaudeHook(t, root, claude.ScopeProject)
	if got := claudeHookScopes(t, root); !slices.Equal(got, []string{"project"}) {
		t.Fatalf("điều kiện thử: %v", got)
	}
	f.sysTab(syscreen.TabSetup)
	f.waitFor("Mẫu cấu hình")
	f.press("e")
	hasAll(t, f.screen(), "Phạm vi hook")
	f.press("tab", " ", "ctrl+s") // tắt hook
	f.waitFor("Xem trước init")
	hasAll(t, f.screen(), "bint init --no-hooks --hooks-scope project")
	f.press("enter")
	f.pump(func() bool {
		return c.count("system.repo_apply") >= 1 && strings.Contains(f.screen(), "Kết quả init")
	}, nil)
	if got := claudeHookScopes(t, root); len(got) != 0 {
		t.Fatalf("hook phải bị gỡ khỏi mọi phạm vi, còn %v", got)
	}
	if b, _ := os.ReadFile(filepath.Join(root, ".claude", "settings.json")); strings.Contains(string(b), "hook claude") {
		t.Fatalf("settings.json còn hook:\n%s", b)
	}
}

// 5b.1: đổi phạm vi khi hook đang bật: gỡ ở phạm vi cũ TRƯỚC rồi cài ở phạm vi mới; hộp xem trước liệt kê từng bước kèm
// dòng CLI; sau khi chạy HookScopes chỉ còn phạm vi đã chọn và form mở lại điền sẵn đúng phạm vi.
func TestSetupTabHookScopeMove(t *testing.T) {
	f, root, c := lifeFixture(t, nil)
	installClaudeHook(t, root, claude.ScopeLocal)
	f.sysTab(syscreen.TabSetup)
	f.waitFor("Mẫu cấu hình")
	f.press("e", "tab", "tab", "tab", " ", "ctrl+s") // phạm vi: local → project
	f.waitFor("Xem trước init")
	out := f.screen()
	// Phạm vi local là mặc định của --hooks-scope nên dòng CLI của bước gỡ không ghi ra.
	hasAll(t, out, "Bước 1/2", "bint init --no-hooks", "Bước 2/2", "bint init --hooks --hooks-scope project", "settings.local.json", "settings.json")
	if strings.Index(out, "bint init --no-hooks") > strings.Index(out, "bint init --hooks --hooks-scope project") {
		t.Fatalf("phải gỡ ở phạm vi cũ trước khi cài ở phạm vi mới:\n%s", out)
	}
	f.press("enter")
	f.pump(func() bool {
		return c.count("system.repo_apply") >= 2 && strings.Contains(f.screen(), "Kết quả init")
	}, nil)
	if got := claudeHookScopes(t, root); !slices.Equal(got, []string{"project"}) {
		t.Fatalf("HookScopes phải chỉ còn project: %v", got)
	}
	if c.count("system.repo_apply") != 2 {
		t.Fatalf("phải chạy đúng hai bước: %d", c.count("system.repo_apply"))
	}
	f.press("esc") // đóng khung kết quả
	f.pump(func() bool { return strings.Contains(f.screen(), "Mẫu cấu hình") && !f.m.sy.Capturing() }, nil)
	f.press("e")
	// Form điền sẵn phạm vi project: gửi ngay thì không có gì thay đổi (nếu còn điền local thì form đề nghị đổi hook).
	f.press("ctrl+s")
	if out := f.screen(); !strings.Contains(out, "Không có gì thay đổi") || c.count("system.repo_plan") != 2 {
		t.Fatalf("form phải điền sẵn phạm vi project:\n%s", out)
	}
}

// gatedInit là hàm nạp InitState có điều khiển: chặn tới khi release, đếm số lần gọi, tuỳ chọn trả lỗi.
type gatedInit struct {
	release chan struct{}
	started atomic.Int32
	fail    atomic.Bool
}

func newGatedInit() *gatedInit { return &gatedInit{release: make(chan struct{})} }

func (g *gatedInit) load(ctx context.Context, dir string) (bintapp.InitState, error) {
	g.started.Add(1)
	<-g.release
	if g.fail.Load() {
		return bintapp.InitState{}, errors.New("không đọc được trạng thái")
	}
	return bintapp.ReadInitState(ctx, dir)
}

// 5b.2: form Cài đặt repo chỉ mở khi thẻ đã nạp xong không lỗi. (a) mở từ bảng lệnh khi chưa nạp: form mở SAU khi nạp xong với giá trị
// từ InitState thật (không đổi gì thì gửi form báo "không có gì thay đổi", chứ không phải đề nghị tắt hook đang bật); (b) e lúc đang
// nạp: không mở form, báo đang nạp; (c) e sau nạp lỗi: không mở form, báo lỗi.
func TestSetupFormWaitsForLoad(t *testing.T) {
	setup := func(t *testing.T) (*fixture, *gatedInit, *lifeCalls) {
		g := newGatedInit()
		f, r, c := lifeFixture(t, func(o *Options) { o.SystemLoad.Init = g.load })
		installClaudeHook(t, r, claude.ScopeProject) // trạng thái thật: hook đang bật ở phạm vi project
		return f, g, c
	}
	t.Run("bảng lệnh khi chưa nạp", func(t *testing.T) {
		f, g, c := setup(t)
		_, cmd := f.m.run("system.repo_plan")
		f.pump(func() bool { return g.started.Load() == 1 }, cmd)
		if f.m.sy.Capturing() {
			t.Fatalf("chưa nạp xong thì chưa được mở form:\n%s", f.screen())
		}
		close(g.release)
		f.pump(func() bool { return f.m.sy.Capturing() }, nil)
		hasAll(t, f.screen(), "Phạm vi hook")
		f.press("ctrl+s") // form điền từ trạng thái thật: hook project bật → không có gì thay đổi
		if out := f.screen(); !strings.Contains(out, "Không có gì thay đổi") || c.count("system.repo_plan") != 0 {
			t.Fatalf("form phải điền sẵn từ InitState thật:\n%s", out)
		}
	})
	t.Run("e lúc đang nạp", func(t *testing.T) {
		f, g, _ := setup(t)
		f.sysTab(syscreen.TabSetup)
		f.pump(func() bool { return g.started.Load() == 1 }, nil)
		f.press("e")
		if f.m.sy.Capturing() {
			t.Fatalf("đang nạp thì e không được mở form:\n%s", f.screen())
		}
		hasAll(t, f.screen(), "Đang nạp")
		close(g.release)
		f.pump(func() bool { return strings.Contains(f.screen(), "Mẫu cấu hình") }, nil)
		if f.m.sy.Capturing() {
			t.Fatal("phím e lúc đang nạp không được để lại việc mở form chờ")
		}
	})
	t.Run("e sau nạp lỗi", func(t *testing.T) {
		f, g, _ := setup(t)
		g.fail.Store(true)
		close(g.release)
		f.sysTab(syscreen.TabSetup)
		f.waitFor("Không nạp được dữ liệu")
		f.press("e")
		if f.m.sy.Capturing() {
			t.Fatalf("nạp lỗi thì không được mở form:\n%s", f.screen())
		}
		hasAll(t, f.screen(), "không đọc được trạng thái")
		_, cmd := f.m.run("system.repo_plan")
		f.settle(cmd)
		if f.m.sy.Capturing() {
			t.Fatal("bảng lệnh sau nạp lỗi cũng không được mở form")
		}
	})
}
