package evolve

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"bint/internal/eval"
	"bint/internal/store"
	"bint/internal/testutil"
)

// testSetMarker là chuỗi đánh dấu đặt trong đề bài, nhật ký và tệp kiểm thử của TẬP TEST.
// Kiểm thử E2E khẳng định nó không bao giờ lọt vào đầu vào của agent reflector (kế hoạch E6).
const testSetMarker = "ZZTESTSETMARKERZZ"

// fakeReflector là agent giả: ghi lại đầu vào (đề bài đã dựng) và thư mục làm việc, trả đầu ra
// cố định. Không gọi mô hình.
type fakeReflector struct {
	reply     string
	prompts   []string
	dirEmpty  []bool
	reflected int
}

func (f *fakeReflector) Name() string                   { return "fake-reflector" }
func (f *fakeReflector) Version(context.Context) string { return "" }
func (f *fakeReflector) ReportsCost() bool              { return false }

func (f *fakeReflector) Run(_ context.Context, req eval.AgentRequest) eval.AgentResult {
	f.reflected++
	f.prompts = append(f.prompts, req.Prompt)
	entries, err := os.ReadDir(req.Dir)
	f.dirEmpty = append(f.dirEmpty, err == nil && len(entries) == 0)
	return eval.AgentResult{Text: f.reply}
}

// attemptLogs ghi nhật ký của một lượt đánh giá (agent.log, test.log, files.txt) vào logDir.
// Đường dẫn trong vết dùng thư mục làm việc tương ứng runs/ (như runAttempt).
func attemptLogs(t *testing.T, logDir string, agentLog, testLog string, files []string) {
	t.Helper()
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatal(err)
	}
	work := strings.Replace(filepath.ToSlash(logDir), "/logs/", "/runs/", 1) + "/"
	agent := strings.ReplaceAll(agentLog, "/RUN/", work)
	for name, body := range map[string]string{"agent.log": agent, "test.log": testLog,
		eval.FilesListName: strings.Join(files, "\n") + "\n"} {
		if err := os.WriteFile(filepath.Join(logDir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// insertAttempt ghi một lượt đánh giá đã thất bại vào evaluation_runs.
func insertAttempt(t *testing.T, f *fixture, batch, id, caseID, split, logDir string) {
	t.Helper()
	if _, err := f.db.Exec(`INSERT INTO evaluation_runs (attempt_id, batch_id, case_id, case_fingerprint, case_split, arm,
		repeat_index, status, harness, started_at, log_dir) VALUES (?, ?, ?, 'fp', ?, 'bint', 0, 'failed', 'claude', ?, ?)`,
		id, batch, caseID, split, store.FormatTime(now), logDir); err != nil {
		t.Fatal(err)
	}
}

// commitChanges ghi các tệp rồi commit; trả về SHA của commit.
func commitChanges(t *testing.T, root string, files map[string]string, msg string) string {
	t.Helper()
	for p, body := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	testutil.Git(t, root, "add", "-A")
	testutil.Git(t, root, "commit", "-m", msg)
	return strings.TrimSpace(testutil.Git(t, root, "rev-parse", "HEAD"))
}

// seedLessonFailures dựng repository tạm có ba tác vụ VALIDATION thất bại và một tác vụ TEST thất
// bại mang chuỗi đánh dấu, nạp các lượt vào evaluation_runs rồi quét cơ hội. Trả về tác vụ đánh
// giá (cả tập test), kết quả quét và lịch sử. Dùng chung cho kiểm thử propose và deferred.
func seedLessonFailures(t *testing.T, f *fixture) ([]*eval.Case, ScanResult, *History) {
	t.Helper()
	ctx := context.Background()

	// Lịch sử: commit gốc (HEAD hiện tại) và hai commit lời giải.
	base := strings.TrimSpace(testutil.Git(t, f.root, "rev-parse", "HEAD"))
	solCore := commitChanges(t, f.root, map[string]string{
		"internal/core/x.go":      "package core\n\n// Retry phải gọi Reset trước.\nfunc Retry() {}\n\nfunc Reset() {}\n\nfunc computeQuotaBudget() int { return 42 }\n",
		"internal/core/x_test.go": "package core\n\nimport \"testing\"\n\nfunc TestRetryMarker(t *testing.T) {}\n",
	}, "lời giải core")
	solAPI := commitChanges(t, f.root, map[string]string{
		"internal/api/h.go":      "package api\n\nfunc Handle() {}\n\nfunc Flush() {}\n",
		"internal/api/h_test.go": "package api\n\nimport \"testing\"\n\nfunc TestFlush(t *testing.T) {}\n",
	}, "lời giải api")

	cases := []*eval.Case{
		{ID: "c-val-1", Split: eval.SplitValidation, Prompt: "Sửa Retry để gọi Reset.", Source: eval.Source{Base: base, Commit: solCore},
			SourceFiles: []string{"internal/core/x.go"}, TestFiles: []string{"internal/core/x_test.go"}},
		{ID: "c-val-2", Split: eval.SplitValidation, Prompt: "Thêm ngân sách truy vấn.", Source: eval.Source{Base: base, Commit: solCore},
			SourceFiles: []string{"internal/core/x.go"}, TestFiles: []string{"internal/core/x_test.go"}},
		{ID: "c-val-3", Split: eval.SplitValidation, Prompt: "Hoàn tất Flush.", Source: eval.Source{Base: base, Commit: solAPI},
			SourceFiles: []string{"internal/api/h.go"}, TestFiles: []string{"internal/api/h_test.go"}},
		{ID: "c-test-1", Split: eval.SplitTest, Prompt: "Đề bài tập test " + testSetMarker, Source: eval.Source{Base: base, Commit: solCore},
			SourceFiles: []string{"internal/core/x.go"}, TestFiles: []string{"internal/core/x_test.go"}},
	}

	batchDir := filepath.Join(t.TempDir(), "logs", "eb-1")
	// Ba lượt validation: hai ở vùng core, một ở vùng api; nhật ký không có chạy test.
	for _, id := range []string{"c-val-1", "c-val-2", "c-val-3"} {
		dir := filepath.Join(batchDir, id)
		files := []string{"internal/core/x.go"}
		if id == "c-val-3" {
			files = []string{"internal/api/h.go"}
		}
		attemptLogs(t, dir, sampleAgentLog, "--- FAIL: TestRetry (0.00s)\nFAIL\tdemo/internal/core\t0.01s\n", files)
	}
	testLogs := filepath.Join(batchDir, "c-test-1")
	attemptLogs(t, testLogs, sampleAgentLog, "--- FAIL: TestSecret "+testSetMarker+" (0.00s)\n", []string{"internal/core/x.go"})

	if _, err := f.db.Exec(`INSERT INTO eval_batches (batch_id, status, harness, leakage_mode, arms, repeats, case_ids,
		prompt_version, seed, started_at) VALUES ('eb-1', 'completed', 'claude', 'economy', '["bint"]', 1, '[]', 1, 1, ?)`,
		store.FormatTime(now)); err != nil {
		t.Fatal(err)
	}
	insertAttempt(t, f, "eb-1", "ea-v1", "c-val-1", eval.SplitValidation, filepath.Join(batchDir, "c-val-1"))
	insertAttempt(t, f, "eb-1", "ea-v2", "c-val-2", eval.SplitValidation, filepath.Join(batchDir, "c-val-2"))
	insertAttempt(t, f, "eb-1", "ea-v3", "c-val-3", eval.SplitValidation, filepath.Join(batchDir, "c-val-3"))
	insertAttempt(t, f, "eb-1", "ea-t1", "c-test-1", eval.SplitTest, testLogs)

	items, _, err := f.k.Items()
	if err != nil {
		t.Fatal(err)
	}
	h, err := LoadHistory(ctx, f.db, now, 90*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	scan, err := Scan(ctx, ScanInput{DB: f.db, Cfg: f.k.Config, Items: items, Cases: cases, Now: now, History: h, Runtime: f.k.Paths.Runtime})
	if err != nil {
		t.Fatal(err)
	}
	return cases, scan, h
}

// TestLessonProposalEndToEnd (E6, không tốn token): repository tạm có ba tác vụ VALIDATION thất
// bại và một tác vụ TEST thất bại mang chuỗi đánh dấu trong đề bài, nhật ký và tệp kiểm thử.
// Agent reflector giả (đầu vào và thư mục được ghi lại) phải:
//   - không bao giờ thấy chuỗi đánh dấu tập test hay lượt của tập test;
//   - làm việc trong thư mục rỗng (không đọc được mã);
//   - cho ra một đề xuất create lesson (bài học chung), còn bài học riêng nêu định danh lời giải
//     bị loại ở kiểm tra rò rỉ.
func TestLessonProposalEndToEnd(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	cases, scan, h := seedLessonFailures(t, f)

	// Tập test không bao giờ được nạp; ba lượt validation thì có.
	for _, fl := range scan.failures {
		if fl.ID == "ea-t1" || fl.Split != eval.SplitValidation {
			t.Fatalf("lượt tập test lọt vào đơn vị thất bại: %+v", fl)
		}
	}
	if len(scan.failures) != 3 {
		t.Fatalf("muốn 3 lượt validation, được %d", len(scan.failures))
	}
	// Nhóm dấu hiệu "không chạy test" gom ba tác vụ qua hai vùng → sẵn sàng.
	var sigOpp Opportunity
	for _, o := range scan.Opportunities {
		if o.Kind == KindFailureLesson && o.Key == failureSignalPrefix+SigNoTest {
			sigOpp = o
		}
	}
	if !sigOpp.Ready || !reflect.DeepEqual(sigOpp.SupportingRuns, []string{"ea-v1", "ea-v2", "ea-v3"}) {
		t.Fatalf("cơ hội bài học chung phải sẵn sàng với ba lượt validation: %+v", sigOpp)
	}

	// Bản tóm tắt của đợt: chỉ lượt validation, lượt tập test bị bỏ qua có ghi nhận.
	rep, err := DigestRef(ctx, f.db, cases, "eb-1", f.k.Paths.Runtime, 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Digests) != 3 || rep.Skipped != 1 {
		t.Fatalf("DigestRef: %d bản tóm tắt, bỏ qua %d (muốn 3 và 1)", len(rep.Digests), rep.Skipped)
	}
	for _, d := range rep.Digests {
		if strings.Contains(d.Text, testSetMarker) {
			t.Fatalf("bản tóm tắt chứa chuỗi tập test:\n%s", d.Text)
		}
	}

	reflector := &fakeReflector{reply: `{"lessons":[` +
		`{"scope":"global","trigger":"Khi sửa mã có test trong nhiều vùng","mistake":"Agent kết thúc mà không chạy test",` +
		`"instead":"Chạy go test của vùng đã sửa trước khi báo xong","evidence":["ea-v1","ea-v3"]},` +
		`{"scope":"domain:internal/core/**","trigger":"Khi sửa internal/core","mistake":"Bỏ qua tính ngân sách",` +
		`"instead":"Gọi computeQuotaBudget trước khi trả kết quả","evidence":["ea-v1","ea-v2"]},` +
		`{"scope":"domain:internal/core/**","trigger":"Chỉ là ứng viên thừa","mistake":"x","instead":"y","evidence":["ea-v1","ea-v2"]}` +
		`]}`}
	pr, err := Propose(ctx, ProposeInput{K: f.k, DB: f.db, Scan: scan, History: h, Refs: []string{sigOpp.ID}, Max: 1,
		Reflector: reflector, Cases: cases, RepoDir: f.root, Timeout: time.Minute, Now: now})
	if err != nil {
		t.Fatal(err)
	}

	// Đầu vào của reflector: không có chuỗi đánh dấu, không có lượt tập test, có lượt validation.
	if reflector.reflected != 1 {
		t.Fatalf("reflector được gọi %d lần, muốn 1", reflector.reflected)
	}
	prompt := reflector.prompts[0]
	if strings.Contains(prompt, testSetMarker) || strings.Contains(prompt, "ea-t1") || strings.Contains(prompt, "c-test-1") {
		t.Fatalf("đầu vào reflector lọt dữ liệu tập test:\n%s", prompt)
	}
	for _, want := range []string{"ea-v1", "ea-v2", "ea-v3"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("đầu vào reflector thiếu lượt validation %s", want)
		}
	}
	if !reflector.dirEmpty[0] {
		t.Fatal("reflector phải làm việc trong thư mục rỗng")
	}

	// Kết quả: một đề xuất create lesson (chung), và ứng viên nêu định danh lời giải bị loại.
	var created *Proposal
	var rejected []string
	for _, oc := range pr.Outcomes {
		rejected = append(rejected, oc.Rejected...)
		if oc.Proposal != nil {
			created = oc.Proposal
		}
	}
	if created == nil {
		t.Fatalf("không có đề xuất bài học: %+v", pr.Outcomes)
	}
	if len(created.Changes) != 1 || created.Changes[0].Op != ActionCreate || created.Changes[0].Draft.Type != "lesson" {
		t.Fatalf("đề xuất phải là create lesson: %+v", created.Changes)
	}
	meta := created.Changes[0].Draft.Lesson
	if meta == nil || meta.Provenance != "failure_lesson" || !reflect.DeepEqual(meta.Runs, []string{"ea-v1", "ea-v3"}) {
		t.Fatalf("nguồn gốc bài học sai: %+v", meta)
	}
	// Tác vụ nguồn = mọi tác vụ có bản tóm tắt trong prompt (ea-v2 của c-val-2 cũng vậy, dù
	// reflector không nêu nó làm bằng chứng): không được chấm lại bài học trên c-val-2 (mục 12.3).
	if !reflect.DeepEqual(created.EvalCases, []string{"c-val-1", "c-val-2", "c-val-3"}) {
		t.Fatalf("tác vụ nguồn sai (phải là mọi tác vụ validation có trong prompt của reflector): %v", created.EvalCases)
	}
	if !reflect.DeepEqual(meta.Runs, []string{"ea-v1", "ea-v3"}) {
		t.Fatalf("bằng chứng (Runs) phải chỉ là các lượt reflector nêu: %v", meta.Runs)
	}
	if !anyContains(rejected, "computeQuotaBudget") {
		t.Fatalf("ứng viên nêu định danh lời giải phải bị loại ở kiểm tra rò rỉ: %v", rejected)
	}
	if got, err := GetProposal(ctx, f.db, created.ID); err != nil || got.ID != created.ID {
		t.Fatalf("đề xuất chưa được lưu: %v", err)
	}
}

func anyContains(xs []string, sub string) bool {
	for _, x := range xs {
		if strings.Contains(x, sub) {
			return true
		}
	}
	return false
}

// sampleAgentLog là nhật ký stream-json mẫu, dùng cho các lượt trong kiểm thử E2E.
const sampleAgentLog = `{"type":"system","subtype":"init","session_id":"s1"}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/RUN/internal/core/x.go"}}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/RUN/internal/core/x.go","old_string":"a","new_string":"b"}}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"go build ./..."}}]}}
{"type":"result","subtype":"success","is_error":false,"num_turns":3,"total_cost_usd":0.05,"result":"Xong."}
`
