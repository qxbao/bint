package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"bint/internal/app"
)

func newTestCmd(stdio IO) *cobra.Command {
	c := &cobra.Command{
		Use:   "test",
		Short: "Công cụ kiểm thử theo thay đổi",
	}
	c.AddCommand(newTestAffectedCmd(stdio))
	return c
}

func newTestAffectedCmd(stdio IO) *cobra.Command {
	var (
		base   string
		asJSON bool
		run    bool
	)
	c := &cobra.Command{
		Use:   "affected",
		Short: "Chọn (và chạy) các test bị ảnh hưởng bởi thay đổi so với một commit gốc",
		Long: `Chọn các test bị ảnh hưởng bởi thay đổi so với --base (mặc định HEAD), bằng bgraph nhúng trong Bint
(Node, Python, Rust, JVM, .NET, Go, PHP, Ruby). Thay đổi gồm phần đã commit kể từ điểm tách của --base
khỏi HEAD (merge-base), phần chưa commit và tệp chưa theo dõi. Phạm vi từ hẹp đến rộng:

  none     không test nào bị ảnh hưởng
  graph    đơn vị test chứng minh được từ đồ thị phụ thuộc (tệp test, gói Go, Cargo target, project…)
  package  nới tới gói/thư mục/target
  module   nới tới cả dự án (manifest đổi, import không phân giải được…)
  full     cả bộ (không có mốc, tệp CI hay toolchain pin đổi, nguồn chọn không dùng được)

Không bao giờ thu hẹp im lặng: không chắc thì nới rộng và ghi lý do. In phạm vi, base/head, tệp đổi,
lý do nới và lệnh chạy.

--run chạy luôn: lệnh bgraph dựng cho runner của dự án khi graph/package/module, eval.test_command
khi full, không chạy gì khi none. Bint thoát theo mã thoát của lệnh test, dùng được trong CI. Với --json,
đầu ra của lệnh test đi sang stderr.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := cwd()
			if err != nil {
				return err
			}
			out := stdio.Out
			if asJSON {
				out = stdio.Err
			}
			res, err := app.TestAffected(cmd.Context(), dir, app.TestAffectedOptions{Base: base, Run: run, Out: out})
			if err != nil {
				return err
			}
			sel := res.Selection
			if asJSON {
				v := map[string]any{"selection": sel, "command": res.Command, "ran": res.Ran}
				if res.Ran {
					v["exit_code"] = res.ExitCode
				}
				if err := writeJSON(stdio.Out, v); err != nil {
					return err
				}
			} else {
				fmt.Fprintf(stdio.Out, "scope: %s\n", sel.Scope)
				if sel.Source != "" {
					fmt.Fprintf(stdio.Out, "nguồn: %s\n", sel.Source)
				}
				fmt.Fprintf(stdio.Out, "base: %s\nhead: %s\n", orDash(sel.Base), orDash(sel.Head))
				fmt.Fprintf(stdio.Out, "tệp đổi: %d%s\n", sel.ChangedTotal, map[bool]string{true: " (chỉ liệt kê 200 đầu)"}[sel.Truncated])
				for _, f := range sel.Changed {
					fmt.Fprintf(stdio.Out, "  %s\n", f)
				}
				for _, w := range sel.Widening {
					fmt.Fprintf(stdio.Out, "nới → %s: %s %s %s\n", w.Tier, orDash(w.Path), w.Reason, w.Detail)
				}
				if len(sel.TestDirs) > 0 {
					fmt.Fprintf(stdio.Out, "thư mục test: %s\n", strings.Join(sel.TestDirs, " "))
				}
				if res.Command != "" {
					fmt.Fprintf(stdio.Out, "lệnh: %s\n", res.Command)
				}
			}
			if res.Ran && res.ExitCode != 0 {
				return withCode(res.ExitCode, fmt.Errorf("lệnh kiểm thử thoát với mã %d", res.ExitCode))
			}
			return nil
		},
	}
	f := c.Flags()
	f.StringVar(&base, "base", "", "commit/ref gốc để so thay đổi (mặc định HEAD)")
	f.BoolVar(&asJSON, "json", false, "xuất JSON")
	f.BoolVar(&run, "run", false, "chạy lệnh kiểm thử đã chọn và thoát theo mã của nó")
	return c
}
