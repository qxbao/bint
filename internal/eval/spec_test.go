package eval

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"bint/internal/testutil"
)

func TestDeriveSpecPythonAndGo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("cần git")
	}
	root := testutil.NewRepo(t, map[string]string{
		"pkg/contracts.py": "def old():\n    return 1\n",
		"pkg/conductor.py": "class Conductor:\n    def run(self):\n        return 0\n",
		"lib/calc.go":      "package lib\n\nfunc Add(a, b int) int { return a + b }\n",
	})
	solution := commit(t, root, "feat: báo cấu hình song song", map[string]string{
		"pkg/contracts.py": "def old():\n    return 1\n\n\nMAX_FANOUT_DEFAULTS = frozenset((\"builder\", \"qa\"))\n\n\ndef max_fanout_for(\n    project: Path,\n    strict: bool = False,\n) -> dict[str, int]:\n    return {\"builder\": 3}\n",
		"pkg/conductor.py": "class Conductor:\n    def run(self):\n        return 0\n\n    def announce_parallel(self, cfg) -> str: return \"Cấu hình song song: builder 3\"\n",
		"lib/calc.go":      "package lib\n\nfunc Add(a, b int) int { return a + b }\n\nfunc MulAll(xs ...int) (int, error) {\n\tp := 1\n\treturn p, nil\n}\n",
		"tests/test_parallel.py": "from pkg.contracts import max_fanout_for, MAX_FANOUT_DEFAULTS\n" +
			"def test_it(c):\n    assert max_fanout_for(p)[\"builder\"] == 3\n    assert c.announce_parallel(x) == \"Cấu hình song song: builder 3\"\n" +
			"    assert MAX_FANOUT_DEFAULTS\n    assert cfg.get(\"parallel_key_x\")\n",
		"lib/calc_test.go": "package lib\n\nfunc TestMul(t *testing.T) { MulAll(2, 3) }\n",
	})
	// parallel_key_x là khoá chuỗi do lời giải đưa vào, không có khai báo để trích chữ ký.
	writeFiles(t, root, map[string]string{"pkg/keys.py": "KEYS = {\"parallel_key_x\": 1}\n"})
	solution = commit(t, root, "thêm khoá", nil)
	base := testutil.Git(t, root, "rev-parse", "HEAD~2")
	c := &Case{
		Source:      Source{Commit: solution, Base: base},
		SourceFiles: []string{"pkg/contracts.py", "pkg/conductor.py", "lib/calc.go", "pkg/keys.py"},
		TestFiles:   []string{"tests/test_parallel.py", "lib/calc_test.go"},
		Prompt:      "runmethod: đầu run báo cấu hình song song",
	}
	ctx := context.Background()
	in, err := BuildLeakInput(ctx, root, c)
	if err != nil {
		t.Fatal(err)
	}
	ApplySpec(ctx, root, c, in)

	want := map[string]string{
		"max_fanout_for":      "def max_fanout_for( project: Path, strict: bool = False, ) -> dict[str, int]",
		"MAX_FANOUT_DEFAULTS": "MAX_FANOUT_DEFAULTS",
		"announce_parallel":   "def announce_parallel(self, cfg) -> str",
		"MulAll":              "func MulAll(xs ...int) (int, error)",
	}
	got := map[string]InterfaceHint{}
	for _, h := range c.Spec.Interface {
		got[h.Name] = h
	}
	for name, sig := range want {
		if got[name].Signature != sig {
			t.Errorf("%s: chữ ký %q, muốn %q", name, got[name].Signature, sig)
		}
	}
	if got["announce_parallel"].Owner != "Conductor" {
		t.Errorf("thiếu lớp chứa: %+v", got["announce_parallel"])
	}
	for _, leak := range []string{"frozenset", "return", "p := 1", "builder\": 3"} {
		if strings.Contains(c.Prompt, leak) {
			t.Errorf("đề bài lộ thân/giá trị %q:\n%s", leak, c.Prompt)
		}
	}
	if !strings.Contains(c.Prompt, InterfaceHeader) || !strings.HasPrefix(c.Prompt, "runmethod: đầu run báo cấu hình song song\n\n") {
		t.Fatalf("đề bài:\n%s", c.Prompt)
	}
	if c.Spec.Status != "warn" {
		t.Fatalf("muốn cảnh báo thiếu đặc tả: %+v", c.Spec)
	}
	kinds := map[string]string{}
	for _, f := range c.Spec.Findings {
		kinds[f.Kind] = f.Detail
	}
	if !strings.Contains(kinds["interface_gap"], "parallel_key_x") {
		t.Errorf("interface_gap: %+v", c.Spec.Findings)
	}
	if !strings.Contains(kinds["exact_text"], "Cấu hình song song: builder 3") {
		t.Errorf("exact_text: %+v", c.Spec.Findings)
	}
	if _, ok := kinds["short_prompt"]; !ok {
		t.Errorf("short_prompt: %+v", c.Spec.Findings)
	}

	// Khối giao diện không bị coi là lộ lời giải; khối lặp lại không bị nhân đôi.
	if r := CheckLeak(c.Prompt, in); r.Status == "fail" {
		t.Fatalf("khối giao diện bị coi là rò rỉ: %+v", r)
	}
	again := *c
	ApplySpec(ctx, root, &again, in)
	if strings.Count(again.Prompt, InterfaceHeader) != 1 || again.Prompt != c.Prompt {
		t.Fatalf("áp lại không ổn định:\n%s", again.Prompt)
	}
	// Mã lời giải chép vào phần đề bài vẫn bị bắt.
	leaky := "Viết: return {\"builder\": 3}  # trả về trần\n" + c.Prompt[len(StripInterface(c.Prompt)):]
	if r := CheckLeak(leaky, in); r.Status != "fail" || !hasKind(r, "solution_code") {
		t.Fatalf("không bắt được rò rỉ ngoài khối giao diện: %+v", r)
	}
}

func TestSignatureEdgeCases(t *testing.T) {
	cases := []struct{ path, src, name, want string }{
		{"a.ts", "export const sum = (a: number, b: number): number => a + b\n", "sum", "export const sum = (a: number, b: number): number"},
		{"a.ts", "export const LIMIT = computeLimit(3)\n", "LIMIT", "export const LIMIT"},
		{"a.ts", "export function parse<T>(s: string): T {\n  return JSON.parse(s)\n}\n", "parse", "export function parse<T>(s: string): T"},
		{"a.php", "    public static function money(?int $v): string\n    {\n", "money", "public static function money(?int $v): string"},
		{"a.py", "LIMIT: int = 3\n", "LIMIT", "LIMIT: int"},
		{"a.py", "class Gate(Base):\n    pass\n", "Gate", "class Gate(Base)"},
		{"a.go", "type Gate struct {\n\tN int\n}\n", "Gate", "type Gate struct"},
		{"a.go", "var ErrX = errors.New(\"x\")\n", "ErrX", "var ErrX"},
	}
	for _, tc := range cases {
		lines := strings.Split(tc.src, "\n")
		if !declares(tc.path, lines[0], tc.name) {
			t.Errorf("%s %s: không nhận ra khai báo %q", tc.path, tc.name, lines[0])
			continue
		}
		if got := signature(tc.path, lines, 0); got != tc.want {
			t.Errorf("%s %s: %q, muốn %q", tc.path, tc.name, got, tc.want)
		}
	}
	if declares("a.py", "    x = max_fanout_for(p)", "max_fanout_for") {
		t.Error("lời gọi bị nhận nhầm là khai báo")
	}
}
