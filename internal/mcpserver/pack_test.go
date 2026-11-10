package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"bint/internal/ctxengine"
	"bint/internal/kindex"
	"bint/internal/knowledge"
)

type packBackend struct{ Backend }

func (packBackend) ForTask(context.Context, string, []string) (ctxengine.Pack, error) {
	return ctxengine.Pack{Mode: "task", Entries: []ctxengine.Entry{{ID: "01ABC", Type: "rule", Status: "approved", Label: ctxengine.LabelRule,
		Title: "Bọc chuỗi bằng _()", Body: "Thân mục tri thức"}}}, nil
}
func (packBackend) Search(context.Context, string, int) ([]kindex.Hit, error) { return nil, nil }
func (packBackend) Get(context.Context, string) (*knowledge.Item, error)      { return nil, nil }

// Claude Code chỉ đưa structuredContent cho mô hình khi có trường này: nội dung tri thức
// phải nằm trong đó, không chỉ ID.
func TestPackStructuredCarriesContent(t *testing.T) {
	s := New("bint", "test", Instructions)
	RegisterTools(s, packBackend{})
	out, rerr := s.dispatch(context.Background(), request{Method: "tools/call",
		Params: json.RawMessage(`{"name":"context_for_task","arguments":{"task":"x"}}`)})
	if rerr != nil {
		t.Fatal(rerr)
	}
	sc, _ := out.(map[string]any)["structuredContent"].(map[string]any)
	text, _ := sc["context"].(string)
	if !strings.Contains(text, "Thân mục tri thức") || !strings.Contains(text, "01ABC") {
		t.Fatalf("structuredContent thiếu nội dung: %+v", sc)
	}
}
