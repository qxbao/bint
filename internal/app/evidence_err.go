package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"bint/internal/knowledge"
)

// EvidenceError là lỗi duyệt mục có bằng chứng lệch: mang ID mục và các vấn đề có cấu trúc để
// CLI/TUI tự dựng câu (đúng ngôn ngữ, gợi ý đúng lệnh hoặc phím cho từng ca). Error() là câu
// tiếng Việt trung tính, không có lệnh bint.
type EvidenceError struct {
	ID string
	// Status là trạng thái hiện tại của mục.
	Status string
	// File là đường dẫn tệp của mục (tương đối với thư mục gốc, dấu '/'), nơi sửa bằng chứng.
	File   string
	Issues []knowledge.EvidenceIssue
}

// Fix trả về cách sửa nặng nhất trong các vấn đề: edit hơn apply hơn refresh. Cách sửa này
// luôn khả thi: refresh chỉ được trả khi mọi vấn đề đều sửa được bằng làm mới fingerprint.
func (e *EvidenceError) Fix() string {
	fix := knowledge.FixNone
	rank := map[string]int{knowledge.FixNone: 0, knowledge.FixRefresh: 1, knowledge.FixApply: 2, knowledge.FixEdit: 3}
	for _, i := range e.Issues {
		if f := i.Fix(); rank[f] > rank[fix] {
			fix = f
		}
	}
	return fix
}

func (e *EvidenceError) Error() string {
	var parts []string
	for _, i := range e.Issues {
		parts = append(parts, i.Describe())
	}
	return fmt.Sprintf("mục %s: %s", e.ID, strings.Join(parts, "; "))
}

// evidenceIssues lấy các vấn đề bằng chứng mức error từ kết quả kiểm tra (bỏ trùng theo
// vị trí bằng chứng và loại). ok=false nếu có lỗi không phải lỗi bằng chứng: khi đó người
// gọi giữ đường lỗi chung.
func evidenceIssues(r knowledge.Result) (issues []knowledge.EvidenceIssue, ok bool) {
	type key struct {
		idx  int
		kind string
	}
	seen := map[key]bool{}
	for _, f := range r.Findings {
		if f.Level != knowledge.LevelError {
			continue
		}
		iss, has := f.IssueOf()
		if !has {
			return nil, false
		}
		if k := (key{iss.Index, iss.Kind}); !seen[k] {
			seen[k] = true
			issues = append(issues, iss)
		}
	}
	return issues, len(issues) > 0
}

func (k *KnowledgeWS) evidenceError(it *knowledge.Item, issues []knowledge.EvidenceIssue) *EvidenceError {
	file := k.Store.PathFor(it.ID)
	if rel, err := filepath.Rel(k.Root, file); err == nil {
		file = rel
	}
	return &EvidenceError{ID: it.ID, Status: it.Status, File: filepath.ToSlash(file), Issues: issues}
}
