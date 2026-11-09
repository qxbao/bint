package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"slices"
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
		newKnowledgeRmCmd(stdio), newKnowledgeTransitionsCmd(stdio))
	for _, p := range []struct{ use, short, phase string }{
		{"search", "Tìm kiếm tri thức (FTS5)", "P3"},
		{"diff", "Tri thức mới hoặc bị sửa từ người khác", "P3"},
		{"stats", "Số lần truy xuất, trích dẫn và tác giả của từng mục", "P3"},
	} {
		p := p
		c.AddCommand(&cobra.Command{
			Use: p.use, Short: p.short + " [" + p.phase + "]", DisableFlagParsing: true,
			RunE: func(*cobra.Command, []string) error {
				return withCode(ExitNotImplemented, fmt.Errorf("`bint knowledge %s` chưa được triển khai trong bản này (dự kiến %s)", p.use, p.phase))
			},
		})
	}
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
lặp, cờ bên thứ ba) thì không được ghi.`,
		Example: `  bint knowledge add --type warning --paths 'internal/db/**' \
    --evidence internal/db/db.go:40-52 "Mở SQLite bằng store.Open; tự mở sql.Open sẽ thiếu busy_timeout"
  bint knowledge add --type rule --title "Không sửa tay *.pb.go" --evidence Makefile:12 - < note.md`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			k, err := openKnowledge(cmd)
			if err != nil {
				return err
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
			if strings.TrimSpace(o.Body) == "" {
				return usageError{fmt.Errorf("thiếu nội dung (đối số, '-' cho stdin, hoặc --body-file)")}
			}
			if !slices.Contains(knowledge.Types, o.Type) {
				return usageError{fmt.Errorf("--type phải là một trong: %s", strings.Join(knowledge.Types, ", "))}
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
				printFindings(stdio.Out, res.Check.Findings, "  ")
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
	f.StringVarP(&o.Type, "type", "t", "", "loại: rule | warning | pattern | fact | decision | lesson (bắt buộc)")
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
	c.MarkFlagRequired("type")
	f.MarkHidden("eval-case")
	return c
}

func nonNilFindings(f []knowledge.Finding) []knowledge.Finding {
	if f == nil {
		return []knowledge.Finding{}
	}
	return f
}

func printFindings(w io.Writer, fs []knowledge.Finding, indent string) {
	for _, f := range fs {
		fmt.Fprintf(w, "%s%s %-12s %s\n", indent, findingIcon[f.Level], f.Code, f.Detail)
	}
}

func newKnowledgeListCmd(stdio IO) *cobra.Command {
	var (
		status, typ []string
		all, asJSON bool
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
			items, bad, err := k.Items()
			if err != nil {
				return err
			}
			var out []*knowledge.Item
			for _, it := range items {
				if len(status) > 0 && !slices.Contains(status, it.Status) {
					continue
				}
				if len(status) == 0 && !all && !it.Active() {
					continue
				}
				if len(typ) > 0 && !slices.Contains(typ, it.Type) {
					continue
				}
				out = append(out, it)
			}
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
	f.StringSliceVar(&status, "status", nil, "lọc theo trạng thái")
	f.StringSliceVar(&typ, "type", nil, "lọc theo loại")
	f.BoolVar(&all, "all", false, "gồm cả mục deprecated")
	f.BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

// printItem trình bày đầy đủ một mục kèm trích đoạn bằng chứng.
func printItem(w io.Writer, k *app.KnowledgeWS, it *knowledge.Item) {
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
	if len(it.Evidence) > 0 {
		fmt.Fprintln(w, "\nBằng chứng:")
		src := k.Source()
		for _, e := range it.Evidence {
			extra := ""
			if e.Commit != "" {
				extra = " (commit " + short(e.Commit) + ")"
			}
			fmt.Fprintf(w, "  %s%s\n", e, extra)
			b, ok := src.Read(e.Path)
			if !ok {
				fmt.Fprintln(w, "    (không còn tệp)")
				continue
			}
			for _, l := range excerpt(e, b, 12) {
				fmt.Fprintf(w, "    │ %s\n", l)
			}
		}
	}
}

func excerpt(e knowledge.Evidence, b []byte, max int) []string {
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	from, to, ok, err := e.LineRange()
	if !ok || err != nil {
		from, to = 1, len(lines)
	}
	if to > len(lines) {
		to = len(lines)
	}
	var out []string
	for i := from; i <= to && i-from < max; i++ {
		out = append(out, fmt.Sprintf("%4d  %s", i, lines[i-1]))
	}
	if to-from+1 > max {
		out = append(out, fmt.Sprintf("      … (%d dòng nữa)", to-from+1-max))
	}
	return out
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
			items, _, err := k.Items()
			if err != nil {
				return err
			}
			it, err := knowledge.Find(items, args[0])
			if err != nil {
				return err
			}
			r := knowledge.Verifier{Src: k.Source(), All: items}.Verify(it)
			if asJSON {
				return writeJSON(stdio.Out, map[string]any{"item": it, "body": it.Body, "path": it.Path, "findings": nonNilFindings(r.Findings)})
			}
			printItem(stdio.Out, k, it)
			if len(r.Findings) > 0 {
				fmt.Fprintln(stdio.Out, "\nKiểm tra:")
				printFindings(stdio.Out, r.Findings, "  ")
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
  verified  → stale     bằng chứng thay đổi hoặc không còn`,
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
					"load_errors": loadErrStrings(rep.LoadErrors), "errors": errorsN, "warnings": warnN}); err != nil {
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
					printFindings(stdio.Out, r.Findings, "    ")
				}
				for _, t := range rep.Transitions {
					fmt.Fprintf(stdio.Out, "→ %s: %s → %s (%s)\n", t.ID, t.From, t.To, t.Reason)
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
	c.Flags().BoolVar(&apply, "apply", false, "áp chuyển trạng thái tự động (candidate→verified cho loại không cần người duyệt; verified→stale)")
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
			fmt.Fprintf(w, "✗ %s: %v\n", o.ID, o.Err)
			if o.Check != nil {
				printFindings(w, o.Check.Findings, "    ")
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
			outs, err := app.ReviewKnowledge(cmd.Context(), k, app.ReviewAction{Refs: args, To: knowledge.StatusVerified, Reason: note, Refresh: refresh})
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
			if strings.TrimSpace(reason) == "" {
				return usageError{fmt.Errorf("cần --reason")}
			}
			k, err := openKnowledge(cmd)
			if err != nil {
				return err
			}
			outs, err := app.ReviewKnowledge(cmd.Context(), k, app.ReviewAction{Refs: args, To: knowledge.StatusDeprecated, Reason: reason})
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
	var statuses []string
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
			if in, k := app.InAgentSession(); in {
				return fmt.Errorf("%w (`bint knowledge review`; phát hiện biến %s)", app.ErrAgentForbidden, k)
			}
			k, err := openKnowledge(cmd)
			if err != nil {
				return err
			}
			items, _, err := k.Items()
			if err != nil {
				return err
			}
			var pending []string
			for _, it := range items {
				if slices.Contains(statuses, it.Status) {
					pending = append(pending, it.ID)
				}
			}
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
					printItem(stdio.Out, k, it)
					r := knowledge.Verifier{Src: k.Source(), All: items}.Verify(it)
					if len(r.Findings) > 0 {
						fmt.Fprintln(stdio.Out, "\nKiểm tra:")
						printFindings(stdio.Out, r.Findings, "  ")
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
						reason, _ := ask("Lý do từ chối (Enter = \"người duyệt từ chối\"): ")
						if reason == "" {
							reason = "người duyệt từ chối"
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
	c.Flags().StringSliceVar(&statuses, "status", []string{knowledge.StatusCandidate, knowledge.StatusStale}, "trạng thái cần duyệt")
	return c
}

func openEditor(path string, stdio IO) error {
	ed := os.Getenv("VISUAL")
	if ed == "" {
		ed = os.Getenv("EDITOR")
	}
	if ed == "" {
		if runtime.GOOS == "windows" {
			ed = "notepad"
		} else {
			ed = "vi"
		}
	}
	f := strings.Fields(ed)
	cmd := exec.Command(f[0], append(f[1:], path)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, stdio.Out, stdio.Err
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mở trình soạn thảo %q: %w", ed, err)
	}
	return nil
}
