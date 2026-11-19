package task

import (
	"strings"
	"testing"
)

func TestHasMeasureTable(t *testing.T) {
	for _, c := range []struct {
		line string
		want bool
	}{
		{"có 12 tệp, 3400 byte", true},
		{"`TestGoal2` đạt", false},
		{"sửa `internal/v2/x.go`", false},
		{"`internal/v2/x.go` có 40 tệp", true},
		{"không số nào", false},
	} {
		if got := hasMeasure(c.line); got != c.want {
			t.Errorf("hasMeasure(%q) = %v, muốn %v", c.line, got, c.want)
		}
	}
}

func baselineReq(hientrang, ac string) string {
	return "# Yêu cầu\n## Hiện trạng\n" + hientrang + "\n## Tiêu chí chấp nhận\n- [ ] AC-1: " + ac + "\n"
}

func TestLintBaselineTable(t *testing.T) {
	for _, c := range []struct {
		name, req, ac        string
		wantBaseline, wantAC bool
		exempt               bool
	}{
		{"đo thật", baselineReq("- `a.go`: 12 tệp", "giảm còn 5 tệp"), "giảm còn 5 tệp", false, false, false},
		{"chỉ tên có chữ số trong backtick", baselineReq("- sửa `TestGoal2` và `internal/v2/x.go`", "x"), "giảm còn 5 tệp", true, false, false},
		{"AC chỉ có định danh", baselineReq("- 12 tệp", "x"), "`TestGoal2` đạt", false, true, false},
		{"miễn trừ", baselineReq("Không đo được: kho rỗng", "x"), "giảm còn 5 tệp", false, false, true},
		{"miễn trừ in đậm có chữ số", baselineReq("- **Không đo được**: lý do 2", "x"), "giảm còn 5 tệp", false, false, true},
		{"miễn trừ nhưng có số đo thật", baselineReq("- 7 tệp\n- Không đo được: byte", "x"), "giảm còn 5 tệp", false, false, false},
		{"thiếu mục", "# Yêu cầu\n## Tiêu chí chấp nhận\n- [ ] AC-1: 5 tệp\n", "5 tệp", true, false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := &reportBuilder{}
			exempt := lintBaseline(b, c.req, []acItem{{N: 1, Text: c.ac}})
			if hasCode(b.r.Errors, "baseline_missing") != c.wantBaseline || hasCode(b.r.Errors, "ac_without_measure") != c.wantAC || exempt != c.exempt {
				t.Errorf("errors=%s exempt=%v", codes(b.r.Errors), exempt)
			}
		})
	}
}

func TestLintPlanningBaselineExemptWarning(t *testing.T) {
	for _, c := range []struct {
		name    string
		version int
		hient   string
		want    bool
	}{
		{"v1 miễn trừ", PlanningContractV1, "Không đo được: kho rỗng", true},
		{"v2 miễn trừ", PlanningContractV2, "Không đo được: kho rỗng", true},
		{"v1 có số đo", PlanningContractV1, "12 tệp", false},
		{"v2 có số đo", PlanningContractV2, "12 tệp", false},
		{"v0 bỏ qua", 0, "Không đo được: kho rỗng", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := goodDocs()
			d["requirement"] = strings.Replace(goodReq, "## Tiêu chí chấp nhận", "## Hiện trạng\n"+c.hient+"\n## Tiêu chí chấp nhận", 1)
			r := lintPlanning(goodTicket(), d, nil, true, c.version)
			if got := hasCode(r.Warnings, "baseline_exempt"); got != c.want {
				t.Errorf("baseline_exempt=%v muốn %v (warnings %s)", got, c.want, codes(r.Warnings))
			}
			if hasCode(r.Errors, "baseline_exempt") {
				t.Error("baseline_exempt không được là lỗi")
			}
		})
	}
}

func TestValidReproBackticks(t *testing.T) {
	root := t.TempDir()
	for _, c := range []struct {
		repro string
		want  bool
	}{
		{"chạy `go test ./internal/task -run X`", true},
		{"chạy `make`", true},
		{"chạy `bint task review`", true},
		{"`foo.go` gọi sai", false},
		{"hàm `blockFinalize` bỏ qua lỗi", false},
		{"`internal/task/repro.go` và `blockFinalize`", false},
		{"lập luận thuần", false},
		{"`sh -c 'exit 1'` ra 1", true},
	} {
		if got := ValidRepro(c.repro, root); got != c.want {
			t.Errorf("ValidRepro(%q) = %v, muốn %v", c.repro, got, c.want)
		}
	}
}
