package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"bint/internal/app"
)

func newKnowledgeRevalidateCmd(stdio IO) *cobra.Command {
	var (
		budget       float64
		maxItems     int
		batch        int
		agentCommand string
		model        string
		asJSON       bool
	)
	c := &cobra.Command{
		Use:   "revalidate [id...]",
		Short: "Cho agent nhận định lại mục stale rủi ro cao (rule, warning), theo lô, trong ngân sách",
		Long: `Xác thực lại theo mục 8.4:
  1. Kiểm tra xác định được trước (bint knowledge verify --apply xử lý mục rủi ro thấp).
  2. Mục stale loại rule/warning: agent đọc mã hiện tại (chỉ Read/Grep/Glob) và nhận
     định valid | outdated | unsure, gom theo lô, dừng khi hết ngân sách.

Nhận định chỉ là dữ liệu cho người duyệt; trạng thái không đổi. Sau đó:
  bint knowledge approve --refresh <id>     nếu đồng ý mục còn đúng
  bint knowledge deprecate <id> -r "..."    nếu mục đã sai`,
		RunE: func(cmd *cobra.Command, args []string) error {
			k, err := openKnowledge(cmd)
			if err != nil {
				return err
			}
			ro := app.RevalidateDefaults(k.Config)
			ro.Refs, ro.MaxItems, ro.BatchSize, ro.BudgetUSD = args, maxItems, batch, budget
			ro.AgentCommand, ro.Model = agentCommand, model
			// Không gọi ro.Validate(): giữ hành vi cũ cho --budget < 0 (agent lệnh vẫn chạy, Claude Code trả
			// ErrNoBudget, mã thoát 1). Form TUI vẫn kiểm tra khi nhập.
			rep, err := app.RevalidateKnowledge(cmd.Context(), k, ro)
			if err != nil {
				return err
			}
			if asJSON {
				if rep.Results == nil {
					rep.Results = []app.Revalidation{}
				}
				if rep.Skipped == nil {
					rep.Skipped = []string{}
				}
				return writeJSON(stdio.Out, rep)
			}
			if len(rep.Results) == 0 && len(rep.Skipped) == 0 {
				fmt.Fprintln(stdio.Out, "Không có mục stale rủi ro cao cần xác thực lại.")
				return nil
			}
			icon := map[string]string{"valid": "✓", "outdated": "✗", "unsure": "?"}
			for _, r := range rep.Results {
				if r.Blocked != "" {
					fmt.Fprintf(stdio.Out, "! %s  kiểm tra xác định được thất bại: %s (sửa mục hoặc deprecate)\n", r.ID, r.Blocked)
					continue
				}
				fmt.Fprintf(stdio.Out, "%s %s  %s — %s\n", icon[r.Verdict], r.ID, r.Verdict, r.Reason)
			}
			for _, id := range rep.Skipped {
				fmt.Fprintf(stdio.Out, "- %s  bỏ qua (giới hạn số mục hoặc ngân sách)\n", id)
			}
			if rep.StopReason != "" {
				fmt.Fprintf(stdio.Out, "\nDừng vì ngân sách: %s.\n", rep.StopReason)
			}
			fmt.Fprintf(stdio.Out, "\nĐã tiêu $%.4f. Nhận định không đổi trạng thái: approve --refresh hoặc deprecate.\n", rep.SpentUSD)
			return nil
		},
	}
	f := c.Flags()
	f.Float64Var(&budget, "budget", 0, "ngân sách USD cho cả lần chạy (bắt buộc với Claude Code)")
	f.IntVar(&maxItems, "max-items", 10, "số mục tối đa")
	f.IntVar(&batch, "batch", 5, "số mục mỗi lần gọi agent")
	f.StringVar(&agentCommand, "agent-command", "", "dùng lệnh tuỳ chỉnh làm agent (đề bài qua stdin)")
	f.StringVar(&model, "model", "", "mô hình (mặc định eval.agent.model)")
	f.BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}
