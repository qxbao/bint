//go:build !windows

package tui

import "syscall"

// RestartSupported: Unix thay tiến trình bằng tệp vừa cài (exec) nên khởi động lại được ngay trong terminal.
const RestartSupported = true

// execFn thay được trong kiểm thử (không exec thật).
var execFn = syscall.Exec

// restartSelf thay tiến trình hiện tại bằng tệp path với đối số args (args[0] là tên lệnh) và môi trường env. Chỉ trở về khi lỗi.
func restartSelf(path string, args, env []string) error {
	if len(args) == 0 {
		args = []string{path}
	}
	return execFn(path, args, env)
}
