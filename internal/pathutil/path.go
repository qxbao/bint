// Package pathutil compares filesystem paths without changing their spelling
// or the persistent identifiers derived from them.
package pathutil

import (
	"errors"
	"os"
	"path/filepath"
)

// Canonical resolves symlinks, including in the nearest existing ancestor of
// a path that has not been created or has been removed. It preserves case.
// This is path comparison, not a race-safe authorization to open a file.
func Canonical(path string) (string, error) {
	cur, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	rest := ""
	for {
		resolved, err := filepath.EvalSymlinks(cur)
		if err == nil {
			return filepath.Join(resolved, rest), nil
		}
		parent := filepath.Dir(cur)
		if !errors.Is(err, os.ErrNotExist) || parent == cur {
			return "", err
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}
