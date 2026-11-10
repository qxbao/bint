package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"bint/internal/app"
	"bint/internal/fsafe"
	"bint/internal/harness"
	"bint/internal/inspect"
	"bint/internal/knowledge"
	"bint/internal/skills"
)

func newInspectCmd(stdio IO) *cobra.Command {
	var (
		asJSON     bool
		maxCommits int
	)
	c := &cobra.Command{
		Use:   "inspect",
		Short: "Static discovery: ngôn ngữ, manifest, lệnh, công cụ, tài liệu, mã sinh, điểm nóng Git",
		Long: `Thu thập dữ liệu xác định được về repository (bước 1 của bootstrap, mục 4.2).
Không chạy lệnh của dự án, không gọi mô hình, không ghi gì vào repository.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := cwd()
			if err != nil {
				return err
			}
			ws, err := app.OpenWorkspace(cmd.Context(), dir, false)
			if err != nil {
				return err
			}
			rep, err := inspect.Inspect(cmd.Context(), ws.Repo.Toplevel, inspect.Options{MaxCommits: maxCommits})
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(stdio.Out, rep)
			}
			printInspect(stdio.Out, rep)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	c.Flags().IntVar(&maxCommits, "max-commits", 1000, "số commit quét để tìm điểm nóng")
	return c
}

func printInspect(w io.Writer, r *inspect.Report) {
	fmt.Fprintf(w, "Repository: %s (%d tệp được theo dõi", r.Root, r.Files)
	if r.Head != "" {
		fmt.Fprintf(w, ", HEAD %s", short(r.Head))
	}
	fmt.Fprintln(w, ")")
	if len(r.Languages) > 0 {
		var parts []string
		for i, l := range r.Languages {
			if i == 8 {
				break
			}
			parts = append(parts, fmt.Sprintf("%s %d", l.Name, l.Files))
		}
		fmt.Fprintf(w, "Ngôn ngữ:   %s\n", strings.Join(parts, ", "))
	}
	for _, m := range r.Manifests {
		extra := ""
		if m.Lockfile != "" {
			extra = " + " + m.Lockfile
		}
		deps := len(m.Dependencies) + len(m.DevDeps)
		fmt.Fprintf(w, "Manifest:   %s%s (%d dependency)\n", m.Path, extra, deps)
	}
	if len(r.Tools) > 0 {
		seen := map[string]bool{}
		var parts []string
		for _, t := range r.Tools {
			if !seen[t.Name] {
				seen[t.Name] = true
				parts = append(parts, t.Name)
			}
		}
		fmt.Fprintf(w, "Công cụ:    %s\n", strings.Join(parts, ", "))
	}
	if len(r.Commands) > 0 {
		fmt.Fprintln(w, "Lệnh (chỉ để đọc, Bint không chạy):")
		tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
		for i, c := range r.Commands {
			if i == 15 {
				fmt.Fprintf(tw, "  …\t%d lệnh nữa (--json)\t\n", len(r.Commands)-15)
				break
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\n", c.Name, truncate(c.Command, 60), c.Source)
		}
		tw.Flush()
	}
	if len(r.Docs) > 0 {
		var parts []string
		for _, d := range r.Docs {
			parts = append(parts, d.Path)
		}
		fmt.Fprintf(w, "Tài liệu:   %s\n", truncate(strings.Join(parts, ", "), 200))
	}
	for _, g := range r.Generated {
		fmt.Fprintf(w, "Mã sinh:    %s (%d tệp, %q)\n", g.Dir, g.Files, g.Marker)
	}
	g := r.Git
	fmt.Fprintf(w, "Git:        %d commit, %d người đóng góp", g.Commits, g.Contributors)
	if g.FirstCommit != "" {
		fmt.Fprintf(w, ", %s → %s", g.FirstCommit, g.LastCommit)
	}
	fmt.Fprintln(w)
	if len(g.FixHotspots) > 0 {
		var parts []string
		for i, f := range g.FixHotspots {
			if i == 6 {
				break
			}
			parts = append(parts, fmt.Sprintf("%s (%d)", f.Path, f.Commits))
		}
		fmt.Fprintf(w, "Hay sửa lỗi: %s\n", strings.Join(parts, ", "))
	}
	if len(g.Reverts) > 0 {
		fmt.Fprintf(w, "Hoàn tác:   %d commit gần đây (--json để xem)\n", len(g.Reverts))
	}
	for _, n := range r.Notes {
		fmt.Fprintf(w, "! %s\n", n)
	}
}

func newBootstrapCmd(stdio IO) *cobra.Command {
	var (
		asJSON  bool
		noSkill bool
		force   bool
	)
	c := &cobra.Command{
		Use:   "bootstrap",
		Short: "Chuẩn bị bootstrap: static discovery, cài skill b-bootstrap, hướng dẫn bước tiếp",
		Long: `Bootstrap sinh một số ít tri thức có căn cứ, tập trung vào những điều bất thường
(mục 4.2). Bint Core làm phần xác định được; phần điều tra do agent làm qua skill
b-bootstrap trong harness:

  1. bint bootstrap            static discovery + cài skill (lệnh này)
  2. /b-bootstrap              trong Claude Code: agent điều tra và ghi mục candidate
  3. bint knowledge review     người duyệt chấp nhận / từ chối từng mục
  4. bint bootstrap publish    đưa quy tắc đã duyệt vào AGENTS.md (xem trước thay đổi)

--json chỉ in dữ liệu khảo sát (dành cho agent), không ghi gì.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			dir, err := cwd()
			if err != nil {
				return err
			}
			k, err := app.OpenKnowledge(ctx, dir)
			if err != nil {
				return err
			}
			rep, err := inspect.Inspect(ctx, k.Repo.Toplevel, inspect.Options{})
			if err != nil {
				return err
			}
			items, bad, err := k.Items()
			if err != nil {
				return err
			}
			if asJSON {
				type itemRow struct {
					ID     string   `json:"id"`
					Type   string   `json:"type"`
					Status string   `json:"status"`
					Title  string   `json:"title"`
					Paths  []string `json:"paths,omitempty"`
				}
				out := struct {
					*inspect.Report
					Knowledge []itemRow `json:"existing_knowledge"`
				}{Report: rep, Knowledge: []itemRow{}}
				for _, it := range items {
					if it.Active() {
						out.Knowledge = append(out.Knowledge, itemRow{it.ID, it.Type, it.Status, it.Title, it.Scope.Paths})
					}
				}
				return writeJSON(stdio.Out, out)
			}
			printInspect(stdio.Out, rep)
			if k.Paths.Initialized() {
				p := filepath.Join(k.Paths.Generated, "inspect.json")
				if data, err := json.MarshalIndent(rep, "", "  "); err == nil {
					if err := fsafe.WriteAtomic(p, append(data, '\n'), 0o600); err == nil {
						fmt.Fprintf(stdio.Out, "\nĐã lưu dữ liệu khảo sát: %s\n", p)
					}
				}
			}
			fmt.Fprintln(stdio.Out)
			if !noSkill {
				if !k.Paths.Initialized() {
					fmt.Fprintln(stdio.Out, "! Chưa `bint init` trên máy này: bỏ qua cài skill.")
				} else if _, err := installSkillsCLI(stdio.Out, k, force); err != nil {
					return err
				}
			}
			counts := map[string]int{}
			for _, it := range items {
				counts[it.Status]++
			}
			fmt.Fprintf(stdio.Out, "Tri thức hiện có: %d verified, %d candidate, %d stale", counts[knowledge.StatusVerified], counts[knowledge.StatusCandidate], counts[knowledge.StatusStale])
			if len(bad) > 0 {
				fmt.Fprintf(stdio.Out, ", %d tệp hỏng", len(bad))
			}
			fmt.Fprintln(stdio.Out)
			fmt.Fprintln(stdio.Out, `
Bước tiếp theo:
  1. Trong Claude Code: /b-bootstrap   (agent điều tra và ghi mục chờ duyệt)
     Harness khác: đưa nội dung skill cho agent — bint bootstrap skill
  2. bint knowledge review              duyệt từng mục
  3. bint bootstrap publish             đưa quy tắc đã duyệt vào AGENTS.md`)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "chỉ in dữ liệu khảo sát và tri thức hiện có dạng JSON (không ghi gì)")
	c.Flags().BoolVar(&noSkill, "no-skill", false, "không cài/cập nhật skill b-bootstrap")
	c.Flags().BoolVar(&force, "force", false, "ghi đè skill gốc đã bị sửa tay (bản cũ được sao lưu)")
	c.AddCommand(newBootstrapPublishCmd(stdio), newBootstrapSkillCmd(stdio))
	return c
}

// installSkillsCLI xuất bản skill gốc (đã ghép lớp phủ); trả về số xung đột.
func installSkillsCLI(w io.Writer, k *app.KnowledgeWS, force bool) (int, error) {
	targets := harness.SkillTargets(k.Config.Harnesses.Enabled)
	if len(targets) == 0 {
		fmt.Fprintln(w, "! Không harness nào có adapter skill; dùng `bint skill show <tên>` để lấy nội dung.")
		return 0, nil
	}
	m, err := fsafe.LoadManifest(k.Paths.Manifest)
	if err != nil {
		return 0, err
	}
	outs, err := skills.Install(skills.InstallOptions{Root: k.Root, Targets: targets, BintVersion: Version, Force: force,
		BackupDir: k.Paths.Backups, Manifest: m, Now: time.Now(), Files: k.Source().Files()})
	if err != nil {
		return 0, err
	}
	if err := m.Save(); err != nil {
		return 0, err
	}
	conflicts := 0
	for _, o := range outs {
		switch o.Result {
		case fsafe.Conflict:
			conflicts++
			fmt.Fprintf(w, "✗ skill %s: %s — %s\n", o.Skill, o.Path, o.Detail)
		case fsafe.Created:
			fmt.Fprintf(w, "+ skill %s: %s\n", o.Skill, o.Path)
		case fsafe.Updated:
			fmt.Fprintf(w, "~ skill %s: %s (cập nhật)\n", o.Skill, o.Path)
		case fsafe.Removed:
			fmt.Fprintf(w, "- skill %s: %s (%s)\n", o.Skill, o.Path, o.Detail)
		default:
			fmt.Fprintf(w, "✓ skill %s: %s\n", o.Skill, o.Path)
		}
	}
	return conflicts, nil
}

func newBootstrapSkillCmd(stdio IO) *cobra.Command {
	return &cobra.Command{
		Use:   "skill [tên]",
		Short: "In nội dung skill (mặc định b-bootstrap, đã ghép lớp phủ) để dùng với harness chưa có adapter",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := "b-bootstrap"
			if len(args) == 1 {
				name = args[0]
			}
			s, ok := skills.Get(name)
			if !ok {
				return fmt.Errorf("không có skill gốc %q", name)
			}
			if k, err := app.OpenKnowledge(cmd.Context(), "."); err == nil {
				if c, _, err := skills.Composed(k.Root, s); err == nil {
					s = c
				}
			}
			_, err := stdio.Out.Write(s.Content)
			return err
		},
	}
}

func newBootstrapPublishCmd(stdio IO) *cobra.Command {
	var dryRun, yes, force bool
	c := &cobra.Command{
		Use:   "publish",
		Short: "Đưa quy tắc và điểm dễ sai đã duyệt vào khối Bint của AGENTS.md (xem trước thay đổi)",
		Long: `Dựng lại khối <!-- bint:begin --> … <!-- bint:end --> trong AGENTS.md từ các mục
rule và warning ở trạng thái verified (không gồm mục stale, mục gắn cờ bên thứ ba
chưa duyệt; tối đa knowledge.agents_md_max_items mục), và thêm @AGENTS.md vào
CLAUDE.md khi harness claude được bật. Nội dung ngoài khối không bị chạm tới;
khối bị sửa tay thì báo xung đột (--force để ghi đè, có sao lưu).

Luôn in bản thay đổi trước; hỏi xác nhận khi chạy trong terminal (--yes để bỏ qua).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := cwd()
			if err != nil {
				return err
			}
			opts := app.PublishOptions{Dir: dir, DryRun: dryRun, Force: force}
			shown := false
			if !yes && !dryRun {
				if !isTerminal(stdio.In) {
					opts.DryRun = true
				} else {
					opts.Confirm = func(diff string) bool {
						fmt.Fprintln(stdio.Out, diff)
						shown = true
						return confirm(stdio, "Ghi thay đổi này vào AGENTS.md?")
					}
				}
			}
			rep, err := app.PublishAgents(cmd.Context(), opts)
			if err != nil {
				return err
			}
			if !shown && rep.Diff != "" {
				fmt.Fprintln(stdio.Out, rep.Diff)
			}
			sel := rep.Selection
			fmt.Fprintf(stdio.Out, "Khối Bint: %d quy tắc, %d điểm dễ sai\n", len(sel.Rules), len(sel.Warnings))
			for _, o := range sel.Omitted {
				fmt.Fprintf(stdio.Out, "  - bỏ %s: %s\n", o.ID, o.Reason)
			}
			for _, s := range rep.Steps {
				fmt.Fprintf(stdio.Out, "  %s %s %s\n", stepIcon[s.Status], s.Name, s.Detail)
			}
			switch {
			case opts.DryRun && rep.Changed && !dryRun:
				fmt.Fprintln(stdio.Out, "Không có terminal để xác nhận: chưa ghi. Chạy lại với --yes để ghi.")
			case opts.DryRun:
				fmt.Fprintln(stdio.Out, "(--dry-run: chưa ghi gì)")
			case !rep.Written && rep.Changed:
				fmt.Fprintln(stdio.Out, "Chưa ghi.")
			case !rep.Changed && !rep.Written:
				fmt.Fprintln(stdio.Out, "AGENTS.md đã cập nhật, không có thay đổi.")
			}
			for _, s := range rep.Steps {
				if s.Status == app.StepConflict {
					return withCode(ExitError, fmt.Errorf("có xung đột, chưa ghi đè nội dung đã bị sửa tay"))
				}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&dryRun, "dry-run", false, "chỉ in bản thay đổi")
	c.Flags().BoolVarP(&yes, "yes", "y", false, "ghi mà không hỏi")
	c.Flags().BoolVar(&force, "force", false, "ghi đè khối bint đã bị sửa tay (bản cũ được sao lưu)")
	return c
}
