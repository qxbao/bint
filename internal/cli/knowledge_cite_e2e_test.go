package cli_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/store"
	"bint/internal/testutil"
)

// `bint knowledge cite` ghi sự kiện knowledge.cited vào spool với run_id từ BINT_RUN_ID; sau khi nạp spool thì
// knowledge_citations có dòng đúng run_id.
func TestKnowledgeCiteEndToEnd(t *testing.T) {
	stateDir := testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n", "Makefile": "gen:\n\tsqlc generate\n"})
	testutil.Git(t, root, "config", "user.name", "Rev")
	testutil.Git(t, root, "config", "user.email", "rev@example.com")
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	out, code := run(t, root, "", "knowledge", "add", "--type", "warning", "--evidence", "Makefile:1-2", "--json", "Chạy make gen sau khi sửa query")
	var added struct {
		ID string `json:"id"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &added) != nil || added.ID == "" {
		t.Fatalf("add: %d\n%s", code, out)
	}

	// Không có BINT_RUN_ID: lỗi rõ, không ghi gì.
	t.Setenv("BINT_RUN_ID", "")
	if out, code := run(t, root, "", "knowledge", "cite", added.ID); code == 0 || !strings.Contains(out, "BINT_RUN_ID") {
		t.Fatalf("thiếu BINT_RUN_ID: %d\n%s", code, out)
	}

	const runID = "01TESTRUNCITE0000000000000"
	t.Setenv("BINT_RUN_ID", runID)
	// Không có ID: lỗi cú pháp; ID không tồn tại (kể cả khi có ID đúng đi kèm): lỗi.
	if out, code := run(t, root, "", "knowledge", "cite"); code == 0 {
		t.Fatalf("thiếu ID phải lỗi: %s", out)
	}
	if out, code := run(t, root, "", "knowledge", "cite", "01NOSUCHITEM000000000000"); code == 0 || !strings.Contains(out, "01NOSUCHITEM000000000000") {
		t.Fatalf("ID sai: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "knowledge", "cite", added.ID, "[k:01NOSUCHITEM000000000000]"); code == 0 || !strings.Contains(out, "01NOSUCHITEM000000000000") {
		t.Fatalf("ID sai lẫn ID đúng: %d\n%s", code, out)
	}
	// Dạng [k:ID] và --note.
	out, code = run(t, root, "", "knowledge", "cite", "[k:"+added.ID+"]", "--note", "áp dụng ở Makefile", "--json")
	var res struct {
		RunID string   `json:"run_id"`
		IDs   []string `json:"ids"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &res) != nil || res.RunID != runID || len(res.IDs) != 1 || res.IDs[0] != added.ID {
		t.Fatalf("cite: %d\n%s", code, out)
	}

	// Nạp spool: dòng knowledge_citations đúng run_id, ID và ghi chú.
	t.Setenv("BINT_RUN_ID", "")
	if out, code := run(t, root, "", "events", "ingest"); code != 0 {
		t.Fatalf("events ingest: %d\n%s", code, out)
	}
	dbs, _ := filepath.Glob(filepath.Join(stateDir, "*", "state.sqlite"))
	if len(dbs) != 1 {
		t.Fatalf("CSDL trạng thái: %v", dbs)
	}
	s, err := store.Open(context.Background(), dbs[0])
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var n int
	var note string
	err = s.DB.QueryRow(`SELECT COUNT(*), COALESCE(MAX(note), '') FROM knowledge_citations WHERE run_id = ? AND item_id = ?`, runID, added.ID).Scan(&n, &note)
	if err != nil || n != 1 || note != "áp dụng ở Makefile" {
		t.Fatalf("knowledge_citations: n=%d note=%q err=%v", n, note, err)
	}
}
