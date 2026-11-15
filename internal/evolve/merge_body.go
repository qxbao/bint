package evolve

import (
	"regexp"
	"strings"

	"bint/internal/knowledge"
)

// mergeBodies dựng body cho mục được giữ lại khi gộp hai mục trùng (P6-05): giữ nguyên keep, rồi nối
// vào cuối phần của drop mà keep chưa có, trong một khối có dòng tiêu đề nêu ID mục bị gộp. Xác định,
// không gọi mô hình.
//
// Đơn vị so sánh của drop: khối mã rào ``` (nguyên khối), mục danh sách (`- `, `* `, `+ `, `1. `, kèm
// dòng thụt tiếp theo) và câu của đoạn văn xuôi (tách tại . ! ? theo sau là khoảng trắng). Một đơn vị
// "đã có" khi dạng chuẩn hoá của nó (gộp khoảng trắng, chữ thường; mục danh sách/câu còn bỏ ký hiệu
// danh sách đầu mục và dấu câu cuối, nên "- X" = "X." = "X", xem normUnit) BẰNG đúng một đơn vị của keep (keep
// được tách bằng cùng hàm tách đơn vị); không so chuỗi con, nên "Retry on timeout." không bị coi là đã có
// trong "Do not retry on timeout." và "- cache" không bị coi là đã có trong "- cache-busting". Phần thiếu được nối nguyên văn theo thứ tự gốc: đoạn thiếu hết giữ nguyên đoạn, đoạn thiếu
// một phần nối các câu thiếu bằng một khoảng trắng.
//
// Không thiếu gì thì trả keep y nguyên. Bất biến: mergeBodies(kết quả, drop, id) trả lại kết quả.
func mergeBodies(keep, drop, dropID string) string {
	have := bodyUnits(keep)
	var missing []string
	dropBlocks, _ := splitBlocks(drop)
	for _, blk := range dropBlocks {
		switch blk.kind {
		case blockCode, blockItem:
			if n := normUnit(blk.kind, blk.text); n != "" && !have[n] {
				missing = append(missing, blk.text)
			}
		default: // đoạn văn xuôi
			sents := splitSentences(blk.text)
			var miss []string
			for _, s := range sents {
				if n := normUnit(blockProse, s); n != "" && !have[n] {
					miss = append(miss, s)
				}
			}
			switch {
			case len(miss) == 0:
			case len(miss) == len(sents):
				missing = append(missing, blk.text)
			default:
				missing = append(missing, strings.Join(miss, " "))
			}
		}
	}
	if len(missing) == 0 {
		return keep
	}
	head := "Gộp từ " + dropID + ":"
	tail := head + "\n\n" + strings.Join(missing, "\n\n")
	if strings.TrimSpace(keep) == "" {
		return tail
	}
	out := strings.TrimRight(keep, " \t\r\n")
	// Khối mã rào chưa đóng ở cuối keep sẽ nuốt khối "Gộp từ" ở lần tách sau (mất tính idempotent):
	// đóng rào trước khi nối.
	if _, unclosed := splitBlocks(keep); unclosed {
		out += "\n```"
	}
	return out + "\n\n" + tail
}

// bodyUnits trả tập đơn vị đã chuẩn hoá của body: khối mã, mục danh sách và câu của đoạn văn xuôi
// (cùng cách tách với phần drop của mergeBodies).
func bodyUnits(body string) map[string]bool {
	units := map[string]bool{}
	blocks, _ := splitBlocks(body)
	for _, blk := range blocks {
		switch blk.kind {
		case blockCode, blockItem:
			if n := normUnit(blk.kind, blk.text); n != "" {
				units[n] = true
			}
		default:
			for _, s := range splitSentences(blk.text) {
				if n := normUnit(blockProse, s); n != "" {
					units[n] = true
				}
			}
		}
	}
	return units
}

// carryThirdParty: gộp (P6-05) đưa nội dung của các mục bị gộp vào mục giữ lại keepID; nếu một mục bị
// gộp mang cờ bên thứ ba thì cờ theo sang (Draft không mang cờ, Bint quyết; chỉ đặt true, mục 15.5).
// Dùng chung cho Apply và Evaluate để bản được đánh giá giống bản được áp dụng.
func carryThirdParty(p *Proposal, it *knowledge.Item, keepID string, byID map[string]*knowledge.Item) {
	if p.Action != ActionMerge {
		return
	}
	for _, id := range p.TargetIDs {
		if other := byID[id]; id != keepID && other != nil && other.Origin.ThirdParty {
			it.Origin.ThirdParty = true
		}
	}
}

// normBody: gộp mọi khoảng trắng thành một dấu cách và đổi chữ thường.
func normBody(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// normUnit chuẩn hoá một đơn vị để so trùng: như normBody, và với mục danh sách/câu văn xuôi còn bỏ
// ký hiệu danh sách đầu mục và dấu câu cuối (. : ;), để "- X", "X." và "X" là cùng một đơn vị. "?" và "!"
// giữ lại vì đổi nghĩa câu (câu hỏi/câu cảm khác câu khẳng định).
// Khối mã giữ nguyên (chỉ gộp khoảng trắng, chữ thường).
func normUnit(kind int, text string) string {
	if kind == blockCode {
		return normBody(text)
	}
	t := strings.TrimSpace(text)
	if loc := listMarkerRe.FindStringSubmatchIndex(t); loc != nil {
		t = t[loc[3]:] // bỏ ký hiệu danh sách; khoảng trắng sau nó do normBody gộp/TrimSpace
	}
	return strings.TrimSpace(strings.TrimRight(normBody(t), ".:; "))
}

const (
	blockProse = iota
	blockItem
	blockCode
)

type bodyBlock struct {
	kind int
	text string
}

var listMarkerRe = regexp.MustCompile(`^\s{0,3}([-*+]|\d{1,3}[.)])\s+\S`)

// splitBlocks tách body thành khối mã rào, mục danh sách và đoạn văn xuôi (theo thứ tự gốc). unclosed
// báo khối mã rào cuối cùng không có dòng đóng (nó nuốt hết phần còn lại của body).
func splitBlocks(body string) (out []bodyBlock, unclosed bool) {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	var cur []string
	curKind := blockProse
	flush := func() {
		if t := strings.TrimSpace(strings.Join(cur, "\n")); t != "" {
			out = append(out, bodyBlock{kind: curKind, text: t})
		}
		cur, curKind = nil, blockProse
	}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trim := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trim, "```"):
			flush()
			code := []string{line}
			closed := false
			for i++; i < len(lines); i++ {
				code = append(code, lines[i])
				if strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
					closed = true
					break
				}
			}
			unclosed = !closed
			out = append(out, bodyBlock{kind: blockCode, text: strings.TrimRight(strings.Join(code, "\n"), " \t\r\n")})
		case trim == "":
			flush()
		case listMarkerRe.MatchString(line):
			flush()
			cur, curKind = []string{line}, blockItem
		case curKind == blockItem && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")):
			cur = append(cur, line) // dòng tiếp của mục danh sách
		default:
			if curKind == blockItem {
				flush()
			}
			cur = append(cur, line)
		}
	}
	flush()
	return out, unclosed
}

// splitSentences tách đoạn văn xuôi thành câu: dấu . ! ? theo sau là khoảng trắng ngắt câu (dấu câu
// ở lại với câu). Không tách được thì cả đoạn là một câu.
func splitSentences(p string) []string {
	var out []string
	rs := []rune(p)
	start := 0
	for i, r := range rs {
		if (r == '.' || r == '!' || r == '?') && i+1 < len(rs) && (rs[i+1] == ' ' || rs[i+1] == '\t' || rs[i+1] == '\n') {
			if s := strings.TrimSpace(string(rs[start : i+1])); s != "" {
				out = append(out, s)
			}
			start = i + 1
		}
	}
	if s := strings.TrimSpace(string(rs[start:])); s != "" {
		out = append(out, s)
	}
	return out
}
