package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"bint/internal/eval"
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
			ctx := cmd.Context()
			e, err := openEval(cmd, false)
			if err != nil {
				return err
			}
			type target struct {
				c        *eval.Case
				approved bool
			}
			var targets []target
			if all {
				if len(args) > 0 {
					return usageError{fmt.Errorf("--all không đi cùng danh sách ID")}
				}
				cands, err := e.cases.Candidates()
				if err != nil {
					return err
				}
				for _, c := range cands {
					if c.Status != eval.StatusApproved {
						targets = append(targets, target{c, false})
					}
				}
				if approvedToo {
					cases, err := e.cases.Cases()
					if err != nil {
						return err
					}
					for _, c := range cases {
						targets = append(targets, target{c, true})
					}
				}
			} else {
				if len(args) == 0 {
					return usageError{fmt.Errorf("cần ID tác vụ hoặc --all")}
				}
				for _, id := range args {
					c, approved, err := e.cases.Get(id)
					if err != nil {
						return err
					}
					if approved && !approvedToo {
						return usageError{fmt.Errorf("%s đã được duyệt: đổi đề bài làm đổi fingerprint; thêm --approved nếu chắc chắn", id)}
					}
					targets = append(targets, target{c, approved})
				}
			}
			counts := map[string]int{}
			for _, t := range targets {
				in, err := eval.BuildLeakInput(ctx, e.ws.Repo.Toplevel, t.c)
				if err != nil {
					return err
				}
				eval.ApplySpec(ctx, e.ws.Repo.Toplevel, t.c, in)
				t.c.Leak = eval.CheckLeak(t.c.Prompt, in)
				save := e.cases.SaveCandidate
				if t.approved {
					save = e.cases.SaveCase
				}
				if err := save(t.c); err != nil {
					return err
				}
				counts[t.c.Spec.Status]++
				if len(t.c.Spec.Interface) > 0 {
					counts["interface"]++
				}
				for _, f := range t.c.Spec.Findings {
					counts[f.Kind]++
				}
			}
			fmt.Fprintf(stdio.Out, "Đã phân tích %d tác vụ: %d đủ đặc tả, %d cảnh báo; %d được nối chữ ký giao diện.\n",
				len(targets), counts["pass"], counts["warn"], counts["interface"])
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
