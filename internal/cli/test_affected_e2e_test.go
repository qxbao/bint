package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/testutil"
)

type affectedOut struct {
	Selection struct {
		Scope    string   `json:"scope"`
		Base     string   `json:"base"`
		Head     string   `json:"head"`
		Changed  []string `json:"changed"`
		TestDirs []string `json:"test_dirs"`
		Widening []struct {
			Path   string `json:"path"`
			Tier   string `json:"tier"`
			Reason string `json:"reason"`
		} `json:"widening"`
	} `json:"selection"`
	Command  string `json:"command"`
	Ran      bool   `json:"ran"`
	ExitCode *int   `json:"exit_code"`
}

func writeRepoFile(t *testing.T, root, name, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTestAffectedCommand(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{
		"go.mod":      "module demo\n\ngo 1.21\n",
		"a/a.go":      "package a\n\nfunc A() int { return 1 }\n",
		"a/a_test.go": "package a\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n",
		"c/c.go":      "package c\n\nfunc C() {}\n",
		"c/c_test.go": "package c\n\nimport \"testing\"\n\nfunc TestC(t *testing.T) {}\n",
	})
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	testutil.Git(t, root, "add", "-A", "-f")
	testutil.Git(t, root, "commit", "-m", "init bint")
	t.Setenv("GOFLAGS", "-count=1")

	decode := func(args ...string) (affectedOut, string, int) {
		t.Helper()
		out, errOut, code := runSplit(t, root, "", args...)
		var o affectedOut
		if strings.Contains(strings.Join(args, " "), "--json") {
			if err := json.Unmarshal([]byte(out), &o); err != nil {
				t.Fatalf("%v: không phải JSON: %v\n%s\n%s", args, err, out, errOut)
			}
		}
		return o, out + errOut, code
	}

	// Không đổi gì: none.
	if o, out, code := decode("test", "affected", "--json"); code != 0 || o.Selection.Scope != "none" {
		t.Fatalf("không đổi: %d %+v\n%s", code, o, out)
	}
	// Tệp chưa commit trong gói a: graph, chỉ ./a; lệnh do bgraph dựng.
	writeRepoFile(t, root, "a/a.go", "package a\n\nfunc A() int { return 2 }\n")
	o, out, code := decode("test", "affected", "--json")
	if code != 0 || o.Selection.Scope != "graph" || len(o.Selection.TestDirs) != 1 || o.Selection.TestDirs[0] != "./a" ||
		o.Selection.Base == "" || len(o.Selection.Changed) != 1 || o.Command != "go test ./a" || o.Ran {
		t.Fatalf("graph: %d %+v\n%s", code, o, out)
	}
	// go.mod đổi: bgraph nới tới cả module, kèm lý do.
	writeRepoFile(t, root, "go.mod", "module demo\n\ngo 1.22\n")
	o, out, code = decode("test", "affected", "--json")
	if code != 0 || o.Selection.Scope != "module" || len(o.Selection.Widening) == 0 || o.Selection.Widening[0].Path != "go.mod" ||
		o.Command != "go test ./a ./c" {
		t.Fatalf("module: %d %+v\n%s", code, o, out)
	}
	// Văn bản.
	if _, txt, code := decode("test", "affected"); code != 0 || !strings.Contains(txt, "scope: module") || !strings.Contains(txt, "manifest_changed") {
		t.Fatalf("văn bản: %d\n%s", code, txt)
	}
	// Ref không hợp lệ: lỗi cách dùng.
	if _, txt, code := decode("test", "affected", "--base", "khong-co-ref"); code != 2 || !strings.Contains(txt, "--base") {
		t.Fatalf("base lạ: %d\n%s", code, txt)
	}
	testutil.Git(t, root, "checkout", "--", "go.mod")

	// --run: chạy lệnh của bgraph, mã thoát 0.
	o, out, code = decode("test", "affected", "--json", "--run")
	if code != 0 || !o.Ran || o.ExitCode == nil || *o.ExitCode != 0 || !strings.Contains(out, "ok") {
		t.Fatalf("--run: %d %+v\n%s", code, o, out)
	}
	// --run thoát theo mã của lệnh: test của a đỏ.
	writeRepoFile(t, root, "a/a_test.go", "package a\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) { t.Fatal(\"đỏ\") }\n")
	if _, out, code := decode("test", "affected", "--run"); code != 1 {
		t.Fatalf("--run test đỏ: mã %d\n%s", code, out)
	}
}
