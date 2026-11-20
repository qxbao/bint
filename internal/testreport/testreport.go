// Package testreport đọc log của một lần chạy test thành kết quả có cấu trúc: test nào đỏ, test nào đang chạy khi hết giờ, gói nào lỗi build,
// mỗi gói mất bao lâu. Luồng (internal/workflow) chỉ dùng Report, không biết định dạng của runner: mỗi runner là một Parser. Bản cài hiện có:
// `go test` (văn bản thường và -json). Không nhận ra định dạng thì Report.Measured=false và người gọi lùi về mã thoát.
package testreport

import (
	"slices"
	"strings"
	"time"
)

// Report là kết quả đọc một log test.
type Report struct {
	// Measured: log được một Parser nhận ra; false thì các trường còn lại không có nghĩa.
	Measured bool `json:"measured"`
	// Format là tên Parser đã đọc log ("gotest", "gotest-json").
	Format string `json:"format,omitempty"`
	// Failed: test đỏ ở mức lá (không có test con đỏ nào bên dưới), dạng "<gói> <tên>", sắp xếp.
	Failed []Test `json:"failed,omitempty"`
	// TimedOut: test đang chạy khi runner hết giờ (Go: `panic: test timed out`), không tính vào Failed.
	TimedOut []Test `json:"timed_out,omitempty"`
	// BuildFailed: gói không build được hoặc không dựng được test (không biết test nào đỏ).
	BuildFailed []string `json:"build_failed,omitempty"`
	// Panicked: gói chết vì panic không phải hết giờ (không biết hết test nào đỏ).
	Panicked []string `json:"panicked,omitempty"`
	// Packages: kết quả và thời lượng từng gói (đơn vị chạy của runner).
	Packages []Package `json:"packages,omitempty"`
}

// Test là một test (lá) trong một gói. Package rỗng khi runner không cho biết.
type Test struct {
	Package string `json:"package,omitempty"`
	Name    string `json:"name"`
}

func (t Test) String() string {
	if t.Package == "" {
		return t.Name
	}
	return t.Package + " " + t.Name
}

// Top là tên test cấp đầu (TestX/sub → TestX).
func (t Test) Top() string {
	n, _, _ := strings.Cut(t.Name, "/")
	return n
}

// Package là kết quả một gói.
type Package struct {
	Name     string        `json:"name"`
	Passed   bool          `json:"passed"`
	Cached   bool          `json:"cached,omitempty"`
	Duration time.Duration `json:"duration_ns,omitempty"`
}

// Parser đọc log của một runner.
type Parser interface {
	Name() string
	// Detect: log có dạng của runner này.
	Detect(text string) bool
	Parse(text string) Report
}

// Parsers là các Parser hỗ trợ, theo thứ tự thử khi tự nhận dạng.
func Parsers() []Parser { return []Parser{GoJSON{}, GoText{}} }

// ByName trả Parser có tên name; nil nếu không có.
func ByName(name string) Parser {
	for _, p := range Parsers() {
		if p.Name() == name {
			return p
		}
	}
	return nil
}

// Parse tự nhận dạng định dạng rồi đọc; không Parser nào nhận ra thì trả Report{Measured: false}.
func Parse(text string) Report {
	for _, p := range Parsers() {
		if p.Detect(text) {
			return p.Parse(text)
		}
	}
	return Report{}
}

// OnlyTimeouts: lần chạy đỏ chỉ vì hết giờ (có test hết giờ, không test nào đỏ thật, không lỗi build hay panic khác).
func (r Report) OnlyTimeouts() bool {
	return r.Measured && len(r.TimedOut) > 0 && len(r.Failed) == 0 && len(r.BuildFailed) == 0 && len(r.Panicked) == 0
}

// Complete: biết đủ test nào đỏ (không có gói lỗi build, panic hay hết giờ che mất phần còn lại).
func (r Report) Complete() bool {
	return r.Measured && len(r.TimedOut) == 0 && len(r.BuildFailed) == 0 && len(r.Panicked) == 0
}

// FailedIn trả các test đỏ không thuộc known (so theo gói và tên đầy đủ; known có Package rỗng thì so theo tên).
func (r Report) FailedIn(known []Test) (fresh []Test) {
	for _, t := range r.Failed {
		if !containsTest(known, t) {
			fresh = append(fresh, t)
		}
	}
	return fresh
}

func containsTest(list []Test, t Test) bool {
	return slices.ContainsFunc(list, func(k Test) bool {
		return k.Name == t.Name && (k.Package == "" || t.Package == "" || k.Package == t.Package)
	})
}

// Slow trả các gói chạy lâu hơn over (cho chọn test mức hàm ở gói chậm và cảnh báo trần), sắp theo thời lượng giảm dần.
func (r Report) Slow(over time.Duration) []Package {
	var out []Package
	for _, p := range r.Packages {
		if !p.Cached && p.Duration > over {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b Package) int { return int(b.Duration - a.Duration) })
	return out
}

func sortTests(ts []Test) {
	slices.SortFunc(ts, func(a, b Test) int {
		if c := strings.Compare(a.Package, b.Package); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
}

// leaves bỏ các tên có test con trong cùng danh sách (TestX đỏ vì TestX/a đỏ thì chỉ giữ TestX/a).
func leaves(ts []Test) []Test {
	var out []Test
	for _, t := range ts {
		leaf := !slices.ContainsFunc(ts, func(o Test) bool { return o.Package == t.Package && strings.HasPrefix(o.Name, t.Name+"/") })
		if leaf && !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	return out
}
