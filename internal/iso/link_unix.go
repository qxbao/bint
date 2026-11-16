//go:build !windows

package iso

import (
	"os"

	"bint/internal/pathutil"
)

func isLink(path string) (bool, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	return fi.Mode()&os.ModeSymlink != 0, nil
}

func linkTarget(link string) (string, error) { return pathutil.LinkTarget(link) }

func createLink(link, target string) error { return os.Symlink(target, link) }

func removeLink(link string) error { return os.Remove(link) }
