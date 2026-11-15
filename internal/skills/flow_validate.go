package skills

import (
	"fmt"
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

// FlowKnowledgeWarnings cảnh báo bước dùng skill tuỳ biến mà thân skill không dặn agent tra tri thức dự án
// (context_for_task/context_for_paths hoặc `bint context`). Gói <bint-context> mà Runner chèn đầu bước chỉ khớp mục
// tiêu và phạm vi của phiếu; skill gốc đều có lời dặn này. Skill không nạp được đã có lỗi riêng nên bỏ qua.
func FlowKnowledgeWarnings(root string, d flow.Def) []string {
	var out []string
	for _, s := range d.Steps {
		if _, core := Get(s.Skill); core || s.Skill == "" {
			continue
		}
		cs, err := LoadCustomOne(root, s.Skill)
		if err != nil || cs.Content == nil {
			continue
		}
		doc, err := ParseDoc(cs.Content)
		if err != nil || MentionsKnowledge(doc.Body) {
			continue
		}
		out = append(out, fmt.Sprintf("bước %q: skill tuỳ biến %q không dặn agent tra quy ước dự án (context_for_task, "+
			"context_for_paths hoặc `bint context`); gói tri thức đầu bước chỉ khớp mục tiêu và phạm vi của phiếu. "+
			"Thêm mục \"Quy ước dự án\" như mẫu của `bint skill new`", s.ID, s.Skill))
	}
	return out
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
