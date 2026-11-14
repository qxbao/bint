package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"bint/internal/app"
	"bint/internal/task"
)

// addRelationJSON thêm khoá quan hệ vào JSON của task new, chỉ khi phiếu có quan hệ.
func addRelationJSON(out map[string]any, t *task.Task) {
	if t.State.Parent != "" {
		out["parent"] = t.State.Parent
	}
	if len(t.State.DependsOn) > 0 {
		out["depends_on"] = t.State.DependsOn
	}
	if len(t.State.Inherited) > 0 {
		out["inherited"] = t.State.Inherited
	}
}

// printNewRelations in quan hệ của phiếu vừa lập (chỉ khi dùng --parent/--depends-on).
func printNewRelations(w io.Writer, t *task.Task) {
	if t.State.Parent != "" {
		fmt.Fprintf(w, "  Phiếu cha: %s\n", t.State.Parent)
	}
	if len(t.State.DependsOn) > 0 {
		fmt.Fprintf(w, "  Phụ thuộc: %s\n", strings.Join(t.State.DependsOn, ", "))
	}
	if len(t.State.Inherited) > 0 {
		fmt.Fprintf(w, "  Kế thừa từ phiếu cha: %s\n", strings.Join(t.State.Inherited, ", "))
	}
}

func relatedLine(r app.RelatedTask) string {
	st := r.Status
	if st == "" {
		st = "không đọc được"
	}
	s := r.ID + " [" + st + "]"
	if r.Goal != "" {
		s += " " + truncateRunes(r.Goal, 60)
	}
	return s
}

// printRelations in khối quan hệ của task show; không in gì khi phiếu không có quan hệ.
func printRelations(w io.Writer, d app.TaskDetail) {
	if d.Parent == nil && len(d.Deps) == 0 && len(d.Children) == 0 {
		return
	}
	fmt.Fprintln(w, "\nQuan hệ:")
	if d.Parent != nil {
		fmt.Fprintf(w, "  Phiếu cha: %s\n", relatedLine(*d.Parent))
	}
	for _, r := range d.Deps {
		fmt.Fprintf(w, "  Phụ thuộc: %s\n", relatedLine(r))
	}
	for _, r := range d.Children {
		fmt.Fprintf(w, "  Phiếu con: %s\n", relatedLine(r))
	}
	if len(d.Task.State.Inherited) > 0 {
		fmt.Fprintf(w, "  Kế thừa từ phiếu cha: %s\n", strings.Join(d.Task.State.Inherited, ", "))
	}
}

func newTaskReadyCmd(stdio IO) *cobra.Command {
	var parent string
	var asJSON bool
	c := &cobra.Command{
		Use:   "ready",
		Short: "Liệt kê phiếu sẵn sàng: chưa kết thúc và mọi phiếu nó phụ thuộc đã done",
		Long: `Liệt kê (cũ nhất trước) phiếu chưa done/cancelled mà mọi phiếu trong depends_on đã done; phiếu không có
depends_on luôn có mặt. Phụ thuộc đang làm dở, cancelled hoặc không đọc được làm phiếu vắng mặt. Phiếu cha
không ngầm phụ thuộc phiếu con. Đặt quan hệ bằng ` + "`bint task new --parent/--depends-on`" + ` hoặc ` + "`bint task set --depends-on`" + `.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ws, err := openTasks(cmd)
			if err != nil {
				return err
			}
			list, err := app.ReadyTasks(cmd.Context(), ws, app.TaskReadyOptions{Parent: parent})
			if err != nil {
				return err
			}
			type row struct {
				ID     string `json:"id"`
				Status string `json:"status"`
				Parent string `json:"parent,omitempty"`
				Goal   string `json:"goal"`
			}
			rows := []row{}
			for _, t := range list.Tasks {
				rows = append(rows, row{t.State.ID, t.State.Status, t.State.Parent, t.Ticket.Goal})
			}
			if asJSON {
				return writeJSON(stdio.Out, rows)
			}
			for _, e := range list.Bad {
				fmt.Fprintf(stdio.Err, "! %v\n", e)
			}
			if len(rows) == 0 {
				fmt.Fprintln(stdio.Out, "Không có tác vụ nào sẵn sàng.")
				return nil
			}
			tw := tabwriter.NewWriter(stdio.Out, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "TASK ID\tTRẠNG THÁI\tCHA\tMỤC TIÊU")
			for _, r := range rows {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.ID, r.Status, orDash(r.Parent), truncateRunes(r.Goal, 70))
			}
			return tw.Flush()
		},
	}
	c.Flags().StringVar(&parent, "parent", "", "chỉ phiếu con của phiếu này (ID hoặc tiền tố)")
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}
