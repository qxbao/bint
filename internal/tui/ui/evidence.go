package ui

import (
	"strconv"
	"strings"
	"unicode"

	"bint/internal/app"
	"bint/internal/i18n"
	"bint/internal/knowledge"
)

// Bằng chứng lệch hiện trong TUI từ dữ liệu có cấu trúc (knowledge.EvidenceIssue, app.EvidenceError), dựng câu qua i18n theo ngôn ngữ
// giao diện và gợi ý phím đúng với từng ca, không qua Rewriter (docs/tui-plan.md mục 12.11). Gợi ý theo cách sửa của vấn đề: dời chỗ/chuyển
// tệp → phím cập nhật vị trí; nội dung đổi → xem lại rồi làm mới fingerprint; còn lại → sửa bằng chứng hoặc từ chối. Không bao giờ chỉ tới
// làm mới fingerprint khi việc đó chắc chắn hỏng.

// EvidenceCause là các dòng mô tả vấn đề: vị trí cũ, chi tiết theo loại, vị trí mới hoặc ứng viên, số dòng hiện có của tệp (khi vùng
// vượt quá, kể cả ca ambiguous từ vùng vượt quá), và ghi chú khi tìm chuyển tệp bị cắt theo giới hạn chi phí. Không gồm cách xử lý.
func EvidenceCause(rw Rewriter, iss knowledge.EvidenceIssue) []string {
	cat := rw.Cat
	out := []string{cat.T("kn.ev.at", i18n.A{"ref": iss.Ref()})}
	if k := "kn.ev." + iss.Kind; cat.Has(k) {
		out = append(out, cat.T(k, i18n.A{"lines": iss.Lines, "symbol": iss.Symbol}))
	}
	if n := iss.NewRef(); n != "" {
		out = append(out, cat.T("kn.ev.new", i18n.A{"new": n}))
	}
	if len(iss.Candidates) > 0 {
		out = append(out, cat.T("kn.ev.candidates", i18n.A{"count": strconv.Itoa(len(iss.Candidates)), "list": strings.Join(iss.Candidates, ", ")}))
	}
	if iss.FileLines > 0 { // out_of_range, và ambiguous phát sinh từ vùng vượt quá tệp (moved đặt FileLines = 0)
		out = append(out, cat.T("kn.ev.file_lines", i18n.A{"count": strconv.Itoa(iss.FileLines)}))
	}
	if iss.Truncated {
		out = append(out, cat.T("kn.ev.truncated"))
	}
	return out
}

// EvidenceHint là cách xử lý bằng phím TUI cho cách sửa fix (knowledge.Fix*); rỗng nếu không cần xử lý.
func EvidenceHint(rw Rewriter, fix string) string {
	switch fix {
	case knowledge.FixApply:
		return rw.Cat.T("kn.ev.fix.apply", i18n.A{"apply": rw.ActionRef("knowledge.verify", "apply")})
	case knowledge.FixRefresh:
		return rw.Cat.T("kn.ev.fix.refresh", i18n.A{"show": rw.ActionRef("knowledge.show"), "refresh": rw.ActionRef("knowledge.approve_refresh")})
	case knowledge.FixEdit:
		return rw.Cat.T("kn.ev.fix.edit", i18n.A{"edit": rw.ActionRef("knowledge.edit"), "reject": rw.ActionRef("knowledge.reject")})
	}
	return ""
}

// EvidenceLines là các dòng của một vấn đề cho khung Detail và hộp kết quả: nguyên nhân rồi cách xử lý.
func EvidenceLines(rw Rewriter, iss knowledge.EvidenceIssue) []string {
	out := EvidenceCause(rw, iss)
	if h := EvidenceHint(rw, iss.Fix()); h != "" {
		out = append(out, h)
	}
	return out
}

// EvidenceText là EvidenceLines nối thành một câu (toast, hộp xác minh).
func EvidenceText(rw Rewriter, iss knowledge.EvidenceIssue) string {
	return strings.Join(EvidenceLines(rw, iss), "; ")
}

// evidenceErrText dựng câu cho lỗi duyệt có cấu trúc: mọi vấn đề rồi một cách xử lý theo cách sửa nặng nhất.
func evidenceErrText(rw Rewriter, e *app.EvidenceError) string {
	var issues []string
	for _, i := range e.Issues {
		issues = append(issues, strings.Join(EvidenceCause(rw, i), "; "))
	}
	return strings.TrimSpace(rw.Cat.T("err.evidence", i18n.A{
		"issues": strings.Join(issues, " | "),
		"hint":   upperFirst(EvidenceHint(rw, e.Fix())),
	}))
}

func upperFirst(s string) string {
	for i, r := range s {
		return string(unicode.ToUpper(r)) + s[i+len(string(r)):]
	}
	return s
}
