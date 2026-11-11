// Package tui là giao diện terminal (Bubble Tea) của Bint. Điểm vào là Run; mô hình gốc ở
// internal/tui/app, các thành phần dùng chung ở internal/tui/components, hệ màu ở internal/tui/theme.
package tui

import (
	"context"
	"io"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"

	bintapp "bint/internal/app"
	"bint/internal/i18n"
	tuiapp "bint/internal/tui/app"
	"bint/internal/tui/gallery"
	"bint/internal/tui/theme"
)

// Options điều khiển một phiên TUI.
type Options struct {
	Dir      string
	Version  string
	BintPath string
	// Lang là giá trị cờ --lang (rỗng = không chỉ định).
	Lang string
	// ReadOnly: ẩn mọi hành động ghi.
	ReadOnly bool
	// Gallery mở trang trưng bày component thay cho giao diện chính.
	Gallery bool
	// Agent: đang trong phiên agent (hành động chỉ-dành-cho-người bị khoá).
	Agent bool
	// Args là đối số khi exec lại sau khi cập nhật (nil = os.Args); thay được trong kiểm thử.
	Args []string
	// Getenv thay được trong kiểm thử; nil = os.Getenv.
	Getenv func(string) string
	In     io.Reader
	Out    io.Writer
}

// ResolveLang chọn ngôn ngữ: --lang > BINT_LANG > ui.language > LC_ALL/LANG > vi.
func ResolveLang(flag, cfgLang string, getenv func(string) string) string {
	return i18n.Resolve(flag, getenv("BINT_LANG"), cfgLang, getenv("LC_ALL"), getenv("LC_MESSAGES"), getenv("LANG"))
}

// DetectMode chọn chế độ màu: NO_COLOR hoặc terminal không màu → đơn sắc; terminal 16 màu → ANSI16;
// ngược lại theo nền terminal.
func DetectMode(getenv func(string) string, p colorprofile.Profile, darkBackground bool) theme.Mode {
	noColor := strings.TrimSpace(getenv("NO_COLOR")) != "" || p <= colorprofile.ASCII
	return theme.Detect(noColor, p == colorprofile.ANSI, darkBackground)
}

// configLang đọc ui.language từ cấu hình của repository ("" nếu không đọc được).
func configLang(ctx context.Context, dir string) string {
	ws, err := bintapp.OpenWorkspace(ctx, dir, false)
	if err != nil {
		return ""
	}
	return ws.Config.UI.Language
}

// Run mở TUI và chạy tới khi người dùng thoát.
func Run(ctx context.Context, o Options) error {
	getenv := o.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	out := o.Out
	if out == nil {
		out = os.Stdout
	}
	lang := ResolveLang(o.Lang, configLang(ctx, o.Dir), getenv)
	p := colorprofile.Detect(out, os.Environ())
	dark := true
	if in, ok := o.In.(*os.File); ok || o.In == nil {
		if !ok {
			in = os.Stdin
		}
		if of, ok := out.(*os.File); ok {
			dark = lipgloss.HasDarkBackground(in, of)
		}
	}
	mode := DetectMode(getenv, p, dark)

	var model tea.Model
	var root *tuiapp.Model
	if o.Gallery {
		model = gallery.New(gallery.Options{Lang: lang, Mode: mode})
	} else {
		root = tuiapp.New(ctx, tuiapp.Options{Dir: o.Dir, Version: o.Version, BintPath: o.BintPath, Lang: lang, Mode: mode,
			Agent: o.Agent, ReadOnly: o.ReadOnly, CanRestart: RestartSupported})
		model = root
	}
	opts := []tea.ProgramOption{tea.WithContext(ctx)}
	if o.In != nil {
		opts = append(opts, tea.WithInput(o.In))
	}
	if o.Out != nil {
		opts = append(opts, tea.WithOutput(o.Out))
	}
	if _, err := tea.NewProgram(model, opts...).Run(); err != nil {
		return err
	}
	// Cập nhật xong và người dùng chọn khởi động lại: Bubble Tea đã thoát (terminal đã được trả lại) rồi mới exec tệp vừa cài với
	// cùng đối số và môi trường. Lỗi exec (vd. tệp không còn) trả về để lệnh in ra và thoát mã 1.
	if root != nil && root.RestartPath() != "" {
		return relaunch(root.RestartPath(), o)
	}
	return nil
}

// relaunch exec lại tệp path với cùng đối số (Options.Args, mặc định os.Args) và môi trường của tiến trình hiện tại.
func relaunch(path string, o Options) error {
	args := o.Args
	if args == nil {
		args = os.Args
	}
	return restartSelf(path, args, os.Environ())
}
