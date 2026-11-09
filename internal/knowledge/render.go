package knowledge

import (
	"fmt"
	"sort"
	"strings"
)

// RenderOptions điều khiển việc dựng phần tri thức cho AGENTS.md.
type RenderOptions struct {
	// MaxItems giới hạn số mục được đưa vào (mục 8.5: AGENTS.md ngắn).
	MaxItems int
	// IncludeCandidates đưa cả mục chưa duyệt vào (chỉ dùng cho nhánh đo lường
	// với tri thức do bootstrap tự động sinh, mục 12.3 strict). Quy tắc chưa duyệt
	// được trình bày như tham khảo, không như quy tắc bắt buộc.
	IncludeCandidates bool
	// MaxBodyLines cắt nội dung mỗi mục.
	MaxBodyLines int
	// ForEval bỏ phần hướng dẫn dùng CLI Bint (thư mục đánh giá không có Bint),
	// chỉ giữ nội dung tri thức.
	ForEval bool
}

// Selection là kết quả chọn mục cho AGENTS.md.
type Selection struct {
	Rules     []*Item // quy tắc bắt buộc đã duyệt
	Warnings  []*Item // điểm dễ sai đã duyệt
	Reference []*Item // mục chưa duyệt (chỉ khi IncludeCandidates)
	Omitted   []Omission
}

// Omission là một mục không được đưa vào và lý do.
type Omission struct {
	ID     string
	Reason string
}

// Select chọn các mục cho AGENTS.md: quy tắc (rule) và điểm dễ sai (warning) đã
// verified; bỏ mục gắn cờ bên thứ ba (mục 15.5) và mục stale (chưa xác thực lại).
func Select(items []*Item, opts RenderOptions) Selection {
	if opts.MaxItems <= 0 {
		opts.MaxItems = 15
	}
	var sel Selection
	sorted := append([]*Item(nil), items...)
	// Toàn cục trước, rồi theo phạm vi, rồi theo ID (thời điểm tạo).
	sort.SliceStable(sorted, func(i, j int) bool {
		gi, gj := sorted[i].Scope.Global(), sorted[j].Scope.Global()
		if gi != gj {
			return gi
		}
		si, sj := scopeKey(sorted[i]), scopeKey(sorted[j])
		if si != sj {
			return si < sj
		}
		return sorted[i].ID < sorted[j].ID
	})
	n := 0
	for _, it := range sorted {
		if it.Type != TypeRule && it.Type != TypeWarning {
			continue
		}
		switch it.Status {
		case StatusDeprecated:
			continue
		case StatusStale:
			sel.Omitted = append(sel.Omitted, Omission{it.ID, "stale: bằng chứng đã đổi, chờ xác thực lại"})
			continue
		case StatusCandidate:
			if !opts.IncludeCandidates {
				continue
			}
		}
		if it.Origin.ThirdParty && it.Status != StatusVerified {
			sel.Omitted = append(sel.Omitted, Omission{it.ID, "đầu vào bên thứ ba, chưa duyệt"})
			continue
		}
		if n >= opts.MaxItems {
			sel.Omitted = append(sel.Omitted, Omission{it.ID, fmt.Sprintf("vượt giới hạn %d mục", opts.MaxItems)})
			continue
		}
		n++
		switch {
		case it.Status == StatusCandidate:
			sel.Reference = append(sel.Reference, it)
		case it.Type == TypeRule && !it.Origin.ThirdParty:
			sel.Rules = append(sel.Rules, it)
		default:
			sel.Warnings = append(sel.Warnings, it)
		}
	}
	return sel
}

func scopeKey(it *Item) string { return strings.Join(it.Scope.Paths, ",") }

// Count trả về số mục được chọn.
func (s Selection) Count() int { return len(s.Rules) + len(s.Warnings) + len(s.Reference) }

// RenderAgents dựng nội dung khối Bint trong AGENTS.md.
func RenderAgents(items []*Item, opts RenderOptions) (string, Selection) {
	if opts.MaxBodyLines <= 0 {
		opts.MaxBodyLines = 8
	}
	sel := Select(items, opts)
	var b strings.Builder
	if opts.ForEval {
		b.WriteString("# Quy ước của dự án\n")
	} else {
		b.WriteString("<!-- Khối do Bint quản lý. Không sửa tay: sửa tệp .bint/knowledge/items/<id>.md\n")
		b.WriteString("rồi chạy `bint bootstrap publish`. Mã [k:<id>] để trích dẫn mục tri thức. -->\n\n")
		b.WriteString("## Quy ước của dự án (Bint)\n\n")
		b.WriteString("Ghi lại điều bất thường vừa phát hiện (lệnh đặc biệt, chỗ dễ sai) bằng\n")
		b.WriteString("`bint knowledge add --type warning --paths '<glob>' --evidence <tệp>:<dòng> \"<nội dung>\"`;\n")
		b.WriteString("mục do agent ghi chờ người duyệt, không tự có hiệu lực.\n")
	}

	if sel.Count() == 0 {
		b.WriteString("\nChưa có quy tắc nào được duyệt.\n")
		return b.String(), sel
	}
	writeGroup := func(title string, list []*Item) {
		if len(list) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n### %s\n", title)
		lastScope := "\x00"
		for _, it := range list {
			if sk := scopeKey(it); sk != lastScope && !it.Scope.Global() {
				fmt.Fprintf(&b, "\nKhi làm việc với %s:\n\n", quotePaths(it.Scope.Paths))
				lastScope = sk
			} else if lastScope == "\x00" {
				b.WriteString("\n")
				lastScope = sk
			}
			writeItem(&b, it, opts.MaxBodyLines)
		}
	}
	writeGroup("Quy tắc bắt buộc", sel.Rules)
	writeGroup("Điểm dễ sai", sel.Warnings)
	if len(sel.Reference) > 0 {
		writeGroup("Tham khảo (chưa được người duyệt — kiểm tra lại trước khi dựa vào)", sel.Reference)
	}
	return b.String(), sel
}

func quotePaths(ps []string) string {
	q := make([]string, len(ps))
	for i, p := range ps {
		q[i] = "`" + p + "`"
	}
	return strings.Join(q, ", ")
}

func writeItem(b *strings.Builder, it *Item, maxLines int) {
	fmt.Fprintf(b, "- **%s** [k:%s]\n", strings.TrimSpace(it.Title), it.ID)
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(it.Body, "\r\n", "\n")), "\n")
	// Bỏ dòng tiêu đề trùng lặp ở đầu nội dung.
	if len(lines) > 0 && strings.EqualFold(strings.TrimSpace(strings.TrimLeft(lines[0], "# ")), strings.TrimSpace(it.Title)) {
		lines = lines[1:]
	}
	cut := false
	if len(lines) > maxLines {
		lines, cut = lines[:maxLines], true
	}
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			b.WriteString("\n")
			continue
		}
		b.WriteString("  " + l + "\n")
	}
	if cut {
		fences := 0
		for _, l := range lines {
			if strings.HasPrefix(strings.TrimSpace(l), "```") {
				fences++
			}
		}
		if fences%2 == 1 {
			b.WriteString("  ```\n")
		}
		fmt.Fprintf(b, "  (… xem đầy đủ: .bint/knowledge/items/%s.md)\n", it.ID)
	}
}
