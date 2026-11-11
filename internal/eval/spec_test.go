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
	commit(t, root, "feat: báo cấu hình song song", map[string]string{
		"pkg/contracts.py": "def old():\n    return 1\n\n\nMAX_FANOUT_DEFAULTS = frozenset((\"builder\", \"qa\"))\n\n\ndef max_fanout_for(\n    project: Path,\n    strict: bool = False,\n) -> dict[str, int]:\n    return {\"builder\": 3}\n",
		"pkg/conductor.py": "class Conductor:\n    def run(self):\n        return 0\n\n    def announce_parallel(self, cfg) -> str: return \"Cấu hình song song: builder 3\"\n",
		"lib/calc.go":      "package lib\n\nfunc Add(a, b int) int { return a + b }\n\nfunc MulAll(xs ...int) (int, error) {\n\tp := 1\n\treturn p, nil\n}\n",
		"tests/test_parallel.py": "from pkg.contracts import max_fanout_for, MAX_FANOUT_DEFAULTS\n" +
			"def test_it(c):\n    assert max_fanout_for(p)[\"builder\"] == 3\n    assert c.announce_parallel(x) == \"Cấu hình song song: builder 3\"\n" +
			"    assert MAX_FANOUT_DEFAULTS\n    assert cfg.get(\"parallel_key_x\")\n    assert cfg.get(\"ghost_name_y\")\n",
		"lib/calc_test.go": "package lib\n\nfunc TestMul(t *testing.T) { MulAll(2, 3) }\n",
	})
	// parallel_key_x là khoá dict do lời giải đưa vào (vào khối giao diện dạng khoá dữ liệu);
	// ghost_name_y chỉ có trong chú thích nên vẫn là khoảng trống đặc tả.
	writeFiles(t, root, map[string]string{"pkg/keys.py": "KEYS = {\"parallel_key_x\": 1}\n# ghost_name_y chỉ nằm trong chú thích\n"})
	solution := commit(t, root, "thêm khoá", nil)
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
	if !strings.Contains(kinds["interface_gap"], "ghost_name_y") || strings.Contains(kinds["interface_gap"], "parallel_key_x") {
		t.Errorf("interface_gap: %+v", c.Spec.Findings)
	}
	if !strings.Contains(c.Prompt, "Data key: parallel_key_x") {
		t.Errorf("thiếu khoá dữ liệu trong khối giao diện:\n%s", c.Prompt)
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

func TestDeriveSpecHiddenContracts(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("cần git")
	}
	root := testutil.NewRepo(t, map[string]string{
		"pkg/flow.py":   "def run(cmd):\n    return cmd\n",
		"pkg/spec.yml":  "name: demo\n",
		"pkg/cli.py":    "def main(args):\n    return 0\n",
		"pkg/report.py": "def fmt(row):\n    return row\n",
	})
	base := testutil.Git(t, root, "rev-parse", "HEAD")
	solution := commit(t, root, "feat: hợp đồng ẩn", map[string]string{
		"pkg/flow.py":   "def run(cmd):\n    return cmd\n\n\ndef ten_moi(x):\n    return x\n",
		"pkg/spec.yml":  "name: demo\nqa_skills:\n  - a\nreviewer-skills: []\n",
		"pkg/cli.py":    "def main(args):\n    p.add_argument(\"--from\")\n    p.add_argument(\"--out-dir\")\n    return 0\n",
		"pkg/report.py": "def fmt(row):\n    row[\"__v\"] = 1\n    return \"THIẾU\" if not row else \"x/y: +1/−1\"\n",
		"tests/test_hidden.py": "import pkg.flow as flow\n" +
			"def test_a(monkeypatch, row, out, args, cmd):\n" +
			"    monkeypatch.setattr(flow, \"ten_moi\", lambda x: 1)\n" +
			"    assert \"THIẾU\" in row\n" +
			"    assert \"x/y: +1/−1\" in out\n" +
			"    assert \"--from elicited\" in cmd\n" +
			"    assert \"--out-dir\" in args\n" +
			"    assert \"qa_skills\" in open(\"pkg/spec.yml\").read()\n" +
			"    assert \"reviewer-skills\" in open(\"pkg/spec.yml\").read()\n" +
			"    assert row[\"__v\"] == 1\n" +
			"    assert \"pkg/spec.yml\" and \"demo\" and \"ab12cd34\" and \"run\"\n",
	})
	c := &Case{
		Source:      Source{Commit: solution, Base: base},
		SourceFiles: []string{"pkg/flow.py", "pkg/spec.yml", "pkg/cli.py", "pkg/report.py"},
		TestFiles:   []string{"tests/test_hidden.py"},
		Prompt:      "chạy luồng và báo cáo",
	}
	ctx := context.Background()
	in, err := BuildLeakInput(ctx, root, c)
	if err != nil {
		t.Fatal(err)
	}
	ApplySpec(ctx, root, c, in)

	for _, want := range []string{
		"CLI flag: --from", "CLI flag: --out-dir",
		"Config key (pkg/spec.yml): qa_skills", "Config key (pkg/spec.yml): reviewer-skills",
		"Data key: __v", "pkg/flow.py: `def ten_moi(x)`",
	} {
		if !strings.Contains(c.Prompt, want) {
			t.Errorf("thiếu %q trong đề bài:\n%s", want, c.Prompt)
		}
	}
	for _, leak := range []string{"elicited", "frozen", "return x", "THIẾU", "+1/−1"} {
		if strings.Contains(c.Prompt, leak) {
			t.Errorf("đề bài lộ %q:\n%s", leak, c.Prompt)
		}
	}
	kinds := map[string]string{}
	for _, f := range c.Spec.Findings {
		kinds[f.Kind] = f.Detail
	}
	if !strings.Contains(kinds["exact_text"], "THIẾU") && !strings.Contains(kinds["exact_text"], "+1/−1") {
		t.Errorf("exact_text: %+v", c.Spec.Findings)
	}
	if strings.Contains(kinds["exact_text"], "pkg/spec.yml") || strings.Contains(kinds["exact_text"], "ab12cd34") {
		t.Errorf("exact_text bắt nhầm đường dẫn/hex: %+v", c.Spec.Findings)
	}
	if _, ok := kinds["interface_gap"]; ok {
		t.Errorf("không còn khoảng trống: %+v", c.Spec.Findings)
	}
	if r := CheckLeak(c.Prompt, in); r.Status == "fail" {
		t.Fatalf("khối giao diện bị coi là rò rỉ: %+v", r)
	}
	again := *c
	ApplySpec(ctx, root, &again, in)
	if again.Prompt != c.Prompt {
		t.Fatalf("áp lại không lũy đẳng:\n%s\n---\n%s", c.Prompt, again.Prompt)
	}
}

func TestSpecHelpers(t *testing.T) {
	for s, want := range map[string]bool{
		"THIẾU": true, "x/y: +1/−1": true, "CHƯA xong": true, "ab": false, "plain": false, "Ok": false,
	} {
		if distinctive(s) != want {
			t.Errorf("distinctive(%q) != %v", s, want)
		}
	}
	for _, s := range []string{"tests/test_a.py", "spec.yml", "ab12cd34", "ident_only", "AC-2", `{"a": 1}`, "run --fast"} {
		if !notSpecific(s) {
			t.Errorf("notSpecific(%q) phải true", s)
		}
	}
	if notSpecific("THIẾU") || notSpecific("a: +1") {
		t.Error("chuỗi đặc thù bị bỏ nhầm")
	}
	if flagName("--from elicited") != "--from" || flagName("-x") != "-x" || flagName("-1") != "" || flagName("a --b") != "" {
		t.Error("flagName sai")
	}
	got := patchTargets([]string{
		`monkeypatch.setattr(_flow, "review_enabled", lambda r: True)`,
		`monkeypatch.setattr("pkg.mod.ten_a", 1)`, `mock.patch("pkg.mod.ten_b")`, `patch.object(svc, "ten_c")`,
	})
	names := map[string]string{}
	for _, p := range got {
		names[p.Name] = p.Mod
	}
	if len(names) != 4 || names["review_enabled"] != "flow" || names["ten_a"] != "mod" || names["ten_b"] != "mod" || names["ten_c"] != "svc" {
		t.Errorf("patchTargets: %+v", got)
	}
	// Tên đã có nơi khác nhưng lời giải import vào đúng mô-đun bị patch: thuộc tính mô-đun.
	src := []fileLines{{"pkg/flow.py", []string{"from ..kernel import review_enabled"}}, {"pkg/other.py", []string{"from x import review_enabled"}}}
	if h, ok := importedAttr(patchTarget{"review_enabled", "flow"}, src); !ok || h.Path != "pkg/flow.py" {
		t.Errorf("importedAttr: %+v %v", h, ok)
	}
	if _, ok := importedAttr(patchTarget{"review_enabled", "operator"}, src); ok {
		t.Error("mô-đun khác không được tính")
	}
}
