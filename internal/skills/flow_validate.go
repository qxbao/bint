package skills

import (
	"os"
	"slices"

	"bint/internal/flow"
)

// UsableInFlow cho biết skill có dùng được cho một bước của flow không: skill gốc, hoặc skill tuỳ biến qua kiểm
// hình thức không lỗi (skill lỗi lint không được xuất bản nên flow không được dựa vào). Skill tiến hoá không tính.
// Dùng chung cho `bint flow lint`, Runner và `bint sync` để lint, chạy và xuất bản cùng một luật.
func UsableInFlow(root string) func(string) bool {
	return func(name string) bool {
		if _, ok := Get(name); ok {
			return true
		}
		cs, err := LoadCustomOne(root, name)
		if err != nil {
			return false
		}
		errs, _ := CheckCustom(root, cs)
		return len(errs) == 0
	}
}

// ValidateFlow kiểm một flow tuỳ biến (file là tên tệp không đuôi) bằng flow.Validate với UsableInFlow, thêm luật về
// tên: flow được xuất bản thành skill cùng tên nên tên không được trùng MỌI thư mục skill tuỳ biến (kể cả skill đang
// lỗi lint), thư mục skill tiến hoá hay tên dành riêng của .bint/skills. Một luật cho cả ba nơi dùng.
func ValidateFlow(root string, d flow.Def, file string) []string {
	exists := UsableInFlow(root)
	errs := flow.Validate(d, file, exists)
	if file == "" || d.Name == "" || d.IsBuiltin() || !flow.ValidName(d.Name) || exists(d.Name) {
		// tên sai cú pháp đã có lỗi riêng; exists(tên) đã bị flow.Validate báo "trùng tên một skill"
		return errs
	}
	if slices.Contains(ReservedNames, d.Name) {
		return append(errs, "name: \""+d.Name+"\" là tên dành riêng của .bint/skills")
	}
	if _, err := os.Stat(EvolvedDir(root, d.Name)); err == nil || CustomExists(root, d.Name) {
		errs = append(errs, "name: \""+d.Name+"\" trùng tên một skill tuỳ biến hoặc skill tiến hoá")
	}
	return errs
}
