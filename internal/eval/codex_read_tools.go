package eval

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"bint/internal/pathglob"
)

const codexReadLimit = 64 << 10

type codexContextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r codexContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func isCodexReader(name string) bool { return name == "Read" || name == "Grep" || name == "Glob" }
func codexReaderSpecs() []any {
	specs := []any{}
	for _, name := range []string{"Read", "Grep", "Glob"} {
		properties := map[string]any{}
		required := []string{}
		description := ""
		switch name {
		case "Read":
			properties["path"] = map[string]string{"type": "string"}
			required = []string{"path"}
			description = "Read a regular UTF-8 source file relative to the repository (bounded output). No symlinks, eval definitions or .git."
		case "Grep":
			properties["pattern"] = map[string]string{"type": "string"}
			required = []string{"pattern"}
			description = "Search repository text using a Go regular expression. Read-only, bounded output; excludes symlinks, .git and eval definitions."
		case "Glob":
			properties["pattern"] = map[string]string{"type": "string"}
			required = []string{"pattern"}
			description = "List repository paths matching a relative glob, including **. Read-only; excludes symlinks, .git and eval definitions."
		}
		specs = append(specs, map[string]any{"type": "function", "name": name, "description": description, "inputSchema": map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}})
	}
	return specs
}

// No shell is involved. Each opened component is checked against its Lstat
// identity so a concurrent symlink replacement cannot redirect the reader.
// Reflection/proposer never register these handlers.
func codexReadablePath(rel string) bool {
	if rel == "" || filepath.IsAbs(rel) || strings.Contains(rel, "\\") {
		return false
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") || filepath.ToSlash(filepath.Clean(rel)) != rel {
		return false
	}
	for _, part := range strings.Split(rel, "/") {
		if strings.EqualFold(part, ".git") {
			return false
		}
	}
	rel = strings.ToLower(rel)
	return rel != ".bint/evals" && !strings.HasPrefix(rel, ".bint/evals/") && rel != ".bint/runtime" && !strings.HasPrefix(rel, ".bint/runtime/")
}
func codexOpenSource(root *os.Root, rel string) (*os.File, error) {
	if !codexReadablePath(rel) {
		return nil, errors.New("path outside readable source scope")
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	current := root
	for _, part := range parts[:len(parts)-1] {
		st, err := current.Lstat(part)
		if err != nil {
			if current != root {
				current.Close()
			}
			return nil, err
		}
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			if current != root {
				current.Close()
			}
			return nil, errors.New("symlinks are not readable")
		}
		next, err := current.OpenRoot(part)
		if current != root {
			current.Close()
		}
		if err != nil {
			return nil, err
		}
		opened, err := next.Stat(".")
		if err != nil || !os.SameFile(st, opened) {
			next.Close()
			return nil, errors.New("source directory changed during open")
		}
		current = next
	}
	if current != root {
		defer current.Close()
	}
	name := parts[len(parts)-1]
	before, err := current.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, errors.New("only regular source files are readable")
	}
	f, err := codexOpenNonblocking(current, name)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || !os.SameFile(before, st) {
		f.Close()
		return nil, errors.New("only regular source files are readable")
	}
	return f, nil
}
func runCodexReaderContext(ctx context.Context, dir, tool string, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !isCodexReader(tool) || dir == "" {
		return "", errors.New("reader not available")
	}
	var args struct{ Path, Pattern string }
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&args); err != nil {
		return "", err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	if tool == "Read" {
		f, err := codexOpenSource(root, args.Path)
		if err != nil {
			return "", err
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(codexContextReader{ctx, f}, codexReadLimit+1))
		if err != nil {
			return "", err
		}
		if bytesBinary(b) {
			return "", errors.New("binary source file")
		}
		if len(b) > codexReadLimit {
			b = b[:codexReadLimit]
			return string(b) + "\n[truncated]", nil
		}
		return string(b), nil
	}
	var expression *regexp.Regexp
	if tool == "Grep" {
		expression, err = regexp.Compile(args.Pattern)
		if err != nil {
			return "", err
		}
	}
	if args.Pattern == "" {
		return "", errors.New("pattern is required")
	}
	var out strings.Builder
	visited := 0
	// Traverse from verified directory handles: reopening a full path after
	// inspecting a DirEntry would allow a symlink swap to redirect Glob.
	var visit func(*os.Root, string, fs.DirEntry) error
	walkDirectory := func(parent *os.Root, name, prefix string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		before, err := parent.Lstat(name)
		if err != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
			return nil
		}

		child, err := parent.OpenRoot(name)
		if err != nil {
			return nil
		}
		defer child.Close()
		opened, err := child.Stat(".")
		if err != nil || !os.SameFile(before, opened) {
			return nil
		}
		dir, err := child.Open(".")
		if err != nil {
			return nil
		}
		defer dir.Close()
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			entries, readErr := dir.ReadDir(128)
			for _, d := range entries {
				rel := d.Name()
				if prefix != "" {
					rel = prefix + "/" + rel
				}
				if err := visit(child, rel, d); err != nil {
					return err
				}
			}
			if readErr == io.EOF {
				return nil
			}
			if readErr != nil {
				return readErr
			}
		}
	}
	visit = func(parent *os.Root, rel string, d fs.DirEntry) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !codexReadablePath(rel) || d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return nil
			}
			return nil
		}
		visited++
		if visited > 10000 || out.Len() >= codexReadLimit {
			return fs.SkipAll
		}
		if d.IsDir() {
			return walkDirectory(parent, d.Name(), rel)
		}
		if tool == "Glob" {
			if pathglob.MatchAny([]string{args.Pattern}, rel) {
				fmt.Fprintln(&out, rel)
			}
			return nil
		}
		f, err := codexOpenSource(root, rel)
		if err != nil {
			return nil
		}
		defer f.Close()
		sc := bufio.NewScanner(io.LimitReader(codexContextReader{ctx, f}, codexReadLimit))
		line := 0
		for sc.Scan() {
			if err := ctx.Err(); err != nil {
				return err
			}
			line++
			if bytesBinary(sc.Bytes()) {
				break
			}
			if expression.Match(sc.Bytes()) {
				fmt.Fprintf(&out, "%s:%d:%s\n", rel, line, sc.Text())
				if out.Len() >= codexReadLimit {
					break
				}
			}
		}
		return ctx.Err()
	}
	err = walkDirectory(root, ".", "")
	if errors.Is(err, fs.SkipAll) {
		err = nil
	}
	if out.Len() > codexReadLimit {
		return out.String()[:codexReadLimit] + "\n[truncated]", err
	}
	return out.String(), err
}
func bytesBinary(b []byte) bool {
	for _, c := range b {
		if c == 0 {
			return true
		}
	}
	return false
}
