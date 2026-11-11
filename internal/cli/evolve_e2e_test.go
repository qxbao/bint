package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"bint/internal/spool"
	"bint/internal/testutil"
	"bint/internal/ulid"
)

func stateRoot(t *testing.T, root string) string {
	t.Helper()
	out, code := run(t, root, "", "status")
	if code != 0 {
		t.Fatalf("status: %d\n%s", code, out)
	}
	for _, l := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(l, "Trạng thái:"); ok {
			return strings.TrimSpace(v)
		}
	}
	t.Fatalf("không thấy thư mục trạng thái:\n%s", out)
	return ""
}

// session ghi các sự kiện của một phiên Claude Code vào spool, như hook làm.
func session(t *testing.T, spoolDir, id string, at time.Time, rating string, modified []string, retrieved []string) {
	t.Helper()
	ev := func(typ string, ts time.Time, p map[string]any) spool.Event {
		return spool.Event{EventID: ulid.New(), Type: typ, Source: "hook:claude", Timestamp: ts, Harness: "claude", SessionID: id, Payload: p}
	}
	evs := []spool.Event{ev(spool.SessionStarted, at, map[string]any{"model": "claude-opus-5-5"})}
	for _, f := range modified {
		evs = append(evs, ev(spool.FileModified, at.Add(time.Minute), map[string]any{"path": f}))
	}
	if len(retrieved) > 0 {
		ids := []any{}
		for _, r := range retrieved {
			ids = append(ids, r)
		}
		evs = append(evs, ev(spool.ContextRetrieved, at.Add(2*time.Minute), map[string]any{"channel": "hook:path", "ids": ids}))
	}
	if rating != "" {
		evs = append(evs, ev(spool.RunRated, at.Add(4*time.Minute), map[string]any{"rating": rating}))
	}
	evs = append(evs, ev(spool.SessionEnded, at.Add(3*time.Minute), map[string]any{"reason": "exit"}))
	if _, err := spool.Write(spoolDir, evs...); err != nil {
		t.Fatal(err)
	}
}

func TestEvolveEndToEnd(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{
		"go.mod":             "module demo\n",
		"internal/api/h.go":  "package api\n\nfunc Handle() {}\n",
		"internal/core/x.go": "package core\n\n// Retry phải gọi Reset trước.\nfunc Retry() {}\n\nfunc Reset() {}\n",
	})
	testutil.Git(t, root, "config", "user.name", "Rev")
	testutil.Git(t, root, "config", "user.email", "rev@example.com")
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %s", out)
	}
	out, code := run(t, root, "", "knowledge", "add", "--type", "warning", "--title", "Handle không được chặn",
		"--paths", "internal/api/**", "--evidence", "internal/api/h.go:3", "--json", "Handle chạy trên luồng chính.")
	var added struct{ ID string }
	if code != 0 || json.Unmarshal([]byte(out), &added) != nil || added.ID == "" {
		t.Fatalf("knowledge add: %d\n%s", code, out)
	}

	sp := filepath.Join(stateRoot(t, root), "spool")
	base := time.Now().Add(-6 * time.Hour)
	core := []string{"internal/core/x.go"}
	session(t, sp, "s1", base, "good", core, []string{added.ID})
	session(t, sp, "s2", base.Add(time.Hour), "bad", core, []string{added.ID})
	session(t, sp, "s3", base.Add(2*time.Hour), "bad", core, []string{added.ID})

	out, code = run(t, root, "", "evolve", "scan", "--json")
	var scan struct {
		Runs, Labeled int
		Opportunities []struct {
			ID, Key, Kind, Generator string
			Ready                    bool
		}
		Prerequisites struct{ Quantitative bool }
	}
	if code != 0 || json.Unmarshal([]byte(out), &scan) != nil {
		t.Fatalf("scan: %d\n%s", code, out)
	}
	ops := map[string]string{}
	for _, o := range scan.Opportunities {
		if o.Ready {
			ops[o.Kind] = o.ID
		}
	}
	if scan.Runs != 3 || scan.Labeled != 3 || ops["unused"] == "" || ops["failure_hotspot"] == "" || ops["exploration"] == "" || scan.Prerequisites.Quantitative {
		t.Fatalf("scan: %+v", scan)
	}
	if out, code := run(t, root, "", "evolve", "scan"); code != 0 || !strings.Contains(out, "CHƯA được kiểm chứng định lượng") {
		t.Fatalf("scan text: %d\n%s", code, out)
	}

	// roles.reflector khác claude (chưa khoá được công cụ): chỉ cảnh báo và bỏ cơ hội failure_lesson,
	// đề xuất xác định vẫn chạy (không làm hỏng cả `bint evolve propose`).
	cfgPath := filepath.Join(root, ".bint", "config.yml")
	cfg, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	patched := regexp.MustCompile(`(?m)^(\s*reflector:\s*)\S+`).ReplaceAllString(string(cfg), "${1}codex")
	if patched == string(cfg) {
		t.Fatalf("config.yml không có roles.reflector để đặt:\n%s", cfg)
	}
	if err := os.WriteFile(cfgPath, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code = run(t, root, "", "evolve", "propose", ops["unused"])
	var dep string
	for _, f := range strings.Fields(lineWith(out, "+ ep_")) {
		if strings.HasPrefix(f, "ep_") {
			dep = f
		}
	}
	if code != 0 || dep == "" || !strings.Contains(out, "không dùng được agent reflector") {
		t.Fatalf("propose với reflector=codex: %d\n%s", code, out)
	}
	if err := os.WriteFile(cfgPath, cfg, 0o644); err != nil {
		t.Fatal(err)
	}

	// Agent evolver giả cho điểm nóng thất bại.
	answer := `{"proposals":[{"type":"warning","title":"Gọi Reset trước Retry","body":"` + "`Retry`" + ` trong internal/core/x.go cần ` + "`Reset`" + ` trước.","paths":["internal/core/**"],"evidence":["internal/core/x.go:3-4"],"expected_benefit":"bớt lượt thất bại"}]}`
	resp, _ := json.Marshal(map[string]any{"type": "result", "result": answer})
	respFile := filepath.Join(t.TempDir(), "resp.json")
	os.WriteFile(respFile, resp, 0o644)
	out, code = run(t, root, "", "evolve", "propose", "--agent-command", "cat "+filepath.ToSlash(respFile), ops["failure_hotspot"])
	if code != 0 || !strings.Contains(out, "+ ep_") || !strings.Contains(out, "create:") {
		t.Fatalf("propose agent: %d\n%s", code, out)
	}
	out, _ = run(t, root, "", "evolve", "list")
	if strings.Count(out, "ep_") != 2 || !strings.Contains(out, "deprecate") || !strings.Contains(out, "create") {
		t.Fatalf("list:\n%s", out)
	}
	out, code = run(t, root, "", "evolve", "show", dep)
	if code != 0 || !strings.Contains(out, "Phiên bản gốc") || !strings.Contains(out, "non_inferiority") || !strings.Contains(out, added.ID) {
		t.Fatalf("show: %d\n%s", code, out)
	}

	// Không có tác vụ đánh giá: không kiểm chứng định lượng được.
	if out, code := run(t, root, "", "evolve", "evaluate", dep, "--yes"); code == 0 || !strings.Contains(out, "không tác vụ nào") {
		t.Fatalf("evaluate: %d\n%s", code, out)
	}
	// Áp dụng cần --unverified; agent không được áp dụng.
	if out, code := run(t, root, "", "evolve", "apply", dep); code != 1 || !strings.Contains(out, "--unverified") {
		t.Fatalf("apply: %d\n%s", code, out)
	}
	asAgent(t)
	if out, code := run(t, root, "", "evolve", "apply", dep, "--unverified"); code == 0 || !strings.Contains(out, "chỉ dành cho người dùng") {
		t.Fatalf("agent apply: %d\n%s", code, out)
	}
	asHuman(t)
	out, code = run(t, root, "", "evolve", "apply", dep, "--unverified", "-r", "đã xem")
	if code != 0 || !strings.Contains(out, "kiểm chứng định lượng: false") {
		t.Fatalf("apply: %d\n%s", code, out)
	}
	if out, _ := run(t, root, "", "knowledge", "show", added.ID); !strings.Contains(out, "deprecated") {
		t.Fatalf("mục chưa bị loại bỏ:\n%s", out)
	}
	out, code = run(t, root, "", "evolve", "history", "--json")
	var hist []struct {
		Action   string
		Verified bool
	}
	if code != 0 || json.Unmarshal([]byte(out), &hist) != nil || len(hist) != 1 || hist[0].Action != "apply" || hist[0].Verified {
		t.Fatalf("history: %d\n%s", code, out)
	}
	// Vòng P8: đề xuất chưa kiểm chứng định lượng không được đối chiếu hay tự áp dụng.
	if out, code := run(t, root, "", "evolve", "cycle", "--budget", "3", "--no-propose"); code != 0 || !strings.Contains(out, "Vòng ec_") || strings.Contains(out, "Tự áp dụng") {
		t.Fatalf("cycle: %d\n%s", code, out)
	}
	if out, _ := run(t, root, "", "evolve", "history"); !strings.Contains(out, "Vòng tự động (bint evolve cycle): 1 vòng") {
		t.Fatalf("history thiếu chi phí vòng:\n%s", out)
	}
	if out, code := run(t, root, "", "evolve", "rollback", dep); code != 0 || !strings.Contains(out, "Đã quay lui") {
		t.Fatalf("rollback: %d\n%s", code, out)
	}
	if out, _ := run(t, root, "", "knowledge", "show", added.ID); strings.Contains(out, "deprecated") {
		t.Fatalf("rollback không khôi phục:\n%s", out)
	}
}

// lineWith trả về dòng đầu tiên của out chứa sub (rỗng nếu không có).
func lineWith(out, sub string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, sub) {
			return l
		}
	}
	return ""
}
