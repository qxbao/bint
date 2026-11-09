package cli

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"bint/internal/app"
	"bint/internal/runlog"
	"bint/internal/store"
)

// openAndIngest mở workspace đã khởi tạo, mở cơ sở dữ liệu và nạp spool trước
// khi đọc, để kết quả luôn gồm các sự kiện mới nhất.
func openAndIngest(cmd *cobra.Command) (*app.Workspace, *store.Store, runlog.IngestResult, error) {
	dir, err := cwd()
	if err != nil {
		return nil, nil, runlog.IngestResult{}, err
	}
	ws, err := app.OpenWorkspace(cmd.Context(), dir, true)
	if err != nil {
		return nil, nil, runlog.IngestResult{}, err
	}
	s, err := ws.OpenStore(cmd.Context())
	if err != nil {
		return nil, nil, runlog.IngestResult{}, err
	}
	res, err := ws.Ingest(cmd.Context(), s, time.Now())
	if err != nil {
		s.Close()
		return nil, nil, res, err
	}
	return ws, s, res, nil
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

	var limit int
	var asJSON bool
	list := &cobra.Command{
		Use:   "list",
		Short: "Liệt kê lượt chạy, mới nhất trước",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, s, _, err := openAndIngest(cmd)
			if err != nil {
				return err
			}
			defer s.Close()
			runs, err := runlog.ListRuns(cmd.Context(), s.DB, limit)
			if err != nil {
				return err
			}
			if asJSON {
				out := make([]runJSON, 0, len(runs))
				for _, r := range runs {
					out = append(out, toJSON(r))
				}
				enc := json.NewEncoder(stdio.Out)
				enc.SetIndent("", "  ")
				return enc.Encode(out)
			}
			if len(runs) == 0 {
				fmt.Fprintln(stdio.Out, "Chưa có lượt chạy nào được ghi nhận.")
				return nil
			}
			tw := tabwriter.NewWriter(stdio.Out, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "RUN ID\tHARNESS\tTRẠNG THÁI\tBẮT ĐẦU\tTHỜI LƯỢNG\tTỆP SỬA\tTEST OK/LỖI\tNHÁNH")
			for _, r := range runs {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d\t%d/%d\t%s\n",
					r.RunID, orDash(r.Harness.String), r.Status, fmtTime(r.StartedAt.String), fmtDuration(r),
					r.FilesModified, r.TestsOK, r.TestsFailed, orDash(r.GitBranch.String))
			}
			return tw.Flush()
		},
	}
	list.Flags().IntVarP(&limit, "limit", "n", 20, "số lượt tối đa")
	list.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")

	var showJSON bool
	show := &cobra.Command{
		Use:   "show <run-id>",
		Short: "Chi tiết một lượt chạy (chấp nhận tiền tố ID)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			_, s, _, err := openAndIngest(cmd)
			if err != nil {
				return err
			}
			defer s.Close()
			r, err := runlog.GetRun(ctx, s.DB, args[0])
			if err != nil {
				return err
			}
			events, err := runlog.RunEvents(ctx, s.DB, r.RunID)
			if err != nil {
				return err
			}
			arts, err := runlog.RunArtifacts(ctx, s.DB, r.RunID)
			if err != nil {
				return err
			}
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
					Run       runJSON `json:"run"`
					Events    []ev    `json:"events"`
					Artifacts []art   `json:"artifacts"`
				}{Run: toJSON(r), Events: []ev{}, Artifacts: []art{}}
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
			fmt.Fprintf(w, "Run:          %s\n", r.RunID)
			fmt.Fprintf(w, "Trạng thái:   %s", r.Status)
			if r.EndReason.Valid {
				fmt.Fprintf(w, " (%s)", r.EndReason.String)
			}
			fmt.Fprintln(w)
			fmt.Fprintf(w, "Harness:      %s  phiên %s\n", orDash(r.Harness.String), orDash(r.SessionID.String))
			fmt.Fprintf(w, "Mô hình:      %s\n", orDash(r.Model.String))
			fmt.Fprintf(w, "Git:          %s @ %s\n", orDash(r.GitBranch.String), orDash(r.GitBaseCommit.String))
			fmt.Fprintf(w, "Worktree:     %s\n", orDash(r.Worktree.String))
			fmt.Fprintf(w, "Thời gian:    %s → %s (%s)\n", fmtTime(r.StartedAt.String), fmtTime(r.FinishedAt.String), fmtDuration(r))
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
	c.AddCommand(list, show)
	return c
}
