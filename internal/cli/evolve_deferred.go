package cli

import (
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"bint/internal/app"
	"bint/internal/eval"
	"bint/internal/evolve"
)

// processDeferred đánh dấu yêu cầu cho cơ hội mới đủ điều kiện rồi xử lý hàng đợi (mục 13.3).
// budget > 0: dùng agent evolver cho yêu cầu cần mô hình; ngược lại chỉ yêu cầu xác định.
func processDeferred(cmd *cobra.Command, e *evolveCtx, w io.Writer, budget float64, max int, asJSON bool) (evolve.ProcessReport, []evolve.Request, error) {
	ctx := cmd.Context()
	scan, h, err := e.scan(ctx, 0, true)
	if err != nil {
		return evolve.ProcessReport{}, nil, err
	}
	now := time.Now()
	added, err := evolve.Enqueue(e.k.Paths.Runtime, scan, now)
	if err != nil {
		return evolve.ProcessReport{}, nil, err
	}
	var agent eval.Agent
	if budget > 0 {
		if agent, err = evolverAgent(e.k, "", ""); err != nil {
			fmt.Fprintf(w, "! không dùng được agent evolver: %v\n", err)
		}
	}
	rep, err := evolve.ProcessRequests(ctx, evolve.ProcessInput{K: e.k, DB: e.st.DB, Scan: scan, History: h, Agent: agent, BudgetUSD: budget,
		Max: max, Timeout: time.Duration(e.k.Config.Eval.TimeoutMinutes) * time.Minute, Now: now})
	return rep, added, err
}

func printDeferred(w io.Writer, rep evolve.ProcessReport, added []evolve.Request, indent string) {
	if len(added) > 0 {
		fmt.Fprintf(w, "%sĐã đánh dấu %d yêu cầu tiến hoá mới (đủ bằng chứng).\n", indent, len(added))
	}
	if rep.Locked != "" {
		fmt.Fprintf(w, "%sChưa xử lý được (khoá tiến hoá đang giữ): %s\n", indent, rep.Locked)
	}
	for _, r := range rep.Processed {
		if r.Status == evolve.ReqDone {
			fmt.Fprintf(w, "%s+ %s → đề xuất %s (bint evolve show %s)\n", indent, r.Line(), r.ProposalID, r.ProposalID)
		} else {
			fmt.Fprintf(w, "%s- %s: bỏ qua — %s\n", indent, r.Line(), r.Note)
		}
	}
	for _, r := range rep.Waiting {
		note := r.Note
		if note == "" {
			note = "chờ lần xử lý sau"
		}
		fmt.Fprintf(w, "%s· %s: %s\n", indent, r.Line(), note)
	}
	if rep.SpentUSD > 0 {
		fmt.Fprintf(w, "%sChi phí tạo ứng viên: $%.4f\n", indent, rep.SpentUSD)
	}
}

// deferredTick chạy ở lần gọi CLI phù hợp (bint status) khi evolution.execution = deferred: chỉ khi
// có sự kiện mới hoặc yêu cầu đang chờ; lỗi chỉ cảnh báo, không bao giờ làm hỏng lệnh gọi.
func deferredTick(cmd *cobra.Command, w io.Writer, cfgEvo func() (bool, float64), newEvents int) {
	on, budget := cfgEvo()
	if !on {
		return
	}
	if in, _ := app.InAgentSession(); in {
		return // đề xuất tiến hoá chỉ dành cho người dùng (mục 15.4)
	}
	e, err := openEvolve(cmd)
	if err != nil {
		return
	}
	defer e.Close()
	waiting, _ := evolve.ListRequests(e.k.Paths.Runtime, evolve.ReqPending)
	if newEvents == 0 && len(waiting) == 0 {
		return
	}
	rep, added, err := processDeferred(cmd, e, w, budget, 0, false)
	if err != nil {
		fmt.Fprintf(w, "Tiến hoá hoãn:  ! %v\n", err)
		return
	}
	if len(added)+len(rep.Processed)+len(rep.Waiting) > 0 || rep.Locked != "" {
		fmt.Fprintln(w, "Tiến hoá hoãn:")
		printDeferred(w, rep, added, "  ")
	}
}

func newEvolveQueueCmd(stdio IO) *cobra.Command {
	var (
		all    bool
		asJSON bool
	)
	c := &cobra.Command{
		Use:   "queue",
		Short: "Yêu cầu tiến hoá đã đánh dấu (chế độ deferred, mục 13.4)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := openEvolve(cmd)
			if err != nil {
				return err
			}
			defer e.Close()
			var st []string
			if !all {
				st = []string{evolve.ReqPending}
			}
			rs, err := evolve.ListRequests(e.k.Paths.Runtime, st...)
			if err != nil {
				return err
			}
			if asJSON {
				if rs == nil {
					rs = []evolve.Request{}
				}
				return writeJSON(stdio.Out, rs)
			}
			if len(rs) == 0 {
				fmt.Fprintf(stdio.Out, "Không có yêu cầu nào (evolution.execution = %s).\n", e.k.Config.Evolution.Execution)
				return nil
			}
			tw := tabwriter.NewWriter(stdio.Out, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tTRẠNG THÁI\tCẤP\tLOẠI\tĐỀ XUẤT\tTẠO LÚC\tGHI CHÚ")
			for _, r := range rs {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.ID, r.Status, r.Level, r.Kind, orDash(r.ProposalID), fmtTime(r.CreatedAt), truncate(r.Note, 60))
			}
			return tw.Flush()
		},
	}
	c.Flags().BoolVar(&all, "all", false, "mọi trạng thái")
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func newEvolveProcessCmd(stdio IO) *cobra.Command {
	var (
		budget float64
		max    int
		asJSON bool
	)
	c := &cobra.Command{
		Use:   "process",
		Short: "Đánh dấu và xử lý yêu cầu tiến hoá ngay (chế độ deferred; dùng được cả khi execution = manual)",
		Long: `Đánh dấu yêu cầu cho cơ hội đủ bằng chứng mới rồi xử lý hàng đợi: kiểm tra khoá tiến hoá và
ngân sách, rồi tạo ĐỀ XUẤT qua cùng đường với bint evolve propose. Không áp dụng gì (chế độ propose).

Không có --budget: chỉ xử lý yêu cầu xác định (loại bỏ, gộp). Yêu cầu cần agent evolver chờ ngân sách.
Với evolution.execution: deferred, bint status tự chạy bước này khi có sự kiện mới (ngân sách lấy từ
evolution.deferred.budget_usd, null = chỉ yêu cầu không cần mô hình).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireHumanCLI("bint evolve process"); err != nil {
				return err
			}
			e, err := openEvolve(cmd)
			if err != nil {
				return err
			}
			defer e.Close()
			if !e.k.Config.Evolution.Enabled {
				return fmt.Errorf("evolution.enabled = false trong cấu hình")
			}
			rep, added, err := processDeferred(cmd, e, stdio.Out, budget, max, asJSON)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(stdio.Out, map[string]any{"added": nonNilSlice(added), "report": rep})
			}
			if len(added)+len(rep.Processed)+len(rep.Waiting) == 0 && rep.Locked == "" {
				fmt.Fprintln(stdio.Out, "Không có yêu cầu tiến hoá nào (chưa đủ bằng chứng mới: `bint evolve scan`).")
				return nil
			}
			printDeferred(stdio.Out, rep, added, "")
			return nil
		},
	}
	c.Flags().Float64Var(&budget, "budget", 0, "ngân sách USD cho agent evolver (0 = chỉ yêu cầu xác định)")
	c.Flags().IntVar(&max, "max", 0, "số yêu cầu xử lý tối đa (mặc định evolution.deferred.max_per_run)")
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}
