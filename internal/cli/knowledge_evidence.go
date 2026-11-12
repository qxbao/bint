package cli

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"bint/internal/app"
	"bint/internal/knowledge"
)

// evRef là mục mà các phát hiện thuộc về: ID và đường dẫn tệp mục (nơi sửa bằng chứng) để dựng
// gợi ý lệnh. ID rỗng = mục chưa được ghi (knowledge add): không có gợi ý nào dùng được.
type evRef struct{ ID, File string }

func evRefOf(k *app.KnowledgeWS, id string) evRef {
	file := k.Store.PathFor(id)
	if rel, err := filepath.Rel(k.Root, file); err == nil {
		file = rel
	}
	return evRef{ID: id, File: filepath.ToSlash(file)}
}

// evidenceText dựng câu mô tả một vấn đề bằng chứng từ tham số có cấu trúc (CLI chỉ có tiếng Việt).
func evidenceText(i knowledge.EvidenceIssue) string {
	r := i.Ref()
	var s string
	switch i.Kind {
	case knowledge.IssueMissingFile:
		s = fmt.Sprintf("bằng chứng %s: không còn tệp %s", r, i.Path)
	case knowledge.IssueOutOfRange:
		if i.FileLines > 0 {
			s = fmt.Sprintf("bằng chứng %s: tệp %s chỉ còn %d dòng, không tìm lại được vùng %s", r, i.Path, i.FileLines, i.Lines)
		} else {
			s = fmt.Sprintf("bằng chứng %s: vùng %s vượt quá tệp, không tìm lại được", r, i.Lines)
		}
	case knowledge.IssueChanged:
		s = fmt.Sprintf("bằng chứng %s đã thay đổi kể từ khi ghi", r)
	case knowledge.IssueSymbolGone:
		s = fmt.Sprintf("bằng chứng %s: không còn tìm thấy ký hiệu %q", r, i.Symbol)
	case knowledge.IssueBadRange:
		s = fmt.Sprintf("bằng chứng %s: vùng dòng không hợp lệ", r)
	case knowledge.IssueNoFingerprint:
		s = fmt.Sprintf("bằng chứng %s chưa có fingerprint (không phát hiện được khi lỗi thời)", r)
	case knowledge.IssueRelocated:
		s = fmt.Sprintf("bằng chứng %s: nội dung không đổi, dịch sang %s", r, i.NewRef())
	case knowledge.IssueMoved:
		s = fmt.Sprintf("bằng chứng %s: nội dung không đổi, đã chuyển sang %s", r, i.NewRef())
	case knowledge.IssueAmbiguous:
		s = fmt.Sprintf("bằng chứng %s: nội dung có ở %d nơi (%s), không chọn được", r, len(i.Candidates), strings.Join(i.Candidates, ", "))
		if i.FileLines > 0 {
			s += fmt.Sprintf("; tệp %s chỉ còn %d dòng", i.Path, i.FileLines)
		}
	default:
		s = i.Describe()
	}
	if i.Truncated {
		s += " (tìm chuyển tệp dừng ở giới hạn chi phí nên chưa chắc đã hết ứng viên)"
	}
	return s
}

// evidenceHint dựng gợi ý lệnh CLI cho cách sửa fix; rỗng nếu không có gợi ý dùng được.
func evidenceHint(fix string, ref evRef) string {
	if ref.ID == "" {
		return ""
	}
	switch fix {
	case knowledge.FixApply:
		return fmt.Sprintf("chạy `bint knowledge verify --apply %s` để cập nhật vị trí", ref.ID)
	case knowledge.FixRefresh:
		return fmt.Sprintf("xem lại (`bint knowledge show %s`) rồi `bint knowledge approve --refresh %s`", ref.ID, ref.ID)
	case knowledge.FixEdit:
		return fmt.Sprintf("sửa bằng chứng trong %s, hoặc từ chối mục bằng `bint knowledge reject %s --reason \"...\"`", ref.File, ref.ID)
	}
	return ""
}

// evidenceErrorText dựng câu cho lỗi duyệt có cấu trúc: các vấn đề rồi một gợi ý theo cách sửa nặng nhất.
func evidenceErrorText(e *app.EvidenceError) string {
	var parts []string
	for _, i := range e.Issues {
		parts = append(parts, evidenceText(i))
	}
	s := strings.Join(parts, "; ")
	if h := evidenceHint(e.Fix(), evRef{ID: e.ID, File: e.File}); h != "" {
		s += "; " + h
	}
	return s
}

// reviewErrText là câu của lỗi duyệt: lỗi bằng chứng có cấu trúc thì dựng từ cấu trúc, lỗi khác giữ nguyên.
func reviewErrText(err error) string {
	var ee *app.EvidenceError
	if errors.As(err, &ee) {
		return evidenceErrorText(ee)
	}
	return err.Error()
}

// printFindings in các phát hiện của một mục. Phát hiện bằng chứng có cấu trúc được dựng câu từ
// tham số kèm gợi ý đúng ca; phát hiện khác in Detail như cũ.
func printFindings(w io.Writer, fs []knowledge.Finding, indent string, ref evRef) {
	for _, f := range fs {
		text := f.Detail
		if iss, ok := f.IssueOf(); ok {
			text = evidenceText(iss)
			if h := evidenceHint(iss.Fix(), ref); h != "" {
				text += "; " + h
			}
		}
		fmt.Fprintf(w, "%s%s %-12s %s\n", indent, findingIcon[f.Level], f.Code, text)
	}
}
