package testsel

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"bint/internal/affected"
	"bint/internal/testutil"
)

func TestMain(m *testing.M) { testutil.Main(m) }

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// goRepo: gói a (có test), gói b phụ thuộc a (có test), gói c độc lập (có test).
func goRepo(t *testing.T) string {
	t.Helper()
	testutil.Isolate(t)
	return testutil.NewRepo(t, map[string]string{
		"go.mod":      "module example.com/m\n\ngo 1.21\n",
		"a/a.go":      "package a\n\nfunc A() int { return 1 }\n",
		"a/a_test.go": "package a\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n",
		"b/b.go":      "package b\n\nimport \"example.com/m/a\"\n\nfunc B() int { return a.A() }\n",
		"b/b_test.go": "package b\n\nimport \"testing\"\n\nfunc TestB(t *testing.T) {}\n",
		"c/c.go":      "package c\n\nfunc C() {}\n",
		"c/c_test.go": "package c\n\nimport \"testing\"\n\nfunc TestC(t *testing.T) {}\n",
	})
}

func TestSelectGoGraph(t *testing.T) {
	root := goRepo(t)
	write(t, root, "a/a.go", "package a\n\nfunc A() int { return 2 }\n")
	sel := Select(context.Background(), Input{Root: root, Base: "HEAD", StateDir: t.TempDir()})
	if sel.Scope != affected.TierGraph || sel.Source != "bgraph" {
		t.Fatalf("scope %s source %q, widening %+v", sel.Scope, sel.Source, sel.Widening)
	}
	if !slices.Equal(sel.TestDirs, []string{"./a", "./b"}) {
		t.Fatalf("test_dirs = %v, muốn ./a ./b (không có ./c)", sel.TestDirs)
	}
	script := sel.Script()
	if !strings.Contains(script, "go test") || !strings.Contains(script, "./a") || !strings.Contains(script, "./b") || strings.Contains(script, "./c") {
		t.Fatalf("lệnh chạy = %q", script)
	}
}

func TestSelectNoChangeAndNoBase(t *testing.T) {
	root := goRepo(t)
	if sel := Select(context.Background(), Input{Root: root, Base: "HEAD", StateDir: t.TempDir()}); sel.Scope != affected.TierNone || sel.Script() != "" {
		t.Fatalf("không đổi gì: scope %s, lệnh %q", sel.Scope, sel.Script())
	}
	sel := Select(context.Background(), Input{Root: root, StateDir: t.TempDir()})
	if sel.Scope != affected.TierFull || len(sel.Widening) == 0 || sel.Widening[0].Reason != affected.ReasonNoBase || sel.Script() != "" {
		t.Fatalf("không có mốc: %+v", sel)
	}
}

// Sổ trạng thái của Bint trong cây làm việc không bao giờ là thay đổi ảnh hưởng tới test.
func TestSelectExcludesLedger(t *testing.T) {
	root := goRepo(t)
	write(t, root, ".bint/tasks/01X/state.json", "{}\n")
	if sel := Select(context.Background(), Input{Root: root, Base: "HEAD", StateDir: t.TempDir()}); sel.Scope == affected.TierNone {
		t.Fatalf("không loại thì tệp lạ phải được tính là thay đổi: %+v", sel)
	}
	sel := Select(context.Background(), Input{Root: root, Base: "HEAD", StateDir: t.TempDir(), Exclude: []string{".bint/tasks/**"}})
	if sel.Scope != affected.TierNone || len(sel.Changed) != 0 {
		t.Fatalf("sổ phiếu bị loại thì không còn thay đổi: %+v", sel)
	}
}

// Only giữ đơn vị chứa tệp được nêu (cổng mục tiêu): test mới c2_test.go trong gói c.
func TestSelectOnly(t *testing.T) {
	root := goRepo(t)
	write(t, root, "a/a.go", "package a\n\nfunc A() int { return 2 }\n")
	write(t, root, "c/goal_test.go", "package c\n\nimport \"testing\"\n\nfunc TestGoal(t *testing.T) {}\n")
	sel := Select(context.Background(), Input{Root: root, Base: "HEAD", StateDir: t.TempDir(), Only: []string{"c/goal_test.go"}})
	if sel.Scope == affected.TierFull || !slices.Equal(sel.TestDirs, []string{"./c"}) {
		t.Fatalf("Only c/goal_test.go: scope %s dirs %v widening %+v", sel.Scope, sel.TestDirs, sel.Widening)
	}
}

func TestSelectNode(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{
		"package.json":        `{"name":"n","private":true,"scripts":{"test":"vitest run"},"devDependencies":{"vitest":"^2.0.0"}}` + "\n",
		"src/a.ts":            "export const a = 1;\n",
		"src/b.ts":            "export const b = 2;\n",
		"tests/a.spec.ts":     "import {test} from 'vitest';\nimport {a} from '../src/a';\ntest('a', () => {});\n",
		"tests/b.spec.ts":     "import {test} from 'vitest';\nimport {b} from '../src/b';\ntest('b', () => {});\n",
		"vitest.config.ts":    "export default {};\n",
		"package-lock.json":   `{"name":"n","lockfileVersion":3,"packages":{"":{"name":"n"}}}` + "\n",
		"node_modules/.keep":  "",
		".gitignore":          "node_modules/\n",
		"src/unused/.gitkeep": "",
	})
	write(t, root, "src/a.ts", "export const a = 3;\n")
	sel := Select(context.Background(), Input{Root: root, Base: "HEAD", StateDir: t.TempDir()})
	if sel.Scope != affected.TierGraph || !slices.Equal(sel.TestFiles, []string{"tests/a.spec.ts"}) {
		t.Fatalf("scope %s files %v widening %+v", sel.Scope, sel.TestFiles, sel.Widening)
	}
	if s := sel.Script(); !strings.Contains(s, "tests/a.spec.ts") || strings.Contains(s, "b.spec") {
		t.Fatalf("lệnh = %q", s)
	}
}
