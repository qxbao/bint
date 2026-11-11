package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"bint/internal/app"
	"bint/internal/config"
	"bint/internal/hcomp"
)

func openComponents(cmd *cobra.Command) (*app.ComponentWS, error) {
	dir, err := cwd()
	if err != nil {
		return nil, err
	}
	return app.OpenComponents(cmd.Context(), dir)
}

func newHarnessComponentCmd(stdio IO) *cobra.Command {
	c := &cobra.Command{
		Use:   "component",
		Short: "Thành phần quy trình của agent: chỉ dẫn, guardrail, script, chiến lược (.bint/harness)",
		Long: `Thành phần quy trình là tệp có phiên bản trong .bint/harness/ (quản lý bằng Git như tri thức):

  guidance/<id>.md      chỉ dẫn dùng công cụ / quy trình làm việc
  guardrails/<id>.yml   luật kiểm tra hành vi (on: pre_edit | pre_bash | stop; action: remind | ask | deny)
  scripts/<id>.sh       script hỗ trợ (kèm <id>.yml mô tả); Bint không bao giờ tự chạy
  strategy.yml          ghi đè một phần context.* của cấu hình

Mỗi thành phần có scope (global | domain:<glob>), status (candidate | verified | deprecated) và
origin (human | agent). Chỉ thành phần verified có hiệu lực ở phiên thật. ` + "`add`" + ` luôn tạo candidate;
duyệt (approve) và loại bỏ (deprecate) chỉ dành cho người, không chạy được từ phiên agent.`,
	}
	c.AddCommand(newComponentListCmd(stdio), newComponentShowCmd(stdio), newComponentAddCmd(stdio),
		newComponentReviewCmd(stdio, "approve", "Duyệt thành phần candidate (→ verified, có hiệu lực); chỉ dành cho người", "verified"),
		newComponentReviewCmd(stdio, "deprecate", "Loại bỏ thành phần (→ deprecated); chỉ dành cho người", "deprecated"))
	return c
}

func newComponentListCmd(stdio IO) *cobra.Command {
	var asJSON bool
	var kind, status string
	c := &cobra.Command{
		Use:   "list",
		Short: "Liệt kê thành phần quy trình trong .bint/harness",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			o := app.ComponentListDefaults(config.Default())
			o.Kind, o.Status = hcomp.Kind(kind), status
			if err := o.Validate(); err != nil {
				return err
			}
			w, err := openComponents(cmd)
			if err != nil {
				return err
			}
			lst, err := app.ListComponents(w, o)
			if err != nil {
				return err
			}
			rows := lst.Rows
			if asJSON {
				return writeJSON(stdio.Out, map[string]any{"components": rows, "problems": lst.Problems})
			}
			if len(rows) == 0 {
				fmt.Fprintln(stdio.Out, "Chưa có thành phần nào trong .bint/harness (thêm: bint harness component add --template test-before-stop).")
			} else {
				tw := tabwriter.NewWriter(stdio.Out, 0, 2, 2, ' ', 0)
				fmt.Fprintln(tw, "LOẠI\tID\tPHẠM VI\tTRẠNG THÁI\tNGUỒN\tPB\tRỦI RO\tCHI TIẾT")
				for _, r := range rows {
					fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d\t%s\t%s\n", r.Kind, r.ID, r.Scope, r.Status, r.Origin, r.Version, r.Risk, r.Detail)
				}
				tw.Flush()
			}
			for _, p := range lst.Problems {
				fmt.Fprintf(stdio.Out, "! bỏ qua %s\n", p)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in dạng JSON")
	c.Flags().StringVar(&kind, "kind", "", "chỉ một loại: guidance | guardrail | script | strategy")
	c.Flags().StringVar(&status, "status", "", "chỉ một trạng thái: candidate | verified | deprecated")
	return c
}

func newComponentShowCmd(stdio IO) *cobra.Command {
	var asJSON bool
	var kind string
	c := &cobra.Command{
		Use:   "show <id>",
		Short: "Chi tiết và nội dung một thành phần",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			o := app.ComponentShowOptions{ID: args[0], Kind: hcomp.Kind(kind)}
			if err := o.Validate(); err != nil {
				return err
			}
			w, err := openComponents(cmd)
			if err != nil {
				return err
			}
			d, err := app.ShowComponent(w, o)
			if err != nil {
				return err
			}
			comp, body := d.Component, d.Content
			if asJSON {
				return writeJSON(stdio.Out, map[string]any{"component": d.Row, "content": d.Content, "script": d.Script})
			}
			r := d.Row
			fmt.Fprintf(stdio.Out, "%s %s — %s · %s · nguồn %s · phiên bản %d · rủi ro %s\n", r.Kind, r.ID, r.Scope, r.Status, r.Origin, r.Version, r.Risk)
			if comp.ApprovedBy != "" {
				fmt.Fprintf(stdio.Out, "duyệt bởi: %s\n", comp.ApprovedBy)
			}
			fmt.Fprintf(stdio.Out, "tệp: %s\n\n%s", r.File, body)
			if len(comp.Source) > 0 {
				fmt.Fprintf(stdio.Out, "\n── scripts/%s.sh (Bint không bao giờ tự chạy) ──\n%s", comp.ID, comp.Source)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in dạng JSON")
	c.Flags().StringVar(&kind, "kind", "", "loại (khi id có ở nhiều loại)")
	return c
}

func newComponentAddCmd(stdio IO) *cobra.Command {
	var o app.AddComponentOptions
	var kind string
	c := &cobra.Command{
		Use:   "add (--template <tên> | --kind <loại> --file <tệp>)",
		Short: "Thêm một thành phần ở trạng thái candidate (chưa có hiệu lực)",
		Long: `Thêm thành phần từ một tệp do bạn soạn (--kind + --file) hoặc từ mẫu dựng sẵn (--template
test-before-stop). Thành phần luôn vào ở trạng thái candidate — dù tệp ghi gì — với origin là
agent nếu gọi từ phiên agent, ngược lại là human. Người duyệt bằng ` + "`bint harness component approve`" + `.

Script: --file là phần mô tả (.yml); nội dung .sh lấy từ --script-file hoặc tệp .sh cùng tên.
Script luôn rủi ro cao và luôn chờ người duyệt; Bint không bao giờ tự chạy nó.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			o.Kind = hcomp.Kind(kind)
			if err := o.Validate(); err != nil {
				return err
			}
			w, err := openComponents(cmd)
			if err != nil {
				return err
			}
			comp, err := app.AddComponent(cmd.Context(), w, o)
			if err != nil {
				return err
			}
			fmt.Fprintf(stdio.Out, "Đã thêm %s %q (candidate, nguồn %s, rủi ro %s): %s\n", comp.Kind, comp.ID, comp.Origin, hcomp.Risk(comp), ".bint/harness/"+hcomp.RelFile(comp.Kind, comp.ID))
			fmt.Fprintf(stdio.Out, "Chưa có hiệu lực. Xem: bint harness component show %s · người duyệt: bint harness component approve %s\n", comp.ID, comp.ID)
			return nil
		},
	}
	c.Flags().StringVar(&kind, "kind", "", "loại: guidance | guardrail | script | strategy")
	c.Flags().StringVar(&o.File, "file", "", "tệp thành phần do bạn soạn")
	c.Flags().StringVar(&o.ScriptFile, "script-file", "", "nội dung .sh của script")
	c.Flags().StringVar(&o.Template, "template", "", "mẫu dựng sẵn: test-before-stop")
	return c
}

func newComponentReviewCmd(stdio IO, use, short, to string) *cobra.Command {
	var kind string
	c := &cobra.Command{
		Use:   use + " <id>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			o := app.ComponentReviewOptions{ID: args[0], Kind: hcomp.Kind(kind)}
			if err := o.Validate(); err != nil {
				return err
			}
			w, err := openComponents(cmd)
			if err != nil {
				return err
			}
			comp, err := app.ReviewComponent(cmd.Context(), w, o, to)
			if err != nil {
				return err
			}
			fmt.Fprintf(stdio.Out, "%s %q → %s (duyệt bởi %s)\n", comp.Kind, comp.ID, comp.Status, comp.ApprovedBy)
			if comp.Kind == hcomp.KindScript && to == hcomp.StatusVerified {
				fmt.Fprintln(stdio.Out, "Lưu ý: script là mã thực thi; Bint chỉ liệt kê nó trong chỉ dẫn, không bao giờ tự chạy.")
			}
			return nil
		},
	}
	c.Flags().StringVar(&kind, "kind", "", "loại (khi id có ở nhiều loại)")
	return c
}

func newHarnessRenderCmd(stdio IO) *cobra.Command {
	var o app.RenderOptions
	var mode, paths string
	c := &cobra.Command{
		Use:   "render",
		Short: "Xem agent sẽ nhận gì từ thành phần quy trình trên một harness",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			o.Mode = hcomp.Mode(mode)
			for _, p := range strings.Split(paths, ",") {
				if p = strings.TrimSpace(p); p != "" {
					o.Paths = append(o.Paths, p)
				}
			}
			if err := o.Validate(); err != nil {
				return err
			}
			w, err := openComponents(cmd)
			if err != nil {
				return err
			}
			out, err := app.RenderHarness(w, o)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(stdio.Out, out)
			return err
		},
	}
	c.Flags().StringVar(&o.Harness, "harness", "claude", "harness: claude | codex | antigravity")
	c.Flags().StringVar(&mode, "mode", "on", "on (verified) | all (cả candidate) | off (chỉ bản dựng sẵn)")
	c.Flags().StringVar(&paths, "paths", "", "tệp giả định đang đọc/sửa (phân tách bằng dấu phẩy), để thấy chỉ dẫn riêng")
	return c
}
