package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// unknownSubcommand phát hiện lệnh con lạ ở mọi cấp: lệnh nhóm (không chạy được, có lệnh con) mà còn đối số không phải cờ.
// Cobra chỉ báo lỗi (kèm gợi ý) ở gốc; ở lệnh nhóm nó in help và thoát 0, nên người gõ sai (`bint task stauts`) không thấy lỗi.
// Trả usageError (mã 2) nêu lệnh lạ và dòng "Ý bạn là:" khi có gợi ý; nil nếu không phải lệnh lạ.
func unknownSubcommand(root *cobra.Command, args []string) error {
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	c, rest, _ := root.Find(args)
	if c == nil || c.Runnable() || !c.HasSubCommands() {
		return nil
	}
	word := ""
	for _, a := range rest {
		if !strings.HasPrefix(a, "-") {
			word = a
			break
		}
	}
	if word == "" || strings.HasPrefix(word, "__") {
		return nil
	}
	msg := fmt.Sprintf("lệnh lạ %q cho %q", word, c.CommandPath())
	c.SuggestionsMinimumDistance = 2 // cobra chỉ đặt mặc định này ở lệnh được thực thi
	if sg := c.SuggestionsFor(word); len(sg) > 0 {
		msg += "\n\nÝ bạn là:"
		for _, s := range sg {
			msg += "\n  " + c.CommandPath() + " " + s
		}
	}
	return usageError{fmt.Errorf("%s\nXem `%s --help`", msg, c.CommandPath())}
}
