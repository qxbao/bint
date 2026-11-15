package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"bint/internal/app"
)

// autoResync tự xuất lại skill gốc khi binary sạch mới hơn core.lock (xem app.AutoResync): một dòng lên stderr, không đổi
// stdout; lỗi chỉ thành cảnh báo, không làm lệnh thất bại. Chỉ gọi ở lệnh thường (status, task new, run), không ở hook.
func autoResync(cmd *cobra.Command, stdio IO) {
	dir, err := cwd()
	if err != nil {
		return
	}
	msg, err := app.AutoResync(cmd.Context(), dir, Version)
	switch {
	case err != nil:
		fmt.Fprintf(stdio.Err, "bint: cảnh báo: không tự xuất lại skill được: %v\n", err)
	case msg != "":
		fmt.Fprintln(stdio.Err, msg)
	}
}
