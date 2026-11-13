package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"bint/internal/app"
	"bint/internal/config"
	"bint/internal/task"
	"bint/internal/workflow"
)

// openTasks mở môi trường thao tác tác vụ (cần `bint init` trên máy này).
func openTasks(cmd *cobra.Command) (*app.TaskWS, error) {
	dir, err := cwd()
	if err != nil {
		return nil, err
	}
	return app.OpenTasks(cmd.Context(), dir, Version)
}

// editFlags là các cờ sửa phiếu tác vụ dùng chung cho task new/set và run.
type editFlags struct {
	goal, verify, autonomy string
	criteria, allow, deny  []string
	constraints            []string
	clearCriteria          bool
	clearScope             bool
}

func addEditFlags(c *cobra.Command, e *editFlags, withClear bool) {
	f := c.Flags()
	f.StringVar(&e.goal, "goal", "", "mục tiêu một dòng")
	f.StringVar(&e.verify, "verify-command", "", "lệnh kiểm chứng (chỉ chạy sau khi người xác nhận)")
	f.StringArrayVar(&e.criteria, "criteria", nil, "tiêu chí chấp nhận (lặp lại được)")
	f.StringSliceVar(&e.allow, "allow", nil, "phạm vi được sửa (glob)")
	f.StringSliceVar(&e.deny, "deny", nil, "phạm vi không được sửa (glob)")
	f.StringArrayVar(&e.constraints, "constraint", nil, "ràng buộc (lặp lại được)")
	f.StringVar(&e.autonomy, "autonomy", "", "ask_always | ask_on_protected | auto")
	if withClear {
		f.BoolVar(&e.clearCriteria, "clear-criteria", false, "xoá tiêu chí cũ trước khi thêm")
		f.BoolVar(&e.clearScope, "clear-scope", false, "xoá phạm vi cũ trước khi thêm")
	} else {
		f.BoolVar(&e.clearScope, "clear-scope", false, "bỏ phạm vi Bint tự đoán từ đường dẫn trong mô tả; chỉ dùng --allow/--deny")
	}
}

// changed cho biết cờ nào của phiếu được đặt tường minh (kể cả giá trị rỗng, vd. xoá lệnh kiểm chứng).
func (e *editFlags) changed(c *cobra.Command) (goal, verify, autonomy bool) {
	f := c.Flags()
	return f.Changed("goal"), f.Changed("verify-command"), f.Changed("autonomy")
}

func readInput(stdio IO, args []string, i int) (string, error) {
	if len(args) <= i || args[i] == "-" {
		b, err := io.ReadAll(stdio.In)
		return string(b), err
	}
	b, err := os.ReadFile(args[i])
	return string(b), err
}

func newTaskCmd(stdio IO) *cobra.Command {
	c := &cobra.Command{
		Use:   "task",
		Short: "Tác vụ của workflow gốc: phiếu, trạng thái, tài liệu, kiểm chứng, review",
		Long: `Mỗi tác vụ có một phiếu (task.yml, mục 6.7) và trạng thái do Bint quản lý, nằm trong thư
mục trạng thái cục bộ: <state>/runtime/tasks/<task-id>/. Skill b-req, b-feat, b-review,
b-do và ` + "`bint run`" + ` ghi mọi đầu ra vào đó, liên kết bằng task ID.

Trạng thái: created → planned → in_progress → in_review → done; blocked / interrupted /
failed tiếp tục được (bint run resume); cancelled kết thúc. Xem ` + "`bint task transitions`" + `.

Chỉ người mới: xác nhận lệnh kiểm chứng và duyệt thay đổi chạm đường dẫn bảo vệ
(bint task approve), đặt autonomy auto, xác nhận hoàn thành khi thiếu bằng chứng (--force).`,
	}
	c.AddCommand(newTaskNewCmd(stdio), newTaskListCmd(stdio), newTaskShowCmd(stdio), newTaskSetCmd(stdio),
		newTaskWriteCmd(stdio), newTaskStatusCmd(stdio), newTaskVerifyCmd(stdio), newTaskCriteriaCmd(stdio),
		newTaskErrorCmd(stdio), newTaskReviewCmd(stdio), newTaskApproveCmd(stdio), newTaskTransitionsCmd(stdio),
		newTaskLintCmd(stdio), newTaskDiffCmd(stdio), newTaskLandCmd(stdio), newTaskCancelCmd(stdio))
	return c
}

func newTaskNewCmd(stdio IO) *cobra.Command {
	var (
		ef       editFlags
		wf       string
		plan     bool
		noPlan   bool
		asJSON   bool
		fromFile string
		depth    string
		worktree bool
		shared   bool
		driver   string
		fromFind []string
	)
	c := &cobra.Command{
		Use:   "new [mô tả | -]",
		Short: "Lập phiếu tác vụ từ mô tả tự do (bước Classify)",
		Long: `Lập phiếu tác vụ từ mô tả tự do: mục tiêu, phạm vi (đường dẫn nêu trong mô tả mà có
thật), lệnh kiểm chứng (eval.test_command đã xác nhận, nếu có), đề xuất có cần b-req
không, và đánh giá rủi ro. Bint chỉ hỏi lại khi thiếu trường quan trọng cho tác vụ rủi
ro cao (chạm đường dẫn bảo vệ mà không có cách kiểm chứng).

Tệp tài liệu (.md, .txt…) mà mô tả coi là kế hoạch/đặc tả cần làm theo ("thực hiện plan
docs/x.md") được ghi vào references thay vì scope.allow. Đề xuất b-req chỉ là heuristic:
orchestrator hoặc người quyết định bằng --plan/--no-plan/--depth và sửa phạm vi bằng
--clear-scope --allow. Agent không bỏ được b-req (--no-plan) khi tác vụ chạm đường dẫn bảo vệ.

Cô lập (chọn một lần lúc lập phiếu, ghi vào phiếu): workflow.isolation = auto sửa thẳng trên cây
đang làm, và tự chuyển tác vụ vào worktree riêng khi cây đó đang có tác vụ khác chưa kết thúc
(isolation_reason = concurrent_task); --worktree buộc worktree riêng (để chạy song song), --shared buộc
sửa thẳng trên cây đang làm. Tác vụ worktree đưa thay đổi về cây này bằng bint task land.

Driver (chọn một lần lúc lập phiếu): process (mặc định, workflow.driver) là bint run tự gọi harness cho từng bước;
--driver session để phiên đang mở chạy bước bằng subagent qua bint run next → Agent → bint run complete.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req := ""
			switch {
			case fromFile != "":
				b, err := os.ReadFile(fromFile)
				if err != nil {
					return err
				}
				req = string(b)
			case len(args) == 1 && args[0] == "-":
				b, err := io.ReadAll(stdio.In)
				if err != nil {
					return err
				}
				req = string(b)
			case len(args) == 1:
				req = args[0]
			}
			o := app.TaskNewDefaults(config.Default())
			o.Request, o.Workflow, o.Plan, o.NoPlan, o.Depth = req, wf, plan, noPlan, depth
			o.Goal, o.VerifyCommand, o.Autonomy, o.Criteria, o.Allow, o.Deny = ef.goal, ef.verify, ef.autonomy, ef.criteria, ef.allow, ef.deny
			o.Constraints, o.ClearScope = ef.constraints, ef.clearScope
			o.GoalSet, o.VerifyCommandSet, o.AutonomySet = ef.changed(cmd)
			o.Worktree, o.Shared, o.Driver = worktree, shared, driver
			o.FromFindings = fromFind
			if err := o.Validate(); err != nil { // lỗi dùng sai (mã 2) thắng lỗi "chưa init", như trước
				return err
			}
			ws, err := openTasks(cmd)
			if err != nil {
				return err
			}
			created, err := app.NewTask(cmd.Context(), ws, o)
			if err != nil {
				return err
			}
			env, t, a := ws.Env, created.Task, created.Assessment
			if asJSON {
				out := map[string]any{"id": t.State.ID, "dir": t.Dir, "status": t.State.Status,
					"ticket": t.Ticket, "classification": t.State.Classification, "assessment": a,
					"verify_confirmed": created.VerifyConfirmed, "isolation": t.State.Isolation(),
					"isolation_reason": isolationReason(t), "isolation_tasks": nonNil(isolationTasks(t)),
					"driver": taskDriver(t), "driver_reason": driverReason(t)}
				if len(t.State.FromFindings) > 0 {
					out["from_findings"] = t.State.FromFindings
				}
				return writeJSON(stdio.Out, out)
			}
			w := stdio.Out
			fmt.Fprintf(w, "Tác vụ %s: %s\n", t.State.ID, t.Ticket.Goal)
			fmt.Fprintf(w, "  Thư mục:   %s\n", t.Dir)
			if c := t.State.Classification; c != nil {
				fmt.Fprintf(w, "  Classify:  %s\n", c.Reason)
				printDepthLine(w, "  Phân tích: ", c)
			}
			printIsolationLine(w, "  Cô lập:    ", t)
			printDriverLines(w, "  ", t)
			if len(t.State.FromFindings) > 0 {
				fmt.Fprintf(w, "  Từ phát hiện: %s\n", strings.Join(t.State.FromFindings, ", "))
			}
			printTicketBrief(w, env, t, a)
			return nil
		},
	}
	addEditFlags(c, &ef, false)
	c.Flags().StringVar(&wf, "workflow", "b-do", "workflow tạo tác vụ (b-do | b-req | b-feat | b-review)")
	c.Flags().BoolVar(&plan, "plan", false, "luôn qua b-req")
	c.Flags().BoolVar(&noPlan, "no-plan", false, "bỏ qua b-req")
	c.Flags().StringVar(&depth, "depth", "auto", depthFlagHelp)
	c.Flags().BoolVar(&worktree, "worktree", false, "chạy tác vụ trong worktree riêng (song song với tác vụ khác); loại trừ --shared")
	c.Flags().BoolVar(&shared, "shared", false, "sửa thẳng trên cây đang làm, không vào worktree riêng; loại trừ --worktree")
	c.Flags().StringVar(&driver, "driver", "auto", "ai chạy các bước của tác vụ: auto (theo workflow.driver) | process | session (phiên đang mở chạy bước bằng subagent: bint run next/complete)")
	c.Flags().StringSliceVar(&fromFind, "from-findings", nil, "lập phiếu sửa từ phát hiện review (ID dạng <task-id>.<n>, lặp lại hoặc dấu phẩy): phạm vi suy từ đường dẫn của chúng, mô tả thành tuỳ chọn; loại phát hiện fixed/rejected")
	c.Flags().StringVar(&fromFile, "file", "", "đọc mô tả từ tệp")
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func printTicketBrief(w io.Writer, env *workflow.Env, t *task.Task, a task.Assessment) {
	tk := t.Ticket
	scope := "(chưa xác định)"
	if len(tk.Scope.Allow) > 0 {
		scope = strings.Join(tk.Scope.Allow, ", ")
	}
	if len(tk.Scope.Deny) > 0 {
		scope += "  deny: " + strings.Join(tk.Scope.Deny, ", ")
	}
	fmt.Fprintf(w, "  Phạm vi:   %s\n", scope)
	if len(tk.References) > 0 {
		fmt.Fprintf(w, "  Tài liệu:  %s\n", strings.Join(tk.References, ", "))
	}
	v := "(chưa có)"
	if tk.Verify.Command != "" {
		v = tk.Verify.Command
		if env.VerifyConfirmed(t) {
			v += "  [đã xác nhận]"
		} else {
			v += "  [chưa xác nhận: bint task approve " + t.State.ID + "]"
		}
	}
	fmt.Fprintf(w, "  Kiểm chứng: %s\n", v)
	if ss := env.SetupStatus(t); ss.Required {
		sv := ss.Command
		if ss.Confirmed {
			sv += "  [đã xác nhận]"
		} else {
			sv += "  [chưa xác nhận: bint task approve " + t.State.ID + " --setup-sha " + ss.SHA256[:workflow.MinSetupSHAPrefix] + "]"
		}
		fmt.Fprintf(w, "  Setup worktree: %s\n", sv)
	} else if ap := t.State.SetupApproval; ap != nil {
		// Worktree đã tạo (hoặc tác vụ kết thúc): lệnh đã duyệt và đã chạy, không phải lệnh của cấu hình hiện tại.
		fmt.Fprintf(w, "  Setup worktree: %s  [đã xác nhận]\n", ap.Command)
	}
	for i, c := range tk.Verify.Criteria {
		fmt.Fprintf(w, "  Tiêu chí %d: %s\n", i+1, c)
	}
	fmt.Fprintf(w, "  Autonomy:  %s   Rủi ro: %s\n", tk.Autonomy, a.Risk)
	for _, q := range a.Questions {
		fmt.Fprintf(w, "? %s\n", q)
	}
	for _, n := range a.Notes {
		fmt.Fprintf(w, "! %s\n", n)
	}
}

func newTaskListCmd(stdio IO) *cobra.Command {
	var all, asJSON bool
	c := &cobra.Command{
		Use:   "list",
		Short: "Liệt kê tác vụ (mặc định: chưa kết thúc)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ws, err := openTasks(cmd)
			if err != nil {
				return err
			}
			list, err := app.ListTasks(cmd.Context(), ws, app.TaskListOptions{All: all})
			if err != nil {
				return err
			}
			bad := list.Bad
			type row struct {
				ID       string `json:"id"`
				Status   string `json:"status"`
				Workflow string `json:"workflow"`
				Updated  string `json:"updated_at"`
				Goal     string `json:"goal"`
				Reason   string `json:"reason,omitempty"`
			}
			rows := []row{}
			for _, t := range list.Tasks {
				rows = append(rows, row{t.State.ID, t.State.Status, t.State.Workflow, t.State.UpdatedAt, t.Ticket.Goal, t.State.Reason})
			}
			if asJSON {
				return writeJSON(stdio.Out, rows)
			}
			for _, e := range bad {
				fmt.Fprintf(stdio.Err, "! %v\n", e)
			}
			if len(rows) == 0 {
				fmt.Fprintln(stdio.Out, "Không có tác vụ nào.")
				return nil
			}
			tw := tabwriter.NewWriter(stdio.Out, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "TASK ID\tTRẠNG THÁI\tWORKFLOW\tCẬP NHẬT\tMỤC TIÊU")
			for _, r := range rows {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", r.ID, r.Status, r.Workflow, fmtRFC(r.Updated), truncateRunes(r.Goal, 70))
			}
			return tw.Flush()
		},
	}
	c.Flags().BoolVarP(&all, "all", "a", false, "gồm cả tác vụ done/cancelled")
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func fmtRFC(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.Local().Format("2006-01-02 15:04")
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func newTaskShowCmd(stdio IO) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "show <task-id>",
		Short: "Chi tiết tác vụ: phiếu, trạng thái, tài liệu, kiểm chứng, tiêu chí, review",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := openTasks(cmd)
			if err != nil {
				return err
			}
			d, err := app.ShowTask(cmd.Context(), ws, app.TaskShowOptions{Ref: args[0]})
			if err != nil {
				return err
			}
			t, a, rev, blockers := d.Task, d.Assessment, d.Review, d.Blockers
			if asJSON {
				arts := map[string]string{}
				for _, x := range d.Artifacts {
					if x.Exists {
						arts[x.Name] = x.Path
					}
				}
				return writeJSON(stdio.Out, map[string]any{"dir": t.Dir, "ticket": t.Ticket, "state": t.State, "classification": t.State.Classification, "assessment": a,
					"verify_confirmed": d.VerifyConfirmed, "setup_command": d.Setup.Command, "setup_confirmed": d.Setup.Confirmed,
					"setup_required": d.Setup.Required, "artifacts": arts, "review": rev, "done_blockers": nonNil(blockers),
					"isolation": t.State.Isolation(), "isolation_reason": isolationReason(t), "isolation_tasks": nonNil(isolationTasks(t)),
					"driver": taskDriver(t), "driver_reason": driverReason(t)})
			}
			printTask(stdio.Out, ws.Env, t, a, rev, blockers)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func nonNil(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}

func printTask(w io.Writer, env *workflow.Env, t *task.Task, a task.Assessment, rev *task.Review, blockers []string) {
	st := t.State
	fmt.Fprintf(w, "Tác vụ %s: %s\n", st.ID, t.Ticket.Goal)
	status := st.Status
	if st.Reason != "" {
		status += " — " + st.Reason
	}
	fmt.Fprintf(w, "  Trạng thái: %s\n", status)
	if st.ResumeFrom != "" {
		fmt.Fprintf(w, "  Tiếp tục từ: %s (bint run resume %s)\n", st.ResumeFrom, st.ID)
	}
	if st.Runner != nil {
		fmt.Fprintf(w, "  Điều phối:  bint run pid %d, lượt %s\n", st.Runner.PID, st.Runner.RunID)
	}
	fmt.Fprintf(w, "  Workflow:   %s   base %s", st.Workflow, orDash(shortSHA(st.Base)))
	if st.Branch != "" {
		fmt.Fprintf(w, " (%s)", st.Branch)
	}
	fmt.Fprintln(w)
	if st.FlowStep != "" {
		fmt.Fprintf(w, "  Bước flow:  %s\n", st.FlowStep)
	}
	if hs := st.HeldStep; hs != nil {
		fmt.Fprintf(w, "  Bước giữ:   %s (lượt %s) đã xong nhưng cây đổi trong lúc chạy: %s — resume áp kết quả của bước, không chạy lại\n",
			hs.Step, hs.RunID, strings.Join(hs.Files, ", "))
	}
	if c := st.Classification; c != nil {
		fmt.Fprintf(w, "  Classify:   %s\n", c.Reason)
		printDepthLine(w, "  Phân tích:  ", c)
	}
	printIsolationLines(w, "  ", t)
	printDriverLines(w, "  ", t)
	fmt.Fprintf(w, "  Thư mục:    %s\n", t.Dir)
	printTicketBrief(w, env, t, a)
	if t.Ticket.Request != "" {
		fmt.Fprintf(w, "\nYêu cầu gốc:\n%s\n", indent(t.Ticket.Request, "  "))
	}
	fmt.Fprintln(w, "\nTài liệu:")
	for _, n := range task.ArtifactNames() {
		mark := "-"
		if _, err := os.Stat(t.ArtifactPath(n)); err == nil {
			mark = "✓"
		}
		fmt.Fprintf(w, "  %s %-12s %s\n", mark, n, t.ArtifactPath(n))
	}
	if len(t.Ticket.Verify.Criteria) > 0 {
		fmt.Fprintln(w, "\nTiêu chí chấp nhận:")
		for i, c := range t.Ticket.Verify.Criteria {
			mark, ev := "?", ""
			if cs := st.CriterionStatus(i + 1); cs != nil {
				mark = "✗"
				if cs.Met {
					mark = "✓"
				}
				ev = " — " + cs.Evidence
			}
			fmt.Fprintf(w, "  %s %d. %s%s\n", mark, i+1, c, ev)
		}
	}
	if v := st.LastVerification(); v != nil {
		conf := ""
		if !v.Confirmed {
			conf = " (chưa xác nhận)"
		}
		fmt.Fprintf(w, "\nKiểm chứng gần nhất: %s → %s (mã %d)%s  %s\n  log: %s\n", v.Command, passText(v.Passed), v.ExitCode, conf, fmtRFC(v.At), v.Log)
	}
	if len(st.PendingPaths) > 0 {
		fmt.Fprintf(w, "\nChờ người duyệt (bint task approve %s):\n  %s\n", st.ID, strings.Join(st.PendingPaths, "\n  "))
	}
	if p := st.ProtectedApproval; p != nil {
		fmt.Fprintf(w, "\nĐã duyệt bởi %s: %s\n", p.By, strings.Join(p.Paths, ", "))
	}
	if last := rev.Last(); last != nil {
		fmt.Fprintf(w, "\nReview vòng %d (%s): %s\n", last.Round, fmtRFC(last.At), last.Summary)
		for _, f := range last.Findings {
			loc := f.Path
			if f.Lines != "" {
				loc += ":" + f.Lines
			}
			fmt.Fprintf(w, "  [%s] %s/%s %s  %s\n", f.ID, f.Kind, f.Severity, f.Title, loc)
		}
		fmt.Fprintf(w, "  báo cáo: %s\n", t.Dir+string(os.PathSeparator)+task.ReviewMD)
	}
	if len(st.Errors) > 0 {
		fmt.Fprintln(w, "\nLỗi / lần thử thất bại:")
		from := max(0, len(st.Errors)-5)
		for _, e := range st.Errors[from:] {
			step := ""
			if e.Step != "" {
				step = "[" + e.Step + "] "
			}
			fmt.Fprintf(w, "  %s %s%s\n", fmtRFC(e.At), step, truncateRunes(e.Message, 200))
		}
	}
	if len(blockers) > 0 && !task.Terminal(st.Status) {
		fmt.Fprintln(w, "\nChưa thể done:")
		for _, b := range blockers {
			fmt.Fprintf(w, "  - %s\n", b)
		}
	}
	if len(st.Runs) > 0 {
		fmt.Fprintf(w, "\nLượt chạy: %s\n", strings.Join(st.Runs, " "))
	}
	fmt.Fprintln(w, "\nLịch sử:")
	for _, h := range st.History {
		from := h.From
		if from == "" {
			from = "∅"
		}
		fmt.Fprintf(w, "  %s  %s → %s  (%s) %s\n", fmtRFC(h.At), from, h.To, h.By, h.Reason)
	}
}

func shortSHA(s string) string {
	if len(s) > 10 {
		return s[:10]
	}
	return s
}

func passText(ok bool) string {
	if ok {
		return "qua"
	}
	return "không qua"
}

func newTaskSetCmd(stdio IO) *cobra.Command {
	var ef editFlags
	c := &cobra.Command{
		Use:   "set <task-id>",
		Short: "Sửa phiếu tác vụ (mục tiêu, kiểm chứng, tiêu chí, phạm vi, autonomy)",
		Long: `Sửa phiếu tác vụ. --criteria/--allow/--deny/--constraint thêm vào danh sách hiện có
(--clear-criteria, --clear-scope để xoá trước). Đổi verify.command thì lần xác nhận cũ
không còn hiệu lực. Agent không đặt được autonomy: auto.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := openTasks(cmd)
			if err != nil {
				return err
			}
			o := app.TaskSetDefaults(ws.K.Config)
			o.Ref, o.Goal, o.VerifyCommand, o.Autonomy = args[0], ef.goal, ef.verify, ef.autonomy
			o.Criteria, o.Allow, o.Deny, o.Constraints = ef.criteria, ef.allow, ef.deny, ef.constraints
			o.ClearCriteria, o.ClearScope = ef.clearCriteria, ef.clearScope
			o.GoalSet, o.VerifyCommandSet, o.AutonomySet = ef.changed(cmd)
			u, err := app.SetTask(cmd.Context(), ws, o)
			if err != nil {
				return err
			}
			fmt.Fprintf(stdio.Out, "Đã cập nhật %s: %s\n", u.Task.State.ID, u.Task.Ticket.Goal)
			for _, n := range u.Notes {
				fmt.Fprintf(stdio.Out, "  đồng bộ tiêu chí: %s\n", n)
			}
			printTicketBrief(stdio.Out, ws.Env, u.Task, u.Assessment)
			return nil
		},
	}
	addEditFlags(c, &ef, true)
	return c
}

func newTaskWriteCmd(stdio IO) *cobra.Command {
	return &cobra.Command{
		Use:   "write <task-id> <requirement|plan|tasks> [tệp | -]",
		Short: "Ghi tài liệu của tác vụ (nội dung từ tệp hoặc stdin)",
		Args:  cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := openTasks(cmd)
			if err != nil {
				return err
			}
			body, err := readInput(stdio, args, 2)
			if err != nil {
				return err
			}
			p, err := app.WriteTaskArtifact(cmd.Context(), ws, app.TaskWriteOptions{Ref: args[0], Name: args[1], Content: body})
			if err != nil {
				return err
			}
			fmt.Fprintf(stdio.Out, "Đã ghi %s\n", p)
			return nil
		},
	}
}

func newTaskStatusCmd(stdio IO) *cobra.Command {
	var reason string
	var force bool
	c := &cobra.Command{
		Use:   "status <task-id> <trạng thái>",
		Short: "Chuyển trạng thái tác vụ theo bảng chuyển (done cần bằng chứng)",
		Long: `Chuyển trạng thái theo bảng chuyển (bint task transitions). Chuyển sang done cần bằng
chứng: lệnh kiểm chứng đã được người xác nhận và lần chạy cuối qua trên đúng trạng thái mã
hiện tại; hoặc (không có lệnh kiểm chứng) mọi tiêu chí đã được đối chiếu là đạt; không có
thay đổi chạm đường dẫn bảo vệ chưa duyệt; không tiêu chí nào bị đánh dấu chưa đạt.
Chỉ người mới --force được khi thiếu bằng chứng (lý do được ghi vào lịch sử).

Tác vụ đang được ` + "`bint run`" + ` điều phối thì trạng thái do Bint Core chuyển.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := openTasks(cmd)
			if err != nil {
				return err
			}
			r, err := app.SetTaskStatus(cmd.Context(), ws, app.TaskStatusOptions{Ref: args[0], To: args[1], Reason: reason, Force: force})
			if err != nil {
				return err
			}
			fmt.Fprintf(stdio.Out, "%s → %s\n", r.Task.State.ID, r.Task.State.Status)
			if force && len(r.Blockers) > 0 {
				fmt.Fprintf(stdio.Out, "! xác nhận dù thiếu bằng chứng: %s\n", strings.Join(r.Blockers, "; "))
			}
			return nil
		},
	}
	c.Flags().StringVarP(&reason, "reason", "m", "", "lý do")
	c.Flags().BoolVar(&force, "force", false, "người xác nhận hoàn thành dù thiếu bằng chứng")
	return c
}

func newTaskVerifyCmd(stdio IO) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "verify <task-id>",
		Short: "Chạy lệnh kiểm chứng của phiếu (chỉ khi người đã xác nhận) và ghi kết quả",
		Long: `Chạy verify.command trong thư mục gốc repository (giới hạn workflow.verify_timeout_minutes),
log trong thư mục tác vụ. Kết quả là tín hiệu kết quả mạnh của lượt chạy (mục 9.6). Lệnh do
agent đề xuất chưa được người xác nhận thì không được chạy (security.require_approval_for_commands).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := openTasks(cmd)
			if err != nil {
				return err
			}
			vr, err := app.VerifyTask(cmd.Context(), ws, app.TaskVerifyOptions{Ref: args[0]})
			if err != nil {
				return err
			}
			t, res := vr.Task, vr.Result
			if asJSON {
				return writeJSON(stdio.Out, map[string]any{"task_id": t.State.ID, "skipped": res.Skipped, "verification": res.Verification})
			}
			if res.Skipped != "" {
				fmt.Fprintf(stdio.Out, "Bỏ qua: %s\n", res.Skipped)
				return nil
			}
			v := res.Verification
			fmt.Fprintf(stdio.Out, "%s → %s (mã %d)\nlog: %s\n", v.Command, passText(v.Passed), v.ExitCode, v.Log)
			if !v.Passed {
				return withCode(ExitError, errors.New("kiểm chứng không qua"))
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func newTaskCriteriaCmd(stdio IO) *cobra.Command {
	var evidence string
	c := &cobra.Command{
		Use:   "criteria <task-id> <số> met|unmet",
		Short: "Ghi kết quả đối chiếu một tiêu chí chấp nhận (kèm bằng chứng)",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := strconv.Atoi(args[1])
			if err != nil {
				return usageError{fmt.Errorf("số tiêu chí không hợp lệ %q", args[1])}
			}
			v := strings.ToLower(args[2])
			if err := (app.TaskCriteriaOptions{Index: n, Result: v}).Validate(); err != nil {
				return err
			}
			ws, err := openTasks(cmd)
			if err != nil {
				return err
			}
			t, err := app.CheckTaskCriterion(cmd.Context(), ws, app.TaskCriteriaOptions{Ref: args[0], Index: n, Result: v, Evidence: evidence})
			if err != nil {
				return err
			}
			fmt.Fprintf(stdio.Out, "%s tiêu chí %d: %s\n", t.State.ID, n, v)
			return nil
		},
	}
	c.Flags().StringVarP(&evidence, "evidence", "e", "", "bằng chứng cụ thể (bắt buộc)")
	return c
}

func newTaskErrorCmd(stdio IO) *cobra.Command {
	var step string
	c := &cobra.Command{
		Use:   "error <task-id> <nội dung>",
		Short: "Ghi một lỗi hoặc lần thử sửa thất bại",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := openTasks(cmd)
			if err != nil {
				return err
			}
			if _, err := app.RecordTaskError(cmd.Context(), ws, app.TaskErrorOptions{Ref: args[0], Message: args[1], Step: step}); err != nil {
				return err
			}
			fmt.Fprintln(stdio.Out, "Đã ghi.")
			return nil
		},
	}
	c.Flags().StringVar(&step, "step", "", "bước (b-req, b-feat, b-review…)")
	return c
}

func newTaskReviewCmd(stdio IO) *cobra.Command {
	return &cobra.Command{
		Use:   "review <task-id> [tệp | -]",
		Short: "Ghi một vòng review (JSON: summary + findings)",
		Long: `Ghi một vòng review từ JSON:

  {"summary": "...", "findings": [{"kind": "bug|risk|suggestion",
    "severity": "critical|high|medium|low", "title": "...", "path": "...",
    "lines": "a-b", "evidence": "...", "body": "...", "criterion": 1}]}

Mỗi phát hiện được gán ID <task-id>.<n>; chuyển thành tri thức bằng
bint knowledge add --from-review <id>. Báo cáo Markdown: review.md trong thư mục tác vụ.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := openTasks(cmd)
			if err != nil {
				return err
			}
			body, err := readInput(stdio, args, 1)
			if err != nil {
				return err
			}
			rv, err := app.SubmitTaskReview(cmd.Context(), ws, app.TaskReviewOptions{Ref: args[0], JSON: body})
			if err != nil {
				return err
			}
			t, r := rv.Task, rv.Round
			fmt.Fprintf(stdio.Out, "Review vòng %d của %s: %d phát hiện, %d nghiêm trọng\n", r.Round, t.State.ID, len(r.Findings), len(r.Blocking()))
			for _, f := range r.Findings {
				fmt.Fprintf(stdio.Out, "  [%s] %s/%s %s\n", f.ID, f.Kind, f.Severity, f.Title)
			}
			return nil
		},
	}
}

func newTaskApproveCmd(stdio IO) *cobra.Command {
	var paths []string
	var setupSHA string
	c := &cobra.Command{
		Use:   "approve <task-id>",
		Short: "Người xác nhận lệnh kiểm chứng và duyệt thay đổi đang chờ (đường dẫn bảo vệ/deny)",
		Long: `Chỉ người dùng (không chạy được từ trong phiên agent):
  - xác nhận verify.command hiện tại của phiếu → Bint được chạy lệnh đó và kết quả là tín hiệu mạnh;
  - xác nhận workflow.worktree.setup_command cho tác vụ worktree chưa tạo worktree, chỉ khi có
    --setup-sha <băm> của đúng lệnh bạn đã xem (thiếu cờ thì lệnh in lệnh và băm để bạn xem rồi chạy lại;
    băm không khớp lệnh hiện tại thì báo lỗi, không duyệt gì) → Bint mới chạy lệnh đó khi tạo worktree;
    đổi lệnh thì phải xác nhận lại;
  - duyệt các tệp đang chờ (thay đổi chạm limits.protected_paths, scope.deny, hoặc ngoài
    scope.allow khi autonomy ask_always).
Không đổi trạng thái; tiếp tục bằng bint run resume <task-id>. Xem diff trước khi duyệt.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := openTasks(cmd)
			if err != nil {
				return err
			}
			t, a, err := app.ApproveTask(cmd.Context(), ws, app.TaskApproveOptions{Ref: args[0], Paths: paths, SetupSHA: setupSHA})
			if err != nil {
				return err
			}
			if a.SetupWaiting != "" {
				fmt.Fprintf(stdio.Out, "! Lệnh setup worktree CHƯA được xác nhận: %s\n  Lệnh chạy với quyền của bạn khi tạo worktree. Xem kỹ rồi xác nhận: bint task approve %s --setup-sha %s\n",
					a.SetupWaiting, t.State.ID, a.SetupWaitingSHA[:workflow.MinSetupSHAPrefix])
			}
			if a.VerifyCommand == "" && a.SetupCommand == "" && len(a.Paths) == 0 {
				if a.SetupWaiting != "" {
					return nil
				}
				fmt.Fprintf(stdio.Out, "%s: không có gì chờ duyệt.\n", t.State.ID)
				return nil
			}
			if a.VerifyCommand != "" {
				fmt.Fprintf(stdio.Out, "✓ Xác nhận lệnh kiểm chứng: %s\n", a.VerifyCommand)
			}
			if a.SetupCommand != "" {
				fmt.Fprintf(stdio.Out, "✓ Xác nhận lệnh setup worktree: %s\n", a.SetupCommand)
			}
			if len(a.Paths) > 0 {
				fmt.Fprintf(stdio.Out, "✓ Duyệt thay đổi: %s\n", strings.Join(a.Paths, ", "))
			}
			if task.Paused(t.State.Status) {
				fmt.Fprintf(stdio.Out, "Tiếp tục: bint run resume %s\n", t.State.ID)
			}
			return nil
		},
	}
	c.Flags().StringVar(&setupSHA, "setup-sha", "", "băm sha256 (≥ 12 ký tự đầu) của lệnh setup worktree bạn đã xem; chỉ xác nhận khi khớp lệnh hiện tại")
	c.Flags().StringArrayVar(&paths, "path", nil, "duyệt trước một tệp cụ thể khi hook đã chặn trước lúc tạo/sửa (lặp lại được, không dùng glob); resume sẽ chạy lại b-feat")
	return c
}

func newTaskTransitionsCmd(stdio IO) *cobra.Command {
	return &cobra.Command{
		Use:   "transitions",
		Short: "Bảng chuyển trạng thái tác vụ",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			tw := tabwriter.NewWriter(stdio.Out, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "TỪ\tSANG\tĐIỀU KIỆN")
			for _, r := range app.TaskTransitions() {
				fmt.Fprintf(tw, "%s\t%s\t%s\n", r.From, r.To, r.Note)
			}
			return tw.Flush()
		},
	}
}

func newTaskLintCmd(stdio IO) *cobra.Command {
	var strict, asJSON bool
	c := &cobra.Command{
		Use:   "lint <task-id>",
		Short: "Kiểm ba tài liệu của b-req (requirement/plan/tasks) theo hợp đồng artifact v1",
		Long: `Kiểm requirement.md, plan.md, tasks.md của tác vụ: đủ tệp không rỗng, AC-N liên tục và khớp từng
tiêu chí trong task.yml, TSK-N có tham chiếu AC và "xong khi:", mọi AC được TSK bao phủ, và liệt kê
câu hỏi mở (## Câu hỏi mở). Không sửa gì và không ghi state.

Task theo hợp đồng v1 (planning_contract_version: 1) luôn kiểm nghiêm ngặt; task cũ chỉ cảnh báo
trừ khi có --strict (chủ động nâng cấp). Mã thoát: 1 khi có lỗi hoặc câu hỏi mở, 0 khi chỉ có cảnh báo.

Câu hỏi mở: ghi câu trả lời dạng "- [x] Q-1: <câu hỏi> — Trả lời: <nội dung>" bằng
bint task write <id> requirement -, rồi bint run resume <id>.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := openTasks(cmd)
			if err != nil {
				return err
			}
			lr, err := app.LintTask(cmd.Context(), ws, app.TaskLintOptions{Ref: args[0], Strict: strict})
			if err != nil {
				return err
			}
			t, rep := lr.Task, lr.Report
			if asJSON {
				if err := writeJSON(stdio.Out, map[string]any{"task_id": t.State.ID, "contract_version": t.State.PlanningContractVersion,
					"errors": rep.Errors, "warnings": rep.Warnings, "open_questions": rep.OpenQuestions}); err != nil {
					return err
				}
			} else {
				w := stdio.Out
				for _, i := range rep.Errors {
					fmt.Fprintf(w, "✗ [%s] %s: %s\n", i.Code, i.Artifact, i.Detail)
				}
				for _, i := range rep.Warnings {
					fmt.Fprintf(w, "! [%s] %s: %s\n", i.Code, i.Artifact, i.Detail)
				}
				for _, i := range rep.OpenQuestions {
					fmt.Fprintf(w, "? [%s] %s: %s\n", i.Code, i.Artifact, i.Detail)
				}
				if len(rep.Errors)+len(rep.Warnings)+len(rep.OpenQuestions) == 0 {
					fmt.Fprintf(w, "✓ %s: artifact đạt hợp đồng v1\n", t.State.ID)
				} else if len(rep.OpenQuestions) > 0 {
					fmt.Fprintf(w, "Ghi câu trả lời vào requirement.md (— Trả lời: …) rồi bint run resume %s\n", t.State.ID)
				}
			}
			if rep.Blocking() || len(rep.OpenQuestions) > 0 {
				return withCode(ExitError, fmt.Errorf("tác vụ %s: %d lỗi, %d câu hỏi mở", t.State.ID, len(rep.Errors), len(rep.OpenQuestions)))
			}
			return nil
		},
	}
	c.Flags().BoolVar(&strict, "strict", false, "áp hợp đồng v1 lên task cũ (không ghi state)")
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

const depthFlagHelp = app.DepthFlagHelp

// printDepthLine in độ sâu phân tích của b-req (chỉ khi tác vụ có b-req).
func printDepthLine(w io.Writer, prefix string, c *task.Classification) {
	if !c.Plan {
		return
	}
	reason := c.DepthReason
	if reason == "" {
		reason = "mặc định (tác vụ chưa ghi độ sâu)"
	}
	fmt.Fprintf(w, "%s%s (%s)\n", prefix, c.EffectiveDepth(), reason)
}

func newTaskDiffCmd(stdio IO) *cobra.Command {
	var o app.TaskDiffOptions
	c := &cobra.Command{
		Use:   "diff <task-id>",
		Short: "Thay đổi của riêng một tác vụ (không lẫn thay đổi có sẵn hay của tác vụ khác)",
		Long: `Cho biết tác vụ đã đổi gì: so ảnh chụp cây làm việc trước b-feat đầu tiên (gồm cả thay đổi chưa commit
có sẵn lúc đó) với cây làm việc hiện tại, chỉ trên các tệp mà các lượt b-feat của tác vụ đã đổi (gồm tệp
mới). Dùng thay cho git diff <base> khi review: git diff toàn cây lẫn thay đổi của tác vụ khác đang chạy
trên cùng worktree. Tệp khác đổi vào cùng cây (người hay tác vụ khác commit/sửa) bị loại khỏi diff; ghi chú
ở stderr nêu số và tên các tệp đã loại, --all để xem lại toàn bộ.

Phiếu lập trước khi Bint theo dõi tệp riêng của từng tác vụ thì so commit base với cây hiện tại trên toàn cây
(có ghi chú ở stderr). Chạy ở thư mục làm việc của tác vụ (State.Worktree), không cần khoá, không ghi cây làm việc.

  --stat        thống kê số dòng đổi theo tệp
  --name-only   chỉ liệt kê tên tệp
  --all         gồm cả tệp không thuộc tác vụ (mặc định bị loại)`,
		Example: `  bint task diff 01J…
  bint task diff 01J… --stat
  bint task diff 01J… --name-only
  bint task diff 01J… --all`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			o.Ref = args[0]
			if err := o.Validate(); err != nil {
				return err
			}
			ws, err := openTasks(cmd)
			if err != nil {
				return err
			}
			res, err := app.DiffTask(cmd.Context(), ws, o)
			if err != nil {
				return err
			}
			if res.Note != "" {
				fmt.Fprintln(stdio.Err, "! "+res.Note)
			}
			fmt.Fprint(stdio.Out, res.Text)
			return nil
		},
	}
	c.Flags().BoolVar(&o.Stat, "stat", false, "thống kê số dòng đổi theo tệp")
	c.Flags().BoolVar(&o.NameOnly, "name-only", false, "chỉ liệt kê tên tệp")
	c.Flags().BoolVar(&o.All, "all", false, "gồm cả tệp không thuộc tác vụ (mặc định bị loại)")
	return c
}

func isolationReason(t *task.Task) string {
	if c := t.State.Classification; c != nil && c.IsolationReason != "" {
		return c.IsolationReason
	}
	return ""
}

// taskDriver là driver của tác vụ (process khi phiếu cũ chưa ghi).
func taskDriver(t *task.Task) string {
	if c := t.State.Classification; c != nil {
		return c.EffectiveDriver()
	}
	return task.DriverProcess
}

func driverReason(t *task.Task) string {
	if c := t.State.Classification; c != nil {
		return c.DriverReason
	}
	return ""
}

// printDriverLines in driver của tác vụ (chỉ khi khác mặc định process hoặc có bước dở) và bước đang chạy bằng subagent.
func printDriverLines(w io.Writer, indent string, t *task.Task) {
	mode := taskDriver(t)
	if ps := t.State.PendingStep; ps != nil {
		fmt.Fprintf(w, "%sDriver:      %s — bước %s đang chạy bằng subagent (lượt %s, hết hạn %s): bint run complete %s --run %s | bint run abort %s\n",
			indent, mode, ps.Step, ps.RunID, fmtRFC(ps.LeaseUntil), t.State.ID, ps.RunID, t.State.ID)
		return
	}
	if mode == task.DriverSession {
		fmt.Fprintf(w, "%sDriver:      session (%s) — bint run next %s --json\n", indent, orDash(driverReason(t)), t.State.ID)
	}
}

func isolationTasks(t *task.Task) []string {
	if c := t.State.Classification; c != nil {
		return c.IsolationTasks
	}
	return nil
}

// printIsolationLine in một dòng chế độ cô lập lúc lập phiếu (task new).
func printIsolationLine(w io.Writer, prefix string, t *task.Task) {
	mode := t.State.Isolation()
	reason := isolationReason(t)
	if reason == "" {
		reason = "phiếu lập trước khi có chế độ cô lập"
	}
	fmt.Fprintf(w, "%s%s (%s)\n", prefix, mode, reason)
	if ids := isolationTasks(t); len(ids) > 0 {
		fmt.Fprintf(w, "%s  vì cây này đang có tác vụ chưa kết thúc: %s (huỷ tác vụ bỏ dở bằng bint task cancel <id>)\n", prefix, strings.Join(ids, ", "))
	}
	if mode == task.IsolationWorktree {
		fmt.Fprintf(w, "%s  worktree riêng được tạo ở lần chạy đầu (bint run); thay đổi về cây này bằng bint task land %s\n", prefix, t.State.ID)
	}
}

// printIsolationLines in chế độ cô lập, nơi chạy và trạng thái đưa về (task show, run status).
func printIsolationLines(w io.Writer, indent string, t *task.Task) {
	st := t.State
	printIsolationLine(w, indent+"Cô lập:     ", t)
	if st.Origin == "" {
		return
	}
	fmt.Fprintf(w, "%sWorktree:    %s   nhánh %s\n%sCây gốc:     %s\n", indent, st.Worktree, orDash(st.Branch), indent, st.Origin)
	switch l := st.Landed; {
	case l == nil && st.AwaitingLand:
		fmt.Fprintf(w, "%sĐưa về:      chưa (đã qua kiểm chứng, chờ đưa về: bint task land %s hoặc %s)\n", indent, st.ID, st.ResumeCommand())
	case l == nil:
		fmt.Fprintf(w, "%sĐưa về:      chưa (bint task land %s)\n", indent, st.ID)
	default:
		fmt.Fprintf(w, "%sĐưa về:      đã đưa %d tệp lúc %s bởi %s", indent, len(l.Files), fmtRFC(l.At), l.By)
		if l.ThreeWay {
			fmt.Fprint(w, " (--3way)")
		}
		if l.Kept {
			fmt.Fprintf(w, "; worktree còn được giữ lại")
		}
		fmt.Fprintln(w)
		if len(l.Conflicts) > 0 {
			fmt.Fprintf(w, "%s             còn dấu xung đột, cần gỡ tay: %s\n", indent, strings.Join(l.Conflicts, ", "))
		}
		if l.Note != "" {
			fmt.Fprintf(w, "%s             ! %s\n", indent, l.Note)
		}
	}
}

func newTaskLandCmd(stdio IO) *cobra.Command {
	var o app.TaskLandOptions
	var asJSON bool
	c := &cobra.Command{
		Use:   "land <task-id>",
		Short: "Đưa thay đổi của tác vụ worktree về cây gốc (dưới dạng chưa commit)",
		Long: `Tác vụ chạy trong worktree riêng (bint task new --worktree, hoặc auto khi cây đang có tác vụ khác) giữ thay
đổi ở worktree. land đưa chúng về cây đã lập phiếu: bản vá nhị phân từ base tới ảnh chụp worktree (chỉ tệp của
tác vụ), áp lên cây gốc KHÔNG qua chỉ mục, nên thay đổi hiện ra như chưa commit, HEAD và chỉ mục của bạn không đổi.

Chỉ đưa về khi điều kiện done (trừ chính việc đưa về) đã đủ: kiểm chứng đã qua trên worktree, không có thay đổi
chưa duyệt. Lấy khoá độc quyền của cây gốc (chờ bước khác đang chạy ở đó xong). Không áp sạch thì cây gốc KHÔNG bị
đụng: lỗi liệt kê tệp xung đột (tác vụ do bint run điều phối chuyển blocked "xung đột khi đưa về"). Thành công thì gỡ
worktree, nhánh bint/<id>, ref base và tệp khoá.

  --3way   chỉ người: tệp xung đột được trộn bằng git merge-file và để lại dấu xung đột trong tệp ở cây gốc để
           bạn gỡ (không dùng git apply --3way vì lệnh đó ngầm ghi chỉ mục)
  --keep   giữ worktree sau khi đưa về (để xem lại)

land không đổi trạng thái tác vụ: sau đó bint run resume <id> hoặc bint task status <id> done.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			o.Ref = args[0]
			if err := o.Validate(); err != nil {
				return err
			}
			ws, err := openTasks(cmd)
			if err != nil {
				return err
			}
			ws.Env.Out = stdio.Out
			res, err := app.LandTask(cmd.Context(), ws, o)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(stdio.Out, map[string]any{"task_id": res.Task.State.ID, "files": nonNil(res.Files),
					"conflicts": nonNil(res.Conflicts), "worktree_removed": res.Removed, "notes": nonNil(res.Notes),
					"landed": res.Task.State.Landed})
			}
			w := stdio.Out
			fmt.Fprintf(w, "✓ Đã đưa %d tệp về cây gốc %s\n", len(res.Files), res.Task.State.Origin)
			for _, f := range res.Files {
				fmt.Fprintf(w, "  %s\n", f)
			}
			if len(res.Conflicts) > 0 {
				fmt.Fprintf(w, "! còn dấu xung đột (<<<<<<<) hoặc chưa trộn được, gỡ tay: %s\n", strings.Join(res.Conflicts, ", "))
			}
			switch {
			case res.Removed:
				fmt.Fprintln(w, "  đã gỡ worktree, nhánh bint/"+res.Task.State.ID+" và ref base")
			case o.Keep:
				fmt.Fprintf(w, "  giữ worktree: %s\n", res.Task.State.Worktree)
			}
			for _, n := range res.Notes {
				fmt.Fprintf(w, "! %s\n", n)
			}
			if !task.Terminal(res.Task.State.Status) {
				fmt.Fprintf(w, "Tiếp: bint run resume %s (hoặc bint task status %s done)\n", res.Task.State.ID, res.Task.State.ID)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&o.ThreeWay, "3way", false, "chỉ người: trộn tệp xung đột và để lại dấu xung đột ở cây gốc (không đụng chỉ mục)")
	c.Flags().BoolVar(&o.Keep, "keep", false, "giữ worktree sau khi đưa về")
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func newTaskCancelCmd(stdio IO) *cobra.Command {
	var o app.TaskCancelOptions
	c := &cobra.Command{
		Use:   "cancel <task-id>",
		Short: "Huỷ tác vụ (tác vụ worktree: gỡ worktree, nhánh, ref base, tệp khoá)",
		Long: `Chuyển tác vụ sang cancelled (cùng đường với bint task status <id> cancelled). Tác vụ chạy trong worktree riêng thì
gỡ worktree, nhánh bint/<id>, ref base và tệp khoá. Worktree còn thay đổi chưa đưa về cây gốc thì huỷ sẽ mất chúng: cần
--discard, chỉ người dùng (không chạy được từ trong phiên agent).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			o.Ref = args[0]
			if err := o.Validate(); err != nil {
				return err
			}
			ws, err := openTasks(cmd)
			if err != nil {
				return err
			}
			t, err := app.CancelTask(cmd.Context(), ws, o)
			if err != nil {
				return err
			}
			fmt.Fprintf(stdio.Out, "%s → %s\n", t.State.ID, t.State.Status)
			return nil
		},
	}
	c.Flags().StringVarP(&o.Reason, "reason", "m", "", "lý do")
	c.Flags().BoolVar(&o.Discard, "discard", false, "chỉ người: huỷ dù worktree còn thay đổi chưa đưa về (sẽ mất)")
	return c
}
