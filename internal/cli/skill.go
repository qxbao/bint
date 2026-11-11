package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"bint/internal/fsafe"
	"bint/internal/harness"
	"bint/internal/skills"
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

type skillRow struct {
	Name      string   `json:"name"`
	Version   string   `json:"version"`
	Anchors   []string `json:"anchors"`
	Overlay   string   `json:"overlay"`
	Published string   `json:"published"`
	// Targets: trạng thái theo từng thư mục xuất bản (current | differs | missing).
	Targets map[string]string `json:"targets,omitempty"`
	Locked  string            `json:"locked,omitempty"`
	Problem string            `json:"problem,omitempty"`
	// Kind: core | evolved. Skill tiến hoá: Status (active | deprecated), Stale (lý do lỗi thời).
	Kind   string `json:"kind"`
	Status string `json:"status,omitempty"`
	Stale  string `json:"stale,omitempty"`
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
			targets := harness.SkillTargets(k.Config.Harnesses.Enabled)
			rows := []skillRow{}
			for _, core := range skills.Core() {
				row := skillRow{Name: core.Name, Version: core.Hash(), Overlay: "-", Published: "-", Kind: "core"}
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
				} else if len(targets) > 0 {
					row.Targets = map[string]string{}
					var parts []string
					for _, t := range targets {
						st, _ := skills.Status(k.Root, t, composed)
						row.Targets[t.Dir] = st
						parts = append(parts, t.Dir+":"+st)
					}
					row.Published = strings.Join(parts, " ")
				}
				rows = append(rows, row)
			}
			evolved, evProblems, _ := skills.LoadEvolved(k.Root)
			files := k.Source().Files()
			for _, ev := range evolved {
				row := skillRow{Name: ev.Meta.Name, Version: fmt.Sprintf("v%d", ev.Meta.Version), Overlay: "-", Published: "-", Kind: "evolved",
					Status: ev.Meta.Status, Anchors: ev.Meta.Triggers.Paths}
				if stale, why := ev.Stale(files); stale {
					row.Stale = why
				}
				if ev.Active() && row.Stale == "" && len(targets) > 0 {
					row.Targets = map[string]string{}
					var parts []string
					for _, t := range targets {
						st, _ := skills.Status(k.Root, t, ev.AsSkill())
						row.Targets[t.Dir] = st
						parts = append(parts, t.Dir+":"+st)
					}
					row.Published = strings.Join(parts, " ")
				}
				rows = append(rows, row)
			}
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
				if k, err := openKnowledge(cmd); err == nil {
					if ev, err := skills.LoadEvolvedOne(k.Root, args[0]); err == nil {
						_, werr := stdio.Out.Write(ev.Content)
						return werr
					}
				}
				return fmt.Errorf("không có skill %q (gốc hoặc tiến hoá)", args[0])
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
adapter: Claude Code → .claude/skills/, Codex và Antigravity → .agents/skills/ (frontmatter
chỉ giữ name, description). Chạy sau khi sửa lớp phủ hoặc nâng cấp Bint.

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
