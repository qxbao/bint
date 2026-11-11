package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"bint/internal/evolve"
)

// newExperienceCmd là nhóm lệnh bài học từ thất bại (kế hoạch E): bản tóm tắt thất bại xác
// định, và thư viện bài học. Không gọi mô hình: rút bài học là việc của `bint evolve propose`.
func newExperienceCmd(stdio IO) *cobra.Command {
	c := &cobra.Command{
		Use:   "experience",
		Short: "Bài học từ thất bại: bản tóm tắt thất bại (digest) và thư viện bài học (show)",
		Long: `Bài học từ thất bại (kế hoạch E). Nguồn: lượt thử trượt của đợt đánh giá trên tập
VALIDATION, và lượt thật có nhãn thất bại. Tập test không bao giờ là đầu vào của agent sinh
bài học.

  bint experience digest <đợt|lượt>   tóm tắt xác định (~1.500 token), gom nhóm theo vùng mã và dấu hiệu
  bint experience show                thư viện bài học: chung trước, riêng nhóm theo vùng, số lần chèn và đạt/trượt

Rút bài học bằng agent reflector: bint evolve propose (cơ hội failure_lesson, khi nhóm có ≥ min_similar_runs tác vụ).`,
		Args: cobra.NoArgs,
	}
	c.AddCommand(newExperienceDigestCmd(stdio), newExperienceShowCmd(stdio))
	return c
}

func newExperienceDigestCmd(stdio IO) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "digest <đợt|lượt>",
		Short: "Dựng bản tóm tắt thất bại cho một đợt đánh giá (ID, tiền tố, latest) hoặc một lượt chạy",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := openEvolve(cmd)
			if err != nil {
				return err
			}
			defer e.Close()
			cases, err := e.cases.Cases()
			if err != nil {
				return err
			}
			tr := e.k.Config.Evolution.Trigger
			minUnits, minNew := max(tr.MinSimilarRuns, 1), max(tr.MinNewEvidence, 1)
			rep, err := evolve.DigestRef(cmd.Context(), e.st.DB, cases, args[0], e.k.Paths.Runtime, minUnits, minNew)
			if err != nil {
				return err
			}
			if asJSON {
				if rep.Digests == nil {
					rep.Digests = []evolve.Digest{}
				}
				if rep.Groups == nil {
					rep.Groups = []evolve.GroupSummary{}
				}
				return writeJSON(stdio.Out, rep)
			}
			printDigestReport(stdio.Out, rep, minUnits)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func printDigestReport(w io.Writer, rep evolve.DigestReport, minUnits int) {
	if rep.Kind == "batch" {
		fmt.Fprintf(w, "Đợt %s: %d lượt thất bại trên tập validation", rep.Ref, len(rep.Digests))
		if rep.Skipped > 0 {
			fmt.Fprintf(w, "; bỏ qua %d lượt không thuộc tập validation (tập test không bao giờ là đầu vào của agent sinh bài học)", rep.Skipped)
		}
		fmt.Fprintln(w)
	} else {
		fmt.Fprintf(w, "Lượt %s (lượt thật)\n", rep.Ref)
	}
	if len(rep.Digests) == 0 {
		fmt.Fprintln(w, "\nKhông có lượt thất bại nào để tóm tắt.")
		return
	}
	for _, d := range rep.Digests {
		fmt.Fprintf(w, "\n%s\n", d.Text)
		fmt.Fprintf(w, "(~%d token)\n", d.Tokens)
	}
	if len(rep.Groups) > 0 {
		fmt.Fprintf(w, "\nNhóm thất bại (cần ≥ %d tác vụ phân biệt để rút bài học):\n", minUnits)
		for _, g := range rep.Groups {
			state := "sẵn sàng: bint evolve propose rút bài học"
			if !g.Ready {
				state = "chưa: " + g.Blocked
			}
			fmt.Fprintf(w, "  - %s — %s [%s]\n", g.Group, g.Summary, state)
		}
	}
}

func newExperienceShowCmd(stdio IO) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "show",
		Short: "Thư viện bài học từ thất bại (chung trước, riêng nhóm theo vùng), kèm số lần chèn và đạt/trượt",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := openEvolve(cmd)
			if err != nil {
				return err
			}
			defer e.Close()
			items, _, err := e.k.Items()
			if err != nil {
				return err
			}
			rows, err := evolve.ExperienceShow(cmd.Context(), e.st.DB, items)
			if err != nil {
				return err
			}
			if asJSON {
				if rows == nil {
					rows = []evolve.LessonRow{}
				}
				return writeJSON(stdio.Out, rows)
			}
			printExperience(stdio.Out, rows)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func printExperience(w io.Writer, rows []evolve.LessonRow) {
	if len(rows) == 0 {
		fmt.Fprintln(w, "Chưa có bài học từ thất bại (`bint evolve propose` tạo đề xuất; người duyệt mới đưa vào thư viện).")
		return
	}
	fmt.Fprintf(w, "Thư viện bài học (%d):\n", len(rows))
	area := "\x00" // chưa in tiêu đề nào
	for _, r := range rows {
		if r.Area != area {
			area = r.Area
			if area == "" {
				fmt.Fprintln(w, "\nChung (mọi vùng mã):")
			} else {
				fmt.Fprintf(w, "\nRiêng — %s:\n", area)
			}
		}
		fmt.Fprintf(w, "  %s  [%s] %s\n", r.ID, r.Status, r.Title)
		tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
		fmt.Fprintf(tw, "      \tchèn %d lần (lượt thật)\teval khi có bài học: đạt %d, trượt %d\tlượt bằng chứng %d\n",
			r.Inserted, r.EvalPass, r.EvalFail, r.Runs)
		tw.Flush()
	}
	fmt.Fprintln(w, strings.TrimSpace(`
Mục candidate chỉ được chèn như tham khảo (nhãn "tham khảo") cho tới khi người duyệt.`))
}
