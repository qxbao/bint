package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"bint/internal/app"
	"bint/internal/ctxengine"
	"bint/internal/kindex"
)

func newContextCmd(stdio IO) *cobra.Command {
	var (
		paths   []string
		asJSON  bool
		budget  int
		session string
		verbose bool
	)
	c := &cobra.Command{
		Use:   `context ["<mô tả tác vụ>"]`,
		Short: "Truy xuất gói ngữ cảnh theo mô tả tác vụ hoặc theo đường dẫn",
		Long: `Context Engine (mục 8.2). Chỉ đọc; không đổi trạng thái tri thức.

  bint context "thêm cột email vào bảng users"      theo mô tả tác vụ (FTS5 + phạm vi đường dẫn)
  bint context --paths internal/db/db.go            quy tắc và cảnh báo có phạm vi khớp tệp
  bint context "sửa handler" --paths internal/api   kết hợp: đường dẫn làm gợi ý khu vực

Quy tắc chỉ được trình bày là bắt buộc khi đã được duyệt và bằng chứng còn khớp;
mục chưa duyệt hoặc có bằng chứng đã đổi được gắn nhãn tham khảo. Chỉ mục FTS5 được
đồng bộ tự động với .bint/knowledge/items trước mỗi lần truy xuất.

--session <id> (hoặc biến BINT_SESSION_ID) ghi nhận lần truy xuất vào lượt chạy
tương ứng; với --paths, mục đã trả về trong phiên đó không được lặp lại.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := cwd()
			if err != nil {
				return err
			}
			res, err := app.ContextPack(cmd.Context(), dir, app.ContextOptions{
				Task: strings.Join(args, " "), Paths: paths, Budget: budget, Session: session})
			if err != nil {
				return err
			}
			for _, w := range res.Warnings {
				fmt.Fprintln(stdio.Err, "cảnh báo:", w)
			}
			p := res.Pack
			if asJSON {
				if p.Entries == nil {
					p.Entries = []ctxengine.Entry{}
				}
				return writeJSON(stdio.Out, map[string]any{"pack": p, "text": p.Render()})
			}
			if len(p.Entries) == 0 && len(p.Lessons) == 0 {
				fmt.Fprintln(stdio.Out, "Không có tri thức liên quan.")
			} else {
				fmt.Fprint(stdio.Out, p.Render())
			}
			if verbose {
				fmt.Fprintf(stdio.Err, "\n~%d/%d token; %d mục", p.Tokens, p.Budget, len(p.Entries))
				if p.WorkKind != "" {
					fmt.Fprintf(stdio.Err, "; loại công việc: %s", p.WorkKind)
				}
				fmt.Fprintln(stdio.Err)
				for _, e := range p.Entries {
					fmt.Fprintf(stdio.Err, "  + %s [%s] %.2f %s\n", e.ID, e.Label, e.Score, strings.Join(e.Reasons, " "))
				}
				for _, o := range p.Omitted {
					fmt.Fprintf(stdio.Err, "  - %s: %s\n", o.ID, o.Reason)
				}
			}
			return nil
		},
	}
	f := c.Flags()
	f.StringSliceVar(&paths, "paths", nil, "đường dẫn tệp sắp đọc/sửa (tương đối repository)")
	f.BoolVar(&asJSON, "json", false, "xuất JSON (gói + văn bản)")
	f.IntVar(&budget, "budget", 0, "ngân sách token (mặc định context.max_tokens)")
	f.StringVar(&session, "session", "", "ID phiên để ghi nhận truy xuất và không lặp lại mục (mặc định $BINT_SESSION_ID)")
	f.BoolVarP(&verbose, "verbose", "v", false, "in lý do chọn/bỏ từng mục ra stderr")
	return c
}

func newIndexCmd(stdio IO) *cobra.Command {
	c := &cobra.Command{Use: "index", Short: "Chỉ mục tri thức dẫn xuất (FTS5)"}
	c.AddCommand(&cobra.Command{
		Use:   "rebuild",
		Short: "Xoá và dựng lại chỉ mục từ .bint/knowledge/items",
		Long: `Chỉ mục FTS5 trong SQLite là dữ liệu dẫn xuất: xoá và dựng lại không mất thông
tin. Bình thường không cần chạy tay — mọi lệnh truy xuất tự đồng bộ khi tệp đổi.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			k, err := openKnowledge(cmd)
			if err != nil {
				return err
			}
			if !k.Paths.Initialized() {
				return app.ErrNotInitialized
			}
			s, err := k.OpenStore(cmd.Context())
			if err != nil {
				return err
			}
			defer s.Close()
			res, err := kindex.Rebuild(cmd.Context(), s.DB, k.Store)
			if err != nil {
				return err
			}
			fmt.Fprintf(stdio.Out, "Đã dựng lại chỉ mục: %d mục", res.Total)
			if len(res.Errors) > 0 {
				fmt.Fprintf(stdio.Out, ", %d tệp hỏng bị bỏ qua", len(res.Errors))
			}
			fmt.Fprintln(stdio.Out)
			for _, e := range res.Errors {
				fmt.Fprintf(stdio.Out, "  ✗ %s\n", e.Error())
			}
			return nil
		},
	})
	return c
}

func newKnowledgeSearchCmd(stdio IO) *cobra.Command {
	var (
		limit  int
		all    bool
		asJSON bool
	)
	c := &cobra.Command{
		Use:   `search "<truy vấn>"`,
		Short: "Tìm tri thức theo từ khoá (FTS5, không phân biệt dấu)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := cwd()
			if err != nil {
				return err
			}
			so := app.SearchOptions{Query: strings.Join(args, " "), Limit: limit, All: all}
			// Không gọi so.Validate(): giữ hành vi cũ cho truy vấn trắng (không có kết quả, mã thoát 0).
			// Form TUI vẫn kiểm tra khi nhập.
			hits, err := app.SearchKnowledge(cmd.Context(), dir, so)
			if err != nil {
				return err
			}
			if asJSON {
				if hits == nil {
					hits = []kindex.Hit{}
				}
				return writeJSON(stdio.Out, hits)
			}
			if len(hits) == 0 {
				fmt.Fprintln(stdio.Out, "Không tìm thấy.")
				return nil
			}
			tw := tabwriter.NewWriter(stdio.Out, 0, 2, 2, ' ', 0)
			for _, h := range hits {
				fmt.Fprintf(tw, "%s\t%s/%s\t%.2f\t%s\n", h.ID, h.Type, h.Status, h.Score, truncate(h.Title, 70))
			}
			return tw.Flush()
		},
	}
	c.Flags().IntVar(&limit, "limit", 20, "số kết quả tối đa")
	c.Flags().BoolVar(&all, "all", false, "gồm cả mục deprecated")
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}

func newKnowledgeStatsCmd(stdio IO) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "stats",
		Short: "Số lần mỗi mục được truy xuất, được trích dẫn, và tác giả",
		Long: `Mức "được truy xuất" (Context Engine đã trả về) và "được áp dụng" (agent báo
qua cite_knowledge) của từng mục (mục 9.7). Đây không phải tác động thực: tác động chỉ
xác định được bằng đánh giá loại bỏ (bint eval), không suy ra từ số lần sử dụng.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := cwd()
			if err != nil {
				return err
			}
			st, err := app.StatsKnowledge(cmd.Context(), dir, app.StatsOptions{})
			if err != nil {
				return err
			}
			if asJSON {
				if st == nil {
					st = []kindex.Stats{}
				}
				return writeJSON(stdio.Out, st)
			}
			tw := tabwriter.NewWriter(stdio.Out, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tLOẠI/TRẠNG THÁI\tTRUY XUẤT\tTRÍCH DẪN\tLƯỢT\tTÁC GIẢ\tTIÊU ĐỀ")
			for _, s := range st {
				fmt.Fprintf(tw, "%s\t%s/%s\t%d\t%d\t%d\t%s\t%s\n", s.ID, s.Type, s.Status, s.Retrieved, s.Cited, s.Runs, truncate(s.Author, 24), truncate(s.Title, 50))
			}
			tw.Flush()
			fmt.Fprintln(stdio.Out, "\nSố lần dùng không phải tác động: tác động thực chỉ đo bằng đánh giá loại bỏ.")
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "xuất JSON")
	return c
}
