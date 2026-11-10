package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"bint/internal/fsafe"
	"bint/internal/skills"
)

func newSkillCmd(stdio IO) *cobra.Command {
	c := &cobra.Command{
		Use:   "skill",
		Short: "Skill gốc và lớp phủ theo dự án",
		Long: `Skill gốc (b-bootstrap, b-req, b-feat, b-review, b-do) đi kèm tệp thực thi Bint và không
được sửa trực tiếp trong repository (mục 6.6). Tuỳ biến theo dự án đặt trong lớp phủ
.bint/skills/overlays/<skill>.md, chỉ tại các điểm neo skill gốc khai báo. ` + "`bint sync`" + `
ghép skill gốc với lớp phủ và xuất bản cho harness; core.lock ghi phiên bản đang dùng.

Khi nâng cấp Bint, lớp phủ được áp lại; điểm neo không còn → xung đột, bản cũ được giữ.`,
	}
	c.AddCommand(newSkillListCmd(stdio), newSkillShowCmd(stdio), newSkillOverlayCmd(stdio))
	return c
}

type skillRow struct {
	Name      string   `json:"name"`
	Version   string   `json:"version"`
	Anchors   []string `json:"anchors"`
	Overlay   string   `json:"overlay"`
	Published string   `json:"published"`
	Locked    string   `json:"locked,omitempty"`
	Problem   string   `json:"problem,omitempty"`
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
			lock, _ := skills.ReadLock(k.Root)
			claude := slices.Contains(k.Config.Harnesses.Enabled, "claude")
			rows := []skillRow{}
			for _, core := range skills.Core() {
				row := skillRow{Name: core.Name, Version: core.Hash(), Overlay: "-", Published: "-"}
				if as, err := core.Anchors(); err == nil {
					for _, a := range as {
						row.Anchors = append(row.Anchors, a.Name)
					}
				}
				if e, ok := lock.Skills[core.Name]; ok {
					row.Locked = e.SHA256
				}
				composed, ov, err := skills.Composed(k.Root, core)
				if ov != nil {
					row.Overlay = fmt.Sprintf("%d khối", len(ov.Patches))
				}
				if err != nil {
					row.Overlay, row.Problem = "xung đột", err.Error()
				} else if claude {
					row.Published, _ = skills.Status(k.Root, skills.ClaudeTarget, composed)
				}
				rows = append(rows, row)
			}
			if asJSON {
				return writeJSON(stdio.Out, rows)
			}
			tw := tabwriter.NewWriter(stdio.Out, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "SKILL\tPHIÊN BẢN\tCORE.LOCK\tLỚP PHỦ\tCLAUDE\tĐIỂM NEO")
			for _, r := range rows {
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
			for _, r := range rows {
				if r.Problem != "" {
					fmt.Fprintf(stdio.Out, "✗ %s: %s\n", r.Name, r.Problem)
				}
			}
			for _, o := range skills.OrphanOverlays(k.Root) {
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
			s, ok := skills.Get(args[0])
			if !ok {
				return fmt.Errorf("không có skill gốc %q", args[0])
			}
			if !core {
				if k, err := openKnowledge(cmd); err == nil {
					c, _, err := skills.Composed(k.Root, s)
					if err != nil {
						return err
					}
					s = c
				}
			}
			_, err := stdio.Out.Write(s.Content)
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
			s, ok := skills.Get(args[0])
			if !ok {
				return fmt.Errorf("không có skill gốc %q", args[0])
			}
			k, err := openKnowledge(cmd)
			if err != nil {
				return err
			}
			rel := skills.OverlayPath(s.Name)
			p := filepath.Join(k.Root, filepath.FromSlash(rel))
			if _, err := os.Stat(p); err == nil {
				fmt.Fprintf(stdio.Out, "Đã có %s (không ghi đè). Sửa rồi chạy `bint sync`.\n", rel)
				return nil
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			body, err := skills.OverlayTemplate(s)
			if err != nil {
				return err
			}
			if err := fsafe.WriteAtomic(p, []byte(body), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(stdio.Out, "Đã tạo %s. Bỏ thụt lề khối mẫu để bật, rồi chạy `bint sync`.\n", rel)
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
adapter (P4: Claude Code → .claude/skills/). Chạy sau khi sửa lớp phủ hoặc nâng cấp Bint.

  - Skill đã xuất bản bị sửa tay → xung đột, không ghi đè (--force để ghi đè, có sao lưu);
    tuỳ biến thuộc về lớp phủ.
  - Lớp phủ dùng điểm neo không còn trong skill gốc mới → xung đột, giữ bản cũ và mục
    core.lock cũ cho tới khi lớp phủ được sửa (--force không vượt qua được).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			k, err := openKnowledge(cmd)
			if err != nil {
				return err
			}
			if !k.Paths.Initialized() {
				return errors.New("chưa `bint init` trên máy này")
			}
			n, err := installSkillsCLI(stdio.Out, k, force)
			if err != nil {
				return err
			}
			if n > 0 {
				return withCode(ExitError, fmt.Errorf("%d xung đột cần xử lý", n))
			}
			return nil
		},
	}
	c.Flags().BoolVar(&force, "force", false, "ghi đè skill đã bị sửa tay (bản cũ được sao lưu)")
	return c
}
