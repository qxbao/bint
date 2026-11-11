package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"bint/internal/app"
	"bint/internal/ctxengine"
	"bint/internal/mcpserver"
)

func newMCPCmd(stdio IO) *cobra.Command {
	c := &cobra.Command{Use: "mcp", Short: "MCP server của Context Engine"}
	var evalCfg string
	serve := &cobra.Command{
		Use:   "serve",
		Short: "Chạy MCP server qua stdio (context_for_task, context_for_paths, knowledge_search, knowledge_get, cite_knowledge, report_observation)",
		Long: `MCP server qua stdio, trung lập với harness (mục 2.3). Đăng ký cho Claude Code:

  bint init --mcp                 ghi mục "bint" vào .mcp.json của repository
  claude mcp add bint -- bint mcp serve

Các công cụ chỉ đọc, trừ cite_knowledge và report_observation (ghi nhận; mục do
agent ghi luôn ở candidate). Truy xuất và trích dẫn được quy về lượt chạy qua hook
PostToolUse của Claude Code (bint init --hooks).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			srv := mcpserver.New("bint", Version, mcpserver.Instructions)
			if evalCfg == "" {
				evalCfg = os.Getenv(ctxengine.EvalEnv)
			}
			if evalCfg != "" {
				ec, err := ctxengine.LoadEvalConfig(evalCfg)
				if err != nil {
					return err
				}
				b, closeFn, err := app.NewEvalBackend(ctx, ec)
				if err != nil {
					return err
				}
				defer closeFn()
				mcpserver.RegisterTools(srv, b)
			} else {
				dir, err := cwd()
				if err != nil {
					return err
				}
				// Kiểm tra sớm để lỗi cấu hình hiện ra ngay khi harness khởi động server.
				if _, err := app.OpenKnowledge(ctx, dir); err != nil {
					return err
				}
				b := &app.RepoBackend{Dir: dir}
				defer b.Close()
				mcpserver.RegisterTools(srv, b)
			}
			return srv.Serve(ctx, stdio.In, stdio.Out)
		},
	}
	serve.Flags().StringVar(&evalCfg, "eval-config", "", "tệp cấu hình ngữ cảnh của lượt đánh giá (nội bộ; mặc định $"+ctxengine.EvalEnv+")")
	serve.Flags().MarkHidden("eval-config")
	tools := &cobra.Command{
		Use:   "tools",
		Short: "Liệt kê công cụ MCP",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			srv := mcpserver.New("bint", Version, "")
			mcpserver.RegisterTools(srv, nil)
			for _, t := range srv.Tools() {
				fmt.Fprintf(stdio.Out, "%-20s %s\n", t.Name, t.Description)
			}
			return nil
		},
	}
	c.AddCommand(serve, tools)
	return c
}

func newKnowledgeDiffCmd(stdio IO) *cobra.Command {
	var asJSON, all bool
	c := &cobra.Command{
		Use:   "diff [<từ>..<tới>]",
		Short: "Tri thức mới hoặc bị sửa trong một khoảng commit (mặc định: lần pull/merge gần nhất)",
		Long: `Liệt kê mục tri thức được thêm, sửa hoặc xoá trong khoảng commit (mặc định
ORIG_HEAD..HEAD, tức thay đổi vừa kéo về). Tri thức từ người khác là đầu vào cần
xem lại (mục 15.5); mục của chính bạn bị ẩn trừ khi có --all.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			k, err := openKnowledge(cmd)
			if err != nil {
				return err
			}
			rng := ""
			if len(args) == 1 {
				rng = args[0]
			}
			rep, err := app.DiffKnowledge(cmd.Context(), k, app.DiffOptions{Range: rng, All: all})
			if err != nil {
				return err
			}
			shown, rng := rep.Entries, rep.Range
			if asJSON {
				if shown == nil {
					shown = []app.DiffEntry{}
				}
				return writeJSON(stdio.Out, map[string]any{"range": rng, "entries": shown})
			}
			if len(shown) == 0 {
				fmt.Fprintf(stdio.Out, "Không có tri thức mới hoặc bị sửa từ người khác trong %s.\n", rng)
				return nil
			}
			fmt.Fprintf(stdio.Out, "Tri thức thay đổi trong %s:\n\n", rng)
			for _, e := range shown {
				icon := map[string]string{"added": "+", "modified": "~", "deleted": "-"}[e.Change]
				title, meta := "", ""
				if e.Item != nil {
					title = e.Item.Title
					meta = fmt.Sprintf(" [%s/%s, %s]", e.Item.Type, e.Item.Status, e.Item.Origin.Kind)
					if e.Item.Origin.ThirdParty {
						meta += " ⚠ bên thứ ba"
					}
				}
				fmt.Fprintf(stdio.Out, "  %s %s%s %s\n", icon, e.ID, meta, truncate(title, 60))
				fmt.Fprintf(stdio.Out, "      bởi %s\n", strings.Join(e.Authors, ", "))
			}
			fmt.Fprintln(stdio.Out, "\nXem: bint knowledge show <id>; mục chờ duyệt: bint knowledge review")
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	c.Flags().BoolVar(&all, "all", false, "gồm cả mục do chính bạn commit")
	return c
}
