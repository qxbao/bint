package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"bint/internal/app"
	"bint/internal/eval"
	"bint/internal/evolve"
	"bint/internal/harness"
	"bint/internal/knowledge"
	"bint/internal/runlog"
	"bint/internal/skills"
)

type evolveCtx struct {
	*evalCtx
	k *app.KnowledgeWS
}

// openEvolve mở không gian tiến hoá: cần repository đã `bint init` (cơ sở dữ liệu
// trạng thái). Nạp spool trước để số liệu gồm cả sự kiện mới nhất.
func openEvolve(cmd *cobra.Command) (*evolveCtx, error) {
	e, err := openEval(cmd, true)
	if err != nil {
		return nil, err
	}
	dir, err := cwd()
	if err != nil {
		e.Close()
		return nil, err
	}
	k, err := app.OpenKnowledge(cmd.Context(), dir)
	if err != nil {
		e.Close()
		return nil, err
	}
	if _, err := e.ws.Ingest(cmd.Context(), e.st, time.Now()); err != nil {
		e.Close()
		return nil, err
	}
	return &evolveCtx{evalCtx: e, k: k}, nil
}

func newEvolveCmd(stdio IO) *cobra.Command {
	c := &cobra.Command{
		Use:   "evolve",
		Short: "Tiến hoá tri thức (L1): phát hiện cơ hội, đề xuất, kiểm chứng, áp dụng, quay lui",
		Long: `Evolution Engine cấp L1 (mục 10–11 của kế hoạch). Chế độ propose: Bint chỉ đề xuất,
người duyệt quyết định; không thay đổi nào được tự áp dụng.

  bint evolve scan                  phát hiện cơ hội + kiểm tra điều kiện tiên quyết (không gọi mô hình)
  bint evolve propose [op-…]        tạo đề xuất (loại bỏ/gộp: không gọi mô hình; tạo/sửa: agent evolver, cần --budget)
  bint evolve show <id>             nội dung thay đổi, phiên bản gốc, bằng chứng, kết quả đối chiếu
  bint evolve evaluate <id>         đối chiếu base/candidate trên tác vụ chịu ảnh hưởng (--split validation, rồi test)
  bint evolve apply <id>            áp dụng (cần kết quả supported, độc lập trên test; hoặc --unverified)
  bint evolve rollback <id>         quay lui
  bint evolve history               lịch sử áp dụng, đối chiếu mô hình, chi phí tiến hoá
  bint evolve queue | process       yêu cầu tiến hoá đã đánh dấu / xử lý ngay (chế độ deferred)

Cấp độ: L1 tiến hoá tri thức (P6); L2 tiến hoá skill chuyên biệt (P7): tạo skill từ quy trình
được xác nhận hiệu quả nhiều lần, hợp nhất skill trùng, loại bỏ skill lỗi thời. Skill gốc bất
biến, không bao giờ là đối tượng của đề xuất (xem bint skill).`,
	}
	c.AddCommand(newEvolveScanCmd(stdio), newEvolveProposeCmd(stdio), newEvolveListCmd(stdio), newEvolveShowCmd(stdio),
		newEvolveEvaluateCmd(stdio), newEvolveApplyCmd(stdio), newEvolveRejectCmd(stdio), newEvolveRollbackCmd(stdio),
		newEvolveHistoryCmd(stdio), newEvolveQueueCmd(stdio), newEvolveProcessCmd(stdio))
	return c
}

func (e *evolveCtx) scan(ctx context.Context, days int, gitScan bool) (evolve.ScanResult, *evolve.History, error) {
	now := time.Now()
	window := time.Duration(days) * 24 * time.Hour
	if days <= 0 {
		window = 0
	}
	if gitScan {
		// Tín hiệu đến muộn (commit, hoàn tác) cập nhật lại nhãn trước khi phân tích (mục 9.6).
		lb := window
		if lb == 0 {
			lb = evolve.DefaultWindow
		}
		if _, err := runlog.ScanGit(ctx, e.st.DB, e.ws.Repo.Toplevel, runlog.ScanOptions{Lookback: lb}); err != nil {
			return evolve.ScanResult{}, nil, fmt.Errorf("quét lịch sử Git: %w", err)
		}
	}
	items, _, err := e.k.Items()
	if err != nil {
		return evolve.ScanResult{}, nil, err
	}
	cases, err := e.cases.Cases()
	if err != nil {
		return evolve.ScanResult{}, nil, err
	}
	evolved, _, err := skills.LoadEvolved(e.k.Root)
	if err != nil {
		return evolve.ScanResult{}, nil, err
	}
	in := evolve.ScanInput{DB: e.st.DB, Cfg: e.k.Config, Items: items, Cases: cases, Now: now, Window: window,
		Evolved: evolved, Files: e.k.Source().Files()}
	if in.Window == 0 {
		in.Window = evolve.DefaultWindow
		if d := e.k.Config.Memory.RetentionDays; d > 0 {
			in.Window = time.Duration(d) * 24 * time.Hour
		}
	}
	h, err := evolve.LoadHistory(ctx, e.st.DB, now, in.Window)
	if err != nil {
		return evolve.ScanResult{}, nil, err
	}
	in.History = h
	res, err := evolve.Scan(ctx, in)
	return res, h, err
}

// caseMap trả về tác vụ đánh giá đã duyệt theo ID (rỗng nếu không đọc được).
func (e *evolveCtx) caseMap() map[string]*eval.Case {
	m := map[string]*eval.Case{}
	if cs, err := e.cases.Cases(); err == nil {
		for _, c := range cs {
			m[c.ID] = c
		}
	}
	return m
}

func newEvolveScanCmd(stdio IO) *cobra.Command {
	var (
		days   int
		asJSON bool
		noGit  bool
	)
	c := &cobra.Command{
		Use:   "scan",
		Short: "Phát hiện cơ hội tiến hoá tri thức và kiểm tra điều kiện tiên quyết về dữ liệu",
		Long: `Không gọi mô hình. Dữ liệu: lượt chạy đã kết thúc trong cửa sổ (memory.retention_days),
gộp các bước workflow vào lượt gốc, nhãn kết quả từ tín hiệu (lượt unlabeled không
được dùng làm bằng chứng), tệp được đọc/sửa, truy xuất/trích dẫn tri thức, quan sát.

Nhóm tác vụ (MVP): theo khu vực mã chính (hai cấp thư mục đầu của tệp được sửa).
Cơ hội:
  exploration       tệp được chạm lại ở ≥ min_similar_runs lượt có nhãn, không tri thức nào áp dụng → create
  failure_hotspot   tệp bị sửa ở ≥ min_new_evidence lượt thất bại, chưa có cảnh báo → create (warning)
  stale_used        mục stale vẫn được truy xuất / agent nhận định đã sai → update
  stale_expired     mục stale quá knowledge.stale_deprecate_days → deprecate
  unused            truy xuất nhiều, chưa từng được trích dẫn → deprecate (đánh giá loại bỏ quyết định)
  duplicate         mục trùng (cùng loại, phạm vi chồng, tiêu đề gần trùng) → merge`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := openEvolve(cmd)
			if err != nil {
				return err
			}
			defer e.Close()
			res, _, err := e.scan(cmd.Context(), days, !noGit)
			if err != nil {
				return err
			}
			if asJSON {
				if res.Groups == nil {
					res.Groups = []evolve.Group{}
				}
				if res.Opportunities == nil {
					res.Opportunities = []evolve.Opportunity{}
				}
				return writeJSON(stdio.Out, res)
			}
			printScan(stdio.Out, res)
			return nil
		},
	}
	c.Flags().IntVar(&days, "days", 0, "cửa sổ lịch sử (ngày; mặc định memory.retention_days)")
	c.Flags().BoolVar(&noGit, "no-git-scan", false, "không quét lịch sử Git để cập nhật tín hiệu commit/hoàn tác trước")
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func printScan(w io.Writer, res evolve.ScanResult) {
	fmt.Fprintf(w, "Lượt chạy từ %s: %d (có nhãn %d, có dữ liệu tệp đọc %d)\n", res.Since[:10], res.Runs, res.Labeled, res.ReadData)
	p := res.Prerequisites
	fmt.Fprintln(w, "\nĐiều kiện tiên quyết (mục 10.6):")
	for _, c := range p.Checks {
		icon := "✓"
		if !c.OK {
			icon = "✗"
		}
		fmt.Fprintf(w, "  %s %-19s %s\n", icon, c.Name, c.Detail)
	}
	if p.Quantitative {
		fmt.Fprintln(w, "  → đề xuất có thể được kiểm chứng định lượng (bint evolve evaluate).")
	} else {
		fmt.Fprintln(w, "  → chưa đủ điều kiện: chỉ đề xuất L1 chờ người duyệt, CHƯA được kiểm chứng định lượng.")
	}
	if len(res.Groups) > 0 {
		fmt.Fprintln(w, "\nNhóm tác vụ (theo khu vực mã):")
		tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "  KHU VỰC\tLƯỢT\tCÓ NHÃN\tĐẠT/HỎNG\tĐỌC TB\tCHI PHÍ TB\tWORKFLOW")
		for _, g := range res.Groups {
			var wf []string
			for _, k := range sortedMapKeys(g.Workflows) {
				wf = append(wf, fmt.Sprintf("%s=%d", k, g.Workflows[k]))
			}
			fmt.Fprintf(tw, "  %s\t%d\t%d\t%d/%d\t%s\t%s\t%s\n", g.Module, g.Runs, g.Labeled, g.Success, g.Failure,
				fmtOptF(g.AvgReads, "%.0f"), fmtOptF(g.AvgCost, "$%.2f"), strings.Join(wf, " "))
		}
		tw.Flush()
	}
	fmt.Fprintf(w, "\nCơ hội (%d):\n", len(res.Opportunities))
	if len(res.Opportunities) == 0 {
		fmt.Fprintln(w, "  (không có)")
		return
	}
	for _, o := range res.Opportunities {
		state := "sẵn sàng"
		if !o.Ready {
			state = "chưa: " + o.Blocked
		}
		lvl := "L1"
		if o.Level != "" {
			lvl = o.Level
		}
		fmt.Fprintf(w, "  %s  %s %-15s → %-9s [%s] %s\n", o.ID, lvl, o.Kind, o.Action, o.Generator, state)
		fmt.Fprintf(w, "      %s\n", o.Summary)
		for _, f := range o.Files {
			fmt.Fprintf(w, "        %s (%d lượt, %d thất bại)\n", f.Path, f.Runs, f.Failures)
			if len(f.Members) > 0 {
				fmt.Fprintf(w, "          gồm: %s\n", strings.Join(f.Members, ", "))
			}
		}
	}
	fmt.Fprintln(w, "\nBước tiếp: `bint evolve propose [op-…]` (cơ hội cần agent: thêm --budget <USD>).")
}

func fmtOptF(v *float64, f string) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf(f, *v)
}

// evolverAgent dựng agent evolver: roles.evolver, chỉ được đọc.
func evolverAgent(k *app.KnowledgeWS, model, command string) (eval.Agent, error) {
	if command != "" {
		return &eval.CommandAgent{Command: command}, nil
	}
	name := k.Config.Roles.Evolver
	if name == "claude" {
		cfg := k.Config.Eval.Agent
		cfg.AllowedTools = []string{"Read", "Grep", "Glob"}
		cfg.PermissionMode = "dontAsk"
		cfg.ExtraArgs = nil
		return &eval.ClaudeAgent{Cfg: cfg, Model: model}, nil
	}
	return harness.NewAgent(name, harness.AgentOptions{Config: k.Config, ReadOnly: true, Model: model})
}

func newEvolveProposeCmd(stdio IO) *cobra.Command {
	var (
		budget       float64
		max          int
		days         int
		agentCommand string
		model        string
		noAgent      bool
		asJSON       bool
	)
	c := &cobra.Command{
		Use:   "propose [op-id|khoá...]",
		Short: "Tạo đề xuất cho cơ hội sẵn sàng (mặc định tối đa evolution.evaluation.max_candidates)",
		Long: `Đề xuất được ghi vào cơ sở dữ liệu trạng thái cục bộ; không tệp tri thức nào bị sửa.

Mỗi đề xuất ghi: hành động, phiên bản gốc của mục bị sửa (băm tệp), lượt có nhãn làm
căn cứ (kèm nhãn lúc đó), thay đổi đề xuất, mức rủi ro do Bint tính, kế hoạch đánh giá
chốt trước (thước đo, ngưỡng, giả thuyết kiểm tra được).

Loại bỏ và gộp mục trùng được dựng xác định, không gọi mô hình. Tạo/sửa tri thức dùng
agent evolver (roles.evolver, chỉ Read/Grep/Glob); đầu ra của agent phải qua kiểm tra
xác định được (bằng chứng tồn tại, không trùng, độ dài, cờ bên thứ ba) mới thành đề xuất.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireHumanCLI("bint evolve propose"); err != nil {
				return err
			}
			e, err := openEvolve(cmd)
			if err != nil {
				return err
			}
			defer e.Close()
			scan, h, err := e.scan(cmd.Context(), days, true)
			if err != nil {
				return err
			}
			var agent eval.Agent
			if !noAgent {
				if agent, err = evolverAgent(e.k, model, agentCommand); err != nil {
					return err
				}
			}
			rep, err := evolve.Propose(cmd.Context(), evolve.ProposeInput{K: e.k, DB: e.st.DB, Scan: scan, History: h, Refs: args,
				Max: max, Agent: agent, BudgetUSD: budget, Timeout: time.Duration(e.k.Config.Eval.TimeoutMinutes) * time.Minute})
			if err != nil {
				return err
			}
			if asJSON {
				if rep.Outcomes == nil {
					rep.Outcomes = []evolve.ProposeOutcome{}
				}
				return writeJSON(stdio.Out, rep)
			}
			w := stdio.Out
			if len(rep.Outcomes) == 0 {
				fmt.Fprintln(w, "Không có cơ hội nào sẵn sàng (`bint evolve scan`).")
				return nil
			}
			made := 0
			for _, o := range rep.Outcomes {
				if o.Proposal != nil {
					made++
					p := o.Proposal
					fmt.Fprintf(w, "+ %s  %s %s  rủi ro %s  (%s)\n", p.ID, p.Action, strings.Join(changeIDs(p), ","), p.Risk, o.Opportunity)
					fmt.Fprintf(w, "    %s\n", p.Reason)
				} else {
					fmt.Fprintf(w, "- %s  bỏ qua: %s\n", o.Opportunity, o.Skipped)
				}
				for _, r := range o.Rejected {
					fmt.Fprintf(w, "    ✗ ứng viên bị loại: %s\n", r)
				}
			}
			if rep.SpentUSD > 0 {
				fmt.Fprintf(w, "\nChi phí tạo ứng viên: $%.4f\n", rep.SpentUSD)
			}
			if made > 0 {
				fmt.Fprintln(w, "\nBước tiếp: `bint evolve show <id>`, `bint evolve evaluate <id> --split validation`, rồi --split test.")
				if !scan.Prerequisites.Quantitative {
					fmt.Fprintln(w, "Lưu ý: điều kiện tiên quyết chưa đạt — đề xuất CHƯA được kiểm chứng định lượng; áp dụng cần --unverified.")
				}
			}
			return nil
		},
	}
	f := c.Flags()
	f.Float64Var(&budget, "budget", 0, "ngân sách USD cho agent evolver (bắt buộc với Claude Code khi có cơ hội cần agent)")
	f.IntVar(&max, "max", 0, "số cơ hội tối đa (mặc định evolution.evaluation.max_candidates)")
	f.IntVar(&days, "days", 0, "cửa sổ lịch sử (ngày)")
	f.StringVar(&agentCommand, "agent-command", "", "dùng lệnh tuỳ chỉnh làm agent evolver (đề bài qua stdin)")
	f.StringVar(&model, "model", "", "mô hình cho agent evolver")
	f.BoolVar(&noAgent, "no-agent", false, "chỉ tạo đề xuất xác định (không gọi mô hình)")
	f.BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func requireHumanCLI(op string) error {
	if in, k := app.InAgentSession(); in {
		return fmt.Errorf("%w (`%s`; phát hiện biến %s)", app.ErrAgentForbidden, op, k)
	}
	return nil
}

func changeIDs(p *evolve.Proposal) []string {
	if p.IsSkill() {
		return evolve.SkillChangeNames(p)
	}
	var out []string
	for _, c := range p.Changes {
		out = append(out, c.Op+":"+c.ItemID)
	}
	return out
}

// proposalState mô tả trạng thái hiển thị: đề xuất chờ có phiên bản gốc đã đổi là "outdated".
func proposalState(e *evolveCtx, p *evolve.Proposal) (string, []evolve.BaseProblem) {
	if p.Status != evolve.StatusProposed {
		return p.Status, nil
	}
	probs, err := p.CheckBaseWS(e.k)
	if err != nil || len(probs) > 0 {
		return "outdated", probs
	}
	return p.Status, nil
}

func latestVerdict(evs []evolve.Evaluation, split string) string {
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Split == split && evs[i].Verdict != "" {
			v := evs[i].Verdict
			if evs[i].Independent != nil && !*evs[i].Independent {
				v += "*"
			}
			return v
		}
	}
	return "-"
}

func newEvolveListCmd(stdio IO) *cobra.Command {
	var (
		all    bool
		asJSON bool
	)
	c := &cobra.Command{
		Use:   "list",
		Short: "Liệt kê đề xuất (mặc định: đang chờ)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := openEvolve(cmd)
			if err != nil {
				return err
			}
			defer e.Close()
			var statuses []string
			if !all {
				statuses = []string{evolve.StatusProposed}
			}
			ps, err := evolve.ListProposals(cmd.Context(), e.st.DB, statuses...)
			if err != nil {
				return err
			}
			if asJSON {
				type row struct {
					*evolve.Proposal
					State string `json:"state"`
				}
				out := []row{}
				for _, p := range ps {
					st, _ := proposalState(e, p)
					out = append(out, row{p, st})
				}
				return writeJSON(stdio.Out, out)
			}
			if len(ps) == 0 {
				fmt.Fprintln(stdio.Out, "Không có đề xuất nào (`bint evolve scan`, `bint evolve propose`).")
				return nil
			}
			tw := tabwriter.NewWriter(stdio.Out, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tTRẠNG THÁI\tHÀNH ĐỘNG\tRỦI RO\tVALIDATION\tTEST\tTẠO LÚC\tLÝ DO")
			for _, p := range ps {
				st, _ := proposalState(e, p)
				evs, err := evolve.Evaluations(cmd.Context(), e.st.DB, p.ID)
				if err != nil {
					return err
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", p.ID, st, p.Action, p.Risk,
					latestVerdict(evs, eval.SplitValidation), latestVerdict(evs, eval.SplitTest), fmtTime(p.CreatedAt), truncate(p.Reason, 70))
			}
			tw.Flush()
			fmt.Fprintln(stdio.Out, "(* = kết quả trên test không độc lập; outdated = phiên bản gốc đã đổi)")
			return nil
		},
	}
	c.Flags().BoolVar(&all, "all", false, "mọi trạng thái")
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func newEvolveShowCmd(stdio IO) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "show <id>",
		Short: "Chi tiết đề xuất: thay đổi, phiên bản gốc, căn cứ, kế hoạch và kết quả đối chiếu",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := openEvolve(cmd)
			if err != nil {
				return err
			}
			defer e.Close()
			p, err := evolve.GetProposal(ctx, e.st.DB, args[0])
			if err != nil {
				return err
			}
			st, probs := proposalState(e, p)
			evs, err := evolve.Evaluations(ctx, e.st.DB, p.ID)
			if err != nil {
				return err
			}
			ads, err := evolve.Adoptions(ctx, e.st.DB, p.ID)
			if err != nil {
				return err
			}
			h, err := evolve.LoadHistory(ctx, e.st.DB, time.Now(), 365*24*time.Hour)
			if err != nil {
				return err
			}
			drift := evolve.LabelDrift(p, h)
			if asJSON {
				return writeJSON(stdio.Out, map[string]any{"proposal": p, "state": st, "base_problems": nonNilSlice(probs),
					"evaluations": nonNilSlice(evs), "adoptions": nonNilSlice(ads), "label_drift": nonNilSlice(drift)})
			}
			items, _, _ := e.k.Items()
			evolved, _, _ := skills.LoadEvolved(e.k.Root)
			printProposal(stdio.Out, p, st, probs, evs, ads, drift, items, evolved, e.caseMap())
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func nonNilSlice[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func printProposal(w io.Writer, p *evolve.Proposal, state string, probs []evolve.BaseProblem, evs []evolve.Evaluation,
	ads []evolve.Adoption, drift []string, items []*knowledge.Item, evolved []*skills.Evolved, cases map[string]*eval.Case) {
	fmt.Fprintf(w, "Đề xuất:     %s (%s)\n", p.ID, state)
	if p.StatusReason != "" {
		fmt.Fprintf(w, "Lý do TT:    %s\n", p.StatusReason)
	}
	fmt.Fprintf(w, "Hành động:   %s — cấp %s, đối tượng %s\n", strings.TrimSpace(p.Action+" "+strings.Join(p.TargetIDs, ", ")), p.Level, p.TargetType)
	fmt.Fprintf(w, "Cơ hội:      %s\n", p.Opportunity)
	fmt.Fprintf(w, "Lý do:       %s\n", p.Reason)
	if p.ExpectedBenefit != "" {
		fmt.Fprintf(w, "Lợi ích:     %s\n", p.ExpectedBenefit)
	}
	fmt.Fprintf(w, "Rủi ro:      %s %s\n", p.Risk, strings.Join(p.RiskReasons, "; "))
	by := []string{p.Generator}
	if p.Model != "" {
		by = append(by, p.Model)
	}
	fmt.Fprintf(w, "Tạo bởi:     %s, %s", strings.Join(by, " "), fmtTime(p.CreatedAt))
	if p.CostUSD != nil {
		fmt.Fprintf(w, ", $%.4f", *p.CostUSD)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Kế hoạch:    %s trên %s — %s\n", p.Plan.Kind, p.Plan.Metric, p.Plan.Hypothesis)
	if !p.Quantitative {
		fmt.Fprintln(w, "Kiểm chứng:  điều kiện tiên quyết chưa đạt lúc đề xuất — CHƯA kiểm chứng định lượng")
	}
	for _, n := range p.Notes {
		fmt.Fprintf(w, "Lưu ý:       %s\n", n)
	}
	if len(p.Base) > 0 {
		fmt.Fprintln(w, "\nPhiên bản gốc:")
		for _, b := range p.Base {
			fmt.Fprintf(w, "  %s v%d %s sha256:%s\n", b.ID, b.Version, b.Status, b.SHA[:min(12, len(b.SHA))])
		}
	}
	for _, b := range probs {
		fmt.Fprintf(w, "  ✗ %s: %s — đề xuất không còn hợp lệ\n", b.ID, b.Detail)
	}
	fmt.Fprintf(w, "\nCăn cứ (%d lượt có nhãn):", len(p.SupportingRuns))
	for i, r := range p.SupportingRuns {
		if i == 12 {
			fmt.Fprintf(w, " …")
			break
		}
		fmt.Fprintf(w, " %s(%s)", r.RunID, r.Label)
	}
	fmt.Fprintln(w)
	for _, d := range drift {
		fmt.Fprintf(w, "  ! nhãn đã đổi: %s — xem xét lại đề xuất\n", d)
	}
	byID := map[string]*knowledge.Item{}
	for _, it := range items {
		byID[it.ID] = it
	}
	fmt.Fprintln(w, "\nThay đổi:")
	printSkillChanges(w, p, evolved)
	for _, ch := range p.Changes {
		fmt.Fprintf(w, "  [%s] %s", ch.Op, ch.ItemID)
		if ch.Reason != "" {
			fmt.Fprintf(w, " — %s", ch.Reason)
		}
		fmt.Fprintln(w)
		if d := ch.Draft; d != nil {
			cur := byID[ch.ItemID]
			field := func(name, old, new string) {
				switch {
				case cur == nil:
					fmt.Fprintf(w, "      %s: %s\n", name, new)
				case old != new:
					fmt.Fprintf(w, "    - %s: %s\n    + %s: %s\n", name, old, name, new)
				}
			}
			oldEv := ""
			var oldTitle, oldBody, oldPaths string
			if cur != nil {
				var es []string
				for _, e := range cur.Evidence {
					es = append(es, e.String())
				}
				oldEv, oldTitle, oldBody, oldPaths = strings.Join(es, ", "), cur.Title, cur.Body, strings.Join(cur.Scope.Paths, ", ")
			}
			field("loại", "", d.Type)
			field("tiêu đề", oldTitle, d.Title)
			field("phạm vi", oldPaths, strings.Join(d.Scope.Paths, ", "))
			field("bằng chứng", oldEv, strings.Join(d.Evidence, ", "))
			if cur == nil || cur.Body != d.Body {
				if cur != nil {
					fmt.Fprintf(w, "    - nội dung:\n%s\n", indent(oldBody, "        "))
					fmt.Fprintf(w, "    + nội dung:\n%s\n", indent(d.Body, "        "))
				} else {
					fmt.Fprintf(w, "      nội dung:\n%s\n", indent(d.Body, "        "))
				}
			}
		}
	}
	if len(evs) > 0 {
		fmt.Fprintln(w, "\nĐối chiếu:")
		for _, e := range evs {
			ind := ""
			if e.Independent != nil {
				ind = map[bool]string{true: " độc lập", false: " KHÔNG độc lập"}[*e.Independent]
			}
			stale := ""
			if e.BaseKey != p.BaseKey() {
				stale = " (phiên bản gốc đã khác)"
			}
			kind := ""
			if k := evolve.EvidenceKind(p, e.CaseIDs, cases); k != "" {
				kind = ", " + evolve.KindLabel(k)
			}
			if e.Kind == evolve.EvalRegression {
				kind += ", kiểm tra hồi quy"
			}
			fmt.Fprintf(w, "  %s  %s  %s, %d tác vụ%s, %s → %s%s%s\n    %s\n", e.ID, e.BatchID, orDash(e.Split), len(e.CaseIDs), kind, e.Delivery,
				orDash(e.Verdict), ind, stale, e.Detail)
		}
	}
	if len(ads) > 0 {
		fmt.Fprintln(w, "\nÁp dụng / quay lui:")
		for _, a := range ads {
			fmt.Fprintf(w, "  %s  %s  %s  %s  kiểm chứng=%v  %s\n", a.ID, a.Action, fmtTime(a.CreatedAt), a.Actor, a.Verified, a.Reason)
		}
	}
}

func newEvolveEvaluateCmd(stdio IO) *cobra.Command {
	var (
		split        string
		repeats      int
		budget       float64
		limit        int
		yes          bool
		harnessName  string
		agentCommand string
		model        string
		leakage      string
		delivery     string
		parallel     int
		seed         uint64
		keep         bool
		bare         bool
		resume       string
		prospective  bool
		regression   bool
		force        bool
		asJSON       bool
	)
	c := &cobra.Command{
		Use:   "evaluate <id>",
		Short: "Đối chiếu base/candidate (Probe) trên tác vụ chịu ảnh hưởng của đề xuất",
		Long: `Chạy khung đánh giá với hai nhánh dùng cùng cách đưa tri thức (evolution.evaluation.delivery):
  base       tri thức hiện có của tác vụ (lọc chống rò rỉ theo commit gốc)
  candidate  cùng tập đó sau khi áp đề xuất (đề xuất deprecate = đánh giá loại bỏ)

Chỉ chạy tác vụ đã duyệt, đã kiểm chứng fail→pass, thuộc --split, mà đề xuất thực sự
làm đổi tập tri thức và mục bị đổi có phạm vi liên quan tới tác vụ. Nội dung mới chỉ
được dùng cho tác vụ có bằng chứng khớp tại commit gốc và không chứa mã của lời giải.

Nội dung mới của đề xuất được viết sau các tác vụ cũ: chỉ dùng cho tác vụ mà bằng chứng
của nó đã khớp tại commit gốc và không chứa mã lời giải; rủi ro rò rỉ gián tiếp còn lại
được ghi trong kết luận. --prospective chỉ dùng tác vụ sinh sau đề xuất (bint eval mine
lại sau một thời gian) để loại hẳn rủi ro này.

Quy trình (mục 12.2): xác thực trên validation, kết luận trên test. Không chỉnh đề
xuất theo kết quả test rồi báo cáo lại trên chính tập đó: đề xuất là bất biến; đề xuất
mới cùng đối tượng đối chiếu lại trên các tác vụ test đã dùng sẽ bị đánh dấu không độc lập.

Probe tuần tự (ít tác vụ, nhiều lần lặp, dừng sớm): trước khi chạy, Bint lập kế hoạch từ
mô hình σ²(R) = σ_b² + W/R — chọn số lần lặp R (evolution.evaluation.repeats = 0) sao cho
tổng số lượt thấp nhất mà số tác vụ cần ≤ số tác vụ sẵn có, rồi in số tác vụ, R, số lượt và
chi phí tối đa. Không khả thi (không phát hiện được mức hiệu ứng đã chọn) thì từ chối và nêu
mức nhỏ nhất phát hiện được, trừ khi có --force. Các lượt chạy theo từng lần lặp; sau mỗi
điểm nhìn (tối đa 4) Bint dừng sớm nếu đã đủ bằng chứng theo biên O'Brien–Fleming (thắng,
không kém, hoặc futility).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := openEvolve(cmd)
			if err != nil {
				return err
			}
			defer e.Close()
			cfg := e.k.Config
			ev := cfg.Evolution.Evaluation
			w := stdio.Out
			if !ev.Enabled {
				return fmt.Errorf("evolution.evaluation.enabled = false")
			}
			p, err := evolve.GetProposal(ctx, e.st.DB, args[0])
			if err != nil {
				return err
			}
			if regression {
				if p.Status != evolve.StatusApplied {
					return usageError{fmt.Errorf("--regression chỉ dùng cho đề xuất đã áp dụng (đề xuất %s: %s)", p.ID, p.Status)}
				}
			} else {
				if p.Status != evolve.StatusProposed {
					return fmt.Errorf("đề xuất %s ở trạng thái %s: chỉ đối chiếu đề xuất đang chờ", p.ID, p.Status)
				}
				if st, probs := proposalState(e, p); st == "outdated" {
					return fmt.Errorf("đề xuất %s không còn hợp lệ: %s: %s", p.ID, probs[0].ID, probs[0].Detail)
				}
			}
			if delivery == "" {
				delivery = ev.Delivery
			}
			if delivery != eval.ArmBint && delivery != eval.ArmMinimal {
				return usageError{fmt.Errorf("--delivery phải là bint | minimal")}
			}
			mode := ev.LeakageMode
			if leakage != "" {
				mode = leakage
			}
			if mode != eval.LeakStrict && mode != eval.LeakEconomy {
				return usageError{fmt.Errorf("--leakage-mode phải là strict | economy")}
			}
			now := time.Now()
			var skillBefore, skillAfter evolve.SkillSet
			if p.IsSkill() {
				list, _, err := skills.LoadEvolved(e.k.Root)
				if err != nil {
					return err
				}
				if regression {
					// Skill đã áp dụng: base = trước đề xuất (dựng lại), candidate = hiện tại.
					if skillBefore, err = evolve.ReverseSet(e.k.Root, p, list); err != nil {
						return err
					}
					skillAfter = evolve.CurrentSet(list)
				} else {
					skillBefore = evolve.CurrentSet(list)
					if skillAfter, err = evolve.ApplyToSet(p, skillBefore, e.k.Source(), cfg.Context.Freshness.Fingerprint); err != nil {
						return err
					}
				}
				e.env.Variant = evolve.SkillVariant(p, delivery, e.ws.Repo.Toplevel, skillBefore, skillAfter)
			} else if regression {
				// Tri thức đã áp dụng: base = tập trước đề xuất (dựng lại từ bản ghi kiểm toán).
				bi, err := evolve.BeforeItems(ctx, e.st.DB, p)
				if err != nil {
					return err
				}
				cur, _, err := e.k.Items()
				if err != nil {
					return err
				}
				touchedNow := map[string]*knowledge.Item{}
				for _, it := range cur {
					if _, ok := bi[it.ID]; ok {
						touchedNow[it.ID] = it
					}
				}
				e.env.Variant = evolve.RegressionVariant(p, delivery, e.ws.Repo.Toplevel, bi, touchedNow)
			} else {
				e.env.Variant = evolve.Variant(p, delivery, e.ws.Repo.Toplevel, e.k.Source(), cfg.Context.Freshness.Fingerprint, now)
			}

			var agent eval.Agent
			if agentCommand != "" {
				agent = &eval.CommandAgent{Command: agentCommand}
			} else {
				h := harnessName
				if h == "" {
					h = cfg.Eval.Harness
				}
				if agent, err = harness.NewAgent(h, harness.AgentOptions{Config: cfg, Eval: true, Model: model,
					MaxCost: cfg.Limits.PerRun.MaxCostUSD, Bare: bare}); err != nil {
					return err
				}
			}
			opts := eval.RunOptions{Agent: agent, RequestedModel: model, Parallel: parallel, Seed: seed, KeepWorkdirs: keep,
				BintVersion: Version, ResumeBatch: resume, PriorRunUSD: cfg.Eval.PriorRunUSD, RepeatMajor: true}
			curBatch := resume // đợt đang chạy, cho callback Look
			if cmd.Flags().Changed("budget") {
				opts.BudgetUSD = &budget
			} else if ev.BudgetUSD != nil {
				opts.BudgetUSD = ev.BudgetUSD
			}
			allCases, err := e.cases.Cases()
			if err != nil {
				return err
			}
			current := map[string]*eval.Case{}
			for _, c := range allCases {
				current[c.ID] = c
			}
			var record evolve.Evaluation
			if resume != "" {
				record, err = evolve.EvaluationByBatch(ctx, e.st.DB, resume)
				if err != nil {
					return fmt.Errorf("đợt %s không thuộc lần đối chiếu đề xuất nào: %w", resume, err)
				}
				if record.ProposalID != p.ID || record.BaseKey != p.BaseKey() {
					return fmt.Errorf("đợt %s thuộc đề xuất/phiên bản gốc khác", resume)
				}
				e.env.Variant.Delivery = record.Delivery
				opts.Cases = allCases
			} else {
				if !regression && split != eval.SplitValidation && split != eval.SplitTest {
					return usageError{fmt.Errorf("--split phải là validation | test (tập build dùng để xây đề xuất, mục 12.2)")}
				}
				candidates := filterSplit(allCases, split)
				if regression {
					// Tập nhỏ đại diện: chính các tác vụ đã làm căn cứ áp dụng đề xuất (mục 12.6).
					ids, err := evolve.OriginalCases(ctx, e.st.DB, p)
					if err != nil {
						return err
					}
					if len(ids) == 0 {
						return fmt.Errorf("đề xuất %s không có lần đối chiếu supported nào để lấy tập đại diện", p.ID)
					}
					candidates = nil
					for _, c := range allCases {
						if slices.Contains(ids, c.ID) {
							candidates = append(candidates, c)
						}
					}
					if limit == 0 {
						limit = 5
					}
				}
				var pool []*eval.Case
				unverified, older := 0, 0
				for _, c := range candidates {
					if !c.Verified() {
						unverified++
						continue
					}
					if prospective && !evolve.Prospective(p, []string{c.ID}, map[string]*eval.Case{c.ID: c}) {
						older++
						continue
					}
					pool = append(pool, c)
				}
				if prospective {
					fmt.Fprintf(w, "Tiến cứu: bỏ %d tác vụ có commit trước khi đề xuất được tạo\n", older)
				}
				// Mọi lượt căn cứ của đề xuất, kể cả ngoài cửa sổ phân tích.
				hist, err := evolve.LoadHistory(ctx, e.st.DB, now, 10*365*24*time.Hour)
				if err != nil {
					return err
				}
				var impacts []evolve.CaseImpact
				switch {
				case regression:
					for _, c := range pool {
						impacts = append(impacts, evolve.CaseImpact{CaseID: c.ID, Split: c.Split, Case: c, Affected: true, Reason: "tập đại diện"})
					}
				case p.IsSkill():
					impacts, err = evolve.SelectSkillCases(ctx, e.env, mode, p, hist, pool, skillBefore, skillAfter)
				default:
					impacts, err = evolve.SelectCases(ctx, e.env, mode, p, hist, pool)
				}
				if err != nil {
					return err
				}
				var chosen []*eval.Case
				reasons := map[string]int{}
				for _, ci := range impacts {
					if ci.Affected {
						chosen = append(chosen, ci.Case)
					} else {
						reasons[firstClause(ci.Reason)]++
					}
				}
				slices.SortFunc(chosen, func(a, b *eval.Case) int { return strings.Compare(a.ID, b.ID) })
				if limit > 0 && len(chosen) > limit {
					chosen = chosen[:limit]
				}
				fmt.Fprintf(w, "Tập %s: %d tác vụ đã kiểm chứng (%d chưa kiểm chứng bị bỏ), %d chịu ảnh hưởng của %s\n",
					split, len(pool), unverified, len(chosen), p.ID)
				for _, r := range sortedMapKeys(reasons) {
					fmt.Fprintf(w, "  bỏ %d: %s\n", reasons[r], r)
				}
				if len(chosen) == 0 {
					return fmt.Errorf("không tác vụ nào thuộc tập %s chịu ảnh hưởng của đề xuất; đề xuất không kiểm chứng định lượng được trên tập này", split)
				}
				if seed == 0 {
					seed = uint64(now.UnixNano())
				}
				// R cố định bởi --repeats hoặc evolution.evaluation.repeats > 0; ngược lại do PlanProbe chọn.
				fixed := 0
				switch {
				case cmd.Flags().Changed("repeats"):
					if repeats < 1 {
						return usageError{fmt.Errorf("--repeats phải >= 1")}
					}
					fixed = repeats
				case ev.Repeats > 0:
					fixed = ev.Repeats
				}
				sequential := !regression && evolve.Sequential(p.Plan)
				var seqNote string
				if regression {
					// Tập đại diện nhỏ (không nhằm phát hiện hiệu ứng): giữ cách cũ, không lập kế hoạch.
					if repeats = fixed; repeats < 1 {
						repeats = max(cfg.Eval.Repeats, 1)
					}
				} else {
					effect := p.Plan.MinEffect
					if p.Plan.Kind == "non_inferiority" {
						effect = p.Plan.Margin
					}
					vm, err := eval.HistoricalVariance(ctx, e.st.DB, agent.Name(), ev.EffectSD)
					if err != nil {
						return err
					}
					in := eval.PlanInput{Available: len(chosen), Effect: effect, SigmaB: vm.SigmaB, SigmaBMeasured: vm.SigmaMeasured, W: vm.W, MaxRepeats: ev.MaxRepeats, FixedRepeats: fixed}
					if avg, n, err := eval.HistoricalCost(ctx, e.st.DB, agent.Name(), model); err != nil {
						return err
					} else if n > 0 {
						in.CostPerRun = avg
					}
					pp := eval.PlanProbe(in)
					printProbePlan(w, pp, vm, in, sequential)
					if !pp.Feasible && !force {
						return withCode(ExitError, fmt.Errorf("kế hoạch không khả thi: với %d tác vụ và tối đa %d lần lặp chỉ phát hiện được chênh lệch từ %.1f điểm %% (cần %.1f); thêm tác vụ, nâng evolution.evaluation.min_effect/non_inferiority_margin, hoặc dùng --force để chạy vẫn",
							len(chosen), pp.Repeats, 100*pp.MinDetectable, 100*effect))
					}
					repeats = pp.Repeats
					if pp.Feasible && pp.Cases < len(chosen) {
						// Chỉ chạy đủ số tác vụ cần: chọn ngẫu nhiên theo hạt giống để không thiên về ID.
						rng := rand.New(rand.NewPCG(seed, 0x70726f62))
						rng.Shuffle(len(chosen), func(i, j int) { chosen[i], chosen[j] = chosen[j], chosen[i] })
						chosen = chosen[:pp.Cases]
						slices.SortFunc(chosen, func(a, b *eval.Case) int { return strings.Compare(a.ID, b.ID) })
					}
					seqNote = fmt.Sprintf("%d tác vụ × %d lần lặp × 2 nhánh, tối đa %d lượt, %d điểm nhìn ở lần lặp %v",
						len(chosen), repeats, len(chosen)*2*repeats, len(evolve.SeqLooks(repeats)), evolve.SeqLooks(repeats))
				}
				arms := []string{eval.ArmBase, eval.ArmCandidate}
				opts.Cases, opts.Arms, opts.Repeats, opts.LeakageMode, opts.Seed = chosen, arms, repeats, mode, seed
				opts.Notes = append(runNotes(chosen, mode, agent, bare), fmt.Sprintf("đối chiếu đề xuất %s (%s), cách đưa tri thức: %s, tập %s", p.ID, p.Action, delivery, split))
				if sequential {
					opts.Notes = append(opts.Notes, evolve.SeqNotePrefix+seqNote)
				}
				est, err := eval.EstimateRun(ctx, e.st.DB, eval.EstimateInput{Cases: len(chosen), Arms: arms, Repeats: repeats,
					Harness: agent.Name(), Model: model, Effect: p.Plan.MinEffect, EffectSD: ev.EffectSD})
				if err != nil {
					return err
				}
				printEstimate(w, est, arms, repeats, opts.BudgetUSD)
				opts.EstimatedUSD = est.TotalCost
				if agent.ReportsCost() {
					if opts.BudgetUSD == nil {
						return usageError{fmt.Errorf("cần ngân sách: --budget <USD> hoặc evolution.evaluation.budget_usd (mục 11.2: duyệt ngân sách trước khi chạy)")}
					}
					if est.TotalCost != nil && *est.TotalCost > *opts.BudgetUSD {
						return withCode(ExitError, fmt.Errorf("%w: ước tính $%.2f > ngân sách $%.2f", eval.ErrBudget, *est.TotalCost, *opts.BudgetUSD))
					}
				}
				record = evolve.Evaluation{ProposalID: p.ID, Split: split, BaseKey: p.BaseKey(), Delivery: delivery, CreatedAt: nowISO(now)}
				if regression {
					record.Kind, record.Split = evolve.EvalRegression, ""
					opts.Notes = append(opts.Notes, "kiểm tra hồi quy mô hình (mục 12.6): kết quả không dùng làm căn cứ áp dụng")
				}
				for _, c := range chosen {
					record.CaseIDs = append(record.CaseIDs, c.ID)
				}
				opts.OnBatch = func(id string) error {
					curBatch = id
					record.BatchID = id
					return evolve.RecordEvaluation(ctx, e.st.DB, &record)
				}
			}
			if !yes {
				if !isTerminal(stdio.In) {
					return usageError{fmt.Errorf("cần --yes khi chạy không tương tác")}
				}
				if !confirm(stdio, "Bắt đầu đối chiếu?") {
					return fmt.Errorf("đã huỷ")
				}
			}
			opts.Progress = func(r eval.AttemptRow, n, total int) {
				if !asJSON {
					fmt.Fprintf(w, "[%d/%d] %s %-9s #%d  %-8s%s\n", n, total, r.CaseID, r.Arm, r.Repeat, r.Status, attemptExtras(r))
				}
			}
			// Điểm nhìn chỉ cho đợt tuần tự: đợt mới (đánh dấu ở ghi chú) hoặc đợt tiếp tục có đánh dấu đó.
			if !regression && evolve.Sequential(p.Plan) && record.Kind != evolve.EvalRegression {
				resumed := false
				if resume != "" {
					if b, err := eval.GetBatch(ctx, e.st.DB, resume); err == nil {
						resumed = evolve.IsSequential(b)
					}
				}
				if resume == "" || resumed {
					opts.Look = evolve.NewLook(ctx, e.st.DB, p.Plan, func() string { return curBatch }, current)
				}
			}
			res, runErr := eval.Run(ctx, e.env, e.st.DB, opts)
			if res.BatchID == "" {
				return runErr
			}
			if record.ID == "" {
				// Tiếp tục: bản ghi đã có từ lần đầu.
				if record, err = evolve.EvaluationByBatch(ctx, e.st.DB, res.BatchID); err != nil {
					return err
				}
			}
			v, rep, err := evolve.Finalize(context.WithoutCancel(ctx), e.st.DB, p, record, current)
			if err != nil {
				return err
			}
			// Kích hoạt skill thực tế trong các lượt đối chiếu (đọc từ đầu ra của agent, không qua hook).
			var activation []evolve.SkillActivation
			if len(p.SkillChanges) > 0 {
				var names []string
				for _, c := range p.SkillChanges {
					if c.Op == evolve.ActionCreate || c.Op == evolve.ActionUpdate {
						names = append(names, c.Name)
					}
				}
				if rows, aerr := eval.Attempts(ctx, e.st.DB, "batch_id = ?", res.BatchID); aerr == nil {
					activation = evolve.ProbeActivation(rows, names)
				}
			}
			if asJSON {
				return writeJSON(w, map[string]any{"evaluation_id": record.ID, "batch_id": res.BatchID, "status": res.Status,
					"verdict": v, "report": rep, "skill_activation": activation})
			}
			fmt.Fprintf(w, "\nĐợt %s: %s, %d/%d lượt\n", res.BatchID, res.Status, res.Done, res.Total)
			if res.Status == eval.StatusStoppedEarly {
				fmt.Fprintf(w, "Dừng sớm: %s\n", res.StopReason)
			}
			printReport(w, rep, false)
			for _, a := range activation {
				fmt.Fprintf(w, "  skill %s, nhánh %s: được gọi ở %d/%d lượt đo được\n", a.Skill, a.Arm, a.Activated, a.Measured)
			}
			fmt.Fprintf(w, "\nKết luận (%s, tập %s): %s — %s\n", p.Plan.Kind, orDash(record.Split), v.Verdict, v.Detail)
			for _, n := range v.Notes {
				fmt.Fprintf(w, "  lưu ý: %s\n", n)
			}
			if res.Status == "cancelled" || errors.Is(runErr, eval.ErrBudget) {
				fmt.Fprintf(w, "Chạy tiếp: bint evolve evaluate %s --resume %s\n", p.ID, res.BatchID)
			}
			if errors.Is(runErr, context.Canceled) {
				return withCode(ExitError, fmt.Errorf("đã dừng theo yêu cầu"))
			}
			return runErr
		},
	}
	f := c.Flags()
	f.StringVar(&split, "split", eval.SplitValidation, "tập tác vụ: validation (xác thực ứng viên) | test (kiểm tra độc lập)")
	f.IntVar(&repeats, "repeats", 3, "cố định số lần lặp mỗi nhánh (mặc định: evolution.evaluation.repeats nếu > 0, ngược lại do bộ lập kế hoạch chọn)")
	f.BoolVar(&force, "force", false, "chạy dù kế hoạch không khả thi (không phát hiện được mức hiệu ứng đã chọn)")
	f.Float64Var(&budget, "budget", 0, "ngân sách USD (mặc định evolution.evaluation.budget_usd)")
	f.IntVar(&limit, "limit", 0, "số tác vụ tối đa (0 = mọi tác vụ chịu ảnh hưởng)")
	f.BoolVarP(&yes, "yes", "y", false, "không hỏi xác nhận")
	f.StringVar(&harnessName, "harness", "", "harness chạy tác vụ (mặc định eval.harness)")
	f.StringVar(&agentCommand, "agent-command", "", "dùng lệnh tuỳ chỉnh làm agent (đề bài qua stdin)")
	f.StringVar(&model, "model", "", "mô hình")
	f.StringVar(&leakage, "leakage-mode", "", "strict | economy (mặc định evolution.evaluation.leakage_mode)")
	f.StringVar(&delivery, "delivery", "", "bint | minimal (mặc định evolution.evaluation.delivery)")
	f.IntVarP(&parallel, "parallel", "j", 1, "số lượt chạy song song")
	f.Uint64Var(&seed, "seed", 0, "hạt giống xáo trộn thứ tự lượt")
	f.BoolVar(&keep, "keep-workdirs", false, "giữ thư mục làm việc")
	f.BoolVar(&bare, "bare", false, "claude --bare")
	f.StringVar(&resume, "resume", "", "chạy tiếp một đợt đối chiếu của đề xuất này")
	f.BoolVar(&regression, "regression", false, "kiểm tra hồi quy khi đổi mô hình: chạy lại tập nhỏ đại diện của đề xuất (tri thức hoặc skill) đã áp dụng (base = trước đề xuất, candidate = hiện tại); không dùng làm căn cứ áp dụng")
	f.BoolVar(&prospective, "prospective", false, "chỉ dùng tác vụ có commit sau khi đề xuất được tạo (không rủi ro rò rỉ; cần khai thác tác vụ mới)")
	f.BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

// printProbePlan in kế hoạch Probe trước khi chạy: số tác vụ, R, số lượt và chi phí tối đa,
// kèm kỳ vọng nếu dừng sớm (giả định thô: điểm dừng phân bố đều trên các điểm nhìn).
func printProbePlan(w io.Writer, pp eval.ProbePlan, vm eval.VarModel, in eval.PlanInput, sequential bool) {
	fmt.Fprintf(w, "Kế hoạch Probe: σ²(R) = σ_b² + W/R; W = %.2f (%s); σ_b = %.2f (%s)\n", vm.W, vm.WSource, vm.SigmaB, vm.SigmaSource)
	if pp.SigmaB > vm.SigmaB+1e-9 {
		fmt.Fprintf(w, "  σ_b nâng lên %.2f (sàn mô hình lật √(δ(1−δ))·(1−W/0,5): tác vụ gần tất định nên hiệu ứng là lật hẳn một phần tác vụ)\n", pp.SigmaB)
	}
	if !pp.Feasible {
		fmt.Fprintf(w, "  KHÔNG KHẢ THI: %d tác vụ sẵn có, R = %d (σ = %.2f) chỉ phát hiện được chênh lệch từ %.1f điểm %% (cần %.1f điểm %%)\n",
			in.Available, pp.Repeats, pp.Sigma, 100*pp.MinDetectable, 100*in.Effect)
		return
	}
	fmt.Fprintf(w, "  %d tác vụ (trong %d sẵn có) × %d lần lặp × 2 nhánh = tối đa %d lượt (σ = %.2f, phát hiện chênh lệch %.1f điểm %%)\n",
		pp.Cases, in.Available, pp.Repeats, pp.MaxRuns, pp.Sigma, 100*in.Effect)
	frac := 1.0
	if sequential {
		k := len(evolve.SeqLooks(pp.Repeats))
		frac = float64(k+1) / float64(2*k)
		fmt.Fprintf(w, "  %d điểm nhìn ở lần lặp %v; kỳ vọng ~%.0f lượt nếu dừng sớm (giả định thô: điểm dừng phân bố đều)\n",
			k, evolve.SeqLooks(pp.Repeats), frac*float64(pp.MaxRuns))
	}
	if pp.CostKnown {
		fmt.Fprintf(w, "  chi phí tối đa ~$%.2f, kỳ vọng ~$%.2f\n", pp.MaxCost, frac*pp.MaxCost)
	} else {
		fmt.Fprintln(w, "  chi phí: chưa có lịch sử chi phí của harness để ước tính")
	}
}

func firstClause(s string) string {
	// Gom lý do theo phần trước dấu ":" thứ hai để không liệt kê từng mục.
	if i := strings.Index(s, " ("); i > 0 {
		s = s[:i]
	}
	return truncate(s, 110)
}

func nowISO(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

func newEvolveApplyCmd(stdio IO) *cobra.Command {
	var (
		unverified  bool
		confirmRisk bool
		reason      string
		asJSON      bool
	)
	c := &cobra.Command{
		Use:   "apply <id>",
		Short: "Áp dụng đề xuất vào .bint/knowledge hoặc .bint/skills/evolved (chỉ người dùng; ghi bản kiểm toán để quay lui)",
		Long: `Điều kiện (mục 12.7): phiên bản gốc còn khớp; có kết quả supported, độc lập, trên tập
test với đúng phiên bản gốc đó — hoặc --unverified (áp dụng như đề xuất L1 chưa kiểm
chứng, được ghi rõ trong lịch sử); rủi ro cao (sửa/loại bỏ quy tắc hoặc cảnh báo đã duyệt)
cần --confirm-risk. Mọi mục được kiểm tra xác định được lại trước khi ghi.

Mục mới do agent soạn vào trạng thái candidate (cần duyệt như mọi mục do agent tạo);
mục được sửa nhận người áp dụng làm người duyệt; loại bỏ dùng trạng thái deprecated.

Đề xuất skill (L2): skill mới/sửa có hiệu lực ngay (người áp dụng là người duyệt nên cần đọc
kỹ SKILL.md: bint evolve show) và được xuất bản cho harness bằng bint sync ngay sau đó; phiên
bản được ghi kèm harness/mô hình đã kiểm chứng (mục 12.6).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := openEvolve(cmd)
			if err != nil {
				return err
			}
			defer e.Close()
			res, err := evolve.Apply(cmd.Context(), evolve.ApplyOptions{K: e.k, DB: e.st.DB, Ref: args[0],
				Unverified: unverified, ConfirmRisk: confirmRisk, Reason: reason, Cases: e.caseMap()})
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(stdio.Out, res)
			}
			w := stdio.Out
			if res.Proposal != nil && res.Proposal.IsSkill() {
				defer syncAfterSkillChange(w, e.k)
			}
			kind := ""
			if res.EvidenceKind != "" {
				kind = " (" + evolve.KindLabel(res.EvidenceKind) + ")"
			}
			fmt.Fprintf(w, "Đã áp dụng %s (%s), bản ghi %s, kiểm chứng định lượng: %v%s\n", res.Proposal.ID, res.Proposal.Action, res.AdoptionID, res.Verified, kind)
			for _, f := range res.Files {
				op := "~"
				switch {
				case f.Before == nil:
					op = "+"
				case f.After == nil:
					op = "-"
				}
				fmt.Fprintf(w, "  %s %s\n", op, f.Path)
			}
			for _, n := range res.Notes {
				fmt.Fprintf(w, "Lưu ý: %s\n", n)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&unverified, "unverified", false, "áp dụng dù chưa có kết quả kiểm chứng trên tập test (ghi lại)")
	c.Flags().BoolVar(&confirmRisk, "confirm-risk", false, "chấp nhận đề xuất rủi ro cao")
	c.Flags().StringVarP(&reason, "reason", "r", "", "ghi chú của người duyệt")
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func newEvolveRejectCmd(stdio IO) *cobra.Command {
	var reason string
	c := &cobra.Command{
		Use:   "reject <id>",
		Short: "Bác bỏ đề xuất đang chờ",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := openEvolve(cmd)
			if err != nil {
				return err
			}
			defer e.Close()
			p, err := evolve.Reject(cmd.Context(), e.st.DB, args[0], reason, time.Now())
			if err != nil {
				return err
			}
			fmt.Fprintf(stdio.Out, "Đã bác bỏ %s: %s\n", p.ID, p.StatusReason)
			return nil
		},
	}
	c.Flags().StringVarP(&reason, "reason", "r", "", "lý do")
	return c
}

func newEvolveRollbackCmd(stdio IO) *cobra.Command {
	var (
		force  bool
		reason string
	)
	c := &cobra.Command{
		Use:   "rollback <id>",
		Short: "Quay lui đề xuất đã áp dụng (khôi phục tệp tri thức từ bản kiểm toán)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := openEvolve(cmd)
			if err != nil {
				return err
			}
			defer e.Close()
			res, err := evolve.Rollback(cmd.Context(), evolve.RollbackOptions{K: e.k, DB: e.st.DB, Ref: args[0], Force: force, Reason: reason})
			if err != nil {
				return err
			}
			fmt.Fprintf(stdio.Out, "Đã quay lui %s (bản ghi %s)\n", res.Proposal.ID, res.AdoptionID)
			if res.Proposal.IsSkill() {
				defer syncAfterSkillChange(stdio.Out, e.k)
			}
			for _, f := range res.Files {
				op := "~"
				if f.After == nil {
					op = "-"
				} else if f.Before == nil {
					op = "+"
				}
				fmt.Fprintf(stdio.Out, "  %s %s\n", op, f.Path)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&force, "force", false, "ghi đè cả khi tệp đã bị sửa sau khi áp dụng")
	c.Flags().StringVarP(&reason, "reason", "r", "", "lý do")
	return c
}

func newEvolveHistoryCmd(stdio IO) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "history",
		Short: "Lịch sử áp dụng/quay lui: kiểm chứng, mô hình, theo dõi sau áp dụng, chi phí tiến hoá",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			e, err := openEvolve(cmd)
			if err != nil {
				return err
			}
			defer e.Close()
			cur := evolve.CurrentModel(ctx, e.st.DB, e.k.Config.Eval.Agent.Model)
			hs, err := evolve.HistoryOf(ctx, e.st.DB, cur, e.k.Config.Evolution.Adoption.BindToModelVersion, e.caseMap())
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(stdio.Out, nonNilSlice(hs))
			}
			if len(hs) == 0 {
				fmt.Fprintln(stdio.Out, "Chưa có lần áp dụng nào.")
				return nil
			}
			w := stdio.Out
			for _, h := range hs {
				kind := ""
				if h.EvidenceKind != "" {
					kind = " " + evolve.KindLabel(h.EvidenceKind)
				}
				fmt.Fprintf(w, "%s  %-8s %s %s (%s)  %s  kiểm chứng=%v%s\n", fmtTime(h.CreatedAt), h.Action, h.ProposalID, h.ProposalAction,
					strings.Join(h.Items, ","), h.Actor, h.Verified, kind)
				if h.Reason != "" {
					fmt.Fprintf(w, "    %s\n", h.Reason)
				}
				if h.ModelStatus != "" {
					fmt.Fprintf(w, "    mô hình: %s\n", h.ModelStatus)
				}
				if h.Action == "apply" {
					fmt.Fprintf(w, "    sau áp dụng: truy xuất %d lượt, trích dẫn %d lượt (chỉ là gợi ý; tác động cần đánh giá loại bỏ)\n", h.RetrievedSince, h.CitedSince)
				}
				fmt.Fprintf(w, "    chi phí tiến hoá: tạo ứng viên %s, đối chiếu %s\n", fmtOptF(h.GenerationUSD, "$%.4f"), fmtOptF(h.ProbeUSD, "$%.2f"))
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}
