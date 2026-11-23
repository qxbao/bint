package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"bint/internal/app"
)

func newKnowledgeAuditCmd(stdio IO) *cobra.Command {
	var asJSON bool
	var o app.AuditOptions
	c := &cobra.Command{
		Use:   "audit [id...]",
		Short: "Liệt kê mục tri thức cần rà soát (stale, bằng chứng lệch; --all: quét tổng quát kèm mức sử dụng) cho agent nhận định",
		Long: `Dành cho agent rà soát tri thức qua skill (không cần --budget, không gọi mô hình):
  1. Mục chỉ dời dòng hoặc chuyển tệp (nội dung bằng chứng không đổi) được tự áp như
     verify --apply và không có trong danh sách.
  2. Mục stale, hoặc verified mà fingerprint của bằng chứng lệch, được liệt kê kèm lý do
     và trích đoạn mã HIỆN TẠI của bằng chứng.
  --all quét tổng quát mọi mục còn hiệu lực (kể cả candidate chờ duyệt và mục bằng chứng vẫn
     khớp) kèm tín hiệu: problem (phải nhận định), never_used / unused (không ai truy xuất,
     trích dẫn), review_old (duyệt đã lâu), warning (trùng lặp…), unenforced (rule/warning chưa có
     lint/test/hook cưỡng chế: enforced_by), và số lần dùng. Mục có vấn đề
     đứng đầu, rồi mục ít dùng.
Đọc mã, nhận định từng mục rồi lưu bằng bint knowledge verdict. Lệnh không đổi trạng thái.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			k, err := openKnowledge(cmd)
			if err != nil {
				return err
			}
			o.Refs = args
			rep, err := app.AuditKnowledgeWith(cmd.Context(), k, o)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(stdio.Out, rep)
			}
			for _, id := range rep.Relocated {
				fmt.Fprintf(stdio.Out, "↪ %s  đã cập nhật vị trí bằng chứng (nội dung không đổi)\n", id)
			}
			if len(rep.Items) == 0 {
				fmt.Fprintln(stdio.Out, "Không có mục nào cần rà soát.")
				return nil
			}
			for _, it := range rep.Items {
				fmt.Fprintf(stdio.Out, "%s", it.Block)
				if len(it.Signals) > 0 {
					fmt.Fprintf(stdio.Out, "Tín hiệu: %s\n", strings.Join(it.Signals, ", "))
				}
				if u := it.Usage; u != nil {
					last := u.LastUsed
					if last == "" {
						last = "chưa"
					}
					fmt.Fprintf(stdio.Out, "Sử dụng: truy xuất %d lần (%d lượt chạy), trích dẫn %d lần, dùng gần nhất %s; duyệt %d ngày trước\n",
						u.Retrieved, u.Runs, u.Cited, last, u.ReviewAgeDays)
				}
				for _, r := range it.Reasons {
					fmt.Fprintf(stdio.Out, "Lý do cần xem: %s\n", r)
				}
				fmt.Fprintln(stdio.Out)
			}
			fmt.Fprintf(stdio.Out, "Nhận định mỗi mục rồi lưu: bint knowledge verdict <id> --verdict <valid|outdated|fix-evidence|unsure> --reason \"<một câu, dẫn tệp:dòng>\" [--path <tệp> --lines <a-b> --symbol <ký hiệu>]\n%s",
				app.VerdictDefinitions)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	c.Flags().BoolVar(&o.All, "all", false, "quét tổng quát mọi mục còn hiệu lực kèm tín hiệu và mức sử dụng")
	c.Flags().StringSliceVar(&o.Types, "type", nil, "chỉ xét các loại này (rule,warning,pattern,fact,decision,lesson)")
	c.Flags().IntVar(&o.UnusedDays, "unused-days", 90, "không được dùng hoặc duyệt quá số ngày này thì gắn tín hiệu unused/review_old")
	return c
}

func newKnowledgeVerdictCmd(stdio IO) *cobra.Command {
	var o app.VerdictOptions
	var asJSON bool
	c := &cobra.Command{
		Use:   "verdict <id>",
		Short: "Lưu nhận định của agent về một mục tri thức (không đổi trạng thái)",
		Long: `Lưu nhận định valid | outdated | fix-evidence | unsure kèm lý do (dẫn tệp:dòng) và agent/mô hình đã tạo ra nó.
fix-evidence kèm bằng chứng đề xuất (--path/--lines/--symbol). Chỉ ghi nhận định vào cơ sở dữ liệu trạng thái:
không đổi trạng thái hay tệp tri thức. Người áp nhận định bằng bint knowledge verdicts --apply hoặc màn Tri thức
(valid → làm mới fingerprint, outdated → deprecate, fix-evidence → sửa bằng chứng rồi làm mới).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			o.Ref = args[0]
			if err := o.Validate(); err != nil {
				return err
			}
			k, err := openKnowledge(cmd)
			if err != nil {
				return err
			}
			rv, err := app.RecordVerdict(cmd.Context(), k, o)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(stdio.Out, rv)
			}
			fmt.Fprintf(stdio.Out, "Đã lưu nhận định %s cho %s (%s", rv.Verdict, rv.ID, rv.Harness)
			if rv.Model != "" {
				fmt.Fprintf(stdio.Out, "/%s", rv.Model)
			}
			fmt.Fprintln(stdio.Out, "). Trạng thái không đổi; người áp bằng bint knowledge verdicts --apply.")
			return nil
		},
	}
	f := c.Flags()
	f.StringVar(&o.Verdict, "verdict", "", "valid | outdated | fix-evidence | unsure")
	f.StringVar(&o.Reason, "reason", "", "lý do một câu, dẫn tệp:dòng")
	f.StringVar(&o.Path, "path", "", "fix-evidence: tệp bằng chứng đề xuất")
	f.StringVar(&o.Lines, "lines", "", "fix-evidence: vùng dòng đề xuất (a-b)")
	f.StringVar(&o.Symbol, "symbol", "", "fix-evidence: ký hiệu đề xuất")
	f.StringVar(&o.Model, "model", "", "mô hình tạo ra nhận định (mặc định BINT_AGENT_MODEL/ANTHROPIC_MODEL)")
	f.BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func newKnowledgeVerdictsCmd(stdio IO) *cobra.Command {
	var apply, asJSON bool
	c := &cobra.Command{
		Use:   "verdicts [id...]",
		Short: "Xem nhận định của agent; --apply để người áp theo lô",
		Long: `Liệt kê nhận định gần nhất của mỗi mục. --apply (chỉ người dùng chạy được) áp các nhận định còn mới:
  valid         làm mới fingerprint (stale → verified, hoặc lưu fingerprint mới cho mục verified)
  outdated      deprecate
  fix-evidence  thay bằng chứng theo đề xuất rồi làm mới fingerprint
unsure và nhận định đã cũ (mã đổi tiếp sau nhận định) bị bỏ qua.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			k, err := openKnowledge(cmd)
			if err != nil {
				return err
			}
			rep, err := app.KnowledgeVerdicts(cmd.Context(), k, app.VerdictsOptions{Refs: args, Apply: apply})
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(stdio.Out, rep)
			}
			if len(rep.Rows) == 0 && len(rep.Applied) == 0 {
				fmt.Fprintln(stdio.Out, "Chưa có nhận định nào.")
				return nil
			}
			for _, a := range rep.Applied {
				mark := "-"
				if a.Applied {
					mark = "✓"
				}
				fmt.Fprintf(stdio.Out, "%s %s  %s: %s\n", mark, a.ID, a.Verdict, a.Detail)
			}
			for _, r := range rep.Rows {
				note := ""
				switch {
				case r.Stale:
					note = " [cũ]"
				case !r.Actionable:
					note = " [không áp được]"
				}
				fmt.Fprintf(stdio.Out, "%s  %s%s — %s (%s)\n", r.ID, r.Verdict, note, strings.TrimSpace(r.Reason), r.Harness+modelSuffix(r.Model))
			}
			if !apply {
				fmt.Fprintln(stdio.Out, "\nÁp theo lô (chỉ người): bint knowledge verdicts --apply")
			}
			return nil
		},
	}
	f := c.Flags()
	f.BoolVar(&apply, "apply", false, "áp nhận định (chỉ người dùng chạy được)")
	f.BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func modelSuffix(m string) string {
	if m == "" {
		return ""
	}
	return "/" + m
}
