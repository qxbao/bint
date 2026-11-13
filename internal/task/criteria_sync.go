package task

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// lintACLineRe tách một dòng AC thành tiền tố checkbox, dấu đánh dấu, số và nội dung (cùng khuôn lintCheckboxRe + lintACRe).
var lintACLineRe = regexp.MustCompile(`^(\s*[-*+]\s+\[)([ xX])(\]\s+)AC-(\d+)\s*:\s*(.*\S)?\s*$`)

// SyncCriteriaArtifacts đưa requirement.md (và tasks.md nếu có) về khớp với verify.criteria của phiếu, để
// `bint task lint --strict` qua ngay sau `bint task set --criteria/--clear-criteria` mà không cần `bint task write`.
//
//   - Không có requirement.md: không làm gì.
//   - Mục "Tiêu chí chấp nhận": AC-N (N ≤ số tiêu chí) mang đúng nội dung tiêu chí thứ N (giữ nguyên [x] khi nội dung
//     không đổi), AC có số lớn hơn bị bỏ, AC còn thiếu được thêm nguyên văn sau dòng AC cuối. Chưa có mục thì chèn
//     trước "Ngoài phạm vi" hoặc cuối tệp. Dòng khác giữ nguyên từng byte.
//   - tasks.md: mỗi AC chưa có TSK nào bao phủ được thêm một TSK ở cuối tệp. TSK tham chiếu AC không còn chỉ được
//     ghi chú trong notes (Bint không đoán nên đổi sang AC nào).
//
// Trả về tên artifact đã ghi (requirement, tasks) và các ghi chú để người dùng đọc.
func SyncCriteriaArtifacts(t *Task) (written, notes []string, err error) {
	crit := t.Ticket.Verify.Criteria
	reqOld, err := os.ReadFile(t.ArtifactPath("requirement"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("không đọc được requirement.md: %w", err)
	}
	if strings.TrimSpace(string(reqOld)) == "" {
		return nil, nil, nil
	}
	reqNew, reqNotes := syncRequirementText(string(reqOld), crit)
	if reqNew != string(reqOld) {
		if _, err := t.WriteArtifact("requirement", reqNew); err != nil {
			return nil, nil, err
		}
		written = append(written, "requirement")
		notes = append(notes, reqNotes...)
	}
	tasksOld, err := os.ReadFile(t.ArtifactPath("tasks"))
	switch {
	case errors.Is(err, os.ErrNotExist):
		return written, notes, nil
	case err != nil:
		return written, notes, fmt.Errorf("không đọc được tasks.md: %w", err)
	case strings.TrimSpace(string(tasksOld)) == "":
		return written, notes, nil
	}
	tasksNew, taskNotes := syncTasksText(string(tasksOld), len(crit), t.State.ID)
	notes = append(notes, taskNotes...)
	if tasksNew != string(tasksOld) {
		if _, err := t.WriteArtifact("tasks", tasksNew); err != nil {
			return written, notes, err
		}
		written = append(written, "tasks")
	}
	return written, notes, nil
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// splitKeepEOL tách văn bản thành dòng (bỏ "\r" cuối dòng) và nhớ kiểu xuống dòng để ghi lại đúng kiểu.
func splitKeepEOL(text string) (lines []string, eol string) {
	eol = "\n"
	if strings.Contains(text, "\r\n") {
		eol = "\r\n"
	}
	lines = strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	return lines, eol
}

type acLine struct {
	idx     int
	n       int
	checked bool
	text    string
	m       []string
}

func syncRequirementText(text string, crit []string) (string, []string) {
	lines, eol := splitKeepEOL(text)
	var acs []acLine
	acHead, outHead := -1, -1
	walkLines(lines, func(i int, section, line string) {
		if section != secAC {
			return
		}
		m := lintACLineRe.FindStringSubmatch(line)
		if m == nil {
			return
		}
		n, _ := strconv.Atoi(m[4])
		acs = append(acs, acLine{idx: i, n: n, checked: m[2] != " ", text: m[5], m: m})
	}, func(i int, kind string) {
		switch {
		case kind == secAC && acHead < 0:
			acHead = i
		case kind == "ngoài phạm vi" && outHead < 0:
			outHead = i
		}
	})

	have := map[int]bool{}
	drop := map[int]bool{}
	replace := map[int]string{}
	lastAC := -1
	var notes []string
	for _, a := range acs {
		lastAC = max(lastAC, a.idx)
		switch {
		case a.n < 1:
		case a.n > len(crit):
			drop[a.idx] = true
			notes = append(notes, fmt.Sprintf("đã bỏ AC-%d khỏi requirement.md (phiếu chỉ còn %d tiêu chí)", a.n, len(crit)))
		case have[a.n]:
			// AC trùng số: để nguyên cho lint báo duplicate_id, không đoán nên giữ dòng nào.
		default:
			have[a.n] = true
			want := oneLine(crit[a.n-1])
			if oneLine(a.text) == want {
				continue
			}
			mark := " "
			replace[a.idx] = a.m[1] + mark + a.m[3] + fmt.Sprintf("AC-%d: ", a.n) + want
			notes = append(notes, fmt.Sprintf("đã cập nhật AC-%d trong requirement.md theo phiếu", a.n))
		}
	}
	var add []string
	for n := 1; n <= len(crit); n++ {
		if !have[n] {
			add = append(add, fmt.Sprintf("- [ ] AC-%d: %s", n, oneLine(crit[n-1])))
			notes = append(notes, fmt.Sprintf("đã thêm AC-%d vào requirement.md", n))
		}
	}

	var out []string
	insertAfter := lastAC
	if insertAfter < 0 && acHead >= 0 {
		insertAfter = acHead
	}
	flush := func() { out = append(out, add...); add = nil }
	if insertAfter < 0 && len(add) > 0 {
		// Chưa có mục AC: dựng mục mới trước "Ngoài phạm vi" hoặc cuối tệp.
		block := append([]string{"## Tiêu chí chấp nhận", ""}, add...)
		add = nil
		at := len(lines)
		if outHead >= 0 {
			at = outHead
		}
		prefix := lines[:at]
		for len(prefix) > 0 && strings.TrimSpace(prefix[len(prefix)-1]) == "" {
			prefix = prefix[:len(prefix)-1]
		}
		out = append(out, prefix...)
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, block...)
		rest := lines[at:]
		if len(rest) > 0 && !(len(rest) == 1 && rest[0] == "") {
			out = append(out, "")
		}
		out = append(out, rest...)
		if len(rest) == 0 {
			out = append(out, "") // kết thúc bằng một dòng mới
		}
		return strings.Join(out, eol), append(notes, "requirement.md chưa có mục Tiêu chí chấp nhận: đã thêm mục mới")
	}
	for i, line := range lines {
		switch {
		case drop[i]:
		case replace[i] != "":
			out = append(out, replace[i])
		default:
			out = append(out, line)
		}
		if i == insertAfter {
			flush()
		}
	}
	return strings.Join(out, eol), notes
}

func syncTasksText(text string, nCrit int, taskID string) (string, []string) {
	lines, eol := splitKeepEOL(text)
	covered := map[int]bool{}
	maxTSK := 0
	var notes []string
	walkLines(lines, func(_ int, _ string, line string) {
		cb := lintCheckboxRe.FindStringSubmatch(line)
		if cb == nil {
			return
		}
		m := lintTSKRe.FindStringSubmatch(cb[2])
		if m == nil {
			return
		}
		n, _ := strconv.Atoi(m[1])
		maxTSK = max(maxTSK, n)
		for _, tok := range strings.FieldsFunc(m[2], func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
			rm := lintRefRe.FindStringSubmatch(tok)
			if rm == nil {
				continue
			}
			ref, _ := strconv.Atoi(rm[1])
			if ref > nCrit {
				notes = append(notes, fmt.Sprintf("TSK-%d trong tasks.md tham chiếu AC-%d không còn trong phiếu: sửa tay (bint task write %s tasks)", n, ref, taskID))
				continue
			}
			covered[ref] = true
		}
	}, nil)
	var add []string
	for n := 1; n <= nCrit; n++ {
		if covered[n] {
			continue
		}
		maxTSK++
		add = append(add, fmt.Sprintf("- [ ] TSK-%d [AC-%d]: Đáp ứng tiêu chí AC-%d bổ sung sau khi lập kế hoạch — xong khi: AC-%d được đối chiếu đạt kèm bằng chứng", maxTSK, n, n, n))
		notes = append(notes, fmt.Sprintf("đã thêm TSK-%d cho AC-%d vào tasks.md", maxTSK, n))
	}
	if len(add) == 0 {
		return text, notes
	}
	// Bỏ dòng trống cuối rồi nối thêm; kết thúc bằng một dòng mới.
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	lines = append(lines, add...)
	return strings.Join(lines, eol) + eol, notes
}
