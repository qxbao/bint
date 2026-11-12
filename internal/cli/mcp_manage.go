package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"bint/internal/app"
	"bint/internal/config"
)

// Lệnh quản lý MCP (docs/mcp-gateway-plan.md mục 5.4): list, add, remove, check, index, grants.
func mcpManageCmds(stdio IO) []*cobra.Command {
	return []*cobra.Command{newMCPListCmd(stdio), newMCPAddCmd(stdio), newMCPRemoveCmd(stdio),
		newMCPCheckCmd(stdio), newMCPIndexCmd(stdio), newMCPGrantsCmd(stdio)}
}

func yesNo(b bool) string {
	if b {
		return "có"
	}
	return "-"
}

func newMCPListCmd(stdio IO) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "list",
		Short: "Danh mục MCP, trạng thái đăng ký trên máy và dự án dùng gì",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := cwd()
			if err != nil {
				return err
			}
			list, err := app.MCPList(cmd.Context(), dir, app.MCPListDefaults(config.Default()))
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(stdio.Out, list)
			}
			tw := tabwriter.NewWriter(stdio.Out, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "SERVICE\tTIỀN TỐ\tKIỂU\tCHIA SẺ\tĐĂNG KÝ\tDỰ ÁN\tNHÓM")
			for _, s := range list {
				proj := "-"
				if s.ProjectUses {
					proj = "bắt buộc"
					if !s.Required {
						proj = "tuỳ chọn"
					}
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", s.ID, s.Prefix, s.Transport, s.Share, yesNo(s.Registered), proj, strings.Join(s.Groups, ", "))
			}
			return tw.Flush()
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func newMCPAddCmd(stdio IO) *cobra.Command {
	var o app.MCPAddOptions
	var asJSON bool
	c := &cobra.Command{
		Use:   "add <service> [--command <lệnh> --arg <đối số>... --env-name <TÊN>... | --url <địa chỉ>]",
		Short: "Đăng ký service MCP trên máy này rồi kiểm tra (chỉ người dùng)",
		Long: `Đăng ký một service của danh mục (bint mcp list) vào mcp.yml ở thư mục cấu hình user, rồi chạy
check. Không cho agent chạy. Bint không tự cài: thiếu binary thì in lệnh cài gợi ý để bạn tự chạy.

Không truyền --command/--url thì dùng lệnh mặc định của danh mục (phiên bản ghim). Chỉ ghi TÊN biến
credential (--env-name); giá trị đặt trong secrets.env ở cùng thư mục (quyền 0600), dạng TÊN=giá trị.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := cwd()
			if err != nil {
				return err
			}
			o.Service = args[0]
			res, err := app.MCPAdd(cmd.Context(), dir, o)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(stdio.Out, res)
			}
			verb := "Đã đăng ký"
			if res.Replaced {
				verb = "Đã cập nhật đăng ký"
			}
			fmt.Fprintf(stdio.Out, "%s %s.\n", verb, res.Service)
			if res.InstallHint != "" {
				fmt.Fprintf(stdio.Out, "Chưa thấy %q trên PATH. Cài rồi chạy lại `bint mcp check %s`:\n  %s\n", res.Registration.Command, res.Service, res.InstallHint)
			}
			for _, w := range res.Warnings {
				fmt.Fprintln(stdio.Out, "Cảnh báo:", w)
			}
			printMCPCheck(stdio, res.Check)
			return nil
		},
	}
	f := c.Flags()
	f.StringVar(&o.Command, "command", "", "lệnh chạy service stdio (mặc định theo danh mục)")
	f.StringArrayVar(&o.Args, "arg", nil, "đối số của lệnh (lặp lại để thêm nhiều đối số)")
	f.StringArrayVar(&o.EnvNames, "env-name", nil, "TÊN biến credential của service (giá trị đặt trong secrets.env)")
	f.StringVar(&o.URL, "url", "", "địa chỉ service http (mặc định theo danh mục)")
	f.BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func newMCPRemoveCmd(stdio IO) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "remove <service>",
		Short: "Gỡ đăng ký service MCP khỏi máy này (chỉ người dùng)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := cwd()
			if err != nil {
				return err
			}
			res, err := app.MCPRemove(cmd.Context(), dir, app.MCPRemoveOptions{Service: args[0]})
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(stdio.Out, res)
			}
			fmt.Fprintf(stdio.Out, "Đã gỡ đăng ký %s (secrets.env giữ nguyên).\n", res.Service)
			if res.ProjectUses {
				fmt.Fprintf(stdio.Out, "Lưu ý: dự án vẫn khai báo %s trong mcp.services; `bint doctor` sẽ báo thiếu.\n", res.Service)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func printMCPCheck(stdio IO, r app.MCPCheckResult) {
	if r.Service == "" {
		return
	}
	icon := map[string]string{app.MCPStatusOK: "✓", app.MCPStatusDegraded: "!", app.MCPStatusFail: "✗"}[r.Status]
	fmt.Fprintf(stdio.Out, "  %s  %-16s %s", icon, r.Service, r.Status)
	if r.Status != app.MCPStatusFail && len(r.Tools) > 0 {
		fmt.Fprintf(stdio.Out, " (%d tool)", len(r.Tools))
	}
	fmt.Fprintln(stdio.Out)
	if r.Detail != "" {
		fmt.Fprintf(stdio.Out, "       %s\n", r.Detail)
	}
	if len(r.Extra) > 0 {
		fmt.Fprintf(stdio.Out, "       tool lạ (danh mục chưa biết): %s\n", strings.Join(r.Extra, ", "))
	}
	for _, w := range r.Warnings {
		fmt.Fprintf(stdio.Out, "       cảnh báo: %s\n", w)
	}
}

func newMCPCheckCmd(stdio IO) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "check [<service>]",
		Short: "Kiểm tra service MCP đã đăng ký (initialize + tools/list, đối chiếu danh mục)",
		Long: `Khởi động từng service đã đăng ký (hoặc kết nối tới URL), gửi initialize rồi tools/list và đối
chiếu với nhóm thao tác của danh mục; báo biến credential thiếu trong secrets.env. Với codegraph còn gọi thử
codegraph_status: bị từ chối thì báo degraded (daemon cũ chạy với env hẹp).

Thoát khác 0 khi có service ở trạng thái fail; degraded vẫn thoát 0 kèm cảnh báo.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := cwd()
			if err != nil {
				return err
			}
			o := app.MCPCheckDefaults(config.Default())
			if len(args) == 1 {
				o.Service = args[0]
			}
			results, err := app.MCPCheck(cmd.Context(), dir, o)
			if err != nil {
				return err
			}
			failed := 0
			for _, r := range results {
				if r.Status == app.MCPStatusFail {
					failed++
				}
			}
			if asJSON {
				if results == nil {
					results = []app.MCPCheckResult{}
				}
				if err := writeJSON(stdio.Out, results); err != nil {
					return err
				}
			} else {
				if len(results) == 0 {
					fmt.Fprintln(stdio.Out, "Chưa đăng ký service MCP nào trên máy này (xem `bint mcp list`, `bint mcp add <service>`).")
				}
				for _, r := range results {
					printMCPCheck(stdio, r)
				}
			}
			if failed > 0 {
				return withCode(ExitError, fmt.Errorf("%d service không qua kiểm tra", failed))
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func newMCPIndexCmd(stdio IO) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "index <service>",
		Short: "Lập hoặc cập nhật chỉ mục của service cho worktree hiện tại (chỉ codegraph; chỉ người dùng)",
		Long: `Chạy codegraph init -y (worktree chưa có .codegraph/codegraph.db) hoặc codegraph sync, với thư mục
làm việc là gốc worktree hiện tại, rồi thêm .codegraph/ vào tệp info/exclude của git (không thêm trùng).
Không chạy tự động vì init tốn RAM (cỡ 800 MB với repository lớn).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := cwd()
			if err != nil {
				return err
			}
			out := stdio.Out
			if asJSON {
				out = stdio.Err // stdout chỉ chứa JSON
			}
			res, err := app.MCPIndex(cmd.Context(), dir, app.MCPIndexOptions{Service: args[0]}, out)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(stdio.Out, res)
			}
			fmt.Fprintf(stdio.Out, "codegraph %s xong tại %s.\n", res.Action, res.Cwd)
			if res.ExcludeAdded {
				fmt.Fprintf(stdio.Out, "Đã thêm .codegraph/ vào %s.\n", res.ExcludeFile)
			} else {
				fmt.Fprintf(stdio.Out, ".codegraph/ đã có trong %s.\n", res.ExcludeFile)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func newMCPGrantsCmd(stdio IO) *cobra.Command {
	var o app.MCPGrantsOptions
	var asJSON bool
	c := &cobra.Command{
		Use:   "grants [--role <role>]",
		Short: "Quyền MCP đã giải: role → tool thật tới agent (kèm service thiếu)",
		Long: `Giải mcp.grants của dự án (<service>.<nhóm> hoặc <service>.*) thành tên tool mà agent thấy
(mcp__bint__<tên>). Role interactive không khai báo thì được mọi service dự án khai báo trong mcp.services mà
máy đã đăng ký. Báo service được cấp nhưng máy chưa đăng ký hoặc dự án không khai báo.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := cwd()
			if err != nil {
				return err
			}
			roles, err := app.MCPGrants(cmd.Context(), dir, o)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(stdio.Out, roles)
			}
			for _, r := range roles {
				note := ""
				if r.Implicit {
					note = " (mặc định theo dự án, chưa khai báo trong mcp.grants)"
				}
				fmt.Fprintf(stdio.Out, "%s%s:\n", r.Role, note)
				if len(r.Tools) == 0 {
					fmt.Fprintln(stdio.Out, "  (không có tool nào)")
				}
				for _, t := range r.Tools {
					fmt.Fprintf(stdio.Out, "  %-40s %s.%s → %s\n", t.Agent, t.Service, t.Group, t.Upstream)
				}
				for _, w := range r.Warnings {
					fmt.Fprintf(stdio.Out, "  ! %s\n", w)
				}
			}
			return nil
		},
	}
	c.Flags().StringVar(&o.Role, "role", "", "chỉ hiện một role")
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}
