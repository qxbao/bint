package affected

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeGraph là đồ thị giả cho bảng phân tầng.
type fakeGraph struct {
	refs     map[string][]string
	pkgs     map[string]bool
	nested   map[string]bool
	hasCode  map[string]bool
	embedded map[string][]string
}

func (f fakeGraph) PackageDir(d string) bool { return f.pkgs[d] }
func (f fakeGraph) HasCode(d string) bool    { return f.hasCode[d] }
func (f fakeGraph) Embedded(p string) []string {
	return f.embedded[p]
}
func (f fakeGraph) Nested(p string) bool {
	for d := range f.nested {
		if strings.HasPrefix(p, d+"/") {
			return true
		}
	}
	return false
}
func (f fakeGraph) IsCode(p string) bool                { return strings.HasSuffix(p, ".go") }
func (f fakeGraph) TestRefs(p string) ([]string, error) { return f.refs[p], nil }
func (f fakeGraph) Select(dirs []string) ([]string, error) {
	var out []string
	for _, d := range dirs {
		out = append(out, d+"/x_test.go")
	}
	return out, nil
}

func TestClassifyTable(t *testing.T) {
	g := fakeGraph{
		pkgs:     map[string]bool{"a": true, "internal/b": true, ".": true},
		nested:   map[string]bool{"tools": true},
		hasCode:  map[string]bool{"a": true, "internal/b": true, "weird": true},
		embedded: map[string][]string{"a/tpl/x.md": {"a"}},
		refs:     map[string][]string{"docs/ref.md": {"a"}, "a/GUIDE.md": {"internal/b"}},
	}
	g.pkgs["."] = false // gốc không phải gói
	tests := []struct {
		name    string
		changed []string
		tier    Tier
		reasons []string // lý do nới theo thứ tự đường dẫn được sắp
	}{
		{"không đổi", nil, TierNone, nil},
		{"md ngoài gói", []string{"docs/a.md", "NOTES.txt"}, TierNone, nil},
		{"md ngoài gói có test nhắc", []string{"docs/ref.md"}, TierPackage, []string{ReasonTestRef}},
		{"txt thư mục con của gói", []string{"a/golden/out.txt"}, TierPackage, []string{ReasonNonCode}},
		{"go trong gói", []string{"a/a.go"}, TierGraph, nil},
		{"go bị build tag loại", []string{"a/a_windows.go"}, TierGraph, nil},
		{"testdata", []string{"a/testdata/x.json"}, TierPackage, []string{ReasonTestdata}},
		{"testdata lồng sâu", []string{"a/testdata/y/z.go"}, TierPackage, []string{ReasonTestdata}},
		{"testdata không thuộc gói", []string{"zzz/testdata/x.json"}, TierFull, []string{ReasonUnknownPackage}},
		{"không phải mã trong gói", []string{"a/data.json"}, TierPackage, []string{ReasonNonCode}},
		{"md trong gói", []string{"a/README.md"}, TierPackage, []string{ReasonNonCode}},
		{"md trong gói có test gói khác nhắc", []string{"a/GUIDE.md"}, TierPackage, []string{ReasonNonCode, ReasonTestRef}},
		{"tệp nhúng", []string{"a/tpl/x.md"}, TierPackage, []string{ReasonEmbedded}},
		{"go.mod", []string{"go.mod"}, TierFull, []string{ReasonRootConfig}},
		{"go.sum", []string{"go.sum"}, TierFull, []string{ReasonRootConfig}},
		{"go.work", []string{"go.work"}, TierFull, []string{ReasonRootConfig}},
		{"go.mod lồng", []string{"tools/go.mod"}, TierFull, []string{ReasonRootConfig}},
		{"cấu hình bint", []string{".bint/config.yml"}, TierFull, []string{ReasonBintConfig}},
		{"CI", []string{".github/workflows/ci.yml"}, TierFull, []string{ReasonCIConfig}},
		{"Makefile", []string{"Makefile"}, TierFull, []string{ReasonCIConfig}},
		{"script", []string{"build.sh"}, TierFull, []string{ReasonCIConfig}},
		{"tệp lạ ngoài gói", []string{"data/blob.bin"}, TierFull, []string{ReasonUnknownFile}},
		{"module lồng", []string{"tools/x/y.go"}, TierFull, []string{ReasonNestedModule}},
		{"gói bị xoá", []string{"gone/g.go"}, TierFull, []string{ReasonDeletedPackage}},
		{"thư mục có mã không ra gói", []string{"weird/w.go"}, TierFull, []string{ReasonUnknownPackage}},
		{"trộn: graph + md + testdata", []string{"a/a.go", "docs/x.md", "internal/b/testdata/t"}, TierPackage, []string{ReasonTestdata}},
		{"trộn có full", []string{"a/a.go", "go.mod"}, TierFull, []string{ReasonRootConfig}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			slices.Sort(tc.changed)
			res := Classify(tc.changed, g)
			if res.Tier != tc.tier {
				t.Fatalf("tầng = %s, muốn %s (nới: %+v)", res.Tier, tc.tier, res.Widening)
			}
			var got []string
			for _, w := range res.Widening {
				if w.Path == "" || w.Tier == "" || w.Reason == "" {
					t.Errorf("lần nới thiếu đường dẫn/tầng/lý do: %+v", w)
				}
				got = append(got, w.Reason)
			}
			if !slices.Equal(got, tc.reasons) {
				t.Errorf("lý do nới = %v, muốn %v", got, tc.reasons)
			}
		})
	}
}

type fakeSource struct {
	detect bool
	err    error
	g      Graph
}

func (fakeSource) Name() string         { return "fake" }
func (f fakeSource) Detect(string) bool { return f.detect }
func (f fakeSource) Load(context.Context, string) (Graph, error) {
	return f.g, f.err
}

func TestSelectTiersAndFallbacks(t *testing.T) {
	ctx := context.Background()
	g := fakeGraph{pkgs: map[string]bool{"a": true}, hasCode: map[string]bool{"a": true}}
	one := func(src Source, changed ...string) Selection {
		return Select(ctx, Input{Root: t.TempDir(), Base: "B", Head: "H", Changed: changed, Sources: []Source{src}})
	}
	if s := one(fakeSource{detect: true, g: g}); s.Scope != TierNone || s.Base != "B" || s.Head != "H" {
		t.Errorf("không đổi: %+v", s)
	}
	s := one(fakeSource{detect: true, g: g}, "a/a.go")
	if s.Scope != TierGraph || !slices.Equal(s.TestDirs, []string{"./a"}) || !slices.Equal(s.TestFiles, []string{"a/x_test.go"}) {
		t.Errorf("graph: %+v", s)
	}
	if s := one(fakeSource{detect: true, g: g}, "a/testdata/q"); s.Scope != TierPackage {
		t.Errorf("package: %+v", s)
	}
	if s := one(fakeSource{detect: true, g: g}, "README.md"); s.Scope != TierNone || len(s.TestFiles) != 0 {
		t.Errorf("none: %+v", s)
	}
	for name, src := range map[string]fakeSource{
		"không nguồn": {detect: false},
		"nguồn lỗi":   {detect: true, err: errors.New("boom")},
	} {
		s := one(src, "a/a.go")
		want := ReasonUnsupported
		if src.detect {
			want = ReasonSourceUnavailable
		}
		if s.Scope != TierFull || len(s.Widening) == 0 || s.Widening[len(s.Widening)-1].Reason != want {
			t.Errorf("%s: %+v", name, s)
		}
	}
	// Không đổi gì thì không cần nguồn; thay đổi cấu hình full dù nguồn lỗi.
	if s := one(fakeSource{err: errors.New("x")}); s.Scope != TierNone {
		t.Errorf("rỗng: %+v", s)
	}
	if s := one(fakeSource{detect: true, err: errors.New("x")}, "go.mod"); s.Scope != TierFull || s.Widening[0].Reason != ReasonRootConfig {
		t.Errorf("go.mod: %+v", s)
	}
}

// TestSelectIgnoreBintConfig: bint_config_full: false (IgnoreBintConfig) thì .bint/** không nới full nhưng vẫn nằm trong Changed;
// mặc định (true) vẫn nới ra full vì ReasonBintConfig.
func TestSelectIgnoreBintConfig(t *testing.T) {
	ctx := context.Background()
	g := fakeGraph{pkgs: map[string]bool{"a": true}, hasCode: map[string]bool{"a": true}}
	sel := func(ignore bool, changed ...string) Selection {
		return Select(ctx, Input{Root: t.TempDir(), Base: "B", Head: "H", Changed: changed, Sources: []Source{fakeSource{detect: true, g: g}}, IgnoreBintConfig: ignore})
	}
	if s := sel(false, ".bint/config.yml"); s.Scope != TierFull || len(s.Widening) != 1 || s.Widening[0].Reason != ReasonBintConfig {
		t.Errorf("mặc định phải nới full vì bint_config: %+v", s)
	}
	s := sel(true, ".bint/config.yml")
	if s.Scope != TierNone || len(s.Widening) != 0 || !slices.Equal(s.Changed, []string{".bint/config.yml"}) {
		t.Errorf("chỉ .bint đổi, bỏ qua: muốn none, Changed giữ tệp, không widening: %+v", s)
	}
	s = sel(true, ".bint/config.yml", "a/a.go")
	if s.Scope != TierGraph || !slices.Equal(s.TestDirs, []string{"./a"}) || !slices.Contains(s.Changed, ".bint/config.yml") {
		t.Errorf(".bint + mã: chỉ tầng của mã: %+v", s)
	}
	if s := sel(true, "go.mod", ".bint/config.yml"); s.Scope != TierFull || s.Widening[0].Reason != ReasonRootConfig {
		t.Errorf("go.mod vẫn nới full: %+v", s)
	}
}

func TestSelectTruncatesChanged(t *testing.T) {
	var ch []string
	for i := range MaxChanged + 5 {
		ch = append(ch, "docs/"+string(rune('a'+i%26))+strings.Repeat("x", i)+".md")
	}
	s := Select(context.Background(), Input{Root: t.TempDir(), Changed: ch, Sources: []Source{fakeSource{detect: true, g: fakeGraph{}}}})
	if !s.Truncated || len(s.Changed) != MaxChanged || s.ChangedTotal != MaxChanged+5 {
		t.Errorf("cắt: truncated=%v len=%d total=%d", s.Truncated, len(s.Changed), s.ChangedTotal)
	}
}

// --- nguồn Go trên module mẫu ---

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func sampleModule(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"go.mod":            "module example.com/m\n\ngo 1.21\n",
		"a/a.go":            "package a\n\nfunc A() int { return 1 }\n",
		"a/a_windows.go":    "//go:build windows\n\npackage a\n\nfunc W() {}\n",
		"a/a_test.go":       "package a\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n",
		"b/b.go":            "package b\n\nimport \"example.com/m/a\"\n\nfunc B() int { return a.A() }\n",
		"b/b_test.go":       "package b\n\nimport \"testing\"\n\nfunc TestB(t *testing.T) {}\n",
		"c/c.go":            "package c\n\nfunc C() {}\n",
		"c/c_test.go":       "package c_test\n\nimport \"testing\"\n\nfunc TestC(t *testing.T) {}\n",
		"e/e.go":            "package e\n\nimport _ \"embed\"\n\n//go:embed tpl/*.txt\nvar T string\n",
		"e/tpl/x.txt":       "x",
		"e/e_test.go":       "package e\n\nimport \"testing\"\n\nfunc TestE(t *testing.T) {}\n",
		"nested/go.mod":     "module example.com/nested\n\ngo 1.21\n",
		"nested/n.go":       "package n\n",
		"docs/readme.md":    "hi",
		"nodir/only_gen.go": "//go:build ignore\n\npackage main\n",
	})
	return root
}

func TestGoSourceSampleModule(t *testing.T) {
	root := sampleModule(t)
	sel := func(changed ...string) Selection {
		return Select(context.Background(), Input{Root: root, Changed: changed})
	}
	s := sel("a/a_windows.go")
	if s.Scope != TierGraph || s.Source != "go" {
		t.Fatalf("a_windows.go: %+v", s)
	}
	want := []string{"./a", "./b"}
	if !slices.Equal(s.TestDirs, want) {
		t.Errorf("test_dirs = %v, muốn %v (không có c)", s.TestDirs, want)
	}
	if s := sel("c/c.go"); !slices.Equal(s.TestDirs, []string{"./c"}) || !slices.Contains(s.TestFiles, "c/c_test.go") {
		t.Errorf("c độc lập: %+v", s)
	}
	if s := sel("b/b.go"); !slices.Equal(s.TestDirs, []string{"./b"}) {
		t.Errorf("b lá: %+v", s)
	}
	if s := sel("e/tpl/x.txt"); s.Scope != TierPackage || !slices.Equal(s.TestDirs, []string{"./e"}) ||
		s.Widening[0].Reason != ReasonEmbedded {
		t.Errorf("nhúng: %+v", s)
	}
	writeFiles(t, root, map[string]string{
		"c/c2_test.go":     "package c_test\n\nimport \"os\"\n\nvar _, _ = os.ReadFile(\"../docs/readme.md\")\n",
		"c/golden/out.txt": "x",
	})
	if s := sel("c/golden/out.txt"); s.Scope != TierPackage || !slices.Equal(s.TestDirs, []string{"./c"}) {
		t.Errorf("txt thư mục con: %+v", s)
	}
	if s := sel("docs/readme.md"); s.Scope != TierPackage || !slices.Equal(s.TestDirs, []string{"./c"}) {
		t.Errorf("md được test nhắc: %+v", s)
	}
	if s := sel("docs/other.md"); s.Scope != TierNone {
		t.Errorf("md: %+v", s)
	}
}

func TestGoSourceNeverNarrows(t *testing.T) {
	ctx := context.Background()
	root := sampleModule(t)
	run := func(root string, changed ...string) Selection {
		return Select(ctx, Input{Root: root, Changed: changed})
	}
	wantFull := func(name string, s Selection, reason string) {
		t.Helper()
		if s.Scope != TierFull {
			t.Errorf("%s: scope = %s, muốn full (%+v)", name, s.Scope, s)
			return
		}
		for _, w := range s.Widening {
			if w.Reason == reason {
				return
			}
		}
		t.Errorf("%s: thiếu lý do %s: %+v", name, reason, s.Widening)
	}
	wantFull("module lồng", run(root, "nested/n.go"), ReasonNestedModule)
	wantFull("thư mục có .go không ra gói", run(root, "nodir/only_gen.go"), ReasonUnknownPackage)
	if err := os.RemoveAll(filepath.Join(root, "c")); err != nil {
		t.Fatal(err)
	}
	wantFull("gói bị xoá", run(root, "c/c.go", "c/c_test.go"), ReasonDeletedPackage)

	// không có go.mod → không có nguồn hỗ trợ
	empty := t.TempDir()
	writeFiles(t, empty, map[string]string{"x/x.go": "package x\n"})
	wantFull("không go.mod", run(empty, "x/x.go"), ReasonUnsupported)

	// go không có trên PATH
	t.Setenv("PATH", t.TempDir())
	wantFull("không go", run(sampleModule(t), "a/a.go"), ReasonSourceUnavailable)
}

// Thư mục cha của module có .git rỗng (vd sandbox gắn /tmp/.git): go list không được đòi dấu VCS, nếu không gói main
// làm nó lỗi 'error obtaining VCS status' và Select nới full.
func TestGoSourceIgnoresBrokenParentVCS(t *testing.T) {
	parent := t.TempDir()
	if err := os.Mkdir(filepath.Join(parent, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "m")
	writeFiles(t, root, map[string]string{
		"go.mod":             "module example.com/m\n\ngo 1.21\n",
		"cmd/m/main.go":      "package main\n\nfunc main() {}\n",
		"cmd/m/main_test.go": "package main\n\nimport \"testing\"\n\nfunc TestM(t *testing.T) {}\n",
	})
	s := Select(context.Background(), Input{Root: root, Changed: []string{"cmd/m/main.go"}})
	if s.Scope != TierGraph || s.Source != "go" {
		t.Fatalf("go list phải chạy được dù cha có .git rỗng: %+v", s)
	}
	if !slices.Equal(s.TestDirs, []string{"./cmd/m"}) {
		t.Errorf("test_dirs = %v", s.TestDirs)
	}
}

func TestGoSourceListFailureWidens(t *testing.T) {
	root := sampleModule(t)
	// go.mod hỏng: go list lỗi.
	writeFiles(t, root, map[string]string{"go.mod": "module\n!!!\n"})
	s := Select(context.Background(), Input{Root: root, Changed: []string{"a/a.go"}})
	if s.Scope != TierFull || s.Widening[0].Reason != ReasonSourceUnavailable {
		t.Errorf("go list hỏng: %+v", s)
	}
	// gói nhập thiếu gói: cũng nới.
	root = sampleModule(t)
	writeFiles(t, root, map[string]string{"a/a.go": "package a\n\nimport _ \"example.com/m/missing\"\n"})
	s = Select(context.Background(), Input{Root: root, Changed: []string{"a/a.go"}})
	if s.Scope != TierFull {
		t.Errorf("gói lỗi: %+v", s)
	}
}

// Trên repo Bint: tài liệu mà test đọc qua đường dẫn tương đối phải kéo theo test của gói đó.
func TestBintDocsReferencedByTests(t *testing.T) {
	if testing.Short() {
		t.Skip("go list toàn repo")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for doc, want := range map[string]string{
		"docs/usage.md":     "./internal/cli",
		"docs/workflows.md": "./internal/tui/actions",
		"README.md":         "./internal/tui/actions",
	} {
		s := Select(context.Background(), Input{Root: root, Changed: []string{doc}})
		if s.Scope == TierNone || !slices.Contains(s.TestDirs, want) {
			t.Errorf("%s: scope=%s test_dirs thiếu %s (%v)", doc, s.Scope, want, s.TestDirs)
		}
	}
}
