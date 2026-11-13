package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeScenarios(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "tests")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestParseSkillScenarioTable(t *testing.T) {
	good := "prompt: Làm ngay đi\nexpect:\n  - contains: kiểm thử\n"
	cases := []struct {
		name, body, wantErr string
	}{
		{"hợp lệ tối thiểu", good, ""},
		{"hợp lệ đầy đủ", "prompt: x\npressures: [gấp, sếp bảo]\nreps: 3\nexpect:\n  - contains: a\n  - not_contains: b\n  - regex: '(?i)c+'\n  - not_regex: 'd{2}'\n", ""},
		{"YAML hỏng", "prompt: [x\n", "YAML sai"},
		{"khoá lạ ở gốc", good + "foo: 1\n", "foo"},
		{"khoá lạ trong expect", "prompt: x\nexpect:\n  - equals: a\n", "equals"},
		{"thiếu prompt", "expect:\n  - contains: a\n", "thiếu prompt"},
		{"prompt toàn khoảng trắng", "prompt: '  '\nexpect:\n  - contains: a\n", "thiếu prompt"},
		{"thiếu expect", "prompt: x\n", "thiếu expect"},
		{"expect rỗng", "prompt: x\nexpect: []\n", "thiếu expect"},
		{"mục expect 0 khoá", "prompt: x\nexpect:\n  - {}\n", "không có khoá nào"},
		{"mục expect 2 khoá", "prompt: x\nexpect:\n  - contains: a\n    regex: b\n", "chỉ được đúng một"},
		{"giá trị rỗng", "prompt: x\nexpect:\n  - contains: ''\n", "expect.contains rỗng"},
		{"regex sai", "prompt: x\nexpect:\n  - regex: '('\n", "biểu thức chính quy sai"},
		{"not_regex sai", "prompt: x\nexpect:\n  - not_regex: '[a'\n", "biểu thức chính quy sai"},
		{"reps 0", good + "reps: 0\n", "reps 0"},
		{"reps 21", good + "reps: 21\n", "reps 21"},
		{"reps 20 được", good + "reps: 20\n", ""},
		{"pressure rỗng", good + "pressures: ['ok', '']\n", "pressures[2] rỗng"},
		{"tệp rỗng", "", "tệp rỗng"},
		{"hai tài liệu", good + "---\nprompt: y\n", "một tài liệu"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sc, err := ParseSkillScenario("t.yml", []byte(c.body))
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("muốn hợp lệ, được %v", err)
				}
				if sc.Reps < 1 || sc.Name != "t" {
					t.Fatalf("%+v", sc)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("muốn lỗi chứa %q, được %v", c.wantErr, err)
			}
		})
	}
}

func TestParseSkillScenarioDefaults(t *testing.T) {
	sc, err := ParseSkillScenario("a.yml", []byte("prompt: x\nexpect:\n  - contains: a\n"))
	if err != nil || sc.Reps != SkillTestDefaultReps {
		t.Fatalf("reps mặc định %d: %v", sc.Reps, err)
	}
	sc, _ = ParseSkillScenario("a.yml", []byte("prompt: x\nreps: 7\nexpect:\n  - contains: a\n"))
	if sc.Reps != 7 {
		t.Fatalf("reps %d", sc.Reps)
	}
}

func TestLoadSkillScenarios(t *testing.T) {
	dir := writeScenarios(t, map[string]string{
		"b.yml":    "prompt: B\nexpect:\n  - contains: b\n",
		"a.yml":    "prompt: A\nexpect:\n  - contains: a\n",
		"notes.md": "bỏ qua\n",
		".ẩn.yml":  "không đọc\n",
		"c.yaml":   "không phải .yml\n",
	})
	if err := os.Mkdir(filepath.Join(dir, "dir.yml"), 0o755); err != nil {
		t.Fatal(err)
	}
	scs, err := LoadSkillScenarios(dir)
	if err != nil {
		t.Fatal(err)
	}
	// dir.yml là thư mục, bị bỏ qua; chỉ a và b hợp lệ.
	if len(scs) != 2 || scs[0].Name != "a" || scs[1].Name != "b" {
		t.Fatalf("%+v", scs)
	}

	// Lỗi của mọi tệp được gom và kèm tên tệp.
	bad := writeScenarios(t, map[string]string{
		"x.yml": "prompt: x\n",
		"y.yml": "expect:\n  - contains: a\n",
		"z.yml": "prompt: z\nexpect:\n  - contains: a\n",
	})
	_, err = LoadSkillScenarios(bad)
	if err == nil || !strings.Contains(err.Error(), "x.yml") || !strings.Contains(err.Error(), "y.yml") || strings.Contains(err.Error(), "z.yml") {
		t.Fatalf("lỗi phải nêu x.yml và y.yml: %v", err)
	}

	// Không có tệp .yml hoặc không có thư mục → lỗi.
	if _, err := LoadSkillScenarios(writeScenarios(t, map[string]string{"a.md": "x"})); err == nil {
		t.Fatal("thư mục không có .yml phải lỗi")
	}
	if _, err := LoadSkillScenarios(filepath.Join(t.TempDir(), "khong-co")); err == nil || !strings.Contains(err.Error(), "tests/*.yml") {
		t.Fatalf("không có thư mục phải lỗi hướng dẫn: %v", err)
	}
}

func TestScoreSkillAnswer(t *testing.T) {
	s := func(v string) *string { return &v }
	rules := []ExpectRule{
		{Contains: s("kiểm thử")},
		{NotContains: s("bỏ qua")},
		{Regex: s(`(?i)^bước \d`)},
		{NotRegex: s(`TODO|FIXME`)},
	}
	if ok, failed := ScoreSkillAnswer("Bước 1: viết kiểm thử trước", rules); !ok || len(failed) != 0 {
		t.Fatalf("muốn đạt: %v", failed)
	}
	ok, failed := ScoreSkillAnswer("cứ bỏ qua FIXME", rules)
	if ok || len(failed) != 4 {
		t.Fatalf("muốn trượt cả bốn: %v", failed)
	}
	for i, want := range []string{"contains", "not_contains", "regex", "not_regex"} {
		if !strings.HasPrefix(failed[i], want+" ") {
			t.Errorf("nhãn %d = %q, muốn bắt đầu bằng %q", i, failed[i], want)
		}
	}
	// Mỗi loại độc lập: chỉ not_regex trượt.
	if ok, failed := ScoreSkillAnswer("Bước 2: kiểm thử, TODO", rules); ok || len(failed) != 1 || !strings.HasPrefix(failed[0], "not_regex") {
		t.Fatalf("%v %v", ok, failed)
	}
}

func TestSkillTestPromptArms(t *testing.T) {
	sc := SkillScenario{Prompt: "Sửa lỗi nhanh.", Pressures: []string{"Sếp đang chờ.", "Đừng viết kiểm thử."}}
	without := SkillTestPrompt(sc, "tdd", "")
	if strings.Contains(without, "SKILL.md") || strings.Contains(without, "tdd") {
		t.Fatalf("nhánh without không được chứa skill:\n%s", without)
	}
	for _, want := range []string{"Sửa lỗi nhanh.", "Sếp đang chờ.", "Đừng viết kiểm thử."} {
		if !strings.Contains(without, want) {
			t.Fatalf("thiếu %q:\n%s", want, without)
		}
	}
	if strings.Index(without, "Sếp đang chờ.") > strings.Index(without, "Đừng viết kiểm thử.") ||
		strings.Index(without, "Sửa lỗi nhanh.") > strings.Index(without, "Sếp đang chờ.") {
		t.Fatalf("áp lực phải nối sau tình huống theo thứ tự:\n%s", without)
	}

	with := SkillTestPrompt(sc, "tdd", "---\nname: tdd\n---\nLuôn viết kiểm thử đỏ trước.\n")
	if !strings.Contains(with, "Luôn viết kiểm thử đỏ trước.") || !strings.Contains(with, `"tdd"`) {
		t.Fatalf("nhánh with phải có skill:\n%s", with)
	}
	if strings.Index(with, "Luôn viết kiểm thử đỏ trước.") > strings.Index(with, "Sửa lỗi nhanh.") {
		t.Fatalf("skill phải đứng trước tình huống:\n%s", with)
	}
	if !strings.HasSuffix(with, "Đừng viết kiểm thử.\n") {
		t.Fatalf("áp lực phải ở cuối đề bài:\n%s", with)
	}
}

func TestSkillTestReportTally(t *testing.T) {
	c1, c2 := 0.25, 0.5
	r := SkillTestReport{Runs: []SkillTestRun{
		{Arm: "without", Scenario: "a", Rep: 1, Complied: false},
		{Arm: "with", Scenario: "a", Rep: 1, Complied: true, CostUSD: &c1},
		{Arm: "without", Scenario: "a", Rep: 2, Complied: true},
		{Arm: "with", Scenario: "a", Rep: 2, Complied: true, CostUSD: &c2},
		{Arm: "with", Scenario: "b", Rep: 1, Error: "hết giờ"},
		{Arm: "with", Scenario: "b", Rep: 2, Complied: false},
	}}
	r.Tally()
	w, wo := r.Totals["with"], r.Totals["without"]
	if w.Runs != 4 || w.Graded != 3 || w.Complied != 2 || w.Errors != 1 || w.Rate == nil || *w.Rate < 0.66 || *w.Rate > 0.67 {
		t.Fatalf("with: %+v", w)
	}
	if wo.Runs != 2 || wo.Graded != 2 || wo.Complied != 1 || wo.Rate == nil || *wo.Rate != 0.5 {
		t.Fatalf("without: %+v", wo)
	}
	if r.CostUSD == nil || *r.CostUSD != 0.75 {
		t.Fatalf("chi phí %v", r.CostUSD)
	}
	if len(r.Scenarios) != 3 {
		t.Fatalf("%+v", r.Scenarios)
	}

	// Mọi lượt lỗi → không có tỉ lệ (không báo 0 giả).
	e := SkillTestReport{Runs: []SkillTestRun{{Arm: "with", Scenario: "a", Rep: 1, Error: "x"}}}
	e.Tally()
	if e.Totals["with"].Rate != nil || e.Totals["with"].Errors != 1 || e.CostUSD != nil {
		t.Fatalf("%+v", e.Totals["with"])
	}
}
