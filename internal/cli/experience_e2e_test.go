package cli_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/app"
	"bint/internal/eval"
	"bint/internal/knowledge"
	"bint/internal/store"
	"bint/internal/testutil"
)

// TestExperienceEndToEnd: `bint experience show` liệt kê bài học (chung trước, riêng theo vùng)
// và `digest` báo lỗi rõ khi tham chiếu không phải đợt đánh giá hay lượt chạy.
func TestExperienceEndToEnd(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{
		"go.mod":             "module demo\n",
		"internal/core/x.go": "package core\n\nfunc X() {}\n",
	})
	testutil.Git(t, root, "config", "user.name", "Rev")
	testutil.Git(t, root, "config", "user.email", "rev@example.com")
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	writeFailureLesson(t, root, "01M74BAFEAP3EAS71818R7KQ01", "Khi sửa core nhớ gọi Reset", "Luôn gọi Reset trước khi kết thúc.", []string{"internal/core/**"})
	writeFailureLesson(t, root, "01M74BAFEAP3EAS71818R7KQ02", "Chạy test trước khi kết thúc", "Luôn chạy test của vùng đã sửa.", nil)
	// Lesson do người viết đi luồng tri thức thường: không có trong thư viện bài học.
	if out, code := run(t, root, "", "knowledge", "add", "--type", "lesson", "--title", "Bài học của người",
		"Ghi chú tự viết."); code != 0 {
		t.Fatalf("knowledge add (người): %d\n%s", code, out)
	}

	out, code := run(t, root, "", "experience", "show", "--json")
	var rows []struct {
		Title    string `json:"title"`
		Area     string `json:"area"`
		Inserted int    `json:"inserted"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &rows) != nil || len(rows) != 2 {
		t.Fatalf("experience show --json: %d\n%s", code, out)
	}
	if rows[0].Area != "" || rows[1].Area != "internal/core" || rows[0].Inserted != 0 {
		t.Fatalf("thứ tự chung→riêng sai: %+v", rows)
	}

	out, code = run(t, root, "", "experience", "show")
	if code != 0 || !strings.Contains(out, "Chung (mọi vùng mã)") || !strings.Contains(out, "Riêng — internal/core") {
		t.Fatalf("experience show text: %d\n%s", code, out)
	}

	out, code = run(t, root, "", "experience", "digest", "không-có-đợt")
	if code == 0 || !strings.Contains(out, "không tìm thấy đợt đánh giá hoặc lượt chạy") {
		t.Fatalf("digest tham chiếu lạ phải báo lỗi rõ: %d\n%s", code, out)
	}
}

// TestExperienceShowCountsRealInsertions: hook UserPromptSubmit chèn bài học từ thất bại →
// spool → ingest → `experience show` báo Inserted > 0 (sự kiện context.retrieved phải mang ID
// bài học, nhãn "lesson").
func TestExperienceShowCountsRealInsertions(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	testutil.Git(t, root, "config", "user.name", "Rev")
	testutil.Git(t, root, "config", "user.email", "rev@example.com")
	if out, code := run(t, root, "", "init", "--harness", "claude", "--hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	lesson := writeFailureLesson(t, root, "01M74BAFEAP3EAS71818R7KQ0T", "Chạy test trước khi kết thúc", "Luôn chạy test của vùng đã sửa.", nil)

	hook := func(m map[string]any) string {
		m["cwd"] = root
		m["session_id"] = "sess-lesson"
		b, _ := json.Marshal(m)
		out, code := run(t, root, string(b), "hook", "claude")
		if code != 0 {
			t.Fatalf("hook phải thoát 0: %d %q", code, out)
		}
		return out
	}
	hook(map[string]any{"hook_event_name": "SessionStart", "source": "startup", "model": "claude-e2e"})
	if out := hook(map[string]any{"hook_event_name": "UserPromptSubmit", "prompt": "Sửa mã và chạy test"}); !strings.Contains(out, "Chạy test trước khi kết thúc") {
		t.Fatalf("hook phải chèn bài học: %q", out)
	}
	hook(map[string]any{"hook_event_name": "SessionEnd", "reason": "other"})

	out, code := run(t, root, "", "experience", "show", "--json")
	var rows []struct {
		ID       string `json:"id"`
		Inserted int    `json:"inserted"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &rows) != nil || len(rows) != 1 {
		t.Fatalf("experience show --json: %d\n%s", code, out)
	}
	if rows[0].ID != lesson.ID || rows[0].Inserted != 1 {
		t.Fatalf("bài học được chèn ở lượt thật phải được đếm (Inserted=1): %+v", rows)
	}

	// Chưa có nhãn lượt: đạt/trượt theo nhãn lượt thật đều 0. Chấm "bad" → lượt có nhãn failure.
	out, code = run(t, root, "", "runs", "list", "--json")
	var runs []struct {
		RunID string `json:"run_id"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &runs) != nil || len(runs) != 1 {
		t.Fatalf("runs list: %d\n%s", code, out)
	}
	runID := runs[0].RunID
	showReal := func() (int, int) {
		t.Helper()
		out, code := run(t, root, "", "experience", "show", "--json")
		var rs []struct {
			RealSuccess int `json:"real_success"`
			RealFailure int `json:"real_failure"`
		}
		if code != 0 || json.Unmarshal([]byte(out), &rs) != nil || len(rs) != 1 {
			t.Fatalf("experience show --json: %d\n%s", code, out)
		}
		return rs[0].RealSuccess, rs[0].RealFailure
	}
	if ok, bad := showReal(); ok != 0 || bad != 0 {
		t.Fatalf("lượt chưa có nhãn không được tính đạt/trượt: %d/%d", ok, bad)
	}
	if out, code := run(t, root, "", "runs", "rate", runID, "bad"); code != 0 {
		t.Fatalf("runs rate: %d\n%s", code, out)
	}
	if ok, bad := showReal(); ok != 0 || bad != 1 {
		t.Fatalf("lượt nhãn failure có bài học được chèn: đạt/trượt = %d/%d, muốn 0/1", ok, bad)
	}
	out, code = run(t, root, "", "experience", "show")
	if code != 0 || !strings.Contains(out, "lượt thật: đạt 0, trượt 1") {
		t.Fatalf("experience show text phải có đạt/trượt theo nhãn lượt thật: %d\n%s", code, out)
	}

	// digest cho lượt thật vừa chấm failure.
	out, code = run(t, root, "", "experience", "digest", runID, "--json")
	var rep struct {
		Kind    string `json:"kind"`
		Ref     string `json:"ref"`
		Digests []struct {
			Text string `json:"text"`
		} `json:"digests"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &rep) != nil || rep.Kind != "run" || rep.Ref != runID || len(rep.Digests) != 1 || rep.Digests[0].Text == "" {
		t.Fatalf("experience digest <lượt>: %d\n%s", code, out)
	}
}

// TestExperienceDigestBatch: `experience digest` cho một đợt đánh giá chỉ tóm tắt lượt thất bại
// của tập validation và đếm riêng (không tóm tắt) lượt tập test.
func TestExperienceDigestBatch(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	testutil.Git(t, root, "config", "user.name", "Rev")
	testutil.Git(t, root, "config", "user.email", "rev@example.com")
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	cs := eval.Store{CasesDir: filepath.Join(root, ".bint", "evals", "cases")}
	for _, c := range []*eval.Case{
		{Schema: 1, ID: "c-val", Status: "approved", Split: eval.SplitValidation, Source: eval.Source{Base: "b1", Commit: "c1"}, Prompt: "Sửa Retry để không panic.",
			SourceFiles: []string{"internal/core/x.go"}, TestFiles: []string{"internal/core/x_test.go"}},
		{Schema: 1, ID: "c-test", Status: "approved", Split: eval.SplitTest, Source: eval.Source{Base: "b1", Commit: "c1"}, Prompt: "Đề bài tập test ZZTESTSETMARKERZZ",
			SourceFiles: []string{"internal/core/x.go"}, TestFiles: []string{"internal/core/x_test.go"}},
	} {
		if err := cs.SaveCase(c); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	k, err := app.OpenKnowledge(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	st, err := k.OpenStore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	at := store.FormatTime(time.Now())
	if _, err := st.DB.Exec(`INSERT INTO eval_batches (batch_id, status, harness, leakage_mode, arms, repeats, case_ids,
		prompt_version, seed, started_at) VALUES ('eb-9', 'completed', 'claude', 'economy', '["bint"]', 1, '[]', 1, 1, ?)`, at); err != nil {
		t.Fatal(err)
	}
	for _, a := range [][2]string{{"ea-val", "c-val"}, {"ea-test", "c-test"}} {
		split := eval.SplitValidation
		if a[1] == "c-test" {
			split = eval.SplitTest
		}
		if _, err := st.DB.Exec(`INSERT INTO evaluation_runs (attempt_id, batch_id, case_id, case_fingerprint, case_split, arm,
			repeat_index, status, harness, started_at, test_passed) VALUES (?, 'eb-9', ?, 'fp', ?, 'bint', 0, 'failed', 'claude', ?, 0)`,
			a[0], a[1], split, at); err != nil {
			t.Fatal(err)
		}
	}
	st.Close()

	out, code := run(t, root, "", "experience", "digest", "latest", "--json")
	var rep struct {
		Kind    string `json:"kind"`
		Ref     string `json:"ref"`
		Skipped int    `json:"skipped_not_validation"`
		Digests []struct {
			Text string `json:"text"`
		} `json:"digests"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &rep) != nil {
		t.Fatalf("experience digest latest: %d\n%s", code, out)
	}
	if rep.Kind != "batch" || rep.Ref != "eb-9" || len(rep.Digests) != 1 || rep.Skipped != 1 {
		t.Fatalf("đợt: muốn 1 bản tóm tắt validation và bỏ qua 1 lượt tập test: %s", out)
	}
	if strings.Contains(out, "ZZTESTSETMARKERZZ") {
		t.Fatalf("đề bài tập test không được lọt vào digest:\n%s", out)
	}
	if text, code := run(t, root, "", "experience", "digest", "eb-9"); code != 0 || !strings.Contains(text, "Đợt eb-9: 1 lượt thất bại trên tập validation") {
		t.Fatalf("digest text theo ID đợt: %d\n%s", code, text)
	}
}

// writeFailureLesson ghi trực tiếp một bài học từ thất bại (failure_lesson, có lượt nguồn) đã verified.
func writeFailureLesson(t *testing.T, root, id, title, body string, paths []string) *knowledge.Item {
	t.Helper()
	it := &knowledge.Item{Schema: knowledge.SchemaVersion, ID: id, Type: knowledge.TypeLesson,
		Status: knowledge.StatusVerified, Title: title, Body: body, Version: 1, Scope: knowledge.Scope{Paths: paths},
		Origin:    knowledge.Origin{Kind: knowledge.OriginAgent, Provenance: knowledge.ProvenanceFailureLesson, Runs: []string{"ea_1"}},
		CreatedAt: "2026-11-11T00:00:00Z"}
	data, err := it.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".bint", "knowledge", "items")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".md"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return it
}
