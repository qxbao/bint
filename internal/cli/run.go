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
	answer       string
	slice        string
	accept       bool
	fixRound     bool
	dropGoal     bool
	taskRef      string
	step         string
	depth        string
	plan, noPlan bool
	fast         bool
	variant      string
	noReview     bool
	noWait       bool
	rerun        bool
	fresh        bool
	worktree     bool
	shared       bool
	driver       string
	agentCommand string
	model        string
	harness      string
	roles        []string
	asJSON       bool
	ef           editFlags
}

// freshFlagHelp: --fresh của run <workflow> --task và run resume (driver process).
const freshFlagHelp = "bỏ nối tiếp phiên của bước dừng gần nhất (hết hạn mức, quá giờ, Ctrl-C, chờ câu trả lời): bước chạy mới với đề bài đầy đủ"

func newRunCmd(stdio IO) *cobra.Command {
	var rf runFlags
	c := &cobra.Command{
		Use:   "run <workflow> [mô tả]",
		Short: "Thực thi workflow b-do, b-req, b-feat, b-review, b-debug hoặc flow tuỳ biến (Bint Core điều phối)",
		Long: `Bint Core điều phối workflow theo chương trình (adapter cấp 3): gọi agent cho từng bước
với nội dung skill gốc + lớp phủ, ghi lượt chạy (cha b-do, con từng bước, liên kết bằng
task ID) và áp giới hạn vận hành ở tầng chương trình (mục 5):

  b-do      Classify → b-req (nếu cần) → b-feat → b-review → Finalize
  b-req     chỉ lập kế hoạch      b-feat   chỉ thực thi      b-review   chỉ review
  b-debug   tái hiện lỗi bằng test hỏng (b-repro, có cổng đỏ) → sửa (b-fix) → b-review; chạy một bước: --step
  <flow>    flow tuỳ biến trong .bint/flows/<flow>.yml (bint flow list): chạy các bước của flow theo thứ tự,
            bước plan/review chỉ đọc, kiểm phạm vi sau mỗi bước implement, kiểm chứng sau bước implement có
            verify và luôn trước review/done. Flow đổi giữa chừng (khác băm lúc lập phiếu) thì tác vụ blocked.

Độ sâu của b-req (--depth auto|standard|deep, chỉ b-do/b-debug/b-req): auto chọn một lần lúc tạo tác vụ —
deep khi phạm vi chạm đường dẫn bảo vệ hoặc yêu cầu dài/nhiều ý (không đổi mức rủi ro quyền ghi),
còn lại standard. --depth standard|deep là ghi đè của người dùng, hàm ý có b-req và không đi cùng
--no-plan; chỉ nhận khi tác vụ còn created (hoặc b-req --task trên tác vụ planned để lập lại).
b-req còn câu hỏi mở (## Câu hỏi mở) thì b-feat bị chặn cho tới khi ghi câu trả lời.

Giới hạn:
  - limits.workflows.<wf>.max_cost_usd / max_turns (mặc định limits.per_run); với b-do là
    tổng của mọi bước. Agent không báo chi phí thì giới hạn chi phí chỉ là chỉ dẫn.
  - Tệp đã sửa chạm limits.protected_paths hoặc scope.deny (và ngoài scope.allow khi
    autonomy ask_always) cần người duyệt: có terminal thì hỏi, không thì tác vụ chuyển blocked.
  - workflow.max_review_rounds: số vòng sửa tối đa sau review (n vòng sửa ⇒ tối đa n+1 vòng review). Review còn đang giảm số phát hiện
    nghiêm trọng thì sửa tiếp ngoài n tới trần 2n (+ vòng --fix-round); không giảm thì dừng ngay. Hết vòng mà còn phát hiện
    nghiêm trọng thì tác vụ blocked và in ba lựa chọn: bint run resume <id> --accept | --fix-round, hoặc bint task cancel <id>.
  - --fast (chỉ b-do, lập phiếu mới hoặc --task trên phiếu created): đường nhanh do người dùng chọn: không b-req, một phiên b-feat,
    kiểm chứng, một vòng b-review (còn phát hiện nghiêm trọng thì một vòng sửa rồi xong, không review lại). Không đi cùng
    --plan/--no-plan/--depth; phạm vi chạm đường dẫn bảo vệ thì vẫn qua b-req. Không có --fast thì Classify quyết như thường: yêu cầu mơ hồ vào b-req, việc nhỏ hay yêu cầu ngắn nêu rõ chỗ sửa vẫn có thể bỏ b-req
    (workflow.fast_path mặc định tắt; bật true thì Bint tự chọn đường nhanh cho yêu cầu ngắn, mơ hồ, rủi ro thấp).
  - Cỡ kế hoạch (b-do): sau b-req, kế hoạch chỉ là "nhỏ" — b-feat chạy ngay, không dừng — khi plan.md ghi Kết luận: nhỏ, không có
    giai đoạn hoãn (G-N), số tệp và số nhóm bề mặt không quá workflow.small_plan_max_files (mặc định 5), requirement.md có
    Kiểm tra mục tiêu nêu tên test và không chạm đường dẫn bảo vệ. Còn lại là "lớn": tác vụ blocked ("kế hoạch lớn: …", có terminal
    thì hỏi) cho người xem kế hoạch rồi bint run resume <id>; resume chạy b-feat, không chạy lại b-req.
  - Bước b-feat không đổi tệp nào (hoặc hỏi người bằng bint task error --question) thì tác vụ blocked chờ người, không kiểm chứng,
    không review, không done; trả lời bằng bint run resume <id> --answer "<nội dung>".
  - Lệnh kiểm chứng chỉ chạy khi người đã xác nhận (bint task approve).
  - Hoàn thành (done) chỉ khi có bằng chứng; thiếu thì blocked kèm lý do.
  - Cô lập: --worktree chạy tác vụ mới trong worktree riêng (song song với tác vụ khác), --shared sửa thẳng trên
    cây đang làm; mặc định theo workflow.isolation (auto: worktree riêng khi cây đang có tác vụ khác chưa kết thúc).
    Chỉ chọn lúc lập phiếu (không đi cùng --task). Tác vụ worktree được đưa về cây gốc ở bước finalize của b-do
    (xung đột ⇒ blocked "xung đột khi đưa về"); chạy từng bước thì dùng bint task land.
  - Driver: --driver session lập phiếu cho phiên điều phối chạy bước bằng subagent rồi dừng (Result created); các bước đi
    qua bint run next | complete | abort. Mặc định (process) là bint run tự gọi harness cho từng bước. Chỉ chọn lúc lập phiếu.
  - Khoá worktree: bước chỉ đọc giữ khoá chia sẻ, b-feat giữ khoá độc quyền; bước đến sau chờ khoá (tối đa
    workflow.timeout_minutes) thay vì dừng ngay, --no-wait để dừng ngay (blocked). Tác vụ luôn chạy ở
    State.Worktree của nó dù đứng ở thư mục khác.

Ctrl-C: agent bị dừng, tác vụ chuyển interrupted; tiếp tục bằng bint run resume <task-id>.

Mã thoát: 0 nếu workflow kết thúc bình thường (done, hoặc bước đơn xong); 1 nếu tác vụ
blocked / failed / interrupted.`,
		Example: `  bint run b-do "Sửa lỗi database is locked khi chạy kiểm thử song song trong internal/db"
  bint run b-req --task 01J…   # chỉ lập kế hoạch cho tác vụ có sẵn
  bint run b-do --depth deep "Đổi cách tính phí trong internal/billing"   # phân tích sâu hơn ở b-req
  bint run b-do --fast "Đổi thông báo lỗi khi thiếu config trong internal/config"   # đường nhanh: không b-req
  bint run b-do --agent-command ./my-agent.sh "…"
  bint run b-do --role implementer=codex "…"   # b-feat bằng Codex, các bước khác theo roles
  bint run b-review --harness antigravity --task 01J…
  bint run tdd "Thêm cờ --json cho bint flow list"   # flow tuỳ biến .bint/flows/tdd.yml`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return usageError{errors.New("thiếu workflow (b-do | b-req | b-feat | b-review | b-debug)")}
			}
			autoResync(cmd, stdio)
			return runWorkflow(cmd, stdio, rf, func(o *app.RunWorkflowOptions) {
				o.Workflow, o.Request, o.Task, o.NoReview, o.Step = args[0], strings.Join(args[1:], " "), rf.taskRef, rf.noReview, rf.step
				o.Plan, o.NoPlan, o.Depth, o.Fast, o.Variant = rf.plan, rf.noPlan, rf.depth, rf.fast, rf.variant
				o.Goal, o.VerifyCommand, o.Autonomy, o.Criteria, o.Allow, o.Deny = rf.ef.goal, rf.ef.verify, rf.ef.autonomy, rf.ef.criteria, rf.ef.allow, rf.ef.deny
				o.Constraints, o.ClearScope = rf.ef.constraints, rf.ef.clearScope
				o.GoalSet, o.VerifyCommandSet, o.AutonomySet = rf.ef.changed(cmd)
			}, nil)
		},
	}
	addRunFlags(c, &rf, true)
	c.AddCommand(newRunStatusCmd(stdio), newRunResumeCmd(stdio), newRunNextCmd(stdio), newRunCompleteCmd(stdio), newRunAbortCmd(stdio))
	return c
}

func addRunFlags(c *cobra.Command, rf *runFlags, full bool) {
	f := c.Flags()
	f.BoolVar(&rf.noReview, "no-review", false, "bỏ bước b-review (b-do)")
	f.BoolVar(&rf.noWait, "no-wait", false, "khoá worktree đang bị bước khác giữ thì dừng ngay (blocked) thay vì chờ")
	f.StringVar(&rf.agentCommand, "agent-command", "", "lệnh tác nhân tuỳ chỉnh cho mọi bước (đề bài qua stdin và $BINT_EVAL_PROMPT_FILE)")
	f.StringVar(&rf.model, "model", "", "mô hình cho mọi bước (ghi đè harnesses.<h>.models.<role>, workflow.agent.model, harnesses.<h>.model)")
	f.StringVar(&rf.harness, "harness", "", "harness cho mọi bước của lượt này (claude | codex | antigravity; phải có trong harnesses.enabled)")
	f.StringArrayVar(&rf.roles, "role", nil, "harness cho một role, dạng role=harness (planner|implementer|reviewer hoặc b-req|b-feat|b-review); lặp lại được")
	f.BoolVar(&rf.fresh, "fresh", false, freshFlagHelp)
	f.BoolVar(&rf.asJSON, "json", false, "in kết quả dạng JSON")
	if full {
		f.StringVar(&rf.taskRef, "task", "", "chạy trên tác vụ có sẵn")
		f.StringVar(&rf.step, "step", "", "chỉ với --task và flow tuỳ biến hoặc b-debug: chạy đúng một bước (phải là bước kế tiếp của tác vụ) rồi dừng; phiên điều phối dùng cho bước gán harness khác Claude")
		f.BoolVar(&rf.plan, "plan", false, "luôn qua b-req")
		f.BoolVar(&rf.noPlan, "no-plan", false, "bỏ qua b-req")
		f.BoolVar(&rf.fast, "fast", false, fastFlagHelp)
		f.StringVar(&rf.variant, "variant", "", variantFlagHelp)
		f.StringVar(&rf.depth, "depth", "auto", depthFlagHelp)
		f.BoolVar(&rf.worktree, "worktree", false, "tác vụ mới chạy trong worktree riêng (song song với tác vụ khác); loại trừ --shared, không đi cùng --task")
		f.BoolVar(&rf.shared, "shared", false, "tác vụ mới sửa thẳng trên cây đang làm; loại trừ --worktree, không đi cùng --task")
		f.StringVar(&rf.driver, "driver", "auto", "ai chạy các bước của tác vụ mới: auto | process | session (session: lập phiếu rồi dừng, bước chạy bằng bint run next/complete); không đi cùng --task")
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
		ro.Ref, ro.NoReview, ro.NoWait, ro.Rerun, ro.Fresh, ro.Harness, ro.Roles, ro.Model, ro.AgentCommand = *resume, rf.noReview, rf.noWait, rf.rerun, rf.fresh, rf.harness, rf.roles, rf.model, rf.agentCommand
		ro.Answer, ro.Accept, ro.FixRound, ro.Slice, ro.DropGoal = rf.answer, rf.accept, rf.fixRound, rf.slice, rf.dropGoal
		if err := ro.Validate(); err != nil {
			return err
		}
	} else {
		o = app.RunWorkflowDefaults(config.Default())
		fillRun(&o)
		o.Harness, o.Roles, o.Model, o.AgentCommand, o.NoWait, o.Fresh = rf.harness, rf.roles, rf.model, rf.agentCommand, rf.noWait, rf.fresh
		o.Worktree, o.Shared, o.Driver = rf.worktree, rf.shared, rf.driver
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
	printChoices(w, res.Choices)
	printGreen(w, res.TaskID, res.LastGreen, res.OpenFindings)
	fmt.Fprintf(w, "Chi tiết: bint task show %s   ·   bint run status %s\n", res.TaskID, res.TaskID)
}

// printChoices in khối "Lựa chọn:" (lệnh cụ thể cho người không đọc mã) khi tác vụ dừng chờ quyết định.
func printChoices(w io.Writer, cs []workflow.Choice) {
	if len(cs) == 0 {
		return
	}
	fmt.Fprintln(w, "Lựa chọn:")
	for i, c := range cs {
		fmt.Fprintf(w, "  %d. %s:\n     %s\n", i+1, c.Label, c.Command)
	}
}

func newRunResumeCmd(stdio IO) *cobra.Command {
	var rf runFlags
	c := &cobra.Command{
		Use:   "resume <task-id>",
		Short: "Tiếp tục tác vụ blocked / interrupted / failed từ bước đang dở",
		Long: `Tiếp tục điều phối tác vụ (workflow đã tạo nó) từ trạng thái trước khi dừng. Lượt mới có
parent_run_id là lượt trước. Thay đổi đang chờ duyệt phải được duyệt trước (bint task approve).

Bước chỉ đọc (b-req, b-review, bước plan/review của flow tuỳ biến) kết thúc mà cây đổi trong lúc chạy thì tác vụ dừng
blocked và kết quả bước được giữ; resume áp kết quả đó (không chạy lại). Đã hoàn tác thay đổi lạ và muốn chạy lại bước:
--rerun bỏ kết quả giữ rồi chạy lại bước (lỗi nếu tác vụ không có bước giữ).

Tác vụ dừng vì b-feat hỏi người (bint task error --question) hoặc b-feat kết thúc mà không đổi tệp nào: trả lời hay nói rõ thêm bằng
--answer "<nội dung>" — b-feat chạy lại trên chính tác vụ này (nối tiếp phiên cũ khi nối tiếp được) và nhận nguyên văn câu hỏi
cùng câu trả lời.

Tác vụ dừng sau khi hết vòng sửa review (workflow.max_review_rounds là số vòng sửa tối đa) in ba lựa chọn: --accept chấp nhận kết
quả như hiện tại (vào thẳng bước hoàn tất, bỏ qua phát hiện còn lại), --fix-round cho sửa thêm đúng một vòng (b-feat rồi b-review),
hoặc bint task cancel. --accept và --fix-round chỉ người dùng chạy được (không phải phiên agent).

Luồng B′ (workflow.variant: bprime) có thước đo thăm dò đóng băng mà người cho là sai (b-feat đã dừng và hỏi): trả lời bằng --answer thì
thước đo được giữ (người xác nhận); --drop-goal thôi đóng băng nó (chỉ người dùng, chỉ tác vụ B′).

Tác vụ chạy theo lát mỏng dừng khi một lát quá trần thời gian agent, không đạt số đo, vượt ngân sách diff/hiệu năng hoặc agent lỗi: Bint GIỮ
cây của lát (không tự quay về điểm xanh) và in ba lựa chọn: --slice keep (chốt cây hiện tại làm điểm xanh, sang lát kế), --slice discard
(đưa tệp của tác vụ về điểm xanh, sang lát kế), --slice more (chạy lại lát từ cây hiện tại, thêm một trần thời gian; mặc định khi không nêu
--slice). --slice chỉ người dùng chạy được. Đặt workflow.slicing: off hoặc slice_timeout_minutes: 0 thì lát chưa chốt gộp vào một b-feat thường.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflow(cmd, stdio, rf, nil, &args[0])
		},
	}
	addRunFlags(c, &rf, false)
	c.Flags().BoolVar(&rf.rerun, "rerun", false, "bỏ kết quả bước chỉ đọc đang giữ và chạy lại bước đó (thay vì áp kết quả đã giữ)")
	c.Flags().StringVar(&rf.answer, "answer", "", "trả lời b-feat đang chờ (câu hỏi --question hoặc b-feat không đổi tệp): b-feat chạy lại với nguyên văn câu hỏi và câu trả lời")
	c.Flags().StringVar(&rf.slice, "slice", "", "lát đang giữ chờ người: keep (chốt cây hiện tại) | discard (đưa tệp của tác vụ về điểm xanh) | more (chạy lại lát từ cây hiện tại); chỉ người dùng")
	c.Flags().BoolVar(&rf.accept, "accept", false, "sau khi hết vòng sửa review: chấp nhận kết quả như hiện tại, vào thẳng bước hoàn tất (chỉ người dùng)")
	c.Flags().BoolVar(&rf.fixRound, "fix-round", false, "sau khi hết vòng sửa review: cho sửa thêm đúng một vòng (b-feat rồi b-review; chỉ người dùng)")
	c.Flags().BoolVar(&rf.dropGoal, "drop-goal", false, "luồng B′: thôi đóng băng thước đo thăm dò mà người cho là sai (tệp test vẫn còn trên đĩa; chỉ người dùng)")
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
				out := map[string]any{"task_id": t.State.ID, "status": t.State.Status, "reason": t.State.Reason,
					"resume_from": t.State.ResumeFrom, "runs": rows, "isolation": t.State.Isolation(),
					"isolation_reason": isolationReason(t), "driver": taskDriver(t)}
				if ls := t.State.LastStep; ls != nil {
					out["last_step"] = ls
				}
				if ps := t.State.PendingStep; ps != nil {
					out["pending_step"] = map[string]any{"step": ps.Step, "run_id": ps.RunID, "lease_until": ps.LeaseUntil}
				}
				if t.State.Origin != "" {
					out["worktree"], out["branch"], out["origin"] = t.State.Worktree, t.State.Branch, t.State.Origin
					out["landed"], out["land"] = t.State.Landed != nil, t.State.Landed
					out["awaiting_land"] = t.State.AwaitingLand
				}
				return writeJSON(stdio.Out, out)
			}
			w := stdio.Out
			fmt.Fprintf(w, "Tác vụ %s: %s", t.State.ID, t.State.Status)
			if t.State.Reason != "" {
				fmt.Fprintf(w, " — %s", t.State.Reason)
			}
			fmt.Fprintf(w, "\n  %s\n", t.Ticket.Goal)
			printIsolationLines(w, "  ", t)
			printDriverLines(w, "  ", t)
			printLastStepLine(w, "  ", t)
			if len(rows) == 0 {
				fmt.Fprintln(w, "\nChưa có lượt chạy nào được ghi nhận cho tác vụ này.")
				if task.Paused(t.State.Status) {
					fmt.Fprintf(w, "\nTiếp tục: %s\n", t.State.ResumeCommand())
				}
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
				fmt.Fprintf(w, "\nTiếp tục: %s\n", t.State.ResumeCommand())
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}
