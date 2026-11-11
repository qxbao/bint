package initwizard

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"bint/internal/app"
	"bint/internal/detect"
	"bint/internal/i18n"
	"bint/internal/tui/actions"
	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

func fake() []detect.Harness {
	return []detect.Harness{{Name: "claude", Present: true}, {Name: "codex"}, {Name: "antigravity"}}
}

func newWiz(ro bool) *Model {
	m := New(Options{Dir: "/work/demo", ReadOnly: ro, Detect: fake})
	m.SetEnv(theme.New(theme.Mono), i18n.MustLoad("vi"))
	return m
}

func key(m *Model, s string) tea.Cmd {
	var k tea.KeyPressMsg
	switch s {
	case "enter":
		k = tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		k = tea.KeyPressMsg{Code: tea.KeyEscape}
	default:
		k = tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
	}
	cmd, _ := m.Key(k)
	return cmd
}

// Options suy ra từ lựa chọn: Hooks/GitMode luôn tường minh nên app.Init không bao giờ hỏi giữa chừng.
func TestOptionsAreAlwaysExplicit(t *testing.T) {
	m := newWiz(false)
	o := m.Options()
	if !o.Hooks || o.NoHooks || o.GitMode != app.GitTrack || o.HooksScope != "local" || len(o.Harnesses) != 1 || o.Harnesses[0] != "claude" || o.NoSkills {
		t.Fatalf("mặc định: %+v", o)
	}
	io, err := o.ToInit("/r", "bint", "v")
	if err != nil || io.Hooks == nil || io.GitMode == "" || io.Confirm != nil {
		t.Fatalf("ToInit: %+v %v", io, err)
	}
	m.hooks, m.mcp, m.skills = false, true, false
	o = m.Options()
	if o.Hooks || !o.NoHooks || !o.MCP || !o.NoSkills {
		t.Fatalf("đổi lựa chọn: %+v", o)
	}
}

// Di chuyển con trỏ không đổi lựa chọn; Space mới chọn. Bước hook: chọn phạm vi mà hook vẫn bật.
func TestCursorMovesWithoutSelectingSpaceSelects(t *testing.T) {
	m := newWiz(false)
	key(m, "enter") // → bước hook
	if m.Step() != stepHooks {
		t.Fatalf("bước %d", m.Step())
	}
	key(m, "j")
	if !m.hooks {
		t.Fatal("di chuyển không được đổi lựa chọn")
	}
	key(m, "j")
	key(m, "j")
	key(m, " ")
	if !m.hooks || m.scope != "project" {
		t.Fatalf("hook=%v scope=%q", m.hooks, m.scope)
	}
	// Chọn "Không" cho hook: hàng phạm vi mờ nhưng vẫn giữ lựa chọn.
	key(m, "k")
	key(m, "k")
	key(m, " ")
	if m.hooks {
		t.Fatal("phải tắt hook")
	}
}

// Đi tới xem trước phát yêu cầu init.plan với Options đã chọn; Enter ở xem trước phát init.run; chưa có kế hoạch thì Enter không chạy.
func TestPreviewRequestsPlanThenRun(t *testing.T) {
	m := newWiz(false)
	cmd := key(m, "d")
	req, ok := cmd().(actions.RequestMsg)
	if !ok || req.ID != "init.plan" || m.Step() != stepPreview || !m.Busy() {
		t.Fatalf("d: %v step=%d", req, m.Step())
	}
	if c := key(m, "enter"); c != nil {
		t.Fatal("chưa có kế hoạch thì Enter không được chạy init")
	}
	m.Update(actions.ResultMsg{ID: "init.plan", Value: app.InitPlan{Items: []app.PlanItem{{Path: ".bint/config.yml", Change: app.PlanCreate}}}})
	c := key(m, "enter")
	run, ok := c().(actions.RequestMsg)
	if !ok || run.ID != "init.run" || !m.Running() {
		t.Fatalf("enter: %v running=%v", run, m.Running())
	}
	o := run.Opts.(app.InitFlagOptions)
	if !o.Hooks || o.GitMode != "track" {
		t.Fatalf("Options: %+v", o)
	}
	// Đang chạy: mọi phím bị nuốt.
	if c := key(m, "esc"); c != nil || !m.Running() {
		t.Fatal("đang chạy không được lùi")
	}
	m.Update(actions.ResultMsg{ID: "init.run", Value: &app.InitReport{NextSteps: []string{"Kiểm tra môi trường: bint doctor"}}})
	if !m.Done() || m.Running() {
		t.Fatal("xong")
	}
	cont, ok := key(m, "enter")().(ContinueMsg)
	if !ok || cont.Nav == nil || cont.Nav.SystemTab != "doctor" {
		t.Fatalf("Enter ở bước doctor phải dẫn tới Hệ thống › Doctor: %+v", cont)
	}
}

// Kế hoạch trả về sau khi người dùng đã lùi bị bỏ qua (không ghi đè trạng thái bước khác).
func TestStalePlanIsIgnoredAfterBack(t *testing.T) {
	m := newWiz(false)
	key(m, "d")
	key(m, "esc")
	m.Update(actions.ResultMsg{ID: "init.plan", Value: app.InitPlan{}})
	if m.Step() != stepGit || m.plan != nil {
		t.Fatalf("step=%d plan=%v", m.Step(), m.plan)
	}
}

func TestNavForMapsNextSteps(t *testing.T) {
	cases := map[string]actions.Screen{"Kiểm tra môi trường: bint doctor": actions.System, "Xem lượt chạy đã ghi nhận: bint runs list": actions.Runs,
		"Bootstrap tri thức: … rồi `bint knowledge review` và …": actions.Knowledge}
	for s, want := range cases {
		if n := NavFor(s); n == nil || n.Screen != want {
			t.Errorf("%q → %+v", s, n)
		}
	}
	if NavFor("Xác nhận lệnh kiểm thử: điền eval.test_command") != nil || NavFor("Commit .bint/config.yml") != nil {
		t.Error("bước không có ánh xạ chỉ hiện chữ")
	}
}

// Chế độ chỉ đọc: không phím nào được nhận; View nói rõ lý do.
func TestReadOnlyHandlesNoKeys(t *testing.T) {
	m := newWiz(true)
	for _, k := range []string{"enter", "d", "j", " ", "esc"} {
		if c, handled := m.Key(tea.KeyPressMsg{Code: []rune(k)[0], Text: k}); c != nil || handled {
			t.Errorf("phím %q phải không được xử lý ở chế độ chỉ đọc", k)
		}
	}
	if m.Command("init.run") != nil || m.Step() != 0 {
		t.Fatal("Command không được chạy")
	}
	out := ansi.Strip(m.View(ui.Env{Th: theme.New(theme.Mono), Cat: i18n.MustLoad("vi"), W: 100, H: 24}))
	if !strings.Contains(out, "chế độ chỉ đọc") || !strings.Contains(out, "bint init") {
		t.Fatalf("view:\n%s", out)
	}
}

// Báo cáo rút gọn home thành ~ trong chi tiết các bước.
func TestReportShortensHomeInStepDetails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	state := home + "/.local/state/bint/demo-7f3a9c"
	m := newWiz(false)
	m.done = true
	m.report = &app.InitReport{StateDir: state, Steps: []app.Step{
		{Name: "state", Status: app.StepCreated, Detail: state + " (schema v6)"},
		{Name: "hook claude", Status: app.StepWarn, Detail: "ghi vào " + home + "/.claude/settings.json"},
	}}
	out := ansi.Strip(m.View(ui.Env{Th: theme.New(theme.Mono), Cat: i18n.MustLoad("vi"), W: 100, H: 30}))
	if strings.Contains(out, home) {
		t.Errorf("báo cáo còn đường dẫn tuyệt đối dưới home %q:\n%s", home, out)
	}
	for _, want := range []string{"~/.local/state/bint/demo-7f3a9c (schema v6)", "~/.claude/settings.json"} {
		if !strings.Contains(out, want) {
			t.Errorf("báo cáo thiếu %q:\n%s", want, out)
		}
	}
}

// Cửa sổ hẹp / đường dẫn dài: rút phần đầu của chi tiết bằng "…" và giữ hậu tố; lỗi nạp lại cũng rút gọn home.
func TestReportKeepsDetailTailAndShortensLoadErr(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	state := "/var/lib/bint-state/projects/some-long-project-name-7f3a9c"
	m := newWiz(false)
	m.done = true
	m.report = &app.InitReport{StateDir: state, Steps: []app.Step{{Name: "state", Status: app.StepCreated, Detail: state + " (schema v6)"}}}
	m.SetLoadErr(errors.New("open " + home + "/.bint/x: denied"))
	env := ui.Env{Th: theme.New(theme.Mono), Cat: i18n.MustLoad("vi"), W: 60, H: 30}
	out := ansi.Strip(m.View(env))
	if !strings.Contains(out, "…") || !strings.Contains(out, "(schema v6)") {
		t.Errorf("phải rút phần đầu và giữ hậu tố (schema v6):\n%s", out)
	}
	if strings.Contains(out, home) || !strings.Contains(out, "~/.bint/x") {
		t.Errorf("lỗi nạp lại phải rút gọn home thành ~:\n%s", out)
	}
	for _, l := range strings.Split(out, "\n") {
		if text.Width(l) > env.W {
			t.Errorf("dòng rộng quá %d: %q", env.W, l)
		}
	}
}
