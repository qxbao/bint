package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"bint/internal/app"
	"bint/internal/task"
	"bint/internal/workflow"
)

func newFindingsCmd(stdio IO) *cobra.Command {
	c := &cobra.Command{
		Use:   "findings",
		Short: "Kết cục của phát hiện review: liệt kê phát hiện còn mở, đặt open|fixed|rejected|deferred",
		Long: `Phát hiện review (bint task review) có kết cục: open (mặc định, kể cả dữ liệu cũ không có
kết cục), fixed (kèm commit), rejected hoặc deferred (kèm lý do). Kết cục ghi chỉ-thêm vào
findings.json trong thư mục tác vụ (kèm người đặt và thời điểm), không sửa review.json.
Lập phiếu sửa từ phát hiện: bint task new --from-findings <id>...`,
	}
	c.AddCommand(newFindingsListCmd(stdio), newFindingsSetCmd(stdio))
	return c
}

func newFindingsListCmd(stdio IO) *cobra.Command {
	var o app.FindingsListOptions
	var asJSON bool
	c := &cobra.Command{
		Use:   "list",
		Short: "Liệt kê phát hiện review qua mọi tác vụ (kể cả done/cancelled) kèm kết cục",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ws, err := openTasks(cmd)
			if err != nil {
				return err
			}
			res, err := app.ListFindings(cmd.Context(), ws, o)
			if err != nil {
				return err
			}
			for _, e := range res.Bad {
				fmt.Fprintln(stdio.Err, "cảnh báo: bỏ qua tác vụ không đọc được:", e)
			}
			if asJSON {
				type row struct {
					ID       string `json:"id"`
					Task     string `json:"task"`
					Round    int    `json:"round"`
					Kind     string `json:"kind"`
					Severity string `json:"severity"`
					Title    string `json:"title"`
					Path     string `json:"path,omitempty"`
					Lines    string `json:"lines,omitempty"`
					Outcome  string `json:"outcome"`
					Commit   string `json:"commit,omitempty"`
					Reason   string `json:"reason,omitempty"`
					By       string `json:"by,omitempty"`
					At       string `json:"at,omitempty"`
				}
				rows := make([]row, 0, len(res.Findings))
				for _, v := range res.Findings {
					f, oc := v.Finding, v.Outcome
					rows = append(rows, row{f.ID, v.TaskID, v.Round, f.Kind, f.Severity, f.Title, f.Path, f.Lines,
						oc.Outcome, oc.Commit, oc.Reason, oc.By, oc.At})
				}
				return writeJSON(stdio.Out, map[string]any{"findings": rows})
			}
			if len(res.Findings) == 0 {
				fmt.Fprintln(stdio.Out, "Không có phát hiện.")
				return nil
			}
			for _, v := range res.Findings {
				printFindingLine(stdio, v)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&o.Open, "open", false, "chỉ phát hiện có kết cục hiệu lực open")
	c.Flags().StringVar(&o.Task, "task", "", "chỉ phát hiện của một tác vụ (ID hoặc tiền tố)")
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func printFindingLine(stdio IO, v workflow.FindingView) {
	f := v.Finding
	loc := f.Path
	if loc != "" && f.Lines != "" {
		loc += ":" + f.Lines
	}
	fmt.Fprintf(stdio.Out, "%s  %-8s %s/%s  %s", f.ID, v.Outcome.Outcome, f.Kind, f.Severity, f.Title)
	if loc != "" {
		fmt.Fprintf(stdio.Out, "  (%s)", loc)
	}
	switch v.Outcome.Outcome {
	case task.OutcomeFixed:
		fmt.Fprintf(stdio.Out, "  [commit %s]", shortSHA(v.Outcome.Commit))
	case task.OutcomeRejected, task.OutcomeDeferred:
		fmt.Fprintf(stdio.Out, "  [%s]", v.Outcome.Reason)
	}
	fmt.Fprintln(stdio.Out)
}

func newFindingsSetCmd(stdio IO) *cobra.Command {
	var o app.FindingsSetOptions
	c := &cobra.Command{
		Use:   "set <open|fixed|rejected|deferred> <id-phát-hiện>...",
		Short: "Đặt kết cục cho một hoặc nhiều phát hiện (fixed cần --commit; rejected/deferred cần --reason)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			o.Outcome, o.IDs = args[0], args[1:]
			if err := o.Validate(); err != nil {
				return err
			}
			ws, err := openTasks(cmd)
			if err != nil {
				return err
			}
			recs, err := app.SetFindings(cmd.Context(), ws, o)
			if err != nil {
				return err
			}
			for _, r := range recs {
				extra := ""
				switch {
				case r.Commit != "":
					extra = " (commit " + r.Commit + ")"
				case r.Reason != "":
					extra = " (" + r.Reason + ")"
				}
				fmt.Fprintf(stdio.Out, "%s → %s%s\n", r.Finding, r.Outcome, extra)
			}
			return nil
		},
	}
	c.Flags().StringVar(&o.Commit, "commit", "", "commit đã sửa (bắt buộc với fixed; lưu SHA đầy đủ)")
	c.Flags().StringVar(&o.Reason, "reason", "", "lý do (bắt buộc với rejected và deferred)")
	return c
}
