package evolve

import (
	"strings"
	"testing"

	"bint/internal/knowledge"
)

// Đầu vào của reflector chỉ là các lượt của nhóm và bài học sẵn có CÙNG phạm vi (E2).
func TestReflectorPromptScopesInputs(t *testing.T) {
	mkLesson := func(id, status string, paths ...string) *knowledge.Item {
		return &knowledge.Item{ID: id, Type: knowledge.TypeLesson, Status: status, Title: "Bài " + id,
			Body: "nội dung " + id, Scope: knowledge.Scope{Paths: paths}}
	}
	items := []*knowledge.Item{
		mkLesson("LG", knowledge.StatusVerified),                       // chung
		mkLesson("LC", knowledge.StatusVerified, "internal/core/**"),   // cùng vùng
		mkLesson("LA", knowledge.StatusVerified, "internal/api/**"),    // vùng khác
		mkLesson("LD", knowledge.StatusDeprecated, "internal/core/**"), // đã bỏ
		{ID: "WN", Type: knowledge.TypeWarning, Status: knowledge.StatusVerified, Title: "không phải bài học"},
	}
	got := activeLessons(items, "internal/core")
	var ids []string
	for _, it := range got {
		ids = append(ids, it.ID)
	}
	if strings.Join(ids, ",") != "LG,LC" {
		t.Fatalf("bài học sẵn có cùng phạm vi = %v, muốn [LG LC]", ids)
	}

	fails := []Failure{sampleFailure(t, "ea_1", "t1", "internal/core"), sampleFailure(t, "ea_2", "t2", "internal/api")}
	o := Opportunity{Key: failureAreaPrefix + "internal/core", Kind: KindFailureLesson, Summary: "nhóm thử"}
	prompt := reflectorPrompt(o, fails, got)
	for _, want := range []string{"--- lượt ea_1 ---", "--- lượt ea_2 ---", "LG", "LC", "domain:<glob>"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt thiếu %q", want)
		}
	}
	if strings.Contains(prompt, "LA") || strings.Contains(prompt, "LD") {
		t.Fatalf("prompt chứa bài học ngoài phạm vi hoặc đã bỏ:\n%s", prompt)
	}
}
