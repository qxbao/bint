package initplaceholder

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"bint/internal/app"
	"bint/internal/i18n"
	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

// Báo cáo khởi tạo rút gọn home thành ~ trong chi tiết các bước (StateDir dưới HOME).
func TestReportShortensHomeInStepDetails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	state := home + "/.local/state/bint/demo-7f3a9c"
	rep := &app.InitReport{
		StateDir: state,
		Steps: []app.Step{
			{Name: "state", Status: app.StepCreated, Detail: state + " (schema v6)"},
			{Name: "hook claude", Status: app.StepWarn, Detail: "ghi vào " + home + "/.claude/settings.json"},
		},
	}
	env := ui.Env{Th: theme.New(theme.Mono), Cat: i18n.MustLoad("vi"), W: 100, H: 30}
	out := ansi.Strip(Model{Dir: "/work/demo", Report: rep}.View(env))
	if strings.Contains(out, home) {
		t.Errorf("báo cáo còn đường dẫn tuyệt đối dưới home %q:\n%s", home, out)
	}
	for _, want := range []string{"~/.local/state/bint/demo-7f3a9c (schema v6)", "~/.claude/settings.json"} {
		if !strings.Contains(out, want) {
			t.Errorf("báo cáo thiếu %q:\n%s", want, out)
		}
	}
}

// Cửa sổ hẹp / đường dẫn dài (state ngoài home): rút phần đầu của chi tiết bằng "…" và giữ hậu tố
// "(schema v6)", không cắt đuôi; lỗi nạp lại cũng rút gọn home thành ~.
func TestReportKeepsDetailTailAndShortensLoadErr(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	state := "/var/lib/bint-state/projects/some-long-project-name-7f3a9c"
	rep := &app.InitReport{
		StateDir: state,
		Steps:    []app.Step{{Name: "state", Status: app.StepCreated, Detail: state + " (schema v6)"}},
	}
	env := ui.Env{Th: theme.New(theme.Mono), Cat: i18n.MustLoad("vi"), W: 60, H: 30}
	out := ansi.Strip(Model{Dir: "/work/demo", Report: rep, LoadErr: errors.New("open " + home + "/.bint/x: denied")}.View(env))
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
