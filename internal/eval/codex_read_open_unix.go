//go:build !windows

package eval

import (
	"os"
	"syscall"
)

func codexOpenNonblocking(root *os.Root, path string) (*os.File, error) {
	return root.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
