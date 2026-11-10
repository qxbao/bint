package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"bint/internal/app"
	"bint/internal/harness/claude"
	"bint/internal/runlog"
	"bint/internal/state"
)

var stepIcon = map[app.StepStatus]string{
	app.StepCreated: "+", app.StepUpdated: "~", app.StepOK: "✓",
	app.StepSkipped: "-", app.StepWarn: "!", app.StepConflict: "✗",
}

var levelIcon = map[app.Level]string{
	app.LevelOK: "✓", app.LevelInfo: "i", app.LevelWarn: "!", app.LevelFail: "✗",
}

func newInitCmd(stdio IO) *cobra.Command {
	var (
		harnesses []string
		hooks     bool
		noHooks   bool
		scope     string
		agentsMD  bool
		force     bool
		noSkills  bool
		mcp       bool
		noMCP     bool
	)
	c := &cobra.Command{
		Use:   "init",
		Short: "Khởi tạo Bint cho repository (chạy lại an toàn)",
		Long: `Khởi tạo Bint: tạo .bint/config.yml (không bao giờ ghi đè), thư mục trạng thái
cục bộ ngoài repository (dùng chung cho mọi worktree) cùng SQLite, cài hook ghi
nhận lượt chạy nếu được đồng ý, và thêm khối bint vào .gitignore.

Không gọi mô hình, không cài phần mềm, không chạy lệnh của dự án.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if hooks && noHooks {
				return usageError{fmt.Errorf("--hooks và --no-hooks loại trừ nhau")}
			}
			sc, err := claude.ParseScope(scope)
			if err != nil {
				return usageError{err}
			}
			dir, err := cwd()
			if err != nil {
				return err
			}
			if mcp && noMCP {
				return usageError{fmt.Errorf("--mcp và --no-mcp loại trừ nhau")}
			}
			opts := app.InitOptions{
				Dir: dir, Harnesses: harnesses, HooksScope: sc, AgentsMD: agentsMD, Force: force,
				BintPath: bintPath(), Version: Version, NoSkills: noSkills,
			}
			if mcp {
				opts.MCP = ptr(true)
			} else if noMCP {
				opts.MCP = ptr(false)
			}
			switch {
			case hooks:
				opts.Hooks = ptr(true)
			case noHooks:
				opts.Hooks = ptr(false)
			default:
				if isTerminal(stdio.In) {
					opts.Confirm = func(q string) bool { return confirm(stdio, q) }
				}
			}
			rep, err := app.Init(cmd.Context(), opts)
			if err != nil {
				return err
			}
			w := stdio.Out
			if rep.FirstInit {
				fmt.Fprintf(w, "Khởi tạo Bint cho %s\n\n", rep.Repo.Toplevel)
			} else {
				fmt.Fprintf(w, "Đồng bộ Bint cho %s\n\n", rep.Repo.Toplevel)
			}
			tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
			for _, s := range rep.Steps {
				fmt.Fprintf(tw, "  %s\t%s\t%s\n", stepIcon[s.Status], s.Name, s.Detail)
			}
			tw.Flush()
			if len(rep.NextSteps) > 0 {
				fmt.Fprintln(w, "\nBước tiếp theo:")
				for i, s := range rep.NextSteps {
					fmt.Fprintf(w, "  %d. %s\n", i+1, s)
				}
			}
			if rep.HasProblem {
				return withCode(ExitError, fmt.Errorf("khởi tạo xong nhưng có mục cần xử lý (đánh dấu ! hoặc ✗)"))
			}
			return nil
		},
	}
	f := c.Flags()
	f.StringSliceVar(&harnesses, "harness", nil, "harness kích hoạt khi tạo cấu hình mới (claude, codex, antigravity)")
	f.BoolVar(&hooks, "hooks", false, "cài hook ghi nhận lượt chạy mà không hỏi")
	f.BoolVar(&noHooks, "no-hooks", false, "không cài (hoặc gỡ) hook ghi nhận")
	f.StringVar(&scope, "hooks-scope", string(claude.ScopeLocal), "nơi cài hook: local | project | user (Claude: .claude/settings[.local].json; Codex: .codex/hooks.json; Antigravity: .agents/hooks.json; local = thêm vào .gitignore)")
	f.BoolVar(&agentsMD, "agents-md", false, "ghi khối bint (quy tắc đã duyệt) vào AGENTS.md và nhập lại AGENTS.md trong CLAUDE.md")
	f.BoolVar(&force, "force", false, "ghi đè khối bint hoặc skill gốc đã bị sửa tay (bản cũ được sao lưu)")
	f.BoolVar(&noSkills, "no-skills", false, "không xuất bản skill gốc cho harness (.claude/skills, .agents/skills)")
	f.BoolVar(&mcp, "mcp", false, "đăng ký MCP server của Bint (Context Engine) cho harness hỗ trợ: .mcp.json (Claude Code), .codex/config.toml (Codex)")
	f.BoolVar(&noMCP, "no-mcp", false, "gỡ MCP server của Bint khỏi cấu hình harness")
	return c
}

func newDoctorCmd(stdio IO) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Kiểm tra môi trường, cấu hình, cơ sở dữ liệu và hook",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := cwd()
			if err != nil {
				return err
			}
			rep := app.Doctor(cmd.Context(), dir)
			if asJSON {
				enc := json.NewEncoder(stdio.Out)
				enc.SetIndent("", "  ")
				if err := enc.Encode(rep.Checks); err != nil {
					return err
				}
			} else {
				tw := tabwriter.NewWriter(stdio.Out, 0, 2, 2, ' ', 0)
				for _, ch := range rep.Checks {
					fmt.Fprintf(tw, "  %s\t%s\t%s\n", levelIcon[ch.Level], ch.Name, ch.Detail)
				}
				tw.Flush()
			}
			if rep.Failed() {
				return withCode(ExitError, fmt.Errorf("có kiểm tra thất bại"))
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func newStatusCmd(stdio IO) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Trạng thái Bint của repository hiện tại",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			ws, s, res, err := openAndIngest(cmd)
			if err != nil {
				return err
			}
			defer s.Close()
			counts, err := runlog.StatusCounts(ctx, s.DB)
			if err != nil {
				return err
			}
			v, _ := s.Version(ctx)
			w := stdio.Out
			fmt.Fprintf(w, "Repository:     %s\n", ws.Repo.Toplevel)
			fmt.Fprintf(w, "Repo ID:        %s\n", ws.Paths.RepoID)
			fmt.Fprintf(w, "Trạng thái:     %s\n", ws.Paths.Root)
			fmt.Fprintf(w, "Schema:         v%d\n", v)
			fmt.Fprintf(w, "Harness:        %s\n", strings.Join(ws.Config.Harnesses.Enabled, ", "))
			total := 0
			var parts []string
			keys := make([]string, 0, len(counts))
			for k := range counts {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				total += counts[k]
				parts = append(parts, fmt.Sprintf("%s=%d", k, counts[k]))
			}
			fmt.Fprintf(w, "Lượt chạy:      %d %s\n", total, strings.Join(parts, " "))
			var pending int
			if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM evolution_proposals WHERE status = 'proposed'`).Scan(&pending); err == nil && pending > 0 {
				fmt.Fprintf(w, "Tiến hoá:       %d đề xuất chờ duyệt (bint evolve list)\n", pending)
			}
			if res.Events > 0 || res.Interrupted > 0 {
				fmt.Fprintf(w, "Vừa nạp:        %d sự kiện, %d lượt chuyển interrupted\n", res.Events, res.Interrupted)
			}
			return nil
		},
	}
}

func newEventsCmd(stdio IO) *cobra.Command {
	c := &cobra.Command{Use: "events", Short: "Hàng đợi sự kiện từ hook"}
	c.AddCommand(&cobra.Command{
		Use:   "ingest",
		Short: "Nạp sự kiện từ spool vào cơ sở dữ liệu (loại trùng theo event_id)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, s, res, err := openAndIngest(cmd)
			if err != nil {
				return err
			}
			defer s.Close()
			fmt.Fprintf(stdio.Out, "Đã nạp %d tệp: %d sự kiện mới, %d trùng, %d tệp hỏng, %d để lần sau; %d lượt chạy chuyển interrupted\n",
				res.Files, res.Events, res.Duplicates, res.Rejected, res.Skipped, res.Interrupted)
			return nil
		},
	})
	return c
}

func newHookCmd(stdio IO) *cobra.Command {
	c := &cobra.Command{
		Use:    "hook",
		Short:  "Điểm vào cho hook của harness (nội bộ)",
		Hidden: true,
	}
	c.AddCommand(&cobra.Command{
		Use:   "claude",
		Short: "Nhận sự kiện hook Claude Code từ stdin và ghi vào spool",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			app.RunClaudeHook(cmd.Context(), stdio.In, stdio.Out, stdio.Err, Version)
			return nil // không bao giờ chặn agent
		},
	}, &cobra.Command{
		Use:   "codex",
		Short: "Nhận sự kiện hook Codex từ stdin và ghi vào spool",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			app.RunHook(cmd.Context(), "codex", "", stdio.In, stdio.Out, stdio.Err, Version)
			return nil
		},
	}, &cobra.Command{
		Use:   "antigravity <sự kiện>",
		Short: "Nhận sự kiện hook Antigravity (SessionStart | PreToolUse | PostToolUse) từ stdin",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ev := ""
			if len(args) > 0 {
				ev = args[0]
			}
			app.RunHook(cmd.Context(), "antigravity", ev, stdio.In, stdio.Out, stdio.Err, Version)
			return nil
		},
	})
	return c
}

func plannedCommands() []*cobra.Command {
	planned := []struct{ use, short, phase string }{
		{"docs", "Sinh tài liệu cho người đọc từ tri thức", "P3"},
	}
	var out []*cobra.Command
	for _, p := range planned {
		p := p
		out = append(out, &cobra.Command{
			Use:                p.use,
			Short:              p.short + " [" + p.phase + "]",
			DisableFlagParsing: true,
			RunE: func(*cobra.Command, []string) error {
				return withCode(ExitNotImplemented,
					fmt.Errorf("`bint %s` chưa được triển khai trong bản này (dự kiến %s)", p.use, p.phase))
			},
		})
	}
	return out
}

// bintPath trả về cách gọi bint cho hook: "bint" nếu PATH trỏ đúng tệp đang chạy,
// nếu không thì đường dẫn tuyệt đối.
func bintPath() string {
	self, err := os.Executable()
	if err != nil {
		return "bint"
	}
	self, _ = filepath.EvalSymlinks(self)
	if p, err := exec.LookPath("bint"); err == nil {
		if rp, err := filepath.EvalSymlinks(p); err == nil && state.NormalizePath(rp) == state.NormalizePath(self) {
			return "bint"
		}
	}
	return self
}

func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func confirm(stdio IO, q string) bool {
	fmt.Fprintf(stdio.Out, "%s [y/N] ", q)
	line, _ := bufio.NewReader(stdio.In).ReadString('\n')
	a := strings.ToLower(strings.TrimSpace(line))
	return a == "y" || a == "yes" || a == "c" || a == "có"
}

func ptr[T any](v T) *T { return &v }

func fmtTime(s string) string {
	if s == "" {
		return "-"
	}
	t, err := time.Parse("2006-01-02T15:04:05.000Z", s)
	if err != nil {
		return s
	}
	return t.Local().Format("2006-01-02 15:04")
}
