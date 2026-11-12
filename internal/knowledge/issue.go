package knowledge

import (
	"fmt"
	"strings"
)

// Loại vấn đề của một bằng chứng (EvidenceIssue.Kind). Đây là mã ổn định để tầng trên
// (CLI, TUI) dựng câu theo ngôn ngữ và chọn gợi ý đúng ca; gói knowledge không viết lệnh.
const (
	IssueMissingFile   = "missing_file"   // tệp không còn (xoá hoặc đổi tên mà không tìm thấy nơi mới)
	IssueOutOfRange    = "out_of_range"   // vùng dòng vượt quá số dòng của tệp và không tìm lại được
	IssueChanged       = "changed"        // nội dung tại vùng tham chiếu đã đổi
	IssueSymbolGone    = "symbol_gone"    // ký hiệu không còn trong tệp
	IssueBadRange      = "bad_range"      // vùng dòng/fingerprint không hợp lệ
	IssueNoFingerprint = "no_fingerprint" // bằng chứng chưa có fingerprint
	IssueRelocated     = "relocated"      // cùng tệp, vùng dòng dời chỗ, nội dung không đổi
	IssueMoved         = "moved"          // khối cùng fingerprint nằm ở tệp khác
	IssueAmbiguous     = "ambiguous"      // khối cùng fingerprint khớp nhiều nơi: không chọn được
)

// IssueKinds liệt kê mọi loại vấn đề (cho test và bảng dịch).
var IssueKinds = []string{IssueMissingFile, IssueOutOfRange, IssueChanged, IssueSymbolGone, IssueBadRange,
	IssueNoFingerprint, IssueRelocated, IssueMoved, IssueAmbiguous}

// Cách sửa (EvidenceIssue.Fix): thao tác người dùng cần làm, không phải lệnh.
const (
	FixApply   = "apply"   // áp vị trí mới (verify --apply)
	FixRefresh = "refresh" // xem lại nội dung rồi làm mới fingerprint (approve --refresh)
	FixEdit    = "edit"    // sửa bằng chứng của mục hoặc từ chối mục
	FixNone    = "none"    // không cần làm gì với bằng chứng này
)

// Fixes liệt kê mọi cách sửa (cho test và bảng dịch).
var Fixes = []string{FixApply, FixRefresh, FixEdit, FixNone}

// EvidenceIssue là một vấn đề của bằng chứng, dạng có cấu trúc: loại cùng tham số,
// không có câu chữ cho người dùng.
type EvidenceIssue struct {
	// Index là vị trí của bằng chứng trong mục.
	Index int    `json:"index"`
	Kind  string `json:"kind"`
	// Path, Lines, Symbol là bằng chứng như đã ghi.
	Path   string `json:"path"`
	Lines  string `json:"lines,omitempty"`
	Symbol string `json:"symbol,omitempty"`
	// FileLines là số dòng hiện có của tệp (out_of_range, changed vùng vượt quá); 0 = không biết.
	FileLines int `json:"file_lines,omitempty"`
	// NewPath/NewLines là vị trí mới (relocated, moved).
	NewPath  string `json:"new_path,omitempty"`
	NewLines string `json:"new_lines,omitempty"`
	// Candidates là các vị trí "path:lines" khớp (ambiguous), tối đa maxCandidates.
	Candidates []string `json:"candidates,omitempty"`
	// Truncated: tìm chuyển tệp dừng vì hết giới hạn chi phí nên "không thấy" chưa chắc là không có.
	Truncated bool `json:"truncated,omitempty"`
}

// Fix trả về cách sửa phù hợp với loại vấn đề.
func (i EvidenceIssue) Fix() string {
	switch i.Kind {
	case IssueRelocated, IssueMoved:
		return FixApply
	case IssueChanged:
		return FixRefresh
	case IssueNoFingerprint:
		return FixNone
	}
	return FixEdit
}

// Ref trình bày vị trí cũ dạng path[:lines][#symbol].
func (i EvidenceIssue) Ref() string {
	return Evidence{Path: i.Path, Lines: i.Lines, Symbol: i.Symbol}.String()
}

// NewRef trình bày vị trí mới dạng path[:lines]; rỗng nếu không có.
func (i EvidenceIssue) NewRef() string {
	if i.NewPath == "" && i.NewLines == "" {
		return ""
	}
	p := i.NewPath
	if p == "" {
		p = i.Path
	}
	return Evidence{Path: p, Lines: i.NewLines}.String()
}

// Describe là câu tiếng Việt trung tính (không có lệnh bint) cho JSON/MCP/lý do trạng thái.
// CLI và TUI dựng câu riêng từ các trường, không dùng hàm này.
func (i EvidenceIssue) Describe() string {
	r := i.Ref()
	switch i.Kind {
	case IssueMissingFile:
		return fmt.Sprintf("bằng chứng %s: không có tệp", r)
	case IssueOutOfRange:
		return fmt.Sprintf("bằng chứng %s: tệp còn %d dòng, không tìm lại được vùng %s", r, i.FileLines, i.Lines)
	case IssueChanged:
		return fmt.Sprintf("bằng chứng %s đã thay đổi kể từ khi ghi", r)
	case IssueSymbolGone:
		return fmt.Sprintf("bằng chứng %s: không còn tìm thấy ký hiệu %q", r, i.Symbol)
	case IssueBadRange:
		return fmt.Sprintf("bằng chứng %s: vùng dòng không hợp lệ", r)
	case IssueNoFingerprint:
		return fmt.Sprintf("bằng chứng %s chưa có fingerprint (không phát hiện được khi lỗi thời)", r)
	case IssueRelocated:
		return fmt.Sprintf("bằng chứng %s: nội dung không đổi, dịch sang %s", r, i.NewRef())
	case IssueMoved:
		return fmt.Sprintf("bằng chứng %s: nội dung không đổi, đã chuyển sang %s", r, i.NewRef())
	case IssueAmbiguous:
		return fmt.Sprintf("bằng chứng %s: nội dung có ở %d nơi (%s), không chọn được", r, len(i.Candidates), strings.Join(i.Candidates, ", "))
	}
	return fmt.Sprintf("bằng chứng %s: %s", r, i.Kind)
}

// EvidenceIssueError là lỗi có cấu trúc khi tính fingerprint không được: Error() giữ chữ
// cũ, tầng trên lấy Issue bằng errors.As.
type EvidenceIssueError struct {
	Issue EvidenceIssue
	msg   string
}

func (e *EvidenceIssueError) Error() string { return e.msg }

// IssueOf lấy vấn đề có cấu trúc từ một phát hiện (nil nếu không phải phát hiện bằng chứng).
func (f Finding) IssueOf() (EvidenceIssue, bool) {
	if f.Evidence == nil {
		return EvidenceIssue{}, false
	}
	return *f.Evidence, true
}
