package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"bint/internal/app"
	"bint/internal/config"
	"bint/internal/task"
	"bint/internal/workflow"
)

type runFlags struct {
	taskRef      string
	depth        string
	plan, noPlan bool
	noReview     bool
	agentCommand string
	model        string
	harness      string
	roles        []string
	asJSON       bool
	ef           editFlags
}

func newRunCmd(stdio IO) *cobra.Command {
	var rf runFlags
	c := &cobra.Command{
		Use:   "run <workflow> [mô tả]",
		Short: "Thực thi workflow b-do, b-req, b-feat, b-review (Bint Core điều phối)",
		Long: `Bint Core điều phối workflow theo chương trình (adapter cấp 3): gọi agent cho từng bước
với nội dung skill gốc + lớp phủ, ghi lượt chạy (cha b-do, con từng bước, liên kết bằng
task ID) và áp giới hạn vận hành ở tầng chương trình (mục 5):

  b-do      Classify → b-req (nếu cần) → b-feat → b-review → Finalize
  b-req     chỉ lập kế hoạch      b-feat   chỉ thực thi      b-review   chỉ review

Độ sâu của b-req (--depth auto|standard|deep, chỉ b-do/b-req): auto chọn một lần lúc tạo tác vụ —
deep khi phạm vi chạm đường dẫn bảo vệ hoặc yêu cầu dài/nhiều ý (không đổi mức rủi ro quyền ghi),
còn lại standard. --depth standard|deep là ghi đè của người dùng, hàm ý có b-req và không đi cùng
--no-plan; chỉ nhận khi tác vụ còn created (hoặc b-req --task trên tác vụ planned để lập lại).
b-req còn câu hỏi mở (## Câu hỏi mở) thì b-feat bị chặn cho tới khi ghi câu trả lời.

Giới hạn:
  - limits.workflows.<wf>.max_cost_usd / max_turns (mặc định limits.per_run); với b-do là
    tổng của mọi bước. Agent không báo chi phí thì giới hạn chi phí chỉ là chỉ dẫn.
  - Tệp đã sửa chạm limits.protected_paths hoặc scope.deny (và ngoài scope.allow khi
    autonomy ask_always) cần người duyệt: có terminal thì hỏi, không thì tác vụ chuyển blocked.
  - workflow.max_review_rounds: số vòng review → sửa tối đa.
  - Lệnh kiểm chứng chỉ chạy khi người đã xác nhận (bint task approve).
  - Hoàn thành (done) chỉ khi có bằng chứng; thiếu thì blocked kèm lý do.

Ctrl-C: agent bị dừng, tác vụ chuyển interrupted; tiếp tục bằng bint run resume <task-id>.

Mã thoát: 0 nếu workflow kết thúc bình thường (done, hoặc bước đơn xong); 1 nếu tác vụ
blocked / failed / interrupted.`,
		Example: `  bint run b-do "Sửa lỗi database is locked khi chạy kiểm thử song song trong internal/db"
  bint run b-req --task 01J…   # chỉ lập kế hoạch cho tác vụ có sẵn
  bint run b-do --depth deep "Đổi cách tính phí trong internal/billing"   # phân tích sâu hơn ở b-req
  bint run b-do --agent-command ./my-agent.sh "…"
  bint run b-do --role implementer=codex "…"   # b-feat bằng Codex, các bước khác theo roles
  bint run b-review --harness antigravity --task 01J…`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return usageError{errors.New("thiếu workflow (b-do | b-req | b-feat | b-review)")}
			}
			return runWorkflow(cmd, stdio, rf, func(o *app.RunWorkflowOptions) {
				o.Workflow, o.Request, o.Task, o.NoReview = args[0], strings.Join(args[1:], " "), rf.taskRef, rf.noReview
				o.Plan, o.NoPlan, o.Depth = rf.plan, rf.noPlan, rf.depth
				o.Goal, o.VerifyCommand, o.Autonomy, o.Criteria, o.Allow, o.Deny = rf.ef.goal, rf.ef.verify, rf.ef.autonomy, rf.ef.criteria, rf.ef.allow, rf.ef.deny
				o.Constraints, o.ClearScope = rf.ef.constraints, rf.ef.clearScope
				o.GoalSet, o.VerifyCommandSet, o.AutonomySet = rf.ef.changed(cmd)
			}, nil)
		},
	}
	addRunFlags(c, &rf, true)
	c.AddCommand(newRunStatusCmd(stdio), newRunResumeCmd(stdio))
	return c
}

func addRunFlags(c *cobra.Command, rf *runFlags, full bool) {
	f := c.Flags()
	f.BoolVar(&rf.noReview, "no-review", false, "bỏ bước b-review (b-do)")
	f.StringVar(&rf.agentCommand, "agent-command", "", "lệnh tác nhân tuỳ chỉnh cho mọi bước (đề bài qua stdin và $BINT_EVAL_PROMPT_FILE)")
	f.StringVar(&rf.model, "model", "", "mô hình cho mọi bước (ghi đè harnesses.<h>.models.<role>, workflow.agent.model, harnesses.<h>.model)")
	f.StringVar(&rf.harness, "harness", "", "harness cho mọi bước của lượt này (claude | codex | antigravity; phải có trong harnesses.enabled)")
	f.StringArrayVar(&rf.roles, "role", nil, "harness cho một role, dạng role=harness (planner|implementer|reviewer hoặc b-req|b-feat|b-review); lặp lại được")
	f.BoolVar(&rf.asJSON, "json", false, "in kết quả dạng JSON")
	if full {
		f.StringVar(&rf.taskRef, "task", "", "chạy trên tác vụ có sẵn")
		f.BoolVar(&rf.plan, "plan", false, "luôn qua b-req")
		f.BoolVar(&rf.noPlan, "no-plan", false, "bỏ qua b-req")
		f.StringVar(&rf.depth, "depth", "auto", depthFlagHelp)
		addEditFlags(c, &rf.ef, false)
	}
}

// runWorkflow chạy `bint run` hoặc `bint run resume` qua dịch vụ: fillRun điền Options của `run`; resume != nil
// thì tiếp tục tác vụ đó. Dùng chung phần chọn harness, câu hỏi tương tác, in kết quả và mã thoát.
func runWorkflow(cmd *cobra.Command, stdio IO, rf runFlags, fillRun func(*app.RunWorkflowOptions), resume *string) error {
	ctx := cmd.Context()
	// Điền Options và kiểm tra trước khi mở workspace: lỗi dùng sai (mã 2) thắng lỗi "chưa init", như trước.
	var (
		ro app.RunResumeOptions
		o  app.RunWorkflowOptions
	)
	if resume != nil {
		ro = app.RunResumeDefaults(config.Default())
		ro.Ref, ro.NoReview, ro.Harness, ro.Roles, ro.Model, ro.AgentCommand = *resume, rf.noReview, rf.harness, rf.roles, rf.model, rf.agentCommand
		if err := ro.Validate(); err != nil {
			return err
		}
	} else {
		o = app.RunWorkflowDefaults(config.Default())
		fillRun(&o)
		o.Harness, o.Roles, o.Model, o.AgentCommand = rf.harness, rf.roles, rf.model, rf.agentCommand
		if err := o.Validate(); err != nil {
			return err
		}
	}
	ws, err := openTasks(cmd)
	if err != nil {
		return err
	}
	cb := app.RunCallbacks{}
	if !rf.asJSON {
		cb.Log = func(line string) { fmt.Fprintln(stdio.Out, line) }
	}
	// Chỉ hỏi người khi có terminal và không chạy trong phiên agent.
	if ws.Human && isTerminal(stdio.In) {
		cb.Confirm = func(q workflow.Question) bool { return confirm(stdio, q.Text) }
		cb.Ask = func(q workflow.Question) string {
			fmt.Fprintf(stdio.Out, "%s\n> ", q.Text)
			line, _ := bufio.NewReader(stdio.In).ReadString('\n')
			return strings.TrimSpace(line)
		}
	}
	var res *workflow.Result
	if resume != nil {
		ro.RunCallbacks = cb
		res, err = app.ResumeWorkflow(ctx, ws, ro)
	} else {
		o.RunCallbacks = cb
		res, err = app.RunWorkflow(ctx, ws, o)
	}
	if err != nil {
		return err
	}
	if rf.asJSON {
		if err := writeJSON(stdio.Out, res); err != nil {
			return err
		}
	} else {
		printRunResult(stdio.Out, res)
	}
	switch res.Status {
	case task.StatusBlocked, task.StatusFailed, task.StatusInterrupted:
		return withCode(ExitError, fmt.Errorf("tác vụ %s: %s", res.TaskID, res.Status))
	case task.StatusPlanned:
		// b-req chạy riêng dừng ở planned khi còn lỗi cấu trúc artifact hoặc câu hỏi mở: không phải thành công.
		if len(res.PlanningIssues) > 0 || len(res.Questions) > 0 {
			return withCode(ExitError, fmt.Errorf("tác vụ %s: planned nhưng chưa sẵn sàng cho b-feat (bint task lint %s)", res.TaskID, res.TaskID))
		}
	}
	return nil
}

func printRunResult(w io.Writer, res *workflow.Result) {
	fmt.Fprintf(w, "\nTác vụ %s: %s", res.TaskID, res.Status)
	if res.Reason != "" {
		fmt.Fprintf(w, " — %s", res.Reason)
	}
	fmt.Fprintf(w, "\nLượt chạy: %s", res.RunID)
	if res.CostUSD != nil {
		fmt.Fprintf(w, "   chi phí: %.4f USD", *res.CostUSD)
	}
	fmt.Fprintln(w)
	if len(res.Steps) > 0 {
		tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "  BƯỚC\tAGENT\tLƯỢT\tKẾT QUẢ\tTHỜI GIAN\tCHI PHÍ")
		for _, s := range res.Steps {
			cost := "-"
			if s.CostUSD != nil {
				cost = fmt.Sprintf("%.4f", *s.CostUSD)
			}
			who := s.Harness
			if who == "" {
				who = "-"
			}
			if s.Model != "" {
				who += "/" + s.Model
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\t%s\n", s.Step, who, s.RunID, s.Status, s.Duration, cost)
		}
		tw.Flush()
	}
	for _, i := range res.PlanningIssues {
		fmt.Fprintf(w, "✗ %s\n", i)
	}
	for _, q := range res.Questions {
		fmt.Fprintf(w, "? %s\n", q)
	}
	for _, n := range res.Notes {
		fmt.Fprintf(w, "! %s\n", n)
	}
	fmt.Fprintf(w, "Chi tiết: bint task show %s   ·   bint run status %s\n", res.TaskID, res.TaskID)
}

func newRunResumeCmd(stdio IO) *cobra.Command {
	var rf runFlags
	c := &cobra.Command{
		Use:   "resume <task-id>",
		Short: "Tiếp tục tác vụ blocked / interrupted / failed từ bước đang dở",
		Long: `Tiếp tục điều phối tác vụ (workflow đã tạo nó) từ trạng thái trước khi dừng. Lượt mới có
parent_run_id là lượt trước. Thay đổi đang chờ duyệt phải được duyệt trước (bint task approve).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflow(cmd, stdio, rf, nil, &args[0])
		},
	}
	addRunFlags(c, &rf, false)
	return c
}

func newRunStatusCmd(stdio IO) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "status <task-id | run-id>",
		Short: "Trạng thái tác vụ và các lượt chạy gắn với nó",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := openTasks(cmd)
			if err != nil {
				return err
			}
			tr, err := app.TaskRuns(cmd.Context(), ws, app.TaskRunsOptions{Ref: args[0]})
			if err != nil {
				return err
			}
			t, rows := tr.Task, tr.Runs
			if asJSON {
				return writeJSON(stdio.Out, map[string]any{"task_id": t.State.ID, "status": t.State.Status, "reason": t.State.Reason,
					"resume_from": t.State.ResumeFrom, "runs": rows})
			}
			w := stdio.Out
			fmt.Fprintf(w, "Tác vụ %s: %s", t.State.ID, t.State.Status)
			if t.State.Reason != "" {
				fmt.Fprintf(w, " — %s", t.State.Reason)
			}
			fmt.Fprintf(w, "\n  %s\n", t.Ticket.Goal)
			if len(rows) == 0 {
				fmt.Fprintln(w, "\nChưa có lượt chạy nào được ghi nhận cho tác vụ này.")
				return nil
			}
			fmt.Fprintln(w)
			tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "RUN ID\tWORKFLOW\tCHA\tTRẠNG THÁI\tBẮT ĐẦU\tTHỜI LƯỢNG\tCHI PHÍ\tKIỂM CHỨNG")
			for _, r := range rows {
				cost := "-"
				if r.CostUSD != nil {
					cost = fmt.Sprintf("%.4f", *r.CostUSD)
				}
				parent := "-"
				if r.Parent != "" {
					parent = r.Parent[len(r.Parent)-6:]
				}
				fmt.Fprintf(tw, "%s\t%s\t…%s\t%s\t%s\t%s\t%s\t%s\n", r.RunID, orDash(r.Workflow), parent, r.Status,
					fmtTime(r.Started), r.Duration, cost, orDash(r.Verify))
			}
			tw.Flush()
			if task.Paused(t.State.Status) {
				fmt.Fprintf(w, "\nTiếp tục: bint run resume %s\n", t.State.ID)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}
