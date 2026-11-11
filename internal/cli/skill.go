package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"bint/internal/app"
	"bint/internal/config"
)

func newSkillCmd(stdio IO) *cobra.Command {
	c := &cobra.Command{
		Use:   "skill",
		Short: "Skill gốc và lớp phủ theo dự án",
		Long: `Skill gốc (b-bootstrap, b-knowledge, b-req, b-feat, b-review, b-do) đi kèm tệp thực thi Bint và không
được sửa trực tiếp trong repository (mục 6.6). Tuỳ biến theo dự án đặt trong lớp phủ
.bint/skills/overlays/<skill>.md, chỉ tại các điểm neo skill gốc khai báo. ` + "`bint sync`" + `
ghép skill gốc với lớp phủ và xuất bản cho harness; core.lock ghi phiên bản đang dùng.

Khi nâng cấp Bint, lớp phủ được áp lại; điểm neo không còn → xung đột, bản cũ được giữ.

Skill chuyên biệt do tiến hoá tạo (L2, ` + "`bint evolve`" + `) nằm trong .bint/skills/evolved/<tên>/ và được
xuất bản cùng skill gốc. Mỗi phiên bản gắn với mô hình đã kiểm chứng nó:
  bint skill diff <tên>      bản thay đổi giữa hai phiên bản
  bint skill versions <tên>  lịch sử phiên bản, mô hình, người duyệt
  bint skill check <tên|đề xuất>  kiểm tra điều kiện kích hoạt
  bint skill regress         skill nào chưa xác thực với mô hình hiện dùng`,
	}
	c.AddCommand(newSkillListCmd(stdio), newSkillShowCmd(stdio), newSkillOverlayCmd(stdio), newSkillDiffCmd(stdio),
		newSkillVersionsCmd(stdio), newSkillCheckCmd(stdio), newSkillRegressCmd(stdio))
	return c
}

func newSkillListCmd(stdio IO) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "list",
		Short: "Liệt kê skill gốc: điểm neo, lớp phủ, trạng thái xuất bản",
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

func newSyncCmd(stdio IO) *cobra.Command {
	var force bool
	c := &cobra.Command{
		Use:   "sync",
		Short: "Ghép skill gốc với lớp phủ và xuất bản cho harness; cập nhật core.lock",
		Long: `Ghép skill gốc với lớp phủ (.bint/skills/overlays/) và xuất bản cho các harness có
adapter: Claude Code → .claude/skills/, Codex và Antigravity → .agents/skills/ (frontmatter
chỉ giữ name, description). Chạy sau khi sửa lớp phủ hoặc nâng cấp Bint.

  - Skill đã xuất bản bị sửa tay → xung đột, không ghi đè (--force để ghi đè, có sao lưu);
    tuỳ biến thuộc về lớp phủ.
  - Lớp phủ dùng điểm neo không còn trong skill gốc mới → xung đột, giữ bản cũ và mục
    core.lock cũ cho tới khi lớp phủ được sửa (--force không vượt qua được).`,
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
