package gitrepo

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func fakeGitDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	name := "git"
	body := "#!/bin/sh\nexit 0\n"
	if runtime.GOOS == "windows" {
		name = "git.exe"
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(exe)
		if err != nil {
			t.Fatal(err)
		}
		body = string(b)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLookGitCachesPerPath(t *testing.T) {
	d1, d2 := fakeGitDir(t), fakeGitDir(t)
	t.Setenv("PATH", d1)
	before := lookupCount()
	c1 := Command("status")
	c2 := Command("status")
	if got := lookupCount() - before; got != 1 {
		t.Fatalf("hai lần gọi cùng PATH dò %d lần, muốn 1", got)
	}
	if filepath.Dir(c1.Path) != d1 || c2.Path != c1.Path {
		t.Fatalf("đường dẫn %q %q, muốn trong %q", c1.Path, c2.Path, d1)
	}
	t.Setenv("PATH", d2)
	c3 := Command("status")
	if got := lookupCount() - before; got != 2 {
		t.Fatalf("đổi PATH phải dò lại: %d lần", got)
	}
	if filepath.Dir(c3.Path) != d2 {
		t.Fatalf("sau đổi PATH dùng %q, muốn git giả trong %q", c3.Path, d2)
	}
	if runtime.GOOS == "windows" {
		t.Setenv("PATHEXT", ".EXE")
		Command("status")
		if got := lookupCount() - before; got != 3 {
			t.Fatalf("đổi PATHEXT phải dò lại: %d lần", got)
		}
	}
}

// moduleRoot tìm thư mục chứa go.mod từ thư mục hiện tại đi lên.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("không thấy go.mod")
		}
		dir = parent
	}
}

// TestNoDirectGitExec chặn hồi quy: mã không phải test (mọi gói trong module, gồm cmd/) chỉ gọi git qua helper.
// Gói os/exec được nhận diện qua import nên import đổi tên vẫn bị bắt.
func TestNoDirectGitExec(t *testing.T) {
	root := moduleRoot(t)
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); p != root && (strings.HasPrefix(n, ".") || n == "vendor" || n == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		if filepath.Base(p) == "command.go" && filepath.Base(filepath.Dir(p)) == "gitrepo" {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			return err
		}
		names := map[string]bool{}
		for _, im := range f.Imports {
			if path, _ := strconv.Unquote(im.Path.Value); path != "os/exec" {
				continue
			}
			name := "exec"
			if im.Name != nil {
				name = im.Name.Name
			}
			names[name] = true
		}
		if len(names) == 0 {
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); !ok || !names[x.Name] || (sel.Sel.Name != "Command" && sel.Sel.Name != "CommandContext") {
				return true
			}
			for _, a := range call.Args {
				if lit, ok := a.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if s, _ := strconv.Unquote(lit.Value); s == "git" {
						t.Errorf("%s: dùng gitrepo.Command thay cho %s.%s(%q)", fset.Position(call.Pos()), sel.X.(*ast.Ident).Name, sel.Sel.Name, s)
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
