package cli

import (
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"bint/internal/app"
)

func newFlowCmd(stdio IO) *cobra.Command {
	c := &cobra.Command{
		Use:   "flow",
		Short: "Xem và kiểm định nghĩa flow (workflow gốc và flow tuỳ biến)",
		Long: `Flow là dãy bước của một workflow; mỗi bước có id, kind (plan | implement | review), skill và role. Bốn workflow
gốc (b-do, b-req, b-feat, b-review) là flow nhúng. Flow tuỳ biến do người dùng khai báo trong .bint/flows/<tên>.yml:

  schema: 1
  name: tdd                  # trùng tên tệp; không trùng workflow gốc, không tiền tố b-, không trùng tên skill
  description: "Dùng khi ..."
  steps:
    - {id: plan,   kind: plan,      skill: b-req, optional: true}
    - {id: red,    kind: implement, skill: write-failing-test, role: test-writer, verify: false}
    - {id: green,  kind: implement, skill: b-feat}
    - {id: review, kind: review,    skill: b-review, fix: green}

Luật: kind không giảm theo thứ tự plan → implement → review; tối đa một bước plan, ít nhất một bước implement;
optional chỉ ở bước plan; fix chỉ ở bước review và trỏ tới bước implement; verify: false không được ở bước
implement cuối; skill phải tồn tại (skill gốc hoặc skill tuỳ biến qua ` + "`bint skill lint`" + `).

Bản này mới đọc và kiểm định nghĩa: flow tuỳ biến CHƯA chạy được bằng ` + "`bint run`" + `.`,
	}
	c.AddCommand(newFlowListCmd(stdio), newFlowShowCmd(stdio), newFlowLintCmd(stdio))
	return c
}

func newFlowListCmd(stdio IO) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "list",
		Short: "Liệt kê flow nhúng và flow tuỳ biến: nguồn, số bước, mô tả, trạng thái lint",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := cwd()
			if err != nil {
				return err
			}
			rows, err := app.ListFlows(cmd.Context(), dir, app.FlowListOptions{})
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(stdio.Out, rows)
			}
			tw := tabwriter.NewWriter(stdio.Out, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "FLOW\tNGUỒN\tBƯỚC\tLINT\tMÔ TẢ")
			for _, r := range rows {
				state := "ok"
				if !r.OK {
					state = "lỗi"
				}
				source := "nhúng"
				if r.Source == app.FlowCustom {
					source = r.Path
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", r.Name, source, strconv.Itoa(r.Steps), state, r.Description)
			}
			tw.Flush()
			for _, r := range rows {
				for _, p := range r.Problems {
					fmt.Fprintf(stdio.Out, "✗ %s: %s\n", r.Name, p)
				}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func newFlowShowCmd(stdio IO) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "show <tên>",
		Short: "In định nghĩa đã chuẩn hoá của một flow (YAML, role mặc định được điền) kèm nguồn và băm",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := cwd()
			if err != nil {
				return err
			}
			res, err := app.ShowFlow(cmd.Context(), dir, app.FlowShowOptions{Name: args[0]})
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(stdio.Out, res)
			}
			src := "flow nhúng"
			if res.Source == app.FlowCustom {
				src = res.Path
			}
			fmt.Fprintf(stdio.Out, "# nguồn: %s\n# băm: %s\n%s", src, res.Hash, res.YAML)
			for _, e := range res.Errors {
				fmt.Fprintf(stdio.Out, "# lỗi: %s\n", e)
			}
			for _, w := range res.Warnings {
				fmt.Fprintf(stdio.Out, "# cảnh báo: %s\n", w)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func newFlowLintCmd(stdio IO) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "lint [tên]",
		Short: "Kiểm flow (không tên: mọi flow tuỳ biến); thoát 1 khi có lỗi",
		Long: `Kiểm định nghĩa flow theo luật của ` + "`bint flow --help`" + `: schema 1, khoá YAML đã biết, tên (khớp tên tệp, không trùng
workflow gốc, không tiền tố b-, không trùng skill), id bước duy nhất và khác "post-feat"/"verify", kind đúng thứ
tự, skill tồn tại, fix trỏ tới bước implement, verify/optional đặt đúng chỗ. Có tên thì kiểm một flow (kể cả flow
nhúng); không tên thì kiểm mọi flow trong .bint/flows. Một tệp hỏng không chặn việc kiểm tệp khác.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := cwd()
			if err != nil {
				return err
			}
			o := app.FlowLintOptions{}
			if len(args) == 1 {
				o.Name = args[0]
			}
			res, err := app.LintFlows(cmd.Context(), dir, o)
			if err != nil {
				return err
			}
			bad := 0
			for _, r := range res {
				if !r.OK() {
					bad++
				}
			}
			if asJSON {
				if err := writeJSON(stdio.Out, res); err != nil {
					return err
				}
			} else {
				if len(res) == 0 {
					fmt.Fprintln(stdio.Out, "Chưa có flow tuỳ biến nào (khai báo trong .bint/flows/<tên>.yml; xem `bint flow --help`).")
				}
				for _, r := range res {
					mark, where := "✓", r.Path
					if !r.OK() {
						mark = "✗"
					}
					if where == "" {
						where = r.Name + " (nhúng)"
					}
					fmt.Fprintf(stdio.Out, "%s %s\n", mark, where)
					for _, e := range r.Errors {
						fmt.Fprintf(stdio.Out, "    lỗi: %s\n", strings.TrimSpace(e))
					}
					for _, w := range r.Warnings {
						fmt.Fprintf(stdio.Out, "    cảnh báo: %s\n", w)
					}
				}
			}
			if bad > 0 {
				return withCode(ExitError, fmt.Errorf("%d flow có lỗi", bad))
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}
