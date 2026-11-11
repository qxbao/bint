package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"bint/internal/app"
	"bint/internal/i18n"
	"bint/internal/tui"
)

// Ba điều kiện mở TUI là biến gói để kiểm thử thay được.
var (
	// stdioIsTTY: stdin và stdout đều là terminal thật. Dùng term.IsTerminal chứ không dùng
	// ModeCharDevice: /dev/null và NUL cũng là thiết bị ký tự nên bị nhận nhầm.
	stdioIsTTY = func(stdio IO) bool { return isTTY(stdio.In) && isTTY(stdio.Out) }
	// inAgentSession: đang trong phiên agent (agent không điều khiển được TUI).
	inAgentSession = app.InAgentSession
	// runTUI mở TUI.
	runTUI = tui.Run
)

func isTTY(v any) bool {
	f, ok := v.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// shouldOpenTUI: `bint` không đối số mở TUI khi có terminal và ngoài phiên agent; ngược lại in trợ
// giúp như cũ (đường ống, CI, agent).
func shouldOpenTUI(stdio IO) bool {
	if in, _ := inAgentSession(); in {
		return false
	}
	return stdioIsTTY(stdio)
}

type tuiRequest struct {
	lang     string
	gallery  bool
	readOnly bool
}

// openTUI mở TUI ở thư mục hiện tại.
func openTUI(ctx context.Context, stdio IO, req tuiRequest) error {
	dir, err := cwd()
	if err != nil {
		return err
	}
	agent, _ := inAgentSession()
	o := tui.Options{Dir: dir, Version: Version, BintPath: bintPath(), Lang: req.lang, Gallery: req.gallery,
		ReadOnly: req.readOnly, Agent: agent}
	if f, ok := stdio.In.(*os.File); ok {
		o.In = f
	}
	if f, ok := stdio.Out.(*os.File); ok {
		o.Out = f
	}
	return runTUI(ctx, o)
}

func newTUICmd(stdio IO) *cobra.Command {
	var req tuiRequest
	c := &cobra.Command{
		Use:   "tui",
		Short: "Mở giao diện terminal của Bint (bint không đối số cũng mở nó)",
		Long: `Mở giao diện terminal: bảy tab (Tổng quan, Tri thức, Tác vụ, Lượt chạy, Đánh giá, Tiến hoá,
Hệ thống), bảng lệnh (:) và trợ giúp (?). Cần terminal tương tác.

Ngôn ngữ giao diện: --lang > biến BINT_LANG > khoá ui.language > LC_ALL/LANG > vi. Đổi được ngay
trong giao diện (phím L).

Mở trong phiên agent vẫn được (người gõ tường minh) nhưng thao tác chỉ dành cho người bị khoá.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if req.lang != "" && !i18n.Supported(req.lang) && req.lang != i18n.Pseudo {
				return usageError{fmt.Errorf("--lang phải là vi | en")}
			}
			if !stdioIsTTY(stdio) {
				return withCode(ExitError, fmt.Errorf("`bint tui` cần terminal tương tác (stdin và stdout phải là terminal)"))
			}
			return openTUI(cmd.Context(), stdio, req)
		},
	}
	f := c.Flags()
	f.StringVar(&req.lang, "lang", "", "ngôn ngữ giao diện: vi | en (mặc định theo BINT_LANG, ui.language, LANG)")
	f.BoolVar(&req.readOnly, "read-only", false, "chỉ xem: ẩn mọi hành động ghi (chế độ tiện dụng, không phải ranh giới bảo mật)")
	f.BoolVar(&req.gallery, "gallery", false, "trang trưng bày component (dành cho nhà phát triển)")
	_ = c.Flags().MarkHidden("gallery")
	return c
}
