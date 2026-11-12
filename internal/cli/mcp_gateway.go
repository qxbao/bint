package cli

import (
	"fmt"
	"os"
	"os/signal"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"bint/internal/app"
	"bint/internal/config"
)

// Lệnh daemon gateway MCP (docs/mcp-gateway-plan.md mục 6): bint mcp gateway start|stop|status|restart.
func newMCPGatewayCmd(stdio IO) *cobra.Command {
	c := &cobra.Command{
		Use:   "gateway",
		Short: "Daemon gateway MCP dùng chung: start, stop, status, restart <service>",
		Long: `Daemon gateway MCP (một bản cho mỗi user) ghép nhiều phiên vào một upstream, phân vùng tiến trình theo chính sách
chia sẻ của danh mục và ghi mỗi lời gọi (mcp.called) vào spool. Nghe 127.0.0.1 cổng động; cổng, PID và bearer token ghi trong
gateway.json (quyền 0600) cạnh gateway.lock và gateway.log trong thư mục trạng thái cấp user.

stop và restart chỉ dành cho người (cắt lời gọi đang chạy của phiên khác); start và status chạy được trong phiên agent.`,
	}
	c.AddCommand(newMCPGatewayStartCmd(stdio), newMCPGatewayStopCmd(stdio), newMCPGatewayStatusCmd(stdio), newMCPGatewayRestartCmd(stdio))
	return c
}

func newMCPGatewayStartCmd(stdio IO) *cobra.Command {
	o := app.MCPGatewayStartDefaults(config.Default())
	var asJSON bool
	c := &cobra.Command{
		Use:   "start [--foreground]",
		Short: "Khởi động daemon gateway tách rời (hoặc chạy tại chỗ với --foreground)",
		Long: `Khởi động daemon tách rời khỏi terminal (nhật ký ở gateway.log) rồi báo cổng và PID. Đã có daemon chạy thì chỉ báo
thông tin của nó. --foreground chạy tại chỗ tới khi Ctrl+C hoặc "bint mcp gateway stop".`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if o.Foreground {
				var stop func()
				ctx, stop = signal.NotifyContext(ctx, os.Interrupt)
				defer stop()
			}
			announce := func(r app.MCPGatewayStartResult) {
				if asJSON {
					_ = writeJSON(stdio.Out, r)
					return
				}
				fmt.Fprintf(stdio.Out, "Gateway chạy: PID %d, cổng %d (127.0.0.1). Thư mục: %s\n", r.PID, r.Port, r.Dir)
			}
			res, err := app.MCPGatewayStart(ctx, Version, o, stdio.Err, func(r app.MCPGatewayStartResult) {
				if o.Foreground {
					announce(r)
				}
			})
			if err != nil {
				return err
			}
			if !o.Foreground {
				if !res.Started && !asJSON {
					fmt.Fprintln(stdio.Out, "Gateway đã chạy từ trước.")
				}
				announce(res)
				if !asJSON {
					fmt.Fprintf(stdio.Out, "Nhật ký: %s\n", res.LogFile)
				}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&o.Foreground, "foreground", false, "chạy tại chỗ thay vì tách rời")
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func newMCPGatewayStopCmd(stdio IO) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "stop",
		Short: "Tắt daemon gateway và mọi upstream của nó (chỉ người dùng)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			res, err := app.MCPGatewayStop(cmd.Context(), app.MCPGatewayStopDefaults(config.Default()))
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(stdio.Out, res)
			}
			if res.Stopped {
				fmt.Fprintln(stdio.Out, "Đã tắt gateway.")
			} else {
				fmt.Fprintln(stdio.Out, "Gateway không chạy.")
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func newMCPGatewayStatusCmd(stdio IO) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "status",
		Short: "Trạng thái daemon: upstream đang chạy, PID, RSS/PSS, số phiên, lời gọi gần nhất, lỗi",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			res, err := app.MCPGatewayStatus(cmd.Context(), app.MCPGatewayStatusDefaults(config.Default()))
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(stdio.Out, res)
			}
			if !res.Running {
				fmt.Fprintf(stdio.Out, "Gateway không chạy (thư mục %s). Khởi động: bint mcp gateway start\n", res.Dir)
				return nil
			}
			st := res.Status
			fmt.Fprintf(stdio.Out, "Gateway chạy: PID %d, cổng %d, %s, %d phiên", st.PID, st.Port, (time.Duration(st.UptimeSec) * time.Second).String(), st.Sessions)
			if st.DroppedEvents > 0 {
				fmt.Fprintf(stdio.Out, ", %d sự kiện mcp.called không ghi được", st.DroppedEvents)
			}
			fmt.Fprintln(stdio.Out)
			if len(st.Upstreams) == 0 {
				fmt.Fprintln(stdio.Out, "Chưa có upstream nào (khởi động lười ở lời gọi đầu tiên).")
				return nil
			}
			tw := tabwriter.NewWriter(stdio.Out, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "SERVICE\tPHÂN VÙNG\tCHẠY\tGEN\tPID\tRSS\tPSS\tPHIÊN\tĐANG GỌI\tLỜI GỌI GẦN NHẤT\tLỖI")
			for _, u := range st.Upstreams {
				last := "-"
				if u.LastCall != nil {
					last = fmt.Sprintf("%s %s (%dms)", u.LastCall.Tool, u.LastCall.Outcome, u.LastCall.DurationMS)
				}
				lastErr := "-"
				if u.LastError != "" {
					lastErr = truncate(u.LastError, 80)
				}
				if u.Limited {
					lastErr = "vượt giới hạn restart: " + lastErr
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\t%s\t%d\t%d\t%s\t%s\n", u.Service, u.Partition, yesNo(u.Running), u.Generation,
					pidText(u.PID), kbText(u.RSSKB), kbText(u.PSSKB), u.Sessions, u.Inflight, last, lastErr)
			}
			return tw.Flush()
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func pidText(n int) string {
	if n <= 0 {
		return "-"
	}
	return fmt.Sprint(n)
}

func kbText(kb uint64) string {
	if kb == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f MB", float64(kb)/1024)
}

func newMCPGatewayRestartCmd(stdio IO) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "restart <service>",
		Short: "Dừng mọi upstream của service và xoá trạng thái lỗi; lời gọi sau khởi động mới (chỉ người dùng)",
		Long: `Dừng mọi upstream đang chạy của service (kill cả cây tiến trình), xoá lỗi và bộ đếm khởi động lại. Lời gọi
đang chạy trên upstream đó nhận lỗi; lời gọi sau khởi động upstream mới với generation tăng 1. Dùng khi service báo
đã vượt giới hạn khởi động lại. Không cho agent chạy.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := app.MCPGatewayRestart(cmd.Context(), app.MCPGatewayRestartOptions{Service: args[0]})
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(stdio.Out, res)
			}
			fmt.Fprintf(stdio.Out, "Đã dừng %d upstream của %s; lời gọi sau khởi động mới.\n", res.Stopped, res.Service)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}
