package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"bint/internal/app"
	"bint/internal/config"
	"bint/internal/harness"
)

var supportIcon = map[harness.Support]string{harness.Yes: "✓", harness.Partial: "~", harness.No: "✗"}

func newHarnessCmd(stdio IO) *cobra.Command {
	c := &cobra.Command{
		Use:   "harness",
		Short: "Adapter harness và bảng khả năng (Claude Code, Codex, Antigravity)",
		Long: `Bint làm việc với harness qua adapter (mục 7). Mỗi adapter khai báo bảng khả năng đã
đối chiếu với một phiên bản cụ thể; khả năng không có được báo, không được mô phỏng:

  ✓ hỗ trợ   ~ hỗ trợ một phần (xem chi tiết)   ✗ không hỗ trợ

Cấp hỗ trợ: 1 Skill + MCP/CLI · 2 + Hook · 3 + thực thi theo chương trình (bint run).
Chọn harness cho từng bước workflow bằng roles trong .bint/config.yml, hoặc ghi đè cho
một lượt: bint run --harness codex …, bint run --role implementer=codex ….`,
	}
	c.AddCommand(newHarnessListCmd(stdio), newHarnessShowCmd(stdio), newHarnessComponentCmd(stdio), newHarnessRenderCmd(stdio))
	return c
}

func newHarnessListCmd(stdio IO) *cobra.Command {
	var asJSON, noVersion bool
	c := &cobra.Command{
		Use:   "list",
		Short: "Liệt kê harness, trạng thái trên máy và bảng khả năng",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := cwd()
			if err != nil {
				return err
			}
			o := app.HarnessListDefaults(config.Default())
			o.NoVersion = noVersion
			infos, err := app.ListHarnesses(cmd.Context(), dir, o)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(stdio.Out, infos)
			}
			w := stdio.Out
			tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "HARNESS\tBẬT\tTRÊN MÁY\tPHIÊN BẢN\tĐỐI CHIẾU\tCẤP")
			for _, in := range infos {
				on, inst, ver := "-", "không", in.Version
				if in.Enabled {
					on = "có"
				}
				if in.Installed {
					inst = "có"
				}
				if ver == "" {
					ver = "-"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d %s\n", in.Name, on, inst, ver, in.VerifiedWith, in.Level, in.LevelTitle)
			}
			tw.Flush()
			fmt.Fprintln(w)
			tw = tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
			head := "KHẢ NĂNG"
			for _, in := range infos {
				head += "\t" + in.Name
			}
			fmt.Fprintln(tw, head)
			for _, ck := range harness.AllCapabilities {
				row := ck.Title
				for _, in := range infos {
					row += "\t" + supportIcon[capOf(in, ck.Key).Support]
				}
				fmt.Fprintln(tw, row)
			}
			tw.Flush()
			fmt.Fprintln(w, "\n✓ hỗ trợ  ~ một phần  ✗ không — chi tiết: bint harness show <tên>")
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in dạng JSON")
	c.Flags().BoolVar(&noVersion, "no-version", false, "không gọi chương trình harness để lấy phiên bản")
	return c
}

func capOf(in harness.Info, key string) harness.Capability {
	for _, c := range in.Capabilities {
		if c.Key == key {
			return c
		}
	}
	return harness.Capability{Key: key, Support: harness.No}
}

func newHarnessShowCmd(stdio IO) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "show <tên>",
		Short: "Chi tiết bảng khả năng của một harness",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := cwd()
			if err != nil {
				return err
			}
			in, err := app.ShowHarness(cmd.Context(), dir, app.HarnessShowOptions{Name: args[0]})
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(stdio.Out, in)
			}
			w := stdio.Out
			fmt.Fprintf(w, "%s (%s) — cấp %d: %s\n", in.Title, in.Name, in.Level, in.LevelTitle)
			loc := in.Binary
			if loc == "" {
				loc = in.Home
			}
			if loc == "" {
				loc = "không tìm thấy trên máy này"
			}
			fmt.Fprintf(w, "  chương trình: %s\n", loc)
			if in.Version != "" {
				fmt.Fprintf(w, "  phiên bản:    %s (bảng khả năng đối chiếu với %s)\n", in.Version, in.VerifiedWith)
			} else {
				fmt.Fprintf(w, "  đối chiếu:    %s\n", in.VerifiedWith)
			}
			enabled := "không (thêm vào harnesses.enabled để dùng)"
			if in.Enabled {
				enabled = "có"
			}
			fmt.Fprintf(w, "  bật trong cấu hình: %s\n  skill: %s\n\n", enabled, in.SkillDir)
			tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
			for _, ck := range harness.AllCapabilities {
				cp := capOf(in, ck.Key)
				fmt.Fprintf(tw, "  %s\t%s\t%s\n", supportIcon[cp.Support], ck.Title, cp.Detail)
			}
			return tw.Flush()
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in dạng JSON")
	return c
}
