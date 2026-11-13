package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"bint/internal/app"
	"bint/internal/config"
	"bint/internal/eval"
)

func newSkillCmd(stdio IO) *cobra.Command {
	c := &cobra.Command{
		Use:   "skill",
		Short: "Skill gốc và lớp phủ theo dự án",
		Long: `Skill gốc (b-bootstrap, b-knowledge, b-req, b-feat, b-review, b-do, b-add-skill, b-add-flow) đi kèm tệp thực thi Bint và không
được sửa trực tiếp trong repository (mục 6.6). Tuỳ biến theo dự án đặt trong lớp phủ
.bint/skills/overlays/<skill>.md, chỉ tại các điểm neo skill gốc khai báo. ` + "`bint sync`" + `
ghép skill gốc với lớp phủ và xuất bản cho harness; core.lock ghi phiên bản đang dùng.

Khi nâng cấp Bint, lớp phủ được áp lại; điểm neo không còn → xung đột, bản cũ được giữ.

Skill tuỳ biến của dự án do người dùng (hoặc agent theo /b-add-skill) viết trong .bint/skills/<tên>/SKILL.md, kèm tệp
phụ (tài liệu tham chiếu, script; thư mục tests/ không xuất bản). ` + "`bint sync`" + ` xuất bản chúng cho mọi harness
như skill gốc:
  bint skill new <tên>       tạo SKILL.md mẫu (không ghi đè)
  bint skill lint [tên]      kiểm hình thức; skill lỗi lint không được xuất bản
  bint skill test <tên>      chạy kịch bản áp lực tests/*.yml ở nhánh có và không có skill (tốn chi phí agent)

Skill chuyên biệt do tiến hoá tạo (L2, ` + "`bint evolve`" + `) nằm trong .bint/skills/evolved/<tên>/ và được
xuất bản cùng skill gốc. Mỗi phiên bản gắn với mô hình đã kiểm chứng nó:
  bint skill diff <tên>      bản thay đổi giữa hai phiên bản
  bint skill versions <tên>  lịch sử phiên bản, mô hình, người duyệt
  bint skill check <tên|đề xuất>  kiểm tra điều kiện kích hoạt
  bint skill regress         skill nào chưa xác thực với mô hình hiện dùng`,
	}
	c.AddCommand(newSkillListCmd(stdio), newSkillShowCmd(stdio), newSkillOverlayCmd(stdio), newSkillNewCmd(stdio),
		newSkillLintCmd(stdio), newSkillTestCmd(stdio), newSkillDiffCmd(stdio), newSkillVersionsCmd(stdio), newSkillCheckCmd(stdio), newSkillRegressCmd(stdio))
	return c
}

func newSkillListCmd(stdio IO) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "list",
		Short: "Liệt kê skill gốc, tiến hoá và tuỳ biến: điểm neo, lớp phủ, trạng thái xuất bản",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			k, err := openKnowledge(cmd)
			if err != nil {
				return err
			}
			lst, err := app.ListSkills(k, app.SkillListDefaults(config.Default()))
			if err != nil {
				return err
			}
			rows, evProblems := lst.Rows, lst.EvolvedProblems
			if asJSON {
				return writeJSON(stdio.Out, rows)
			}
			tw := tabwriter.NewWriter(stdio.Out, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "SKILL\tPHIÊN BẢN\tCORE.LOCK\tLỚP PHỦ\tXUẤT BẢN\tĐIỂM NEO")
			for _, r := range rows {
				if r.Kind == "evolved" {
					state := "tiến hoá/" + r.Status
					if r.Stale != "" {
						state += ", lỗi thời"
					}
					fmt.Fprintf(tw, "%s\t%s\t-\t%s\t%s\t%s\n", r.Name, r.Version, state, r.Published, strings.Join(r.Anchors, ", "))
					continue
				}
				locked := "-"
				if r.Locked != "" {
					locked = r.Locked
					if r.Locked != r.Version {
						locked += " (cũ)"
					}
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", r.Name, r.Version, locked, r.Overlay, r.Published, strings.Join(r.Anchors, ", "))
			}
			tw.Flush()
			for _, p := range evProblems {
				fmt.Fprintf(stdio.Out, "✗ skill tiến hoá hỏng: %s\n", p)
			}
			for _, r := range rows {
				if r.Stale != "" {
					fmt.Fprintf(stdio.Out, "! %s lỗi thời, không được xuất bản: %s\n", r.Name, r.Stale)
				}
				if r.Problem != "" {
					fmt.Fprintf(stdio.Out, "✗ %s: %s\n", r.Name, r.Problem)
				}
			}
			for _, o := range lst.Orphans {
				fmt.Fprintf(stdio.Out, "! %s: không ứng với skill gốc nào\n", o)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func newSkillShowCmd(stdio IO) *cobra.Command {
	var core bool
	c := &cobra.Command{
		Use:     "show <tên>",
		Aliases: []string{"inspect"},
		Short:   "In skill đã ghép lớp phủ (dùng với harness chưa có adapter); --core để in bản gốc",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := cwd()
			if err != nil {
				return err
			}
			sc, err := app.ShowSkill(cmd.Context(), dir, app.SkillShowOptions{Name: args[0], Core: core})
			if err != nil {
				return err
			}
			_, err = stdio.Out.Write(sc.Content)
			return err

		},
	}
	c.Flags().BoolVar(&core, "core", false, "in skill gốc, không áp lớp phủ")
	return c
}

func newSkillOverlayCmd(stdio IO) *cobra.Command {
	return &cobra.Command{
		Use:   "overlay <tên>",
		Short: "Tạo tệp lớp phủ mẫu (liệt kê điểm neo) cho một skill gốc",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := cwd()
			if err != nil {
				return err
			}
			res, err := app.CreateOverlay(cmd.Context(), dir, app.SkillOverlayOptions{Name: args[0]})
			if err != nil {
				return err
			}
			if res.Existed {
				fmt.Fprintf(stdio.Out, "Đã có %s (không ghi đè). Sửa rồi chạy `bint sync`.\n", res.Path)
				return nil
			}
			fmt.Fprintf(stdio.Out, "Đã tạo %s. Bỏ thụt lề khối mẫu để bật, rồi chạy `bint sync`.\n", res.Path)
			return nil
		},
	}
}

func newSkillNewCmd(stdio IO) *cobra.Command {
	return &cobra.Command{
		Use:   "new <tên>",
		Short: "Tạo skill tuỳ biến mẫu trong .bint/skills/<tên>/ (không ghi đè)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := cwd()
			if err != nil {
				return err
			}
			res, err := app.NewSkill(cmd.Context(), dir, app.SkillNewOptions{Name: args[0]})
			if err != nil {
				return err
			}
			if res.Existed {
				fmt.Fprintf(stdio.Out, "Đã có %s (không ghi đè). Sửa rồi chạy `bint skill lint %s` và `bint sync`.\n", res.Path, args[0])
				return nil
			}
			fmt.Fprintf(stdio.Out, "Đã tạo %s. Viết skill, rồi chạy `bint skill lint %s` và `bint sync`.\n", res.Path, args[0])
			return nil
		},
	}
}

func newSkillLintCmd(stdio IO) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "lint [tên]",
		Short: "Kiểm hình thức skill tuỳ biến (không tên: mọi skill tuỳ biến); thoát 1 khi có lỗi",
		Long: `Kiểm skill tuỳ biến trong .bint/skills/<tên>/: tên hợp lệ (không tiền tố b-, không trùng skill gốc hay
tiến hoá), name khớp tên thư mục, description không rỗng và tối đa 1024 ký tự, frontmatter chỉ có name, description,
disable-model-invocation, argument-hint, thân không rỗng và tối đa 500 dòng, liên kết tới tệp phụ phải tồn tại, tệp phụ
là tệp thường trong trần kích thước. Description không mở đầu "Dùng khi" chỉ bị cảnh báo. Skill có lỗi không được
` + "`bint sync`" + ` xuất bản.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := cwd()
			if err != nil {
				return err
			}
			o := app.SkillLintOptions{}
			if len(args) == 1 {
				o.Name = args[0]
			}
			res, err := app.LintSkills(cmd.Context(), dir, o)
			if err != nil {
				return err
			}
			bad := 0
			for _, r := range res {
				if !r.OK() {
					bad++
				}
			}
			if asJSON {
				if err := writeJSON(stdio.Out, res); err != nil {
					return err
				}
			} else {
				if len(res) == 0 {
					fmt.Fprintln(stdio.Out, "Chưa có skill tuỳ biến nào (tạo bằng `bint skill new <tên>`).")
				}
				for _, r := range res {
					mark := "✓"
					if !r.OK() {
						mark = "✗"
					}
					fmt.Fprintf(stdio.Out, "%s %s\n", mark, r.Path)
					for _, e := range r.Errors {
						fmt.Fprintf(stdio.Out, "    lỗi: %s\n", e)
					}
					for _, w := range r.Warnings {
						fmt.Fprintf(stdio.Out, "    cảnh báo: %s\n", w)
					}
				}
			}
			if bad > 0 {
				return withCode(ExitError, fmt.Errorf("%d skill tuỳ biến có lỗi", bad))
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func newSyncCmd(stdio IO) *cobra.Command {
	var force bool
	c := &cobra.Command{
		Use:   "sync",
		Short: "Ghép skill gốc với lớp phủ, xuất bản skill cho harness; cập nhật core.lock",
		Long: `Ghép skill gốc với lớp phủ (.bint/skills/overlays/), thêm skill tuỳ biến (.bint/skills/<tên>/,
kèm tệp phụ) và skill tiến hoá, rồi xuất bản cho các harness có adapter: Claude Code → .claude/skills/, Codex và Antigravity → .agents/skills/ (frontmatter
chỉ giữ name, description). Chạy sau khi sửa lớp phủ hoặc nâng cấp Bint.

  - Skill đã xuất bản bị sửa tay → xung đột, không ghi đè (--force để ghi đè, có sao lưu);
    tuỳ biến thuộc về lớp phủ.
  - Lớp phủ dùng điểm neo không còn trong skill gốc mới → xung đột, giữ bản cũ và mục
    core.lock cũ cho tới khi lớp phủ được sửa (--force không vượt qua được).
  - Skill tuỳ biến không qua ` + "`bint skill lint`" + ` → xung đột, không xuất bản bản mới, giữ bản đã xuất
    bản. Tệp phụ hay cả skill bị xoá khỏi nguồn thì bản đã xuất bản được gỡ (nếu chưa bị sửa tay).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := cwd()
			if err != nil {
				return err
			}
			si, err := app.Sync(cmd.Context(), dir, Version, app.SyncOptions{Force: force})
			if err != nil {
				return err
			}
			printSkillInstall(stdio.Out, si)
			n := si.Conflicts
			if n > 0 {
				return withCode(ExitError, fmt.Errorf("%d xung đột cần xử lý", n))
			}
			return nil
		},
	}
	c.Flags().BoolVar(&force, "force", false, "ghi đè skill đã bị sửa tay (bản cũ được sao lưu)")
	return c
}

func newSkillTestCmd(stdio IO) *cobra.Command {
	var (
		arm, harnessName, model, agentCommand string
		reps                                  int
		costPerRun                            float64
		yes, asJSON                           bool
	)
	c := &cobra.Command{
		Use:   "test <tên>",
		Short: "Chạy kịch bản áp lực tests/*.yml của skill tuỳ biến ở nhánh có và không có skill, đo tỉ lệ tuân thủ",
		Long: `Chạy kịch bản trong .bint/skills/<tên>/tests/*.yml (không xuất bản) bằng agent chỉ đọc, ở hai nhánh:
  without  đề bài = prompt + pressures (RED: agent làm gì khi chưa có skill)
  with     đề bài có thêm nội dung SKILL.md trước tình huống (GREEN: skill có đổi được hành vi không)
Mỗi lượt chạy trong thư mục tạm rỗng ngoài repository (nhánh with có thêm tệp phụ của skill). Một lượt tuân thủ khi mọi mục
expect đúng trên câu trả lời cuối; lượt lỗi hạ tầng, hết giờ hoặc không có câu trả lời không tính vào mẫu số.

Mỗi kịch bản là một tệp YAML:
  prompt: tình huống giao cho agent          (bắt buộc)
  pressures: [câu áp lực, ...]               (nối vào cuối đề bài)
  reps: 5                                    (1–20, mặc định 5 lượt mỗi nhánh)
  expect:                                    (≥ 1 mục, mỗi mục đúng một khoá)
    - contains: chuỗi phải có
    - not_contains: chuỗi không được có
    - regex: biểu thức Go phải khớp
    - not_regex: biểu thức Go không được khớp

Agent gọi mô hình nên tốn chi phí: ước tính được in trước, và khi không chạy trong terminal phải có --yes (chạy một lần không --yes
để xem ước tính). Agent theo role skill-tester (roles.skill-tester, mặc định theo planner). Kết quả JSON ghi vào
<thư mục trạng thái>/runtime/skill-tests/<tên>/<id>.json. Tỉ lệ là số đo tham khảo trên vài lượt, không phải bằng chứng thống kê.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := cwd()
			if err != nil {
				return err
			}
			// Với --json, stdout chỉ có JSON kết quả; ước tính và tiến độ đi sang stderr.
			w, log := stdio.Out, stdio.Out
			if asJSON {
				log = stdio.Err
			}
			opts := app.SkillTestDefaults(config.Default())
			opts.Name, opts.Reps, opts.Harness, opts.Model, opts.AgentCommand, opts.Yes = args[0], reps, harnessName, model, agentCommand, yes
			if arm != "" {
				opts.Arm = arm
			}
			if cmd.Flags().Changed("cost-per-run") {
				opts.CostPerRun = &costPerRun
			}
			if isTerminal(stdio.In) {
				opts.Confirm = func(req app.ConfirmRequest) bool { return confirm(stdio, req.Text) }
			}
			opts.OnEstimate = func(e app.SkillTestEstimate) { printSkillTestEstimate(log, e) }
			opts.Progress = func(r eval.SkillTestRun, n, total int) {
				mark := "✓"
				switch {
				case r.Error != "":
					mark = "! " + truncate(r.Error, 100)
				case !r.Complied:
					mark = "✗ " + strings.Join(r.Failed, "; ")
				}
				fmt.Fprintf(log, "[%d/%d] %s %-7s #%d  %s\n", n, total, r.Scenario, r.Arm, r.Rep, mark)
			}
			res, err := app.SkillTest(cmd.Context(), dir, opts)
			if res != nil && res.Path != "" {
				if asJSON {
					if werr := writeJSON(w, res.Report); werr != nil {
						return werr
					}
				} else {
					printSkillTestReport(w, res)
				}
			}
			if errors.Is(err, context.Canceled) {
				return withCode(ExitError, fmt.Errorf("đã dừng theo yêu cầu (kết quả một phần đã ghi)"))
			}
			return err
		},
	}
	f := c.Flags()
	f.StringVar(&arm, "arm", "both", "nhánh chạy: both | without | with (without = RED khi skill chưa viết)")
	f.IntVar(&reps, "reps", 0, "số lượt mỗi nhánh cho mọi kịch bản, 1–20 (mặc định theo reps của từng kịch bản)")
	f.StringVar(&harnessName, "harness", "", "harness chạy agent: claude | codex | antigravity (mặc định roles.skill-tester, rồi roles.planner)")
	f.StringVar(&model, "model", "", "mô hình (mặc định harnesses.<h>.models.skill-tester, rồi models.planner)")
	f.StringVar(&agentCommand, "agent-command", "", "dùng lệnh tuỳ chỉnh làm agent thay cho harness (đề bài qua stdin; in JSON kiểu Claude Code có trường result)")
	f.Float64Var(&costPerRun, "cost-per-run", 0, "chi phí ước tính mỗi lượt (mặc định: trung bình lần chạy trước, rồi eval.prior_run_usd)")
	f.BoolVarP(&yes, "yes", "y", false, "không hỏi xác nhận (bắt buộc khi chạy không tương tác)")
	f.BoolVar(&asJSON, "json", false, "in kết quả dạng JSON thay cho bảng")
	return c
}

func printSkillTestEstimate(w io.Writer, e app.SkillTestEstimate) {
	model := e.Model
	if model == "" {
		model = "mặc định của harness"
	}
	fmt.Fprintf(w, "Kiểm thử skill %s: %d kịch bản × nhánh %s → %d lượt agent (chỉ đọc, thư mục tạm rỗng)\n",
		e.Skill, e.Scenarios, strings.Join(e.Arms, "+"), e.Runs)
	fmt.Fprintf(w, "Agent:          %s, mô hình %s (role skill-tester)\n", e.Harness, model)
	if e.CostPerRun != nil && e.Total != nil {
		fmt.Fprintf(w, "Chi phí/lượt:   $%.3f (%s)\n", *e.CostPerRun, e.CostSource)
		fmt.Fprintf(w, "Tổng chi phí:   ~$%.2f\n", *e.Total)
	} else {
		fmt.Fprintf(w, "Chi phí:        chưa biết (%s) — dùng --cost-per-run để ước tính\n", e.CostSource)
	}
	if !e.ReportsCost {
		fmt.Fprintln(w, "Lưu ý:          harness này không báo chi phí nên không đo được chi phí thật.")
	}
	if e.Timeout > 0 {
		fmt.Fprintf(w, "Hạn mỗi lượt:   %s (eval.timeout_minutes)\n", e.Timeout.Round(time.Second))
	}
}

func printSkillTestReport(w io.Writer, res *app.SkillTestResult) {
	rep := res.Report
	rate := func(s eval.SkillArmStats) string {
		if s.Rate == nil {
			return "-"
		}
		return fmt.Sprintf("%.0f%%", 100**s.Rate)
	}
	cell := func(s eval.SkillArmStats) string {
		out := fmt.Sprintf("%d/%d (%s)", s.Complied, s.Graded, rate(s))
		if s.Errors > 0 {
			out += fmt.Sprintf(", %d lỗi", s.Errors)
		}
		return out
	}
	stats := map[string]eval.SkillArmStats{}
	for _, s := range rep.Scenarios {
		stats[s.Scenario+"\x00"+s.Arm] = s.SkillArmStats
	}
	var names []string
	for _, s := range rep.Scenarios {
		if !slices.Contains(names, s.Scenario) {
			names = append(names, s.Scenario)
		}
	}
	fmt.Fprintf(w, "\nTỉ lệ tuân thủ (đạt/chấm được):\n")
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "KỊCH BẢN\tWITHOUT\tWITH")
	row := func(label string, get func(arm string) (eval.SkillArmStats, bool)) {
		cols := make([]string, 0, 2)
		for _, arm := range []string{eval.SkillArmWithout, eval.SkillArmWith} {
			if s, ok := get(arm); ok {
				cols = append(cols, cell(s))
			} else {
				cols = append(cols, "-")
			}
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", label, cols[0], cols[1])
	}
	for _, n := range names {
		row(n, func(arm string) (eval.SkillArmStats, bool) { s, ok := stats[n+"\x00"+arm]; return s, ok })
	}
	row("TỔNG", func(arm string) (eval.SkillArmStats, bool) { s, ok := rep.Totals[arm]; return s, ok })
	tw.Flush()
	if rep.CostUSD != nil {
		fmt.Fprintf(w, "Chi phí đo được: $%.3f\n", *rep.CostUSD)
	}
	if rep.Status != eval.SkillTestCompleted {
		fmt.Fprintf(w, "Trạng thái: %s (chỉ có phần đã chạy)\n", rep.Status)
	}
	fmt.Fprintf(w, "Kết quả: %s\n", res.Path)
}
