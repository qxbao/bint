package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"bint/internal/app"
	"bint/internal/eval"
)

// openEval mở không gian đánh giá ở thư mục hiện tại.
func openEval(cmd *cobra.Command, withDB bool) (*app.EvalWorkspace, error) {
	dir, err := cwd()
	if err != nil {
		return nil, err
	}
	return app.OpenEval(cmd.Context(), dir, withDB)
}

func newEvalCmd(stdio IO) *cobra.Command {
	c := &cobra.Command{
		Use:   "eval",
		Short: "Khung đánh giá: khai thác tác vụ từ Git, chạy so sánh các nhánh, báo cáo",
		Long: `Khung đánh giá đo hiệu quả của một thay đổi (tri thức, skill, quy trình) trên chính
repository này (mục 12 của kế hoạch).

Quy trình:
  1. Xác nhận eval.test_command (và eval.setup_command nếu cần) trong .bint/config.yml.
  2. bint eval mine --verify     khai thác commit có kiểm thử fail→pass làm ứng viên
  3. bint eval show <id>         đọc, sửa đề bài trong tệp ứng viên nếu cần
     bint eval approve <id>...   duyệt vào .bint/evals/cases/ (có thể commit)
  4. bint eval bootstrap --budget 5   (nhánh minimal, strict) dựng tri thức tại commit gốc
     bint eval estimate          ước tính chi phí và cỡ mẫu
  5. bint eval run --budget 20   chạy các nhánh × lần lặp trong thư mục cô lập
  6. bint eval report            so sánh theo cặp kèm khoảng tin cậy 95%`,
	}
	c.AddCommand(newEvalMineCmd(stdio), newEvalVerifyCmd(stdio), newEvalListCmd(stdio), newEvalShowCmd(stdio), newEvalSpecCmd(stdio),
		newEvalApproveCmd(stdio), newEvalRejectCmd(stdio), newEvalEstimateCmd(stdio), newEvalRunCmd(stdio),
		newEvalReportCmd(stdio), newEvalBootstrapCmd(stdio), newEvalCleanCmd(stdio))
	return c
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func verifyLabel(c *eval.Case) string {
	if c.Verification == nil {
		return "chưa"
	}
	return c.Verification.Status
}

func newEvalMineCmd(stdio IO) *cobra.Command {
	var (
		rev        string
		limit      int
		verify     bool
		verifyRuns int
		fromTasks  bool
		asJSON     bool
	)
	c := &cobra.Command{
		Use:   "mine",
		Short: "Khai thác tác vụ đánh giá từ lịch sử Git (tạo ứng viên chờ duyệt)",
		Long: `Quét lịch sử (eval.mine.max_commits commit, bỏ merge) và chọn các commit: có một commit
cha; thêm/sửa kiểm thử và sửa mã nguồn; quy mô mã nguồn trong [min_source_lines,
max_source_lines] và tối đa max_files tệp; mô tả đủ dài làm đề bài.

Đề bài được viết lại tự động (bỏ trailer, khối mã, tham chiếu commit) rồi đối chiếu
với lời giải để phát hiện rò rỉ. Ứng viên nằm trong thư mục trạng thái cục bộ, chưa
vào tập đánh giá cho tới khi được duyệt.

--verify chạy eval.test_command trong thư mục cô lập để xác nhận kiểm thử của commit
thất bại tại commit cha và qua với lời giải thật; ứng viên không đạt bị loại.

--from-tasks dựng ứng viên từ phiếu b-do đã done thay vì lịch sử Git: đề bài lấy từ yêu cầu và
tiêu chí chấp nhận của phiếu; lời giải là cây lúc đưa về (hoặc của lần kiểm chứng qua cuối cùng),
đóng băng thành hai commit tổng hợp giữ bằng ref refs/bint/eval/<id>/. test_command là
verify.command của phiếu nếu người đã xác nhận. Tri thức rút từ chính phiếu bị loại khi chấm.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			e, err := openEval(cmd, false)
			if err != nil {
				return err
			}
			w := stdio.Out
			opts := app.EvalMineDefaults(e.WS.Config)
			opts.Rev, opts.Limit, opts.Verify, opts.VerifyRuns, opts.FromTasks = rev, limit, verify, verifyRuns, fromTasks
			if !asJSON {
				opts.Progress = func(p app.EvalProgress) {
					if p.Done {
						fmt.Fprintln(w, verifyLabel(p.Case))
						return
					}
					fmt.Fprintf(w, "[%d/%d] kiểm chứng %s %s ... ", p.Index, p.Total, p.Case.ID, truncate(p.Case.Source.Subject, 50))
				}
			}
			res, err := app.EvalMine(ctx, e, opts)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(w, map[string]any{"scanned": res.Scanned, "new": res.New, "skipped": res.Skipped})
			}
			unit := "commit"
			if fromTasks {
				unit = "phiếu"
			}
			fmt.Fprintf(w, "Đã quét %d %s, %d ứng viên mới.\n", res.Scanned, unit, len(res.New))
			if len(res.Skipped) > 0 {
				fmt.Fprintln(w, "Bỏ qua:")
				for _, k := range sortedMapKeys(res.Skipped) {
					fmt.Fprintf(w, "  %5d  %s\n", res.Skipped[k], k)
				}
			}
			if len(res.New) > 0 {
				fmt.Fprintln(w)
				printCaseTable(w, res.New)
				fmt.Fprintf(w, "\nỨng viên lưu tại %s\n", res.CandidatesDir)
				fmt.Fprintln(w, "Bước tiếp: `bint eval show <id>` để đọc đề bài, rồi `bint eval approve <id>...`.")
				if !verify {
					fmt.Fprintln(w, "Chưa kiểm chứng fail→pass: chạy `bint eval verify --all` sau khi xác nhận eval.test_command.")
				}
			}
			return nil
		},
	}
	f := c.Flags()
	f.StringVar(&rev, "rev", "HEAD", "điểm bắt đầu quét lịch sử")
	f.IntVar(&limit, "limit", 0, "số ứng viên mới tối đa (0 = không giới hạn)")
	f.BoolVar(&verify, "verify", false, "kiểm chứng fail→pass từng ứng viên mới (chạy eval.test_command)")
	f.BoolVar(&fromTasks, "from-tasks", false, "dựng ứng viên từ phiếu b-do đã done thay vì lịch sử Git")
	f.IntVar(&verifyRuns, "verify-runs", 2, "số lần chạy kiểm thử với lời giải thật (bắt kiểm thử chập chờn)")
	f.BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

// specLabel: trạng thái đặc tả và số chữ ký giao diện đã nối vào đề bài ("-" = chưa phân tích).
func specLabel(c *eval.Case) string {
	if c.Spec == nil {
		return "-"
	}
	return fmt.Sprintf("%s·%d", c.Spec.Status, len(c.Spec.Interface))
}

func printCaseTable(w io.Writer, cs []*eval.Case) {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tTRẠNG THÁI\tTẬP\tNGÀY\tMÃ/TEST (dòng)\tRÒ RỈ\tĐẶC TẢ\tFAIL→PASS\tTIÊU ĐỀ")
	for _, c := range cs {
		date := c.Source.Date
		if len(date) >= 10 {
			date = date[:10]
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d/%d\t%s\t%s\t%s\t%s\n", c.ID, c.Status, orDash(c.Split), date,
			c.Stats.SourceLines, c.Stats.TestLines, c.Leak.Status, specLabel(c), verifyLabel(c), truncate(c.Source.Subject, 60))
	}
	tw.Flush()
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func sortedMapKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func newEvalVerifyCmd(stdio IO) *cobra.Command {
	var (
		all  bool
		runs int
	)
	c := &cobra.Command{
		Use:   "verify [id...]",
		Short: "Kiểm chứng fail→pass cho ứng viên hoặc tác vụ (chạy eval.test_command)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := openEval(cmd, false)
			if err != nil {
				return err
			}
			w := stdio.Out
			res, err := app.EvalVerify(ctx, e, app.EvalVerifyOptions{IDs: args, All: all, Runs: runs,
				Progress: func(p app.EvalProgress) {
					if !p.Done {
						fmt.Fprintf(w, "[%d/%d] %s %s ... ", p.Index, p.Total, p.Case.ID, truncate(p.Case.Source.Subject, 50))
						return
					}
					fmt.Fprintln(w, verifyLabel(p.Case))
					if !p.Case.Verified() {
						fmt.Fprintf(w, "      %s\n", p.Case.Verification.Detail)
					}
				}})
			if err != nil {
				return err // mã thoát 2 cho lỗi dùng sai do exitCode (root.go) xử lý
			}
			if res.Total == 0 {
				fmt.Fprintln(w, "Không có tác vụ nào cần kiểm chứng.")
				return nil
			}
			if res.Failed > 0 {
				return withCode(ExitError, fmt.Errorf("%d/%d tác vụ không qua kiểm chứng", res.Failed, res.Total))
			}
			return nil
		},
	}
	c.Flags().BoolVar(&all, "all", false, "mọi ứng viên và tác vụ chưa kiểm chứng")
	c.Flags().IntVar(&runs, "runs", 2, "số lần chạy kiểm thử với lời giải thật")
	return c
}

func newEvalListCmd(stdio IO) *cobra.Command {
	var (
		all    bool
		split  string
		asJSON bool
	)
	c := &cobra.Command{
		Use:   "list",
		Short: "Liệt kê tác vụ đã duyệt và ứng viên chờ duyệt",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := openEval(cmd, false)
			if err != nil {
				return err
			}
			res, err := app.EvalList(e, app.EvalListOptions{All: all, Split: split})
			if err != nil {
				return err
			}
			cases, pending := res.Cases, res.Pending
			if asJSON {
				return writeJSON(stdio.Out, map[string]any{"cases": nonNilCases(cases), "candidates": nonNilCases(pending)})
			}
			w := stdio.Out
			fmt.Fprintf(w, "Tác vụ đã duyệt (%d) — %s\n", len(cases), res.CasesDir)
			if len(cases) > 0 {
				printCaseTable(w, cases)
				counts := map[string]int{}
				for _, c := range cases {
					counts[c.Split]++
				}
				fmt.Fprintf(w, "Theo tập: build=%d validation=%d test=%d\n", counts[eval.SplitBuild], counts[eval.SplitValidation], counts[eval.SplitTest])
			}
			fmt.Fprintf(w, "\nỨng viên chờ duyệt (%d) — %s\n", len(pending), res.CandidatesDir)
			if len(pending) > 0 {
				printCaseTable(w, pending)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&all, "all", false, "gồm cả ứng viên đã loại")
	c.Flags().StringVar(&split, "split", "", "chỉ tác vụ thuộc tập build | validation | test")
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func nonNilCases(cs []*eval.Case) []*eval.Case {
	if cs == nil {
		return []*eval.Case{}
	}
	return cs
}

func newEvalShowCmd(stdio IO) *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Chi tiết một tác vụ: đề bài, tệp, kết quả kiểm tra rò rỉ và kiểm chứng",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := openEval(cmd, false)
			if err != nil {
				return err
			}
			r, err := app.EvalShow(cmd.Context(), e, app.EvalShowOptions{ID: args[0]})
			if err != nil {
				return err
			}
			c := r.Case
			w := stdio.Out
			fmt.Fprintf(w, "Tác vụ:      %s (%s)\n", c.ID, c.Status)
			fmt.Fprintf(w, "Tệp:         %s\n", r.Path)
			fmt.Fprintf(w, "Commit:      %s (gốc %s)\n", c.Source.Commit, c.Source.Base)
			fmt.Fprintf(w, "Tác giả:     %s, %s\n", c.Source.Author, c.Source.Date)
			fmt.Fprintf(w, "Tập:         %s\n", orDash(c.Split))
			fmt.Fprintf(w, "Quy mô:      %d tệp, %d dòng mã nguồn, %d dòng kiểm thử\n", c.Stats.Files, c.Stats.SourceLines, c.Stats.TestLines)
			fmt.Fprintf(w, "Mã nguồn:    %s\n", strings.Join(c.SourceFiles, ", "))
			fmt.Fprintf(w, "Kiểm thử:    %s\n", strings.Join(c.TestFiles, ", "))
			if r.TestCommandErr == nil {
				fmt.Fprintf(w, "Lệnh chấm:   %s\n", r.TestCommand)
			} else {
				fmt.Fprintf(w, "Lệnh chấm:   (chưa có) %v\n", r.TestCommandErr)
			}
			if v := c.Verification; v != nil {
				fmt.Fprintf(w, "Fail→pass:   %s (fail trước=%v, qua sau=%v ×%d) %s\n", v.Status, v.FailBefore, v.PassAfter, v.AfterRuns, v.Detail)
			} else {
				fmt.Fprintln(w, "Fail→pass:   chưa kiểm chứng")
			}
			if c.RejectReason != "" {
				fmt.Fprintf(w, "Lý do loại:  %s\n", c.RejectReason)
			}
			fmt.Fprintf(w, "Rò rỉ:       %s\n", c.Leak.Status)
			for _, f := range c.Leak.Findings {
				fmt.Fprintf(w, "  [%s] %s: %s\n", f.Severity, f.Kind, f.Detail)
			}
			if s := c.Spec; s != nil {
				fmt.Fprintf(w, "Đặc tả:      %s, %d chữ ký giao diện trong đề bài\n", s.Status, len(s.Interface))
				for _, f := range s.Findings {
					fmt.Fprintf(w, "  [%s] %s: %s\n", f.Severity, f.Kind, f.Detail)
				}
			} else {
				fmt.Fprintln(w, "Đặc tả:      chưa phân tích (`bint eval spec "+c.ID+"`)")
			}
			fmt.Fprintf(w, "\n--- Đề bài ---\n%s\n", c.Prompt)
			if c.OriginalMessage != "" && c.OriginalMessage != c.Prompt {
				fmt.Fprintf(w, "\n--- Thông điệp commit gốc ---\n%s\n", c.OriginalMessage)
			}
			return nil
		},
	}
}

func newEvalApproveCmd(stdio IO) *cobra.Command {
	var (
		force      bool
		unverified bool
		split      string
	)
	c := &cobra.Command{
		Use:   "approve <id>...",
		Short: "Duyệt ứng viên vào tập đánh giá (.bint/evals/cases/)",
		Long: `Duyệt ứng viên. Đề bài được kiểm tra rò rỉ lại (bạn có thể đã sửa tệp ứng viên):
mức fail bị từ chối trừ khi có --force; mức warn được chấp nhận kèm cảnh báo.
Tác vụ phải qua kiểm chứng fail→pass trừ khi có --unverified.

Tập con (build/validation/test) được gán tất định theo ID nếu không chỉ định --split.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			opts := app.EvalApproveOptions{IDs: args, Force: force, Unverified: unverified, Split: split}
			if err := opts.Validate(); err != nil {
				return err
			}
			e, err := openEval(cmd, false)
			if err != nil {
				return err
			}
			outs, err := app.EvalApprove(ctx, e, opts)
			problems := 0
			for _, o := range outs {
				switch {
				case o.Already:
					fmt.Fprintf(stdio.Out, "  - %s đã được duyệt trước đó\n", o.ID)
				case o.Approved:
					fmt.Fprintf(stdio.Out, "  ✓ %s → %s [%s]%s\n", o.ID, o.Path, o.Split, o.Note)
				default:
					problems++
				}
			}
			if err != nil {
				return err
			}
			if problems > 0 {
				for _, o := range outs {
					if o.Problem != "" {
						fmt.Fprintf(stdio.Out, "  ✗ %s\n", o.Problem)
					}
				}
				return withCode(ExitError, fmt.Errorf("%d tác vụ chưa được duyệt", problems))
			}
			return nil
		},
	}
	c.Flags().BoolVar(&force, "force", false, "duyệt kể cả khi kiểm tra rò rỉ báo fail hoặc ứng viên đã bị loại")
	c.Flags().BoolVar(&unverified, "unverified", false, "cho phép duyệt tác vụ chưa kiểm chứng fail→pass")
	c.Flags().StringVar(&split, "split", "", "gán tập con: build | validation | test")
	return c
}

func newEvalRejectCmd(stdio IO) *cobra.Command {
	var reason string
	c := &cobra.Command{
		Use:   "reject <id>...",
		Short: "Loại ứng viên (khai thác lại sẽ không tạo lại)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := openEval(cmd, false)
			if err != nil {
				return err
			}
			done, err := app.EvalReject(e, app.EvalRejectOptions{IDs: args, Reason: reason})
			for _, id := range done {
				fmt.Fprintf(stdio.Out, "  - %s đã loại\n", id)
			}
			return err
		},
	}
	c.Flags().StringVar(&reason, "reason", "người duyệt loại", "lý do")
	return c
}

// evalSelectionFlags gắn các cờ chọn tác vụ dùng chung cho estimate, run và bootstrap.
func evalSelectionFlags(c *cobra.Command, s *app.EvalSelection) {
	c.Flags().StringVar(&s.Split, "split", "", "chỉ tác vụ thuộc tập build | validation | test")
	c.Flags().StringSliceVar(&s.CaseIDs, "case", nil, "chỉ các tác vụ này (ID hoặc tiền tố, lặp lại được)")
	c.Flags().IntVar(&s.Limit, "limit", 0, "số tác vụ tối đa (0 = tất cả)")
}

func newEvalEstimateCmd(stdio IO) *cobra.Command {
	var (
		sel        app.EvalSelection
		arms       []string
		repeats    int
		effect     float64
		costPerRun float64
		model      string
		harness    string
		asJSON     bool
	)
	c := &cobra.Command{
		Use:   "estimate",
		Short: "Ước tính số lượt, chi phí và cỡ mẫu cần thiết trước khi chạy",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := openEval(cmd, true)
			if err != nil {
				return err
			}
			defer e.Close()
			opts := app.EvalEstimateDefaults(e.WS.Config)
			opts.EvalSelection = sel
			if cmd.Flags().Changed("arms") {
				opts.Arms, opts.ArmsExplicit = arms, true
			}
			if cmd.Flags().Changed("repeats") {
				opts.Repeats = repeats
			}
			if cmd.Flags().Changed("cost-per-run") {
				opts.CostPerRun = &costPerRun
			}
			opts.Effect, opts.Model, opts.Harness, opts.Log = effect, model, harness, stdio.Err
			res, err := app.EvalEstimate(cmd.Context(), e, opts)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(stdio.Out, res.Estimate)
			}
			printEstimate(stdio.Out, res.Estimate, res.Arms, res.Repeats, res.BudgetUSD)
			return nil
		},
	}
	evalSelectionFlags(c, &sel)
	c.Flags().StringSliceVar(&arms, "arms", nil, "nhánh (mặc định eval.arms)")
	c.Flags().IntVar(&repeats, "repeats", 3, "số lần lặp mỗi nhánh (mặc định eval.repeats)")
	c.Flags().Float64Var(&effect, "effect", 0.10, "chênh lệch tỷ lệ đạt muốn phát hiện (0..1)")
	c.Flags().Float64Var(&costPerRun, "cost-per-run", 0, "chi phí ước tính mỗi lượt (USD) khi chưa có lịch sử")
	c.Flags().StringVar(&model, "model", "", "chỉ dùng lịch sử của mô hình này")
	c.Flags().StringVar(&harness, "harness", "claude", "dùng lịch sử của harness: claude | command (--agent-command)")
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func printEstimate(w io.Writer, est eval.Estimate, arms []string, repeats int, budget *float64) {
	fmt.Fprintf(w, "Lượt chạy:      %d (%d tác vụ × %d nhánh [%s] × %d lần lặp)\n",
		est.Runs, est.AvailableCases, len(arms), strings.Join(arms, ", "), repeats)
	if est.CostPerRun != nil {
		fmt.Fprintf(w, "Chi phí/lượt:   $%.3f (%s)\n", *est.CostPerRun, est.CostSource)
		fmt.Fprintf(w, "Tổng chi phí:   ~$%.2f\n", *est.TotalCost)
	} else {
		fmt.Fprintf(w, "Chi phí:        chưa biết (%s) — dùng --cost-per-run để ước tính\n", est.CostSource)
	}
	if budget != nil {
		fmt.Fprintf(w, "Ngân sách:      $%.2f (eval.budget_usd)\n", *budget)
	}
	if est.SecondsPerRun != nil {
		total := time.Duration(*est.SecondsPerRun*float64(est.Runs)) * time.Second
		fmt.Fprintf(w, "Thời gian:      ~%s/lượt, ~%s tuần tự\n", (time.Duration(*est.SecondsPerRun) * time.Second).Round(time.Second), total.Round(time.Minute))
	}
	fmt.Fprintf(w, "Độ lệch chuẩn:  %.2f của chênh lệch theo tác vụ (%s)\n", est.SDDiff, est.SDSource)
	fmt.Fprintf(w, "Mô hình:        σ²(R) = σ_b² + W/R; W = %.2f (%s); σ_b = %.2f (%s)\n", est.W, est.WSource, est.SigmaB, est.SigmaBSource)
	if est.NeededAtRepeat > 0 {
		fmt.Fprintf(w, "                theo mô hình, R=%d lần lặp cần ~%d tác vụ\n", repeats, est.NeededAtRepeat)
	}
	fmt.Fprintf(w, "Cỡ mẫu:         cần ~%d tác vụ để phát hiện chênh lệch %.0f điểm %% (alpha=0.05, power=0.8); hiện có %d\n",
		est.NeededCases, 100*est.Effect, est.AvailableCases)
	if est.NeededCases > est.AvailableCases {
		fmt.Fprintln(w, "                → tập hiện tại chưa đủ để phát hiện mức chênh lệch này; kết quả sẽ có khoảng tin cậy rộng.")
	}
}

func newEvalRunCmd(stdio IO) *cobra.Command {
	var (
		sel          app.EvalSelection
		arms         []string
		repeats      int
		budget       float64
		costPerRun   float64
		agentCommand string
		harnessName  string
		model        string
		bare         bool
		parallel     int
		seed         uint64
		keep         bool
		resume       string
		yes          bool
		leakage      string
	)
	c := &cobra.Command{
		Use:   "run",
		Short: "Chạy các nhánh so sánh × nhiều lần lặp trên tác vụ đã duyệt",
		Long: `Mỗi lượt chạy trong một repository tạm chỉ chứa lịch sử tới commit gốc (agent không
thể đọc commit lời giải), đã gỡ mọi tệp ngữ cảnh agent (AGENTS.md, CLAUDE.md, .claude/,
.bint/...). Nhánh "none" giữ nguyên như vậy; nhánh "minimal" thêm AGENTS.md dựng từ
tri thức (eval.minimal_source: knowledge; strict dùng ảnh chụp của ` + "`bint eval bootstrap`" + `
hoặc tri thức đã commit tại commit gốc) hoặc từ tệp eval.minimal_context (file). Sau khi agent làm xong, tệp kiểm thử của commit được áp vào và
eval.test_command quyết định đạt/không.

Ngân sách là bắt buộc với harness có chi phí (--budget hoặc eval.budget_usd): Bint từ
chối chạy nếu ước tính vượt ngân sách, và dừng giữa chừng khi chi phí thực tế chạm
ngân sách. Ctrl-C dừng an toàn; chạy tiếp bằng --resume <batch-id>.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			e, err := openEval(cmd, true)
			if err != nil {
				return err
			}
			defer e.Close()
			w := stdio.Out
			opts := app.EvalRunDefaults(e.WS.Config)
			opts.EvalSelection = sel
			if cmd.Flags().Changed("arms") {
				opts.Arms, opts.ArmsExplicit = arms, true
			}
			if cmd.Flags().Changed("repeats") {
				opts.Repeats = repeats
			}
			if cmd.Flags().Changed("budget") {
				opts.BudgetUSD, opts.BudgetExplicit = &budget, true
			}
			if cmd.Flags().Changed("cost-per-run") {
				opts.CostPerRun = &costPerRun
			}
			if harnessName != "" {
				opts.Harness = harnessName
			}
			if leakage != "" {
				opts.LeakageMode = leakage
			}
			opts.AgentCommand, opts.Model, opts.Bare, opts.Parallel, opts.Seed = agentCommand, model, bare, parallel, seed
			opts.KeepWorkdirs, opts.Resume, opts.Yes, opts.BintVersion = keep, resume, yes, Version
			opts.Log = w
			if isTerminal(stdio.In) {
				opts.Confirm = func(req app.ConfirmRequest) bool { return confirm(stdio, req.Text) }
			}
			opts.OnResume = func(b eval.Batch) {
				fmt.Fprintf(w, "Tiếp tục đợt %s (%s, nhánh %s × %d lần lặp, %d tác vụ)\n", b.BatchID, b.Status,
					strings.Join(b.Arms, ","), b.Repeats, len(b.CaseIDs))
			}
			opts.OnEstimate = func(est eval.Estimate, armList []string, reps int, bud *float64) {
				printEstimate(w, est, armList, reps, bud)
			}
			opts.OnNotes = func(notes []string, reportsCost bool) {
				if !reportsCost {
					fmt.Fprintln(w, "Lưu ý: harness này không báo chi phí nên không áp được ngân sách.")
				}
				for _, n := range notes {
					fmt.Fprintf(w, "Lưu ý: %s\n", n)
				}
			}
			opts.Progress = func(r eval.AttemptRow, n, total int) {
				fmt.Fprintf(w, "[%d/%d] %s %-7s #%d  %-8s%s\n", n, total, r.CaseID, r.Arm, r.Repeat, r.Status, attemptExtras(r))
			}
			res, err := app.EvalRun(ctx, e, opts)
			if res != nil && res.BatchID != "" {
				fmt.Fprintf(w, "\nĐợt %s: %s, %d/%d lượt", res.BatchID, res.Status, res.Done, res.Total)
				if res.ReportsCost {
					fmt.Fprintf(w, ", đã tiêu $%.2f", res.SpentUSD)
				}
				fmt.Fprintln(w)
				if res.Status == "completed" {
					fmt.Fprintf(w, "Xem báo cáo: bint eval report %s\n", res.BatchID)
				} else if res.Status == "cancelled" {
					fmt.Fprintf(w, "Chạy tiếp: bint eval run --resume %s\n", res.BatchID)
				}
			}
			if errors.Is(err, context.Canceled) {
				return withCode(ExitError, fmt.Errorf("đã dừng theo yêu cầu"))
			}
			return err
		},
	}
	evalSelectionFlags(c, &sel)
	f := c.Flags()
	f.StringSliceVar(&arms, "arms", nil, "nhánh: none, minimal, bint, ideal (ideal chỉ để chẩn đoán; mặc định eval.arms)")
	f.IntVar(&repeats, "repeats", 3, "số lần lặp mỗi nhánh (mặc định eval.repeats)")
	f.Float64Var(&budget, "budget", 0, "ngân sách USD cho đợt này (mặc định eval.budget_usd)")
	f.Float64Var(&costPerRun, "cost-per-run", 0, "chi phí ước tính mỗi lượt khi chưa có lịch sử")
	f.StringVar(&agentCommand, "agent-command", "", "dùng lệnh tuỳ chỉnh làm agent thay cho harness (đề bài qua stdin)")
	f.StringVar(&harnessName, "harness", "", "harness chạy tác vụ: claude | codex | antigravity (mặc định eval.harness); nhánh harness không hỗ trợ được bỏ qua kèm lý do")
	f.StringVar(&model, "model", "", "mô hình (mặc định eval.agent.model hoặc mặc định của harness)")
	f.BoolVar(&bare, "bare", false, "claude --bare: không nạp CLAUDE.md/bộ nhớ cấp người dùng (cần ANTHROPIC_API_KEY)")
	f.IntVarP(&parallel, "parallel", "j", 1, "số lượt chạy song song")
	f.Uint64Var(&seed, "seed", 0, "hạt giống xáo trộn thứ tự lượt (mặc định ngẫu nhiên, được ghi lại)")
	f.BoolVar(&keep, "keep-workdirs", false, "giữ thư mục làm việc sau mỗi lượt (để điều tra)")
	f.StringVar(&resume, "resume", "", "chạy tiếp các lượt còn thiếu của một đợt")
	f.BoolVarP(&yes, "yes", "y", false, "không hỏi xác nhận")
	f.StringVar(&leakage, "leakage-mode", "", "strict | economy (mặc định eval.leakage_mode)")
	return c
}

func attemptExtras(r eval.AttemptRow) string {
	var parts []string
	if r.AgentDurationMS != nil {
		parts = append(parts, (time.Duration(*r.AgentDurationMS) * time.Millisecond).Round(time.Second).String())
	}
	if r.CostUSD != nil {
		parts = append(parts, fmt.Sprintf("$%.3f", *r.CostUSD))
	}
	if r.NumTurns != nil {
		parts = append(parts, fmt.Sprintf("%d lượt", *r.NumTurns))
	}
	if r.Error != "" && r.Status != "passed" && r.Status != "failed" {
		parts = append(parts, truncate(r.Error, 100))
	}
	if len(parts) == 0 {
		return ""
	}
	return "  " + strings.Join(parts, "  ")
}

func newEvalReportCmd(stdio IO) *cobra.Command {
	var (
		baseline string
		asJSON   bool
		list     bool
		perCase  bool
	)
	c := &cobra.Command{
		Use:   "report [batch-id]",
		Short: "Báo cáo một đợt: tỷ lệ đạt, so sánh theo cặp kèm khoảng tin cậy, độ dao động",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := openEval(cmd, true)
			if err != nil {
				return err
			}
			defer e.Close()
			w := stdio.Out
			opts := app.EvalReportOptions{Baseline: baseline, List: list}
			if len(args) == 1 {
				opts.BatchID = args[0]
			}
			res, err := app.EvalReport(cmd.Context(), e, opts)
			if err != nil {
				return err
			}
			if list {
				if asJSON {
					return writeJSON(w, res.Batches)
				}
				tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
				fmt.Fprintln(tw, "BATCH\tTRẠNG THÁI\tHARNESS\tNHÁNH\tLẶP\tTÁC VỤ\tCHỐNG RÒ RỈ\tBẮT ĐẦU")
				for _, b := range res.Batches {
					fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%d\t%s\t%s\n", b.BatchID, b.Status, b.Harness, strings.Join(b.Arms, ","),
						b.Repeats, len(b.CaseIDs), b.LeakageMode, fmtISO(b.StartedAt))
				}
				return tw.Flush()
			}
			if asJSON {
				return writeJSON(w, res.Report)
			}
			printReport(w, res.Report, perCase)
			return nil
		},
	}
	c.Flags().StringVar(&baseline, "baseline", "", "nhánh làm mốc so sánh (mặc định none)")
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	c.Flags().BoolVar(&list, "list", false, "liệt kê các đợt")
	c.Flags().BoolVar(&perCase, "cases", false, "in kết quả theo từng tác vụ")
	return c
}

func fmtISO(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.Local().Format("2006-01-02 15:04")
}

func pct(v float64) string {
	if math.IsNaN(v) {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", 100*v)
}

func ciStr(c eval.CI, scale float64, format string) string {
	if !c.Valid() {
		return "[-]"
	}
	return fmt.Sprintf("["+format+", "+format+"]", c.Lo*scale, c.Hi*scale)
}

func statStr(s *eval.Stat, format string) string {
	if s == nil {
		return "-"
	}
	return fmt.Sprintf(format+" %s", s.Mean, ciStr(s.CI, 1, format))
}

func printReport(w io.Writer, rep eval.Report, perCase bool) {
	b := rep.Batch
	fmt.Fprintf(w, "Đợt %s — %s\n", b.BatchID, b.Status)
	fmt.Fprintf(w, "  Harness %s %s, mô hình yêu cầu %s, chống rò rỉ %s, %d tác vụ × %d lần lặp, seed %d\n",
		b.Harness, orDash(b.HarnessVersion), orDash(b.RequestedModel), b.LeakageMode, len(b.CaseIDs), b.Repeats, b.Seed)
	fmt.Fprintf(w, "  Bắt đầu %s, kết thúc %s, git %s\n\n", fmtISO(b.StartedAt), fmtISO(b.FinishedAt), orDash(short(b.GitHead)))

	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "NHÁNH\tĐẠT (CI95)\tCHẤM/LƯỢT\tLỖI/BỎ/QUÁ GIỜ\tCHI PHÍ $\tTHỜI GIAN s\tLƯỢT AGENT\tDAO ĐỘNG\tMÔ HÌNH")
	for _, a := range rep.Arms {
		varStr := "-"
		if a.WithinCaseSD != nil {
			varStr = fmt.Sprintf("sd %.2f, %d/%d tác vụ không nhất quán", *a.WithinCaseSD, a.MixedCases, a.CasesWithRepeats)
		}
		fmt.Fprintf(tw, "%s\t%s %s\t%d/%d\t%d/%d/%d\t%s\t%s\t%s\t%s\t%s\n", a.Arm, pct(a.PassRate.Mean), ciStr(a.PassRate.CI, 100, "%.0f"),
			a.Graded, a.Attempts, a.Errors, a.Skipped, a.Timeouts, statStr(a.Cost, "%.3f"), statStr(a.Duration, "%.0f"),
			statStr(a.Turns, "%.1f"), varStr, orDash(strings.Join(a.Models, ",")))
	}
	tw.Flush()

	if len(rep.Paired) > 0 {
		fmt.Fprintln(w, "\nSo sánh theo cặp (cùng tác vụ; khoảng tin cậy 95% bootstrap theo tác vụ):")
		tw = tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "NHÁNH − MỐC\tTÁC VỤ\tΔ ĐẠT (điểm %)\tΔ CHI PHÍ $\tΔ THỜI GIAN s\tΔ LƯỢT AGENT")
		for _, p := range rep.Paired {
			fmt.Fprintf(tw, "%s − %s\t%d\t%+.1f %s\t%s\t%s\t%s\n", p.Arm, p.Baseline, p.PassDiff.N, 100*p.PassDiff.Mean,
				ciStr(p.PassDiff.CI, 100, "%+.1f"), statStr(p.CostDiff, "%+.3f"), statStr(p.TimeDiff, "%+.0f"), statStr(p.TurnDiff, "%+.1f"))
		}
		tw.Flush()
	}
	if perCase {
		fmt.Fprintln(w, "\nTheo tác vụ (đạt/chấm được):")
		tw = tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
		header := "TÁC VỤ\tTẬP"
		for _, a := range rep.Arms {
			header += "\t" + strings.ToUpper(a.Arm)
		}
		fmt.Fprintln(tw, header)
		for _, c := range rep.Cases {
			line := c.CaseID + "\t" + orDash(c.Split)
			for _, a := range rep.Arms {
				cell := c.Arms[a.Arm]
				s := fmt.Sprintf("%d/%d", cell.Passed, cell.Graded)
				if cell.Other > 0 {
					s += fmt.Sprintf(" (+%d lỗi/bỏ)", cell.Other)
				}
				line += "\t" + s
			}
			fmt.Fprintln(tw, line)
		}
		tw.Flush()
	}
	if len(rep.Guardrails) > 0 {
		fmt.Fprintln(w, "\nGuardrail của quy trình (số lượt; mẫu số = lượt có guardrail hiệu lực):")
		tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "  NHÁNH\tGUARDRAIL\tLƯỢT\tĐỦ ĐK\tKÍCH HOẠT\tLÀM THEO\tBỊ CHẶN\tVƯỢT TRẦN")
		for _, g := range rep.Guardrails {
			fmt.Fprintf(tw, "  %s\t%s\t%d\t%d\t%d (%d lần)\t%d\t%d\t%d\n", g.Arm, g.Guardrail, g.Attempts, g.Eligible, g.Fired, g.FiredCount, g.Followed, g.Blocked, g.Bypass)
		}
		tw.Flush()
	}
	fmt.Fprintln(w, "\nĐộ tin cậy của phép đo:")
	if len(rep.Diagnostics) == 0 {
		fmt.Fprintln(w, "  ✓ không có cảnh báo")
	} else {
		for _, d := range rep.Diagnostics {
			icon := "i"
			if d.Level == "warn" {
				icon = "!"
			}
			fmt.Fprintf(w, "  %s %s\n", icon, d.Detail)
		}
	}
	if g := rep.Gate2; g != nil {
		fmt.Fprintln(w, "\nChẩn đoán cổng quyết định 2 (mục 12.9; chỉ dùng tập validation để chẩn đoán):")
		for _, c := range g.Checks {
			fmt.Fprintf(w, "  · %s\n", c)
		}
		fmt.Fprintf(w, "  → %s: %s\n", g.Cause, g.Detail)
		for _, a := range rep.Arms {
			if a.Arm == eval.ArmIdeal {
				fmt.Fprintln(w, "  Nhánh ideal là cận trên chẩn đoán (lộ gián tiếp vị trí cần sửa), không phải kết quả của Bint.")
				break
			}
		}
	}
}

func short(s string) string {
	if len(s) > 10 {
		return s[:10]
	}
	return s
}

func newEvalBootstrapCmd(stdio IO) *cobra.Command {
	var (
		sel          app.EvalSelection
		budget       float64
		agentCommand string
		model        string
		force        bool
		keep         bool
		list         bool
		yes          bool
	)
	c := &cobra.Command{
		Use:   "bootstrap",
		Short: "Dựng tri thức tại commit gốc của từng tác vụ (b-bootstrap) cho nhánh minimal ở chế độ strict",
		Long: `Chế độ strict (mục 12.3) yêu cầu tri thức của nhánh minimal được dựng TẠI commit gốc
của tác vụ, không phải từ trạng thái hiện tại (vốn đã chứa lời giải). Lệnh này chạy
skill b-bootstrap bằng agent trong một repository cô lập tại mỗi commit gốc (chỉ có
lịch sử tới đó, đã gỡ mọi tệp ngữ cảnh agent) và lưu các mục agent ghi được vào
<state>/eval-work/knowledge/<commit>/. ` + "`bint eval run`" + ` dùng ảnh chụp này cho nhánh minimal.

Các mục trong ảnh chụp là đầu ra tự động, chưa được người duyệt — đó chính là thứ
cổng quyết định 1 đo. Commit gốc đã có ảnh chụp được bỏ qua (--force để làm lại).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			e, err := openEval(cmd, true)
			if err != nil {
				return err
			}
			defer e.Close()
			w := stdio.Out
			opts := app.EvalBootstrapDefaults(e.WS.Config)
			opts.EvalSelection = sel
			if cmd.Flags().Changed("budget") {
				opts.BudgetUSD = &budget
			}
			opts.AgentCommand, opts.Model, opts.Force, opts.KeepWorkdirs = agentCommand, model, force, keep
			opts.List, opts.Yes, opts.BintVersion = list, yes, Version
			if isTerminal(stdio.In) {
				opts.Confirm = func(req app.ConfirmRequest) bool { return confirm(stdio, req.Text) }
			}
			opts.OnPlan = func(cases, bases, todo int) {
				fmt.Fprintf(w, "%d tác vụ, %d commit gốc, %d cần dựng tri thức\n", cases, bases, todo)
			}
			opts.OnStep = func(st app.EvalBootstrapStep) {
				if st.Stopped {
					fmt.Fprintf(w, "Dừng: đã tiêu $%.2f, chạm ngân sách $%.2f; %d commit gốc còn lại\n", st.Spent, st.Budget, st.RemainingBases)
					return
				}
				cost := ""
				if st.Meta.CostUSD != nil {
					cost = fmt.Sprintf(" $%.2f", *st.Meta.CostUSD)
				}
				if st.Err != nil {
					fmt.Fprintf(w, "[%d/%d] %s ✗ %v%s\n", st.Index, st.Total, short(st.Base), st.Err, cost)
					return
				}
				fmt.Fprintf(w, "[%d/%d] %s ✓ %d mục%s %s\n", st.Index, st.Total, short(st.Base), st.Meta.Items, cost, st.Meta.Error)
			}
			res, err := app.EvalBootstrap(ctx, e, opts)
			if err != nil {
				return err
			}
			if list {
				tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
				fmt.Fprintln(tw, "COMMIT GỐC\tẢNH CHỤP\tMỤC\tCHI PHÍ\tMÔ HÌNH\tTẠO LÚC")
				for _, sn := range res.Snapshots {
					switch {
					case sn.Err != nil:
						fmt.Fprintf(tw, "%s\tlỗi: %v\t\t\t\t\n", short(sn.Base), sn.Err)
					case !sn.Has:
						fmt.Fprintf(tw, "%s\t-\t\t\t\t\n", short(sn.Base))
					default:
						cost := "-"
						if sn.Meta.CostUSD != nil {
							cost = fmt.Sprintf("$%.2f", *sn.Meta.CostUSD)
						}
						fmt.Fprintf(tw, "%s\tcó\t%d\t%s\t%s\t%s\n", short(sn.Base), sn.Meta.Items, cost, sn.Meta.Model, fmtISO(sn.Meta.CreatedAt))
					}
				}
				return tw.Flush()
			}
			if res.Todo == 0 {
				return nil
			}
			if res.ReportsCost {
				fmt.Fprintf(w, "Tổng chi phí: $%.2f\n", res.Spent)
			}
			if res.Failed > 0 {
				return withCode(ExitError, fmt.Errorf("%d commit gốc không dựng được tri thức", res.Failed))
			}
			return nil
		},
	}
	evalSelectionFlags(c, &sel)
	f := c.Flags()
	f.Float64Var(&budget, "budget", 0, "ngân sách USD (mặc định eval.budget_usd)")
	f.StringVar(&agentCommand, "agent-command", "", "dùng lệnh tuỳ chỉnh làm agent (đề bài qua stdin; BINT_AGENT=1 và bint trong PATH)")
	f.StringVar(&model, "model", "", "mô hình")
	f.BoolVar(&force, "force", false, "dựng lại cả commit gốc đã có ảnh chụp")
	f.BoolVar(&keep, "keep-workdirs", false, "giữ thư mục làm việc để điều tra")
	f.BoolVar(&list, "list", false, "chỉ liệt kê ảnh chụp hiện có")
	f.BoolVarP(&yes, "yes", "y", false, "không hỏi xác nhận")
	return c
}

func newEvalCleanCmd(stdio IO) *cobra.Command {
	return &cobra.Command{
		Use:   "clean",
		Short: "Xoá kho đệm commit gốc (<eval-work>/bases) để giải phóng đĩa",
		Long: `Xoá toàn bộ kho đệm commit gốc của bint eval. Kho chỉ là bộ nhớ tạm và được nạp lại
khi cần; thư mục làm việc, kết quả và nhật ký không bị đụng tới. Bình thường kho đã được dọn
sau mỗi đợt (trừ khi eval.keep_bases bật); lệnh này dành cho kho còn sót sau khi bị ngắt giữa chừng.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := openEval(cmd, false)
			if err != nil {
				return err
			}
			defer e.Close()
			res, err := app.EvalClean(e, app.EvalCleanOptions{})
			fmt.Fprintf(stdio.Out, "Đã xoá %d kho đệm, giải phóng %.1f MB.\n", res.Removed, float64(res.Freed)/(1<<20))
			return err
		},
	}
}
