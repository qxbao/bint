package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"bint/internal/app"
)

func newUpdateCmd(stdio IO) *cobra.Command {
	var (
		source string
		pull   bool
	)
	c := &cobra.Command{
		Use:   "update",
		Short: "Build lại và cài Bint từ thư mục mã nguồn (go install ./cmd/bint)",
		Long: `Chạy ` + "`go install ./cmd/bint`" + ` trong thư mục mã nguồn Bint, thay cho việc tự cd về đó.

Thư mục mã nguồn lấy theo thứ tự: --source, biến ` + app.SourceEnv + `, rồi thư mục mà bản
đang chạy được build từ đó (chỉ có khi build không dùng -trimpath).

--pull chạy ` + "`git pull --ff-only`" + ` trước khi build; mặc định chỉ build mã hiện có, kể cả
thay đổi chưa commit.

Sau khi cập nhật, chạy ` + "`bint init --update`" + ` (hoặc ` + "`bint sync`" + `) trong từng repository để
xuất bản skill gốc mới.`,
		Example: `  bint update
  bint update --pull
  bint update --source ~/src/bint`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			res, err := app.UpdateSelf(cmd.Context(), app.UpdateOptions{Source: source, Pull: pull, Out: stdio.Err})
			if res.Source != "" {
				fmt.Fprintf(stdio.Out, "Mã nguồn: %s\n", res.Source)
			}
			if err != nil {
				return err
			}
			if res.Pulled != "" {
				fmt.Fprintf(stdio.Out, "git pull: %s\n", res.Pulled)
			}
			fmt.Fprintf(stdio.Out, "Đã cài: %s (%s → %s)\n", res.Target, orUnknown(res.OldRev), orUnknown(res.NewRev))
			if res.OldBackup != "" {
				fmt.Fprintf(stdio.Out, "Bản cũ được đổi tên thành %s (xoá được sau khi thoát lệnh này)\n", res.OldBackup)
			}
			if res.Mismatch {
				fmt.Fprintf(stdio.Out, "! Bản đang chạy là %s, khác tệp vừa cài; kiểm tra PATH (`which bint`) nếu lệnh bint vẫn là bản cũ.\n", res.Current)
			}
			fmt.Fprintln(stdio.Out, "Cập nhật skill trong từng repository: bint init --update (hoặc bint sync)")
			return nil
		},
	}
	c.Flags().StringVar(&source, "source", "", "thư mục mã nguồn Bint (mặc định: "+app.SourceEnv+" hoặc thư mục lúc build)")
	c.Flags().BoolVar(&pull, "pull", false, "git pull --ff-only trước khi build")
	return c
}

func orUnknown(s string) string {
	if s == "" {
		return "không rõ"
	}
	return s
}
