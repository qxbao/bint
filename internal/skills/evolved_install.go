package skills

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"bint/internal/fsafe"
)

// installEvolved xuất bản skill tiến hoá còn hiệu lực cho các harness và gỡ bản đã
// xuất bản của skill không còn hiệu lực (bị loại bỏ, xoá, hoặc lỗi thời so với mã).
// Quy tắc an toàn giống skill gốc: tệp đã bị sửa tay không bị ghi đè hay xoá.
func installEvolved(opts InstallOptions, lock *Lock) ([]Outcome, error) {
	var outs []Outcome
	list, problems, err := LoadEvolved(opts.Root)
	if err != nil {
		return nil, err
	}
	for _, p := range problems {
		outs = append(outs, Outcome{Skill: strings.SplitN(p, ":", 2)[0], Path: EvolvedRel, Result: fsafe.Conflict, Detail: "skill tiến hoá hỏng: " + p})
	}
	if lock.Evolved == nil {
		lock.Evolved = map[string]LockEntry{}
	}
	live := map[string]bool{}
	for _, ev := range list {
		if !ev.Active() {
			continue
		}
		if stale, why := ev.Stale(opts.Files); stale {
			outs = append(outs, Outcome{Skill: ev.Meta.Name, Path: EvolvedRelPath(ev.Meta.Name), Result: fsafe.Unchanged,
				Detail: "lỗi thời, không xuất bản: " + why})
			continue
		}
		if probs := CheckDraft(ev.Content, ev.Meta.Triggers); len(probs) > 0 {
			outs = append(outs, Outcome{Skill: ev.Meta.Name, Path: EvolvedRelPath(ev.Meta.Name, "SKILL.md"), Result: fsafe.Conflict,
				Detail: "không qua kiểm tra: " + strings.Join(probs, "; ")})
			continue
		}
		live[ev.Meta.Name] = true
		prev := lock.Evolved[ev.Meta.Name]
		s := ev.AsSkill()
		entry := LockEntry{SHA256: s.Hash()}
		for _, t := range opts.Targets {
			rel := filepath.ToSlash(filepath.Join(t.Dir, s.Name, "SKILL.md"))
			ts := ForTarget(s, t)
			prevHash := prev.SHA256
			if t.Format != "" {
				prevHash = prev.Variants[t.Format]
				if entry.Variants == nil {
					entry.Variants = map[string]string{}
				}
				entry.Variants[t.Format] = ts.Hash()
			}
			o, err := publish(opts, ts, rel, prevHash)
			if err != nil {
				return outs, err
			}
			outs = append(outs, o)
			if o.Result != fsafe.Conflict {
				entry.Published = append(entry.Published, rel)
			}
		}
		lock.Evolved[s.Name] = entry
	}
	for name, entry := range lock.Evolved {
		if live[name] {
			continue
		}
		keep := entry
		keep.Published = nil
		for _, rel := range entry.Published {
			o, err := unpublish(opts, name, rel, entry)
			if err != nil {
				return outs, err
			}
			outs = append(outs, o)
			if o.Result == fsafe.Conflict {
				keep.Published = append(keep.Published, rel)
			}
		}
		if len(keep.Published) == 0 {
			delete(lock.Evolved, name)
		} else {
			lock.Evolved[name] = keep
		}
	}
	return outs, nil
}

// unpublish gỡ một bản đã xuất bản nếu chưa bị sửa tay.
func unpublish(opts InstallOptions, name, rel string, entry LockEntry) (Outcome, error) {
	o := Outcome{Skill: name, Path: rel}
	full := filepath.Join(opts.Root, filepath.FromSlash(rel))
	have, err := os.ReadFile(full)
	if errors.Is(err, os.ErrNotExist) {
		o.Result = fsafe.Unchanged
		return o, nil
	}
	if err != nil {
		return o, err
	}
	cur := Skill{Content: have}.Hash()
	prev, known := opts.Manifest.Get(rel + "#skill")
	pristine := cur == entry.SHA256 || (known && prev == fsafe.Hash(string(have)))
	for _, v := range entry.Variants {
		pristine = pristine || cur == v
	}
	if !pristine && !opts.Force {
		o.Result = fsafe.Conflict
		o.Detail = "skill không còn hiệu lực nhưng bản đã xuất bản bị sửa tay: không xoá; xoá tay hoặc --force"
		return o, nil
	}
	if _, err := fsafe.Backup(full, opts.BackupDir, rel, opts.Now); err != nil {
		return o, err
	}
	if err := os.Remove(full); err != nil {
		return o, err
	}
	os.Remove(filepath.Dir(full)) // chỉ xoá được khi thư mục rỗng
	opts.Manifest.Delete(rel + "#skill")
	o.Result = fsafe.Removed
	o.Detail = "gỡ: skill không còn hiệu lực"
	return o, nil
}
