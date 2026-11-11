package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"bint/internal/app"
	"bint/internal/app/evolvesvc"
	"bint/internal/evolve"
	"bint/internal/skills"
)

// syncAfterSkillChange xuất bản (hoặc gỡ) skill tiến hoá cho harness sau khi áp dụng/quay lui.
func syncAfterSkillChange(w io.Writer, k *app.KnowledgeWS) {
	if !k.Paths.Initialized() {
		return
	}
	if n, err := installSkillsCLI(w, k, false); err != nil {
		fmt.Fprintf(w, "! không đồng bộ được skill: %v (chạy `bint sync`)\n", err)
	} else if n > 0 {
		fmt.Fprintf(w, "! %d xung đột khi đồng bộ skill: xem trên, rồi chạy `bint sync`\n", n)
	}
}

// printSkillChanges in thay đổi skill của đề xuất L2: nội dung mới hoặc bản thay đổi so với
// phiên bản hiện tại. Đây là chỗ người duyệt đọc chỉ dẫn sẽ nằm trong ngữ cảnh của agent.
func printSkillChanges(w io.Writer, p *evolve.Proposal, evolved []*skills.Evolved) {
	byName := map[string]*skills.Evolved{}
	for _, e := range evolved {
		byName[e.Meta.Name] = e
	}
	for _, c := range p.SkillChanges {
		fmt.Fprintf(w, "  [%s] skill %s", c.Op, c.Name)
		if c.MergedInto != "" {
			fmt.Fprintf(w, " → gộp vào %s", c.MergedInto)
		}
		if c.Reason != "" {
			fmt.Fprintf(w, " — %s", c.Reason)
		}
		fmt.Fprintln(w)
		d := c.Draft
		if d == nil {
			continue
		}
		fmt.Fprintf(w, "      mô tả:      %s\n", d.Description)
		fmt.Fprintf(w, "      kích hoạt:  paths=%s keywords=%s\n", strings.Join(d.Triggers.Paths, ","), strings.Join(d.Triggers.Keywords, ","))
		fmt.Fprintf(w, "      bằng chứng: %s\n", strings.Join(d.Evidence, ", "))
		cur := byName[c.Name]
		if cur != nil && !(p.Status == evolve.StatusApplied) {
			fmt.Fprintf(w, "    bản thay đổi so với v%d:\n%s", cur.Meta.Version, indent(skills.Diff("v"+fmt.Sprint(cur.Meta.Version), "đề xuất", string(cur.Content), string(d.Content())), "      "))
		} else {
			fmt.Fprintf(w, "    SKILL.md:\n%s\n", indent(string(d.Content()), "      "))
		}
	}
}

func newSkillDiffCmd(stdio IO) *cobra.Command {
	var from, to int
	c := &cobra.Command{
		Use:   "diff <tên>",
		Short: "Bản thay đổi của skill tiến hoá giữa hai phiên bản (mặc định: phiên bản trước → hiện tại)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := cwd()
			if err != nil {
				return err
			}
			res, err := app.DiffSkill(cmd.Context(), dir, app.SkillDiffOptions{Name: args[0], From: from, To: to})
			if err != nil {
				return err
			}
			if res.OnlyVersion {
				fmt.Fprintf(stdio.Out, "%s chỉ có phiên bản %d: không có bản thay đổi (dùng `bint skill show %s`).\n", res.Name, res.Current, res.Name)
				return nil
			}
			if res.Same {
				fmt.Fprintf(stdio.Out, "Phiên bản %d và %d giống nhau.\n", res.From, res.To)
				return nil
			}
			d := res.Diff
			fmt.Fprint(stdio.Out, d)
			return nil
		},
	}
	c.Flags().IntVar(&from, "from", 0, "phiên bản đầu (mặc định: trước phiên bản --to)")
	c.Flags().IntVar(&to, "to", 0, "phiên bản cuối (mặc định: hiện tại)")
	return c
}

func newSkillVersionsCmd(stdio IO) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "versions <tên>",
		Short: "Lịch sử phiên bản skill tiến hoá, gắn với harness/mô hình đã kiểm chứng (mục 12.6)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := cwd()
			if err != nil {
				return err
			}
			meta, err := app.SkillVersions(cmd.Context(), dir, app.SkillVersionsOptions{Name: args[0]})
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(stdio.Out, meta)
			}
			fmt.Fprintf(stdio.Out, "%s — %s, phiên bản hiện tại %d", meta.Name, meta.Status, meta.Version)
			if meta.StatusReason != "" {
				fmt.Fprintf(stdio.Out, " (%s)", meta.StatusReason)
			}
			fmt.Fprintln(stdio.Out)
			tw := tabwriter.NewWriter(stdio.Out, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "PB\tSHA\tĐỀ XUẤT\tHARNESS\tMÔ HÌNH\tKIỂM CHỨNG\tNGƯỜI DUYỆT\tLÚC\tGHI CHÚ")
			for _, v := range meta.Versions {
				fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%v\t%s\t%s\t%s\n", v.N, v.SHA256, orDash(v.Proposal), orDash(v.Harness), orDash(v.Model),
					v.Verified, orDash(v.By), fmtTime(v.At), truncate(v.Note, 50))
			}
			return tw.Flush()
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func newSkillCheckCmd(stdio IO) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "check <tên-skill | id-đề-xuất>",
		Short: "Kiểm tra điều kiện kích hoạt: mẫu còn khớp tệp, kích hoạt đúng/nhầm theo lịch sử, trùng chức năng",
		Long: `Kiểm tra xác định được (không gọi mô hình) điều kiện kích hoạt của một skill tiến hoá hoặc của
skill trong một đề xuất chưa áp dụng:

  - mỗi mẫu đường dẫn phải còn khớp tệp trong repository;
  - kích hoạt được ở ít nhất 50% lượt thành công làm căn cứ (không quá hẹp);
  - không kích hoạt ở quá 35% lượt có nhãn không liên quan (không quá rộng);
  - mô tả không trùng chức năng với skill khác (harness dễ chọn nhầm).

Đây là kiểm tra theo đường dẫn: harness quyết định kích hoạt thật theo mô tả skill, và Bint
chưa nhận được sự kiện skill.activated nên không đo được kích hoạt thực tế.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := openEvolve(cmd)
			if err != nil {
				return err
			}
			defer e.Close()
			res, err := evolvesvc.SkillCheck(cmd.Context(), e, evolvesvc.SkillCheckOptions{Target: args[0]})
			if err != nil {
				return err
			}
			reports, bad := res.Reports, !res.OK
			if asJSON {
				if err := writeJSON(stdio.Out, reports); err != nil {
					return err
				}
			} else {
				for _, r := range reports {
					printTriggerReport(stdio.Out, r)
				}
			}
			if bad {
				return withCode(ExitError, errors.New("điều kiện kích hoạt không đạt"))
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func printTriggerReport(w io.Writer, r evolve.TriggerReport) {
	icon := "✓"
	if !r.OK {
		icon = "✗"
	}
	fmt.Fprintf(w, "%s skill %s\n", icon, r.Skill)
	for _, p := range sortedMapKeys(r.Paths) {
		fmt.Fprintf(w, "    %-30s khớp %d tệp\n", p, r.Paths[p])
	}
	if r.Positives > 0 {
		fmt.Fprintf(w, "    kích hoạt đúng:  %d/%d lượt thành công làm căn cứ\n", r.PositiveHits, r.Positives)
	}
	if r.Negatives > 0 {
		fmt.Fprintf(w, "    kích hoạt nhầm:  %d/%d lượt có nhãn không liên quan\n", r.FalseHits, r.Negatives)
	}
	if r.Eligible > 0 {
		fmt.Fprintf(w, "    kích hoạt thực tế: %d/%d lượt đủ điều kiện (sự kiện skill.activated)\n", r.Activated, r.Eligible)
	}
	for _, o := range r.Overlaps {
		fmt.Fprintf(w, "    chồng chức năng: %s\n", o)
	}
	for _, p := range r.Problems {
		fmt.Fprintf(w, "    ✗ %s\n", p)
	}
	for _, n := range r.Notes {
		fmt.Fprintf(w, "    · %s\n", n)
	}
}

func newSkillRegressCmd(stdio IO) *cobra.Command {
	var (
		asJSON bool
		model  string
	)
	c := &cobra.Command{
		Use:   "regress",
		Short: "Phát hiện hồi quy khi đổi mô hình: skill nào chưa xác thực với mô hình hiện dùng (mục 12.6)",
		Long: `Mọi phiên bản skill ghi harness và mô hình đã kiểm chứng nó. Khi mô hình hiện dùng khác, skill
được đánh dấu "chưa xác thực với mô hình hiện tại" và kết quả đối chiếu cũ không được dùng làm mốc.

Để xác thực lại bằng một tập nhỏ đại diện (chính các tác vụ đã làm căn cứ áp dụng):

  bint evolve evaluate <id đề xuất> --regression --model <mô hình> --yes

Kết quả được ghi là lần kiểm tra hồi quy; lệnh này cho biết skill nào vẫn có lợi (revalid),
không còn lợi (regressed) hay chưa kết luận. Bint chỉ báo cáo và gợi ý; không tự gỡ skill.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := openEvolve(cmd)
			if err != nil {
				return err
			}
			defer e.Close()
			res, err := evolvesvc.SkillRegress(cmd.Context(), e, evolvesvc.SkillRegressOptions{Model: model})
			if err != nil {
				return err
			}
			cur, sts := res.Model, res.Statuses
			if asJSON {
				return writeJSON(stdio.Out, sts)
			}
			if len(sts) == 0 {
				fmt.Fprintln(stdio.Out, "Không có skill tiến hoá nào đang hoạt động.")
				return nil
			}
			fmt.Fprintf(stdio.Out, "Mô hình hiện dùng: %s\n", orDash(cur))
			tw := tabwriter.NewWriter(stdio.Out, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "SKILL\tPB\tTRẠNG THÁI\tĐÃ KIỂM CHỨNG VỚI\tCHI TIẾT")
			for _, s := range sts {
				fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\n", s.Skill, s.Version, s.Status, orDash(s.Verified), s.Detail)
			}
			return tw.Flush()
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	c.Flags().StringVar(&model, "model", "", "mô hình hiện dùng (mặc định: mô hình của lượt chạy gần nhất)")
	return c
}
