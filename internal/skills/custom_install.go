package skills

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"

	"bint/internal/fsafe"
)

// installCustom xuất bản skill tuỳ biến (.bint/skills/<tên>/) cho mọi target: SKILL.md (theo định dạng của
// target) và từng tệp phụ nguyên văn, dùng chung cơ chế an toàn của skill gốc (tệp đã xuất bản bị sửa tay → xung
// đột, không ghi đè; --force ghi đè kèm sao lưu). Băm theo từng tệp nằm trong LockEntry.Files.
//
// Skill không qua lint (CheckCustom) → xung đột: không xuất bản bản mới, GIỮ bản đã xuất bản và mục core.lock cũ
// (như lớp phủ hỏng) để một lần sửa dở không làm mất skill đang dùng. Tệp phụ bị xoá khỏi nguồn, hoặc cả skill
// bị xoá, thì gỡ bản đã xuất bản nếu chưa bị sửa tay.
func installCustom(opts InstallOptions, lock *Lock) ([]Outcome, error) {
	list, err := LoadCustom(opts.Root)
	if err != nil {
		return nil, err
	}
	if lock.Custom == nil {
		lock.Custom = map[string]LockEntry{}
	}
	var outs []Outcome
	live := map[string]bool{}
	for _, cs := range list {
		live[cs.Name] = true
		if errs, _ := CheckCustom(opts.Root, cs); len(errs) > 0 {
			outs = append(outs, Outcome{Skill: cs.Name, Path: CustomPath(cs.Name, "SKILL.md"), Result: fsafe.Conflict,
				Detail: "skill tuỳ biến không qua `bint skill lint`, giữ bản đã xuất bản: " + strings.Join(errs, "; ")})
			continue
		}
		prev := lock.Custom[cs.Name]
		s := cs.AsSkill()
		entry := LockEntry{SHA256: s.Hash(), Files: map[string]string{}}
		handled := map[string]bool{}
		conflict := "skill tuỳ biến đã bị sửa ngoài Bint: sửa nguồn trong " + CustomPath(cs.Name) + "/ rồi `bint sync`; --force để ghi đè (bản cũ được sao lưu)"
		write := func(rel string, content []byte, perm os.FileMode) error {
			handled[rel] = true
			o, err := publishFile(opts, cs.Name, rel, content, perm, prev.Files[rel], conflict)
			if err != nil {
				return err
			}
			outs = append(outs, o)
			switch {
			case o.Result != fsafe.Conflict:
				entry.Published = append(entry.Published, rel)
				entry.Files[rel] = Skill{Content: content}.Hash()
			case prev.Files[rel] != "":
				entry.Published = append(entry.Published, rel)
				entry.Files[rel] = prev.Files[rel]
			}
			return nil
		}
		for _, t := range opts.Targets {
			if err := write(path.Join(t.Dir, cs.Name, "SKILL.md"), ForTarget(s, t).Content, 0o644); err != nil {
				return outs, err
			}
			for _, f := range cs.Files {
				if err := write(path.Join(t.Dir, cs.Name, f.Rel), f.Content, f.Mode); err != nil {
					return outs, err
				}
			}
		}
		for _, rel := range prev.Published {
			if handled[rel] {
				continue
			}
			if !underTargets(opts.Targets, cs.Name, rel) {
				// harness đã tắt: không đụng tới bản đã xuất bản ở thư mục đó
				entry.Published = append(entry.Published, rel)
				entry.Files[rel] = prev.Files[rel]
				continue
			}
			o, err := removeCustom(opts, cs.Name, rel, prev)
			if err != nil {
				return outs, err
			}
			outs = append(outs, o)
			if o.Result == fsafe.Conflict {
				entry.Published = append(entry.Published, rel)
				entry.Files[rel] = prev.Files[rel]
			}
		}
		if len(entry.Files) == 0 {
			entry.Files = nil
		}
		lock.Custom[cs.Name] = entry
	}
	for name, entry := range lock.Custom {
		if live[name] {
			continue
		}
		keep := entry
		keep.Published, keep.Files = nil, map[string]string{}
		for _, rel := range entry.Published {
			if claimedByOther(lock, "custom", name, rel) {
				// skill tiến hoá (hoặc flow) cùng tên vừa ghi đè tệp này ở lần Install này: gỡ sẽ xoá nhầm bản mới
				continue
			}
			o, err := removeCustom(opts, name, rel, entry)
			if err != nil {
				return outs, err
			}
			outs = append(outs, o)
			if o.Result == fsafe.Conflict {
				keep.Published = append(keep.Published, rel)
				keep.Files[rel] = entry.Files[rel]
			}
		}
		if len(keep.Published) == 0 {
			delete(lock.Custom, name)
		} else {
			lock.Custom[name] = keep
		}
	}
	return outs, nil
}

// underTargets cho biết rel nằm dưới thư mục skill <target>/<name>/ của một target đang bật.
func underTargets(targets []Target, name, rel string) bool {
	for _, t := range targets {
		if strings.HasPrefix(rel, path.Join(t.Dir, name)+"/") {
			return true
		}
	}
	return false
}

// removeCustom gỡ một tệp đã xuất bản của skill tuỳ biến (nếu chưa bị sửa tay) rồi dọn thư mục rỗng.
func removeCustom(opts InstallOptions, name, rel string, entry LockEntry) (Outcome, error) {
	o, err := unpublishFile(opts, name, rel, []string{entry.Files[rel]},
		"tệp không còn trong nguồn skill tuỳ biến nhưng bản đã xuất bản bị sửa tay: không xoá; xoá tay hoặc --force",
		"gỡ: không còn trong nguồn skill tuỳ biến")
	if err != nil || o.Result != fsafe.Removed {
		return o, err
	}
	// dọn thư mục rỗng từ thư mục chứa tệp lên tới thư mục skill (gồm cả nó)
	stop := filepath.Dir(filepath.Join(opts.Root, filepath.FromSlash(rel)))
	for _, t := range opts.Targets {
		if p := path.Join(t.Dir, name); strings.HasPrefix(rel, p+"/") {
			stop = filepath.Join(opts.Root, filepath.FromSlash(p))
		}
	}
	for dir := filepath.Dir(filepath.Join(opts.Root, filepath.FromSlash(rel))); ; dir = filepath.Dir(dir) {
		// unpublishFile đã thử xoá thư mục chứa tệp: không còn thì đi tiếp lên cha
		if err := os.Remove(dir); (err != nil && !errors.Is(err, os.ErrNotExist)) || dir == stop {
			break
		}
	}
	return o, nil
}
