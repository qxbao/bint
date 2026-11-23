package skills

import (
	"fmt"
	"path"
	"sort"

	"bint/internal/fsafe"
)

// publishCoreFiles xuất bản tệp phụ của skill gốc s cho target t, cùng cơ chế an toàn của SKILL.md: tệp đã xuất bản
// bị sửa tay (băm khác bản Bint ghi trong core.lock/manifest) → xung đột, không ghi đè trừ khi Force. Băm từng tệp
// ghi vào entry.Files; handled đánh dấu đường dẫn đã xử lý để removeStaleCoreFiles không gỡ nhầm.
func publishCoreFiles(opts InstallOptions, t Target, s Skill, prev LockEntry, entry *LockEntry, handled map[string]bool) ([]Outcome, error) {
	var outs []Outcome
	conflict := "tệp phụ của skill gốc đã bị sửa ngoài Bint (tuỳ biến đặt trong " + OverlayPath(s.Name) +
		" hoặc skill tuỳ biến riêng); --force để ghi đè (bản cũ được sao lưu)"
	for _, f := range s.Files {
		rel := path.Join(t.Dir, s.Name, f.Rel)
		handled[rel] = true
		o, err := publishFile(opts, s.Name, rel, f.Content, f.Mode, prev.Files[rel], conflict)
		if err != nil {
			return outs, err
		}
		outs = append(outs, o)
		if entry.Files == nil {
			entry.Files = map[string]string{}
		}
		switch {
		case o.Result != fsafe.Conflict:
			entry.Published = append(entry.Published, rel)
			entry.Files[rel] = Skill{Content: f.Content}.Hash()
		case prev.Files[rel] != "":
			entry.Published = append(entry.Published, rel)
			entry.Files[rel] = prev.Files[rel]
		}
	}
	return outs, nil
}

// removeStaleCoreFiles gỡ tệp phụ đã xuất bản ở lần trước (prev.Files) mà bản skill gốc mới không còn, nếu chưa bị
// sửa tay. Tệp dưới thư mục của harness đã tắt được giữ nguyên (không đụng tới), như skill tuỳ biến.
func removeStaleCoreFiles(opts InstallOptions, name string, prev LockEntry, entry *LockEntry, handled map[string]bool) ([]Outcome, error) {
	rels := make([]string, 0, len(prev.Files))
	for rel := range prev.Files {
		if !handled[rel] {
			rels = append(rels, rel)
		}
	}
	sort.Strings(rels)
	var outs []Outcome
	keep := func(rel string) {
		if entry.Files == nil {
			entry.Files = map[string]string{}
		}
		entry.Published = append(entry.Published, rel)
		entry.Files[rel] = prev.Files[rel]
	}
	for _, rel := range rels {
		if !underTargets(opts.Targets, name, rel) {
			keep(rel)
			continue
		}
		o, err := removeSkillFile(opts, name, rel, prev.Files[rel],
			"tệp phụ không còn trong skill gốc nhưng bản đã xuất bản bị sửa tay: không xoá; xoá tay hoặc --force",
			"gỡ: không còn trong skill gốc")
		if err != nil {
			return outs, err
		}
		outs = append(outs, o)
		if o.Result == fsafe.Conflict {
			keep(rel)
		}
	}
	return outs, nil
}

// foldAuxOutcomes gộp kết quả tệp phụ bình thường (tạo mới, không đổi, cập nhật không cần sao lưu) vào dòng SKILL.md của cùng
// skill/target để báo cáo init/sync không liệt kê hàng chục tệp phụ; xung đột và ghi đè có sao lưu vẫn là dòng riêng. SKILL.md
// không đổi mà tệp phụ được ghi thì dòng đó thành "cập nhật".
func foldAuxOutcomes(skill Outcome, aux []Outcome) (Outcome, []Outcome) {
	var keep []Outcome
	written := 0
	for _, o := range aux {
		switch {
		case o.Result == fsafe.Unchanged:
		case (o.Result == fsafe.Created || o.Result == fsafe.Updated) && o.Backup == "":
			written++
		default:
			keep = append(keep, o)
		}
	}
	if written > 0 {
		if skill.Result == fsafe.Unchanged {
			skill.Result = fsafe.Updated
		}
		note := fmt.Sprintf("+%d tệp phụ", written)
		if skill.Detail != "" {
			note = skill.Detail + "; " + note
		}
		skill.Detail = note
	}
	return skill, keep
}
