package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"bint/internal/app"
	"bint/internal/ctxengine"
	"bint/internal/harness"
	"bint/internal/runlog"
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
			o := workflow.RunOptions{Workflow: args[0], Request: strings.Join(args[1:], " "), TaskRef: rf.taskRef,
				NoReview: rf.noReview, Edit: rf.ef.edit(cmd)}
			if rf.plan && rf.noPlan {
				return usageError{errors.New("--plan và --no-plan loại trừ nhau")}
			}
			if rf.plan || rf.noPlan {
				o.Plan = ptr(rf.plan)
			}
			depth, err := depthOption(cmd, rf.depth, rf.noPlan, args[0])
			if err != nil {
				return err
			}
			o.Depth = depth
			return runWorkflow(cmd, stdio, rf, o)
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
	f.StringVar(&rf.model, "model", "", "mô hình (ghi đè workflow.agent.model / harnesses.<h>.model)")
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

func runWorkflow(cmd *cobra.Command, stdio IO, rf runFlags, o workflow.RunOptions) error {
	ctx := cmd.Context()
	env, k, err := openWorkflow(cmd)
	if err != nil {
		return err
	}
	if env.RunID != "" {
		return fmt.Errorf("không chạy `bint run` lồng trong một bước workflow khác (BINT_RUN_ID=%s)", env.RunID)
	}
	sel := harness.Selection{All: rf.harness}
	if rf.harness != "" {
		if _, ok := harness.Get(rf.harness); !ok {
			return usageError{fmt.Errorf("--harness: harness không hỗ trợ %q (%s)", rf.harness, strings.Join(harness.Names(), ", "))}
		}
	}
	if sel.Roles, err = harness.ParseRoleOverrides(rf.roles); err != nil {
		return usageError{err}
	}
	if rf.agentCommand != "" && (rf.harness != "" || len(rf.roles) > 0) {
		return usageError{errors.New("--agent-command không đi cùng --harness/--role")}
	}
	r := &workflow.Runner{Env: env, Agent: workflow.HarnessAgents(k.Config, rf.agentCommand, rf.model, sel)}
	if !rf.asJSON {
		r.Logf = func(format string, a ...any) { fmt.Fprintf(stdio.Out, format+"\n", a...) }
	}
	// Chỉ hỏi người khi có terminal và không chạy trong phiên agent.
	if env.Human && isTerminal(stdio.In) {
		r.Confirm = func(q string) bool { return confirm(stdio, q) }
		r.Ask = func(q string) string {
			fmt.Fprintf(stdio.Out, "%s\n> ", q)
			line, _ := bufio.NewReader(stdio.In).ReadString('\n')
			return strings.TrimSpace(line)
		}
	}
	if k.Config.Context.Interfaces.CLI {
		r.Context = func(ctx context.Context, goal string, paths []string) (ctxengine.Pack, error) {
			c, err := app.OpenContext(ctx, k.Root)
			if err != nil {
				return ctxengine.Pack{}, err
			}
			defer c.Close()
			return c.Engine.ForTask(ctx, goal, paths)
		}
	}
	res, err := r.Run(ctx, o)
	if err != nil {
		return err
	}
	// Nạp sự kiện ngay để `bint runs` thấy lượt vừa chạy.
	if s, err := k.OpenStore(context.Background()); err == nil {
		k.Ingest(context.Background(), s, time.Now())
		s.Close()
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
		fmt.Fprintln(tw, "  BƯỚC\tLƯỢT\tKẾT QUẢ\tTHỜI GIAN\tCHI PHÍ")
		for _, s := range res.Steps {
			cost := "-"
			if s.CostUSD != nil {
				cost = fmt.Sprintf("%.4f", *s.CostUSD)
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", s.Step, s.RunID, s.Status, s.Duration, cost)
		}
		tw.Flush()
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
			env, _, err := openWorkflow(cmd)
			if err != nil {
				return err
			}
			t, err := env.Tasks.Load(args[0])
			if err != nil {
				return err
			}
			t = env.Reconcile(t)
			if len(t.State.PendingPaths) > 0 {
				return fmt.Errorf("tác vụ %s có thay đổi chờ người duyệt: %s (bint task approve %s)", t.State.ID,
					strings.Join(t.State.PendingPaths, ", "), t.State.ID)
			}
			wf := t.State.Workflow
			if wf == "" {
				wf = workflow.StepDo
			}
			o := workflow.RunOptions{Workflow: wf, TaskRef: t.State.ID, Resume: true, NoReview: rf.noReview}
			return runWorkflow(cmd, stdio, rf, o)
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
			ctx := cmd.Context()
			env, _, err := openWorkflow(cmd)
			if err != nil {
				return err
			}
			_, s, _, err := openAndIngest(cmd)
			if err != nil {
				return err
			}
			defer s.Close()
			t, terr := env.Tasks.Load(args[0])
			if terr != nil {
				r, err := runlog.GetRun(ctx, s.DB, args[0])
				if err != nil {
					return fmt.Errorf("không tìm thấy tác vụ hay lượt chạy %s", args[0])
				}
				m, err := runlog.GetRunMeta(ctx, s.DB, r.RunID)
				if err != nil || !m.TaskID.Valid {
					return fmt.Errorf("lượt %s không gắn với tác vụ nào (bint runs show %s)", r.RunID, r.RunID)
				}
				if t, err = env.Tasks.Load(m.TaskID.String); err != nil {
					return err
				}
			}
			t = env.Reconcile(t)
			runs, err := runlog.RunsForTask(ctx, s.DB, t.State.ID)
			if err != nil {
				return err
			}
			type runRow struct {
				RunID    string   `json:"run_id"`
				Workflow string   `json:"workflow"`
				Parent   string   `json:"parent_run_id,omitempty"`
				Status   string   `json:"status"`
				Started  string   `json:"started_at"`
				Duration string   `json:"duration"`
				CostUSD  *float64 `json:"cost_usd"`
				Verify   string   `json:"verification_status,omitempty"`
				Reason   string   `json:"end_reason,omitempty"`
			}
			rows := []runRow{}
			for _, r := range runs {
				m, _ := runlog.GetRunMeta(ctx, s.DB, r.RunID)
				row := runRow{RunID: r.RunID, Workflow: r.Workflow.String, Parent: m.ParentRunID.String, Status: r.Status,
					Started: r.StartedAt.String, Duration: fmtDuration(r), Verify: m.VerificationStatus.String, Reason: r.EndReason.String}
				if m.CostUSD.Valid {
					c := m.CostUSD.Float64
					row.CostUSD = &c
				}
				rows = append(rows, row)
			}
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
