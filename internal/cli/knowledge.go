package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"bint/internal/app"
	"bint/internal/knowledge"
)

var findingIcon = map[string]string{knowledge.LevelError: "✗", knowledge.LevelWarn: "!"}

func openKnowledge(cmd *cobra.Command) (*app.KnowledgeWS, error) {
	dir, err := cwd()
	if err != nil {
		return nil, err
	}
	return app.OpenKnowledge(cmd.Context(), dir)
}

func newKnowledgeCmd(stdio IO) *cobra.Command {
	c := &cobra.Command{
		Use:   "knowledge",
		Short: "Tri thức dự án: add, list, show, verify, review, approve, reject",
		Long: `Mỗi mục tri thức là một tệp .bint/knowledge/items/<ulid>.md (commit được) gồm phần
đầu YAML (loại, phạm vi, bằng chứng, nguồn gốc, người duyệt) và nội dung Markdown.

Trạng thái: candidate → verified → stale → deprecated (xem ` + "`bint knowledge transitions`" + `).
Mục do người ghi (origin: human) có hiệu lực ngay; mục do agent ghi chờ người duyệt.
Chỉ người mới duyệt được quy tắc bắt buộc (rule).`,
	}
	c.AddCommand(newKnowledgeAddCmd(stdio), newKnowledgeListCmd(stdio), newKnowledgeShowCmd(stdio),
		newKnowledgeVerifyCmd(stdio), newKnowledgeReviewCmd(stdio), newKnowledgeApproveCmd(stdio),
		newKnowledgeRejectCmd(stdio, "reject", "Từ chối mục chờ duyệt (→ deprecated, giữ dấu vết)"),
		newKnowledgeRejectCmd(stdio, "deprecate", "Loại bỏ một mục (→ deprecated)"),
		newKnowledgeRmCmd(stdio), newKnowledgeTransitionsCmd(stdio),
		newKnowledgeSearchCmd(stdio), newKnowledgeStatsCmd(stdio), newKnowledgeDiffCmd(stdio),
		newKnowledgeRevalidateCmd(stdio))
	return c
}

func newKnowledgeAddCmd(stdio IO) *cobra.Command {
	var (
		o        app.AddOptions
		bodyFile string
		asJSON   bool
	)
	c := &cobra.Command{
		Use:   "add [nội dung | -]",
		Short: "Ghi nhanh một mục tri thức",
		Long: `Ghi một mục tri thức. Nội dung lấy từ đối số, từ stdin ("-") hoặc --body-file.

Người dùng (origin human, mặc định ngoài phiên agent): mục có hiệu lực ngay, tác
giả = người duyệt = danh tính Git. Agent (origin agent, mặc định trong phiên agent):
mục ở trạng thái candidate, cần ít nhất một --evidence, chờ người duyệt.

Bằng chứng: --evidence 'path[:a-b][#Symbol][@commit]' (lặp lại được). Fingerprint
của vùng dòng (hoặc cả tệp) được tính để phát hiện khi bằng chứng thay đổi.

Mục không qua kiểm tra xác định được (đường dẫn, dòng, ký hiệu, ví dụ mã, trùng
lặp, cờ bên thứ ba) thì không được ghi.

--from-review <finding-id> chuyển một phát hiện của b-review (ID <task-id>.<n>) thành
mục warning (mặc định) hoặc lesson: tiêu đề, nội dung, phạm vi và bằng chứng lấy từ phát
hiện; cờ khác ghi đè. Mục do agent đề xuất theo cách này vẫn ở trạng thái candidate.`,
		Example: `  bint knowledge add --type warning --paths 'internal/db/**' \
    --evidence internal/db/db.go:40-52 "Mở SQLite bằng store.Open; tự mở sql.Open sẽ thiếu busy_timeout"
  bint knowledge add --type rule --title "Không sửa tay *.pb.go" --evidence Makefile:12 - < note.md`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			k, err := openKnowledge(cmd)
			if err != nil {
				return err
			}
			if o.FromReview != "" {
				if err := o.FillFromReview(k); err != nil { // trước Validate: --type không bắt buộc khi có phát hiện
					return err
				}
			}
			switch {
			case bodyFile != "":
				b, err := os.ReadFile(bodyFile)
				if err != nil {
					return err
				}
				o.Body = string(b)
			case len(args) == 1 && args[0] == "-":
				b, err := io.ReadAll(stdio.In)
				if err != nil {
					return err
				}
				o.Body = string(b)
			case len(args) == 1:
				o.Body = args[0]
			}
			if err := o.Validate(); err != nil {
				return err
			}
			res, err := app.AddKnowledge(cmd.Context(), k, o)
			if err != nil {
				return err
			}
			if asJSON {
				if err := writeJSON(stdio.Out, map[string]any{"saved": res.Saved, "id": res.Item.ID, "status": res.Item.Status,
					"origin": res.Origin, "findings": nonNilFindings(res.Check.Findings)}); err != nil {
					return err
				}
			} else {
				printFindings(stdio.Out, res.Check.Findings, "  ", evRef{})
				if res.Saved {
					fmt.Fprintf(stdio.Out, "Đã ghi %s [%s/%s, origin %s]: %s\n", res.Item.ID, res.Item.Type, res.Item.Status, res.Origin, res.Item.Title)
					if res.Item.Status == knowledge.StatusCandidate {
						fmt.Fprintln(stdio.Out, "Mục chờ người duyệt: bint knowledge review")
					}
				}
			}
			if !res.Saved {
				return withCode(ExitError, fmt.Errorf("mục không qua kiểm tra, chưa được ghi"))
			}
			return nil
		},
	}
	f := c.Flags()
	f.StringVarP(&o.Type, "type", "t", "", "loại: rule | warning | pattern | fact | decision | lesson (bắt buộc, trừ --from-review)")
	f.StringVar(&o.Title, "title", "", "tiêu đề một dòng (mặc định: dòng đầu nội dung)")
	f.StringSliceVar(&o.Paths, "paths", nil, "phạm vi đường dẫn (glob), bỏ trống = toàn repository")
	f.StringSliceVar(&o.Symbols, "symbols", nil, "ký hiệu liên quan")
	f.StringSliceVar(&o.Tags, "tags", nil, "thẻ")
	f.StringSliceVar(&o.Domains, "domains", nil, "miền nghiệp vụ")
	f.StringArrayVarP(&o.Evidence, "evidence", "e", nil, "bằng chứng path[:a-b][#Symbol][@commit] (lặp lại được)")
	f.StringArrayVar(&o.Inputs, "input", nil, "đầu vào đã đọc để rút ra mục này (lặp lại được)")
	f.StringVar(&o.Origin, "origin", "", "human | agent (mặc định theo môi trường)")
	f.BoolVar(&o.ThirdParty, "third-party", false, "mục dựa trên nội dung bên thứ ba (dependency, tài liệu ngoài, issue)")
	f.StringVar(&o.Confidence, "confidence", "", "high | medium | low")
	f.StringVar(&o.RunID, "run-id", "", "lượt chạy đã tạo mục này")
	f.StringVar(&o.EvalCase, "eval-case", "", "tác vụ đánh giá mà mục này rút ra từ đó")
	f.StringVar(&bodyFile, "body-file", "", "đọc nội dung từ tệp")
	f.BoolVar(&asJSON, "json", false, "xuất JSON")
	f.StringVar(&o.FromReview, "from-review", "", "chuyển phát hiện review <task-id>.<n> thành mục warning/lesson")
	f.MarkHidden("eval-case")
	return c
}

func nonNilFindings(f []knowledge.Finding) []knowledge.Finding {
	if f == nil {
		return []knowledge.Finding{}
	}
	return f
}

func newKnowledgeListCmd(stdio IO) *cobra.Command {
	var (
		lo     app.KnowledgeListOptions
		asJSON bool
	)
	c := &cobra.Command{
		Use:   "list",
		Short: "Liệt kê tri thức (mặc định: trừ deprecated)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			k, err := openKnowledge(cmd)
			if err != nil {
				return err
			}
			res, err := app.ListKnowledge(cmd.Context(), k, lo)
			if err != nil {
				return err
			}
			out, bad := res.Items, res.Bad
			if asJSON {
				type row struct {
					*knowledge.Item
					Body string `json:"body"`
					Path string `json:"path"`
				}
				rows := []row{}
				for _, it := range out {
					rows = append(rows, row{it, it.Body, it.Path})
				}
				return writeJSON(stdio.Out, rows)
			}
			for _, b := range bad {
				fmt.Fprintf(stdio.Err, "✗ không đọc được %s\n", b.Error())
			}
			if len(out) == 0 {
				fmt.Fprintln(stdio.Out, "Chưa có mục tri thức nào. Ghi bằng `bint knowledge add`, hoặc chạy /b-bootstrap trong harness.")
				return nil
			}
			tw := tabwriter.NewWriter(stdio.Out, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tLOẠI\tTRẠNG THÁI\tNGUỒN\tPHẠM VI\tTIÊU ĐỀ")
			counts := map[string]int{}
			for _, it := range out {
				counts[it.Status]++
				scope := "*"
				if !it.Scope.Global() {
					scope = strings.Join(it.Scope.Paths, ",")
				}
				origin := it.Origin.Kind
				if it.Origin.ThirdParty {
					origin += "⚑"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", it.ID, it.Type, it.Status, origin, truncate(scope, 30), truncate(it.Title, 70))
			}
			tw.Flush()
			var parts []string
			for _, s := range knowledge.Statuses {
				if counts[s] > 0 {
					parts = append(parts, fmt.Sprintf("%s=%d", s, counts[s]))
				}
			}
			fmt.Fprintf(stdio.Out, "\n%d mục: %s\n", len(out), strings.Join(parts, " "))
			if counts[knowledge.StatusCandidate]+counts[knowledge.StatusStale] > 0 {
				fmt.Fprintln(stdio.Out, "Có mục chờ duyệt: bint knowledge review")
			}
			return nil
		},
	}
	f := c.Flags()
	f.StringSliceVar(&lo.Status, "status", nil, "lọc theo trạng thái")
	f.StringSliceVar(&lo.Type, "type", nil, "lọc theo loại")
	f.BoolVar(&lo.All, "all", false, "gồm cả mục deprecated")
	f.BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

// printItem trình bày đầy đủ một mục kèm trích đoạn bằng chứng.
func printItem(w io.Writer, d app.KnowledgeDetail) {
	it := d.Item
	fmt.Fprintf(w, "%s  [%s/%s]\n", it.ID, it.Type, it.Status)
	fmt.Fprintf(w, "Tiêu đề:     %s\n", it.Title)
	scope := "toàn repository"
	if !it.Scope.Global() {
		scope = strings.Join(it.Scope.Paths, ", ")
	}
	fmt.Fprintf(w, "Phạm vi:     %s\n", scope)
	if len(it.Scope.Symbols)+len(it.Scope.Tags)+len(it.Scope.Domains) > 0 {
		fmt.Fprintf(w, "Ký hiệu/thẻ: %s\n", strings.Join(append(append(append([]string{}, it.Scope.Symbols...), it.Scope.Tags...), it.Scope.Domains...), ", "))
	}
	origin := it.Origin.Kind
	if it.Origin.ThirdParty {
		origin += " — ⚑ có đầu vào bên thứ ba"
	}
	if it.Origin.Session != "" {
		origin += ", phiên " + it.Origin.Session
	}
	if it.Origin.RunID != "" {
		origin += ", lượt " + it.Origin.RunID
	}
	fmt.Fprintf(w, "Nguồn gốc:   %s (tác giả %s, %s)\n", origin, it.Author, fmtISO(it.CreatedAt))
	if len(it.Origin.Inputs) > 0 {
		fmt.Fprintf(w, "Đầu vào:     %s\n", strings.Join(it.Origin.Inputs, ", "))
	}
	if it.ReviewedBy != "" {
		fmt.Fprintf(w, "Người duyệt: %s (%s)\n", it.ReviewedBy, fmtISO(it.ReviewedAt))
	}
	if it.StatusReason != "" {
		fmt.Fprintf(w, "Lý do:       %s\n", it.StatusReason)
	}
	if it.Confidence != "" {
		fmt.Fprintf(w, "Tin cậy:     %s\n", it.Confidence)
	}
	fmt.Fprintf(w, "Tệp:         %s\n", it.Path)
	fmt.Fprintf(w, "\n%s\n", indent(it.Body, "  "))
	if len(d.Evidence) > 0 {
		fmt.Fprintln(w, "\nBằng chứng:")
		for _, ev := range d.Evidence {
			e := ev.Evidence
			extra := ""
			if e.Commit != "" {
				extra = " (commit " + short(e.Commit) + ")"
			}
			fmt.Fprintf(w, "  %s%s\n", e, extra)
			if ev.Missing {
				fmt.Fprintln(w, "    (không còn tệp)")
				continue
			}
			for _, l := range ev.Lines {
				fmt.Fprintf(w, "    │ %s\n", l)
			}
			if ev.More > 0 {
				fmt.Fprintf(w, "    │       … (%d dòng nữa)\n", ev.More)
			}
		}
	}
}

func indent(s, pre string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = pre + l
		}
	}
	return strings.Join(lines, "\n")
}

func newKnowledgeShowCmd(stdio IO) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "show <id>",
		Short: "Xem một mục kèm trích đoạn bằng chứng và kết quả kiểm tra",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			k, err := openKnowledge(cmd)
			if err != nil {
				return err
			}
			d, err := app.ShowKnowledge(cmd.Context(), k, app.KnowledgeShowOptions{Ref: args[0]})
			if err != nil {
				return err
			}
			it, r := d.Item, d.Check
			if asJSON {
				return writeJSON(stdio.Out, map[string]any{"item": it, "body": it.Body, "path": it.Path, "findings": nonNilFindings(r.Findings)})
			}
			printItem(stdio.Out, d)
			if len(r.Findings) > 0 {
				fmt.Fprintln(stdio.Out, "\nKiểm tra:")
				printFindings(stdio.Out, r.Findings, "  ", evRefOf(k, it.ID))
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func newKnowledgeVerifyCmd(stdio IO) *cobra.Command {
	var apply, asJSON bool
	c := &cobra.Command{
		Use:     "verify [id...]",
		Aliases: []string{"check"},
		Short:   "Kiểm tra xác định được: đường dẫn, dòng, ký hiệu, ví dụ mã, fingerprint, trùng lặp",
		Long: `Kiểm tra mọi mục còn hiệu lực (hoặc các mục được chỉ định) mà không gọi mô hình.

--apply áp các chuyển trạng thái tự động (chỉ người dùng chạy được):
  candidate → verified  loại không cần người duyệt (không phải rule, không trong
                        knowledge.require_human_approval_for, không gắn cờ bên thứ ba)
                        và không có lỗi
  verified  → stale     bằng chứng thay đổi hoặc không còn
  stale     → verified  loại rủi ro thấp (fact, pattern, decision, lesson): mọi kiểm
                        tra trừ fingerprint qua → fingerprint được làm mới
  vùng dòng             bằng chứng chỉ bị dịch chuyển (nội dung không đổi) → cập
                        nhật lines, trạng thái giữ nguyên
  chuyển tệp            khối bằng chứng không còn ở tệp cũ (tệp mất, ngắn đi, ký hiệu
                        mất) và nằm nguyên vẹn ở đúng một tệp khác → cập nhật path và
                        lines, trạng thái giữ nguyên; nội dung đổi tại chỗ thì không
                        tìm chuyển tệp (vẫn là stale/changed), nhiều nơi khớp hoặc tìm
                        bị cắt theo giới hạn chi phí thì không áp

Mục stale rủi ro cao (rule, warning) cần người duyệt (bint knowledge revalidate
để agent nhận định trước). Mục stale quá knowledge.stale_deprecate_days được đề
xuất deprecated; Bint không tự chuyển.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			k, err := openKnowledge(cmd)
			if err != nil {
				return err
			}
			rep, err := app.VerifyKnowledge(cmd.Context(), k, args, apply, time.Now())
			if err != nil {
				return err
			}
			errorsN, warnN := 0, 0
			for _, r := range rep.Results {
				for _, f := range r.Findings {
					if f.Level == knowledge.LevelError {
						errorsN++
					} else {
						warnN++
					}
				}
			}
			if asJSON {
				if err := writeJSON(stdio.Out, map[string]any{"results": rep.Results, "transitions": rep.Transitions,
					"relocated": nonNilStrings(rep.Relocated), "moved": nonNilStrings(rep.Moved), "deprecate_proposals": nonNilStrings(rep.DeprecateProposals),
					"needs_review": nonNilStrings(rep.NeedsReview),
					"load_errors":  loadErrStrings(rep.LoadErrors), "errors": errorsN, "warnings": warnN}); err != nil {
					return err
				}
			} else {
				for _, b := range rep.LoadErrors {
					fmt.Fprintf(stdio.Out, "✗ %s\n", b.Error())
				}
				for _, r := range rep.Results {
					it := rep.Items[r.ID]
					icon := "✓"
					if !r.OK() {
						icon = "✗"
					} else if len(r.Findings) > 0 {
						icon = "!"
					}
					fmt.Fprintf(stdio.Out, "%s %s [%s/%s] %s\n", icon, r.ID, it.Type, it.Status, truncate(it.Title, 70))
					printFindings(stdio.Out, r.Findings, "    ", evRefOf(k, r.ID))
				}
				for _, t := range rep.Transitions {
					fmt.Fprintf(stdio.Out, "→ %s: %s → %s (%s)\n", t.ID, t.From, t.To, t.Reason)
				}
				if len(rep.Relocated) > 0 {
					fmt.Fprintf(stdio.Out, "↕ cập nhật vị trí bằng chứng (nội dung không đổi): %s\n", strings.Join(rep.Relocated, ", "))
				}
				if len(rep.Moved) > 0 {
					fmt.Fprintf(stdio.Out, "⇄ trong đó đổi đường dẫn vì khối chuyển tệp: %s\n", strings.Join(rep.Moved, ", "))
				}
				if len(rep.NeedsReview) > 0 {
					fmt.Fprintf(stdio.Out, "? stale rủi ro cao, cần xác thực lại (`bint knowledge revalidate` hoặc `approve --refresh`): %s\n", strings.Join(rep.NeedsReview, ", "))
				}
				if len(rep.DeprecateProposals) > 0 {
					fmt.Fprintf(stdio.Out, "⌛ stale quá %d ngày, đề xuất `bint knowledge deprecate`: %s\n", k.Config.Knowledge.StaleDeprecateDays, strings.Join(rep.DeprecateProposals, ", "))
				}
				fmt.Fprintf(stdio.Out, "\n%d mục, %d lỗi, %d cảnh báo", len(rep.Results), errorsN, warnN)
				if apply {
					fmt.Fprintf(stdio.Out, ", %d chuyển trạng thái", len(rep.Transitions))
				}
				fmt.Fprintln(stdio.Out)
			}
			if errorsN > 0 || len(rep.LoadErrors) > 0 {
				return withCode(ExitError, fmt.Errorf("có mục không qua kiểm tra"))
			}
			return nil
		},
	}
	c.Flags().BoolVar(&apply, "apply", false, "áp chuyển trạng thái tự động (candidate→verified cho loại không cần người duyệt; verified→stale) và cập nhật vị trí bằng chứng (lines, cả path khi khối chuyển tệp)")
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func loadErrStrings(bad []knowledge.LoadError) []string {
	out := []string{}
	for _, b := range bad {
		out = append(out, b.Error())
	}
	return out
}

func printReviewOutcomes(w io.Writer, outs []app.ReviewOutcome) int {
	failed := 0
	for _, o := range outs {
		if o.Err != nil {
			failed++
			fmt.Fprintf(w, "✗ %s: %s\n", o.ID, reviewErrText(o.Err))
			var ee *app.EvidenceError
			if o.Check != nil && !errors.As(o.Err, &ee) {
				printFindings(w, o.Check.Findings, "    ", evRef{ID: o.ID})
			}
			continue
		}
		fmt.Fprintf(w, "✓ %s: %s → %s\n", o.ID, o.From, o.To)
	}
	return failed
}

func newKnowledgeApproveCmd(stdio IO) *cobra.Command {
	var (
		refresh bool
		note    string
	)
	c := &cobra.Command{
		Use:   "approve <id>...",
		Short: "Duyệt mục (candidate/stale → verified); chỉ người dùng",
		Long: `Duyệt mục tri thức. Mục phải qua kiểm tra xác định được. Người duyệt (danh tính
Git) và thời điểm được ghi vào tệp. Không chạy được từ trong phiên agent.

Mục stale (bằng chứng đã đổi): xem lại bằng ` + "`bint knowledge show`" + ` rồi duyệt với
--refresh để ghi fingerprint mới.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			k, err := openKnowledge(cmd)
			if err != nil {
				return err
			}
			outs, err := app.ApproveKnowledge(cmd.Context(), k, app.ApproveOptions{Refs: args, Note: note, Refresh: refresh})
			if err != nil {
				return err
			}
			if n := printReviewOutcomes(stdio.Out, outs); n > 0 {
				return withCode(ExitError, fmt.Errorf("%d mục không duyệt được", n))
			}
			fmt.Fprintln(stdio.Out, "Đưa quy tắc đã duyệt vào AGENTS.md: bint bootstrap publish")
			return nil
		},
	}
	c.Flags().BoolVar(&refresh, "refresh", false, "làm mới fingerprint của bằng chứng (đã xem lại bằng chứng hiện tại)")
	c.Flags().StringVar(&note, "note", "", "ghi chú của người duyệt")
	return c
}

func newKnowledgeRejectCmd(stdio IO, use, short string) *cobra.Command {
	var reason string
	c := &cobra.Command{
		Use:   use + " <id>...",
		Short: short + "; chỉ người dùng",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ro := app.RejectOptions{Refs: args, Reason: reason}
			if err := ro.Validate(); err != nil {
				return err
			}
			k, err := openKnowledge(cmd)
			if err != nil {
				return err
			}
			outs, err := app.RejectKnowledge(cmd.Context(), k, ro)
			if err != nil {
				return err
			}
			if n := printReviewOutcomes(stdio.Out, outs); n > 0 {
				return withCode(ExitError, fmt.Errorf("%d mục không xử lý được", n))
			}
			return nil
		},
	}
	c.Flags().StringVar(&reason, "reason", "", "lý do (bắt buộc)")
	return c
}

func newKnowledgeRmCmd(stdio IO) *cobra.Command {
	return &cobra.Command{
		Use:   "rm <id>...",
		Short: "Xoá tệp của mục candidate (mục đã duyệt dùng deprecate)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			k, err := openKnowledge(cmd)
			if err != nil {
				return err
			}
			removed, err := app.RemoveKnowledge(k, args)
			for _, id := range removed {
				fmt.Fprintf(stdio.Out, "Đã xoá %s\n", id)
			}
			return err
		},
	}
}

func newKnowledgeTransitionsCmd(stdio IO) *cobra.Command {
	return &cobra.Command{
		Use:   "transitions",
		Short: "Bảng chuyển trạng thái của tri thức",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			tw := tabwriter.NewWriter(stdio.Out, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "TỪ\tĐẾN\tĐIỀU KIỆN")
			for _, r := range knowledge.TransitionTable {
				fmt.Fprintf(tw, "%s\t%s\t%s\n", r.From, r.To, r.Condition)
			}
			tw.Flush()
			fmt.Fprintln(stdio.Out, "\nAgent không bao giờ chuyển được trạng thái hiệu lực; mọi thao tác duyệt bị từ chối trong phiên agent.")
			return nil
		},
	}
}

func newKnowledgeReviewCmd(stdio IO) *cobra.Command {
	statuses := app.ReviewQueueDefaults().Statuses
	c := &cobra.Command{
		Use:   "review",
		Short: "Duyệt theo lô các mục chờ (candidate, stale); chỉ người dùng",
		Long: `Hiển thị lần lượt các mục chờ duyệt, mỗi mục kèm bằng chứng, nguồn gốc, phạm vi và
kết quả kiểm tra. Mỗi mục nhập một phím rồi Enter:

  a  chấp nhận (→ verified)        r  từ chối (→ deprecated, hỏi lý do)
  e  sửa trong $EDITOR rồi xem lại  s  bỏ qua
  f  chấp nhận và làm mới fingerprint (mục stale)
  q  dừng`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := cwd()
			if err != nil {
				return err
			}
			rs, err := app.ReviewQueue(cmd.Context(), dir, app.ReviewQueueOptions{Statuses: statuses})
			if err != nil {
				return err
			}
			k, pending := rs.K, rs.Pending
			if len(pending) == 0 {
				fmt.Fprintln(stdio.Out, "Không có mục chờ duyệt.")
				return nil
			}
			in := bufio.NewReader(stdio.In)
			ask := func(q string) (string, bool) {
				fmt.Fprint(stdio.Out, q)
				line, err := in.ReadString('\n')
				if err != nil && line == "" {
					return "", false
				}
				return strings.TrimSpace(line), true
			}
			stats := map[string]int{}
		loop:
			for i, id := range pending {
				for {
					items, _, err := k.Items()
					if err != nil {
						return err
					}
					it, err := knowledge.Find(items, id)
					if err != nil {
						break // mục bị xoá trong lúc sửa
					}
					fmt.Fprintf(stdio.Out, "\n━━ %d/%d ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n", i+1, len(pending))
					d := k.DetailOf(it, items)
					printItem(stdio.Out, d)
					if r := d.Check; len(r.Findings) > 0 {
						fmt.Fprintln(stdio.Out, "\nKiểm tra:")
						printFindings(stdio.Out, r.Findings, "  ", evRefOf(k, it.ID))
					}
					a, ok := ask("\n[a]chấp nhận [r]từ chối [e]sửa [f]chấp nhận+làm mới fingerprint [s]bỏ qua [q]dừng > ")
					if !ok {
						break loop
					}
					switch strings.ToLower(a) {
					case "a", "f":
						outs, err := app.ReviewKnowledge(cmd.Context(), k, app.ReviewAction{Refs: []string{id}, To: knowledge.StatusVerified, Refresh: strings.EqualFold(a, "f")})
						if err != nil {
							return err
						}
						if printReviewOutcomes(stdio.Out, outs) > 0 {
							continue // cho người duyệt sửa hoặc bỏ qua
						}
						stats["chấp nhận"]++
					case "r":
						reason, _ := ask("Lý do từ chối (Enter = \"" + app.DefaultRejectReason + "\"): ")
						if reason == "" {
							reason = app.DefaultRejectReason
						}
						outs, err := app.ReviewKnowledge(cmd.Context(), k, app.ReviewAction{Refs: []string{id}, To: knowledge.StatusDeprecated, Reason: reason})
						if err != nil {
							return err
						}
						printReviewOutcomes(stdio.Out, outs)
						stats["từ chối"]++
					case "e":
						if err := openEditor(it.Path, stdio); err != nil {
							fmt.Fprintf(stdio.Out, "✗ %v\n", err)
						}
						continue
					case "s", "":
						stats["bỏ qua"]++
					case "q":
						break loop
					default:
						fmt.Fprintln(stdio.Out, "Không hiểu lựa chọn.")
						continue
					}
					break
				}
			}
			var parts []string
			for _, k := range []string{"chấp nhận", "từ chối", "bỏ qua"} {
				if stats[k] > 0 {
					parts = append(parts, fmt.Sprintf("%s %d", k, stats[k]))
				}
			}
			fmt.Fprintf(stdio.Out, "\nXong: %s.\n", strings.Join(parts, ", "))
			if stats["chấp nhận"] > 0 {
				fmt.Fprintln(stdio.Out, "Đưa quy tắc đã duyệt vào AGENTS.md: bint bootstrap publish")
			}
			return nil
		},
	}
	c.Flags().StringSliceVar(&statuses, "status", app.ReviewQueueDefaults().Statuses, "trạng thái cần duyệt")
	return c
}

func openEditor(path string, stdio IO) error {
	argv := app.EditorArgv(path)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, stdio.Out, stdio.Err
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mở trình soạn thảo %q: %w", strings.Join(argv[:len(argv)-1], " "), err)
	}
	return nil
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
