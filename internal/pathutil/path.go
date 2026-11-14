// Package pathutil compares filesystem paths without changing their spelling
// or the persistent identifiers derived from them.
package pathutil

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// maxLinks bounds how many dangling symlinks Canonical follows itself, so a
// chain that only cycles through missing targets still terminates.
const maxLinks = 255

var errTooManyLinks = errors.New("too many links")

// Canonical resolves symlinks, including in the nearest existing ancestor of
// a path that has not been created or has been removed. A dangling symlink is
// followed to its target: writing through it creates the target, so that is
// the path a policy must see. It preserves case.
// This is path comparison, not a race-safe authorization to open a file.
func Canonical(path string) (string, error) {
	cur, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	rest := ""
	links := 0
	for {
		resolved, err := filepath.EvalSymlinks(cur)
		if err == nil {
			return filepath.Join(resolved, rest), nil
		}
		if !missing(err) {
			return "", err
		}
		// cur exists only as a link whose target is missing (EvalSymlinks on an
		// existing non-link would have succeeded): continue from the target.
		if target, lerr := LinkTarget(cur); lerr == nil {
			if links++; links > maxLinks {
				return "", &os.PathError{Op: "canonical", Path: path, Err: errTooManyLinks}
			}
			cur = target
			continue
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", err
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// BestEffort is Canonical, falling back to the cleaned absolute path when the
// filesystem cannot answer (permissions, volumes EvalSymlinks does not
// support). Policy checks use it so such errors degrade to the lexical
// comparison instead of skipping the check.
func BestEffort(path string) string {
	if c, err := Canonical(path); err == nil {
		return c
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return filepath.Clean(path)
}

// LinkTarget returns where the symlink at link points. A relative target is
// resolved against the physical directory holding the link: the OS applies
// ".." after resolving that directory, so a lexical join is wrong when the
// directory is itself reached through a symlink.
func LinkTarget(link string) (string, error) {
	target, err := os.Readlink(link)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(target) {
		return filepath.Clean(target), nil
	}
	dir := filepath.Dir(link)
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		dir = r
	}
	return filepath.Join(dir, target), nil
}

// missing reports errors meaning some component of the path does not exist
// yet; ENOTDIR covers a regular file where a directory is expected.
func missing(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}
