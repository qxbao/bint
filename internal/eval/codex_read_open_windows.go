//go:build windows

package eval

import "os"

func codexOpenNonblocking(root *os.Root, path string) (*os.File, error) {
	return root.Open(path)
}
