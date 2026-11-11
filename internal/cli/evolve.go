package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"bint/internal/app"
	"bint/internal/app/evolvesvc"
	"bint/internal/eval"
	"bint/internal/evolve"
	"bint/internal/knowledge"
	"bint/internal/skills"
)

// openEvolve mở không gian tiến hoá ở thư mục hiện tại: cần repository đã `bint init` (cơ sở
// dữ liệu trạng thái); nạp spool trước để số liệu gồm cả sự kiện mới nhất.
func openEvolve(cmd *cobra.Command) (*evolvesvc.Workspace, error) {
	dir, err := cwd()
	if err != nil {
		return nil, err
	}
	return evolvesvc.Open(cmd.Context(), dir)
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
  bint evolve cycle --budget <USD>  một vòng tự động Propose–Probe–Compose (P8)

Cấp độ: L1 tiến hoá tri thức (P6); L2 tiến hoá skill chuyên biệt (P7): tạo skill từ quy trình
được xác nhận hiệu quả nhiều lần, hợp nhất skill trùng, loại bỏ skill lỗi thời. Skill gốc bất
biến, không bao giờ là đối tượng của đề xuất (xem bint skill).`,
	}
	c.AddCommand(newEvolveScanCmd(stdio), newEvolveProposeCmd(stdio), newEvolveListCmd(stdio), newEvolveShowCmd(stdio),
		newEvolveEvaluateCmd(stdio), newEvolveApplyCmd(stdio), newEvolveRejectCmd(stdio), newEvolveRollbackCmd(stdio),
		newEvolveHistoryCmd(stdio), newEvolveQueueCmd(stdio), newEvolveProcessCmd(stdio), newEvolveCycleCmd(stdio))
	return c
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
  duplicate         mục trùng (cùng loại, phạm vi chồng, tiêu đề gần trùng) → merge
  failure_lesson    nhóm ≥ min_similar_runs tác vụ thất bại (tập validation) cùng vùng/dấu hiệu → create lesson (reflector)`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := openEvolve(cmd)
			if err != nil {
				return err
			}
			defer e.Close()
			res, err := evolvesvc.Scan(cmd.Context(), e, evolvesvc.ScanOptions{Days: days, NoGit: noGit})
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

func newEvolveProposeCmd(stdio IO) *cobra.Command {
	var (
		budget         float64
		max            int
		days           int
		agentCommand   string
		model          string
		reflectorModel string
		noAgent        bool
		asJSON         bool
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
			e, err := openEvolve(cmd)
			if err != nil {
				return err
			}
			defer e.Close()
			res, err := evolvesvc.Propose(cmd.Context(), e, evolvesvc.ProposeOptions{Refs: args, BudgetUSD: budget, Max: max,
				Days: days, AgentCommand: agentCommand, Model: model, ReflectorModel: reflectorModel, NoAgent: noAgent, Warn: stdio.Err})
			if err != nil {
				return err
			}
			rep, scan := res.ProposeReport, res.Scan
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
	f.StringVar(&reflectorModel, "reflector-model", "", "mô hình cho agent reflector (rút bài học từ thất bại)")
	f.BoolVar(&noAgent, "no-agent", false, "chỉ tạo đề xuất xác định (không gọi mô hình)")
	f.BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func changeIDs(p *evolve.Proposal) []string {
	if p.IsSkill() {
		return evolve.SkillChangeNames(p)
	}
	if p.IsHarness() {
		return evolve.HarnessChangeNames(p)
	}
	var out []string
	for _, c := range p.Changes {
		out = append(out, c.Op+":"+c.ItemID)
	}
	return out
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
			rows, err := evolvesvc.List(cmd.Context(), e, evolvesvc.ListOptions{All: all})
			if err != nil {
				return err
			}
			if asJSON {
				type row struct {
					*evolve.Proposal
					State string `json:"state"`
				}
				out := []row{}
				for _, r := range rows {
					out = append(out, row{r.Proposal, r.State})
				}
				return writeJSON(stdio.Out, out)
			}
			if len(rows) == 0 {
				fmt.Fprintln(stdio.Out, "Không có đề xuất nào (`bint evolve scan`, `bint evolve propose`).")
				return nil
			}
			tw := tabwriter.NewWriter(stdio.Out, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tTRẠNG THÁI\tHÀNH ĐỘNG\tRỦI RO\tVALIDATION\tTEST\tTẠO LÚC\tLÝ DO")
			for _, r := range rows {
				p := r.Proposal
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", p.ID, r.State, p.Action, p.Risk,
					latestVerdict(r.Evaluations, eval.SplitValidation), latestVerdict(r.Evaluations, eval.SplitTest), fmtTime(p.CreatedAt), truncate(p.Reason, 70))
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
			e, err := openEvolve(cmd)
			if err != nil {
				return err
			}
			defer e.Close()
			r, err := evolvesvc.Show(cmd.Context(), e, evolvesvc.ShowOptions{ID: args[0]})
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(stdio.Out, map[string]any{"proposal": r.Proposal, "state": r.State, "base_problems": nonNilSlice(r.BaseProblems),
					"evaluations": nonNilSlice(r.Evaluations), "adoptions": nonNilSlice(r.Adoptions), "label_drift": nonNilSlice(r.LabelDrift)})
			}
			printProposal(stdio.Out, r.Proposal, r.State, r.BaseProblems, r.Evaluations, r.Adoptions, r.LabelDrift, r.Items, r.Evolved, r.Cases)
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
	if p.Pair != nil {
		fmt.Fprintf(w, "Cặp:         bản %s (anh em %s%s) — đo trên tập khác nhau, chỉ một bản được giữ (không gộp)\n",
			map[string]string{evolve.PairGlobal: "chung", evolve.PairDomain: "riêng"}[p.Pair.Variant], p.Pair.Sibling,
			map[bool]string{true: ", vùng " + p.Pair.Area, false: ""}[p.Pair.Area != ""])
	}
	fmt.Fprintln(w, "\nThay đổi:")
	printSkillChanges(w, p, evolved)
	printHarnessChanges(w, p)
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
			w := stdio.Out
			opts := evolvesvc.EvaluateDefaults(e.K.Config)
			opts.ID = args[0]
			if cmd.Flags().Changed("split") {
				opts.Split = split
			}
			if cmd.Flags().Changed("repeats") {
				opts.Repeats, opts.RepeatsExplicit = repeats, true
			}
			if cmd.Flags().Changed("budget") {
				opts.BudgetUSD = &budget
			}
			if cmd.Flags().Changed("delivery") {
				opts.Delivery, opts.DeliveryExplicit = delivery, true
			}
			if harnessName != "" {
				opts.Harness = harnessName
			}
			if leakage != "" {
				opts.LeakageMode = leakage
			}
			opts.Force, opts.Limit, opts.Yes, opts.AgentCommand, opts.Model = force, limit, yes, agentCommand, model
			opts.Parallel, opts.Seed, opts.KeepWorkdirs, opts.Bare = parallel, seed, keep, bare
			opts.Resume, opts.Regression, opts.Prospective, opts.BintVersion = resume, regression, prospective, Version
			if isTerminal(stdio.In) {
				opts.Confirm = func(req app.ConfirmRequest) bool { return confirm(stdio, req.Text) }
			}
			opts.OnSelect = func(s evolvesvc.EvaluateSelection) {
				if s.Prospective {
					fmt.Fprintf(w, "Tiến cứu: bỏ %d tác vụ có commit trước khi đề xuất được tạo\n", s.Older)
				}
				fmt.Fprintf(w, "Tập %s: %d tác vụ đã kiểm chứng (%d chưa kiểm chứng bị bỏ), %d chịu ảnh hưởng của %s\n",
					s.Split, s.Pool, s.Unverified, s.Chosen, s.ProposalID)
				for _, r := range sortedMapKeys(s.Dropped) {
					fmt.Fprintf(w, "  bỏ %d: %s\n", s.Dropped[r], r)
				}
			}
			opts.OnPlan = func(pp eval.ProbePlan, vm eval.VarModel, in eval.PlanInput, sequential bool) {
				printProbePlan(w, pp, vm, in, sequential)
			}
			opts.OnEstimate = func(est eval.Estimate, arms []string, reps int, bud *float64) {
				printEstimate(w, est, arms, reps, bud)
			}
			if !asJSON {
				opts.Progress = func(r eval.AttemptRow, n, total int) {
					fmt.Fprintf(w, "[%d/%d] %s %-9s #%d  %-8s%s\n", n, total, r.CaseID, r.Arm, r.Repeat, r.Status, attemptExtras(r))
				}
			}
			res, err := evolvesvc.Evaluate(ctx, e, opts)
			if err != nil {
				return err
			}
			p, record, v, rep, runErr := res.Proposal, res.Record, res.Verdict, res.Report, res.RunErr
			if asJSON {
				return writeJSON(w, map[string]any{"evaluation_id": record.ID, "batch_id": res.Run.BatchID, "status": res.Run.Status,
					"verdict": v, "report": rep, "skill_activation": res.Activation})
			}
			fmt.Fprintf(w, "\nĐợt %s: %s, %d/%d lượt\n", res.Run.BatchID, res.Run.Status, res.Run.Done, res.Run.Total)
			if res.Run.Status == eval.StatusStoppedEarly {
				fmt.Fprintf(w, "Dừng sớm: %s\n", res.Run.StopReason)
			}
			printReport(w, rep, false)
			for _, a := range res.Activation {
				fmt.Fprintf(w, "  skill %s, nhánh %s: được gọi ở %d/%d lượt đo được\n", a.Skill, a.Arm, a.Activated, a.Measured)
			}
			fmt.Fprintf(w, "\nKết luận (%s, tập %s): %s — %s\n", p.Plan.Kind, orDash(record.Split), v.Verdict, v.Detail)
			for _, n := range v.Notes {
				fmt.Fprintf(w, "  lưu ý: %s\n", n)
			}
			if res.Run.Status == "cancelled" || errors.Is(runErr, eval.ErrBudget) || errors.Is(runErr, eval.ErrQuota) {
				fmt.Fprintf(w, "Chạy tiếp: bint evolve evaluate %s --resume %s\n", p.ID, res.Run.BatchID)
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
			res, err := evolvesvc.Apply(cmd.Context(), e, evolvesvc.ApplyOptions{Ref: args[0],
				Unverified: unverified, ConfirmRisk: confirmRisk, Reason: reason})
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(stdio.Out, res)
			}
			w := stdio.Out
			if res.Proposal != nil && res.Proposal.IsSkill() {
				defer syncAfterSkillChange(w, e.K)
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
			p, err := evolvesvc.Reject(cmd.Context(), e, evolvesvc.RejectOptions{Ref: args[0], Reason: reason})
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
			res, err := evolvesvc.Rollback(cmd.Context(), e, evolvesvc.RollbackOptions{Ref: args[0], Force: force, Reason: reason})
			if err != nil {
				return err
			}
			fmt.Fprintf(stdio.Out, "Đã quay lui %s (bản ghi %s)\n", res.Proposal.ID, res.AdoptionID)
			if res.Proposal.IsSkill() {
				defer syncAfterSkillChange(stdio.Out, e.K)
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
			e, err := openEvolve(cmd)
			if err != nil {
				return err
			}
			defer e.Close()
			res, err := evolvesvc.History(cmd.Context(), e, evolvesvc.HistoryOptions{})
			if err != nil {
				return err
			}
			hs := res.Entries
			if asJSON {
				return writeJSON(stdio.Out, nonNilSlice(hs))
			}
			w := stdio.Out
			if n, sum := res.Cycles, res.CycleTotals; n > 0 {
				fmt.Fprintf(w, "Vòng tự động (bint evolve cycle): %d vòng, đã tiêu $%.2f (tạo ứng viên $%.2f, đối chiếu $%.2f); tự áp dụng %d, tự quay lui %d, tự bác bỏ %d\n",
					n, sum.SpentUSD, sum.GenerationUSD, sum.ProbeUSD, len(sum.AutoApplied), len(sum.RolledBack), len(sum.AutoRejected))
			}
			if len(hs) == 0 {
				fmt.Fprintln(stdio.Out, "Chưa có lần áp dụng nào.")
				return nil
			}
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
