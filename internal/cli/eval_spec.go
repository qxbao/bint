package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"bint/internal/app"
)

func newEvalSpecCmd(stdio IO) *cobra.Command {
	var all, approvedToo bool
	c := &cobra.Command{
		Use:   "spec [id...]",
		Short: "Phân tích đặc tả đề bài: nối chữ ký giao diện kiểm thử sẽ dùng, cảnh báo đề bài thiếu đặc tả",
		Long: `Kiểm thử ẩn thường gọi hàm/lớp/hằng mới mà đề bài (tiêu đề commit) không nhắc tới;
agent không thể đoán đúng tên nên tác vụ không giải được ở mọi nhánh. bint eval mine tự làm việc
này cho ứng viên mới; lệnh này áp lại cho ứng viên đã khai thác trước đó.

Khối "Interface the hidden tests will use" chỉ chứa đường dẫn và chữ ký (không thân hàm, không
giá trị hằng) và được thay mới mỗi lần chạy; phần còn lại của đề bài (kể cả chỉnh tay) giữ nguyên.
Cảnh báo: interface_gap (định danh không trích được chữ ký), exact_text (kiểm thử so khớp nguyên văn
chuỗi mới), short_prompt (đề bài quá ngắn).

Tác vụ đã duyệt chỉ được áp khi có --approved: đổi đề bài làm đổi fingerprint của tác vụ, nên
kết quả các đợt chạy cũ không còn so sánh được với đợt mới.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := openEval(cmd, false)
			if err != nil {
				return err
			}
			res, err := app.EvalSpec(cmd.Context(), e, app.EvalSpecOptions{IDs: args, All: all, Approved: approvedToo})
			if err != nil {
				return err
			}
			counts := res.Counts
			fmt.Fprintf(stdio.Out, "Đã phân tích %d tác vụ: %d đủ đặc tả, %d cảnh báo; %d được nối chữ ký giao diện.\n",
				res.Analyzed, counts["pass"], counts["warn"], counts["interface"])
			if counts["warn"] > 0 {
				fmt.Fprintf(stdio.Out, "Cảnh báo: interface_gap %d, exact_text %d, short_prompt %d — xem `bint eval show <id>`, viết lại `prompt` hoặc loại tác vụ.\n",
					counts["interface_gap"], counts["exact_text"], counts["short_prompt"])
			}
			return nil
		},
	}
	f := c.Flags()
	f.BoolVar(&all, "all", false, "mọi ứng viên chưa duyệt")
	f.BoolVar(&approvedToo, "approved", false, "áp cả cho tác vụ đã duyệt (đổi fingerprint)")
	return c
}
