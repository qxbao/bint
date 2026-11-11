package hookcore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/ctxengine"
	"bint/internal/knowledge"
)

// TestEvalPromptHookInjectsLessonsWithoutKnowledge: hook theo đề bài của lượt đánh giá (nhánh bint)
// chèn phần bài học từ thất bại kể cả khi gói tri thức thường rỗng (kế hoạch E5).
func TestEvalPromptHookInjectsLessonsWithoutKnowledge(t *testing.T) {
	dir := t.TempDir()
	items := filepath.Join(dir, "items")
	if err := os.MkdirAll(items, 0o755); err != nil {
		t.Fatal(err)
	}
	lesson := &knowledge.Item{Schema: knowledge.SchemaVersion, ID: "01M74BAFEAP3EAS71818R7KQ0T", Type: knowledge.TypeLesson,
		Status: knowledge.StatusVerified, Title: "Chạy test trước khi kết thúc", Body: "Luôn chạy test của vùng đã sửa.", Version: 1,
		Origin:    knowledge.Origin{Kind: knowledge.OriginAgent, Provenance: knowledge.ProvenanceFailureLesson, Runs: []string{"ea_1"}},
		CreatedAt: "2026-11-11T00:00:00Z"}
	data, err := lesson.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(items, lesson.ID+".md"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := ctxengine.EvalConfig{ItemsDir: items, Root: dir, Log: filepath.Join(dir, "log", "ctx.jsonl"),
		MaxTokens: 6000, PathMaxItems: 5, LessonGlobalK: 3, LessonMaxTokens: 600}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "eval-context.json")
	if err := os.WriteFile(cfgPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := handleEvalPrompt(Input{Prompt: "Sửa mã và chạy test", SessionID: "s1"},
		Dialect{Harness: "claude", PromptOutput: ClaudeStylePromptOutput}, cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "Chạy test trước khi kết thúc") || !strings.Contains(res.Output, "Bài học từ thất bại") {
		t.Fatalf("hook phải chèn bài học chung dù không có tri thức thường: %q (bỏ qua: %q)", res.Output, res.Skipped)
	}
}
