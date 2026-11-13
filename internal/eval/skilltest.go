package eval

// Kiểm thử skill tuỳ biến (GĐ6 của docs/custom-flow-plan.md): kịch bản áp lực viết trong
// .bint/skills/<tên>/tests/*.yml, chạy ở hai nhánh (không skill / có skill trong đề bài), chấm bằng chuỗi hoặc
// biểu thức chính quy trên câu trả lời cuối của agent. Phần này thuần (đọc tệp, dựng đề bài, chấm, tổng hợp);
// việc chạy agent và ghi kết quả ở app.SkillTest.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Nhánh của kiểm thử skill.
const (
	SkillArmWithout = "without"
	SkillArmWith    = "with"
)

// Giới hạn số lượt mỗi kịch bản mỗi nhánh.
const (
	SkillTestDefaultReps = 5
	SkillTestMaxReps     = 20
	// maxScenarioBytes chặn tệp kịch bản lớn bất thường (chép nhầm).
	maxScenarioBytes = 256 << 10
)

// ExpectRule là một điều kiện trên câu trả lời cuối: đúng một khoá được đặt.
type ExpectRule struct {
	Contains    *string `yaml:"contains,omitempty"`
	NotContains *string `yaml:"not_contains,omitempty"`
	Regex       *string `yaml:"regex,omitempty"`
	NotRegex    *string `yaml:"not_regex,omitempty"`
}

// keys trả về các khoá đã đặt (theo thứ tự cố định).
func (r ExpectRule) keys() []string {
	var out []string
	for _, k := range []struct {
		name string
		v    *string
	}{{"contains", r.Contains}, {"not_contains", r.NotContains}, {"regex", r.Regex}, {"not_regex", r.NotRegex}} {
		if k.v != nil {
			out = append(out, k.name)
		}
	}
	return out
}

// kind và value: khoá và giá trị của quy tắc đã hợp lệ (đúng một khoá).
func (r ExpectRule) kindValue() (kind, value string) {
	switch {
	case r.Contains != nil:
		return "contains", *r.Contains
	case r.NotContains != nil:
		return "not_contains", *r.NotContains
	case r.Regex != nil:
		return "regex", *r.Regex
	case r.NotRegex != nil:
		return "not_regex", *r.NotRegex
	}
	return "", ""
}

// Label là mô tả ngắn của quy tắc, dùng khi báo mục trượt.
func (r ExpectRule) Label() string {
	k, v := r.kindValue()
	return fmt.Sprintf("%s %q", k, v)
}

func (r ExpectRule) validate() error {
	ks := r.keys()
	switch len(ks) {
	case 0:
		return errors.New("mục expect không có khoá nào (cần đúng một trong contains, not_contains, regex, not_regex)")
	case 1:
	default:
		return fmt.Errorf("mục expect có %d khoá (%s): chỉ được đúng một", len(ks), strings.Join(ks, ", "))
	}
	k, v := r.kindValue()
	if v == "" {
		return fmt.Errorf("expect.%s rỗng", k)
	}
	if k == "regex" || k == "not_regex" {
		if _, err := regexp.Compile(v); err != nil {
			return fmt.Errorf("expect.%s: biểu thức chính quy sai: %v", k, err)
		}
	}
	return nil
}

// matches cho biết quy tắc đúng với text.
func (r ExpectRule) matches(text string) bool {
	k, v := r.kindValue()
	switch k {
	case "contains":
		return strings.Contains(text, v)
	case "not_contains":
		return !strings.Contains(text, v)
	case "regex", "not_regex":
		re, err := regexp.Compile(v)
		if err != nil {
			return false
		}
		return re.MatchString(text) == (k == "regex")
	}
	return false
}

// SkillScenario là một kịch bản áp lực (một tệp tests/*.yml).
type SkillScenario struct {
	// Name là tên tệp bỏ đuôi .yml; File là tên tệp (để báo lỗi).
	Name string `yaml:"-" json:"name"`
	File string `yaml:"-" json:"file"`
	// Prompt là tình huống giao cho agent; Pressures nối vào cuối đề bài theo thứ tự.
	Prompt    string       `yaml:"prompt" json:"prompt"`
	Pressures []string     `yaml:"pressures,omitempty" json:"pressures,omitempty"`
	Expect    []ExpectRule `yaml:"expect" json:"-"`
	// Reps là số lượt mỗi nhánh; 0 trong tệp = mặc định SkillTestDefaultReps (sau khi nạp luôn ≥ 1).
	Reps int `yaml:"-" json:"reps"`
}

// scenarioFile là dạng tệp YAML (Reps con trỏ để phân biệt "không đặt" với 0).
type scenarioFile struct {
	Prompt    string       `yaml:"prompt"`
	Pressures []string     `yaml:"pressures"`
	Expect    []ExpectRule `yaml:"expect"`
	Reps      *int         `yaml:"reps"`
}

// ParseSkillScenario đọc và kiểm một tệp kịch bản. file chỉ để đặt tên và báo lỗi.
func ParseSkillScenario(file string, data []byte) (SkillScenario, error) {
	sc := SkillScenario{File: file, Name: strings.TrimSuffix(file, filepath.Ext(file))}
	var f scenarioFile
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		if errors.Is(err, io.EOF) {
			return sc, errors.New("tệp rỗng")
		}
		return sc, fmt.Errorf("YAML sai: %v", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err == nil {
		return sc, errors.New("chỉ được một tài liệu YAML mỗi tệp")
	}
	var problems []string
	add := func(format string, a ...any) { problems = append(problems, fmt.Sprintf(format, a...)) }
	if strings.TrimSpace(f.Prompt) == "" {
		add("thiếu prompt (tình huống giao cho agent)")
	}
	for i, p := range f.Pressures {
		if strings.TrimSpace(p) == "" {
			add("pressures[%d] rỗng", i+1)
		}
	}
	if len(f.Expect) == 0 {
		add("thiếu expect (cần ít nhất một mục)")
	}
	for i, r := range f.Expect {
		if err := r.validate(); err != nil {
			add("expect[%d]: %v", i+1, err)
		}
	}
	reps := SkillTestDefaultReps
	if f.Reps != nil {
		reps = *f.Reps
		if reps < 1 || reps > SkillTestMaxReps {
			add("reps %d ngoài khoảng 1–%d", reps, SkillTestMaxReps)
		}
	}
	if len(problems) > 0 {
		return sc, errors.New(strings.Join(problems, "; "))
	}
	sc.Prompt, sc.Pressures, sc.Expect, sc.Reps = f.Prompt, f.Pressures, f.Expect, reps
	return sc, nil
}

// LoadSkillScenarios đọc mọi tests/*.yml trong testsDir (sắp theo tên tệp). Lỗi mọi tệp được gom, mỗi lỗi kèm tên tệp;
// không có tệp .yml nào cũng là lỗi.
func LoadSkillScenarios(testsDir string) ([]SkillScenario, error) {
	entries, err := os.ReadDir(testsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("không có thư mục kịch bản %s (đặt kịch bản áp lực vào tests/*.yml)", testsDir)
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".yml") && !strings.HasPrefix(e.Name(), ".") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, fmt.Errorf("không có kịch bản nào trong %s (cần tests/*.yml)", testsDir)
	}
	var out []SkillScenario
	var errs []error
	for _, n := range names {
		p := filepath.Join(testsDir, n)
		fi, err := os.Lstat(p)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("%s: %v", n, err))
			continue
		case !fi.Mode().IsRegular():
			errs = append(errs, fmt.Errorf("%s: không phải tệp thường (symlink hoặc loại khác)", n))
			continue
		case fi.Size() > maxScenarioBytes:
			errs = append(errs, fmt.Errorf("%s: quá lớn (%d byte, tối đa %d)", n, fi.Size(), maxScenarioBytes))
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %v", n, err))
			continue
		}
		sc, err := ParseSkillScenario(n, b)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %v", n, err))
			continue
		}
		out = append(out, sc)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return out, nil
}

// SkillTestPrompt dựng đề bài của một lượt. skillBody rỗng = nhánh không skill (chỉ tình huống và áp lực); khác rỗng =
// nhánh có skill: nội dung SKILL.md đặt trước tình huống, kèm lời dặn tuân theo.
func SkillTestPrompt(sc SkillScenario, skillName, skillBody string) string {
	var b strings.Builder
	if skillBody != "" {
		fmt.Fprintf(&b, "Bạn đang làm việc với skill %q. Nội dung skill nằm giữa hai dòng phân cách; hãy tuân theo skill này khi xử lý "+
			"tình huống bên dưới. Tệp phụ của skill (nếu skill nhắc tới) nằm trong thư mục làm việc hiện tại theo đường dẫn tương đối.\n\n", skillName)
		b.WriteString("----- SKILL.md -----\n")
		b.WriteString(strings.TrimRight(skillBody, "\n"))
		b.WriteString("\n----- hết SKILL.md -----\n\n")
		b.WriteString("Tình huống:\n\n")
	}
	b.WriteString(strings.TrimRight(sc.Prompt, "\n"))
	for _, p := range sc.Pressures {
		b.WriteString("\n\n")
		b.WriteString(strings.TrimRight(p, "\n"))
	}
	b.WriteString("\n")
	return b.String()
}

// ScoreSkillAnswer chấm câu trả lời: tuân thủ khi mọi quy tắc đúng; failed là nhãn của các quy tắc trượt.
func ScoreSkillAnswer(text string, rules []ExpectRule) (ok bool, failed []string) {
	for _, r := range rules {
		if !r.matches(text) {
			failed = append(failed, r.Label())
		}
	}
	return len(failed) == 0, failed
}

// Trạng thái của một lần kiểm thử skill.
const (
	SkillTestCompleted = "completed"
	SkillTestCancelled = "cancelled"
	SkillTestQuota     = "quota"
)

// SkillTestRun là kết quả một lượt.
type SkillTestRun struct {
	Arm      string   `json:"arm"`
	Scenario string   `json:"scenario"`
	Rep      int      `json:"rep"` // từ 1
	Complied bool     `json:"complied"`
	Failed   []string `json:"failed,omitempty"` // mục expect trượt
	CostUSD  *float64 `json:"cost_usd,omitempty"`
	// Error khác rỗng: lượt hỏng hạ tầng/hết giờ/không có câu trả lời; không tính vào mẫu số.
	Error string `json:"error,omitempty"`
	// Log là đường dẫn thư mục của lượt, tương đối với thư mục chứa tệp JSON.
	Log string `json:"log,omitempty"`
}

// Graded cho biết lượt được chấm (có câu trả lời, không lỗi).
func (r SkillTestRun) Graded() bool { return r.Error == "" }

// SkillArmStats là số liệu một nhánh (của một kịch bản hoặc tổng).
type SkillArmStats struct {
	Runs     int `json:"runs"`
	Complied int `json:"complied"`
	Graded   int `json:"graded"`
	Errors   int `json:"errors"`
	// Rate = Complied/Graded; nil khi chưa có lượt nào được chấm (không báo 0 giả).
	Rate *float64 `json:"rate"`
}

func (s *SkillArmStats) add(r SkillTestRun) {
	s.Runs++
	if r.Graded() {
		s.Graded++
		if r.Complied {
			s.Complied++
		}
	} else {
		s.Errors++
	}
	s.Rate = nil
	if s.Graded > 0 {
		v := float64(s.Complied) / float64(s.Graded)
		s.Rate = &v
	}
}

// SkillScenarioStats là số liệu của một kịch bản ở một nhánh.
type SkillScenarioStats struct {
	Scenario string `json:"scenario"`
	Arm      string `json:"arm"`
	SkillArmStats
}

// SkillTestReport là kết quả một lần `bint skill test` (tệp JSON).
type SkillTestReport struct {
	ID         string   `json:"id"`
	Skill      string   `json:"skill"`
	SkillHash  string   `json:"skill_sha256"`
	Harness    string   `json:"harness"`
	Model      string   `json:"model,omitempty"`
	Arms       []string `json:"arms"`
	Status     string   `json:"status"`
	StartedAt  string   `json:"started_at"`
	FinishedAt string   `json:"finished_at"`
	// CostUSD là tổng chi phí harness báo (nil = không báo).
	CostUSD   *float64                 `json:"cost_usd,omitempty"`
	Totals    map[string]SkillArmStats `json:"totals"`
	Scenarios []SkillScenarioStats     `json:"scenarios"`
	Runs      []SkillTestRun           `json:"runs"`
}

// Tally tính lại Totals, Scenarios và CostUSD từ Runs.
func (r *SkillTestReport) Tally() {
	r.Totals = map[string]SkillArmStats{}
	r.Scenarios = []SkillScenarioStats{}
	r.CostUSD = nil
	idx := map[[2]string]int{}
	for _, run := range r.Runs {
		t := r.Totals[run.Arm]
		t.add(run)
		r.Totals[run.Arm] = t
		k := [2]string{run.Scenario, run.Arm}
		i, ok := idx[k]
		if !ok {
			i = len(r.Scenarios)
			idx[k] = i
			r.Scenarios = append(r.Scenarios, SkillScenarioStats{Scenario: run.Scenario, Arm: run.Arm})
		}
		r.Scenarios[i].add(run)
		if run.CostUSD != nil {
			c := *run.CostUSD
			if r.CostUSD != nil {
				c += *r.CostUSD
			}
			r.CostUSD = &c
		}
	}
}
