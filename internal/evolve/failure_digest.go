package evolve

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// DigestTokens là ngân sách token của một bản tóm tắt thất bại (kế hoạch E1: ~1.500 token).
const DigestTokens = 1500

// digestCaps: trần ký tự (rune) cho từng mục của bản tóm tắt. Tổng ≈ 4.350 ký tự, tức
// ~1.450 token theo ước lượng 3 ký tự/token (cùng quy ước với ctxengine). Trần theo mục
// làm chi phí của bước rút bài học không phụ thuộc độ dài log.
var digestCaps = map[string]int{
	"header":   240,
	"prompt":   900,
	"signals":  300,
	"commands": 1000,
	"files":    700,
	"tests":    900,
	"notes":    300,
}

// Digest là bản tóm tắt xác định của một lượt thất bại (E1). Cùng đầu vào cho cùng đầu ra.
type Digest struct {
	ID      string   `json:"id"`
	Source  string   `json:"source"`
	Unit    string   `json:"unit"`
	Area    string   `json:"area"`
	Signals []string `json:"signals"`
	Text    string   `json:"text"`
	Tokens  int      `json:"tokens"`
}

// tokensOf ước lượng số token của chuỗi (3 ký tự/token, làm tròn lên).
func tokensOf(s string) int {
	return (utf8.RuneCountInString(s) + 2) / 3
}

// BuildDigest dựng bản tóm tắt thất bại từ đơn vị đã nạp (không đọc lại log, không gọi mô
// hình). Đề bài của lượt đánh giá chỉ là đề bài, không bao giờ có diff lời giải.
func BuildDigest(f Failure) Digest {
	var parts []string
	add := func(name, body string) {
		body = strings.TrimRight(body, "\n")
		if strings.TrimSpace(body) == "" {
			return
		}
		parts = append(parts, clipRunes(body, digestCaps[name]))
	}

	add("header", fmt.Sprintf("Lượt %s (%s, nhánh %s, trạng thái %s, tác vụ %s, vùng %s)",
		f.ID, f.Source, dashIfEmpty(f.Arm), dashIfEmpty(f.Status), dashIfEmpty(f.Unit), dashIfEmpty(f.Area)))

	prompt := f.Prompt
	if strings.TrimSpace(prompt) == "" {
		prompt = "(không đọc được)"
	}
	add("prompt", "Đề bài: "+strings.Join(strings.Fields(prompt), " "))

	if len(f.Signals) == 0 {
		add("signals", "Dấu hiệu: không có")
	} else {
		var labels []string
		for _, s := range f.Signals {
			labels = append(labels, signalLabels[s])
		}
		add("signals", "Dấu hiệu: "+strings.Join(labels, "; "))
	}

	add("commands", commandsSection(f))
	add("files", filesSection(f))
	add("tests", testsSection(f))

	var notes []string
	if f.Source == "eval" {
		notes = append(notes, "nguồn: tập validation")
	}
	if f.Source == "run" {
		notes = append(notes, "nguồn: lượt thật có nhãn thất bại")
	}
	if f.Error != "" {
		notes = append(notes, "lỗi ghi nhận: "+f.Error)
	}
	add("notes", "Ghi chú: "+strings.Join(notes, "; "))

	text := strings.Join(parts, "\n")
	return Digest{ID: f.ID, Source: f.Source, Unit: f.Unit, Area: f.Area, Signals: append([]string{}, f.Signals...),
		Text: text, Tokens: tokensOf(text)}
}

// commandsSection liệt kê chuỗi lệnh, gộp lệnh lặp (×N). Không đo được thì nói rõ.
func commandsSection(f Failure) string {
	switch {
	case f.Source == "run":
		return "Lệnh đã chạy: không đo được (phiên thật chỉ ghi lệnh kiểm thử)"
	case !f.Measured:
		return "Lệnh đã chạy: không đo được (log không có vết công cụ)"
	case len(f.Trace.Commands) == 0:
		return "Lệnh đã chạy: không có lệnh nào"
	}
	type entry struct {
		cmd string
		n   int
	}
	var entries []*entry
	index := map[string]*entry{}
	for _, c := range f.Trace.Commands {
		k := strings.Join(strings.Fields(c), " ")
		if e := index[k]; e != nil {
			e.n++
			continue
		}
		e := &entry{cmd: k, n: 1}
		index[k] = e
		entries = append(entries, e)
	}
	const maxEntries = 12
	var b strings.Builder
	fmt.Fprintf(&b, "Lệnh đã chạy (%d, gộp lệnh lặp):", len(f.Trace.Commands))
	for i, e := range entries {
		if i == maxEntries {
			fmt.Fprintf(&b, "\n  … (+%d lệnh khác)", len(entries)-maxEntries)
			break
		}
		if e.n > 1 {
			fmt.Fprintf(&b, "\n  - %s ×%d", clipRunes(e.cmd, 160), e.n)
		} else {
			fmt.Fprintf(&b, "\n  - %s", clipRunes(e.cmd, 160))
		}
	}
	return b.String()
}

// filesSection liệt kê tệp đã đọc và đã sửa (sắp xếp, không trùng).
func filesSection(f Failure) string {
	reads := f.Read
	if f.Source == "eval" {
		if !f.Measured {
			reads = nil
		} else {
			reads = f.Trace.Reads
		}
	}
	readLine := "Tệp đọc: " + listOrNone(uniqueSorted(reads), 12)
	if f.Source == "eval" && !f.Measured {
		readLine = "Tệp đọc: không đo được (log không có vết công cụ)"
	}
	writes := append(append([]string{}, f.Modified...), f.Trace.Writes...)
	if f.Source == "eval" && !f.Measured {
		writes = append([]string{}, f.Modified...)
	}
	return readLine + "\nTệp sửa: " + listOrNone(uniqueSorted(writes), 12)
}

// testsSection: tên test trượt lúc chấm và đuôi log. Lượt thật không có kết quả chấm.
func testsSection(f Failure) string {
	if f.Source == "run" {
		return "Test lúc chấm: không đo được"
	}
	if len(f.TestFails) == 0 && len(f.TestTail) == 0 {
		return "Test lúc chấm: không đo được (không có test.log)"
	}
	var b strings.Builder
	b.WriteString("Test lúc chấm:")
	for _, l := range f.TestFails {
		b.WriteString("\n  " + l)
	}
	if len(f.TestTail) > 0 {
		b.WriteString("\nĐuôi log:")
		for _, l := range f.TestTail {
			b.WriteString("\n  " + l)
		}
	}
	return b.String()
}

func uniqueSorted(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if x != "" && !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}

func listOrNone(xs []string, max int) string {
	if len(xs) == 0 {
		return "(không có)"
	}
	if len(xs) > max {
		return strings.Join(xs[:max], ", ") + fmt.Sprintf(" … (+%d)", len(xs)-max)
	}
	return strings.Join(xs, ", ")
}

func dashIfEmpty(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
