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
	"bint/internal/task"
	"bint/internal/workflow"
)

// openWorkflow mở môi trường thao tác tác vụ (cần `bint init` trên máy này).
func openWorkflow(cmd *cobra.Command) (*workflow.Env, *app.KnowledgeWS, error) {
	k, err := openKnowledge(cmd)
	if err != nil {
		return nil, nil, err
	}
	if !k.Paths.Initialized() {
		return nil, nil, app.ErrNotInitialized
	}
	inAgent, _ := app.InAgentSession()
	env := &workflow.Env{
		Root: k.Repo.Toplevel, ConfigRoot: k.Root, Cfg: k.Config, Tasks: task.NewStore(k.Paths.Runtime),
		Spool: k.Paths.Spool, Version: Version, Human: !inAgent, Actor: "agent",
		RunID: strings.TrimSpace(os.Getenv(workflow.EnvRunID)), Now: time.Now,
	}
	if env.Human {
		env.Actor = app.GitIdentity(cmd.Context(), k.Repo.Toplevel)
	}
	return env, k, nil
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

func (e *editFlags) edit(c *cobra.Command) workflow.TicketEdit {
	f := c.Flags()
	ed := workflow.TicketEdit{Criteria: e.criteria, Allow: e.allow, Deny: e.deny, Constraints: e.constraints,
		ClearCriteria: e.clearCriteria, ClearScope: e.clearScope}
	if f.Changed("goal") {
		ed.Goal = &e.goal
	}
	if f.Changed("verify-command") {
		ed.VerifyCommand = &e.verify
	}
	if f.Changed("autonomy") {
		ed.Autonomy = &e.autonomy
	}
	return ed
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
		newTaskLintCmd(stdio))
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
--clear-scope --allow. Agent không bỏ được b-req (--no-plan) khi tác vụ chạm đường dẫn bảo vệ.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if plan && noPlan {
				return usageError{errors.New("--plan và --no-plan loại trừ nhau")}
			}
			env, _, err := openWorkflow(cmd)
			if err != nil {
				return err
			}
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
			if strings.TrimSpace(req) == "" {
				return usageError{errors.New("thiếu mô tả tác vụ")}
			}
			o := workflow.NewTaskOptions{Request: req, Workflow: wf, Edit: ef.edit(cmd)}
			if plan || noPlan {
				o.Plan = ptr(plan)
			}
			if o.Depth, err = depthOption(cmd, depth, noPlan, wfOrDefault(wf)); err != nil {
				return err
			}
			if o.Edit.Autonomy != nil && *o.Edit.Autonomy == task.AutonomyAuto && !env.Human {
				return fmt.Errorf("%w: chỉ người mới đặt được autonomy: auto", workflow.ErrAgent)
			}
			t, a, err := env.NewTask(cmd.Context(), o)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(stdio.Out, map[string]any{"id": t.State.ID, "dir": t.Dir, "status": t.State.Status,
					"ticket": t.Ticket, "classification": t.State.Classification, "assessment": a,
					"verify_confirmed": env.VerifyConfirmed(t)})
			}
			w := stdio.Out
			fmt.Fprintf(w, "Tác vụ %s: %s\n", t.State.ID, t.Ticket.Goal)
			fmt.Fprintf(w, "  Thư mục:   %s\n", t.Dir)
			if c := t.State.Classification; c != nil {
				fmt.Fprintf(w, "  Classify:  %s\n", c.Reason)
				printDepthLine(w, "  Phân tích: ", c)
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
			env, _, err := openWorkflow(cmd)
			if err != nil {
				return err
			}
			ts, bad := env.Tasks.List()
			type row struct {
				ID       string `json:"id"`
				Status   string `json:"status"`
				Workflow string `json:"workflow"`
				Updated  string `json:"updated_at"`
				Goal     string `json:"goal"`
				Reason   string `json:"reason,omitempty"`
			}
			rows := []row{}
			for _, t := range ts {
				t = env.Reconcile(t)
				if !all && task.Terminal(t.State.Status) {
					continue
				}
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
			env, _, err := openWorkflow(cmd)
			if err != nil {
				return err
			}
			t, err := env.Tasks.Load(args[0])
			if err != nil {
				return err
			}
			t = env.Reconcile(t)
			a := env.Assess(t)
			rev, err := t.LoadReview()
			if err != nil {
				return err
			}
			blockers := task.DoneBlockers(t, task.DoneInput{Worktree: task.WorktreeFingerprint(cmd.Context(), env.Root),
				VerifyConfirmed: env.VerifyConfirmed(t)})
			if asJSON {
				arts := map[string]string{}
				for _, n := range task.ArtifactNames() {
					if _, err := os.Stat(t.ArtifactPath(n)); err == nil {
						arts[n] = t.ArtifactPath(n)
					}
				}
				return writeJSON(stdio.Out, map[string]any{"dir": t.Dir, "ticket": t.Ticket, "state": t.State, "classification": t.State.Classification, "assessment": a,
					"verify_confirmed": env.VerifyConfirmed(t), "artifacts": arts, "review": rev, "done_blockers": nonNil(blockers)})
			}
			printTask(stdio.Out, env, t, a, rev, blockers)
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
	if c := st.Classification; c != nil {
		fmt.Fprintf(w, "  Classify:   %s\n", c.Reason)
		printDepthLine(w, "  Phân tích:  ", c)
	}
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
			env, _, err := openWorkflow(cmd)
			if err != nil {
				return err
			}
			t, a, err := env.SetTicket(args[0], ef.edit(cmd))
			if err != nil {
				return err
			}
			fmt.Fprintf(stdio.Out, "Đã cập nhật %s: %s\n", t.State.ID, t.Ticket.Goal)
			printTicketBrief(stdio.Out, env, t, a)
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
			env, _, err := openWorkflow(cmd)
			if err != nil {
				return err
			}
			body, err := readInput(stdio, args, 2)
			if err != nil {
				return err
			}
			_, p, err := env.WriteArtifact(args[0], args[1], body)
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
			env, _, err := openWorkflow(cmd)
			if err != nil {
				return err
			}
			t, blockers, err := env.SetStatus(cmd.Context(), args[0], args[1], workflow.TransitionOptions{Reason: reason, Force: force})
			if err != nil {
				return err
			}
			fmt.Fprintf(stdio.Out, "%s → %s\n", t.State.ID, t.State.Status)
			if force && len(blockers) > 0 {
				fmt.Fprintf(stdio.Out, "! xác nhận dù thiếu bằng chứng: %s\n", strings.Join(blockers, "; "))
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
			env, _, err := openWorkflow(cmd)
			if err != nil {
				return err
			}
			t, res, err := env.Verify(cmd.Context(), args[0])
			if err != nil {
				return err
			}
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
			if v != "met" && v != "unmet" {
				return usageError{errors.New("kết quả phải là met hoặc unmet")}
			}
			env, _, err := openWorkflow(cmd)
			if err != nil {
				return err
			}
			t, err := env.CheckCriterion(args[0], n, v == "met", evidence)
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
			env, _, err := openWorkflow(cmd)
			if err != nil {
				return err
			}
			if _, err := env.RecordError(args[0], step, args[1]); err != nil {
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
			env, _, err := openWorkflow(cmd)
			if err != nil {
				return err
			}
			body, err := readInput(stdio, args, 1)
			if err != nil {
				return err
			}
			t, r, err := env.SubmitReview(args[0], []byte(body))
			if err != nil {
				return err
			}
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
	c := &cobra.Command{
		Use:   "approve <task-id>",
		Short: "Người xác nhận lệnh kiểm chứng và duyệt thay đổi đang chờ (đường dẫn bảo vệ/deny)",
		Long: `Chỉ người dùng (không chạy được từ trong phiên agent):
  - xác nhận verify.command hiện tại của phiếu → Bint được chạy lệnh đó và kết quả là tín hiệu mạnh;
  - duyệt các tệp đang chờ (thay đổi chạm limits.protected_paths, scope.deny, hoặc ngoài
    scope.allow khi autonomy ask_always).
Không đổi trạng thái; tiếp tục bằng bint run resume <task-id>. Xem diff trước khi duyệt.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, _, err := openWorkflow(cmd)
			if err != nil {
				return err
			}
			if !env.Human {
				_, k := app.InAgentSession()
				return fmt.Errorf("%w (`bint task approve`; phát hiện biến %s)", app.ErrAgentForbidden, k)
			}
			t, a, err := env.Approve(args[0], paths...)
			if err != nil {
				return err
			}
			if a.VerifyCommand == "" && len(a.Paths) == 0 {
				fmt.Fprintf(stdio.Out, "%s: không có gì chờ duyệt.\n", t.State.ID)
				return nil
			}
			if a.VerifyCommand != "" {
				fmt.Fprintf(stdio.Out, "✓ Xác nhận lệnh kiểm chứng: %s\n", a.VerifyCommand)
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
			for _, r := range task.TransitionTable() {
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
			env, _, err := openWorkflow(cmd)
			if err != nil {
				return err
			}
			t, err := env.Tasks.Load(args[0])
			if err != nil {
				return err
			}
			v1 := t.State.PlanningContractVersion >= task.PlanningContractV1
			rep := task.LintPlanningTask(t, strict || v1)
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

const depthFlagHelp = "độ sâu phân tích của b-req: auto | standard | deep (chỉ b-do/b-req; standard|deep hàm ý có b-req)"

func wfOrDefault(wf string) string {
	if wf == "" {
		return workflow.StepDo
	}
	return wf
}

// depthOption kiểm cờ --depth. Chỉ xét xung đột khi người dùng đặt tường minh; `auto` không đổi
// nghĩa --plan/--no-plan. Trả "" khi tự chọn.
func depthOption(cmd *cobra.Command, value string, noPlan bool, wf string) (string, error) {
	if !cmd.Flags().Changed("depth") {
		return "", nil
	}
	d, err := task.NormalizeDepth(value)
	if err != nil {
		return "", usageError{err}
	}
	if d == "" {
		return "", nil
	}
	if noPlan {
		return "", usageError{errors.New("--depth standard|deep hàm ý có b-req: không đi cùng --no-plan")}
	}
	if wf != workflow.StepDo && wf != workflow.StepReq {
		return "", usageError{fmt.Errorf("--depth chỉ dùng với workflow b-do hoặc b-req (không dùng với %s)", wf)}
	}
	return d, nil
}

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
