package skills

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"bint/internal/flow"
	"bint/internal/fsafe"
)

// installFlows xuất bản skill điều phối của mỗi flow tuỳ biến hợp lệ (.bint/flows/<tên>.yml → .claude/skills/<tên>/ và
// .agents/skills/<tên>/) và gỡ bản đã xuất bản của flow đã bị xoá (GĐ5 custom-flow-plan). Cùng quy tắc an toàn với skill
// gốc: tệp đã xuất bản bị sửa tay → xung đột, không ghi đè (--force ghi đè kèm sao lưu), không xoá.
//
// Flow không qua kiểm (tệp hỏng hoặc skills.ValidateFlow có lỗi) → xung đột: không xuất bản bản mới, GIỮ bản đã xuất bản
// và mục core.lock cũ, để một lần sửa dở không làm mất skill điều phối đang dùng. Ngoại lệ: lỗi do tên nay trùng một
// skill khác (gốc, tuỳ biến, tiến hoá): chỉ những tệp nguồn kia thật sự đã nhận (claimedByOther) bị bỏ khỏi mục flows mà
// không gỡ tệp; tệp chưa ai nhận (skill chiếm tên chưa xuất bản: lỗi lint, tiến hoá không active) vẫn thuộc mục flows để
// lần sync sau gỡ được khi flow bị xoá, không thành tệp mồ côi.
func installFlows(opts InstallOptions, lock *Lock) ([]Outcome, error) {
	files, err := flow.Load(opts.Root)
	if err != nil {
		return nil, err
	}
	if lock.Flows == nil {
		lock.Flows = map[string]LockEntry{}
	}
	var outs []Outcome
	live := map[string]bool{}
	for _, f := range files {
		name := f.Name
		var errs []string
		switch {
		case f.Err != nil:
			errs = []string{f.Err.Error()}
		default:
			errs = ValidateFlow(opts.Root, f.Def, name)
		}
		if len(errs) > 0 {
			if nameTaken(opts.Root, name) {
				releaseClaimed(lock, name)
				live[name] = true // còn tệp nguồn: mục flows (nếu còn) chỉ bị gỡ khi flow bị xoá
				outs = append(outs, Outcome{Skill: name, Path: f.Path, Result: fsafe.Conflict,
					Detail: "tên flow trùng một skill khác, không xuất bản skill điều phối: " + strings.Join(errs, "; ")})
				continue
			}
			live[name] = true
			outs = append(outs, Outcome{Skill: name, Path: f.Path, Result: fsafe.Conflict,
				Detail: "flow không qua `bint flow lint`, giữ bản đã xuất bản: " + strings.Join(errs, "; ")})
			continue
		}
		live[name] = true
		o, err := publishFlow(opts, lock, f)
		outs = append(outs, o...)
		if err != nil {
			return outs, err
		}
	}
	for name, entry := range lock.Flows {
		if live[name] {
			continue
		}
		keep := entry
		keep.Published = nil
		for _, rel := range entry.Published {
			if claimedByOther(lock, "flows", name, rel) {
				// vừa được skill tuỳ biến/tiến hoá cùng tên xuất bản ở lần Install này: không gỡ nhầm
				continue
			}
			o, err := unpublishFile(opts, name, rel, entryHashes(entry),
				"flow đã bị xoá nhưng skill điều phối đã xuất bản bị sửa tay: không xoá; xoá tay hoặc --force",
				"gỡ: flow "+name+" không còn")
			if err != nil {
				return outs, err
			}
			outs = append(outs, o)
			if o.Result == fsafe.Conflict {
				keep.Published = append(keep.Published, rel)
			}
		}
		if len(keep.Published) == 0 {
			delete(lock.Flows, name)
		} else {
			lock.Flows[name] = keep
		}
	}
	return outs, nil
}

// releaseClaimed bỏ khỏi mục flows của name những tệp đã được nguồn khác (skill tuỳ biến/tiến hoá cùng tên) xuất bản ở lần
// Install này; tệp chưa ai nhận được giữ lại (mục bị xoá khi không còn tệp nào).
func releaseClaimed(lock *Lock, name string) {
	entry, ok := lock.Flows[name]
	if !ok {
		return
	}
	var keep []string
	for _, rel := range entry.Published {
		if !claimedByOther(lock, "flows", name, rel) {
			keep = append(keep, rel)
		}
	}
	if len(keep) == 0 {
		delete(lock.Flows, name)
		return
	}
	entry.Published = keep
	lock.Flows[name] = entry
}

// publishFlow xuất bản skill điều phối của một flow đã qua kiểm cho mọi target và cập nhật lock.Flows[tên].
func publishFlow(opts InstallOptions, lock *Lock, f flow.File) ([]Outcome, error) {
	name := f.Name
	s := FlowSkill(f.Def)
	prev := lock.Flows[name]
	entry := LockEntry{SHA256: prev.SHA256, Variants: cloneMap(prev.Variants)}
	published := map[string]bool{}
	for _, rel := range prev.Published {
		published[rel] = true
	}
	conflict := "skill điều phối của flow đã bị sửa ngoài Bint: sửa nguồn trong " + f.Path + " rồi `bint sync`; --force để ghi đè (bản cũ được sao lưu)"
	var outs []Outcome
	for _, t := range opts.Targets {
		rel := filepath.ToSlash(filepath.Join(t.Dir, name, "SKILL.md"))
		ts := ForTarget(s, t)
		prevHash := prev.SHA256
		if t.Format != "" {
			prevHash = prev.Variants[t.Format]
		}
		o, err := publishFile(opts, name, rel, ts.Content, 0o644, prevHash, conflict)
		if err != nil {
			return outs, err
		}
		outs = append(outs, o)
		if o.Result == fsafe.Conflict {
			continue // giữ băm và mục Published cũ của rel này (nếu có)
		}
		published[rel] = true
		if t.Format == "" {
			entry.SHA256 = ts.Hash()
		} else {
			if entry.Variants == nil {
				entry.Variants = map[string]string{}
			}
			entry.Variants[t.Format] = ts.Hash()
		}
	}
	for rel := range published {
		entry.Published = append(entry.Published, rel)
	}
	sort.Strings(entry.Published)
	if len(entry.Published) == 0 {
		delete(lock.Flows, name)
	} else {
		lock.Flows[name] = entry
	}
	return outs, nil
}

func cloneMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// entryHashes là mọi băm nội dung đã xuất bản của một mục (định dạng gốc và các biến thể).
func entryHashes(e LockEntry) []string {
	hs := []string{e.SHA256}
	for _, v := range e.Variants {
		hs = append(hs, v)
	}
	return hs
}

// nameTaken cho biết tên đã thuộc một skill khác: skill gốc, thư mục skill tuỳ biến (kể cả đang lỗi lint), thư mục
// skill tiến hoá, hoặc tên dành riêng của .bint/skills. Flow mang tên đó không được xuất bản.
func nameTaken(root, name string) bool {
	if _, ok := Get(name); ok || slices.Contains(ReservedNames, name) || CustomExists(root, name) {
		return true
	}
	_, err := os.Stat(EvolvedDir(root, name))
	return err == nil
}

// claimedByOther cho biết rel đang thuộc mục Published của một nguồn khác cùng tên với nguồn `own` ("custom" hoặc
// "flows"): nguồn đó vừa ghi đè tệp ở lần Install này nên nguồn cũ không được gỡ nó.
func claimedByOther(lock *Lock, own, name, rel string) bool {
	for src, m := range map[string]map[string]LockEntry{"custom": lock.Custom, "flows": lock.Flows, "evolved": lock.Evolved} {
		if src == own {
			continue
		}
		if e, ok := m[name]; ok && slices.Contains(e.Published, rel) {
			return true
		}
	}
	return false
}
