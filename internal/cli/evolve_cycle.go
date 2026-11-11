package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"bint/internal/app/evolvesvc"
	"bint/internal/config"
)

// Vòng Propose–Probe–Compose (P8, mục 16) chạy ở evolvesvc.Cycle; tệp này chỉ gắn cờ và in.

func newEvolveCycleCmd(stdio IO) *cobra.Command {
	o := evolvesvc.CycleDefaults(config.Default())
	var asJSON bool
	c := &cobra.Command{
		Use:   "cycle",
		Short: "Một vòng tiến hoá tự động Propose–Probe–Compose trong giới hạn ngân sách (P8)",
		Long: `Làm trọn một vòng: kiểm tra hồi quy của đề xuất đã áp dụng → tạo đề xuất → đối chiếu
trên validation rồi test → tự áp dụng tối đa một đề xuất rủi ro thấp (chỉ tạo mục mới, mục
vào trạng thái candidate; cần evolution.adoption.auto_low_risk: true). Đề xuất khác đã đạt
được liệt kê chờ người duyệt (bint evolve apply). Mọi chi phí (tạo ứng viên, đối chiếu) cộng
vào --budget; hết ngân sách hoặc agent hết hạn mức thì vòng dừng, gọi lại để làm tiếp.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := o.Validate(); err != nil {
				return err
			}
			e, err := openEvolve(cmd)
			if err != nil {
				return err
			}
			defer e.Close()
			o.BintVersion = Version
			if !asJSON {
				o.Log = stdio.Out
			}
			rep, err := evolvesvc.Cycle(cmd.Context(), e, o)
			if asJSON {
				if werr := writeJSON(stdio.Out, rep); werr != nil {
					return werr
				}
				return err
			}
			printCycle(stdio.Out, rep)
			return err
		},
	}
	f := c.Flags()
	f.Float64Var(&o.BudgetUSD, "budget", 0, "ngân sách USD cho cả vòng (tạo ứng viên + đối chiếu)")
	f.Float64Var(&o.ProposeBudget, "propose-budget", 1, "ngân sách USD cho agent evolver trong vòng")
	f.IntVar(&o.MaxProposals, "max", 2, "số đề xuất tối đa được đối chiếu trong vòng")
	f.IntVarP(&o.Parallel, "parallel", "j", 3, "số lượt chạy song song khi đối chiếu")
	f.StringVar(&o.Model, "model", "", "mô hình cho evolver và các lượt đối chiếu")
	f.BoolVar(&o.NoPropose, "no-propose", false, "không tạo đề xuất mới, chỉ xử lý đề xuất đang chờ")
	f.BoolVar(&o.KeepFailed, "keep-failed", false, "không tự bác bỏ đề xuất đã kết luận not_supported")
	f.BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func printCycle(w io.Writer, r evolvesvc.CycleReport) {
	fmt.Fprintf(w, "\nVòng %s: đã tiêu $%.2f / $%.2f (tạo ứng viên $%.2f, đối chiếu $%.2f)\n", r.ID, r.SpentUSD, r.BudgetUSD, r.GenerationUSD, r.ProbeUSD)
	for _, s := range r.Steps {
		cost := ""
		if s.CostUSD > 0 {
			cost = fmt.Sprintf(" ($%.2f)", s.CostUSD)
		}
		fmt.Fprintf(w, "  %-11s %-32s %s%s\n", s.Step, s.Target, s.Outcome, cost)
	}
	if len(r.AutoApplied) > 0 {
		fmt.Fprintf(w, "Tự áp dụng: %s (mục mới ở candidate; quay lui: bint evolve rollback <id>)\n", strings.Join(r.AutoApplied, ", "))
	}
	if len(r.Ready) > 0 {
		fmt.Fprintf(w, "Chờ người duyệt: %s (bint evolve show/apply)\n", strings.Join(r.Ready, ", "))
	}
	if len(r.AutoRejected) > 0 {
		fmt.Fprintf(w, "Tự bác bỏ (không đạt): %s\n", strings.Join(r.AutoRejected, ", "))
	}
	if len(r.RollbackSuggested) > 0 {
		fmt.Fprintf(w, "Đề xuất quay lui (hồi quy): %s\n", strings.Join(r.RollbackSuggested, ", "))
	}
	if r.StopReason != "" {
		fmt.Fprintf(w, "Dừng: %s\n", r.StopReason)
	}
}
