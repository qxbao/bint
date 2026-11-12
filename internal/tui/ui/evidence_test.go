package ui

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"bint/internal/app"
	"bint/internal/knowledge"
)

var viLettersUI = regexp.MustCompile(`(?i)[àáạảãăằắặẳẵâầấậẩẫèéẹẻẽêềếệểễìíịỉĩòóọỏõôồốộổỗơờớợởỡùúụủũưừứựửữỳýỵỷỹđ]`)

func issueFor(kind string) knowledge.EvidenceIssue {
	i := knowledge.EvidenceIssue{Kind: kind, Path: "internal/cli/eval.go", Lines: "11", Symbol: "Run"}
	switch kind {
	case knowledge.IssueOutOfRange:
		i.FileLines = 4
	case knowledge.IssueRelocated:
		i.NewLines = "13"
	case knowledge.IssueMoved:
		i.NewPath, i.NewLines = "internal/app/eval_run.go", "4"
	case knowledge.IssueAmbiguous:
		i.Candidates = []string{"a.go:2", "b.go:2"}
		i.Truncated = true
	}
	return i
}

// Mọi loại vấn đề dựng được ở vi và en; gợi ý phím đúng theo cách sửa: không bao giờ phím f trừ khi cách sửa là refresh.
func TestEvidenceLinesEveryKindBothLanguages(t *testing.T) {
	vi, en := cats(t)
	keys := map[string]map[string][]string{
		"vi": {knowledge.FixApply: {"phím v ở Tri thức [ô Áp dụng]"}, knowledge.FixRefresh: {"phím Enter ở Tri thức", "phím f ở Tri thức"},
			knowledge.FixEdit: {"phím e ở Tri thức", "phím r ở Tri thức"}},
		"en": {knowledge.FixApply: {"key v in Knowledge [field"}, knowledge.FixRefresh: {"key Enter in Knowledge", "key f in Knowledge"},
			knowledge.FixEdit: {"key e in Knowledge", "key r in Knowledge"}},
	}
	for lang, cat := range map[string]*Rewriter{"vi": {Cat: vi}, "en": {Cat: en}} {
		for _, kind := range knowledge.IssueKinds {
			iss := issueFor(kind)
			lines := EvidenceLines(*cat, iss)
			all := strings.Join(lines, "\n")
			if !strings.Contains(all, "internal/cli/eval.go:11") {
				t.Errorf("%s/%s: thiếu vị trí cũ:\n%s", lang, kind, all)
			}
			for _, want := range keys[lang][iss.Fix()] {
				if !strings.Contains(all, want) {
					t.Errorf("%s/%s: thiếu %q:\n%s", lang, kind, want, all)
				}
			}
			if iss.Fix() != knowledge.FixRefresh && (strings.Contains(all, "phím f") || strings.Contains(all, "key f ")) {
				t.Errorf("%s/%s: cách sửa %s không được gợi ý f:\n%s", lang, kind, iss.Fix(), all)
			}
			if strings.Contains(all, "bint ") || strings.Contains(all, "{") || strings.Contains(all, "kn.ev") {
				t.Errorf("%s/%s: lệnh CLI hoặc khoá/tham số thô:\n%s", lang, kind, all)
			}
			if lang == "en" && viLettersUI.MatchString(all) {
				t.Errorf("en/%s có chữ tiếng Việt:\n%s", kind, all)
			}
			// Phần tham số có cấu trúc.
			switch kind {
			case knowledge.IssueMoved:
				if !strings.Contains(all, "internal/app/eval_run.go:4") {
					t.Errorf("%s/moved: thiếu vị trí mới:\n%s", lang, all)
				}
			case knowledge.IssueOutOfRange:
				if !strings.Contains(all, "4") {
					t.Errorf("%s/out_of_range: thiếu số dòng tệp:\n%s", lang, all)
				}
			case knowledge.IssueAmbiguous:
				if !strings.Contains(all, "a.go:2, b.go:2") {
					t.Errorf("%s/ambiguous: thiếu ứng viên:\n%s", lang, all)
				}
			}
		}
	}
}

// Ca ambiguous phát sinh từ vùng vượt quá tệp giữ số dòng hiện có của tệp, ở vi và en (phát hiện .5); moved không in số dòng.
func TestEvidenceCauseAmbiguousKeepsFileLines(t *testing.T) {
	vi, en := cats(t)
	iss := issueFor(knowledge.IssueAmbiguous)
	iss.FileLines = 4
	for lang, want := range map[string]struct {
		cat  Rewriter
		text string
	}{"vi": {Rewriter{Cat: vi}, "tệp hiện có 4 dòng"}, "en": {Rewriter{Cat: en}, "the file now has 4 lines"}} {
		if got := strings.Join(EvidenceCause(want.cat, iss), "\n"); !strings.Contains(got, want.text) {
			t.Errorf("%s: thiếu %q:\n%s", lang, want.text, got)
		}
		if got := EvidenceText(want.cat, iss); !strings.Contains(got, want.text) {
			t.Errorf("%s: EvidenceText thiếu %q: %s", lang, want.text, got)
		}
		mv := issueFor(knowledge.IssueMoved)
		if got := strings.Join(EvidenceCause(want.cat, mv), "\n"); strings.Contains(got, want.text) {
			t.Errorf("%s: moved không được in số dòng tệp:\n%s", lang, got)
		}
	}
}

// ErrText của lỗi duyệt có cấu trúc: không đi qua Rewriter, một cách xử lý theo cách sửa nặng nhất, đúng ngôn ngữ.
func TestErrTextEvidenceError(t *testing.T) {
	vi, en := cats(t)
	ee := &app.EvidenceError{ID: "01ABC", File: ".bint/knowledge/items/01ABC.md",
		Issues: []knowledge.EvidenceIssue{issueFor(knowledge.IssueChanged), issueFor(knowledge.IssueMissingFile)}}
	wrapped := errors.Join(errors.New("ngữ cảnh"), ee)
	got := ErrText(Rewriter{Cat: vi}, wrapped)
	if !strings.Contains(got, "Không duyệt được") || !strings.Contains(got, "Cách xử lý: sửa bằng chứng") || strings.Contains(got, "phím f") {
		t.Errorf("vi (edit nặng hơn refresh): %s", got)
	}
	gotEn := ErrText(Rewriter{Cat: en}, wrapped)
	if !strings.HasPrefix(gotEn, "Cannot approve:") || !strings.Contains(gotEn, "How to fix: edit the evidence") || viLettersUI.MatchString(gotEn) || strings.Contains(gotEn, "key f ") {
		t.Errorf("en: %s", gotEn)
	}
	if got := ErrText(Rewriter{Cat: en, ReadOnly: true}, &app.EvidenceError{Issues: []knowledge.EvidenceIssue{issueFor(knowledge.IssueMoved)}}); !strings.Contains(got, "read-only") {
		t.Errorf("chỉ đọc phải ghi chú cho phím áp dụng: %s", got)
	}
}

// FindingText của finding có cấu trúc không đi qua Rewriter; finding thường vẫn dùng Detail.
func TestFindingTextStructured(t *testing.T) {
	vi, en := cats(t)
	iss := issueFor(knowledge.IssueMoved)
	f := knowledge.Finding{Level: knowledge.LevelError, Code: knowledge.CodeMoved, Detail: "chi tiết trung tính", Evidence: &iss}
	if got := FindingText(Rewriter{Cat: vi}, f); !strings.Contains(got, "Khối bằng chứng đã chuyển sang tệp khác") || !strings.Contains(got, "phím v ở Tri thức") || strings.Contains(got, "chi tiết trung tính") {
		t.Errorf("vi: %s", got)
	}
	if got := FindingText(Rewriter{Cat: en}, f); !strings.Contains(got, "moved to another file") || viLettersUI.MatchString(got) {
		t.Errorf("en: %s", got)
	}
	// Thêm mục: chưa có mục để duyệt hay sửa nên chỉ nêu nguyên nhân.
	if got := FindingCause(Rewriter{Cat: vi}, f); strings.Contains(got, "phím") || !strings.Contains(got, "internal/app/eval_run.go:4") {
		t.Errorf("cause: %s", got)
	}
	plain := knowledge.Finding{Level: knowledge.LevelError, Code: knowledge.CodeSchema, Detail: "thiếu tiêu đề"}
	if got := FindingText(Rewriter{Cat: vi}, plain); !strings.HasSuffix(got, "thiếu tiêu đề") {
		t.Errorf("finding thường: %s", got)
	}
}
