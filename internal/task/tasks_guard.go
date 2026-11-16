package task

import (
	"regexp"
	"strconv"
	"strings"
)

// Bước implement (b-feat) chỉ được đánh dấu tiến độ trong tasks.md ([ ] → [x]); khi agent ghi lại cả tệp, nó có thể làm
// mất `— xong khi: …` và tác vụ bị chặn chờ người ở lần lint kế tiếp (hợp đồng v1). Runner chụp tasks.md hợp lệ trước
// bước và, nếu sau bước tệp không còn đạt hợp đồng, phục hồi bằng RestoreTaskLines.

var (
	guardTSKIDRe = regexp.MustCompile(`^TSK-(\d+)\b`)
	guardBoxRe   = regexp.MustCompile(`^(\s*[-*+]\s+\[)[ xX](\])`)
)

// taskChecks trả trạng thái [x]/[ ] theo TSK-ID của các dòng TSK (ngoài code fence) và vị trí dòng của chúng. ok=false
// khi một TSK-ID xuất hiện nhiều lần (không biết lấy trạng thái nào).
func taskChecks(lines []string) (checks map[int]bool, idx map[int]int, ok bool) {
	checks, idx, ok = map[int]bool{}, map[int]int{}, true
	walkLines(lines, func(i int, _ string, line string) {
		cb := lintCheckboxRe.FindStringSubmatch(line)
		if cb == nil {
			return
		}
		m := guardTSKIDRe.FindStringSubmatch(cb[2])
		if m == nil {
			return
		}
		n, _ := strconv.Atoi(m[1])
		if _, dup := checks[n]; dup {
			ok = false
			return
		}
		checks[n], idx[n] = cb[1] != " ", i
	}, nil)
	return checks, idx, ok
}

// RestoreTaskLines dựng lại tasks.md từ bản trước bước (before, đạt hợp đồng) và giữ trạng thái đánh dấu theo TSK-ID của
// bản sau bước (after): nội dung mọi dòng lấy từ before, chỉ ô [ ]/[x] của dòng TSK lấy từ after. merged=false khi không
// ghép được (tập TSK-ID hai bản khác nhau, TSK-ID trùng, before không có TSK): nơi gọi phục hồi nguyên before.
func RestoreTaskLines(before, after string) (out string, merged bool) {
	bl, eol := splitKeepEOL(before)
	al, _ := splitKeepEOL(after)
	bChecks, bIdx, bok := taskChecks(bl)
	aChecks, _, aok := taskChecks(al)
	if !bok || !aok || len(bChecks) == 0 || len(bChecks) != len(aChecks) {
		return before, false
	}
	for n := range bChecks {
		if _, ok := aChecks[n]; !ok {
			return before, false
		}
	}
	for n, i := range bIdx {
		mark := " "
		if aChecks[n] {
			mark = "x"
		}
		bl[i] = guardBoxRe.ReplaceAllString(bl[i], "${1}"+mark+"${2}")
	}
	return strings.Join(bl, eol), true
}
