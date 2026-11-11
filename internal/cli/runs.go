package cli

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"bint/internal/app"
	"bint/internal/config"
	"bint/internal/runlog"
)

// openRuns mở không gian lượt chạy ở thư mục hiện tại và nạp spool trước khi đọc, để kết quả luôn gồm các
// sự kiện mới nhất.
func openRuns(cmd *cobra.Command) (*app.RunsWS, error) {
	dir, err := cwd()
	if err != nil {
		return nil, err
	}
	return app.OpenRuns(cmd.Context(), dir, true)
}

type runJSON struct {
	RunID         string  `json:"run_id"`
	Workflow      *string `json:"workflow"`
	Status        string  `json:"status"`
	Harness       *string `json:"harness"`
	SessionID     *string `json:"harness_session_id"`
	Model         *string `json:"model"`
	BintVersion   *string `json:"bint_version"`
	GitBaseCommit *string `json:"git_base_commit"`
	GitBranch     *string `json:"git_branch"`
	Worktree      *string `json:"worktree"`
	StartedAt     *string `json:"started_at"`
	FinishedAt    *string `json:"finished_at"`
	LastEventAt   *string `json:"last_event_at"`
	DurationMS    *int64  `json:"duration_ms"`
	EndReason     *string `json:"end_reason"`
	FilesModified int     `json:"files_modified"`
	TestsOK       int     `json:"tests_ok"`
	TestsFailed   int     `json:"tests_failed"`
	Events        int     `json:"events"`
}

func toJSON(r runlog.Run) runJSON {
	ns := func(v sql.NullString) *string {
		if v.Valid {
			return &v.String
		}
		return nil
	}
	out := runJSON{
		RunID: r.RunID, Status: r.Status,
		Workflow: ns(r.Workflow), Harness: ns(r.Harness), SessionID: ns(r.SessionID), Model: ns(r.Model),
		BintVersion: ns(r.BintVersion), GitBaseCommit: ns(r.GitBaseCommit), GitBranch: ns(r.GitBranch),
		Worktree: ns(r.Worktree), StartedAt: ns(r.StartedAt), FinishedAt: ns(r.FinishedAt),
		LastEventAt: ns(r.LastEventAt), EndReason: ns(r.EndReason),
		FilesModified: r.FilesModified, TestsOK: r.TestsOK, TestsFailed: r.TestsFailed, Events: r.Events,
	}
	if r.DurationMS.Valid {
		d := r.DurationMS.Int64
		out.DurationMS = &d
	}
	return out
}

func fmtDuration(r runlog.Run) string {
	if !r.DurationMS.Valid {
		return "-"
	}
	return (time.Duration(r.DurationMS.Int64) * time.Millisecond).Round(time.Second).String()
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func newRunsCmd(stdio IO) *cobra.Command {
	c := &cobra.Command{Use: "runs", Short: "Lịch sử lượt chạy đã ghi nhận"}

	var asJSON bool
	lo := app.RunsListDefaults(config.Default())
	list := &cobra.Command{
		Use:   "list",
		Short: "Liệt kê lượt chạy, mới nhất trước",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, err := openRuns(cmd)
			if err != nil {
				return err
			}
			defer r.Close()
			rows, err := app.ListRuns(cmd.Context(), r, lo)
			if err != nil {
				return err
			}
			if asJSON {
				out := make([]runJSON, 0, len(rows))
				for _, row := range rows {
					out = append(out, toJSON(row.Run))
				}
				enc := json.NewEncoder(stdio.Out)
				enc.SetIndent("", "  ")
				return enc.Encode(out)
			}
			if len(rows) == 0 {
				fmt.Fprintln(stdio.Out, "Chưa có lượt chạy nào được ghi nhận.")
				return nil
			}
			tw := tabwriter.NewWriter(stdio.Out, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "RUN ID\tHARNESS\tTRẠNG THÁI\tBẮT ĐẦU\tTHỜI LƯỢNG\tTỆP SỬA\tTEST OK/LỖI\tNHÃN\tNHÁNH")
			for _, row := range rows {
				run := row.Run
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d\t%d/%d\t%s\t%s\n",
					run.RunID, orDash(run.Harness.String), run.Status, fmtTime(run.StartedAt.String), fmtDuration(run),
					run.FilesModified, run.TestsOK, run.TestsFailed, row.Label, orDash(run.GitBranch.String))
			}
			return tw.Flush()
		},
	}
	list.Flags().IntVarP(&lo.Limit, "limit", "n", lo.Limit, "số lượt tối đa")
	list.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")

	var showJSON bool
	show := &cobra.Command{
		Use:   "show <run-id>",
		Short: "Chi tiết một lượt chạy (chấp nhận tiền tố ID)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := openRuns(cmd)
			if err != nil {
				return err
			}
			defer r.Close()
			d, err := app.ShowRun(cmd.Context(), r, app.RunsShowOptions{Ref: args[0]})
			if err != nil {
				return err
			}
			run, events, arts, sigs, label, why := d.Run, d.Events, d.Artifacts, d.Signals, d.Label, d.Reason
			if showJSON {
				type ev struct {
					EventID   string          `json:"event_id"`
					Type      string          `json:"event_type"`
					Source    string          `json:"source"`
					Timestamp string          `json:"timestamp"`
					Payload   json.RawMessage `json:"payload"`
				}
				type art struct {
					Kind  string `json:"kind"`
					Path  string `json:"path"`
					Count int    `json:"count"`
				}
				out := struct {
					Run         runJSON         `json:"run"`
					Label       string          `json:"label"`
					LabelReason string          `json:"label_reason"`
					Signals     []runlog.Signal `json:"signals"`
					Events      []ev            `json:"events"`
					Artifacts   []art           `json:"artifacts"`
				}{Run: toJSON(run), Label: label, LabelReason: why, Signals: sigs, Events: []ev{}, Artifacts: []art{}}
				if out.Signals == nil {
					out.Signals = []runlog.Signal{}
				}
				for _, e := range events {
					out.Events = append(out.Events, ev{e.EventID, e.Type, e.Source, e.Timestamp, json.RawMessage(e.Payload)})
				}
				for _, a := range arts {
					out.Artifacts = append(out.Artifacts, art{a.Kind, a.Path, a.Count})
				}
				enc := json.NewEncoder(stdio.Out)
				enc.SetIndent("", "  ")
				return enc.Encode(out)
			}
			w := stdio.Out
			fmt.Fprintf(w, "Run:          %s\n", run.RunID)
			fmt.Fprintf(w, "Trạng thái:   %s", run.Status)
			if run.EndReason.Valid {
				fmt.Fprintf(w, " (%s)", run.EndReason.String)
			}
			fmt.Fprintln(w)
			fmt.Fprintf(w, "Harness:      %s  phiên %s\n", orDash(run.Harness.String), orDash(run.SessionID.String))
			fmt.Fprintf(w, "Mô hình:      %s\n", orDash(run.Model.String))
			fmt.Fprintf(w, "Git:          %s @ %s\n", orDash(run.GitBranch.String), orDash(run.GitBaseCommit.String))
			fmt.Fprintf(w, "Worktree:     %s\n", orDash(run.Worktree.String))
			fmt.Fprintf(w, "Thời gian:    %s → %s (%s)\n", fmtTime(run.StartedAt.String), fmtTime(run.FinishedAt.String), fmtDuration(run))
			fmt.Fprintf(w, "Nhãn:         %s (%s)\n", label, why)
			if len(sigs) > 0 {
				fmt.Fprintln(w, "\nTín hiệu kết quả:")
				for _, sg := range sigs {
					pol := "+"
					if !sg.Positive {
						pol = "-"
					}
					fmt.Fprintf(w, "  %s %-10s %-7s %-8s %s\n", pol, sg.Kind, sg.Strength, sg.Source, sg.Detail)
				}
			}
			if len(arts) > 0 {
				fmt.Fprintln(w, "\nĐầu ra:")
				for _, a := range arts {
					fmt.Fprintf(w, "  %-14s %s (x%d)\n", a.Kind, a.Path, a.Count)
				}
			}
			fmt.Fprintf(w, "\nSự kiện (%d):\n", len(events))
			for _, e := range events {
				fmt.Fprintf(w, "  %s  %-16s %s\n", fmtTime(e.Timestamp), e.Type, e.Payload)
			}
			return nil
		},
	}
	show.Flags().BoolVar(&showJSON, "json", false, "xuất JSON")
	c.AddCommand(list, show, newRunsRateCmd(stdio), newRunsScanCmd(stdio))
	return c
}

func newRunsRateCmd(stdio IO) *cobra.Command {
	o := app.RunsRateDefaults(config.Default())
	c := &cobra.Command{
		Use:   "rate <run-id> good|bad",
		Short: "Chấm kết quả một lượt chạy (tín hiệu mạnh, mục 9.6)",
		Long: `Người dùng chấm trực tiếp là tín hiệu mạnh nhưng thưa. Chấm lại thì lần sau
ghi đè lần trước. Không chạy được từ trong phiên agent (agent không tự chấm mình).`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			o.Ref, o.Rating = args[0], args[1]
			dir, err := cwd()
			if err != nil {
				return err
			}
			res, err := app.RateRun(cmd.Context(), dir, o)
			if err != nil {
				return err
			}
			fmt.Fprintf(stdio.Out, "Đã chấm %s: %s\n", res.RunID, res.Rating)
			return nil
		},
	}
	c.Flags().StringVarP(&o.Note, "note", "m", "", "ghi chú")
	return c
}

func newRunsScanCmd(stdio IO) *cobra.Command {
	o := app.RunsScanDefaults(config.Default())
	c := &cobra.Command{
		Use:   "scan",
		Short: "Quét lịch sử Git: gắn commit với lượt chạy, phát hiện thay đổi bị hoàn tác",
		Long: `Thu tín hiệu kết quả từ Git (mục 9.6):
  committed  commit chạm tệp lượt đã sửa, trong 24 giờ sau lượt — tích cực, yếu
  reverted   commit đó bị hoàn tác ("This reverts commit ...") — tiêu cực, mạnh

Gắn commit với lượt theo thời gian và tệp là gần đúng. Tín hiệu yếu không đủ để gán
nhãn; lượt chỉ có tín hiệu yếu là unlabeled.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, err := openRuns(cmd)
			if err != nil {
				return err
			}
			defer r.Close()
			res, err := app.ScanRuns(cmd.Context(), r, o)
			if err != nil {
				return err
			}
			fmt.Fprintf(stdio.Out, "Đã quét %d lượt: %d commit được gắn, %d hoàn tác\n", res.Runs, res.Committed, res.Reverted)
			return nil
		},
	}
	c.Flags().IntVar(&o.Days, "days", o.Days, "xét lượt chạy trong N ngày gần nhất")
	c.Flags().IntVar(&o.RevertDays, "revert-days", o.RevertDays, "hoàn tác trong N ngày sau commit được tính")
	return c
}
