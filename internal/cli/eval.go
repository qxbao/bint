package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"bint/internal/app"
	"bint/internal/eval"
	"bint/internal/store"
)

type evalCtx struct {
	ws    *app.Workspace
	st    *store.Store
	cases eval.Store
	env   eval.Env
}

func (e *evalCtx) Close() {
	if e.st != nil {
		e.st.Close()
	}
}

func openEval(cmd *cobra.Command, withDB bool) (*evalCtx, error) {
	dir, err := cwd()
	if err != nil {
		return nil, err
	}
	ws, err := app.OpenWorkspace(cmd.Context(), dir, true)
	if err != nil {
		return nil, err
	}
	bintDir := filepath.Dir(ws.Sources.Project)
	e := &evalCtx{
		ws:    ws,
		cases: eval.NewStore(ws.Paths.Root, bintDir),
		env:   eval.Env{RepoDir: ws.Repo.Toplevel, WorkRoot: ws.Paths.EvalWork, BintRoot: filepath.Dir(bintDir), Cfg: ws.Config},
	}
	if withDB {
		if e.st, err = ws.OpenStore(cmd.Context()); err != nil {
			return nil, err
		}
	}
	return e, nil
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
	c.AddCommand(newEvalMineCmd(stdio), newEvalVerifyCmd(stdio), newEvalListCmd(stdio), newEvalShowCmd(stdio),
		newEvalApproveCmd(stdio), newEvalRejectCmd(stdio), newEvalEstimateCmd(stdio), newEvalRunCmd(stdio),
		newEvalReportCmd(stdio), newEvalBootstrapCmd(stdio))
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
thất bại tại commit cha và qua với lời giải thật; ứng viên không đạt bị loại.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			e, err := openEval(cmd, false)
			if err != nil {
				return err
			}
			if verify {
				if _, err := e.env.TestCommand(&eval.Case{}); err != nil {
					return err
				}
			}
			res, err := eval.Mine(ctx, e.ws.Repo.Toplevel, e.ws.Config.Eval.Mine, e.cases, eval.MineOptions{Rev: rev, Limit: limit})
			if err != nil {
				return err
			}
			w := stdio.Out
			if verify {
				for i, c := range res.New {
					if !asJSON {
						fmt.Fprintf(w, "[%d/%d] kiểm chứng %s %s ... ", i+1, len(res.New), c.ID, truncate(c.Source.Subject, 50))
					}
					applyVerification(ctx, e, c, verifyRuns)
					if err := e.cases.SaveCandidate(c); err != nil {
						return err
					}
					if !asJSON {
						fmt.Fprintln(w, verifyLabel(c))
					}
					if ctx.Err() != nil {
						return ctx.Err()
					}
				}
			}
			if asJSON {
				return writeJSON(w, map[string]any{"scanned": res.Scanned, "new": res.New, "skipped": res.Skipped})
			}
			fmt.Fprintf(w, "Đã quét %d commit, %d ứng viên mới.\n", res.Scanned, len(res.New))
			if len(res.Skipped) > 0 {
				fmt.Fprintln(w, "Bỏ qua:")
				for _, k := range sortedMapKeys(res.Skipped) {
					fmt.Fprintf(w, "  %5d  %s\n", res.Skipped[k], k)
				}
			}
			if len(res.New) > 0 {
				fmt.Fprintln(w)
				printCaseTable(w, res.New)
				fmt.Fprintf(w, "\nỨng viên lưu tại %s\n", e.cases.CandidatesDir)
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
	f.IntVar(&verifyRuns, "verify-runs", 2, "số lần chạy kiểm thử với lời giải thật (bắt kiểm thử chập chờn)")
	f.BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

// applyVerification chạy kiểm chứng và cập nhật trạng thái tác vụ: ứng viên không
// đạt fail→pass bị loại; lỗi hạ tầng giữ nguyên trạng thái để chạy lại.
func applyVerification(ctx context.Context, e *evalCtx, c *eval.Case, runs int) {
	c.Verification = eval.VerifyCase(ctx, e.env, c, runs, time.Now())
	if c.Verification.Status == "failed" && c.Status == eval.StatusCandidate {
		c.Status = eval.StatusRejected
		c.RejectReason = "kiểm chứng: " + c.Verification.Detail
	}
}

func printCaseTable(w io.Writer, cs []*eval.Case) {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tTRẠNG THÁI\tTẬP\tNGÀY\tMÃ/TEST (dòng)\tRÒ RỈ\tFAIL→PASS\tTIÊU ĐỀ")
	for _, c := range cs {
		date := c.Source.Date
		if len(date) >= 10 {
			date = date[:10]
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d/%d\t%s\t%s\t%s\n", c.ID, c.Status, orDash(c.Split), date,
			c.Stats.SourceLines, c.Stats.TestLines, c.Leak.Status, verifyLabel(c), truncate(c.Source.Subject, 60))
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
			if _, err := e.env.TestCommand(&eval.Case{}); err != nil {
				return err
			}
			type target struct {
				c        *eval.Case
				approved bool
			}
			var targets []target
			if all {
				if len(args) > 0 {
					return usageError{fmt.Errorf("--all không đi cùng danh sách ID")}
				}
				cands, err := e.cases.Candidates()
				if err != nil {
					return err
				}
				for _, c := range cands {
					if c.Status == eval.StatusCandidate && !c.Verified() {
						targets = append(targets, target{c, false})
					}
				}
				cases, err := e.cases.Cases()
				if err != nil {
					return err
				}
				for _, c := range cases {
					if !c.Verified() {
						targets = append(targets, target{c, true})
					}
				}
			} else {
				if len(args) == 0 {
					return usageError{fmt.Errorf("cần ID tác vụ hoặc --all")}
				}
				for _, id := range args {
					c, approved, err := e.cases.Get(id)
					if err != nil {
						return err
					}
					targets = append(targets, target{c, approved})
				}
			}
			if len(targets) == 0 {
				fmt.Fprintln(stdio.Out, "Không có tác vụ nào cần kiểm chứng.")
				return nil
			}
			failed := 0
			for i, t := range targets {
				fmt.Fprintf(stdio.Out, "[%d/%d] %s %s ... ", i+1, len(targets), t.c.ID, truncate(t.c.Source.Subject, 50))
				applyVerification(ctx, e, t.c, runs)
				var err error
				if t.approved {
					err = e.cases.SaveCase(t.c)
				} else {
					err = e.cases.SaveCandidate(t.c)
				}
				if err != nil {
					return err
				}
				fmt.Fprintln(stdio.Out, verifyLabel(t.c))
				if !t.c.Verified() {
					failed++
					fmt.Fprintf(stdio.Out, "      %s\n", t.c.Verification.Detail)
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
			}
			if failed > 0 {
				return withCode(ExitError, fmt.Errorf("%d/%d tác vụ không qua kiểm chứng", failed, len(targets)))
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
			cases, err := e.cases.Cases()
			if err != nil {
				return err
			}
			cands, err := e.cases.Candidates()
			if err != nil {
				return err
			}
			var pending []*eval.Case
			for _, c := range cands {
				if c.Status == eval.StatusCandidate || (all && c.Status == eval.StatusRejected) {
					pending = append(pending, c)
				}
			}
			if split != "" {
				cases = filterSplit(cases, split)
			}
			if asJSON {
				return writeJSON(stdio.Out, map[string]any{"cases": nonNilCases(cases), "candidates": nonNilCases(pending)})
			}
			w := stdio.Out
			fmt.Fprintf(w, "Tác vụ đã duyệt (%d) — %s\n", len(cases), e.cases.CasesDir)
			if len(cases) > 0 {
				printCaseTable(w, cases)
				counts := map[string]int{}
				for _, c := range cases {
					counts[c.Split]++
				}
				fmt.Fprintf(w, "Theo tập: build=%d validation=%d test=%d\n", counts[eval.SplitBuild], counts[eval.SplitValidation], counts[eval.SplitTest])
			}
			fmt.Fprintf(w, "\nỨng viên chờ duyệt (%d) — %s\n", len(pending), e.cases.CandidatesDir)
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

func filterSplit(cs []*eval.Case, split string) []*eval.Case {
	var out []*eval.Case
	for _, c := range cs {
		if c.Split == split {
			out = append(out, c)
		}
	}
	return out
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
			c, approved, err := e.cases.Get(args[0])
			if err != nil {
				return err
			}
			// Kiểm tra lại rò rỉ trên đề bài hiện tại (có thể vừa được sửa tay).
			if in, err := eval.BuildLeakInput(cmd.Context(), e.ws.Repo.Toplevel, c); err == nil {
				c.Leak = eval.CheckLeak(c.Prompt, in)
			}
			w := stdio.Out
			path := e.cases.CandidatePath(c.ID)
			if approved {
				path = e.cases.CasePath(c.ID)
			}
			fmt.Fprintf(w, "Tác vụ:      %s (%s)\n", c.ID, c.Status)
			fmt.Fprintf(w, "Tệp:         %s\n", path)
			fmt.Fprintf(w, "Commit:      %s (gốc %s)\n", c.Source.Commit, c.Source.Base)
			fmt.Fprintf(w, "Tác giả:     %s, %s\n", c.Source.Author, c.Source.Date)
			fmt.Fprintf(w, "Tập:         %s\n", orDash(c.Split))
			fmt.Fprintf(w, "Quy mô:      %d tệp, %d dòng mã nguồn, %d dòng kiểm thử\n", c.Stats.Files, c.Stats.SourceLines, c.Stats.TestLines)
			fmt.Fprintf(w, "Mã nguồn:    %s\n", strings.Join(c.SourceFiles, ", "))
			fmt.Fprintf(w, "Kiểm thử:    %s\n", strings.Join(c.TestFiles, ", "))
			if cmdStr, err := e.env.TestCommand(c); err == nil {
				fmt.Fprintf(w, "Lệnh chấm:   %s\n", cmdStr)
			} else {
				fmt.Fprintf(w, "Lệnh chấm:   (chưa có) %v\n", err)
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
			fmt.Fprintf(w, "\n--- Đề bài ---\n%s\n", c.Prompt)
			if c.OriginalMessage != "" && c.OriginalMessage != c.Prompt {
				fmt.Fprintf(w, "\n--- Thông điệp commit gốc ---\n%s\n", c.OriginalMessage)
			}
			return nil
		},
	}
}

func reviewer(ctx context.Context, dir string) string { return app.GitIdentity(ctx, dir) }

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
			if split != "" && !slices.Contains(eval.Splits, split) {
				return usageError{fmt.Errorf("--split phải là build | validation | test")}
			}
			e, err := openEval(cmd, false)
			if err != nil {
				return err
			}
			who := reviewer(ctx, e.ws.Repo.Toplevel)
			var problems []string
			for _, id := range args {
				c, approved, err := e.cases.Get(id)
				if err != nil {
					problems = append(problems, err.Error())
					continue
				}
				if approved {
					fmt.Fprintf(stdio.Out, "  - %s đã được duyệt trước đó\n", c.ID)
					continue
				}
				if c.Status == eval.StatusRejected && !force {
					problems = append(problems, fmt.Sprintf("%s đã bị loại (%s); dùng --force nếu vẫn muốn duyệt", c.ID, c.RejectReason))
					continue
				}
				in, err := eval.BuildLeakInput(ctx, e.ws.Repo.Toplevel, c)
				if err != nil {
					return err
				}
				c.Leak = eval.CheckLeak(c.Prompt, in)
				if c.Leak.Status == "fail" && !force {
					problems = append(problems, fmt.Sprintf("%s: đề bài lộ lời giải (%s) — sửa `prompt` trong %s hoặc dùng --force",
						c.ID, c.Leak.Findings[0].Detail, e.cases.CandidatePath(c.ID)))
					continue
				}
				if !c.Verified() && !unverified {
					problems = append(problems, fmt.Sprintf("%s: chưa qua kiểm chứng fail→pass (`bint eval verify %s`) — hoặc dùng --unverified", c.ID, c.ID))
					continue
				}
				out := *c
				out.Status = eval.StatusApproved
				out.OriginalMessage = ""
				out.ReviewedBy, out.ReviewedAt = who, time.Now().UTC().Format(time.RFC3339)
				out.RejectReason = ""
				out.Split = split
				if out.Split == "" {
					out.Split = eval.AssignSplit(c.ID)
				}
				if err := e.cases.SaveCase(&out); err != nil {
					return err
				}
				c.Status = eval.StatusApproved
				if err := e.cases.SaveCandidate(c); err != nil {
					return err
				}
				note := ""
				if c.Leak.Status != "pass" {
					note = " (rò rỉ: " + c.Leak.Status + ")"
				}
				if !c.Verified() {
					note += " (chưa kiểm chứng)"
				}
				fmt.Fprintf(stdio.Out, "  ✓ %s → %s [%s]%s\n", c.ID, e.cases.CasePath(c.ID), out.Split, note)
			}
			if len(problems) > 0 {
				for _, p := range problems {
					fmt.Fprintf(stdio.Out, "  ✗ %s\n", p)
				}
				return withCode(ExitError, fmt.Errorf("%d tác vụ chưa được duyệt", len(problems)))
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
			for _, id := range args {
				c, approved, err := e.cases.Get(id)
				if err != nil {
					return err
				}
				if approved {
					return fmt.Errorf("%s đã được duyệt; xoá %s nếu muốn bỏ khỏi tập đánh giá", c.ID, e.cases.CasePath(c.ID))
				}
				c.Status = eval.StatusRejected
				c.RejectReason = reason
				if err := e.cases.SaveCandidate(c); err != nil {
					return err
				}
				fmt.Fprintf(stdio.Out, "  - %s đã loại\n", c.ID)
			}
			return nil
		},
	}
	c.Flags().StringVar(&reason, "reason", "người duyệt loại", "lý do")
	return c
}

// selection là bộ lọc tác vụ dùng chung cho estimate và run.
type selection struct {
	split   string
	caseIDs []string
	limit   int
}

func (s *selection) flags(c *cobra.Command) {
	c.Flags().StringVar(&s.split, "split", "", "chỉ tác vụ thuộc tập build | validation | test")
	c.Flags().StringSliceVar(&s.caseIDs, "case", nil, "chỉ các tác vụ này (ID hoặc tiền tố, lặp lại được)")
	c.Flags().IntVar(&s.limit, "limit", 0, "số tác vụ tối đa (0 = tất cả)")
}

func (s *selection) pick(e *evalCtx) ([]*eval.Case, error) {
	all, err := e.cases.Cases()
	if err != nil {
		return nil, err
	}
	var out []*eval.Case
	if len(s.caseIDs) > 0 {
		for _, id := range s.caseIDs {
			c, approved, err := e.cases.Get(id)
			if err != nil {
				return nil, err
			}
			if !approved {
				return nil, fmt.Errorf("%s chưa được duyệt (`bint eval approve %s`)", c.ID, c.ID)
			}
			out = append(out, c)
		}
	} else {
		out = all
	}
	if s.split != "" {
		if !slices.Contains(eval.Splits, s.split) {
			return nil, usageError{fmt.Errorf("--split phải là build | validation | test")}
		}
		out = filterSplit(out, s.split)
	}
	slices.SortFunc(out, func(a, b *eval.Case) int { return strings.Compare(a.ID, b.ID) })
	if s.limit > 0 && len(out) > s.limit {
		out = out[:s.limit]
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("không có tác vụ đã duyệt nào phù hợp (`bint eval mine`, rồi `bint eval approve`)")
	}
	return out, nil
}

// resolveArms trả về các nhánh sẽ chạy. Nhánh lấy từ cấu hình mà chưa có trong bản
// này thì bị bỏ kèm thông báo; nhánh do người dùng chỉ định thì báo lỗi.
func resolveArms(e *evalCtx, flagArms []string, explicit bool, w io.Writer) ([]string, error) {
	arms := e.ws.Config.Eval.Arms
	if explicit {
		arms = flagArms
	}
	var out []string
	for _, a := range arms {
		a = strings.TrimSpace(a)
		if slices.Contains(out, a) {
			continue
		}
		if err := eval.ArmNotImplemented(a); err != nil {
			if explicit {
				return nil, usageError{err}
			}
			fmt.Fprintf(w, "Bỏ nhánh %s: %v\n", a, err)
			continue
		}
		if a == eval.ArmMinimal && e.ws.Config.Eval.MinimalSource == "file" {
			p := filepath.Join(e.ws.Repo.Toplevel, filepath.FromSlash(e.ws.Config.Eval.MinimalContext))
			if _, err := os.Stat(p); err != nil {
				msg := fmt.Sprintf("chưa có %s (eval.minimal_source: file); đặt minimal_source: knowledge để dựng từ tri thức", e.ws.Config.Eval.MinimalContext)
				if explicit {
					return nil, fmt.Errorf("nhánh minimal: %s", msg)
				}
				fmt.Fprintf(w, "Bỏ nhánh minimal: %s\n", msg)
				continue
			}
		}
		out = append(out, a)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("không còn nhánh nào để chạy")
	}
	return out, nil
}

func newEvalEstimateCmd(stdio IO) *cobra.Command {
	var (
		sel        selection
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
			cases, err := sel.pick(e)
			if err != nil {
				return err
			}
			armList, err := resolveArms(e, arms, cmd.Flags().Changed("arms"), stdio.Err)
			if err != nil {
				return err
			}
			if !cmd.Flags().Changed("repeats") {
				repeats = e.ws.Config.Eval.Repeats
			}
			in := eval.EstimateInput{Cases: len(cases), Arms: armList, Repeats: repeats, Harness: harness, Model: model, Effect: effect}
			if cmd.Flags().Changed("cost-per-run") {
				in.CostPerRun = &costPerRun
			}
			est, err := eval.EstimateRun(cmd.Context(), e.st.DB, in)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(stdio.Out, est)
			}
			printEstimate(stdio.Out, est, armList, repeats, e.ws.Config.Eval.BudgetUSD)
			return nil
		},
	}
	sel.flags(c)
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
	fmt.Fprintf(w, "Cỡ mẫu:         cần ~%d tác vụ để phát hiện chênh lệch %.0f điểm %% (alpha=0.05, power=0.8); hiện có %d\n",
		est.NeededCases, 100*est.Effect, est.AvailableCases)
	if est.NeededCases > est.AvailableCases {
		fmt.Fprintln(w, "                → tập hiện tại chưa đủ để phát hiện mức chênh lệch này; kết quả sẽ có khoảng tin cậy rộng.")
	}
}

func newEvalRunCmd(stdio IO) *cobra.Command {
	var (
		sel          selection
		arms         []string
		repeats      int
		budget       float64
		costPerRun   float64
		agentCommand string
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
			cfg := e.ws.Config
			w := stdio.Out

			var agent eval.Agent
			if agentCommand != "" {
				agent = &eval.CommandAgent{Command: agentCommand}
			} else {
				agent = &eval.ClaudeAgent{Cfg: cfg.Eval.Agent, Model: model, MaxCostUSD: cfg.Limits.PerRun.MaxCostUSD, Bare: bare}
			}
			opts := eval.RunOptions{
				Agent: agent, RequestedModel: model, Parallel: parallel, Seed: seed, KeepWorkdirs: keep,
				BintVersion: Version, ResumeBatch: resume,
			}
			if cmd.Flags().Changed("budget") {
				opts.BudgetUSD = &budget
			} else if cfg.Eval.BudgetUSD != nil {
				opts.BudgetUSD = cfg.Eval.BudgetUSD
			}

			allCases, err := e.cases.Cases()
			if err != nil {
				return err
			}
			if resume != "" {
				opts.Cases = allCases
				b, err := eval.GetBatch(ctx, e.st.DB, resume)
				if err != nil {
					return err
				}
				if !cmd.Flags().Changed("budget") {
					opts.BudgetUSD = b.BudgetUSD
				}
				fmt.Fprintf(w, "Tiếp tục đợt %s (%s, nhánh %s × %d lần lặp, %d tác vụ)\n", b.BatchID, b.Status,
					strings.Join(b.Arms, ","), b.Repeats, len(b.CaseIDs))
			} else {
				cases, err := sel.pick(e)
				if err != nil {
					return err
				}
				armList, err := resolveArms(e, arms, cmd.Flags().Changed("arms"), w)
				if err != nil {
					return err
				}
				if !cmd.Flags().Changed("repeats") {
					repeats = cfg.Eval.Repeats
				}
				if repeats < 1 {
					return usageError{fmt.Errorf("--repeats phải >= 1")}
				}
				mode := cfg.Eval.LeakageMode
				if leakage != "" {
					mode = leakage
				}
				if mode != eval.LeakStrict && mode != eval.LeakEconomy {
					return usageError{fmt.Errorf("--leakage-mode phải là strict | economy")}
				}
				if seed == 0 {
					seed = uint64(time.Now().UnixNano())
				}
				opts.Cases, opts.Arms, opts.Repeats, opts.LeakageMode, opts.Seed = cases, armList, repeats, mode, seed
				opts.Notes = runNotes(cases, mode, agent, bare)

				in := eval.EstimateInput{Cases: len(cases), Arms: armList, Repeats: repeats, Harness: agent.Name(), Model: model, Effect: 0.10}
				if cmd.Flags().Changed("cost-per-run") {
					in.CostPerRun = &costPerRun
				}
				est, err := eval.EstimateRun(ctx, e.st.DB, in)
				if err != nil {
					return err
				}
				printEstimate(w, est, armList, repeats, opts.BudgetUSD)
				opts.EstimatedUSD = est.TotalCost
				if agent.ReportsCost() {
					if opts.BudgetUSD == nil {
						return usageError{fmt.Errorf("cần ngân sách: --budget <USD> hoặc eval.budget_usd (mục 12.5: không chạy khi chưa có ngân sách được duyệt)")}
					}
					if est.TotalCost != nil && *est.TotalCost > *opts.BudgetUSD {
						return withCode(ExitError, fmt.Errorf("%w: ước tính $%.2f > ngân sách $%.2f; giảm --limit/--repeats/--arms hoặc tăng --budget",
							eval.ErrBudget, *est.TotalCost, *opts.BudgetUSD))
					}
				} else {
					fmt.Fprintln(w, "Lưu ý: harness này không báo chi phí nên không áp được ngân sách.")
				}
				for _, n := range opts.Notes {
					fmt.Fprintf(w, "Lưu ý: %s\n", n)
				}
			}
			if !yes {
				if !isTerminal(stdio.In) {
					return usageError{fmt.Errorf("cần --yes khi chạy không tương tác")}
				}
				if !confirm(stdio, "Bắt đầu chạy?") {
					return fmt.Errorf("đã huỷ")
				}
			}
			opts.Progress = func(r eval.AttemptRow, n, total int) {
				fmt.Fprintf(w, "[%d/%d] %s %-7s #%d  %-8s%s\n", n, total, r.CaseID, r.Arm, r.Repeat, r.Status, attemptExtras(r))
			}
			res, err := eval.Run(ctx, e.env, e.st.DB, opts)
			if res.BatchID != "" {
				fmt.Fprintf(w, "\nĐợt %s: %s, %d/%d lượt", res.BatchID, res.Status, res.Done, res.Total)
				if agent.ReportsCost() {
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
	sel.flags(c)
	f := c.Flags()
	f.StringSliceVar(&arms, "arms", nil, "nhánh: none, minimal (mặc định eval.arms, bỏ nhánh chưa có)")
	f.IntVar(&repeats, "repeats", 3, "số lần lặp mỗi nhánh (mặc định eval.repeats)")
	f.Float64Var(&budget, "budget", 0, "ngân sách USD cho đợt này (mặc định eval.budget_usd)")
	f.Float64Var(&costPerRun, "cost-per-run", 0, "chi phí ước tính mỗi lượt khi chưa có lịch sử")
	f.StringVar(&agentCommand, "agent-command", "", "dùng lệnh tuỳ chỉnh làm agent thay cho Claude Code (đề bài qua stdin)")
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

func runNotes(cases []*eval.Case, mode string, agent eval.Agent, bare bool) []string {
	var notes []string
	if mode == eval.LeakEconomy {
		notes = append(notes, "chế độ economy: kết quả chỉ dùng để sàng lọc, không dùng cho cổng quyết định")
	}
	unv := 0
	for _, c := range cases {
		if !c.Verified() {
			unv++
		}
	}
	if unv > 0 {
		notes = append(notes, fmt.Sprintf("%d tác vụ chưa kiểm chứng fail→pass", unv))
	}
	if agent.Name() == "claude" && !bare {
		notes = append(notes, "CLAUDE.md và bộ nhớ cấp người dùng (~/.claude) vẫn có thể được nạp ở mọi nhánh; dùng --bare để cô lập hoàn toàn")
	}
	notes = append(notes, "agent chạy với quyền của người dùng, mạng không bị chặn ở tầng hệ điều hành (security.allow_network_in_evals chỉ là chỉ dẫn ở P1)")
	return notes
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
			ctx := cmd.Context()
			e, err := openEval(cmd, true)
			if err != nil {
				return err
			}
			defer e.Close()
			w := stdio.Out
			if list {
				bs, err := eval.ListBatches(ctx, e.st.DB, 50)
				if err != nil {
					return err
				}
				if asJSON {
					return writeJSON(w, bs)
				}
				tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
				fmt.Fprintln(tw, "BATCH\tTRẠNG THÁI\tHARNESS\tNHÁNH\tLẶP\tTÁC VỤ\tCHỐNG RÒ RỈ\tBẮT ĐẦU")
				for _, b := range bs {
					fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%d\t%s\t%s\n", b.BatchID, b.Status, b.Harness, strings.Join(b.Arms, ","),
						b.Repeats, len(b.CaseIDs), b.LeakageMode, fmtISO(b.StartedAt))
				}
				return tw.Flush()
			}
			id := "latest"
			if len(args) == 1 {
				id = args[0]
			}
			b, err := eval.GetBatch(ctx, e.st.DB, id)
			if err != nil {
				return err
			}
			current := map[string]*eval.Case{}
			if cs, err := e.cases.Cases(); err == nil {
				for _, c := range cs {
					current[c.ID] = c
				}
			}
			rep, err := eval.BuildReport(ctx, e.st.DB, b, baseline, current)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(w, rep)
			}
			printReport(w, rep, perCase)
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
	if len(rep.Diagnostics) > 0 {
		fmt.Fprintln(w, "\nĐộ tin cậy của phép đo:")
		for _, d := range rep.Diagnostics {
			icon := "i"
			if d.Level == "warn" {
				icon = "!"
			}
			fmt.Fprintf(w, "  %s %s\n", icon, d.Detail)
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
		sel          selection
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
			cases, err := sel.pick(e)
			if err != nil {
				return err
			}
			var bases []string
			for _, cs := range cases {
				if !slices.Contains(bases, cs.Source.Base) {
					bases = append(bases, cs.Source.Base)
				}
			}
			if list {
				tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
				fmt.Fprintln(tw, "COMMIT GỐC\tẢNH CHỤP\tMỤC\tCHI PHÍ\tMÔ HÌNH\tTẠO LÚC")
				for _, b := range bases {
					meta, _, ok, err := eval.ReadSnapshot(e.env.WorkRoot, b)
					switch {
					case err != nil:
						fmt.Fprintf(tw, "%s\tlỗi: %v\t\t\t\t\n", short(b), err)
					case !ok:
						fmt.Fprintf(tw, "%s\t-\t\t\t\t\n", short(b))
					default:
						cost := "-"
						if meta.CostUSD != nil {
							cost = fmt.Sprintf("$%.2f", *meta.CostUSD)
						}
						fmt.Fprintf(tw, "%s\tcó\t%d\t%s\t%s\t%s\n", short(b), meta.Items, cost, meta.Model, fmtISO(meta.CreatedAt))
					}
				}
				return tw.Flush()
			}
			var todo []string
			for _, b := range bases {
				if _, _, ok, _ := eval.ReadSnapshot(e.env.WorkRoot, b); ok && !force {
					continue
				}
				todo = append(todo, b)
			}
			fmt.Fprintf(w, "%d tác vụ, %d commit gốc, %d cần dựng tri thức\n", len(cases), len(bases), len(todo))
			if len(todo) == 0 {
				return nil
			}
			var agent eval.Agent
			if agentCommand != "" {
				agent = &eval.CommandAgent{Command: agentCommand}
			} else {
				agent = &eval.ClaudeAgent{Cfg: e.ws.Config.Eval.Agent, Model: model, MaxCostUSD: e.ws.Config.Limits.PerRun.MaxCostUSD}
			}
			var bud *float64
			if cmd.Flags().Changed("budget") {
				bud = &budget
			} else if e.ws.Config.Eval.BudgetUSD != nil {
				bud = e.ws.Config.Eval.BudgetUSD
			}
			if agent.ReportsCost() && bud == nil {
				return usageError{fmt.Errorf("cần ngân sách: --budget <USD> hoặc eval.budget_usd")}
			}
			if !yes {
				if !isTerminal(stdio.In) {
					return usageError{fmt.Errorf("cần --yes khi chạy không tương tác")}
				}
				if !confirm(stdio, fmt.Sprintf("Chạy b-bootstrap bằng %s cho %d commit gốc?", agent.Name(), len(todo))) {
					return fmt.Errorf("đã huỷ")
				}
			}
			self, _ := os.Executable()
			spent, failed := 0.0, 0
			for i, b := range todo {
				if bud != nil && spent >= *bud {
					fmt.Fprintf(w, "Dừng: đã tiêu $%.2f, chạm ngân sách $%.2f; %d commit gốc còn lại\n", spent, *bud, len(todo)-i)
					break
				}
				meta, err := eval.BootstrapSnapshot(ctx, e.env, b, eval.BootstrapOptions{Agent: agent, BintPath: self, BintVersion: Version, Keep: keep})
				if meta.CostUSD != nil {
					spent += *meta.CostUSD
				}
				cost := ""
				if meta.CostUSD != nil {
					cost = fmt.Sprintf(" $%.2f", *meta.CostUSD)
				}
				if err != nil {
					failed++
					fmt.Fprintf(w, "[%d/%d] %s ✗ %v%s\n", i+1, len(todo), short(b), err, cost)
					if ctx.Err() != nil {
						return withCode(ExitError, fmt.Errorf("đã dừng theo yêu cầu"))
					}
					continue
				}
				fmt.Fprintf(w, "[%d/%d] %s ✓ %d mục%s %s\n", i+1, len(todo), short(b), meta.Items, cost, meta.Error)
			}
			if agent.ReportsCost() {
				fmt.Fprintf(w, "Tổng chi phí: $%.2f\n", spent)
			}
			if failed > 0 {
				return withCode(ExitError, fmt.Errorf("%d commit gốc không dựng được tri thức", failed))
			}
			return nil
		},
	}
	sel.flags(c)
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
